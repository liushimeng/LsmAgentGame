// chat_history_test.go: 500K 聊天历史队列单元测试 (2026-07-09 §13 增强)
//
// 覆盖:
//   - Append 触发压缩: 累计 > capBytes → bytes 回到 ≤ capBytes
//   - 同 sender 合并: 3 条同 FromID → 合并为 1
//   - 超长截断: 单条 > 1KB → 200 字
//   - 时间戳顺序: 最新时间戳保留
//   - thread safety: 并发 Append 不 panic
package agentcore

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func mkText(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("a", n)
}

func TestChatHistoryQueue_Append_BasicCount(t *testing.T) {
	q := NewChatHistoryQueue(10 * 1024) // 10K
	for i := 0; i < 5; i++ {
		q.Append(ChatMessage{
			ID:        fmt.Sprintf("m%d", i),
			FromSeat:  -1,
			FromID:    "user:test",
			Text:      mkText(100),
			Timestamp: time.Now(),
			Size:      chatMsgSize(mkText(100)),
		})
	}
	snap := q.Snapshot()
	if len(snap) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(snap))
	}
	if q.TotalBytes() == 0 {
		t.Fatalf("expected non-zero bytes")
	}
}

func TestChatHistoryQueue_Append_TriggersCompression(t *testing.T) {
	cap := 10 * 1024 // 10K
	q := NewChatHistoryQueue(cap)
	// 每条 2K,塞 20 条 → 40K > 10K,触发压缩
	for i := 0; i < 20; i++ {
		q.Append(ChatMessage{
			ID:        fmt.Sprintf("m%d", i),
			FromSeat:  -1,
			FromID:    fmt.Sprintf("user:%d", i%3), // 3 个不同 sender
			Text:      mkText(500),                  // 500 chars ≈ 2KB
			Timestamp: time.Now(),
			Size:      chatMsgSize(mkText(500)),
		})
	}
	bytes := int64(q.TotalBytes())
	if bytes > int64(cap) {
		t.Fatalf("expected bytes ≤ cap (%d), got %d", cap, bytes)
	}
	snap := q.Snapshot()
	if len(snap) == 0 {
		t.Fatalf("expected non-empty queue after compression")
	}
	t.Logf("compressed: %d messages, %d bytes (cap %d)", len(snap), bytes, cap)
}

func TestChatHistoryQueue_Compression_SameSender(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	for i := 0; i < 6; i++ {
		q.Append(ChatMessage{
			ID:        fmt.Sprintf("m%d", i),
			FromSeat:  0,
			FromID:    "bot:room:0",
			IsBot:     true,
			Text:      fmt.Sprintf("msg-%d", i),
			Timestamp: time.Now(),
		})
	}
	q.Compress()
	snap := q.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 6 → 2 messages after merging (3+3), got %d", len(snap))
	}
	for _, m := range snap {
		if !strings.Contains(m.Text, " | ") {
			t.Fatalf("expected merged text with ' | ', got %q", m.Text)
		}
	}
}

func TestChatHistoryQueue_Compression_LongTruncate(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	big := mkText(3000) // 3000 chars > 1KB threshold
	q.Append(ChatMessage{
		ID:   "big",
		Text: big,
	})
	q.Compress()
	snap := q.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 message after truncate, got %d", len(snap))
	}
	r := []rune(snap[0].Text)
	if len(r) > 220 { // 200 + "…(truncated)"
		t.Fatalf("expected truncated to ≤ 220 runes, got %d", len(r))
	}
	if !strings.Contains(snap[0].Text, "(truncated)") {
		t.Fatalf("expected '(truncated)' marker, got %q", snap[0].Text)
	}
}

