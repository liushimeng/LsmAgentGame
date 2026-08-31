// Package debatejudge — judge_llm_stats.go: 裁判 LLM 调用统计(Token/API/延迟)。
//
// 2026-08-31 §20260831-09 — 辩论比赛 Agent 实时统计:
// 仿狼人杀 ServerGo/agent/wwjudge/judge.go::judgeTokenStats 的字段语义,
// 把每位裁判的 LLM 调用次数 + Token 输入/输出/总 + 成功率 / 失败率独立统计,
// 供 DebateRoom.AggregateAgentStats() 聚合后下发到前端 AgentStatsPanel。
//
// 字段全部由 j.mu 保护。
package debatejudge

import (
	"time"

	"LsmAgentGame/llm/types"
)

// judgeTokenStats 单裁判 Token + API 统计的快照值对象(跨包传递用)。
//
// 字段与 wolfplayer.agentTokenStats / wolfjudge.judgeTokenStats 严格对齐,
// 便于前端类型统一(均含 TotalInputTokens / TotalOutputTokens /
// TotalAPITokens / APICallCount / APISuccessCount / APIFailCount / Last*)。
type judgeTokenStats struct {
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
// 必须在 j.provider.Chat() 调用之前调用;由 runJudgeTurn() 入口触发。
func (j *AgentJudge) MarkLLMCallStart() {
	j.mu.Lock()
	j.llmCallInProgress = true
	j.llmCallStartedAt = time.Now()
	j.mu.Unlock()
}

// MarkLLMCallEndWithUsage 标记一次 LLM 调用成功完成,累加 Token + 成功计数。
//
// 必须在拿到 LLMResponse 的路径上调用;失败路径请改用 RecordAPIFailure。
func (j *AgentJudge) MarkLLMCallEndWithUsage(usage types.LLMUsage) {
	j.mu.Lock()
	if j.llmCallInProgress && !j.llmCallStartedAt.IsZero() {
		elapsed := time.Since(j.llmCallStartedAt)
		ms := elapsed.Milliseconds()
		j.lastLLMLatencyMs = ms
		if j.avgLLMLatencyMs == 0 {
			j.avgLLMLatencyMs = ms
		} else {
			j.avgLLMLatencyMs = int64(0.7*float64(j.avgLLMLatencyMs) + 0.3*float64(ms))
		}
		j.totalLLMCalls++
	}
	j.lastInputTokens = usage.InputTokens
	j.lastOutputTokens = usage.OutputTokens
	j.lastAPITokens = usage.InputTokens + usage.OutputTokens
	j.totalInputTokens += usage.InputTokens
	j.totalOutputTokens += usage.OutputTokens
	j.totalAPITokens += j.lastAPITokens
	j.apiCallCount++
	j.apiSuccessCount++
	j.lastLLMCallAt = time.Now()
	j.llmCallInProgress = false
	j.mu.Unlock()
}

// RecordAPIFailure 记录一次 LLM 调用失败:递增 apiCallCount + apiFailCount,
// 不累加 Token。
func (j *AgentJudge) RecordAPIFailure() {
	j.mu.Lock()
	j.apiCallCount++
	j.apiFailCount++
	j.llmCallInProgress = false
	j.mu.Unlock()
}

// ResetLLMCallState 纯复位 LLM 调用状态(耗时/in-progress),不累加任何计数。
//
// 适用于 fallback 兜底、best-effort 重试等不计数的辅助调用。
func (j *AgentJudge) ResetLLMCallState() {
	j.mu.Lock()
	if j.llmCallInProgress && !j.llmCallStartedAt.IsZero() {
		elapsed := time.Since(j.llmCallStartedAt)
		j.lastLLMLatencyMs = elapsed.Milliseconds()
		j.llmCallInProgress = false
	}
	j.mu.Unlock()
}

// JudgeTokenStats 返回本裁判的 Token + API 统计快照(供房间级聚合)。
func (j *AgentJudge) JudgeTokenStats() judgeTokenStats {
	j.mu.Lock()
	defer j.mu.Unlock()
	return judgeTokenStats{
		TotalInputTokens:  j.totalInputTokens,
		TotalOutputTokens: j.totalOutputTokens,
		TotalAPITokens:    j.totalAPITokens,
		APICallCount:      j.apiCallCount,
		APISuccessCount:   j.apiSuccessCount,
		APIFailCount:      j.apiFailCount,
		LastInputTokens:   j.lastInputTokens,
		LastOutputTokens:  j.lastOutputTokens,
		LastAPITokens:     j.lastAPITokens,
	}
}

// TotalLLMCalls 返回本裁判本局累计 LLM 调用次数。
func (j *AgentJudge) TotalLLMCalls() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.totalLLMCalls
}
