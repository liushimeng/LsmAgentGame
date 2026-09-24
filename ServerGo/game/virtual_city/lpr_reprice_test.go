// Package virtual_city — lpr_reprice_test.go: LPR 年度重定价引擎单测(2026-09-16 §财商流P1)。
//
// 实现设计文档 §8.1 验收清单。
package virtual_city

import (
	"math"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestComputeAnnuityPayment_StandardValue 标准等额本息(v2.60 N12-3)。
func TestComputeAnnuityPayment_StandardValue(t *testing.T) {
	// 100 万,4% 年化,360 期 → 月供 ≈ 4774。
	got := computeAnnuityPayment(1000000, 0.04, 360)
	// 允许 ±1 元误差(浮点取整)。
	if math.Abs(float64(got)-4774) > 1.5 {
		t.Errorf("computeAnnuityPayment(1M, 0.04, 360): got %d, want ≈ 4774", got)
	}
	// 边界:0 期 → 0。
	if got := computeAnnuityPayment(1000000, 0.04, 0); got != 0 {
		t.Errorf("computeAnnuityPayment months=0: got %d, want 0", got)
	}
	// 边界:0 本金 → 0。
	if got := computeAnnuityPayment(0, 0.04, 360); got != 0 {
		t.Errorf("computeAnnuityPayment principal=0: got %d, want 0", got)
	}
	// 边界:利率 0 → 均摊。
	if got := computeAnnuityPayment(120000, 0, 12); got != 10000 {
		t.Errorf("computeAnnuityPayment rate=0: got %d, want 10000", got)
	}
	// 边界:负利率 → 均摊。
	if got := computeAnnuityPayment(120000, -0.01, 12); got != 10000 {
		t.Errorf("computeAnnuityPayment rate<0: got %d, want 10000", got)
	}
}

// TestLPRReprice_RateChange LPR 上调后月供增加。
func TestLPRReprice_RateChange(t *testing.T) {
	w := newTestWorld(42)
	p := w.Players[0]
	if p == nil {
		t.Fatal("seat 0 nil")
	}
	// 初始房贷:100 万余额,30 年剩余,利率 3%。
	// OrigSpread = 利率 - LPR_ref(LPR_ref = 0.03 初始) = 0。
	w.CB.PolicyRate = 0.03
	p.Loans = []Loan{
		{
			ID: "L1", Kind: LoanMortgage, Principal: 1000000, Balance: 1000000,
			AnnualRate: 0.03, TermN: 360, MonthsLeft: 360,
			OrigSpread: 0.03 - 0.03, RateFixed: false,
		},
	}
	p.Loans[0].MonthlyPayment = computeAnnuityPayment(p.Loans[0].Balance, p.Loans[0].AnnualRate, p.Loans[0].MonthsLeft)
	oldPayment := p.Loans[0].MonthlyPayment

	// LPR 上调至 5% → 重定价后月供增加。
	w.CB.PolicyRate = 0.05
	w.CB.LastCPI = 0.02
	records := w.RepriceMortgageLPR()

	if len(records) != 1 {
		t.Fatalf("Expected 1 reprice record, got %d", len(records))
	}
	if records[0].NewPayment <= records[0].OldPayment {
		t.Errorf("New payment %d should be > old %d after LPR increase", records[0].NewPayment, records[0].OldPayment)
	}
	if p.Loans[0].MonthlyPayment <= oldPayment {
		t.Errorf("MonthlyPayment should increase: got %d, old %d", p.Loans[0].MonthlyPayment, oldPayment)
	}
	// 事件 emit。
	found := false
	for _, e := range w.Events {
		if e.Type == "lpr_reprice" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected lpr_reprice event not emitted")
	}
}

// TestLPRReprice_RateFixed 固定利率贷款不参与重定价。
func TestLPRReprice_RateFixed(t *testing.T) {
	w := newTestWorld(42)
	p := w.Players[0]
	if p == nil {
		t.Fatal("seat 0 nil")
	}
	w.CB.PolicyRate = 0.03
	// 固定利率贷款(RateFixed = true)。
	p.Loans = []Loan{
		{
			ID: "L1", Kind: LoanMortgage, Principal: 1000000, Balance: 1000000,
			AnnualRate: 0.03, TermN: 360, MonthsLeft: 360,
			OrigSpread: 0, RateFixed: true,
		},
	}
	p.Loans[0].MonthlyPayment = computeAnnuityPayment(p.Loans[0].Balance, p.Loans[0].AnnualRate, p.Loans[0].MonthsLeft)
	oldPayment := p.Loans[0].MonthlyPayment

	// LPR 上调。
	w.CB.PolicyRate = 0.05
	records := w.RepriceMortgageLPR()

	// 固定利率不重定价(应无记录)。
	if len(records) != 0 {
		t.Errorf("RateFixed loan should not reprice, got %d records", len(records))
	}
	if p.Loans[0].MonthlyPayment != oldPayment {
		t.Errorf("RateFixed loan payment should not change: got %d, want %d", p.Loans[0].MonthlyPayment, oldPayment)
	}
}

// TestLPRReprice_OnlyMortgage 仅房贷参与重定价(消费贷/经营贷/信用贷跳过)。
func TestLPRReprice_OnlyMortgage(t *testing.T) {
	w := newTestWorld(42)
	p := w.Players[0]
	if p == nil {
		t.Fatal("seat 0 nil")
	}
	w.CB.PolicyRate = 0.03
	p.Loans = []Loan{
		{ID: "L1", Kind: LoanMortgage, Principal: 1000000, Balance: 1000000, AnnualRate: 0.03, TermN: 360, MonthsLeft: 360, RateFixed: false},
		{ID: "L2", Kind: LoanConsumer, Principal: 50000, Balance: 50000, AnnualRate: 0.10, TermN: 36, MonthsLeft: 36, RateFixed: false},
		{ID: "L3", Kind: LoanBusiness, Principal: 100000, Balance: 100000, AnnualRate: 0.05, TermN: 60, MonthsLeft: 60, RateFixed: false},
		{ID: "L4", Kind: LoanCreditT1, Principal: 50000, Balance: 50000, AnnualRate: 0.096, TermN: 36, MonthsLeft: 36, RateFixed: false},
	}
	w.CB.PolicyRate = 0.06
	records := w.RepriceMortgageLPR()

	// 仅房贷重定价。
	if len(records) != 1 {
		t.Fatalf("Only mortgage should reprice, got %d records", len(records))
	}
	if records[0].LoanID != "L1" {
		t.Errorf("Expected L1 to reprice, got %s", records[0].LoanID)
	}
}

// TestComputeL5Y_Standard 5Y LPR 包含期限溢价 + CPI 加点。
func TestComputeL5Y_Standard(t *testing.T) {
	cb := NewCentralBank()
	// PolicyRate = 0.03, TermPremium = 0.005, CPI = TargetCPI(0.02) → 无加点。
	l5y := cb.ComputeL5Y()
	expected := 0.03 + TermPremium
	if math.Abs(l5y-expected) > 1e-9 {
		t.Errorf("ComputeL5Y at target CPI: got %.4f, want %.4f", l5y, expected)
	}
	// CPI 高于目标 → 加点。
	cb.LastCPI = 0.04
	l5y = cb.ComputeL5Y()
	expected = 0.03 + TermPremium + (0.04-0.02)*0.5
	if math.Abs(l5y-expected) > 1e-9 {
		t.Errorf("ComputeL5Y with high CPI: got %.4f, want %.4f", l5y, expected)
	}
}

// TestLPRReprice_MultiPlayer 多玩家房贷批量重定价。
func TestLPRReprice_MultiPlayer(t *testing.T) {
	w := newTestWorld(42)
	w.CB.PolicyRate = 0.03
	for seat := 0; seat < 4; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		p.Loans = []Loan{
			{ID: "L1", Kind: LoanMortgage, Principal: 500000, Balance: 500000, AnnualRate: 0.035, TermN: 360, MonthsLeft: 360, RateFixed: false},
		}
	}
	w.CB.PolicyRate = 0.045
	records := w.RepriceMortgageLPR()
	if len(records) != 4 {
		t.Errorf("Expected 4 records (4 players), got %d", len(records))
	}
}

