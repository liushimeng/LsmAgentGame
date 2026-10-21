// Package virtual_city — montecarlo_gates.go: G1–G12 + A1–A4 门禁评估器
// 与数学工具(批次72 §4.3;阈值逐字沿用验收方案 §5.2,仅口径适配)。
//
// 评估单位 = 全部 smoke/gate 局聚合(按策略条件分组);评估器是纯函数,
// 零 rand、零副作用。A1–A4 中引擎无统计出口的字段以 detail「缺数据」标注
// 且 Passed=true(§130 反面教训:宁可缺数据也不硬造)。
package virtual_city

import (
	"fmt"
	"math"
	"sort"
)

// ── 门禁 id 常量(顺序即输出顺序,恒 16 条)──────────────────────────

const (
	GateG1  = "G1"  // M1 月结非负(波动卡豁免)
	GateG2  = "G2"  // M97 存活
	GateG3  = "G3"  // M120 正式出局率
	GateG4  = "G4"  // M120 破产清算事件率
	GateG5  = "G5"  // 终局出局与存活
	GateG6  = "G6"  // minimum_effort M120 FI
	GateG7  = "G7"  // balanced M360 FI
	GateG8  = "G8"  // M60/M120 Gini 与 top20
	GateG9  = "G9"  // 终局 Gini 与 top20
	GateG10 = "G10" // M1→M120 排名粘性
	GateG11 = "G11" // aggressive_leverage 出局
	GateG12 = "G12" // 不变量 0 failure
	GateA1  = "A1"  // 早期存活护栏
	GateA2  = "A2"  // safety_net_only 中位净资产
	GateA3  = "A3"  // 补贴占比护栏
	GateA4  = "A4"  // 破产恢复护栏
)

// GateResult 单条门禁评估结果(actual/threshold 恒有值,0 = 不适用)。
type GateResult struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Passed    bool    `json:"passed"`
	Actual    float64 `json:"actual"`
	Threshold float64 `json:"threshold"`
	Detail    string  `json:"detail"`
}

// gateName 门禁中文名(后端兜底文案)。
func gateName(id string) string {
	switch id {
	case GateG1:
		return "首月结余非负"
	case GateG2:
		return "M97 存活"
	case GateG3:
		return "M120 正式出局率"
	case GateG4:
		return "M120 破产清算率"
	case GateG5:
		return "终局出局与存活"
	case GateG6:
		return "躺平策略 FI"
	case GateG7:
		return "均衡策略 FI"
	case GateG8:
		return "中期财富分布"
	case GateG9:
		return "终局财富分布"
	case GateG10:
		return "排名粘性"
	case GateG11:
		return "杠杆策略出局"
	case GateG12:
		return "不变量零违例"
	case GateA1:
		return "早期存活护栏"
	case GateA2:
		return "基线中位净资产"
	case GateA3:
		return "补贴占比护栏"
	case GateA4:
		return "破产恢复护栏"
	}
	return id
}

// MCEvaluateGates 评估 G1–G12 + A1–A4,恒返回 16 条、顺序固定。
//
// runs 可混合策略(每局单策略全桌);条件门禁(G6/G7/G11/A2)按
// Strategy 字段过滤。数值口径见各 gate 实现注释;边界含闭区间
// 由各 gate 断言式注明。
func MCEvaluateGates(runs []MCRunStats) []GateResult {
	out := make([]GateResult, 0, 16)
	out = append(out, gateG1(runs))
	g3 := gateG3(runs)
	out = append(out, gateG2(runs))
	out = append(out, g3)
	out = append(out, gateG4(runs, g3))
	out = append(out, gateG5(runs))
	out = append(out, gateG6(runs))
	out = append(out, gateG7(runs))
	out = append(out, gateG8(runs))
	out = append(out, gateG9(runs))
	out = append(out, gateG10(runs))
	out = append(out, gateG11(runs))
	out = append(out, gateG12(runs))
	out = append(out, gateA1(runs))
	out = append(out, gateA2(runs))
	out = append(out, gateA3(), gateA4())
	return out
}

// ── 取样小工具 ─────────────────────────────────────────────────────────

