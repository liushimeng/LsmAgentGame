package thpagent

import (
	"math"
	"math/rand"
	"testing"
)

func TestPotOdds(t *testing.T) {
	tests := []struct {
		name        string
		callAmount  int
		pot         int
		wantOdds    float64
		wantEquity  float64
	}{
		{"pot 100, bet 50", 50, 100, 0.333, 0.333},
		{"pot 50, bet 50", 50, 50, 0.5, 0.5},
		{"pot 200, bet 100", 100, 200, 0.333, 0.333},
		{"callAmount 0", 0, 100, 0, 0},
		{"negative call", -10, 100, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			odds, equity := PotOdds(tt.callAmount, tt.pot)
			if math.Abs(odds-tt.wantOdds) > 0.01 {
				t.Errorf("odds = %f, want %f", odds, tt.wantOdds)
			}
			if math.Abs(equity-tt.wantEquity) > 0.01 {
				t.Errorf("equity = %f, want %f", equity, tt.wantEquity)
			}
		})
	}
}

func TestPosition(t *testing.T) {
	tests := []struct {
		name       string
		seat       int
		button     int
		wantLabel  string
		wantLabelZh string
	}{
		{"BTN when button=0", 0, 0, "BTN", "庄家位"},
		{"SB when button=0", 1, 0, "SB", "小盲位"},
		{"BB when button=0", 2, 0, "BB", "大盲位"},
		{"UTG when button=0", 3, 0, "UTG", "枪口位"},
		{"MP when button=0", 4, 0, "MP", "中位"},
		{"CO when button=0", 5, 0, "CO", "关煞位"},
		{"BTN when button=3", 3, 3, "BTN", "庄家位"},
		{"UTG when button=3", 0, 3, "UTG", "枪口位"},
		{"BTN when button=5", 5, 5, "BTN", "庄家位"},
		{"CO when button=5", 4, 5, "CO", "关煞位"},
		{"invalid button=-1", 0, -1, "BTN", "庄家位"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, labelZh := Position(tt.seat, tt.button)
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
			if labelZh != tt.wantLabelZh {
				t.Errorf("labelZh = %q, want %q", labelZh, tt.wantLabelZh)
			}
		})
	}
}

func TestBluffFrequency(t *testing.T) {
	tests := []struct {
		name  string
		rate  float64
		want  float64
	}{
		{"5% fold rate (very sticky)", 0.05, 0.03},
		{"20% fold rate (sticky)", 0.20, 0.08},
		{"40% fold rate (neutral)", 0.40, 0.15},
		{"60% fold rate (high)", 0.60, 0.25},
		{"75% fold rate (very high)", 0.75, 0.35},
		{"100% fold rate", 1.0, 0.35},
		{"0% fold rate", 0.0, 0.03},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BluffFrequency(tt.rate)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("BluffFrequency(%f) = %f, want %f", tt.rate, got, tt.want)
			}
		})
	}
}

func TestHandStrength_BasicInvariants(t *testing.T) {
	// 规范编码(cards.go): encode = (rank-2)*4 + (suit-1) + 1
	// A♠ = (14-2)*4+0+1 = 49, A♥ = 50 → 口袋对 A，preflop 应高胜率
	hole := [2]int{49, 50}
	community := [5]int{}
	win, draw := HandStrength(hole, community, 0, 500)
	if win+draw > 1.0 {
		t.Errorf("win+draw > 1.0: win=%f draw=%f", win, draw)
	}
	if win < 0 || draw < 0 {
		t.Errorf("negative probability: win=%f draw=%f", win, draw)
	}
	if win < 0.6 {
		t.Errorf("AA preflop should be a strong favorite, win=%f", win)
	}
	t.Logf("A-A preflop win=%f draw=%f", win, draw)
}

// eval5 编码辅助: c(rank, suit)。
func ec(rank, suit int) int { return EncodeCard(rank, suit) }

func TestEval5_StraightFlush(t *testing.T) {
	// A-2-3-4-5 同花顺（轮子）
	s := eval5([5]int{ec(14, 1), ec(2, 1), ec(3, 1), ec(4, 1), ec(5, 1)})
	if s.category != catStraightFlush {
		t.Errorf("A-2-3-4-5 same suit should be straight flush, got %d", s.category)
	}
	if s.tiebreak[0] != 5 {
		t.Errorf("wheel straight flush high should be 5, got %d", s.tiebreak[0])
	}
	// 皇家同花顺 = 同花顺 + A 高
	royal := eval5([5]int{ec(10, 2), ec(11, 2), ec(12, 2), ec(13, 2), ec(14, 2)})
	if royal.category != catStraightFlush || royal.tiebreak[0] != 14 {
		t.Errorf("royal flush should be straight flush with A high, got %+v", royal)
	}
	if royal.compare(s) <= 0 {
		t.Error("royal flush should beat wheel straight flush")
	}
}

func TestEval5_FourOfAKind(t *testing.T) {
	s := eval5([5]int{ec(5, 1), ec(5, 2), ec(5, 3), ec(5, 4), ec(13, 1)})
	if s.category != catQuads {
		t.Errorf("four 5s should be quads, got %d", s.category)
	}
	if s.tiebreak[0] != 5 || s.tiebreak[1] != 13 {
		t.Errorf("quads tiebreak should be [5, K], got %v", s.tiebreak)
	}
}

