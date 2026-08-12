// Package werewolf — regression tests for BUG-WEREWOLF-P0-NEW-42b (Round 38).
//
// Root cause: a phase can permanently stall when:
//   - A quarantined bot's skip action fails (engine validation race with a
//     concurrent wake), or
//   - An agent goroutine silently stalls (goroutine panic with no recovery,
//     wake-channel drop, or scheduleReWake that never fires).
//
// The existing auto-skip mechanism fires per-agent based on
// failAutoSkipThreshold (1 LLM failure → skip), but if the agent goroutine
// never runs (no wake received) or panics before the failure counter
// increments, the phase is stuck forever with no recovery path.
//
// Fix: add a per-room phase watchdog goroutine that polls every 5s. When a
// phase+actingSeat persists for longer than phaseWatchdogDeadline (90s), the
// watchdog dispatches the phase's safe skip via dispatchQuarantinedSkipLocked.
// Also emits heartbeat WARN logs at phaseWatchdogWarningInterval (60s) for
// diagnostics.
//
// These tests verify:
//   - watchdogActingSeat returns the correct seat for each phase.
//   - phaseWatchdogTick fires when a phase is stuck past the deadline.
//   - phaseWatchdogTick resets on phase/seat changes.
//   - The watchdog goroutine terminates when context is cancelled.
package werewolf

import (
	"context"
	"strconv"
	"testing"
	"time"

	"LsmWebGame/agent/wwplayer"
)

// itoa is a convenience helper for building phase/actingSeat keys in tests.
func itoa(n int) string { return strconv.Itoa(n) }

// TestWatchdogActingSeat_NightPhases verifies that watchdogActingSeat returns
// the engine's TurnActingSeat for all three night phases.
func TestWatchdogActingSeat_NightPhases(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	phases := []Phase{PhaseNightWolves, PhaseNightSeer, PhaseNightWitch}
	for _, phase := range phases {
		r.State.Phase = phase
		r.State.TurnActingSeat = Seat(3)
		got := watchdogActingSeat(r)
		if got != 3 {
			t.Errorf("phase %v: expected acting seat 3, got %d", phase, got)
		}
	}
}

// TestWatchdogActingSeat_SpeakPhase verifies that watchdogActingSeat returns
// the SpeakTurnSeat for PhaseSpeak.
func TestWatchdogActingSeat_SpeakPhase(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.Phase = PhaseSpeak
	r.State.SpeakTurnSeat = Seat(5)
	got := watchdogActingSeat(r)
	if got != 5 {
		t.Errorf("PhaseSpeak: expected acting seat 5, got %d", got)
	}
}

// TestWatchdogActingSeat_NoSingleActor verifies that PhaseVote returns -1
// (no single actor) and PhaseHunterShoot returns the hunter's seat.
//
// BUG-WEREWOLF-P0-2 (R42): PhaseHunterShoot previously returned -1 alongside
// PhaseVote, so the watchdog could never dispatch a skip for a stuck hunter.
// Now it finds the hunter seat explicitly so the watchdog can force-skip
// when a dead/quarantined hunter leaves the phase permanently stalled.
func TestWatchdogActingSeat_NoSingleActor(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	// PhaseVote still returns -1 (no single acting seat).
	r.State.Phase = PhaseVote
	got := watchdogActingSeat(r)
	if got != -1 {
		t.Errorf("PhaseVote: expected acting seat -1, got %d", got)
	}

	// PhaseHunterShoot returns the hunter seat (BUG-WEREWOLF-P0-2).
	r.State.Phase = PhaseHunterShoot
	got = watchdogActingSeat(r)
	hunterSeat := findHunterSeat(r)
	if got != hunterSeat {
		t.Errorf("PhaseHunterShoot: expected acting seat %d (hunter), got %d", hunterSeat, got)
	}
}

