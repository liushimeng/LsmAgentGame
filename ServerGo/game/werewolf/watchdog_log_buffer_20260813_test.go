// Package werewolf — watchdog_log_buffer_20260813_test.go: 单测。
//
// 2026-08-13 §20260813-01 优化: 验证容量控制 + Snapshot 排序 + nil 安全。
package werewolf

import (
	"sync"
	"testing"
	"time"
)

func TestWatchdogLog_AppendAndSize(t *testing.T) {
	b := &WatchdogLogBuffer{}
	b.Append(WatchdogLogEntry{Kind: WatchdogLogKindTick, Phase: "p1"})
	b.Append(WatchdogLogEntry{Kind: WatchdogLogKindSkip, Phase: "p2"})
	if got := b.Size(); got != 2 {
		t.Fatalf("Size = %d, want 2", got)
	}
}

func TestWatchdogLog_CapacityEviction(t *testing.T) {
	b := &WatchdogLogBuffer{}
	for i := 0; i < WatchdogLogMaxEntries+5; i++ {
		b.Append(WatchdogLogEntry{
			Kind:    WatchdogLogKindTick,
			Phase:   "p",
			At:      int64(i * 1000),
			Round:   i,
		})
	}
	if got := b.Size(); got != WatchdogLogMaxEntries {
		t.Fatalf("Size after overflow = %d, want %d", got, WatchdogLogMaxEntries)
	}
}

func TestWatchdogLog_SnapshotSorted(t *testing.T) {
	b := &WatchdogLogBuffer{}
	// 故意乱序追加
	b.Append(WatchdogLogEntry{Kind: "a", At: 3000})
	b.Append(WatchdogLogEntry{Kind: "b", At: 1000})
	b.Append(WatchdogLogEntry{Kind: "c", At: 2000})

	snap := b.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	if snap[0].At != 1000 || snap[1].At != 2000 || snap[2].At != 3000 {
		t.Fatalf("Snapshot not sorted: %+v", snap)
	}
}

func TestWatchdogLog_EvictionPreservesNewest(t *testing.T) {
	b := &WatchdogLogBuffer{}
	// 写满 + 1,确保最旧被淘汰,最新仍在
	for i := 0; i < WatchdogLogMaxEntries+1; i++ {
		b.Append(WatchdogLogEntry{Kind: "x", At: int64(i * 100)})
	}
	snap := b.Snapshot()
	// 最新一条 At = WatchdogLogMaxEntries * 100
	want := int64(WatchdogLogMaxEntries * 100)
	if snap[len(snap)-1].At != want {
		t.Fatalf("newest at = %d, want %d", snap[len(snap)-1].At, want)
	}
	// 最旧一条 At = 100
	if snap[0].At != 100 {
		t.Fatalf("oldest at = %d, want 100", snap[0].At)
	}
}

func TestWatchdogLog_Clear(t *testing.T) {
	b := &WatchdogLogBuffer{}
	b.Append(WatchdogLogEntry{Kind: "x"})
	b.Append(WatchdogLogEntry{Kind: "y"})
	b.Clear()
	if got := b.Size(); got != 0 {
		t.Fatalf("Size after Clear = %d, want 0", got)
	}
}

func TestWatchdogLog_DefaultAtToNow(t *testing.T) {
	b := &WatchdogLogBuffer{}
	before := timeUnixMs()
	b.Append(WatchdogLogEntry{Kind: "x"})
	after := timeUnixMs()

	snap := b.Snapshot()
	if snap[0].At < before || snap[0].At > after {
		t.Fatalf("At = %d, want in [%d, %d]", snap[0].At, before, after)
	}
}

func TestWatchdogLog_ConcurrentSafe(t *testing.T) {
	b := &WatchdogLogBuffer{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			b.Append(WatchdogLogEntry{Kind: "x", At: int64(i)})
		}(i)
		go func() {
			defer wg.Done()
			_ = b.Snapshot()
		}()
	}
	wg.Wait()
	// 不 panic 即通过
}

func TestWatchdogLog_NilSafe(t *testing.T) {
	var b *WatchdogLogBuffer
	b.Append(WatchdogLogEntry{Kind: "x"}) // 不应 panic
	if got := b.Size(); got != 0 {
		t.Fatalf("nil buffer Size = %d, want 0", got)
	}
	if snap := b.Snapshot(); snap != nil {
		t.Fatalf("nil buffer Snapshot = %v, want nil", snap)
	}
	b.Clear() // 不应 panic
}

func timeUnixMs() int64 {
	return time.Now().UnixMilli()
}
