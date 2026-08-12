// Package werewolf — regression test for BUG-R231-P0-01 (Round 231).
//
// R231 (report 20260802_100056) observed a full 13-AI werewolf room deadlocking
// during the night_wolves force-tally path: after the watchdog dispatched the
// early force-tally (either the all-wolves-quarantined fast path at
// phaseWatchdogTick:233 or the 120s-after-first-skip path at :373), the REST API
// GET /api/rooms/:id started returning alive=null for every player — the
// lockRoomBriefly 200ms timeout fallback kicking in because r.mu was being held
// continuously by the stuck phaseWatchdogTick goroutine.
//
// Root cause: the force-tally path called m.EmitAutoSkip / m.EmitWolfKill /
// m.wakeActingAgentsLocked WHILE holding r.mu. Those helpers fan out through
// EmitRoomActivity → hub.BroadcastRoomIncludingSpectators (h.mu.RLock) and,
// for the full-AI cascade, through wakeActingAgentsLocked →
// tryDispatchQuarantinedActingSkip → (a deep chain of *Locked variants that
// each re-enter wakeActingAgentsLocked). Holding r.mu across that entire fan-out
// created a lock-ordering hazard with any downstream path that takes h.mu (write)
// then needs r.mu, and — more critically in practice — could exhaust
// quarantineSkipDepth (50) mid-cascade, leaving the emit+wake half-done with
// r.mu still locked.
//
// Fix (BUG-R231-P0-01): apply the §130 "lock-in-record / lock-out-dispatch"
// two-phase pattern. Inside the r.mu critical section, do ONLY pure state
// mutations (tallyWolfVotes + endWolfPhase) and record the wake kind in a
// string variable. The deferred function at the end of phaseWatchdogTick runs
// AFTER r.mu.Unlock() and performs EmitAutoSkip / EmitWolfKill /
// wakeActingAgentsLocked outside the lock. Identical treatment for the
// night_wolves deadline path (phaseWatchdogTick:~301) which had the same flaw.
//
// These tests pin the fix:
//   (a) the force-tally path advances the phase (functional correctness), and
//   (b) r.mu is released after phaseWatchdogTick returns (no deadlock) —
//       verified by a bounded-time TryLock in a goroutine.
package werewolf

import (
	"testing"
	"time"

	"LsmWebGame/agent/wwplayer"
)

// TestForceTally_ReleasesLock_AllWolvesQuarantined pins the all-wolves-quarantined
// fast path (phaseWatchdogTick:233): after firing the tick, the phase must
// advance past night_wolves AND r.mu must be released within a bounded time.
func TestForceTally_ReleasesLock_AllWolvesQuarantined(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Collect wolf and seer seats from the seed layout.
	var wolves []int
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleWerewolf {
			wolves = append(wolves, i)
		}
	}
	if len(wolves) < 1 {
		r.mu.Unlock()
		t.Fatalf("seed layout needs >=1 wolf, got %d", len(wolves))
	}

	// Force night_wolves with ALL alive wolves quarantined and none voted.
	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(wolves[0])
	for i := range r.State.WolfVoteCast {
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
	}
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}
	// Quarantine every wolf bot so allAliveWolvesQuarantinedLocked returns true.
	for _, ws := range wolves {
		r.BotAgents[ws].SetQuarantined()
	}
	r.mu.Unlock()

	// Fire the watchdog tick. With the bug, this would hold r.mu indefinitely
	// (deadlock); with the fix it returns promptly after recording the wake kind.
	done := make(chan error, 1)
	go func() {
		done <- m.phaseWatchdogTick(r)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("phaseWatchdogTick did not return within 5s — probable deadlock on r.mu (BUG-R231-P0-01)")
	}

	// The phase must have advanced past night_wolves.
	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseNightWolves {
		t.Fatalf("expected phase to advance past PhaseNightWolves after force-tally; still PhaseNightWolves")
	}
	t.Logf("all-wolves-quarantined force-tally advanced phase to %s", phase)

	// r.mu must be releasable — bounded-time TryLock. With the bug, the tick's
	// deferred r.mu.Unlock() either never ran (deadlock) or ran only after the
	// deep emit+wake cascade, so TryLock would fail within the timeout.
	// We give a generous 2s window: the deferred unlock runs synchronously
	// after the tick returns, so in practice this is near-instant.
	lockDone := make(chan bool, 1)
	go func() {
		lockDone <- r.mu.TryLock()
	}()
	select {
	case acquired := <-lockDone:
		if !acquired {
			t.Fatal("r.mu could not be acquired after phaseWatchdogTick returned — lock was not released (BUG-R231-P0-01)")
		}
		r.mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("r.mu TryLock timed out after phaseWatchdogTick returned — probable unreleased lock (BUG-R231-P0-01)")
	}
}

