// Package werewolf — room_judge_qa.go: 法官问答(@法官 / 私聊)manager 侧管线。
//
// 2026-10-06 §法官对话 B2。设计文档:
//   docs/狼人杀/狼人杀法官对话与遗言聊天-设计与实现-20261006.md §4.3.2。
//
// 双通道:
//   私聊 chat.whisper → ws.ChatService.Whisper 截获 → main.go 注入的
//     judgeQuestionHook → RecordJudgeQuestion(本文件)
//   公屏 chat.send "@法官 …" → RecordRoomMessage 前缀检测 →
//     recordJudgePublicQuestionLocked(本文件,公屏消息本身照常广播)
//
// 两者锁内构建【公开-only 问答上下文】(buildJudgeQAContextLocked)后经
// wakeJudgeLocked(wwjudge.KindJudgeQuestion, extra) 唤醒法官 goroutine,
// 法官侧 handleQuestionEvent 调 LLM 生成回答,经 onQAReply 回调落到
// RecordJudgeAnswer(私聊走 WhisperFromJudge / 公屏走 SendFromJudge)。
//
// §3 公平性总纲(能力隔离)在本文件的落点:
//   buildJudgeQAContextLocked 的输入白名单 = phase/day/存活死亡座位/警长/
//   RolePubliclyRevealed 公开口径/recentSpeeches 尾 8 条/JudgeMemoryRing
//   最近 3 条宣告/该 asker QA 历史。**结构上不读** WolfSeats / night_* 字段 /
//   whisperInbox / chatQueue 私聊段 —— 问答模式的法官根本「不知道」秘密。
package werewolf

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"LsmAgentGame/agent/wwjudge"
	"LsmAgentGame/config"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// judgeQATurn 是 judgeQAHistory 的单轮记录(本地 struct,不进 DB)。
type judgeQATurn struct {
	Question string
	Answer   string
	At       time.Time
}

// judgeQAHistoryMaxTurns 每位 asker 保留的最近问答轮数(环形)。
const judgeQAHistoryMaxTurns = 4

// judgeQAPublicCooldownExtraSec 公屏路径在私聊冷却之上追加的秒数
// (§3.4 限流矩阵:私聊 20s / 公屏 30s)。
const judgeQAPublicCooldownExtraSec = 10

// cfgWerewolfJudgeQAEnabled 读取法官问答总开关(默认 true;applyDefaults 已
// 填充,此处 recover + 显式默认双保险,与 cfgWerewolfJudgeMode 同款模式)。
func cfgWerewolfJudgeQAEnabled() bool {
	defer func() { _ = recover() }()
	return config.Load().Werewolf.JudgeQAEnabled
}

// cfgWerewolfJudgeQACooldownSec 读取 per-user 提问冷却秒数(默认 20)。
func cfgWerewolfJudgeQACooldownSec() int {
	defer func() { _ = recover() }()
	v := config.Load().Werewolf.JudgeQACooldownSec
	if v <= 0 {
		return 20
	}
	return v
}

// RecordJudgeQuestion 私聊问答入口(chat_service 的 judgeQuestionHook 调用,
// **不持锁进入**;§92a)。
//
// 校验顺序(任一失败即返回 error → ws 层 chat.error(20001),文案为英文短句):
//   房间存在 → 问答开关 → 法官就绪(r.judge != nil)→ per-user 冷却
//   (judge_qa_cooldown_sec,默认 20s) → 构建公开-only 上下文 → 唤醒法官。
func (m *WerewolfManager) RecordJudgeQuestion(roomID, askerID, askerAccount, text string) error {
	r := m.getRoom(roomID)
	if r == nil {
		return errors.New("room not found")
	}
	if !cfgWerewolfJudgeQAEnabled() {
		return errors.New("AI judge not enabled in this room")
	}
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return errors.New("room is busy, retry shortly")
	}
	extra, err := m.judgeQAAskLocked(r, askerID, askerAccount, text, false, cfgWerewolfJudgeQACooldownSec())
	r.mu.Unlock()
	if err != nil {
		return err
	}
	// 锁外投递(§92a:不持 r.mu 做 channel send 之外的任何事)。
	dispatchJudgeQuestionEvent(r, extra)
	return nil
}

