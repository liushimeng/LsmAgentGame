// Package werewolf - regression tests for BUG-WEREWOLF-P0-NEW-42 (Round 37).
//
// Root cause: dispatchQuarantinedSkipLocked's non-finish_speak cases (wolf_kill
// / seer_check / witch_act_skip / vote_skip / sheriff_elect / start_day) all
// routed through the public Action_* methods, each of which does r.mu.Lock().
// But dispatchQuarantinedSkipLocked is ALWAYS called with r.mu already held
// (callers: wakeActingAgentsLocked / wakeAllAgentsLocked / notifyQuarantine).
// Go's sync.Mutex is not reentrant -> the re-Lock self-deadlocked the goroutine
// holding r.mu, freezing the whole room.
//
// R37 reproduced this on the speak->vote transition: the last speaker's
// agent-side finish_speak (Action_FinishSpeak, holding r.mu) ->
// wakeActingAgentsLocked -> quarantined seat 3 (MyTurn in PhaseVote) ->
// dispatchQuarantinedSkipLocked("vote_skip") -> Action_DayVote -> r.mu.Lock()
// => permanent deadlock. After 13:17:01.437 the room emitted no further log
// lines for ~6 minutes until the test was force-terminated.
//
// Fix: lock-held *Locked variants of every Action_* used by
// dispatchQuarantinedSkipLocked (wolfKillLocked / seerCheckLocked /
// witchLocked / dayVoteLocked / sheriffElectLocked / startDayLocked), mirroring
// the pre-existing finishSpeakLocked. They skip the r.mu.Lock and wake the next
// acting seat via wakeActingAgentsLocked.
//
// The pre-existing TestDispatchQuarantinedSkip_VoteSkip_AbstainsNotSelfVote
// (P0-NEW-35) did NOT catch this because it called dispatchQuarantinedSkipLocked
// WITHOUT holding r.mu - the opposite of every production caller. These tests
// hold r.mu (matching production) and use a timeout guard so a regression
// deadlocks the test (FAIL) instead of hanging the suite forever.
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/errcode"
)

// deadlockTimeout caps how long we wait for a dispatchQuarantinedSkipLocked
// call (run in a goroutine) before declaring a self-deadlock. The happy path
// completes in microseconds; the old code (Action_* re-Lock) would block
// forever. 2s is far above any legitimate latency and well below CI patience.
const deadlockTimeout = 2 * time.Second

// runWithDeadlockGuard invokes fn in a goroutine and fails the test (with a
// clear message) if it does not return within deadlockTimeout. Used so a
// mutex self-deadlock surfaces as a test FAILURE instead of an indefinite
// hang. BUG-WEREWOLF-P0-NEW-42.
func runWithDeadlockGuard(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		// ok
	case <-time.After(deadlockTimeout):
		t.Fatalf("%s did not complete within %s - likely an r.mu self-deadlock "+
			"(BUG-WEREWOLF-P0-NEW-42 regression: a public Action_* method was "+
			"re-acquired while r.mu was held)", what, deadlockTimeout)
	}
}

