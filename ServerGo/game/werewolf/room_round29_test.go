// Package werewolf — regression tests for BUG-WEREWOLF-P0-NEW-16
// (Round 29 automated test report). Speak phase stuck after one or more
// auto-skipped seats: phase advanced to the next SpeakTurnSeat but the
// next acting agent never received a wake event, leaving the room
// permanently in "phase=speak round=N" with no recoverable event source.
//
// Root cause: Action_FinishSpeak previously only advanced gs.SpeakTurnSeat
// via NextSpeaker and returned. The wake to the NEXT acting seat was the
// caller's responsibility — agentRunner.FinishSpeak calls r.wakeAll()
// after success, and the WS path goes through broadcastWerewolfState →
// wakeWerewolfAgents. But the in-process dispatchQuarantinedSkipLocked
// path (used for permanently broken LLMs and any future in-process
// caller that forgets to wake) bypassed both — the phase advanced yet
// no agent was woken.
//
// Fix: Action_FinishSpeak now itself wakes the next acting seat using
// the lock-held variant wakeActingAgentsLocked. This makes the wake
// authoritative on the engine side rather than the caller side.
//
// These tests pin the contract:
//
//  1. finish_speak at the middle of the SpeakOrder wakes the next alive
//     speaker (the regression scenario).
//  2. finish_speak at the END of SpeakOrder (last alive speaker) wakes
//     the vote-phase driver — phase transitioned to PhaseVote and the
//     driver's bot channel receives a wake so vote can begin.
//  3. finish_speak by a seat NOT in SpeakOrder (e.g. stale caller /
//     post-restart snapshot) returns ErrNotYourTurn without touching
//     wakes or phase.
package werewolf

import (
	"testing"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
)

// stubBotWithChannel builds a minimal *wwplayer.Agent with a fresh buffered
// events channel (size 16, matching production). The Agent has no LLM,
// no memory, no Runner — we only need PushEvent to deliver to a channel
// we can drain and inspect. Caller is responsible for closing the
// channel after the test to release any wwplayer.Run goroutines that may
// have been started (we don't start any here).
func stubBotWithChannel(seat int) (*wwplayer.Agent, chan wwplayer.AgentEvent) {
	ch := make(chan wwplayer.AgentEvent, 16)
	a := &wwplayer.Agent{Seat: seat}
	a.SetEvents(ch)
	return a, ch
}

// drainOneEvent waits up to d for an event on ch. Returns the event and
// true on success, or zero/false on timeout. Used to assert "a wake was
// pushed" without coupling to internal timing details.
func drainOneEvent(t *testing.T, ch chan wwplayer.AgentEvent, label string) (wwplayer.AgentEvent, bool) {
	t.Helper()
	select {
	case evt := <-ch:
		return evt, true
	default:
		// No event ready — fail loudly so the regression scenario is obvious.
		t.Fatalf("expected wake event for %s but channel was empty (channel "+
			"buffer size 16, no concurrent reader — PushEvent should have "+
			"succeeded if Action_FinishSpeak woke the next speaker)", label)
		return wwplayer.AgentEvent{}, false
	}
}

