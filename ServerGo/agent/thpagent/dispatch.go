// Package thpagent — dispatch.go: 工具派发与状态机（2026-08-19）。
//
// 关键约束（沿用 CLAUDE.md §92a + §20260812-04）：
//   1. poker_action 每轮仅 1 次（强制, 第 2 次直接丢弃）
//   2. poker_chat 每手牌 ≤ 3 次 + 相邻 ≥ 20s（§3.2 放宽,原 2 次/30s 过紧）
//   3. 30s 决策超时服务端兜底 fold（与配置项 texasholdem.agent_action_timeout_sec 对齐）
package thpagent

import (
	"sync"
	"time"
)

// Action 是德州扑克动作（与 texasholdem.Action 平行, 避免循环 import）。
type Action struct {
	Type     string // "fold"/"check"/"call"/"bet"/"raise"/"allin"
	Amount   int    // bet/raise 目标金额
	Thought  string // 内心独白（协议层隔离,绝不入 chat 表 — §119）
	// ChatText 是 LLM 在同一响应里通过 poker_chat 工具给出的公屏发言。
	// 仅在通过 Dispatcher.DispatchPokerChat 限流 + 文本去重后填充;
	// 2026-08-20 §B5 之前 poker_chat 只拼进日志字符串,从未真正广播。
	ChatText string
}

// ActionType 枚举常量
const (
	ActFold  = "fold"
	ActCheck = "check"
	ActCall  = "call"
	ActBet   = "bet"
	ActRaise = "raise"
	ActAllIn = "allin"
)

// Dispatcher 是 Agent 的工具派发器（每 Agent 一个）。
type Dispatcher struct {
	mu sync.Mutex

	// poker_action 限流: 每轮 1 次
	currentRoundActionTaken bool
	currentRound            int

	// poker_chat 限流: token bucket
	chatCountThisHand  int
	chatLastTimestamp  time.Time
	maxChatPerHand     int
	minChatIntervalSec int

	// 决策超时
	actionTimeout time.Duration
}

// NewDispatcher 构造一个默认配置的派发器。
//
// 默认配置：
//   - 每轮 1 次 poker_action（强制）
//   - 每手牌 3 次 poker_chat（限流,§3.2 放宽:德扑一手多街,2 次过紧）
//   - 相邻 chat 间隔 ≥ 20s（§3.2:原 30s 过紧）
//   - 30s 决策超时
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		maxChatPerHand:     3,
		minChatIntervalSec: 20,
		actionTimeout:      30 * time.Second,
	}
}

// OnNewRound 重置轮次状态（每轮开始时调用）。
func (d *Dispatcher) OnNewRound() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentRoundActionTaken = false
	d.currentRound++
}

// OnNewHand 重置手牌状态（每手牌开始时调用）。
// 2026-08-20 P0-1: 同时重置 currentRoundActionTaken,否则跨手牌时
// poker_action 限流标志残留,所有 bot 首轮决策即被拒。
func (d *Dispatcher) OnNewHand() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentRoundActionTaken = false
	d.currentRound = 0
	d.chatCountThisHand = 0
	d.chatLastTimestamp = time.Time{}
}

// DispatchPokerAction 处理 poker_action 工具调用。
//
// 校验规则：
//   - 每轮 1 次：第 2 次直接拒绝 + 返回错误
//   - raise amount 必须 ≥ 0
//   - allin 时 amount 必须 > 0
//
// 返回 (action, error)。
func (d *Dispatcher) DispatchPokerAction(act Action) (Action, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.currentRoundActionTaken {
		// 第 2 次 poker_action — 拒绝
		return Action{}, ErrTooManyPokerActions
	}

	// 校验 action 类型
	switch act.Type {
	case ActFold, ActCheck, ActCall:
		// 无需 amount
	case ActBet, ActRaise:
		if act.Amount < 0 {
			return Action{}, ErrInvalidAmount
		}
	case ActAllIn:
		if act.Amount <= 0 {
			return Action{}, ErrInvalidAmount
		}
	default:
		return Action{}, ErrUnknownAction
	}

	d.currentRoundActionTaken = true
	return act, nil
}

// DispatchPokerChat 处理 poker_chat 工具调用。
//
// 校验规则：
//   - 每手牌 ≤ maxChatPerHand
//   - 相邻 ≥ minChatIntervalSec（除首条无限制）
func (d *Dispatcher) DispatchPokerChat(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.chatCountThisHand >= d.maxChatPerHand {
		return ErrTooManyChat
	}

	if d.chatCountThisHand > 0 {
		elapsed := time.Since(d.chatLastTimestamp)
		if elapsed < time.Duration(d.minChatIntervalSec)*time.Second {
			return ErrChatIntervalTooShort
		}
	}

	d.chatCountThisHand++
	d.chatLastTimestamp = time.Now()
	return nil
}

// IsPokerActionTaken 报告本轮是否已调用 poker_action。
func (d *Dispatcher) IsPokerActionTaken() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentRoundActionTaken
}

// ChatCountThisHand 返回本手牌已用聊天次数。
func (d *Dispatcher) ChatCountThisHand() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.chatCountThisHand
}

// 错误定义
var (
	ErrTooManyPokerActions = dispatchError("本轮已调用 poker_action, 每轮限 1 次")
	ErrInvalidAmount       = dispatchError("bet/raise/allin 必须填合法 amount")
	ErrUnknownAction       = dispatchError("未知 action 类型")
	ErrTooManyChat         = dispatchError("本手牌已用满聊天次数")
	ErrChatIntervalTooShort = dispatchError("聊天间隔过短, 需 ≥ 20s")
)

type dispatchError string

func (e dispatchError) Error() string { return string(e) }