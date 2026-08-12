// Package werewolf — regression test for BUG-WEREWOLF-P0-2 (R45 follow-up).
//
// R45 (report 20260708_233906) observed the early R42 fix already in
// watchdogActingSeat (findHunterSeat returns the hunter seat for
// PhaseHunterShoot), but the full end-to-end path was never pinned down by a
// test: does the phase watchdog actually ADVANCE the engine out of
// PhaseHunterShoot when the hunter is quarantined?
//
// Scenario:
//   - The hunter (one of the 7 seats) has just been voted out by the day
//     vote. FinishVote set HunterPendingShoot=true, HunterPendingFrom="vote",
//     Phase=PhaseHunterShoot (engine.go::FinishVote).
//   - The hunter's bot agent is quarantined (LLM stream drops), so no agent
//     goroutine will ever call HunterShoot on its own.
//   - The phase watchdog fires after phaseWatchdogDeadline: it computes
//     actingSeat=findHunterSeat(r) (>=0), calls
//     SkipPhaseAction("hunter_shoot", ...) → ("hunter_shoot", -1), then
//     dispatchQuarantinedSkipLocked(r, hunterSeat, "hunter_shoot", -1) →
//     hunterShootLocked(r, r.Seats[hunterSeat], NoSeat).
//   - HunterShoot(hunterSeat, NoSeat) clears HunterPendingShoot and calls
//     advanceDay() → Phase transitions to PhaseNightWolves (next night).
//
// Without the watchdog fallback the room stays in PhaseHunterShoot forever,
// exactly the R45 observation (4 watchdog cycles × ~95s each). Without the
// hunter_shoot case in dispatchQuarantinedSkipLocked (the R42 addendum) the
// dispatch silently returns nil and the phase never advances — that's the
// regression THIS test pins down.
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// TestPhaseWatchdogTick_FiresOnHunterShoot verifies the full causal chain the
// R45 report hit: hunter voted out → PhaseHunterShoot → hunter quarantined →
// watchdog fires hunter_shoot skip → engine advances past PhaseHunterShoot.
// BUG-WEREWOLF-P0-2 (R42+R45).
func TestPhaseWatchdogTick_FiresOnHunterShoot(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Find the hunter seat.
	r.mu.Lock()
	hunterSeat := findHunterSeat(r)
	if hunterSeat < 0 {
		t.Fatalf("expected a hunter seat in the 7-player roles, got %d", hunterSeat)
	}

	// Reproduce the engine state after the hunter is voted out by the day
	// vote: FinishVote set HunterPendingShoot=true, HunterPendingFrom="vote",
	// Phase=PhaseHunterShoot (engine.go::FinishVote lines 803-806).
	r.State.Phase = PhaseHunterShoot
	r.State.DayNumber = 1
	r.State.HunterPendingShoot = true
	r.State.HunterPendingFrom = "vote"

	// Quarantine the hunter's bot agent so the watchdog has to do the work
	// (the agent goroutine would otherwise call hunter_shoot itself).
	r.BotAgents = make(map[int]*wwplayer.Agent)
	bot, _ := stubBotWithChannel(hunterSeat)
	bot.SetQuarantined()
	r.BotAgents[hunterSeat] = bot

	// Pre-set the watchdog so it believes PhaseHunterShoot/<hunterSeat> has
	// been stuck for > phaseWatchdogDeadline. Key must match what
	// watchdogActingSeat() computes for this phase.
	r.phaseWatchdog.key = "hunter_shoot/" + itoa(hunterSeat)
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1*time.Second))
	r.mu.Unlock()

	// Fire the watchdog tick. It must dispatch hunter_shoot(-1) via
	// hunterShootLocked and advance the engine out of PhaseHunterShoot.
	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	r.mu.Lock()
	phase := r.State.Phase
	pending := r.State.HunterPendingShoot
	r.mu.Unlock()

	if phase == PhaseHunterShoot {
		t.Fatalf("expected phase to advance past PhaseHunterShoot after watchdog "+
			"dispatched hunter_shoot(-1); still PhaseHunterShoot means the "+
			"watchdog skip did not fire or did not advance the engine "+
			"(BUG-WEREWOLF-P0-2 R45 regression)")
	}
	if pending {
		t.Fatalf("expected HunterPendingShoot to be cleared after hunter_shoot(-1), "+
			"still true (BUG-WEREWOLF-P0-2 R45 regression)")
	}
}

// TestDispatchQuarantinedSkip_HunterShoot_HoldsLock_AdvancesPhase verifies the
// single dispatchQuarantinedSkipLocked call for hunter_shoot — the primitive
// the watchdog relies on — succeeds under r.mu and actually calls
// HunterShoot(NoSeat) → advanceDay(). Without the dedicated hunter_shoot case
// in dispatchQuarantinedSkipLocked (added in the R42 addendum) the switch
// would fall through to `return nil` and the engine would never advance.
// BUG-WEREWOLF-P0-2 (R42).
func TestDispatchQuarantinedSkip_HunterShoot_HoldsLock_AdvancesPhase(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	hunterSeat := findHunterSeat(r)
	if hunterSeat < 0 {
		t.Fatalf("expected a hunter seat in the 7-player roles, got %d", hunterSeat)
	}
	// Reproduce "hunter just voted out" engine state.
	r.State.Phase = PhaseHunterShoot
	r.State.DayNumber = 1
	r.State.HunterPendingShoot = true
	r.State.HunterPendingFrom = "vote"
	r.mu.Unlock()

	// dispatchQuarantinedSkipLocked is ALWAYS called with r.mu held in
	// production (wakeActingAgentsLocked / phaseWatchdogTick). Call it under
	// the lock with a deadlock guard — the old hunterShootLocked path would
	// self-deadlock if it ever re-acquired r.mu. Hunterseat's bot userID is
	// r.Seats[hunterSeat] ("bot_<hunterSeat>").
	r.mu.Lock()
	defer r.mu.Unlock()
	runWithDeadlockGuard(t, "dispatchQuarantinedSkipLocked(hunter_shoot) under r.mu", func() {
		if e := m.dispatchQuarantinedSkipLocked(r, hunterSeat, "hunter_shoot", -1); e != nil {
			t.Errorf("dispatchQuarantinedSkipLocked(hunter_shoot) must succeed, got %d (%s)",
				e.Code, e.Message)
		}
	})

	// Phase must have advanced out of PhaseHunterShoot. With HunterPendingShoot
	// true, HunterShoot(NoSeat) → advanceDay() → startNight() (no status
	// "over" yet) → PhaseNightWolves.
	if r.State.Phase == PhaseHunterShoot {
		t.Fatalf("expected phase to advance past PhaseHunterShoot after "+
			"dispatchQuarantinedSkipLocked(hunter_shoot); still PhaseHunterShoot "+
			"means the hunter_shoot case was not wired up "+
			"(BUG-WEREWOLF-P0-2 R42 addendum regression)")
	}
	if r.State.HunterPendingShoot {
		t.Fatalf("expected HunterPendingShoot cleared after hunter_shoot(-1); "+
			"still true (BUG-WEREWOLF-P0-2 R42 addendum regression)")
	}
}
