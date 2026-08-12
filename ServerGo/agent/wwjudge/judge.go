// Package agent — judge.go: Agent 法官(主持人)driver。
//
// 2026-07-10 §123 增强。法官不是身份牌,是身份牌之外的系统角色,由 LLM 驱动,
// 负责公开宣告、流程引导、阶段切换口播、死因宣告。详见:
//   - docs/狼人杀-重构方案/主持人Agent重构设计.md
//   - docs/狼人杀死亡语义设计.md
//
// 与 Agent(玩家 bot)的区别:
//   - 工具集分离(announce/prompt_actor/summary/declare_cause/idle_silent)
//   - 限流更严(30s/条 vs 玩家 45s),防刷屏
//   - 不参与投票 / 夜间行动 / 胜负
//   - 不影响 phase 状态(只有 watchdog + host driver 能切 phase)
//   - 死亡事件由 Player.DeathCause/DeathVerdict 派生,法官只宣告
package wwjudge

import (
	"context"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/core"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// JudgePendingAnnounceKind 法官事件类型常量(与 docs/狼人杀-重构方案/主持人Agent重构设计.md §2.3 一一对应)。
//
// 2026-07-16 主持人重构 — 取值改为 judge_ 前缀(对齐 docs/狼人杀-重构方案/主持人Agent重构设计.md §6.3
// 映射表),让前端/活动流能按前缀识别法官事件;旧 unprefixed 值不再使用。
// 秘密阶段(NightWolves/Seer/Witch)不单独成常量 — judgeKindForPhase 对它们返回空字符串。
const (
	JudgePendingFillingWelcome      = "judge_filling_welcome"
	JudgePendingPreWolves           = "judge_pre_wolves"
	JudgePendingDawnAnnounce        = "judge_dawn_announce"
	JudgePendingSheriffStart        = "judge_sheriff_start"
	JudgePendingSpeakStart          = "judge_speak_start"
	JudgePendingVoteStart           = "judge_vote_start"
	JudgePendingDeathAnnounce       = "judge_death_announce"
	JudgePendingSheriffStreamSettle = "judge_sheriff_stream_settle"
	JudgePendingIdiotReveal         = "judge_idiot_reveal"
	JudgePendingHunterShoot         = "judge_hunter_shoot"
	JudgePendingLastWords           = "judge_last_words"
	JudgePendingRestartVoteResult   = "judge_restart_vote_result"
	JudgePendingGameOver            = "judge_game_over"
	// JudgePendingGameOverSummary — 2026-07-10 §125 增强:触发「整局总结」生成。
	JudgePendingGameOverSummary = "game_over_summary"
)

// JudgeActivity 是法官「一举一动」活动流的单条记录(供前端 JudgePanel 活动时间线渲染)。
type JudgeActivity struct {
	At    int64  `json:"at"`    // 毫秒时间戳
	Tool  string `json:"tool"`  // 工具名(announce/prompt_actor/summary/declare_cause/idle_silent)
	Input string `json:"input"` // 参数摘要(≤120 字符)
	Out   string `json:"out"`   // 产物文本
	LLMMs int64  `json:"llm_ms,omitempty"` // 本次 LLM 耗时(可选)
}

// JudgeTranscript 法官的对外可见摘要(挂在 game.state.judge_context)。
// 前端 JudgePanel.tsx 据此渲染「法官宣告」面板。
type JudgeTranscript struct {
	Model              string    `json:"model"`
	LastAnnouncement   string    `json:"last_announcement,omitempty"`
	LastTool           string    `json:"last_tool,omitempty"`
	RecentAnnouncements []string `json:"recent_announcements,omitempty"`
	ToolCalls          []string  `json:"tool_calls,omitempty"`
	LastUpdatedAt      int64     `json:"last_updated_at"`
	Quarantined        bool      `json:"quarantined,omitempty"`
	QuarantineReason   string    `json:"quarantine_reason,omitempty"`
	// LastSummary 2026-07-10 §125 增强 — 最近一次「整局总结」(5 段)。
	LastSummary string `json:"last_summary,omitempty"`
	LastSummaryAt int64 `json:"last_summary_at,omitempty"`
	LastSummarySections SummarySectionsJSON `json:"last_summary_sections,omitempty"`
	// Activities 2026-07-16 主持人重构 —「一举一动」活动流(最近 30 条,队首淘汰)。
	Activities []JudgeActivity `json:"activities,omitempty"`
	// LastLLMMs 最近一次 LLM 耗时(毫秒)。
	LastLLMMs int64 `json:"last_llm_ms,omitempty"`
	// 2026-07-30 §统计增强 — Token + API 统计（纯内存态）。
	JudgeLastInputTokens  int `json:"judge_last_input_tokens,omitempty"`
	JudgeLastOutputTokens int `json:"judge_last_output_tokens,omitempty"`
	JudgeLastAPITokens    int `json:"judge_last_api_tokens,omitempty"`
	JudgeTotalInputTokens  int `json:"judge_total_input_tokens,omitempty"`
	JudgeTotalOutputTokens int `json:"judge_total_output_tokens,omitempty"`
	JudgeTotalAPITokens    int `json:"judge_total_api_tokens,omitempty"`
	JudgeAPICallCount      int `json:"judge_api_call_count,omitempty"`
	JudgeAPISuccessCount   int `json:"judge_api_success_count,omitempty"`
	JudgeAPIFailCount      int `json:"judge_api_fail_count,omitempty"`
}

// AgentJudge 是法官 bot 的 driver 实例。与玩家 Agent 同抽象级别,但工具集 / 限流 / 摘要独立。
type AgentJudge struct {
	RoomID   string
	ModelKey string

	// Transcript 对外可见摘要。setTranscript / JudgeTranscript() 加锁读写。
	mu          sync.Mutex
	transcript  JudgeTranscript

	// events 接收 phaseWatchdogTick 推送的事件(channel 由 room 持有,只写)。
	events chan JudgeEvent

	// announceLimiter 法官发言限流(默认 30s 间隔,防刷屏)。
	announceLimiter *agentcore.SpeakLimiter

	// 2026-07-10 §125 增强 — 整局总结限流(60s)。
	// 由 NewAgentJudge 初始化;handleEvent 对 JudgePendingGameOverSummary 走它。
	summaryLimiter *agentcore.SpeakLimiter

	// consecutiveFailures / quarantined 沿用玩家 Agent 的语义;
	// quarantine 后法官仅用 fallback 文本兜底,不调 LLM。
	consecutiveFailures int
	quarantined         bool
	lastError           string

	// §127 — 法官 LLM 调用所需的可选 provider;nil 时直接走 fallback。
	Provider llm.LLMProvider
	apiKey   string
	Registry *llm.Registry

	// 2026-07-30 §统计增强 — 法官 Token + API 统计（纯内存态）。
	judgeTotalInputTokens  int
	judgeTotalOutputTokens int
	judgeTotalAPITokens    int
	judgeAPICallCount      int
	judgeAPISuccessCount   int
	judgeAPIFailCount      int
	judgeLastInputTokens   int
	judgeLastOutputTokens  int
	judgeLastAPITokens     int

	// onAnnounce 法官宣告广播回调(announce/declare_cause 成功后调用)。
	// 由 startJudgeGoroutine 在 goroutine 启动前注入,goroutine 内只读。
	// 签名:(roomID, text, kind)。nil 时仅记 transcript 不广播。
	onAnnounce func(roomID, text, kind string)

	// §20260809-02 U1 法官多轮记忆 —— 环形缓冲,仅存法官自己 announce
	// 出去的公开文本(§119 协议层隔离),不存 GameSnapshot 全知视野。
	// 由 NewAgentJudge 初始化;每次 announce/summary 成功追加;
	// 构造 Messages 时 PrependHistory() 把最近 N 条作为 assistant 历史。
	Memory *JudgeMemoryRing
}

// NewAgentJudge 创建法官 driver(不启动 goroutine,调用方负责 Run)。
//
// 2026-07-12 §LLM-5min:announceLimiter 间隔 30s → 15s,允许法官在节奏较密的
// 阶段切换(黎明/警长/发言/投票等连续 hook)里每个间隔至少一次宣告;summary 限流
// 60s 不变(整局总结每局仅一次)。
func NewAgentJudge(roomID, modelKey string) *AgentJudge {
	return &AgentJudge{
		RoomID:          roomID,
		ModelKey:        modelKey,
		events:          make(chan JudgeEvent, 32),
		announceLimiter: agentcore.NewSpeakLimiter(15 * time.Second),
		summaryLimiter:  agentcore.NewSpeakLimiter(60 * time.Second),
		transcript:      JudgeTranscript{Model: modelKey, LastUpdatedAt: time.Now().UnixMilli()},
		// §20260809-02 U1:初始化 20 条环形缓冲(默认容量)。
		Memory: NewJudgeMemoryRing(20),
	}
}

// SetProvider 注入法官 LLM 调用所需的 provider + api_key。
// 必须在 goroutine 启动前调用(由 startJudgeGoroutine 注入);goroutine 内只读 j.Provider/j.apiKey。
// 封装赋值避免外部直接写字段。
func (j *AgentJudge) SetProvider(p llm.LLMProvider, key string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Provider = p
	j.apiKey = key
}

// SetOnAnnounceBroadcast 注入法官宣告广播回调(announce/declare_cause 成功后调用)。
// 必须在 goroutine 启动前调用(由 startJudgeGoroutine 注入);goroutine 内只读。
// 签名:(roomID, text, kind)。
func (j *AgentJudge) SetOnAnnounceBroadcast(fn func(roomID, text, kind string)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.onAnnounce = fn
}

// broadcastAnnounce 2026-08-05 §Agent聊天显示优化 (B6, 修复 P1-1) — 统一的
// 法官播报出口。此前只有 announce / declare_cause 两个工具会调 j.onAnnounce,
// 「prompt_actor / LLM 纯文本响应 / fallback 文案」三条路径只写 JudgeTranscript,
// 公屏上出现法官静默空窗。
//
// nil-guard 内建;空文本不广播。
//
// ⚠️ 调用约束:**不得**在持有 j.mu 时调用(recordAnnouncement / appendActivity
// 内部都取 j.mu,回调本身也可能取 room 侧的锁)。全部调用点都在这两者之后。
func (j *AgentJudge) broadcastAnnounce(text, kind string) {
	if text == "" {
		return
	}
	j.mu.Lock()
	cb := j.onAnnounce
	roomID := j.RoomID
	mem := j.Memory
	j.mu.Unlock()
	if cb == nil {
		return
	}
	cb(roomID, text, kind)
	// §20260809-02 U1:announce 广播成功后,把本次文本写入法官环形缓冲,
	// 供下一次 LLM 调用的 messages 历史。Memory nil-guard 兼容旧测试。
	if mem != nil {
		mem.Append(JudgeMemoryEntry{
			Round:    0, // Round 由调用方在 handleEvent 路径填(此处 0 表示未填)
			Phase:    "",
			WakeKind: kind,
			Text:     text,
		})
	}
}

// recordJudgeAPIStat 累加法官 Token + API 统计（内部持锁）。
// 2026-07-30 §统计增强。success=false 时仅累加调用次数 + 失败次数。
func (j *AgentJudge) recordJudgeAPIStat(usage llm.LLMUsage, success bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.judgeAPICallCount++
	if success {
		j.judgeAPISuccessCount++
		j.judgeLastInputTokens = usage.InputTokens
		j.judgeLastOutputTokens = usage.OutputTokens
		j.judgeLastAPITokens = usage.InputTokens + usage.OutputTokens
		j.judgeTotalInputTokens += usage.InputTokens
		j.judgeTotalOutputTokens += usage.OutputTokens
		j.judgeTotalAPITokens += j.judgeLastAPITokens
	} else {
		j.judgeAPIFailCount++
	}
}

// JudgeTokenStats 返回法官 Token + API 统计快照（供房间级聚合）。
// 2026-07-30 §统计增强。
func (j *AgentJudge) JudgeTokenStats() judgeTokenStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	return judgeTokenStats{
		TotalInputTokens:  j.judgeTotalInputTokens,
		TotalOutputTokens: j.judgeTotalOutputTokens,
		TotalAPITokens:    j.judgeTotalAPITokens,
		APICallCount:      j.judgeAPICallCount,
		APISuccessCount:   j.judgeAPISuccessCount,
		APIFailCount:      j.judgeAPIFailCount,
	}
}

