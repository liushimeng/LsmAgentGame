// Package werewolf — chat_recall.go: chat_recall 主动检索工具的服务端实现。
//
// 2026-08-13 §20260813-02 U3 新增。
// 灵感来源: OpenClaw Lane-1 memory_search(记忆 §4) —— 模型主动检索情景层,
// 零模型调用(规则式打分)、闭集参数校验(模型不能扩大召回边界)、冷却限流、
// 截断留可观测标记。
//
// 解决的问题: bot 的 LLM prompt 只能看到 500K chatQueue 窗口内「read pointer
// 之后」的增量条目,早期关键发言(如 R1 预言家跳身份)被 4 级压缩折叠后
// 永久不可召回。chat_recall 让 bot 在白天 speak/vote 阶段按关键词/座位/天数
// 主动检索队列全量条目。
//
// 隐私护栏(§119/§135):
//   - 只检索该 bot 的可见域 = 公开 chat + 发给它的 whisper + 活动事件;
//     他人之间的 whisper 物理剔除(双重防御:visibility 过滤 + 测试断言)。
//   - chatQueue 本就不含 HeartThought / 夜间私密信息(协议层隔离),
//     检索结果天然不含;测试做双保险断言。
//
// 闭集钳制(OpenClaw readCorpusParam 思想):
//   - query 截 50 字(rune);seat 越界 → 忽略(不报错);day 钳到 [1, currentDay]。
//   - 模型自撰参数不能扩大召回边界。
package werewolf

