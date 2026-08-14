// Package agent — agent.go: the Agent driver. One Agent occupies one werewolf
// seat, observes game state, calls the LLM, and executes tools via ToolRunner.
//
// Lifecycle:
//   - New(...) builds an Agent
//   - Run(ctx) starts the decision loop; the agent sleeps until the room pushes
//     an AgentEvent onto its channel, then decides.
//
// To keep this file decoupled from the engine (and avoid an import cycle),
// event observation is injected via the `events` channel set in Run.
package wwplayer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
	"LsmAgentGame/llm/anthropic"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// interjectMaxPer5Min R76 P1-3 (2026-07-10): 单 bot 5 分钟滑动窗口最多
// 4 次插话,防止单 bot(MiniMax #6)每轮互插刷屏导致其他 5 个 AI 几乎
// 丧失发言机会(R76 报告 MiniMax 一局累计 7+ 条插话)。
const interjectMaxPer5Min = 4

// Agent drives one bot seat.
type Agent struct {
	Seat     int
	RoomID   string
	UserID   string
	ModelKey string
	Provider llm.LLMProvider
	Registry *llm.Registry

	// botID is a stable per-Agent identifier produced at construction
	// (room:seat:modelkey). All logger calls in run.go use this so log
	// analytics can correlate events back to one bot without confusing
	// "current ModelKey string field" with "registry-driven transient
	// alias". BUG-WEREWOLF-P0-NEW-30 (Round 33).
	botID string

	// apiKey is the bearer key resolved from the registry for this agent's
	// ModelKey. BUG-WEREWOLF-AGENT-FULL P0-4: previously NewWithRoom
	// discarded the key (registry.Get -> _) and Run passed "" to
	// Provider.Chat, so every LLM call went out with "Authorization: Bearer "
	// (empty) and the gateway rejected it with HTTP 401 "invalid
	// Authorization format" — agents logged "provider chat failed" and never
	// acted. The key is real (registry.Get rejects placeholders); it just
	// never reached the wire.
	apiKey string

	Memory  *Memory
	Limiter *agentcore.SpeakLimiter // 发言限流：≤2 次/分钟 (45s 间隔，2026-07-08 从 30s 调高)

	// Role / Faction / Win 是构造期一次性写入的身份字段(2026-07-21 §5.2)。
	// 之前 NewWithRoom 只把它们喂给 NewMemory,导致 SetWolfTeammateSeat
	// 后期需要替换 identity 时找不到这些字段;这里以"只读快照"形式保留,
	// 整个房间生命周期不变,避免每次 LLM 调用都反查 GameState。
	Role    string
	Faction string
	Win     string

	// SelfPortraitText 是 §20260810-10 U2 的「模型自画像」文本(可选)。
	// 由房间装配层(StartAgentsLocked)基于 t_lsm_game_model_game_log 聚合
	// 生成,一局内只赋值一次;run.go 两处 BuildSystemPrompt 调用点透传。
	// 空串 = 降级(开关关闭 / DB 无数据 / 查询失败),system 输出与旧版一致。
	SelfPortraitText string

	// DifficultyDirective 是 §20260811-09 U2 的 Agent 难度档位 directive 文本。
	// 由房间装配层(StartAgentsLocked)经 ProfileFor(AgentDifficulty)计算,
	// easy/hard/hell 三档非空(normal 档为空串保持 prompt cache 命中)。
	// run.go 两处 BuildSystemPrompt 调用点透传,追加在 personality 段之后,
	// 整段 prompt 末尾。空串 = 不注入,正常档(natural)行为零回归。
	DifficultyDirective string

	// Personality 是 §20260811-04 U2 的「人设倾向参数」(5 维向量)。
	// 由房间装配层(StartAgentsLocked)从 t_lsm_game_agent_personality 读取。
	// 零向量 = 关闭 / 降级,system 输出与 §20260810-10 U2 字节一致
	// (Anthropic prompt cache 前缀命中)。
	Personality PersonalityVector
	// PersonalityPresetKey 是预设 key（"logical"/"emotional"/.../"custom"）。
	// 仅用于 PersonalityBlock 文案渲染,空串 = 自定义向量。
	PersonalityPresetKey string

	// 2026-08-10 §20260810-12 D1 — 决策留痕运行时回放开关。
	// true(默认) → recordTranscript 末尾调 AppendDecisionEntry 追加
	// 一条 DecisionEntry 到 BotTranscript.DecisionTrail;false → 零开销
	// (AppendDecisionEntry 提前 return,wire 上 DecisionTrail 字段 omitempty 不出现)。
	// 由 NewWithRoom 构造时读取 cfgWerewolf.BotsLogDecisions 一次性赋值。
	botsLogDecisions bool

	// 2026-08-12 §20260812-04 U4 — 长期记忆注入的 rune 预算(0 = 用
	// MemoryInjectMaxRunes 默认常量)。由 StartAgentsLocked 按房间难度档位
	// (difficulty.MemoryInjectRunes)注入 —— 该配置此前 4 处赋值 0 处读取,
	// 属 §130「声明了却从不接线」,本次一并修复。
	memoryInjectRunes int

	// v4 §13.1：开局狼队友座位（>=0 表示本 bot 是狼人 + 系统随机选了 1 个队友）。
	// -1 = 未启用（如非狼人 / 概率未中 / 队友已死）。
	// wolf_whisper 工具仅在 wolfTeammateSeat >= 0 时挂载；wolfpack prompt
	// 仅在 wolfTeammateSeat >= 0 时渲染，避免身份未确认的狼误用。
	WolfTeammateSeat int

	WhisperLimiter *agentcore.SpeakLimiter // 私聊限流：≤1 次/90秒 (90s 间隔，2026-07-08 从 60s 调高)，
	// 比发言更严，防止 Agent 用 whisper 私聊替代推理或绕过发言限流。

	// §130 重构(2026-07-13):LLMCallLimiter 字段已删除。
	// 每个 bot 现在按模型自身响应速率自由调用 LLM,不再有最小间隔锁定。
	// 上游代理瞬断防护由 anthropic.Provider 内层重试 + 5min timeout 兜底。

	// InterjectLimiter 节流 R76 P1-3 (2026-07-10) — 单 bot 插话刷屏:
	// 共享 speakLimiter(45s) 仅防止"speak 和 interject 都调",但同一 bot
	// 可以每 45s 插一次话,跨整个对局(>20 分钟)会积累 25+ 条,挤掉其他玩家
	// 发言节奏。InterjectLimiter 设 60s 间隔 + 每 5 分钟 cap 4 条,
	// 强制 bot 至少 1 分钟插一次,且 5 分钟窗口最多 4 条。
	InterjectLimiter *agentcore.SpeakLimiter
	// interjectWindowStart + interjectWindowCount 实现 5-min/4 次 软上限:
	interjectWindowStart time.Time
	interjectWindowCount int
	interjectMu          sync.Mutex

	// §130 重构(2026-07-13):MaxToolUse 字段保留但不再使用。
	// 每个 bot 现在按 LLM 输出自由循环(end_turn/refusal/max_tokens 退出),
	// 无硬轮次上限。watchdog / phase deadline / consecutiveFailures 提供死锁兜底。
	//
	// Deprecated: 2026-08-13 §20260813-04 U3 起,难度档位的轮次收紧改由
	// difficultyRoundCap 承载(经 maxInnerRoundsFor 调制 phaseMaxInnerRounds)。
	// 本字段保留仅为 wire 兼容,新代码不要读写它。
	MaxToolUse int

	// difficultyRoundCap 是难度档位对内层循环轮次的**收紧**上限(0 = 不收紧)。
	//
	// 2026-08-13 §20260813-04 U3 —— 接线 difficulty.go 的 4 处档位配置。
	// 此前 difficulty.go 给 easy/normal/hard/hell 设 3/6/8/0,但 agent 侧
	// 把 MaxToolUse 硬设 0 且注释写「不再使用」,**难度档位对工具上限完全无效**
	// (§130 第七次复发,与 §20260812-04 U4 的 MemoryInjectRunes 同一模式)。
	//
	// 语义:只在小于 phase 基线时生效(见 maxInnerRoundsFor),不放宽。
	difficultyRoundCap int

	// consecutiveFailures tracks the number of LLM call failures in a row.
	// BUG-WEREWOLF-P0-2 FIX: when this counter exceeds failAutoSkipThreshold
	// the agent auto-calls the phase's default "skip" action so the game can
	// continue even when one bot's LLM is permanently broken. Without this
	// safeguard, a single misconfigured model (DeepSeek/GLM thinking mismatch)
	// would lock the entire phase in a 6-8s retry loop until the test ends.
	consecutiveFailures int

	// BUG-R48-P0-1: lastFailureTime tracks when the last LLM failure occurred.
	// Used to implement a cooldown window: if the last failure was within
	// failCooldownWindow (30s), the consecutiveFailures counter is NOT
	// incremented. This prevents transient upstream disconnects (1.5-4s
	// proxy drops) from rapidly accumulating to the quarantine threshold.
	lastFailureTime time.Time

	// onTranscriptPublished is an optional callback fired (in a goroutine) after
	// recordTranscript publishes a fresh BotTranscript snapshot. The room
	// manager registers this so it can broadcast game.state (with the new
	// bot_contexts[] incl. emotion + fx fields) to all players / spectators —
	// giving the frontend a real-time sync for emotion_switch_speak without
	// waiting for the next phase-driven broadcast.
	//
	// Runs in a goroutine to avoid lock ordering hazards (recordTranscript
	// fires under a.mu; the callback needs room.mu for the broadcast).
	onTranscriptPublished func()

	// BUG-R232-P1-02 (2026-08-02): circuitOpenFailureCount tracks the number
	// of consecutive failures that hit the model_400_circuit open path.
	// Used to (a) throttle reWakeDelay from 8s to circuitOpenMinReWakeDelay
	// (30s) so the retry storm slows down, and (b) down-sample the failure
	// log to "every N-th" instead of every failure (R232 observed 106
	// failures/min flooding the log while P0 was being investigated).
	// Reset to 0 by ResetConsecutiveFailures (model recovered).
	circuitOpenFailureCount int

	// quarantined is set true once consecutiveFailures crosses
	// maxConsecutiveFailures (or 2 consecutive permanent errors). A
	// quarantined agent yields every wake without calling the LLM or
	// scheduling reWake — the room manager's per-seat skip logic is then
	// the only thing that advances its turn in each phase.
	//
	// BUG-WEREWOLF-P0-NEW-3: previously no such flag existed, so a single
	// 403-quota-exhausted model looped forever (40+ consecutive failures
	// observed in Round 15) flooding auto-skip / [30008] errors.
	quarantined bool

	// 2026-07-29 优化:emotion_switch 单独调用计数器 — 已删除(§重构)。
	// 2026-08-04 emotion_switch 工具合并到 emotion_switch_speak,本计数器
	// 失去意义,字段一并移除。

	// 2026-07-29 修复:speak 阶段当前发言者仅调 idle_silent 计数器。
	// 当 LLM 在 speakTurn 仅调 idle_silent 时递增,正常发言后重置。
	// 连续 3 次后允许通过(避免死锁)。
	speakTurnIdleSilentCount int

	// lastError captures the last provider error string so the UI can
	// surface "已禁用: <reason>" instead of an opaque "已禁用" tag.
	// Updated on every LLM failure inside handleEvent; cleared on any
	// successful LLM response via ResetConsecutiveFailures. Surfaced
	// through BotTranscript.QuarantineReason in recordTranscript.
	//
	// BUG-WEREWOLF-P1-NEW-46 (Round 39).
	lastError string

	// reWakeCancel cancels any pending scheduleReWake timer so that when the
	// agent hits quarantine (or the room tears down) we don't fire a stale
	// wake event into the channel that will pull the agent back into an
	// LLM call it's no longer allowed to make. BUG-WEREWOLF-P0-NEW-4 (Round
	// 24): previously a quarantined agent could still receive a delayed
	// reWake event because scheduleReWake ran as an un-cancellable
	// time.After goroutine. The fix: every scheduleReWake registers its
	// cancel func here; setting quarantined cancels the in-flight reWake.
	reWakeCancel context.CancelFunc

	// onQuarantine is an optional callback fired (in a goroutine) when the
	// agent transitions to quarantined state via SetQuarantined(). The room
	// manager registers this at agent construction time so that when an agent
	// quarantines mid-turn (inside handleEvent → SetQuarantined), the manager
	// can immediately check whether this bot is the current acting seat and
	// dispatch the phase's safe skip action — without waiting for the next
	// external wake event that may never come.
	//
	// BUG-WEREWOLF-P0-NEW-27 (Round 34): quarantined acting bot in speak
	// phase → no subsequent wake → tryDispatchQuarantinedActingSkip never
	// fires → room dead-locked at "phase=speak round=N" for 7+ minutes.
	// The callback breaks the causal gap between "agent quarantines" and
	// "manager notices quarantine" by pushing the notification proactively.
	//
	// Runs in a goroutine to avoid lock ordering hazards (agent.mu is held
	// when SetQuarantined fires; the callback needs room.mu).
	onQuarantine func()

	// lastTranscript is the agent's most recent decision snapshot, refreshed
	// after every successful LLM round by recordTranscript. The room's
	// broadcast path reads it via BotTranscript() to populate
	// game.state.bot_contexts[] for the spectator HistoryDrawer(🤖独白 sub-tab).
	// BUG-WEREWOLF-P0-NEW-2: previously the server never serialized any bot
	// thinking / tool activity, so the spectator "Agent 思考" panel was
	// permanently empty (showed `(0)`). Guarded by a.mu (same lock as the
	// events-channel swap) so the broadcast goroutine can read it safely
	// while the agent loop writes.
	lastTranscript *BotTranscript

	// events carries snapshots + wake signals from the room. Set via SetEvents.
	events chan AgentEvent
	// now injects time for tests; defaults to time.Now.
	now func() time.Time

	// llmSema is the per-room concurrency gate for LLM HTTP calls. Optional
	// (nil = uncapped). When set, handleEvent acquires a token before
	// Provider.Chat and releases on return. Used to throttle N-agent rooms
	// against slow upstream proxies (BUG-WEREWOLF-P0-NEW-31).
	//
	// AcquireLLMSlot tries to grab a slot within `wait`; returns true if it
	// got one, false if it had to back off (caller should treat as a
	// transient failure and requeue via scheduleReWake). The slot is held
	// for the duration of the LLM call; the same defer releases it.
	llmSema chan struct{}

	// §20260811-01 新增: SteeringQueue 是游戏事件实时注入通道。
	// 灵感来源: PI Agent 的 PendingMessageQueue (steering/follow-up) 机制。
	// room manager 在 agent 运行中非阻塞写入观众消息/道具命中/阶段提示,
	// handleEvent 内层循环每轮开始前 drain 一次,注入到 user prompt 末尾。
	// 不设置时为 nil (drain 逻辑跳过)。
	steeringQueue *SteeringQueue

	// §20260811-01 新增: ToolHooks 是工具执行前/后 hooks 管道。
	// 灵感来源: PI Agent 的 beforeToolCall/afterToolCall hooks 机制。
	// 不设置时为 nil (DispatchTool 走原路径)。
	toolHooks *ToolHooks

	// 2026-08-13 §20260813-02 U2 — per-Agent 工具定义缓存。
	// seat 在 Agent 生命周期内固定,缓存实例不跨 bot 共享;run.go 内层循环
	// 与 speak_floor 路径经 BuildToolsCached 命中,避免每轮全量重建 ~30 个
	// 工具定义(prompt cache 前缀字节稳定的前提)。
	toolsCache *ToolsCache

	// §20260811-01 新增: compactConfig 控制 LLM 记忆压缩行为。
	// 灵感来源: PI Agent 的 compaction/compaction.ts。
	// 每局第 N 轮 LLM 调用前检查消息数,超过阈值时用 LLM 压缩旧消息。
	// 2026-08-13 §20260813-02 U1: 新增 SetCompactConfig setter 接线
	// (此前无任何 setter,Enabled 恒 false,run.go 触发判断永不生效 —— §130)。
	compactConfig CompactConfig

	// compactDone 标记本轮是否已执行过 LLM 压缩 (每局最多一次)。
	compactDone bool

	// 2026-08-13 §20260813-02 U1 — 压缩结果可观测标记(禁止假成功)。
	// 由 run_compact.go 的 maybeCompactMemory 写入,经 BotTranscript() 透出:
	//   - compactAt:        最近一次压缩尝试的 unix 毫秒(0 = 未尝试)
	//   - compactFallback:  true = LLM 压缩失败,已显式回退规则式压缩
	//   - compactNote:      一行说明(成功: N→M 条 [增量];失败: 回退原因)
	compactAt       int64
	compactFallback bool
	compactNote     string

	// 2026-08-13 §20260813-04 U4 — pre-flight 上下文裁剪的可观测标记。
	//
	// **降级必留可观测标记**(§20260812-04 教训 4):静默裁剪会让
	// 「上下文被裁短」与「模型本来就没什么可说」在日志/UI 里同形,
	// 正是 §20260811-08 教训 (5) 批评的模式。
	// 由 run.go 的 pre-flight 分支写入,经 BotTranscript 透出前端。
	preflightNoteText string
	preflightAt       int64

	// mu guards events-channel swap (Lock/Unlock are exported so the manager
	// can stop an in-flight Run cleanly).
	mu sync.Mutex

	// 2026-07-09 §13 增强 — speakCounter (60s 滑动窗口) + chatQueue (500K 滚动 buffer)
	//
	// speakCounter:用于实现"白天发言阶段每分钟 ≥ 2 次"硬下限。
	// 由 recordSpeakDaytime / allowSpeakDaytime / snapshotSpeakCounter 操作。
	// 内部 mutex 独立于 a.mu,避免与 events channel 切换争抢。
	//
	// chatQueue:每个 Agent 独立 500K 字节滚动聊天历史队列。
	// 由 appendRoomMessage (在 werewolf/room.go) 注入;通过 ChatQueue() 暴露给 room;
	// 通过 ChatHistoryBytes/Cap/LastCompressionAt 暴露给 BotTranscript。
	speakCounter speakCounterState
	chatQueue    *agentcore.ChatHistoryQueue
	chatCap      int

	// startedAt 记录 Agent 的创建时间(用于 populateBotContexts 占位场景显示
	// "已就绪 X 秒,等待发言"),由 NewWithRoom 在构造时打点。BUG FIX 2026-07-09 §13.6。
	startedAt time.Time

	// 2026-07-09 §13-bugfix: LLM 调用中实时状态。由 MarkLLMCallStart/End 维护,
	// 受 a.mu 保护。BotTranscript() 读取这些字段以反映当前是否正在调用 LLM,
	// 让前端能显示"正在调用大模型… 已等待 Ns"实时计时器。
	llmCallInProgress bool
	llmCallStartedAt  time.Time

	// 2026-07-10 §120 增强:API 调用耗时统计 — 公平性机制核心数据。
	// avgLLMLatencyMs 是本 bot 本局所有 LLM 调用的指数加权滑动平均(α=0.3),
	// 让 BuildUserPrompt 在 prompt 末尾渲染【模型响应速率】块,LLM 据此
	// 调整策略:反应慢的 bot 减少单轮 tool_use(避免 8s LLMCallLimiter
	// 触发活锁),反应快的 bot 多承担对话(允许更激进的 speak / interject
	// / 多 tool 合并)。lastLLMLatencyMs 保留最后一次原始耗时(供前端展示
	// "上次 X.X s / 平均 Y.Y s")。totalLLMCalls 用于上下文窗口压缩决策。
	avgLLMLatencyMs  int64
	lastLLMLatencyMs int64
	totalLLMCalls    int

	// 2026-07-30 §统计增强 — Token + API 统计（纯内存态，不进 DB，房间解散自动释放）。
	// 对齐 Anthropic Response.Body.usage = {input_tokens, output_tokens}。
	// input_tokens=0 表示命中缓存，按 0 累计，前端展示 ⚡ 缓存命中图标。
	totalInputTokens  int // 本局累计 input tokens（含缓存命中 0）
	totalOutputTokens int // 本局累计 output tokens
	totalAPITokens    int // 本局累计 input+output
	lastInputTokens   int // 最近一次 input tokens
	lastOutputTokens  int // 最近一次 output tokens
	lastAPITokens     int // 最近一次 input+output
	apiCallCount      int // 本局累计 API 调用次数（含失败）
	apiSuccessCount   int // 本局累计成功次数
	apiFailCount      int // 本局累计失败次数

	// 2026-07-09 §重构 - 决策可观测性:由 run.go::handleEvent 在 STAGE 3/4 之间
	// 写入,recordTranscript 读取后清空。受 a.mu 保护。
	lastDecision RecordDecisionState

	// 2026-07-10 §重构 — LLM 调用相位状态机 live 字段。
	// 与 BotTranscript.LLMCallPhase 等字段一一对应;BotTranscript() 读方法
	// 在锁内拷给 bt,以反映"此刻正在发生"的状态(speak 前是 calling,
	// retry loop 内是 retrying, 流式首 token 后是 streaming, 失败达上限
	// 是 quarantined)。所有字段由 run.go 在 a.mu 保护下写入。
	//
	// 设计要点:
	//   - llmCallPhase:状态机 5 态(idle/calling/streaming/retrying/quarantined)
	//   - retryAttempt/retryMaxAttempts:retry loop 进度
	//   - nextRetryAtMs:下次重试 unix ms,前端可计算倒计时
	//   - lastErrorClass:失败分类,前端据此选不同重试徽章(5xx/429/timeout/permanent)
	llmCallPhase     string
	retryAttempt     int
	retryMaxAttempts int
	nextRetryAtMs    int64
	lastErrorClass   string

	// 2026-07-12 §127 — quarantine 时主动广播的系统消息,供 BuildClientState 透传。
	// 由 SetQuarantined 写入,BotTranscript 携带;前端 WerewolfStatusBar 展示。
	quarantineBroadcast string

	// 2026-07-10 §4 - 模型对局日志 hook
	// RecordLog 是 agentcore.RecordLogService 引用(可选,nil = no-op)。
	// GameLogID 是 GameStarted 调用时由 RecordLog 返回的 game_log.id,
	// 后续每条 chat/action 都引用此 id。测试桩 / 老代码路径可保持 nil/""。
	RecordLog *agentcore.RecordLogService
	GameLogID string

	// 2026-07-12 §127 增强 — 外层 LLM 重试上限，由 cfg.Werewolf.LLMMaxRetries 注入，默认 7。
	maxLLMRetries int

	// 2026-07-12 §127 增强 — 聊天 SSE 流式解析：每次 LLM 调用首 token 到达 / 文本增量 / 调用结束。
	// 由 room.go 接线，默认 nil（调用前必须 if != nil 守卫）。
	onLLMStreamStart func(string)         // 首次 text_delta 前 onProgress 已发 _first_token，此处为 stream_start
	onLLMStreamDelta func(string, string) // (streamID, delta)
	onLLMStreamEnd   func(string, string) // (streamID, fullText)
	// 2026-07-12 §127 — 当前正在流式输出的 stream_id（空 = 未在流）。避免多轮 LLM 并发。
	activeStreamID string

	// 2026-07-12 §127 增强 — 最近一次 LLM 调用的完成时间（unix ms），用于前端「Xs 前」相对时间。
	lastLLMCallAt time.Time

	// §128 对话即思考重构:ParallelThink 相关字段已删除(原 §122)。
	// LLM API 输出的 text + tool_use 即是模型"思考"的产物,无需辅助并行 worker。

	// 2026-07-10 §124 增强 — Agent 情绪模块。
	//
	// 情绪是 agent 的「拟人化」状态:每个 bot 在开始游戏时随机抽取一个
	// 初始情绪(狼人有 20% 概率 guilty),后续 LLM 通过 emotion_switch 工具
	// 自主切换。情绪状态写入 BotTranscript 下发,所有真人玩家 + 其它 Agent
	// + 观众都能看到(与 §119 HeartThought 的协议层隔离对照)。
	//
	// emotionMu:独立的 RWMutex,避免与 a.mu(events channel)争抢;
	//     写路径(SwitchEmotion)只持 emotionMu;读路径(CurrentEmotion 等)
	//     用 RLock。
	emotion emotionState

	// 2026-07-20 §131 新增 — 持久化记忆(MEMORY.md)。
	// MemoryMD 是本模型(model_key)跨局积累的 Markdown 记忆,由
	// StartAgentsLocked 在 NewWithRoom 后一次性从 DB 加载(失败仅 log),
	// run.go 每次构造 LLM 请求时经 InjectBlock 追加到 user prompt 末尾。
	// 只在本局启动时赋值一次,之后整个房间生命周期只读;空串 = 不注入。
	MemoryMD string
}