// judgeTokenStats 是法官 Token + API 统计的快照值对象（跨包传递用）。
// 2026-07-30 §统计增强。
type judgeTokenStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalAPITokens    int
	APICallCount      int
	APISuccessCount   int
	APIFailCount      int
}

// appendActivity 追加一条「一举一动」到 transcript(超 30 条队首淘汰)。加锁写入。
func (j *AgentJudge) appendActivity(tool, input, out string, llmMs int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	act := JudgeActivity{
		At:    time.Now().UnixMilli(),
		Tool:  tool,
		Input: truncateJudgeInput(input),
		Out:   out,
		LLMMs: llmMs,
	}
	j.transcript.Activities = append(j.transcript.Activities, act)
	if len(j.transcript.Activities) > 30 {
		j.transcript.Activities = j.transcript.Activities[len(j.transcript.Activities)-30:]
	}
}

// truncateJudgeInput 把活动流输入摘要截到 ≤120 字符。
func truncateJudgeInput(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// JudgeEvent 法官事件(由 room 推入 events channel)。
// 简化设计:Kind 是事件类型字符串;其他字段为可选上下文。
type JudgeEvent struct {
	Kind  string
	Snap  *GameSnapshot
	Extra map[string]any
	At    time.Time
}

// GameSnapshot 是法官 LLM 调用所需的最小对局快照(避免与 GameState 循环依赖)。
//
// 2026-07-12 §127 增强 — 把"用户描述了哪些人活着/死了"扩展为"对局关键上下文",
// 让 LLM 在 system prompt 之外也能拿到关键事实,生成的宣告精准对位场上实际状态。
//
// 所有字段由 (r *WerewolfRoom).buildJudgeSnapshotLocked 一次性填好,以快照方式
// 跨 lock 边界传递给 judge goroutine,避免重复进入 room.mu 读活共享状态。
type GameSnapshot struct {
	Phase        string   // 当前 phase(如 "PhaseSpeak")
	Day          int      // 第几天(从 1 开始)
	AliveSeats   []int    // 存活座位(0-indexed)
	DeadSeats    []int    // 已死亡座位 + 该座位最近一轮的死亡原因(从 Players[].DeathCause 派生)
	SheriffSeat  int      // 当前警长座位(NoSeat = -1 = 无警长)
	WolfSeats    []int    // 狼人座位 — 法官可见(不能透露给玩家,但法官 prompt 需要)
	Votes        []string // 投票快照(每人投了谁 / 或弃权)
	SpeakOrder   []int    // 当前 speak 轮次的发言座位顺序
	LastDeathCause  string // 最近一次死亡原因(空字符串 = 无新死亡)
	LastDeathVerdict string // 最近一次死亡 verdict(execution/death/空)
	Winner       string   // 对局胜方(仅当 Status=over 时有意义;否则空)
	IsHumanInRoom bool    // 房间是否有真人玩家(影响 prompt 文案)
	PhaseDeadlineSec int  // 当前 phase 距离 deadline 还剩多少秒(由 room 端 timestamp 换算)
}

// Run 法官主循环:从 events channel 读事件 → 调 LLM → 执行工具 → 记录 transcript。
// LLM 失败 / 超时 / quarantine:用 JudgeFallbackText(kind) 兜底,通过 chat.SendFromJudge 广播。
func (j *AgentJudge) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-j.events:
			j.handleEvent(ctx, evt)
		}
	}
}

