package texasholdem

import "testing"

// ── helpers ──

func c(rank int) Card {
	return Card{Rank: rank, Suit: SuitSpade}
}

func heart(rank int) Card {
	return Card{Rank: rank, Suit: SuitHeart}
}

func diamond(rank int) Card {
	return Card{Rank: rank, Suit: SuitDiamond}
}

func club(rank int) Card {
	return Card{Rank: rank, Suit: SuitClub}
}

// ── 牌组测试 ──

func TestNewDeck(t *testing.T) {
	d := NewDeck()
	if len(d) != 52 {
		t.Fatalf("deck size = %d, want 52", len(d))
	}
	seen := map[[2]int]bool{}
	for _, card := range d {
		key := [2]int{card.Rank, card.Suit}
		if seen[key] {
			t.Fatalf("duplicate card: %+v", card)
		}
		seen[key] = true
	}
}

// ── 牌型评估测试 ──

func TestHandRank_AllCategories(t *testing.T) {
	cases := []struct {
		name     string
		hole     [2]Card
		community [5]Card
		wantCat  int
	}{
		{"high_card", [2]Card{c(RankA), heart(RankK)}, [5]Card{c(Rank9), c(Rank7), c(Rank5), diamond(Rank3), club(Rank2)}, 0},
		{"pair", [2]Card{c(RankA), heart(RankA)}, [5]Card{c(Rank9), c(Rank7), c(Rank5), diamond(Rank3), club(Rank2)}, 1},
		{"two_pair", [2]Card{c(RankA), heart(RankA)}, [5]Card{c(RankK), heart(RankK), c(Rank5), diamond(Rank3), club(Rank2)}, 2},
		{"three_of_kind", [2]Card{c(RankA), heart(RankA)}, [5]Card{c(RankA), c(Rank7), c(Rank5), diamond(Rank3), club(Rank2)}, 3},
		{"straight", [2]Card{c(Rank10), heart(RankJ)}, [5]Card{c(RankQ), c(RankK), c(RankA), diamond(Rank3), club(Rank2)}, 4},
		{"flush", [2]Card{c(RankA), c(RankK)}, [5]Card{c(Rank9), c(Rank7), c(Rank5), heart(Rank3), club(Rank2)}, 5},
		{"full_house", [2]Card{c(RankA), heart(RankA)}, [5]Card{c(RankK), c(RankK), c(RankK), diamond(Rank3), club(Rank2)}, 6},
		{"four_of_kind", [2]Card{c(RankA), heart(RankA)}, [5]Card{c(RankA), diamond(RankA), c(Rank5), diamond(Rank3), club(Rank2)}, 7},
		{"straight_flush", [2]Card{c(Rank9), c(Rank10)}, [5]Card{c(RankJ), c(RankQ), c(RankK), heart(Rank3), club(Rank2)}, 8},
		{"royal_flush", [2]Card{c(RankA), c(RankK)}, [5]Card{c(RankQ), c(RankJ), c(Rank10), heart(Rank3), club(Rank2)}, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rank := EvaluateBest5(tc.hole, tc.community)
			if rank.Category != tc.wantCat {
				t.Errorf("got category %d, want %d (tiebreak=%v)", rank.Category, tc.wantCat, rank.Tiebreak)
			}
		})
	}
}

func TestHandRank_CategoryOrdering(t *testing.T) {
	// 验证牌型大小关系
	ranks := []HandRank{
		{Category: 0, Tiebreak: [5]int{14, 13, 12, 11, 9}}, // high card
		{Category: 1, Tiebreak: [5]int{2, 14, 13, 12}},      // pair
		{Category: 2, Tiebreak: [5]int{3, 2, 14}},            // two pair
		{Category: 3, Tiebreak: [5]int{4, 14, 13}},           // three of kind
		{Category: 4, Tiebreak: [5]int{5}},                   // straight
		{Category: 5, Tiebreak: [5]int{14, 13, 12, 11, 9}},   // flush
		{Category: 6, Tiebreak: [5]int{6, 5}},                // full house
		{Category: 7, Tiebreak: [5]int{7, 14}},               // four of kind
		{Category: 8, Tiebreak: [5]int{9}},                   // straight flush
		{Category: 9, Tiebreak: [5]int{14}},                  // royal flush
	}
	for i := 0; i < len(ranks)-1; i++ {
		if ranks[i].Compare(ranks[i+1]) >= 0 {
			t.Errorf("category %d should be < category %d", ranks[i].Category, ranks[i+1].Category)
		}
	}
}

