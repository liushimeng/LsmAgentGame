// Package werewolf — regression test for BUG-R228-P0 (Round 228).
//
// R228 (report 20260801_215242) observed the night_wolves phase in a 13-AI
// full-room stalling ~4-8 minutes per stuck night. Root cause: when one wolf
// bot is permanently broken (e.g. MinMax-model sitting in model_400_circuit
// open), the watchdog's wolf_kill skip path casts the stuck wolf's vote and
// wakes the other wolves — but if THOSE other wolves are ALSO broken
// (quarantined / circuit-open), they never answer the wake, allWolvesVoted()
// stays false, and the phase has to wait for the NEXT watchdog tick (120s)
// to force-tally. So one stuck night = 360s (general deadline) + 120s (force
// tally gate) ≈ 8 minutes of cumulative stall across the match.
//
// Fix (BUG-R228-P0): in the wolf_kill locked variant (the watchdog skip
// path), after the acting wolf's vote is cast, scan all other living wolves;
// if any non-voter is permanently broken (IsQuarantined() or
// ConsecutiveFailures() >= permanentQuarantineThreshold), auto-mark them as
// abstained (target=NoSeat, WolfVoteCast=true). If this brings
// allWolvesVoted() to true, force-tally immediately — same watchdog tick, no
// 120s extra wait.
//
// These tests pin both halves of the fix:
//   (a) autoVoteStuckWolvesLocked correctly abstains a quarantined wolf.
//   (b) wolfKillLocked for a stuck wolf + a quarantined sibling forces the
//       tally in one call (no need to wait 120s for the next tick).
package werewolf

import (
	"testing"

	"LsmAgentGame/agent/wwplayer"
)

// TestAutoVoteStuckWolvesLocked_QuarantinedSibling pins path (a): when the
// watchdog skip path has cast one wolf's vote, the helper abstains another
// wolf that is permanently broken (IsQuarantined()=true) and reports
// allWolvesVoted()=true so the caller can force-tally on the same tick.
func TestAutoVoteStuckWolvesLocked_QuarantinedSibling(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	// Collect the wolf seats from the seed layout. fillAndStart uses seed=1
	// (7-player compatible layout): we just iterate Roles and pick wolves.
	var wolves []int
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleWerewolf {
			wolves = append(wolves, i)
		}
	}
	if len(wolves) < 2 {
		r.mu.Unlock()
		t.Fatalf("seed layout needs >=2 wolves, got %d (wolves=%v)", len(wolves), wolves)
	}
	stuckSeat := wolves[0]
	quarantinedSeat := wolves[1]

	// Force night_wolves with all wolves alive but none voted.
	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(stuckSeat)
	for i := range r.State.WolfVoteCast {
		r.State.WolfVoteCast[i] = false
		r.State.WolfVotes[i] = NoSeat
	}
	for i := 0; i < 7; i++ {
		r.State.Players[i].Alive = true
	}

	// Register bots on every seat so the helper can inspect them.
	r.BotAgents = make(map[int]*wwplayer.Agent)
	for i := 0; i < 7; i++ {
		bot, _ := stubBotWithChannel(i)
		bot.ModelKey = "Test-model"
		r.BotAgents[i] = bot
	}
	// Simulate the stuck + quarantined wolves. All other wolves (if any
	// beyond the first two) are also quarantined so allWolvesVoted() can
	// succeed after the helper marks them abstain.
	r.BotAgents[stuckSeat].ModelKey = "Stuck-model"
	r.BotAgents[quarantinedSeat].ModelKey = "Quarantined-model"
	r.BotAgents[quarantinedSeat].SetQuarantined()
	for _, w := range wolves[2:] {
		r.BotAgents[w].ModelKey = "Other-wolf"
		r.BotAgents[w].SetQuarantined()
	}

	// Pre-mark the stuck wolf as already-cast (mirrors the post-NightWolfKill
	// state where the acting wolf's vote is in). Helper then abstains the
	// rest and reports allWolvesVoted()=true.
	r.State.WolfVoteCast[stuckSeat] = true
	r.State.WolfVotes[stuckSeat] = NoSeat

	// Run the helper with the stuck wolf as the just-voted actor.
	gotAllVoted := r.autoVoteStuckWolvesLocked(Seat(stuckSeat))

	if !gotAllVoted {
		r.mu.Unlock()
		t.Fatalf("expected autoVoteStuckWolvesLocked to return true (all " +
			"wolves accounted for), got false — helper did not treat the " +
			"quarantined sibling as 'voted abstain'")
	}
	if !r.State.WolfVoteCast[quarantinedSeat] {
		r.mu.Unlock()
		t.Fatalf("expected quarantined sibling WolfVoteCast=true (abstain), got false")
	}
	if r.State.WolfVotes[quarantinedSeat] != NoSeat {
		r.mu.Unlock()
		t.Fatalf("expected quarantined sibling WolfVotes=NoSeat (abstain), got %d",
			r.State.WolfVotes[quarantinedSeat])
	}
	r.mu.Unlock()
}

