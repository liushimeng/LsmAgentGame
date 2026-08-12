// Package werewolf — regression test for BUG-R193-001 (Round 193).
//
// BUG: the day-vote auto-tally gate allAliveVoted() requires every *alive*
// seat to have voted. A quarantined bot is alive (Players[seat].Alive=true)
// but its LLM is broken so it can never set Voted=true. With 5+ quarantined
// bots in a 13-seat room the gate never becomes true and PhaseVote deadlocks
// — even though the 360s phase deadline / watchdog should recover, the acting
// seat selection (lowestAliveBotSeatLocked) does not exclude quarantined
// bots, so the rescue path can stall on a driver that can never be woken.
//
// Fix:
//   - New allActiveVoted() helper that excludes QuarantinedSeats[i].
//   - DayVote / dayVoteLocked auto-tally now uses allActiveVoted().
//   - New QuarantinedSeats [MaxPlayers]bool field on GameState, synced from
//     r.BotAgents[*].IsQuarantined() via syncQuarantinedLocked() before each
//     vote operation / context build.
//   - New lowestActiveBotSeatLocked helper (skips quarantined) replaces
//     lowestAliveBotSeatLocked in all driver-selection call sites
//     (watchdogActingSeat / buildAgentContextLocked / wakeAllAgentsLocked /
//     WakeActingAgentsLocked / notifyQuarantine / restart_vote paths).
//   - AllVoted reported to LLM prompt uses allActiveVoted().
package werewolf

import (
	"testing"

	"LsmWebGame/agent/wwplayer"
)

// TestBUG_R193_001_AllActiveVoted_ExcludesQuarantined verifies the core
// allActiveVoted() helper correctly excludes quarantined seats from the
// vote-completion gate.
func TestBUG_R193_001_AllActiveVoted_ExcludesQuarantined(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	// 7 alive seats. 3 of them (2,4,6) are quarantined.
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.State.QuarantinedSeats = [MaxPlayers]bool{
		2: true, 4: true, 6: true,
	}
	// All active (non-quarantined) seats voted.
	for _, i := range []int{0, 1, 3, 5} {
		r.State.Players[i].Voted = true
		r.State.Players[i].VoteTarget = Seat(1)
	}
	// allAliveVoted() must still be false (quarantined 2/4/6 never voted).
	if r.State.allAliveVoted() {
		t.Fatalf("allAliveVoted() must be false while quarantined seats haven't voted")
	}
	// allActiveVoted() must be true — active seats all voted.
	if !r.State.allActiveVoted() {
		t.Fatalf("allActiveVoted() must be true once all active seats voted, even with 3 quarantined")
	}
}

// TestBUG_R193_001_DayVote_AutoTally_WithQuarantined verifies that calling
// DayVote for the last active seat triggers the auto-tally (FinishVote)
// even though 3 alive-but-quarantined seats still haven't voted.
func TestBUG_R193_001_DayVote_AutoTally_WithQuarantined(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	// 7 alive seats; seats 2,4,6 are quarantined.
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.State.QuarantinedSeats = [MaxPlayers]bool{
		2: true, 4: true, 6: true,
	}
	// Active seats 0,1,3 voted; seat 5 is the last active not-yet-voted.
	for _, i := range []int{0, 1, 3} {
		r.State.Players[i].Voted = true
		r.State.Players[i].VoteTarget = Seat(1)
	}

	// Seat 5 (last active) votes — this should trigger FinishVote auto-tally.
	if e := r.State.DayVote(Seat(5), Seat(1)); e != nil {
		t.Fatalf("DayVote(5) returned error: %d (%s)", e.Code, e.Message)
	}
	// PhaseVote must have transitioned. With allActiveVoted() true, FinishVote
	// auto-tally fires immediately.
	if r.State.Phase == PhaseVote {
		t.Fatalf("PhaseVote must transition after last active seat voted, still phase=%s (BUG-R193-001)",
			r.State.Phase)
	}
}

