// Package wealthplayer — run_llm.go: Provider 抽象(流式优先,§197「接收到字节
// 即刷新超时」,2026-09-14 §财商流P0)。
//
// 仿 wwplayer/run_llm.go 的窄接口 + ChatStreamAccumulate 优先模式;
// 模型取 registry.Get(modelKey);思考预算注入由 AgentClassName 自动获得。
//
// 2026-09-21 §虚拟城市 B3:ModelKey=="" 的座位为「线路池驱动」—— 每次调用
// 先经 LinePool.Acquire 取一条线路(令牌内含已构造 Provider + 解密 key),
// 用毕 Release;Acquire 失败 = ErrAllLinesBusy → 走既有失败兜底(强制
// submit_month),不重试不干等。
package wealthplayer

import (
	"context"
	"errors"

	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// errPoolModeNoSource 池模式未注入线路池来源(装配缺口;按 LLM 失败处理)。
var errPoolModeNoSource = errors.New("wealthplayer: pool-mode agent has no line pool source bound")

// callProvider 与 wwplayer.run_llm.callProvider 同构:thinking 自动注入 +
// 短路信号处理 + ChatStreamAccumulate 优先(§197 字节刷新)。
func (a *Agent) callProvider(ctx context.Context, req llm.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llm.LLMResponse, error) {
	if req.AgentClassName == "" {
		req.AgentClassName = string(a.AgentClass())
	}
	// 池模式分支(ModelKey==""):先取线路,再用租约内的 Provider/Key 调用。
	if a.ModelKey == "" {
		return a.callProviderViaPool(ctx, req, onProgress)
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

// callProviderViaPool 池模式调用:Acquire → 租约 Provider 调用 → Release。
// 硬约束(契约 01 §3.2 B3):
//   - pool 缺失或 Total()==0 → 立即返回错误(不得干等 ctx 超时);
//   - Acquire 失败(ErrAllLinesBusy)= 一次 LLM 失败,由 OnMonthStart 的
//     既有兜底强制 submit_month;
//   - 成功则用 lease.Provider/APIKey 发起调用(沿用 ChatStreamAccumulate
//     流式路径与 AgentClassName 机制),defer Release(幂等)。
func (a *Agent) callProviderViaPool(ctx context.Context, req llm.LLMRequest, onProgress func(llmtypes.StreamEvent) error) (llm.LLMResponse, error) {
	if a.linePoolSource == nil {
		return llm.LLMResponse{}, errPoolModeNoSource
	}
	pool := a.linePoolSource()
	if pool == nil || pool.Total() == 0 {
		return llm.LLMResponse{}, llm.ErrAllLinesBusy
	}
	lease, err := pool.Acquire(ctx)
	if err != nil {
		return llm.LLMResponse{}, err
	}
	defer lease.Release()
	req.Model = lease.ModelKey
	if req.Thinking == nil && a.registry != nil {
		if enabled, budget := a.registry.GetThinkingEnabled(lease.ModelKey); enabled {
			req.Thinking = &llmtypes.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
		}
	}
	type streamingProvider interface {
		ChatStreamAccumulate(context.Context, string, llm.LLMRequest, func(llmtypes.StreamEvent) error) (llm.LLMResponse, error)
	}
	if sp, ok := lease.Provider.(streamingProvider); ok {
		return sp.ChatStreamAccumulate(ctx, lease.APIKey, req, onProgress)
	}
	return lease.Provider.Chat(ctx, lease.APIKey, req)
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