// Package virtual_city — mc_ab.go: A/B 成对对照 harness(批次73 O5,
// 框架 §6.3)+ MCOverride 参数覆盖。
//
// 硬纪律(方案 §3):
//   - **MCOverride 只允许常量类参数,禁止触碰 rand 序列**(§3-1/§3-4):
//     初始现金缩放(世界构建后、StartGame 前,零 rand)、股票景气期月收益率
//     与衰退期最短月数(市场相位参数的「覆盖值优先于 const」最小侵入读取);
//   - `MCRunOneWith(seed, strategy, nil)` 与 `MCRunOne(seed, strategy)`
//     **逐字段相等**(T-AB1 钉死)⇒ 默认路径零行为差异、零 rand 序列移动;
//   - MCCompare 同策略同种子带双侧各跑 n 局 ⇒ 配对样本;主指标 D5 破产率
//     (χ²)+ D6 品类年化均值(Welch,stock 品类);护栏 D2 越界 ⇒
//     guard_breach 不判胜负;n<20 ⇒ underpowered(§6.3 最小样本 20)。
package virtual_city

// ── MCOverride(§8.2;全部指针,nil = 引擎默认)──────────────────────────

// MCOverride 单参数覆盖。只允许常量类参数,禁止向世界注入新 rand 消费。
type MCOverride struct {
	// InitialCashMul §6.2 行2:初始现金 ±20%(乘数;世界构建后、StartGame
	// 前按乘数缩放 p.Cash 与 p.Card.Savings ⇒ Ledger 初始注入同比例缩放,
	// I1/I2 守恒保持)。
	InitialCashMul *float64
	// StockBoomMonthly §6.2 行1:股票景气期月收益率(现引擎
	// PhaseTable[boom].StockAnnual/12 = 0.30/12 ≈ 0.025;《总体设计》§8.3
	// 复苏期 +10% 为年化口径,扫描档距 5%/10%/15%/20% 按年化 ÷12 落地)。
	StockBoomMonthly *float64
	// RecessionMinMonths §6.2 行3:衰退期最短持续月数(现引擎
	// phaseDurationMonths = U(24,48) = 2–4 年;覆盖值 = 进入衰退相位时
	// 时长的下限 clamp)。
	RecessionMinMonths *int
}

// apply 把覆盖写到世界/市场(零 rand;nil 字段不动 ⇒ 默认路径零差异)。
// 生效点(最小侵入读取,market.go / montecarlo.go 消费):
//   - InitialCashMul:MCRunOneWith 在 NewWorld 后、StartGame 前缩放;
//   - StockBoomMonthly:MarketState.MonthStep 繁荣相位漂移项「覆盖值优先
//     于 PhaseTable const」;
//   - RecessionMinMonths:MarketState.RerollPhase 进入衰退时
//     「max(覆盖值, 随机时长)」。
func (ov *MCOverride) apply(w *World) {
	if w == nil || ov == nil {
		return
	}
	if ov.InitialCashMul != nil && *ov.InitialCashMul > 0 && *ov.InitialCashMul != 1 {
		mul := *ov.InitialCashMul
		for _, p := range w.Players {
			if p == nil {
				continue
			}
			p.Cash = int64(float64(p.Cash) * mul)
			p.Card.Savings = int64(float64(p.Card.Savings) * mul)
		}
	}
	if w.Market != nil {
		if ov.StockBoomMonthly != nil {
			w.Market.BoomStockMonthlyDrift = ov.StockBoomMonthly
		}
		if ov.RecessionMinMonths != nil {
			w.Market.RecessionMinMonths = ov.RecessionMinMonths
		}
	}
}

// ── ABReport(§8.2;JSON tags 供批次74 调参决策与报告落盘)───────────────

// ABVerdict 判定结论 id。
const (
	ABVerdictSignificantBetter = "significant_better" // 变体显著更优(D5 降)
	ABVerdictSignificantWorse  = "significant_worse"  // 变体显著更差(D5 升)
	ABVerdictNoDifference      = "no_difference"      // 无显著差异
	ABVerdictGuardBreach       = "guard_breach"       // 护栏越界(D2),不判胜负
)

// ABReport 成对对照报告。
type ABReport struct {
	Strategy string `json:"strategy"`
	N        int    `json:"n"`

	// D5 破产率(χ² 两比例)。
	BaseD5   float64 `json:"base_d5"`
	VariantD5 float64 `json:"variant_d5"`
	DeltaD5  float64 `json:"delta_d5"` // VariantD5 − BaseD5(负 = 变好)
	Chi2     float64 `json:"chi2"`
	Chi2P    float64 `json:"chi2_p"`

	// D6 品类年化均值(stock;Welch t)。
	BaseD6    float64 `json:"base_d6"`
	VariantD6 float64 `json:"variant_d6"`
	DeltaD6   float64 `json:"delta_d6"`
	T         float64 `json:"t"`
	Df        float64 `json:"df"`
	TP        float64 `json:"t_p"`

	GuardBreach  bool   `json:"guard_breach"`
	Underpowered bool   `json:"underpowered"`
	Verdict      string `json:"verdict"`
}

