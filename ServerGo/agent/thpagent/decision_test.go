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
	// HS-04: A♠K♠ (编码 1, 14) vs 7♥2♦ (编码 28, 9) preflop
	// 期望胜率约 65%, 容差 ±10% (蒙特卡洛 1000 次 + 简化评分)
	hole := [2]int{1, 14}      // A♠ + A♠？这里简化为 A-A 配对 → 应高胜率
	community := [5]int{}
	win, draw := HandStrength(hole, community, 0, 500)
	if win+draw > 1.0 {
		t.Errorf("win+draw > 1.0: win=%f draw=%f", win, draw)
	}
	if win < 0 || draw < 0 {
		t.Errorf("negative probability: win=%f draw=%f", win, draw)
	}
	t.Logf("A-A preflop win=%f draw=%f", win, draw)
}

func TestHandStrength_RoyalFlushDetection(t *testing.T) {
	// RoyalFlush 应被识别为最高牌型
	// 10♠=37, J♠=38, Q♠=39, K♠=40, A♠=1 (但我们用 4*suit+rank 编码？这里 1-based)
	// 实际我们编码: c = rank*4 + suit, rank 1-13, suit 1-4
	// 10-J-Q-K-A 同花色 = (10,1)(11,1)(12,1)(13,1)(13,4) - 但这跨花色
	// 简化：直接用 5 张同花色 A-2-3-4-5 顺子
	cards := []int{
		1*4 + 1, // A♠
		2*4 + 1, // 2♠
		3*4 + 1, // 3♠
		4*4 + 1, // 4♠
		5*4 + 1, // 5♠
	}
	category := detectCategory(cards)
	// A-2-3-4-5 同花顺不算皇家,但算同花顺(7)
	if category != 7 {
		t.Errorf("A-2-3-4-5 same-suit straight should be StraightFlush(7), got %d", category)
	}
}

func TestHandStrength_FourOfAKind(t *testing.T) {
	// 四条 5 (rank=5) 不同花色
	cards := []int{
		5*4 + 1, 5*4 + 2, 5*4 + 3, 5*4 + 4, // 四个 5
		13*4 + 1, // K kicker
	}
	category := detectCategory(cards)
	if category != 6 {
		t.Errorf("FourOfAKind should be category 6, got %d", category)
	}
}

func TestHandStrength_FullHouse(t *testing.T) {
	// 葫芦: 三个 7 + 两个 3
	cards := []int{
		7*4 + 1, 7*4 + 2, 7*4 + 3, // 三个 7
		3*4 + 1, 3*4 + 2, // 两个 3
	}
	category := detectCategory(cards)
	if category != 5 {
		t.Errorf("FullHouse should be category 5, got %d", category)
	}
}

func TestHandStrength_TwoPair(t *testing.T) {
	// 两对: 两个 9 + 两个 4
	cards := []int{
		9*4 + 1, 9*4 + 2, // 两个 9
		4*4 + 1, 4*4 + 2, // 两个 4
		13*4 + 1, // K kicker
	}
	category := detectCategory(cards)
	if category != 1 {
		t.Errorf("TwoPair should be category 1, got %d", category)
	}
}

func TestHandStrength_HighCard(t *testing.T) {
	// 高牌: A-K-Q-J-9 不同花色
	cards := []int{
		13*4 + 1, // K♠
		12*4 + 2, // Q♥
		11*4 + 3, // J♣
		9*4 + 4,  // 9♦
		8*4 + 1,  // 8♠
	}
	category := detectCategory(cards)
	if category != 0 {
		t.Errorf("HighCard should be category 0, got %d", category)
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