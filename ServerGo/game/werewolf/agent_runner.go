// Package werewolf — agent_runner.go: bridges the in-memory WerewolfManager to
// the wwplayer.ToolRunner interface so the Agent.Run loop can drive game actions
// and chat without a ws.Client.
//
// One agentRunner is created per bot seat at StartGame time. It holds a
// reference to the owning WerewolfManager (for Action_* calls) and the
// ChatService (for SendFromBot / WhisperFromBot). The runner is safe to call
// from the agent goroutine because all state mutations go through the manager
// lock.
package werewolf

import (
	"context"
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// BotChatSender is declared in the leaf agent package (see agent/agent.go) so
// both ws.ChatService and the werewolf agentRunner can agree on it without
// creating an import cycle (werewolf → ws → werewolf). Re-declared here as a
// type alias for readability within this package.
type BotChatSender = wwplayer.BotChatSender

// agentRunner implements wwplayer.ToolRunner for one bot seat.
type agentRunner struct {
	mgr        *WerewolfManager
	roomID     string
	seat       Seat
	botUserID  string
	botAccount string
	modelKey   string // LLM model_key of this bot; surfaced in chat frames
	chatSvc    BotChatSender

	// agent R76 P1-3 (2026-07-10): 反向引用 owning Agent,以便 runner.Interject
	// 可以直接调用 Agent.AllowInterject / MarkInterject 做单 bot 插话 quota。
	agent *wwplayer.Agent

	// v3 §G2 — 当前轮 GameContext(供 prop_inspect/prop_status/prop_history 工具查询)。
	// 由 Run 调用 DispatchTool 前同步设置;CurrentGC() 返回,实现 PropInspectRunner 接口。
	currentGC *wwtypes.GameContext

	// recentSpeakDedup BUG-R70-P2 (2026-07-09): 跨消息级发言内容去重。
	// SpeakLimiter 只节流时间间隔(45s/条),本去重器在 90s 窗口内识别
	// "我是 X 号"复读式刷屏。每个 bot seat 一份独立状态。
	recentSpeakDedup *wwplayer.RecentSpeakDedup

	// speakLimiter BUG-R74-1 (2026-07-09): 45s 间隔限流桶。每次 Speak/Interject
	// 派发前调 Allow() 检查;通过后 Mark() 重置计时器。nil = 不限流(测试桩)。
	speakLimiter SpeakRateLimiter
	// filterCfg BUG-R74-1/2: 控制 rate-limit / identity-leak 过滤开关。
	filterCfg SpeakFilterConfig

	// lastMysteryHint (2026-07-16 R132):最近一次 MysteryMaskText 命中后
	// 拼装的"风险提示"字符串,Speak / SpeakAuto / SpeakWithThought / Interject
	// 末尾返回 result 时附带。LLM 看到这条 tool_result 后在下一轮学到"如何
	// 用铺垫化的版本表达同一意图"。nil 表示无 (无命中 或 上次未命中)。
	lastMysteryHint string

	// lastSpeechText (2026-08-05 §Agent聊天显示优化):最近一次经 recordLastSpeech
	// 落库的**过滤后**发言原文。EmotionSwitchSpeak 需要在内层 r.Speak 之后用
	// kind="emotion_speak" 覆写同一条记录,但它手上只有**过滤前**的 text ——
	// 直接回写会把 MysteryMaskText / StripLLMInternalTags / ScrubVerdictClaim
	// 已经改写掉的内容重新放上全房可见的 wire 字段(BUG-R238-P0-1 同源风险)。
	// 因此由 recordLastSpeech 统一缓存过滤后文本供其复用。
	// 与 lastMysteryHint 同样是纯 per-seat 字段:agentRunner 只被该 bot 的
	// 单条 agent goroutine 串行驱动,无需加锁。
	lastSpeechText string

	// lastChatRecallAt 2026-08-13 §20260813-02 U3 — chat_recall 工具的
	// per-bot 60s 冷却计时(chat_recall.go::ChatRecall 读写,单 goroutine 驱动)。
	lastChatRecallAt time.Time
}

func newAgentRunner(mgr *WerewolfManager, roomID string, seat Seat, botUserID, botAccount, modelKey string, chatSvc BotChatSender) *agentRunner {
	return &agentRunner{
		mgr:              mgr,
		roomID:           roomID,
		seat:             seat,
		botUserID:        botUserID,
		botAccount:       botAccount,
		modelKey:         modelKey,
		chatSvc:          chatSvc,
		recentSpeakDedup: wwplayer.NewRecentSpeakDedup(),
		filterCfg:        newDefaultSpeakFilterConfig(),
	}
}

// SetCurrentGC 设置当前轮的 GameContext（v3 §G2）。
// 由 wwplayer.Run 在每次调用 DispatchTool 前同步设置,让 prop_inspect/prop_status/
// prop_history 三个查询工具能拿到本轮的 GameContext 数据。
func (r *agentRunner) SetCurrentGC(gc *wwtypes.GameContext) {
	if r == nil {
		return
	}
	r.currentGC = gc
}

// CurrentGC 实现 wwplayer.PropInspectRunner 接口（v3 §G2）。
func (r *agentRunner) CurrentGC() *wwtypes.GameContext {
	if r == nil {
		return nil
	}
	return r.currentGC
}

// recordLastSpeech 2026-08-05 §Agent聊天显示优化 (B4) — 统一的「广播成功后」
// 落 BotTranscript.LastSpeech* 入口。**只允许**在 chatSvc 广播真正成功之后
// (即 Mark/bump 那一组调用之后)调用;失败/被拒路径一律不调,座位卡气泡
// 因此永远只反映公屏上确实出现过的内容。
//
// round 取自本轮 GameContext(由 wwplayer.Run 在 DispatchTool 前 SetCurrentGC
// 同步设置)—— **纯字段读、不取任何锁**,严格遵守 §92a:agentRunner 的广播
// 路径绝不能为了拿一个展示用的天数去竞争 r.mu。currentGC 为 nil(测试桩 /
// 尚未派发过工具)时传 0,前端按「未知/夜间」处理。
//
// §133 红线:WolfWhisper **绝不**调用本方法。
func (r *agentRunner) recordLastSpeech(text, kind string) {
	if r == nil || r.agent == nil {
		return
	}
	round := 0
	if r.currentGC != nil {
		round = r.currentGC.Round
	}
	// 缓存过滤后文本供 EmotionSwitchSpeak 覆写 kind 时复用(见字段注释)。
	// whisper 传 "",不污染缓存。
	if text != "" {
		r.lastSpeechText = text
	}
	r.agent.RecordLastSpeech(text, kind, round)
}

func (r *agentRunner) errStr(e *errcode.Error) string {
	if e == nil {
		return "ok"
	}
	return fmt.Sprintf("errcode=%d msg=%s", e.Code, e.Message)
}

// RecordLog / GameLogID 是 2026-07-10 §4 model_game_log hook 的 ToolRunner
// 接口实现。agentRunner 持有 Agent 反向引用,直接代理 a.RecordLog /
// a.GameLogID。nil-safe:agent=nil 或 a.RecordLog=nil 时都返回 nil/"",与
// 测试桩 / 老代码路径兼容。
func (r *agentRunner) RecordLog() *agentcore.RecordLogService {
	if r.agent == nil {
		return nil
	}
	return r.agent.RecordLog
}

func (r *agentRunner) GameLogID() string {
	if r.agent == nil {
		return ""
	}
	return r.agent.GameLogID
}

// ConsumeMirrorExposeFlag §20260811-10 U1 — 读取并清空 mirror_check 标志。
//
// agent 在 LLM 调用前检测到 flag=true → BuildSystemPrompt 追加「请如实写下
// 真实身份」指令,然后调本方法清空,避免重复注入。ToolRunner 接口扩展,
// 仅本批次新增(向后兼容:旧实现返回 false 即 no-op)。
func (r *agentRunner) ConsumeMirrorExposeFlag() bool {
	room := r.mgr.getRoom(r.roomID)
	if room == nil {
		return false
	}
	return room.ConsumeMirrorExposeActive(r.seat)
}

// MirrorExposeFlagNonConsuming §20260811-10 U1 — 仅读取,不消费。
// 用于 buildAgentContextLocked 拼 GameContext 时决定是否注入 MirrorExposePromptBlock。
func (r *agentRunner) MirrorExposeFlagNonConsuming() bool {
	room := r.mgr.getRoom(r.roomID)
	if room == nil {
		return false
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.mirrorExposeActive == nil {
		return false
	}
	return room.mirrorExposeActive[int(r.seat)]
}

// BotUserID 是 wwplayer.BotRunner 接口的实现,返回 bot 玩家 user_id。
// 供 DispatchTool 内部 dispatchToolRecordAction 调 RecordAction 时
// 填充 bot_user_id 字段(必填)。nil-safe:无 agent 时返回 ""。
func (r *agentRunner) BotUserID() string {
	return r.botUserID
}

// CurrentPhase 返回当前房间的 phase(短时持锁读取)。供 dispatchToolRecordAction
// 调 RecordAction 时填充 phase 字段。无房间 / 锁竞争 / agent=nil 时返回 ""。
func (r *agentRunner) CurrentPhase() string {
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok || mgrRoom == nil {
		return ""
	}
	if !lockRoomBriefly(mgrRoom, 100*time.Millisecond) {
		return ""
	}
	defer mgrRoom.mu.Unlock()
	if mgrRoom.State == nil {
		return ""
	}
	return mgrRoom.State.Phase.String()
}

// CurrentStatus 返回当前房间的 status("playing"/"over"/"filling")。供
// ScrubVerdictClaim 等 status-conditional 过滤器判断对局是否已结束。
// 与 CurrentPhase 同锁风格(短时持锁 100ms 兜底)。无房间/锁竞争返回 ""。
//
// BUG-R232-P1-01 (2026-08-02): 服务端硬性屏蔽 verdict claim(对局未结束时
// Agent 宣告「游戏结束/阵营胜利/赛后总结」等)需要 status 字段;不在 Speak /
// SpeakAuto / SpeakWithThought 调用点重新实现状态机,而是直接读权威
// GameState.Status 字段。
func (r *agentRunner) CurrentStatus() string {
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok || mgrRoom == nil {
		return ""
	}
	if !lockRoomBriefly(mgrRoom, 100*time.Millisecond) {
		return ""
	}
	defer mgrRoom.mu.Unlock()
	if mgrRoom.State == nil {
		return ""
	}
	return mgrRoom.State.Status
}

// wakeAll pings every bot agent in the room to re-evaluate state after this
// runner mutated the game.
//
// BUG-WEREWOLF-P0-4: agent tool calls go in-process via Action_* and bypass
// the WS handler (handleWerewolfAction) that normally calls
// broadcastWerewolfState → wakeWerewolfAgents. Without this nudge, the moment
// one agent advances the phase (e.g. wolf_kill → night_seer), the next seat
// in the chain is never woken and the game stalls one step in. We wake only
// seats whose MyTurn is true (WakeActingAgents) so the chain advances without
// forcing the other bots to burn an LLM round-trip replying "保持沉默".
func (r *agentRunner) wakeAll() {
	r.mgr.WakeActingAgents(r.roomID, "state_change")
}

// WolfKill — §20260810-04 U2: reason 为 LLM 附带的刀人理由(≤30 字,
// 引擎内 rune 截断;仅狼 bot GameContext 可见),透传到 Action_WolfKill。
func (r *agentRunner) WolfKill(target int, reason string) (string, error) {
	_, e := r.mgr.Action_WolfKill(r.roomID, r.botUserID, Seat(target), reason)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) SeerCheck(target int) (string, error) {
	_, e := r.mgr.Action_SeerCheck(r.roomID, r.botUserID, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) WitchAct(action string, target int) (string, error) {
	_, e := r.mgr.Action_Witch(r.roomID, r.botUserID, action, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// GuardProtect §134 守卫夜间守护动作。target = -1 表示空守(NoSeat);其他为
// 目标座位号。映射到引擎 Action_GuardProtect(r.roomID, r.botUserID, target)。
func (r *agentRunner) GuardProtect(target int) (string, error) {
	_, e := r.mgr.Action_GuardProtect(r.roomID, r.botUserID, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// KnightDuel §198 骑士白天决斗动作。
// target = -1 表示「本轮放弃不消耗机会」(枚举保留,见 tools.go BuildTools);
// 其他值为发动决斗目标座位号。映射到引擎 Action_KnightDuel(r.roomID, r.botUserID, target)。
//
// 注意:不调用 r.wakeAll() 在 action 失败分支(LLM 收到错误后可继续用 speak 发言);
// 但与 GuardProtect 保持一致,在成功分支调 wakeAll 以让下一阶段玩家尽早行动。
func (r *agentRunner) KnightDuel(target int) (string, error) {
	_, e := r.mgr.Action_KnightDuel(r.roomID, r.botUserID, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// DemonHunterHunt §猎魔人 夜间狩猎 — 派发到 mgr.Action_DemonHunterHunt。
// 与 KnightDuel / GuardProtect 完全同构。
func (r *agentRunner) DemonHunterHunt(target int) (string, error) {
	_, e := r.mgr.Action_DemonHunterHunt(r.roomID, r.botUserID, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) Speak(text string) (string, error) {
	if r.chatSvc == nil {
		return "chat unavailable", nil
	}
	// BUG-R74-1 (2026-07-09): 上限检查。SpeakLimiter.Allow() 在 runner 这一侧
	// 强制执行(此前 agent.allowSpeakDaytime 总是返 true,导致 Bot 5 分钟 16 条
	// 刷屏)。通过后 Mark() 写回时间戳,不通过返回 hint 让 LLM 收敛。
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil && !r.speakLimiter.Allow() {
		logger.L().Warn("agentRunner.Speak rate-limited",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak rate-limited: 距上次公开发言不足 45s,请等待或用 idle_think 记录思考", nil
	}
	// 2026-07-14 BUG-R116-03: 同座位单轮发言冷却(默认 60s),在 SpeakLimiter 45s
	// 之外再加一层 room 级全局保护,防止 GLM 等快模型在 speak_floor / spectator_wake
	// 叠加下一轮内刷屏(同一座位 6 分钟 3 条)。
	if !r.allowSameSeatPublicSpeak() {
		logger.L().Warn("agentRunner.Speak same-seat cooldown",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak rate-limited: 同座位单轮发言冷却中,请等待", nil
	}
	// 2026-07-15 BUG-R124-UI-001: 同座位单阶段发言次数上限(默认 3),在 cooldown
	// 之外再加一层 room 级全局保护,防止 Qwen3.7-Max / GLM 等快模型在同一发言
	// 阶段反复补充发言(R124 报告: Bot 9 占 11/26 = 42%)。达到上限直接拒绝,
	// 提示 LLM 收敛到 idle_silent。
	if !r.allowSameSeatSpeakThisPhase() {
		logger.L().Warn("agentRunner.Speak phase-count exceeded",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak rate-limited: 同座位单阶段发言次数已达上限,请保持沉默或使用 idle_silent", nil
	}
	// R132 (2026-07-16)「公屏猜疑化」重构:
	// 原 ScrubIdentityLeak 整段 hide 改为 MysteryMaskText,三类处理:
	//   - MysteryAllow: 心理战 / 阵营叙事 / 悍跳 原文发出(玩家可见);
	//   - MysteryDeferToGame: 隐晦身份(用药/查验) 原文 + 反馈 LLM 风险提示;
	//   - MysteryFuzzyIntent: 真 bug(0-indexed 座位号/系统元信息) 改写。
	// 这是把"hide 心理战文本"换成"心理战合法 + 玩家自行识破"。原 R74-2
	// 是为了让 LLM 不暴露阵营,R132 起让心理战回到玩家手里 — LLM 学会
	// 表达心理战,玩家获得最强的阵营推断武器(看到原文)。
	if r.filterCfg.EnableIdentityFilter {
		if res := MysteryMaskText(text); res.Hit {
			logger.L().Info("agentRunner.Speak mystery-mask hit",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("original", text), zap.String("masked", res.Text),
				zap.String("mode", res.Mode.String()),
				zap.Strings("categories", res.HitCategories))
			text = res.Text
			// R132 反馈拼到 result 末尾:让 LLM 在下一轮学到"如何铺垫化"
			mysteryHint := ComposeMysteryHint(res)
			if mysteryHint != "" {
				r.lastMysteryHint = mysteryHint
			}
		}
	}
	// BUG-R11-001 (2026-08-17): 狼队内沟通公屏泄漏 hard-reject。
	// R132 把「狼队-击杀意图」定为 MysteryAllow(原文放行),§R10-NEW3 的
	// regex 又只接在 ScrubIdentityLeak(主路径死代码),导致 Bot 9 号
	// 「狼人队友，今晚先刀7号」原文上公屏。此处与 R93-P1 同范式:狼阵营
	// bot 命中即整条不广播,hint 引导改用 wolf_whisper。详见 speak_wolfguard.go。
	if r.isWolfSeat() {
		if cat, hit := CheckWolfCoordinationLeak(text); hit {
			logger.L().Info("agentRunner.Speak wolf-coordination rejected (R11-001)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("pattern", cat), zap.String("rejected", text))
			return "speak rejected: " + wolfCoordinationRejectHint, nil
		}
	}
	// BUG-R13-NEW-001 (2026-08-17): 神职未来时计划泄漏公屏 hard-reject。
	// R13 22:30 报告 §二.P0-1 实测 Bot 4 号 (MiniMax M3) 公开发言
	// 「今晚查 11 号。理由:发言模板化强、像悍跳预言家。」—— 真预言家
	// 首夜已验完人,公屏应直接报「昨夜我验了 X 是 Y」,不可能用「今晚要查」
	// 这种未来时句式。该 leak 不分阵营(预言家/女巫/守卫都可能误用,狼悍跳
	// 神职也可能复用此模式),故**不门控 isWolfSeat**,全 bot 生效。
	// 详见 speak_wolfguard.go::CheckFutureTenseSkillPlan。
	if cat, hit := CheckFutureTenseSkillPlan(text); hit {
		logger.L().Info("agentRunner.Speak future-tense-skill-plan rejected (R13-NEW-001)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("pattern", cat), zap.String("rejected", text))
		return "speak rejected: " + futureTenseSkillPlanRejectHint, nil
	}
	// BUG-R70-P2 (2026-07-09): 跨消息级内容去重。SpeakLimiter 只按时间节流,
	// 拦截不了"30s 内说相同主题"。recentSpeakDedup 在 90s 窗口内识别
	// Jaccard ≥ 0.6 的复读,直接拒绝并返回 hint 让 LLM 在下一轮收敛。
	if r.recentSpeakDedup != nil {
		if allowed, hint := r.recentSpeakDedup.CheckAndRecord(text, time.Now()); !allowed {
			return hint, nil
		}
	}
	// BUG-R79 P1-NEW (2026-07-10): 反死亡信息幻觉事实校验。MiniMax M3
	// (Seat 3) 在 R79 多次在公屏编造未发生的死亡(典型: "4号走了" 但
	// 4号 仍存活)。Defense-in-depth:在 broadcast 前过 FactCheckDeathClaims,
	// 把与 authoritative 存活列表矛盾的死亡声明改为 hedge 表达("听说X号走了")。
	// 命中时返回 result 字符串追加 hint,促使 LLM 下一轮收敛。
	//
	// R93 P1 (2026-07-11) 升级:改为 hard-reject 模式。R93 报告中 inline
	// 替换 "[已过滤:无可证实的死亡信息]" 让真人观众当场识别出"bot 在
	// 被过滤",等于把过滤机制本身暴露了——观战者可能会利用这种 silent
	// discriminator 推断其他 bot 的内部状态。改为 shouldReject=true:
	// 整条 speak 直接 drop,把 reject hint 反馈给 LLM;真人观众**完全看不到**
	// 任何 marker,与 LLM 自主拒答无差异。
	if knownDead, alive := r.getAuthoritativeDeathsAndAlive(); knownDead != nil || alive != nil {
		if cleaned, hit := wwplayer.FactCheckDeathClaimsWithReject(text, knownDead, alive, true); hit {
			logger.L().Info("agentRunner.Speak death-fact rejected (R93-P1)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", text), zap.String("cleaned", cleaned))
			// hard-reject: 直接 return 让 LLM 在下一轮重新生成,真人
			// spectator 看不到任何 marker / 残留文本。
			return "speak rejected: 编造死亡声明与权威存活列表矛盾,请以 user prompt 中【存活玩家】为准,重新组织你的发言(关注行为/投票/发言模式而非猜测死亡)", nil
		}
	}
	// BUG-R151-FAIRNESS-001 (2026-07-18): 反私聊内容幻觉事实校验。R151
	// 全 AI 局端到端报告暴露的严重公平性缺陷 — Bot 8号 捏造"9号 私聊
	// 告诉我 12号 是狼队友"并诱导投票,导致真实预言家被冤杀。Defense-in-
	// depth:在 broadcast 前过 FactCheckWhisperAttribution,比对权威
	// WhisperInbox;若 seat 从未发私聊给我 → hard-reject,与 R93-P1
	// 一致的策略(inline 替换会暴露过滤机制)。
	//
	// getAuthoritativeWhisperInbox 与 getAuthoritativeDeathsAndAlive 同样
	// 走短时持锁(200ms),失败时降级到 (nil, nil),fact-check 跳过本次
	// 校验(防御性,锁竞争不应阻塞玩家发言)。
	if inbox := r.getAuthoritativeWhisperInbox(); inbox != nil {
		if _, hit := wwplayer.FactCheckWhisperAttribution(text, inbox); hit {
			logger.L().Info("agentRunner.Speak whisper-attribution rejected (R151-FAIRNESS)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", text))
			return "speak rejected: 引用了从未收到过的私聊内容,请仅基于 user prompt 中【发给你的私聊】段引用真实私聊;若要指控他人请改用公开行为/公开发言,不要捏造不存在的私聊", nil
		}
	}
	// R91-P0-2 (2026-07-11): HTML/XML 标签泄露防护。LLM (典型:GLM / DouBao)
	// 在 text 字段末尾输出 `</text></invoke>` 等内部 tool_call XML 闭合标签,
	// 是 Anthropic 协议 XML 编码的尾部残留(LLM 把 streaming 工具调用过程的
	// 闭合片段塞到了最终 text 内)。直接发给公屏 = 把内部实现暴露给所有玩家,
	// 且可能触发 XSS(若前端用 dangerouslySetInnerHTML 渲染)。Defense-in-
	// depth:在 broadcast 前过 StripLLMInternalTags,清除常见 XML/HTML 标签。
	if cleaned, hit := StripLLMInternalTags(text); hit {
		logger.L().Info("agentRunner.Speak internal-tag stripped",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("original", text), zap.String("cleaned", cleaned))
		text = cleaned
	}
	// BUG-R232-P1-01 (2026-08-02): verdict claim guard. 对局进行中
	// (status != "over")时服务端硬性替换「游戏结束/阵营胜利/赛后总结」等
	// 措辞为中性短语,这是 §R232 提示词硬约束的服务端兜底。Speak /
	// SpeakAuto / SpeakWithThought 三条广播路径都需走该过滤器。
	if status := r.CurrentStatus(); status != "" && status != "over" {
		if scrubbed, hit := ScrubVerdictClaim(text, status); hit {
			logger.L().Info("agentRunner.Speak verdict-claim scrubbed (R232-P1-01)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("status", status),
				zap.String("original", text), zap.String("scrubbed", scrubbed))
			text = scrubbed
		}
	}
	// BUG Round 40 §95: PhasePreWolves 阶段走强制发言计数路径(不走 PhaseSpeak
	// 的 broadcast 链),先在持锁位置累加 PreWolvesSpeakCount,再 broadcast。
	// 切阶段判断交给 room.watchdog 5s tick 的 advancePreWolvesRoundLocked。
	if isPreWolvesPhase(r.mgr, r.roomID) {
		_ = accumulatePreWolvesSpeakLocked(r.mgr, r.roomID, r.seat)
	}
	// BUG-R233-P1-01 (2026-08-02): 见 SpeakAuto 同源注释。Speak 是 LLM 显式调
	// speak 工具的入口,过滤链(MysteryMaskText / StripLLMInternalTags /
	// ScrubVerdictClaim)改写后可能变空。复检 TrimSpace == "" 时直接硬拒,不消耗
	// 限流令牌,与 R93-P1 hard-reject 语义对齐。
	if strings.TrimSpace(text) == "" {
		logger.L().Info("agentRunner.Speak blank-text rejected (R233-P1-01)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.Int("orig_len", len(text)))
		return "speak rejected: 过滤后内容为空(LLM 仅产出空白/标签),请重新组织有效发言或改用 idle_silent", nil
	}
	_, err := r.chatSvc.SendFromBot(r.roomID, r.botUserID, r.botAccount, r.modelKey, text)
	if err != nil {
		return "speak failed: " + err.Error(), err
	}
	// R74-1: 成功后写回时间戳(Mark 只在 broadcast 成功后调用,失败不计)。
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil {
		r.speakLimiter.Mark()
	}
	// 2026-07-14 BUG-R116-03: 公开发言成功后更新 room 级同座位冷却时间戳。
	r.markSameSeatPublicSpeak()
	// R132 (2026-07-16): 把 lastMysteryHint 拼到工具 result 末尾,LLM 看到
	// "已发送 + 玩家可见 + 风险提示",学习"下次如何铺垫化"。
	resultText := "sent"
	if r.lastMysteryHint != "" {
		resultText += "\n" + r.lastMysteryHint
		r.lastMysteryHint = "" // 用完即清,避免污染下次发言
	}
	// 2026-07-15 BUG-R124-UI-001: 公开发言成功后增加该座位本阶段发言计数。
	r.bumpSeatSpeakCountThisPhase()
	// 2026-08-05 §Agent聊天显示优化 (B4):广播成功 → 落 BotTranscript.LastSpeech*
	// 并立即触发 game.state 推送,座位卡气泡与公屏同帧更新。
	r.recordLastSpeech(text, "speak")
	// BUG-WEREWOLF-P0-9: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	r.wakeAll()
	return resultText, nil
}

// SpeakAuto 是 2026-07-13 §130 重构新增的「text-block 自动发言」入口。
//
// 动机:参考 CluadeCode的Anthropic协议-ResposeBody 用例,Claude Code Agent
// 在 assistant content 中同时输出 text 块(自然语言发言) + tool_use 块(功能性
// 操作)。发言**不**通过 tools 字段,而是直接以 text 块呈现;LLM 在需要执行动作
// 时才调 tool_use。狼人杀 13 人局 Bot 当前强制所有发言走 speak / speak_with_thought
// / interject tool,浪费一个 tool_use round-trip 且让 LLM 推理链与公开文本耦合。
//
// SpeakAuto 是 Speak 的轻量别名,**完全复用**同一条过滤链(rate-limit /
// identity-leak scrub / fact-check death claims / XML strip / PreWolves
// counter / SendFromBot → RecordRoomMessage → ChatHistoryQueue)。run.go 在
// LLM 返回 assistant content 且 ToolUses() 列表中没有 speak/speak_with_thought
// /interject 时,把 resp.Text() 作为"自动发言"喂给 SpeakAuto。
//
// 关键不变式:
//   - 走 speakLimiter + recentSpeakDedup + MysteryMaskText(R132 新替换 ScrubIdentityLeak) + FactCheckDeathClaims +
//     StripLLMInternalTags + chatSvc.SendFromBot + chatQueue.Append — 与 Speak
//     一字不差;LLM 改用 text block 后,filter / limiter / 500K 队列 自动适配。
//   - 不调 wakeAll()(Speak 会 wakeAll,因为 speak tool 的 saidSomething=true
//     会让 handleEvent 直接 return,而这里 SpeakAuto 是 handleEvent 末尾的
//     补充路径,handleEvent 自然结束后由事件循环驱动下次 wake)。
//   - 不分配 SaidSomething 标记 — run.go 通过返回值 (非空 + nil error) 视为
//     "已发言",从而触发后续的 early-return,避免在同一次 LLM round 中再调
//     tool_use dispatch。
//
// 与 Speak 的微小差异:SpeakAuto 不调 wakeAll()(避免与 handleEvent 自身的
// 退出流程抢锁竞争);Speak 必须 wakeAll 是因为 Speak 被 tool dispatch 调,
// 之后还有 saidSomething 早退 + 等待新事件,而 SpeakAuto 紧跟在 tool
// dispatch 循环之后,handleEvent 已经准备 return,无需再唤醒一次。
func (r *agentRunner) SpeakAuto(text string) (string, error) {
	if r.chatSvc == nil {
		return "chat unavailable", nil
	}
	// 共享 45s 令牌桶(与 Speak / SpeakWithThought / Interject 一致)。
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil && !r.speakLimiter.Allow() {
		logger.L().Warn("agentRunner.SpeakAuto rate-limited",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak_auto rate-limited: 距上次公开发言不足 45s", nil
	}
	// 2026-07-14 BUG-R116-03: 同座位单轮发言冷却(默认 60s),与 Speak 共享。
	if !r.allowSameSeatPublicSpeak() {
		logger.L().Warn("agentRunner.SpeakAuto same-seat cooldown",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak_auto rate-limited: 同座位单轮发言冷却中,请等待", nil
	}
	// 2026-07-15 BUG-R124-UI-001: 同座位单阶段发言次数上限(默认 3)。
	if !r.allowSameSeatSpeakThisPhase() {
		logger.L().Warn("agentRunner.SpeakAuto phase-count exceeded",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak_auto rate-limited: 同座位单阶段发言次数已达上限,请保持沉默或使用 idle_silent", nil
	}
	// R132 (2026-07-16)「公屏猜疑化」:同 Speak 的处理。
	if r.filterCfg.EnableIdentityFilter {
		if res := MysteryMaskText(text); res.Hit {
			logger.L().Info("agentRunner.SpeakAuto mystery-mask hit",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("original", text), zap.String("masked", res.Text),
				zap.String("mode", res.Mode.String()),
				zap.Strings("categories", res.HitCategories))
			text = res.Text
			if hint := ComposeMysteryHint(res); hint != "" {
				r.lastMysteryHint = hint
			}
		}
	}
	// BUG-R11-001 (2026-08-17): 狼队内沟通公屏泄漏 hard-reject,与 Speak 同源。
	if r.isWolfSeat() {
		if cat, hit := CheckWolfCoordinationLeak(text); hit {
			logger.L().Info("agentRunner.SpeakAuto wolf-coordination rejected (R11-001)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("pattern", cat), zap.String("rejected", text))
			return "speak_auto rejected: " + wolfCoordinationRejectHint, nil
		}
	}
	// BUG-R13-NEW-001 (2026-08-17): 神职未来时计划 hard-reject,与 Speak 同源。
	// 详见 Speak 注释与 speak_wolfguard.go::CheckFutureTenseSkillPlan。
	if cat, hit := CheckFutureTenseSkillPlan(text); hit {
		logger.L().Info("agentRunner.SpeakAuto future-tense-skill-plan rejected (R13-NEW-001)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("pattern", cat), zap.String("rejected", text))
		return "speak_auto rejected: " + futureTenseSkillPlanRejectHint, nil
	}
	// BUG-R70-P2 跨消息级去重。
	if r.recentSpeakDedup != nil {
		if allowed, hint := r.recentSpeakDedup.CheckAndRecord(text, time.Now()); !allowed {
			return hint, nil
		}
	}
	// R93-P1 death-fact hard-reject。
	if knownDead, alive := r.getAuthoritativeDeathsAndAlive(); knownDead != nil || alive != nil {
		if cleaned, hit := wwplayer.FactCheckDeathClaimsWithReject(text, knownDead, alive, true); hit {
			logger.L().Info("agentRunner.SpeakAuto death-fact rejected (R93-P1)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", text), zap.String("cleaned", cleaned))
			return "speak_auto rejected: 编造死亡声明与权威存活列表矛盾", nil
		}
	}
	// BUG-R158-FAIRNESS-001 (2026-07-19): 反私聊内容幻觉事实校验补全。
	// R151 (commit 7451782) 仅在 Speak() 通道过 FactCheckWhisperAttribution,
	// 遗漏了 SpeakAuto / SpeakWithThought / Interject 三条同样调 chatSvc
	// 广播的路径。本轮 R158 测试在 spectator 视图直接看到 Bot 通过
	// SpeakAuto / SpeakWithThought / Interject 绕过 filter 公屏捏造
	// 「X号 私聊我指挥刀Y号」并成功广播, 完全复现 R151 修复前的问题。
	// 修复:在 broadcast 前过 FactCheckWhisperAttribution, 与 Speak 一致。
	if inbox := r.getAuthoritativeWhisperInbox(); inbox != nil {
		if _, hit := wwplayer.FactCheckWhisperAttribution(text, inbox); hit {
			logger.L().Info("agentRunner.SpeakAuto whisper-attribution rejected (R158-FAIRNESS)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", text))
			return "speak_auto rejected: 引用了从未收到过的私聊内容,请仅基于 user prompt 中【发给你的私聊】段引用真实私聊;若要指控他人请改用公开行为/公开发言,不要捏造不存在的私聊", nil
		}
	}
	// R91-P0-2 XML/HTML tag strip。
	if cleaned, hit := StripLLMInternalTags(text); hit {
		logger.L().Info("agentRunner.SpeakAuto internal-tag stripped",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("original", text), zap.String("cleaned", cleaned))
		text = cleaned
	}
	// BUG-R232-P1-01 (2026-08-02): verdict claim guard. 对局进行中(status !=
	// "over")时,服务端硬性替换「游戏结束/阵营胜利/赛后总结」等措辞为中性
	// 「我先静观其变」短语。这是 §R232 提示词硬约束的服务端兜底,与
	// ScrubIdentityLeak / FactCheckDeathClaims 共享同一「协议层优于 UI 层」
	// 哲学。
	if status := r.CurrentStatus(); status != "" && status != "over" {
		if scrubbed, hit := ScrubVerdictClaim(text, status); hit {
			logger.L().Info("agentRunner.SpeakAuto verdict-claim scrubbed (R232-P1-01)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("status", status),
				zap.String("original", text), zap.String("scrubbed", scrubbed))
			text = scrubbed
		}
	}
	if isPreWolvesPhase(r.mgr, r.roomID) {
		_ = accumulatePreWolvesSpeakLocked(r.mgr, r.roomID, r.seat)
	}
	// BUG-R233-P1-01 (2026-08-02): 过滤链(MysteryMaskText / StripLLMInternalTags /
	// ScrubVerdictClaim)改写 text 后可能清空全部非空字符,只剩空白。LLM 偶尔也会
	// 在 text 块直接产出纯空白。这种情况下若仍走 SendFromBot 会产生「空气泡」,
	// 且占用 speakLimiter / 同座位冷却 / 阶段计数 3 类令牌,等同用看不见的消息挤
	// 掉真实发言机会。修复:在 SendFromBot 前复检 TrimSpace(text) == "" 时直接
	// 拒绝返回,不走 Mark / 不走 bump 计数,与 R93-P1 hard-reject 语义对齐。
	if strings.TrimSpace(text) == "" {
		logger.L().Info("agentRunner.SpeakAuto blank-text rejected (R233-P1-01)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.Int("orig_len", len(text)))
		return "speak_auto rejected: 过滤后内容为空(LLM 仅产出空白/标签),请在下轮生成有效发言文本或调用 idle_silent", nil
	}
	_, err := r.chatSvc.SendFromBot(r.roomID, r.botUserID, r.botAccount, r.modelKey, text)
	if err != nil {
		return "speak_auto failed: " + err.Error(), err
	}
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil {
		r.speakLimiter.Mark()
	}
	// 2026-07-14 BUG-R116-03: 公开发言成功后更新 room 级同座位冷却时间戳。
	r.markSameSeatPublicSpeak()
	// 2026-07-15 BUG-R124-UI-001: 公开发言成功后增加该座位本阶段发言计数。
	r.bumpSeatSpeakCountThisPhase()
	// 2026-08-05 §Agent聊天显示优化 (B4):与 Speak 同源 — 广播成功后落
	// LastSpeech* 并触发即时 game.state 推送。kind 同为 "speak"(§130 的
	// text-block 自动发言在玩家视角与显式 speak 完全等价)。
	r.recordLastSpeech(text, "speak")
	// R132 (2026-07-16): 拼 lastMysteryHint 到工具 result。
	resultText := "sent [§130 text-block 自动发言]"
	if r.lastMysteryHint != "" {
		resultText += "\n" + r.lastMysteryHint
		r.lastMysteryHint = ""
	}
	// 不调 wakeAll():SpeakAuto 在 handleEvent 工具派发循环之后立即执行,
	// handleEvent 准备退出,事件循环会因 chat.message 帧自然驱动下次 wake。
	return resultText, nil
}

// SpeakWithThought 是 2026-07-10 §119「心口不一」机制的核心实现。
// publicText 经 dedupSpeakText / ScrubIdentityLeak / FactCheckDeathClaims
// 处理后通过 chatSvc.SendFromBot 广播给所有玩家(对外可见,等同于 speak);
// internalThought **绝不**进入 chat_message 表或 chat_history 队列,
// 只通过 wwplayer.RecordLastThought 写入 BotTranscript.FullThinking 与
// RecentMessages 末尾,供人类观战者在「Agent 思考」面板查看。
//
// 关键约束:
//  1. internalThought 不进 chat_message 表 → 真人玩家看不到;
//  2. internalThought 不进 chat_history 队列 → 其他 bot 看不到;
//  3. internalThought 进 BotTranscript.FullThinking → 观战者通过
//     AgentInteractionPanel 组件可见(仅观战 + 本人)。
//  4. 限流与 speak 共用同一个 45s 令牌桶,共享去重窗口(防止 LLM 用
//     speak + speak_with_thought 绕过限流刷屏)。
func (r *agentRunner) SpeakWithThought(publicText, internalThought string) (string, error) {
	if r.chatSvc == nil {
		return "chat unavailable", nil
	}
	// 共享同一 speakLimiter (45s 间隔) — 防止 LLM 用 speak + speak_with_thought
	// 两条工具在同一窗口绕过限流刷屏。R74-1 同样的上限检查语义。
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil && !r.speakLimiter.Allow() {
		logger.L().Warn("agentRunner.SpeakWithThought rate-limited",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak_with_thought rate-limited: 距上次公开发言不足 45s", nil
	}
	// 2026-07-14 BUG-R116-03: 同座位单轮发言冷却(默认 60s),与 Speak 共享。
	if !r.allowSameSeatPublicSpeak() {
		logger.L().Warn("agentRunner.SpeakWithThought same-seat cooldown",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak_with_thought rate-limited: 同座位单轮发言冷却中,请等待", nil
	}
	// 2026-07-15 BUG-R124-UI-001: 同座位单阶段发言次数上限(默认 3),与 Speak 共享。
	if !r.allowSameSeatSpeakThisPhase() {
		logger.L().Warn("agentRunner.SpeakWithThought phase-count exceeded",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
		return "speak_with_thought rate-limited: 同座位单阶段发言次数已达上限", nil
	}
	// R132 (2026-07-16)「公屏猜疑化」:同 Speak 的处理;publicText 走 MysteryMaskText
	// (internalThought 走 BotTranscript,不经 filter,与 §119 心口不一机制一致)。
	if r.filterCfg.EnableIdentityFilter {
		if res := MysteryMaskText(publicText); res.Hit {
			logger.L().Info("agentRunner.SpeakWithThought mystery-mask hit",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("original", publicText), zap.String("masked", res.Text),
				zap.String("mode", res.Mode.String()),
				zap.Strings("categories", res.HitCategories))
			publicText = res.Text
			if hint := ComposeMysteryHint(res); hint != "" {
				r.lastMysteryHint = hint
			}
		}
	}
	// BUG-R11-001 (2026-08-17): 狼队内沟通公屏泄漏 hard-reject,与 Speak 同源。
	if r.isWolfSeat() {
		if cat, hit := CheckWolfCoordinationLeak(publicText); hit {
			logger.L().Info("agentRunner.SpeakWithThought wolf-coordination rejected (R11-001)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("pattern", cat), zap.String("rejected", publicText))
			return "speak_with_thought rejected: " + wolfCoordinationRejectHint, nil
		}
	}
	// BUG-R13-NEW-001 (2026-08-17): 神职未来时计划 hard-reject,与 Speak 同源。
	if cat, hit := CheckFutureTenseSkillPlan(publicText); hit {
		logger.L().Info("agentRunner.SpeakWithThought future-tense-skill-plan rejected (R13-NEW-001)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("pattern", cat), zap.String("rejected", publicText))
		return "speak_with_thought rejected: " + futureTenseSkillPlanRejectHint, nil
	}
	if r.recentSpeakDedup != nil {
		if allowed, hint := r.recentSpeakDedup.CheckAndRecord(publicText, time.Now()); !allowed {
			return hint, nil
		}
	}
	if knownDead, alive := r.getAuthoritativeDeathsAndAlive(); knownDead != nil || alive != nil {
		if cleaned, hit := wwplayer.FactCheckDeathClaimsWithReject(publicText, knownDead, alive, true); hit {
			logger.L().Info("agentRunner.SpeakWithThought death-fact rejected (R93-P1)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", publicText), zap.String("cleaned", cleaned))
			// R93-P1 hard-reject: 与 Speak 共享同一策略。
			return "speak_with_thought rejected: 编造死亡声明与权威存活列表矛盾,请以 user prompt 中【存活玩家】为准,重新组织你的发言", nil
		}
	}
	// BUG-R158-FAIRNESS-001 (2026-07-19): 反私聊内容幻觉事实校验补全。
	// R151 (commit 7451782) 仅在 Speak() 通道过 FactCheckWhisperAttribution,
	// 遗漏了 SpeakAuto / SpeakWithThought / Interject 三条同样调 chatSvc
	// 广播的路径。本轮 R158 测试在 spectator 视图直接看到 Bot 通过
	// SpeakWithThought 绕过 filter 公屏捏造「X号 私聊我指挥刀Y号」并
	// 成功广播, 完全复现 R151 修复前的问题。修复:在 broadcast 前过
	// FactCheckWhisperAttribution, 与 Speak 一致。
	if inbox := r.getAuthoritativeWhisperInbox(); inbox != nil {
		if _, hit := wwplayer.FactCheckWhisperAttribution(publicText, inbox); hit {
			logger.L().Info("agentRunner.SpeakWithThought whisper-attribution rejected (R158-FAIRNESS)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("rejected", publicText))
			return "speak_with_thought rejected: 引用了从未收到过的私聊内容,请仅基于 user prompt 中【发给你的私聊】段引用真实私聊;若要指控他人请改用公开行为/公开发言,不要捏造不存在的私聊", nil
		}
	}
	// R91-P0-2 (2026-07-11): HTML/XML 标签泄露防护 — 与 Speak 共享同一
	// StripLLMInternalTags。
	if cleaned, hit := StripLLMInternalTags(publicText); hit {
		logger.L().Info("agentRunner.SpeakWithThought internal-tag stripped",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.String("original", publicText), zap.String("cleaned", cleaned))
		publicText = cleaned
	}
	// BUG-R232-P1-01 (2026-08-02): verdict claim guard. 与 SpeakAuto 同源。
	if status := r.CurrentStatus(); status != "" && status != "over" {
		if scrubbed, hit := ScrubVerdictClaim(publicText, status); hit {
			logger.L().Info("agentRunner.SpeakWithThought verdict-claim scrubbed (R232-P1-01)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("status", status),
				zap.String("original", publicText), zap.String("scrubbed", scrubbed))
			publicText = scrubbed
		}
	}
	if isPreWolvesPhase(r.mgr, r.roomID) {
		_ = accumulatePreWolvesSpeakLocked(r.mgr, r.roomID, r.seat)
	}
	// BUG-R233-P1-01 (2026-08-02): 见 SpeakAuto 同源注释。
	if strings.TrimSpace(publicText) == "" {
		logger.L().Info("agentRunner.SpeakWithThought blank-text rejected (R233-P1-01)",
			zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
			zap.Int("orig_len", len(publicText)))
		return "speak_with_thought rejected: 过滤后内容为空(LLM 仅产出空白/标签),请重新组织有效发言或改用 idle_silent", nil
	}
	// 1. 公开 text 走 chatSvc.SendFromBot — 与 Speak 完全一致的广播路径,
	//    对所有玩家(包括其他 bot + 真人 + 观战者)可见。
	_, err := r.chatSvc.SendFromBot(r.roomID, r.botUserID, r.botAccount, r.modelKey, publicText)
	if err != nil {
		return "speak_with_thought failed: " + err.Error(), err
	}
	// 2. internalThought **仅**写入 BotTranscript — 不进 chat_message 表
	//    / chat_history 队列 / 任何公开 channel。这是「心口不一」机制的
	//    物理隔离边界:违反此约束等于让 other player 能读 bot 的内心独白,
	//    整个欺骗机制将失效。
	//
	//    BUG-R200-SEC-01 (2026-07-24): internal_thought 同样需要过滤 —
	//    R200 实测 Kimi k3 在 BotTranscript.HeartThought 写出「(1号是1号Bot、
	//    3号是4号Bot...)」逐座位 bot 身份映射,虽不进公屏,但通过
	//    BotTranscript 下发给人类观战者 + server-side logger.Info 落盘后,
	//    所有运维 / 测试 Agent 可读取,与「心口不一」设计意图冲突。
	//    修复:在写入前过 MysteryMaskText(FuzzyIntent 改写) + ScrubIdentityLeak
	//    ([已过滤] 替换),与 publicText / streaming-delta 共用同一套规则集合。
	if r.filterCfg.EnableIdentityFilter && internalThought != "" {
		if res := MysteryMaskText(internalThought); res.Hit {
			logger.L().Info("agentRunner.SpeakWithThought internal-thought mystery-mask hit (R200-SEC-01)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
				zap.String("mode", res.Mode.String()),
				zap.Strings("categories", res.HitCategories))
			internalThought = res.Text
		}
	}
	if internalThought != "" {
		if cleaned, hit := ScrubIdentityLeak(internalThought); hit {
			logger.L().Info("agentRunner.SpeakWithThought internal-thought scrub hit (R200-SEC-01)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
			internalThought = cleaned
		}
	}
	if r.agent != nil {
		r.agent.RecordLastThought(internalThought)
	}
	if r.filterCfg.EnableRateLimit && r.speakLimiter != nil {
		r.speakLimiter.Mark()
	}
	// 2026-07-14 BUG-R116-03: 公开发言成功后更新 room 级同座位冷却时间戳。
	r.markSameSeatPublicSpeak()
	// 2026-07-15 BUG-R124-UI-001: 公开发言成功后增加该座位本阶段发言计数。
	r.bumpSeatSpeakCountThisPhase()
	// 2026-08-05 §Agent聊天显示优化 (B4):**只记 publicText**。internalThought
	// 已由上方 RecordLastThought 写入 HeartThought(§119 协议层隔离),绝不能
	// 混入 LastSpeech —— 后者是全房可见字段。
	r.recordLastSpeech(publicText, "speak")
	r.wakeAll()
	// R132 (2026-07-16): 拼 lastMysteryHint。
	resultText := "sent [§119 心口不一: 公开 text 已广播, internal_thought 仅记录到 BotTranscript]"
	if r.lastMysteryHint != "" {
		resultText += "\n" + r.lastMysteryHint
		r.lastMysteryHint = ""
	}
	return resultText, nil
}

// isPreWolvesPhase 在持锁短时获取阶段名,避免 agentRunner.Speak 阻塞。
// 走 lockRoomBriefly 限 200ms(与 REST 接口共享同一套机制)。
func isPreWolvesPhase(mgr *WerewolfManager, roomID string) bool {
	r, ok := mgr.rooms[roomID]
	if !ok {
		return false
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return false
	}
	defer r.mu.Unlock()
	if r.State == nil {
		return false
	}
	return r.State.Phase == PhasePreWolves
}

// accumulatePreWolvesSpeakLocked 调 actionSpeakPreWolvesLocked 累加计数。
// 若房间不存在 / 锁竞争 / 阶段不是 pre_wolves,静默返回(不影响 speak 广播)。
func accumulatePreWolvesSpeakLocked(mgr *WerewolfManager, roomID string, seat Seat) error {
	r, ok := mgr.rooms[roomID]
	if !ok {
		return nil
	}
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return nil
	}
	defer r.mu.Unlock()
	return mgr.actionSpeakPreWolvesLocked(r, seat)
}

// allowSameSeatPublicSpeak reports whether this seat's last public speech was
// long enough ago to allow another one. It is a room-level guard on top of the
// per-agent SpeakLimiter (BUG-R116-03).
// 2026-07-15 BUG-R124-UI-001: 在 cooldown 检查前先过 per-phase per-seat 发言次数上限,
// 防止单个 Agent 反复补充发言占据 40%+ 份额(典型:Qwen 3.7-Max 在 PhaseSpeak
// 阶段 11/26=42%)。
func (r *agentRunner) allowSameSeatSpeakThisPhase() bool {
	if r.mgr == nil {
		return true
	}
	room, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return true
	}
	return room.allowSeatSpeakThisPhase(r.seat)
}

// bumpSeatSpeakCountThisPhase 在 SendFromBot 成功后增加该座位本阶段计数。
// nil-safe:房间不存在 / 锁竞争时静默返回。
func (r *agentRunner) bumpSeatSpeakCountThisPhase() {
	if r.mgr == nil {
		return
	}
	room, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return
	}
	room.bumpSeatSpeakCountThisPhase(r.seat)
}

func (r *agentRunner) allowSameSeatPublicSpeak() bool {
	if r.mgr == nil {
		return true
	}
	room, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return true
	}
	if !lockRoomBriefly(room, 200*time.Millisecond) {
		return true // lock contention: be lenient, let SpeakLimiter handle it
	}
	defer room.mu.Unlock()
	if room.seatLastPublicSpeak == nil {
		return true
	}
	last, ok := room.seatLastPublicSpeak[int(r.seat)]
	if !ok {
		return true
	}
	cooldown := time.Duration(cfgWerewolfSameSeatSpeakCooldownSec()) * time.Second
	return time.Since(last) >= cooldown
}

// markSameSeatPublicSpeak records that this seat just made a public speech.
func (r *agentRunner) markSameSeatPublicSpeak() {
	if r.mgr == nil {
		return
	}
	room, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return
	}
	if !lockRoomBriefly(room, 200*time.Millisecond) {
		return
	}
	defer room.mu.Unlock()
	if room.seatLastPublicSpeak == nil {
		room.seatLastPublicSpeak = make(map[int]time.Time)
	}
	room.seatLastPublicSpeak[int(r.seat)] = time.Now()
}

// getAuthoritativeDeathsAndAlive 在持锁短时窗口内(200ms, 与
// isPreWolvesPhase / accumulatePreWolvesSpeakLocked 一致)读取当前房间的
// 已公开死亡座位 + 存活座位。供 agentRunner.Speak 调 FactCheckDeathClaims
// 使用:
//
//   - knownDead: LastNightDeaths(本轮 dawn 阶段已广播)+ 之前 vote 处决
//     (NightKills / 最近一次 DayVote 处决的座位)。
//   - alive: 当前 AliveSeat() 列表(0-indexed)。
//
// 锁竞争/房间不存在时返回 (nil, nil),fact-check 跳过本条 speak(防御性)。
func (r *agentRunner) getAuthoritativeDeathsAndAlive() (knownDead []int, alive []int) {
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return nil, nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return nil, nil
	}
	defer mgrRoom.mu.Unlock()
	if mgrRoom.State == nil {
		return nil, nil
	}
	// 死亡名单:LastNightDeaths(dawn 已广播) + 之前的 vote 处决。
	// 这里只取 LastNightDeaths 作为 authoritative,日间 vote 处决会被加到
	// 下一夜 LastNightDeaths(若 vote 后立即转夜);vote 处决本身在 vote
	// 阶段结束时通过 startDay / dawn 流程进入 LastNightDeaths。
	for _, d := range mgrRoom.State.LastNightDeaths {
		knownDead = append(knownDead, int(d))
	}
	for i := 0; i < MaxPlayers; i++ {
		if mgrRoom.State.AliveSeat(Seat(i)) {
			alive = append(alive, i)
		}
	}
	return knownDead, alive
}

// getAuthoritativeWhisperInbox 在持锁短时窗口内(200ms)读取本 bot 座位
// 真实收到的私聊事件列表。供 agentRunner.Speak 调 FactCheckWhisperAttribution
// 使用 — bot 公开发言引用「X号 私聊告诉我...」时,若 X 不在 inbox 集合
// 内,视为捏造 → hard-reject(R151-FAIRNESS-001 修复)。
//
// 实现:房间状态 whisperInbox map[int][]WhisperEvent(recipient seat → inbox),
// 与 buildAgentContextLocked 写入 gc.WhisperInbox 同源。深拷贝以避免持锁
// 期内 caller 端 mutate(inbox 在 FactCheckWhisperAttribution 里只读,
// 但保守起见复制一份)。
//
// 锁竞争/房间不存在时返回 nil,fact-check 跳过本条 speak(防御性)。
func (r *agentRunner) getAuthoritativeWhisperInbox() []wwtypes.WhisperEvent {
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return nil
	}
	defer mgrRoom.mu.Unlock()
	inbox, ok := mgrRoom.whisperInbox[int(r.seat)]
	if !ok || len(inbox) == 0 {
		return nil
	}
	// Defensive copy.
	out := make([]wwtypes.WhisperEvent, len(inbox))
	copy(out, inbox)
	return out
}

func (r *agentRunner) FinishSpeak() (string, error) {
	_, e := r.mgr.Action_FinishSpeak(r.roomID, r.botUserID)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) Vote(target int) (string, error) {
	_, e := r.mgr.Action_DayVote(r.roomID, r.botUserID, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) FinishVote(tiedRound int) (string, error) {
	_, e := r.mgr.Action_FinishVote(r.roomID, tiedRound)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) StartDay() (string, error) {
	_, e := r.mgr.Action_StartDay(r.roomID)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) SheriffCandidate(target int) (string, error) {
	// SheriffCandidate's engine signature takes only (roomID, userID); the
	// engine uses the caller's seat as the candidate. The agent's `target`
	// input is validated by the engine to equal the caller's seat.
	if int(r.seat) != target {
		msg := fmt.Sprintf("sheriff_candidate: target must be self seat %d, got %d", r.seat, target)
		return msg, fmt.Errorf("%s", msg)
	}
	_, e := r.mgr.Action_SheriffCandidate(r.roomID, r.botUserID)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) SheriffElect() (string, error) {
	// §报告-20260804-03 BUG-06: bot 是真实入座玩家,传 botUserID 走存活校验 ——
	// 与人类走同一条路径(§89「同一语义的动作在 manager/agent 路径必须有
	// 完全一致的引擎调用」)。死亡 bot 的兜底 skip 走 sheriffElectLocked(NoSeat)。
	_, e := r.mgr.Action_SheriffElect(r.roomID, r.botUserID)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// SheriffSetSpeakOrder 是 §20260810-09 警长定序工具实现。
// 警长在 PhaseSheriffOrder 阶段选择发言方向(CW/CCW)与自身位置
// (First/Last),引擎按其选择生成 SpeakOrder 并启动 PhaseSpeak。
//
// 与 §92a 的关系:Action_SheriffSetSpeakOrder 是公开方法,内部已经走
// sheriffSetSpeakOrderLocked(持锁),所以本方法不再触发 r.mu 二次加锁,
// 与 Action_SheriffElect / Action_SheriffStream 等对称。
//
// agent 路径(auto-skip 兜底时 SkipPhaseAction → dispatchToolInner)
// 走本入口;manager/watchdog 路径走 dispatchQuarantinedSkipLocked 直调
// sheriffSetSpeakOrderLocked(带默认值,见 room_agent.go:634)。
func (r *agentRunner) SheriffSetSpeakOrder(direction, selfPos string) (string, error) {
	// §20260810-09 — auto-skip 路径以空字符串进入时,使用默认值(顺时针 +
	// 警长先发言),与 manager 路径 dispatchQuarantinedSkipLocked 兜底
	// 行为一致。LLM 真·调用应该填入合法取值。
	if direction == "" {
		direction = SheriffOrderDefaultDirection
	}
	if selfPos == "" {
		selfPos = SheriffOrderDefaultSelfPos
	}
	// 引擎入口会做白名单校验(direction ∈ {CW,CCW} / selfPos ∈ {First,Last})。
	_, e := r.mgr.Action_SheriffSetSpeakOrder(r.roomID, r.botUserID, direction, selfPos)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) HunterShoot(target int) (string, error) {
	_, e := r.mgr.Action_HunterShoot(r.roomID, r.botUserID, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// SheriffStream 是 2026-07-10 §7 / §12 新增的「警徽流声明」工具实现。
// 预言家警长(slot∈{1,2})在白天声明/撤回警徽流目标。
//
// 接口/引擎依赖:
//   - 调用 r.mgr.Action_SheriffStream(roomID, userID, slot, target),要求引擎
//     实现该方法;若引擎暂不支持(独立后端-dev 任务),方法体会退化返回占位
//     提示字符串 sent back to LLM,让 LLM 收敛。
//   - TODO-12P(engine-missing):Action_SheriffStream 由 backend-dev 实现后替换
//     下方 fallback 分支。
func (r *agentRunner) SheriffStream(slot int, target int) (string, error) {
	if slot != 1 && slot != 2 {
		return "sheriff_stream: slot 仅支持 1(第一)或 2(第二)", nil
	}
	if target < -1 {
		return "sheriff_stream: target 不得小于 -1", nil
	}
	// 校验 caller 是警长 + 预言家(通过持锁短时读取房间状态)。
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return "sheriff_stream: room not found", nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return "sheriff_stream: 锁竞争失败", nil
	}
	isSheriff := mgrRoom.State != nil && mgrRoom.State.SheriffSeat == r.seat
	role := mgrRoom.State.Roles[r.seat]
	mgrRoom.mu.Unlock()
	if !isSheriff {
		return "sheriff_stream: 只有警长可以声明警徽流", nil
	}
	if role != RoleSeer {
		return "sheriff_stream: 只有预言家警长可以声明警徽流", nil
	}

	// 2026-07-10 §7:调引擎 lock-held 派发路径。
	_, e := r.mgr.Action_SheriffStream(r.roomID, r.botUserID, slot, Seat(target))
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// UseProp 是 2026-07-21 道具系统的「使用道具」工具实现。
// 由 agent 侧 DispatchTool("use_prop") 派发到此方法。
// 流程：解析参数 → 持锁调用 PropEngine.UseProp → 广播 → 返回结果。
// 注意：§92a 自死锁约束 — 本方法内部不再调 r.mu 持锁的长函数，
// 仅通过 r.mgr 暴露的 lockHeld 派发路径完成操作。
func (r *agentRunner) UseProp(propID string, target int, payload string) (string, error) {
	if propID == "" {
		return "use_prop rejected: prop_id required", nil
	}
	// payload 长度限制
	if len(payload) > 200 {
		payload = payload[:200]
	}
	// 获取房间状态（持锁短时读）
	mgrRoom, ok := r.mgr.rooms[r.roomID]
	if !ok {
		return "use_prop rejected: room not found", nil
	}
	if mgrRoom.State == nil {
		return "use_prop rejected: game state unavailable", nil
	}
	if !lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		return "use_prop rejected: lock contention", nil
	}
	// 获取目标信息
	roleTo := ""
	if target >= 0 && target < len(mgrRoom.State.Roles) {
		// 2026-08-07 §20260807-04 P1-1 修复:此前误填 r.seat(使用者)的角色,
		// 注入文本按角色差异化(P1-1)需要的是**目标**角色。
		roleTo = mgrRoom.State.Roles[target].String()
	}
	toUserID := ""
	if target >= 0 && target < len(mgrRoom.Seats) {
		toUserID = mgrRoom.Seats[target]
	}
	phaseAtUse := mgrRoom.State.Phase.String()
	roundAtUse := mgrRoom.State.DayNumber
	// 检查 prop 目录
	catEntry, catOK := r.mgr.propCatalog.GetEnabled(propID)
	if !catOK {
		mgrRoom.mu.Unlock()
		return "use_prop rejected: 道具不存在或已禁用", nil
	}
	// 构造请求（持锁时间仅用于读状态，不调用 PropEngine）
	req := PropUseRequest{
		RoomID:     r.roomID,
		FromSeat:   int(r.seat),
		FromUserID: r.botUserID,
		ToSeat:     target,
		ToUserID:   toUserID,
		PropKey:    propID,
		Payload:    payload,
		RoleTo:     roleTo,
		PhaseAtUse: phaseAtUse,
		RoundAtUse: roundAtUse,
	}
	// 获取引擎引用（管理器持有）
	engine := r.mgr.propEngine
	mgrRoom.mu.Unlock()

	if engine == nil {
		return "use_prop rejected: prop engine unavailable", nil
	}

	// 调用 PropEngine（内部会持锁）
	result := engine.UseProp(context.Background(), req, mgrRoom)

	if !result.Success {
		return fmt.Sprintf("use_prop rejected: %s", result.ErrorMsg), nil
	}

	// 构建结果字符串
	hint := fmt.Sprintf("use_prop ✓ %s → %d号 支付%d金币", catEntry.NameZh, target+1, result.PricePaid)
	if result.Hit {
		hint += " [目标中招!]"
		// v2：把注入文本 + 干扰信号入队（持锁）。EffectTypes 来自目录的 EffectSpec；
		// TwistSeat 由房间的 computeTwistSeatLocked 按 TwistSeatSrc 计算。
		// R176 P2 补缺(v4 链式效果):若 catEntry.EffectSpec.Steps 非空,把延迟 step
		// 排入 propEffectSchedule 等待 N 轮后再 ApplyEffects。即时 step 走 EffectTypes 入队。
		// 2026-08-07 §20260807-04 P0-2:AOE 道具 target=-1 时旧条件 target>=0 恒 false
		// → EffectTypes 永不落地;改为 AOE 时遍历所有存活 bot 逐个入队。
		if mgrRoom.State != nil {
			if lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
				steps := catEntry.EffectSpec.Steps
				effectTypesForEntry := catEntry.EffectSpec.EffectTypes
				if len(steps) > 0 {
					var immediate []string
					for _, st := range steps {
						if st.DelayTurns <= 0 && st.EffectType != "" {
							immediate = append(immediate, st.EffectType)
						}
					}
					if len(immediate) > 0 {
						effectTypesForEntry = strings.Join(immediate, ",")
					}
				}
				enqueueFor := func(targetSeat int) {
					if targetSeat < 0 || targetSeat >= len(mgrRoom.State.Players) {
						return
					}
					twistSeat := mgrRoom.computeTwistSeatLocked(catEntry.EffectSpec.TwistSeatSrc, int(r.seat), targetSeat)
					if len(steps) > 0 {
						for _, st := range steps {
							if st.DelayTurns > 0 {
								mgrRoom.schedulePropEffectStepLocked(targetSeat, int(r.seat), propID, st)
							}
						}
					}
					mgrRoom.enqueuePropHitLocked(targetSeat, PropInjectEntry{
						FromSeat:     int(r.seat),
						PropKey:      propID,
						InjectText:   result.InjectResult.InjectText,
						EffectTypes:  effectTypesForEntry,
						TwistSeat:    twistSeat,
						Hit:          result.Hit,
						ExpiresAfter: 1,
						Steps:        steps,
					})
				}
				switch {
				case catEntry.IsAOE:
					// AOE:对所有存活 Agent 座位逐个入队。
					for seat, p := range mgrRoom.State.Players {
						if !p.Alive || !p.IsBot {
							continue
						}
						enqueueFor(seat)
					}
				case target >= 0:
					enqueueFor(target)
				}
				// 2026-08-07 §20260807-04 P0-3:人类反制道具 — 把 debuff 直接写到
				// 目标人类座位的 Player.HumanDebuff(客户端视图渲染),不回传给使用者
				// Agent(使用者本无感;若由同房间其它 bot 消费,propHitSummary 会告知)。
				if catEntry.TargetCamp == "human" && target >= 0 && target < len(mgrRoom.State.Players) {
					spec := buildHumanDebuffSpecLocked(catEntry, int(r.seat), target)
					if spec != nil {
						mgrRoom.setHumanDebuffLocked(target, *spec)
						hint += fmt.Sprintf(" [已对人类 %d号 施加「%s」干扰]", target+1, spec.PropNameZh)
					}
				}
				mgrRoom.mu.Unlock()
			}
		}
	} else {
		hint += " [未中招]"
	}

	// R190 Bug 1: AOE 道具无论 agent 传入什么 target,一律归一化为 -1,
	// 保证 broadcast 文案("对所有玩家")与 prop_history.to_seat 一致。
	broadcastTarget := target
	if catEntry.IsAOE {
		broadcastTarget = -1
	}
	// 广播道具使用事件（公开）
	// 2026-07-23 §道具特效:附 propKey,驱动前端 game.werewolf_prop_used 特效帧。
	r.mgr.broadcastPropUseLocked(mgrRoom, int(r.seat), broadcastTarget, propID, catEntry.NameZh, result.Hit)

	// v3 §G5 — 写入公开道具使用历史(环形 buffer,供 prop_history 工具查询)。
	if lockRoomBriefly(mgrRoom, 200*time.Millisecond) {
		mgrRoom.recordPropHistoryLocked(PropHistoryRecord{
			FromSeat:   int(r.seat),
			ToSeat:     broadcastTarget,
			PropKey:    propID,
			PropNameZh: catEntry.NameZh,
			Hit:        result.Hit,
			EffectHint: result.InjectResult.EffectHint,
			Phase:      phaseAtUse,
			Round:      roundAtUse,
			CreatedAt:  time.Now().Unix(),
		})
		mgrRoom.mu.Unlock()
	}

	// 唤醒所有 Agent（让他们看到道具使用事件）
	r.wakeAll()
	return hint, nil
}

// IdiotReveal 是 2026-07-10 §3.5 / §12 新增的「白痴翻牌」工具实现。
//
// choice = "reveal" → 翻牌免死(失去投票权); choice = "skip" → 放弃翻牌(正常放逐)。
// TODO-12P(engine-missing):Action_IdiotReveal 由 backend-dev 实现后替换下方
// fallback。
func (r *agentRunner) IdiotReveal(choice string) (string, error) {
	if choice != "reveal" && choice != "skip" {
		return "idiot_reveal: choice 仅支持 reveal/skip", nil
	}
	// 2026-07-10 §3.5:调引擎 lock-held 派发路径。
	_, e := r.mgr.Action_IdiotReveal(r.roomID, r.botUserID, choice)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

func (r *agentRunner) WolfSuicide() (string, error) {
	_, e := r.mgr.Action_WolfSuicide(r.roomID, r.botUserID)
	if e != nil {
		return r.errStr(e), e
	}
	r.wakeAll()
	return "ok", nil
}

// IdleThink 2026-07-08 §13.2 / Round 39 §94: LLM 主动选择沉默思考的
// 工具回调。语义:不广播任何消息,仅由 run.go handleEvent 路径在
// BotTranscript 追加审计行(LLM 主动调 idle_think 时,通过 tools.go
// DispatchTool 的 `case "idle_think"` 走到这里,这里只是占位)。
//
// 实际"留 audit"的写入由 run.go 在 spectator_speech 路径上做(更通用,
// 不依赖 LLM 是否主动调工具),本方法本身只需不报错即可。wakeAll()
// 推进 phase 让其他 bot 有机会继续决策。
func (r *agentRunner) IdleThink(reason string) (string, error) {
	r.wakeAll()
	return "idle_think recorded", nil
}

// RestartVote 2026-07-10 重开局投票的 agent 端实现。
//
// 走 manager.RestartVoteBotLocked(lock-held 派发路径,manager 已在内部
// 完成 quorum 评估 + 通过时 restartGameLocked)。本方法只负责转发参数与
// 失败原因透传给 LLM。
//
// 注: 不需要 wakeAll — quorum 评估内部已决定是否推进 phase / 关闭房间,
// 期间会主动推 wake 给所有 bot(参见 manager.tryEnterRestartVoteFromGameOverLocked)。
func (r *agentRunner) RestartVote(choice string) (string, error) {
	if r.mgr == nil {
		return "restart_vote rejected: no manager", nil
	}
	// 这里 runner 不持锁 — Action_RestartVote 内部 lockRoomBriefly,
	// 用 lockRoomBriefly 防止与 watchdog 5s tick / in-process dispatch 互锁。
	_, e := r.mgr.Action_RestartVote(r.roomID, r.botUserID, choice)
	if e != nil {
		return r.errStr(e), nil // 工具调用:返回字符串 + nil 让 LLM 重试或收敛
	}
	return "ok", nil
}

// ProposeVote 预言家在白天发言阶段发起投票,直接结束讨论进入投票阶段。
// 2026-07-11: 走 Action_ProposeVote 公开路径(自身持 r.mu)。
func (r *agentRunner) ProposeVote() (string, error) {
	if r.mgr == nil {
		return "propose_vote rejected: no manager", nil
	}
	_, e := r.mgr.Action_ProposeVote(r.roomID, r.botUserID)
	if e != nil {
		return r.errStr(e), nil
	}
	return "ok — 已发起投票,即将进入投票阶段", nil
}

// BUG 2026-07-09: LastWords 提交遗言。遗言 actor 调用 manager 的 Action_LastWords,
// 成功后由 manager 广播遗言内容 + 活动事件 + wake 下一位遗言座位。
// 走 Action_* 公开路径(自身持 r.mu,§92a 兼容)。
func (r *agentRunner) LastWords(text string) (string, error) {
	_, e := r.mgr.Action_LastWords(r.roomID, r.botUserID, text)
	if e != nil {
		return r.errStr(e), e
	}
	// 2026-08-05 §Agent聊天显示优化 (B4):遗言已由 manager 广播成功(活动流
	// death_lyric_spoken),仅成功路径落 LastSpeech*,kind="last_words"。
	r.recordLastSpeech(text, "last_words")
	return "last_words submitted", nil
}

// BUG 2026-07-09: LastWordsSkip 放弃遗言。
func (r *agentRunner) LastWordsSkip() (string, error) {
	_, e := r.mgr.Action_SkipLastWords(r.roomID, r.botUserID)
	if e != nil {
		return r.errStr(e), e
	}
	return "last_words skipped", nil
}

// IdleSilent 2026-07-08 §15 / Round 40 §95: "沉默思考"工具。
// §128 对话即思考重构:与原 idle_think 合并,role 区分调用方。
//   - 工具描述强调"本轮已发过言才能调",引导 LLM 行为
//   - 不发消息、不广播(zero token cost)
//   - 由 run.go 在 handleEvent 路径上自动追加 [idle_silent] 审计行
//   - 不 wakeAll()(本轮发言已达成,无需继续推进 phase)
//
// role 区分 player / judge,审计行内容根据 role 略有不同(玩家侧 / 法官侧)。
// 本方法只返回"ok"占位,真正的留 audit 写入由 run.go handleEvent 在工具调用后做。
func (r *agentRunner) IdleSilent(role, reason string) (string, error) {
	if role == "" {
		role = "player"
	}
	return "idle_silent recorded (" + role + ")", nil
}

// EmotionSwitchSpeak 2026-08-04 §重构 — 合并发言 + 切情绪。
//
// 顺序：先走完整 speak 限流/去重/身份脱敏链(r.Speak 内部已覆盖) → 广播成功后才切情绪。
// speak 失败(被服务端拒绝/cooldown/去重为空)时 emotion 与 fx 都不动,reason 忽略。
//
// 2026-08-04 §表情特效(§5.2/§5.3):fx wwplayer.EmotionFx 已由 dispatch 层
// NormalizeEmotionFx 归一化(clamp/截断/非法 effect 归一 pulse),这里直接透传给
// SwitchEmotionFx。回滚语义不变 — speak 被拒整组字段(emotion + fx)都不生效。
//
// 复用 r.Speak(cleaned) 是为了避免重写 150+ 行 speak 过滤链。
// 注意:不在 lock 下调用 r.Speak(它是锁外的 chat 路径),与 §92a 锁内变体约束一致。
func (r *agentRunner) EmotionSwitchSpeak(text, emotion, reason string, fx wwplayer.EmotionFx) (string, error) {
	if r.agent == nil {
		return "", nil
	}
	// 2026-08-05 §Agent聊天显示优化 (B4):先清空缓存,让「本次 r.Speak 是否真的
	// 广播成功」有一个可靠哨兵 —— 只有成功路径才会由 recordLastSpeech 重新填上。
	// 否则限流 / 去重 / 硬拒时会误用上一轮的陈旧文本覆写 kind。
	r.lastSpeechText = ""
	// 1. 走 r.Speak 完整过滤链(rate-limit / dedup / identity-leak / death-fact / XML strip / chatSvc.SendFromBot / chatQueue.Append)
	result, err := r.Speak(text)
	if err != nil {
		return result + " (no emotion change)", err
	}
	// speak rejected 状态(rate-limited / identity leak / death claim / dedup-empty 等) → 不切 emotion,fx 同样不生效
	if strings.HasPrefix(result, "speak rejected") || strings.HasPrefix(result, "speak_rate_limited") {
		return result + " (no emotion change)", nil
	}

	// 2. BUG-R238-P0-1 (2026-08-04) 纵深防御 — reason / caption 是 LLM 自由文本,
	// 可能包含身份自述(如「继续隐藏预言家身份」),在写入 emotion 状态(进而进
	// BotTranscript → WS 下发)前过身份过滤链,与 SpeakWithThought 对 internalThought
	// 的处理 (BUG-R200-SEC-01) 完全一致。Fix A/B 是主防线(服务端脱敏+不填),本段是
	// 让「协议层隔离红线」注释成真的最后兜底;同时清洗服务端日志/审计副本。
	if r.filterCfg.EnableIdentityFilter {
		if reason != "" {
			if res := MysteryMaskText(reason); res.Hit {
				logger.L().Info("agentRunner.EmotionSwitchSpeak reason mystery-mask hit (BUG-R238-P0-1)",
					zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
					zap.String("mode", res.Mode.String()),
					zap.Strings("categories", res.HitCategories))
				reason = res.Text
			}
		}
		if fx.Caption != "" {
			if res := MysteryMaskText(fx.Caption); res.Hit {
				logger.L().Info("agentRunner.EmotionSwitchSpeak caption mystery-mask hit (BUG-R238-P0-1)",
					zap.Int("seat", int(r.seat)), zap.String("room", r.roomID),
					zap.String("mode", res.Mode.String()),
					zap.Strings("categories", res.HitCategories))
				fx.Caption = res.Text
			}
		}
	}
	if reason != "" {
		if cleaned, hit := ScrubIdentityLeak(reason); hit {
			logger.L().Info("agentRunner.EmotionSwitchSpeak reason scrub hit (BUG-R238-P0-1)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
			reason = cleaned
		}
	}
	if fx.Caption != "" {
		if cleaned, hit := ScrubIdentityLeak(fx.Caption); hit {
			logger.L().Info("agentRunner.EmotionSwitchSpeak caption scrub hit (BUG-R238-P0-1)",
				zap.Int("seat", int(r.seat)), zap.String("room", r.roomID))
			fx.Caption = cleaned
		}
	}

	// 3. speak 真正广播成功才切 emotion + fx;reason 仅在 emotion 提供时记入 history
	emotionChanged := false
	if emotion != "" && wwplayer.IsValidEmotion(emotion) {
		r.agent.SwitchEmotionFx(emotion, reason, fx)
		emotionChanged = true
	}

	// 2026-08-05 §Agent聊天显示优化 (B4):内层 r.Speak 已按 kind="speak" 记过一次,
	// 这里在情绪切换之后用 kind="emotion_speak" 覆写同一条记录(文本一致,仅 kind
	// 更精确),让前端座位卡能把「带表情的发言」与普通发言区分渲染。
	// 放在 SwitchEmotionFx 之后:RecordLastSpeech 自身会触发 game.state 推送,
	// 让这一帧同时带上最终 kind 与切换后的 emotion,人类只看到一次刷新。
	// **必须**复用 r.lastSpeechText(Speak 过滤链的产物),不能回写入参 text ——
	// 后者是未过滤原文,写到全房可见字段等于绕过整条过滤链。
	if r.lastSpeechText != "" {
		r.recordLastSpeech(r.lastSpeechText, "emotion_speak")
	}

	// 4. 拼接反馈后缀(LLM 下一轮能看见 emotion + 特效变化)
	suffix := ""
	if emotionChanged {
		meta := wwplayer.EmotionMeta(emotion)
		suffix += fmt.Sprintf(" [emotion→%s(%s) %s]", meta.Name, emotion, meta.Emoji)
		fxDesc := fx.Effect
		if fx.Caption != "" {
			fxDesc += " caption=\"" + fx.Caption + "\""
		}
		suffix += fmt.Sprintf(" [fx→%s]", fxDesc)
	}
	return result + suffix, nil
}

// PublicCommit 实现 wwplayer.PublicCommitRunner 接口（§20260810-06）。
// Agent 调 public_commit 工具时派发到此。
func (r *agentRunner) PublicCommit(template string, targetSeat int, reason string) (string, error) {
	if r.mgr == nil {
		return "public_commit rejected: no manager", nil
	}
	// 转换模板字符串到 CommitTemplate
	ct := CommitTemplate(template)
	_, e := r.mgr.Action_PublicCommit(r.roomID, r.botUserID, ct, targetSeat, "", reason)
	if e != nil {
		return r.errStr(e), nil
	}
	return "ok — 承诺已公开", nil
}

// agentEventSink is the per-seat channel the manager uses to wake an agent.
// The manager creates one per agent seat at StartGame time and pushes events
// onto it whenever the phase changes or it's the agent's turn.
type agentEventSink struct {
	seat int
	ch   chan wwplayer.AgentEvent
	room *WerewolfRoom
}

// push sends an event to the agent, dropping it if the channel is full (the
// agent is still processing a previous turn). Non-blocking so the manager
// never stalls on a slow agent.
func (s *agentEventSink) push(evt wwplayer.AgentEvent) {
	if s.ch == nil {
		return
	}
	select {
	case s.ch <- evt:
	default:
		logger.L().Debug("agent event dropped: channel full",
			zap.String("room_id", s.room.RoomID), zap.Int("seat", s.seat))
	}
}

// pickRandomEmotion 是 §124 情绪模块在 werewolf 侧的封装 — 调用 agent 包
// 的 pickRandomEmotion(已封装在 emotion.go),确保 Engine 侧不直接 import
// math/rand 而破坏单元测试的可重现性。
func pickRandomEmotion() string {
	return wwplayer.PickRandomEmotion()
}

// truncate 是 agent.go 在 agent 包内的 helper — 在 werewolf 侧不可见,
// 这里重新实现一份以支持 emotion_switch 工具的 result 字符串截断。
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ReasoningChain §20260811-06 U3 — reasoning_chain 工具派发实现。
//
// 行为:
//   - 校验 topic / steps / conclusion 非空;
//   - 限制 steps / evidence 最多 3 条,每条 ≤30 字;
//   - 限制 topic ≤20 字,conclusion ≤40 字;
//   - 写 BotTranscript.ReasoningChains(由 recordTranscript 持久化);
//   - **不**计入 consecutiveFailures,**不**触发 quarantine(§120 公平性);
//   - **不**触发任何 phase 推进;只记录推理。
//
// §130 接线验证:此方法是 BotTranscript.ReasoningChains 字段的唯一写入路径;
// 关闭 opt-in(appendReasoningChainEnabled=false)时返回"已记录"但不写。
func (r *agentRunner) ReasoningChain(topic string, steps, evidence []string, conclusion string, confidence int) (string, error) {
	if r.agent == nil {
		return "reasoning_chain recorded (no agent)", nil
	}
	// 长度归一
	topic = truncate(topic, 20)
	conclusion = truncate(conclusion, 40)
	stepNorm := normalizeChainItems(steps, 30, 3)
	evidenceNorm := normalizeChainItems(evidence, 30, 3)
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 100 {
		confidence = 100
	}
	// round / phase:从 currentGC 拿(GameContext 在 run.go::Run 入口设入,
	// 这里作为最后镜像读)。
	round := 0
	phase := ""
	if r.currentGC != nil {
		round = r.currentGC.Round
	}
	r.agent.AppendReasoningChain(wwplayer.ReasoningChainEntry{
		Round:      round,
		Phase:      phase,
		Topic:      topic,
		Steps:      stepNorm,
		Evidence:   evidenceNorm,
		Conclusion: conclusion,
		Confidence: confidence,
	})
	return "reasoning_chain recorded", nil
}

// normalizeChainItems 把字符串数组归一为长度/数量限制范围内。
// 过滤空字符串,单条 rune 截断,最多 maxItems 条。
// §20260811-06 U3 — reasoning_chain 输入清洗。
func normalizeChainItems(items []string, maxRunes, maxItems int) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, maxItems)
	for _, s := range items {
		s = truncate(s, maxRunes)
		if s == "" {
			continue
		}
		out = append(out, s)
		if len(out) >= maxItems {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
