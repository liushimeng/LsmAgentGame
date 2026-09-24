// Package virtual_city — insurance_test.go: 商业保险与风险转移引擎单测
// (2026-09-19 §财商流P1-4,契约文档 §12.1 清单)。
//
// 覆盖:投保定价/I1 守恒/校验链(35037-35041)、月结扣缴(断缴→宽限→失效→
// 重投保等待期重算)、年结重定价(CPI 3%/年龄档/economy 关闭不变)、医疗与
// 重疾理赔、等待期、意外 70/30 分支、身故对接(HandleDeath)、I4 insurer
// EntityNet、view 序列化(insured_kinds / my.insurance)、insurance_enabled=false
// rand 序列零偏移回归。
package virtual_city

import (
	"encoding/json"
	"testing"

	"LsmAgentGame/errcode"
)

// insWorld 构造 1 人测试世界(大储蓄防破产噪声;可指定保险开关)。
func insWorld(seed int64, insurance bool) *World {
	w := NewWorld(seed, emptyCardsFor(1))
	w.Players[0] = newPlayerFromCard(0, synthCard(0, 500000))
	w.InsuranceEnabled = insurance
	w.StartGame()
	return w
}

// insPremiumCount 统计 seat 保费条目数。
func insPremiumCount(w *World, seat int) (n int, total int64) {
	for _, e := range w.Ledger.Entries {
		if e.From == SeatEntity(seat) && e.To == EntityInsurer && e.Category == CatPremium {
			n++
			total += e.AmountCNY
		}
	}
	return n, total
}

// insClaimCount 统计 seat 理赔条目数与合计。
func insClaimTotal(w *World, seat int) (n int, total int64) {
	for _, e := range w.Ledger.Entries {
		if e.From == EntityInsurer && e.To == SeatEntity(seat) && e.Category == CatClaim {
			n++
			total += e.AmountCNY
		}
	}
	return n, total
}

// TestInsurance_BuyPricingAndConservation 投保:25 岁四险种首月保费 = 年保费/12
// 且落在《规则》§6.7 区间;Ledger 出现 seat→insurer/premium;Cash 同步扣减(I1)。
func TestInsurance_BuyPricingAndConservation(t *testing.T) {
	w := insWorld(11, true)
	p := w.Players[0]
	want := map[string]struct {
		annual, monthly int64
		low, high       int64
	}{
		InsCriticalIllness: {5000, 417, 3000, 8000},
		InsMedicalMillion:  {1000, 83, 500, 1500},
		InsTermLife:        {2000, 167, 1000, 3000},
		InsAccident:        {350, 29, 200, 500},
	}
	cashBefore := p.Cash
	var sumMonthly int64
	for kind, tc := range want {
		p.ActionBudget = monthlyActionBudget // 4 连投超过月预算 3,逐次重置隔离校验链
		if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: kind}); e != nil {
			t.Fatalf("buy %s: %v", kind, e)
		}
		pol := p.Policies[kind]
		if pol == nil || !pol.Active {
			t.Fatalf("policy %s not created/active", kind)
		}
		if pol.AnnualPremiumCNY != tc.annual {
			t.Errorf("%s annual: got %d, want %d", kind, pol.AnnualPremiumCNY, tc.annual)
		}
		if got := monthlyPremium(pol.AnnualPremiumCNY); got != tc.monthly {
			t.Errorf("%s monthly: got %d, want %d", kind, got, tc.monthly)
		}
		if pol.AnnualPremiumCNY < tc.low || pol.AnnualPremiumCNY > tc.high {
			t.Errorf("%s annual %d outside rule band [%d,%d]", kind, pol.AnnualPremiumCNY, tc.low, tc.high)
		}
		if pol.CoverageCNY != policyDefs[kind].CoverageCNY || pol.StartMonth != w.Month || pol.PaidMonths != 1 || pol.CPIFactor != 1.0 {
			t.Errorf("%s policy fields wrong: %+v", kind, pol)
		}
		sumMonthly += tc.monthly
	}
	// Ledger:4 条 seat→insurer / premium;现金扣减 = Σ月保费(I1 守恒)。
	n, total := insPremiumCount(w, 0)
	if n != 4 || total != sumMonthly {
		t.Errorf("premium entries: got n=%d total=%d, want 4/%d", n, total, sumMonthly)
	}
	if diff := (p.Cash - cashBefore) - w.Ledger.SeatNetFlow(0, w.Month); diff != 0 {
		t.Errorf("I1 broken: cash delta %d vs ledger net %d", p.Cash-cashBefore, w.Ledger.SeatNetFlow(0, w.Month))
	}
}

