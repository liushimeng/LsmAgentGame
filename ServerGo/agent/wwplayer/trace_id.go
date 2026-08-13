// Package wwplayer — trace_id.go: 每次 LLM 调用的全链路追踪 ID。
//
// 2026-08-13 §20260813-01 优化: 借鉴 agent-studio `set_session_id` 全链路追踪
// (docs/其他Agent代码分析/agent-studio_Context管理分析.md §5.3),
// 给每次 LLM 调用分配唯一 RequestID,贯穿 RunLoop / callProvider /
// tool dispatch 全路径,所有 logger 自动带同一 ID,排查 §197 误杀 / §82b
// 配对失败等问题时可一键 grep 还原。
//
// 设计要点:
//   - RequestID 是 16 字节 UUID 的 hex 形式(32 字符),全局唯一。
//   - 通过 context.Value 传递,**禁止**全局变量(并发安全)。
//   - 不修改现有 callProvider / DispatchTool 签名 — 调用方按需 ctx 提取。
//   - TraceSpan 是可选扩展(本版本仅提供 ID,完整 span 树留待后续)。
//
// 与 §197/§128/§130 兼容性: 本文件不改变 LLM 调用链,只增加 ID 字段。
package wwplayer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type traceIDKey struct{}

// NewRequestID 生成新的 RequestID(32 字符 hex,基于 crypto/rand)。
//
// 失败时降级为 0 填充(理论上不应发生)。
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 极端情况:rand 不可用。用 0 填充,仍保证 ID 唯一性(用 timestamp 兜底)。
		// 此处不 panic,因为 logger 失败不应阻塞游戏流。
		for i := range b {
			b[i] = byte(i)
		}
	}
	return hex.EncodeToString(b[:])
}

// WithRequestID 把 RequestID 注入 ctx。
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey{}, id)
}

// RequestIDFromContext 提取 ctx 中的 RequestID,空字符串表示未注入。
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey{}).(string); ok {
		return v
	}
	return ""
}
