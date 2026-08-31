// Package debatecommentator — 辩论比赛「AI 实时解说」Agent (2026-08-31 §20260831-03)。
//
// 设计动机:为观战模式增加体育解说式体验。一个独立的 CommentaryAgent goroutine
// 挂在每个启用解说的房间,以**上帝视角**(知道全部发言 + 阶段 + 比分)实时生成
// 局势点评。与裁判的本质区别:
//   - 裁判:全房广播评分,影响比赛结果,落库 JudgeScore
//   - 解说:仅推送给观战者(spectator-only),不落库,不影响对局
//
// 与 §119 协议层隔离的关系:
//   内部独白/上帝视角输入走 CommentarySnapshot 快照(由 manager 锁内构造),
//   输出文本**绝不**进 chat_message 表 / chat_history 队列;
//   仅 spectator-only WS 帧 debate.commentary 下发。
//
// §130 自检:本包的 Provider 注入路径(startCommentatorGoroutine → SetProvider)
// 是 §130 要求的"真实生产注入点",Provider == nil 时主循环跳过,与法官 §130
// 教训同款兜底(失败静默,不计入 bot consecutiveFailures,§120 公平性)。
package debatecommentator

import (
	"context"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/llm"
)

// 事件触发 kind。
const (
	CommentaryPendingPhaseChange = "phase_change"
	CommentaryPendingSpeech      = "speech"
	CommentaryPendingCrossExam   = "cross_exam"
	CommentaryPendingJudgeScore  = "judge_score"
	CommentaryPendingGameOver    = "game_over"
)

// CommentaryEvent 事件结构体。
type CommentaryEvent struct {
	Kind  string         // 触发 kind
	Extra map[string]any // 事件附加数据
}

// CommentarySnapshot 解说快照(由 manager 锁内构造,出锁使用)。
// 关键纪律:快照只用于**构造 LLM 输入**,绝不参与任何玩家可见字段下发。
type CommentarySnapshot struct {
	RoomID    string
	Style     string // "pro" | "fun"
	ModelKey  string
	Phase     string
	PhaseCN   string
	Topic     string
	TeamCount int

	// 最近发言摘要(最近 5 条)
	RecentSpeeches []SpeechSummary

	// 当前比分(评审阶段后填充)
	TeamScores []TeamScoreSummary

	// 事件信息
	EventKind string
	Extra     map[string]any

	// 解说自身最近 5 条输出(去重用)
	History []string
}

// SpeechSummary 简化发言信息。
type SpeechSummary struct {
	SpeakerName string
	StanceLabel string
	RoleCN      string
	Content     string
	PhaseCN     string
}

// TeamScoreSummary 队伍得分摘要。
type TeamScoreSummary struct {
	TeamID     int
	TeamName   string
	TotalScore float64
}

// CommentatorAgent 解说主控结构。
type CommentatorAgent struct {
	RoomID   string
	Style    string
	ModelKey string

	mu          sync.Mutex
	events      chan CommentaryEvent
	limiter     *agentcore.SpeakLimiter
	consecutive int // 连续失败计数(自 quarantine 用)
	quarantined bool
	lastError   string

	Provider llm.LLMProvider
	apiKey   string
	Registry *llm.Registry

	// onBroadcast 由 manager 注入,接收 (roomID, text, style) 三元组,
	// manager 负责走 spectator-only 通道。
	onBroadcast func(roomID, text, style string)
	history     []string // 环形,最近 5 条输出,用于去重

	// 房间级 LLM 并发信号量(可选,nil = 不限流)。
	llmSema chan struct{}
}

// SetLLMSemaphore 安装房间级 LLM 并发闸门(nil = 不限流,向后兼容)。
func (c *CommentatorAgent) SetLLMSemaphore(sema chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.llmSema = sema
}

