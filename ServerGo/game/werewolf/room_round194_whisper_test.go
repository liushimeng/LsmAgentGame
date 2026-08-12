// Package werewolf — regression test for BUG-R194-002 (Round 194 whisper leak).
//
// BUG: any alive bot could whisper to any other alive seat via the generic
// `whisper` tool, including wolf bots whispering wolf pack strategy
// (kill plan, vote concentration, hunter identity claims) to the human
// seer or other good-faction players. The public chat panel then renders
// these whispers as "🔒 私聊→ test_01" frames visible to the recipient —
// the human could read wolf coordination as if it were ordinary chat,
// completely breaking fairness.
//
// Fix: server-side cross-faction guard in (agentRunner).Whisper.
//   - wolf → wolf: allowed (legitimate wolf night council).
//   - wolf → good (incl. human seer/villager): rejected; LLM must
//     use wolf_whisper for wolf-team coordination.
//   - good → good: allowed (legitimate seer↔witch coordination).
//   - good → wolf: rejected (defence against wolf impersonation).
//
// Tests pin down the four cases and the "self" / "invalid seat" guards.
package werewolf

import (
	"strings"
	"testing"
)

// TestBUG_R194_002_Whisper_Blocks_WolfToGood verifies the core fix: a wolf
// bot whispering to a good-faction seat is rejected by the server with
// a message that nudges the LLM to use wolf_whisper instead.
func TestBUG_R194_002_Whisper_Blocks_WolfToGood(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	// Force roles: seat 0 = werewolf, seat 1 = seer (good faction human slot).
	r.mu.Lock()
	r.State.Roles[0] = RoleWerewolf
	r.State.Roles[1] = RoleSeer
	r.State.Players[0].Alive = true
	r.State.Players[1].Alive = true
	r.mu.Unlock()

	runner := newAgentRunnerForTest(r, 0, "bot_wolf", "wolf_acct", "DeepSeek-model")
	got, err := runner.Whisper(1, "今晚刀 2 号,统一票型")
	if err != nil {
		t.Fatalf("Whisper returned err=%v; want nil (rejection is returned as a string)", err)
	}
	if !strings.Contains(got, "rejected") {
		t.Fatalf("expected wolf→good whisper to be rejected, got %q", got)
	}
	if !strings.Contains(got, "wolf_whisper") {
		t.Fatalf("rejection should hint at wolf_whisper, got %q", got)
	}
	if strings.Contains(got, "刀") {
		t.Fatalf("rejection must NOT echo wolf strategy text; got %q", got)
	}
}

// TestBUG_R194_002_Whisper_Blocks_GoodToWolf verifies the mirror case:
// a good-faction bot whispering to a wolf is rejected (defends against
// impersonation / fishing for wolf identity).
func TestBUG_R194_002_Whisper_Blocks_GoodToWolf(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	r.mu.Lock()
	r.State.Roles[0] = RoleWerewolf
	r.State.Roles[1] = RoleSeer
	r.State.Players[0].Alive = true
	r.State.Players[1].Alive = true
	r.mu.Unlock()

	runner := newAgentRunnerForTest(r, 1, "bot_seer", "seer_acct", "GLM-model")
	got, err := runner.Whisper(0, "你到底是不是狼?")
	if err != nil {
		t.Fatalf("Whisper returned err=%v", err)
	}
	if !strings.Contains(got, "rejected") {
		t.Fatalf("expected good→wolf whisper to be rejected, got %q", got)
	}
	if !strings.Contains(got, "cross-faction") {
		t.Fatalf("rejection should mention cross-faction, got %q", got)
	}
}

