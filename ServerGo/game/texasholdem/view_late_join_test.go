// R7 P0-1 回归测试: 迟到加入玩家(未参与本手)底牌为零值,
// 视图层不得渲染为 2 张牌背 / 填充 MyHole / HoleCount=2。
package texasholdem

import (
	"testing"
)

// TestLateJoinerEmptyHole 验证手牌进行中 AddPlayer(迟到加入) 的玩家
// 在视图中 MyHole 为空 / HoleCount=0 / 其他玩家也不可见其底牌。
func TestLateJoinerEmptyHole(t *testing.T) {
	// 开局:alice + bob 入座 → StartHand → alice/bob 各有 2 张有效底牌
	mgr := NewTexasHoldemManager()
	mgr.BigBlind = 100
	mgr.StartStack = 5000
	if _, _, e := mgr.JoinGame("r1", "alice"); e != nil {
		t.Fatalf("join alice: %v", e)
	}
	_, started, e := mgr.JoinGame("r1", "bob")
	if e != nil {
		t.Fatalf("join bob: %v", e)
	}
	if !started {
		t.Fatalf("expected game to start with 2 players")
	}

	// 迟到加入:carol 在手牌进行中入座 → AddPlayer 写 UserID/Stack 但 Hole 保持零值
	if _, _, e := mgr.JoinGame("r1", "carol"); e != nil {
		t.Fatalf("late join carol: %v", e)
	}

	// Carol 视角
	cs := mgr.StateForSeat("r1", 2) // carol 在座位 2
	if cs == nil {
		t.Fatal("nil state for carol")
	}
	if len(cs.MyHole) != 0 {
		t.Fatalf("late joiner MyHole should be empty, got %+v", cs.MyHole)
	}
	if cs.Players[2].HoleCount != 0 {
		t.Fatalf("late joiner HoleCount should be 0, got %d", cs.Players[2].HoleCount)
	}
	if len(cs.Players[2].Hole) != 0 {
		t.Fatalf("late joiner Hole should be hidden, got %+v", cs.Players[2].Hole)
	}

	// Alice 视角:carol 的底牌也不可见
	csAlice := mgr.StateForSeat("r1", 0)
	if csAlice == nil {
		t.Fatal("nil state for alice")
	}
	if len(csAlice.Players[2].Hole) != 0 {
		t.Fatalf("alice should not see carol's hole, got %+v", csAlice.Players[2].Hole)
	}
	if csAlice.Players[2].HoleCount != 0 {
		t.Fatalf("alice should see carol's HoleCount=0, got %d", csAlice.Players[2].HoleCount)
	}
	// alice 自己的底牌仍完整
	if len(csAlice.MyHole) != 2 {
		t.Fatalf("alice MyHole should be 2 cards, got %d", len(csAlice.MyHole))
	}

	// 下一手 StartHand 后 carol 才被发有效底牌
	r := mgr.getRoom("r1")
	if r == nil {
		t.Fatal("room not found")
	}
	r.mu.Lock()
	_ = r.State.StartHand()
	r.mu.Unlock()
	csCarol2 := mgr.StateForSeat("r1", 2)
	if csCarol2 == nil {
		t.Fatal("nil state for carol after new hand")
	}
	if len(csCarol2.MyHole) != 2 {
		t.Fatalf("carol MyHole should be 2 cards after new hand, got %d", len(csCarol2.MyHole))
	}
	if csCarol2.Players[2].HoleCount != 2 {
		t.Fatalf("carol HoleCount should be 2 after new hand, got %d", csCarol2.Players[2].HoleCount)
	}
}

// TestIsValidCard 验证辅助函数的边界。
func TestIsValidCard(t *testing.T) {
	cases := []struct {
		c    Card
		want bool
	}{
		{Card{Rank: 0, Suit: 0}, false},        // 零值(未发牌)
		{Card{Rank: 1, Suit: SuitSpade}, false},  // Rank 低于 Rank2
		{Card{Rank: 15, Suit: SuitSpade}, false}, // Rank 高于 RankA
		{Card{Rank: 2, Suit: 0}, false},          // Suit 低于 SuitSpade
		{Card{Rank: 2, Suit: 5}, false},          // Suit 高于 SuitDiamond
		{Card{Rank: 2, Suit: SuitSpade}, true},   // 最小合法牌
		{Card{Rank: 14, Suit: SuitDiamond}, true},// 最大合法牌
		{Card{Rank: 10, Suit: SuitHeart}, true},  // 中间合法牌
	}
	for i, tc := range cases {
		if got := isValidCard(tc.c); got != tc.want {
			t.Fatalf("case %d: isValidCard(%+v) = %v, want %v", i, tc.c, got, tc.want)
		}
	}
}