func TestHandRank_WheelStraight(t *testing.T) {
	// A-2-3-4-5 是顺子（top=5）
	hole := [2]Card{c(RankA), heart(Rank2)}
	community := [5]Card{c(Rank3), c(Rank4), c(Rank5), diamond(Rank9), club(Rank7)}
	rank := EvaluateBest5(hole, community)
	if rank.Category != 4 {
		t.Fatalf("wheel straight: got category %d, want 4", rank.Category)
	}
	if rank.Tiebreak[0] != 5 {
		t.Fatalf("wheel straight top = %d, want 5", rank.Tiebreak[0])
	}

	// Q-K-A-2-3 不是顺子
	hole2 := [2]Card{c(RankQ), heart(RankK)}
	community2 := [5]Card{c(RankA), diamond(Rank2), club(Rank3), heart(Rank9), c(Rank7)}
	rank2 := EvaluateBest5(hole2, community2)
	if rank2.Category == 4 {
		t.Fatalf("Q-K-A-2-3 should NOT be a straight, but got category 4")
	}
}

func TestHandRank_TwoPairTiebreak(t *testing.T) {
	// A-A-K-K-Q > A-A-Q-Q-K
	a := HandRank{Category: 2, Tiebreak: [5]int{14, 13, 12}}
	b := HandRank{Category: 2, Tiebreak: [5]int{14, 12, 13}}
	if a.Compare(b) <= 0 {
		t.Errorf("A-A-K-K-Q should beat A-A-Q-Q-K")
	}
}

func TestHandRank_FullHouseTiebreak(t *testing.T) {
	// K-K-K-7-7 > Q-Q-Q-A-A
	a := HandRank{Category: 6, Tiebreak: [5]int{13, 7}}
	b := HandRank{Category: 6, Tiebreak: [5]int{12, 14}}
	if a.Compare(b) <= 0 {
		t.Errorf("K-K-K-7-7 should beat Q-Q-Q-A-A")
	}
}

func TestHandRank_FlushKickers(t *testing.T) {
	a := HandRank{Category: 5, Tiebreak: [5]int{14, 13, 12, 11, 9}}
	b := HandRank{Category: 5, Tiebreak: [5]int{14, 13, 12, 11, 8}}
	if a.Compare(b) <= 0 {
		t.Errorf("flush A-K-Q-J-9 should beat A-K-Q-J-8")
	}
}

func TestHandRank_SevenCardBest(t *testing.T) {
	// 7 张牌含葫芦，评估应返回葫芦
	hole := [2]Card{c(RankA), heart(RankA)}
	community := [5]Card{c(RankA), diamond(RankK), club(RankK), heart(Rank2), c(Rank3)}
	rank := EvaluateBest5(hole, community)
	if rank.Category != 6 {
		t.Fatalf("got category %d, want 6 (full house)", rank.Category)
	}
	// 最佳应该是 A-A-A-K-K
	if rank.Tiebreak[0] != 14 || rank.Tiebreak[1] != 13 {
		t.Errorf("full house tiebreak = %v, want [14, 13]", rank.Tiebreak[:2])
	}
}

// ── 押注逻辑测试 ──

func newTestGame(blinds []int, stacks []int) *GameState {
	gs := NewGame(42, 200)
	for i, uid := range []string{"p0", "p1", "p2", "p3"} {
		if i < len(blinds) {
			gs.AddPlayer(uid, stacks[i])
		}
	}
	return gs
}

