// Package wwplayer — agent_transcript.go: BotTranscript 结构体及相关决策快照方法。
// 从 agent.go 拆分出来,单文件 ≤ 1800 行硬约束(CLAUDE.md §4)。
package wwplayer

import (
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
	"LsmAgentGame/llm/anthropic"
)

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
func (a *Agent) roundCtx(ctx wwtypes.GameContext) []llm.Message {
	msgs, _ := a.Memory.Snapshot()
	msgs = append(msgs, llm.Message{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: BuildUserPrompt(ctx)}},
	})
	return msgs
}
func (a *Agent) recordAssistant(resp llm.LLMResponse) {
	a.Memory.Push(llm.Message{Role: "assistant", Content: resp.Content})
}
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
func (a *Agent) RecordToolResultForTest(toolUseID, content string, isErr bool) {
	a.recordToolResult(toolUseID, content, isErr)
}