// TestInsurance_BuyValidation 校验链:35037/35038/35040/35007/35041。
func TestInsurance_BuyValidation(t *testing.T) {
	w := insWorld(12, true)
	p := w.Players[0]
	buy := func(kind string) *errcode.Error {
		p.ActionBudget = monthlyActionBudget // 逐次重置,隔离校验链(月预算 3 不够 5 连投)
		_, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: kind})
		return e
	}
	if e := buy("unit_link"); e == nil || e.Code != errcode.ErrVirtualCityInsuranceKindInvalid {
		t.Errorf("kind invalid: got %v, want 35037", e)
	}
	if e := buy(InsAccident); e != nil {
		t.Fatalf("first buy should pass: %v", e)
	}
	if e := buy(InsAccident); e == nil || e.Code != errcode.ErrVirtualCityInsuranceExists {
		t.Errorf("dup buy: got %v, want 35038", e)
	}
	// 56 岁(w.Month=373 → Age=56)禁止新投保;55 岁仍可。
	w.Month = (55-25)*12 + 1
	if e := buy(InsTermLife); e != nil {
		t.Fatalf("age 55 buy should pass: %v", e)
	}
	w.Month = (56-25)*12 + 1
	if e := buy(InsCriticalIllness); e == nil || e.Code != errcode.ErrVirtualCityInsuranceAgeGate {
		t.Errorf("age 56 buy: got %v, want 35040", e)
	}
	// 现金不足首月保费 → 35007(回到 25 岁,避免年龄门先行短路)。
	w.Month = 1
	p.Cash = 10
	if e := buy(InsCriticalIllness); e == nil || e.Code != errcode.ErrVirtualCityInsufficientCash {
		t.Errorf("insufficient cash: got %v, want 35007", e)
	}
	// insurance_enabled=false → 35041(投保与退保均拒)。
	wd := insWorld(13, false)
	if _, e := wd.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsAccident}); e == nil || e.Code != errcode.ErrVirtualCityInsuranceDisabled {
		t.Errorf("disabled buy: got %v, want 35041", e)
	}
	if _, e := wd.ApplyAction(0, Action{Type: ActCancelInsurance, Kind: InsAccident}); e == nil || e.Code != errcode.ErrVirtualCityInsuranceDisabled {
		t.Errorf("disabled cancel: got %v, want 35041", e)
	}
}

// TestInsurance_CancelZeroValue 退保:零现金价值(无 insurer→seat 条目),
// 立即 Active=false,次月起不再扣缴。
func TestInsurance_CancelZeroValue(t *testing.T) {
	w := insWorld(14, true)
	p := w.Players[0]
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsMedicalMillion}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	cashAtCancel := p.Cash
	if _, e := w.ApplyAction(0, Action{Type: ActCancelInsurance, Kind: InsMedicalMillion}); e != nil {
		t.Fatalf("cancel: %v", e)
	}
	if pol := p.Policies[InsMedicalMillion]; pol == nil || pol.Active {
		t.Error("policy should be inactive after cancel")
	}
	if p.Cash != cashAtCancel {
		t.Errorf("cancel must not change cash: %d vs %d", p.Cash, cashAtCancel)
	}
	if n, _ := insClaimTotal(w, 0); n != 0 {
		t.Errorf("cancel must produce zero claim entries, got %d", n)
	}
	// 退保后当月月结不再扣缴。
	w.SettleMonth()
	if n, _ := insPremiumCount(w, 0); n != 1 {
		t.Errorf("premium entries after cancel+settle: got %d, want 1", n)
	}
	// 重复退保 → 35039。
	if _, e := w.ApplyAction(0, Action{Type: ActCancelInsurance, Kind: InsMedicalMillion}); e == nil || e.Code != errcode.ErrVirtualCityInsuranceNotFound {
		t.Errorf("cancel again: got %v, want 35039", e)
	}
}