func TestEval5_FullHouse(t *testing.T) {
	s := eval5([5]int{ec(7, 1), ec(7, 2), ec(7, 3), ec(3, 1), ec(3, 2)})
	if s.category != catFullHouse {
		t.Errorf("77733 should be full house, got %d", s.category)
	}
	if s.tiebreak[0] != 7 || s.tiebreak[1] != 3 {
		t.Errorf("full house tiebreak should be [7, 3], got %v", s.tiebreak)
	}
}

func TestEval5_TwoPair(t *testing.T) {
	s := eval5([5]int{ec(9, 1), ec(9, 2), ec(4, 1), ec(4, 2), ec(13, 1)})
	if s.category != catTwoPair {
		t.Errorf("99 44 K should be two pair, got %d", s.category)
	}
	if s.tiebreak[0] != 9 || s.tiebreak[1] != 4 || s.tiebreak[2] != 13 {
		t.Errorf("two pair tiebreak should be [9, 4, K], got %v", s.tiebreak)
	}
}

// TestEval5_OnePairBeatsHighCard 是 §B1 的回归锚点：旧 detectCategory 把
// OnePair 与 HighCard 同档（都 return 0），平局判定系统性错误。
func TestEval5_OnePairBeatsHighCard(t *testing.T) {
	pair := eval5([5]int{ec(9, 1), ec(9, 2), ec(4, 1), ec(6, 2), ec(13, 1)})
	high := eval5([5]int{ec(13, 1), ec(12, 2), ec(11, 3), ec(9, 4), ec(8, 1)})
	if pair.category != catOnePair {
		t.Errorf("pair should be category %d, got %d", catOnePair, pair.category)
	}
	if high.category != catHighCard {
		t.Errorf("high card should be category %d, got %d", catHighCard, high.category)
	}
	if pair.compare(high) <= 0 {
		t.Error("one pair must beat high card")
	}
}

// TestEvalBest_SevenCards 验证 7 选 5 最优组合 —— 旧 scoreHand 只取前 5 张
// （all[:5]），turn/river 的好牌被完全忽略。本用例中第 7 张牌（river）才
// 凑成同花，evalBest 必须发现它。
func TestEvalBest_SevenCards(t *testing.T) {
	// 底牌 2♠ 3♠ + 公共牌 K♠ Q♦ 9♣ 8♠ 4♠ → 5 张黑桃同花（含第 7 张 4♠）
	cards := []int{ec(2, 1), ec(3, 1), ec(13, 1), ec(12, 4), ec(9, 3), ec(8, 1), ec(4, 1)}
	s := evalBest(cards)
	if s.category != catFlush {
		t.Errorf("7-card best should find the spade flush (uses river card), got category %d", s.category)
	}
}

// TestEvalBest_KickerDecides 验证 kicker 字典序：同样一对 A，K kicker > Q kicker。
func TestEvalBest_KickerDecides(t *testing.T) {
	a := evalBest([]int{ec(14, 1), ec(13, 1), ec(14, 2), ec(9, 3), ec(7, 4), ec(5, 2), ec(2, 3)})
	b := evalBest([]int{ec(14, 3), ec(12, 1), ec(14, 4), ec(9, 3), ec(7, 4), ec(5, 2), ec(2, 3)})
	if a.category != catOnePair || b.category != catOnePair {
		t.Fatalf("both should be one pair of aces, got %d / %d", a.category, b.category)
	}
	if a.compare(b) <= 0 {
		t.Error("pair of aces with K kicker must beat pair of aces with Q kicker")
	}
}

func TestMakeDeckExcluding(t *testing.T) {
	deck := makeDeckExcluding([]int{1, 2, 3})
	if len(deck) != 49 {
		t.Errorf("expected 49 cards, got %d", len(deck))
	}
	for _, c := range deck {
		if c == 1 || c == 2 || c == 3 {
			t.Errorf("deck contains excluded card %d", c)
		}
	}
}

func TestSampleN(t *testing.T) {
	deck := make([]int, 52)
	for i := range deck {
		deck[i] = i + 1
	}
	rng := newRand()
	sample := sampleN(deck, 5, rng)
	if len(sample) != 5 {
		t.Errorf("sample length = %d, want 5", len(sample))
	}
	seen := make(map[int]bool)
	for _, c := range sample {
		if seen[c] {
			t.Errorf("duplicate card %d in sample", c)
		}
		seen[c] = true
	}
}

func newRand() *rand.Rand {
	return rand.New(rand.NewSource(42))
}

// TestHandStrengthCache 测试缓存命中
func TestHandStrengthCache(t *testing.T) {
	hole := [2]int{1, 14}
	community := [5]int{}
	// 第一次计算
	win1, draw1 := HandStrength(hole, community, 0, 100)
	// 第二次应该命中缓存,结果相同
	win2, draw2 := HandStrength(hole, community, 0, 100)
	if win1 != win2 || draw1 != draw2 {
		t.Errorf("cache hit returned different result: %f/%f vs %f/%f", win1, draw1, win2, draw2)
	}
}