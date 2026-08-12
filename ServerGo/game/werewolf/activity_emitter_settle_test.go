// Package werewolf — settlement (coin pot-split) unit tests.
//
// v2.0 (DEFECT 4): computeCoinDelta / countWinLose had ZERO test coverage, which
// is why the DEFECT 1 "wolf" vs "werewolf" mismatch (wolves-win mis-settled every
// player to -ante) survived undetected. These tests lock the contract:
//   - canonical winner string is "wolf" (engine.go:614) / "good" / "draw";
//   - wolves-win gives a POSITIVE delta to wolves and -ante to good (DEFECT 1 regression);
//   - the pot is bounded zero-sum: Σ delta ∈ (-winCount, 0] (integer-division
//     remainder stays with the house, never creates coins — FIX 4).
//
// See docs/狼人杀13人局金币系统设计.md §12 for the authoritative test matrix.
package werewolf

import "testing"

// setRoles fills a fresh GameState-backed room with the given per-seat roles.
// Unlisted seats stay RoleUnknown (FactionUnknown → excluded from win/lose counts).
func roomWithRoles(roles []Role) *WerewolfRoom {
	gs := &GameState{}
	for i, role := range roles {
		if i >= MaxPlayers {
			break
		}
		gs.Roles[i] = role
		gs.Players[i] = Player{Seat: Seat(i), Alive: true}
	}
	return &WerewolfRoom{State: gs}
}

// buildRoles13 = 4 wolves + (seer/witch/hunter/idiot) + 5 villagers = 13.
func buildRoles13() []Role {
	return []Role{
		RoleWerewolf, RoleWerewolf, RoleWerewolf, RoleWerewolf,
		RoleSeer, RoleWitch, RoleHunter, RoleIdiot,
		RoleVillager, RoleVillager, RoleVillager, RoleVillager, RoleVillager,
	}
}

// buildRoles12 = 4 wolves + (seer/witch/hunter) + 5 villagers = 12.
func buildRoles12() []Role {
	return []Role{
		RoleWerewolf, RoleWerewolf, RoleWerewolf, RoleWerewolf,
		RoleSeer, RoleWitch, RoleHunter,
		RoleVillager, RoleVillager, RoleVillager, RoleVillager, RoleVillager,
	}
}

// buildRoles7 = 3 wolves + 4 good (seer/witch/hunter/villager).
func buildRoles7() []Role {
	return []Role{
		RoleWerewolf, RoleWerewolf, RoleWerewolf,
		RoleSeer, RoleWitch, RoleHunter, RoleVillager,
	}
}

// zeroSumOverRoom sums computeCoinDelta over every faction-known seat and
// asserts the pot is bounded zero-sum: winners collectively receive at most what
// losers pay, and the undistributed remainder is in [0, winCount). i.e. the sum
// of all deltas ∈ (-winCount, 0]. Never positive (never creates coins).
func assertBoundedZeroSum(t *testing.T, roles []Role, winner string, winCount, loseCount, ante int64) {
	t.Helper()
	var sum int64
	for _, role := range roles {
		if FactionOf(role) == FactionUnknown {
			continue
		}
		sum += computeCoinDelta(winner, role, winCount, loseCount, ante)
	}
	if sum > 0 {
		t.Fatalf("pot not zero-sum: Σdelta=%d > 0 (coins created!) winner=%s roles=%d", sum, winner, len(roles))
	}
	if winCount > 0 && sum <= -winCount {
		t.Fatalf("pot leak too large: Σdelta=%d, want in (-%d, 0] winner=%s", sum, winCount, winner)
	}
}

// TestComputeCoinDelta_WolfWin_Regression is the DEFECT 1 regression: with the
// canonical winner="wolf", a WOLF role must get a POSITIVE delta and a GOOD role
// must get exactly -ante. On the old buggy code (comparing "werewolf"), the wolf
// branch was never taken → every seat got -ante → this test would FAIL.
func TestComputeCoinDelta_WolfWin_Regression(t *testing.T) {
	const ante int64 = 100
	// 13-player: 4 wolves win vs 9 good.
	winCount, loseCount := int64(4), int64(9)

	wolfDelta := computeCoinDelta("wolf", RoleWerewolf, winCount, loseCount, ante)
	if wolfDelta <= 0 {
		t.Fatalf("DEFECT 1 regression: wolf delta on winner=wolf = %d, want positive (=ante*lose/win=225)", wolfDelta)
	}
	if want := ante * loseCount / winCount; wolfDelta != want { // 100*9/4 = 225
		t.Fatalf("wolf delta = %d, want %d", wolfDelta, want)
	}

	goodDelta := computeCoinDelta("wolf", RoleVillager, winCount, loseCount, ante)
	if goodDelta != -ante {
		t.Fatalf("DEFECT 1 regression: good delta on winner=wolf = %d, want -%d", goodDelta, ante)
	}
	// A good SPECIAL role (seer) must also lose exactly ante, never win.
	if d := computeCoinDelta("wolf", RoleSeer, winCount, loseCount, ante); d != -ante {
		t.Fatalf("seer delta on winner=wolf = %d, want -%d", d, ante)
	}
}

