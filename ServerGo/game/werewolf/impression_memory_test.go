// Package werewolf — impression_memory_test.go: 记忆印象 ImpressionStore 单测（§20260826-01 U2）。
//
// 测试矩阵:
//   T1: AddOrUpdateDimLocked 5 维累加
//   T2: 多事件累加 + 钳制到 [0, 1]
//   T3: 衰减:48h 后 trust 减半 (向 0.5 中性值收敛)
//   T4: SnapshotAllLocked 排序
//   T5: §92a — nil receiver 安全
package werewolf

import (
	"testing"
	"time"
)

// T1: 5 维累加基础路径。
func TestImpression_AddOrUpdate(t *testing.T) {
	s := NewImpressionStore()
	now := time.Now()
	s.AddOrUpdateDimLocked(0, 5, ImpressionDims{
		Trust: +0.3, Sincerity: -0.2, Threat: +0.1,
	}, "vote_accurate", now)

	mem := s.GetLocked(0, now)
	if mem == nil {
		t.Fatal("nil memory")
	}
	if len(mem.Entries) != 1 {
		t.Fatalf("entries=%d, want 1", len(mem.Entries))
	}
	e := mem.Entries[0]
	if e.TargetSeat != 5 {
		t.Errorf("target=%d, want 5", e.TargetSeat)
	}
	if e.Dims.Trust < 0.79 || e.Dims.Trust > 0.81 {
		t.Errorf("trust=%.4f, want ~0.8 (0.5+0.3)", e.Dims.Trust)
	}
	if e.Dims.Sincerity < 0.29 || e.Dims.Sincerity > 0.31 {
		t.Errorf("sincerity=%.4f, want ~0.3 (0.5-0.2)", e.Dims.Sincerity)
	}
	if e.EventCount != 1 {
		t.Errorf("event_count=%d, want 1", e.EventCount)
	}
	if len(e.SampleEvents) != 1 || e.SampleEvents[0] != "vote_accurate" {
		t.Errorf("sample_events=%v", e.SampleEvents)
	}
}

// T2: 多次累加 + 钳制。
func TestImpression_MultiAccumulateAndClamp(t *testing.T) {
	s := NewImpressionStore()
	now := time.Now()
	// 累加 5 次 +0.3 trust → 应钳制到 1.0
	for i := 0; i < 5; i++ {
		s.AddOrUpdateDimLocked(0, 3, ImpressionDims{Trust: +0.3}, "event", now)
	}
	mem := s.GetLocked(0, now)
	if mem == nil || len(mem.Entries) == 0 {
		t.Fatal("empty")
	}
	if mem.Entries[0].Dims.Trust != 1.0 {
		t.Errorf("trust=%.4f, want 1.0 (clamped)", mem.Entries[0].Dims.Trust)
	}
	// 累加 3 次 -0.3 sincerity → 应钳制到 0.0
	for i := 0; i < 3; i++ {
		s.AddOrUpdateDimLocked(0, 3, ImpressionDims{Sincerity: -0.3}, "event", now)
	}
	mem = s.GetLocked(0, now)
	if mem.Entries[0].Dims.Sincerity != 0.0 {
		t.Errorf("sincerity=%.4f, want 0.0 (clamped)", mem.Entries[0].Dims.Sincerity)
	}
}

// T3: 衰减 — lastUpdateMS 距 now 48h, trust 应回收到 ~0.5 (中性值)。
func TestImpression_Decay(t *testing.T) {
	s := NewImpressionStore()
	past := time.Now().Add(-48 * time.Hour)
	s.AddOrUpdateDimLocked(0, 5, ImpressionDims{
		Trust: +0.3, // → 0.8
	}, "init", past)

	// now = past + 48h → 衰减半衰期 → trust 应 ≈ 0.5 + (0.8 - 0.5) * 0.5 = 0.65
	mem := s.GetLocked(0, past.Add(48*time.Hour))
	if mem == nil {
		t.Fatal("nil")
	}
	t.Logf("after 48h decay: trust=%.4f (expected ~0.65)", mem.Entries[0].Dims.Trust)
	// 范围 [0.6, 0.7]
	if mem.Entries[0].Dims.Trust < 0.6 || mem.Entries[0].Dims.Trust > 0.7 {
		t.Errorf("decay trust=%.4f, want ~0.65", mem.Entries[0].Dims.Trust)
	}
}

// T4: SnapshotAllLocked 排序 + 多 bot。
func TestImpression_SnapshotAll(t *testing.T) {
	s := NewImpressionStore()
	now := time.Now()
	s.AddOrUpdateDimLocked(0, 5, ImpressionDims{Trust: +0.1}, "x", now)
	s.AddOrUpdateDimLocked(3, 5, ImpressionDims{Trust: +0.1}, "x", now)
	snap := s.SnapshotAllLocked(now)
	if len(snap) != 2 {
		t.Fatalf("snap=%d, want 2", len(snap))
	}
	if snap[0].Seat != 0 || snap[1].Seat != 3 {
		t.Errorf("seats=%d,%d, want 0,3", snap[0].Seat, snap[1].Seat)
	}
}

// T5: nil receiver 安全。
func TestImpression_NilReceiverSafe(t *testing.T) {
	var s *ImpressionStore
	if got := s.GetLocked(0, time.Now()); got != nil {
		t.Errorf("nil receiver should return nil, got %+v", got)
	}
}