// judgeChatOrFallback 调 LLM 生成法官宣告,失败/无 provider 时返回 false 让调用方走 fallback。
//
// 2026-07-12 §127: 这是 §123 设计中"法官 LLM 化"的入口。流程:
//   1. 构造 system + user prompt + tools;
//   2. 调 Provider.Chat(含 90s 上下文超时,伴随后端 LLM 超时从 30s 提升到
//      5min 后法官也相应放宽,避免在 thinking 模型静默期被判 timeout);
//   3. 解析 tool_use block → DispatchJudgeTool 记录 transcript;
//   4. 失败 → 返回 false → handleEvent 走 JudgeFallbackText 兜底。
func (j *AgentJudge) judgeChatOrFallback(ctx context.Context, kind string, evt JudgeEvent) bool {
	if j.Provider == nil || j.apiKey == "" {
		return false
	}
	snap := GameSnapshot{Phase: kind, Day: 0}
	if evt.Snap != nil {
		snap = *evt.Snap
	}
	system := BuildJudgeSystemPrompt(kind, snap)
	user := BuildJudgeUserPrompt(kind, snap)
	// §20260809-02 U1:把法官自己历史 announce 的 assistant 消息前置到
	// user prompt 之前(Anthropic 协议要求 user/assistant 严格交替,
	// 最后一条必须是 user)。限制最近 8 条避免上下文膨胀;Provider.Chat
	// 内部走 SanitizeMessagesForAnthropic 做兜底合并(§14.1)。
	msgs := []llm.Message{}
	if history := j.Memory.Snapshot(); len(history) > 0 {
		start := 0
		if len(history) > 8 {
			start = len(history) - 8
		}
		for _, h := range history[start:] {
			msgs = append(msgs, llm.Message{
				Role:    "assistant",
				Content: []llm.ContentBlock{{Type: "text", Text: h.Text}},
			})
		}
	}
	// user 永远作为最后一条(Anthropic 协议强制)。
	msgs = append(msgs, llm.Message{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: user}},
	})

	req := llm.LLMRequest{
		Model:     resolveModelName(j.ModelKey),
		System:    system,
		Messages:  msgs,
		Tools:     BuildJudgeTools(),
		MaxTokens: 384,
		// 2026-08-06 §AgentClassName 增强:法官调用 LLM 时携带
		// AgentClassWerewolfJudge,让上游 / 网关区分玩家 Bot 与法官调用。
		// 常量集中在 ServerGo/agent/class_names.go。
		AgentClassName: string(agentroot.AgentClassWerewolfJudge),
		// 与普通 bot 协议对齐:每请求挂 ClaudeCode 形态的 metadata.user_id
		// (stringified JSON blob),设备/账户/会话三键。详见
		// buildJudgeMetadataUserID。
		Metadata: llm.Metadata{
			UserID: BuildJudgeMetadataUserID(j.RoomID, j.ModelKey),
		},
	}
	// BUG-R226-P1-02 (2026-08-01) — extended thinking 顶层字段注入入口(与
	// agent.callProvider 同形状)。Registry 由 startJudgeGoroutine 注入;
	// nil 时跳过(允许旧测试 / 无 registry 场景 fallback)。
	if j.Registry != nil {
		if enabled, budget := j.Registry.GetThinkingEnabled(j.ModelKey); enabled {
			req.Thinking = &llmtypes.ThinkingConfig{
				Type:         "enabled",
				BudgetTokens: budget,
			}
		}
	}
	// §127 + LLM-5min: 30s → 90s,允许 slow thinking 模型完成一次完整响应;
	// 超过 90s 仍 fallback,不阻塞游戏流(phase 由 watchdog 推动,与法官解耦)。
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	llmStart := time.Now()
	resp, err := j.Provider.Chat(cctx, j.apiKey, req)
	llmMs := time.Since(llmStart).Milliseconds()
	if err != nil {
		// 2026-07-30 §统计增强 — 失败单独计数（不累加 Token）。
		j.recordJudgeAPIStat(llmtypes.LLMUsage{}, false)
		logger.L().Warn("judge: LLM chat failed; using fallback",
			zap.String("room_id", j.RoomID), zap.String("kind", kind), zap.Error(err))
		return false
	}
	// 2026-07-30 §统计增强 — 成功累加 Token + 成功次数。
	j.recordJudgeAPIStat(resp.Usage, true)
	// 记录最近一次 LLM 耗时(供前端活动流展示)。
	j.mu.Lock()
	j.transcript.LastLLMMs = llmMs
	j.mu.Unlock()
	if len(resp.ToolUses()) == 0 {
		// 没有工具调用,直接把 response text 作为宣告记录。
		text := resp.Text()
		if text != "" {
			j.recordAnnouncement(kind, text, "llm_text")
			// 2026-08-05 §Agent聊天显示优化 (B6, 修复 P1-1):LLM 成功返回但没调
			// 任何工具时,这段文本就是法官本轮**唯一**的产出;此前只写
			// transcript,公屏完全看不到(且 judgeChatOrFallback 返回 true 还会
			// 抑制 fallback 广播 → 法官静默空窗)。补齐广播,与 announce 对称。
			j.broadcastAnnounce(text, kind)
		}
		return true
	}
	// 派发工具调用。
	j.mu.Lock()
	transcript := j.transcript
	j.mu.Unlock()
	for _, tu := range resp.ToolUses() {
		DispatchJudgeTool(tu.Name, tu.Input, j, &transcript)
	}
	j.mu.Lock()
	j.transcript = transcript
	j.transcript.LastUpdatedAt = time.Now().UnixMilli()
	j.mu.Unlock()
	return true
}