// TestBUG_R194_002_Whisper_Allows_WolfToWolf verifies the legitimate
// wolf night council path still works: a wolf bot whispering to another
// wolf seat is allowed (the rejection string is absent and the call
// proceeds to the broadcast path, which fails only because chatSvc is nil).
func TestBUG_R194_002_Whisper_Allows_WolfToWolf(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	r.mu.Lock()
	r.State.Roles[0] = RoleWerewolf
	r.State.Roles[2] = RoleWerewolf
	r.State.Players[0].Alive = true
	r.State.Players[2].Alive = true
	r.mu.Unlock()

	// chatSvc is nil → runner.Whisper must return "chat unavailable" only
	// AFTER the faction guard; if guard mis-fired we'd see "rejected".
	runner := newAgentRunnerForTest(r, 0, "bot_wolf_a", "wolf_acct_a", "DeepSeek-model")
	got, _ := runner.Whisper(2, "今晚刀 5 号")
	if strings.Contains(got, "rejected") {
		t.Fatalf("wolf→wolf whisper must NOT be rejected by faction guard; got %q", got)
	}
	if got != "chat unavailable" {
		t.Fatalf("expected whisper to fall through to chatSvc path with \"chat unavailable\", got %q", got)
	}
}

// TestBUG_R194_002_Whisper_Allows_GoodToGood verifies the mirror
// good↔good path still works (seer↔witch coordination).
func TestBUG_R194_002_Whisper_Allows_GoodToGood(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	r.mu.Lock()
	r.State.Roles[1] = RoleSeer
	r.State.Roles[2] = RoleWitch
	r.State.Players[1].Alive = true
	r.State.Players[2].Alive = true
	r.mu.Unlock()

	runner := newAgentRunnerForTest(r, 1, "bot_seer", "seer_acct", "GLM-model")
	got, _ := runner.Whisper(2, "今晚我会查 Y,你别用毒他")
	if strings.Contains(got, "rejected") {
		t.Fatalf("good→good whisper must NOT be rejected by faction guard; got %q", got)
	}
	if got != "chat unavailable" {
		t.Fatalf("expected whisper to fall through to chatSvc path with \"chat unavailable\", got %q", got)
	}
}

// TestBUG_R194_002_Whisper_Blocks_Self ensures a bot cannot whisper to
// itself (would be a no-op / noisy loop bait).
func TestBUG_R194_002_Whisper_Blocks_Self(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	r.mu.Lock()
	r.State.Roles[0] = RoleWerewolf
	r.State.Players[0].Alive = true
	r.mu.Unlock()

	runner := newAgentRunnerForTest(r, 0, "bot_wolf", "wolf_acct", "DeepSeek-model")
	got, _ := runner.Whisper(0, "自言自语")
	if !strings.Contains(got, "self") {
		t.Fatalf("expected self-whisper to be rejected with 'self' hint, got %q", got)
	}
}

// TestBUG_R194_002_Whisper_Blocks_InvalidSeat ensures out-of-range toSeat
// is rejected before any state lookup.
func TestBUG_R194_002_Whisper_Blocks_InvalidSeat(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)
	runner := newAgentRunnerForTest(r, 0, "bot_wolf", "wolf_acct", "DeepSeek-model")
	got, _ := runner.Whisper(99, "x")
	if !strings.Contains(got, "invalid") {
		t.Fatalf("expected invalid-seat whisper to be rejected, got %q", got)
	}
	got, _ = runner.Whisper(-2, "x")
	if !strings.Contains(got, "invalid") {
		t.Fatalf("expected negative-seat whisper to be rejected, got %q", got)
	}
}

// newAgentRunnerForTest builds a minimal agentRunner wired to the given
// room but with chatSvc=nil (so the broadcast path is a no-op; tests only
// assert what happens BEFORE the broadcast).
func newAgentRunnerForTest(r *WerewolfRoom, seat int, botUserID, botAccount, modelKey string) *agentRunner {
	m := stubWWMgr()
	// ensure manager has this room
	if _, ok := m.rooms[r.RoomID]; !ok {
		m.rooms[r.RoomID] = r
	}
	return &agentRunner{
		mgr:        m,
		roomID:     r.RoomID,
		seat:       Seat(seat),
		botUserID:  botUserID,
		botAccount: botAccount,
		modelKey:   modelKey,
		// chatSvc left nil intentionally — tests assert the guard layer,
		// not the broadcast.
	}
}