// TestInsurance_MonthlyPremiumFlow 月结扣缴:连续 3 月逐月扣、PaidMonths 递增;
// 现金不足首月 → grace;宽限次月仍不足 → lapsed 且不再扣;lapsed 后可重新投保
// (等待期重算)。
func TestInsurance_MonthlyPremiumFlow(t *testing.T) {
	w := insWorld(15, true)
	p := w.Players[0]
	// 月 1 投保(首月即时扣,PaidMonths=1)。
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsCriticalIllness}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	// 月 1 月结:StartMonth==Month 跳过(防双扣)。
	w.SettleMonth()
	if n, total := insPremiumCount(w, 0); n != 1 || total != 417 {
		t.Errorf("settle of purchase month: n=%d total=%d, want 1/417", n, total)
	}
	// 月 2-4:逐月扣缴,PaidMonths 1→4。
	for i := 2; i <= 4; i++ {
		w.SettleMonth()
		if n, _ := insPremiumCount(w, 0); n != i {
			t.Errorf("month %d: premium entries %d, want %d", i, n, i)
		}
	}
	if pol := p.Policies[InsCriticalIllness]; pol.PaidMonths != 4 || pol.Status(w.Month) != "active" {
		t.Errorf("paid months/status: got %d/%s, want 4/active", pol.PaidMonths, pol.Status(w.Month))
	}
	// 现金不足 → 宽限期(保障仍有效,本月不扣)。直调 SettlePremiums 隔离
	// 月结工资收入(完整 SettleMonth 会先发工资,无法构造断缴)。
	p.Cash = 100
	w.SettlePremiums(p)
	pol := p.Policies[InsCriticalIllness]
	if !pol.GraceActive || pol.Status(w.Month) != "grace" {
		t.Errorf("first missed premium should enter grace: %+v", pol)
	}
	if n, _ := insPremiumCount(w, 0); n != 4 {
		t.Errorf("grace month must not deduct, entries=%d", n)
	}
	// 宽限期次月仍不足 → 失效;之后不再扣缴。
	w.Month++
	p.Cash = 100
	w.SettlePremiums(p)
	if pol.Active || pol.Status(w.Month) != "lapsed" {
		t.Errorf("second missed premium should lapse: %+v", pol)
	}
	w.Month++
	w.SettlePremiums(p)
	w.Month++
	w.SettlePremiums(p)
	if n, _ := insPremiumCount(w, 0); n != 4 {
		t.Errorf("lapsed policy must stop deducting, entries=%d", n)
	}
	// 失效后可重新投保,等待期重新计算(重疾 3 月)。
	p.Cash = 500000
	p.ActionBudget = monthlyActionBudget
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsCriticalIllness}); e != nil {
		t.Fatalf("rebuy after lapse: %v", e)
	}
	pol2 := p.Policies[InsCriticalIllness]
	if pol2.StartMonth != w.Month || pol2.PaidMonths != 1 || pol2.Status(w.Month) != "waiting" {
		t.Errorf("rebuy policy should restart waiting: start=%d paid=%d status=%s",
			pol2.StartMonth, pol2.PaidMonths, pol2.Status(w.Month))
	}
}

