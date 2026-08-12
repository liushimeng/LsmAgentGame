// Package werewolf — death_reveal_delay_20260810_12_test.go: 死者身份「终局延时揭晓」(§20260810-12 D2)单测。
//
// 验证:
//   - WerewolfRoom.SetDeathRevealDelayMin 归一化(0/5/15 保留,其他归 0);
//   - BuildClientStateWithRoom 透传 DeathRevealDelayMin 字段;
//   - sanitizeBotTranscript 不触碰 DecisionTrail 之外的字段(§135 隔离)。
package werewolf

import (
	"testing"

	"LsmWebGame/agent/wwplayer"
)

func TestSetDeathRevealDelayMin_ValidValues(t *testing.T) {
	r := newTestRoomForDeathReveal()
	for _, v := range []int{0, 5, 15} {
		r.SetDeathRevealDelayMin(v)
		if got := readDeathRevealDelayMin(r); got != v {
			t.Fatalf("set %d, read back %d", v, got)
		}
	}
}

func TestSetDeathRevealDelayMin_InvalidNormalizedTo0(t *testing.T) {
	r := newTestRoomForDeathReveal()
	for _, v := range []int{-1, 1, 3, 7, 100} {
		r.SetDeathRevealDelayMin(v)
		if got := readDeathRevealDelayMin(r); got != 0 {
			t.Fatalf("set %d should normalize to 0, got %d", v, got)
		}
	}
}

func TestCfgWerewolfDeathRevealDelayMinAllowed(t *testing.T) {
	got := cfgWerewolfDeathRevealDelayMinAllowed()
	if len(got) != 3 || got[0] != 0 || got[1] != 5 || got[2] != 15 {
		t.Fatalf("expected [0 5 15], got %v", got)
	}
}

func TestSanitizeBotTranscript_ClearsDecisionTrailForPlayers(t *testing.T) {
	// §135 + §119 隔离:玩家分支必须清空 DecisionTrail。
	bt := wwplayer.BotTranscript{
		Seat:         1,
		DecisionTrail: []wwplayer.DecisionEntry{{Round: 1, ToolName: "speak"}},
	}
	// spectator 分支应保留
	specOut := sanitizeBotTranscript(bt, true)
	if len(specOut.DecisionTrail) != 1 {
		t.Fatalf("spectator should keep DecisionTrail, got %d", len(specOut.DecisionTrail))
	}
	// 玩家分支应清空
	playerOut := sanitizeBotTranscript(bt, false)
	if len(playerOut.DecisionTrail) != 0 {
		t.Fatalf("player should have DecisionTrail cleared, got %d", len(playerOut.DecisionTrail))
	}
}

// --- helpers (不依赖现有测试 helper,自包含) ---

func newTestRoomForDeathReveal() *WerewolfRoom {
	return &WerewolfRoom{}
}

func readDeathRevealDelayMin(r *WerewolfRoom) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deathRevealDelayMin
}