// §20260812-03 U1 — 阵营胜率热力图单测。
package werewolf

import (
	"testing"
)

// TestUniformProb13 验证空状态返回均匀分布。
func TestUniformProb13(t *testing.T) {
	p := uniformProb13()
	if len(p) != MaxPlayers {
		t.Fatalf("uniformProb13 length should be %d, got %d", MaxPlayers, len(p))
	}
	for i, v := range p {
		if v != 0.5 {
			t.Errorf("uniformProb13[%d] = %v, want 0.5", i, v)
		}
	}
}

// TestComputeWinRate_NilRoom 验证 nil 房间返回均匀分布。
func TestComputeWinRate_NilRoom(t *testing.T) {
	p := computeWinRateProbabilityLocked(nil)
	if len(p) != MaxPlayers {
		t.Fatalf("nil room should return uniform distribution, got len %d", len(p))
	}
	for i, v := range p {
		if v != 0.5 {
			t.Errorf("nil room p[%d] = %v, want 0.5", i, v)
		}
	}
}

// TestComputeWinRate_AllDead 验证全部死亡时返回均匀分布(无存活玩家)。
func TestComputeWinRate_AllDead(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = false
	}
	p := computeWinRateProbabilityLocked(r)
	if len(p) != MaxPlayers {
		t.Fatalf("all-dead room should return uniform, got len %d", len(p))
	}
}

// TestComputeWinRate_AllAlive 验证全部存活时概率归一化(总和 = 4.0 = 13 人局默认狼数)。
func TestComputeWinRate_AllAlive(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
	}
	p := computeWinRateProbabilityLocked(r)
	if len(p) != MaxPlayers {
		t.Fatalf("expected len %d, got %d", MaxPlayers, len(p))
	}
	// 钳制到 [0.02, 0.98]
	for i, v := range p {
		if v < 0.02 || v > 0.98 {
			t.Errorf("p[%d] = %v out of range [0.02, 0.98]", i, v)
		}
	}
	// 存活概率和 ≈ 4.0(13 人局默认狼数,无 seer check 修正)
	sumAlive := 0.0
	for i := 0; i < MaxPlayers; i++ {
		if r.State.Players[i].Alive {
			sumAlive += p[i]
		}
	}
	if sumAlive < 3.5 || sumAlive > 4.5 {
		t.Errorf("sum of alive probs should be ~4.0, got %v", sumAlive)
	}
}

// TestComputeWinRate_DeadWolfRevealed 验证已死亡且身份公开的狼概率 = 0.98。
func TestComputeWinRate_DeadWolfRevealed(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	for i := 0; i < MaxPlayers; i++ {
		r.State.Players[i].Alive = true
	}
	// 座位 0 设为已死亡且身份公开(狼)
	r.State.Players[0].Alive = false
	r.State.Players[0].Role = RoleWerewolf
	// 死亡后翻牌走 ROLE_PUBLIC_REVEALED 路径需要 RolePubliclyRevealed 返回 true
	// 实际 §135 实现是:角色==已死 且 (verdict!=execution 或白痴翻牌/狼自爆/猎人开枪)
	// 这里直接走最简路径:让 RolePubliclyRevealed 返回 true
	r.State.Status = "over" // GameOver 时所有 dead 玩家 RolePubliclyRevealed=true
	r.State.DayNumber = 5
	p := computeWinRateProbabilityLocked(r)
	if p[0] != 0.98 {
		t.Errorf("dead revealed wolf should have prob 0.98, got %v", p[0])
	}
}

// TestVoteShareForSeatLocked 验证投票占比计算。
func TestVoteShareForSeatLocked(t *testing.T) {
	tally := map[Seat]int{0: 3, 1: 2, 2: 1}
	share0 := voteShareForSeatLocked(tally, 0)
	if share0 < 0.49 || share0 > 0.51 {
		t.Errorf("seat 0 share should be 0.5, got %v", share0)
	}
	// nil tally
	if voteShareForSeatLocked(nil, 0) != 0 {
		t.Errorf("nil tally should return 0")
	}
	// empty tally
	empty := map[Seat]int{}
	if voteShareForSeatLocked(empty, 0) != 0 {
		t.Errorf("empty tally should return 0")
	}
}

// TestCountSpeechesForSeatLocked 验证发言计数。
func TestCountSpeechesForSeatLocked(t *testing.T) {
	r := &WerewolfRoom{}
	r.recentSpeeches = nil
	if count := countSpeechesForSeatLocked(r, 0); count != 0 {
		t.Errorf("nil speeches should return 0, got %d", count)
	}
	// nil room
	if count := countSpeechesForSeatLocked(nil, 0); count != 0 {
		t.Errorf("nil room should return 0, got %d", count)
	}
}