// TestDispatchQuarantinedSkip_VoteSkip_HoldsLock_NoDeadlock is the direct
// regression for the R37 deadlock. Production callers (wakeActingAgentsLocked
// / notifyQuarantine) ALWAYS hold r.mu when invoking
// dispatchQuarantinedSkipLocked, so the test must too. Before the fix the
// "vote_skip" case called Action_DayVote -> r.mu.Lock() while r.mu was already
// held => self-deadlock (the goroutine blocks forever holding the room lock).
func TestDispatchQuarantinedSkip_VoteSkip_HoldsLock_NoDeadlock(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// PhaseVote, all alive, no votes cast.
	r.mu.Lock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.mu.Unlock()

	// Hold r.mu (matching production) and dispatch vote_skip under a deadlock
	// guard. The old code hung here; the new dayVoteLocked path returns nil.
	r.mu.Lock()
	defer r.mu.Unlock()
	runWithDeadlockGuard(t, "dispatchQuarantinedSkipLocked(vote_skip) under r.mu", func() {
		if e := m.dispatchQuarantinedSkipLocked(r, 3, "vote_skip", 0); e != nil {
			t.Errorf("dispatchQuarantinedSkipLocked(vote_skip) must succeed, got %d (%s)",
				e.Code, e.Message)
		}
	})

	// seat 3 must be marked Voted (abstain) - the P0-NEW-35 contract still holds.
	if !r.State.Players[3].Voted {
		t.Fatalf("seat 3 must be marked Voted after vote_skip")
	}
	if r.State.Players[3].VoteTarget != NoSeat {
		t.Fatalf("vote_skip must register abstain (NoSeat=%d), got %d",
			NoSeat, r.State.Players[3].VoteTarget)
	}
}

// TestAction_FinishSpeak_ToVote_QuarantinedActingBot_NoDeadlock reproduces the
// full R37 causal chain: the last speaker's finish_speak transitions the room
// to PhaseVote, and wakeActingAgentsLocked (called inside Action_FinishSpeak
// while r.mu is held) finds a quarantined acting bot in the vote phase. Before
// the fix, dispatchQuarantinedSkipLocked("vote_skip") -> Action_DayVote ->
// r.mu.Lock() self-deadlocked Action_FinishSpeak's goroutine, freezing the room.
//
// After the fix the vote_skip is dispatched via dayVoteLocked (no re-Lock), the
// quarantined bot abstains, and (with every alive bot quarantined) the vote
// auto-tallies via FinishVote -> phase advances past PhaseVote.
func TestAction_FinishSpeak_ToVote_QuarantinedActingBot_NoDeadlock(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Register bot agents for every seat so wakeActingAgentsLocked has
	// something to iterate. channels let us observe (or ignore) wakes.
	channels := make(map[int]chan wwplayer.AgentEvent)
	r.mu.Lock()
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for seat := 0; seat < MaxPlayers; seat++ {
		bot, ch := stubBotWithChannel(seat)
		r.BotAgents[seat] = bot
		channels[seat] = ch
	}

	// Put the engine at the LAST speaker of a 2-seat speak order so a single
	// finish_speak transitions to PhaseVote. SpeakOrder = [0, 1]; seat 1 is
	// the current (last) speaker.
	r.State.Phase = PhaseSpeak
	r.State.DayNumber = 1
	r.State.SpeakOrder = []Seat{0, 1}
	r.State.SpeakTurnSeat = 1 // seat 1 is the current/last speaker
	r.State.Players[0].HasSpoken = true
	r.State.Players[1].HasSpoken = false
	// Clear any prior votes so the vote phase starts clean.
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.mu.Unlock()

	// Quarantine EVERY alive bot. In the vote phase all alive non-voted bots
	// have MyTurn=true, so wakeActingAgentsLocked will dispatch vote_skip for
	// each via tryDispatchQuarantinedActingSkip. Before the fix the FIRST
	// vote_skip deadlocked Action_FinishSpeak. After the fix the chain
	// abstains for all seats -> allAliveVoted() -> FinishVote -> phase leaves
	// PhaseVote.
	for seat := 0; seat < MaxPlayers; seat++ {
		if r.State.AliveSeat(Seat(seat)) {
			r.BotAgents[seat].SetQuarantined()
		}
	}

	userID1 := r.Seats[1]
	// Action_FinishSpeak acquires r.mu, transitions speak->vote, then calls
	// wakeActingAgentsLocked (still under r.mu) which hits the quarantined
	// vote_skip path. Run under a deadlock guard: old code hangs, new code
	// returns.
	runWithDeadlockGuard(t, "Action_FinishSpeak (last speaker) -> vote -> quarantined vote_skip", func() {
		if _, e := m.Action_FinishSpeak("test-room-full-ai", userID1); e != nil {
			t.Errorf("Action_FinishSpeak must succeed, got %d (%s)", e.Code, e.Message)
		}
	})

	// The room must have advanced past PhaseVote (FinishVote fired once every
	// alive bot abstained). If we are still in PhaseVote the deadlock-guard
	// "success" was hollow - the chain did not complete.
	r.mu.Lock()
	phase := r.State.Phase
	r.mu.Unlock()
	if phase == PhaseVote {
		t.Fatalf("phase must advance past PhaseVote after all quarantined bots " +
			"abstained via vote_skip chain; still PhaseVote means the skip chain " +
			"stalled (BUG-WEREWOLF-P0-NEW-42 regression)")
	}
	if phase == PhaseSpeak {
		t.Fatalf("phase must not still be PhaseSpeak; finish_speak did not advance")
	}
}

