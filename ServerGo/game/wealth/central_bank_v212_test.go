// Package wealth — central_bank_v212_test.go: v2.12 阶段3 宏观金融深化单测
// (货币政策工具箱 + 菲利普斯曲线 + 利率传导 4 步链 + 季度央行公告)
// (2026-09-21 §城市扩张v2.12)。
package wealth

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"LsmAgentGame/game/wealth/profession"
)

// newV212World 构造 1 人固定种子测试世界(P01 零值卡占座 0)。
func newV212World() *World {
	return NewWorld(42, [MaxSeats]profession.Card{{ID: "P01"}})
}

// TestPolicyToolbox_ApplyRRR 测试存款准备金率调整(降准 0.25%)。
func TestPolicyToolbox_ApplyRRR(t *testing.T) {
	w := newV212World()
	tb := NewPolicyToolbox(w.CB)
	act := PolicyAction{Instrument: InstrumentRRRRateCut, Month: 1, Magnitude: -0.0025, Stance: "dovish"}
	if err := tb.ApplyInstrument(w, act); err != nil {
		t.Fatalf("ApplyInstrument(RRR cut) failed: %v", err)
	}
	if w.CB.ReserveRatio != InitialReserveRatio-0.0025 {
		t.Errorf("ReserveRatio = %f, want %f", w.CB.ReserveRatio, InitialReserveRatio-0.0025)
	}
	// 联动:乘数上限 = 1/r;历史入册。
	if math.Abs(w.CB.MultiplierCeiling-1.0/w.CB.ReserveRatio) > 1e-12 {
		t.Errorf("MultiplierCeiling = %f, want %f", w.CB.MultiplierCeiling, 1.0/w.CB.ReserveRatio)
	}
	if len(tb.History) != 1 {
		t.Errorf("History len = %d, want 1", len(tb.History))
	}
	// 事件流播报(§130 接线验证)。
	if !hasEventText(w, "降准") {
		t.Errorf("expected policy event containing 降准, got events: %v", w.Events)
	}
}

// TestPolicyToolbox_ApplyRRRBounds 降准到 5% 下限 / 升准到 20% 上限后 clamp。
func TestPolicyToolbox_ApplyRRRBounds(t *testing.T) {
	w := newV212World()
	tb := NewPolicyToolbox(w.CB)
	// 25 次降准(25×0.25% = 6.25% > 可降空间 5%)→ 触底 RRRMin=5%。
	for i := 0; i < 25; i++ {
		if err := tb.ApplyInstrument(w, PolicyAction{Instrument: InstrumentRRRRateCut, Month: i + 1, Magnitude: -0.0025}); err != nil {
			t.Fatalf("cut #%d failed: %v", i, err)
		}
	}
	if math.Abs(w.CB.ReserveRatio-RRRMin) > 1e-12 {
		t.Errorf("ReserveRatio after floor = %f, want %f", w.CB.ReserveRatio, RRRMin)
	}
	// 升准触顶:从 5% 起升至 20% 需 60 步,多升 10 步验证 clamp 在 RRRMax=20%。
	for i := 0; i < 70; i++ {
		if err := tb.ApplyInstrument(w, PolicyAction{Instrument: InstrumentRRRRateHike, Month: i + 1, Magnitude: 0.0025}); err != nil {
			t.Fatalf("hike #%d failed: %v", i, err)
		}
	}
	if math.Abs(w.CB.ReserveRatio-RRRMax) > 1e-12 {
		t.Errorf("ReserveRatio after cap = %f, want %f", w.CB.ReserveRatio, RRRMax)
	}
	// 零步长拒绝。
	if err := tb.ApplyInstrument(w, PolicyAction{Instrument: InstrumentRRRRateCut, Magnitude: 0.0001}); err == nil {
		t.Errorf("RRR magnitude < step should be rejected")
	}
}

