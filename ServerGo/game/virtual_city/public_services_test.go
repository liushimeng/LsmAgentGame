// Package virtual_city — public_services_test.go: 阶段8 公共服务+监管+选举 单测
// (2026-09-21 §城市扩张v2.12,最终阶段)。
package virtual_city

import (
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
	"LsmAgentGame/game/virtual_city/regulators"
)

// ─────────────────── 公共服务五件套 ───────────────────

// TestPublicServices_Initial 初始状态:五指标零值 + 投入子系统非 nil。
func TestPublicServices_Initial(t *testing.T) {
	ps := NewPublicServices()
	if ps.EduQuality != 0 || ps.MedQuality != 0 || ps.PensionLevel != 0 ||
		ps.HousingAfford != 0 || ps.Employment != 0 {
		t.Fatalf("initial five indicators not zero: %+v", ps)
	}
	if ps.Edu == nil || ps.Med == nil {
		t.Fatalf("Edu/Med subsystem nil at init")
	}
	if ps.MonthsRun != 0 || ps.LastStepMonth != 0 {
		t.Fatalf("MonthsRun/LastStepMonth not zero: %d/%d", ps.MonthsRun, ps.LastStepMonth)
	}
}

// TestPublicServices_QualityAccumulation 财政投入累积质量提升:
// LastMonthExpense>0 时三质量按 FiscalBudget 份额 × 0.01 递增。
func TestPublicServices_QualityAccumulation(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{{ID: "P01"}})
	w.Treasury.LastMonthExpense = 1_000_000
	ps := NewPublicServices()
	ps.MonthlyStep(w)

	wantEdu := FiscalBudget.Education * PublicSvcQualityGain  // 0.30 × 0.01 = 0.003
	wantMed := FiscalBudget.Healthcare * PublicSvcQualityGain // 0.25 × 0.01 = 0.0025
	wantPen := FiscalBudget.Pension * PublicSvcQualityGain    // 0.20 × 0.01 = 0.002
	if ps.EduQuality != wantEdu {
		t.Errorf("EduQuality = %v, want %v", ps.EduQuality, wantEdu)
	}
	if ps.MedQuality != wantMed {
		t.Errorf("MedQuality = %v, want %v", ps.MedQuality, wantMed)
	}
	if ps.PensionLevel != wantPen {
		t.Errorf("PensionLevel = %v, want %v", ps.PensionLevel, wantPen)
	}
	if ps.MonthsRun != 1 {
		t.Errorf("MonthsRun = %d, want 1", ps.MonthsRun)
	}
}

// TestPublicServices_HousingAfford 住房可负担性映射:无玩家收入 → 0;
// 有玩家时 ∈ [0,1]。
func TestPublicServices_HousingAfford(t *testing.T) {
	// 无玩家:中位收入 0 → afford 0。
	w0 := NewWorld(7, [MaxSeats]profession.Card{})
	ps0 := NewPublicServices()
	ps0.MonthlyStep(w0)
	if ps0.HousingAfford != 0 {
		t.Fatalf("no-player HousingAfford = %v, want 0", ps0.HousingAfford)
	}
	// 有玩家:映射 ∈ [0,1]。
	w := NewWorld(7, [MaxSeats]profession.Card{{ID: "P09", Salary: 20000}})
	ps := NewPublicServices()
	ps.MonthlyStep(w)
	if ps.HousingAfford < 0 || ps.HousingAfford > 1 {
		t.Fatalf("HousingAfford = %v out of [0,1]", ps.HousingAfford)
	}
}

// TestEducation_CognitionBoost 教育代际效应:月收入 > 15000 的座位累积
// 0.1/年;连续 10 年 → Cognition +1;低收入座位不累积。
func TestEducation_CognitionBoost(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{
		{ID: "P09", Salary: 20000, Cognition: 5}, // 高收入:可累积
		{ID: "P01", Salary: 5000, Cognition: 5},  // 低收入:门槛下
	})
	ps := NewPublicServices()
	w.PublicSvc = ps

	for i := 0; i < 10; i++ {
		ps.AnnualEduStep(w)
	}
	hi := w.Players[0]
	lo := w.Players[1]
	if hi.Cognition != 6 {
		t.Errorf("high-income Cognition = %d, want 6 (5 + 10年×0.1)", hi.Cognition)
	}
	if lo.Cognition != 5 {
		t.Errorf("low-income Cognition = %d, want 5 (门槛下不累积)", lo.Cognition)
	}
}

// ─────────────────── 反垄断 ───────────────────

// mockCapacity 反垄断测试用产能源。
type mockCapacity struct{ caps map[string]float64 }

func (m *mockCapacity) CapacityOf(id string) float64 { return m.caps[id] }
func (m *mockCapacity) TotalCapacity() float64 {
	var s float64
	for _, v := range m.caps {
		s += v
	}
	return s
}
func (m *mockCapacity) SetCapacity(id string, c float64) { m.caps[id] = c }

