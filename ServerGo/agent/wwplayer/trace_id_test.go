package wwplayer

import (
	"context"
	"sync"
	"testing"
)

// TestNewRunID_Unique 验证 RunID 在多次生成下不重复。
func TestNewRunID_Unique(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		id := NewRunID()
		if seen[id] {
			t.Fatalf("RunID collision at iteration %d: %s", i, id)
		}
		seen[id] = true
		if len(id) != 32+len("run_") {
			t.Fatalf("RunID len = %d, want %d", len(id), 32+len("run_"))
		}
	}
}

// TestRunIDContext_InjectExtract 验证 WithRunID + RunIDFromContext。
func TestRunIDContext_InjectExtract(t *testing.T) {
	id := NewRunID()
	ctx := WithRunID(context.Background(), id)
	got := RunIDFromContext(ctx)
	if got != id {
		t.Fatalf("RunIDFromContext got=%q want=%q", got, id)
	}
}

func TestRunIDContext_Empty(t *testing.T) {
	if RunIDFromContext(context.Background()) != "" {
		t.Fatalf("empty ctx should return empty RunID")
	}
	if RunIDFromContext(nil) != "" {
		t.Fatalf("nil ctx should return empty RunID")
	}
}

// TestWithRunID_EmptyNoOp 验证空 ID 不注入 ctx。
func TestWithRunID_EmptyNoOp(t *testing.T) {
	ctx := context.WithValue(context.Background(), "marker", "orig")
	out := WithRunID(ctx, "")
	if v, _ := ctx.Value("marker").(string); v != "orig" {
		t.Fatalf("WithRunID(\"\") should not modify ctx")
	}
	_ = out
}

// TestAgentRunTrace_NextSeq_Monotonic 验证 NextSeq 单调递增。
func TestAgentRunTrace_NextSeq_Monotonic(t *testing.T) {
	tr := NewAgentRunTrace(3, "room-7", "MeiTuan-model")
	if tr.RunID() == "" {
		t.Fatalf("RunID should not be empty")
	}
	if tr.Seat() != 3 {
		t.Fatalf("Seat = %d want 3", tr.Seat())
	}
	if tr.RoomID() != "room-7" {
		t.Fatalf("RoomID = %q want room-7", tr.RoomID())
	}
	if tr.ModelKey() != "MeiTuan-model" {
		t.Fatalf("ModelKey = %q want MeiTuan-model", tr.ModelKey())
	}
	if tr.StartedAt().IsZero() {
		t.Fatalf("StartedAt should be set")
	}

	s1 := tr.NextSeq()
	s2 := tr.NextSeq()
	s3 := tr.NextSeq()
	if !(s2 > s1 && s3 > s2) {
		t.Fatalf("NextSeq not monotonic: %d,%d,%d", s1, s2, s3)
	}
	if got := tr.CurrentSeq(); got != s3 {
		t.Fatalf("CurrentSeq = %d want %d", got, s3)
	}
}

// TestAgentRunTrace_NextSeq_Concurrent 验证并发安全(§92a 教训:trace_id 也可能并发访问)。
func TestAgentRunTrace_NextSeq_Concurrent(t *testing.T) {
	tr := NewAgentRunTrace(0, "r", "m")
	const goroutines = 16
	const incs = 1000
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incs; j++ {
				tr.NextSeq()
			}
		}()
	}
	wg.Wait()
	expected := uint64(goroutines * incs)
	if got := tr.CurrentSeq(); got != expected {
		t.Fatalf("CurrentSeq got=%d want=%d (concurrency loss)", got, expected)
	}
}

// TestAgentRunTrace_NilSafe 验证 nil 接收不 panic。
func TestAgentRunTrace_NilSafe(t *testing.T) {
	var tr *AgentRunTrace // nil
	if tr.RunID() != "" {
		t.Fatalf("nil trace should return empty")
	}
	if tr.Seat() != -1 {
		t.Fatalf("nil trace Seat() = %d want -1", tr.Seat())
	}
	if tr.NextSeq() != 0 {
		t.Fatalf("nil trace NextSeq() should return 0")
	}
	if NewStreamMarker(nil, "text", "", true) != nil {
		t.Fatalf("NewStreamMarker(nil) should return nil")
	}
}

// TestNewStreamMarker_Validity 验证 StreamMarker 字段非空。
func TestNewStreamMarker_Validity(t *testing.T) {
	tr := NewAgentRunTrace(5, "r", "m")
	m := NewStreamMarker(tr, "text", "block-1", true)
	if m == nil {
		t.Fatalf("marker should not be nil")
	}
	if m.RunID != tr.RunID() {
		t.Fatalf("RunID mismatch")
	}
	if m.Seat != 5 {
		t.Fatalf("Seat = %d want 5", m.Seat)
	}
	if m.Seq == 0 {
		t.Fatalf("Seq should be > 0")
	}
	if m.BlockKind != "text" {
		t.Fatalf("BlockKind mismatch")
	}
	if m.BlockID != "block-1" {
		t.Fatalf("BlockID mismatch")
	}
	if !m.Begin {
		t.Fatalf("Begin should be true")
	}

	// 调用 NextSeq 多分配一个序号,但 marker 自己已经占了。
	prior := tr.CurrentSeq()
	_ = tr.NextSeq()
	if tr.CurrentSeq() <= prior {
		t.Fatalf("NextSeq should advance")
	}
}