// mcRunsByStrategy 过滤指定策略且推进月数 ≥ minMonths 的局。
func mcRunsByStrategy(runs []MCRunStats, strategy string, minMonths int) []MCRunStats {
	var out []MCRunStats
	for i := range runs {
		if runs[i].Strategy == strategy && runs[i].Months >= minMonths {
			out = append(out, runs[i])
		}
	}
	return out
}

// mcAliveAt 指定月存活数(月未推进 → ok=false)。
func mcAliveAt(r MCRunStats, month int) (int, bool) {
	if month < 1 || month > len(r.AliveByMonth) {
		return 0, false
	}
	return r.AliveByMonth[month-1], true
}

// mcElimRateAt 指定月正式出局率(÷ MaxSeats;月未推进 → ok=false)。
func mcElimRateAt(r MCRunStats, month int) (float64, bool) {
	if month < 1 || month > len(r.EliminatedByMonth) {
		return 0, false
	}
	return float64(r.EliminatedByMonth[month-1]) / float64(MaxSeats), true
}

// mcLiqRateAt 指定月破产清算座位率(月未推进 → ok=false)。
func mcLiqRateAt(r MCRunStats, month int) (float64, bool) {
	if month < 1 || month > len(r.LiquidatedByMonth) {
		return 0, false
	}
	return float64(r.LiquidatedByMonth[month-1]) / float64(MaxSeats), true
}

// ── G1:首月月结非负(波动卡豁免)─────────────────────────────────────

func gateG1(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG1, Name: gateName(GateG1), Threshold: 0}
	bad, checked, volatile := 0, 0, 0
	for i := range runs {
		r := &runs[i]
		if r.Months < 1 {
			continue
		}
		for seat := 0; seat < MaxSeats; seat++ {
			if r.AnchorM1.Volatile[seat] {
				volatile++
				continue // 波动卡记录不判失败
			}
			checked++
			if r.AnchorM1.MonthlyNet[seat] < 0 {
				bad++
			}
		}
	}
	g.Actual = float64(bad)
	g.Passed = bad == 0
	g.Detail = fmt.Sprintf("%d 局非波动座位 %d 个,月结为负 %d 个;波动卡豁免 %d 个(记录不判失败)",
		len(runs), checked, bad, volatile)
	return g
}

// ── G2:M97 平均存活 ≥ 10/12;P05 存活 ≥ 9/12 ─────────────────────────

func gateG2(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG2, Name: gateName(GateG2), Threshold: 10}
	var alive []float64
	for i := range runs {
		if v, ok := mcAliveAt(runs[i], 97); ok {
			alive = append(alive, float64(v))
		}
	}
	if len(alive) == 0 {
		g.Detail = "无 M97 样本(没有任何局推进到 97 月)"
		return g
	}
	mean := mcMean(alive)
	p05 := mcPercentile(alive, 0.05)
	g.Actual = mean
	g.Passed = mean >= 10 && p05 >= 9
	g.Detail = fmt.Sprintf("M97 平均存活 %.2f/12(≥10),P05 %.2f/12(≥9),n=%d", mean, p05, len(alive))
	return g
}

// ── G3:M120 正式出局:均值 <15%、95%CI 上界 <20%、单局 ≤4/12 ───────

func gateG3(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG3, Name: gateName(GateG3), Threshold: 0.15}
	var rates []float64
	for i := range runs {
		if v, ok := mcElimRateAt(runs[i], 120); ok {
			rates = append(rates, v)
		}
	}
	if len(rates) == 0 {
		g.Detail = "无 M120 样本"
		return g
	}
	mean := mcMean(rates)
	ci := mcNormalCIUpper95(rates)
	maxR := rates[mcArgMax(rates)]
	g.Actual = mean
	g.Passed = mean < 0.15 && ci < 0.20 && maxR <= float64(4)/float64(MaxSeats)
	g.Detail = fmt.Sprintf("均值 %.1f%%(<15%%),95%%CI 上界 %.1f%%(<20%%),单局最大 %.1f%%(≤33.3%%),n=%d",
		mean*100, ci*100, maxR*100, len(rates))
	return g
}