// acquireLLMSlot 尝试取得房间级 LLM 槽位,最多等待 wait。
func (c *CommentatorAgent) acquireLLMSlot(wait time.Duration) bool {
	c.mu.Lock()
	sema := c.llmSema
	c.mu.Unlock()

	if sema == nil {
		return true
	}
	if wait <= 0 {
		select {
		case sema <- struct{}{}:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case sema <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// releaseLLMSlot 归还一个槽位。只应在 acquireLLMSlot 返回 true 后 defer 调用。
func (c *CommentatorAgent) releaseLLMSlot() {
	c.mu.Lock()
	sema := c.llmSema
	c.mu.Unlock()

	if sema == nil {
		return
	}
	select {
	case <-sema:
	default:
	}
}

// 默认限流 45s(解说是锦上添花,不应刷屏)。
const defaultCommentaryInterval = 45 * time.Second

// commentarySlotAcquireWait 是解说等待房间级 LLM 槽位的上限。
const commentarySlotAcquireWait = 2 * time.Second

// 默认 LLM 调用的总预算秒数。
const defaultCommentaryBudgetSec = 240

// NewCommentatorAgent 构造一个 CommentatorAgent。
func NewCommentatorAgent(roomID, style, modelKey string) *CommentatorAgent {
	if style != "pro" && style != "fun" {
		style = "pro"
	}
	return &CommentatorAgent{
		RoomID:   roomID,
		Style:    style,
		ModelKey: modelKey,
		events:   make(chan CommentaryEvent, 32),
		limiter:  agentcore.NewSpeakLimiter(defaultCommentaryInterval),
	}
}

// SetProvider 注入 Provider(§130 自检要求),由 manager 在 goroutine 启动前调用。
func (c *CommentatorAgent) SetProvider(p llm.LLMProvider, apiKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Provider = p
	c.apiKey = apiKey
}

// SetRegistry 注入 Registry(用于查 ThinkingEnabled)。
func (c *CommentatorAgent) SetRegistry(r *llm.Registry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Registry = r
}

// SetOnBroadcast 注入广播回调。回调由 manager 提供,内部走 spectator-only 通道。
func (c *CommentatorAgent) SetOnBroadcast(cb func(roomID, text, style string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onBroadcast = cb
}

// PushEvent 非阻塞投递事件。channel 满时丢弃(解说丢帧无害,过期事件不补)。
func (c *CommentatorAgent) PushEvent(evt CommentaryEvent) {
	select {
	case c.events <- evt:
	default:
		// 静默丢弃,不影响 game state。
	}
}

// IsQuarantined 返回解说是否已被自我 quarantine(连续失败 ≥ 5 次)。
func (c *CommentatorAgent) IsQuarantined() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quarantined
}

// Run 主循环(由 manager 在 goroutine 内启动)。
func (c *CommentatorAgent) Run(ctx context.Context, snapProvider func() *CommentarySnapshot) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-c.events:
			if snapProvider == nil {
				continue
			}
			snap := snapProvider()
			if snap == nil {
				continue
			}
			snap.EventKind = evt.Kind
			if evt.Extra != nil {
				snap.Extra = evt.Extra
			}
			c.handleEvent(ctx, snap)
		}
	}
}

// handleEvent 是单事件处理入口:限流 → chatOrFallback → 失败计数 → 广播/去重。
func (c *CommentatorAgent) handleEvent(ctx context.Context, snap *CommentarySnapshot) {
	c.mu.Lock()
	if c.quarantined {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	if !c.limiter.Allow() {
		return
	}
	c.mu.Lock()
	if c.Provider == nil || c.apiKey == "" {
		c.mu.Unlock()
		return
	}
	style := c.Style
	modelKey := c.ModelKey
	provider := c.Provider
	apiKey := c.apiKey
	c.mu.Unlock()

	// 房间级 LLM 并发信号量。
	// 解说是「锦上添花,不应刷屏」调用,抢不到槽位就让位给推进游戏的 player bot。
	if !c.acquireLLMSlot(commentarySlotAcquireWait) {
		return
	}
	defer c.releaseLLMSlot()

	// §197 长预算:parentCtx = base + extended
	budget := time.Duration(defaultCommentaryBudgetSec) * time.Second
	parentCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	text, ok := c.chatOrFallback(parentCtx, provider, apiKey, modelKey, snap)
	if !ok {
		c.mu.Lock()
		c.consecutive++
		c.lastError = "chat_or_fallback_failed"
		if c.consecutive >= 5 {
			c.quarantined = true
		}
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	c.consecutive = 0
	c.mu.Unlock()

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// 自身去重:与上一条完全相同则丢弃
	c.mu.Lock()
	for _, prev := range c.history {
		if prev == text {
			c.mu.Unlock()
			return
		}
	}
	c.history = append(c.history, text)
	if len(c.history) > 5 {
		c.history = c.history[len(c.history)-5:]
	}
	cb := c.onBroadcast
	c.mu.Unlock()

	if cb != nil {
		cb(snap.RoomID, text, style)
	}
}

// chatOrFallback 调用 LLM 生成解说;失败返回 ok=false 让上层计数。
func (c *CommentatorAgent) chatOrFallback(
	ctx context.Context,
	provider llm.LLMProvider,
	apiKey, modelKey string,
	snap *CommentarySnapshot,
) (string, bool) {
	system := buildSystemPrompt(snap.Style)
	user := buildUserPrompt(snap)
	req := llm.LLMRequest{
		Model:          modelKey,
		System:         []llm.SystemBlock{{Type: "text", Text: system}},
		Messages:       []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: user}}}},
		MaxTokens:     300,
		AgentClassName: string(agentroot.AgentClassDebateCommentator),
	}
	resp, err := provider.Chat(ctx, apiKey, req)
	if err != nil {
		return "", false
	}
	txt := resp.Text()
	if strings.TrimSpace(txt) == "" {
		return "", false
	}
	return txt, true
}