// TestComputeCoinDelta_Matrix covers §12.1 rows 1-8.
func TestComputeCoinDelta_Matrix(t *testing.T) {
	const ante int64 = 100
	cases := []struct {
		name      string
		winner    string
		role      Role
		winCount  int64
		loseCount int64
		ante      int64
		want      int64
	}{
		// Row 1: wolf-win 13p (4/9): wolf +225.
		{"13p wolf-win wolf", "wolf", RoleWerewolf, 4, 9, ante, 225},
		{"13p wolf-win good", "wolf", RoleVillager, 4, 9, ante, -100},
		// Row 2: good-win 13p (9/4): good +44 (=100*4/9), wolf -100.
		{"13p good-win good", "good", RoleVillager, 9, 4, ante, 44},
		{"13p good-win wolf", "good", RoleWerewolf, 9, 4, ante, -100},
		// Row 3: draw → 0 for any role.
		{"draw wolf", "draw", RoleWerewolf, 0, 0, ante, 0},
		{"draw good", "draw", RoleVillager, 0, 0, ante, 0},
		// Row 4: wolf-win 7p (3/4): wolf +133 (=100*4/3).
		{"7p wolf-win wolf", "wolf", RoleWerewolf, 3, 4, ante, 133},
		{"7p wolf-win good", "wolf", RoleVillager, 3, 4, ante, -100},
		// Row 5: good-win 7p (4/3): good +75 (=100*3/4).
		{"7p good-win good", "good", RoleVillager, 4, 3, ante, 75},
		{"7p good-win wolf", "good", RoleWerewolf, 4, 3, ante, -100},
		// Row 7: ante=0 →博弈关闭 → 0.
		{"ante=0 wolf", "wolf", RoleWerewolf, 4, 9, 0, 0},
		{"ante<0 good", "good", RoleVillager, 9, 4, -5, 0},
		// Row 8: FactionUnknown (RoleUnknown) → 0 (conservative).
		{"unknown role wolf-win", "wolf", RoleUnknown, 4, 9, ante, 0},
		{"unknown role good-win", "good", RoleUnknown, 9, 4, ante, 0},
		// winCount<=0 guard on a winner role → 0.
		{"winCount=0 guard", "wolf", RoleWerewolf, 0, 9, ante, 0},
	}
	for _, c := range cases {
		if got := computeCoinDelta(c.winner, c.role, c.winCount, c.loseCount, c.ante); got != c.want {
			t.Errorf("%s: computeCoinDelta(%q,%v,%d,%d,%d) = %d, want %d",
				c.name, c.winner, c.role, c.winCount, c.loseCount, c.ante, got, c.want)
		}
	}
}

// TestComputeCoinDelta_AcceptsLegacyWerewolfString asserts isWolfWinner tolerates
// the historical/foreign "werewolf" spelling (robustness), while the engine's
// canonical value remains "wolf".
func TestComputeCoinDelta_AcceptsLegacyWerewolfString(t *testing.T) {
	const ante int64 = 100
	canonical := computeCoinDelta("wolf", RoleWerewolf, 4, 9, ante)
	legacy := computeCoinDelta("werewolf", RoleWerewolf, 4, 9, ante)
	if canonical != legacy {
		t.Fatalf("isWolfWinner robustness: 'wolf'=%d != 'werewolf'=%d", canonical, legacy)
	}
	if canonical != 225 {
		t.Fatalf("canonical wolf delta = %d, want 225", canonical)
	}
	if !isWolfWinner("wolf") || !isWolfWinner("werewolf") {
		t.Fatalf("isWolfWinner must accept both 'wolf' and 'werewolf'")
	}
	if isWolfWinner("good") || isWolfWinner("draw") || isWolfWinner("") {
		t.Fatalf("isWolfWinner must reject non-wolf winner strings")
	}
}