// TestForceTally_ReleasesLock_120sEarlyPath pins the 120s-after-first-skip force-tally
// path (phaseWatchdogTick:~373): same lock-release guarantee, but triggered by
// skipCount>=1 + elapsed>=phaseWatchdogSingleActorDeadline rather than the
// all-quarantined fast path.
func TestForceTally_ReleasesLock_120sEarlyPath(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	wolfSeat := -1
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleWerewolf && wolfSeat < 0 {
			wolfSeat = i
		}
	}
	if wolfSeat < 0 {
		r.mu.Unlock()
		t.Fatal("seed layout has no wolf")
	}

	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(wolfSeat)
	for i := range r.State.WolfVoteCast {
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
	}
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}
	r.BotAgents[wolfSeat].SetQuarantined()

	// Pre-advance the watchdog: 1 skip already dispatched (skipCount=1), and
	// elapsed is just past the 120s single-actor deadline.
	r.phaseWatchdog.key = "night_wolves/" + itoa(wolfSeat)
	r.phaseWatchdog.skipCount = 1
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogSingleActorDeadline + 1*time.Second))
	r.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- m.phaseWatchdogTick(r)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("phaseWatchdogTick did not return within 5s — probable deadlock on r.mu (BUG-R231-P0-01)")
	}

	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseNightWolves {
		t.Fatalf("expected phase to advance past PhaseNightWolves after early force-tally; still PhaseNightWolves")
	}
	t.Logf("120s-early force-tally advanced phase to %s", phase)

	// r.mu must be releasable.
	lockDone := make(chan bool, 1)
	go func() {
		lockDone <- r.mu.TryLock()
	}()
	select {
	case acquired := <-lockDone:
		if !acquired {
			t.Fatal("r.mu could not be acquired after phaseWatchdogTick returned — lock was not released (BUG-R231-P0-01)")
		}
		r.mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("r.mu TryLock timed out after phaseWatchdogTick returned — probable unreleased lock (BUG-R231-P0-01)")
	}
}

// TestForceTally_ReleasesLock_DeadlinePath pins the night_wolves deadline path
// (phaseWatchdogTick:~318): phase deadline reached → tallyWolfVotes + endWolfPhase.
// Same lock-release guarantee.
func TestForceTally_ReleasesLock_DeadlinePath(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	wolfSeat := -1
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleWerewolf && wolfSeat < 0 {
			wolfSeat = i
		}
	}
	if wolfSeat < 0 {
		r.mu.Unlock()
		t.Fatal("seed layout has no wolf")
	}

	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(wolfSeat)
	for i := range r.State.WolfVoteCast {
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
	}
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		r.BotAgents[i] = bot
	}
	r.BotAgents[wolfSeat].SetQuarantined()

	// Pre-advance the watchdog: phase deadline reached. Use the general
	// deadline for 7-player (phaseWatchdogDeadlineFor(7)).
	r.phaseWatchdog.key = "night_wolves/" + itoa(wolfSeat)
	r.phaseWatchdog.skipCount = 0
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1*time.Second))
	// Arm a non-zero PhaseDeadlineAt so the deadline branch fires (it requires
	// !PhaseDeadlineAt.IsZero() && now.After(PhaseDeadlineAt)).
	r.State.PhaseDeadlineAt = time.Now().Add(-1 * time.Second)
	r.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- m.phaseWatchdogTick(r)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("phaseWatchdogTick returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("phaseWatchdogTick did not return within 5s — probable deadlock on r.mu (BUG-R231-P0-01)")
	}

	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseNightWolves {
		t.Fatalf("expected phase to advance past PhaseNightWolves after deadline force-tally; still PhaseNightWolves")
	}
	t.Logf("deadline force-tally advanced phase to %s", phase)

	lockDone := make(chan bool, 1)
	go func() {
		lockDone <- r.mu.TryLock()
	}()
	select {
	case acquired := <-lockDone:
		if !acquired {
			t.Fatal("r.mu could not be acquired after phaseWatchdogTick returned — lock was not released (BUG-R231-P0-01)")
		}
		r.mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("r.mu TryLock timed out after phaseWatchdogTick returned — probable unreleased lock (BUG-R231-P0-01)")
	}
}
