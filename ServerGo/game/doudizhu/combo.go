package doudizhu

import "sort"

// ComboType 枚举所有合法牌型。
type ComboType int

const (
	ComboInvalid     ComboType = iota // 非法牌型
	ComboSingle                       // 单张
	ComboPair                         // 对子
	ComboTriple                       // 三张
	ComboTripleSingle                 // 三带一
	ComboTriplePair                   // 三带二
	ComboStraight                     // 顺子（≥5 连续单张）
	ComboPairStraight                 // 连对（≥3 连续对子）
	ComboPlane                        // 飞机（≥2 连续三张，不带）
	ComboPlaneSingle                  // 飞机带单翅膀
	ComboPlanePair                    // 飞机带对翅膀
	ComboQuadTwoSingle                // 四带二（两张单）
	ComboQuadTwoPair                  // 四带二（两个对子）
	ComboBomb                         // 炸弹（四张）
	ComboRocket                       // 王炸（火箭）
)

// Combo 表示一组已解析的合法出牌。
//   - Type:  牌型
//   - Main:  主体点数（用于同型比较）。顺子/连对/飞机取最大连续点数。
//   - Len:   主体长度（顺子张数、连对对数、飞机组数），用于同型必须等长校验。
//   - Cards: 原始牌。
type Combo struct {
	Type  ComboType
	Main  int
	Len   int
	Cards []Card
}

// IsBomb 报告该牌型是否为炸弹或王炸（可压任意非炸弹牌型）。
func (c *Combo) IsBomb() bool {
	return c.Type == ComboBomb || c.Type == ComboRocket
}

// ParseCombo 将一组牌解析为合法牌型；非法返回 (nil, false)。
// 这是服务端权威规则判断的入口。
func ParseCombo(cards []Card) (*Combo, bool) {
	n := len(cards)
	if n == 0 {
		return nil, false
	}

	counts := rankCounts(cards)

	// 王炸：恰好小王 + 大王。
	if n == 2 && counts[RankSmall] == 1 && counts[RankBig] == 1 {
		return &Combo{Type: ComboRocket, Main: RankBig, Len: 1, Cards: cards}, true
	}

	switch n {
	case 1:
		return &Combo{Type: ComboSingle, Main: cards[0].Rank, Len: 1, Cards: cards}, true
	case 2:
		if isSameRank(counts, 2) {
			return &Combo{Type: ComboPair, Main: cards[0].Rank, Len: 1, Cards: cards}, true
		}
		return nil, false
	case 3:
		if isSameRank(counts, 3) {
			return &Combo{Type: ComboTriple, Main: cards[0].Rank, Len: 1, Cards: cards}, true
		}
		return nil, false
	}

	// 炸弹：四张同点。
	if n == 4 {
		if r, ok := singleRankOfCount(counts, 4); ok {
			return &Combo{Type: ComboBomb, Main: r, Len: 1, Cards: cards}, true
		}
	}

	// 三带一 / 三带二。
	if c, ok := parseTripleWithKicker(cards, counts); ok {
		return c, true
	}

	// 四带二（两单 / 两对）。
	if c, ok := parseQuadWithKickers(cards, counts); ok {
		return c, true
	}

	// 顺子。
	if c, ok := parseStraight(cards, counts); ok {
		return c, true
	}

	// 连对。
	if c, ok := parsePairStraight(cards, counts); ok {
		return c, true
	}

	// 飞机（不带 / 带单 / 带对）。
	if c, ok := parsePlane(cards, counts); ok {
		return c, true
	}

	return nil, false
}