// TestInsurance_AnnualReprice 年结重定价:CPIYoY=3% 时次年保费 =
// clamp(Base×ageMult×1.03, Low, High×1.03);45 岁档 ×1.6;
// CPI 未就绪/economy 关闭 → 保费不变。
func TestInsurance_AnnualReprice(t *testing.T) {
	// 直接驱动 RepricePolicies(AnnualAdjust 同一入口,公式级单测)。
	w := insWorld(16, true)
	p := w.Players[0]
	// Goods 未就绪(无 12 月历史)→ CPI 联动为 0,保费不变。
	p.Policies = map[string]*Policy{InsCriticalIllness: {
		Kind: InsCriticalIllness, AnnualPremiumCNY: 5000, StartMonth: 1, Active: true, CPIFactor: 1.0,
	}}
	w.RepricePolicies(p)
	if got := p.Policies[InsCriticalIllness].AnnualPremiumCNY; got != 5000 {
		t.Errorf("goods not ready: premium %d, want 5000 (unchanged)", got)
	}
	// 就绪 + CPIYoY=3%(25 岁档 mult=1.0):5000×1.03=5150(区间内)。
	for i := 0; i < 13; i++ {
		w.GoodsMonthStep() // 积累 ≥12 月环比历史
	}
	w.Goods.CPIYoY = 0.03
	w.Month = 2 // 年龄 25 不变档
	w.RepricePolicies(p)
	pol := p.Policies[InsCriticalIllness]
	if pol.CPIFactor < 1.029 || pol.CPIFactor > 1.031 {
		t.Errorf("CPIFactor after 3%% repricing: %f, want ≈1.03", pol.CPIFactor)
	}
	if got := pol.AnnualPremiumCNY; got != 5150 {
		t.Errorf("repriced premium: got %d, want 5150", got)
	}
	// 45 岁档 ×1.6:5000×1.6=8000(CPIFactor=1 时 clamp 至区间上限 8000)。
	w2 := insWorld(17, true)
	p2 := w2.Players[0]
	w2.Month = (45-25)*12 + 1
	p2.Policies = map[string]*Policy{InsCriticalIllness: {
		Kind: InsCriticalIllness, AnnualPremiumCNY: 5000, StartMonth: 1, Active: true, CPIFactor: 1.0,
	}}
	w2.RepricePolicies(p2)
	if got := p2.Policies[InsCriticalIllness].AnnualPremiumCNY; got != 8000 {
		t.Errorf("age-45 premium: got %d, want 8000 (5000×1.6 clamp 8000)", got)
	}
	// 50 岁档 ×2.0 → clamp 8000(§3.3 校验样例)。
	if got := priceAnnual(policyDefs[InsCriticalIllness], 50, 1.0); got != 8000 {
		t.Errorf("age-50 pricing: got %d, want 8000", got)
	}
	// economy_enabled=false → 保费不变(CPI 不联动)。
	w3 := insWorld(18, true)
	w3.EconomyEnabled = false
	p3 := w3.Players[0]
	p3.Policies = map[string]*Policy{InsMedicalMillion: {
		Kind: InsMedicalMillion, AnnualPremiumCNY: 1000, StartMonth: 1, Active: true, CPIFactor: 1.0,
	}}
	w3.RepricePolicies(p3)
	if got := p3.Policies[InsMedicalMillion].AnnualPremiumCNY; got != 1000 {
		t.Errorf("economy off: premium %d, want 1000 (unchanged)", got)
	}
	if f := p3.Policies[InsMedicalMillion].CPIFactor; f != 1.0 {
		t.Errorf("economy off: CPIFactor %f, want 1.0", f)
	}
}