// handleEvent 单事件处理。LLM 路径失败时直接用 fallback 文本兜底,不阻塞游戏。
// 2026-07-12 §127: 真实 LLM 调用入口已加(judgeChatOrFallback);fallback 兜底保留。
func (j *AgentJudge) handleEvent(ctx context.Context, evt JudgeEvent) {
	kind := evt.Kind
	if kind == "" {
		return
	}
	// 2026-07-10 §125 增强 — 整局总结走独立路径,限流 60s。
	if kind == JudgePendingGameOverSummary {
		j.summaryLimiter.Allow() // 不严格阻塞(避免错过总结)
		j.handleGameOverSummaryInternal(evt)
		return
	}
	// 限流:同一 kind 30s 内不重复宣告。
	if !j.announceLimiter.Allow() {
		return
	}
	// quarantine 路径:用 fallback 文本兜底,不再调 LLM。
	// BUG-R213-P2-02 (2026-07-31): fallback 必须带 evt.Snap —— 法官 LLM 常因
	// 上游 400/超时熔断,此时玩家看到的**唯一**宣告就是 fallback 文本;若
	// fallback 不带快照,「第 N 天/死亡数/阶段语义」全部缺失,跑马灯会长
	// 时间停留在「首夜强制发言阶段」这类**陈旧**描述上(报告:自动化测试
	// 报告 2026-07-31 04:32:56 §5.3)。
	if j.quarantined {
		fallback := JudgeFallbackTextWithSnapshot(kind, judgeSnapshotOrEmpty(evt.Snap))
		if fallback != "" {
			j.recordAnnouncement(kind, fallback, "fallback_quarantined")
			j.appendActivity("fallback:"+kind, kind, fallback, 0)
			// 2026-08-05 §Agent聊天显示优化 (B6, 修复 P1-1):法官熔断期 fallback
			// 是玩家能看到的**唯一**宣告,必须进公屏(此前只写 transcript)。
			j.broadcastAnnounce(fallback, kind)
		}
		return
	}
	// 尝试调 LLM(若 Provider 已注入);失败/超时/无 provider → 走 fallback。
	if j.judgeChatOrFallback(ctx, kind, evt) {
		return
	}
	// BUG-R213-P2-02 (2026-07-31): 同上,fallback 带快照。
	fallback := JudgeFallbackTextWithSnapshot(kind, judgeSnapshotOrEmpty(evt.Snap))
	if fallback == "" {
		return
	}
	j.recordAnnouncement(kind, fallback, "fallback_initial")
	j.appendActivity("fallback:"+kind, kind, fallback, 0)
	// 2026-08-05 §Agent聊天显示优化 (B6, 修复 P1-1):LLM 调用失败后的 fallback
	// 同样进公屏,与 quarantined 分支对称。
	j.broadcastAnnounce(fallback, kind)
}

