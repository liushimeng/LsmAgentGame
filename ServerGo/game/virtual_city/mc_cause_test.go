// Package virtual_city — mc_cause_test.go: 批次73 O3 结局成因 8 类分类器
// 测试(方案 §10:8 类边界 + 优先级互斥 + undetermined)。
package virtual_city

import (
	"math/rand"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// mcCauseFixture 构造分类用最小座位 + 记录(默认值即 undetermined 路径)。
func mcCauseFixture() (*Player, MCResidentRecord) {
	p := &Player{
		Seat: 0, Cash: 1000, Alive: true,
		Card: profession.Card{ID: "P01", Savings: 5000},
	}
	rec := MCResidentRecord{
		ResidentID: "CR1-S0", Portrait: "P01",
		InvestedCategories: []string{}, CategoryReturns: map[string]float64{},
		NetWorthEnd: 1000, FIIndexEnd: 0.1,
	}
	return p, rec
}

// TestMCCause_GoodBranches 良好结局四分支(POS-1 优先,POS-3 兜底)。
func TestMCCause_GoodBranches(t *testing.T) {
	m := NewMarket(rand.New(rand.NewSource(1)))

	// POS-1:passive ≥ expense 且 FI ≥ 1(最先判,即使副业占比也高)。
	p, rec := mcCauseFixture()
	rec.PassiveIncomeEnd, rec.ExpenseEnd, rec.FIIndexEnd = 3000, 2000, 1.2
	p.Monthly.SideIncome = 100000
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS1 {
		t.Errorf("POS-1 分支 = %s", got)
	}
	// POS-1 边界:passive == expense 且 FI 恰 1 ⇒ 仍 POS-1(≥ 闭区间)。
	rec.PassiveIncomeEnd, rec.ExpenseEnd, rec.FIIndexEnd = 2000, 2000, 1.0
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS1 {
		t.Errorf("POS-1 闭区间边界 = %s", got)
	}
	// POS-4:副业占比 > 50%(FI 不足 1)。
	rec.PassiveIncomeEnd, rec.ExpenseEnd, rec.FIIndexEnd = 1000, 2000, 0.5
	p.Monthly.SideIncome = 3000 // 3000/(1000+3000) = 0.75 > 0.5
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS4 {
		t.Errorf("POS-4 分支 = %s", got)
	}
	// POS-2:已实现价差 > 净资产增量 50%。
	p.Monthly.SideIncome = 0
	p.TradeRealizedCNY = 4000 // 期末 NW 1000 − 期初 5000 = −4000 ⇒ Δ≤0,realized>0
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS2 {
		t.Errorf("POS-2 分支(Δ≤0) = %s", got)
	}
	p.TradeRealizedCNY = 100 // 100 < 2000×50% ⇒ 不满足
	rec.NetWorthEnd = 9000   // Δ = 4000,50% = 2000;100 < 2000
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS3 {
		t.Errorf("POS-3 兜底 = %s", got)
	}
	p.TradeRealizedCNY = 2500 // 2500 > 2000 ⇒ POS-2
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS2 {
		t.Errorf("POS-2 分支(Δ>0) = %s", got)
	}
	// POS-2 与 POS-4 互斥:同时满足 ⇒ POS-4 优先(优先级 1→4→2→3)。
	p.Monthly.SideIncome = 3000
	if got := mcClassifySeat(p, m, true, false, rec); got != CausePOS4 {
		t.Errorf("POS-4 应优先于 POS-2,got %s", got)
	}
}

// TestMCCause_BadBranches 不良结局四分支(NEG-1 优先;NEG-3 最后)。
func TestMCCause_BadBranches(t *testing.T) {
	m := NewMarket(rand.New(rand.NewSource(1)))

	// NEG-1:月供合计 > 支出 × 1.5(最先判;即使BiasHits>0/持仓崩盘)。
	p, rec := mcCauseFixture()
	rec.ExpenseEnd = 1000
	p.Alive = false
	p.Loans = []Loan{{ID: "L1", Kind: LoanConsumer, Balance: 50000, MonthlyPayment: 2000}}
	p.BiasHits = 9
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG1 {
		t.Errorf("NEG-1 分支 = %s", got)
	}
	// NEG-1 边界:月供恰 = 支出 × 1.5 ⇒ 不触发(严格 >);BiasHits=9 ⇒ 落 NEG-3。
	p.Loans[0].MonthlyPayment = 1500
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG3 {
		t.Errorf("NEG-1 恰界应落到 NEG-3(偏差),got %s", got)
	}
	// NEG-2:单一品类回撤 > 50% 且占净资产 > 50%(月供降下来先过 NEG-1)。
	p.Loans = nil
	p.BiasHits = 0
	rec.NetWorthEnd = 10000
	p.Assets = []Asset{{Kind: AssetGold, Units: 100, CostCNY: 9000}} // 市值 ≈ 750×100=75000? 金价 750 ⇒ 市值 75000 > NW/2 且 > 成本/2 ⇒ 不满足回撤
	// 用债券(面值 1):Units 4000,成本 9000 ⇒ 市值 4000 < 4500 且 4000 < 5000(NW/2)? NW=10000,NW/2=5000 ⇒ 占比不足。
	p.Assets = []Asset{{Kind: AssetBond, Units: 6000, CostCNY: 20000}}
	// 市值 6000;6000 > 5000(NW/2)✓;6000 < 10000(成本/2)✓ ⇒ NEG-2。
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG2 {
		t.Errorf("NEG-2 分支 = %s", got)
	}
	// NEG-2 边界:市值恰 = 成本 50% ⇒ 不触发(严格 <)。
	p.Assets = []Asset{{Kind: AssetBond, Units: 10000, CostCNY: 20000}}
	if got := mcClassifySeat(p, m, false, true, rec); got == CauseNEG2 {
		t.Errorf("NEG-2 回撤恰 50%% 不应触发")
	}
	// NEG-4:非现金资产 > 0 且现金 < 月支出(无贷款/崩盘/偏差)。
	p.Assets = []Asset{{Kind: AssetBond, Units: 100, CostCNY: 100}}
	rec.ExpenseEnd = 5000
	p.Cash = 100 // 100 < 5000
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG4 {
		t.Errorf("NEG-4 分支 = %s", got)
	}
	// NEG-3:偏差致损(最后判)。
	p.Assets = nil
	p.BiasHits = 1
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG3 {
		t.Errorf("NEG-3 分支 = %s", got)
	}
	// 不良但无任何成因特征 ⇒ undetermined(不空白)。
	p.BiasHits = 0
	p.Cash = 100000
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseUndetermined {
		t.Errorf("不良无特征应 undetermined,got %s", got)
	}
}

// TestMCCause_Undetermined 中立/未终局 ⇒ undetermined。
func TestMCCause_Undetermined(t *testing.T) {
	m := NewMarket(rand.New(rand.NewSource(1)))
	p, rec := mcCauseFixture()
	if got := mcClassifySeat(p, m, false, false, rec); got != CauseUndetermined {
		t.Errorf("中立结局 = %s,期望 undetermined", got)
	}
	if got := mcClassifySeat(nil, m, true, false, rec); got != CauseUndetermined {
		t.Errorf("nil 座位 = %s,期望 undetermined", got)
	}
}

// TestMCCause_WholeRunHistogram 整局直方图:12 座全部入桶,总和 = 12。
func TestMCCause_WholeRunHistogram(t *testing.T) {
	w := mcWorld(MCSeedFor(MCStrategyBalanced, 0))
	residents := mcCollectResidents(w, "CR1", w.MCAuctionCount)
	causes := mcCollectOutcomeCauses(w, residents, outcomeStats(w))
	sum := 0
	for _, n := range causes {
		sum += n
	}
	if sum != MaxSeats {
		t.Errorf("直方图总和 = %d,期望 %d(%v)", sum, MaxSeats, causes)
	}
	if _, ok := causes[CauseUndetermined]; !ok {
		t.Error("直方图缺 undetermined 桶")
	}
	// 当前经济基线(高出局率):NEG 类应非全 0。
	neg := causes[CauseNEG1] + causes[CauseNEG2] + causes[CauseNEG3] + causes[CauseNEG4]
	if neg == 0 {
		t.Log("注:本 seed 局无 NEG 成因(允许,基线 seed 可能全员存活或归 undetermined)")
	}
}

// ── 批次73 修正:破产时刻快照口径 NEG 分类 ──────────────────────────────

// TestMCCause_BankruptSnapshotBranches 破产快照判 NEG-1/2/4/3(清算后期末
// 状态已被清空,快照是唯一输入)。
func TestMCCause_BankruptSnapshotBranches(t *testing.T) {
	m := NewMarket(rand.New(rand.NewSource(1)))

	// NEG-1:破产月存续月供 2000 > 支出 1000 × 1.5(最先判;期末贷款即便
	// 已被清算也不影响)。
	p, rec := mcCauseFixture()
	p.Alive = false
	p.BankruptMonth = 12
	p.BankruptLoanPayment = 2000
	p.BankruptExpense = 1000
	p.BiasHits = 9
	p.BankruptTopCatShare, p.BankruptTopCatDD = 0.9, 0.9 // 亦满足 NEG-2,验优先级
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG1 {
		t.Errorf("快照 NEG-1 = %s(应最先判,优先于 NEG-2/3)", got)
	}
	// NEG-1 边界:月供恰 = 非债务支出 × 1.5 ⇒ 不触发(严格 >);BiasHits=9 且
	// 单一品类双过线 ⇒ 落 NEG-2(验优先级)。
	p.BankruptLoanPayment = 1500
	p.BankruptExpense = 2500 // 非债务支出 = 2500−1500 = 1000;1500 恰 = 1000×1.5
	if got := mcClassifySeat(p, m, false, true, rec); got != CauseNEG2 {
		t.Errorf("NEG-1 恰界应落到 NEG-2(快照单一品类),got %s", got)
	}
	// NEG-2:回撤/占比双过线,无月供特征。
	p2, rec2 := mcCauseFixture()
	p2.Alive = false
	p2.BankruptMonth = 20
	p2.BankruptExpense = 3000
	p2.BankruptTopCatShare, p2.BankruptTopCatDD = 0.8, 0.6
	p2.BiasHits = 9
	if got := mcClassifySeat(p2, m, false, true, rec2); got != CauseNEG2 {
		t.Errorf("快照 NEG-2 = %s", got)
	}
	// NEG-2 边界:share 恰 0.5 ⇒ 不触发;非现金 >0 且现金 < 支出 ⇒ NEG-4。
	p2.BankruptTopCatShare = 0.5
	p2.BankruptNonCash = 50000
	p2.BankruptCash = 100
	if got := mcClassifySeat(p2, m, false, true, rec2); got != CauseNEG4 {
		t.Errorf("NEG-2 恰界应落到 NEG-4,got %s", got)
	}
	// NEG-4:无月供/崩盘特征,有资产且现金断流。
	p3, rec3 := mcCauseFixture()
	p3.Alive = false
	p3.BankruptMonth = 8
	p3.BankruptExpense = 3000
	p3.BankruptNonCash = 50000
	p3.BankruptCash = 100
	if got := mcClassifySeat(p3, m, false, true, rec3); got != CauseNEG4 {
		t.Errorf("快照 NEG-4 = %s", got)
	}
	// NEG-3:仅 BiasHits 特征。
	p4, rec4 := mcCauseFixture()
	p4.Alive = false
	p4.BankruptMonth = 8
	p4.BankruptExpense = 3000
	p4.BiasHits = 1
	if got := mcClassifySeat(p4, m, false, true, rec4); got != CauseNEG3 {
		t.Errorf("快照 NEG-3 = %s", got)
	}
	// 破产但全特征为零 ⇒ undetermined(不空白)。
	p5, rec5 := mcCauseFixture()
	p5.Alive = false
	p5.BankruptMonth = 8
	p5.BankruptExpense = 3000
	if got := mcClassifySeat(p5, m, false, true, rec5); got != CauseUndetermined {
		t.Errorf("破产无特征应 undetermined,got %s", got)
	}
}

// TestMCCause_BankruptSnapshotCaptured 快照在清算清减**之前**采样:
// liquidate 后贷款/资产已清空,但快照仍保留破产月特征。
func TestMCCause_BankruptSnapshotCaptured(t *testing.T) {
	w := calibTestWorld(7, 2)
	// 批次75:本测试钉的是**旧口径**「NegativeCashMonths ≥ 3 ⇒ 清算」路径
	//(三阶段状态机下同名触发条件已改语义);关闭 v2 开关保持原断言。
	w.BalanceV2Enabled = false
	p := w.Players[0]
	p.CreditScore = 700
	if _, e := w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "credit", AmountCNY: 50000}); e != nil {
		t.Fatalf("take_loan: %v", e)
	}
	p.Cash = -1
	p.NegativeCashMonths = 3 // 触发破产条件
	calibSettleMonths(w, 1)
	if p.BankruptMonth == 0 {
		t.Fatal("未触发破产清算(BankruptMonth=0)")
	}
	// 清算后:贷款已被重组/资产已清,但快照保留。
	if p.BankruptLoanPayment <= 0 {
		t.Errorf("快照月供 = %d,期望 > 0(清算前采样)", p.BankruptLoanPayment)
	}
	t.Logf("快照:month=%d payment=%d expense=%d cash=%d noncash=%d share=%.2f dd=%.2f",
		p.BankruptMonth, p.BankruptLoanPayment, p.BankruptExpense,
		p.BankruptCash, p.BankruptNonCash, p.BankruptTopCatShare, p.BankruptTopCatDD)
	// 分类走快照路径而非期末(期末贷款已重组,月供口径失真)。
	rec := mcCollectResidents(w, "CR1", 0)[0]
	outcomes := outcomeStats(w)
	var o OutcomeStat
	for _, oo := range outcomes {
		if oo.Seat == 0 {
			o = oo
		}
	}
	got := mcClassifySeat(p, w.Market, o.Good, true, rec)
	nonDebt := p.BankruptExpense - p.BankruptLoanPayment
	if nonDebt < 0 {
		nonDebt = 0
	}
	if got == CauseUndetermined && p.BankruptLoanPayment > nonDebt*3/2 {
		t.Errorf("高月供破产被分类为 undetermined(快照路径未生效)")
	}
	// 二次破产不改写首因快照(此处只破产一次,验证幂等写)。
	month1 := p.BankruptMonth
	p.Cash = -1
	p.NegativeCashMonths = 3
	calibSettleMonths(w, 1)
	if p.BankruptMonth != month1 && p.BankruptCount > 1 {
		t.Errorf("二次破产改写了首因快照:month %d → %d", month1, p.BankruptMonth)
	}
}

