// Package werewolf — regression test for BUG-REPORT-20260816-09:
// win-probability REST endpoint returns 10403 to legitimate spectators
// who were previously seated players (player→spectator downgrade).
//
// Root cause: FactionByUserID checked r.Seats before r.Spectators, so a
// stale seat entry caused the user to be reported as isSpectator=false
// even though they had been properly moved to the Spectators map.
//
// Fix: check r.Spectators FIRST in FactionByUserID.

package werewolf

import (
	"testing"

	"LsmAgentGame/errcode"
)

// TestFactionByUserID_SpectatorDowngrade_ReturnsSpectator verifies that a
// user who was downgraded from seated player to spectator (e.g. after WS
// disconnect cleanup) is correctly reported as isSpectator=true by
// FactionByUserID, even if their userID still appears in r.Seats due to
// stale data. This is the root cause of the win-probability 10403 bug.
func TestFactionByUserID_SpectatorDowngrade_ReturnsSpectator(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	// After fillAndStart, Seats[0] is occupied by bot_0.
	// Simulate: bot_0 disconnects, gets moved to Spectators, but r.Seats[0]
	// still contains "bot_0" (stale data — the bug scenario).
	r.mu.Lock()
	if r.Seats[0] != "bot_0" {
		t.Fatalf("expected Seats[0]=bot_0, got %q", r.Seats[0])
	}
	if r.Spectators == nil {
		r.Spectators = make(map[string]struct{})
	}
	r.Spectators["bot_0"] = struct{}{}
	r.mu.Unlock()

	// bot_0 should now be reported as isSpectator=true (they're a spectator),
	// NOT as a seated player. Before the fix, this returned isSpectator=false
	// because SeatOf found them in r.Seats[0] first.
	faction, alive, isSpectator := m.FactionByUserID(roomID, "bot_0")
	if !isSpectator {
		t.Errorf("downgraded user 'bot_0': expected isSpectator=true, got false — "+
			"this is the BUG: stale seat data causes spectator to be rejected by "+
			"win-probability endpoint (10403)")
	}
	if alive {
		t.Errorf("downgraded user 'bot_0': expected alive=false, got true")
	}
	if faction != "unknown" {
		t.Errorf("downgraded user 'bot_0': expected faction='unknown', got %q", faction)
	}

	// bot_1 is still a normal seated player — should NOT be a spectator.
	faction, alive, isSpectator = m.FactionByUserID(roomID, "bot_1")
	if isSpectator {
		t.Errorf("seated player 'bot_1': expected isSpectator=false, got true")
	}
	if !alive {
		t.Errorf("seated player 'bot_1': expected alive=true, got false")
	}
	if faction == "unknown" {
		t.Errorf("seated player 'bot_1': expected real faction, got 'unknown'")
	}

	// Completely absent user should be reported as spectator (non-player).
	faction, alive, isSpectator = m.FactionByUserID(roomID, "user-absent")
	if !isSpectator {
		t.Errorf("absent user: expected isSpectator=true, got false")
	}
}

// TestFactionByUserID_NilSpectatorsMap_DoesNotPanic ensures FactionByUserID
// handles rooms with a nil Spectators map gracefully (no panic).
func TestFactionByUserID_NilSpectatorsMap_DoesNotPanic(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	r.mu.Lock()
	r.Spectators = nil
	r.mu.Unlock()

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("FactionByUserID panicked with nil Spectators: %v", rec)
		}
	}()

	// Should not panic; seated player should still be resolved correctly.
	faction, alive, isSpectator := m.FactionByUserID(roomID, "bot_0")
	if isSpectator {
		t.Errorf("seated player: expected isSpectator=false, got true")
	}
	if !alive {
		t.Errorf("seated player: expected alive=true, got false")
	}
	if faction == "unknown" {
		t.Errorf("seated player: expected real faction, got 'unknown'")
	}
}

// TestGetWinProbability_SpectatorDowngrade_AccessAllowed is an integration
// sanity check: a downgraded spectator should be able to access the
// win-probability endpoint (no 10403). We test the manager-level lookup
// that the API handler uses.
func TestGetWinProbability_SpectatorDowngrade_AccessAllowed(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	// Simulate player→spectator downgrade with stale seat data.
	r.mu.Lock()
	if r.Spectators == nil {
		r.Spectators = make(map[string]struct{})
	}
	r.Spectators["bot_0"] = struct{}{}
	r.mu.Unlock()

	// The API handler checks: _, _, isSpectator := m.FactionByUserID(roomID, uid)
	// if !isSpectator { return 403 }
	_, _, isSpectator := m.FactionByUserID(roomID, "bot_0")
	if !isSpectator {
		t.Fatalf("downgraded spectator 'bot_0' should pass isSpectator check; "+
			"this means the 10403 bug is NOT fixed")
	}

	// A real seated player should be rejected (403).
	_, _, isSpectator = m.FactionByUserID(roomID, "bot_1")
	if isSpectator {
		t.Errorf("seated player 'bot_1' should NOT pass isSpectator check; "+
			"win-probability should return 403 for seated players")
	}

	// An absent user (pure spectator) should pass.
	_, _, isSpectator = m.FactionByUserID(roomID, "random-spectator")
	if !isSpectator {
		t.Errorf("absent user should pass isSpectator check")
	}

	_ = errcode.ErrPermissionDenied // reference for clarity
}
