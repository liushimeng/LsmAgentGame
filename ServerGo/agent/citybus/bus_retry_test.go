// Package citybus — bus_retry_test.go: 批次86 §3.4 单测。
//
// 覆盖:Send 成功 / 失败记 outbox;Post 入队 / 队列满丢弃记 outbox;
// postLoop handler 失败记 outbox;SendWithRetry 瞬时失败重试、3 次后放弃、
// 未注册不重试、退避尊重 ctx 取消;Hub.OutboxSnapshot 委派。
package citybus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// outboxSince 返回包级 outbox 中 Seq > start 的留痕(旧→新)。
//
// 包级 outbox 是全局单例,测试用序号切片隔离彼此的记录。
func outboxSince(start int64) []OutboxRecord {
	recs := DefaultOutbox().Snapshot(0) // 新→旧
	out := make([]OutboxRecord, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Seq > start {
			out = append(out, recs[i])
		}
	}
	return out
}

// TestSendRecordsOutbox Send 成功记 sent,失败(handler / 未注册)记 failed。
func TestSendRecordsOutbox(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	if err := h.Register("A", echoHandler("ok", nil)); err != nil {
		t.Fatalf("register A: %v", err)
	}
	reply, err := h.Send(context.Background(), "R1", "sys", "A", "message", nil)
	if err != nil || reply != "ok" {
		t.Fatalf("send = %v, %v", reply, err)
	}

	wantErr := errors.New("boom")
	if err := h.Register("B", echoHandler(nil, wantErr)); err != nil {
		t.Fatalf("register B: %v", err)
	}
	if _, err := h.Send(context.Background(), "R1", "sys", "B", "message", nil); !errors.Is(err, wantErr) {
		t.Fatalf("send B err = %v, want boom", err)
	}
	if _, err := h.Send(context.Background(), "R1", "sys", "missing", "message", nil); err == nil {
		t.Fatal("send to unregistered must fail")
	}

	recs := outboxSince(start)
	if len(recs) != 3 {
		t.Fatalf("outbox records = %+v, want 3", recs)
	}
	if recs[0].Delivery != OutboxSent || recs[0].To != "A" {
		t.Fatalf("rec[0] = %+v, want sent to A", recs[0])
	}
	if recs[1].Delivery != OutboxFailed || recs[1].ErrMsg != "boom" {
		t.Fatalf("rec[1] = %+v, want failed boom", recs[1])
	}
	if recs[2].Delivery != OutboxFailed || !strings.Contains(recs[2].ErrMsg, "agent not registered") {
		t.Fatalf("rec[2] = %+v, want failed not registered", recs[2])
	}

	// Hub.OutboxSnapshot 委派包级 outbox。
	found := false
	for _, r := range h.OutboxSnapshot(0) {
		if r.Seq > start && r.Delivery == OutboxSent {
			found = true
		}
	}
	if !found {
		t.Fatal("Hub.OutboxSnapshot must delegate to package outbox")
	}
}

