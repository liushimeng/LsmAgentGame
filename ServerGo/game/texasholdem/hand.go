package texasholdem

// hand.go — 五张最优牌型评估。
//
// 从 7 张牌（2 张底牌 + 5 张公共牌）中选出最优 5 张组合。
// 枚举 C(7,5)=21 个 5 张子集，对每个打分取最大。
//
// 牌型分类（从大到小）：
//   9 = 皇家同花顺   Straight Flush with top=14 (10-J-Q-K-A same suit)
//   8 = 同花顺       5 同花色连续牌
//   7 = 四条         4 张同点数
//   6 = 葫芦         3 张同点数 + 1 对
//   5 = 同花         5 张同花色
//   4 = 顺子         5 张点数连续
//   3 = 三条         3 张同点数
//   2 = 两对         2 组对子
//   1 = 一对         2 张同点数
//   0 = 高牌         以上都不满足

// HandRank 是一手牌的可比较评分。
type HandRank struct {
	Category int    // 0..9
	Tiebreak [5]int // 5 个降序比较键（不同牌型使用方式不同）
}

// Compare 比较两手牌大小。返回 >0 代表 a 优于 b，<0 则反之，0 为完全平局。
func (a HandRank) Compare(b HandRank) int {
	if a.Category != b.Category {
		return a.Category - b.Category
	}
	for i := 0; i < 5; i++ {
		if a.Tiebreak[i] != b.Tiebreak[i] {
			return a.Tiebreak[i] - b.Tiebreak[i]
		}
	}
	return 0
}

// EvaluateBest5 从 7 张牌（2+5）中找到最优的 5 张牌型。
func EvaluateBest5(hole [2]Card, community [5]Card) HandRank {
	all := make([]Card, 7)
	copy(all[:2], hole[:])
	copy(all[2:], community[:])

	best := HandRank{Category: -1}
	// C(7,5) = 21 种组合
	indices := combos5of7()
	for _, idx := range indices {
		var five [5]Card
		for j, i := range idx {
			five[j] = all[i]
		}
		h := score5(five)
		if h.Compare(best) > 0 {
			best = h
		}
	}
	return best
}

// combos5of7 返回 7 选 5 的所有下标组合（21 组）。
func combos5of7() [][5]int {
	var result [][5]int
	for a := 0; a < 3; a++ {
		for b := a + 1; b < 4; b++ {
			for c := b + 1; c < 5; c++ {
				for d := c + 1; d < 6; d++ {
					for e := d + 1; e < 7; e++ {
						result = append(result, [5]int{a, b, c, d, e})
					}
				}
			}
		}
	}
	return result
}

// score5 给 5 张牌打分。
func score5(cards [5]Card) HandRank {
	// 先按 rank 升序排列，便于顺子/同花判断。
	sorted := make([]Card, 5)
	copy(sorted, cards[:])
	SortCardsAsc(sorted)

	ranks := [5]int{sorted[0].Rank, sorted[1].Rank, sorted[2].Rank, sorted[3].Rank, sorted[4].Rank}
	suits := [5]int{sorted[0].Suit, sorted[1].Suit, sorted[2].Suit, sorted[3].Suit, sorted[4].Suit}

	isFlush := suits[0] == suits[1] && suits[1] == suits[2] && suits[2] == suits[3] && suits[3] == suits[4]
	straightTop := detectStraight(ranks)

	// 按出现次数归类：count → ranks（降序）
	counts := rankCountsFromSorted(ranks)

	// 检测各牌型
	if isFlush && straightTop > 0 {
		if straightTop == 14 {
			// 皇家同花顺
			return HandRank{Category: 9, Tiebreak: [5]int{14}}
		}
		// 同花顺
		return HandRank{Category: 8, Tiebreak: [5]int{straightTop}}
	}

	// 四条
	if quad, kick := findNOfAKind(counts, 4); quad > 0 {
		return HandRank{Category: 7, Tiebreak: [5]int{quad, kick}}
	}

	// 葫芦 (三带二)
	if trip, pair := findFullHouse(counts); trip > 0 {
		return HandRank{Category: 6, Tiebreak: [5]int{trip, pair}}
	}

	if isFlush {
		// 同花：5 张降序
		return HandRank{Category: 5, Tiebreak: [5]int{ranks[4], ranks[3], ranks[2], ranks[1], ranks[0]}}
	}

	if straightTop > 0 {
		// 顺子
		return HandRank{Category: 4, Tiebreak: [5]int{straightTop}}
	}

	// 三条
	if trip, k1, k2 := findThreeOfAKind(counts); trip > 0 {
		return HandRank{Category: 3, Tiebreak: [5]int{trip, k1, k2}}
	}

	// 两对
	if hp, lp, k := findTwoPair(counts); hp > 0 {
		return HandRank{Category: 2, Tiebreak: [5]int{hp, lp, k}}
	}

	// 一对
	if pair, k1, k2, k3 := findOnePair(counts); pair > 0 {
		return HandRank{Category: 1, Tiebreak: [5]int{pair, k1, k2, k3}}
	}

	// 高牌
	return HandRank{Category: 0, Tiebreak: [5]int{ranks[4], ranks[3], ranks[2], ranks[1], ranks[0]}}
}

