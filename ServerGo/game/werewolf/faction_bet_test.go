// §20260812-03 U3 — 阵营赌注系统单测。
package werewolf

import "testing"

// TestFactionBet_Constants 验证 §133 独立常量。
func TestFactionBet_Constants(t *testing.T) {
	if FactionBetMinAmount != 10 {
		t.Errorf("FactionBetMinAmount should be 10, got %d", FactionBetMinAmount)
	}
	if FactionBetMaxAmount != 500 {
		t.Errorf("FactionBetMaxAmount should be 500, got %d", FactionBetMaxAmount)
	}
	if FactionBetDestroyRate != 50 {
		t.Errorf("FactionBetDestroyRate should be 50%%, got %d", FactionBetDestroyRate)
	}
	if FactionBetPayoutRatio != 2 {
		t.Errorf("FactionBetPayoutRatio should be 2x, got %d", FactionBetPayoutRatio)
	}
	if FactionBetWindowSec != 30 {
		t.Errorf("FactionBetWindowSec should be 30, got %d", FactionBetWindowSec)
	}
}

// TestSettleBetsCore_Win 验证押中翻倍。
func TestSettleBetsCore_Win(t *testing.T) {
	bets := []FactionBetInput{
		{BetID: "b1", UserID: "u1", Amount: 100, PredictedFaction: "wolf", TargetSeat: 1},
		{BetID: "b2", UserID: "u2", Amount: 200, PredictedFaction: "good", TargetSeat: 2},
	}
	// 实际被票死的是 1 号(假设 1 号是狼)
	out := settleBetsCore(bets, "wolf")
	if len(out) != 2 {
		t.Fatalf("expected 2 settlements, got %d", len(out))
	}
	// b1 押 wolf,实际 wolf → win, Payout = 100 * 2 = 200
	if out[0].Result != "win" || out[0].Payout != 200 {
		t.Errorf("b1 should win with payout 200, got %+v", out[0])
	}
	// b2 押 good,实际 wolf → lose, Payout = 0
	if out[1].Result != "lose" || out[1].Payout != 0 {
		t.Errorf("b2 should lose with payout 0, got %+v", out[1])
	}
}

// TestSettleBetsCore_Lose 验证未押中 Payout = 0。
func TestSettleBetsCore_Lose(t *testing.T) {
	bets := []FactionBetInput{
		{BetID: "b1", UserID: "u1", Amount: 50, PredictedFaction: "wolf", TargetSeat: 3},
	}
	out := settleBetsCore(bets, "good")
	if len(out) != 1 {
		t.Fatalf("expected 1 settlement, got %d", len(out))
	}
	if out[0].Result != "lose" {
		t.Errorf("b1 should lose, got %s", out[0].Result)
	}
	if out[0].Payout != 0 {
		t.Errorf("lose payout should be 0, got %d", out[0].Payout)
	}
}

// TestFactionOfRole 验证角色→阵营映射。
func TestFactionOfRole(t *testing.T) {
	if got := FactionOfRole(RoleWerewolf); got != "wolf" {
		t.Errorf("werewolf should map to 'wolf', got %q", got)
	}
	if got := FactionOfRole(RoleVillager); got != "good" {
		t.Errorf("villager should map to 'good', got %q", got)
	}
	// 默认 fallback(未知角色 → good,不会误判为狼)
	if got := FactionOfRole(Role(99)); got != "good" {
		t.Errorf("unknown role should map to 'good', got %q", got)
	}
}
