// Package citybus — bus_test.go: 消息总线单测(2026-11-03 §批次81)。
//
// 覆盖:注册 / 发送 / 房间级优先 / 未注册报错 / Agents 清单 /
// Post 异步投递 / 并发安全(-race)。
package citybus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func echoHandler(reply any, err error) Handler {
	return func(ctx context.Context, msg Message) (any, error) {
		return reply, err
	}
}

// TestRegisterAndSend 类级注册后可 Send,未注册报错。
func TestRegisterAndSend(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.Register("A", echoHandler("hello", nil)); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := h.Send(context.Background(), "R1", "sys", "A", "message", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if got != "hello" {
		t.Fatalf("reply = %v, want hello", got)
	}
	if _, err := h.Send(context.Background(), "R1", "sys", "missing", "message", nil); err == nil {
		t.Fatal("send to unregistered agent must fail")
	} else if err.Error() != "agent not registered: missing" {
		t.Fatalf("unregistered error = %q", err.Error())
	}
}

// TestRegisterDuplicate 同类重复注册报错(防覆盖既有 handler)。
func TestRegisterDuplicate(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.Register("A", echoHandler(nil, nil)); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := h.Register("A", echoHandler(nil, nil)); err == nil {
		t.Fatal("duplicate class register must fail")
	}
	if err := h.RegisterRoom("R1", "A", echoHandler(nil, nil)); err != nil {
		t.Fatalf("room register: %v", err)
	}
	if err := h.RegisterRoom("R1", "A", echoHandler(nil, nil)); err == nil {
		t.Fatal("duplicate room register must fail")
	}
	if err := h.Register("", echoHandler(nil, nil)); err == nil {
		t.Fatal("empty class must fail")
	}
	if err := h.Register("B", nil); err == nil {
		t.Fatal("nil handler must fail")
	}
}

// TestRoomLevelPriority 房间级优先,未注册的房间回退类级。
func TestRoomLevelPriority(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.Register("A", echoHandler("class", nil)); err != nil {
		t.Fatalf("register class: %v", err)
	}
	if err := h.RegisterRoom("R1", "A", echoHandler("room", nil)); err != nil {
		t.Fatalf("register room: %v", err)
	}
	got, _ := h.Send(context.Background(), "R1", "sys", "A", "message", nil)
	if got != "room" {
		t.Fatalf("room-level reply = %v, want room", got)
	}
	got, _ = h.Send(context.Background(), "R2", "sys", "A", "message", nil)
	if got != "class" {
		t.Fatalf("fallback reply = %v, want class", got)
	}
	// 注销后回退类级。
	h.UnregisterRoom("R1", "A")
	got, _ = h.Send(context.Background(), "R1", "sys", "A", "message", nil)
	if got != "class" {
		t.Fatalf("after unregister reply = %v, want class", got)
	}
	// 注销不存在的房间 no-op。
	h.UnregisterRoom("nope", "A")
}

// TestAgentsSorted Agents 只含类级且排序。
func TestAgentsSorted(t *testing.T) {
	h := New()
	defer h.Close()
	for _, c := range []string{"Zeta", "Alpha", "Mike"} {
		if err := h.Register(c, echoHandler(nil, nil)); err != nil {
			t.Fatalf("register %s: %v", c, err)
		}
	}
	if err := h.RegisterRoom("R1", "RoomOnly", echoHandler(nil, nil)); err != nil {
		t.Fatalf("register room: %v", err)
	}
	got := h.Agents()
	want := []string{"Alpha", "Mike", "Zeta"}
	if len(got) != len(want) {
		t.Fatalf("Agents() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Agents() = %v, want %v", got, want)
		}
	}
}

// TestPostAsync Post 不阻塞,异步到达;未注册目标丢弃不 panic。
func TestPostAsync(t *testing.T) {
	h := New()
	defer h.Close()
	got := make(chan Message, 1)
	if err := h.Register("A", func(ctx context.Context, msg Message) (any, error) {
		got <- msg
		return nil, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := h.Post(context.Background(), "R1", "sys", "A", "collect", map[string]any{"category": "macro"}); err != nil {
		t.Fatalf("post: %v", err)
	}
	select {
	case msg := <-got:
		if msg.Type != "collect" || msg.Payload.(map[string]any)["category"] != "macro" {
			t.Fatalf("unexpected message: %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post not delivered within 2s")
	}
	// 未注册目标:Post 成功返回(丢弃),不 panic。
	if err := h.Post(context.Background(), "R1", "sys", "nope", "message", nil); err != nil {
		t.Fatalf("post to unregistered: %v", err)
	}
}

// TestPostQueueFull 队列满时丢弃而非阻塞(容量 64;此处灌满后立即再投)。
func TestPostQueueFull(t *testing.T) {
	h := New()
	defer h.Close()
	block := make(chan struct{})
	if err := h.Register("slow", func(ctx context.Context, msg Message) (any, error) {
		<-block // 永不返回,让 postLoop 卡在第一条
		return nil, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	// 第 1 条被 postLoop 取走并阻塞;后续 64 条填满队列;再投必须立即返回。
	for i := 0; i < postQueueCap+1; i++ {
		if err := h.Post(context.Background(), "R1", "sys", "slow", "message", i); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	close(block)
}

// TestConcurrentAccess 并发注册 / 发送 / 注销(-race 覆盖)。
func TestConcurrentAccess(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.Register("A", echoHandler("ok", nil)); err != nil {
		t.Fatalf("register: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			_, _ = h.Send(context.Background(), fmt.Sprintf("R%d", i%3), "sys", "A", "message", i)
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = h.Post(context.Background(), "R1", "sys", "A", "message", i)
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = h.RegisterRoom(fmt.Sprintf("R%d", i), "B", echoHandler("b", nil))
			h.UnregisterRoom(fmt.Sprintf("R%d", i), "B")
			_ = h.Agents()
		}(i)
	}
	wg.Wait()
	_, _ = h.Send(context.Background(), "R1", "sys", "A", "message", nil)
}

// TestSendHandlerError handler 错误原样冒泡。
func TestSendHandlerError(t *testing.T) {
	h := New()
	defer h.Close()
	want := errors.New("boom")
	if err := h.Register("A", echoHandler(nil, want)); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, err := h.Send(context.Background(), "R1", "sys", "A", "message", nil)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestDefaultSingleton Default() 返回同一实例。
func TestDefaultSingleton(t *testing.T) {
	if Default() != Default() {
		t.Fatal("Default() must return the same instance")
	}
}
