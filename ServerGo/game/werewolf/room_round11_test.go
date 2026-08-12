// Package werewolf — regression test for BUG-R11-P0 (Round 11).
//
// R11 (report 20260729_220212) observed a night_wolves stuck → skip → stuck
// loop that stalled the match 12+ minutes: seat 2 (GLM-model wolf) kept
// LLM-failing, the watchdog dispatched wolf_kill skip, but the acting seat
// stayed locked on the same stuck wolf and the phase never advanced. By the
// time the 360s general deadline fired, a NEW night_wolves round had already
// started and re-stuck on the same seat.
//
// The fix (BUG-R11-P0) adds two early-safety nets that mirror the d7d3558
// hunter_shoot mechanism:
//
//   (a) night_wolves early force-tally: 120s after the FIRST wolf_kill skip
//       (skipCount>=1), immediately tallyWolfVotes + endWolfPhase — no need
//       to wait the full 360s general deadline. This is the core loop-breaker.
//   (b) single-actor night phase early skip: for night_guard / night_seer /
//       night_witch (the "one seat, one tool" phases isomorphic to
//       hunter_shoot), a 120s role-checked early skip via
//       dispatchQuarantinedSkipLocked, instead of waiting the general
//       deadline.
//
// These tests pin both paths: each sets up a fresh full-AI room, forces the
// engine into the target phase with the acting bot quarantined (so no agent
// goroutine will act on its own), pre-advances the watchdog clock past the
// 120s threshold, fires ONE phaseWatchdogTick, and asserts the phase advances
// past the stuck phase. Without the fix the tick would be a no-op and the
// phase would remain unchanged.
package werewolf

import (
	"testing"
	"time"

	"LsmWebGame/agent/wwplayer"
)

// stubBotWithChannel is declared in room_round29_test.go (same package).

// TestPhaseWatchdogTick_NightWolves_EarlyForceTally pins the core R11 loop
// breaker: after the first wolf_kill skip (skipCount=1), 120s later the
// watchdog force-tallies wolf votes and advances the phase out of
// night_wolves — without waiting the full general deadline.
func TestPhaseWatchdogTick_NightWolves_EarlyForceTally(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Seed=1 7-player layout: seat3 + seat6 are wolves, seat2 is seer,
	// seat5 is witch, seat0 hunter, seat1/4 villagers (see probe in
	// room_round11_test.go design notes). We force the acting seat onto a
	// wolf so the watchdog targets it.
	wolfSeat := -1
	seerSeat := -1
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleWerewolf && wolfSeat < 0 {
			wolfSeat = i
		}
		if r.State.Roles[i] == RoleSeer && seerSeat < 0 {
			seerSeat = i
		}
	}
	if wolfSeat < 0 || seerSeat < 0 {
		t.Fatalf("seed layout unexpected: wolf=%d seer=%d", wolfSeat, seerSeat)
	}

	// Force night_wolves with the acting seat on the stuck wolf. All wolves
	// alive but none voted (simulates the stuck state after a prior skip).
	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(wolfSeat)
	for i := range r.State.WolfVoteCast {
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	// Ensure all seats alive so the wolf has legal kill targets during
	// tally (tallyWolfVotes falls back to randomAliveNonWolf otherwise).
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
	}

	// Register bots on every seat so wakeActingAgentsLocked / context build
	// works and the acting bot is the stuck one (quarantined → no self-act).
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}
	r.BotAgents[wolfSeat].SetQuarantined()

	// Pre-advance the watchdog: 1 skip already dispatched (skipCount=1), and
	// elapsed is just past the 120s single-actor deadline. Key must match
	// what watchdogActingSeat computes for night_wolves (= TurnActingSeat).
	r.phaseWatchdog.key = "night_wolves/" + itoa(wolfSeat)
	r.phaseWatchdog.skipCount = 1
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogSingleActorDeadline + 1*time.Second))
	r.mu.Unlock()

	// Fire the watchdog tick — it must break the loop by force-tallying.
	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()

	if phase == PhaseNightWolves {
		t.Fatalf("expected phase to advance past PhaseNightWolves after early "+
			"force-tally (BUG-R11-P0 core loop-breaker); still PhaseNightWolves "+
			"means the 120s force-tally path did not fire")
	}
	t.Logf("night_wolves early force-tally advanced phase to %s (acting wolf "+
		"seat=%d, seer seat=%d)", phase, wolfSeat, seerSeat)
}

// TestPhaseWatchdogTick_SingleActorNightPhase_EarlySkip pins path (b): a
// single-actor night phase (night_seer here) whose acting bot is stuck — the
// watchdog fires the role-checked early skip and advances the phase out of
// night_seer.
//
// NOTE on structure: the single-actor block is nested inside the general
// `elapsed >= phaseWatchdogDeadlineFor(SeatCount)` block (240s for 7-player),
// exactly mirroring the d7d3558 hunter_shoot block. So this test pre-advances
// the watchdog past the GENERAL deadline (not 120s) to exercise it. The
// genuinely-early 120s path is the night_wolves force-tally (path a, tested
// above), which sits BEFORE the general-deadline block.
func TestPhaseWatchdogTick_SingleActorNightPhase_EarlySkip(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	seerSeat := -1
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleSeer && seerSeat < 0 {
			seerSeat = i
		}
	}
	if seerSeat < 0 {
		t.Fatalf("seed layout unexpected: no seer (seat=%d)", seerSeat)
	}

	// Force night_seer with the acting seat on the seer.
	r.State.Phase = PhaseNightSeer
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(seerSeat)
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
	}

	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}
	// Quarantine the seer so it cannot self-act; the watchdog must rescue.
	r.BotAgents[seerSeat].SetQuarantined()

	// Pre-advance watchdog past the GENERAL deadline (240s for 7-player).
	// The single-actor block lives inside that block, so it only fires once
	// the general deadline is reached. Key matches watchdogActingSeat for
	// night_seer (= TurnActingSeat).
	r.phaseWatchdog.key = "night_seer/" + itoa(seerSeat)
	r.phaseWatchdog.skipCount = 0
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1*time.Second))
	r.mu.Unlock()

	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()

	if phase == PhaseNightSeer {
		t.Fatalf("expected phase to advance past PhaseNightSeer after single-actor "+
			"early skip (BUG-R11-P0 path b); still PhaseNightSeer means the "+
			"role-checked early skip did not fire")
	}
	t.Logf("night_seer single-actor early skip advanced phase to %s (seer seat=%d)",
		phase, seerSeat)
}
