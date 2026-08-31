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
	"sync"
	"time"

	"LsmAgentGame/agent/core"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
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

	// 2026-08-14 §20260814-01 U2 — 难度档位的「发言节奏」缩放系数。
	// 0 / 未注入 = 1.0（不缩放，逐字节零回归）。由 StartAgentsLocked 按
	// difficulty.SpeakLimiterScale 注入 —— 该配置自 §20260811-09 落地起
	// 4 处赋值 0 处读取，是本 struct 内第三个 §130 死字段
	// （memoryInjectRunes 见上，difficultyRoundCap 见 §20260813-04 U3）。
	// setter / clamp / limiter 重算逻辑在 difficulty_speak.go。
	difficultySpeakScale float64

	// v20260830-01：所有狼队友座位列表（非空表示本 bot 是狼人，可看到所有狼队友身份）。
	// 空切片 = 非狼人身份，不可见狼人阵营信息。
	// wolf_whisper 工具仅在 len(WolfTeammateSeats)>0 时挂载；wolfpack prompt
	// 仅在 len(WolfTeammateSeats)>0 时渲染，避免身份未确认的狼误用。
	WolfTeammateSeats []int

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

	// preflightOverflowCount 跟踪 pre-step 主动压缩失败累计(DSH §8.6 不变量)。
	// 每次成功 LLM 调用(resetPreflightOverflowCount)或成功主动压缩都清零。
	// 失败 +1,达到 preflightCompressMaxOverflow=2 后告警并继续 fallback。
	// 2026-08-13 §20260813-05 U3 — pre-step 主动压缩双触发。
	preflightOverflowCount int

	// systemPromptBytes 是构造期一次性计算 + freeze 的 system prompt 字节快照。
	// 2026-08-13 §20260813-05 U5 — 借鉴 dsh agent.ts:465-470 request bytes 稳定路线
	// 让 provider 自动 cache 命中。运行时仅比对 invariant I11,不发 HTTP 时复用。
	systemPromptBytes []byte

	// revealRoleOnDeath §20260830-01 §6.2 — 本局「死亡亮身份」开关(system
	// prompt §135 规则段双模式)。构造期默认 false(竞技规则文案),由
	// buildAgentContextLocked 在**每次 wake** 幂等同步(SetRevealRoleOnDeath),
	// 首次 wake 即切到本局模式并重算 systemPromptBytes → invariant I11 保持一致。
	// setter/getter 见 agent_reveal_20260830.go(§4 拆分,agent.go 已超限)。
	revealRoleOnDeath bool

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

// HypothesisEntryJSON 是对外暴露给前端的假说条目（避免循环导入 werewolf 包）。

// BotChatSender is the subset of ChatService the werewolf agentRunner needs to
// translate speak/whisper tool calls into real chat broadcasts. Declared here
// (in the leaf agent package) so both the werewolf engine and the ws.ChatService
// can agree on it without an import cycle (ws → werewolf → agent is OK; we must
// avoid werewolf → ws).
//
// The execution layer (ServerGo/game/werewolf) must pass something that
// implements these two methods. The agent.Run loop only inspects the error
// return — the success payload is ignored.

// IdleThinkRunner is the (optional) interface the ToolRunner must implement
// §128 对话即思考重构:IdleThinkRunner 已删除,与 idle_silent 合并。
// 玩家 / 法官均通过 IdleSilentRunner.IdleSilent(role, reason) 留 audit。
// 与 idle_silent 的区别:role 区分调用方(player=玩家,judge=法官);语义上仍是"沉默思考"
// (不广播、不发消息),只是工具名+role 字段让语义区分更精确。


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

	// 2026-08-13 §20260813-05 U5 — Provider Cache 字节稳定路线(DSH 启发)。
	// 构造期一次性计算 + 冻结 system prompt 字节,跨局不变 → provider
	// server-side KV cache 自动命中。runtime invariant I11 比对 req.System
	// 字节与本快照,任何漂移立即 Debug 日志 + 计数器。
	// §20260830-01:构造期按关闭模式(零值 false)冻结;若本局开启死亡亮身份,
	// SetRevealRoleOnDeath 在首次 wake 前同步并重算本快照(I11 一致)。
	a.systemPromptBytes = BuildSystemPromptBytes(a.SelfPortraitText, a.Personality, a.PersonalityPresetKey, a.DifficultyDirective, a.revealRoleOnDeath)

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




// publishQuarantineTranscript writes a one-line BotTranscript marking the
// agent as permanently quarantined, so the spectator AgentInteractionPanel
// stops showing "blank" after the bot's last successful LLM response. Called
// from SetQuarantined in a goroutine — must not block the caller. Re-uses
// the lock pattern from recordTranscript to avoid races with concurrent
// recordTranscript calls (which would otherwise overwrite our note with a
// stale, un-quarantined snapshot). BUG-WEREWOLF-P1-NEW-46 (Round 39).

// sleepUntil lets the agent wait until `t` or ctx done.

// roundCtx builds an llm.Message list from the latest context + memory.

// recordAssistant pushes an assistant turn onto memory (text + tool_use).

// recordToolResult pushes a tool_result message.
// R143 (2026-07-17) — 防御 Anthropic 400 "missing messages.content.text parameter":
// 当工具返回空字符串(LLM 误调未知工具如 "skip" → DispatchTool 返回 (空, err);
// 引擎 IdleSilent 在某些 runner 上返回 "" 等) 时,如果直接把 Text 留空,
// ContentBlock.MarshalJSON 在 text 块上 omitempty 会吞掉 text 字段,产出
// `{"type":"text"}` 的非法 wire 块,触发上游 doubao-seed-2.0-pro 等严格代理
// 400 拒绝,并连带污染下一轮整次 LLM 调用。这里统一兜底:content 为空时,
// 至少写一条占位文本,is_error=true 时优先用 "(tool error: <name>)" 形式。

// RecordToolResultForTest 暴露 recordToolResult 给测试桩使用(同包内可调用私有方法,
// 但 *_test 在 agent_test 包 — 所以这里提供一个薄包装,供外部测试驱动)。


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
