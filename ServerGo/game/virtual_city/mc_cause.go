// Package virtual_city — mc_cause.go: 结局成因 8 类分类器(批次73 O3,
// 框架 §4.4)。对每座位输出单主因,确定性优先级固定:
//
//	良好结局(winner/affluent):
//	  POS-1  passive_income ≥ expense 且 FI ≥ 1        (FIRE 正解,最先判)
//	  POS-4  被动收入中合伙/分红/副业占比 > 50%
//	  POS-2  已实现价差盈利 > 净资产增量的 50%
//	  POS-3  其余良好结局(兜底归周期/择时)
//	不良结局(出局/破产;破产座位用破产时刻快照判,见 mcClassifyBankrupt):
//	  NEG-1  破产月存续月供 > 非债务支出 × 1.5        (高杠杆爆仓,最先判)
//	  NEG-2  单一品类持仓回撤 > 50% 且该品类占净资产 > 50%
//	  NEG-4  破产月非现金资产 > 0 且现金 < 月支出        (流动性枯竭)
//	  NEG-3  BiasHits > 0                               (偏差致损,最后判)
//	中立/未终局/unclassified:
//	  undetermined                                      (兜底 + 计数,绝不空白)
//
// ⚠ 破产侧输入时点(批次73 修正):NEG-1/2/4 对**破产座位**一律使用
//「破产时刻上下文快照」(Player.Bankrupt*;liquidate 清减贷款/资产**之前**
// captureBankruptSnapshot 采样,首次破产才写)——清算会清掉贷款/资产,用
// 终局期末状态判 NEG 会恒落空(gate 千局实测 NEG-1/2/4 全 0 的根因)。
// 未破产的不良结局(如意外身故)回退期末状态规则(下方 mcClassifySeat
// 回退分支)再 undetermined。
//
// 硬纪律(方案 §3):零 rand、零账目语义改动、采集失败静默降级为
// undetermined / 空聚合。
//
// ⚠ 引擎口径代理(实现裁决,实施记录同步):
//   - POS-4:引擎无合伙/分红科目 ⇒ 以 副业收入 ÷ (被动收入 + 副业收入) > 0.5
//     代理「合伙/分红/副业占比」;
//   - POS-2:净资产增量 = 期末净资产 − 卡面初始储蓄(期初口径);
//   - NEG 快照口径:非现金资产 = recomputeNetWorth − Cash;回撤 = 1 − 市值/
//     买入成本(成本基准代理,引擎无逐品类历史峰值);
//   - NEG-4:破产清算会清空资产 ⇒ 快照口径主要捕获清算前仍有资产且
//     现金断流的路径。
package virtual_city

// 结局成因 id 常量(8 类 + 兜底;聚合直方图键)。
const (
	CausePOS1 = "POS-1" // FIRE 正解
	CausePOS2 = "POS-2" // 价差盈利主导
	CausePOS3 = "POS-3" // 周期/择时(良好兜底)
	CausePOS4 = "POS-4" // 合伙/分红/副业主导
	CauseNEG1 = "NEG-1" // 高杠杆爆仓
	CauseNEG2 = "NEG-2" // 单一资产崩盘
	CauseNEG3 = "NEG-3" // 偏差致损
	CauseNEG4 = "NEG-4" // 流动性枯竭
	// CauseUndetermined 兜底桶(绝不空白;含中立结局与未终局座位)。
	CauseUndetermined = "undetermined"
)