// TestMinskyRecordAndReclassify 发放贷款时分级 + 月结重分级。
func TestMinskyRecordAndReclassify(t *testing.T) {
	w := newTestWorld(42)
	p := w.Players[0]
	if p == nil {
		t.Fatal("seat 0 nil")
	}
	// 月供 8000,月收入 10000 → 庞氏。
	loan := &Loan{ID: "L1", Kind: LoanConsumer, Principal: 100000, Balance: 100000,
		AnnualRate: 0.10, MonthlyPayment: 8000, TermN: 36, MonthsLeft: 36}
	p.Loans = []Loan{*loan}
	w.recordMinskyForLoan(p, &p.Loans[0])
	if ms := p.MinskyByLoan["L1"]; ms == nil || ms.Tier != MinskyPonzi {
		t.Errorf("Expected Ponzi, got %v", ms)
	}
	// 月结后收入降至 5000(月供/收入 = 160%) → 仍是庞氏。
	p.SalaryBase = 5000
	w.reclassifyPlayerMinsky(p)
	ms2 := p.MinskyByLoan["L1"]
	if ms2 == nil || ms2.Tier != MinskyPonzi {
		t.Errorf("Expected Ponzi after reclassify, got %v", ms2)
	}
	if ms2 != nil && ms2.DebtToIncome != 8000.0/5000.0 {
		t.Errorf("DebtToIncome: got %.4f, want %.4f", ms2.DebtToIncome, 8000.0/5000.0)
	}
}