import (
	"fmt"
	"strings"
	"time"

	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// chat_recall 检索参数常量。
const (
	// chatRecallTopN 返回的最大条数。
	chatRecallTopN = 5
	// chatRecallTextMaxRunes 单条返回文本的 rune 上限。
	chatRecallTextMaxRunes = 120
	// chatRecallQueryMaxRunes query 参数的 rune 上限(闭集钳制)。
	chatRecallQueryMaxRunes = 50
	// chatRecallCooldown 是 per-bot 冷却窗口(agentRunner.ChatRecall 强制)。
	chatRecallCooldown = 60 * time.Second
)

// chatRecallHit 是一条命中条目的评分视图(仅内部使用)。
type chatRecallHit struct {
	seq         uint64
	day         int
	fromSeat    int
	fromAccount string
	kind        string // public | whisper | activity
	text        string
	score       int
}

// clampChatRecallParams 闭集钳制(纯函数,便于单测)。
// 返回 (截断后的 query, 合法化后的 filterSeat(-1=忽略), 合法化后的 day(0=忽略))。
func clampChatRecallParams(query string, filterSeat, day, currentDay, maxPlayers int) (string, int, int) {
	q := chatRecallTruncRunes(strings.TrimSpace(query), chatRecallQueryMaxRunes)
	fs := -1
	if filterSeat >= 0 && filterSeat < maxPlayers {
		fs = filterSeat
	}
	d := 0
	if day >= 1 {
		d = day
		if currentDay >= 1 && d > currentDay {
			d = currentDay
		}
	}
	return q, fs, d
}

// chatRecallTruncRunes rune 安全截断(超长跑补 "…")。
func chatRecallTruncRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// chatRecallEntryVisible 判定一条 chatQueue 条目对该 bot 座位是否可见。
// 可见域 = 公开 chat + 活动事件 + 收发任一方是本 bot 的 whisper。
// 他人之间的 whisper 物理剔除(§119 隐私护栏)。
func chatRecallEntryVisible(m agentcore.ChatMessage, seat int) bool {
	if !m.IsWhisper {
		return true
	}
	return m.ToSeat == seat || m.FromSeat == seat
}

// scoreChatRecallEntry 规则式打分(零模型调用):
// 关键词命中次数×2 + 座位匹配×3 + 天数匹配×2 + 条目类型加权
// (公开发言 2 > 私聊 1 > 活动事件 0)。
func scoreChatRecallEntry(m agentcore.ChatMessage, query string, filterSeat, day int) int {
	score := 0
	if query != "" {
		score += 2 * strings.Count(m.Text, query)
	}
	if filterSeat >= 0 && m.FromSeat == filterSeat {
		score += 3
	}
	if day > 0 && m.Round == day {
		score += 2
	}
	switch {
	case m.IsActivity:
		// +0
	case m.IsWhisper:
		score++
	default:
		score += 2
	}
	return score
}

// ChatRecall 是 chat_recall 工具的管理器入口(由 agentRunner.ChatRecall 调用)。
//
// 锁纪律(§92a):getRoom(m.mu.RLock)→ lockRoomBriefly 快照(chatQueue 全量
// Tail + currentDay)→ 解锁后在锁外打分排序,绝不持 r.mu 跨计算。
// chatQueue 有自己的锁,Tail(0) 返回防御性 copy。
//
// 返回值是给 LLM 的 tool_result 文本(永不返回 error;失败情形降级为
// 一行说明文字,由 agentRunner 保证**不计入 consecutiveFailures**)。
func (m *WerewolfManager) ChatRecall(roomID string, seat int, query string, filterSeat, day int) string {
	r := m.getRoom(roomID)
	if r == nil {
		return "chat_recall: 房间不存在或已关闭"
	}
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return "chat_recall: 房间繁忙,请稍后再试"
	}
	var msgs []agentcore.ChatMessage
	if r.chatQueue != nil {
		msgs = r.chatQueue.Tail(0)
	}
	currentDay := 0
	if r.State != nil {
		currentDay = r.State.DayNumber
	}
	r.mu.Unlock()

	q, fs, d := clampChatRecallParams(query, filterSeat, day, currentDay, MaxPlayers)
	if q == "" && fs < 0 && d == 0 {
		return "chat_recall: query 不能为空(seat/day 均为可选,但三者至少给一个有效条件)"
	}

	// 可见域过滤 + 打分(锁外)。
	hits := make([]chatRecallHit, 0, 16)
	for _, msg := range msgs {
		if !chatRecallEntryVisible(msg, seat) {
			continue
		}
		score := scoreChatRecallEntry(msg, q, fs, d)
		// query 非空时,要求至少一次关键词命中或显式过滤条件命中,
		// 避免「类型权重」把无关条目也捞回来。
		if score <= 0 {
			continue
		}
		if q != "" && !strings.Contains(msg.Text, q) && fs < 0 && d == 0 {
			continue
		}
		kind := "public"
		if msg.IsActivity {
			kind = "activity"
		} else if msg.IsWhisper {
			kind = "whisper"
		}
		hits = append(hits, chatRecallHit{
			seq:         msg.Seq,
			day:         msg.Round,
			fromSeat:    msg.FromSeat,
			fromAccount: msg.FromAccount,
			kind:        kind,
			text:        msg.Text,
			score:       score,
		})
	}
	if len(hits) == 0 {
		return "【chat_recall 结果】未找到匹配的早期发言(可能已被 500K 队列压缩淘汰,或关键词无命中)。"
	}

	// 稳定排序:分数降序;同分按 seq 降序(越新越前)。
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].score > hits[i].score ||
				(hits[j].score == hits[i].score && hits[j].seq > hits[i].seq) {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}

	truncated := len(hits) > chatRecallTopN
	show := hits
	if truncated {
		show = hits[:chatRecallTopN]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "【chat_recall 结果】query=%q", q)
	if fs >= 0 {
		fmt.Fprintf(&sb, " seat=%d号", fs+1)
	}
	if d > 0 {
		fmt.Fprintf(&sb, " day=%d", d)
	}
	fmt.Fprintf(&sb, " 命中 %d 条(返回前 %d):\n", len(hits), len(show))
	for _, h := range show {
		who := h.fromAccount
		if who == "" {
			who = fmt.Sprintf("%d号", h.fromSeat+1)
		}
		dayTag := ""
		if h.day > 0 {
			dayTag = fmt.Sprintf(" Day%d", h.day)
		}
		kindTag := ""
		switch h.kind {
		case "whisper":
			kindTag = " [私聊]"
		case "activity":
			kindTag = " [活动]"
		}
		fmt.Fprintf(&sb, "[seq=%d%s]%s %s: %s\n",
			h.seq, dayTag, kindTag, who, chatRecallTruncRunes(h.text, chatRecallTextMaxRunes))
	}
	if truncated {
		fmt.Fprintf(&sb, "[已截断: 共 %d 条命中,仅返回分数最高的前 %d 条]\n", len(hits), chatRecallTopN)
	}
	logger.L().Debug("werewolf: chat_recall executed",
		zap.String("room_id", roomID),
		zap.Int("seat", seat),
		zap.String("query", q),
		zap.Int("hits", len(hits)),
		zap.Bool("truncated", truncated))
	return sb.String()
}

// ChatRecall 实现 wwplayer.ChatRecallRunner 接口(agentRunner 侧入口)。
//
// 冷却:per-bot 60s(单响应最多 1 次的语义由冷却自然保证 —— 同响应第二次
// 调用立即命中冷却)。超限返回友好错误字符串,**不**返回 error,
// dispatch 路径因此不会把它计入任何失败计数(§112:辅助工具失败不污染
// consecutiveFailures)。
//
// 锁纪律:agentRunner 只被该 bot 的单条 agent goroutine 串行驱动,
// lastChatRecallAt 是纯 per-seat 字段,无需加锁(与 lastSpeechText 同模式)。
func (r *agentRunner) ChatRecall(query string, seat, day int) string {
	if r == nil || r.mgr == nil {
		return "chat_recall unavailable: runner not wired"
	}
	now := time.Now()
	if !r.lastChatRecallAt.IsZero() && now.Sub(r.lastChatRecallAt) < chatRecallCooldown {
		return "chat_recall rate-limited: 60 秒内只能检索一次,请稍后再试"
	}
	r.lastChatRecallAt = now
	return r.mgr.ChatRecall(r.roomID, int(r.seat), query, seat, day)
}
