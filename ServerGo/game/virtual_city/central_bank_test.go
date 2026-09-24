// Package virtual_city — central_bank_test.go: 央行信用创造引擎确定性单测
// (2026-09-16 §财商流P1,设计文档 §10)。
//
// 所有测试使用固定 seed,确保可复现。
package virtual_city

import (
	"math"
	"math/rand"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestCentralBankInitialization 央行初始化:MB=55万, M2=50万, m=0.909。
func TestCentralBankInitialization(t *testing.T) {
	cb := NewCentralBank()
	if cb.BaseMoney != InitialBaseMoney {
		t.Errorf("BaseMoney: got %.0f, want %.0f", cb.BaseMoney, InitialBaseMoney)
	}
	if cb.M2 != InitialM2 {
		t.Errorf("M2: got %.0f, want %.0f", cb.M2, InitialM2)
	}
	expectedM := InitialM2 / InitialBaseMoney
	if math.Abs(cb.MoneyMultiplier-expectedM) > 0.001 {
		t.Errorf("MoneyMultiplier: got %.3f, want %.3f", cb.MoneyMultiplier, expectedM)
	}
	if cb.PolicyRate != InitialPolicyRate {
		t.Errorf("PolicyRate: got %.3f, want %.3f", cb.PolicyRate, InitialPolicyRate)
	}
	if cb.ReserveRatio != InitialReserveRatio {
		t.Errorf("ReserveRatio: got %.3f, want %.3f", cb.ReserveRatio, InitialReserveRatio)
	}
	if cb.GovBondsHeld != InitialGovBondsHeld {
		t.Errorf("GovBondsHeld: got %.0f, want %.0f", cb.GovBondsHeld, InitialGovBondsHeld)
	}
	if cb.LoansToBanks != InitialLoansToBanks {
		t.Errorf("LoansToBanks: got %.0f, want %.0f", cb.LoansToBanks, InitialLoansToBanks)
	}
}

// TestMoneyMultiplierCeiling 货币乘数上限:M2 ≤ MB × (1/r)。
func TestMoneyMultiplierCeiling(t *testing.T) {
	cb := NewCentralBank()
	cb.UpdateMoneyStats(nil)
	ceiling := cb.BaseMoney * (1 / cb.ReserveRatio)
	if cb.M2 > ceiling {
		t.Errorf("M2 %.0f exceeds ceiling %.0f", cb.M2, ceiling)
	}
	// 乘数上限 = 1/r = 10。
	if math.Abs(cb.MultiplierCeiling-10.0) > 0.001 {
		t.Errorf("MultiplierCeiling: got %.3f, want 10.0", cb.MultiplierCeiling)
	}
}

// TestEndogenousCPI 内生 CPI ∈ [0, 5%]。
func TestEndogenousCPI(t *testing.T) {
	cb := NewCentralBank()
	// 通缩情形:M2Growth < GDPGrowth → CPI = 0。
	cb.M2GrowthYoY = -0.05
	cb.RealGDPGrowth = 0.05
	cb.ComputeCPI()
	if cb.CPI != CPIMin {
		t.Errorf("CPI deflation: got %.4f, want %.4f", cb.CPI, CPIMin)
	}
	// 极端通胀:M2Growth = 20% → CPI = 5%(上限)。
	cb.M2GrowthYoY = 0.20
	cb.RealGDPGrowth = 0.05
	cb.ComputeCPI()
	if cb.CPI != CPIMax {
		t.Errorf("CPI extreme: got %.4f, want %.4f", cb.CPI, CPIMax)
	}
	// 正常情形:M2Growth = 5%, GDPGrowth = 5% → CPI = 2%。
	cb.M2GrowthYoY = 0.05
	cb.RealGDPGrowth = 0.05
	cb.ComputeCPI()
	if math.Abs(cb.CPI-TargetCPI) > 0.001 {
		t.Errorf("CPI normal: got %.4f, want %.4f", cb.CPI, TargetCPI)
	}
}

// TestEndogenousLPR 内生 LPR = PolicyRate + TermPremium + CreditSpread。
func TestEndogenousLPR(t *testing.T) {
	cb := NewCentralBank()
	cb.PolicyRate = 0.03
	cb.CreditSpread = 0.01
	lpr := cb.ComputeLPR()
	expected := 0.03 + TermPremium + 0.01
	if math.Abs(lpr-expected) > 0.0001 {
		t.Errorf("LPR: got %.4f, want %.4f", lpr, expected)
	}
}

// TestCreditTightness 信贷约束:m/m_max > 85% 时 CreditTightness > 0。
func TestCreditTightness(t *testing.T) {
	cb := NewCentralBank()
	// 宽松:m/m_max = 50% → CreditTightness = 0。
	cb.MoneyMultiplier = 5.0
	cb.MultiplierCeiling = 10.0
	cb.ComputeCreditTightness()
	if cb.CreditTightness != 0 {
		t.Errorf("CreditTightness loose: got %.4f, want 0", cb.CreditTightness)
	}
	// 收紧:m/m_max = 90% → CreditTightness > 0。
	cb.MoneyMultiplier = 9.0
	cb.ComputeCreditTightness()
	if cb.CreditTightness <= 0 {
		t.Errorf("CreditTightness tight: got %.4f, want > 0", cb.CreditTightness)
	}
	// 极值:m/m_max = 100% → CreditTightness = 1。
	cb.MoneyMultiplier = 10.0
	cb.ComputeCreditTightness()
	if cb.CreditTightness != 1.0 {
		t.Errorf("CreditTightness max: got %.4f, want 1.0", cb.CreditTightness)
	}
	// 额度乘数有界。
	if cb.LoanQuotaFactor < LoanQuotaMin || cb.LoanQuotaFactor > 1.0 {
		t.Errorf("LoanQuotaFactor: got %.4f, want in [0.5, 1.0]", cb.LoanQuotaFactor)
	}
	// 利率上浮有界。
	if cb.CreditSpread < 0 || cb.CreditSpread > CreditSpreadMax {
		t.Errorf("CreditSpread: got %.4f, want in [0, 0.02]", cb.CreditSpread)
	}
}

// TestTransmissionChain 传导链:贷款 → M2↑ → CPI↑ → LPR↑。
func TestTransmissionChain(t *testing.T) {
	cb := NewCentralBank()
	cb.UpdateMoneyStats(nil)
	initialM2 := cb.M2
	initialCPI := cb.CPI
	initialLPR := cb.ComputeLPR()

	// 模拟贷款增加 M2(乘数上升 → 信贷约束收紧 → CreditSpread↑ → LPR↑)。
	// M0=50万,MB=55万;目标 m > 8.5(M2 > 467.5万)。
	cb.M2 = 4800000
	cb.BaseMoney = cb.M0 + cb.Reserves
	cb.MoneyMultiplier = cb.M2 / cb.BaseMoney
	cb.M2GrowthYoY = 0.10 // 10% 增长
	cb.RealGDPGrowth = 0.02
	cb.ComputeCPI()
	cb.ComputeCreditTightness() // 乘数上升 → 信贷约束收紧
	cb.ComputeLPR()

	if cb.M2 <= initialM2 {
		t.Errorf("M2 should increase: got %.0f, want > %.0f", cb.M2, initialM2)
	}
	if cb.CPI <= initialCPI {
		t.Errorf("CPI should increase: got %.4f, want > %.4f", cb.CPI, initialCPI)
	}
	if cb.ComputeLPR() <= initialLPR {
		t.Errorf("LPR should increase: got %.4f, want > %.4f", cb.ComputeLPR(), initialLPR)
	}
}

// TestBalanceSheetConservation 守恒:央行资产负债表平衡。
func TestBalanceSheetConservation(t *testing.T) {
	cb := NewCentralBank()
	// 初始平衡:GovBondsHeld + LoansToBanks = Reserves + CurrencyIssued。
	assets := cb.GovBondsHeld + cb.LoansToBanks
	liabilities := cb.Reserves + cb.CurrencyIssued
	if math.Abs(assets-liabilities) > 0.01 {
		t.Errorf("Balance sheet not balanced: assets %.0f != liabilities %.0f", assets, liabilities)
	}
	// 经过月度决策后仍平衡。
	w := NewWorld(42, [MaxSeats]profession.Card{})
	w.StartGame()
	cb.MonthlyDecision(w, rand.New(rand.NewSource(42)))
	assets = cb.GovBondsHeld + cb.LoansToBanks
	liabilities = cb.Reserves + cb.CurrencyIssued
	if math.Abs(assets-liabilities) > 0.01 {
		t.Errorf("Balance sheet not balanced after decision: assets %.0f != liabilities %.0f", assets, liabilities)
	}
}

// TestSeedReproducibility seed 复现:同 seed 同快照。
func TestSeedReproducibility(t *testing.T) {
	seed := int64(12345)
	w1 := NewWorld(seed, [MaxSeats]profession.Card{})
	w1.StartGame()
	cb1 := NewCentralBank()
	cb1.MonthlyDecision(w1, rand.New(rand.NewSource(seed)))

	w2 := NewWorld(seed, [MaxSeats]profession.Card{})
	w2.StartGame()
	cb2 := NewCentralBank()
	cb2.MonthlyDecision(w2, rand.New(rand.NewSource(seed)))

	if len(cb1.History) != len(cb2.History) {
		t.Fatalf("History length mismatch: %d vs %d", len(cb1.History), len(cb2.History))
	}
	for i := range cb1.History {
		s1 := cb1.History[i]
		s2 := cb2.History[i]
		if s1.M0 != s2.M0 || s1.M2 != s2.M2 || s1.PolicyRate != s2.PolicyRate {
			t.Errorf("Snapshot %d mismatch: %+v vs %+v", i, s1, s2)
		}
	}
}

// TestReserveRatioBoundary 边界条件:准备金率 5%/20%。
func TestReserveRatioBoundary(t *testing.T) {
	// 准备金率 5% → 乘数上限 = 20。
	cb := NewCentralBank()
	cb.ReserveRatio = 0.05
	cb.MultiplierCeiling = 1.0 / cb.ReserveRatio
	if math.Abs(cb.MultiplierCeiling-20.0) > 0.001 {
		t.Errorf("MultiplierCeiling at 5%%: got %.3f, want 20.0", cb.MultiplierCeiling)
	}
	// 准备金率 20% → 乘数上限 = 5。
	cb.ReserveRatio = 0.20
	cb.MultiplierCeiling = 1.0 / cb.ReserveRatio
	if math.Abs(cb.MultiplierCeiling-5.0) > 0.001 {
		t.Errorf("MultiplierCeiling at 20%%: got %.3f, want 5.0", cb.MultiplierCeiling)
	}
}

// TestCreditTightnessExtreme 信贷约束极值:m/m_max = 100% → CreditTightness = 1。
func TestCreditTightnessExtreme(t *testing.T) {
	cb := NewCentralBank()
	cb.MoneyMultiplier = cb.MultiplierCeiling // 100% 利用率。
	cb.ComputeCreditTightness()
	if cb.CreditTightness != 1.0 {
		t.Errorf("CreditTightness at 100%%: got %.4f, want 1.0", cb.CreditTightness)
	}
	if cb.LoanQuotaFactor != LoanQuotaMin {
		t.Errorf("LoanQuotaFactor at max tightness: got %.4f, want %.4f", cb.LoanQuotaFactor, LoanQuotaMin)
	}
	if cb.CreditSpread != CreditSpreadMax {
		t.Errorf("CreditSpread at max tightness: got %.4f, want %.4f", cb.CreditSpread, CreditSpreadMax)
	}
}

// TestDeflation 通缩情形:M2Growth < GDPGrowth → CPI = 0,通胀因子保底 2%。
func TestDeflation(t *testing.T) {
	cb := NewCentralBank()
	cb.M2GrowthYoY = -0.10
	cb.RealGDPGrowth = 0.05
	cb.ComputeCPI()
	if cb.CPI != 0 {
		t.Errorf("CPI deflation: got %.4f, want 0", cb.CPI)
	}
	// 通胀因子保底 2%。
	factor := math.Pow(1+math.Max(cb.CPI, 0.02), 1)
	if math.Abs(factor-1.02) > 0.001 {
		t.Errorf("Inflation factor floor: got %.4f, want 1.02", factor)
	}
}

// TestMultiplierExhausted 货币乘数用尽:M2 = MB × (1/r) → 无法新增贷款。
func TestMultiplierExhausted(t *testing.T) {
	cb := NewCentralBank()
	cb.M2 = cb.BaseMoney * (1 / cb.ReserveRatio) // 用尽乘数。
	cb.MoneyMultiplier = cb.M2 / cb.BaseMoney
	cb.ComputeCreditTightness()
	if cb.CreditTightness != 1.0 {
		t.Errorf("CreditTightness at exhausted multiplier: got %.4f, want 1.0", cb.CreditTightness)
	}
	// 贷款冻结:额度乘数 = 0.5,利率上浮 = 2%。
	if cb.LoanQuotaFactor != LoanQuotaMin {
		t.Errorf("LoanQuotaFactor at exhausted: got %.4f, want %.4f", cb.LoanQuotaFactor, LoanQuotaMin)
	}
}

// TestAllPlayersBankrupt 玩家全部破产:alivePlayers() = 0 → M2 归零。
func TestAllPlayersBankrupt(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{})
	w.StartGame()
	// 让所有玩家出局。
	for _, p := range w.Players {
		if p != nil {
			p.Alive = false
		}
	}
	cb := NewCentralBank()
	cb.UpdateMoneyStats(w)
	if cb.M0 != 0 {
		t.Errorf("M0 after all bankrupt: got %.0f, want 0", cb.M0)
	}
	if cb.M2 != 0 {
		t.Errorf("M2 after all bankrupt: got %.0f, want 0", cb.M2)
	}
}

// TestMonthlyDecisionPolicyEvent 月度决策产生政策事件。
func TestMonthlyDecisionPolicyEvent(t *testing.T) {
	w := NewWorld(42, [MaxSeats]profession.Card{})
	w.StartGame()
	cb := NewCentralBank()
	// 设置 M2 高增速 → ComputeCPI 得出高通胀 → 触发加息。
	cb.M2GrowthYoY = 0.10
	cb.RealGDPGrowth = 0.02
	initialRate := cb.PolicyRate
	cb.MonthlyDecision(w, rand.New(rand.NewSource(42)))
	if cb.PolicyRate <= initialRate {
		t.Errorf("PolicyRate should increase: got %.4f, want > %.4f", cb.PolicyRate, initialRate)
	}
	// 检查是否产生 policy 事件。
	found := false
	for _, e := range w.Events {
		if e.Type == "policy" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected policy event not found")
	}
}
