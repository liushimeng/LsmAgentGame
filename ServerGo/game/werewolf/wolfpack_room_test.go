// Package werewolf — wolfpack_room_test.go: WolfPackRoom 单元测试（v4 §13.1）。
//
// 覆盖：
//  1. Append 接收狼成员 + 拒绝非成员（ErrWolfPackNotMember）
//  2. Append 长度超限（ErrWolfPackMsgTooLong）
//  3. Snapshot 按 maxN 截断,顺序按时间正序
//  4. PurgeByDeath 清理死亡狼的所有留言
//  5. 并发安全（多 goroutine 写无 panic）
package werewolf

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestWolfPackRoom_AppendAsMember(t *testing.T) {
	w := NewWolfPackRoom("room-1", 10)
	w.SetMembers([]int{1, 3, 5})
	if err := w.Append(3, "uid-3", "今晚刀 7 号"); err != nil {
		t.Fatalf("Append as member should succeed, got: %v", err)
	}
	if w.Len() != 1 {
		t.Fatalf("Len after 1 Append should be 1, got %d", w.Len())
	}
}

func TestWolfPackRoom_AppendRejectsNonMember(t *testing.T) {
	w := NewWolfPackRoom("room-1", 10)
	w.SetMembers([]int{1, 3, 5})
	err := w.Append(2, "uid-2", "伪装混入")
	if !errors.Is(err, ErrWolfPackNotMember) {
		t.Fatalf("Append as non-member should return ErrWolfPackNotMember, got: %v", err)
	}
	if w.Len() != 0 {
		t.Fatalf("Len after rejected Append should be 0, got %d", w.Len())
	}
}

func TestWolfPackRoom_AppendRejectsTooLong(t *testing.T) {
	w := NewWolfPackRoom("room-1", 10)
	w.SetMembers([]int{1})
	longText := ""
	for i := 0; i < WolfPackMsgLenMax+1; i++ {
		longText += "字"
	}
	err := w.Append(1, "uid-1", longText)
	if !errors.Is(err, ErrWolfPackMsgTooLong) {
		t.Fatalf("Append with >80 chars should return ErrWolfPackMsgTooLong, got: %v", err)
	}
}

func TestWolfPackRoom_AppendAcceptsMaxLen(t *testing.T) {
	w := NewWolfPackRoom("room-1", 10)
	w.SetMembers([]int{1})
	maxText := ""
	for i := 0; i < WolfPackMsgLenMax; i++ {
		maxText += "字"
	}
	if err := w.Append(1, "uid-1", maxText); err != nil {
		t.Fatalf("Append at exactly %d chars should succeed, got: %v", WolfPackMsgLenMax, err)
	}
}

func TestWolfPackRoom_SnapshotTruncates(t *testing.T) {
	w := NewWolfPackRoom("room-1", 50)
	w.SetMembers([]int{1, 2})
	for i := 0; i < 30; i++ {
		if err := w.Append(1, "uid-1", fmt.Sprintf("msg-%d", i)); err != nil {
			t.Fatalf("Append #%d failed: %v", i, err)
		}
	}
	snap := w.Snapshot(10)
	if len(snap) != 10 {
		t.Fatalf("Snapshot(10) should return 10 msgs, got %d", len(snap))
	}
	// 后入的在尾部 → 应该看到最新 10 条
	if snap[0].Text != "msg-20" || snap[9].Text != "msg-29" {
		t.Fatalf("Snapshot should be last 10 in chronological order, got: %s..%s", snap[0].Text, snap[9].Text)
	}
}

func TestWolfPackRoom_PurgeByDeath(t *testing.T) {
	w := NewWolfPackRoom("room-1", 50)
	w.SetMembers([]int{1, 2, 3})
	// seat 1 发 3 条, seat 2 发 2 条
	for i := 0; i < 3; i++ {
		_ = w.Append(1, "uid-1", "a狼msg")
	}
	for i := 0; i < 2; i++ {
		_ = w.Append(2, "uid-2", "b狼msg")
	}
	if w.Len() != 5 {
		t.Fatalf("setup: expected 5 msgs, got %d", w.Len())
	}
	purged := w.PurgeByDeath([]int{1})
	if purged != 3 {
		t.Fatalf("expected to purge 3 msgs from seat 1, got %d", purged)
	}
	if w.Len() != 2 {
		t.Fatalf("after purge: expected 2 msgs, got %d", w.Len())
	}
	// seat 1 不再是成员
	if w.IsMember(1) {
		t.Fatalf("seat 1 should be removed from members after PurgeByDeath")
	}
}

func TestWolfPackRoom_MaxLenFIFOEviction(t *testing.T) {
	w := NewWolfPackRoom("room-1", 5)
	w.SetMembers([]int{1})
	for i := 0; i < 10; i++ {
		_ = w.Append(1, "uid-1", fmt.Sprintf("msg-%d", i))
	}
	if w.Len() != 5 {
		t.Fatalf("FIFO: expected len=5 after 10 appends, got %d", w.Len())
	}
	snap := w.Snapshot(0)
	if snap[0].Text != "msg-5" || snap[4].Text != "msg-9" {
		t.Fatalf("FIFO should keep last 5, got: %s..%s", snap[0].Text, snap[4].Text)
	}
}

func TestWolfPackRoom_ConcurrentSafety(t *testing.T) {
	w := NewWolfPackRoom("room-1", 1000)
	w.SetMembers([]int{1, 2, 3})
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(seat int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = w.Append(seat, fmt.Sprintf("uid-%d", seat), fmt.Sprintf("g%d-i%d", seat, i))
			}
		}(g%3 + 1)
	}
	wg.Wait()
	if w.Len() != 200 {
		t.Fatalf("concurrent: expected 200 msgs, got %d", w.Len())
	}
}

func TestWolfPackRoom_NilReceiver(t *testing.T) {
	var w *WolfPackRoom
	if err := w.Append(1, "uid", "x"); err == nil {
		t.Fatal("nil Append should error")
	}
	if got := w.Snapshot(10); got != nil {
		t.Fatalf("nil Snapshot should return nil, got %v", got)
	}
	if w.Len() != 0 {
		t.Fatalf("nil Len should return 0, got %d", w.Len())
	}
}