// captureBankruptSnapshot 破产时刻上下文快照(liquidate 清减**之前**调用;
// 首次破产才写,BankruptMonth>0 时不覆盖)。结局成因 NEG-1/2/4 输入。
//
// 口径:
//   - 非现金资产 = recomputeNetWorth − Cash(养老金 + 资产市值 − 贷款余额);
//   - TopCat = 市值最大的持仓品类;Share = 其市值 ÷ 净资产(NW ≤0 且
//     市值 >0 ⇒ 1,负净资产下「单一品类主导」语义);DD(回撤) =
//     1 − 市值/买入成本(成本 ≤0 ⇒ 0;引擎无逐品类历史峰值,以成本基准
//     代理「最大缩水」,注释标注)。
//
// 零 rand、零账目语义、不参与 StateHash。
func (w *World) captureBankruptSnapshot(p *Player) {
	if w == nil || p == nil || p.BankruptMonth > 0 {
		return
	}
	p.BankruptMonth = w.Month
	var paymentSum int64
	for i := range p.Loans {
		paymentSum += p.Loans[i].MonthlyPayment
	}
	p.BankruptLoanPayment = paymentSum
	p.BankruptExpense = p.Monthly.Expense
	p.BankruptCash = p.Cash
	nw := recomputeNetWorth(p, w.Market)
	p.BankruptNonCash = nw - p.Cash
	// 最大单一品类(市值口径)。
	value := map[string]int64{}
	cost := map[string]int64{}
	for i := range p.Assets {
		a := &p.Assets[i]
		cat := mcAssetCategory(a.Kind)
		if cat == "" {
			continue
		}
		value[cat] += AssetValue(a, w.Market)
		cost[cat] += a.CostCNY
	}
	topCat, topValue := "", int64(0)
	for cat, v := range value {
		if v > topValue {
			topCat, topValue = cat, v
		}
	}
	if topCat != "" {
		if nw > 0 {
			p.BankruptTopCatShare = clamp01(float64(topValue) / float64(nw))
		} else if topValue > 0 {
			p.BankruptTopCatShare = 1 // 负净资产且仍有持仓 ⇒ 单一品类主导语义
		}
		if c := cost[topCat]; c > 0 {
			p.BankruptTopCatDD = clamp01(1 - float64(topValue)/float64(c))
		}
	}
}

// mcAssetCategory 资产 kind → 8 类品类 id(空 = 不归类)。
func mcAssetCategory(kind string) string {
	switch {
	case kind == AssetStockIndex:
		return "stock"
	case kind == AssetBond:
		return "bond"
	case kind == AssetGold:
		return "gold"
	case isHouseKind(kind):
		return "house"
	case isShopKind(kind):
		return "shop"
	case kind == AssetSideBusiness:
		return "side_business"
	case kind == AssetPension:
		return "pension"
	}
	return ""
}

// mcClassifySeat 单座位结局成因分类(纯函数;优先级 1→4→2→3 / 1→2→4→3)。
//
//   - good:该座位是否良好结局(outcome winner/affluent,与 OutcomeStat.Good
//     同口径,由调用方经 outcomes 对齐);
//   - rec:居民级记录(mcCollectResidents 同源采集);
//   - bad:不良结局(出局/破产)标志;!good && !bad ⇒ 中立/未终局 ⇒ undetermined。
func mcClassifySeat(p *Player, m *MarketState, good, bad bool, rec MCResidentRecord) string {
	if p == nil {
		return CauseUndetermined
	}
	if good {
		// POS-1:FIRE 正解(最先判)。
		if rec.PassiveIncomeEnd >= rec.ExpenseEnd && rec.FIIndexEnd >= 1 {
			return CausePOS1
		}
		// POS-4:合伙/分红/副业占比 > 50%(副业收入代理口径)。
		denom := rec.PassiveIncomeEnd + p.Monthly.SideIncome
		if denom > 0 && float64(p.Monthly.SideIncome)/float64(denom) > 0.5 {
			return CausePOS4
		}
		// POS-2:已实现价差盈利 > 净资产增量的 50%。
		deltaNW := rec.NetWorthEnd - p.Card.Savings
		if p.TradeRealizedCNY > 0 && (deltaNW <= 0 || p.TradeRealizedCNY > deltaNW/2) {
			return CausePOS2
		}
		// POS-3:其余良好结局(兜底归周期/择时)。
		return CausePOS3
	}
	if bad {
		// 批次73 修正:破产座位优先用**破产时刻快照**判 NEG(清算会清掉
		// 贷款/资产,期末状态恒落空);未破产的不良结局(如意外身故)回退
		// 期末状态规则再 undetermined。
		if p.BankruptMonth > 0 {
			return mcClassifyBankrupt(p)
		}
		// NEG-1:高杠杆爆仓(终局存续月供合计 > 最近月结总支出 × 1.5,最先判)。
		var paymentSum int64
		for i := range p.Loans {
			paymentSum += p.Loans[i].MonthlyPayment
		}
		expense := rec.ExpenseEnd
		if expense < 1 {
			expense = 1
		}
		if paymentSum > expense*3/2 {
			return CauseNEG1
		}
		// NEG-2:单一品类持仓回撤 > 50% 且占净资产 > 50%。
		if mcSingleCategoryCrash(p, m, rec.NetWorthEnd) {
			return CauseNEG2
		}
		// NEG-4:流动性枯竭(非现金资产 > 0 且现金 < 月支出)。
		if mcNonCashAssets(p, m) > 0 && p.Cash < rec.ExpenseEnd {
			return CauseNEG4
		}
		// NEG-3:偏差致损(最后判)。
		if p.BiasHits > 0 {
			return CauseNEG3
		}
		return CauseUndetermined
	}
	return CauseUndetermined
}

