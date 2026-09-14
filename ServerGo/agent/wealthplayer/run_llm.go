// Package wealthplayer — run_llm.go: Provider 抽象(流式优先,§197「接收到字节
// 即刷新超时」,2026-09-14 §财商流P0)。
//
// 仿 wwplayer/run_llm.go 的窄接口 + ChatStreamAccumulate 优先模式;
// 模型取 registry.Get(modelKey);思考预算注入由 AgentClassName 自动获得。
package wealthplayer

import (
	"context"

	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// callProvider 与 wwplayer.run_llm.callProvider 同构:thinking 自动注入 +
// 短路信号处理 + ChatStreamAccumulate 优先(§197 字节刷新)。
func (a *Agent) callProvider(ctx context.Context, req llm.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llm.LLMResponse, error) {
	if req.AgentClassName == "" {
		req.AgentClassName = string(a.AgentClass())
	}
	if req.Thinking == nil && a.registry != nil {
		if enabled, budget := a.registry.GetThinkingEnabled(a.ModelKey); enabled {
			req.Thinking = &llmtypes.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
		}
	}
	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llm.LLMRequest, func(llmtypes.StreamEvent) error) (llm.LLMResponse, error)
	}
	if sp, ok := a.provider().(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, a.apiKey(), req, onProgress)
	}
	return a.provider().Chat(ctx, a.apiKey(), req)
}

// provider / apiKey 每次调 LLM 都重新解析(支持 registry reload 后生效)。
func (a *Agent) provider() llmtypes.LLMProvider {
	if a.registry == nil {
		return nil
	}
	p, _, err := a.registry.Get(a.ModelKey)
	if err != nil {
		return nil
	}
	return p
}

func (a *Agent) apiKey() string {
	if a.registry == nil {
		return ""
	}
	_, k, err := a.registry.Get(a.ModelKey)
	if err != nil {
		return ""
	}
	return k
}