// setupSpeakRoom constructs a 7-seat room with all 7 bots registered,
// kills seat 5 (so SpeakOrder = [0,1,2,3,4,6]), and forces the engine
// into PhaseSpeak with SpeakTurnSeat = 0. Returns the manager, roomID,
// room, and a map from seat → its bot's events channel for assertions.
func setupSpeakRoom(t *testing.T) (*WerewolfManager, string, *WerewolfRoom, map[int]chan wwplayer.AgentEvent) {
	t.Helper()
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Register bots 0..6 with channels so Action_FinishSpeak has something
	// to wake.
	channels := make(map[int]chan wwplayer.AgentEvent)
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for seat := 0; seat < MaxPlayers; seat++ {
		bot, ch := stubBotWithChannel(seat)
		r.BotAgents[seat] = bot
		channels[seat] = ch
	}

	// Kill seat 5 to mimic the R29 report's night_wolves → seer-killed scenario.
	// Use the engine's kill helper directly so roles stay consistent.
	if e := r.State.killPlayer(Seat(5), "wolf"); e != nil {
		t.Fatalf("kill seat 5: %v", e)
	}

	// Force the engine into PhaseSpeak with a deterministic SpeakOrder.
	r.State.Phase = PhaseSpeak
	r.State.DayNumber = 1
	r.State.SpeakOrder = []Seat{0, 1, 2, 3, 4, 6}
	r.State.SpeakTurnSeat = r.State.SpeakOrder[0] // seat 0
	r.State.Players[0].HasSpoken = false
	for _, s := range r.State.SpeakOrder[1:] {
		r.State.Players[s].HasSpoken = false
	}

	return m, roomID, r, channels
}

// TestAction_FinishSpeak_WakesNextSpeaker is the core regression test for
// BUG-WEREWOLF-P0-NEW-16. Before the fix, Action_FinishSpeak only advanced
// SpeakTurnSeat; it did NOT push a wake to the next speaker's agent
// channel. Caller paths (agentRunner.FinishSpeak via r.wakeAll, or
// ws/game_service.go via broadcastWerewolfState → wakeWerewolfAgents)
// covered most production paths, but dispatchQuarantinedSkipLocked (and
// any future in-process caller that forgets) silently skipped the wake.
//
// After the fix, Action_FinishSpeak itself wakes the next acting seat
// via wakeActingAgentsLocked. This test calls Action_FinishSpeak on seat
// 0 (the first speaker) and asserts that seat 1's bot channel receives a
// state_change event with SpeakTurn=1.
func TestAction_FinishSpeak_WakesNextSpeaker(t *testing.T) {
	m, roomID, r, channels := setupSpeakRoom(t)

	// Sanity: pre-conditions are correct.
	if r.State.SpeakTurnSeat != 0 {
		t.Fatalf("expected initial SpeakTurnSeat=0, got %d", r.State.SpeakTurnSeat)
	}
	if r.State.Phase != PhaseSpeak {
		t.Fatalf("expected PhaseSpeak, got %v", r.State.Phase)
	}

	if _, e := m.Action_FinishSpeak(roomID, r.Seats[0]); e != nil {
		t.Fatalf("Action_FinishSpeak returned err: %d (%s)", e.Code, e.Message)
	}

	// Phase advanced — SpeakTurnSeat should now be seat 1.
	if r.State.SpeakTurnSeat != 1 {
		t.Fatalf("expected SpeakTurnSeat=1 after first finish_speak, got %d",
			r.State.SpeakTurnSeat)
	}

	// CRITICAL: seat 1's bot channel must have received a wake. Before the
	// fix this was the caller's responsibility; the regression report
	// observed exactly this case where the channel stayed empty.
	evt, ok := drainOneEvent(t, channels[1], "seat 1 (next speaker)")
	if !ok {
		return
	}
	if evt.Kind != "state_change" {
		t.Errorf("expected kind=state_change, got %q", evt.Kind)
	}
	if evt.Context.SpeakTurn != 1 {
		t.Errorf("expected Context.SpeakTurn=1, got %d", evt.Context.SpeakTurn)
	}
	if !evt.Context.MyTurn {
		t.Errorf("expected Context.MyTurn=true for the next speaker, got false")
	}

	// Other alive seats should NOT have been woken (WakeActingAgents only
	// pushes to the acting seat). seat 0's channel may or may not have a
	// stale event from earlier; the contract is "no NEW wake from this
	// Action_FinishSpeak" — drain any pre-existing ones and verify the
	// channel is empty after that.
	for seat, ch := range channels {
		if seat == 1 || !r.State.AliveSeat(Seat(seat)) {
			continue
		}
		select {
		case evt := <-ch:
			t.Errorf("seat %d (non-acting) received unexpected wake: "+
				"kind=%q SpeakTurn=%d MyTurn=%v — WakeActingAgents should "+
				"only push to the acting seat",
				seat, evt.Kind, evt.Context.SpeakTurn, evt.Context.MyTurn)
		default:
			// expected: channel empty
		}
	}
}

