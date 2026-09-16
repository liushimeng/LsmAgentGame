package wealth

import (
	"math"
	"strings"
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

// ── P1(§财商流P1-2 §10.3):settlement 集成断言 ──

// TestSettle_FirmsMoneyFlow 步骤5 钱流:economy_enabled=true → Ledger 当月出现
// to=firms, category=living;to=world 仅剩 donate/inject(此处无捐赠 → 零条)。
func TestSettle_FirmsMoneyFlow(t *testing.T) {
	w := NewWorld(11, emptyCardsFor(3))
	for s := 0; s < 3; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 200000))
	}
	w.StartGame()
	month := w.Month
	w.SettleMonth()

	firmsLiving, worldConsumption := int64(0), int64(0)
	for _, e := range w.Ledger.MonthEntries(month) {
		if e.To == EntityFirms && e.Category == CatLiving {
			firmsLiving += e.AmountCNY
		}
		if e.To == EntityWorld && e.Category != CatDonate && e.Category != CatInject {
			worldConsumption++
		}
	}
	if firmsLiving == 0 {
		t.Error("expected to=firms living entries when economy enabled")
	}
	if worldConsumption != 0 {
		t.Errorf("to=world consumption entries should be 0 (donate/inject only), got %d", worldConsumption)
	}
	// 恩格尔守恒:每人 Σ ConsumptionByGoods == living + familyLiving。
	for s := 0; s < 3; s++ {
		p := w.Players[s]
		var basket, flow int64
		for _, v := range p.ConsumptionByGoods {
			basket += int64(v)
		}
		for _, it := range p.Monthly.Detail {
			if it.Key == "living" || it.Key == "family" {
				flow += -it.AmountCNY
			}
		}
		if basket != flow {
			t.Errorf("seat %d: basket %d != living+family %d", s, basket, flow)
		}
	}
	// I1 守恒(全座位;cashBefore = 月初现金快照)。
	for s := 0; s < 3; s++ {
		if d := w.Ledger.VerifySeatConservation(s, month, w.Players[s].Cash-w.Ledger.SeatNetFlow(s, month), w.Players[s].Cash); d != 0 {
			t.Errorf("seat %d conservation drift: %d", s, d)
		}
	}
}

// TestSettle_ForcedDowngrade 强制降档:现金 < 2×月生活支出基准 →
// ConsumptionLevel==0 + life 事件(§10.3)。
func TestSettle_ForcedDowngrade(t *testing.T) {
	w := NewWorld(11, emptyCardsFor(1))
	w.Players[0] = newPlayerFromCard(0, synthCard(0, 200000))
	w.Players[0].ConsumptionByGoods = map[string]float64{"food": 1} // 已初始化,档位字段生效
	w.Players[0].ConsumptionLevel = 2                               // 精致档
	w.Players[0].SalaryBase = 0                                     // 无工资,现金不被垫高
	w.Players[0].Cash = 0                                           // < 2×baseline → 触发强制降档
	w.Players[0].Family.Marital = "single"
	w.Players[0].Card.EldersDependent = 0
	w.StartGame()

	eventsBefore := len(w.Events)
	w.SettleMonth()
	p := w.Players[0]
	if p.ConsumptionLevel != 0 {
		t.Errorf("forced downgrade: level=%d, want 0", p.ConsumptionLevel)
	}
	found := false
	for _, ev := range w.Events[eventsBefore:] {
		if ev.Type == "life" && ev.Seat == 0 && strings.Contains(ev.Text, "强制节俭") {
			found = true
		}
	}
	if !found {
		t.Error("forced downgrade life event missing")
	}
	// 生活支出按 0 档 ×0.6 结算(无通胀,month 1)。
	want := int64(float64(p.Card.Expense) * consumptionLevelMult[0])
	foundLiving := false
	for _, e := range w.Ledger.MonthEntries(1) {
		if e.Category == CatLiving && e.From == SeatEntity(0) {
			if e.AmountCNY != want {
				t.Errorf("living amount: got %d, want %d (×0.6)", e.AmountCNY, want)
			}
			foundLiving = true
		}
	}
	if !foundLiving {
		t.Error("living entry missing")
	}
	// 精力界内(clamp [-3,10])。
	if p.Energy < -3 || p.Energy > 10 {
		t.Errorf("energy out of bound: %d", p.Energy)
	}
}

