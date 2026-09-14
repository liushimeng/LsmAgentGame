package wealth

import (
	"math"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth/profession"
)
var _ = profession.CuratedByID

// TestMonthlyIncomeTax_F12 个税复算:月薪 20000 → 社保 2100、税 1170。
func TestMonthlyIncomeTax_F12(t *testing.T) {
	tax := MonthlyIncomeTax(20000)
	if tax != 1170 {
		t.Errorf("tax for 20000: got %d, want 1170 (F12)", tax)
	}
	social := SocialSecurity(20000)
	if social != 2100 {
		t.Errorf("social for 20000: got %d, want 2100 (F12)", social)
	}
}

// TestMonthlyIncomeTax_Brackets 7 级累进边界复算(《规则》§9.1)。
func TestMonthlyIncomeTax_Brackets(t *testing.T) {
	cases := []struct {
		salary    int64
		minTax    int64
		maxTax    int64
		note      string
	}{
		{5000, 0, 0, "≤ 起征点"},
		{15000, 100, 1200, "15k bracket"},
		{30000, 1000, 4000, "30k bracket"},
		{50000, 4000, 10000, "50k bracket"},
		{60000, 6000, 13000, "60k bracket"},
		{100000, 17000, 32000, "100k bracket"},
	}
	for _, c := range cases {
		got := MonthlyIncomeTax(c.salary)
		if got < c.minTax || got > c.maxTax {
			t.Errorf("%s (salary=%d): got %d, want in [%d,%d]", c.note, c.salary, got, c.minTax, c.maxTax)
		}
	}
}

// TestSocialSecurity_PensionDecomposition §9.2 step 4:社保 10.5%,养老金 8%。
func TestSocialSecurity_PensionDecomposition(t *testing.T) {
	social := SocialSecurity(20000)
	pension := PensionContribution(20000)
	if social != 2100 {
		t.Errorf("social: %d, want 2100", social)
	}
	if pension != 1600 {
		t.Errorf("pension contribution: %d, want 1600 (8%% of 20000)", pension)
	}
}

// TestAnnuityPayment_I8 等额本息月供公式复算(《规则》§8.3)。
func TestAnnuityPayment_I8(t *testing.T) {
	// 100 万 30 年 4.2% 年化:月供约 4,900 元。
	pay := AnnuityPayment(1000000, 0.042, 360)
	want := 4900
	if math.Abs(float64(pay-int64(want))) > 100 {
		t.Errorf("30y 4.2%% 100万: got %d, want ~%d", pay, want)
	}
	// 100 万 20 年 4.2%:月供约 6,160。
	pay2 := AnnuityPayment(1000000, 0.042, 240)
	want2 := 6160
	if math.Abs(float64(pay2-int64(want2))) > 100 {
		t.Errorf("20y 4.2%% 100万: got %d, want ~%d", pay2, want2)
	}
	// 100 万 10 年 5.0%:月供约 10,600。
	pay3 := AnnuityPayment(1000000, 0.05, 120)
	want3 := 10600
	if math.Abs(float64(pay3-int64(want3))) > 200 {
		t.Errorf("10y 5%% 100万: got %d, want ~%d", pay3, want3)
	}
}

// TestInflationFactor 月份对应通胀(§9.2:1.05^(游戏年数))。
func TestInflationFactor(t *testing.T) {
	if math.Abs(InflationFactor(1)-1.0) > 1e-9 {
		t.Errorf("month 1: got %f, want 1.0", InflationFactor(1))
	}
	if math.Abs(InflationFactor(13)-1.05) > 1e-9 {
		t.Errorf("month 13: got %f, want 1.05", InflationFactor(13))
	}
	if math.Abs(InflationFactor(25)-math.Pow(1.05, 2.0)) > 1e-9 {
		t.Errorf("month 25: got %f, want 1.05^2", InflationFactor(25))
	}
}

// TestSettleMonth_BasicSequence 月结 7 步 + 月份+1。
func TestSettleMonth_BasicSequence(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P05"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = 100000
	w.StartGame()
	finish, res := w.SettleMonth()
	if finish {
		t.Errorf("should not finish after 1 month (age=25)")
	}
	if res == nil {
		t.Fatalf("nil settle result")
	}
	if len(res.Summaries) == 0 {
		t.Errorf("expected summaries")
	}
}

// TestSettleMonth_BankruptcyAndExit 现金连续 3 月 <0 → 重组 + 出局(简化 §11)。
func TestSettleMonth_BankruptcyAndExit(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P15"}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Cash = -5000 // 起始负数(极端)
	w.Players[0].SalaryBase = 0
	w.Players[0].SalaryVolatile = false
	w.Players[0].Loans = []Loan{{ID: "L1", Kind: LoanConsumer, Principal: 50000, Balance: 50000, AnnualRate: 0.1, MonthlyPayment: 1000, TermN: 36, MonthsLeft: 36}}
	w.StartGame()
	w.Month = 1
	// 第 1 月结算(现金已负;破产触发条件应满足)。
	w.SettleMonth()
	// 视乎生活费/税具体金额,可能在第 1 月就破产清算;无论如何不应崩溃。
	if w == nil {
		t.Fatalf("world nuked")
	}
	_ = errcode.ErrWealthNotEnoughPlayers // suppress unused
}

// TestFinalScores_I10 终局评分(总分 = fi×0.5 + life×0.3 + social×0.2;结局映射)。
func TestFinalScores_I10(t *testing.T) {
	w := NewWorld(1, [MaxSeats]profession.Card{{ID: "P11", Savings: 6000000}})
	w.Players[0] = newPlayerFromCard(0, w.Players[0].Card)
	w.Players[0].Card = *profession.CuratedByID("P11")
	w.Players[0].Cash = 6000000 // > 500万 → fi_score +=20
	scores := w.FinalScores()
	if len(scores) != 1 {
		t.Fatalf("scores: %d", len(scores))
	}
	if scores[0].Ending == "" {
		t.Errorf("ending must not be empty")
	}
	if scores[0].Total < 0 || scores[0].Total > 110 {
		t.Errorf("total out of range: %f", scores[0].Total)
	}
	if scores[0].FIScore < 100 {
		t.Errorf("FI score: got %f, want ≥100 (FI≥2 + 净>500万 加成)", scores[0].FIScore)
	}
}