// TestApplyMinskyRateAdjustment 庞氏利率优惠(陷阱)。
func TestApplyMinskyRateAdjustment(t *testing.T) {
	w := newTestWorld(42)
	p := w.Players[0]
	if p == nil {
		t.Fatal("seat 0 nil")
	}
	// 庞氏等级贷款,年化 5%。
	p.Loans = []Loan{
		{ID: "L1", Kind: LoanConsumer, Principal: 100000, Balance: 100000,
			AnnualRate: 0.05, MonthlyPayment: 3000, TermN: 36, MonthsLeft: 36},
	}
	p.MinskyByLoan["L1"] = &MinskyStatus{Tier: MinskyPonzi}
	oldPayment := p.Loans[0].MonthlyPayment
	w.applyMinskyRateAdjustment(p, &p.Loans[0])
	// 利率降低 0.5%(5%→4.5%),月供减少。
	if math.Abs(p.Loans[0].AnnualRate-0.045) > 1e-9 {
		t.Errorf("Ponzi rate: got %.4f, want 0.045", p.Loans[0].AnnualRate)
	}
	if p.Loans[0].MonthlyPayment >= oldPayment {
		t.Errorf("Ponzi payment should decrease: got %d, old %d", p.Loans[0].MonthlyPayment, oldPayment)
	}
	// 对冲不调整。
	p.MinskyByLoan["L1"] = &MinskyStatus{Tier: MinskyHedge}
	rateBefore := p.Loans[0].AnnualRate
	w.applyMinskyRateAdjustment(p, &p.Loans[0])
	if p.Loans[0].AnnualRate != rateBefore {
		t.Errorf("Hedge rate should not change: got %.4f, want %.4f", p.Loans[0].AnnualRate, rateBefore)
	}
}

// TestBuildMinskyOverview 全局概览统计。
func TestBuildMinskyOverview(t *testing.T) {
	w := newTestWorld(42)
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		tier := MinskyHedge
		switch {
		case seat < 4:
			tier = MinskyPonzi
		case seat < 8:
			tier = MinskySpeculative
		}
		p.MinskyByLoan["L1"] = &MinskyStatus{Tier: tier}
	}
	w.MinskyMomentCount = 2
	w.MinskyMomentCooldown = 5
	overview := buildMinskyOverview(w)
	if overview.PonziCount != 4 {
		t.Errorf("PonziCount: got %d, want 4", overview.PonziCount)
	}
	if overview.SpecCount != 4 {
		t.Errorf("SpecCount: got %d, want 4", overview.SpecCount)
	}
	if overview.HedgeCount != 4 {
		t.Errorf("HedgeCount: got %d, want 4", overview.HedgeCount)
	}
	if overview.MomentCount != 2 {
		t.Errorf("MomentCount: got %d, want 2", overview.MomentCount)
	}
	if overview.CooldownLeft != 5 {
		t.Errorf("CooldownLeft: got %d, want 5", overview.CooldownLeft)
	}
}

// ensure profession import used.
var _ = profession.Card{}