// judgeSnapshotOrEmpty 统一 nil-snap 兜底,避免两条 fallback 分支各写一遍。
func judgeSnapshotOrEmpty(snap *GameSnapshot) GameSnapshot {
	if snap == nil {
		return GameSnapshot{}
	}
	return *snap
}

// recordAnnouncement 写入最近宣告摘要。
func (j *AgentJudge) recordAnnouncement(kind, text, toolName string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.transcript.LastAnnouncement = text
	j.transcript.LastTool = toolName
	j.transcript.RecentAnnouncements = appendRecentString(j.transcript.RecentAnnouncements, text, 10)
	j.transcript.ToolCalls = appendRecentString(j.transcript.ToolCalls, toolName+": "+kind, 5)
	j.transcript.LastUpdatedAt = time.Now().UnixMilli()
}

// RecordAnnouncement 是 recordAnnouncement 的导出包装,供跨包测试
// (如 werewolf 包的终局法官语覆盖回归测试)注入一条历史宣告。
// 生产代码不得调用 —— 生产写入永远走 recordAnnouncement 内部路径。
func (j *AgentJudge) RecordAnnouncement(text string) {
	j.recordAnnouncement("test_inject", text, "test")
}

// Events 2026-07-10 §125 增强 — 返回内部 events channel 引用,
// 供 werewolf.room 在 NewAgentJudge 后绑定 + 投递事件。
func (j *AgentJudge) Events() chan JudgeEvent {
	return j.events
}