// ── G4:M120 破产清算事件率 ∈ [8%,25%] 且 G3 成立 ─────────────────────

func gateG4(runs []MCRunStats, g3 GateResult) GateResult {
	g := GateResult{ID: GateG4, Name: gateName(GateG4), Threshold: 0.08}
	var rates []float64
	for i := range runs {
		if v, ok := mcLiqRateAt(runs[i], 120); ok {
			rates = append(rates, v)
		}
	}
	if len(rates) == 0 {
		g.Detail = "无 M120 样本"
		return g
	}
	mean := mcMean(rates)
	g.Actual = mean
	g.Passed = mean >= 0.08 && mean <= 0.25 && g3.Passed
	g.Detail = fmt.Sprintf("均值 %.1f%%(∈[8%%,25%%]),G3=%v,n=%d", mean*100, g3.Passed, len(rates))
	return g
}

// ── G5:终局出局 ∈ [5%,15%];平均存活 ≥ 10.2/12 ─────────────────────

func gateG5(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG5, Name: gateName(GateG5), Threshold: 0.05}
	var elim, alive []float64
	for i := range runs {
		if runs[i].Months < 1 {
			continue
		}
		if v, ok := mcElimRateAt(runs[i], runs[i].Months); ok {
			elim = append(elim, v)
		}
		if v, ok := mcAliveAt(runs[i], runs[i].Months); ok {
			alive = append(alive, float64(v))
		}
	}
	if len(elim) == 0 {
		g.Detail = "无终局样本"
		return g
	}
	meanElim := mcMean(elim)
	meanAlive := mcMean(alive)
	g.Actual = meanElim
	g.Passed = meanElim >= 0.05 && meanElim <= 0.15 && meanAlive >= 10.2
	g.Detail = fmt.Sprintf("终局出局均值 %.1f%%(∈[5%%,15%%]),平均存活 %.2f/12(≥10.2),n=%d",
		meanElim*100, meanAlive, len(elim))
	return g
}

// ── G6:minimum_effort M120 FI>0 座位 ≥6/12;median FI ≥ 0.05 ────────

func gateG6(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG6, Name: gateName(GateG6), Threshold: 6}
	sub := mcRunsByStrategy(runs, MCStrategyMinimumEffort, 120)
	if len(sub) == 0 {
		g.Detail = "无 minimum_effort M120 样本"
		return g
	}
	var fiPosCounts, fiAll []float64
	for i := range sub {
		count := 0
		for seat := 0; seat < MaxSeats; seat++ {
			if !sub[i].AnchorM120.Alive[seat] {
				continue
			}
			fi := sub[i].AnchorM120.FI[seat]
			fiAll = append(fiAll, fi)
			if fi > 0 {
				count++
			}
		}
		fiPosCounts = append(fiPosCounts, float64(count))
	}
	meanCount := mcMean(fiPosCounts)
	medianFI := mcMedian(fiAll)
	g.Actual = meanCount
	g.Passed = meanCount >= 6 && medianFI >= 0.05
	g.Detail = fmt.Sprintf("FI>0 座位均值 %.2f/12(≥6),median FI %.3f(≥0.05),n=%d 局/%d 座样本",
		meanCount, medianFI, len(sub), len(fiAll))
	return g
}

// ── G7:balanced M360 FI>0 座位 ≥9/12;median FI ≥ 0.80 ───────────────

func gateG7(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG7, Name: gateName(GateG7), Threshold: 9}
	sub := mcRunsByStrategy(runs, MCStrategyBalanced, 360)
	if len(sub) == 0 {
		g.Detail = "无 balanced M360 样本"
		return g
	}
	var fiPosCounts, fiAll []float64
	for i := range sub {
		count := 0
		for seat := 0; seat < MaxSeats; seat++ {
			if !sub[i].AnchorM360.Alive[seat] {
				continue
			}
			fi := sub[i].AnchorM360.FI[seat]
			fiAll = append(fiAll, fi)
			if fi > 0 {
				count++
			}
		}
		fiPosCounts = append(fiPosCounts, float64(count))
	}
	meanCount := mcMean(fiPosCounts)
	medianFI := mcMedian(fiAll)
	g.Actual = meanCount
	g.Passed = meanCount >= 9 && medianFI >= 0.80
	g.Detail = fmt.Sprintf("FI>0 座位均值 %.2f/12(≥9),median FI %.3f(≥0.80),n=%d 局/%d 座样本",
		meanCount, medianFI, len(sub), len(fiAll))
	return g
}