// TestDispatchQuarantinedSkip_AllCases_HoldLock_NoDeadlock sweeps every skip
// case to confirm none re-acquire r.mu. Each case is set up in its matching
// phase so the engine mutation is legal; the dispatch is run under r.mu with a
// deadlock guard. Before the fix every case called a public Action_* that
// re-Locked r.mu. BUG-WEREWOLF-P0-NEW-42.
func TestDispatchQuarantinedSkip_AllCases_HoldLock_NoDeadlock(t *testing.T) {
	cases := []struct {
		name     string
		skipName string
		setup    func(r *WerewolfRoom) // called under r.mu; leaves r.mu held
	}{
		{
			name:     "vote_skip",
			skipName: "vote_skip",
			setup: func(r *WerewolfRoom) {
				r.State.Phase = PhaseVote
				r.State.DayNumber = 1
				for i := 0; i < MaxPlayers; i++ {
					r.State.Players[i].Voted = false
					r.State.Players[i].VoteTarget = NoSeat
				}
			},
		},
		{
			name:     "sheriff_elect",
			skipName: "sheriff_elect",
			setup: func(r *WerewolfRoom) {
				r.State.Phase = PhaseSheriff
				r.State.DayNumber = 1
				r.State.SheriffSeat = NoSeat
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := stubWWMgr()
			_, r := fillAndStart(t, m)

			r.mu.Lock()
			tc.setup(r)
			// dispatchQuarantinedSkipLocked is called while r.mu is held, exactly
			// as wakeActingAgentsLocked / notifyQuarantine do in production.
			runWithDeadlockGuard(t, "dispatchQuarantinedSkipLocked("+tc.skipName+") under r.mu", func() {
				// seat 3 is alive and a valid actor for these phases.
				_ = m.dispatchQuarantinedSkipLocked(r, 3, tc.skipName, 0)
			})
			r.mu.Unlock()
		})
	}
}

// TestDispatchQuarantinedSkip_VoteSkip_NonExistentRoomSafe is a defensive
// check that dayVoteLocked (and by extension the other *Locked variants)
// returns a clean errcode rather than panicking when r.State is nil. The
// public Action_* methods guard this via getRoom + r.State == nil; the
// *Locked variants must too so a tearing-down room doesn't crash the wake
// goroutine. BUG-WEREWOLF-P0-NEW-42.
func TestDispatchQuarantinedSkip_VoteSkip_NilStateSafe(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	prevState := r.State
	r.State = nil
	r.mu.Unlock()

	// dayVoteLocked returns ErrGameNotStarted (no panic, no deadlock).
	r.mu.Lock()
	defer r.mu.Unlock()
	runWithDeadlockGuard(t, "dispatchQuarantinedSkipLocked(vote_skip) with nil State", func() {
		e := m.dispatchQuarantinedSkipLocked(r, 3, "vote_skip", 0)
		if e == nil {
			t.Errorf("expected non-nil error for nil State, got nil")
		} else if e.Code != errcode.ErrGameNotStarted {
			t.Errorf("expected ErrGameNotStarted (%d), got %d (%s)",
				errcode.ErrGameNotStarted, e.Code, e.Message)
		}
	})

	// Restore so the room doesn't leak a nil State into other shared state.
	r.State = prevState
}