// TestPhaseWatchdogTick_PhaseChanged_ResetsTimer verifies that when the
// watchdog observes a new phase key (phase or acting seat changed), it
// resets the timer and does NOT fire the skip.
func TestPhaseWatchdogTick_PhaseChanged_ResetsTimer(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Pre-set the watchdog state as if PhaseNightWolves/seat0 has been
	// running for 200s — but then change the phase.
	r.mu.Lock()
	r.State.Phase = PhaseNightWolves
	r.State.TurnActingSeat = Seat(0)
	r.phaseWatchdog.key = "night_wolves/0"
	r.phaseWatchdog.enteredAt = time.Now().Add(-200 * time.Second)
	r.mu.Unlock()

	// Now mutate the phase to night_seer before calling tick.
	r.mu.Lock()
	r.State.Phase = PhaseNightSeer
	r.State.TurnActingSeat = Seat(2)
	r.mu.Unlock()

	// Run the tick — the phase changed, so the watchdog should NOT fire a skip.
	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	// Verify the new key was set.
	r.mu.Lock()
	key := r.phaseWatchdog.key
	entered := r.phaseWatchdog.enteredAt
	r.mu.Unlock()
	if key != "night_seer/2" {
		t.Fatalf("expected key 'night_seer/2', got %q", key)
	}
	// enteredAt should be recent (within last second).
	if time.Since(entered) > 1*time.Second {
		t.Fatalf("enteredAt should be recent after phase change, got %v ago", time.Since(entered))
	}
}

// TestPhaseWatchdogTick_FiresOnNightWolves verifies that the watchdog dispatches
// a wolf_kill skip when PhaseNightWolves is stuck past phaseWatchdogDeadline.
// This is the primary regression test: a wolf agent stalls, the watchdog fires,
// and the engine advances to PhaseNightSeer.
func TestPhaseWatchdogTick_FiresOnNightWolves(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	// Force seat 0 to be the ONLY werewolf so the wolf_kill skip passes
	// engine validation (engine rejects wolf-on-wolf kills). Clear all
	// other roles to villager and refresh the alive counts.
	r.State.Roles[0] = RoleWerewolf
	for i := 1; i < MaxPlayers; i++ {
		r.State.Roles[i] = RoleVillager
	}
	r.State.refreshCounts()
	r.State.Phase = PhaseNightWolves
	r.State.TurnActingSeat = Seat(0)
	// Pre-set the watchdog as stuck for > 90s on this key.
	r.phaseWatchdog.key = "night_wolves/0"
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1 * time.Second))
	r.mu.Unlock()

	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
	}

	// After wolf_kill skip, the engine should have advanced past night_wolves.
	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseNightWolves {
		t.Fatalf("expected phase to advance past PhaseNightWolves after watchdog skip, "+
			"still PhaseNightWolves means the watchdog skip did not fire "+
			"(BUG-WEREWOLF-P0-NEW-42b regression)")
	}
}

// TestPhaseWatchdogTick_SkipsOverdueSeerCheck verifies that the watchdog can
// fire a seer_check for a stuck PhaseNightSeer.
func TestPhaseWatchdogTick_SkipsOverdueSeerCheck(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Find the seer seat.
	r.mu.Lock()
	var seerSeat int
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Roles[i] == RoleSeer {
			seerSeat = i
			break
		}
	}
	r.State.Phase = PhaseNightSeer
	r.State.TurnActingSeat = Seat(seerSeat)
	// Key must match what watchdogActingSeat() computes: "night_seer/<seerSeat>".
	r.phaseWatchdog.key = "night_seer/" + itoa(seerSeat)
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1 * time.Second))
	r.mu.Unlock()

	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned error: %v", err)
	}

	// After seer_check skip, the engine should have advanced past night_seer.
	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseNightSeer {
		t.Fatalf("expected phase to advance past PhaseNightSeer after watchdog skip, "+
			"still PhaseNightSeer (BUG-WEREWOLF-P0-NEW-42b regression)")
	}
}

// TestPhaseWatchdogTick_NilStateSafe verifies that the watchdog tick does not
// panic when the room's State is nil (tearing down).
func TestPhaseWatchdogTick_NilStateSafe(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State = nil
	r.mu.Unlock()

	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned error on nil State: %v", err)
	}
}

// TestPhaseWatchdogTick_GameOverSafe verifies that the watchdog tick does not
// fire when the game has ended.
func TestPhaseWatchdogTick_GameOverSafe(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseGameOver
	r.State.Status = "over"
	r.mu.Unlock()

	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("phaseWatchdogTick returned error on game over: %v", err)
	}
}

// TestStartPhaseWatchdog_TerminatesOnCancel verifies that the background
// watchdog goroutine exits promptly when its context is cancelled.
func TestStartPhaseWatchdog_TerminatesOnCancel(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.startPhaseWatchdog(ctx, r)
		close(done)
	}()

	// Give the goroutine a moment to start.
	time.Sleep(phaseWatchdogTickInterval + 200*time.Millisecond)

	// Cancel → should exit.
	cancel()

	select {
	case <-done:
		// Watchdog exited cleanly.
	case <-time.After(3 * time.Second):
		t.Fatalf("startPhaseWatchdog did not exit within 3s after context cancel")
	}
}