// SetEvents 注入 room 持有的 channel(供 room.go phaseWatchdogTick 写入)。
func (j *AgentJudge) SetEvents(ch chan JudgeEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = ch
}

// JudgeTranscript 获取当前法官摘要(加锁读)。
// 2026-07-30 §统计增强 — 透出 Token + API 统计字段。
func (j *AgentJudge) JudgeTranscript() JudgeTranscript {
	j.mu.Lock()
	defer j.mu.Unlock()
	t := j.transcript
	t.RecentAnnouncements = append([]string{}, j.transcript.RecentAnnouncements...)
	t.ToolCalls = append([]string{}, j.transcript.ToolCalls...)
	if j.transcript.Activities != nil {
		t.Activities = make([]JudgeActivity, len(j.transcript.Activities))
		copy(t.Activities, j.transcript.Activities)
	}
	t.JudgeLastInputTokens = j.judgeLastInputTokens
	t.JudgeLastOutputTokens = j.judgeLastOutputTokens
	t.JudgeLastAPITokens = j.judgeLastAPITokens
	t.JudgeTotalInputTokens = j.judgeTotalInputTokens
	t.JudgeTotalOutputTokens = j.judgeTotalOutputTokens
	t.JudgeTotalAPITokens = j.judgeTotalAPITokens
	t.JudgeAPICallCount = j.judgeAPICallCount
	t.JudgeAPISuccessCount = j.judgeAPISuccessCount
	t.JudgeAPIFailCount = j.judgeAPIFailCount
	return t
}