// TestDispatchQuarantinedSkip_WolfKill_SkipsWolfTarget is the R73 P1
// regression. Before the fix, dispatchQuarantinedSkipLocked("wolf_kill") with
// target=-1 scanned ANY alive non-self seat — when only one wolf remained it
// picked the dead fellow-wolf and NightWolfKill rejected with "wolves cannot
// kill each other" [ErrValidationFailed]. The watchdog retried forever and the
// phase stalled. After the fix the scan only considers non-wolf seats; when no
// legal non-wolf target exists the skip returns nil (no-op), so the watchdog
// can retry cleanly.
func TestDispatchQuarantinedSkip_WolfKill_SkipsWolfTarget(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseNightWolves
	// Standard 7P layout: seat 0,1 wolves; rest good. Kill seat 1 so only
	// seat 0 is the lone alive wolf.
	r.State.Roles = [MaxPlayers]Role{
		RoleWerewolf, RoleWerewolf, RoleSeer, RoleWitch, RoleHunter, RoleVillager, RoleVillager,
	}
	r.State.Players[1].Alive = false
	// Only seat 0 (wolf) + seats 2..6 are alive; target=-1 means scan:
	// should land on seat 2 (first alive non-wolf), NOT seat 1 (dead wolf).
	initialWolfTarget := r.State.WolfKillTarget
	runWithDeadlockGuard(t, "dispatchQuarantinedSkipLocked(wolf_kill,-1) scans for non-wolf target", func() {
		if e := m.dispatchQuarantinedSkipLocked(r, 0, "wolf_kill", -1); e != nil {
			t.Errorf("dispatchQuarantinedSkipLocked(wolf_kill,-1) returned %d (%s), want nil",
				e.Code, e.Message)
		}
	})
	if r.State.WolfKillTarget == initialWolfTarget {
		t.Fatalf("wolf_kill skip must have advanced the night phase; WolfKillTarget still %d",
			initialWolfTarget)
	}
	if int(r.State.WolfKillTarget) == 1 {
		t.Fatalf("wolf_kill skip must NOT kill fellow-wolf seat 1; got %d", r.State.WolfKillTarget)
	}
	if r.State.Phase != PhaseNightSeer {
		t.Fatalf("phase must advance to PhaseNightSeer after wolf_kill skip, got %s", r.State.Phase)
	}
}

// TestDispatchQuarantinedSkip_WolfKill_LoneWolfNoNonWolf is the pure lone-wolf
// edge case: all other seats already dead, no legal non-wolf target exists.
// The skip must return nil without crashing and without mutating WolfKillTarget.
func TestDispatchQuarantinedSkip_WolfKill_LoneWolfNoNonWolf(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseNightWolves
	r.State.Roles = [MaxPlayers]Role{
		RoleWerewolf, RoleWerewolf, RoleSeer, RoleWitch, RoleHunter, RoleVillager, RoleVillager,
	}
	// Kill everyone except the lone wolf at seat 0.
	for i := 1; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = false
	}
	initialWolfTarget := r.State.WolfKillTarget
	runWithDeadlockGuard(t, "dispatchQuarantinedSkipLocked(wolf_kill,-1) with lone wolf", func() {
		if e := m.dispatchQuarantinedSkipLocked(r, 0, "wolf_kill", -1); e != nil {
			t.Errorf("lone-wolf skip must return nil, got %d (%s)", e.Code, e.Message)
		}
	})
	if r.State.WolfKillTarget != initialWolfTarget {
		t.Fatalf("lone-wolf skip must NOT change WolfKillTarget; was %d, now %d",
			initialWolfTarget, r.State.WolfKillTarget)
	}
}
