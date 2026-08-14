// Package wwplayer — preflight_compress.go: pre-step 主动语义压缩 + overflow 兜底。
//
// 2026-08-13 §20260813-05 U3。借鉴 dsh compaction-basic/src/index.ts:147-223
// 双触发(主动 + overflow 兜底)模式。
//
// 与 §20260813-04 U4 已有 preflight(纯字节裁剪)互补:
//   - U4 preflight = 廉价字节回收(不动语义)
//   - 本文件 pre-step = 贵但有效的语义压缩(LLM 摘要)
// DSH 的两层设计: 廉价手段先上,贵手段兜底,与现有架构天然契合。
//
// 设计要点:
//
//   - **三梯度协同**(与既有 §20260813-04 U4 共存):
//     60% → PruneToolResultsOnly       (无 LLM, 廉价, tool_result 文本截断)
//     80% → 本文件主动压缩              (调 LLM summarizer, 贵但保留语义)
//     100% → preflight PruneByBytes    (无 LLM, 整条消息丢弃)
//     400% → post-error PruneByBytesAggressive (上游 400 后兜底)
//   - **overflow 计数**: 每次成功 LLM 调用必须清零(DSH §8.6 不变量)
//   - **失败 fallback**: 主动压缩失败时退化为 PruneByBytesAggressive(50% 预算)
package wwplayer