// mcClassifyBankrupt 破产时刻快照口径的 NEG 分类(优先级 1→2→4→3):
//
//	NEG-1  破产月存续月供合计 > 非债务支出 × 1.5    (高杠杆爆仓,最先判)
//	NEG-2  破产月单一品类占净资产 > 50% 且回撤 > 50%
//	NEG-4  破产月非现金资产 > 0 且现金 < 月支出     (流动性枯竭)
//	NEG-3  BiasHits > 0                          (偏差致损,最后判)
//
// ⚠ NEG-1 口径裁决(实施记录偏离):引擎月结总支出(settlement.go 步骤6,
// addExpense "贷款月供")**已含贷款月供自身**,字面「月供 > 当月总支出×1.5」
// 恒假(支出 ≥ 月供 ⇒ 矛盾式)⇒ 比较基准取**非债务支出**(总支出 − 月供,
// 下限 0),忠实框架「高杠杆爆仓 = 月供远超生活支出」语义。字段
// BankruptExpense 仍按协调口径存「最近月结总支出」原值。
//
// 快照在 liquidate 清减之前采样(见 captureBankruptSnapshot);均无特征 ⇒
// undetermined(不空白)。
func mcClassifyBankrupt(p *Player) string {
	nonDebtExpense := p.BankruptExpense - p.BankruptLoanPayment
	if nonDebtExpense < 0 {
		nonDebtExpense = 0
	}
	if p.BankruptLoanPayment > nonDebtExpense*3/2 {
		return CauseNEG1
	}
	if p.BankruptTopCatShare > 0.5 && p.BankruptTopCatDD > 0.5 {
		return CauseNEG2
	}
	if p.BankruptNonCash > 0 && p.BankruptCash < p.BankruptExpense {
		return CauseNEG4
	}
	if p.BiasHits > 0 {
		return CauseNEG3
	}
	return CauseUndetermined
}

// mcSingleCategoryCrash NEG-2 判定(期末回退路径):存在品类期末市值 <
// 累计成本 × 50%(回撤 > 50%)且该品类市值 > 净资产 × 50%(零 rand;
// 市值按 AssetValue 现算)。
func mcSingleCategoryCrash(p *Player, m *MarketState, netWorth int64) bool {
	if netWorth <= 0 {
		return false
	}
	value := map[string]int64{}
	cost := map[string]int64{}
	for i := range p.Assets {
		a := &p.Assets[i]
		cat := mcAssetCategory(a.Kind)
		if cat == "" {
			continue
		}
		value[cat] += AssetValue(a, m)
		cost[cat] += a.CostCNY
	}
	for cat, v := range value {
		if v > netWorth/2 && cost[cat] > 0 && v < cost[cat]/2 {
			return true
		}
	}
	return false
}

// mcNonCashAssets 非现金资产市值合计(AssetValue 口径;side_business /
// pension 残值恒 0;零 rand)。NEG-4 输入。
func mcNonCashAssets(p *Player, m *MarketState) int64 {
	var total int64
	for i := range p.Assets {
		total += AssetValue(&p.Assets[i], m)
	}
	return total
}

// mcCollectOutcomeCauses 整局结局成因直方图(8 类 + undetermined;
// RunRecord.outcome_causes 出口)。良好/不良标志经 outcomes 按座位对齐。
func mcCollectOutcomeCauses(w *World, residents []MCResidentRecord, outcomes []OutcomeStat) map[string]int {
	out := map[string]int{
		CausePOS1: 0, CausePOS2: 0, CausePOS3: 0, CausePOS4: 0,
		CauseNEG1: 0, CauseNEG2: 0, CauseNEG3: 0, CauseNEG4: 0,
		CauseUndetermined: 0,
	}
	if w == nil {
		return out
	}
	outcomeBySeat := make(map[int]OutcomeStat, len(outcomes))
	for _, o := range outcomes {
		outcomeBySeat[o.Seat] = o
	}
	for _, rec := range residents {
		p := w.Players[rec.Seat]
		if p == nil {
			out[CauseUndetermined]++
			continue
		}
		o := outcomeBySeat[rec.Seat]
		bad := !p.Alive || o.Outcome == OutcomeBankrupt || o.Outcome == OutcomeOut
		out[mcClassifySeat(p, w.Market, o.Good, bad, rec)]++
	}
	return out
}