// CanBeat 报告 next 是否能压过 prev（即可作为对 prev 的合法跟牌）。
//   - prev 为 nil 表示自由出牌（首出或上家被压完），任何合法牌型都可。
//   - 王炸压一切；炸弹压非炸弹；炸弹间比点数。
//   - 同型必须等长，比主体点数。
func CanBeat(prev, next *Combo) bool {
	if next == nil || next.Type == ComboInvalid {
		return false
	}
	if prev == nil {
		return true // 自由出牌
	}
	// 王炸压一切。
	if next.Type == ComboRocket {
		return prev.Type != ComboRocket
	}
	if prev.Type == ComboRocket {
		return false
	}
	// 炸弹。
	if next.Type == ComboBomb {
		if prev.Type != ComboBomb {
			return true // 炸弹压任意非炸弹
		}
		return next.Main > prev.Main // 炸弹间比点数
	}
	if prev.Type == ComboBomb {
		return false // 非炸弹压不过炸弹
	}
	// 普通牌型：同型且等长，比主体点数。
	if next.Type != prev.Type || next.Len != prev.Len {
		return false
	}
	return next.Main > prev.Main
}

// ─────────────────── 内部解析辅助 ───────────────────

func isSameRank(counts map[int]int, want int) bool {
	if len(counts) != 1 {
		return false
	}
	for _, c := range counts {
		return c == want
	}
	return false
}

// singleRankOfCount 返回出现次数恰为 want 的唯一点数；若不止一个或没有则 false。
func singleRankOfCount(counts map[int]int, want int) (int, bool) {
	found := -1
	for r, c := range counts {
		if c == want {
			if found != -1 {
				return 0, false
			}
			found = r
		} else {
			return 0, false
		}
	}
	if found == -1 {
		return 0, false
	}
	return found, true
}

// parseTripleWithKicker 解析三带一(4张)与三带二(5张)。
func parseTripleWithKicker(cards []Card, counts map[int]int) (*Combo, bool) {
	n := len(cards)
	if n != 4 && n != 5 {
		return nil, false
	}
	tripleRank := -1
	for r, c := range counts {
		if c == 3 {
			if tripleRank != -1 {
				return nil, false // 不止一个三张
			}
			tripleRank = r
		}
	}
	if tripleRank == -1 {
		return nil, false
	}
	if n == 4 {
		// 三带一：剩一张任意单（不能也是同点构成炸弹，已被 count==3 排除）。
		for r, c := range counts {
			if r == tripleRank {
				continue
			}
			if c != 1 {
				return nil, false
			}
		}
		return &Combo{Type: ComboTripleSingle, Main: tripleRank, Len: 1, Cards: cards}, true
	}
	// n == 5：三带二，带的必须是一个对子。
	for r, c := range counts {
		if r == tripleRank {
			continue
		}
		if c != 2 {
			return nil, false
		}
	}
	return &Combo{Type: ComboTriplePair, Main: tripleRank, Len: 1, Cards: cards}, true
}

// parseQuadWithKickers 解析四带二：四张同点 + 两张单(6张) 或 + 两个对子(8张)。
// 四带二不算炸弹。
func parseQuadWithKickers(cards []Card, counts map[int]int) (*Combo, bool) {
	n := len(cards)
	if n != 6 && n != 8 {
		return nil, false
	}
	quadRank := -1
	for r, c := range counts {
		if c == 4 {
			if quadRank != -1 {
				return nil, false
			}
			quadRank = r
		}
	}
	if quadRank == -1 {
		return nil, false
	}
	if n == 6 {
		// 带两张单：剩余两张，可同点(对子)或不同点，均视为两张“单”。
		rest := 0
		for r, c := range counts {
			if r == quadRank {
				continue
			}
			rest += c
		}
		if rest != 2 {
			return nil, false
		}
		return &Combo{Type: ComboQuadTwoSingle, Main: quadRank, Len: 1, Cards: cards}, true
	}
	// n == 8：带两个对子。
	pairs := 0
	for r, c := range counts {
		if r == quadRank {
			continue
		}
		if c != 2 {
			return nil, false
		}
		pairs++
	}
	if pairs != 2 {
		return nil, false
	}
	return &Combo{Type: ComboQuadTwoPair, Main: quadRank, Len: 1, Cards: cards}, true
}

