// Package vcplayer — agent.go: 虚拟城市玩家 Bot Agent 核心结构
// (2026-09-14 §财商流P0)。
//
// 设计原则(对齐 thpagent,§15/§128):
//  1. 一个 bot 座位 = 一个 Agent struct;月度事件驱动(每月 acting 一次决策)。
//  2. Agent 经 ToolRunner 接口调用引擎(in-process,不走 WS,与人类同一路径)。
//  3. AgentClassName = "LsmAgentGame-City-Human"(class_names.go 登记;
//     2026-09-22 §CityHuman重构: 五类 AgentClass 合一,City-Human 即城市居民)。
//  4. 每月 ≤ BotMaxActionsPerMonth(3)个动作工具 + ≤1 次 speak + submit_month。
package vcplayer

import (
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// LLMRegistry 是 llm.Registry 的窄接口(避免直接 import llm 造成耦合;
// 测试可注入 fake)。
type LLMRegistry interface {
	Get(modelKey string) (llmtypes.LLMProvider, string, error)
	GetThinkingEnabled(modelKey string) (bool, int)
}

// Agent 是单个虚拟城市 Bot。
type Agent struct {
	// 身份静态。
	RoomID    string
	GameKind  string // 固定 "virtual_city"
	MySeat    int
	MyUserID  string
	ModelKey  string
	ModelName string

	// LLM 依赖(构造期经 BindRegistry 注入;Provider 每次 wake 现取,
	// registry reload 后自动生效)。
	registry LLMRegistry
	// linePoolSource 是池模式(ModelKey=="")的线路来源(2026-09-21
	// §虚拟城市-城市Agent规模化 B3)。每次调 LLM 现取 —— Registry.Reload
	// 换池后自动生效。nil 且 ModelKey=="" 时 callProvider 立即按 LLM 失败
	// 处理(不得干等 ctx 超时),走既有 submit_month 兜底。
	linePoolSource func() *llm.LinePool

	// runner:引擎桥(game/virtual_city/agent_runner.go 注入)。
	runner ToolRunner

	// transcriptSink 由 game/virtual_city.AgentRunner 实现。Agent 在每次月度决策
	// 开始 / 结束前先把带 month + updated_at 的快照写入房间,避免 LLM 延迟
	// 或 submit 月结竞态导致前端 bot_contexts 停留在旧值。
	transcriptSink TranscriptSink

	memory *Memory

	// speak 限流:每月 ≤1 次 + 相邻 ≥30s(§9;仅决策 goroutine 访问)。
	lastSpeakAt time.Time
	// speakCount 本月 speak 次数(area+private 合计 ≤2;2026-09-22
	// §CityHuman重构 由 spokenThisMonth bool 升级;仅决策 goroutine 访问)。
	speakCount int
	// senseUsed 感知工具本月已用次数(see/hear/smell 各 ≤2;§CityHuman重构)。
	senseUsed map[string]int

	maxActions      int
	decisionTimeout time.Duration

	// 思维可见性(game.state.bot_contexts;读侧锁)。
	mu         sync.Mutex
	transcript BotTranscript
	cancelled  bool
}

// BotTranscript 是 game.state.bot_contexts 单座位快照。
type BotTranscript struct {
	Month               int
	UpdatedAt           int64
	Active              bool
	LastDecisionSummary string
	LastToolInput       string
	LastToolResult      string
	HeartThought        string
}

// TranscriptSink 把 Agent 思维快照同步到权威房间状态。它不并入 ToolRunner,
// 避免所有测试 fake 被迫实现 39 个工具以外的观测接口。
type TranscriptSink interface {
	RecordTranscript(seat int, transcript BotTranscript)
}

// DecisionScheduler 是房间级 expected-month 调度令牌。Agent 必须先在房间锁内
// 原子获取本月决策槽;动作桥执行时会再次校验同一 expected month。
type DecisionScheduler interface {
	BeginDecision(seat, month int) error
	EndDecision(seat, month int)
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
		GameKind:        "virtual_city",
		MySeat:          seat,
		MyUserID:        userID,
		ModelKey:        modelKey,
		ModelName:       modelName,
		memory:          NewMemory(),
		maxActions:      maxActions,
		decisionTimeout: decisionTimeout,
		senseUsed:       map[string]int{},
	}
}

// AgentClass 返回 AgentClassName(class_names.go 常量;§130 防散写字面量)。
func (a *Agent) AgentClass() agentroot.AgentClassName {
	return agentroot.AgentClassCityHuman
}

// BindRegistry 注入 LLM 注册表(Manager 构造后调用)。
func (a *Agent) BindRegistry(r LLMRegistry) {
	a.registry = r
}

// BindLinePoolSource 注入 LLM 线路池来源(池模式;Manager.EnsureAgents 在
// ModelKey=="" 座位上调用,2026-09-21 §虚拟城市 B3)。
func (a *Agent) BindLinePoolSource(fn func() *llm.LinePool) {
	a.linePoolSource = fn
}

// IsPoolMode 报告该 Agent 是否线路池驱动(ModelKey=="")。
func (a *Agent) IsPoolMode() bool { return a.ModelKey == "" }

// BindRunner 注入引擎桥(ToolRunner)。
func (a *Agent) BindRunner(runner ToolRunner) {
	a.runner = runner
	if sink, ok := runner.(TranscriptSink); ok {
		a.transcriptSink = sink
	}
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

// allowSpeak 每月 ≤2 次 speak(area+private 合计)+ 30s 令牌桶节流
// (§9;2026-09-22 §CityHuman重构 1→2)。
func (a *Agent) allowSpeak() bool {
	if a.speakCount >= 2 {
		return false
	}
	return time.Since(a.lastSpeakAt) >= 30*time.Second
}

func (a *Agent) markSpoken() {
	a.speakCount++
	a.lastSpeakAt = time.Now()
}
