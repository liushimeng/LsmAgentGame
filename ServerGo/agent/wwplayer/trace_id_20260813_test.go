// Package wwplayer — trace_id_20260813_test.go: RequestID 单测。
//
// 2026-08-13 §20260813-01 优化: 验证唯一性 + ctx 传递 + nil 安全。
package wwplayer

import (
	"context"
	"testing"
)

func TestRequestID_Unique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewRequestID()
		if len(id) != 32 {
			t.Fatalf("RequestID length = %d, want 32", len(id))
		}
		if seen[id] {
			t.Fatalf("RequestID collision: %q", id)
		}
		seen[id] = true
	}
}

func TestRequestID_HexFormat(t *testing.T) {
	id := NewRequestID()
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("RequestID contains non-hex char: %q", c)
		}
	}
}

func TestRequestID_ContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := RequestIDFromContext(ctx); got != "" {
		t.Fatalf("RequestIDFromContext(empty ctx) = %q, want \"\"", got)
	}

	id := NewRequestID()
	ctx2 := WithRequestID(ctx, id)
	if got := RequestIDFromContext(ctx2); got != id {
		t.Fatalf("RequestIDFromContext = %q, want %q", got, id)
	}

	// 原始 ctx 不应受影响
	if got := RequestIDFromContext(ctx); got != "" {
		t.Fatalf("original ctx was mutated: %q", got)
	}
}

func TestRequestID_EmptyStringNoop(t *testing.T) {
	ctx := context.Background()
	ctx2 := WithRequestID(ctx, "") // 空字符串应当是 no-op
	if got := RequestIDFromContext(ctx2); got != "" {
		t.Fatalf("WithRequestID(\"\") should be no-op, got %q", got)
	}
	// ctx2 应等价于 ctx
	if ctx2 != ctx {
		t.Fatal("WithRequestID(\"\") should return same ctx")
	}
}

func TestRequestID_NilContextSafe(t *testing.T) {
	//nolint:staticcheck // 故意测试 nil ctx
	if got := RequestIDFromContext(nil); got != "" {
		t.Fatalf("RequestIDFromContext(nil) = %q, want \"\"", got)
	}
}