// TestPolicyToolbox_ApplyOthers MLF/SLF/OMO/CreditWindow 四件工具数值。
func TestPolicyToolbox_ApplyOthers(t *testing.T) {
	// MLF 加息 25bp:PolicyRate 初始 3% + 0.25% = 3.25%。
	w := newV212World()
	tb := NewPolicyToolbox(w.CB)
	if err := tb.ApplyInstrument(w, PolicyAction{Instrument: InstrumentMLF, Month: 1, Magnitude: 0.0025, Stance: "hawkish"}); err != nil {
		t.Fatalf("ApplyInstrument(MLF) failed: %v", err)
	}
	if math.Abs(w.CB.PolicyRate-0.0325) > 1e-12 {
		t.Errorf("PolicyRate = %f, want 0.0325", w.CB.PolicyRate)
	}
	if math.Abs(w.CB.RediscountRate-(0.0325+RediscountMarkup)) > 1e-12 {
		t.Errorf("RediscountRate = %f, want %f", w.CB.RediscountRate, 0.0325+RediscountMarkup)
	}

	// SLF 走廊上限抬 10bp:SLFRate = 3% + 0.5% + 0.1% = 3.6%(以当时 PolicyRate 计)。
	if err := tb.ApplyInstrument(w, PolicyAction{Instrument: InstrumentSLF, Month: 2, Magnitude: 0.001}); err != nil {
		t.Fatalf("ApplyInstrument(SLF) failed: %v", err)
	}
	if math.Abs(w.CB.SLFRate-(w.CB.PolicyRate+SLFCorridorWidth+0.001)) > 1e-12 {
		t.Errorf("SLFRate = %f, want %f", w.CB.SLFRate, w.CB.PolicyRate+SLFCorridorWidth+0.001)
	}

	// OMO 投放 5%:BaseMoney 550000 → 577500。
	w2 := newV212World()
	tb2 := NewPolicyToolbox(w2.CB)
	oldMB := w2.CB.BaseMoney
	if err := tb2.ApplyInstrument(w2, PolicyAction{Instrument: InstrumentOMO, Month: 1, Magnitude: 0.05, Stance: "dovish"}); err != nil {
		t.Fatalf("ApplyInstrument(OMO) failed: %v", err)
	}
	if math.Abs(w2.CB.BaseMoney-oldMB*1.05) > 1e-6 {
		t.Errorf("BaseMoney = %f, want %f", w2.CB.BaseMoney, oldMB*1.05)
	}

	// 信贷窗口收紧 0.2:乘数 1.0→0.8,额度乘数同乘。
	w3 := newV212World()
	tb3 := NewPolicyToolbox(w3.CB)
	if err := tb3.ApplyInstrument(w3, PolicyAction{Instrument: InstrumentCreditWindow, Month: 1, Magnitude: -0.2}); err != nil {
		t.Fatalf("ApplyInstrument(CreditWindow) failed: %v", err)
	}
	if math.Abs(w3.CB.CreditWindowFactor-0.8) > 1e-12 {
		t.Errorf("CreditWindowFactor = %f, want 0.8", w3.CB.CreditWindowFactor)
	}
	if math.Abs(w3.CB.LoanQuotaFactor-0.8) > 1e-12 {
		t.Errorf("LoanQuotaFactor = %f, want 0.8", w3.CB.LoanQuotaFactor)
	}
}

// TestPolicyToolbox_HistoryTrim 历史仅保留最近 12 月。
func TestPolicyToolbox_HistoryTrim(t *testing.T) {
	w := newV212World()
	tb := NewPolicyToolbox(w.CB)
	for i := 0; i < 15; i++ {
		if err := tb.ApplyInstrument(w, PolicyAction{Instrument: InstrumentMLF, Month: i + 1, Magnitude: 0}); err != nil {
			t.Fatalf("apply #%d failed: %v", i, err)
		}
	}
	if len(tb.History) != ToolboxHistoryMonths {
		t.Errorf("History len = %d, want %d", len(tb.History), ToolboxHistoryMonths)
	}
	if tb.History[len(tb.History)-1].Month != 15 {
		t.Errorf("last history month = %d, want 15", tb.History[len(tb.History)-1].Month)
	}
}

