// Package thpagent — hand_eval.go: 真正的 7 选 5 最优牌型评估器（2026-08-20 §德州扑克Agent-B1）。
//
// 背景（B1 P0 修复）：
//   旧 scoreHand 只取「前 5 张」（2 底牌 + 前 3 公共牌），turn/river 被完全忽略；
//   且 OnePair 与 HighCard 同档（return 0），平局几乎不可能正确判定。
//
// 本文件实现：
//   - eval5: 5 张牌 → (category, tiebreak[5])，按字典序比较（含 kicker）
//   - evalBest: 5..7 张牌枚举 C(n,5) 组合取最大
//
// 牌型分类（category，从大到小比较）：
//   8 = 同花顺（含皇家，靠 tiebreak 区分）
//   7 = 四条 / 6 = 葫芦 / 5 = 同花 / 4 = 顺子 / 3 = 三条 / 2 = 两对 / 1 = 一对 / 0 = 高牌
//
// 约束：thpagent **不能** import game/texasholdem（room.go 已 import thpagent，
// 反向会循环），因此此处自实现，编码走 cards.go 的唯一规范编码。
package thpagent

// handScore 是一手 5 张牌的可比较评分。
type handScore struct {
	category int    // 0..8（见文件头）
	tiebreak [5]int // 降序比较键（rank 2..14；不同牌型使用方式不同，同 texasholdem.HandRank）
}

// compare 比较两手牌。>0 表示 s 大，<0 表示 o 大，0 完全平局。
func (s handScore) compare(o handScore) int {
	if s.category != o.category {
		return s.category - o.category
	}
	for i := 0; i < 5; i++ {
		if s.tiebreak[i] != o.tiebreak[i] {
			return s.tiebreak[i] - o.tiebreak[i]
		}
	}
	return 0
}

// 牌型类别常量。
const (
	catHighCard     = 0
	catOnePair      = 1
	catTwoPair      = 2
	catTrips        = 3
	catStraight     = 4
	catFlush        = 5
	catFullHouse    = 6
	catQuads        = 7
	catStraightFlush = 8
)

// eval5 评估恰好 5 张规范编码牌。cards 含 0（无牌哨兵）时返回 category=-1 的最小分。
func eval5(cards [5]int) handScore {
	var ranks [5]int
	suit0 := 0
	isFlush := true
	rankCount := [15]int{} // rank 2..14

	for i, c := range cards {
		rank, suit := DecodeCard(c)
		if rank == 0 {
			return handScore{category: -1}
		}
		ranks[i] = rank
		rankCount[rank]++
		if i == 0 {
			suit0 = suit
		} else if suit != suit0 {
			isFlush = false
		}
	}

	// 唯一 rank 降序列表
	unique := make([]int, 0, 5)
	for r := 14; r >= 2; r-- {
		if rankCount[r] > 0 {
			unique = append(unique, r)
		}
	}

	// 顺子判定（含 A-2-3-4-5 轮子）
	straightHigh := 0
	if len(unique) == 5 {
		if unique[0]-unique[4] == 4 {
			straightHigh = unique[0]
		} else if unique[0] == 14 && unique[1] == 5 && unique[4] == 2 {
			straightHigh = 5 // 轮子顺子，5 为高牌
		}
	}

	switch {
	case isFlush && straightHigh > 0:
		return handScore{category: catStraightFlush, tiebreak: [5]int{straightHigh}}
	}

	// 统计 count 分组：quads / trips / pairs
	quad, trip := 0, 0
	pairs := make([]int, 0, 2)
	for r := 14; r >= 2; r-- {
		switch rankCount[r] {
		case 4:
			quad = r
		case 3:
			trip = r
		case 2:
			pairs = append(pairs, r)
		}
	}

	kickersDesc := func(exclude map[int]bool) []int {
		out := make([]int, 0, 5)
		for r := 14; r >= 2; r-- {
			if rankCount[r] > 0 && !exclude[r] {
				out = append(out, r)
			}
		}
		return out
	}

	switch {
	case quad > 0:
		k := kickersDesc(map[int]bool{quad: true})
		return handScore{category: catQuads, tiebreak: [5]int{quad, k[0]}}
	case trip > 0 && len(pairs) > 0:
		return handScore{category: catFullHouse, tiebreak: [5]int{trip, pairs[0]}}
	case isFlush:
		var tb [5]int
		copy(tb[:], unique)
		return handScore{category: catFlush, tiebreak: tb}
	case straightHigh > 0:
		return handScore{category: catStraight, tiebreak: [5]int{straightHigh}}
	case trip > 0:
		k := kickersDesc(map[int]bool{trip: true})
		return handScore{category: catTrips, tiebreak: [5]int{trip, k[0], k[1]}}
	case len(pairs) == 2:
		k := kickersDesc(map[int]bool{pairs[0]: true, pairs[1]: true})
		return handScore{category: catTwoPair, tiebreak: [5]int{pairs[0], pairs[1], k[0]}}
	case len(pairs) == 1:
		k := kickersDesc(map[int]bool{pairs[0]: true})
		return handScore{category: catOnePair, tiebreak: [5]int{pairs[0], k[0], k[1], k[2]}}
	default:
		var tb [5]int
		copy(tb[:], unique)
		return handScore{category: catHighCard, tiebreak: tb}
	}
}

// evalBest 从 5..7 张规范编码牌中枚举所有 C(n,5) 组合，返回最优评分。
// 不足 5 张或含非法牌时返回 category=-1 的最小分。
func evalBest(cards []int) handScore {
	if len(cards) < 5 {
		return handScore{category: -1}
	}
	best := handScore{category: -1}
	n := len(cards)
	// 枚举 5 张组合（n ≤ 7 → 最多 21 种）
	var combo [5]int
	var dfs func(start, depth int)
	dfs = func(start, depth int) {
		if depth == 5 {
			s := eval5(combo)
			if s.compare(best) > 0 {
				best = s
			}
			return
		}
		for i := start; i <= n-(5-depth); i++ {
			combo[depth] = cards[i]
			dfs(i+1, depth+1)
		}
	}
	dfs(0, 0)
	return best
}

// evalHoleCommunity 评估「底牌 + 公共牌」的最优 5 张（hole 2 张 + community 3..5 张）。
func evalHoleCommunity(hole, community []int) handScore {
	all := make([]int, 0, 7)
	all = append(all, hole...)
	all = append(all, community...)
	return evalBest(all)
}

// CompareIntCards 比较两手 5..7 张规范编码牌：>0 a 胜，0 平局，<0 b 胜。
//
// 导出用途：game/texasholdem 的编码一致性测试（card_encoding_test.go）
// 把本评估器与引擎自带 EvaluateBest5 做交叉对照，防止两套实现漂移。
// 生产路径仅 decision.go 使用（不导出会让测试无法锚定唯一规范编码语义）。
func CompareIntCards(a, b []int) int {
	return evalBest(a).compare(evalBest(b))
}
