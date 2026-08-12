// Package werewolf — recall_chat.go: 狼人杀「赛后复盘问答」(RecallChat)。
//
// 2026-08-11 §20260811-05 U2 新增。
//
// 设计动机(知识库 Agent-Surpport-01 §4.5 / K3 §3 A2):
// §128 确立了「对话即思考」,赛后问答是这条理念的自然对偶 —— 玩家用语言提问,
// Agent 用本局 Memory 快照(对局结束即冻结,不再追加)回答,全程不需要新增任何
// 「结构化留痕」字段。冷却期(§129)里人类从「被动看静态 bot_contexts」升级为
// 「主动提问:你第二晚为什么毒 5 号?」。
//
// 数据流:
//
//	POST /api/games/werewolf/rooms/:roomId/recall_chat {seat, question}
//	  → api 层: JWT 鉴权 + 房间成员(玩家或观战者)校验 + 限流
//	  → manager.RecallChat(roomID, seat, question)
//	      1. lockRoomBriefly 快照: r.Status=="over" 守卫 + bot := r.BotAgents[seat] +
//	         Memory.Snapshot()(messages) + modelKey + 本局角色/阵营/胜负
//	      2. 锁外构造请求: system = BuildRecallSystemPrompt(角色/阵营/胜负/天数)
//	         messages = memory 快照(PruneByBytes 截断到预算) + [user: 提问]
//	      3. registry.Get(modelKey).Chat(parentCtx + extendedTimeout)  // §197
//	         AgentClassName = LsmWebGame-Werewolf-Recall               // §24
//	      4. 失败 → 返回降级文案(bot「太累不想复盘」),不报 500
//
// 关键设计决策:
//   - Memory 冻结:复盘期间 bot 的 Run 循环已停(游戏结束),Memory.Snapshot()
//     天然是冻结态;不需要额外 freeze 标志。
//   - 单轮问答不写回:复盘问答**不** Push 回 Memory,避免污染下一局
//     (§128:复盘对话不是对局内思考);也不进 chat_message 表 / chat_history
//     队列(§119 的反面 —— 对局已结束,没有「频道隔离」对象需要保护)。
//   - 身份公开:终局后 RolePubliclyRevealed 白名单第 1 条已全场公开,
//     无泄密面;但仍跑一遍 ScrubIdentityLeak 兜底引擎内部字段名泄漏。
//
// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-05.md §U2。
package werewolf

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	agentroot "LsmWebGame/agent"
	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/config"
	"LsmWebGame/llm"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── 错误哨兵(api 层映射 HTTP 状态码) ───────────────────

var (
	// ErrRecallNotOver 对局尚未结束(或房间不存在)。
	ErrRecallNotOver = errors.New("recall chat: game not over")
	// ErrRecallNoBot 目标座位不是本局 bot。
	ErrRecallNoBot = errors.New("recall chat: seat is not a bot")
	// ErrRecallDisabled 运维开关关闭。
	ErrRecallDisabled = errors.New("recall chat: disabled by config")
)

// recallQuestionMaxRunes / recallAnswerMaxRunes 是问答长度上限。
// 与知识库 §4.5 设计对齐:question ≤ 200 字,answer ≤ 600 字。
const (
	recallQuestionMaxRunes = 200
	recallAnswerMaxRunes   = 600
)

// cfgRecallChatEnabled 安全读取 config.WerewolfConfig.RecallChatEnabled。
// 默认 true;测试环境 config.Load() panic 时按"关闭"兜底(避免无配置
// 环境下误触发 LLM 调用)。
func cfgRecallChatEnabled() (enabled bool) {
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return false
	}
	return c.Werewolf.RecallChatEnabled
}

// cfgRecallChatMaxTokens 安全读取 RecallChatMaxTokens(默认 1024)。
func cfgRecallChatMaxTokens() (n int) {
	n = 1024
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return 1024
	}
	if c.Werewolf.RecallChatMaxTokens > 0 {
		n = c.Werewolf.RecallChatMaxTokens
	}
	return n
}

// ─────────────────── 提问限流(每用户每房间) ───────────────────

// recallRateKey 是限流键:(roomID, userID)。
type recallRateKey struct {
	roomID string
	userID string
}