// MCCompare 同策略同种子带双侧各跑 n 局(§8.2 配对 harness):
//
//   - 种子带 = MCSeedFor(strategy, 0..n−1) 前 n 个;
//   - 主指标 D5(每局终局正式出局率)与 D6(stock 品类年化,月数<12 缺省);
//   - 护栏 D2:任一侧局均 D2 > 7 或 < 2 ⇒ guard_breach(不判胜负);
//   - n<20 ⇒ underpowered(仍出报告);n≤0 ⇒ 空报告(verdict=no_difference)。
//
// 零 rand 新消费:全部确定性复用 MCRunOneWith。
func MCCompare(strategy string, base, variant MCOverride, n int) ABReport {
	rep := ABReport{Strategy: strategy, N: n, Verdict: ABVerdictNoDifference}
	if n <= 0 || mcStrategyIndex(strategy) < 0 {
		rep.N = 0 // 空报告(未跑任何局)
		return rep
	}
	baseD5 := make([]float64, 0, n)
	varD5 := make([]float64, 0, n)
	baseD6 := make([]float64, 0, n)
	varD6 := make([]float64, 0, n)
	baseD2 := make([]float64, 0, n)
	varD2 := make([]float64, 0, n)
	baseBankrupt := 0
	varBankrupt := 0
	for i := 0; i < n; i++ {
		seed := MCSeedFor(strategy, i)
		b := MCRunOneWith(seed, strategy, &base)
		v := MCRunOneWith(seed, strategy, &variant)
		if d5, ok := mcFinalRate(b.EliminatedByMonth, MaxSeats); ok {
			baseD5 = append(baseD5, d5)
			if d5 > 0 {
				baseBankrupt++
			}
		}
		if d5, ok := mcFinalRate(v.EliminatedByMonth, MaxSeats); ok {
			varD5 = append(varD5, d5)
			if d5 > 0 {
				varBankrupt++
			}
		}
		if r, ok := b.CategoryAnnualized[AssetStockIndex]; ok {
			baseD6 = append(baseD6, r)
		}
		if r, ok := v.CategoryAnnualized[AssetStockIndex]; ok {
			varD6 = append(varD6, r)
		}
		if d2, ok := mcFinalRateF(b.D2ByMonth); ok {
			baseD2 = append(baseD2, d2)
		}
		if d2, ok := mcFinalRateF(v.D2ByMonth); ok {
			varD2 = append(varD2, d2)
		}
	}
	rep.BaseD5 = mcMean(baseD5)
	rep.VariantD5 = mcMean(varD5)
	rep.DeltaD5 = rep.VariantD5 - rep.BaseD5
	rep.Chi2, rep.Chi2P = ChiSquareTwoProp(baseBankrupt, len(baseD5), varBankrupt, len(varD5))
	rep.BaseD6 = mcMean(baseD6)
	rep.VariantD6 = mcMean(varD6)
	rep.DeltaD6 = rep.VariantD6 - rep.BaseD6
	rep.T, rep.Df, rep.TP = WelchTTest(baseD6, varD6)

	// 护栏 D2(红线 R-E3/R-E4 阈值 7.0 / 2.0);无样本侧不判。
	if len(baseD2) > 0 {
		if m := mcMean(baseD2); m > redlineMoneyExpansionMax || m < redlineMoneyExpansionMin {
			rep.GuardBreach = true
		}
	}
	if len(varD2) > 0 {
		if m := mcMean(varD2); m > redlineMoneyExpansionMax || m < redlineMoneyExpansionMin {
			rep.GuardBreach = true
		}
	}
	rep.Underpowered = n < 20

	switch {
	case rep.GuardBreach:
		rep.Verdict = ABVerdictGuardBreach
	case rep.Chi2P < abAlpha && rep.DeltaD5 < 0:
		rep.Verdict = ABVerdictSignificantBetter
	case rep.Chi2P < abAlpha && rep.DeltaD5 > 0:
		rep.Verdict = ABVerdictSignificantWorse
	default:
		rep.Verdict = ABVerdictNoDifference
	}
	return rep
}

// mcFinalRate 序列终值 ÷ denom(空序列 ⇒ ok=false)。
func mcFinalRate(xs []int, denom int) (float64, bool) {
	if len(xs) == 0 || denom <= 0 {
		return 0, false
	}
	return float64(xs[len(xs)-1]) / float64(denom), true
}

// mcFinalRateF float64 序列终值(空 ⇒ ok=false)。
func mcFinalRateF(xs []float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	return xs[len(xs)-1], true
}
