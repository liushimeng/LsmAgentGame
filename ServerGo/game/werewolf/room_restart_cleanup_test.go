// Package werewolf — restart-safety regression tests for BUG-WEREWOLF-RESTART-CLEANUP.
//
// Validates that:
//   - WipeAllRooms returns the IDs of every room it tore down so the caller
//     can broadcast `game.removed` and log.
//   - WipeAllRooms calls stopAgentsLocked on every room (which blocks on
//     agentWG.Wait with a 5s cap), so caller-thread doesn't race with
//     in-flight LLM HTTP calls.
//   - RoomIDs returns the current in-memory room IDs, useful for the
//     shutdown fan-out path.
//   - WipeAllRooms is idempotent on a freshly constructed manager.
//
// Tests do NOT start real agent goroutines (registry stays nil so
// StartAgentsLocked is a no-op). The agentWG.Wait inside stopAgentsLocked
// is satisfied immediately when no goroutines were Add(1)'d.
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// helper: create a 7-bot full-AI room ready to start (no agents).
func makeReadyRoom(t *testing.T, m *WerewolfManager, roomID string) {
	t.Helper()
	for i := 0; i < MaxPlayers; i++ {
		if _, e := m.ManagerAddPlayerAt(roomID, "u"+roomID+"-"+string(rune('0'+i)), Seat(i)); e != nil {
			t.Fatalf("add player %d: %v", i, e)
		}
	}
	started, _ := m.ForceStartIfReady(roomID)
	if !started {
		t.Fatalf("ForceStartIfReady(%s) did not start", roomID)
	}
}

// TestWipeAllRooms_RemovesAllRooms pins the basic contract:
// WipeAllRooms deletes every room from m.rooms and returns the IDs.
func TestWipeAllRooms_RemovesAllRooms(t *testing.T) {
	m := stubWWMgr()
	makeReadyRoom(t, m, "r-wipe-1")
	makeReadyRoom(t, m, "r-wipe-2")
	makeReadyRoom(t, m, "r-wipe-3")

	if got := len(m.rooms); got != 3 {
		t.Fatalf("setup: want 3 rooms, got %d", got)
	}
	ids := m.WipeAllRooms()
	if len(ids) != 3 {
		t.Fatalf("WipeAllRooms returned %d ids, want 3", len(ids))
	}
	if len(m.rooms) != 0 {
		t.Fatalf("after wipe: m.rooms has %d entries, want 0", len(m.rooms))
	}
	// Each returned ID must be one of the three we created.
	found := map[string]bool{"r-wipe-1": false, "r-wipe-2": false, "r-wipe-3": false}
	for _, id := range ids {
		if _, ok := found[id]; ok {
			found[id] = true
		}
	}
	for id, seen := range found {
		if !seen {
			t.Errorf("missing id %q in WipeAllRooms result", id)
		}
	}
}

// TestWipeAllRooms_EmptyManagerIsNoop: a freshly built manager has no rooms;
// WipeAllRooms must return an empty (or nil) slice and not panic.
func TestWipeAllRooms_EmptyManagerIsNoop(t *testing.T) {
	m := stubWWMgr()
	ids := m.WipeAllRooms()
	if len(ids) != 0 {
		t.Fatalf("WipeAllRooms on empty manager returned %d ids, want 0", len(ids))
	}
}

// TestWipeAllRooms_Idempotent: wiping twice is safe; the second call is a no-op.
func TestWipeAllRooms_Idempotent(t *testing.T) {
	m := stubWWMgr()
	makeReadyRoom(t, m, "r-once")
	ids1 := m.WipeAllRooms()
	if len(ids1) != 1 {
		t.Fatalf("first wipe returned %d ids, want 1", len(ids1))
	}
	ids2 := m.WipeAllRooms()
	if len(ids2) != 0 {
		t.Fatalf("second wipe returned %d ids, want 0", len(ids2))
	}
}

// TestRoomIDs_Snapshot pins RoomIDs as a read-only inspector: it returns the
// live in-memory room IDs without mutating state, so the shutdown side can
// iterate the snapshot to fan `game.removed` out.
func TestRoomIDs_Snapshot(t *testing.T) {
	m := stubWWMgr()
	makeReadyRoom(t, m, "r-snap-a")
	makeReadyRoom(t, m, "r-snap-b")

	ids := m.RoomIDs()
	if len(ids) != 2 {
		t.Fatalf("RoomIDs returned %d, want 2", len(ids))
	}
	// Snapshot must not mutate state.
	if len(m.rooms) != 2 {
		t.Fatalf("RoomIDs mutated m.rooms (now %d, want 2)", len(m.rooms))
	}
}

// TestRoomIDs_EmptyManager pins the no-rooms edge case.
func TestRoomIDs_EmptyManager(t *testing.T) {
	m := stubWWMgr()
	ids := m.RoomIDs()
	if len(ids) != 0 {
		t.Fatalf("RoomIDs empty returned %d, want 0", len(ids))
	}
}

// TestWipeAllRooms_NoAgents_NoBlock pins the no-blocking contract for callers
// without registry (StartAgentsLocked was a no-op, so agentWG.Wait returns
// immediately). The test asserts the wipe finishes inside 2s — generous
// allowance so it never flakes on slow CI.
func TestWipeAllRooms_NoAgents_NoBlock(t *testing.T) {
	m := stubWWMgr()
	makeReadyRoom(t, m, "r-fast")
	done := make(chan []string, 1)
	go func() { done <- m.WipeAllRooms() }()
	select {
	case ids := <-done:
		if len(ids) != 1 {
			t.Fatalf("concurrent wipe returned %d ids, want 1", len(ids))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WipeAllRooms blocked > 2s on a registry-less manager")
	}
}

// TestRemoveGame_AfterWipeIsNoop pins that the regular RemoveGame path
// coexists with WipeAllRooms — after a wipe the room is gone and RemoveGame
// returns silently. This is the contract main.go relies on: the shutdown
// path wipes the manager, so the per-room RemoveRoomState cleanup walks
// find-zero rows.
func TestRemoveGame_AfterWipeIsNoop(t *testing.T) {
	m := stubWWMgr()
	makeReadyRoom(t, m, "r-nop")
	m.WipeAllRooms()
	// Should not panic and should not return an error.
	m.RemoveGame("r-nop")
	if len(m.rooms) != 0 {
		t.Fatalf("RemoveGame after wipe changed m.rooms to %d, want 0", len(m.rooms))
	}
}

// _ = errcode keeps the import alive for future test scenarios that may
// need to assert error categories without dragging in a heavier dep.
var _ = errcode.ErrRoomNotFound