// recallRateLimiter 是 manager 级的复盘提问限流器。
// 每 (room, user) 一个计数器,达到 cfg.Werewolf.RecallChatPerUserLimit
// (默认 10) 后拒绝。房间 GC 时经 ResetRecallRateLimit(roomID) 清理。
//
// 并发模型:独立 sync.Mutex(不走 r.mu,避免 §92a 锁序纠缠)。
type recallRateLimiter struct {
	mu     sync.Mutex
	counts map[recallRateKey]int
}

// Allow 报告本次提问是否被允许;允许时计数 +1。limit <= 0 时按 10 兜底。
func (l *recallRateLimiter) Allow(roomID, userID string, limit int) bool {
	if limit <= 0 {
		limit = 10
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts == nil {
		l.counts = make(map[recallRateKey]int, 16)
	}
	k := recallRateKey{roomID: roomID, userID: userID}
	if l.counts[k] >= limit {
		return false
	}
	l.counts[k]++
	return true
}

// ResetRoom 清理指定房间的全部限流计数(房间 GC 时调用)。
func (l *recallRateLimiter) ResetRoom(roomID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k := range l.counts {
		if k.roomID == roomID {
			delete(l.counts, k)
		}
	}
}

// ─────────────────── 复盘 system prompt ───────────────────

// BuildRecallSystemPrompt 构造复盘问答的 system 指令(纯函数,便于测试)。
// 复盘人格:对局已结束,你是 X 号位的扮演者,用第一人称诚实复盘,
// 可以承认欺骗/失误;禁止暴露引擎内部字段名/工具名(如 heart_thought)。
func BuildRecallSystemPrompt(role, faction, winner string, dayNumber, seat int) string {
	var sb strings.Builder
	sb.WriteString("你是一场刚刚结束的狼人杀对局的复盘嘉宾。")
	fmt.Fprintf(&sb, "你在本局坐 %d 号位,角色是 %s(%s 阵营)。", seat+1, role, faction)
	switch winner {
	case "wolf":
		sb.WriteString("本局狼人阵营获胜。")
	case "good":
		sb.WriteString("本局好人阵营获胜。")
	}
	fmt.Fprintf(&sb, "对局共进行约 %d 天,现在已经终局,所有身份都已公开。\n\n", dayNumber)
	sb.WriteString("【复盘规则】\n")
	sb.WriteString("1. 用第一人称、轻松坦诚的语气回答观众/玩家的提问,像赛后采访一样。\n")
	sb.WriteString("2. 可以诚实承认自己的欺骗、失误、误判和运气——对局已结束,没有需要再守护的秘密。\n")
	sb.WriteString("3. 回答基于你的本局记忆(对话与决策记录);记不清的细节可以坦率说「我记不清了」。\n")
	sb.WriteString("4. 回答控制在 300 字以内,直接回答问题本身,不要复述整局。\n")
	sb.WriteString("5. 禁止使用引擎/程序术语(如「工具」「字段」「Memory」「HeartThought」),用玩家语言表述。\n")
	return sb.String()
}

// ─────────────────── manager.RecallChat 主入口 ───────────────────

// RecallChatResult 是复盘问答的结果。
type RecallChatResult struct {
	Seat      int    `json:"seat"`
	ModelKey  string `json:"model_key"`
	Role      string `json:"role"`
	Answer    string `json:"answer"`
	Fallback  bool   `json:"fallback"` // true = LLM 失败的降级文案
	TookMs    int64  `json:"took_ms"`
}

// RecallChat 执行一次赛后复盘问答。调用方(api 层)已完成 JWT 鉴权与
// 房间成员校验;本函数负责:
//   - 开关 / 终局守卫 / bot 座位守卫;
//   - lockRoomBriefly 快照 Memory(§92a:绝不裸持 r.mu 跨 LLM 调用);
//   - 锁外调 LLM(parentCtx + extendedTimeout,§197);
//   - LLM 失败 → 降级文案(Fallback=true),不返回 error(不报 500)。
//
// question 超长在入口处截断(≤200 rune)。
func (m *WerewolfManager) RecallChat(ctx context.Context, roomID string, seat int, question string) (*RecallChatResult, error) {
	if !cfgRecallChatEnabled() {
		return nil, ErrRecallDisabled
	}
	// 入口截断(防御性;api 层已校验)。
	if rs := []rune(question); len(rs) > recallQuestionMaxRunes {
		question = string(rs[:recallQuestionMaxRunes])
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, errors.New("recall chat: empty question")
	}

	m.mu.RLock()
	r, ok := m.rooms[roomID]
	registry := m.registry
	m.mu.RUnlock()
	if !ok || r == nil {
		return nil, ErrRecallNotOver
	}
	if registry == nil {
		return nil, errors.New("recall chat: llm registry not wired")
	}

	// ── 锁内快照(§92a) ──
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return nil, errors.New("recall chat: room busy, retry later")
	}
	if r.State == nil || r.State.Phase != PhaseGameOver {
		r.mu.Unlock()
		return nil, ErrRecallNotOver
	}
	bot, isBot := r.BotAgents[seat]
	if !isBot || bot == nil {
		r.mu.Unlock()
		return nil, ErrRecallNoBot
	}
	modelKey := r.seatModelKeys[seat]
	role := r.State.Roles[seat].String()
	faction := factionOfRoleString(role)
	winner := r.State.Winner
	day := r.State.DayNumber
	// Memory 快照(冻结态:游戏结束后 Run 循环已停,不再追加)。
	msgs, _ := bot.Memory.Snapshot()
	r.mu.Unlock()

	if modelKey == "" {
		return nil, ErrRecallNoBot
	}

	// ── 锁外构造请求 ──
	system := BuildRecallSystemPrompt(role, faction, winner, day, seat)
	// messages = memory 快照(经 Anthropic 协议清洗 + 字节预算截断) + 提问。
	sanitized, _ := wwplayer.SanitizeMessagesForAnthropic(msgs)
	sanitized = pruneRecallMessages(sanitized, recallMemoryMaxBytes)
	sanitized = append(sanitized, llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{Type: "text", Text: "【复盘提问】观众/玩家问你: " + question +
			"\n\n请按复盘规则回答(第一人称、坦诚、≤300 字、不用程序术语)。"}},
	})

	provider, key, err := registry.Get(modelKey)
	if err != nil {
		logger.L().Warn("werewolf: recall chat registry.Get failed",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		return recallFallback(seat, modelKey, role), nil
	}

	// §197 流式续命:parentCtx + extendedTimeout 长预算。
	// 复盘问答是非流式单轮,预算 = callTimeout + extendedTimeout 合并为一个
	// 总预算(与 agent run.go 的 parentCtx 语义对齐)。
	timeoutSec := cfgRecallChatTimeoutSec()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := provider.Chat(callCtx, key, llm.LLMRequest{
		Model: modelKey,
		System: []llm.SystemBlock{{Type: "text", Text: system}},
		Messages:  sanitized,
		MaxTokens: cfgRecallChatMaxTokens(),
		// §24 AgentClassName:复盘问答是独立的 Agent 类别,与玩家 Bot /
		// 法官 / 记忆迭代 / 画像迭代调用分开计费/归因。
		AgentClassName: string(agentroot.AgentClassWerewolfRecall),
	})
	took := time.Since(start).Milliseconds()
	if err != nil {
		// 调用方 ctx 已取消(客户端断开) → 直接透传,不降级。
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		logger.L().Warn("werewolf: recall chat LLM failed, fallback",
			zap.String("room_id", roomID),
			zap.String("model_key", modelKey),
			zap.Error(err))
		out := recallFallback(seat, modelKey, role)
		out.TookMs = took
		return out, nil
	}

	answer := strings.TrimSpace(resp.Text())
	if answer == "" {
		out := recallFallback(seat, modelKey, role)
		out.TookMs = took
		return out, nil
	}
	// §123 术语一致 + 身份泄漏兜底(终局后身份已公开,Scrub 主要拦
	// 引擎内部字段名/工具名泄漏)。
	if scrubbed, _ := ScrubIdentityLeak(answer); scrubbed != "" {
		answer = scrubbed
	}
	if rs := []rune(answer); len(rs) > recallAnswerMaxRunes {
		answer = string(rs[:recallAnswerMaxRunes]) + "…"
	}
	return &RecallChatResult{
		Seat:     seat,
		ModelKey: modelKey,
		Role:     role,
		Answer:   answer,
		TookMs:   took,
	}, nil
}