// TestBUG_R193_001_DayVote_NoAutoTally_WhenActivePending verifies that
// allActiveVoted() is still false when an active seat hasn't voted yet — the
// gate must not fire prematurely (which would let votes go uncounted).
func TestBUG_R193_001_DayVote_NoAutoTally_WhenActivePending(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.State.QuarantinedSeats = [MaxPlayers]bool{
		2: true, 4: true,
	}
	// Active seats 0,1 voted; seats 3 and 5 active but not voted.
	for _, i := range []int{0, 1} {
		r.State.Players[i].Voted = true
		r.State.Players[i].VoteTarget = Seat(1)
	}

	if e := r.State.DayVote(Seat(6), Seat(1)); e != nil {
		t.Fatalf("DayVote(6) returned error: %d (%s)", e.Code, e.Message)
	}
	// Two active seats (3,5) still not voted → PhaseVote must persist.
	if r.State.Phase != PhaseVote {
		t.Fatalf("PhaseVote must persist while active seats haven't all voted, got phase=%s",
			r.State.Phase)
	}
}

// TestBUG_R193_001_LowestActiveBotSeatLocked_SkipsQuarantined verifies that
// the new driver-selection helper skips quarantined bots.
func TestBUG_R193_001_LowestActiveBotSeatLocked_SkipsQuarantined(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	// All 7 bots alive; seats 0 and 1 are quarantined.
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		if i == 0 || i == 1 {
			bot.SetQuarantined()
		}
		r.BotAgents[i] = bot
	}
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
	}

	got := lowestActiveBotSeatLocked(r)
	if got != 2 {
		t.Fatalf("lowestActiveBotSeatLocked() = %d, want 2 (skipping quarantined 0 and 1)", got)
	}
}

// TestBUG_R193_001_DayVoteLocked_DriverTally_WithQuarantined reproduces the
// exact BUG-R193-001 scenario: a full-AI 7-seat room with 3 quarantined bots
// in PhaseVote, where the last non-driver active bot votes via dayVoteLocked
// (the quarantined-skip dispatch path). Before the fix, allAliveVoted() stayed
// false and PhaseVote deadlocked. After the fix, allActiveVoted() is true and
// dayVoteLocked's driver-tally branch fires FinishVote to advance the phase.
func TestBUG_R193_001_DayVoteLocked_DriverTally_WithQuarantined(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.Phase = PhaseVote
	r.State.DayNumber = 1
	// All 7 seats alive; seats 3,5 quarantined. Active seats: 0,1,2,4,6.
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
		r.State.Players[i].Voted = false
		r.State.Players[i].VoteTarget = NoSeat
	}
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		if i == 3 || i == 5 {
			bot.SetQuarantined()
		}
		r.BotAgents[i] = bot
	}
	// Active seats 0,1,2,4 already voted; seat 6 is the last active not-voted.
	for _, i := range []int{0, 1, 2, 4} {
		r.State.Players[i].Voted = true
		r.State.Players[i].VoteTarget = Seat(1)
	}
	// Sync quarantine state into the engine.
	syncQuarantinedLocked(r)

	// Drive dayVoteLocked for seat 6 — the last active voter. This path is used
	// by the dispatchQuarantinedSkipLocked chain and the auto-tally branch
	// (BUG-WEREWOLF-P0-NEW-43).
	runWithDeadlockGuard(t, "dayVoteLocked(6) under r.mu", func() {
		if e := m.dayVoteLocked(r, r.Seats[6], Seat(1)); e != nil {
			t.Errorf("dayVoteLocked returned error: %d (%s)", e.Code, e.Message)
		}
	})
	// PhaseVote must have transitioned (or tied-vote status).
	if r.State.Phase == PhaseVote && r.State.Status != "over" && len(r.State.DayTiedPlayers) == 0 {
		t.Fatalf("PhaseVote must transition after all active voted, still phase=%s (BUG-R193-001)",
			r.State.Phase)
	}
}

// TestBUG_R193_001_LowestActiveBotSeatLocked_AllQuarantined returns -1 when
// every alive bot is quarantined — the watchdog must still be able to
// force-tally via the actingSeat<0 fallback path.
func TestBUG_R193_001_LowestActiveBotSeatLocked_AllQuarantined(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		bot.SetQuarantined()
		r.BotAgents[i] = bot
	}
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
	}
	if got := lowestActiveBotSeatLocked(r); got != -1 {
		t.Fatalf("lowestActiveBotSeatLocked() = %d, want -1 when all bots quarantined", got)
	}
}
