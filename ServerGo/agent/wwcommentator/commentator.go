// Package wwcommentator — 狼人杀「AI 实时解说」Agent (2026-08-11 §20260811-09 U1)
//
// 设计动机:为观战模式增加体育解说式体验。一个独立的 CommentaryAgent goroutine
// 挂在每个启用解说的房间,以**上帝视角**(知道全部身份)实时生成局势点评。
// 与法官(judge)的本质区别:
//   - 法官:全房广播,落库 t_lsm_game_chat_message,喂 bot chatQueue → 玩家可见
//   - 解说:仅推送给观战者(spectator-only),不落库,不喂 bot,不影响对局
//
// 与 §119 协议层隔离的关系:
//   内部独白/上帝视角输入走 GameContext 快照(由 manager 锁内构造),
//   输出文本**绝不**进 chat_message 表 / chat_history 队列 / HeartThought;
//   仅 spectator-only WS 帧 chat.commentary + ClientGameState.CommentaryFeed
//   下发。与 wolfpack/ghost_voice 同款"协议层隔离"。
//
// §130 自检:本包的 Provider 注入路径(startCommentatorGoroutine → SetProvider)
// 是 §130 要求的"真实生产注入点",Provider == nil 时主循环跳过,与法官 §130
// 教训同款兜底(失败静默,不计入 bot consecutiveFailures,§120 公平性)。
package wwcommentator

import (
	"context"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	agentcore "LsmAgentGame/agent/core"
	"LsmAgentGame/llm"
)

// 事件触发 kind。复用与法官同构的事件常量,但只用于触发解说上下文,
// 不影响 game.state phase 推进。
const (
	CommentaryPendingPhaseChange   = "phase_change"
	CommentaryPendingVoteResult    = "vote_result"
	CommentaryPendingDeathAnnounce = "death_announce"
	CommentaryPendingSkillDramatic = "skill_dramatic"
	CommentaryPendingGameOver      = "game_over"
)

// 事件结构体。
type CommentaryEvent struct {
	Kind  string         // 触发 kind
	Extra map[string]any // 事件附加数据(如 death_seat / vote_result 等)
}

// 解说快照(由 manager 锁内构造,出锁使用)。
// 关键纪律:快照只用于**构造 LLM 输入**,绝不参与任何玩家可见字段下发。
type CommentarySnapshot struct {
	RoomID    string
	Style     string // "pro" | "fun"
	ModelKey  string
	Phase     string
	Round     int
	Day       int
	Alive     []int            // 存活玩家 1-indexed 座位号
	Roles     map[int]string   // 上帝视角真实身份(仅快照内)
	Factions  map[int]string
	RecentPub []string         // 最近 6 条公开发言摘要
	WolfVote  []string         // 最近一夜 wolfpack 协商摘要(已 §119 隔离过的副本)
	EventKind string
	Extra     map[string]any
	History   []string // 解说自身最近 5 条输出(去重用)
}

// 解说主控结构。
type CommentatorAgent struct {
	RoomID  string
	Style   string
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
	// manager 负责走 Hub.BroadcastRoomSpectators(玩家收不到) + 写
	// ClientGameState.CommentaryFeed(只对 viewer<0 渲染)。
	onBroadcast func(roomID, text, style string)
	history     []string // 环形,最近 5 条输出,用于去重

	// 2026-08-14 §20260814-01 U3 — 房间级 LLM 并发信号量(可选,nil = 不限流)。
	// 此前解说完全绕过 WerewolfRoom.llmSema,与法官同为「超配额」来源:
	// cap=4 的房间实际在飞可达 6(4 bot + 法官 + 解说)。
	// 解说是本文件自述的「锦上添花,不应刷屏」调用,故抢不到槽位即跳过本轮。
	llmSema chan struct{}
}

// SetLLMSemaphore 安装房间级 LLM 并发闸门(nil = 不限流,向后兼容)。
// 由 manager 在 goroutine 启动前注入;goroutine 内只读。
func (c *CommentatorAgent) SetLLMSemaphore(sema chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.llmSema = sema
}

