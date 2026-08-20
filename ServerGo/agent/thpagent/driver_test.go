package thpagent

import (
	"context"
	"testing"
	"time"
)

func TestDriver_RegisterAgents(t *testing.T) {
	d := NewDriver()
	err := d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
		{Seat: 2, UserID: "bot2", ModelKey: "ModelB", ModelName: "ModelB"},
		{Seat: 4, UserID: "bot4", ModelKey: "ModelC", ModelName: "ModelC"},
	})
	if err != nil {
		t.Fatalf("RegisterAgents returned: %v", err)
	}

	if got := d.GetAgentCountForRoom("room1"); got != 3 {
		t.Errorf("expected 3 agents, got %d", got)
	}
}

func TestDriver_UnregisterAgents(t *testing.T) {
	d := NewDriver()
	d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
	})
	if got := d.GetAgentCountForRoom("room1"); got != 1 {
		t.Errorf("expected 1 agent before unregister, got %d", got)
	}

	d.UnregisterAgents("room1")
	if got := d.GetAgentCountForRoom("room1"); got != 0 {
		t.Errorf("expected 0 agents after unregister, got %d", got)
	}
}

func TestDriver_DecideAction_TimeoutFolds(t *testing.T) {
	d := NewDriver()
	d.maxActionTimeoutSec = 1 // 1s 超时
	d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
	})

	ctx := context.Background()
	promptCtx := &GameContextForAgent{
		RoomID:     "room1",
		HandNumber: 1,
		Street:     "preflop",
		MySeat:     0,
		MyHole:     [2]int{1, 14},
		MyStack:    1000,
	}

	action, err := d.DecideAction(ctx, "room1", 0, promptCtx)
	if err != nil {
		t.Errorf("DecideAction should not error on timeout, got: %v", err)
	}
	if action.Type != ActFold {
		t.Errorf("expected fold on timeout, got: %s", action.Type)
	}
	if action.Thought == "" {
		t.Error("expected thought on timeout fold")
	}
}

func TestDriver_DecideAction_UnregisteredRoom(t *testing.T) {
	d := NewDriver()
	_, err := d.DecideAction(context.Background(), "nonexistent", 0, &GameContextForAgent{})
	if err != ErrRoomNotRegistered {
		t.Errorf("expected ErrRoomNotRegistered, got: %v", err)
	}
}

func TestDriver_DecideAction_UnregisteredSeat(t *testing.T) {
	d := NewDriver()
	d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
	})
	// 座位 3 没注册
	_, err := d.DecideAction(context.Background(), "room1", 3, &GameContextForAgent{})
	if err != ErrSeatNotRegistered {
		t.Errorf("expected ErrSeatNotRegistered, got: %v", err)
	}
}

func TestDriver_DecideAction_RespectsContext(t *testing.T) {
	d := NewDriver()
	d.maxActionTimeoutSec = 60 // 长超时
	d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	action, err := d.DecideAction(ctx, "room1", 0, &GameContextForAgent{})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if action.Type != ActFold {
		t.Errorf("expected fold on ctx timeout, got: %s", action.Type)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("DecideAction took too long: %v", elapsed)
	}
}

// TestDriver_OnNewHand_ResetsChatCounts 验证 §130 防御:
// Driver.OnNewHandLocked 必须重置所有 bot 的 chat 计数,
// 否则 dispatcher.OnNewHand 永远是死代码 → bot 跨手牌累积 chat 计数后静音。
func TestDriver_OnNewHand_ResetsChatCounts(t *testing.T) {
	d := NewDriver()
	d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
	})

	// 通过 dispatcher 直读 — 用 d.mu 取出
	d.mu.RLock()
	disp := d.rooms["room1"].dispatch[0]
	d.mu.RUnlock()
	if disp == nil {
		t.Fatal("dispatcher should be initialized for seat 0")
	}

	// 用尽 2 次 chat
	disp.minChatIntervalSec = 0 // 测试时关掉时间限制
	_ = disp.DispatchPokerChat("1")
	_ = disp.DispatchPokerChat("2")
	if disp.ChatCountThisHand() != 2 {
		t.Fatalf("expected chat count=2 before reset, got %d", disp.ChatCountThisHand())
	}

	// 触发 OnNewHandLocked
	d.OnNewHandLocked("room1", [6]string{"bot0", "", "", "", "", ""})
	if disp.ChatCountThisHand() != 0 {
		t.Errorf("expected chat count=0 after OnNewHandLocked, got %d", disp.ChatCountThisHand())
	}
}

// TestDriver_OnNewRound_ResetsAction 验证每轮 poker_action 限流被正确重置。
func TestDriver_OnNewRound_ResetsAction(t *testing.T) {
	d := NewDriver()
	d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "ModelA", ModelName: "ModelA"},
	})

	d.mu.RLock()
	disp := d.rooms["room1"].dispatch[0]
	d.mu.RUnlock()

	_, _ = disp.DispatchPokerAction(Action{Type: ActFold})
	if !disp.IsPokerActionTaken() {
		t.Fatal("expected poker_action taken before reset")
	}

	d.OnNewRoundLocked("room1", 0)
	if disp.IsPokerActionTaken() {
		t.Error("expected poker_action reset after OnNewRoundLocked")
	}
}

// TestDriver_SetMaxActionTimeoutSec 验证超时配置注入。
func TestDriver_SetMaxActionTimeoutSec(t *testing.T) {
	d := NewDriver()
	d.SetMaxActionTimeoutSec(0) // 应回退 30
	if d.maxActionTimeoutSec != 30 {
		t.Errorf("expected default 30 on zero, got %d", d.maxActionTimeoutSec)
	}
	d.SetMaxActionTimeoutSec(45)
	if d.maxActionTimeoutSec != 45 {
		t.Errorf("expected 45, got %d", d.maxActionTimeoutSec)
	}
	d.SetMaxActionTimeoutSec(-5) // 负数回退默认
	if d.maxActionTimeoutSec != 30 {
		t.Errorf("expected default 30 on negative, got %d", d.maxActionTimeoutSec)
	}
}