// Package wwtypes — short_memory_20260813_test.go: 短期记忆 ring buffer 单测。
//
// 2026-08-13 §20260813-01 优化: 验证容量控制 + 关键事件永不淘汰 +
// 关键事件去重 + 并发安全 + Snapshot 排序。
package wwtypes

import (
	"sync"
	"testing"
	"time"
)

func mkEvent(kind string, actor, target int, round int, at int64) ShortMemoryEvent {
	return ShortMemoryEvent{
		At:      at,
		Kind:    kind,
		Actor:   actor,
		Target:  target,
		Round:   round,
		Phase:   "phase_test",
		Summary: "test",
	}
}

func TestShortMemory_AddAndSnapshot(t *testing.T) {
	b := NewShortMemoryBuffer("room1", 0, 10)
	b.AddEvent(mkEvent(ShortMemKindSpeak, 0, -1, 1, 1000))
	b.AddEvent(mkEvent(ShortMemKindVote, 0, 3, 1, 2000))
	b.AddEvent(mkEvent(ShortMemKindSpeak, 1, -1, 1, 1500))

	if got := b.Size(); got != 3 {
		t.Fatalf("Size = %d, want 3", got)
	}

	snap := b.Snapshot()
	// 排序后:1000, 1500, 2000
	if snap[0].At != 1000 || snap[1].At != 1500 || snap[2].At != 2000 {
		t.Fatalf("Snapshot not sorted by At: %+v", snap)
	}
}

func TestShortMemory_KeyEventNeverEvicted(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 3)
	b.AddEvent(mkEvent(ShortMemKindDeath, 0, -1, 1, 1000)) // 关键
	b.AddEvent(mkEvent(ShortMemKindSpeak, 1, -1, 1, 2000))
	b.AddEvent(mkEvent(ShortMemKindSpeak, 2, -1, 1, 3000))
	b.AddEvent(mkEvent(ShortMemKindSpeak, 3, -1, 1, 4000)) // 触发 FIFO 淘汰

	// 死亡事件应仍在
	snap := b.Snapshot()
	found := false
	for _, e := range snap {
		if e.Kind == ShortMemKindDeath && e.Actor == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("Key event (death) should never be evicted, snap = %+v", snap)
	}
}

func TestShortMemory_NonKeyFIFOEviction(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 2)
	b.AddEvent(mkEvent(ShortMemKindSpeak, 0, -1, 1, 1000)) // 普通
	b.AddEvent(mkEvent(ShortMemKindSpeak, 1, -1, 1, 2000)) // 普通
	b.AddEvent(mkEvent(ShortMemKindSpeak, 2, -1, 1, 3000)) // 触发淘汰,挤掉 0

	snap := b.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("len(snap) = %d, want 2", len(snap))
	}
	for _, e := range snap {
		if e.Actor == 0 {
			t.Fatalf("oldest non-key event should be evicted, but actor=0 found: %+v", e)
		}
	}
}

func TestShortMemory_KeyEventDedupe(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 10)
	b.AddEvent(mkEvent(ShortMemKindDeath, 0, -1, 1, 1000))
	b.AddEvent(mkEvent(ShortMemKindDeath, 0, -1, 1, 1000)) // 重复
	b.AddEvent(mkEvent(ShortMemKindDeath, 0, -1, 1, 1000)) // 重复

	if got := b.Size(); got != 1 {
		t.Fatalf("dedupe failed: Size = %d, want 1", got)
	}
}

func TestShortMemory_FilterByActor(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 10)
	b.AddEvent(mkEvent(ShortMemKindVote, 0, 3, 1, 1000))   // 0 投 3
	b.AddEvent(mkEvent(ShortMemKindVote, 1, 3, 1, 2000))   // 1 投 3
	b.AddEvent(mkEvent(ShortMemKindDeath, 0, -1, 1, 3000)) // 0 死
	b.AddEvent(mkEvent(ShortMemKindSpeak, 2, -1, 1, 4000))  // 无关

	got := b.FilterByActor(0)
	if len(got) != 2 {
		t.Fatalf("FilterByActor(0) = %d, want 2 (vote + death)", len(got))
	}
	got2 := b.FilterByActor(3)
	if len(got2) != 2 {
		t.Fatalf("FilterByActor(3) = %d, want 2 (target of 2 votes)", len(got2))
	}
}

func TestShortMemory_Clear(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 10)
	b.AddEvent(mkEvent(ShortMemKindSpeak, 0, -1, 1, 1000))
	b.Clear()
	if got := b.Size(); got != 0 {
		t.Fatalf("Size after Clear = %d, want 0", got)
	}
}

func TestShortMemory_AtDefaultToNow(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 10)
	before := time.Now().UnixMilli()
	b.AddEvent(ShortMemoryEvent{Kind: ShortMemKindSpeak, Actor: 0})
	after := time.Now().UnixMilli()

	snap := b.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("len = %d, want 1", len(snap))
	}
	if snap[0].At < before || snap[0].At > after {
		t.Fatalf("At = %d, want in [%d, %d]", snap[0].At, before, after)
	}
}

func TestShortMemory_ConcurrentSafe(t *testing.T) {
	b := NewShortMemoryBuffer("r", 0, 100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			b.AddEvent(mkEvent(ShortMemKindSpeak, i%5, -1, 1, int64(i*100)))
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = b.Snapshot()
			_ = b.FilterByActor(i % 5)
		}(i)
	}
	wg.Wait()
	// 不 panic 即通过
}

func TestShortMemory_NilSafe(t *testing.T) {
	var b *ShortMemoryBuffer
	b.AddEvent(ShortMemoryEvent{Kind: ShortMemKindSpeak}) // 不应 panic
	if got := b.Size(); got != 0 {
		t.Fatalf("nil buffer Size = %d, want 0", got)
	}
	if snap := b.Snapshot(); snap != nil {
		t.Fatalf("nil buffer Snapshot = %v, want nil", snap)
	}
	if got := b.RoomID(); got != "" {
		t.Fatalf("nil buffer RoomID = %q, want \"\"", got)
	}
	if got := b.Seat(); got != 0 {
		t.Fatalf("nil buffer Seat = %d, want 0", got)
	}
}