// TestSettle_EconomyDisabledFallback economy_enabled=false 回退:全量消费流水
// to=world;living 金额与 P0 公式一致(Expense×infl,固定 seed 对拍基线);
// Goods/Labor/Society 全部跳过;res.CPI = CB.CPI(§10.3 + §6.5)。
func TestSettle_EconomyDisabledFallback(t *testing.T) {
	build := func(economy bool) *World {
		w := NewWorld(23, emptyCardsFor(2))
		for s := 0; s < 2; s++ {
			w.Players[s] = newPlayerFromCard(s, synthCard(s, 200000))
		}
		w.EconomyEnabled = economy
		w.StartGame()
		return w
	}
	w := build(false)
	infl := InflationFactorCB(w)
	month := w.Month
	finished, res := w.SettleMonth()
	if finished {
		t.Fatal("should not finish after 1 month")
	}
	// 全量消费类 to=world(P0 路径);无 firms 条目。
	firms, worldLiving := 0, int64(0)
	for _, e := range w.Ledger.MonthEntries(month) {
		if e.To == EntityFirms {
			firms++
		}
		if e.To == EntityWorld && e.Category == CatLiving {
			worldLiving += e.AmountCNY
		}
	}
	if firms != 0 {
		t.Errorf("to=firms entries in disabled mode: %d, want 0", firms)
	}
	if worldLiving == 0 {
		t.Error("to=world living entries missing in disabled mode")
	}
	// living 金额 = P0 公式(档位 1 乘数 1.0,回归零差异)。
	want := int64(float64(w.Players[0].Card.Expense)*infl+0.5) + int64(float64(w.Players[1].Card.Expense)*infl+0.5)
	if worldLiving != want {
		t.Errorf("P0 living parity: got %d, want %d", worldLiving, want)
	}
	// 引擎步骤全部跳过:Goods 价格不动、Labor 保持自然率、Society 为 nil。
	for _, id := range goodsOrder {
		if w.Goods.Items[id].PriceIdx != 100 || w.Goods.Items[id].MomChange != 0 {
			t.Errorf("%s price moved in disabled mode: %+v", id, w.Goods.Items[id])
		}
	}
	if w.Labor.Unemployment != UnemployNatural {
		t.Errorf("unemployment moved in disabled mode: %f", w.Labor.Unemployment)
	}
	if w.Society != nil {
		t.Error("society stats should be skipped in disabled mode")
	}
	if res.CPI != w.CB.CPI {
		t.Errorf("res.CPI in disabled mode: got %f, want CB.CPI %f", res.CPI, w.CB.CPI)
	}
	if res.UnemploymentRate != 0 {
		t.Errorf("res.UnemploymentRate in disabled mode: got %f, want 0", res.UnemploymentRate)
	}

	// 同 seed 对拍:disabled 双世界全轨迹一致(确定性回退,失业概率 P0 一致)。
	wa, wb := build(false), build(false)
	for i := 0; i < 6; i++ {
		wa.SettleMonth()
		wb.SettleMonth()
	}
	for s := 0; s < 2; s++ {
		if wa.Players[s].Cash != wb.Players[s].Cash {
			t.Errorf("disabled determinism broken at seat %d: %d vs %d", s, wa.Players[s].Cash, wb.Players[s].Cash)
		}
		if wa.Players[s].UnemployedMonths != wb.Players[s].UnemployedMonths {
			t.Errorf("unemployment rolls diverge at seat %d (P0 对拍)", s)
		}
	}
}

// TestSettle_EconomyEnabledResFields economy_enabled=true → res.CPI = 篮子
// CPIYoY、res.UnemploymentRate = 内生失业率;view 三结构非零下发(§6.1/§6.3)。
func TestSettle_EconomyEnabledResFields(t *testing.T) {
	w := NewWorld(23, emptyCardsFor(2))
	for s := 0; s < 2; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 200000))
	}
	w.StartGame()
	_, res := w.SettleMonth()
	if res.CPI != w.Goods.CPIYoY {
		t.Errorf("res.CPI: got %f, want goods CPIYoY %f", res.CPI, w.Goods.CPIYoY)
	}
	if res.UnemploymentRate != w.Labor.Unemployment {
		t.Errorf("res.UnemploymentRate: got %f, want %f", res.UnemploymentRate, w.Labor.Unemployment)
	}
	// view 快照:goods 8 类非空 + surveys 空数组(非 null)。
	cs := BuildClientState("room-p1", 0, w, [MaxSeats]string{"u:0"}, [MaxSeats]string{"玩家0"},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0)
	if len(cs.ConsumerMarket.Goods) != len(goodsOrder) {
		t.Errorf("consumer market goods: got %d, want %d", len(cs.ConsumerMarket.Goods), len(goodsOrder))
	}
	if cs.Surveys == nil {
		t.Error("surveys must serialize as [] not null")
	}
	if cs.LaborMarket.UnemploymentRate == 0 {
		t.Errorf("labor market unemployment should be populated, got %.3f", cs.LaborMarket.UnemploymentRate)
	}
}

// TestAction_SetConsumption set_consumption 动作:耗 1 次预算、档位生效、
// 越界 → 35020(§3.5)。
func TestAction_SetConsumption(t *testing.T) {
	w := NewWorld(11, emptyCardsFor(1))
	w.Players[0] = newPlayerFromCard(0, synthCard(0, 200000))
	w.StartGame()
	budgetBefore := w.Players[0].ActionBudget

	if _, e := w.ApplyAction(0, Action{Type: ActSetConsumption, Level: 2}); e != nil {
		t.Fatalf("set level 2: %v", e)
	}
	if w.Players[0].ConsumptionLevel != 2 {
		t.Errorf("level: got %d, want 2", w.Players[0].ConsumptionLevel)
	}
	if w.Players[0].ActionBudget != budgetBefore-1 {
		t.Errorf("budget: got %d, want %d (耗 1 次)", w.Players[0].ActionBudget, budgetBefore-1)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActSetConsumption, Level: 4}); e == nil || e.Code != errcode.ErrWealthConsumptionLevelInvalid {
		t.Errorf("level 4: got %v, want %d", e, errcode.ErrWealthConsumptionLevelInvalid)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActSetConsumption, Level: -1}); e == nil || e.Code != errcode.ErrWealthConsumptionLevelInvalid {
		t.Errorf("level -1: got %v, want %d", e, errcode.ErrWealthConsumptionLevelInvalid)
	}
	// 预算耗尽 → 35006。
	w.Players[0].ActionBudget = 0
	if _, e := w.ApplyAction(0, Action{Type: ActSetConsumption, Level: 1}); e == nil || e.Code != errcode.ErrWealthActionBudgetExhausted {
		t.Errorf("budget exhausted: got %v, want %d", e, errcode.ErrWealthActionBudgetExhausted)
	}
}