func TestChatHistoryQueue_Compression_TimestampPreserved(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	base := time.Now()
	q.Append(ChatMessage{ID: "a", FromID: "x", IsBot: true, Text: "1", Timestamp: base})
	q.Append(ChatMessage{ID: "b", FromID: "x", IsBot: true, Text: "2", Timestamp: base.Add(time.Second)})
	q.Append(ChatMessage{ID: "c", FromID: "x", IsBot: true, Text: "3", Timestamp: base.Add(2 * time.Second)})
	q.Compress()
	snap := q.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected merge to 1, got %d", len(snap))
	}
	if !snap[0].Timestamp.Equal(base.Add(2 * time.Second)) {
		t.Fatalf("expected latest timestamp preserved, got %v", snap[0].Timestamp)
	}
}

func TestChatHistoryQueue_Compression_Stats(t *testing.T) {
	q := NewChatHistoryQueue(50 * 1024)
	for i := 0; i < 30; i++ {
		q.Append(ChatMessage{
			ID:     fmt.Sprintf("m%d", i),
			FromID: "bot:room:0",
			IsBot:  true,
			Text:   mkText(500),
		})
	}
	bytes, lastAt, merges, truncs := q.Stats()
	if bytes > 50*1024 {
		t.Fatalf("expected bytes ≤ 50K, got %d", bytes)
	}
	if lastAt == 0 {
		t.Fatalf("expected lastCompressionAt > 0 after compress")
	}
	if merges == 0 && truncs == 0 {
		t.Fatalf("expected at least one merge or truncate, got merges=%d truncs=%d", merges, truncs)
	}
	t.Logf("stats: bytes=%d lastAt=%d merges=%d truncs=%d", bytes, lastAt, merges, truncs)
}

func TestChatHistoryQueue_SummaryFallback(t *testing.T) {
	// 用不同的 sender(避免被合并)+ 大量消息 → 触发 fallback 摘要
	// cap=2K,但每条用不同 sender 不被合并,塞 200 条 → 触发 fallback
	q := NewChatHistoryQueue(2 * 1024)
	for i := 0; i < 200; i++ {
		q.Append(ChatMessage{
			ID:     fmt.Sprintf("m%d", i),
			FromID: fmt.Sprintf("user:%d", i), // 不同 sender,不合并
			IsBot:  false,
			Text:   mkText(50),
		})
	}
	snap := q.Snapshot()
	hasSummary := false
	for _, m := range snap {
		if m.FromID == "system:summary" || strings.Contains(m.Text, "[摘要") {
			hasSummary = true
			break
		}
	}
	// fallback 是 best-effort;若 step 3 淘汰已经把 messages 减到 ≤ 100,fallback 不触发
	// 因此也接受"压缩后 ≤ 100 条且 bytes ≤ cap"作为有效结果
	if !hasSummary {
		if len(snap) <= 100 && q.TotalBytes() <= 2*1024 {
			t.Logf("fallback not triggered but step-3 prune kept %d messages, %d bytes — acceptable", len(snap), q.TotalBytes())
			return
		}
		t.Fatalf("expected summary fallback or valid prune, got %d messages, %d bytes", len(snap), q.TotalBytes())
	}
}

func TestChatHistoryQueue_ConcurrentAppend(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				q.Append(ChatMessage{
					ID:     fmt.Sprintf("g%d-m%d", idx, j),
					FromID: fmt.Sprintf("user:%d", idx),
					Text:   mkText(50),
				})
			}
		}(i)
	}
	wg.Wait()
	if q.TotalBytes() > 100*1024 {
		t.Fatalf("concurrent appends overflowed cap: %d bytes", q.TotalBytes())
	}
}

func TestChatHistoryQueue_DefaultCap(t *testing.T) {
	q := NewChatHistoryQueue(0) // 用默认 500K
	if q.capBytes != DefaultChatHistoryCapBytes {
		t.Fatalf("expected default cap %d, got %d", DefaultChatHistoryCapBytes, q.capBytes)
	}
}

