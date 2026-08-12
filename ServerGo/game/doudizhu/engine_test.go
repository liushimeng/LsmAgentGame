package doudizhu

import "testing"

// c 构造一张牌（默认黑桃，王用 SuitNone）。
func c(rank int) Card {
	if rank == RankSmall || rank == RankBig {
		return Card{Rank: rank, Suit: SuitNone}
	}
	return Card{Rank: rank, Suit: SuitSpade}
}

// cs 构造一手牌（避免同点同花色重复，逐张换花色）。
func cs(ranks ...int) []Card {
	seen := map[int]int{}
	suits := []int{SuitSpade, SuitHeart, SuitClub, SuitDiamond}
	out := make([]Card, 0, len(ranks))
	for _, r := range ranks {
		if r == RankSmall || r == RankBig {
			out = append(out, Card{Rank: r, Suit: SuitNone})
			continue
		}
		out = append(out, Card{Rank: r, Suit: suits[seen[r]%4]})
		seen[r]++
	}
	return out
}

func TestNewDeck(t *testing.T) {
	d := NewDeck()
	if len(d) != 54 {
		t.Fatalf("deck size = %d, want 54", len(d))
	}
	jokers := 0
	for _, card := range d {
		if card.IsJoker() {
			jokers++
		}
	}
	if jokers != 2 {
		t.Fatalf("jokers = %d, want 2", jokers)
	}
}

func TestParseCombo_Basic(t *testing.T) {
	cases := []struct {
		name string
		hand []Card
		want ComboType
		ok   bool
	}{
		{"single", cs(Rank5), ComboSingle, true},
		{"pair", cs(Rank8, Rank8), ComboPair, true},
		{"pair-mismatch", cs(Rank8, Rank9), ComboInvalid, false},
		{"triple", cs(RankJ, RankJ, RankJ), ComboTriple, true},
		{"triple-single", cs(Rank7, Rank7, Rank7, Rank4), ComboTripleSingle, true},
		{"triple-pair", cs(RankJ, RankJ, RankJ, Rank5, Rank5), ComboTriplePair, true},
		{"triple-pair-bad", cs(RankJ, RankJ, RankJ, Rank5, Rank6), ComboInvalid, false},
		{"bomb", cs(Rank6, Rank6, Rank6, Rank6), ComboBomb, true},
		{"rocket", cs(RankSmall, RankBig), ComboRocket, true},
		{"straight5", cs(Rank3, Rank4, Rank5, Rank6, Rank7), ComboStraight, true},
		{"straight-with-2", cs(RankJ, RankQ, RankK, RankA, Rank2), ComboInvalid, false},
		{"straight-too-short", cs(Rank3, Rank4, Rank5, Rank6), ComboInvalid, false},
		{"straight-AtoK", cs(Rank10, RankJ, RankQ, RankK, RankA), ComboStraight, true},
		{"pair-straight", cs(Rank3, Rank3, Rank4, Rank4, Rank5, Rank5), ComboPairStraight, true},
		{"pair-straight-short", cs(Rank3, Rank3, Rank4, Rank4), ComboInvalid, false},
		{"plane", cs(Rank7, Rank7, Rank7, Rank8, Rank8, Rank8), ComboPlane, true},
		{"plane-single", cs(Rank7, Rank7, Rank7, Rank8, Rank8, Rank8, Rank3, Rank4), ComboPlaneSingle, true},
		{"plane-pair", cs(Rank7, Rank7, Rank7, Rank8, Rank8, Rank8, Rank3, Rank3, Rank4, Rank4), ComboPlanePair, true},
		{"quad-two-single", cs(Rank9, Rank9, Rank9, Rank9, Rank3, Rank5), ComboQuadTwoSingle, true},
		{"quad-two-pair", cs(Rank9, Rank9, Rank9, Rank9, Rank3, Rank3, Rank5, Rank5), ComboQuadTwoPair, true},
		{"garbage", cs(Rank3, Rank5, Rank9), ComboInvalid, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			combo, ok := ParseCombo(tc.hand)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && combo.Type != tc.want {
				t.Fatalf("type = %v, want %v", combo.Type, tc.want)
			}
		})
	}
}

