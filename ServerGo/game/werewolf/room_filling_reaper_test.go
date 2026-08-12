// Package werewolf — unit tests for the filling-phase room reaper
// (R187-1, 2026-07-23): FillingRoomSnapshot + ForceCloseFillingRoom.
// Hermetic: in-memory only, no DB / network / LLM.
package werewolf

import (
	"testing"
	"time"
)

// newFillingTestManager builds a bare manager with a deterministic seed.
func newFillingTestManager() *WerewolfManager {
	return NewWerewolfManagerWithRegistry(nil)
}

func TestFillingRoomSnapshot_OnlyFillingRooms(t *testing.T) {
	m := newFillingTestManager()
	// JoinGame creates the in-memory room in PhaseFilling.
	if _, _, e := m.JoinGame("r-fill", "user-1"); e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	// A second room that has been manually pushed out of filling must not
	// appear in the snapshot.
	if _, _, e := m.JoinGame("r-started", "user-2"); e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	r2 := m.getRoom("r-started")
	r2.mu.Lock()
	r2.State.Phase = PhaseSpeak
	r2.mu.Unlock()

	snap := m.FillingRoomSnapshot()
	if len(snap) != 1 || snap[0].RoomID != "r-fill" {
		t.Fatalf("expected only r-fill in snapshot, got %+v", snap)
	}
	if snap[0].Phase != PhaseFilling.String() {
		t.Fatalf("expected phase=filling, got %q", snap[0].Phase)
	}
	if snap[0].OccupiedSeats != 1 {
		t.Fatalf("expected 1 occupied seat, got %d", snap[0].OccupiedSeats)
	}
	if snap[0].CreatedAt.IsZero() {
		t.Fatal("CreatedAt must be recorded at room creation")
	}
	if time.Since(snap[0].CreatedAt) > time.Minute {
		t.Fatalf("CreatedAt should be ~now, got %v", snap[0].CreatedAt)
	}
}

func TestFillingRoomSnapshot_NilManager(t *testing.T) {
	var m *WerewolfManager
	if snap := m.FillingRoomSnapshot(); snap != nil {
		t.Fatalf("nil manager must return nil, got %+v", snap)
	}
}

func TestForceCloseFillingRoom_RemovesRoom(t *testing.T) {
	m := newFillingTestManager()
	if _, _, e := m.JoinGame("r-x", "user-1"); e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	if !m.ForceCloseFillingRoom("r-x") {
		t.Fatal("ForceCloseFillingRoom must succeed on a filling room")
	}
	if m.getRoom("r-x") != nil {
		t.Fatal("room must be removed from manager")
	}
	// Idempotent: second call returns false (room absent).
	if m.ForceCloseFillingRoom("r-x") {
		t.Fatal("second close must return false")
	}
	if snap := m.FillingRoomSnapshot(); len(snap) != 0 {
		t.Fatalf("snapshot must be empty after close, got %+v", snap)
	}
}

func TestForceCloseFillingRoom_AbortsWhenStarted(t *testing.T) {
	m := newFillingTestManager()
	if _, _, e := m.JoinGame("r-y", "user-1"); e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	// Simulate the mid-sweep race: the room started between snapshot and
	// force-close.
	r := m.getRoom("r-y")
	r.mu.Lock()
	r.State.Phase = PhaseSpeak
	r.mu.Unlock()

	if m.ForceCloseFillingRoom("r-y") {
		t.Fatal("force-close must abort when the room already started")
	}
	// The room must have been restored into the manager (not torn down).
	if m.getRoom("r-y") == nil {
		t.Fatal("started room must be restored to the manager")
	}
}

func TestForceCloseFillingRoom_NilSafe(t *testing.T) {
	var m *WerewolfManager
	if m.ForceCloseFillingRoom("r") {
		t.Fatal("nil manager must return false")
	}
	m2 := newFillingTestManager()
	if m2.ForceCloseFillingRoom("") {
		t.Fatal("empty roomID must return false")
	}
	if m2.ForceCloseFillingRoom("never-existed") {
		t.Fatal("unknown room must return false")
	}
}