// dispatchJudgeQuestionEvent 把 judge_question 事件投递给法官 goroutine。
//
// 刻意不走 wakeJudgeLocked(它会构建含 WolfSeats 全知视野的 GameSnapshot):
// 问答事件 **Snap 显式 nil** —— §3.1 能力隔离的结构性落实,QA 链路上
// (channel → handleQuestionEvent → prompt)从头到尾不出现任何秘密字段,
// 未来即使有人误读 evt.Snap 也只是 nil。channel 满则非阻塞丢弃 + WARN
// (与 wakeJudgeLocked 同语义)。
func dispatchJudgeQuestionEvent(r *WerewolfRoom, extra map[string]any) {
	if r == nil || r.judge == nil || r.judgeEvents == nil {
		return
	}
	evt := wwjudge.JudgeEvent{
		Kind:  wwjudge.KindJudgeQuestion,
		Snap:  nil, // 问答模式零秘密载荷(§3.1)
		Extra: extra,
		At:    time.Now(),
	}
	select {
	case r.judgeEvents <- evt:
	default:
		logger.L().Warn("werewolf: judge question dropped, events channel full",
			zap.String("room_id", r.RoomID))
	}
}

// recordJudgePublicQuestionLocked 公屏 @法官 入口(RecordRoomMessage 检测
// "@法官" 前缀后调用)。**调用方必须持有 r.mu**(§92a 锁内变体)。
//
// 冷却阈值 = judge_qa_cooldown_sec + 10s(默认 30s);命中冷却**仅记 Info
// 日志跳过回答**(公屏消息本身照常广播,不返回错误)。返回 extra 非 nil
// 表示应唤醒法官(调用方须在**锁外**调 wakeJudgeLocked)。
func (m *WerewolfManager) recordJudgePublicQuestionLocked(r *WerewolfRoom, askerID, askerAccount, text string) map[string]any {
	if r == nil || r.State == nil {
		return nil
	}
	if !cfgWerewolfJudgeQAEnabled() || r.judge == nil {
		// 公屏路径不报错(消息照常),静默跳过回答。
		return nil
	}
	cooldown := cfgWerewolfJudgeQACooldownSec() + judgeQAPublicCooldownExtraSec
	extra, err := m.judgeQAAskLocked(r, askerID, askerAccount, text, true, cooldown)
	if err != nil {
		// 公屏命中冷却 → 仅记日志跳过,不向发送者回错误。
		logger.L().Info("werewolf: public @judge question throttled or rejected",
			zap.String("room_id", r.RoomID),
			zap.String("asker", askerID),
			zap.Error(err))
		return nil
	}
	return extra
}

// judgeQAAskLocked 是私聊/公屏共用的锁内问答处理:冷却校验 + 游标推进 +
// 构建公开-only 上下文。caller 必须持有 r.mu。返回 (extra, nil) 成功;
// err 非 nil = 未通过校验(不唤醒)。
func (m *WerewolfManager) judgeQAAskLocked(r *WerewolfRoom, askerID, askerAccount, text string, isPublic bool, cooldownSec int) (map[string]any, error) {
	if r.judge == nil {
		return nil, errors.New("judge not ready yet")
	}
	// per-user 冷却(私聊/公屏共用同一游标,阈值不同)。
	if r.judgeQALastAsk == nil {
		r.judgeQALastAsk = make(map[string]time.Time, MaxPlayers+4)
	}
	if last, ok := r.judgeQALastAsk[askerID]; ok {
		cooldown := time.Duration(cooldownSec) * time.Second
		if remain := cooldown - time.Since(last); remain > 0 {
			return nil, fmt.Errorf("judge Q&A cooldown: retry in %ds", int(remain.Seconds()+0.999))
		}
	}
	r.judgeQALastAsk[askerID] = time.Now()
	return m.buildJudgeQAContextLocked(r, askerID, askerAccount, text, isPublic), nil
}

// judgeQAPhaseLabelCN 把引擎 phase 映射为玩家可见中文阶段名(问答上下文用)。
// wwjudge.judgePublicPhaseLabel 未导出,此处按设计文档 §4.3.2 在 werewolf 侧
// 本地映射(标签与 activity_emitter.go::phaseLabelCN 语义对齐;未命中回退
// phaseLabelCN,再未命中原样返回 phase 串)。
func judgeQAPhaseLabelCN(phase string) string {
	switch phase {
	case "filling":
		return "等待玩家入座"
	case "pre_wolves":
		return "首夜强制发言阶段"
	case "night_guard":
		return "夜间 · 守卫行动"
	case "night_wolves", "night_seer", "night_witch", "night_demon_hunter":
		return "夜间 · 秘密阶段"
	case "dawn":
		return "黎明 · 公布死亡"
	case "sheriff", "sheriff_election":
		return "警长竞选"
	case "speak", "day":
		return "白天 · 轮流发言"
	case "vote":
		return "白天 · 投票放逐"
	case "idiot_reveal":
		return "白痴翻牌"
	case "hunter_shoot":
		return "猎人开枪"
	case "death_lyric", "last_words":
		return "遗言阶段"
	case "restart_vote":
		return "重开局投票"
	case "over", "gameover":
		return "对局结束"
	}
	if cn := phaseLabelCN(phase); cn != phase {
		return cn
	}
	return phase
}