// TestAntitrust_MarketShare 市占 >40% 连续 3 月 → 分拆(Capacity × 0.5)。
func TestAntitrust_MarketShare(t *testing.T) {
	src := &mockCapacity{caps: map[string]float64{
		"giant": 60, "a": 10, "b": 10, "c": 10, "d": 10, // giant = 60%
	}}
	a := regulators.NewAntitrustRegulator()

	var lastSplit []regulators.SplitAction
	for m := 1; m <= 3; m++ {
		lastSplit = a.MonthlyStep(m, src, []string{"a", "b", "c", "d", "giant"})
	}
	if a.Splits != 1 {
		t.Fatalf("Splits = %d, want 1 after 3 consecutive months", a.Splits)
	}
	if got := src.CapacityOf("giant"); got != 30 { // 60 × 0.5
		t.Errorf("giant capacity after split = %v, want 30", got)
	}
	if len(lastSplit) != 1 || lastSplit[0].NodeID != "giant" {
		t.Errorf("split action = %+v, want single giant entry", lastSplit)
	}
	if a.Investigations != 1 {
		t.Errorf("Investigations = %d, want 1", a.Investigations)
	}
}

// TestAntitrust_UnderThreshold 市占 ≤40% 不触发分拆。
func TestAntitrust_UnderThreshold(t *testing.T) {
	src := &mockCapacity{caps: map[string]float64{
		"big": 35, "a": 30, "b": 35, // big = 35% < 40%
	}}
	a := regulators.NewAntitrustRegulator()
	for m := 1; m <= 6; m++ {
		a.MonthlyStep(m, src, []string{"a", "big", "b"})
	}
	if a.Splits != 0 || a.Investigations != 0 {
		t.Fatalf("under-threshold triggered: Splits=%d Investigations=%d", a.Splits, a.Investigations)
	}
}

// ─────────────────── 证监会 ───────────────────

// TestSecurities_InsiderFine 大额买入+当月反向卖出 → 罚款 5%,且受
// 当月收入 5% 封顶(R8-3)。
func TestSecurities_InsiderFine(t *testing.T) {
	sr := regulators.NewSecuritiesRegulator()
	trades := []regulators.TradeRecord{
		{Seat: 0, Month: 1, Side: regulators.TradeBuy, AmountCNY: 600_000},
		{Seat: 0, Month: 1, Side: regulators.TradeSell, AmountCNY: 600_000},
	}
	fines := sr.MonthlyStep(1, trades, func(seat int) int64 { return 100_000 })
	if len(fines) != 1 {
		t.Fatalf("fines len = %d, want 1: %+v", len(fines), fines)
	}
	// 应罚 600000×5% = 30000;封顶 100000×5% = 5000 → 实罚 5000。
	if fines[0].FineCNY != 5000 {
		t.Errorf("FineCNY = %d, want 5000 (5%% of income cap)", fines[0].FineCNY)
	}
	if sr.InsiderCases != 1 || sr.InsiderTradeFines != 5000 {
		t.Errorf("cases=%d fines=%d, want 1/5000", sr.InsiderCases, sr.InsiderTradeFines)
	}
}

// TestSecurities_SmallTradeExempt 小额交易(<50 万)不触发。
func TestSecurities_SmallTradeExempt(t *testing.T) {
	sr := regulators.NewSecuritiesRegulator()
	trades := []regulators.TradeRecord{
		{Seat: 0, Month: 1, Side: regulators.TradeBuy, AmountCNY: 499_999},
		{Seat: 0, Month: 1, Side: regulators.TradeSell, AmountCNY: 499_999},
	}
	fines := sr.MonthlyStep(1, trades, func(seat int) int64 { return 100_000 })
	if len(fines) != 0 || sr.InsiderCases != 0 {
		t.Fatalf("small trade fined: %+v", fines)
	}
}

// ─────────────────── 市长选举 ───────────────────

// TestCivicElection_Disabled R8-2:默认关闭 MonthlyStep 完全 no-op。
func TestCivicElection_Disabled(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{{ID: "P09"}})
	w.Treasury.Cash = 10_000_000
	ce := NewCivicElection()
	before := w.Treasury.Cash
	ce.MonthlyStep(w) // Enabled=false
	if ce.MonthsRun != 0 {
		t.Errorf("disabled election MonthsRun = %d, want 0", ce.MonthsRun)
	}
	if w.Treasury.Cash != before {
		t.Errorf("disabled election moved treasury cash: %d → %d", before, w.Treasury.Cash)
	}
}