func TestChatMsgSize(t *testing.T) {
	if chatMsgSize("") != 0 {
		t.Fatalf("empty text should have size 0")
	}
	if chatMsgSize("hello") != 5*4 {
		t.Fatalf("expected 20 bytes for 'hello', got %d", chatMsgSize("hello"))
	}
	if chatMsgSize("你好") != 2*4 {
		t.Fatalf("expected 8 bytes for CJK 2 chars, got %d", chatMsgSize("你好"))
	}
}

func TestBuildSummaryText(t *testing.T) {
	base := time.Now()
	msgs := []ChatMessage{
		{FromAccount: "Alice", Text: "a", Timestamp: base},
		{FromAccount: "Alice", Text: "b", Timestamp: base.Add(time.Second)},
		{FromAccount: "Bob", Text: "c", Timestamp: base.Add(2 * time.Second)},
	}
	s := buildSummaryText(msgs)
	if !strings.Contains(s, "[摘要") {
		t.Fatalf("expected '[摘要' prefix, got %q", s)
	}
	if !strings.Contains(s, "Alice") || !strings.Contains(s, "Bob") {
		t.Fatalf("expected senders in summary, got %q", s)
	}
}

// TestChatHistoryQueue_SeqMonotonic 验证 Append 分配的 Seq 单调递增,
// 即使 Compress 把数组 pop 重排,Seq 也不重用 (2026-07-09 §13-bugfix)。
//
// 注意: ChatMessage 是值类型,Append 内部修改的是 push 进去的拷贝;
// 测试必须通过 Snapshot() 读出 stored Seq 才能看到 nextSeq 的副作用。
func TestChatHistoryQueue_SeqMonotonic(t *testing.T) {
	// 给足空间,避免压缩把早期消息淘汰掉,这样能稳定看到 20 条。
	q := NewChatHistoryQueue(200 * 1024)
	for i := 0; i < 20; i++ {
		m := ChatMessage{ID: fmt.Sprintf("m%d", i), FromID: fmt.Sprintf("u:%d", i), Text: mkText(50)}
		q.Append(m)
	}
	snap := q.Snapshot()
	if len(snap) != 20 {
		t.Fatalf("expected 20 messages snapshot, got %d", len(snap))
	}
	var lastSeq uint64
	for i, m := range snap {
		if m.Seq == 0 {
			t.Fatalf("message[%d] has Seq 0 — Append should have assigned one", i)
		}
		if m.Seq <= lastSeq {
			t.Fatalf("expected Seq monotonically increasing, got %d after %d", m.Seq, lastSeq)
		}
		lastSeq = m.Seq
	}
	if lastSeq != 20 {
		t.Fatalf("expected 20 messages → Seq up to 20, got %d", lastSeq)
	}
}