// TestPolicyToolbox_NilGuards nil World / 未知工具返回错误。
func TestPolicyToolbox_NilGuards(t *testing.T) {
	tb := NewPolicyToolbox(nil) // cb nil 兜底新建,不 panic。
	if tb.CB == nil {
		t.Fatalf("NewPolicyToolbox(nil) should fallback to NewCentralBank")
	}
	w := newV212World()
	if err := tb.ApplyInstrument(nil, PolicyAction{Instrument: InstrumentMLF}); err == nil {
		t.Errorf("nil world should be rejected")
	}
	if err := tb.ApplyInstrument(w, PolicyAction{Instrument: PolicyInstrument(99)}); err == nil {
		t.Errorf("unknown instrument should be rejected")
	}
}

// TestPhillipsCurve_Dovish 低通胀(CPI=1%)+ 自然失业率 → 鸽派倾向。
func TestPhillipsCurve_Dovish(t *testing.T) {
	w := newV212World()
	w.CB.CPI = 0.01 // 低通胀
	out := EvaluatePhillips(w)
	if out.Stance != "dovish" && out.Stance != "neutral" {
		t.Errorf("Phillips CPI=1%% → stance=%s, want dovish/neutral", out.Stance)
	}
	if out.RateBias > 0 {
		t.Errorf("Phillips CPI=1%% → RateBias=%f, want ≤ 0", out.RateBias)
	}
}

// TestPhillipsCurve_Hawkish 高通胀(CPI=6%)→ 鹰派倾向。
func TestPhillipsCurve_Hawkish(t *testing.T) {
	w := newV212World()
	w.CB.CPI = 0.06 // 高通胀
	out := EvaluatePhillips(w)
	if out.Stance != "hawkish" && out.Stance != "neutral" {
		t.Errorf("Phillips CPI=6%% → stance=%s, want hawkish/neutral", out.Stance)
	}
	if out.RateBias <= 0 {
		t.Errorf("Phillips CPI=6%% → RateBias=%f, want > 0", out.RateBias)
	}
}

// TestPhillipsCurve_Neutral 通胀 2.5% + 失业 5% → 中性,RateBias=0。
func TestPhillipsCurve_Neutral(t *testing.T) {
	w := newV212World()
	w.CB.CPI = 0.025
	w.Labor.Unemployment = 0.05
	out := EvaluatePhillips(w)
	if out.Stance != "neutral" {
		t.Errorf("Phillips CPI=2.5%%/U=5%% → stance=%s, want neutral", out.Stance)
	}
	if out.RateBias != 0 {
		t.Errorf("Phillips neutral → RateBias=%f, want 0", out.RateBias)
	}
}

// TestPhillipsCurve_BiasClamp 线性插值分支 clamp ±5% + 高失业触发鸽派规则。
func TestPhillipsCurve_BiasClamp(t *testing.T) {
	w := newV212World()
	w.CB.CPI = 0.049            // 未触发鹰派规则
	w.Labor.Unemployment = 0.02 // 低失业放大加息倾向
	out := EvaluatePhillips(w)
	if math.Abs(out.RateBias) > PhillipsBiasMax+1e-12 {
		t.Errorf("RateBias = %f exceeds clamp ±%f", out.RateBias, PhillipsBiasMax)
	}
	if out.Stance != "hawkish" {
		t.Errorf("CPI=4.9%%/U=2%% → stance=%s, want hawkish", out.Stance)
	}

	// 失业率 > 8% → 鸽派规则(即使 CPI 偏高也按失业优先于插值分支)。
	w2 := newV212World()
	w2.CB.CPI = 0.03
	w2.Labor.Unemployment = 0.09
	out2 := EvaluatePhillips(w2)
	if out2.Stance != "dovish" || out2.RateBias != PhillipsDovishBias {
		t.Errorf("U=9%% → stance=%s bias=%f, want dovish/%f", out2.Stance, out2.RateBias, PhillipsDovishBias)
	}
	// nil World 安全。
	if EvaluatePhillips(nil).Description == "" {
		t.Errorf("EvaluatePhillips(nil) should be nil-safe with description")
	}
}

