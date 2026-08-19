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