// recallFallback 生成 LLM 失败/空输出时的降级文案(bot「太累不想复盘」)。
// 返回 error=nil 的友好结果而非 500 —— 复盘是赛后娱乐功能,失败不应惊动
// 前端错误通道(§7.1:本地展示即可)。
func recallFallback(seat int, modelKey, role string) *RecallChatResult {
	return &RecallChatResult{
		Seat:     seat,
		ModelKey: modelKey,
		Role:     role,
		Answer:   fmt.Sprintf("(%d 号位揉了揉眼睛)「刚打完一局太累了,脑子转不动了……等我缓缓,你待会儿再问我一次吧。」", seat+1),
		Fallback: true,
	}
}

// recallMemoryMaxBytes 是复盘问答注入的 memory 快照字节预算。
// 与 §131 MemoryInjectMaxRunes(4000 字 ≈ 2K token)同量级;复盘不需要
// 完整对局流水,近期上下文 + 关键决策点足够回答「你为什么这么做」。
const recallMemoryMaxBytes = 24 * 1024

// pruneRecallMessages 按字节预算从**头部**裁掉最老的消息(保留最近的
// 上下文,与 Memory.PruneByBytes 语义对齐)。identity 首条(user)保留。
func pruneRecallMessages(msgs []llm.Message, maxBytes int) []llm.Message {
	if maxBytes <= 0 || len(msgs) == 0 {
		return msgs
	}
	total := 0
	for _, m := range msgs {
		total += recallMessageBytes(m)
	}
	for total > maxBytes && len(msgs) > 2 {
		// 保留首条(identity)与尾部;从第 2 条开始裁。
		total -= recallMessageBytes(msgs[1])
		msgs = append(msgs[:1], msgs[2:]...)
	}
	return msgs
}