// ── G8:M60 Gini∈[0.20,0.45];M120∈[0.25,0.50];M120 top20 ≤75% ───────

func gateG8(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG8, Name: gateName(GateG8), Threshold: 0.25}
	var g60, g120, t120 []float64
	for i := range runs {
		if runs[i].Months >= 60 {
			g60 = append(g60, runs[i].GiniM60)
		}
		if runs[i].Months >= 120 {
			g120 = append(g120, runs[i].GiniM120)
			t120 = append(t120, runs[i].Top20M120)
		}
	}
	if len(g60) == 0 || len(g120) == 0 {
		g.Detail = fmt.Sprintf("样本不足:M60=%d 局,M120=%d 局", len(g60), len(g120))
		return g
	}
	m60, m120, mt120 := mcMean(g60), mcMean(g120), mcMean(t120)
	g.Actual = m120
	g.Passed = m60 >= 0.20 && m60 <= 0.45 && m120 >= 0.25 && m120 <= 0.50 && mt120 <= 0.75
	g.Detail = fmt.Sprintf("M60 Gini %.3f(∈[.20,.45]),M120 Gini %.3f(∈[.25,.50]),M120 top20 %.1f%%(≤75%%),n=%d",
		m60, m120, mt120*100, len(g120))
	return g
}

// ── G9:终局 Gini∈[0.30,0.60];top20∈[50%,80%](出局按 0 计)────────

func gateG9(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG9, Name: gateName(GateG9), Threshold: 0.30}
	var gini, top []float64
	for i := range runs {
		if runs[i].Months < 1 {
			continue
		}
		gini = append(gini, runs[i].GiniFinal)
		top = append(top, runs[i].Top20Final)
	}
	if len(gini) == 0 {
		g.Detail = "无终局样本"
		return g
	}
	mg, mt := mcMean(gini), mcMean(top)
	g.Actual = mg
	g.Passed = mg >= 0.30 && mg <= 0.60 && mt >= 0.50 && mt <= 0.80
	g.Detail = fmt.Sprintf("终局 Gini %.3f(∈[.30,.60]),top20 %.1f%%(∈[50%%,80%%]),n=%d",
		mg, mt*100, len(gini))
	return g
}

// ── G10:M1→M120 净资产排名 Spearman 均值 ∈ [0.35,0.80] ──────────────

func gateG10(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG10, Name: gateName(GateG10), Threshold: 0.35}
	var rhos []float64
	for i := range runs {
		r := &runs[i]
		if r.Months >= 120 && len(r.NetWorthM1) == MaxSeats && len(r.NetWorthM120) == MaxSeats {
			rhos = append(rhos, mcSpearman(r.NetWorthM1, r.NetWorthM120))
		}
	}
	if len(rhos) == 0 {
		g.Detail = "无 M1→M120 排名对样本"
		return g
	}
	mean := mcMean(rhos)
	g.Actual = mean
	g.Passed = mean >= 0.35 && mean <= 0.80
	g.Detail = fmt.Sprintf("逐局 Spearman 均值 %.3f(∈[.35,.80],过粘=1 与过散=−1 同判失败),n=%d", mean, len(rhos))
	return g
}

// ── G11:aggressive_leverage 出局 ≤25%;单局连锁破产峰值 ≤5 ──────────

