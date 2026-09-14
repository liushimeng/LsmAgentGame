// Package wealthplayer — agent.go: 财商流游戏玩家 Bot Agent 核心结构
// (2026-09-14 §财商流P0)。
//
// 设计原则(对齐 thpagent,§15/§128):
//   1. 一个 bot 座位 = 一个 Agent struct;月度事件驱动(每月 acting 一次决策)。
//   2. Agent 经 ToolRunner 接口调用引擎(in-process,不走 WS,与人类同一路径)。
//   3. AgentClassName = "LsmAgentGame-Wealth-Player"(class_names.go 登记)。
//   4. 每月 ≤ BotMaxActionsPerMonth(3)个动作工具 + ≤1 次 speak + submit_month。
package wealthplayer

import (
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	llmtypes "LsmAgentGame/llm/types"
)

// LLMRegistry 是 llm.Registry 的窄接口(避免直接 import llm 造成耦合;
// 测试可注入 fake)。
type LLMRegistry interface {
	Get(modelKey string) (llmtypes.LLMProvider, string, error)
	GetThinkingEnabled(modelKey string) (bool, int)
}

// Agent 是单个财商流 Bot。
type Agent struct {
	// 身份静态。
	RoomID   string
	GameKind string // 固定 "wealth"
	MySeat   int
	MyUserID string
	ModelKey string
	ModelName string

	// LLM 依赖(构造期经 BindRegistry 注入;Provider 每次 wake 现取,
	// registry reload 后自动生效)。
	registry LLMRegistry

	// runner:引擎桥(game/wealth/agent_runner.go 注入)。
	runner ToolRunner

	memory *Memory

	// speak 限流:每月 ≤1 次 + 相邻 ≥30s(§9;仅决策 goroutine 访问)。
	spokenThisMonth bool
	lastSpeakAt     time.Time

	maxActions      int
	decisionTimeout time.Duration

	// 思维可见性(game.state.bot_contexts;读侧锁)。
	mu         sync.Mutex
	transcript BotTranscript
	cancelled  bool
}

// BotTranscript 是 game.state.bot_contexts 单座位快照。
type BotTranscript struct {
	LastDecisionSummary string
	LastToolInput       string
	LastToolResult      string
	HeartThought        string
}

// Transcript 返回最近思维快照(线程安全)。
func (a *Agent) Transcript() BotTranscript {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.transcript
}

// NewAgent 构造 Bot。
func NewAgent(roomID, userID, modelKey, modelName string, seat int, maxActions int, decisionTimeout time.Duration) *Agent {
	if maxActions <= 0 {
		maxActions = 3
	}
	if decisionTimeout <= 0 {
		decisionTimeout = 20 * time.Second
	}
	return &Agent{
		RoomID:          roomID,
		GameKind:        "wealth",
		MySeat:          seat,
		MyUserID:        userID,
		ModelKey:        modelKey,
		ModelName:       modelName,
		memory:          NewMemory(),
		maxActions:      maxActions,
		decisionTimeout: decisionTimeout,
	}
}

// AgentClass 返回 AgentClassName(class_names.go 常量;§130 防散写字面量)。
func (a *Agent) AgentClass() agentroot.AgentClassName {
	return agentroot.AgentClassWealthPlayer
}

// BindRegistry 注入 LLM 注册表(Manager 构造后调用)。
func (a *Agent) BindRegistry(r LLMRegistry) {
	a.registry = r
}

// BindRunner 注入引擎桥(ToolRunner)。
func (a *Agent) BindRunner(runner ToolRunner) {
	a.runner = runner
}

// Memory 返回记忆对象(测试可见)。
func (a *Agent) Memory() *Memory { return a.memory }

// Cancel 标记取消(房间关闭时;进行中的决策经 ctx 尽快退出)。
func (a *Agent) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelled = true
}

// IsCancelled 报告是否已取消。
func (a *Agent) IsCancelled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelled
}

// allowSpeak 每月 ≤1 次 speak + 30s 令牌桶节流(§9)。
func (a *Agent) allowSpeak() bool {
	if !a.spokenThisMonth {
		// 月度配额仍可用;再检查 30s 节流(跨月边界自然满足)。
		if time.Since(a.lastSpeakAt) >= 30*time.Second {
			return true
		}
	}
	return false
}

func (a *Agent) markSpoken() {
	a.spokenThisMonth = true
	a.lastSpeakAt = time.Now()
}