func TestCanBeat(t *testing.T) {
	parse := func(ranks ...int) *Combo {
		combo, ok := ParseCombo(cs(ranks...))
		if !ok {
			t.Fatalf("parse failed for %v", ranks)
		}
		return combo
	}

	// 自由出牌：任何合法都可。
	if !CanBeat(nil, parse(Rank3)) {
		t.Fatal("free lead should allow any combo")
	}
	// 同型比点数。
	if !CanBeat(parse(Rank5), parse(Rank8)) {
		t.Fatal("8 should beat 5")
	}
	if CanBeat(parse(Rank8), parse(Rank5)) {
		t.Fatal("5 should not beat 8")
	}
	// 不同型不可比（非炸弹）。
	if CanBeat(parse(Rank5, Rank5), parse(Rank9)) {
		t.Fatal("single cannot beat pair")
	}
	// 顺子等长才能比。
	if CanBeat(parse(Rank3, Rank4, Rank5, Rank6, Rank7), parse(Rank4, Rank5, Rank6, Rank7, Rank8, Rank9)) {
		t.Fatal("straights of different length cannot compare")
	}
	if !CanBeat(parse(Rank3, Rank4, Rank5, Rank6, Rank7), parse(Rank4, Rank5, Rank6, Rank7, Rank8)) {
		t.Fatal("higher straight of same length should beat")
	}
	// 炸弹压非炸弹。
	if !CanBeat(parse(Rank3, Rank3), parse(Rank4, Rank4, Rank4, Rank4)) {
		t.Fatal("bomb should beat pair")
	}
	// 炸弹间比点数。
	if !CanBeat(parse(Rank4, Rank4, Rank4, Rank4), parse(Rank5, Rank5, Rank5, Rank5)) {
		t.Fatal("higher bomb should beat lower bomb")
	}
	// 火箭压一切。
	rocket := parse(RankSmall, RankBig)
	if !CanBeat(parse(RankA, RankA, RankA, RankA), rocket) {
		t.Fatal("rocket should beat bomb")
	}
	if CanBeat(rocket, parse(Rank3, Rank3, Rank3, Rank3)) {
		t.Fatal("nothing should beat rocket")
	}
	// 三带一只比三张主体。
	if !CanBeat(parse(Rank5, Rank5, Rank5, Rank3), parse(Rank8, Rank8, Rank8, RankA)) {
		t.Fatal("triple+kicker compares triple body")
	}
}

func TestDeal(t *testing.T) {
	gs := NewGame(42, 0)
	for s := 0; s < 3; s++ {
		if len(gs.Hands[s]) != 17 {
			t.Fatalf("seat %d hand = %d, want 17", s, len(gs.Hands[s]))
		}
	}
	if len(gs.Bottom) != 3 {
		t.Fatalf("bottom = %d, want 3", len(gs.Bottom))
	}
	if gs.Phase != PhaseBidding {
		t.Fatalf("phase = %v, want bidding", gs.Phase)
	}
}

func TestBidding_BidThreeWins(t *testing.T) {
	gs := NewGame(1, 0)
	// 座位 0 叫 3 分，直接成地主。
	redeal, e := gs.Bid(0, 3)
	if e != nil || redeal {
		t.Fatalf("bid 3 failed: redeal=%v err=%v", redeal, e)
	}
	if gs.Phase != PhasePlaying {
		t.Fatalf("phase = %v, want playing", gs.Phase)
	}
	if gs.LandlordSeat != 0 {
		t.Fatalf("landlord = %d, want 0", gs.LandlordSeat)
	}
	// 地主拿到底牌：17 + 3 = 20 张。
	if len(gs.Hands[0]) != 20 {
		t.Fatalf("landlord hand = %d, want 20", len(gs.Hands[0]))
	}
	if gs.Turn != 0 {
		t.Fatalf("turn = %d, want landlord 0", gs.Turn)
	}
}

func TestBidding_AllPassRedeal(t *testing.T) {
	gs := NewGame(1, 0)
	gs.Bid(0, 0)
	gs.Bid(1, 0)
	redeal, e := gs.Bid(2, 0)
	if e != nil {
		t.Fatalf("bid err: %v", e)
	}
	if !redeal {
		t.Fatal("all-pass should request redeal")
	}
}

func TestBidding_NotYourTurn(t *testing.T) {
	gs := NewGame(1, 0)
	if _, e := gs.Bid(1, 2); e == nil {
		t.Fatal("bidding out of turn should error")
	}
}

func TestPlayAndWin(t *testing.T) {
	gs := NewGame(7, 0)
	// 直接进入出牌阶段：座位 1 叫 3 分当地主，便于构造。
	if _, e := gs.Bid(0, 0); e != nil {
		t.Fatal(e)
	}
	if _, e := gs.Bid(1, 3); e != nil {
		t.Fatal(e)
	}
	if gs.Phase != PhasePlaying || gs.LandlordSeat != 1 {
		t.Fatalf("setup failed: phase=%v landlord=%d", gs.Phase, gs.LandlordSeat)
	}

	// 强制设定地主手牌为单张 3，其余两家各一张较小牌，验证出完即胜。
	gs.Hands[1] = cs(Rank3)
	gs.Hands[0] = cs(Rank4)
	gs.Hands[2] = cs(Rank5)
	gs.Last = nil
	gs.Turn = 1

	over, e := gs.Play(1, cs(Rank3))
	if e != nil {
		t.Fatalf("play err: %v", e)
	}
	if !over {
		t.Fatal("landlord emptied hand, game should be over")
	}
	if gs.Status != StatusLandlordWin {
		t.Fatalf("status = %v, want landlord_win", gs.Status)
	}
}