// TestPostRecordsOutbox Post 入队记 posted,队列满记 dropped(不再静默)。
func TestPostRecordsOutbox(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	block := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	if err := h.Register("slow", func(ctx context.Context, msg Message) (any, error) {
		once.Do(func() { close(started) })
		<-block // 阻塞,让 postLoop 停在第一条
		return nil, nil
	}); err != nil {
		t.Fatalf("register slow: %v", err)
	}

	// 第 1 条入队并被 postLoop 取走(阻塞);再投 64 条填满队列,第 65 条丢弃。
	if err := h.Post(context.Background(), "R1", "sys", "slow", "message", 0); err != nil {
		t.Fatalf("first post: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("postLoop did not pick up first message")
	}
	for i := 0; i < postQueueCap+1; i++ {
		if err := h.Post(context.Background(), "R1", "sys", "slow", "message", i); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	close(block)

	recs := outboxSince(start)
	if len(recs) != postQueueCap+2 {
		t.Fatalf("outbox records len = %d, want %d", len(recs), postQueueCap+2)
	}
	posted, dropped := 0, 0
	droppedHasErr := false
	for _, r := range recs {
		switch r.Delivery {
		case OutboxPosted:
			posted++
		case OutboxDropped:
			dropped++
			if r.ErrMsg != "" {
				droppedHasErr = true
			}
		}
	}
	if posted != postQueueCap+1 {
		t.Fatalf("posted = %d, want %d", posted, postQueueCap+1)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if !droppedHasErr {
		t.Fatal("dropped record must carry ErrMsg")
	}
}

// TestPostLoopHandlerErrorRecordsOutbox postLoop handler 失败记 failed。
func TestPostLoopHandlerErrorRecordsOutbox(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	if err := h.Register("bad", echoHandler(nil, errors.New("post boom"))); err != nil {
		t.Fatalf("register bad: %v", err)
	}
	if err := h.Post(context.Background(), "R1", "sys", "bad", "message", nil); err != nil {
		t.Fatalf("post: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		recs := outboxSince(start)
		if len(recs) >= 2 {
			if recs[0].Delivery != OutboxPosted || recs[1].Delivery != OutboxFailed {
				t.Fatalf("records = %+v, want posted then failed", recs)
			}
			if recs[1].ErrMsg != "post boom" {
				t.Fatalf("failed ErrMsg = %q, want post boom", recs[1].ErrMsg)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("post handler failure not recorded within 2s: %+v", recs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSendWithRetrySuccessAfterTransient 前两次瞬时失败,第三次成功。
func TestSendWithRetrySuccessAfterTransient(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	var calls atomic.Int32
	if err := h.Register("A", func(ctx context.Context, msg Message) (any, error) {
		n := calls.Add(1)
		if n < 3 {
			return nil, fmt.Errorf("transient %d", n)
		}
		return "ok", nil
	}); err != nil {
		t.Fatalf("register A: %v", err)
	}

	reply, err := h.SendWithRetry(context.Background(), "R1", "sys", "A", "message", nil)
	if err != nil {
		t.Fatalf("SendWithRetry: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %v, want ok", reply)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}

	recs := outboxSince(start)
	if len(recs) != 3 {
		t.Fatalf("outbox records = %+v, want 3 (每次尝试一条)", recs)
	}
	if recs[0].Delivery != OutboxFailed || recs[0].Retries != 0 {
		t.Fatalf("rec[0] = %+v, want failed retries 0", recs[0])
	}
	if recs[1].Delivery != OutboxFailed || recs[1].Retries != 1 {
		t.Fatalf("rec[1] = %+v, want failed retries 1", recs[1])
	}
	if recs[2].Delivery != OutboxSent || recs[2].Retries != 2 {
		t.Fatalf("rec[2] = %+v, want sent retries 2", recs[2])
	}
}

// TestSendWithRetryGivesUpAfterThree 持续失败 ⇒ 3 次后放弃,err 原样冒泡。
func TestSendWithRetryGivesUpAfterThree(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	wantErr := errors.New("always boom")
	var calls atomic.Int32
	if err := h.Register("A", func(ctx context.Context, msg Message) (any, error) {
		calls.Add(1)
		return nil, wantErr
	}); err != nil {
		t.Fatalf("register A: %v", err)
	}

	_, err := h.SendWithRetry(context.Background(), "R1", "sys", "A", "message", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want always boom", err)
	}
	if got := calls.Load(); got != sendRetryMaxAttempts {
		t.Fatalf("calls = %d, want %d", got, sendRetryMaxAttempts)
	}
	recs := outboxSince(start)
	if len(recs) != sendRetryMaxAttempts {
		t.Fatalf("outbox records len = %d, want %d", len(recs), sendRetryMaxAttempts)
	}
	for i, r := range recs {
		if r.Delivery != OutboxFailed || r.Retries != i || r.ErrMsg != "always boom" {
			t.Fatalf("rec[%d] = %+v, want failed retries %d", i, r, i)
		}
	}
}

// TestSendWithRetryNoRetryOnUnregistered 未注册是确定性失败 ⇒ 不重试。
func TestSendWithRetryNoRetryOnUnregistered(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	_, err := h.SendWithRetry(context.Background(), "R1", "sys", "missing", "message", nil)
	if !errors.Is(err, ErrAgentNotRegistered) {
		t.Fatalf("err = %v, want ErrAgentNotRegistered", err)
	}
	if err.Error() != "agent not registered: missing" {
		t.Fatalf("err text = %q, want %q", err.Error(), "agent not registered: missing")
	}
	recs := outboxSince(start)
	if len(recs) != 1 {
		t.Fatalf("outbox records = %+v, want exactly 1 (no retry)", recs)
	}
	if recs[0].Delivery != OutboxFailed || recs[0].Retries != 0 {
		t.Fatalf("rec = %+v, want failed retries 0", recs[0])
	}
}

// TestSendWithRetryContextCancelDuringBackoff ctx 取消 ⇒ 退避立即返回,
// 不再重试。
func TestSendWithRetryContextCancelDuringBackoff(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 先取消:首次尝试仍执行,退避等待时立即返回。

	var calls atomic.Int32
	if err := h.Register("A", func(ctx context.Context, msg Message) (any, error) {
		calls.Add(1)
		return nil, errors.New("transient")
	}); err != nil {
		t.Fatalf("register A: %v", err)
	}

	_, err := h.SendWithRetry(ctx, "R1", "sys", "A", "message", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (no retry after cancel)", got)
	}
	recs := outboxSince(start)
	if len(recs) != 2 {
		t.Fatalf("outbox records = %+v, want attempt + cancel record", recs)
	}
	if recs[1].Delivery != OutboxFailed || !strings.Contains(recs[1].ErrMsg, "context canceled") {
		t.Fatalf("cancel record = %+v, want failed context canceled", recs[1])
	}
}

// TestSendWithRetrySuccessFirstAttempt 首次即成功 ⇒ 只记一条 sent。
func TestSendWithRetrySuccessFirstAttempt(t *testing.T) {
	h := New()
	defer h.Close()
	start := DefaultOutbox().Seq()

	if err := h.Register("A", echoHandler("ok", nil)); err != nil {
		t.Fatalf("register A: %v", err)
	}
	reply, err := h.SendWithRetry(context.Background(), "R1", "sys", "A", "message", nil)
	if err != nil || reply != "ok" {
		t.Fatalf("SendWithRetry = %v, %v", reply, err)
	}
	recs := outboxSince(start)
	if len(recs) != 1 || recs[0].Delivery != OutboxSent || recs[0].Retries != 0 {
		t.Fatalf("records = %+v, want one sent retries 0", recs)
	}
}