import (
	"context"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 主动压缩触发阈值与重试上限。借鉴 dsh defaultThresholdRatio=0.8。
const (
	// preflightCompressTriggerPct 是 pre-step 主动压缩的触发阈值(占有效预算)。
	// 取 80%: 比 preflight(100%)早触发,给慢模型留出充分恢复时间(§197)。
	preflightCompressTriggerPct = 80

	// preflightCompressMaxOverflow 是 overflow 兜底最大重试。
	// DSH 默认 2。语义压缩慢(每局可能 1-2min),激进重试代价高,取 2。
	preflightCompressMaxOverflow = 2

	// preflightCompressMinPayloadBytes 是压缩触发的下限。
	// 与 §20260813-04 U4 preflightMinBudgetBytes=64KB 保持一致,
	// 防止短 payload(<32KB)被误压缩,丢失关键上下文。
	preflightCompressMinPayloadBytes = 32 * 1024

	// preflightCompressMinOverflowPayloadRatio 是 overflow 重试触发的最小 payload 比例。
	// 借鉴 dsh:overflow 重试不应每次都触发,只在 payload 仍 > 30% budget 时才重试,
	// 防止"压缩成功但仍反复触发"的抖动。
	preflightCompressMinOverflowPayloadRatio = 30
)

// preflightCompressLoop 双触发主动压缩入口。每次发请求前调用。
//
// 入参:
//   - ctx 用于 LLM 摘要调用(可取消)
//   - a 当前 Agent(含 Memory / Provider / apiKey)
//   - payloadBytes 当前消息体字节估算(同 a.Memory.TotalPayloadBytes())
//   - budget 有效输入预算(同 preflightBudgetBytes)
//   - overflowCount 本局已 overflow 重试累计(传入上次的值,返回新值)
//   - gc 当前 GameContext(用于 CompactWithLLM;nil 时跳过 LLM 路径,直接 fallback)
//
// 出参:
//   - newPayloadBytes 压缩后字节数(0 = 未压缩)
//   - newOverflowCount 更新后的 overflow 计数(成功清零 / 失败 +1)
//   - compressed true = 触发了压缩(主动或 overflow)
func preflightCompressLoop(ctx context.Context, a *Agent, payloadBytes int, budget int, overflowCount int, gc *wwtypes.GameContext) (int, int, bool) {
	if a == nil || budget <= 0 {
		return payloadBytes, overflowCount, false
	}
	if payloadBytes < preflightCompressMinPayloadBytes {
		// 极小 payload 不压缩,避免误杀小局(<5 条消息的发言阶段)。
		return payloadBytes, overflowCount, false
	}

	// 触发 1: 主动 — payloadBytes > 80% budget
	triggerBytes := budget * preflightCompressTriggerPct / 100
	if payloadBytes > triggerBytes {
		return runCompactLocked(ctx, a, payloadBytes, "preflight_pre_step", overflowCount, true, gc)
	}

	// 触发 2: overflow 兜底 — 已 overflow 累计 > 0 且 payload 仍 > 30% budget
	if overflowCount > 0 {
		minPayload := budget * preflightCompressMinOverflowPayloadRatio / 100
		if payloadBytes > minPayload {
			return runCompactLocked(ctx, a, payloadBytes, "preflight_overflow", overflowCount, false, gc)
		}
	}

	return payloadBytes, overflowCount, false
}

// runCompactLocked 实际执行一次压缩:优先 LLM 摘要,失败 fallback 字节裁剪。
//
// 参数:
//   - payloadBytes 当前字节数(用于日志)
//   - source 触发源("preflight_pre_step" / "preflight_overflow" / "post_error")
//   - overflowCount 上次 overflow 累计
//   - isPreStep true = 主动路径(成功后清零 overflow),false = overflow 路径
//   - gc 当前 GameContext(用于 CompactWithLLM)
//
// 出参: (newPayloadBytes, newOverflowCount, true)
func runCompactLocked(ctx context.Context, a *Agent, payloadBytes int, source string, overflowCount int, isPreStep bool, gc *wwtypes.GameContext) (int, int, bool) {
	before := payloadBytes
	var newBytes int
	var success bool

	// 尝试 1: LLM 摘要 (CompactWithLLM + CompressAndPrune)
	if a.Memory != nil && a.Provider != nil && a.apiKey != "" && gc != nil {
		result := a.Memory.CompactWithLLM(ctx, a.Provider, a.apiKey, a.ModelKey, gc, a.compactConfig)
		if result.Success && result.Summary != "" {
			// CompressAndPrune(maxTurns, compressTurns): 取 conservative 默认值
			// 30 turns 上限 + 20 turns 压缩起点,与既有 run_compact.go 路径一致。
			a.Memory.CompressAndPrune(30, DefaultCompressTurns)
			success = true
		} else {
			logger.L().Warn("agent: preflight compact LLM call failed; falling back to byte prune",
				zap.Int("seat", a.Seat), zap.String("model", a.ModelKey),
				zap.String("source", source), zap.Error(result.Error))
		}
	}

	// 尝试 2: 字节裁剪 fallback(CompactWithLLM 失败或未启用)
	if !success {
		if a.Memory == nil {
			return before, overflowCount, false
		}
		a.Memory.PruneByBytesAggressive()
	}

	newBytes = a.Memory.TotalPayloadBytes()

	// overflow 计数管理(DSH §8.6 不变量)
	newCount := overflowCount
	if success && isPreStep {
		// 主动压缩成功 → 清零 overflow
		newCount = 0
	} else if !success {
		// 失败 → +1,达到上限后告警
		newCount++
		if newCount > preflightCompressMaxOverflow {
			logger.L().Error("agent: preflight compact overflow exhausted",
				zap.Int("seat", a.Seat), zap.String("model", a.ModelKey),
				zap.String("source", source),
				zap.Int("overflow_count", newCount))
		}
	}

	logger.L().Info("agent: preflight compact done",
		zap.Int("seat", a.Seat), zap.String("model", a.ModelKey),
		zap.String("source", source),
		zap.Bool("success", success),
		zap.Int("payload_bytes_before", before),
		zap.Int("payload_bytes_after", newBytes),
		zap.Int("overflow_count", newCount))

	return newBytes, newCount, true
}

// resetPreflightOverflowCount 每次成功 LLM 调用后由 Run() 调,
// 实现 DSH §8.6 不变量:"overflowRetries 在每次 assistant/message 成功时清零"。
func (a *Agent) resetPreflightOverflowCount() {
	if a == nil {
		return
	}
	a.preflightOverflowCount = 0
}