// TestAction_FinishSpeak_LastSpeakerWakesVoteDriver covers the boundary
// where the last alive speaker calls finish_speak: NextSpeaker returns
// NoSeat and Phase transitions to PhaseVote. The wake that
// wakeActingAgentsLocked pushes must reach the vote-phase driver (the
// lowest alive bot seat, i.e. seat 0) so the driver can call finish_vote
// once everyone has voted. Without the fix, the room would sit in
// PhaseVote with no bot MyTurn=true and no recoverable event.
func TestAction_FinishSpeak_LastSpeakerWakesVoteDriver(t *testing.T) {
	m, roomID, r, channels := setupSpeakRoom(t)

	// Manually mark every speaker except the last (seat 6) as already
	// spoken, then position SpeakTurnSeat on the last alive speaker (seat 6).
	r.mu.Lock()
	for _, s := range []Seat{0, 1, 2, 3, 4} {
		r.State.Players[s].HasSpoken = true
	}
	r.State.SpeakTurnSeat = 6
	r.mu.Unlock()

	if _, e := m.Action_FinishSpeak(roomID, r.Seats[6]); e != nil {
		t.Fatalf("Action_FinishSpeak returned err: %d (%s)", e.Code, e.Message)
	}

	// Phase should have transitioned to PhaseVote (NextSpeaker exhausted
	// SpeakOrder).
	if r.State.Phase != PhaseVote {
		t.Fatalf("expected PhaseVote after last speaker's finish_speak, got %v",
			r.State.Phase)
	}
	if r.State.SpeakTurnSeat != NoSeat {
		t.Fatalf("expected SpeakTurnSeat=NoSeat after vote transition, got %d",
			r.State.SpeakTurnSeat)
	}

	// Vote-phase driver = lowest alive bot seat = 0. Its channel should
	// have a wake with MyTurn=true so it can call finish_vote once all
	// alive seats have voted.
	evt, ok := drainOneEvent(t, channels[0], "seat 0 (vote driver)")
	if !ok {
		return
	}
	if evt.Context.Phase != "vote" {
		t.Errorf("expected Context.Phase=vote, got %q", evt.Context.Phase)
	}
	if !evt.Context.MyTurn {
		t.Errorf("expected Context.MyTurn=true for the vote driver, got false")
	}
}

// TestAction_FinishSpeak_NotCurrentSpeaker_NoWake pins the negative path:
// a stale caller (e.g. a duplicate finish_speak envelope, or a wake
// delivered to the wrong seat) that calls Action_FinishSpeak with the
// wrong userID must NOT advance the phase AND must NOT wake any bot.
// Without the guard, a confused wake could otherwise cause a second bot
// to fire finish_speak in parallel and double-advance.
func TestAction_FinishSpeak_NotCurrentSpeaker_NoWake(t *testing.T) {
	m, roomID, r, channels := setupSpeakRoom(t)

	// Drain any pre-existing wakes from setupSpeakRoom (none expected,
	// but defensive).
	for _, ch := range channels {
		select {
		case <-ch:
		default:
		}
	}

	// Current speaker is seat 0. Call Action_FinishSpeak as seat 2 — the
	// engine must reject because seat 2 != SpeakTurnSeat.
	_, e := m.Action_FinishSpeak(roomID, r.Seats[2])
	if e == nil {
		t.Fatalf("expected ErrNotYourTurn, got nil")
	}
	if e.Code != errcode.ErrNotYourTurn {
		t.Fatalf("expected ErrNotYourTurn (%d), got %d (%s)",
			errcode.ErrNotYourTurn, e.Code, e.Message)
	}

	// Phase must not have advanced.
	if r.State.SpeakTurnSeat != 0 {
		t.Fatalf("SpeakTurnSeat must not change on rejected finish_speak; got %d",
			r.State.SpeakTurnSeat)
	}
	if r.State.Phase != PhaseSpeak {
		t.Fatalf("Phase must not change on rejected finish_speak; got %v", r.State.Phase)
	}

	// No bot channel should have received a wake.
	for seat, ch := range channels {
		select {
		case evt := <-ch:
			t.Errorf("seat %d received unexpected wake after rejected "+
				"finish_speak: kind=%q phase=%q — engine must not push wakes "+
				"when the action is rejected", seat, evt.Kind, evt.Context.Phase)
		default:
			// expected
		}
	}
}