// TestPhaseWatchdogTick_PhaseHeartbeatLogs checks that the watchdog's lastLog
// timestamp is updated after the first tick.
func TestPhaseWatchdogTick_PhaseHeartbeatLogs(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// First tick — sets the initial key.
	r.mu.Lock()
	r.State.Phase = PhaseNightWolves
	r.State.TurnActingSeat = Seat(1)
	r.mu.Unlock()

	err := m.phaseWatchdogTick(r)
	if err != nil {
		t.Fatalf("first tick: %v", err)
	}

	r.mu.Lock()
	key := r.phaseWatchdog.key
	lastLog := r.phaseWatchdog.lastLog
	r.mu.Unlock()
	if key != "night_wolves/1" {
		t.Fatalf("expected key 'night_wolves/1', got %q", key)
	}
	if lastLog.IsZero() {
		t.Fatalf("lastLog should be set after first tick")
	}
}

// ─────────────────── BUG-WEREWOLF-P0-NEW-43 (Round 38, stack overflow) ───────────────────
//
// P0-NEW-43 is the in-process complement to P0-NEW-42b: the watchdog above
// keeps the engine alive across *minutes* of stall; P0-NEW-43 keeps the
// goroutine stack alive across *milliseconds* of recursion. Without
// quarantineSkipDepth / FinishVote auto-tally in dayVoteLocked, a full-AI
// 7-seat wolf room with a quarantined driver produced 1427747+ stack
// frames in a single dispatcher goroutine (room
// 46f08eb5-1bbd-4926-b075-5a3fa256d64a on 2026-07-08 15:26:20) — Go's
// runtime SIGQUIT'd the whole server with "stack overflow".

// TestVoteSkip_DriverSelfLoop_BoundedDepth reproduces the R38 stack-overflow
// pattern in isolation. Set up PhaseVote with seat 0 (the driver) marked
// alive but NOT yet voted, seat 1 already voted. Quarantine seat 0. Invoke
// tryDispatchQuarantinedActingSkip under r.mu — without the depth cap and
// the FinishVote auto-tally, the chain
//   tryDispatch -> dispatch(dayVoteLocked) -> wakeActingAgentsLocked -> tryDispatch
// recurses until stack overflow. With the fix in place the depth cap kicks
// in (driver MyTurn=true forever because it can't be the deciding voter
// here — without seats 2..6 voting allAliveVoted won't ever be true), the
// chain returns false, and r.quarantineSkipDepth goes back to 0.
//
// deadlockTimeout is reused from room_round37_test.go.
func TestVoteSkip_DriverSelfLoop_BoundedDepth(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	// Configure so seat 0 is the driver AND the only not-yet-voted seat.
	// MyTurn formula for PhaseVote: alive && (!MyVoted || seat == driverSeat).
	// seat 0 here is the driver, so it stays MyTurn=true forever — the R38
	// recursion target.
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.State.Players[0].Voted = false // driver has not voted yet
	r.State.Players[1].Voted = true  // seat 1 cast a vote already
	// Stub a bot agent for seat 0 — the only seat whose skip gets
	// dispatched — so we can flip its quarantine flag.
	r.BotAgents = make(map[int]*wwplayer.Agent)
	bot, _ := stubBotWithChannel(0)
	bot.SetQuarantined() // mark as permanently broken LLM
	r.BotAgents[0] = bot
	r.mu.Unlock()

	// Build the gc that mimics what wakeActingAgentsLocked would feed.
	r.mu.Lock()
	gc := buildAgentContextLocked(r, 0, 0)
	r.mu.Unlock()
	if !gc.MyTurn {
		t.Fatalf("setup precondition: seat 0 (driver, not yet voted) must have MyTurn=true, got %v",
			gc.MyTurn)
	}

	// Drive tryDispatchQuarantinedActingSkip repeatedly. Without the depth
	// cap, each call recurses back via dispatchQuarantinedSkipLocked ->
	// dayVoteLocked -> wakeActing -> tryDispatch, growing the stack
	// unboundedly until runtime throws. With the cap, the helper just
	// returns false after quarantineSkipDepthLimit entries.
	r.mu.Lock()
	defer r.mu.Unlock()
	const iterations = 1000 // generous headroom above quarantineSkipDepthLimit
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			tryDispatchQuarantinedActingSkip(m, r, bot, gc)
			// Reset Players[0].Voted so the next iteration re-hits the
			// "driver has not voted yet" precondition (the helper just
			// set it via DayVote(NoSeat)).
			r.State.Players[0].Voted = false
		}
	}()
	select {
	case <-done:
		// ok — finished cleanly without stack overflow
	case <-time.After(deadlockTimeout):
		t.Fatalf("tryDispatchQuarantinedActingSkip did not return within %s "+
			"across %d iterations — depth cap is not breaking the recursion "+
			"(BUG-WEREWOLF-P0-NEW-43 regression)", deadlockTimeout, iterations)
	}
	if r.quarantineSkipDepth != 0 {
		t.Errorf("quarantineSkipDepth must be 0 after every helper returns, got %d",
			r.quarantineSkipDepth)
	}
}