func TestBetting_CallAndCheck(t *testing.T) {
	gs := newTestGame([]int{200, 200, 200, 200}, []int{10000, 10000, 10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// UTG（座位 2）跟注 BB=200（preflop 先动是 BB+1）
	utg := gs.Turn
	if _, e := gs.ApplyAction(utg, Action{Type: ActCall}); e != nil {
		t.Fatalf("UTG call failed: %v", e)
	}

	// BB 应该能 check（如果无人加注）
	// 继续行动直到轮到 BB
	for gs.Turn != gs.nextActiveSeat(gs.nextActiveSeat(gs.Button)) && gs.Street == PhasePreflop {
		seat := gs.Turn
		if gs.CurrentBet > gs.Players[seat].RoundCommitted {
			gs.ApplyAction(seat, Action{Type: ActCall})
		} else {
			gs.ApplyAction(seat, Action{Type: ActCheck})
		}
	}

	// BB 应该能 check（当前下注额 = BB，BB 已经投入 BB）
	bb := gs.nextActiveSeat(gs.nextActiveSeat(gs.Button))
	if gs.CurrentBet == gs.Players[bb].RoundCommitted {
		if _, e := gs.ApplyAction(bb, Action{Type: ActCheck}); e != nil {
			t.Fatalf("BB check failed: %v", e)
		}
	} else {
		t.Logf("BB committed=%d, currentBet=%d, skip check test (different structure)", gs.Players[bb].RoundCommitted, gs.CurrentBet)
	}
}

func TestBetting_MinRaise(t *testing.T) {
	gs := newTestGame([]int{200, 200}, []int{10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// SB 先动（单挑），加注到 500（增量 300 ≥ BB=200）
	sb := gs.Turn
	_, e := gs.ApplyAction(sb, Action{Type: ActRaise, Amount: 500})
	if e != nil {
		t.Fatalf("raise to 500 failed: %v", e)
	}

	// BB 加注到 700（增量 200 < MinRaise=300）应该失败
	bb := gs.Turn
	_, e = gs.ApplyAction(bb, Action{Type: ActRaise, Amount: 700})
	if e == nil {
		t.Errorf("raise to 700 (increment=200 < 300) should have failed")
	}

	// 加注到 800（增量 300）应该成功
	_, e = gs.ApplyAction(bb, Action{Type: ActRaise, Amount: 800})
	if e != nil {
		t.Fatalf("raise to 800 failed: %v", e)
	}
}

func TestBetting_CheckRequiresNoBet(t *testing.T) {
	gs := newTestGame([]int{200, 200}, []int{10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	sb := gs.Turn
	// SB 尝试 check（但还有 BB 要跟）→ 应失败
	_, e := gs.ApplyAction(sb, Action{Type: ActCheck})
	if e == nil {
		t.Errorf("SB check when BB is pending should fail")
	}
}

func TestHandEnd_AllFold(t *testing.T) {
	gs := newTestGame([]int{200, 200, 200, 200}, []int{10000, 10000, 10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// UTG 和下一个玩家弃牌
	utg := gs.Turn
	gs.ApplyAction(utg, Action{Type: ActFold})
	next := gs.Turn
	gs.ApplyAction(next, Action{Type: ActFold})

	// 剩余两人：SB 和 BB；SB 跟注
	sb := gs.Turn
	if gs.Players[sb].Folded {
		t.Fatalf("SB should not be folded")
	}
	gs.ApplyAction(sb, Action{Type: ActFold})

	// BB 赢
	if len(gs.Winners) != 1 {
		t.Fatalf("expected 1 winner, got %d", len(gs.Winners))
	}
	if gs.Status != StatusOver {
		t.Fatalf("status should be over, got %v", gs.Status)
	}
}

func TestStreets_CommunityReveal(t *testing.T) {
	gs := newTestGame([]int{200, 200}, []int{10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// Preflop: 翻牌前公共牌 = 0
	if gs.CommunityShown != 0 {
		t.Fatalf("preflop community shown = %d, want 0", gs.CommunityShown)
	}

	// SB 跟注，BB check，进入 flop
	sb := gs.Turn
	gs.ApplyAction(sb, Action{Type: ActCall})
	bb := gs.Turn
	gs.ApplyAction(bb, Action{Type: ActCheck})

	if gs.Street != PhaseFlop {
		t.Fatalf("after preflop complete: street = %v, want flop", gs.Street)
	}
	if gs.CommunityShown != 3 {
		t.Fatalf("flop community shown = %d, want 3", gs.CommunityShown)
	}
}

// TestStartHand_ResetsCommunity 回归测试 BUG-TEXAS-DEALCOMMUNITY-OOB:
// 历史 Hand 走到 showdown 后 CommunityShown=5、Community[] 残留。
// 下一手 StartHand 必须清零，否则 advanceToNextStreet → dealCommunity(3) →
// Community[5] PANIC（runtime error: index out of range [5] with length 5）。
func TestStartHand_ResetsCommunity(t *testing.T) {
	gs := newTestGame([]int{200, 200}, []int{10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}
	// 强行把 Hand #1 推到 showdown
	gs.CommunityShown = 5
	gs.Street = PhaseShowdown
	gs.Status = StatusShowdown
	gs.Winners = []int{0}

	// 模拟 Hand #2 开始（room.go 5 秒后自动 StartHand）
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}
	if gs.CommunityShown != 0 {
		t.Fatalf("after StartHand: CommunityShown = %d, want 0 (regression: BUG-TEXAS-DEALCOMMUNITY-OOB)", gs.CommunityShown)
	}
	for i, c := range gs.Community {
		if c != (Card{}) {
			t.Fatalf("after StartHand: Community[%d] = %+v, want zero (regression: BUG-TEXAS-DEALCOMMUNITY-OOB)", i, c)
		}
	}
}

// TestDealCommunity_OOB_NoPanic 回归 BUG-TEXAS-DEALCOMMUNITY-OOB 防御层:
// 即使 CommunityShown 已被外部篡改到 >=5，dealCommunity 也必须 no-op 而非 PANIC。
func TestDealCommunity_OOB_NoPanic(t *testing.T) {
	gs := newTestGame([]int{200, 200}, []int{10000, 10000})
	gs.Button = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}
	// 模拟历史 Hand 残留
	gs.CommunityShown = 5
	for i := range gs.Community {
		gs.Community[i] = Card{Rank: 14, Suit: 1}
	}
	// dealCommunity 应当 no-op（不 panic、不写越界）
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("dealCommunity panicked on OOB (regression: BUG-TEXAS-DEALCOMMUNITY-OOB): %v", r)
		}
	}()
	gs.dealCommunity(3)
	if gs.CommunityShown != 5 {
		t.Fatalf("dealCommunity(3) on OOB should be no-op, got CommunityShown=%d", gs.CommunityShown)
	}
}

// ── Manager 测试 ──

func TestManager_JoinAndStart(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.BigBlind = 100
	mgr.StartStack = 5000

	room, started, e := mgr.JoinGame("r1", "alice")
	if e != nil {
		t.Fatal(e)
	}
	if started {
		t.Fatal("should not start with 1 player")
	}
	if room.Occupied() != 1 {
		t.Fatalf("occupied = %d, want 1", room.Occupied())
	}

	// 第二人加入 → 自动开始
	_, started, e = mgr.JoinGame("r1", "bob")
	if e != nil {
		t.Fatal(e)
	}
	if !started {
		t.Fatal("should start with 2 players")
	}

	// 幂等：重复加入
	_, started, e = mgr.JoinGame("r1", "alice")
	if e != nil {
		t.Fatal(e)
	}
	if started {
		t.Fatal("idempotent rejoin should not start new hand")
	}
}

func TestManager_GetState_HidesHole(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.BigBlind = 100
	mgr.StartStack = 5000
	mgr.JoinGame("r1", "alice")
	mgr.JoinGame("r1", "bob")

	// Alice 看自己状态
	state, e := mgr.GetState("r1", "alice")
	if e != nil {
		t.Fatal(e)
	}
	if len(state.MyHole) != 2 {
		t.Fatalf("alice my_hole = %d, want 2", len(state.MyHole))
	}
	// Bob 的底牌对 Alice 不可见
	for _, p := range state.Players {
		if p.UserID == "bob" {
			if len(p.Hole) != 0 {
				t.Fatalf("bob's hole should be hidden from alice, got %d cards", len(p.Hole))
			}
		}
	}
}

func TestManager_Resign(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.BigBlind = 100
	mgr.StartStack = 5000
	mgr.JoinGame("r1", "alice")
	mgr.JoinGame("r1", "bob")

	// Alice 认输 → Bob 赢
	room, e := mgr.Resign("r1", "alice")
	if e != nil {
		t.Fatal(e)
	}
	if room.State.Status != StatusOver {
		t.Fatalf("status should be over after resign, got %v", room.State.Status)
	}
	if len(room.State.Winners) != 1 {
		t.Fatalf("expected 1 winner, got %d", len(room.State.Winners))
	}
}

// ── 双人单挑测试 ──

func TestHeadsUp_ButtonIsSB(t *testing.T) {
	gs := NewGame(42, 200)
	gs.AddPlayer("p0", 10000)
	gs.AddPlayer("p1", 10000)
	gs.Button = -1 // 第一手 button=0 后 nextActiveSeat(-1) = 0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// 单挑：button = SB，对手 = BB
	if gs.Button != 0 && gs.Button != 1 {
		t.Fatalf("button should be 0 or 1, got %d", gs.Button)
	}
	// 先行动者 = SB（button）
	sb := gs.Button
	if gs.Turn != sb {
		t.Fatalf("preflop first actor in heads-up should be SB (button=%d), got turn=%d", sb, gs.Turn)
	}
}