// TestInsurance_MedicalClaims 医疗理赔:cost=120,000(isMajor)+ 双险有效 →
// insurer→seat = 400,000 + 108,000 两条 claim;重疾保单终止;ClaimsTotalCNY 累加。
func TestInsurance_MedicalClaims(t *testing.T) {
	w := insWorld(19, true)
	p := w.Players[0]
	// 月 1 双险投保,推进到月 5(重疾等待期 3 月已过)。
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsCriticalIllness}); e != nil {
		t.Fatalf("buy ci: %v", e)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsMedicalMillion}); e != nil {
		t.Fatalf("buy mm: %v", e)
	}
	for w.Month < 5 {
		w.SettleMonth()
	}
	cashBefore := p.Cash
	w.SettleMedicalClaims(0, 120000, true)
	n, total := insClaimTotal(w, 0)
	if n != 2 || total != 400000+108000 {
		t.Errorf("claims: n=%d total=%d, want 2/508000", n, total)
	}
	ci := p.Policies[InsCriticalIllness]
	mm := p.Policies[InsMedicalMillion]
	if ci.Active || ci.Status(w.Month) != "lapsed" {
		t.Error("critical illness policy must terminate after payout")
	}
	if ci.ClaimsTotalCNY != 400000 {
		t.Errorf("ci claims total: %d, want 400000", ci.ClaimsTotalCNY)
	}
	if !mm.Active || mm.ClaimsTotalCNY != 108000 {
		t.Errorf("mm policy: active=%v claims=%d, want true/108000", mm.Active, mm.ClaimsTotalCNY)
	}
	if p.Cash-cashBefore != 508000 {
		t.Errorf("cash delta: %d, want 508000", p.Cash-cashBefore)
	}
}

// TestInsurance_WaitingPeriod 等待期:重疾 3 月内出险不赔(零 claim 条目);
// 意外险无等待期当月生效。
func TestInsurance_WaitingPeriod(t *testing.T) {
	w := insWorld(20, true)
	p := w.Players[0]
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsCriticalIllness}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsAccident}); e != nil {
		t.Fatalf("buy accident: %v", e)
	}
	if got := p.Policies[InsCriticalIllness].Status(w.Month); got != "waiting" {
		t.Errorf("ci status in waiting: %s", got)
	}
	if got := p.Policies[InsAccident].Status(w.Month); got != "active" {
		t.Errorf("accident status (no waiting): %s", got)
	}
	w.SettleMedicalClaims(0, 150000, true)
	if n, _ := insClaimTotal(w, 0); n != 0 {
		t.Errorf("waiting-period claim must pay 0, got %d entries", n)
	}
}

// findAccidentSeed 经验搜索:找一个使 MonthlyEvents 触发意外且落入指定分支的
// 种子(1 人无工资座位 → MonthlyEvents 只剩意外掷骰;NewWorld 构造期 NewMarket
// 已消费部分 rand,不能用裸 rand 源预演,直接跑真实世界判定分支)。
func findAccidentSeed(t *testing.T, wantDeath bool) int64 {
	t.Helper()
	for seed := int64(1); seed <= 200000; seed++ {
		w := NewWorld(seed, emptyCardsFor(1))
		card := synthCard(0, 500000)
		card.Salary = 0
		w.Players[0] = newPlayerFromCard(0, card)
		w.StartGame()
		w.MonthlyEvents()
		p := w.Players[0]
		if !p.Alive {
			if wantDeath {
				return seed
			}
			continue
		}
		if p.StoppedMonths >= 1 && !wantDeath {
			return seed
		}
	}
	t.Fatalf("no seed found for wantDeath=%v", wantDeath)
	return 0
}