// TestComputeCoinDelta_BoundedZeroSum asserts the pot is bounded zero-sum across
// all seats for both factions and all seat counts (§12.2). Σdelta ∈ (-winCount, 0].
func TestComputeCoinDelta_BoundedZeroSum(t *testing.T) {
	const ante int64 = 100
	// 13p wolf-win: 4 win / 9 lose. Wolves each +225, 9 good each -100 → Σ=0 exactly.
	assertBoundedZeroSum(t, buildRoles13(), "wolf", 4, 9, ante)
	// 13p good-win: 9 win / 4 lose. 9 good each +44 (=396), 4 wolves each -100 (=-400)
	// → Σ=-4, remainder=4 stays with house (bounded loss, in (-9,0]).
	assertBoundedZeroSum(t, buildRoles13(), "good", 9, 4, ante)
	// 12p good-win: 8 good win / 4 wolf lose. 8*(100*4/8=50)=400, 4*-100=-400 → Σ=0.
	assertBoundedZeroSum(t, buildRoles12(), "good", 8, 4, ante)
	// 12p wolf-win: 4 wolf win / 8 good lose. 4*(100*8/4=200)=800, 8*-100=-800 → Σ=0.
	assertBoundedZeroSum(t, buildRoles12(), "wolf", 4, 8, ante)
	// 7p good-win: 4 win / 3 lose. 4*75=300, 3*-100=-300 → Σ=0.
	assertBoundedZeroSum(t, buildRoles7(), "good", 4, 3, ante)
	// 7p wolf-win: 3 win / 4 lose. 3*133=399, 4*-100=-400 → Σ=-1, in (-3,0].
	assertBoundedZeroSum(t, buildRoles7(), "wolf", 3, 4, ante)
}

// TestComputeCoinDelta_GoodWinRemainderExact pins the documented -4 remainder for
// the 13-player good-win case (§12.2 example): winners collectively receive 4 less
// than losers pay; the house keeps the remainder, no coins created.
func TestComputeCoinDelta_GoodWinRemainderExact(t *testing.T) {
	const ante int64 = 100
	winCount, loseCount := int64(9), int64(4) // 9 good win, 4 wolf lose
	perWinner := computeCoinDelta("good", RoleVillager, winCount, loseCount, ante)
	if perWinner != 44 { // 100*4/9 = 44
		t.Fatalf("good winner per-head = %d, want 44", perWinner)
	}
	distributed := perWinner * winCount // 44*9 = 396
	collected := ante * loseCount       // 100*4 = 400
	remainder := collected - distributed
	if remainder != 4 {
		t.Fatalf("remainder = %d, want 4 (bounded house截留)", remainder)
	}
	if remainder < 0 || remainder >= winCount {
		t.Fatalf("remainder %d out of bound [0,%d)", remainder, winCount)
	}
}

// TestCountWinLose covers §12.3: correct (winCount, loseCount) per faction/seat count.
func TestCountWinLose(t *testing.T) {
	cases := []struct {
		name             string
		roles            []Role
		winner           string
		wantWin, wantLos int
	}{
		{"13p wolf-win", buildRoles13(), "wolf", 4, 9},
		{"13p good-win", buildRoles13(), "good", 9, 4},
		{"13p legacy 'werewolf'", buildRoles13(), "werewolf", 4, 9}, // robustness
		{"12p good-win", buildRoles12(), "good", 8, 4},
		{"12p wolf-win", buildRoles12(), "wolf", 4, 8},
		{"7p wolf-win", buildRoles7(), "wolf", 3, 4},
		{"7p good-win", buildRoles7(), "good", 4, 3},
		{"draw", buildRoles13(), "draw", 0, 0},
	}
	for _, c := range cases {
		r := roomWithRoles(c.roles)
		win, los := countWinLose(r, c.winner)
		if win != c.wantWin || los != c.wantLos {
			t.Errorf("%s: countWinLose = (%d,%d), want (%d,%d)",
				c.name, win, los, c.wantWin, c.wantLos)
		}
	}
}

// TestCountWinLose_NilState guards the nil / empty-state early return.
func TestCountWinLose_NilState(t *testing.T) {
	if w, l := countWinLose(&WerewolfRoom{State: nil}, "wolf"); w != 0 || l != 0 {
		t.Fatalf("nil state countWinLose = (%d,%d), want (0,0)", w, l)
	}
}
