// Package doudizhu implements the 斗地主 (Landlord) card game engine.
//
// 斗地主是平台首个卡牌类 / 3 人游戏（1 地主 + 2 农民），与现有 1v1 棋类差异最大：
//   - 三个座位 seat 0/1/2，逆时针轮转。
//   - 两个阶段：bidding（叫地主）→ playing（出牌）。
//   - 隐藏手牌：每个玩家只见自己完整手牌，其余仅见张数。
//   - 全部规则判断在服务端完成（防作弊）。
//
// 本文件 cards.go 定义牌、点数/花色、牌组构造、洗牌与计数等基础工具。
// 牌型识别与比较见 combo.go，状态机见 engine.go，视图过滤见 view.go，
// 房间与管理器见 room.go。
package doudizhu

import (
	"math/rand"
	"sort"
)

// 点数常量。3 最小，2 较大，小王/大王最大。
// 采用 3..17 连续整数，便于顺子/连对/飞机的连续性判断（王不参与连续）。
const (
	Rank3     = 3
	Rank4     = 4
	Rank5     = 5
	Rank6     = 6
	Rank7     = 7
	Rank8     = 8
	Rank9     = 9
	Rank10    = 10
	RankJ     = 11
	RankQ     = 12
	RankK     = 13
	RankA     = 14
	Rank2     = 15 // 2 比 A 大，但不参与顺子/连对/飞机
	RankSmall = 16 // 小王
	RankBig   = 17 // 大王
)

// 花色常量。王无花色（用 SuitNone）。
const (
	SuitNone    = 0
	SuitSpade   = 1 // 黑桃
	SuitHeart   = 2 // 红桃
	SuitClub    = 3 // 梅花
	SuitDiamond = 4 // 方块
)

// Card 表示一张牌。Rank 为点数，Suit 为花色。
// 斗地主中花色不影响大小，仅用于美术展示与去重。
type Card struct {
	Rank int `json:"rank"`
	Suit int `json:"suit"`
}

// IsJoker 报告该牌是否为大小王。
func (c Card) IsJoker() bool {
	return c.Rank == RankSmall || c.Rank == RankBig
}

// NewDeck 构造一副标准 54 张牌（13 点数 × 4 花色 + 大小王）。
func NewDeck() []Card {
	deck := make([]Card, 0, 54)
	for rank := Rank3; rank <= Rank2; rank++ {
		for _, suit := range []int{SuitSpade, SuitHeart, SuitClub, SuitDiamond} {
			deck = append(deck, Card{Rank: rank, Suit: suit})
		}
	}
	deck = append(deck, Card{Rank: RankSmall, Suit: SuitNone})
	deck = append(deck, Card{Rank: RankBig, Suit: SuitNone})
	return deck
}

// Shuffle 使用注入的 *rand.Rand 原地洗牌（不依赖全局 rand，便于测试可复现）。
func Shuffle(deck []Card, rng *rand.Rand) {
	rng.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
}

// SortCards 按点数从大到小排序（同点数按花色稳定排序），用于手牌展示。
func SortCards(cards []Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Rank != cards[j].Rank {
			return cards[i].Rank > cards[j].Rank
		}
		return cards[i].Suit > cards[j].Suit
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
// 返回剩余牌与是否全部命中。用于出牌后从手牌扣除。
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

// containsAll 报告 hand 是否包含 want 中的每一张牌（按 rank+suit 精确匹配）。
func containsAll(hand, want []Card) bool {
	_, ok := removeCards(hand, want)
	return ok
}
