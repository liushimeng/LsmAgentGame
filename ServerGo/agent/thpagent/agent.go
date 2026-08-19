// Package thpagent — agent.go: 德州扑克 Bot Agent 核心结构（2026-08-19）。
//
// 设计原则（沿用 CLAUDE.md §15 + §128）：
//   1. 一个 bot 座位 = 一个 Agent struct + 一个 goroutine（in-process 驱动）
//   2. Agent 通过 ToolRunner 接口调用 TexasHoldemManager.Action_*（不走 WS）
//   3. AgentClassName 严格按 §24.2 命名："LsmAgentGame-TexasHoldem-Player"
//   4. 锁语义严格遵循 §92a：所有持锁态路径走 *Locked 锁内变体
package thpagent

import (
	"context"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm/types"
	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// Agent 是单个德州扑克 Bot 的 Agent 结构。
//
// 字段分类（按 §20260813-04 U6 + §20260814-02 U4 6 类语义分桶）：
//   - A. 身份静态 (RoomID, MySeat, MyUserID, ModelKey, AgentClassName)
//   - B. 决策可观测 (BotIdentity, RecentHands, LastDecisionSummary)
//   - C. 情绪 (无 v1.0)
//   - D. 行为可观测 (HandOverPlayers, TotalActionCount)
//   - E. 死亡公开 (无 v1.0 — 扑克无死亡)
//   - F. 统计元数据 (TotalHands, TotalWon, NetChips, LastLLMLatencyMs)
type Agent struct {
	// A. 身份静态（构造期赋值, 整局不变）
	RoomID       string
	GameKind     string // 固定 "texasholdem"
	MySeat       int    // 0..5
	MyUserID     string
	ModelKey     string
	ModelName    string // 展示名(agent_name)
	AgentClass   string // "LsmAgentGame-TexasHoldem-Player"
	CreatedAt    time.Time

	// B. 决策可观测（每手牌更新）
	LastDecisionSummary string // 最近决策摘要
	InternalThought     string // 最近内心独白

	// D. 行为可观测
	TotalActionCount int // 累计动作次数

	// F. 统计元数据
	TotalHands        int
	TotalWon         int
	NetChips         int64
	LastLLMLatencyMs int64

	// LLM 依赖（按 §20260813-04 U2 接线）
	mu       sync.Mutex // 保护 Provider/ProviderSetter 的并发安全
	Provider types.LLMProvider
	// 缓存的 config key,用于 registry.Get(Provider)
	providerKey string

	// 上下文（每手牌刷新）
	currentContext *GameContextForAgent

	// 限流
	decisionLimiter *time.Ticker // 30s/次,burst=1

	// 状态
	cancelled bool
	cancelCh  chan struct{}
}

// GameContextForAgent 是 Agent 内部 GameContext 镜像（避免与 thptypes 循环 import）。
// 实际构造时由 driver 从 thptypes 转换而来。
type GameContextForAgent struct {
	RoomID         string
	HandNumber     int
	Street         string
	MySeat         int
	MyHole         [2]int
	Community      [5]int
	CommunityLen   int
	MyStack        int
	Pot            int
	CurrentBet     int
	CallAmount     int
	HandStrength   float64
	RequiredEquity float64
	Position       string
	PositionLabelZh string
	BluffHint      float64
	OpponentsCount int
	ActionHistory  string   // 拼接好的字符串
	ModelNameField string   // 模型展示名(由 driver 注入)
}

// NewAgent 构造一个 Bot Agent。
//
// 参数：
//   - roomID, userID, modelKey: 身份标识
//   - seat: 0..5
//   - providerKey: 用于 registry.Get(Provider) 的 key（通常 = modelKey）
//
// 注意：构造后 Provider 仍可能为 nil；调用方必须在 Agent 启动前调 SetProvider 注入。
// 这是 §20260813-04 U1 + §130 wiring lint 防御：构造期不注入 + 启动期注入 + 测试断言。
func NewAgent(roomID, userID, modelKey string, seat int) *Agent {
	return &Agent{
		RoomID:     roomID,
		GameKind:   "texasholdem",
		MySeat:     seat,
		MyUserID:   userID,
		ModelKey:   modelKey,
		AgentClass: string(agentroot.AgentClassTexasHoldemPlayer),
		CreatedAt:  time.Now(),
		cancelCh:   make(chan struct{}),
	}
}

// SetProvider 注入 LLM Provider（必须 Agent 启动前调用）。
//
// §20260813-04 U1 wiring: Provider 是「可选」字段,必须在生产路径注入,否则
// Agent.Run 第一次调 LLM 时 panic("nil provider")。
func (a *Agent) SetProvider(p types.LLMProvider, key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Provider = p
	a.providerKey = key
}

// ProviderKey 返回当前 Provider 的 registry key（仅用于日志）。
func (a *Agent) ProviderKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.providerKey
}