// acquireLLMSlot 尝试取得房间级 LLM 槽位,最多等待 wait。
// 语义与 wwjudge.AcquireLLMSlot 完全一致(见 wwjudge/judge_llm_slot.go 文件头)。
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

// 默认限流 45s(比法官 15s 更克制,解说是锦上添花,不应刷屏)。
const defaultCommentaryInterval = 45 * time.Second

// commentarySlotAcquireWait 是解说等待房间级 LLM 槽位的上限
// (2026-08-14 §20260814-01 U3)。
//
// 取 2s,与 wwjudge.JudgeSlotAcquireWait 一致 —— 二者同为「不推进 phase」的
// 旁路装饰性调用,应当给 player bot 的 5s 预算(run.go:346)让路。
// 不直接 import wwjudge 的常量:两包平级且语义独立(将来若要给解说更短的
// 预算,不应牵动法官),此处以注释锚定对齐关系。
const commentarySlotAcquireWait = 2 * time.Second

// 默认 LLM 调用的总预算秒数(基础 + 流式续命)。慢模型可达 15 分钟。
// 复用 §197 defaultStreamExtendedTimeoutSec=900 的精神,这里给解说
// 一个独立的合理上限(避免解说拖垮 bot 主线)。
const defaultCommentaryBudgetSec = 240

// NewCommentatorAgent 构造一个 CommentatorAgent。
// events channel 缓冲 32(与法官同款);限流器 45s。
func NewCommentatorAgent(roomID, style, modelKey string) *CommentatorAgent {
	if style != "pro" && style != "fun" {
		style = "pro"
	}
	return &CommentatorAgent{
		RoomID:  roomID,
		Style:   style,
		ModelKey: modelKey,
		events:  make(chan CommentaryEvent, 32),
		limiter: agentcore.NewSpeakLimiter(defaultCommentaryInterval),
	}
}

// SetProvider 注入 Provider(§130 自检要求),由 manager 在
// startCommentatorGoroutine 时调用。
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
// 由 manager 用来在投票/开局等关键时机跳过解说。
func (c *CommentatorAgent) IsQuarantined() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quarantined
}

// Run 主循环(由 manager 在 goroutine 内启动)。
// 监听 ctx.Done() 优雅退出;select 默认分支 nil-guard 处理 Provider 未注入。
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
// 设计文档 §U1.2.1:失败静默,连续 5 次失败自我 quarantine,
// 与 §120 公平性教训对齐(不影响 bot consecutiveFailures)。
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

	// 2026-08-14 §20260814-01 U3 — 房间级 LLM 并发信号量。
	//
	// 解说是本文件自述的「锦上添花,不应刷屏」调用,抢不到槽位就让位给推进
	// 游戏的 player bot(2s 预算,与法官 wwjudge.JudgeSlotAcquireWait 一致)。
	//
	// ⚠️ 必须在 chatOrFallback **之外**获取,且失败时直接 return ——
	// 不能复用 `ok=false` 那条路径,因为它会 `c.consecutive++`,连续 5 次
	// 就 quarantine。槽位繁忙是**瞬态资源竞争,不是解说自身故障**;
    // 在 13 人局高峰期 5 次抢不到槽位轻易发生,会把解说永久打死。
	// 这与 §112「speak_floor 失败不计入 consecutiveFailures,否则误 quarantine」
	// 及 §20260812-04 U5「endpoint breaker 必须列为 transient」是同一条教训。
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
// 设计文档 §U1.4 解说 prompt 设计。
func (c *CommentatorAgent) chatOrFallback(
	ctx context.Context,
	provider llm.LLMProvider,
	apiKey, modelKey string,
	snap *CommentarySnapshot,
) (string, bool) {
	system := buildSystemPrompt(snap.Style)
	user := buildUserPrompt(snap)
	req := llm.LLMRequest{
		Model:         modelKey,
		System:        []llm.SystemBlock{{Type: "text", Text: system}},
		Messages:      []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: user}}}},
		MaxTokens:     300,
		AgentClassName: string(agentroot.AgentClassWerewolfCommentator),
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