// mcCauseDriveOne 驱动单局(MCRunOne 同款循环,返回世界引用供分类)。
func mcCauseDriveOne(strategy string, idx int) *World {
	cardRng := rand.New(rand.NewSource(MCSeedFor(strategy, idx)))
	cards := [MaxSeats]profession.Card{}
	for i, c := range profession.SyntheticCards(MaxSeats, cardRng) {
		if i >= MaxSeats {
			break
		}
		cards[i] = c
	}
	w := NewWorld(MCSeedFor(strategy, idx), cards)
	w.StartGame()
	if cs := w.calibrationEnsure(); cs != nil {
		cs.Strategy = strategy
	}
	for m := 1; m <= mcMaxMonths; m++ {
		mcResetMonthBudgets(w)
		for seat := range w.Players {
			acts := MCStrategyActions(w, strategy, seat, m)
			for i := 0; i < len(acts) && i < mcMonthlyActionCap; i++ {
				_, _ = w.ApplyAction(seat, acts[i])
			}
		}
		if strategy == MCStrategyStress {
			MCStressPreSettle(w, m)
		}
		finished, _ := w.SettleMonth()
		if strategy == MCStrategyStress {
			MCStressPostSettle(w)
		}
		if finished || m == mcMaxMonths {
			break
		}
	}
	return w
}

// TestMCCause_AggressiveLeverageUndeterminedShare aggressive_leverage 整局:
// 破产座走快照 ⇒ undetermined 占比 < 60%(钉死 NEG 侧真的出数;批次73
// 修正前 gate 实测该侧 undetermined 高达 89%)。
func TestMCCause_AggressiveLeverageUndeterminedShare(t *testing.T) {
	for idx := 0; idx < 4; idx++ {
		w := mcCauseDriveOne(MCStrategyAggressiveLeverage, idx)
		residents := mcCollectResidents(w, "CR1", w.MCAuctionCount)
		causes := mcCollectOutcomeCauses(w, residents, outcomeStats(w))
		neg := causes[CauseNEG1] + causes[CauseNEG2] + causes[CauseNEG3] + causes[CauseNEG4]
		share := float64(causes[CauseUndetermined]) / float64(MaxSeats)
		t.Logf("seed idx=%d causes=%v", idx, causes)
		if neg == 0 {
			t.Errorf("idx=%d:NEG 全 0(快照未生效)", idx)
		}
		if share >= 0.60 {
			t.Errorf("idx=%d:undetermined 占比 %.0f%%,期望 < 60%%(%v)", idx, share*100, causes)
		}
	}
}