// TestInsurance_AccidentInjury 意外伤残(70% 分支):医疗支出 U(20,000,50,000)
// + 意外险给付 250,000 + 精力 −3 + StoppedMonths ≥1。
func TestInsurance_AccidentInjury(t *testing.T) {
	seed := findAccidentSeed(t, false)
	w := NewWorld(seed, emptyCardsFor(1))
	card := synthCard(0, 500000)
	card.Salary = 0 // 消除失业掷骰 rand 干扰:MonthlyEvents 只剩意外掷骰
	p := newPlayerFromCard(0, card)
	w.Players[0] = p
	w.StartGame()
	// 精力置 0,便于断言 −3 clamp;投保意外险(无等待期,当月可赔)。
	// I1 快照须含投保扣费(SeatNetFlow 按月聚合,不区分时点)。
	p.Energy = 0
	cashBefore := p.Cash
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsAccident}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	w.MonthlyEvents()
	if !p.Alive {
		t.Fatal("injury branch must not kill")
	}
	pol := p.Policies[InsAccident]
	if pol.ClaimsTotalCNY != 250000 {
		t.Errorf("accident disability payout: %d, want 250000", pol.ClaimsTotalCNY)
	}
	// Ledger:1 条医疗(seat→firms/world)+ 1 条 claim(insurer→seat 250,000)。
	n, total := insClaimTotal(w, 0)
	if n != 1 || total != 250000 {
		t.Errorf("claim entries: n=%d total=%d, want 1/250000", n, total)
	}
	medTotal := int64(0)
	for _, e := range w.Ledger.Entries {
		if e.Category == CatMedical {
			medTotal += e.AmountCNY
		}
	}
	if medTotal < accidentInjuryLowCNY || medTotal > accidentInjuryLowCNY+accidentInjurySpanCNY-1 {
		t.Errorf("medical cost %d outside U(20000,50000)", medTotal)
	}
	if p.Energy != -3 {
		t.Errorf("energy after injury: %d, want -3", p.Energy)
	}
	if p.StoppedMonths < 1 {
		t.Errorf("stopped months: %d, want >=1", p.StoppedMonths)
	}
	if diff := (p.Cash - cashBefore) - w.Ledger.SeatNetFlow(0, w.Month); diff != 0 {
		t.Errorf("I1 broken: %d", diff)
	}
}

// TestInsurance_AccidentDeath 意外身故(30% 分支)→ HandleDeath:双险有效 →
// Cash += 1,500,000;Alive=false、Ending="accident_death"。
func TestInsurance_AccidentDeath(t *testing.T) {
	seed := findAccidentSeed(t, true)
	w := NewWorld(seed, emptyCardsFor(1))
	card := synthCard(0, 500000)
	card.Salary = 0
	p := newPlayerFromCard(0, card)
	w.Players[0] = p
	w.StartGame()
	for _, kind := range []string{InsTermLife, InsAccident} {
		if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: kind}); e != nil {
			t.Fatalf("buy %s: %v", kind, e)
		}
	}
	// 寿险等待期 3 月:推进到生效后(>3 月)再触发。
	for w.Month < 5 {
		w.Month++
	}
	cashBefore := p.Cash
	w.MonthlyEvents()
	if p.Alive {
		t.Fatal("death branch must kill")
	}
	if p.Ending != EndingAccidentDeath {
		t.Errorf("ending: %s, want %s", p.Ending, EndingAccidentDeath)
	}
	if got := p.Cash - cashBefore; got != 1500000 {
		t.Errorf("death payout into estate: %d, want 1500000", got)
	}
	n, total := insClaimTotal(w, 0)
	if n != 2 || total != 1500000 {
		t.Errorf("death claims: n=%d total=%d, want 2/1500000", n, total)
	}
	// 身故座位月结跳过(不再扣缴保费):投保 2 笔,身后月结零新增。
	w.SettleMonth()
	if n, _ := insPremiumCount(w, 0); n != 2 {
		t.Errorf("premium entries after death settle: %d, want 2 (no deduction after death)", n)
	}
}

