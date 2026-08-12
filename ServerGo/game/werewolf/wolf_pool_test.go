package werewolf

import (
	"testing"
)

// TestApplyWolfPoolPenalty_Suicide 测试 W-01:狼人自爆触发阵营金币池 -30。
func TestApplyWolfPoolPenalty_Suicide(t *testing.T) {
	gs := NewGame(0)
	// 设 3 号玩家为狼。
	gs.Roles[3] = RoleWerewolf
	gs.Players[3].Alive = true

	if got := gs.ApplyWolfPoolPenaltyLocked(3, DeathCauseSuicide); got != wolfPoolPenaltyCoin {
		t.Fatalf("expected penalty %d, got %d", wolfPoolPenaltyCoin, got)
	}
	if gs.WolfPoolBalance != -wolfPoolPenaltyCoin {
		t.Fatalf("expected WolfPoolBalance=-%d, got %d", wolfPoolPenaltyCoin, gs.WolfPoolBalance)
	}
	if !gs.WolfPoolPenaltyApplied[3] {
		t.Fatalf("expected WolfPoolPenaltyApplied[3]=true after suicide")
	}
}

// TestApplyWolfPoolPenalty_VoteElimination 测试 W-02:白天投票放逐触发阵营金币池 -30。
func TestApplyWolfPoolPenalty_VoteElimination(t *testing.T) {
	gs := NewGame(0)
	gs.Roles[5] = RoleWerewolf
	gs.Players[5].Alive = true

	if got := gs.ApplyWolfPoolPenaltyLocked(5, DeathCauseVote); got != wolfPoolPenaltyCoin {
		t.Fatalf("expected penalty %d, got %d", wolfPoolPenaltyCoin, got)
	}
	if gs.WolfPoolBalance != -wolfPoolPenaltyCoin {
		t.Fatalf("expected balance -%d, got %d", wolfPoolPenaltyCoin, gs.WolfPoolBalance)
	}
}

// TestApplyWolfPoolPenalty_NonTriggers 测试 W-03:狼夜间互杀/女巫毒杀/猎人开枪狼
// 均不触发阵营金币池扣款。
func TestApplyWolfPoolPenalty_NonTriggers(t *testing.T) {
	cases := []struct {
		name  string
		cause string
	}{
		{"wolf_kill", DeathCauseWolf},
		{"witch_poison", DeathCauseWitchPoison},
		{"hunter", DeathCauseHunter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gs := NewGame(0)
			gs.Roles[2] = RoleWerewolf
			gs.Players[2].Alive = true

			if got := gs.ApplyWolfPoolPenaltyLocked(2, c.cause); got != 0 {
				t.Fatalf("cause=%s should not trigger penalty, got %d", c.cause, got)
			}
			if gs.WolfPoolBalance != 0 {
				t.Fatalf("WolfPoolBalance must stay 0, got %d", gs.WolfPoolBalance)
			}
		})
	}
}

// TestApplyWolfPoolPenalty_Reentry 测试 W-04:同一死亡事件多次触发只扣一次。
func TestApplyWolfPoolPenalty_Reentry(t *testing.T) {
	gs := NewGame(0)
	gs.Roles[7] = RoleWerewolf
	gs.Players[7].Alive = true

	// 第一次扣款。
	if got := gs.ApplyWolfPoolPenaltyLocked(7, DeathCauseSuicide); got != wolfPoolPenaltyCoin {
		t.Fatalf("first penalty expected %d, got %d", wolfPoolPenaltyCoin, got)
	}
	// 第二次同 seat 同 cause → 重入保护,返回 0。
	if got := gs.ApplyWolfPoolPenaltyLocked(7, DeathCauseSuicide); got != 0 {
		t.Fatalf("reentry expected 0, got %d", got)
	}
	if gs.WolfPoolBalance != -wolfPoolPenaltyCoin {
		t.Fatalf("reentry must not double-deduct, got balance %d", gs.WolfPoolBalance)
	}
	// vote 也不能再触发(同 seat)。
	if got := gs.ApplyWolfPoolPenaltyLocked(7, DeathCauseVote); got != 0 {
		t.Fatalf("different cause on same seat expected 0, got %d", got)
	}
	if gs.WolfPoolBalance != -wolfPoolPenaltyCoin {
		t.Fatalf("balance still %d expected -%d", gs.WolfPoolBalance, wolfPoolPenaltyCoin)
	}
}

// TestApplyWolfPoolPenalty_ResetForNewGame 测试重置路径。
func TestApplyWolfPoolPenalty_ResetForNewGame(t *testing.T) {
	gs := NewGame(0)
	gs.Roles[2] = RoleWerewolf
	gs.ApplyWolfPoolPenaltyLocked(2, DeathCauseSuicide)
	gs.ResetWolfPoolForNewGameLocked()
	if gs.WolfPoolBalance != 0 {
		t.Fatalf("expected reset balance=0, got %d", gs.WolfPoolBalance)
	}
	for i, b := range gs.WolfPoolPenaltyApplied {
		if b {
			t.Fatalf("WolfPoolPenaltyApplied[%d] should reset to false", i)
		}
	}
}

// TestApplyWolfPoolPenalty_ViaKillPlayer 测试 killPlayer 集成路径:
//
// killPlayer(suicide) 应当顺带扣阵营金币池;killPlayer(wolf) 不扣。
func TestApplyWolfPoolPenalty_ViaKillPlayer(t *testing.T) {
	gs := NewGame(0)
	gs.DayNumber = 1
	gs.Roles[0] = RoleWerewolf
	gs.Players[0].Alive = true

	if err := gs.killPlayer(0, DeathCauseSuicide); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.WolfPoolBalance != -wolfPoolPenaltyCoin {
		t.Fatalf("expected WolfPoolBalance=-%d after suicide, got %d", wolfPoolPenaltyCoin, gs.WolfPoolBalance)
	}

	// 狼刀不扣(测试用 RoleVillager 充当被狼刀的狼,以免自爆重入保护阻断)。
	gs.Roles[1] = RoleWerewolf
	gs.Players[1].Alive = true
	if err := gs.killPlayer(1, DeathCauseWolf); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.WolfPoolBalance != -wolfPoolPenaltyCoin {
		t.Fatalf("WolfKill must not deduct, balance should stay -%d, got %d", wolfPoolPenaltyCoin, gs.WolfPoolBalance)
	}
}
