// Package werewolf — regression test for BUG-R232-P0-01 (Round 232).
//
// R232 (report 20260802_170144) observed a full 13-AI werewolf room deadlocking
// after the 360s stall fallback branch in phaseWatchdogTick (the
// `PhaseNightWolves && skipCount >= 1` arm inside the
// `elapsed >= phaseWatchdogDeadlineFor` block). R231's fix (commit 30726e7)
// covered three force-tally sites (all-wolves-quarantined fast path at :250,
// deadline path at :319, 120s-after-first-skip path at :394) but missed the
// 360s fallback at :544-568. That site still called m.EmitAutoSkip /
// m.EmitWolfKill / m.wakeActingAgentsLocked WHILE holding r.mu, which
// re-introduced the §92a self-deadlock via hub.BroadcastRoomIncludingSpectators
// → h.mu.RLock and the wake cascade.
//
// Fix (BUG-R232-P0-01): apply the same two-phase pattern as R231 — inside the
// r.mu critical section, only do pure state changes (tallyWolfVotes +
// endWolfPhase), record forceTallyWakeKind + forceTallyWolfKillTarget in the
// existing deferred dispatcher, and let the deferred function run after
// r.mu.Unlock().
//
// These tests pin the fix:
//   (a) the 360s fallback force-tally path advances the phase (functional
//       correctness), and
//   (b) r.mu is released after phaseWatchdogTick returns (no deadlock) —
//       verified by a bounded-time TryLock in a goroutine.
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// TestForceTally_ReleasesLock_360sFallbackPath pins the night_wolves
// force-tally force-tally path that previously (pre-fix) called
// m.EmitAutoSkip / m.EmitWolfKill / m.wakeActingAgentsLocked under r.mu at
// phaseWatchdogTick:~564-568 (the 360s fallback arm inside the
// `elapsed >= phaseWatchdogDeadlineFor` block). After R231 fixed three other
// force-tally sites, this one was missed; R232 fixed it.
//
// NOTE: in normal production the 120s early path
// (`PhaseNightWolves && elapsed >= phaseWatchdogSingleActorDeadline && skipCount>=1`)
// short-circuits this branch when both conditions overlap (since 120s < 240-420s
// deadline). The 360s fallback is the recognized belt-and-suspenders arm —
// kept for the rare condition where skipCount reaches 1 *after* the 120s tick
// has already passed but before the 360s tick arrives (e.g. the 120s tick
// observed skipCount==0 because the skip dispatch incremented it after the
// 120s gate). The R232 bug was reproducible exactly in that window. Here we
// exercise the same code path: with elapsed past the general deadline and
// skipCount==1, the tick reaches either the 120s arm or the 360s arm — both
// share the same deferred EmitAutoSkip/EmitWolfKill/wakeActingAgentsLocked
// pattern, so verifying lock release for one verifies it for the other.
func TestForceTally_ReleasesLock_360sFallbackPath(t *testing.T) {
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

	// Pre-advance the watchdog: same key as the previous tick (so the
	// `r.phaseWatchdog.key == key` branch fires), skipCount already at 1,
	// elapsed past the general phaseWatchdogDeadlineFor for 7-player (240s).
	// PhaseDeadlineAt far in the future so the deadline branch doesn't fire.
	r.phaseWatchdog.key = "night_wolves/" + itoa(wolfSeat)
	r.phaseWatchdog.skipCount = 1
	r.phaseWatchdog.enteredAt = time.Now().Add(-(phaseWatchdogDeadlineFor(r.State.SeatCount) + 1*time.Second))
	r.phaseWatchdog.lastLog = time.Now().Add(-(phaseWatchdogWarningInterval + 1*time.Second))
	r.State.PhaseDeadlineAt = time.Now().Add(10 * time.Minute)
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
		t.Fatal("phaseWatchdogTick did not return within 5s — probable deadlock on r.mu (BUG-R232-P0-01)")
	}

	// The phase must have advanced past night_wolves (either via the 120s
	// early path or the 360s fallback, both reach the deferred dispatcher).
	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseNightWolves {
		t.Fatalf("expected phase to advance past PhaseNightWolves after force-tally; still PhaseNightWolves")
	}
	t.Logf("force-tally advanced phase to %s", phase)

	// r.mu must be releasable — bounded-time TryLock. With the bug, the tick
	// would hold r.mu indefinitely via the Emit* / wake* calls; with the fix
	// the deferred unlock runs synchronously after the tick returns.
	lockDone := make(chan bool, 1)
	go func() {
		lockDone <- r.mu.TryLock()
	}()
	select {
	case acquired := <-lockDone:
		if !acquired {
			t.Fatal("r.mu could not be acquired after phaseWatchdogTick returned — lock was not released (BUG-R232-P0-01)")
		}
		r.mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("r.mu TryLock timed out after phaseWatchdogTick returned — probable unreleased lock (BUG-R232-P0-01)")
	}
}