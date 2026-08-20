// Package thpagent — decision.go: 德州扑克 Bot 决策数学引擎（2026-08-19）。
//
// 本文件实现 4 个纯函数（无 LLM 调用，纯数学），按 [德州扑克Agent数学引擎设计.md]
// 文档规约：
//   1. handStrength — 蒙特卡洛 1000 次胜率抽样
//   2. potOdds      — 底池赔率 + 跟注所需最低胜率
//   3. position     — 6-max 位置标签 (BTN/SB/BB/UTG/MP/CO)
//   4. bluffFrequency — 基于对手弃牌率反推的虚张频率建议
//
// 性能预算：
//   - handStrength ≈ 1.0s（1000 次蒙特卡洛）；缓存命中 O(1)
//   - potOdds / position / bluffFrequency < 0.1ms
package thpagent

import (
	"math/rand"
	"sync"
	"time"
)

// HandStrengthCache 是 handStrength 的 sync.Map 缓存，key 是 (底牌+公共牌+亮张数)。
// 6-max 单房间内最多约 100 个唯一 key, sync.Map 已足够。
var HandStrengthCache sync.Map

// handStrengthKey 是缓存键 — 用 7 个 int 编码 (hole0, hole1, c0..c4, nShown)。
type handStrengthKey struct {
	h0, h1, c0, c1, c2, c3, c4, nShown int
}

// handStrengthResult 是缓存值。
type handStrengthResult struct {
	Win  float64
	Draw float64
}