// TestAction_FinishSpeak_QuarantinedActingBot_DoesNotDoubleAdvance pins
// the safety net that mirrors the existing dispatchQuarantinedSkipLocked
// path: when the next speaker is quarantined, wakeActingAgentsLocked
// must dispatch the in-process skip on the quarantined bot's behalf
// (rather than pushing a wake that the bot's IsQuarantined() guard
// would silently drop). This makes the quarantine-skip path
// indistinguishable from the agentRunner.FinishSpeak path in terms of
// forward progress.
func TestAction_FinishSpeak_QuarantinedActingBot_DispatchesSkip(t *testing.T) {
	m, roomID, r, channels := setupSpeakRoom(t)

	// Quarantine seat 1 (the next speaker after seat 0).
	r.BotAgents[1].SetQuarantined()

	if _, e := m.Action_FinishSpeak(roomID, r.Seats[0]); e != nil {
		t.Fatalf("Action_FinishSpeak returned err: %d (%s)", e.Code, e.Message)
	}

	// Phase must have advanced THROUGH seat 1 (because the quarantined
	// acting-bot path dispatched finish_speak on seat 1's behalf) to
	// seat 2.
	if r.State.SpeakTurnSeat != 2 {
		t.Fatalf("expected SpeakTurnSeat=2 after quarantined seat-1 skip, got %d",
			r.State.SpeakTurnSeat)
	}

	// Seat 1's channel must NOT have received a wake — quarantined agents
	// ignore wakes per run.go's IsQuarantined() guard, so the manager
	// dispatches the skip directly.
	select {
	case evt := <-channels[1]:
		t.Errorf("quarantined seat 1 received unexpected wake: kind=%q — "+
			"the manager should have dispatched skip in-place, not pushed "+
			"a wake that the quarantine guard would drop", evt.Kind)
	default:
		// expected
	}

	// Seat 2's channel SHOULD have a wake (the new acting seat after
	// seat 1 was auto-skipped).
	if _, ok := drainOneEvent(t, channels[2], "seat 2 (next-after-skip)"); !ok {
		return
	}
}