// speakCounterState 60s 滑动窗口状态,内部 mutex 与 a.mu 分离。
//
// 每条窗口独立的 mutex 防止与 events channel 切换争抢。
type speakCounterState struct {
	mu          sync.Mutex
	windowStart time.Time // 60s 窗口起点
	count       int       // 窗口内 speak/interject 累计
}

// SetLLMSemaphore installs the per-room LLM concurrency gate. Pass nil to
// disable throttling. Should be called once at agent construction. BUG-WEREWOLF-P0-NEW-31.
func (a *Agent) SetLLMSemaphore(sema chan struct{}) {
	a.llmSema = sema
}

// AcquireLLMSlot blocks (up to `wait`) to acquire one slot in the per-room LLM
// semaphore. Returns true if acquired; false if the wait elapsed first. Caller
// must defer ReleaseLLMSlot when acquired.
func (a *Agent) AcquireLLMSlot(wait time.Duration) bool {
	if a.llmSema == nil {
		return true
	}
	if wait <= 0 {
		select {
		case a.llmSema <- struct{}{}:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case a.llmSema <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// ReleaseLLMSlot returns one slot to the semaphore. Safe to call without a
// matching acquire (no-op when llmSema is nil or the token was never taken).
func (a *Agent) ReleaseLLMSlot() {
	if a.llmSema == nil {
		return
	}
	select {
	case <-a.llmSema:
	default:
	}
}

// AgentEvent wakes an agent up. `Kind` is one of: "state_change", "your_turn",
// "sync", "game_over", "spectator_speech" (2026-07-08 §13). Context carries the
// latest wwtypes.GameContext.
//
// "spectator_speech" (2026-07-08 §13 / Round 39 §94): fired by
// WerewolfManager.maybeSpectatorWake when a human spectator's public chat
// arrives at the room. The agent loop uses this Kind to:
//
//	(a) require the LLM to make a decision — interject / whisper / idle_silent —
//	    and treat a no-tool end_turn as a silent-but-considered [idle_silent]
//	    audit line (see appendIdleAuditLine);
//	(b) bypass the 8s LLMCallLimiter: the bot's own limiter decides whether
//	    to call LLM now or later. Phase whitelist (pre_wolves / sheriff /
//	    speak / vote) is enforced at the manager side, not here.
type AgentEvent struct {
	Kind    string
	Context wwtypes.GameContext
}

// BotTranscript is a serializable snapshot of one agent's latest decision,
// surfaced to spectators via game.state.bot_contexts[]. It is kept in the leaf
// agent package (no werewolf dependency) so the engine can embed it directly in
// its ClientGameState wire type without an import cycle (werewolf → agent is
// allowed; agent → werewolf is not).
//
// Field json tags MUST stay in sync with ClientWeb/src/types/werewolf.ts
// `BotContextJSON`. BUG-WEREWOLF-P0-NEW-2.
//
// 2026-07-09 §13 增强 — 字段:
//   - ChatHistoryBytes   当前 chatQueue 字节数
//   - ChatHistoryCap     chatQueue 容量上限
//   - LastCompressionAt  上次压缩 unix millis
//   - SpeakCountLastMin  60s 窗口内 speak 累计
//
// 2026-07-12 §128 重构 — "对话即思考":
// LLM API 输出的 text + tool_use 即是模型"思考"的产物,无需单独的思考环节。
// LastThinking / FullThinking / RecentMessages 兼容字段已删除,仅保留决策可观测字段
// (LastDecisionSummary / LastToolInput / LastToolResult / LastOutcome / DecisionInputs)
// 完全替代旧 CoT 展示。
//
// 2026-07-09 §重构 — "Agent 思考 → Agent 交互"：
// LLM 的 CoT 不再下发到 wire 协议(噪声 + 身份泄露风险),改用
// 决策可观测性字段(输入摘要 + 工具调用 + 工具结果 + 决策结果)。
// §128 对话即思考重构:LastThinking / FullThinking / RecentMessages 已物理删除,
// ToolCalls 保留(用于决策可观测)。详见 docs/狼人杀对话即思考设计.md。
//
// 新增 5 字段:
//   - LastDecisionSummary 1 句话(动作 + 目标)
//   - LastToolInput       工具入参的 JSON 字符串(经 sanitizeToolInput 脱敏)
//   - LastToolResult      工具结果前 80 字
//   - LastOutcome         OK / FAIL / skip / idle / quarantine
//   - DecisionInputs      决策输入摘要(数字聚合,无 CoT 文本)
type BotTranscript struct {
	Seat      int      `json:"seat"`
	Model     string   `json:"model"`
	LastTool  string   `json:"last_tool,omitempty"` // 工具名
	ToolCalls []string `json:"tool_calls"`          // 最近工具调用
	UpdatedAt int64    `json:"updated_at"`

	// ==== 2026-07-09 §重构 - 决策可观测性(替代 LastThinking) ====
	// LastDecisionSummary 一句话总结本次决策(如 "speak → 3号(目标 4号 狼人)" / "vote → 2号" / "idle (没轮到我)")。
	// ≤ 50 字,空表示本轮无有效决策。供 AgentInteractionPanel 主区展示。
	LastDecisionSummary string `json:"last_decision_summary,omitempty"`
	// LastToolInput 工具入参的 JSON 字符串(经 sanitizeToolInput 脱敏);
	// 供观众/玩家看到"LLM 决定调什么工具 + 入参是什么"。
	LastToolInput string `json:"last_tool_input,omitempty"`
	// LastToolResult 工具结果的前 80 字截断;供观众/玩家看到"工具调用结果"。
	LastToolResult string `json:"last_tool_result,omitempty"`
	// LastOutcome 决策结果分类:OK / FAIL / skip / idle / quarantine;
	// 供 AgentInteractionPanel 决策输出区右侧徽章展示。
	LastOutcome string `json:"last_outcome,omitempty"`
	// DecisionInputs 决策输入的数字摘要(阶段 / 轮数 / 存活数 / 收到发言数 / 收到 whisper 数 / 500K 队列增量);
	// 替代旧 RecentMessages 文本列表,避免把 LLM 输入的全文(或 CoT)下发。
	DecisionInputs string `json:"decision_inputs,omitempty"`

	// Quarantined is true when this agent has been permanently disabled
	// (5+ consecutive LLM failures, no further LLM calls will fire).
	// The frontend AgentInteractionPanel uses this to render a "已禁用 /
	// 5连失败" indicator instead of showing the bot as "blank". Set by
	// recordTranscript via IsQuarantined() so it stays accurate even
	// after a successful LLM call (which calls ResetConsecutiveFailures
	// and clears quarantine).
	//
	// BUG-WEREWOLF-P1-NEW-46 (Round 39).
	Quarantined bool `json:"quarantined,omitempty"`
	// QuarantineReason carries the last_error string captured when the
	// agent crossed the failure threshold, so the spectator UI can show
	// "已禁用: 403 usage limit" instead of just "已禁用". Trimmed to
	// 200 chars to keep the wire payload bounded.
	//
	// BUG-WEREWOLF-P1-NEW-46 (Round 39).
	QuarantineReason string `json:"quarantine_reason,omitempty"`

	// 2026-07-09 §13 增强 (保留)
	// §128 对话即思考重构:FullThinking 字段已删除。
	ChatHistoryBytes  int   `json:"chat_history_bytes"`            // 当前 chatQueue 字节数
	ChatHistoryCap    int   `json:"chat_history_cap"`              // chatQueue 容量上限(默认 500K)
	LastCompressionAt int64 `json:"last_compression_at,omitempty"` // 上次压缩 unix millis;0=未压缩
	SpeakCountLastMin int   `json:"speak_count_last_min"`          // 60s 窗口内 speak 累计

	// 2026-07-09 §13-bugfix: LLM 调用中实时状态,让前端能显示"正在调用大模型…"倒计时。
	// LLMCallInProgress=true 表示当前 bot 正在等待 LLM HTTP 响应(stream 或 non-stream)。
	// LLMCallStartedAt 是调用开始的 unix 毫秒时间戳(调用结束后保留,供最后一次用时显示)。
	LLMCallInProgress bool  `json:"llm_call_in_progress,omitempty"`
	LLMCallStartedAt  int64 `json:"llm_call_started_at,omitempty"`

	// 2026-07-10 §120 增强 — API 调用耗时统计(公平性机制可见性数据)。
	// LastLLMLatencyMs: 最近一次 LLM 调用耗时(毫秒),前端展示 "上次 X.X s"。
	// AvgLLMLatencyMs: 本局累计的指数加权滑动平均(α=0.3),前端展示 "平均 Y.Y s"。
	// TotalLLMCalls: 本局累计 LLM 调用次数,前端展示 "已调 N 次"。
	// 这三个字段均由 Agent.MarkLLMCallEnd 在 a.mu 保护下更新,通过 recordTranscript
	// 拷到 BotTranscript 上,前端 AgentInteractionPanel 据此渲染性能徽章。
	LastLLMLatencyMs int64 `json:"last_llm_latency_ms,omitempty"`
	AvgLLMLatencyMs  int64 `json:"avg_llm_latency_ms,omitempty"`
	TotalLLMCalls    int   `json:"total_llm_calls,omitempty"`

	// 2026-07-30 §统计增强 — Token + API 统计（纯内存态，不进 DB，房间解散自动释放）。
	// 对齐 Anthropic Response.Body.usage = {input_tokens, output_tokens}。
	// LastXxx: 最近一次调用; TotalXxx: 本局累计。
	// APICallCount = APISuccessCount + APIFailCount。
	LastInputTokens  int `json:"last_input_tokens,omitempty"`
	LastOutputTokens int `json:"last_output_tokens,omitempty"`
	LastAPITokens    int `json:"last_api_tokens,omitempty"`
	TotalInputTokens  int `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int `json:"total_output_tokens,omitempty"`
	TotalAPITokens    int `json:"total_api_tokens,omitempty"`
	APICallCount      int `json:"api_call_count,omitempty"`
	APISuccessCount   int `json:"api_success_count,omitempty"`
	APIFailCount      int `json:"api_fail_count,omitempty"`

	// 2026-07-10 §119「心口不一」机制：HeartThought + HeartThoughtAt 由
	// Agent.RecordLastThought 写入,只在 BotTranscript 上持久化,
	// **绝不**进 chat_message / chat_history 队列。前端 AgentInteractionPanel
	// 可据此高亮显示带 "心口不一" 标签的发言(例:狼人悍跳预言家时,text 说
	// "我昨晚查了 3 号是狼人",HeartThought 是"我是真狼,3 号昨晚被刀了")。
	HeartThought   string `json:"heart_thought,omitempty"`    // 内心独白(LLM 真实想法)
	HeartThoughtAt int64  `json:"heart_thought_at,omitempty"` // 写入时间 unix 毫秒

	// 2026-08-05 §Agent聊天显示优化 — 最后一次公开发言(座位卡实时展示)。
	//
	// **只承载已经广播成功的公开内容**:speak / speak_with_thought 的 public
	// text / emotion_switch_speak / SpeakAuto / interject / last_words。
	// 私聊(whisper)只记 kind 与时间,**不记原文**(接收方之外不可见);
	// wolf_whisper **完全不记**(§133 狼队频道协议层隔离,任何字段都不承载)。
	//
	// 与 HeartThought 的区别:HeartThought 是「没说出口的」,仅观战者可见;
	// LastSpeech 是「已经广播给所有人的」,**所有人可见**(公屏上本就看得到,
	// 座位卡只是把它就近呈现,不新增任何信息面)。
	//
	// 写入入口唯一:Agent.RecordLastSpeech —— 由 werewolf agentRunner 在
	// chatSvc 广播**成功之后**调用,写完立即触发 onTranscriptPublished,
	// 让 game.state 在同一时刻推给前端(修复 P0-2 恒滞后一轮)。
	LastSpeech      string `json:"last_speech,omitempty"`       // 发言原文(≤200 rune 截断)
	LastSpeechAt    int64  `json:"last_speech_at,omitempty"`    // 广播成功时间 unix 毫秒
	LastSpeechKind  string `json:"last_speech_kind,omitempty"`  // speak|emotion_speak|interject|whisper|last_words
	LastSpeechRound int    `json:"last_speech_round,omitempty"` // 发生时的天数(0=夜/未知)

	// 2026-07-10 §124 增强 — 情绪模块字段。
	// Emotion: 当前 bot 的情绪 key(confident/excited/calm/panic/wary/
	//     irritated/grievance/confused/guilty/tired)。**走 wire 公开**,真人
	//     玩家 + 其它 Agent + 观众都能看到(与 HeartThought 协议层隔离对照)。
	// EmotionReason: 当前 emotion 的切换原因(LLM 在 emotion_switch_speak.reason
	//     给出,≤80 字)。2026-08-04 §重构 补齐后端契约,前端 emotion badge
	//     可用此字段做"为什么突然变成紧张"tooltip。
	// EmotionUpdatedAt: 情绪切换时间 unix 毫秒;前端可用于「刚切换」高亮动画。
	// EmotionHistory: 最近 5 次切换记录(供前端展示情绪曲线 / tooltip)。
	Emotion          string          `json:"emotion,omitempty"`
	EmotionReason    string          `json:"emotion_reason,omitempty"`
	EmotionUpdatedAt int64           `json:"emotion_updated_at,omitempty"`
	EmotionHistory   []EmotionRecord `json:"emotion_history,omitempty"`

	// 2026-08-04 §表情特效 — emotion_switch_speak 扩展参数下发
	// (docs/Agent拟人化和表情特效-解决和设计方案-20260804-02.md §5.2)。
	// 前端 SeatCell 据此渲染特效层;全部 omitempty,旧客户端零感知。
	// **协议层隔离红线**(对齐 §119/§133):EmotionCaption 只进本结构,
	// **绝不**写入 chat_message 表 / chat_history 队列 / HeartThought。
	EmotionEffect        string `json:"emotion_effect,omitempty"`          // pulse/shake/sweat/rage/tears/spin_question/glow/drowsy
	EmotionIntensity     string `json:"emotion_intensity,omitempty"`       // low/mid/high
	EmotionCaption       string `json:"emotion_caption,omitempty"`         // ≤20 字表情文字气泡
	EmotionFxStartedAtMs int64  `json:"emotion_fx_started_at_ms,omitempty"` // 特效开始(unix ms)
	EmotionFxDurationMs  int64  `json:"emotion_fx_duration_ms,omitempty"`   // 特效持续(ms, clamp 8–30s)

	// 2026-07-10 §123 增强 — 最近死亡事件。LastDeathVerdict / LastDeathCause
	// 由 Room.appendRoomMessageLocked 在写法官 declare_cause 消息时同步填入。
	// LastDeathSeat 是死亡座位(0-indexed);LastDeathRound 是第几轮白天(0 = 夜间)。
	// 前端 AgentInteractionPanel 据此渲染"最近死亡事件"小卡片,让 bot 看到
	// "上一局 N 号被 X 死亡/处决"的语义。
	LastDeathVerdict string `json:"last_death_verdict,omitempty"`
	LastDeathCause   string `json:"last_death_cause,omitempty"`
	LastDeathSeat    int    `json:"last_death_seat,omitempty"`
	LastDeathRound   int    `json:"last_death_round,omitempty"`

	// 2026-07-10 §重构 — LLM 调用相位状态机,驱动前端多态思考指示器。
	// 设计目标:让前端能区分 5 个状态(idle / calling / streaming / retrying /
	// quarantined),而不再只看到 0/≥5 二态。原来 LLMCallInProgress 只能区分
	// "调用中 vs 没调用",retry loop 内部的 1+1s backoff / 首 token 到达 /
	// 退避等待都看不见。
	//
	// 字段语义:
	//   - LLMCallPhase: 当前 LLM 调用所处阶段。"idle"=未调 / 已完成;"calling"
	//     =HTTP 调用进行中(还没收到首 token,或 non-stream 调用整体未完成);
	//     "streaming"=流式响应,首 token 已到达;"retrying"=retry loop 内,等待
	//     backoff 后重试;"quarantined"=永久禁用,不再调 LLM。
	//   - RetryAttempt: 当前 retry 轮次(1-based)。0 表示首次调用,1 表示已重试
	//     1 次,以此类推。仅在 LLMCallPhase=="retrying" 时非零。
	//   - RetryMaxAttempts: 允许的最大重试次数(默认 llmMaxRetries=1,业务上
	//     通常 +1 展示为 N/M)。
	//   - NextRetryAtMs: 下一次重试的 unix 毫秒时间戳(用于前端倒计时)。
	//     仅在 retrying 时有效。
	//   - LastErrorClass: 上次失败分类。"none"=从未失败 / 已恢复;"5xx"=上游
	//     5xx 服务端错误;"429"=上游限流(RAG 冷却);"timeout"=客户端超时;
	//     "permanent"=永久错误(401/403/400 missing thinking 等)。
	//
	// 写入位置:run.go 的 6 个 hook(safety-net / limiter / semaphore /
	// MarkLLMCallStart / retry loop / MarkLLMCallEnd)。前端 BotPhaseIndicator
	// 据此渲染 5 态指示器,详见 docs/狼人杀-Agent与系统/狼人杀对话即思考设计.md。
	LLMCallPhase     string `json:"llm_call_phase,omitempty"`     // idle|calling|streaming|retrying|quarantined
	RetryAttempt     int    `json:"retry_attempt,omitempty"`      // 1..N,0=首次
	RetryMaxAttempts int    `json:"retry_max_attempts,omitempty"` // 默认 1
	NextRetryAtMs    int64  `json:"next_retry_at_ms,omitempty"`   // 下次重试 unix ms
	LastErrorClass   string `json:"last_error_class,omitempty"`   // none|5xx|429|timeout|permanent|queued|throttled
	// 2026-07-12 §127 — quarantine 时向所有玩家广播的系统提示,BuildClientState 透传。
	QuarantineBroadcast string `json:"quarantine_broadcast,omitempty"`

	// 2026-07-12 §127 增强 — 最近一次 LLM 调用的 unix ms 完成时间戳(0 = 从未调用过)。
	// 前端据此渲染「Xs 前」相对时间 + 实时脉冲。
	LastLLMCallAtMs int64 `json:"last_llm_call_at_ms,omitempty"`

	// 2026-08-13 §20260813-02 U1 — 局内 LLM 语义压缩可观测标记(禁止假成功)。
	// LastCompactAt: 最近一次压缩尝试 unix 毫秒(0=未尝试);
	// LastCompactFallback: true = LLM 压缩失败已显式回退规则式压缩;
	// LastCompactNote: 一行说明(≤120 字)。全部 omitempty,旧客户端零感知。
	LastCompactAt       int64  `json:"last_compact_at,omitempty"`
	LastCompactFallback bool   `json:"last_compact_fallback,omitempty"`
	LastCompactNote     string `json:"last_compact_note,omitempty"`

	// 2026-08-13 §20260813-04 U4 — pre-flight 上下文裁剪可观测标记。
	// LastPreflightAt: 最近一次 pre-flight 裁剪 unix 毫秒(0=从未裁剪);
	// LastPreflightNote: 一行说明(≤160 字,含 payload/预算/窗口/预留四个数)。
	// omitempty 保证未触发时不出现在 wire 上,旧客户端零感知。
	LastPreflightAt   int64  `json:"last_preflight_at,omitempty"`
	LastPreflightNote string `json:"last_preflight_note,omitempty"`

	// §20260810-15 P2: 熔断器状态 wire 字段。前端 BotPhaseIndicator 据此渲染
	// "🔌 限流中"徽章(open_429) / "⚠️ 模型错误"徽章(open_400),与"已禁用"并列。
	// closed/open_400/open_429 三态;空 = closed(避免历史客户端解析报错)。
	CircuitState string `json:"circuit_state,omitempty"`

	// 2026-08-10 §20260810-07 — 多假说并行推演摘要。
	// 解析自 LastDecisionSummary 末尾的「📊 [...]」JSON 段(由 werewolf 房间侧解析后回填),
	// 前端 HistoryDrawer 第 5 sub-tab「🔮 假说」渲染折线图(spectator-only)。
	// **§135 spectator 隔离**:sanitizeBotTranscript 在人类玩家分支清空此字段。
	HypothesisSummary []HypothesisEntryJSON `json:"hypothesis_summary,omitempty"`

	// 2026-08-10 §20260810-12 D1 — 决策留痕运行时回放。
	// 本局 Agent 每次决策追加一条 DecisionEntry,最多保留 30 条(≈30 个轮次),
	// 超过则 FIFO 丢弃最早一条。前端 HistoryDrawer 第 6 sub-tab「🧠 决策回放」渲染。
	//
	// **§135 spectator 隔离**:sanitizeBotTranscript 在人类玩家分支清空此字段。
	// **§128 兼容**:不复制 LLM CoT,只存结构化步骤(轮/阶段/工具/耗时/一句话)。
	// **opt-in**:werewolf.bots_log_decisions=false 时为 nil,零内存 / 零 wire 开销。
	DecisionTrail []DecisionEntry `json:"decision_trail,omitempty"`

	// §20260811-06 U3 — 公开推理链(reasoning_chain 工具产出)。
	// 与 DecisionTrail 的区别:DecisionTrail 自动记录每次决策的工具+耗时;
	// ReasoningChains 由 LLM 显性调用推理工具追加,展示 steps / evidence / conclusion。
	// 同样 §135 spectator 隔离 + opt-in 开关 werewolf.reasoning_chain_enabled。
	ReasoningChains []ReasoningChainEntry `json:"reasoning_chains,omitempty"`

	// §20260811-06 U4 — 行为一致性校验。
	// RoleClaims:本 bot 最近 30 轮的「我是 X」声明(LLM 自由文本关键词抽取);
	// LastConsistencyCheck:最近一次校验结果(R1 反复跳变/R2 平民跳神/R3 投票矛盾/OK)。
	// 玩家侧 sanitize 不清空(本身不揭示身份),但 LLM 不能据此推断身份。
	RoleClaims             []RoleClaim               `json:"role_claims,omitempty"`
	LastConsistencyCheck   *ConsistencyCheckResult   `json:"last_consistency_check,omitempty"`
}

// HypothesisEntryJSON 是对外暴露给前端的假说条目（避免循环导入 werewolf 包）。
type HypothesisEntryJSON struct {
	TargetSeat int    `json:"target_seat"`
	RoleGuess  string `json:"role_guess"`
	Confidence int    `json:"confidence"`
	Supporting string `json:"supporting"`
	Refuting   string `json:"refuting"`
	UpdatedAt  int64  `json:"updated_at"`
}

// BotChatSender is the subset of ChatService the werewolf agentRunner needs to
// translate speak/whisper tool calls into real chat broadcasts. Declared here
// (in the leaf agent package) so both the werewolf engine and the ws.ChatService
// can agree on it without an import cycle (ws → werewolf → agent is OK; we must
// avoid werewolf → ws).
//
// The execution layer (ServerGo/game/werewolf) must pass something that
// implements these two methods. The agent.Run loop only inspects the error
// return — the success payload is ignored.
type BotChatSendResult struct{}

// IdleThinkRunner is the (optional) interface the ToolRunner must implement
// §128 对话即思考重构:IdleThinkRunner 已删除,与 idle_silent 合并。
// 玩家 / 法官均通过 IdleSilentRunner.IdleSilent(role, reason) 留 audit。
// 与 idle_silent 的区别:role 区分调用方(player=玩家,judge=法官);语义上仍是"沉默思考"
// (不广播、不发消息),只是工具名+role 字段让语义区分更精确。
type IdleSilentRunner interface {
	IdleSilent(role, reason string) (string, error)
}

type BotChatSender interface {
	SendFromBot(roomID, botUserID, botAccount, modelKey, text string) (*BotChatSendResult, error)
	WhisperFromBot(roomID, botUserID, botAccount, modelKey, toUserID, toAccount, text string) (*BotChatSendResult, error)
	// SendInterjectFromBot is the bot-equivalent of an "out-of-turn" chat
	// message. BUG-WEREWOLF-AGENT-INTERJECT: during the speak phase any alive
	// bot can voluntarily chime in (follow-up question, banter, mild
	// challenge) without being the formal speaker. The wire envelope
	// includes is_interject=true so the UI can render it as 💬插话
	// distinct from the formal speak broadcast. Same broadcast path as
	// SendFromBot — only the marker differs.
	SendInterjectFromBot(roomID, botUserID, botAccount, modelKey, text string) (*BotChatSendResult, error)
	// 2026-07-16 主持人重构 — SendFromJudge 是法官宣告的广播路径(对齐 SendFromBot)。
	// FromRole="judge",FromAccount="[法官·{model}]";写 chat_message 表(is_judge=true)+
	// BroadcastRoomIncludingSpectators + feed transcript。kind 是事件类型(仅供
	// transcript/活动流记录,不影响广播)。
	SendFromJudge(roomID, fromAccount, modelKey, text, kind string) (*BotChatSendResult, error)
}

// New builds an Agent. The identity turn is seeded from role + faction; the
// Seat is fixed for the agent's lifetime.
//
// roomID + userID identify the bot user in the engine so room-level operations
// (chat broadcast, action routing) work without a second lookup. seat, userID
// remain fixed for the lifetime of the engine.
func New(seat int, modelKey string, role, faction, win string, registry *llm.Registry) (*Agent, error) {
	return NewWithRoom(seat, modelKey, role, faction, win, registry, "", "")
}

// SetProviderForTest overrides the agent's LLM provider. It exists solely to
// let unit tests inject a fake provider that observes / controls the wire
// payload (e.g. to verify BUG-WEREWOLF-P0-8b's thinking-fallback retry). Production
// code must never call this — New / NewWithRoom bind the real provider from the
// registry at construction.
func (a *Agent) SetProviderForTest(p llm.LLMProvider) {
	a.Provider = p
}

// NewWithRoom is like New but also binds the agent to a specific room + bot
// user, so the manager can fire events at the right agent without a lookup.
func NewWithRoom(seat int, modelKey string, role, faction, win string, registry *llm.Registry, roomID, userID string) (*Agent, error) {
	provider, key, err := registry.Get(modelKey)
	if err != nil {
		return nil, fmt.Errorf("agent.New model %q: %w", modelKey, err)
	}
	a := &Agent{
		Seat:           seat,
		RoomID:         roomID,
		UserID:         userID,
		ModelKey:       modelKey,
		Provider:       provider,
		Registry:       registry,
		apiKey:         key,
		Role:           role,
		Faction:        faction,
		Win:            win,
		Memory:         NewMemory(role, faction, win, seat),
		Limiter:        agentcore.NewSpeakLimiter(30 * time.Second), // R81 P0-1 修复: 45s→30s,放宽发言间隔，提高 Agent 发言覆盖率
		WhisperLimiter: agentcore.NewSpeakLimiter(60 * time.Second), // R81 P0-1 修复: 90s→60s,放宽私聊间隔
		// §130 重构(2026-07-13):LLMCallLimiter 已删除 — 按模型自身响应速率自由调用。
		// R76 P1-3 (2026-07-10): 独立 InterjectLimiter,60s 间隔(> speak 45s),
		// 配合 interjectWindow 5min/4条 软上限,防止单 bot 插话刷屏。
		InterjectLimiter: agentcore.NewSpeakLimiter(60 * time.Second),
		MaxToolUse:       0, // §130 重构:0 表示无硬上限,LLM 输出 end_turn 即退出循环
		now:              time.Now,
		// 2026-08-13 §20260813-02 U2 — per-Agent 工具缓存(seat 固定,不共享)。
		toolsCache: NewToolsCache(),
		// BUG FIX 2026-07-09 §13.6: stamp creation time so the spectator
		// HistoryDrawer(🤖独白 sub-tab) can show "started at HH:MM:SS" for bots that
		// haven't spoken yet (placeholder branch in populateBotContexts).
		startedAt: time.Now(),
		// BUG-WEREWOLF-P0-NEW-30: lock the bot identifier into an immutable
		// form at construction. All log entries should reference this via
		// BotID() so two bots at different seats never appear to share one
		// ModelKey value due to log-msg construction reordering.
		botID: fmt.Sprintf("%s:seat=%d:model=%s", roomID, seat, modelKey),
		// 2026-08-10 §20260810-12 D1 — 决策留痕 opt-in 开关(默认 true)。
		// 关停时 AppendDecisionEntry 提前 return + BotTranscript.DecisionTrail nil。
		botsLogDecisions: botsLogDecisionsEnabled(),
	}

	// 2026-08-10 §20260810-14 增强:按模型设置字节预算。
	// 之前所有模型使用同一 DefaultMaxPromptBytes(200KB),不区分上下文窗口大小。
	// DouBao 等小窗口模型(上下文窗口 ~128K-256K)在累积大量历史后容易触发
	// 400 "exceed max message tokens"错误。现在根据模型名称设置更紧凑的预算。
	//
	// 预算策略:按模型上下文窗口的 60% 设置(留 40% 给 system + tools + max_tokens 输出)
	modelBudget := getModelContextBudget(modelKey)
	a.Memory.SetMaxPromptBytes(modelBudget)

	// §128 对话即思考重构:loadAgentParallelInto 已删除(原 §122 配置注入)。

	// 2026-07-12 §127 增强 — 注入外层 LLM 重试上限(cfg.Werewolf.LLMMaxRetries,默认 7)。
	loadAgentRetryConfigInto(a)

	// 2026-07-10 §124 增强 — 初始情绪随机抽取。
	// 没有历史对战数据时(首次开局),从 10 类情绪中随机选一个;狼人 20% 概率 guilty。
	// 历史情绪对接 model_game_log 在后续版本实现;当前为每个 Agent 必随机一次。
	a.emotion.current = pickInitialEmotion(role)
	a.emotion.updatedAtMs = time.Now().UnixMilli()
	a.emotion.reason = "开局初始情绪"
	a.emotion.history = []EmotionRecord{{
		Emotion: a.emotion.current,
		Reason:  a.emotion.reason,
		AtMs:    a.emotion.updatedAtMs,
	}}
	return a, nil
}

// BotID returns the immutable per-Agent identity string set at construction
// time. Safe to use in log fields without race; identical for a given
// (roomID, seat, modelKey) tuple. BUG-WEREWOLF-P0-NEW-30 (Round 33): the
// R33 logs showed seat=4 with model=Qwen-model (≠ initial MinMax-model);
// the suspicion that ModelKey was mutated mid-game turned into a defensive
// fix: use BotID() in run.go's logger calls so the value is provably stable.
func (a *Agent) BotID() string {
	return a.botID
}

// Shutdown detaches the event channel (called when the room is torn down).
// Closing the channel unblocks any goroutine inside Run's select, causing it
// to return.
func (a *Agent) Shutdown() {
	a.Lock()
	defer a.Unlock()
	if a.events != nil {
		close(a.events)
		a.events = nil
	}
}

// PushEvent enqueues `evt` on the per-agent events channel without blocking.
// If the channel buffer is full (e.g. the agent loop is stuck) the event is
// dropped — better to lose a wakeup than to deadlock the broadcast path. If
// the channel has been closed (Shutdown already ran) PushEvent is a no-op.
// Used by the werewolf manager's WakeAllAgents to nudge every bot on a phase
// change.
func (a *Agent) PushEvent(evt AgentEvent) {
	a.Lock()
	ch := a.events
	a.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
		// 缓冲区满，丢弃唤醒；Run 还在处理上一个事件，next state push 会再次触发。
	}
}

// Lock/unlock keep event-channel swap goroutine-safe. We embed a Mutex via a
// struct field so callers can stop an in-flight Run cleanly.
//
// See also wwtypes.GameContext in prompt.go.
func (a *Agent) Lock()   { a.mu.Lock() }
func (a *Agent) Unlock() { a.mu.Unlock() }

// MarkWhisper records that a whisper happened now, resetting the 60s whisper
// interval. Called by Run after a successful whisper tool_use so the tighter
// whisper throttle is enforced independently of the 30s speak throttle.
func (a *Agent) MarkWhisper() {
	if a.WhisperLimiter != nil {
		a.WhisperLimiter.Mark()
	}
}

// BotTranscript returns a copy of the agent's latest decision snapshot, or nil
// if the agent has never completed a decision. Safe to call from the room's
// broadcast goroutine while the agent loop may be writing — it copies under
// a.mu without holding it during Memory reads. BUG-WEREWOLF-P0-NEW-2.
func (a *Agent) BotTranscript() *BotTranscript {
	a.Lock()
	defer a.Unlock()
	if a.lastTranscript == nil {
		return nil
	}
	bt := *a.lastTranscript
	// 2026-07-09: always populate LLM call state from live fields, not the
	// stored snapshot, so the transcript reflects whether a call is in flight
	// right now (even for quarantined bots or those with prior decisions).
	bt.LLMCallInProgress = a.llmCallInProgress
	bt.LLMCallStartedAt = a.llmCallStartedAt.UnixMilli()
	// 2026-07-10 §120 增强 — API 调用耗时统计字段。
	bt.LastLLMLatencyMs = a.lastLLMLatencyMs
	bt.AvgLLMLatencyMs = a.avgLLMLatencyMs
	bt.TotalLLMCalls = a.totalLLMCalls
	// 2026-07-30 §统计增强 — 透出 Token + API 统计（纯内存态）。
	bt.LastInputTokens = a.lastInputTokens
	bt.LastOutputTokens = a.lastOutputTokens
	bt.LastAPITokens = a.lastAPITokens
	bt.TotalInputTokens = a.totalInputTokens
	bt.TotalOutputTokens = a.totalOutputTokens
	bt.TotalAPITokens = a.totalAPITokens
	bt.APICallCount = a.apiCallCount
	bt.APISuccessCount = a.apiSuccessCount
	bt.APIFailCount = a.apiFailCount
	// 2026-07-10 §重构 — LLM 调用相位状态机 live 字段透出。
	// 这些字段全部由 run.go 在 a.mu 保护下写入,这里在锁内一并拷给 bt,
	// 前端 BotPhaseIndicator 据此渲染 5 态指示器。
	bt.LLMCallPhase = a.llmCallPhase
	bt.RetryAttempt = a.retryAttempt
	bt.RetryMaxAttempts = a.retryMaxAttempts
	bt.NextRetryAtMs = a.nextRetryAtMs
	bt.LastErrorClass = a.lastErrorClass
	// 2026-08-13 §20260813-02 U1 — 压缩可观测标记从 live 字段透出。
	bt.LastCompactAt = a.compactAt
	bt.LastCompactFallback = a.compactFallback
	bt.LastCompactNote = a.compactNote
	// 2026-08-13 §20260813-04 U4 — pre-flight 裁剪标记同源透出。
	bt.LastPreflightAt = a.preflightAt
	bt.LastPreflightNote = a.preflightNoteText
	// §20260810-15 P2: 透传熔断器状态。前端据此外显显示 "🔌 限流中" /
	// "⚠️ 模型错误"徽章,避免用户在 Tencent-model 16:32 这类场景下只能
	// 看到"该 bot 停了"而无任何原因提示。
	if p, ok := a.Provider.(*anthropic.Provider); ok {
		bt.CircuitState = p.CircuitState(a.ModelKey)
	}
	// 2026-07-12 §127 — 透传 quarantine 广播消息(仅一次有效)。
	bt.QuarantineBroadcast = a.quarantineBroadcast
	a.quarantineBroadcast = ""
	// 2026-07-12 §127 增强 — 透出最近一次 LLM 调用完成时间,前端据此渲染相对时间。
	if !a.lastLLMCallAt.IsZero() {
		bt.LastLLMCallAtMs = a.lastLLMCallAt.UnixMilli()
	}
	// 2026-07-10 §124 增强 — 情绪字段始终从 live emotion 状态读取(实时反映
	// 当前情绪,与 lastTranscript 何时生成无关)。
	bt.Emotion = a.emotion.current
	bt.EmotionReason = a.emotion.reason
	bt.EmotionUpdatedAt = a.emotion.updatedAtMs
	if len(a.emotion.history) > 0 {
		bt.EmotionHistory = make([]EmotionRecord, len(a.emotion.history))
		copy(bt.EmotionHistory, a.emotion.history)
	}
	// 2026-08-04 §表情特效 — fx 字段与 emotion 同源(随 SwitchEmotionFx
	// 原子更新;speak 失败回滚时整组不动)。零值自动 omitempty,旧客户端零感知。
	bt.EmotionEffect = a.emotion.fxEffect
	bt.EmotionIntensity = a.emotion.fxIntensity
	bt.EmotionCaption = a.emotion.fxCaption
	bt.EmotionFxStartedAtMs = a.emotion.fxStartedAtMs
	bt.EmotionFxDurationMs = a.emotion.fxDurationMs
	return &bt
}

// appendIdleAuditLine 在 BotTranscript.LastOutcome 标记 "idle" 并刷新时间戳,
// 用于 §13.5 "Agent 必须思考" 语义:即使 LLM end_turn 后没调任何工具,也必须
// 留痕表明该 bot 思考过但选择沉默。
//
// 2026-07-09 §重构:不再追加到 RecentMessages(已置空);改写 LastOutcome=idle
// + UpdatedAt,让 AgentInteractionPanel 在决策输出区显示"💤 idle (沉默思考)"。
// 与 LLM 主动调 idle_think 工具等价(§13.2),但本方法由 run.go 在 handleEvent
// 的 spectator_speech 路径上自动触发,zero LLM token cost。
//
// reason:留痕原因(不展示,仅供日志/调试)。
func (a *Agent) appendIdleAuditLine(reason string) {
	now := time.Now()
	a.Lock()
	if a.lastTranscript == nil {
		a.lastTranscript = &BotTranscript{
			Seat:  a.Seat,
			Model: a.ModelKey,
		}
	}
	a.lastTranscript.LastOutcome = "idle"
	a.lastTranscript.LastDecisionSummary = "idle (沉默思考: " + truncate(reason, 30) + ")"
	a.lastTranscript.UpdatedAt = now.UnixMilli()
	a.Unlock()
}

// RecordLastThought 是 2026-07-10 §119「心口不一」机制的核心写入入口。
// SpeakWithThought 调用此方法把 LLM 的 internal_thought 字段写入 BotTranscript,
// **仅**修改 FullThinking 与 RecentMessages 字段(已通过 recordTranscript
// 在每次 LLM 调用后被覆写,本身存的就是「最近一次 LLM 响应的思考文本」)。
//
// 设计要点:
//   - **不** append 到 RecentMessages(那里是 LLM 文本流,会被下一次 recordTranscript
//     全量替换);改写 FullThinking 即可,前端 AgentInteractionPanel 直接读此字段。
//   - **不**进 chat_message 表 / chat_history 队列 → 其他玩家看不到;
//   - **不**写入 Memory.messages → 不会影响下一轮 LLM 上下文(避免 LLM 看到
//     自己的「真实内心」后不再能继续表演);
//   - LastDecisionSummary 加 "💭 心口不一" 前缀,方便观战者识别哪些发言
//     用了欺骗工具。
//   - 加 markHeartThoughtLocked 字段(新加)在 BotTranscript 上,前端可以
//     高亮显示带 "心口不一" 标签的发言。
func (a *Agent) RecordLastThought(internalThought string) {
	if internalThought == "" {
		return
	}
	now := time.Now()
	a.Lock()
	defer a.Unlock()
	if a.lastTranscript == nil {
		a.lastTranscript = &BotTranscript{
			Seat:  a.Seat,
			Model: a.ModelKey,
		}
	}
	// §128 对话即思考重构:FullThinking 字段已删除,HeartThought 协议层隔离保留。
	// 标记「心口不一」状态 — 前端 FactionDrawer (spectator 守卫) 可据此高亮显示。
	a.lastTranscript.HeartThought = internalThought
	a.lastTranscript.HeartThoughtAt = now.UnixMilli()
	// LastDecisionSummary 追加标记 — 观战者一眼能识别该 bot 在玩「心口不一」。
	if a.lastTranscript.LastDecisionSummary == "" {
		a.lastTranscript.LastDecisionSummary = "💭 心口不一发言"
	} else if !strings.Contains(a.lastTranscript.LastDecisionSummary, "心口不一") {
		a.lastTranscript.LastDecisionSummary = a.lastTranscript.LastDecisionSummary + " · 💭心口不一"
	}
	a.lastTranscript.UpdatedAt = now.UnixMilli()
}

// RecordLastSpeech 是 2026-08-05 §Agent聊天显示优化 的核心写入入口(修复 P0-2/P0-3)。
//
// 由 werewolf agentRunner 在 chatSvc 广播**成功之后**调用,把「刚刚已经广播出去
// 的公开发言」落到 BotTranscript.LastSpeech* 四字段,并**立即**触发
// onTranscriptPublished 回调 → room 侧 broadcast game.state,让人类在同一次推送里
// 看到座位卡气泡更新,而不必等下一次无关广播(阶段切换 / watchdog / 其他 bot 的
// transcript 发布)。
//
// 参数:
//   - text  发言原文;**必须**是已通过全部过滤链、已经广播出去的公开文本。
//     whisper 传 ""(只记事件不记原文 — 私聊原文只对收发双方可见)。
//   - kind  speak / emotion_speak / interject / whisper / last_words。空 = no-op。
//   - round 发生时的天数(0 = 夜间 / 调用方无法廉价获取时的占位)。
//
// 协议层隔离红线(§119/§133):
//   - internal_thought 走 RecordLastThought → HeartThought,**不**经本方法;
//   - wolf_whisper **绝不**调用本方法(狼队频道不可见是核心博弈价值)。
func (a *Agent) RecordLastSpeech(text, kind string, round int) {
	if kind == "" {
		return
	}
	now := time.Now()
	a.Lock()
	if a.lastTranscript == nil {
		a.lastTranscript = &BotTranscript{
			Seat:  a.Seat,
			Model: a.ModelKey,
		}
	}
	// ≤200 rune 截断(rune-safe,超出补 "…"),与 wire 上其它文本字段同风格。
	a.lastTranscript.LastSpeech = truncate(text, 200)
	a.lastTranscript.LastSpeechAt = now.UnixMilli()
	a.lastTranscript.LastSpeechKind = kind
	a.lastTranscript.LastSpeechRound = round
	a.lastTranscript.UpdatedAt = now.UnixMilli()
	// 与 recordTranscript 末尾同一范式:在 a.mu **解锁之后**才 go cb(),避免
	// 回调内取 room.mu 时与 a.mu 形成锁序风险。注意本函数用显式 Unlock 而
	// **不用** defer(§212 教训:两段式解锁风格混用会导致双重解锁 fatal)。
	cb := a.onTranscriptPublished
	a.Unlock()
	if cb != nil {
		go cb()
	}
}

// IsQuarantined reports whether this agent has been permanently quarantined
// (its LLM provider is broken for the rest of the game). Theroom manager uses
// this to emit a silent skip action for the bot's turn without waking the
// agent goroutine — avoiding infinite auto-skip / scheduleReWake loops.
// BUG-WEREWOLF-P0-NEW-3.
func (a *Agent) IsQuarantined() bool {
	a.Lock()
	defer a.Unlock()
	return a.quarantined
}

// SetQuarantined marks this agent permanently broken for the rest of the
// game AND cancels any pending scheduleReWake timer. After this returns the
// agent will not receive further re-wake events for prior failures, even if
// the reWake goroutine already fired a time.After. Safe to call from any
// goroutine; idempotent. BUG-WEREWOLF-P0-NEW-4 (Round 24): without the
// cancel, a quarantined agent kept receiving delayed reWakes that re-entered
// handleEvent, called the LLM again, failed again, and re-set quarantine —
// logging duplicate "quarantined" lines and flooding the in-memory
// BotAgents map with dead wakes until the goroutine finally timed out.
//
// BUG-WEREWOLF-P0-NEW-27 (Round 34): after setting quarantine, fire the
// onQuarantine callback (in a goroutine) so the room manager can immediately
// dispatch the phase's safe skip for a quarantined acting bot. Without this
// notification, the manager only learns about quarantine on the next wake
// event — which may never come if the bot is the current speaker and no
// other event source pushes a wake.
func (a *Agent) SetQuarantined() {
	a.Lock()
	prev := a.reWakeCancel
	a.quarantined = true
	a.reWakeCancel = nil
	cb := a.onQuarantine
	// 2026-07-12 §127 — 生成一条系统广播消息,让人类/观众立即看到该 bot 被禁用。
	a.quarantineBroadcast = fmt.Sprintf("⚠️ %d号Agent(%s)因连续调用失败被禁用,后续由系统代为操作。", a.Seat+1, a.ModelKey)
	a.Unlock()
	if prev != nil {
		prev() // cancel any in-flight scheduleReWake
	}
	// BUG-WEREWOLF-P1-NEW-46 (Round 39): the bot just stopped calling the
	// LLM forever. Publish a refreshed BotTranscript so the spectator
	// HistoryDrawer(🤖独白 sub-tab) shows the "已禁用" badge instead of going blank
	// until the next memory-driven snapshot fires (which it never will).
	a.publishQuarantineTranscript()
	if cb != nil {
		go cb()
	}
}

// SetOnQuarantine registers a callback that fires (in a goroutine) when the
// agent transitions to quarantined state. Called once at agent construction
// by the room manager. BUG-WEREWOLF-P0-NEW-27.
func (a *Agent) SetOnQuarantine(cb func()) {
	a.Lock()
	a.onQuarantine = cb
	a.Unlock()
}

// SetOnTranscriptPublished registers a callback that fires (in a goroutine)
// after recordTranscript publishes a fresh BotTranscript snapshot. The room
// manager wires it to broadcast game.state for real-time emotion sync.
// Called once at agent construction by the room manager.
func (a *Agent) SetOnTranscriptPublished(cb func()) {
	a.Lock()
	a.onTranscriptPublished = cb
	a.Unlock()
}

// ResetConsecutiveFailures clears the failure counter after a successful LLM
// call — also clears quarantine if it was set on a transient blip that the
// model recovered from. Safe to call from handleEvent after any successful
// provider.Chat response. BUG-WEREWOLF-P0-NEW-3.
func (a *Agent) ResetConsecutiveFailures() {
	a.Lock()
	defer a.Unlock()
	a.consecutiveFailures = 0
	a.quarantined = false
	// BUG-WEREWOLF-P1-NEW-46 (Round 39): the model came back; clear the
	// stale error so a subsequent quarantine (if any) records the new
	// failure, not the long-cleared previous one.
	a.lastError = ""
	// BUG-R48-P0-1: 重置失败时间, 让下次失败重新开始冷却窗口计时。
	a.lastFailureTime = time.Time{}
	// 2026-07-10 §重构 — 成功调用后清场相位/retry/error class。
	// phase 留 "idle",retry 计数清零,lastErrorClass 重置为 "none"。
	a.llmCallPhase = PhaseIdle
	a.retryAttempt = 0
	a.retryMaxAttempts = 0
	a.nextRetryAtMs = 0
	a.lastErrorClass = "none"
	// 2026-07-29 优化:成功调用后重置 speak idle 计数器。
	// 2026-08-04 §重构 — emotionSwitchAloneCount 字段已删除。
	a.speakTurnIdleSilentCount = 0
	// BUG-R232-P1-02 (2026-08-02): 模型恢复后清零熔断失败计数器,
	// 下次熔断打开时重新从 0 开始计数与日志降噪。
	a.circuitOpenFailureCount = 0
}

// ConsecutiveFailures 返回当前连续 LLM 调用失败次数。
// 道具系统用此判断目标是否"心态崩了"（>2 时中招率 +10%）。
// 并发安全：持 a.mu 读。
func (a *Agent) ConsecutiveFailures() int {
	a.Lock()
	defer a.Unlock()
	return a.consecutiveFailures
}

// RecordFailure 处理一次 LLM 调用失败，更新 consecutiveFailures 与
// lastFailureTime（持 a.mu 写）。返回 (新的 consecutiveFailures,
// 是否处于 cooldown 窗口内 —— 即本次未递增 / 未更新 lft 但被窗口吸收)。
//
// 语义：
//   - network/timeout transient：bump lastFailureTime 滑动 cooldown 窗口，
//     不递增 consecutiveFailures（避免慢模型/上游抖动被永久 quarantine）。
//     但 transient 仍算"进入冷却"，返回 inCooldown=true。
//   - 其它错误且在 cooldown 窗口内：既不递增、也不动 lastFailureTime
//     （与原行为兼容）。inCooldown=true。
//   - 其它错误且超出 cooldown：递增 + 重置 lastFailureTime。inCooldown=false。
//
// FIX (R186-A): 之前 run.go:746-762 直接 `a.consecutiveFailures++` /
// `a.lastFailureTime = now`，与其它路径（ResetConsecutiveFailures / ConsecutiveFailures /
// SetQuarantined / recordTranscript）持 a.mu 形成 data race，Go race detector
// 会 flag。本次修复把所有 mutation 收敛到持锁的 helper。
//
// 同时修正一个语义 bug：transient 错误之前完全不更新 lastFailureTime，导致一个
// bot 持续 timeout 5 分钟后再撞 403 时 cooldown 早已过期、单次 403 立即计数。
// 现在 transient 也滑动 cooldown 窗口，让失败序列在窗口内持续累计。
func (a *Agent) RecordFailure(now time.Time, transient bool, window time.Duration) (newCF int, inCooldown bool) {
	a.Lock()
	defer a.Unlock()
	if transient {
		// Transient 错误只滑动 cooldown 窗口,不递增计数。但视作已进入
		// 冷却(下一次非 transient 失败会被吸收)。
		a.lastFailureTime = now
		return a.consecutiveFailures, true
	}
	if !a.lastFailureTime.IsZero() && now.Sub(a.lastFailureTime) < window {
		return a.consecutiveFailures, true
	}
	a.consecutiveFailures++
	a.lastFailureTime = now
	return a.consecutiveFailures, false
}

// FailureSnapshot 持 a.mu 一次性读 consecutiveFailures 与 quarantined，
// 用于 run.go:798-801 quarantine 检查时不被 ResetConsecutiveFailures 并发清零撕裂。
func (a *Agent) FailureSnapshot() (int, bool) {
	a.Lock()
	defer a.Unlock()
	return a.consecutiveFailures, a.quarantined
}

// SetLastError records the most recent LLM provider error so the
// BotTranscript.QuarantineReason field can show it on the spectator panel.
// Truncated to 240 chars here so a runaway upstream error string cannot
// blow up the wire payload. BUG-WEREWOLF-P1-NEW-46 (Round 39).
func (a *Agent) SetLastError(msg string) {
	a.Lock()
	defer a.Unlock()
	a.lastError = truncate(msg, 240)
}

// recordTranscript builds and stores a fresh decision snapshot from the agent's
// memory, then publishes it under a.mu. Called by run.go after each successful
// LLM response so spectators see the bot's latest decision (NOT the LLM CoT,
// 2026-07-09 §重构). Memory is read under its own lock (m.mu) BEFORE taking
// a.mu, so the two locks are never held nested — this avoids any
// lock-ordering hazard with the broadcast path (which takes only a.mu).
// BUG-WEREWOLF-P0-NEW-2.
//
// 2026-07-09 §重构 - "Agent 思考 → Agent 交互":
// 旧 LastThinking / FullThinking / RecentMessages 字段保留 wire 兼容但置空。
// 新增 5 个决策可观测字段:LastDecisionSummary / LastTool / LastToolInput /
// LastToolResult / LastOutcome / DecisionInputs(详见
// docs/Agent交互设计.md §2.2)。
//
// 旧 5 个 chatQueue 统计字段(ChatHistoryBytes / ChatHistoryCap /
// LastCompressionAt / SpeakCountLastMin)保留,这些与决策正交。
func (a *Agent) recordTranscript() {
	if a.Memory == nil {
		return
	}
	// 收集 chatQueue 统计(独立 mutex,不放进 a.mu)
	var chatBytes int64
	var chatCap int
	var lastCompress int64
	if a.chatQueue != nil {
		chatBytes, lastCompress, _, _ = a.chatQueue.Stats()
		chatCap = a.chatCap
	}
	// 收集本轮决策可观测性(由 run.go::handleEvent 在 STAGE 3/4 之间写入)
	a.Lock()
	dec := a.lastDecision
	a.lastDecision = RecordDecisionState{} // 一次性消费
	a.Unlock()

	// 工具调用最近列表(供前端"工具调用"折叠区使用)— 5 条
	tools := a.Memory.RecentTools(5)
	toolCalls := make([]string, 0, len(tools))
	for _, t := range tools {
		toolCalls = append(toolCalls, t.Name+": "+truncate(t.Result, 60))
	}

	// 决策可观测字段
	decisionSummary := dec.LastDecisionSummary
	if decisionSummary == "" {
		// fallback:从 toolCalls 最后一条生成简短总结
		decisionSummary = BuildDecisionSummary(dec.LastToolName, dec.LastToolInput, dec.LastToolResult)
	}
	toolInputJSON := SanitizeToolInput(dec.LastToolName, dec.LastToolInput)
	toolResult := truncate(dec.LastToolResult, 80)
	outcome := dec.LastOutcome
	if outcome == "" {
		outcome = "OK" // 默认成功(若 run.go 未显式分类)
	}

	now := time.Now().Unix()
	bt := &BotTranscript{
		Seat:      a.Seat,
		Model:     a.ModelKey,
		LastTool:  dec.LastToolName,
		ToolCalls: toolCalls,
		UpdatedAt: now,

		// 新增 5 字段 - 决策可观测性
		LastDecisionSummary: decisionSummary,
		LastToolInput:       toolInputJSON,
		LastToolResult:      toolResult,
		LastOutcome:         outcome,
		DecisionInputs:      dec.DecisionInputs,
		// §128 对话即思考重构:LastThinking / FullThinking / RecentMessages 字段已删除
	}
	// BUG-WEREWOLF-P1-NEW-46 (Round 39): include quarantine state in the
	// snapshot so the frontend AgentInteractionPanel can render an explicit
	// "已禁用 / 5连失败" badge instead of going blank after the bot stops
	// calling the LLM. Captured under a.mu along with lastTranscript so a
	// concurrent SetQuarantined / ResetConsecutiveFailures cannot race the
	// publish.
	a.Lock()
	bt.Quarantined = a.quarantined
	bt.QuarantineReason = truncate(a.lastError, 200)
	// 2026-07-12 §127 — 透传 quarantine 广播消息,只传一次后清空,避免重复刷屏。
	bt.QuarantineBroadcast = a.quarantineBroadcast
	a.quarantineBroadcast = ""
	bt.ChatHistoryBytes = int(chatBytes)
	bt.ChatHistoryCap = chatCap
	bt.LastCompressionAt = lastCompress
	bt.SpeakCountLastMin = a.snapshotSpeakCounter()
	// 2026-08-04 §重构 — 保留前一次 transcript 的 HeartThought。
	// SpeakWithThought 路径会先调 RecordLastThought 写入 HeartThought,
	// recordTranscript 在所有工具调用之后触发 → 直接覆盖会把心口不一
	// 的内心独白抹掉。这里把上一次 transcript 的 heart_thought 拷贝到新
	// transcript(只在未设置时覆盖,避免本轮 overwrite 上轮结果)。
	if bt.HeartThought == "" && a.lastTranscript != nil {
		bt.HeartThought = a.lastTranscript.HeartThought
		bt.HeartThoughtAt = a.lastTranscript.HeartThoughtAt
	}
	// 2026-08-05 §Agent聊天显示优化 — 与 HeartThought 同源问题:RecordLastSpeech
	// 在工具派发阶段(广播成功那一刻)写入,recordTranscript 在所有工具调用之后
	// 才重建 BotTranscript,直接整体替换会把本轮刚写入的发言原文抹掉,座位卡
	// 气泡瞬间闪空。这里把上一次 transcript 的 last_speech* 一组拷过来。
	if a.lastTranscript != nil {
		bt.LastSpeech = a.lastTranscript.LastSpeech
		bt.LastSpeechAt = a.lastTranscript.LastSpeechAt
		bt.LastSpeechKind = a.lastTranscript.LastSpeechKind
		bt.LastSpeechRound = a.lastTranscript.LastSpeechRound
	}
	// 2026-08-13 §20260813-02 U1 — 与 LastSpeech 同源:压缩标记由
	// maybeCompactMemory 在 LLM 调用之外的 goroutine 写入,recordTranscript
	// 重建快照时直接从 live 字段透传,避免被整体替换抹掉。
	bt.LastCompactAt = a.compactAt
	bt.LastCompactFallback = a.compactFallback
	bt.LastCompactNote = a.compactNote
	// 2026-08-13 §20260813-04 U4 — pre-flight 裁剪标记同源透出。
	bt.LastPreflightAt = a.preflightAt
	bt.LastPreflightNote = a.preflightNoteText
	// 2026-08-04 §表情特效 — fx 字段与 live emotion 状态同源,recordTranscript
	// 作为每次 LLM 响应后的快照也一并透传(与 BotTranscript() 的实时读取
	// 保持一致的 wire 形状)。
	bt.EmotionEffect = a.emotion.fxEffect
	bt.EmotionIntensity = a.emotion.fxIntensity
	bt.EmotionCaption = a.emotion.fxCaption
	bt.EmotionFxStartedAtMs = a.emotion.fxStartedAtMs
	bt.EmotionFxDurationMs = a.emotion.fxDurationMs
	a.lastTranscript = bt
	// 2026-08-10 §20260810-12 D1 — 决策留痕 hook(在 lastTranscript 赋值之后追加)。
	// §130 接线验证:本调用点与 run.go:1113 / run.go:1654 两处 recordTranscript 同源,
	// 只要 recordTranscript 调用即触发 → trail 自动累计。AppendDecisionEntry 内部
	// 判 botsLogDecisions=false 直接 return,零开销承诺。
	a.AppendDecisionEntry(DecisionEntry{
		ToolName:    dec.LastToolName,
		ToolSummary: decisionSummary,
		TookMs:      a.lastLLMLatencyMs,
		CreatedAt:   time.Now().UnixMilli(),
	})
	// §20260811-06 U4 — 一致性校验 hook。
	// 1) 抽取本轮发言中的「我是 X」声明 → 追加到 RoleClaims(round=0 表示
	//    "未填 round",一致性检测时跳过 round==0 的 entries,避免 R1 误报);
	// 2) 跑 R1/R2 检测(§120 不计入 consecutiveFailures);
	// 3) 写 LastConsistencyCheck(供 prompt 末尾 ⚠️ 块消费)。
	if dec.LastDecisionSummary != "" {
		if claim := extractRoleClaim(dec.LastDecisionSummary); claim != "" {
			// 简化版:round 暂存 0(后续 dispatch 路径若能拿到 ctx.Round 可
			// 改为真实 round;RunCheckLocked 内部对 round==0 跳过同 round 比对,
			// 不影响 R2 跨 round 判定)。
			a.AppendRoleClaim(0, claim)
		}
	}
	if check := runConsistencyCheckLocked(a); check.Rule != "OK" {
		a.SetLastConsistencyCheck(check)
	}
	// 2026-08-05 §表情实时同步:在 a.mu 解锁后触发 transcript 发布回调(在
	// goroutine 内运行,避免回调内取 room.mu 时与 a.mu 产生锁序风险)。
	// 回调 = room 侧 broadcast game.state → 前端座位卡 EmotionAvatar 即时刷新
	// 情绪 + fx。无回调(= nil)时静默跳过,旧部署零感知。
	cb := a.onTranscriptPublished
	a.Unlock()
	if cb != nil {
		go cb()
	}
}

// ---------- 2026-07-09 §13 增强:Speak Counter + Chat History Queue 接入 ----------

// ChatQueue 返回 agent 的 agentcore.ChatHistoryQueue(由 WerewolfRoom.StartAgentsLocked
// 注入)。调用方(appendRoomMessage)据此 push 消息。返回 nil 表示 chatQueue 未启用。
func (a *Agent) ChatQueue() *agentcore.ChatHistoryQueue {
	return a.chatQueue
}

// ChatCap 返回 chatQueue 的容量上限(由 SetChatQueue 时锁定);若 chatQueue
// 尚未注入,返回 0。BUG FIX 2026-07-09 §13.6: 占位场景需要该值以便前端
// summary 行展示 "0KB / 500KB"。
func (a *Agent) ChatCap() int {
	return a.chatCap
}

// StartedAt 返回 Agent 的构造时间(由 NewWithRoom 打点)。BUG FIX
// 2026-07-09 §13.6: 占位场景(BotTranscript() == nil)使用该时间戳作为
// UpdatedAt,使前端能区分"刚启动未发言"和"长时间未刷新"。
func (a *Agent) StartedAt() time.Time {
	return a.startedAt
}

// MarkLLMCallStart records that an LLM HTTP call is now in flight. Called by
// run.go immediately before callProvider. Guarded by a.mu so the broadcast
// path can read it safely via IsLLMCallInProgress / BotTranscript.
func (a *Agent) MarkLLMCallStart() {
	a.Lock()
	a.llmCallInProgress = true
	a.llmCallStartedAt = time.Now()
	a.Unlock()
}

// MarkLLMCallEnd records that the LLM HTTP call has finished (success or
// error). The startedAt timestamp is preserved so the UI can show the last
// call's duration. Guarded by a.mu.
//
// 2026-07-10 §120 增强:累加本 bot 的 API 调用耗时到 avgLLMLatencyMs(滑动平均)
// 与 lastLLMLatencyMs(最后一次),并写入 BotTranscript.LLMLatencyMs /
// BotTranscript.AvgLLMLatencyMs 字段。前端「Agent 思考」面板可以展示
// "当前模型平均耗时 X.X s / 上次 Y.Y s",观战者据此判断哪个 bot 反应快。
// 公平性机制:BuildUserPrompt 在 prompt 末尾追加【模型响应速率】块,
// 让 LLM 知道自己的相对速率,引导"反应慢的 bot 减少工具调用 / 反应快的
// bot 多承担对话"(§120)。
//
// 2026-08-01 BUG-R225-P2-03: 本函数曾无条件 +1 成功,与文档承诺矛盾;
// 现拆为「纯复位」ResetLLMCallState 与「成功计数」MarkLLMCallEndWithUsage
// 两条路径,本函数仅保留向后兼容语义(同 ResetLLMCallState)。
func (a *Agent) MarkLLMCallEnd() {
	a.ResetLLMCallState()
}

// ResetLLMCallState 纯复位 LLM 调用状态(耗时/lastLLMCallAt/in-progress),
// 不累加任何 apiCallCount / apiSuccessCount / apiFailCount / token 统计。
// 适用于:(a) 循环 iteration 顶部的 safety-net 清理;(b) 已知不会计数的
// 辅助调用失败(如 speak_floor 失败,本属可选提醒,既不计入成功也不计入
// 失败)。错误处理:RecordAPIFailure / 真正的失败计数走独立路径。
func (a *Agent) ResetLLMCallState() {
	a.Lock()
	if a.llmCallInProgress && !a.llmCallStartedAt.IsZero() {
		elapsed := time.Since(a.llmCallStartedAt)
		ms := elapsed.Milliseconds()
		a.lastLLMLatencyMs = ms
		// 滑动平均(指数加权,α=0.3):新观测占 30% 权重,历史平均占 70%。
		// 避免冷启动时一个极端长尾样本把均值带偏。
		if a.avgLLMLatencyMs == 0 {
			a.avgLLMLatencyMs = ms
		} else {
			a.avgLLMLatencyMs = int64(0.7*float64(a.avgLLMLatencyMs) + 0.3*float64(ms))
		}
		a.totalLLMCalls++
	}
	// §127: 记录完成时间,前端据此渲染「Xs 前」。不论是否成功,以EndTime为准。
	a.lastLLMCallAt = time.Now()
	a.llmCallInProgress = false
	a.Unlock()
}

// MarkLLMCallEndWithUsage 标记一次 LLM 调用成功完成(累加 Token + 成功计数)。
// 2026-07-30 §统计增强:从 LLMResponse.Usage 读取 input_tokens / output_tokens 累加。
// 仅在「确实拿到 LLMResponse」的路径上调用;失败路径请改用 RecordAPIFailure。
func (a *Agent) MarkLLMCallEndWithUsage(usage llmtypes.LLMUsage) {
	a.Lock()
	if a.llmCallInProgress && !a.llmCallStartedAt.IsZero() {
		elapsed := time.Since(a.llmCallStartedAt)
		ms := elapsed.Milliseconds()
		a.lastLLMLatencyMs = ms
		// 滑动平均(指数加权,α=0.3):新观测占 30% 权重,历史平均占 70%。
		// 避免冷启动时一个极端长尾样本把均值带偏。
		if a.avgLLMLatencyMs == 0 {
			a.avgLLMLatencyMs = ms
		} else {
			a.avgLLMLatencyMs = int64(0.7*float64(a.avgLLMLatencyMs) + 0.3*float64(ms))
		}
		a.totalLLMCalls++
	}
	// 2026-07-30 §统计增强:累加 Token（纯内存态）。
	a.lastInputTokens = usage.InputTokens
	a.lastOutputTokens = usage.OutputTokens
	a.lastAPITokens = usage.InputTokens + usage.OutputTokens
	a.totalInputTokens += usage.InputTokens
	a.totalOutputTokens += usage.OutputTokens
	a.totalAPITokens += a.lastAPITokens
	a.apiCallCount++
	a.apiSuccessCount++
	// §127: 记录完成时间,前端据此渲染「Xs 前」。不论是否成功,以EndTime为准。
	a.lastLLMCallAt = time.Now()
	a.llmCallInProgress = false
	a.Unlock()
}

// RecordAPIFailure 记录一次 LLM 调用失败（递增 apiCallCount + apiFailCount,
// 不累加 Token）。2026-07-30 §统计增强。
func (a *Agent) RecordAPIFailure() {
	a.Lock()
	defer a.Unlock()
	a.apiCallCount++
	a.apiFailCount++
}

// AgentTokenStats 返回本 bot 的 Token + API 统计快照（供房间级聚合）。
// 2026-07-30 §统计增强。
func (a *Agent) AgentTokenStats() agentTokenStats {
	a.Lock()
	defer a.Unlock()
	return agentTokenStats{
		TotalInputTokens:  a.totalInputTokens,
		TotalOutputTokens: a.totalOutputTokens,
		TotalAPITokens:    a.totalAPITokens,
		APICallCount:      a.apiCallCount,
		APISuccessCount:   a.apiSuccessCount,
		APIFailCount:      a.apiFailCount,
	}
}

// agentTokenStats 是 Agent Token + API 统计的快照值对象（跨包传递用）。
// 2026-07-30 §统计增强。
type agentTokenStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalAPITokens    int
	APICallCount      int
	APISuccessCount   int
	APIFailCount      int
}

// AvgLLMLatencyMs 返回本 bot 当前模型 API 调用的滑动平均耗时(毫秒)。
// 2026-07-10 §120 公平性机制 — BuildUserPrompt 据此渲染【模型响应速率】块。
// 加锁读;若从未调过 LLM 返回 0。
func (a *Agent) AvgLLMLatencyMs() int64 {
	a.Lock()
	defer a.Unlock()
	return a.avgLLMLatencyMs
}

// LastLLMLatencyMs 返回本 bot 最近一次 LLM 调用耗时(毫秒)。
func (a *Agent) LastLLMLatencyMs() int64 {
	a.Lock()
	defer a.Unlock()
	return a.lastLLMLatencyMs
}

// TotalLLMCalls 返回本 bot 本局累计 LLM 调用次数。
func (a *Agent) TotalLLMCalls() int {
	a.Lock()
	defer a.Unlock()
	return a.totalLLMCalls
}

// MaxLLMRetries 返回外层 LLM 重试上限(由 cfg.Werewolf.LLMMaxRetries 注入,默认 5)。
// 2026-07-12 §127 增强;2026-07-15 R131 默认 7→5。
func (a *Agent) MaxLLMRetries() int {
	a.Lock()
	defer a.Unlock()
	return a.maxLLMRetries
}

// ClearLastFailureTimeForTest 清空 lastFailureTime,让下一次失败跨过 60s 冷却窗口
// 计入 consecutiveFailures。**仅供单测使用** (quarantine_round24_test.go 等需要
// 模拟 "持续 70s+ 故障" 场景的测试)。生产代码禁止调用。
// 2026-07-15 R131 修复: 永久错误也走冷却后,黑盒测试需要这条逃生口才能在合理
// 时间内累计到 permanentQuarantineThreshold=4。
func (a *Agent) ClearLastFailureTimeForTest() {
	a.Lock()
	defer a.Unlock()
	a.lastFailureTime = time.Time{}
}

// SetActiveStreamID / ActiveStreamID — §127 聊天 SSE 流式,每个 LLM 调用唯一 ID。
func (a *Agent) SetActiveStreamID(id string) {
	a.Lock()
	a.activeStreamID = id
	a.Unlock()
}
func (a *Agent) ActiveStreamID() string {
	a.Lock()
	defer a.Unlock()
	return a.activeStreamID
}

// OnLLMStreamStart / OnLLMStreamDelta / OnLLMStreamEnd — §127 聊天 SSE 流式回调接线。
// 只在 room 接线时调用一次;锁保护足够。
func (a *Agent) OnLLMStreamStart(fn func(string)) {
	a.Lock()
	a.onLLMStreamStart = fn
	a.Unlock()
}
func (a *Agent) OnLLMStreamDelta(fn func(string, string)) {
	a.Lock()
	a.onLLMStreamDelta = fn
	a.Unlock()
}
func (a *Agent) OnLLMStreamEnd(fn func(string, string)) {
	a.Lock()
	a.onLLMStreamEnd = fn
	a.Unlock()
}

// IsLLMCallInProgress returns (inProgress, startedAtUnixMilli) for the current
// LLM call state. Safe to call from the broadcast goroutine. When inProgress
// is false, startedAt still reflects the most recent call's start time.
func (a *Agent) IsLLMCallInProgress() (bool, int64) {
	a.Lock()
	defer a.Unlock()
	return a.llmCallInProgress, a.llmCallStartedAt.UnixMilli()
}

// 2026-07-10 §重构 — LLM 调用相位状态机 setter/getter。
// 5 态:idle / calling / streaming / retrying / quarantined。
// 全部走 a.mu,供 run.go 在主循环 6 个写点(safety-net / limiter /
// semaphore / MarkLLMCallStart / retry loop / MarkLLMCallEnd)调用。
//
// 设计原则:
//   - 单一 setter (SetLLMCallPhase) 集中切换 phase,便于审计/日志
//   - retryAttempt/retryMaxAttempts/nextRetryAtMs/lastErrorClass 各自独立
//     setter,因为它们的生命周期不同(phase 是瞬时切换,retry 状态只在 loop 内)
//   - ResetLLMCallPhase 用于"成功调用结束"或"quarantine"时整体清场

// SetLLMCallPhase 切换相位状态机。phase 必须是 5 态之一,非法值会被
// 视为 "idle"(并打 debug 日志)。受 a.mu 保护。
func (a *Agent) SetLLMCallPhase(phase string) {
	if phase != PhaseIdle && phase != PhaseCalling && phase != PhaseStreaming &&
		phase != PhaseRetrying && phase != PhaseQuarantined {
		logger.L().Debug("agent: ignoring invalid LLMCallPhase",
			zap.Int("seat", a.Seat), zap.String("phase", phase))
		return
	}
	a.Lock()
	a.llmCallPhase = phase
	a.Unlock()
}

// ResetLLMCallPhase 在成功调用结束或 quarantine 时调用,把相位+retry+nextRetryAt
// 全部清场(lastErrorClass 保留,供后续失败时复用)。
func (a *Agent) ResetLLMCallPhase(phase string) {
	a.Lock()
	a.llmCallPhase = phase
	a.retryAttempt = 0
	a.retryMaxAttempts = 0
	a.nextRetryAtMs = 0
	a.Unlock()
}

// SetRetryAttempt 记录当前 retry 轮次(1-based);maxAttempts 是上限,
// 供前端展示 N/M。仅在 retrying phase 内调用。
func (a *Agent) SetRetryAttempt(attempt int, maxAttempts int, nextRetryAtMs int64) {
	a.Lock()
	a.retryAttempt = attempt
	a.retryMaxAttempts = maxAttempts
	a.nextRetryAtMs = nextRetryAtMs
	a.Unlock()
}

// SetLastErrorClass 写入上次失败分类。
// class ∈ {"none", "5xx", "429", "timeout", "permanent", "queued", "throttled"}。
// 失败时调用;成功后由 ResetConsecutiveFailures 同时清零。
// §127 新增 queued/throttled:区分"等待 LLM 并发槽"与"被 LLMCallLimiter 限流"。
func (a *Agent) SetLastErrorClass(class string) {
	if class == "" {
		class = "none"
	}
	a.Lock()
	a.lastErrorClass = class
	a.Unlock()
}

// SetQueuedState 把 phase 切到 retrying 并标记 last_error_class=queued,
// 供 run.go 在 semaphore 等待失败 / LLMCallLimiter 限流时统一展示"排队中"。
func (a *Agent) SetQueuedState(reason string) {
	a.Lock()
	a.llmCallPhase = PhaseRetrying
	switch reason {
	case "semaphore":
		a.lastErrorClass = "queued"
	case "limiter":
		a.lastErrorClass = "throttled"
	default:
		a.lastErrorClass = "queued"
	}
	a.Unlock()
}

// LLMCallPhase 返回当前 phase(读字段,加锁)。
func (a *Agent) LLMCallPhase() string {
	a.Lock()
	defer a.Unlock()
	return a.llmCallPhase
}

// LLM call phase 5 态常量。前端 BotPhaseIndicator 据此渲染多态指示器;
// 后端 run.go 的 6 个写点统一使用这些常量,避免散落字符串。
// §128 对话即思考重构:常量名 wire 值不变(idle/calling/streaming/retrying/quarantined),
// 仅注释文案"思考中"改为"响应中";LLM 不在思考,而是在响应 — 真正的"思考"已发生在 API 内部。
const (
	PhaseIdle        = "idle"        // 未调 / 已完成 / 占位
	PhaseCalling     = "calling"     // HTTP 调用中(响应等待中),non-stream 整体或 stream 首 token 前
	PhaseStreaming   = "streaming"   // 流式首 token 到达(可选,目前 non-stream 退化为 calling)
	PhaseRetrying    = "retrying"    // retry loop 内,等待 backoff 后重试
	PhaseQuarantined = "quarantined" // 永久禁用,不再调 LLM
)

// SetChatQueue 注入 agentcore.ChatHistoryQueue 并记录容量上限。在 NewWithRoom 之后由
// WerewolfRoom.StartAgentsLocked 调用一次,设置后整个房间生命周期不变。
func (a *Agent) SetChatQueue(q *agentcore.ChatHistoryQueue) {
	a.chatQueue = q
	if q != nil {
		a.chatCap = q.CapBytes()
	}
}

// SetMemoryMD 注入持久化记忆(MEMORY.md)。2026-07-20 §131 新增。
// 在 NewWithRoom 之后由 StartAgentsLocked 同步调用一次(2s timeout DB 读,
// 失败仅 log 不阻塞启动);之后整个房间生命周期只读(run.go 注入路径)。
// 空串 = 不注入。
func (a *Agent) SetMemoryMD(md string) {
	a.MemoryMD = md
}

// SetMemoryInjectRunes 设置长期记忆注入的 rune 预算(0 = 用默认常量)。
//
// 2026-08-12 §20260812-04 U4 新增。由 StartAgentsLocked 在 SetMemoryMD 之后
// 按房间难度档位调用,接线 difficulty.MemoryInjectRunes(easy 1500 /
// normal 4000 / hard 6000)—— 该字段此前 4 处赋值 0 处读取。
func (a *Agent) SetMemoryInjectRunes(n int) {
	if n < 0 {
		n = 0
	}
	a.memoryInjectRunes = n
}

// SetDifficultyRoundCap 设置难度档位对内层循环轮次的收紧上限(0 = 不收紧)。
//
// 2026-08-13 §20260813-04 U3 新增。由 StartAgentsLocked 紧邻
// SetMemoryInjectRunes 调用 —— 两者同源于 difficulty.ProfileFor,
// 且都是「difficulty.go 4 处赋值 + agent 侧 0 处读取」的 §130 实例
// (MemoryInjectRunes 在 §20260812-04 U4 已修,本字段是漏掉的那一个)。
//
// 只收紧不放宽:见 maxInnerRoundsFor 的 cap < base 守卫。
func (a *Agent) SetDifficultyRoundCap(n int) {
	a.Lock()
	defer a.Unlock()
	if n < 0 {
		n = 0
	}
	a.difficultyRoundCap = n
}

// DifficultyRoundCap 返回难度轮次收紧上限(0 = 不收紧)。
func (a *Agent) DifficultyRoundCap() int {
	a.Lock()
	defer a.Unlock()
	return a.difficultyRoundCap
}

// SetWolfTeammateSeat 注入"开局互认狼队友"提示(2026-07-21 §5.2)。
// 在 NewWithRoom 之后由 StartAgentsLocked 同步调用一次:
//   - wolfTeammateSeat < 0  → no-op(等价于禁用本设计的注入路径)
//   - Faction != "wolf"     → no-op(本设计仅作用于狼人)
//   - Agent / Memory 为 nil → no-op(测试构造期防御)
//
// 替换 m.messages[0] 的 identity 文本但保留后续对话,避免 LLM 上下文污染。
// 锁安全:Memory.ReplaceIdentity 内部持 m.mu,本方法不持 r.mu;
// StartAgentsLocked 在持 r.mu 调用本方法是安全的(只动单条 user message,
// 无 LLM / DB / WS IO),符合 §92a 自死锁约束。
func (a *Agent) SetWolfTeammateSeat(wolfTeammateSeat int) {
	if a == nil || a.Memory == nil {
		return
	}
	if wolfTeammateSeat < 0 {
		return
	}
	if a.Faction != "wolf" {
		return
	}
	a.Memory.ReplaceIdentity(a.Role, a.Faction, a.Win, a.Seat, wolfTeammateSeat)
	// v4 §13.1：同步保存到 Agent 结构体,供 wolf_whisper 工具挂载判断。
	a.WolfTeammateSeat = wolfTeammateSeat
}

// recordSpeakDaytime 把当前时间戳记录进 60s 滑动窗口(speak / interject
// 成功后由 Run 调用)。窗口超过 60s 时自动重置。
func (a *Agent) recordSpeakDaytime(now time.Time) {
	a.speakCounter.mu.Lock()
	defer a.speakCounter.mu.Unlock()
	if a.speakCounter.windowStart.IsZero() || now.Sub(a.speakCounter.windowStart) > 60*time.Second {
		a.speakCounter.windowStart = now
		a.speakCounter.count = 0
	}
	a.speakCounter.count++
}

// allowSpeakDaytime 报告当前窗口是否允许再发一次,并返回当前累计次数。
//
// 返回 (allowed, currentCount):
//   - allowed = true → 当前可继续 speak(interject/whisper 走自己的 limiter)
//   - allowed = false → 已达上限;但 speak floor watchdog 在 ≤ 强制唤醒时
//     可以绕过此判断(speak_floor_tick 路径强制要求 LLM 调一次 speak)
//
// 调用方负责决定是否放行(speak_floor_tick 路径会忽略返回值强制调 LLM)。
func (a *Agent) allowSpeakDaytime(now time.Time) (allowed bool, currentCount int) {
	a.speakCounter.mu.Lock()
	defer a.speakCounter.mu.Unlock()
	if a.speakCounter.windowStart.IsZero() || now.Sub(a.speakCounter.windowStart) > 60*time.Second {
		// 窗口已过期 → 等价于"全新窗口,可发"
		return true, 0
	}
	return true, a.speakCounter.count // 总是 allowed=true;上限检查由 manager watchdog 处理
}

// NoteIfSpeaking 是 recordSpeakDaytime 的便捷包装,使用当前时间戳。
// BUG 2026-07-09: 遗言(last_words)视为一次公开发言,计入 speakCounter 滑动窗口,
// 避免 speak floor watchdog 在遗言阶段误 wake 正在发言的 bot。
func (a *Agent) NoteIfSpeaking() {
	a.recordSpeakDaytime(time.Now())
}

// AllowSpeakDaytimePublic 公开版本,供外部包(werewolf/speak_floor.go)调用,
// 避免反向依赖。语义与 allowSpeakDaytime 完全一致。
func (a *Agent) AllowSpeakDaytimePublic(now time.Time) (allowed bool, currentCount int) {
	return a.allowSpeakDaytime(now)
}

// snapshotSpeakCounter 返回当前窗口累计次数(用于 BotTranscript 展示)。
func (a *Agent) snapshotSpeakCounter() int {
	a.speakCounter.mu.Lock()
	defer a.speakCounter.mu.Unlock()
	if a.speakCounter.windowStart.IsZero() || time.Since(a.speakCounter.windowStart) > 60*time.Second {
		return 0
	}
	return a.speakCounter.count
}

// AllowInterject R76 P1-3 (2026-07-10): 检查单 bot 是否可以继续插话。
// 双层门:
//  1. InterjectLimiter 60s 最小间隔(> speak 45s,确保插话比正式发言慢)
//  2. 5 分钟滑动窗口 ≤ interjectMaxPer5Min(默认 4) 条/窗,防"程序化刷屏"
//
// 调用方在调 `interject` 工具前先 check;不通过时 runner 返回带 reason 的
// result(类似 BUG-R74-1 的 rate-limited 文案),LLM 在下一轮收敛。
func (a *Agent) AllowInterject(now time.Time) bool {
	if a.InterjectLimiter != nil && !a.InterjectLimiter.Allow() {
		return false
	}
	a.interjectMu.Lock()
	defer a.interjectMu.Unlock()
	if a.interjectWindowStart.IsZero() || now.Sub(a.interjectWindowStart) > 5*time.Minute {
		return true // 窗口已过期,等价全新窗口
	}
	return a.interjectWindowCount < interjectMaxPer5Min
}

// MarkInterject R76 P1-3 (2026-07-10): 配合 AllowInterject,登记一次插话。
func (a *Agent) MarkInterject(now time.Time) {
	if a.InterjectLimiter != nil {
		a.InterjectLimiter.Mark()
	}
	a.interjectMu.Lock()
	defer a.interjectMu.Unlock()
	if a.interjectWindowStart.IsZero() || now.Sub(a.interjectWindowStart) > 5*time.Minute {
		a.interjectWindowStart = now
		a.interjectWindowCount = 1
		return
	}
	a.interjectWindowCount++
}

// publishQuarantineTranscript writes a one-line BotTranscript marking the
// agent as permanently quarantined, so the spectator AgentInteractionPanel
// stops showing "blank" after the bot's last successful LLM response. Called
// from SetQuarantined in a goroutine — must not block the caller. Re-uses
// the lock pattern from recordTranscript to avoid races with concurrent
// recordTranscript calls (which would otherwise overwrite our note with a
// stale, un-quarantined snapshot). BUG-WEREWOLF-P1-NEW-46 (Round 39).
func (a *Agent) publishQuarantineTranscript() {
	a.Lock()
	if a.quarantined && a.lastTranscript != nil && !a.lastTranscript.Quarantined {
		// Refresh the existing snapshot in-place: copy so we don't mutate
		// a value the broadcast path may already be marshalling.
		updated := *a.lastTranscript
		updated.Quarantined = true
		updated.QuarantineReason = truncate(a.lastError, 200)
		updated.LastOutcome = "quarantine"
		updated.UpdatedAt = time.Now().Unix()
		a.lastTranscript = &updated
	}
	a.Unlock()
}

// sleepUntil lets the agent wait until `t` or ctx done.
func sleepUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// roundCtx builds an llm.Message list from the latest context + memory.
func (a *Agent) roundCtx(ctx wwtypes.GameContext) []llm.Message {
	msgs, _ := a.Memory.Snapshot()
	msgs = append(msgs, llm.Message{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: BuildUserPrompt(ctx)}},
	})
	return msgs
}

// recordAssistant pushes an assistant turn onto memory (text + tool_use).
func (a *Agent) recordAssistant(resp llm.LLMResponse) {
	a.Memory.Push(llm.Message{Role: "assistant", Content: resp.Content})
}

// recordToolResult pushes a tool_result message.
// R143 (2026-07-17) — 防御 Anthropic 400 "missing messages.content.text parameter":
// 当工具返回空字符串(LLM 误调未知工具如 "skip" → DispatchTool 返回 (空, err);
// 引擎 IdleSilent 在某些 runner 上返回 "" 等) 时,如果直接把 Text 留空,
// ContentBlock.MarshalJSON 在 text 块上 omitempty 会吞掉 text 字段,产出
// `{"type":"text"}` 的非法 wire 块,触发上游 doubao-seed-2.0-pro 等严格代理
// 400 拒绝,并连带污染下一轮整次 LLM 调用。这里统一兜底:content 为空时,
// 至少写一条占位文本,is_error=true 时优先用 "(tool error: <name>)" 形式。
func (a *Agent) recordToolResult(toolUseID, content string, isErr bool) {
	text := content
	if text == "" {
		if isErr {
			text = fmt.Sprintf("(tool returned no output; tool_use_id=%s)", toolUseID)
		} else {
			text = "(empty tool result)"
		}
	}
	a.Memory.Push(llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type:      "tool_result",
			ToolUseID: toolUseID,
			Content:   []llm.ContentBlock{{Type: "text", Text: text}},
			IsError:   isErr,
		}},
	})
}

// RecordToolResultForTest 暴露 recordToolResult 给测试桩使用(同包内可调用私有方法,
// 但 *_test 在 agent_test 包 — 所以这里提供一个薄包装,供外部测试驱动)。
func (a *Agent) RecordToolResultForTest(toolUseID, content string, isErr bool) {
	a.recordToolResult(toolUseID, content, isErr)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getModelContextBudget 根据模型名称返回建议的字节预算。
// 2026-08-10 §20260810-14 新增:按模型上下文窗口大小动态设置预算。
//
// 预算策略:按模型上下文窗口的 60% 设置(留 40% 给 system + tools + max_tokens 输出)。
// 实测数据:
//   - DouBao: ~128K-256K context → 400 "exceed max message tokens" 在 ~810KB 时触发
//   - Kimi: ~256K context → 类似限制
//   - DeepSeek: ~64K-128K context → 更紧凑
//
// 返回值单位:字节(bytes)。0 表示使用默认值(DefaultMaxPromptBytes)。
func getModelContextBudget(modelKey string) int {
	// 模型上下文窗口估算(基于实测数据和文档)
	// 这些值是保守估计,宁可压缩过早也不要溢出
	modelContextWindows := map[string]int{
		// DouBao: 实测 ~810KB 请求体触发 400,保守设 400KB 预算
		"DouBao-model": 400 * 1024,
		// Kimi: 类似 DouBao,保守设 400KB 预算
		"Kimi-model": 400 * 1024,
		// DeepSeek: 实测上下文窗口较小,保守设 300KB 预算
		"DeepSeek-model": 300 * 1024,
		// GLM: 类似 DeepSeek,保守设 300KB 预算
		"GLM-model": 300 * 1024,
		// MeiTuan: 较大上下文窗口,可设宽松
		"MeiTuan-model": 600 * 1024,
		// MinMax: 中等上下文窗口
		"MinMax-model": 500 * 1024,
		// Qwen: 较大上下文窗口
		"Qwen-model": 600 * 1024,
		// Xiaomi: 中等上下文窗口
		"Xiaomi-model": 500 * 1024,
	}

	if budget, ok := modelContextWindows[modelKey]; ok {
		return budget
	}
	// 未知模型使用默认值
	return DefaultMaxPromptBytes
}