// TestCivicElection_VoteWeights 得票权重:财富 30% + 人脉 40% + 满意度 30%;
// 全能者得分最高且当选。
func TestCivicElection_VoteWeights(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{
		{ID: "P12", Savings: 5_000_000, Network: 9}, // 富 + 人脉高
		{ID: "P01", Savings: 5_000, Network: 2},     // 弱
	})
	ce := NewCivicElection()
	votes := ce.ComputeVotes(w)
	if len(votes) != 2 {
		t.Fatalf("votes len = %d, want 2", len(votes))
	}
	if votes[0].Seat != 0 { // 0 号位全能者
		t.Errorf("top vote seat = %d, want 0", votes[0].Seat)
	}
	for _, v := range votes {
		// 满意度为全城同值;综合 = 0.3×财富 + 0.4×人脉 + 0.3×满意度。
		want := v.WealthScore*0.3 + v.NetworkScore*0.4 + v.Satisfaction*0.3
		if diff := v.Score - want; diff > 0.01 || diff < -0.01 {
			t.Errorf("seat %d Score=%.2f, want %.2f (30/40/30)", v.Seat, v.Score, want)
		}
	}
	// 当选 + 事件记录。
	got := ce.RunElection(w)
	if got != 0 {
		t.Errorf("RunElection winner = %d, want 0", got)
	}
	if ce.MayorSeat != 0 || ce.LastElectionMonth != w.Month {
		t.Errorf("MayorSeat=%d LastElectionMonth=%d", ce.MayorSeat, ce.LastElectionMonth)
	}
}

// TestCivicElection_Stipend 开启后市长每月津贴 500 元(国库充足才发)。
func TestCivicElection_Stipend(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{{ID: "P09"}})
	w.Treasury.Cash = 10_000
	ce := NewCivicElection()
	ce.Enabled = true
	ce.MayorSeat = 0
	ce.LastElectionMonth = 1 // 避免立即重选
	before := w.Treasury.Cash
	ce.MonthlyStep(w)
	if w.Treasury.Cash != before-MayorStipendCNY {
		t.Errorf("treasury = %d, want %d (stipend 500)", w.Treasury.Cash, before-MayorStipendCNY)
	}
}

// ─────────────────── 接线与确定性 ───────────────────

// TestPublicServices_WiredIntoSettleMonth §130:SettleMonth ⑨G 推进
// PublicSvc/Regulators + BuildClientState 下发 public_services。
func TestPublicServices_WiredIntoSettleMonth(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{{ID: "P01"}, {ID: "P05"}, {ID: "P09"}})
	w.StartGame()
	run0 := w.PublicSvc.MonthsRun
	w.SettleMonth()
	if w.PublicSvc.MonthsRun != run0+1 {
		t.Errorf("PublicSvc.MonthsRun = %d, want %d (⑨G wired)", w.PublicSvc.MonthsRun, run0+1)
	}
	if w.Regulators.LastStepMonth != w.Month-1 {
		t.Errorf("Regulators.LastStepMonth = %d, want %d", w.Regulators.LastStepMonth, w.Month-1)
	}

	cs := BuildClientState("room-s8", 0, w, [MaxSeats]string{"u:0"}, [MaxSeats]string{"玩家0"},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil)
	if cs.PublicSvc == nil {
		t.Fatalf("view public_services nil after SettleMonth (wired)")
	}
	if cs.PublicSvc.ElectionEnabled {
		t.Errorf("ElectionEnabled default true, want false (R8-2)")
	}
	if cs.PublicSvc.MayorSeat != -1 {
		t.Errorf("MayorSeat = %d, want -1", cs.PublicSvc.MayorSeat)
	}
}

// TestAnnualEduStep_Wired §130:AnnualAdjust(年末)推进教育代际效应。
func TestAnnualEduStep_Wired(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{{ID: "P09", Salary: 20000, Cognition: 5}})
	ps := NewPublicServices()
	w.PublicSvc = ps
	w.AnnualAdjust()
	if ps.Edu.accrual[0] != EduCognitionAnnualGain {
		t.Errorf("accrual after AnnualAdjust = %v, want %v (wired)", ps.Edu.accrual[0], EduCognitionAnnualGain)
	}
}

// TestPublicServices_Deterministic 同种子两个世界各结 3 月:
// 公共服务五指标 + 监管监测面逐字段一致。
func TestPublicServices_Deterministic(t *testing.T) {
	settle3 := func() PublicServices {
		w := NewWorld(99, [MaxSeats]profession.Card{
			{ID: "P01"}, {ID: "P05"}, {ID: "P09", Salary: 20000},
		})
		w.StartGame()
		for i := 0; i < 3; i++ {
			w.SettleMonth()
		}
		return *w.PublicSvc
	}
	a, b := settle3(), settle3()
	if a.EduQuality != b.EduQuality || a.MedQuality != b.MedQuality ||
		a.PensionLevel != b.PensionLevel || a.HousingAfford != b.HousingAfford ||
		a.Employment != b.Employment {
		t.Fatalf("public services diverged: %+v vs %+v", a, b)
	}
}