// TestWolfKillLocked_AllWolvesBrokenForcesImmediateTally pins path (b): one
// wolf bot is stuck, another wolf bot is quarantined, the watchdog calls
// wolfKillLocked for the stuck wolf — the fix must force-tally in this same
// call (without waiting 120s for the next watchdog tick).
func TestWolfKillLocked_AllWolvesBrokenForcesImmediateTally(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	var wolves []int
	r.mu.Lock()
	for i := 0; i < 7; i++ {
		if r.State.Roles[i] == RoleWerewolf {
			wolves = append(wolves, i)
		}
	}
	if len(wolves) < 2 {
		r.mu.Unlock()
		t.Fatalf("seed layout needs >=2 wolves, got %d (wolves=%v)", len(wolves), wolves)
	}
	stuckSeat := wolves[0]
	quarantinedSeat := wolves[1]

	// Build the room with two broken wolves and no other wolves present.
	// Easiest: drop the remaining wolves (if any) so only these two are
	// alive wolves; then "kill" all non-wolf seats to keep the kill target
	// pool legal. But fillAndStart uses a 7-player layout — we'll keep all
	// players alive and just verify the helper short-circuits the tally.
	r.State.Phase = PhaseNightWolves
	r.State.DayNumber = 1
	r.State.TurnActingSeat = Seat(stuckSeat)
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
		bot.ModelKey = "Test-model"
		r.BotAgents[i] = bot
	}
	r.BotAgents[stuckSeat].ModelKey = "Stuck-wolf"
	r.BotAgents[quarantinedSeat].ModelKey = "Quarantined-wolf"
	r.BotAgents[quarantinedSeat].SetQuarantined()
	// Quarantine any other wolves (3+ wolf layouts) so the helper can
	// force-tally on the first call.
	for _, w := range wolves[2:] {
		r.BotAgents[w].ModelKey = "Other-wolf"
		r.BotAgents[w].SetQuarantined()
	}

	// Pre-mark the acting wolf as already-cast (since the watchdog skip
	// path resets & re-casts). This mirrors the post-NightWolfKill state
	// where the acting wolf has its vote in.
	r.State.WolfVoteCast[stuckSeat] = false

	// Pick any legal non-wolf target for the kill.
	var killTarget Seat = -1
	for i := 0; i < 7; i++ {
		if Seat(i) == Seat(stuckSeat) {
			continue
		}
		if r.State.Roles[i] == RoleWerewolf {
			continue
		}
		if r.State.AliveSeat(Seat(i)) {
			killTarget = Seat(i)
			break
		}
	}
	if killTarget < 0 {
		r.mu.Unlock()
		t.Fatalf("could not find a legal non-wolf target in seed layout")
	}
	stuckUserID := r.Seats[stuckSeat]
	r.mu.Unlock()

	// Invoke the locked wolf-kill entry point (caller must hold r.mu, but
	// the manager method itself doesn't acquire it — see existing pattern
	// in dispatchQuarantinedSkipLocked).
	err := m.wolfKillLocked(r, stuckUserID, killTarget)
	if err != nil {
		t.Fatalf("wolfKillLocked returned unexpected error: %v", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// After the fix: phase MUST advance out of night_wolves in a single
	// call (no need to wait the 120s force-tally gate).
	if r.State.Phase == PhaseNightWolves {
		t.Fatalf("BUG-R228-P0 regression: night_wolves stuck after wolfKillLocked " +
			"with a quarantined sibling wolf — should force-tally in same call " +
			"(watchdog should not have to wait another 120s tick)")
	}
	// And the quarantined sibling's vote should be auto-marked as abstain.
	if !r.State.WolfVoteCast[quarantinedSeat] {
		t.Fatalf("expected quarantined sibling WolfVoteCast=true (auto-abstain), got false")
	}
	if r.State.WolfVotes[quarantinedSeat] != NoSeat {
		t.Fatalf("expected quarantined sibling WolfVotes=NoSeat, got %d",
			r.State.WolfVotes[quarantinedSeat])
	}
	// Acting wolf's vote must be the picked target (engine default path).
	if r.State.WolfVoteCast[stuckSeat] != true {
		t.Fatalf("expected acting wolf WolfVoteCast=true after wolfKillLocked")
	}
	if r.State.WolfVotes[stuckSeat] != killTarget {
		t.Fatalf("expected acting wolf WolfVotes=%d, got %d", killTarget,
			r.State.WolfVotes[stuckSeat])
	}
}