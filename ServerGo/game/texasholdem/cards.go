// Package texasholdem implements the Texas Hold'em poker engine.
//
// 德州扑克是平台第五款游戏，2-6 人对局，与棋类和斗地主的核心差异：
//   - 多轮下注（preflop → flop → turn → river → showdown）
//   - 共享公共牌（5 张）+ 底牌（每人 2 张）
//   - 牌型评估（5 of 7 取最优 5 张）
//   - 盲注 + 庄家按钮顺时针旋转
//
// 本文件 cards.go 定义牌面、点数/花色、牌组构造与洗牌。
// 牌型评估见 hand.go，状态机见 engine.go，视图过滤见 view.go，房间管理见 room.go。
package texasholdem

import (
	"math/rand"
	"sort"
)

// 点数常量。2 最小，A 最大；A 可作为 1 参与 A-2-3-4-5 顺子。
const (
	Rank2  = 2
	Rank3  = 3
	Rank4  = 4
	Rank5  = 5
	Rank6  = 6
	Rank7  = 7
	Rank8  = 8
	Rank9  = 9
	Rank10 = 10
	RankJ  = 11
	RankQ  = 12
	RankK  = 13
	RankA  = 14 // 也代表 1（用于轮子顺子）
)

// 花色常量。德州扑克无大小王。
const (
	SuitSpade   = 1 // ♠
	SuitHeart   = 2 // ♥
	SuitClub    = 3 // ♣
	SuitDiamond = 4 // ♦
)

// Card 表示一张牌。Rank 为点数，Suit 为花色。
type Card struct {
	Rank int `json:"rank"`
	Suit int `json:"suit"`
}

// NewDeck 构造一副标准 52 张牌（13 点数 × 4 花色，无大小王）。
func NewDeck() []Card {
	deck := make([]Card, 0, 52)
	for rank := Rank2; rank <= RankA; rank++ {
		for _, suit := range []int{SuitSpade, SuitHeart, SuitClub, SuitDiamond} {
			deck = append(deck, Card{Rank: rank, Suit: suit})
		}
	}
	return deck
}

// Shuffle 使用注入的 *rand.Rand 原地洗牌。
func Shuffle(deck []Card, rng *rand.Rand) {
	rng.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
}

// SortCards 按点数从大到小排序（同点数按花色稳定排序）。
func SortCards(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Rank != cards[j].Rank {
			return cards[i].Rank > cards[j].Rank
		}
		return cards[i].Suit > cards[j].Suit
	})
}

// SortCardsAsc 按点数从小到大排序（用于顺子判断）。
func SortCardsAsc(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Rank != cards[j].Rank {
			return cards[i].Rank < cards[j].Rank
		}
		return cards[i].Suit < cards[j].Suit
	})
}

// rankCounts 统计每个点数出现的次数。
func rankCounts(cards []Card) map[int]int {
	m := make(map[int]int)
	for _, c := range cards {
		m[c.Rank]++
	}
	return m
}

// removeCards 从 src 中移除 toRemove 中的牌（按 rank+suit 精确匹配，各一张）。
func removeCards(src, toRemove []Card) ([]Card, bool) {
	remaining := make([]Card, len(src))
	copy(remaining, src)
	for _, r := range toRemove {
		idx := -1
		for i, c := range remaining {
			if c.Rank == r.Rank && c.Suit == r.Suit {
				idx = i
				break
			}
		}
		if idx < 0 {
			return src, false
		}
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
	return remaining, true
}

// containsAll 报告 hand 是否包含 want 中的每一张牌。
func containsAll(hand, want []Card) bool {
	_, ok := removeCards(hand, want)
	return ok
}
