// Unit tests for JanitorSweepStaleFilling (R187-1, 2026-07-23).
// Hermetic: no DB, no network — the sweep is driven entirely by the
// fakeGameSvc / fakeHubHook fakes declared in room_force_disband_test.go.
package service

import (
	"context"
	"testing"
	"time"
)

// TestJanitorSweepStaleFilling_NoGameSvc is the nil-safety check: a
// RoomService without the gameSvc hook (unit-test / legacy wiring) must
// no-op instead of panicking.
func TestJanitorSweepStaleFilling_NoGameSvc(t *testing.T) {
	s := &RoomService{}
	stats := s.JanitorSweepStaleFilling(context.Background(), time.Minute)
	if stats.Scanned != 0 || stats.Deleted != 0 {
		t.Fatalf("nil gameSvc must no-op, got %+v", stats)
	}
}

// TestJanitorSweepStaleFilling_ReapsOldEmptyRoom covers the R187 scenario:
// a werewolf room created >5min ago, stuck in filling, with no humans
// connected (hub reports empty) must be force-closed + broadcast removed.
func TestJanitorSweepStaleFilling_ReapsOldEmptyRoom(t *testing.T) {
	fake := &fakeGameSvc{
		fillingRooms: []WerewolfFillingRoomInfo{
			{
				RoomID:        "r-stale",
				Phase:         "filling",
				CreatedAt:     time.Now().Add(-10 * time.Minute),
				OccupiedSeats: 1,
			},
		},
	}
	hub := &fakeHubHook{rooms: map[string]bool{"r-stale": true}} // empty
	s := &RoomService{gameSvc: fake, hubHook: hub}               // db=nil → skip DB delete path

	stats := s.JanitorSweepStaleFilling(context.Background(), 5*time.Minute)
	if stats.Scanned != 1 || stats.Deleted != 1 || stats.Skipped != 0 {
		t.Fatalf("expected 1 scanned / 1 deleted / 0 skipped, got %+v", stats)
	}
	if len(fake.closeCalls) != 1 || fake.closeCalls[0] != "r-stale" {
		t.Fatalf("expected ForceCloseWerewolfFillingRoom(r-stale), got %v", fake.closeCalls)
	}
	if len(fake.bcastCalls) != 1 || fake.bcastCalls[0].Reason != "filling_reaper" {
		t.Fatalf("expected BroadcastRoomRemoved(filling_reaper), got %v", fake.bcastCalls)
	}
}

// TestJanitorSweepStaleFilling_KeepsRoomWithHuman is the safety hard
// constraint: a filling room with a connected human (hub NOT empty) must
// never be reaped, no matter how old it is — a human waiting for friends
// must not be kicked.
func TestJanitorSweepStaleFilling_KeepsRoomWithHuman(t *testing.T) {
	fake := &fakeGameSvc{
		fillingRooms: []WerewolfFillingRoomInfo{
			{
				RoomID:    "r-live",
				Phase:     "filling",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		},
	}
	hub := &fakeHubHook{rooms: map[string]bool{"r-live": false}} // NOT empty
	s := &RoomService{gameSvc: fake, hubHook: hub}

	stats := s.JanitorSweepStaleFilling(context.Background(), 5*time.Minute)
	if stats.Deleted != 0 || stats.Skipped != 1 {
		t.Fatalf("room with human must be kept, got %+v", stats)
	}
	if len(fake.closeCalls) != 0 {
		t.Fatalf("ForceClose must not fire, got %v", fake.closeCalls)
	}
}

// TestJanitorSweepStaleFilling_KeepsFreshRoom: a filling room younger
// than maxAge must be kept (the human may still be inviting friends).
func TestJanitorSweepStaleFilling_KeepsFreshRoom(t *testing.T) {
	fake := &fakeGameSvc{
		fillingRooms: []WerewolfFillingRoomInfo{
			{
				RoomID:    "r-fresh",
				Phase:     "filling",
				CreatedAt: time.Now().Add(-1 * time.Minute),
			},
		},
	}
	hub := &fakeHubHook{rooms: map[string]bool{"r-fresh": true}}
	s := &RoomService{gameSvc: fake, hubHook: hub}

	stats := s.JanitorSweepStaleFilling(context.Background(), 5*time.Minute)
	if stats.Deleted != 0 || stats.Skipped != 1 {
		t.Fatalf("fresh room must be kept, got %+v", stats)
	}
}

// TestJanitorSweepStaleFilling_SkipsZeroCreatedAt: legacy in-memory
// objects with a zero createdAt cannot be aged — the sweep must skip
// them conservatively (the 30-minute JanitorSweepStale is the backstop).
func TestJanitorSweepStaleFilling_SkipsZeroCreatedAt(t *testing.T) {
	fake := &fakeGameSvc{
		fillingRooms: []WerewolfFillingRoomInfo{
			{RoomID: "r-legacy", Phase: "filling"}, // CreatedAt zero
		},
	}
	hub := &fakeHubHook{rooms: map[string]bool{"r-legacy": true}}
	s := &RoomService{gameSvc: fake, hubHook: hub}

	stats := s.JanitorSweepStaleFilling(context.Background(), 5*time.Minute)
	if stats.Deleted != 0 || stats.Skipped != 1 {
		t.Fatalf("zero-createdAt room must be skipped, got %+v", stats)
	}
}

// TestJanitorSweepStaleFilling_SkipsWhenForceCloseAborts: when the
// in-memory manager refuses the close (room started mid-sweep), the
// sweep must not broadcast game.removed nor count a deletion.
func TestJanitorSweepStaleFilling_SkipsWhenForceCloseAborts(t *testing.T) {
	fake := &fakeGameSvc{
		fillingRooms: []WerewolfFillingRoomInfo{
			{
				RoomID:    "r-race",
				Phase:     "filling",
				CreatedAt: time.Now().Add(-10 * time.Minute),
			},
		},
		forceCloseOK: map[string]bool{"r-race": false},
	}
	hub := &fakeHubHook{rooms: map[string]bool{"r-race": true}}
	s := &RoomService{gameSvc: fake, hubHook: hub}

	stats := s.JanitorSweepStaleFilling(context.Background(), 5*time.Minute)
	if stats.Deleted != 0 || stats.Skipped != 1 {
		t.Fatalf("aborted force-close must count as skipped, got %+v", stats)
	}
	if len(fake.bcastCalls) != 0 {
		t.Fatalf("no broadcast on aborted close, got %v", fake.bcastCalls)
	}
}