// straightRanks 返回去重后的点数升序列表，并校验全为单张计数。
func sortedRanks(counts map[int]int) []int {
	rs := make([]int, 0, len(counts))
	for r := range counts {
		rs = append(rs, r)
	}
	sort.Ints(rs)
	return rs
}

// isConsecutive 报告升序点数序列是否连续，且全部 < Rank2（2/王不入连续牌型）。
func isConsecutive(rs []int) bool {
	if len(rs) == 0 {
		return false
	}
	for _, r := range rs {
		if r >= Rank2 { // 2、小王、大王都不允许进入顺子/连对/飞机
			return false
		}
	}
	for i := 1; i < len(rs); i++ {
		if rs[i] != rs[i-1]+1 {
			return false
		}
	}
	return true
}

// parseStraight 解析顺子：≥5 张连续单牌。
func parseStraight(cards []Card, counts map[int]int) (*Combo, bool) {
	if len(cards) < 5 {
		return nil, false
	}
	for _, c := range counts {
		if c != 1 {
			return nil, false
		}
	}
	rs := sortedRanks(counts)
	if len(rs) != len(cards) || !isConsecutive(rs) {
		return nil, false
	}
	return &Combo{Type: ComboStraight, Main: rs[len(rs)-1], Len: len(rs), Cards: cards}, true
}

// parsePairStraight 解析连对：≥3 对连续对子。
func parsePairStraight(cards []Card, counts map[int]int) (*Combo, bool) {
	if len(cards) < 6 || len(cards)%2 != 0 {
		return nil, false
	}
	for _, c := range counts {
		if c != 2 {
			return nil, false
		}
	}
	rs := sortedRanks(counts)
	if len(rs) < 3 || !isConsecutive(rs) {
		return nil, false
	}
	return &Combo{Type: ComboPairStraight, Main: rs[len(rs)-1], Len: len(rs), Cards: cards}, true
}

// parsePlane 解析飞机：≥2 组连续三张，可不带、带等量单、带等量对。
func parsePlane(cards []Card, counts map[int]int) (*Combo, bool) {
	// 找出所有三张(及以上)的点数。注意四张同点在此场景按三张主体 + 单翅膀处理较复杂，
	// 这里只接受恰为三张的连续主体；炸弹/四带二已在前面单独处理。
	tripleRanks := make([]int, 0)
	otherSingles := 0
	otherPairs := 0
	for r, c := range counts {
		switch c {
		case 3:
			tripleRanks = append(tripleRanks, r)
		case 1:
			otherSingles++
		case 2:
			otherPairs++
		case 4:
			// 四张：拆为三张主体 + 一张单翅膀。
			tripleRanks = append(tripleRanks, r)
			otherSingles++
		default:
			return nil, false
		}
	}
	if len(tripleRanks) < 2 {
		return nil, false
	}
	sort.Ints(tripleRanks)
	if !isConsecutive(tripleRanks) {
		return nil, false
	}
	groups := len(tripleRanks)
	mainTop := tripleRanks[groups-1]

	// 不带翅膀：总数 == 3*groups。
	if len(cards) == 3*groups && otherSingles == 0 && otherPairs == 0 {
		return &Combo{Type: ComboPlane, Main: mainTop, Len: groups, Cards: cards}, true
	}
	// 带单翅膀：每组带一张单，翅膀数 == groups。翅膀来自非三张点数的单 + 拆出的四张多余单。
	if len(cards) == 4*groups && otherSingles == groups && otherPairs == 0 {
		return &Combo{Type: ComboPlaneSingle, Main: mainTop, Len: groups, Cards: cards}, true
	}
	// 带对翅膀：每组带一对，翅膀对数 == groups。
	if len(cards) == 5*groups && otherPairs == groups && otherSingles == 0 {
		return &Combo{Type: ComboPlanePair, Main: mainTop, Len: groups, Cards: cards}, true
	}
	return nil, false
}