// TestInsurance_HandleDeathDirect HandleDeath 直测:等待期内身故不赔不退;
// 破产出局(eliminate)不触发任何寿险赔付。
func TestInsurance_HandleDeathDirect(t *testing.T) {
	w := insWorld(21, true)
	p := w.Players[0]
	// 等待期(月 1 投保,寿险等 3 月)内身故 → 零赔付。
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsTermLife}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	cashBefore := p.Cash
	w.HandleDeath(0, "意外身故")
	if p.Cash != cashBefore {
		t.Errorf("waiting-period death must pay 0, delta %d", p.Cash-cashBefore)
	}
	if p.Alive || p.Ending != EndingAccidentDeath {
		t.Errorf("death registration wrong: alive=%v ending=%s", p.Alive, p.Ending)
	}

	// 破产出局不是身故:eliminate 不触发赔付。
	w2 := insWorld(22, true)
	p2 := w2.Players[0]
	if _, e := w2.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsTermLife}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	for w2.Month < 5 {
		w2.SettleMonth()
	}
	cash := p2.Cash
	w2.eliminate(p2, "破产出局")
	if p2.Cash != cash || p2.Ending != EndingBankrupt {
		t.Errorf("bankrupt elimination must not trigger claims: cash %d→%d ending %s", cash, p2.Cash, p2.Ending)
	}
	if n, _ := insClaimTotal(w2, 0); n != 0 {
		t.Errorf("bankrupt elimination claims: %d, want 0", n)
	}
}

// TestInsurance_EntityNetInsurer I4:EntityNet("insurer") = Σpremium − Σclaim
// (全月结后核算;insurer 净额 = 保险公司利润)。
func TestInsurance_EntityNetInsurer(t *testing.T) {
	w := insWorld(23, true)
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsMedicalMillion}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	for w.Month < 5 {
		w.SettleMonth()
	}
	w.SettleMedicalClaims(0, 100000, false) // 报销 90,000(非重疾)
	var prem, claim int64
	for _, e := range w.Ledger.Entries {
		if e.Category == CatPremium {
			prem += e.AmountCNY
		}
		if e.Category == CatClaim {
			claim += e.AmountCNY
		}
	}
	if got := w.Ledger.EntityNet(EntityInsurer); got != prem-claim {
		t.Errorf("EntityNet(insurer): %d, want prem(%d)−claim(%d)=%d", got, prem, claim, prem-claim)
	}
}