// TestInterestTransmission_Chain 测试利率传导 4 步链。
func TestInterestTransmission_Chain(t *testing.T) {
	w := newV212World()
	steps := TraceTransmission(w)
	if len(steps) < 4 {
		t.Fatalf("Transmission steps = %d, want >= 4", len(steps))
	}
	// 第 1 步数值:SHIBOR = MLF(PolicyRate) + 0.1% + 0.2%。
	wantSHIBOR := w.CB.PolicyRate + SHIBORBaseSpread + LiquidityPremium
	if math.Abs(steps[0].OutRate-wantSHIBOR) > 1e-12 {
		t.Errorf("step0 SHIBOR = %f, want %f", steps[0].OutRate, wantSHIBOR)
	}
	// 第 3 步:存款利率 < 零售贷款利率(NIM 拆分为正)。
	if steps[2].DepositRate >= steps[2].OutRate {
		t.Errorf("deposit %f should < loan %f", steps[2].DepositRate, steps[2].OutRate)
	}
	// 步骤名有序且非空。
	for i, s := range steps {
		if s.Name == "" || s.Description == "" {
			t.Errorf("step %d missing name/description", i)
		}
		if s.Index != i {
			t.Errorf("step %d Index = %d, want %d", i, s.Index, i)
		}
	}
	// 消费贷利率 = 1Y LPR + ConsumerSpread + CreditSpread。
	wantConsumer := w.CB.LPR1Y() + ConsumerSpread + w.CB.CreditSpread
	if math.Abs(w.CB.ConsumerLoanRate()-wantConsumer) > 1e-12 {
		t.Errorf("ConsumerLoanRate = %f, want %f", w.CB.ConsumerLoanRate(), wantConsumer)
	}
	// 单步派发器:合法 idx 前进,越界返回错误描述。
	if _, name, _ := RunTransmissionStep(w, 0, w.CB.PolicyRate); name != StepCentralToInterbank {
		t.Errorf("RunTransmissionStep(0) name = %s, want %s", name, StepCentralToInterbank)
	}
	if _, _, desc := RunTransmissionStep(w, 4, 0.03); desc == "" {
		t.Errorf("RunTransmissionStep(4) should return out-of-range description")
	}
	// nil World 安全。
	if TraceTransmission(nil) != nil {
		t.Errorf("TraceTransmission(nil) should return nil")
	}
}

// TestCentralBankStatement_Quarterly 验证央行公告格式(占位文本拼接)。
func TestCentralBankStatement_Quarterly(t *testing.T) {
	w := newV212World()
	stmt := GenerateCentralBankStatement(w, 12)
	if stmt == "" {
		t.Fatalf("GenerateCentralBankStatement returned empty")
	}
	if !strings.HasPrefix(stmt, "央行12月:") {
		t.Errorf("statement prefix mismatch: %q", stmt)
	}
	if GenerateCentralBankStatement(nil, 3) != "" {
		t.Errorf("nil world should return empty statement")
	}
}

// TestCentralBankStatement_WiredIntoMonthlyDecision §130 接线验证:
// MonthlyDecision 每 3 月追加一条「央行公告:」事件(6 月内恰 2 条:3 月/6 月)。
func TestCentralBankStatement_WiredIntoMonthlyDecision(t *testing.T) {
	w := newV212World()
	rng := rand.New(rand.NewSource(7))
	counts := map[int]int{}
	for m := 1; m <= 6; m++ {
		w.Month = m
		w.CB.MonthlyDecision(w, rng)
		for _, ev := range w.Events {
			if ev.Month == m && strings.HasPrefix(ev.Text, "央行公告:") {
				counts[m]++
			}
		}
	}
	if counts[1]+counts[2] != 0 {
		t.Errorf("months 1-2 should have no statement, got %v", counts)
	}
	if counts[3] != 1 || counts[6] != 1 {
		t.Errorf("quarterly statement wiring: counts = %v, want {3:1, 6:1}", counts)
	}
}

// hasEventText 判断事件流中是否存在包含 substr 的记录(测试辅助)。
func hasEventText(w *World, substr string) bool {
	for _, ev := range w.Events {
		if strings.Contains(ev.Text, substr) {
			return true
		}
	}
	return false
}
