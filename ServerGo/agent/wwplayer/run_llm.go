package wwplayer

import (
	"context"
	"time"

	"LsmWebGame/llm"
	"LsmWebGame/llm/anthropic"
	llmtypes "LsmWebGame/llm/types"
)

// circuitShortCircuitMinElapsedTime §20260810-15: 仅在 bot 已尝试调用 ≥1 次 LLM 后
// 才信任短路信号。构造期 / 首次 wake 的 Provider model400Window / model429Window
// 都为空,短路信号一定是 false(closed)——这是预期行为。
//
// 该常量主要是为测试提供一致的「首次调用不短路」契约;生产逻辑无副作用。
const circuitShortCircuitMinElapsedTime = 1 * time.Millisecond

func (a *Agent) callProvider(ctx context.Context, req llm.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llm.LLMResponse, error) {
	// BUG-R226-P1-02 (2026-08-01) — extended thinking 顶层字段注入入口。
	// 当 registry.GetThinkingEnabled(modelKey) == true 且调用方未显式设置
	// 时,把 req.Thinking 填上;provider 在 pre-flight 阶段将其序列化为
	// 顶层 `{"type":"enabled","budget_tokens":N}`(§14.1 权威用例形状)。
	//
	// 设计:agent 不感知具体 budget 数值 — 由 registry 从 cfg / DB 单点读,
	// 调用方只在 req.Thinking == nil 时填,允许上层(测试 / 调试)在 req
	// 上直接覆盖(无需碰 cfg)。
	if req.Thinking == nil && a.Registry != nil {
		if enabled, budget := a.Registry.GetThinkingEnabled(a.ModelKey); enabled {
			req.Thinking = &llmtypes.ThinkingConfig{
				Type:         "enabled",
				BudgetTokens: budget,
			}
		}
	}

	// §20260810-15: 短路前置 — model_400 / model_429 熔断 / 端点 breaker
	// 打开时立即返回错误,避免空跑完整 retry chain (浪费 5-30s × N 次)。
	// 实测 Tencent-model 16:32-16:34 期间 4 次调用即停的根因之一就是
	// 熔断器已开但 callProvider 仍发 HTTP,导致每次 retry 都失败,
	// 累计 cf 触发了误 quarantine。
	if p, ok := a.Provider.(*anthropic.Provider); ok {
		if p.Model400CircuitOpen(req.Model) {
			return llm.LLMResponse{}, &anthropic.Error{
				HTTPStatus: 400,
				Retryable:  true,
				Source:     "model_400_circuit",
				Message:    "anthropic: model 400 circuit open; short-circuited before send (§20260810-15)",
			}
		}
		if p.Model429CircuitOpen(req.Model) {
			return llm.LLMResponse{}, &anthropic.Error{
				HTTPStatus: 429,
				Retryable:  true,
				Source:     "model_429_circuit",
				Message:    "anthropic: model 429 circuit open (rate limit); short-circuited before send (§20260810-15)",
			}
		}
		if p.BreakerOpenAny() {
			return llm.LLMResponse{}, &anthropic.Error{
				HTTPStatus: 503,
				Retryable:  true,
				Source:     "breaker",
				Message:    "anthropic: endpoint breaker open (all endpoints); short-circuited before send (§20260810-15)",
			}
		}
	}

	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llm.LLMRequest, func(llmtypes.StreamEvent) error) (llm.LLMResponse, error)
	}
	if sp, ok := a.Provider.(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, a.apiKey, req, onProgress)
	}
	return a.Provider.Chat(ctx, a.apiKey, req)
}