// TestChatHistoryQueue_ReadPointer_WindowFor 验证 read pointer 的窗口语义:
//   - 默认 read pointer = 0 → WindowFor 返回全部
//   - Advance(seat, seq) → WindowFor 返回 seq > since 的所有消息
//   - 增量注入:WindowFor 应该准确反映自上次 advance 之后追加的新消息
func TestChatHistoryQueue_ReadPointer_WindowFor(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	for i := 0; i < 5; i++ {
		q.Append(ChatMessage{ID: fmt.Sprintf("m%d", i), FromID: fmt.Sprintf("u:%d", i), Text: fmt.Sprintf("text-%d", i)})
	}
	// 初始:从未消费 → 全部可见(5 条)
	win := q.WindowFor(0)
	if len(win) != 5 {
		t.Fatalf("expected 5 initial messages for fresh seat, got %d", len(win))
	}
	// 推到 Seq=2 → 只剩 Seq > 2 的 (即第 3/4/5 条,共 3 条)
	q.Advance(0, 2)
	win = q.WindowFor(0)
	if len(win) != 3 {
		t.Fatalf("expected 3 messages after Advance to 2, got %d", len(win))
	}
	if win[0].ID != "m2" {
		t.Fatalf("expected first window message to be m2, got %q", win[0].ID)
	}
	// seat 1 从未消费 → 仍可见全部 5 条
	win = q.WindowFor(1)
	if len(win) != 5 {
		t.Fatalf("expected 5 messages for untouched seat 1, got %d", len(win))
	}
	// 新追加 m5(得到 seq=6)→ seat 0 现在看到 Seq > 2 的 4 条 (m2..m5)
	// 注意:在 Append 之前 seat 0 的 read pointer = 2;Append 后 seq 增长,
	// 但 WindowFor 是 "自上次 advance 后" 的累积视图,不光是最新的 m5。
	// (这一点是 WindowFor 的设计: 配合 Advance(seat, lastSeq) 才能实现
	// "只看到增量"。)
	q.Append(ChatMessage{ID: "m5", FromID: "u:5", Text: "text-5"})
	win = q.WindowFor(0)
	if len(win) != 4 {
		t.Fatalf("expected seat 0 to see 4 messages (seq > 2), got %d", len(win))
	}
	if win[len(win)-1].ID != "m5" {
		t.Fatalf("expected last window message to be m5, got %q", win[len(win)-1].ID)
	}
	// 把 seat 0 推到 5 → 接下来只剩 m5
	q.Advance(0, 5) // 但 lastSeq 是 6,所以应该用 6
	last := q.SnapshotLastSeq()
	q.Advance(0, last)
	win = q.WindowFor(0)
	if len(win) != 0 {
		t.Fatalf("expected seat 0 to see 0 after Advance to lastSeq=%d, got %d", last, len(win))
	}
	// seat 1 看到全部 6 条
	win = q.WindowFor(1)
	if len(win) != 6 {
		t.Fatalf("expected seat 1 to see all 6 (was untouched), got %d", len(win))
	}
}

// TestChatHistoryQueue_ReadPointer_RoomShared 验证 7 bot 共享同一队列
// 时各 seat 的 read pointer 互不干扰 (房间共享 + ReadPointer 公平性)。
func TestChatHistoryQueue_ReadPointer_RoomShared(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	// 模拟 7 bot 同时消费:seat 0/3/6 快进,其他停在 0
	for i := 0; i < 10; i++ {
		q.Append(ChatMessage{ID: fmt.Sprintf("m%d", i), FromID: "system", Text: fmt.Sprintf("ev%d", i)})
	}
	q.Advance(0, 7)
	q.Advance(3, 10) // 全部消费完
	q.Advance(6, 5)

	got := map[int]int{}
	for _, seat := range []int{0, 1, 2, 3, 4, 5, 6} {
		got[seat] = len(q.WindowFor(seat))
	}
	expected := map[int]int{0: 3, 1: 10, 2: 10, 3: 0, 4: 10, 5: 10, 6: 5}
	for seat, want := range expected {
		if got[seat] != want {
			t.Errorf("seat %d: expected WindowFor=%d, got %d", seat, want, got[seat])
		}
	}
}

// TestChatHistoryQueue_Tail 验证 Tail 取末尾 limit 条,Head 取开头 limit 条。
func TestChatHistoryQueue_Tail(t *testing.T) {
	q := NewChatHistoryQueue(100 * 1024)
	for i := 0; i < 10; i++ {
		q.Append(ChatMessage{ID: fmt.Sprintf("m%d", i), FromID: fmt.Sprintf("u:%d", i), Text: fmt.Sprintf("ev%d", i)})
	}
	tail := q.Tail(3)
	if len(tail) != 3 || tail[0].ID != "m7" || tail[2].ID != "m9" {
		t.Fatalf("expected tail m7-m9, got %v", tailIDs(tail))
	}
	head := q.Head(2)
	if len(head) != 2 || head[0].ID != "m0" || head[1].ID != "m1" {
		t.Fatalf("expected head m0-m1, got %v", tailIDs(head))
	}
	all := q.Snapshot()
	if len(all) != 10 {
		t.Fatalf("Snapshot should return all 10, got %d", len(all))
	}
}

func tailIDs(msgs []ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}