// buildJudgeQAContextLocked 构建公开-only 问答上下文(§3.1 能力隔离的落点)。
// **调用方必须持有 r.mu。**
//
// 输入白名单(设计 §4.2 Extra 载荷):question(≤300 rune)/asker_id/
// asker_display("5号(张三)" / "死亡-5号(张三)" / "观战-李四")/asker_account/
// asker_is_spectator/is_public/phase_label/day/public_state(存活/死亡座位/
// 警长/已公开死者身份)/recent_public(尾 8 条 × ≤60 rune)/
// recent_announcements(JudgeMemoryRing 尾 3 条)/qa_history(该 asker 尾 4 轮)。
//
// 【硬断言 · TestJudgeQAContext_NoSecrets 锁死】
// 不读 r.State.WolfSeats 派生物、不读任何 night_* 字段、不读 whisperInbox /
// chatQueue 私聊段 —— 法官在问答模式下「不知道」秘密,因此不可能说漏。
func (m *WerewolfManager) buildJudgeQAContextLocked(r *WerewolfRoom, askerID, askerAccount, text string, isPublic bool) map[string]any {
	extra := map[string]any{
		"question": truncate(text, 300),
		"asker_id": askerID,
		"is_public": isPublic,
	}
	// asker 身份解析:座位 / 死亡座位 / 观战者。
	seat := NoSeat
	if askerID != "" {
		for i, u := range r.Seats {
			if u != "" && u == askerID {
				seat = Seat(i)
				break
			}
		}
	}
	isSpectator := seat == NoSeat
	display := ""
	if seat != NoSeat && r.State != nil {
		display = strconv.Itoa(int(seat)+1) + "号(" + askerAccount + ")"
		if !r.State.AliveSeat(seat) {
			display = "死亡-" + display
		}
	} else {
		display = "观战-" + askerAccount
	}
	extra["asker_display"] = display
	extra["asker_account"] = askerAccount
	extra["asker_is_spectator"] = isSpectator

	if r.State != nil {
		extra["phase_label"] = judgeQAPhaseLabelCN(r.State.Phase.String())
		extra["day"] = r.State.DayNumber
		extra["public_state"] = judgeQAPublicStateLocked(r)
	}

	// recent_public:公开发言尾 8 条,每条截 60 rune。
	if n := len(r.recentSpeeches); n > 0 {
		take := n
		if take > 8 {
			take = 8
		}
		var b strings.Builder
		for i := n - take; i < n; i++ {
			sp := r.recentSpeeches[i]
			who := sp.Account
			if sp.Seat >= 0 {
				who = strconv.Itoa(sp.Seat+1) + "号"
			} else if sp.IsSpectator {
				who = "观战-" + sp.Account
			}
			b.WriteString(who + ": " + truncate(sp.Text, 60) + "\n")
		}
		extra["recent_public"] = b.String()
	}

	// recent_announcements:法官最近 3 条已广播宣告(JudgeMemoryRing 快照,
	// 环形缓冲只存法官自己 announce 出去的公开文本,§119 协议层隔离)。
	if r.judge != nil && r.judge.Memory != nil {
		hist := r.judge.Memory.Snapshot()
		if n := len(hist); n > 0 {
			take := n
			if take > 3 {
				take = 3
			}
			var b strings.Builder
			for i := n - take; i < n; i++ {
				b.WriteString(truncate(hist[i].Text, 80) + "\n")
			}
			extra["recent_announcements"] = b.String()
		}
	}

	// qa_history:该 asker 最近 4 轮问答渲染文本。
	if turns := r.judgeQAHistory[askerID]; len(turns) > 0 {
		var b strings.Builder
		for i, t := range turns {
			b.WriteString("问: " + truncate(t.Question, 60) + "\n答: " + truncate(t.Answer, 120) + "\n")
			if i >= judgeQAHistoryMaxTurns-1 {
				break
			}
		}
		extra["qa_history"] = b.String()
	}
	return extra
}

