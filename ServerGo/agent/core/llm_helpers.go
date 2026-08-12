// Package agentcore — llm_helpers.go: 通用 LLM 错误分类与重试退避策略。
//
// 2026-08-06 §Agent 重构:从原 ServerGo/agent/run_helpers.go 抽出真正
// 跨游戏可复用的部分(LLM 错误分类 / 退避)。狼人杀专属的 containsSeat /
// phaseAllowsPublicSpeech / buildMetadataUserID(*Agent) 留在 agent 包
// 内,Step 3 整体搬到 wwplayer。
//
// 设计原则(§14 / §197 / §186):
//   - 不依赖 *Agent / GameContext 等游戏专属类型
//   - 不依赖 game/* 包
//   - 仅依赖 anthropic wire 错误类型(已归一化,跨 provider 适用)
//
// 后续若加入 OpenAI/Ollama provider,这些分类函数可加 Provider 维度开关,
// 但当前实现已能覆盖 4xx/5xx/429/timeout/network 全部常见瞬时错误。
package agentcore

import (
	"context"
	"errors"
	"strings"

	"LsmWebGame/llm/anthropic"
)

// LLMErrorIsAnthropic429 报告 err 是否代表上游 429/限流。
//
// 用于触发指数退避重试(§197/§186)。覆盖:
//   - *anthropic.Error.HTTPStatus == 429
//   - err.Error() 含 "429" / "rate limit" / "too many requests"(裸 http 客户端错误)
func LLMErrorIsAnthropic429(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*anthropic.Error); ok {
		return ae.HTTPStatus == 429
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests")
}

// LLMErrorIsTimeout 报告 err 是否为超时错误(进程级 / I/O / 上下文 deadline)。
func LLMErrorIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout") {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return false
}

// LLMErrorIsNetworkTransient 报告 err 是否为"网络瞬时错误",应进重试。
//
// 覆盖:
//   - 超时(LLMErrorIsTimeout)
//   - 连接被重置 / connection refused / EOF / use of closed network connection
//   - TLS handshake 失败(短窗可恢复)
//
// 2026-08-06 §95 教训:上游主动断连(`context canceled` / `use of closed
// network connection`)必须 classifier 标记 Retryable=true 进重试,
// 否则 4/7 Agent 被无谓 quarantine。
func LLMErrorIsNetworkTransient(err error) bool {
	if err == nil {
		return false
	}
	if LLMErrorIsTimeout(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	transient := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"unexpected eof",
		"eof",
		"use of closed network connection",
		"tls handshake",
		"no such host",
		"i/o timeout",
	}
	for _, needle := range transient {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// LLMErrorIsModel400Circuit 报告 err 是否代表"上游 Provider 4xx 错误且
// 永久不可重试"(典型:invalid api key 401、模型下架 404、参数非法 400)。
//
// 用于直接 quarantine(§R108 永久错误绕过冷却快速 quarantine),不进重试。
func LLMErrorIsModel400Circuit(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*anthropic.Error); ok {
		// 4xx 永久错误:401/403/404 → 不重试
		if ae.HTTPStatus >= 400 && ae.HTTPStatus < 500 {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	// 裸 http 客户端文本匹配兜底
	for _, needle := range []string{"401", "403", "404", "unauthorized", "forbidden", "not found"} {
		if strings.Contains(msg, needle) {
			// 排除已经在 429 路径上的("429" 不在 4xx 兜底,见 LLMErrorIsAnthropic429)
			if strings.Contains(msg, "429") {
				continue
			}
			return true
		}
	}
	return false
}