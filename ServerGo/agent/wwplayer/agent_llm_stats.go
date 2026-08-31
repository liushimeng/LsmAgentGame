// Package wwplayer — agent_llm_stats.go: LLM 调用统计(Token/API/延迟)及相关方法。
// 从 agent.go 拆分出来,单文件 ≤ 1800 行硬约束(CLAUDE.md §4)。
package wwplayer

import (
	"time"

	llmtypes "LsmAgentGame/llm/types"
)

// agentTokenStats 是 Agent Token + API 统计的快照值对象（跨包传递用）。
// 2026-07-30 §统计增强。
type agentTokenStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalAPITokens    int
	APICallCount      int
	APISuccessCount   int
	APIFailCount      int
}
// MarkLLMCallStart records that an LLM HTTP call is now in flight. Called by
// run.go immediately before callProvider. Guarded by a.mu so the broadcast
// path can read it safely via IsLLMCallInProgress / BotTranscript.
func (a *Agent) MarkLLMCallStart() {
	a.Lock()
	a.llmCallInProgress = true
	a.llmCallStartedAt = time.Now()
	a.Unlock()
}

// MarkLLMCallEnd records that the LLM HTTP call has finished (success or
// error). The startedAt timestamp is preserved so the UI can show the last
// call's duration. Guarded by a.mu.
//
// 2026-07-10 §120 增强:累加本 bot 的 API 调用耗时到 avgLLMLatencyMs(滑动平均)
// 与 lastLLMLatencyMs(最后一次),并写入 BotTranscript.LLMLatencyMs /
// BotTranscript.AvgLLMLatencyMs 字段。前端「Agent 思考」面板可以展示
// "当前模型平均耗时 X.X s / 上次 Y.Y s",观战者据此判断哪个 bot 反应快。
// 公平性机制:BuildUserPrompt 在 prompt 末尾追加【模型响应速率】块,
// 让 LLM 知道自己的相对速率,引导"反应慢的 bot 减少工具调用 / 反应快的
// bot 多承担对话"(§120)。
//
// 2026-08-01 BUG-R225-P2-03: 本函数曾无条件 +1 成功,与文档承诺矛盾;
// 现拆为「纯复位」ResetLLMCallState 与「成功计数」MarkLLMCallEndWithUsage
// 两条路径,本函数仅保留向后兼容语义(同 ResetLLMCallState)。
func (a *Agent) MarkLLMCallEnd() {
	a.ResetLLMCallState()
}

// ResetLLMCallState 纯复位 LLM 调用状态(耗时/lastLLMCallAt/in-progress),
// 不累加任何 apiCallCount / apiSuccessCount / apiFailCount / token 统计。
// 适用于:(a) 循环 iteration 顶部的 safety-net 清理;(b) 已知不会计数的
// 辅助调用失败(如 speak_floor 失败,本属可选提醒,既不计入成功也不计入
// 失败)。错误处理:RecordAPIFailure / 真正的失败计数走独立路径。
func (a *Agent) ResetLLMCallState() {
	a.Lock()
	if a.llmCallInProgress && !a.llmCallStartedAt.IsZero() {
		elapsed := time.Since(a.llmCallStartedAt)
		ms := elapsed.Milliseconds()
		a.lastLLMLatencyMs = ms
		// 滑动平均(指数加权,α=0.3):新观测占 30% 权重,历史平均占 70%。
		// 避免冷启动时一个极端长尾样本把均值带偏。
		if a.avgLLMLatencyMs == 0 {
			a.avgLLMLatencyMs = ms
		} else {
			a.avgLLMLatencyMs = int64(0.7*float64(a.avgLLMLatencyMs) + 0.3*float64(ms))
		}
		a.totalLLMCalls++
	}
	// §127: 记录完成时间,前端据此渲染「Xs 前」。不论是否成功,以EndTime为准。
	a.lastLLMCallAt = time.Now()
	a.llmCallInProgress = false
	a.Unlock()
}

// MarkLLMCallEndWithUsage 标记一次 LLM 调用成功完成(累加 Token + 成功计数)。
// 2026-07-30 §统计增强:从 LLMResponse.Usage 读取 input_tokens / output_tokens 累加。
// 仅在「确实拿到 LLMResponse」的路径上调用;失败路径请改用 RecordAPIFailure。
func (a *Agent) MarkLLMCallEndWithUsage(usage llmtypes.LLMUsage) {
	a.Lock()
	if a.llmCallInProgress && !a.llmCallStartedAt.IsZero() {
		elapsed := time.Since(a.llmCallStartedAt)
		ms := elapsed.Milliseconds()
		a.lastLLMLatencyMs = ms
		// 滑动平均(指数加权,α=0.3):新观测占 30% 权重,历史平均占 70%。
		// 避免冷启动时一个极端长尾样本把均值带偏。
		if a.avgLLMLatencyMs == 0 {
			a.avgLLMLatencyMs = ms
		} else {
			a.avgLLMLatencyMs = int64(0.7*float64(a.avgLLMLatencyMs) + 0.3*float64(ms))
		}
		a.totalLLMCalls++
	}
	// 2026-07-30 §统计增强:累加 Token（纯内存态）。
	a.lastInputTokens = usage.InputTokens
	a.lastOutputTokens = usage.OutputTokens
	a.lastAPITokens = usage.InputTokens + usage.OutputTokens
	a.totalInputTokens += usage.InputTokens
	a.totalOutputTokens += usage.OutputTokens
	a.totalAPITokens += a.lastAPITokens
	a.apiCallCount++
	a.apiSuccessCount++
	// §127: 记录完成时间,前端据此渲染「Xs 前」。不论是否成功,以EndTime为准。
	a.lastLLMCallAt = time.Now()
	a.llmCallInProgress = false
	a.Unlock()
}

