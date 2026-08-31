// Package wwplayer — agent_phase.go: LLM 调用相位状态机(5 态)及相关方法。
// 从 agent.go 拆分出来,单文件 ≤ 1800 行硬约束(CLAUDE.md §4)。
package wwplayer

import (
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

const (
	PhaseIdle        = "idle"        // 未调 / 已完成 / 占位
	PhaseCalling     = "calling"     // HTTP 调用中(响应等待中),non-stream 整体或 stream 首 token 前
	PhaseStreaming   = "streaming"   // 流式首 token 到达(可选,目前 non-stream 退化为 calling)
	PhaseRetrying    = "retrying"    // retry loop 内,等待 backoff 后重试
	PhaseQuarantined = "quarantined" // 永久禁用,不再调 LLM
)
// 2026-07-10 §重构 — LLM 调用相位状态机 setter/getter。
// 5 态:idle / calling / streaming / retrying / quarantined。
// 全部走 a.mu,供 run.go 在主循环 6 个写点(safety-net / limiter /
// semaphore / MarkLLMCallStart / retry loop / MarkLLMCallEnd)调用。
//
// 设计原则:
//   - 单一 setter (SetLLMCallPhase) 集中切换 phase,便于审计/日志
//   - retryAttempt/retryMaxAttempts/nextRetryAtMs/lastErrorClass 各自独立
//     setter,因为它们的生命周期不同(phase 是瞬时切换,retry 状态只在 loop 内)
//   - ResetLLMCallPhase 用于"成功调用结束"或"quarantine"时整体清场

// SetLLMCallPhase 切换相位状态机。phase 必须是 5 态之一,非法值会被
// 视为 "idle"(并打 debug 日志)。受 a.mu 保护。
func (a *Agent) SetLLMCallPhase(phase string) {
	if phase != PhaseIdle && phase != PhaseCalling && phase != PhaseStreaming &&
		phase != PhaseRetrying && phase != PhaseQuarantined {
		logger.L().Debug("agent: ignoring invalid LLMCallPhase",
			zap.Int("seat", a.Seat), zap.String("phase", phase))
		return
	}
	a.Lock()
	a.llmCallPhase = phase
	a.Unlock()
}

// ResetLLMCallPhase 在成功调用结束或 quarantine 时调用,把相位+retry+nextRetryAt
// 全部清场(lastErrorClass 保留,供后续失败时复用)。
func (a *Agent) ResetLLMCallPhase(phase string) {
	a.Lock()
	a.llmCallPhase = phase
	a.retryAttempt = 0
	a.retryMaxAttempts = 0
	a.nextRetryAtMs = 0
	a.Unlock()
}

// SetRetryAttempt 记录当前 retry 轮次(1-based);maxAttempts 是上限,
// 供前端展示 N/M。仅在 retrying phase 内调用。
func (a *Agent) SetRetryAttempt(attempt int, maxAttempts int, nextRetryAtMs int64) {
	a.Lock()
	a.retryAttempt = attempt
	a.retryMaxAttempts = maxAttempts
	a.nextRetryAtMs = nextRetryAtMs
	a.Unlock()
}

// SetLastErrorClass 写入上次失败分类。
// class ∈ {"none", "5xx", "429", "timeout", "permanent", "queued", "throttled"}。
// 失败时调用;成功后由 ResetConsecutiveFailures 同时清零。
// §127 新增 queued/throttled:区分"等待 LLM 并发槽"与"被 LLMCallLimiter 限流"。
func (a *Agent) SetLastErrorClass(class string) {
	if class == "" {
		class = "none"
	}
	a.Lock()
	a.lastErrorClass = class
	a.Unlock()
}

// SetQueuedState 把 phase 切到 retrying 并标记 last_error_class=queued,
// 供 run.go 在 semaphore 等待失败 / LLMCallLimiter 限流时统一展示"排队中"。
func (a *Agent) SetQueuedState(reason string) {
	a.Lock()
	a.llmCallPhase = PhaseRetrying
	switch reason {
	case "semaphore":
		a.lastErrorClass = "queued"
	case "limiter":
		a.lastErrorClass = "throttled"
	default:
		a.lastErrorClass = "queued"
	}
	a.Unlock()
}

// LLMCallPhase 返回当前 phase(读字段,加锁)。
func (a *Agent) LLMCallPhase() string {
	a.Lock()
	defer a.Unlock()
	return a.llmCallPhase
}