// TestInsurance_ViewJSON view 序列化:my.insurance 逐键对齐 §8.2;
// insured_kinds 仅含 Active 险种;insurance_enabled=false 时 insurance 段 omit。
func TestInsurance_ViewJSON(t *testing.T) {
	w := insWorld(24, true)
	p := w.Players[0]
	if _, e := w.ApplyAction(0, Action{Type: ActBuyInsurance, Kind: InsAccident}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	ins := w.buildInsuranceJSON(p)
	if ins == nil || len(ins.Policies) != 1 || len(ins.Quotes) != 3 {
		t.Fatalf("insurance json shape: %+v", ins)
	}
	pol := ins.Policies[0]
	if pol.Kind != InsAccident || pol.AnnualPremiumCNY != 350 || pol.MonthlyPremiumCNY != 29 ||
		pol.CoverageCNY != 500000 || pol.StartMonth != 1 || pol.PaidMonths != 1 ||
		pol.WaitingLeft != 0 || pol.Status != "active" || pol.ClaimsTotalCNY != 0 {
		t.Errorf("policy json fields: %+v", pol)
	}
	if ins.MonthlyPremium != 29 {
		t.Errorf("monthly premium sum: %d, want 29", ins.MonthlyPremium)
	}
	// quotes 覆盖 3 个未投保险种。
	quoteKinds := map[string]bool{}
	for _, q := range ins.Quotes {
		quoteKinds[q.Kind] = true
		if q.AnnualPremiumCNY == 0 {
			t.Errorf("quote %s zero annual premium: %+v", q.Kind, q)
		}
		// 百万医疗是比例报销型,CoverageCNY=0 合法;其余三险保额必须 >0。
		if q.Kind != InsMedicalMillion && q.CoverageCNY == 0 {
			t.Errorf("quote %s zero coverage: %+v", q.Kind, q)
		}
	}
	for _, k := range []string{InsCriticalIllness, InsMedicalMillion, InsTermLife} {
		if !quoteKinds[k] {
			t.Errorf("quote missing kind %s", k)
		}
	}
	// insured_kinds 仅含 Active。
	if kinds := w.insuredKindsOf(p); len(kinds) != 1 || kinds[0] != InsAccident {
		t.Errorf("insured kinds: %v", kinds)
	}
	// JSON wire 键名对齐(§8.2)。
	bs, err := json.Marshal(ins)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(bs, &m)
	for _, key := range []string{"policies", "monthly_premium", "quotes"} {
		if _, ok := m[key]; !ok {
			t.Errorf("insurance json missing key %s", key)
		}
	}
	// insurance_enabled=false → my.insurance omit + insured_kinds 恒空。
	wd := insWorld(25, false)
	pd := wd.Players[0]
	if kinds := wd.insuredKindsOf(pd); len(kinds) != 0 {
		t.Errorf("disabled insured kinds: %v", kinds)
	}
	my := &MyJSON{}
	bs2, _ := json.Marshal(my)
	if containsKey(bs2, "insurance") {
		t.Error("nil Insurance must be omitted from MyJSON wire")
	}
}

// containsKey 判断 JSON 字节串是否包含裸键 "key":。
func containsKey(b []byte, key string) bool {
	needle := `"` + key + `":`
	for i := 0; i+len(needle) <= len(b); i++ {
		if string(b[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// TestInsurance_DisabledRandZeroOffset insurance_enabled=false 时 MonthlyEvents
// rand 序列零偏移(回归):无工资座位 → 失业分支零消费;关闭时 MonthlyEvents
// 前后 rand 流与「未调用」完全一致;开启时则额外消费(每存活玩家 1 掷)。
func TestInsurance_DisabledRandZeroOffset(t *testing.T) {
	// 关闭:MonthlyEvents 不消费任何 rand。
	wa := NewWorld(31, emptyCardsFor(0))
	pa := newPlayerFromCard(0, synthCard(0, 100000))
	pa.SalaryBase = 0
	wa.Players[0] = pa
	wa.StartGame()
	wa.InsuranceEnabled = false
	wb := NewWorld(31, emptyCardsFor(0))
	pb := newPlayerFromCard(0, synthCard(0, 100000))
	pb.SalaryBase = 0
	wb.Players[0] = pb
	wb.StartGame()
	wb.InsuranceEnabled = false
	wa.MonthlyEvents()
	for i := 0; i < 4; i++ {
		if a, b := wa.Rand.Float64(), wb.Rand.Float64(); a != b {
			t.Fatalf("disabled MonthlyEvents consumed rand (draw %d: %v vs %v)", i, a, b)
		}
	}
	// 双世界全轨迹对拍(同 settlement_test 零偏移惯例)。
	wc := insWorld(32, false)
	wd2 := insWorld(32, false)
	for i := 0; i < 6; i++ {
		wc.SettleMonth()
		wd2.SettleMonth()
	}
	if wc.Players[0].Cash != wd2.Players[0].Cash {
		t.Errorf("disabled determinism broken: %d vs %d", wc.Players[0].Cash, wd2.Players[0].Cash)
	}
	// 开启时确实掷骰(引擎生效证明,非死代码):同种子「调用过 MonthlyEvents」
	// 与「未调用」的 rand 流必须偏离(相等概率 ≈ 2^-53,工程上确定性)。
	wf := NewWorld(31, emptyCardsFor(0))
	pf := newPlayerFromCard(0, synthCard(0, 100000))
	pf.SalaryBase = 0
	wf.Players[0] = pf
	wf.StartGame()
	wf.MonthlyEvents()
	wf2 := NewWorld(31, emptyCardsFor(0))
	pf2 := newPlayerFromCard(0, synthCard(0, 100000))
	pf2.SalaryBase = 0
	wf2.Players[0] = pf2
	wf2.StartGame()
	if wf.Rand.Float64() == wf2.Rand.Float64() {
		t.Error("enabled engine should consume rand in MonthlyEvents (accident roll missing?)")
	}
}