// HandStrength 蒙特卡洛计算胜率（对手底牌完全随机）。返回胜率与平局率。
//   - hole: 自己的两张底牌（cards.go 唯一规范编码 1..52）
//   - community: 已亮的 0..5 张公共牌；未亮部分填 0
//   - nShown: 已亮公共牌数 0..5
//   - nSimulations: 蒙特卡洛抽样次数；0 时用默认 1000
//
// 注意：时间种子 + 结果缓存（同 key O(1) 命中）。需要确定性结果的测试
// 请走 HandStrengthSeed / HandStrengthVS（§数学引擎设计.md §3 基准测试）。
func HandStrength(hole [2]int, community [5]int, nShown, nSimulations int) (win, draw float64) {
	if nSimulations <= 0 {
		nSimulations = 1000
	}
	if nShown < 0 {
		nShown = 0
	}
	if nShown > 5 {
		nShown = 5
	}

	key := handStrengthKey{
		h0: hole[0], h1: hole[1],
		c0: community[0], c1: community[1], c2: community[2],
		c3: community[3], c4: community[4],
		nShown: nShown,
	}
	if v, ok := HandStrengthCache.Load(key); ok {
		r := v.(handStrengthResult)
		return r.Win, r.Draw
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	win, draw = handStrengthMC(hole, nil, community, nShown, nSimulations, rng)
	HandStrengthCache.Store(key, handStrengthResult{Win: win, Draw: draw})
	return win, draw
}

// HandStrengthSeed 与 HandStrength 相同但使用固定种子（测试确定性，防 flaky）。
// 不走缓存（缓存键不含种子，走缓存会让基准测试互相污染）。
func HandStrengthSeed(hole [2]int, community [5]int, nShown, nSimulations int, seed int64) (win, draw float64) {
	rng := rand.New(rand.NewSource(seed))
	return handStrengthMC(hole, nil, community, nShown, nSimulations, rng)
}

// HandStrengthVS 蒙特卡洛计算 vs 指定对手底牌的胜率（测试/分析用）。
// oppHole 必须不与 hole / community 冲突；冲突时对手牌仍在牌堆中会被剔除
// （makeDeckExcluding 保证），但胜率语义已失真 —— 调用方保证不冲突。
func HandStrengthVS(hole, oppHole [2]int, community [5]int, nShown, nSimulations int, rng *rand.Rand) (win, draw float64) {
	if rng == nil {
		rng = rand.New(rand.NewSource(42))
	}
	return handStrengthMC(hole, &oppHole, community, nShown, nSimulations, rng)
}

// handStrengthMC 是蒙特卡洛主循环。oppHole 为 nil 时对手底牌随机。
//
// 2026-08-20 §B1 修复：旧实现有两处物理正确性 bug：
//  1. 旧编码（Rank*4+Suit+1，值域 10..61）的底牌 >52 永远不会从 1..52 的
//     抽样牌堆中剔除 → 对手可被发到与自己相同的物理牌；
//  2. 对手底牌从「未剔除已抽样公共牌」的牌堆中再抽 → 对手可拿到与本次
//     抽样公共牌相同的牌。
// 现在：牌堆一次性剔除 hole + 已亮公共牌（+ 指定 oppHole），每次迭代从同一
// 剩余牌堆无放回地抽「待亮公共牌 + 对手底牌」。
func handStrengthMC(hole [2]int, oppHole *[2]int, community [5]int, nShown, nSimulations int, rng *rand.Rand) (win, draw float64) {
	if nSimulations <= 0 {
		nSimulations = 1000
	}
	if nShown < 0 {
		nShown = 0
	}
	if nShown > 5 {
		nShown = 5
	}

	exclude := [][]int{hole[:], community[:nShown]}
	if oppHole != nil {
		exclude = append(exclude, oppHole[:])
	}
	deck := makeDeckExcluding(exclude...)

	wins := 0
	draws := 0
	for i := 0; i < nSimulations; i++ {
		nCommNeeded := 5 - nShown
		nOpp := 0
		if oppHole == nil {
			nOpp = 2
		}
		drawn := sampleN(deck, nCommNeeded+nOpp, rng)

		// 抽样补全到 5 张公共牌
		sample := append([]int{}, community[:nShown]...)
		sample = append(sample, drawn[:nCommNeeded]...)

		var opp []int
		if oppHole != nil {
			opp = oppHole[:]
		} else {
			opp = drawn[nCommNeeded : nCommNeeded+nOpp]
		}

		myScore := evalHoleCommunity(hole[:], sample)
		oppScore := evalHoleCommunity(opp, sample)

		cmp := myScore.compare(oppScore)
		if cmp > 0 {
			wins++
		} else if cmp == 0 {
			draws++
		}
	}

	return float64(wins) / float64(nSimulations), float64(draws) / float64(nSimulations)
}

// makeDeckExcluding 返回完整 52 张牌的索引数组（0..51），剔除指定牌。
func makeDeckExcluding(exclude ...[]int) []int {
	excludeSet := make(map[int]bool)
	for _, slice := range exclude {
		for _, c := range slice {
			if c > 0 {
				excludeSet[c] = true
			}
		}
	}
	deck := make([]int, 0, 52-len(excludeSet))
	for c := 1; c <= 52; c++ {
		if !excludeSet[c] {
			deck = append(deck, c)
		}
	}
	return deck
}

// sampleN 从 deck 中随机抽取 n 张，返回新切片（不修改 deck）。
func sampleN(deck []int, n int, rng *rand.Rand) []int {
	if n >= len(deck) {
		out := make([]int, len(deck))
		copy(out, deck)
		return out
	}
	// Fisher-Yates 抽样
	 work := make([]int, len(deck))
	 copy(work, deck)
	 out := make([]int, n)
	 for i := 0; i < n; i++ {
		 j := i + rng.Intn(len(work)-i)
		 work[i], work[j] = work[j], work[i]
		 out[i] = work[i]
	 }
	 return out
}

// scoreHand / detectCategory 已于 2026-08-20 §B1 删除 —— 旧实现只取前 5 张
// （turn/river 被忽略）且 OnePair 与 HighCard 同档。牌型评估统一走
// hand_eval.go 的 evalBest（C(7,5)=21 组合取最大 + kicker 字典序）。

// PotOdds 计算底池赔率。返回 (odds, required_equity)，都是 0.0-1.0。
//   - callAmount: 跟注所需金额
//   - pot: 当前底池
//
// 公式：odds = callAmount / (pot + callAmount)，required_equity = odds
// 边界：callAmount <= 0 → 返回 (0, 0)
func PotOdds(callAmount, pot int) (odds, requiredEquity float64) {
	if callAmount <= 0 {
		return 0, 0
	}
	totalPot := pot + callAmount
	if totalPot <= 0 {
		return 0, 0
	}
	odds = float64(callAmount) / float64(totalPot)
	requiredEquity = odds
	return odds, requiredEquity
}

// Position 返回 6-max 位置标签。
//   - seat: 当前座位 0..5
//   - button: 庄家座位 0..5
//
// 6-max 位置映射（按 button 顺时针）：
//
//        Button(BTN) <- button+0
//       /            \
//    SB <- button+1  BB <- button+2
//    |                |
//   UTG <- button+3  CO <- button+5
//    MP <- button+4
func Position(seat, button int) (label, labelZh string) {
	if button < 0 || button >= 6 {
		return "BTN", "庄家位"
	}
	offset := (seat - button + 6) % 6
	switch offset {
	case 0:
		return "BTN", "庄家位"
	case 1:
		return "SB", "小盲位"
	case 2:
		return "BB", "大盲位"
	case 3:
		return "UTG", "枪口位"
	case 4:
		return "MP", "中位"
	case 5:
		return "CO", "关煞位"
	default:
		return "BTN", "庄家位"
	}
}

// BluffFrequency 基于对手弃牌率反推的虚张频率建议。
//   - opponentFoldRate: 对手最近 N 手牌的弃牌率 0.0-1.0
//
// 映射表：
//   - ≥ 70% → 0.35（高弃牌率对手，多偷）
//   - 50-70% → 0.25（中高弃牌率，标准偷盲）
//   - 30-50% → 0.15（中性，偶尔偷盲）
//   - 10-30% → 0.08（黏池对手，少偷）
//   - ≤ 10% → 0.03（极黏池，几乎不偷）
func BluffFrequency(opponentFoldRate float64) float64 {
	switch {
	case opponentFoldRate <= 0.10:
		return 0.03
	case opponentFoldRate <= 0.30:
		return 0.08
	case opponentFoldRate <= 0.50:
		return 0.15
	case opponentFoldRate <= 0.70:
		return 0.25
	default:
		return 0.35
	}
}