// judgeQAPublicStateLocked 生成多行公开状态摘要:存活座位 / 死亡座位 /
// 警长 / 已公开死者身份。**只能用 RolePubliclyRevealed 单点判定的公开口径**
// (§135;与 view.go / room_state.go 的 PublicPlayerState 同源),绝不能直接
// 读 Roles。caller 必须持有 r.mu。
func judgeQAPublicStateLocked(r *WerewolfRoom) string {
	if r.State == nil {
		return "(对局尚未开始)"
	}
	var alive, dead, revealed []string
	for i := 0; i < MaxPlayers; i++ {
		if r.Seats[i] == "" && r.State.Seats[i] == "" {
			continue
		}
		n := strconv.Itoa(i + 1)
		if r.State.AliveSeat(Seat(i)) {
			alive = append(alive, n)
		} else {
			dead = append(dead, n)
			// §135 公开口径:仅 RolePubliclyRevealed 命中的死者才带身份。
			if r.State.RolePubliclyRevealed(Seat(i)) {
				revealed = append(revealed, n+"号·"+r.State.Roles[i].String())
			}
		}
	}
	var b strings.Builder
	b.WriteString("存活座位: " + strings.Join(alive, ",") + "\n")
	b.WriteString("死亡座位: " + strings.Join(dead, ","))
	if r.State.SheriffSeat != NoSeat {
		b.WriteString("\n警长: " + strconv.Itoa(int(r.State.SheriffSeat)+1) + "号")
	} else {
		b.WriteString("\n警长: 无")
	}
	if len(revealed) > 0 {
		b.WriteString("\n已公开死者身份: " + strings.Join(revealed, ","))
	}
	if r.State.Status == "over" && r.State.Winner != "" {
		b.WriteString("\n对局已结束,胜方: " + r.State.Winner)
	}
	return b.String()
}

// RecordJudgeAnswer 法官回答落地(wwjudge onQAReply 回调注入,**不持锁进入**)。
// 追加 r.judgeQAHistory[askerID](环形 4 轮)后分流:
//   - isPublic  → chatSvc.SendFromJudge(kind="judge_reply",公屏广播);
//   - 私聊      → chatSvc.WhisperFromJudge(定向投递提问者)。
//
// fallback=true(兜底文案,E2E 2026-10-06 精化):
//   - ① delete(r.judgeQALastAsk, askerID) 退还 per-user 冷却 —— 用户拿到的
//     是占位文案而非答案,不应再被 20s 冷却锁住,可立即重试;
//   - ② 不追加 judgeQAHistory —— 防「答:法官暂时无法回答」污染该 asker 后续
//     QA 上下文(否则 LLM 会以为刚才真的回答过)。
//     兜底文案本身照常投递给提问者(回声纪律不变)。
//
// 私聊回答【刻意不】写 JudgeTranscript(judge_context 对全员可见,会泄露
// 「有人问过」→ 公平性 F-2);WhisperFromJudge 本身不广播、不 emitRoomMessage。
func (m *WerewolfManager) RecordJudgeAnswer(roomID, askerID, askerAccount, question, answer string, isPublic, fallback bool) {
	r := m.getRoom(roomID)
	if r == nil {
		return
	}
	// 解析法官模型 key(房间级显式 → seatModelKeys 首个非空),锁内短时完成。
	modelKey := ""
	if lockRoomBriefly(r, 500*time.Millisecond) {
		modelKey = r.JudgeModelKey
		if modelKey == "" {
			for _, k := range r.seatModelKeys {
				if k != "" {
					modelKey = k
					break
				}
			}
		}
		if fallback {
			// 兜底:退还冷却游标(用户可立即重试),不追加问答历史。
			delete(r.judgeQALastAsk, askerID)
		} else {
			// 成功:追加问答历史(环形 4 轮,惰性初始化)。
			if r.judgeQAHistory == nil {
				r.judgeQAHistory = make(map[string][]judgeQATurn, 4)
			}
			turns := append(r.judgeQAHistory[askerID], judgeQATurn{
				Question: truncate(question, 300),
				Answer:   truncate(answer, 300),
				At:       time.Now(),
			})
			if len(turns) > judgeQAHistoryMaxTurns {
				turns = turns[len(turns)-judgeQAHistoryMaxTurns:]
			}
			r.judgeQAHistory[askerID] = turns
		}
		r.mu.Unlock()
	}
	if m.chatSvc == nil || modelKey == "" {
		logger.L().Warn("werewolf: judge answer dropped (no chat service or model key)",
			zap.String("room_id", roomID),
			zap.String("asker", askerID),
			zap.Bool("is_public", isPublic),
			zap.Bool("fallback", fallback))
		return
	}
	if isPublic {
		if _, err := m.chatSvc.SendFromJudge(roomID, "[法官·"+modelKey+"]", modelKey, answer, "judge_reply"); err != nil {
			logger.L().Warn("werewolf: judge public reply SendFromJudge failed",
				zap.String("room_id", roomID), zap.Error(err))
		}
		return
	}
	if err := m.chatSvc.WhisperFromJudge(roomID, askerID, askerAccount, modelKey, answer); err != nil {
		logger.L().Warn("werewolf: judge private reply WhisperFromJudge failed",
			zap.String("room_id", roomID), zap.String("asker", askerID), zap.Error(err))
	}
}

// isJudgePublicQuestion 检测公屏文本是否以 "@法官" 开头(TrimSpace 后前缀
// 匹配)。公屏提问本来就是一条普通公屏消息(bot 能看到问题文本属既有语义),
// 本检测只决定是否额外唤醒法官。
func isJudgePublicQuestion(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "@法官")
}
