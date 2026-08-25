// 2026-08-25 R20-B2-P1 regression test:
// late joiners during in-progress hand are marked Folded=true by joinInternal,
// excluded from current turn queue, then naturally un-folded on next StartHand.
package texasholdem

import (
	"testing"
)

// joinAndStart helper: alice + bob join + first hand starts, returns room.
func joinAndStart(t *testing.T, mgr *TexasHoldemManager, roomID string) *TexasHoldemRoom {
	t.Helper()
	if _, _, e := mgr.JoinGame(roomID, "alice"); e != nil {
		t.Fatalf("join alice: %v", e)
	}
	_, didStart, e := mgr.JoinGame(roomID, "bob")
	if e != nil {
		t.Fatalf("join bob: %v", e)
	}
	if !didStart {
		t.Fatalf("expected hand to start with 2 players")
	}
	r := mgr.getRoom(roomID)
	if r == nil {
		t.Fatal("room not found")
	}
	return r
}

// TestLateJoinerMarkedFolded verifies in-hand joiner gets Folded=true and is
// excluded from active player count; next StartHand naturally un-folds the
// late joiner and deals hole cards.
func TestLateJoinerMarkedFolded(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.BigBlind = 100
	mgr.StartStack = 5000
	r := joinAndStart(t, mgr, "r1")

	r.mu.Lock()
	if r.State.Street != PhasePreflop {
		r.mu.Unlock()
		t.Fatalf("expected hand at Preflop, got %v", r.State.Street)
	}
	r.mu.Unlock()

	if _, _, e := mgr.JoinGame("r1", "carol"); e != nil {
		t.Fatalf("late join carol: %v", e)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	carolSeat, ok := r.State.GetSeat("carol")
	if !ok {
		t.Fatal("carol should be seated")
	}
	if !r.State.Players[carolSeat].Folded {
		t.Fatalf("late joiner should be Folded=true until next hand, got Folded=%v",
			r.State.Players[carolSeat].Folded)
	}

	if r.State.activePlayers() != 2 {
		t.Fatalf("expected activePlayers=2 (alice+bob) after late join, got %d",
			r.State.activePlayers())
	}

	if r.State.Players[carolSeat].Hole[0] != (Card{}) || r.State.Players[carolSeat].Hole[1] != (Card{}) {
		t.Fatalf("late joiner should have empty Hole, got %+v",
			r.State.Players[carolSeat].Hole)
	}

	if e := r.State.StartHand(); e != nil {
		t.Fatalf("start hand 2: %v", e)
	}
	if r.State.Players[carolSeat].Folded {
		t.Fatalf("carol should be un-folded at start of next hand, got Folded=true")
	}
	if r.State.Players[carolSeat].Hole[0] == (Card{}) || r.State.Players[carolSeat].Hole[1] == (Card{}) {
		t.Fatalf("carol should be dealt 2 hole cards in hand 2, got %+v",
			r.State.Players[carolSeat].Hole)
	}
	if r.State.activePlayers() != 3 {
		t.Fatalf("expected activePlayers=3 (alice+bob+carol) in hand 2, got %d",
			r.State.activePlayers())
	}
}

// TestLateJoinerDoesNotBlockTurn verifies the late joiner Folded=true does
// not stall alice/bob turn rotation. With 3 players (alice + bob + carol-folded),
// Turn after alice fold must skip carol and land on bob (endHandFold only
// triggers when only 1 player remains, so 2-player-alive still continues).
func TestLateJoinerDoesNotBlockTurn(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.BigBlind = 100
	mgr.StartStack = 5000
	r := joinAndStart(t, mgr, "r1")

	if _, _, e := mgr.JoinGame("r1", "carol"); e != nil {
		t.Fatalf("join carol: %v", e)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	aliceSeat, _ := r.State.GetSeat("alice")
	bobSeat, _ := r.State.GetSeat("bob")
	if r.State.Turn != aliceSeat {
		t.Fatalf("expected Turn=alice(%d), got %d", aliceSeat, r.State.Turn)
	}

	// alice folds. After her fold, only bob and (folded) carol remain.
	// 2 players still in (bob + carol but carol is Folded, so actually 1 live).
	// Since carol is Folded, only bob remains → endHandFold triggers,
	// not nextActableSeat. This is correct: carol MUST be skipped.
	if _, e := r.State.ApplyAction(aliceSeat, Action{Type: ActFold}); e != nil {
		t.Fatalf("alice fold: %v", e)
	}
	// After alice fold → 1 live player (bob) → endHandFold → Street=PhaseOver
	if r.State.Street != PhaseOver && r.State.Turn != bobSeat {
		t.Fatalf("after alice fold, expected hand end or Turn=bob(%d), got Street=%v Turn=%d",
			bobSeat, r.State.Street, r.State.Turn)
	}
	// 关键断言:carol 不在 turn 队列中(若 carol 没标 Folded,Turn 会跳到 carol=2,
	// 导致 bot driver 永久卡在无效座位等待动作)
	carolSeat, _ := r.State.GetSeat("carol")
	if r.State.Turn == carolSeat {
		t.Fatalf("Turn landed on late joiner carol(%d) — folded status not honored!", carolSeat)
	}
}
