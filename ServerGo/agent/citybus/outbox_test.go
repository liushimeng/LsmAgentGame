// Package citybus — outbox_test.go: 批次86 §3.4.1 outbox 单测。
//
// 覆盖:4 种 delivery 记录 / Snapshot 新→旧 + 副本语义 / limit /
// 环形 cap 200 淘汰最旧 / 零值可用 + nil receiver / 并发记录 /
// 落盘默认关 / 落盘追加 / 轮转保尾。
package citybus

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ─────────────────────────── 环形缓冲与快照 ───────────────────────────

// TestOutboxRecordAndSnapshotOrder 记录 4 种 delivery,Snapshot 新→旧,
// Seq / At 自动填充,返回值为副本。
func TestOutboxRecordAndSnapshotOrder(t *testing.T) {
	o := NewOutbox()
	o.Record(OutboxRecord{From: "sys", To: "A", Type: "message", Delivery: OutboxSent})
	o.Record(OutboxRecord{From: "sys", To: "A", Type: "collect", Delivery: OutboxPosted})
	o.Record(OutboxRecord{From: "sys", To: "A", Type: "message", Delivery: OutboxDropped, ErrMsg: "post queue full"})
	o.Record(OutboxRecord{From: "sys", To: "A", Type: "message", Delivery: OutboxFailed, ErrMsg: "boom", Retries: 2})

	got := o.Snapshot(0)
	if len(got) != 4 {
		t.Fatalf("Snapshot len = %d, want 4", len(got))
	}
	// 新→旧:最后记录的 failed 在最前。
	want := []string{OutboxFailed, OutboxDropped, OutboxPosted, OutboxSent}
	for i, w := range want {
		if got[i].Delivery != w {
			t.Fatalf("Snapshot[%d].Delivery = %q, want %q", i, got[i].Delivery, w)
		}
	}
	// Seq 单调 4..1(新→旧),At 均自动填充。
	for i := 0; i < 4; i++ {
		if got[i].Seq != int64(4-i) {
			t.Fatalf("Snapshot[%d].Seq = %d, want %d", i, got[i].Seq, 4-i)
		}
		if got[i].At == 0 {
			t.Fatalf("Snapshot[%d].At = 0, want non-zero", i)
		}
	}
	if o.Seq() != 4 {
		t.Fatalf("Seq() = %d, want 4", o.Seq())
	}
	// 副本语义:修改返回值不得污染环内数据。
	got[0].ErrMsg = "mutated"
	if again := o.Snapshot(0); again[0].ErrMsg != "boom" {
		t.Fatalf("Snapshot must return copies, got ErrMsg = %q", again[0].ErrMsg)
	}
}

// TestOutboxSnapshotLimit limit > 0 时返回最新 limit 条(新→旧)。
func TestOutboxSnapshotLimit(t *testing.T) {
	o := NewOutbox()
	for i := 0; i < 5; i++ {
		o.Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent})
	}
	got := o.Snapshot(2)
	if len(got) != 2 {
		t.Fatalf("Snapshot(2) len = %d, want 2", len(got))
	}
	if got[0].Seq != 5 || got[1].Seq != 4 {
		t.Fatalf("Snapshot(2) seq = %d,%d, want 5,4", got[0].Seq, got[1].Seq)
	}
	// limit ≤ 0 返回全部。
	if all := o.Snapshot(0); len(all) != 5 {
		t.Fatalf("Snapshot(0) len = %d, want 5", len(all))
	}
}

// TestOutboxRingCap 超出 cap 200 时淘汰最旧(保留 seq 51..250)。
func TestOutboxRingCap(t *testing.T) {
	o := NewOutbox()
	const n = outboxCap + 50
	for i := 0; i < n; i++ {
		o.Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent})
	}
	got := o.Snapshot(0)
	if len(got) != outboxCap {
		t.Fatalf("Snapshot len = %d, want %d", len(got), outboxCap)
	}
	if got[0].Seq != int64(n) {
		t.Fatalf("newest Seq = %d, want %d", got[0].Seq, n)
	}
	if got[outboxCap-1].Seq != int64(n-outboxCap+1) {
		t.Fatalf("oldest Seq = %d, want %d", got[outboxCap-1].Seq, n-outboxCap+1)
	}
}

