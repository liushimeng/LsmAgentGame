// Package debateplayer — agent_llm_stats.go: LLM 调用统计(Token/API/延迟)及相关方法。
//
// 2026-08-31 §20260831-09 — 辩论比赛 Agent 实时统计:
// 仿狼人杀 ServerGo/agent/wwplayer/agent_llm_stats.go 的范式(§17 §20260817-03 U3)
// 把每个 Bot 的 LLM 调用次数 + Token 输入/输出/总 + 成功率 / 失败率独立统计,
// 供 DebateRoom.AggregateAgentStats() 聚合后下发到前端 AgentStatsPanel。
//
// 字段全部由 a.mu 保护,a.provider.Chat() 之前调 MarkLLMCallStart,
// 之后分支:成功 → MarkLLMCallEndWithUsage(usage);失败 → RecordAPIFailure。
package debateplayer

import (
	"time"

	"LsmAgentGame/llm/types"
)

// agentTokenStats 单 Agent Token + API 统计的快照值对象(跨包传递用)。
//
// 与 wolfplayer.agentTokenStats 字段对齐,便于前端类型统一。
type agentTokenStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalAPITokens    int
	APICallCount      int
	APISuccessCount   int
	APIFailCount      int
	LastInputTokens   int
	LastOutputTokens  int
	LastAPITokens     int
}

// MarkLLMCallStart 记录一次 LLM HTTP 调用开始。
//
// 必须在 a.provider.Chat() 调用之前调用;由 runTurn() 入口触发。
// 加锁保护:广播路径 / transcript 读取与本方法并发。
func (a *Agent) MarkLLMCallStart() {
	a.mu.Lock()
	a.llmCallInProgress = true
	a.llmCallStartedAt = time.Now()
	a.mu.Unlock()
}

// MarkLLMCallEndWithUsage 标记一次 LLM 调用成功完成,累加 Token + 成功计数。
//
// 必须在拿到 LLMResponse 的路径上调用;失败路径请改用 RecordAPIFailure。
// 滑动平均延迟(α=0.3)与狼人杀同款(防冷启动极端值带偏均值)。
func (a *Agent) MarkLLMCallEndWithUsage(usage types.LLMUsage) {
	a.mu.Lock()
	if a.llmCallInProgress && !a.llmCallStartedAt.IsZero() {
		elapsed := time.Since(a.llmCallStartedAt)
		ms := elapsed.Milliseconds()
		a.lastLLMLatencyMs = ms
		if a.avgLLMLatencyMs == 0 {
			a.avgLLMLatencyMs = ms
		} else {
			a.avgLLMLatencyMs = int64(0.7*float64(a.avgLLMLatencyMs) + 0.3*float64(ms))
		}
		a.totalLLMCalls++
	}
	a.lastInputTokens = usage.InputTokens
	a.lastOutputTokens = usage.OutputTokens
	a.lastAPITokens = usage.InputTokens + usage.OutputTokens
	a.totalInputTokens += usage.InputTokens
	a.totalOutputTokens += usage.OutputTokens
	a.totalAPITokens += a.lastAPITokens
	a.apiCallCount++
	a.apiSuccessCount++
	a.lastLLMCallAt = time.Now()
	a.llmCallInProgress = false
	a.mu.Unlock()
}

// RecordAPIFailure 记录一次 LLM 调用失败:递增 apiCallCount + apiFailCount,
// 不累加 Token。失败路径由 runTurn / dispatchTool 内部失败处理调用。
func (a *Agent) RecordAPIFailure() {
	a.mu.Lock()
	a.apiCallCount++
	a.apiFailCount++
	a.llmCallInProgress = false
	a.mu.Unlock()
}

// ResetLLMCallState 纯复位 LLM 调用状态(耗时/in-progress),不累加任何计数。
//
// 适用于:(a) 已知不会计数的辅助调用失败(如 quota 已耗尽的 best-effort 重试);
// (b) 循环 iteration 顶部 safety-net 清理。
func (a *Agent) ResetLLMCallState() {
	a.mu.Lock()
	if a.llmCallInProgress && !a.llmCallStartedAt.IsZero() {
		elapsed := time.Since(a.llmCallStartedAt)
		a.lastLLMLatencyMs = elapsed.Milliseconds()
		a.llmCallInProgress = false
	}
	a.mu.Unlock()
}

// AgentTokenStats 返回本 Bot 的 Token + API 统计快照(供房间级聚合)。
func (a *Agent) AgentTokenStats() agentTokenStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return agentTokenStats{
		TotalInputTokens:  a.totalInputTokens,
		TotalOutputTokens: a.totalOutputTokens,
		TotalAPITokens:    a.totalAPITokens,
		APICallCount:      a.apiCallCount,
		APISuccessCount:   a.apiSuccessCount,
		APIFailCount:      a.apiFailCount,
		LastInputTokens:   a.lastInputTokens,
		LastOutputTokens:  a.lastOutputTokens,
		LastAPITokens:     a.lastAPITokens,
	}
}

// TotalLLMCalls 返回本 Bot 本局累计 LLM 调用次数(对齐狼人杀字段语义)。
func (a *Agent) TotalLLMCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.totalLLMCalls
}

// AvgLLMLatencyMs 返回本 Bot 当前模型 API 调用的滑动平均耗时(毫秒)。
func (a *Agent) AvgLLMLatencyMs() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.avgLLMLatencyMs
}

// LastLLMLatencyMs 返回本 Bot 最近一次 LLM 调用耗时(毫秒)。
func (a *Agent) LastLLMLatencyMs() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastLLMLatencyMs
}