// RecordAPIFailure 记录一次 LLM 调用失败（递增 apiCallCount + apiFailCount,
// 不累加 Token）。2026-07-30 §统计增强。
func (a *Agent) RecordAPIFailure() {
	a.Lock()
	defer a.Unlock()
	a.apiCallCount++
	a.apiFailCount++
}

// AgentTokenStats 返回本 bot 的 Token + API 统计快照（供房间级聚合）。
// 2026-07-30 §统计增强。
func (a *Agent) AgentTokenStats() agentTokenStats {
	a.Lock()
	defer a.Unlock()
	return agentTokenStats{
		TotalInputTokens:  a.totalInputTokens,
		TotalOutputTokens: a.totalOutputTokens,
		TotalAPITokens:    a.totalAPITokens,
		APICallCount:      a.apiCallCount,
		APISuccessCount:   a.apiSuccessCount,
		APIFailCount:      a.apiFailCount,
	}
}
// AvgLLMLatencyMs 返回本 bot 当前模型 API 调用的滑动平均耗时(毫秒)。
// 2026-07-10 §120 公平性机制 — BuildUserPrompt 据此渲染【模型响应速率】块。
// 加锁读;若从未调过 LLM 返回 0。
func (a *Agent) AvgLLMLatencyMs() int64 {
	a.Lock()
	defer a.Unlock()
	return a.avgLLMLatencyMs
}

// LastLLMLatencyMs 返回本 bot 最近一次 LLM 调用耗时(毫秒)。
func (a *Agent) LastLLMLatencyMs() int64 {
	a.Lock()
	defer a.Unlock()
	return a.lastLLMLatencyMs
}

// TotalLLMCalls 返回本 bot 本局累计 LLM 调用次数。
func (a *Agent) TotalLLMCalls() int {
	a.Lock()
	defer a.Unlock()
	return a.totalLLMCalls
}

// MaxLLMRetries 返回外层 LLM 重试上限(由 cfg.Werewolf.LLMMaxRetries 注入,默认 5)。
// 2026-07-12 §127 增强;2026-07-15 R131 默认 7→5。
func (a *Agent) MaxLLMRetries() int {
	a.Lock()
	defer a.Unlock()
	return a.maxLLMRetries
}

// ClearLastFailureTimeForTest 清空 lastFailureTime,让下一次失败跨过 60s 冷却窗口
// 计入 consecutiveFailures。**仅供单测使用** (quarantine_round24_test.go 等需要
// 模拟 "持续 70s+ 故障" 场景的测试)。生产代码禁止调用。
// 2026-07-15 R131 修复: 永久错误也走冷却后,黑盒测试需要这条逃生口才能在合理
// 时间内累计到 permanentQuarantineThreshold=4。
func (a *Agent) ClearLastFailureTimeForTest() {
	a.Lock()
	defer a.Unlock()
	a.lastFailureTime = time.Time{}
}

// SetActiveStreamID / ActiveStreamID — §127 聊天 SSE 流式,每个 LLM 调用唯一 ID。
func (a *Agent) SetActiveStreamID(id string) {
	a.Lock()
	a.activeStreamID = id
	a.Unlock()
}
func (a *Agent) ActiveStreamID() string {
	a.Lock()
	defer a.Unlock()
	return a.activeStreamID
}

// OnLLMStreamStart / OnLLMStreamDelta / OnLLMStreamEnd — §127 聊天 SSE 流式回调接线。
// 只在 room 接线时调用一次;锁保护足够。
func (a *Agent) OnLLMStreamStart(fn func(string)) {
	a.Lock()
	a.onLLMStreamStart = fn
	a.Unlock()
}
func (a *Agent) OnLLMStreamDelta(fn func(string, string)) {
	a.Lock()
	a.onLLMStreamDelta = fn
	a.Unlock()
}
func (a *Agent) OnLLMStreamEnd(fn func(string, string)) {
	a.Lock()
	a.onLLMStreamEnd = fn
	a.Unlock()
}

// IsLLMCallInProgress returns (inProgress, startedAtUnixMilli) for the current
// LLM call state. Safe to call from the broadcast goroutine. When inProgress
// is false, startedAt still reflects the most recent call's start time.
func (a *Agent) IsLLMCallInProgress() (bool, int64) {
	a.Lock()
	defer a.Unlock()
	return a.llmCallInProgress, a.llmCallStartedAt.UnixMilli()
}
