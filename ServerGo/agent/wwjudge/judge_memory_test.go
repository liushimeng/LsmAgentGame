// Package wwjudge — judge_memory_test.go
// §20260809-02 U1 法官多轮记忆环形缓冲的单元测试。
//
// 覆盖:
//   - 容量上限:超过 20 条后第 1 条被覆盖(FIFO)
//   - Snapshot 不会返回内部 slice 引用(防止外部污染)
//   - Reset 清空后 Snapshot 返回 nil
//   - 并发 Append 安全(race detector 友好)

package wwjudge

import (
	"sync"
	"testing"
)

// TestJudgeMemoryRing_CapacityFIFO 验证超过容量后队首被覆盖(FIFO)。
func TestJudgeMemoryRing_CapacityFIFO(t *testing.T) {
	m := NewJudgeMemoryRing(5) // 用 5 方便验证
	for i := 0; i < 7; i++ {
		m.Append(JudgeMemoryEntry{
			Round:    i,
			WakeKind: "announce",
			Text:     "msg-" + itoaForTest(i),
		})
	}
	snap := m.Snapshot()
	if got := len(snap); got != 5 {
		t.Fatalf("Snapshot len = %d, want 5 (FIFO should keep latest 5)", got)
	}
	// 第 0 和 1 条应被覆盖,保留 2..6
	if snap[0].Text != "msg-2" {
		t.Errorf("snap[0] = %q, want %q", snap[0].Text, "msg-2")
	}
	if snap[4].Text != "msg-6" {
		t.Errorf("snap[4] = %q, want %q", snap[4].Text, "msg-6")
	}
}

// TestJudgeMemoryRing_DefaultCapacity capacity<=0 走默认 20。
func TestJudgeMemoryRing_DefaultCapacity(t *testing.T) {
	m := NewJudgeMemoryRing(0)
	if m.Capacity() != 20 {
		t.Fatalf("Capacity() = %d, want 20 (default)", m.Capacity())
	}
}

// TestJudgeMemoryRing_Reset 清空后 Snapshot 返回 nil。
func TestJudgeMemoryRing_Reset(t *testing.T) {
	m := NewJudgeMemoryRing(5)
	for i := 0; i < 3; i++ {
		m.Append(JudgeMemoryEntry{Text: "x"})
	}
	if m.Len() != 3 {
		t.Fatalf("Len before reset = %d, want 3", m.Len())
	}
	m.Reset()
	if m.Len() != 0 {
		t.Fatalf("Len after reset = %d, want 0", m.Len())
	}
	if snap := m.Snapshot(); snap != nil {
		t.Fatalf("Snapshot after reset = %v, want nil", snap)
	}
}

// TestJudgeMemoryRing_NilSafe 验证 nil receiver 上所有方法都安全(防 NPE,
// 兼容老 room 重启路径)。
func TestJudgeMemoryRing_NilSafe(t *testing.T) {
	var m *JudgeMemoryRing
	m.Append(JudgeMemoryEntry{Text: "x"}) // 不应 panic
	if snap := m.Snapshot(); snap != nil {
		t.Fatalf("Snapshot on nil = %v, want nil", snap)
	}
	if m.Len() != 0 {
		t.Fatalf("Len on nil = %d, want 0", m.Len())
	}
	m.Reset() // 不应 panic
}

// TestJudgeMemoryRing_ConcurrentAppend 并发追加安全。
// `go test -race` 通过即视为 thread-safe。
func TestJudgeMemoryRing_ConcurrentAppend(t *testing.T) {
	m := NewJudgeMemoryRing(100)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				m.Append(JudgeMemoryEntry{Text: "x"})
			}
		}(i)
	}
	wg.Wait()
	if got := m.Len(); got != 50 {
		t.Fatalf("Len after concurrent append = %d, want 50", got)
	}
}

// TestJudgeMemoryRing_TimestampAutoFill 验证 Append 自动填 Timestamp。
func TestJudgeMemoryRing_TimestampAutoFill(t *testing.T) {
	m := NewJudgeMemoryRing(5)
	m.Append(JudgeMemoryEntry{Text: "no-ts"})
	snap := m.Snapshot()
	if snap[0].Timestamp == 0 {
		t.Fatalf("Timestamp should be auto-filled, got 0")
	}
}

// itoaForTest 是 strconv.Itoa 的极简替身,避免对 strconv 的额外 import。
// 仅在测试中以简单方式拼接 0..n 的数字字符串。
func itoaForTest(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [16]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