// TestVoteSkip_DriverTallyOnce_TransitionsPhase covers the OTHER half of the
// fix: when the driver's skip IS the deciding vote, FinishVote must fire
// within dayVoteLocked and the phase must transition out of PhaseVote. We
// arrange seats 1..6 already voted; the driver's abstain makes
// allAliveVoted true, and dayVoteLocked's new auto-tally branch
// (BUG-WEREWOLF-P0-NEW-43) runs FinishVote before the wake chain recurses.
//
// Without the new branch the room would stack-overflow instead of advancing.
// With the new branch PhaseVote -> next phase within a single lock-held call.
func TestVoteSkip_DriverTallyOnce_TransitionsPhase(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = (i != 0) // everyone BUT the driver voted
		r.State.Players[i].VoteTarget = Seat(1)
	}
	r.mu.Unlock()

	// Drive dayVoteLocked directly (lock-held caller, matching the
	// dispatchQuarantinedSkipLocked path). Seat 0 = driver, NoSeat = abstain.
	r.mu.Lock()
	defer r.mu.Unlock()
	userID := r.Seats[0]
	if e := m.dayVoteLocked(r, userID, NoSeat); e != nil {
		for i := 0; i < MaxPlayers; i++ {
			t.Logf("seat %d: alive=%v role=%v", i, r.State.AliveSeat(Seat(i)), r.State.Roles[i])
		}
		t.Fatalf("dayVoteLocked(driver, abstain) must succeed, got %d (%s)",
			e.Code, e.Message)
	}

	// PhaseVote must have transitioned. With allAliveVoted() now true the
	// new auto-tally branch inside dayVoteLocked (BUG-WEREWOLF-P0-NEW-43)
	// must have run FinishVote, advancing the phase out of PhaseVote. If
	// the tally produced a tie the engine keeps PhaseVote for a tiedRound
	// re-vote — assert we at least got a phase change OR a tied-round
	// status (DayTiedPlayers populated).
	transitioned := r.State.Phase != PhaseVote || r.State.Status == "over" ||
		len(r.State.DayTiedPlayers) > 0
	if !transitioned {
		t.Fatalf("phase must transition out of PhaseVote after the driver's deciding "+
			"abstain (FinishVote auto-tally), still phase=%s status=%s",
			r.State.Phase, r.State.Status)
	}
	if r.quarantineSkipDepth != 0 {
		t.Errorf("quarantineSkipDepth must be 0 after dayVoteLocked returns, got %d",
			r.quarantineSkipDepth)
	}
}

// TestFinishVoteLocked_BasicAccepted covers the freshly-added
// finishVoteLocked variant — the dispatchQuarantinedSkipLocked path must be
// able to call it without re-acquiring r.mu, and it must advance the engine
// just like Action_FinishVote would. Mirrors room_round37_test.go's pattern
// for the other *Locked helpers.
func TestFinishVoteLocked_BasicAccepted(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = (i != 0) // driver didn't vote yet
		r.State.Players[i].VoteTarget = Seat(1)
	}
	r.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	runWithDeadlockGuard(t, "finishVoteLocked under r.mu", func() {
		if e := m.finishVoteLocked(r, 0); e != nil {
			t.Errorf("finishVoteLocked must return nil on a valid vote phase, got %d (%s)",
				e.Code, e.Message)
		}
	})
	if r.quarantineSkipDepth != 0 {
		t.Errorf("quarantineSkipDepth must be 0 after finishVoteLocked returns, got %d",
			r.quarantineSkipDepth)
	}
}