// TestOutboxZeroValue 零值 Outbox 可用;nil receiver no-op 不 panic。
func TestOutboxZeroValue(t *testing.T) {
	var o Outbox
	o.Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent})
	if o.Seq() != 1 {
		t.Fatalf("Seq() = %d, want 1", o.Seq())
	}
	if got := o.Snapshot(0); len(got) != 1 || got[0].Delivery != OutboxSent {
		t.Fatalf("Snapshot = %+v, want one sent record", got)
	}

	var nilO *Outbox
	nilO.Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent}) // no panic
	if got := nilO.Snapshot(0); got == nil || len(got) != 0 {
		t.Fatalf("nil Snapshot = %#v, want non-nil empty", got)
	}
	if nilO.Seq() != 0 {
		t.Fatalf("nil Seq() = %d, want 0", nilO.Seq())
	}
}

// TestOutboxConcurrentRecord 并发记录无 race,序号总数正确(-race 覆盖)。
func TestOutboxConcurrentRecord(t *testing.T) {
	o := NewOutbox()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				o.Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent})
			}
		}()
	}
	wg.Wait()
	if o.Seq() != 800 {
		t.Fatalf("Seq() = %d, want 800", o.Seq())
	}
	if got := o.Snapshot(0); len(got) != outboxCap {
		t.Fatalf("Snapshot len = %d, want %d", len(got), outboxCap)
	}
}

// ─────────────────────────── 落盘(可选,默认关)───────────────────────────

// TestOutboxPersistenceDisabledByDefault 默认不落盘(零回归)。
func TestOutboxPersistenceDisabledByDefault(t *testing.T) {
	o := NewOutbox()
	dir := t.TempDir()
	o.SetDir(dir)
	o.Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent})
	if _, err := os.Stat(filepath.Join(dir, outboxFileName)); !os.IsNotExist(err) {
		t.Fatalf("persistence must default off, stat err = %v", err)
	}
}

// TestSetOutboxPersistencePackageLevel 包级 SetOutboxDir /
// SetOutboxPersistence 接线:开启后写 JSONL,关闭后停写。
func TestSetOutboxPersistencePackageLevel(t *testing.T) {
	dir := t.TempDir()
	SetOutboxDir(dir)
	SetOutboxPersistence(true)
	t.Cleanup(func() {
		SetOutboxPersistence(false)
		SetOutboxDir("")
	})

	start := DefaultOutbox().Seq()
	DefaultOutbox().Record(OutboxRecord{From: "f", To: "t", Type: "m", Delivery: OutboxSent})
	recs := outboxSince(start)
	if len(recs) != 1 || recs[0].Delivery != OutboxSent {
		t.Fatalf("records = %+v, want one sent", recs)
	}

	data, err := os.ReadFile(filepath.Join(dir, outboxFileName))
	if err != nil {
		t.Fatalf("read outbox file: %v", err)
	}
	if !strings.Contains(string(data), `"delivery":"sent"`) {
		t.Fatalf("outbox file content = %q, want delivery sent", string(data))
	}
}

// TestRotateOutboxFile 行数超限时保尾 keepOutboxLines 原子重写。
func TestRotateOutboxFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, outboxFileName)
	var buf strings.Builder
	const total = maxOutboxLines + 5
	for i := 0; i < total; i++ {
		buf.WriteString(`{"seq":`)
		buf.WriteString(strconv.Itoa(i + 1))
		buf.WriteString(`,"delivery":"sent"}`)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := rotateOutboxFile(path); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rotated: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != keepOutboxLines {
		t.Fatalf("rotated lines = %d, want %d", len(lines), keepOutboxLines)
	}
	// 保尾:首行应为第 total-keepOutboxLines+1 条。
	wantFirst := strconv.Itoa(total - keepOutboxLines + 1)
	if !strings.Contains(lines[0], `"seq":`+wantFirst+`,`) {
		t.Fatalf("first rotated line = %q, want seq %s", lines[0], wantFirst)
	}
}