func TestPlay_PassRules(t *testing.T) {
	gs := NewGame(11, 0)
	gs.Bid(0, 3) // 座位 0 地主
	gs.Phase = PhasePlaying
	gs.Turn = 0
	gs.Last = nil
	gs.Hands[0] = cs(Rank5, Rank6)
	gs.Hands[1] = cs(Rank8, Rank9)
	gs.Hands[2] = cs(RankK, RankA)

	// 首出不能过。
	if e := gs.Pass(0); e == nil {
		t.Fatal("leading player cannot pass")
	}
	// 座位 0 出 5。
	if _, e := gs.Play(0, cs(Rank5)); e != nil {
		t.Fatalf("play err: %v", e)
	}
	if gs.Turn != 1 {
		t.Fatalf("turn = %d, want 1", gs.Turn)
	}
	// 座位 1、2 连续过 → 回到座位 0 自由出牌。
	if e := gs.Pass(1); e != nil {
		t.Fatal(e)
	}
	if e := gs.Pass(2); e != nil {
		t.Fatal(e)
	}
	if gs.Turn != 0 {
		t.Fatalf("turn = %d, want 0 (regain lead)", gs.Turn)
	}
	if gs.Last != nil {
		t.Fatal("last play should reset after two passes")
	}
}

func TestPlay_BombMultiplier(t *testing.T) {
	gs := NewGame(3, 0)
	// 座位 0 叫 2 分，其余不叫 → 座位 0 当地主，基础分 2。
	gs.Bid(0, 2)
	gs.Bid(1, 0)
	if _, e := gs.Bid(2, 0); e != nil {
		t.Fatal(e)
	}
	if gs.LandlordSeat != 0 || gs.Phase != PhasePlaying {
		t.Fatalf("setup: landlord=%d phase=%v", gs.LandlordSeat, gs.Phase)
	}
	gs.Turn = 0
	gs.Last = nil
	gs.Hands[0] = cs(Rank6, Rank6, Rank6, Rank6, Rank3)
	gs.Hands[1] = cs(Rank9)
	gs.Hands[2] = cs(RankK)

	if gs.Multiplier != 2 {
		t.Fatalf("base multiplier = %d, want 2", gs.Multiplier)
	}
	if _, e := gs.Play(0, cs(Rank6, Rank6, Rank6, Rank6)); e != nil {
		t.Fatalf("bomb play err: %v", e)
	}
	if gs.Multiplier != 4 {
		t.Fatalf("multiplier after bomb = %d, want 4", gs.Multiplier)
	}
	if gs.BombCount != 1 {
		t.Fatalf("bomb count = %d, want 1", gs.BombCount)
	}
}

func TestPlay_CannotPlayUnownedCards(t *testing.T) {
	gs := NewGame(5, 0)
	gs.Bid(0, 1)
	gs.Phase = PhasePlaying
	gs.Turn = 0
	gs.Last = nil
	gs.Hands[0] = cs(Rank3, Rank4)

	if _, e := gs.Play(0, cs(RankA)); e == nil {
		t.Fatal("playing cards not in hand should error")
	}
}

func TestManager_JoinAndStart(t *testing.T) {
	m := NewDoudizhuManager()
	m.seedFn = func() int64 { return 99 } // 固定种子

	if _, started, e := m.JoinGame("room1", "u0"); e != nil || started {
		t.Fatalf("first join: started=%v err=%v", started, e)
	}
	if _, started, e := m.JoinGame("room1", "u1"); e != nil || started {
		t.Fatalf("second join: started=%v err=%v", started, e)
	}
	_, started, e := m.JoinGame("room1", "u2")
	if e != nil {
		t.Fatalf("third join err: %v", e)
	}
	if !started {
		t.Fatal("game should start when 3rd player joins")
	}
	// 第四人加入 → 房满。
	if _, _, e := m.JoinGame("room1", "u3"); e == nil {
		t.Fatal("4th join should fail (room full)")
	}
	// 已入座者再 join 幂等。
	if _, _, e := m.JoinGame("room1", "u0"); e != nil {
		t.Fatalf("idempotent rejoin failed: %v", e)
	}

	st, e := m.GetState("room1", "u0")
	if e != nil {
		t.Fatalf("getstate err: %v", e)
	}
	if st.MySeat != 0 || len(st.MyHand) != 17 {
		t.Fatalf("seat0 state wrong: seat=%d hand=%d", st.MySeat, len(st.MyHand))
	}
	// 其他座位手牌对 u0 不可见（只见张数）。
	if st.HandCounts[1] != 17 || st.HandCounts[2] != 17 {
		t.Fatalf("hand counts wrong: %v", st.HandCounts)
	}
}