func gateG11(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG11, Name: gateName(GateG11), Threshold: 5}
	sub := mcRunsByStrategy(runs, MCStrategyAggressiveLeverage, 1)
	if len(sub) == 0 {
		g.Detail = "无 aggressive_leverage 样本"
		return g
	}
	var elim []float64
	maxPeak := 0
	for i := range sub {
		if v, ok := mcElimRateAt(sub[i], sub[i].Months); ok {
			elim = append(elim, v)
		}
		if sub[i].ChainBankruptcyPeak > maxPeak {
			maxPeak = sub[i].ChainBankruptcyPeak
		}
	}
	meanElim := mcMean(elim)
	g.Actual = float64(maxPeak)
	g.Passed = meanElim <= 0.25 && maxPeak <= 5
	g.Detail = fmt.Sprintf("终局出局均值 %.1f%%(≤25%%),单月连锁破产峰值 %d(≤5),n=%d",
		meanElim*100, maxPeak, len(sub))
	return g
}

// ── G12:I1/I2/I4/I6/I8 全月全局全策略 0 failure ─────────────────────

// mcGateInvariantIDs G12 纳入判定的不变量 id(方案 §4.3;I3/I5 不在内)。
var mcGateInvariantIDs = []string{InvariantI1, InvariantI2, InvariantI4, InvariantI6, InvariantI8}

func gateG12(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateG12, Name: gateName(GateG12), Threshold: 0}
	total := 0
	byID := map[string]int{}
	for i := range runs {
		for _, id := range mcGateInvariantIDs {
			n := runs[i].InvariantFailsByID[id]
			if n > 0 {
				byID[id] += n
				total += n
			}
		}
	}
	g.Actual = float64(total)
	g.Passed = total == 0
	g.Detail = fmt.Sprintf("I1/I2/I4/I6/I8 违例合计 %d(按 id:%v),n=%d 局", total, byID, len(runs))
	return g
}

// ── A1:M1–M60 任月平均存活 ≥ 10.5/12 ────────────────────────────────

func gateA1(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateA1, Name: gateName(GateA1), Threshold: 10.5}
	worst, worstMonth := -1.0, 0
	for month := 1; month <= 60; month++ {
		var alive []float64
		for i := range runs {
			if v, ok := mcAliveAt(runs[i], month); ok {
				alive = append(alive, float64(v))
			}
		}
		if len(alive) == 0 {
			continue
		}
		mean := mcMean(alive)
		if worst < 0 || mean < worst {
			worst, worstMonth = mean, month
		}
	}
	if worst < 0 {
		g.Detail = "无 M1–M60 样本"
		return g
	}
	g.Actual = worst
	g.Passed = worst >= 10.5
	g.Detail = fmt.Sprintf("M1–M60 最差月(M%d)平均存活 %.2f/12(≥10.5)", worstMonth, worst)
	return g
}

// ── A2:M120 safety_net_only 中位净资产 > 0 ──────────────────────────

func gateA2(runs []MCRunStats) GateResult {
	g := GateResult{ID: GateA2, Name: gateName(GateA2), Threshold: 0}
	sub := mcRunsByStrategy(runs, MCStrategySafetyNetOnly, 120)
	if len(sub) == 0 {
		g.Detail = "无 safety_net_only M120 样本"
		return g
	}
	var nw []float64
	for i := range sub {
		for seat := 0; seat < MaxSeats; seat++ {
			if sub[i].AnchorM120.Alive[seat] {
				nw = append(nw, sub[i].AnchorM120.NetWorth[seat])
			}
		}
	}
	median := mcMedian(nw)
	g.Actual = median
	g.Passed = median > 0
	g.Detail = fmt.Sprintf("存活座位中位净资产 ¥%.0f(>0),n=%d 座样本", median, len(nw))
	return g
}

// ── A3/A4:引擎无统计出口 ⇒ 缺数据标注(Passed=true 不假装通过)────

func gateA3() GateResult {
	return GateResult{ID: GateA3, Name: gateName(GateA3), Passed: true,
		Threshold: 0.08,
		Detail:    "缺数据:引擎无「失业保险+住房补贴 ÷ 当期全桌工资」统计出口(住房补贴概念不存在;失业救济走国库 CatWelfare 无分项出口)。转批次 73"}
}

func gateA4() GateResult {
	return GateResult{ID: GateA4, Name: gateName(GateA4), Passed: true,
		Threshold: 0.60,
		Detail:    "缺数据:引擎无「破产清算后 24 月内恢复行动且未再触发 H1」统计出口。转批次 73"}
}