// detectStraight 检测顺子，返回最大牌点数（top），0 表示非顺子。
// 轮子 (A-2-3-4-5) 返回 top=5。
func detectStraight(ranks [5]int) int {
	// 必须 5 张不重复
	if ranks[0] == ranks[1] || ranks[1] == ranks[2] || ranks[2] == ranks[3] || ranks[3] == ranks[4] {
		return 0
	}

	// 普通顺子：最末 - 最前 == 4
	if ranks[4]-ranks[0] == 4 {
		return ranks[4]
	}

	// 轮子：A(14) 作 1，即 1,2,3,4,14 → 视为 A-2-3-4-5
	if ranks[0] == 2 && ranks[1] == 3 && ranks[2] == 4 && ranks[3] == 5 && ranks[4] == 14 {
		return 5
	}

	return 0
}

// rankCountsFromSorted 从已升序排列的 5 个 rank 中构建 count → ranks 映射。
func rankCountsFromSorted(ranks [5]int) map[int][]int {
	counts := make(map[int][]int)
	for _, r := range ranks {
		counts[r] = append(counts[r], r)
	}
	return counts
}

// findNOfAKind 在 counts 中寻找恰好出现 n 次的 rank，返回 (该 rank, 剩余最大 kicker)。
func findNOfAKind(counts map[int][]int, n int) (int, int) {
	target := 0
	kicker := 0
	for rank, list := range counts {
		if len(list) == n && rank > target {
			target = rank
		}
	}
	if target == 0 {
		return 0, 0
	}
	for rank, list := range counts {
		if len(list) != n && rank > kicker {
			kicker = rank
		}
	}
	return target, kicker
}

// findFullHouse 在 counts 中寻找葫芦 (三带二)。
func findFullHouse(counts map[int][]int) (trip int, pair int) {
	for rank, list := range counts {
		if len(list) == 3 {
			trip = rank
		}
	}
	if trip == 0 {
		return 0, 0
	}
	for rank, list := range counts {
		if len(list) >= 2 && rank != trip {
			if rank > pair {
				pair = rank
			}
		}
	}
	if pair == 0 {
		return 0, 0
	}
	return trip, pair
}

// findThreeOfAKind 返回三条 rank + 两个 kicker（降序）。
func findThreeOfAKind(counts map[int][]int) (int, int, int) {
	trip := 0
	for rank, list := range counts {
		if len(list) == 3 {
			trip = rank
		}
	}
	if trip == 0 {
		return 0, 0, 0
	}
	var kickers []int
	for rank, list := range counts {
		if len(list) != 3 {
			kickers = append(kickers, rank)
		}
	}
	// 降序
	for i := 0; i < len(kickers)-1; i++ {
		for j := i + 1; j < len(kickers); j++ {
			if kickers[j] > kickers[i] {
				kickers[i], kickers[j] = kickers[j], kickers[i]
			}
		}
	}
	return trip, kickers[0], kickers[1]
}

// findTwoPair 返回 (高对, 低对, 单张)。
func findTwoPair(counts map[int][]int) (int, int, int) {
	var pairs []int
	kicker := 0
	for rank, list := range counts {
		if len(list) == 2 {
			pairs = append(pairs, rank)
		} else {
			kicker = rank
		}
	}
	if len(pairs) < 2 {
		return 0, 0, 0
	}
	// 高对 / 低对
	if pairs[0] > pairs[1] {
		return pairs[0], pairs[1], kicker
	}
	return pairs[1], pairs[0], kicker
}

// findOnePair 返回 (对子 rank, 3 个 kicker 降序)。
func findOnePair(counts map[int][]int) (int, int, int, int) {
	pair := 0
	for rank, list := range counts {
		if len(list) == 2 {
			pair = rank
		}
	}
	if pair == 0 {
		return 0, 0, 0, 0
	}
	var kickers []int
	for rank, list := range counts {
		if len(list) != 2 {
			kickers = append(kickers, rank)
		}
	}
	// 降序
	for i := 0; i < len(kickers)-1; i++ {
		for j := i + 1; j < len(kickers); j++ {
			if kickers[j] > kickers[i] {
				kickers[i], kickers[j] = kickers[j], kickers[i]
			}
		}
	}
	return pair, kickers[0], kickers[1], kickers[2]
}
