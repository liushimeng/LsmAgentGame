// Package thptypes — 德州扑克玩家 Agent 的 GameContext 契约类型（2026-08-19）。
//
// 本包与 ServerGo/agent/wwtypes/ 同源设计：
//   - thptypes 仅 import agentcore（通用基础设施）
//   - 不得 import game/texasholdem 或 agent/thpagent（避免循环 import）
//
// GameContext 是 thpagent（玩家 Bot）与 game/texasholdem（引擎）共享的核心契约。
// 引擎侧在持锁态构造快照填充 GameContext，玩家 Agent 消费 GameContext 做决策。
//
// 与狼人杀 GameContext 的关键差异：
//   - 无 Phase 字段（德州扑克每轮独立决策，无长阶段）
//   - 无 Role/Faction 字段（德州扑克所有玩家对称，无阵营/角色概念）
//   - 新增 HandStrength / PotOdds / Position 等数学信号（服务端纯函数计算）
//   - 新增 MyHole（自己的底牌）+ CommunityCards（已亮公共牌）
package thptypes

import (
	agentcore "LsmAgentGame/agent/core"
)

// GameContext 是德州扑克 Bot 决策所需的全部上下文。
//
// 生命周期：每手牌 StartHand() 触发一次完整重建；每轮 ApplyAction() 触发一次局部更新。
// 引擎侧在持锁态构造快照（避免锁内回调），Agent 侧只读消费。
type GameContext struct {
	// RoomID / GameKind / MySeat / MyUserID — 身份元数据
	RoomID   string
	GameKind string // 固定 "texasholdem"
	MySeat   int    // 0..5；-1 = 未入座
	MyUserID string
	ModelKey string // LLM 模型标识

	// HandNumber / Street / Button — 当前手牌状态
	HandNumber int
	Street     string // "preflop" | "flop" | "turn" | "river" | "showdown" | "over"
	ButtonSeat int    // 庄家座位 -1 = 未定

	// MyHole / CommunityCards — 牌面
	MyHole       [2]int // 我的底牌（rank*4+suit，与 texasholdem.Card 编码一致）
	CommunityLen int    // 已亮公共牌数 0..5
	Community    [5]int // 已亮公共牌；未亮部分用 0 占位

	// Stack / Pot / CurrentBet — 筹码状态
	MyStack         int
	MyRoundCommitted int
	MyTotalCommitted int
	Pot             int
	CurrentBet      int
	BigBlind        int

	// PotOdds / RequiredEquity — 服务端纯函数计算（详见 decision.go）
	CallAmount      int     // 跟注所需金额
	PotOdds         float64 // 0.0-1.0
	RequiredEquity  float64 // 跟注所需最低胜率
	HandStrength    float64 // 0.0-1.0（蒙特卡洛 1000 次）
	Position        string  // "BTN" | "SB" | "BB" | "UTG" | "MP" | "CO"
	PositionLabelZh string  // 中文位置标签

	// Opponents — 对手信息（不含自己）
	Opponents []OpponentBrief

	// ActionHistory — 本手牌所有动作（按时间顺序）
	ActionHistory []ActionRecord

	// ChatHistory — 房间聊天历史（沿用 agentcore.ChatMessage 共享类型）
	ChatHistory []agentcore.ChatMessage

	// BotIdentity — 自己信息
	BotIdentity BotIdentityBrief

	// RecentHands — 最近 5 手牌回顾（v1.0 简化版，从 r.State.PlayHistory 取）
	RecentHands []HandRecord

	// HandOverPlayers — 上手牌各玩家净盈亏（用于 Profile 统计）
	HandOverPlayers []PlayerNetChip

	// EconTier — 当前房间经济档位（影响抽水率）
	EconTier string // "health" | "caution" | "danger"

	// MyTurn — 是否轮到我行动
	MyTurn bool
	// TimeRemainingSec — 决策剩余时间（秒）
	TimeRemainingSec int
}

// OpponentBrief 是单个对手的简要信息。
type OpponentBrief struct {
	Seat       int
	UserID     string
	Stack      int
	Folded     bool
	AllIn      bool
	IsBot      bool
	ModelKey   string // 空 = 人类
	ModelName  string // 展示名（agent_name）
}

// BotIdentityBrief 是 Bot 自己的身份信息。
type BotIdentityBrief struct {
	UserID        string
	ModelKey      string
	ModelName     string
	AgentClass    string // "LsmAgentGame-TexasHoldem-Player"
	TotalHands    int    // 累计参与手牌数
	TotalWon      int    // 累计胜出手牌数
	NetChips      int    // 累计净盈亏
	AvgPayout    int    // 平均净盈亏
	CurrentRoomID string
}

// ActionRecord 是单个动作记录（v1.0 简化版）。
type ActionRecord struct {
	Seat       int
	ActionType string // "fold"/"check"/"call"/"bet"/"raise"/"allin"
	Amount     int
	Pot        int // 动作后底池
	Street     string
}

// HandRecord 是历史手牌回顾。
type HandRecord struct {
	HandNumber  int
	MySeat      int
	MyHole      [2]int
	Community   [5]int
	CommunityLen int
	Winners     []int
	NetChipDelta int // 净盈亏
}

// PlayerNetChip 是上手牌各玩家的净盈亏（用于 Profile 统计）。
type PlayerNetChip struct {
	Seat      int
	UserID    string
	NetDelta  int
	IsWinner  bool
}