// TestWakeAllAgents_QuarantinedNonActingBot_DoesNotBlockSpeakerWake is the
// regression test for BUG-WEREWOLF-P0-NEW-27 (R32 automated test). During
// speak phase, a quarantined NON-acting bot (e.g. seat 6 GLM with a broken
// LLM provider) must NOT prevent the real current speaker (e.g. seat 5
// Xiaomi) from receiving a wake.
//
// Before the fix, the broadcast path (WakeAllAgents → wakeAllAgentsLocked)
// had no quarantine handling at all, and the in-process path
// (WakeActingAgents / wakeActingAgentsLocked) dispatched finish_speak for
// ANY quarantined bot regardless of whether it was actually the acting seat.
// When the quarantined bot's seat != SpeakTurnSeat the skip failed with
// [30008] "not current speaker"; the `continue` then skipped PushEvent for the
// real acting bot. With the acting bot's events channel buffer drained by
// earlier retries, its wake was dropped and the room dead-locked at
// "phase=speak round=N".
//
// After the fix, tryDispatchQuarantinedActingSkip only dispatches the skip
// when the quarantined bot is BOTH quarantined AND the acting seat
// (gc.MyTurn=true). Quarantined non-acting bots fall through to PushEvent,
// so the acting bot's wake is never skipped.
func TestWakeAllAgents_QuarantinedNonActingBot_DoesNotBlockSpeakerWake(t *testing.T) {
	m, _, r, channels := setupSpeakRoom(t)

	// The speaker order is [0,1,2,3,4,6] with SpeakTurnSeat=0. Quarantine
	// seat 6 — a NON-acting bot (current speaker is seat 0). Seat 6 is
	// alive but its LLM is broken, so it was quarantined earlier.
	r.mu.Lock()
	r.BotAgents[6].SetQuarantined()
	r.mu.Unlock()

	// Drain any pre-existing wakes from setupSpeakRoom.
	for _, ch := range channels {
		select {
		case <-ch:
		default:
		}
	}

	// Broadcast a "state_change": this is the path that
	// broadcastWerewolfState → wakeWerewolfAgents takes. It must wake the
	// acting seat (seat 0) even though seat 6 is quarantined.
	m.WakeAllAgents(r.RoomID, "state_change", wwtypes.GameContext{Phase: "speak"})

	// Acting seat (seat 0) MUST have received a wake. This is the
	// regression: before the fix this channel stayed empty because the
	// quarantine handling skipped PushEvent for everyone.
	evt, ok := drainOneEvent(t, channels[0], "seat 0 (current speaker)")
	if !ok {
		return
	}
	if evt.Kind != "state_change" {
		t.Errorf("expected kind=state_change, got %q", evt.Kind)
	}
	if evt.Context.SpeakTurn != 0 {
		t.Errorf("expected SpeakTurn=0 (current speaker), got %d", evt.Context.SpeakTurn)
	}
	if !evt.Context.MyTurn {
		t.Errorf("expected MyTurn=true for the current speaker, got false")
	}
	if evt.Context.Phase != "speak" {
		t.Errorf("expected phase=speak, got %q", evt.Context.Phase)
	}

	// Quarantined non-acting bot (seat 6) may or may not receive a PushEvent
	// (handleEvent's IsQuarantined guard drops it either way). The key
	// contract for P0-NEW-27 is that seat 6 did NOT get an in-place skip
	// dispatched — i.e., the engine state is unchanged by the wake. Verify
	// phase unchanged + SpeakTurnSeat still 0 (skip for the wrong seat would
	// have failed and not advanced; but if the engine HAD advanced, the test
	// above would see a different SpeakTurn).
	r.mu.Lock()
	phase := r.State.Phase
	speakTurn := r.State.SpeakTurnSeat
	r.mu.Unlock()
	if phase != PhaseSpeak {
		t.Errorf("phase must not change from just a wake (no action taken); got %v", phase)
	}
	if speakTurn != 0 {
		t.Errorf("SpeakTurnSeat must not change from just a wake; got %d", speakTurn)
	}
}