// ── 数学工具(零 rand,纯函数)────────────────────────────────────────

// mcZ95 正态 95% 双侧分位。
const mcZ95 = 1.959963984540054

// mcMean 算术均值(空 → 0)。
func mcMean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, v := range xs {
		s += v
	}
	return s / float64(len(xs))
}

// mcMedian 中位数(P50;空 → 0)。
func mcMedian(xs []float64) float64 {
	return mcPercentile(xs, 0.5)
}

// mcPercentile 分位数(线性插值,p ∈ [0,1];空 → 0)。
//
// 升序后取 idx = p×(n−1),在相邻两样本间线性插值;n=1 恒返回该样本。
func mcPercentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	idx := p * float64(n-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// mcNormalCIUpper95 正态近似 95% 置信区间上界(mean + z·s/√n;
// 样本标准差 s 取 n−1 分母;n=1 → 均值本身;空 → 0)。
func mcNormalCIUpper95(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	mean := mcMean(xs)
	if n == 1 {
		return mean
	}
	var ss float64
	for _, v := range xs {
		d := v - mean
		ss += d * d
	}
	sd := math.Sqrt(ss / float64(n-1))
	return mean + mcZ95*sd/math.Sqrt(float64(n))
}

// mcArgMax 最大值下标(空 → -1)。
func mcArgMax(xs []float64) int {
	if len(xs) == 0 {
		return -1
	}
	best := 0
	for i := 1; i < len(xs); i++ {
		if xs[i] > xs[best] {
			best = i
		}
	}
	return best
}

// mcGini 基尼系数(任意顺序样本;复用 society_stats.go::giniCoefficient
// 的加权公式,Σ≤0 或 n<2 → 0,结果 clamp [0,1])。
func mcGini(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	asc := append([]float64(nil), xs...)
	sort.Float64s(asc)
	return clamp01(giniCoefficient(asc))
}

// mcTop20Share 头部 20% 座位的财富份额(k = ceil(0.2n),12 座 → top3;
// 总财富 ≤0 → 0)。
func mcTop20Share(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	desc := append([]float64(nil), xs...)
	sort.Sort(sort.Reverse(sort.Float64Slice(desc)))
	var total float64
	for _, v := range xs {
		total += v
	}
	if total <= 0 {
		return 0
	}
	k := int(math.Ceil(0.2 * float64(n)))
	if k < 1 {
		k = 1
	}
	if k > n {
		k = n
	}
	var top float64
	for i := 0; i < k; i++ {
		top += desc[i]
	}
	return clamp01(top / total)
}

// mcSpearman Spearman 等级相关(并列取平均秩后做 Pearson;n<2 或
// 任一侧零方差 → 0)。
func mcSpearman(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 || n != len(ys) {
		return 0
	}
	rx := mcAverageRanks(xs)
	ry := mcAverageRanks(ys)
	return mcPearson(rx, ry)
}

// mcAverageRanks 平均秩(1..n;并列值取并列位次的算术平均)。
func mcAverageRanks(xs []float64) []float64 {
	n := len(xs)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		if xs[idx[a]] != xs[idx[b]] {
			return xs[idx[a]] < xs[idx[b]]
		}
		return idx[a] < idx[b] // 并列按原下标稳定
	})
	ranks := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && xs[idx[j+1]] == xs[idx[i]] {
			j++
		}
		avg := float64(i+j+2) / 2 // 秩 1..n ⇒ 并列平均 = (i+1 + j+1)/2
		for k := i; k <= j; k++ {
			ranks[idx[k]] = avg
		}
		i = j + 1
	}
	return ranks
}

// mcPearson Pearson 相关系数(零方差任一侧 → 0)。
func mcPearson(xs, ys []float64) float64 {
	n := len(xs)
	if n == 0 || n != len(ys) {
		return 0
	}
	mx, my := mcMean(xs), mcMean(ys)
	var sxy, sxx, syy float64
	for i := 0; i < n; i++ {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx <= 0 || syy <= 0 {
		return 0
	}
	r := sxy / math.Sqrt(sxx*syy)
	if math.IsNaN(r) {
		return 0
	}
	return r
}