// recallMessageBytes 粗估单条 message 的字节数(遍历 content blocks)。
func recallMessageBytes(m llm.Message) int {
	n := 0
	for _, b := range m.Content {
		n += len(b.Text) + len(b.Name) + 32
	}
	return n + 16
}

// cfgRecallChatTimeoutSec 复盘问答的总超时(秒)。
// 取 cfgLLMCallTimeoutSec 的默认档(300s)与 stream-extended(900s)之间的
// 折中:复盘是单轮非流式调用,600s 足够覆盖慢模型;配置不可读时兜底 600。
func cfgRecallChatTimeoutSec() (out int) {
	out = 600
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return 600
	}
	// 复用既有 llm_timeout 族配置,不为复盘新增独立配置项(运维心智最小化)。
	if c.Werewolf.LLMCallTimeoutSec > 0 {
		out = c.Werewolf.LLMCallTimeoutSec * 2
	}
	return out
}

// ResetRecallRateLimit 清理指定房间的复盘限流计数(房间 GC 时调用)。
// 由 forceCloseRoomLocked / RemoveGame 路径接线(§130:必须真实接线)。
func (m *WerewolfManager) ResetRecallRateLimit(roomID string) {
	m.recallLimiter.ResetRoom(roomID)
}

// AllowRecallChat 报告 (roomID, userID) 的本次复盘提问是否被限流允许;
// 允许时计数 +1。限额读 cfg.Werewolf.RecallChatPerUserLimit(默认 10)。
func (m *WerewolfManager) AllowRecallChat(roomID, userID string) bool {
	return m.recallLimiter.Allow(roomID, userID, cfgRecallChatPerUserLimit())
}

// cfgRecallChatPerUserLimit 安全读取 RecallChatPerUserLimit(默认 10)。
func cfgRecallChatPerUserLimit() (n int) {
	n = 10
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return 10
	}
	if c.Werewolf.RecallChatPerUserLimit > 0 {
		n = c.Werewolf.RecallChatPerUserLimit
	}
	return n
}

// IsSpectatorOf 报告 userID 是否在指定房间的观战者集合中。
// lockRoomBriefly 快照(§92a);锁争用/房间不存在时返回 false。
func (m *WerewolfManager) IsSpectatorOf(roomID, userID string) bool {
	if roomID == "" || userID == "" {
		return false
	}
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	m.mu.RUnlock()
	if !ok || r == nil {
		return false
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return false
	}
	defer r.mu.Unlock()
	_, ok = r.Spectators[userID]
	return ok
}