// TestDispatchQuarantinedSkip_VoteSkip_AbstainsNotSelfVote is the regression
// test for BUG-WEREWOLF-P0-NEW-35 (Round 35 automated test). In the vote
// phase, a quarantined acting bot must be auto-skipped via vote_skip so the
// phase can advance. The engine rejects self-votes ("cannot vote self"), so
// the skip MUST register an弃权 (Vote(NoSeat)) — matching tools.go's agent-
// side vote_skip mapping.
//
// Before the fix, dispatchQuarantinedSkipLocked's "vote_skip" case called
// Action_DayVote(roomID, userID, Seat(seat)) — a self-vote that the engine
// rejected. The bot's Voted flag stayed false, so allAliveVoted() never
// returned true and FinishVote never fired: the room dead-locked at
// PhaseVote with no recoverable event source.
//
// After the fix, the case calls Action_DayVote(roomID, userID, NoSeat),
// which the engine accepts as a弃权 (Voted=true, VoteTarget=NoSeat). The
// vote phase can then complete once every alive bot has voted.
func TestDispatchQuarantinedSkip_VoteSkip_AbstainsNotSelfVote(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Force the engine into PhaseVote with all 7 seats alive and no votes
	// cast. DayNumber must be >= 1 so the vote phase is a real daytime vote.
	r.mu.Lock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	// BUG-WEREWOLF-P0-NEW-42 (Round 37): production callers
	// (wakeActingAgentsLocked / notifyQuarantine) ALWAYS hold r.mu when
	// invoking dispatchQuarantinedSkipLocked. The previous version of this
	// test released r.mu first - which is why it never caught the R37
	// self-deadlock (the "vote_skip" case called Action_DayVote -> r.mu.Lock()
	// while r.mu was already held => permanent freeze). We now hold r.mu to
	// match production; dayVoteLocked (the lock-held variant) skips the
	// re-Lock and returns nil.
	defer r.mu.Unlock()
	if e := m.dispatchQuarantinedSkipLocked(r, 3, "vote_skip", 0); e != nil {
		t.Fatalf("dispatchQuarantinedSkipLocked(vote_skip) must succeed, got %d (%s)",
			e.Code, e.Message)
	}

	// CRITICAL: seat 3 must now be marked Voted with a弃权 target (NoSeat).
	// Before the P0-NEW-35 fix Voted stayed false → allAliveVoted() never
	// true → PhaseVote dead-locked.
	if !r.State.Players[3].Voted {
		t.Fatalf("seat 3 must be marked Voted after vote_skip; the engine rejected the self-vote and the bot's vote was never registered (P0-NEW-35 root cause)")
	}
	if r.State.Players[3].VoteTarget != NoSeat {
		t.Fatalf("vote_skip must register a弃权 (VoteTarget=NoSeat=%d), got %d — self-vote would have been rejected",
			NoSeat, r.State.Players[3].VoteTarget)
	}
}

// TestWakeActingAgents_QuarantinedNonActingBot_SkipsNotDispatched pins a
// subtler variant: WakeActingAgents also must NOT dispatch an in-place skip
// for a quarantined bot that isn't the acting seat. If it did, the skip
// (finish_speak) would fail with [30008], and the real acting bot would be
// left without a wake (the `continue` in the pre-fix code).
func TestWakeActingAgents_QuarantinedNonActingBot_SkipsNotDispatched(t *testing.T) {
	m, _, r, channels := setupSpeakRoom(t)

	// Seat 0 is current speaker. Quarantine seat 4 (also alive, further in
	// SpeakOrder). Seat 4 is NOT the acting seat.
	r.mu.Lock()
	r.BotAgents[4].SetQuarantined()
	r.mu.Unlock()

	// Drain any pre-existing wakes.
	for _, ch := range channels {
		select {
		case <-ch:
		default:
		}
	}

	// Use the lock-held variant because we already hold assertions; the
	// public API would also work.
	r.mu.Lock()
	m.wakeActingAgentsLocked(r, "state_change")
	r.mu.Unlock()

	// Acting seat (seat 0) MUST have received a wake — this is the
	// contract that P0-NEW-27 broke.
	if _, ok := drainOneEvent(t, channels[0], "seat 0 (current speaker)"); !ok {
		return
	}

	// Seat 4 (quarantined non-acting) may have received a PushEvent (which
	// handleEvent's IsQuarantined guard drops) — but must NOT have had an
	// in-place skip dispatched (that would have errored, but a successful
	// skip for seat 4 wouldn't change SpeakTurnSeat since SpeakTurn=0 != 4).
	// Verify speak state is unchanged: phase still speak, SpeakTurn still 0.
	r.mu.Lock()
	phase := r.State.Phase
	speakTurn := r.State.SpeakTurnSeat
	r.mu.Unlock()
	if phase != PhaseSpeak {
		t.Errorf("phase must not change; got %v", phase)
	}
	if speakTurn != 0 {
		t.Errorf("SpeakTurnSeat must not change; got %d", speakTurn)
	}
}