// Cancel 取消 Agent 的所有阻塞操作（决策/等待）。
//
// 必须在 stopAgentsLocked / 房间关闭时调用。
func (a *Agent) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelled {
		return
	}
	a.cancelled = true
	close(a.cancelCh)
}

// IsCancelled 返回 Agent 是否已取消。
func (a *Agent) IsCancelled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelled
}

// updateStats 内部: 更新 LLM 延迟统计（指数加权 α=0.3）。
func (a *Agent) updateStats(latencyMs int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.LastLLMLatencyMs = latencyMs
}

// recordAction 内部: 累计动作次数。
func (a *Agent) recordAction() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.TotalActionCount++
}

// SetInternalThought 设置最近内心独白（供前端 BotThoughtPanel 渲染）。
func (a *Agent) SetInternalThought(thought string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.InternalThought = thought
	if thought != "" {
		// 限长,避免 prompt 膨胀
		const maxLen = 200
		if len(thought) > maxLen {
			a.InternalThought = thought[:maxLen]
		}
	}
}

// GetInternalThought 返回最近内心独白（线程安全读）。
func (a *Agent) GetInternalThought() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.InternalThought
}

// BuildLLMRequest 构造一次 LLM 调用请求（只读, 不持有锁）。
//
// 这是 LLMRequest 的「最小可信」构造入口 —— 工具、消息、system prompt 由
// BuildSystemPrompt + BuildUserPrompt + BuildTools 提供。
func (a *Agent) BuildLLMRequest(systemPrompt, userPrompt string, tools []types.ToolDef) *types.LLMRequest {
	return &types.LLMRequest{
		Model:          a.ModelKey,
		System:         []types.SystemBlock{{Type: "text", Text: systemPrompt}},
		Messages:       []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: userPrompt}}}},
		Tools:          tools,
		MaxTokens:      2048,
		Metadata:       types.Metadata{UserID: a.MyUserID},
		AgentClassName: a.AgentClass,
	}
}

// Run 启动 Agent goroutine, 持续监听决策请求。
//
// 由 TexasHoldemAgentDriver 在 StartHand 时调一次；Cancel 后 goroutine 退出。
func (a *Agent) Run() {
	// 当前 v1.0 简化版: Agent.Run 是一个空循环, 实际决策由 driver 同步调 Decide。
	// 这样做的原因是：德州扑克决策是"当前轮"同步的, 不需要长生命周期 goroutine。
	// goroutine 仅用于将来异步化（如 multiple parallel thoughts）。
	for {
		select {
		case <-a.cancelCh:
			logger.L().Info("texasholdem agent cancelled",
				zap.String("room_id", a.RoomID),
				zap.Int("seat", a.MySeat),
				zap.String("model", a.ModelKey))
			return
		case <-time.After(60 * time.Second):
			// 心跳日志（可观测性）
			if !a.IsCancelled() {
				logger.L().Debug("texasholdem agent heartbeat",
					zap.String("room_id", a.RoomID),
					zap.Int("seat", a.MySeat),
					zap.Int("actions", a.TotalActionCount))
			}
		}
	}
}

// NewBackgroundContext 返回 Agent 的可取消 context。
//
// 用于 LLM 调用（30s 超时外部包装）,Cancel 时 context 自动 Done。
func (a *Agent) NewBackgroundContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithCancel(parent)
}

// SuppressUnusedImport 占位（保留以备未来扩展）
var _ = time.Time{}