// SetQuarantined 设置法官 quarantine(LLM 永久禁用,改用 fallback 兜底)。
func (j *AgentJudge) SetQuarantined(reason string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.quarantined {
		return
	}
	j.quarantined = true
	j.lastError = reason
	j.transcript.Quarantined = true
	j.transcript.QuarantineReason = reason
}

// JudgeFallbackText 法官 fallback 文本(LLM 失败/超时/quarantine 兜底)。
// 简单文本,不依赖外部 i18n,前端 JudgePanel 可直接展示。
// 保留旧签名:仅给「只需要静态占位文案、没有 GameSnapshot 上下文」的调用方
// (如测试)使用;生产路径应调用 JudgeFallbackTextWithSnapshot。
func JudgeFallbackText(kind string) string {
	return JudgeFallbackTextWithSnapshot(kind, GameSnapshot{})
}

// JudgeFallbackTextWithSnapshot 是 JudgeFallbackText 的快照感知变体
// (BUG-R213-P2-02, 2026-07-31)。
//
// 背景:法官 LLM 常因上游 400/超时熔断,此时玩家看到的**唯一**宣告就是
// fallback 文本。旧实现对所有阶段只返回一句静态占位(如「进入白天发言
// 阶段,请依次发言。」),导致两个可观测缺陷:
//
//  1. 信息陈旧:fallback 文本不含「第 N 天/存活数/死亡数」,跑马灯长时间
//     停留在「首夜强制发言阶段」这类**与当前阶段不符**的描述上(自动化
//     测试报告 2026-07-31 04:32:56 §5.3);
//  2. 语义不清:玩家无法从 fallback 判断当前是第几天、死了几个人、是否
//     该进入下一阶段,只能等 LLM 恢复或 watchdog 推进。
//
// 修复:fallback 文本改为「阶段语义 + 当前对局事实」拼接,数据全部来自
// 服务端权威 GameSnapshot(AliveSeats/DeadSeats/Day),与 LLM 路径看到
// 的事实一致;快照为空时退化为旧静态文案(测试/兜底路径)。
//
// 所有文案仍 ≤ 80 字,保证前端跑马灯单行可渲染。
func JudgeFallbackTextWithSnapshot(kind string, snap GameSnapshot) string {
	// 阶段语义静态段(与旧 JudgeFallbackText 完全一致)。
	var base string
	switch kind {
	case JudgePendingFillingWelcome:
		base = "欢迎进入狼人杀对局。"
	case JudgePendingPreWolves:
		base = "首夜强制发言阶段:所有存活玩家依次发言。"
	case JudgePendingDawnAnnounce:
		base = "黎明已至,请查看昨夜伤亡。"
	case JudgePendingSheriffStart:
		base = "进入警长竞选阶段。"
	case JudgePendingSpeakStart:
		base = "进入白天发言阶段,请依次发言。"
	case JudgePendingVoteStart:
		base = "进入投票放逐阶段,请投票。"
	case JudgePendingDeathAnnounce:
		base = "有人死亡。"
	case JudgePendingSheriffStreamSettle:
		base = "警徽流结算完成。"
	case JudgePendingIdiotReveal:
		base = "白痴翻牌阶段。"
	case JudgePendingHunterShoot:
		base = "猎人开枪阶段。"
	case JudgePendingLastWords:
		base = "遗言阶段。"
	case JudgePendingRestartVoteResult:
		base = "重开局投票已结算。"
	case JudgePendingGameOver:
		base = "对局结束。"
	default:
		return ""
	}
	// 快照为空(测试/兜底路径)→ 返回旧静态文案,行为完全兼容。
	if snap.Day <= 0 && len(snap.AliveSeats) == 0 && len(snap.DeadSeats) == 0 {
		return base
	}
	// 拼接「第 N 天 · 存活 X / 死亡 Y」事实段,让 fallback 也具备阶段
	// 同步能力。白天阶段(day >= 1)前缀「第 N 天」;首夜(day == 0)
	// 不加,避免「第 0 天」这种不自然表述。
	facts := ""
	if snap.Day >= 1 {
		facts += "第 " + itoa(snap.Day) + " 天"
	}
	if len(snap.AliveSeats) > 0 || len(snap.DeadSeats) > 0 {
		if facts != "" {
			facts += " · "
		}
		facts += "存活 " + itoa(len(snap.AliveSeats)) + " / 死亡 " + itoa(len(snap.DeadSeats))
	}
	if facts == "" {
		return base
	}
	return base + "(" + facts + ")"
}

// appendRecentString 把 x 追加到 s 末尾,保留最近 max 个。
func appendRecentString(s []string, x string, max int) []string {
	s = append(s, x)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}
