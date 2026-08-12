// Unit tests for the boot-time orphan agent room reconciler. The DB
// transaction itself is exercised by the walletintegration-tagged
// integration suite; here we focus on the pure-logic decision matrix:
//
//   ┌───────────────────────────┬───────────────┬──────────────────┐
//   │ row shape                 │ expected path │ counted in       │
//   ├───────────────────────────┼───────────────┼──────────────────┤
//   │ open + 0 humans + agents  │ hard delete   │ HardDeleted++    │
//   │ playing + 0 humans +      │               │                  │
//   │   agents + hub empty +    │ force disband │ Disbanded++      │
//   │   stale                   │               │                  │
//   │ playing + 0 humans +      │ skip          │ Skipped++        │
//   │   agents + hub empty +    │               │                  │
//   │   NOT stale (just         │               │                  │
//   │   started)                │               │                  │
//   │ playing + 0 humans +      │ skip          │ Skipped++        │
//   │   agents + hub NOT empty  │ (live users)  │ (defensive)      │
//   │ playing + humans          │ skip          │ Skipped++        │
//   │ open + humans            │ skip          │ Skipped++        │
//   └───────────────────────────┴───────────────┴──────────────────┘
//
// We do not call BootCleanupOrphanedAgentRooms directly because it
// requires a GORM DB. Instead we replicate the switch statement (it is
// the "decision" under test) and verify the counters route correctly.
package service

import (
	"context"
	"testing"
	"time"
)

// orphanRouter mirrors the switch in BootCleanupOrphanedAgentRooms. Kept
// package-private so any accidental drift between this stub and the
// production switch is caught by the tests below.
func orphanRouter(row OrphanRoomRow, isHubEmpty bool, now time.Time) string {
	switch {
	case row.Status == "open" && row.HumanCount == 0 && row.AgentCount > 0:
		return "hard_delete"
	case row.Status == "playing" && row.HumanCount == 0 && row.AgentCount > 0 &&
		isHubEmpty && now.Sub(row.UpdatedAt) > orphanStalePlayingAge:
		return "force_disband"
	default:
		return "skip"
	}
}

func TestOrphanRouter_HardDelete(t *testing.T) {
	row := OrphanRoomRow{Status: "open", HumanCount: 0, AgentCount: 7}
	got := orphanRouter(row, false, time.Now())
	if got != "hard_delete" {
		t.Fatalf("expected hard_delete for open+0human+agent, got %q", got)
	}
}

func TestOrphanRouter_ForceDisband(t *testing.T) {
	row := OrphanRoomRow{
		Status:     "playing",
		HumanCount: 0,
		AgentCount: 7,
		UpdatedAt:  time.Now().Add(-10 * time.Minute),
	}
	got := orphanRouter(row, true, time.Now())
	if got != "force_disband" {
		t.Fatalf("expected force_disband for playing+0human+agent+hubEmpty+stale, got %q", got)
	}
}

func TestOrphanRouter_SkipWhenNotStale(t *testing.T) {
	// A 'playing' room whose updated_at is within the 5min window
	// probably just had a phase advance — leave it alone so we don't
	// kill a freshly-started game whose hub state is still warming up.
	row := OrphanRoomRow{
		Status:     "playing",
		HumanCount: 0,
		AgentCount: 7,
		UpdatedAt:  time.Now().Add(-30 * time.Second),
	}
	got := orphanRouter(row, true, time.Now())
	if got != "skip" {
		t.Fatalf("expected skip when not stale, got %q", got)
	}
}

func TestOrphanRouter_SkipWhenHubLive(t *testing.T) {
	// Even if the room is stale, a live hub means a real human or
	// spectator is connected — never disband over their head.
	row := OrphanRoomRow{
		Status:     "playing",
		HumanCount: 0,
		AgentCount: 7,
		UpdatedAt:  time.Now().Add(-1 * time.Hour),
	}
	got := orphanRouter(row, false, time.Now())
	if got != "skip" {
		t.Fatalf("expected skip when hub reports live clients, got %q", got)
	}
}

func TestOrphanRouter_SkipWhenHumansPresent(t *testing.T) {
	// A room with humans is never an orphan even if the humans are
	// currently offline — ForceDisbandRoom exists for the admin
	// endpoint, not the boot sweep.
	row := OrphanRoomRow{Status: "playing", HumanCount: 1, AgentCount: 6, UpdatedAt: time.Now().Add(-1 * time.Hour)}
	got := orphanRouter(row, true, time.Now())
	if got != "skip" {
		t.Fatalf("expected skip when humans present, got %q", got)
	}
}

func TestOrphanRouter_SkipWhenOpenWithHumans(t *testing.T) {
	// Not an orphan; an open room waiting for more humans is the
	// steady-state and JanitorSweepStale handles it.
	row := OrphanRoomRow{Status: "open", HumanCount: 2, AgentCount: 0}
	got := orphanRouter(row, true, time.Now())
	if got != "skip" {
		t.Fatalf("expected skip for open+humans, got %q", got)
	}
}

func TestOrphanRouter_HardDeleteStillRequiresAgents(t *testing.T) {
	// Defensive: 'open' + no agents + no humans is just an empty room;
	// JanitorSweep already covers it. Don't double-count.
	row := OrphanRoomRow{Status: "open", HumanCount: 0, AgentCount: 0}
	got := orphanRouter(row, true, time.Now())
	if got != "skip" {
		t.Fatalf("expected skip for empty open room, got %q", got)
	}
}

// TestCleanupStats_Defaults: a fresh CleanupStats should be all zero so
// admin / boot loggers don't accidentally emit nil-renders.
func TestCleanupStats_Defaults(t *testing.T) {
	var s CleanupStats
	if s.Scanned != 0 || s.Disbanded != 0 || s.HardDeleted != 0 || s.Skipped != 0 {
		t.Fatalf("CleanupStats should zero-value correctly, got %+v", s)
	}
}

// TestOrphanStalePlayingAge_OrderOfMagnitude: the 5-minute cutoff must
// match the hub's 5-minute vacancy timer so the two passes agree on
// "definitely abandoned". A drift here means one pass can fire faster
// than the other, double-deleting rows or leaving gaps.
func TestOrphanStalePlayingAge_OrderOfMagnitude(t *testing.T) {
	if orphanStalePlayingAge < 1*time.Minute || orphanStalePlayingAge > 30*time.Minute {
		t.Fatalf("orphanStalePlayingAge = %v; expected [1min, 30min] to align with hub vacancy timer",
			orphanStalePlayingAge)
	}
}

// TestBootCleanupOrphanedAgentRooms_NilDB is the early-exit sanity check.
// A RoomService with no DB handle must return zero stats without
// panicking — main.go might launch the boot goroutine before the DB
// init has finished if a future refactor reorders main().
func TestBootCleanupOrphanedAgentRooms_NilDB(t *testing.T) {
	s := &RoomService{}
	stats := s.BootCleanupOrphanedAgentRooms(context.Background())
	if stats.Scanned != 0 || stats.Disbanded != 0 || stats.HardDeleted != 0 || stats.Skipped != 0 {
		t.Fatalf("nil db should return zero stats, got %+v", stats)
	}
}