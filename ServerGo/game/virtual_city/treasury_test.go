// Package virtual_city — treasury_test.go: 政府财政子系统阶段4 单测
// (2026-09-21 §城市扩张v2.12 阶段4)。
//
// 覆盖:国库初值 / 税收汇总 / 转移支付四类(等待期/递减/低保/儿童津贴)/
// 预算比例 / 支出上限 / 国债发行 / R4-1 不向央行透支 / SettleMonth 接线 /
// 确定性。纯引擎测试:不启动房间、不触 LLM。
package virtual_city

import (
	"math"
	"testing"
)

// treasuryWorld 构造 n 座位测试世界(salary 10000,与 ledger_test 的 synthCard
// 同源;不跑 SettleMonth,由各用例自行驱动)。
func treasuryWorld(seed int64, n int) *World {
	w := NewWorld(seed, emptyCardsFor(0))
	for s := 0; s < n; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 50000))
	}
	w.StartGame()
	return w
}

// 1. TestTreasuryInitialState 初始国库:现金 500 万,无国债,无历史,聚合器非 nil。
func TestTreasuryInitialState(t *testing.T) {
	tr := NewTreasury()
	if tr.Cash != TreasuryInitialCash {
		t.Errorf("initial cash = %d, want %d", tr.Cash, TreasuryInitialCash)
	}
	if tr.Cash != 5_000_000 {
		t.Errorf("initial cash = %d, want 5,000,000", tr.Cash)
	}
	if tr.BondsOutstanding != 0 || len(tr.Bonds) != 0 {
		t.Errorf("initial bonds should be empty: outstanding=%d n=%d", tr.BondsOutstanding, len(tr.Bonds))
	}
	if len(tr.History) != 0 || tr.DeficitRun != 0 {
		t.Errorf("initial history/deficit run should be zero: %d/%d", len(tr.History), tr.DeficitRun)
	}
	if tr.MonthlyCounter == nil {
		t.Error("MonthlyCounter must be non-nil")
	}
	// NewWorld 接线:World.Treasury 非 nil 且为全新国库。
	w := NewWorld(1, emptyCardsFor(0))
	if w.Treasury == nil || w.Treasury.Cash != TreasuryInitialCash {
		t.Errorf("NewWorld should wire a fresh Treasury, got %+v", w.Treasury)
	}
}

// 2. TestTreasuryCollectTax 12 玩家月税汇总:个税/社保逐座位累加,增值税/
// 企业所得税按 Labor 口径,总税收入国库。
func TestTreasuryCollectTax(t *testing.T) {
	w := treasuryWorld(7, 12)
	// 模拟月结步骤4 已缴快照:salary 10000 → tax/social 由引擎纯函数给出。
	salary := int64(10000)
	tax, social := MonthlyIncomeTax(salary), SocialSecurity(salary)
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		p.Monthly.Tax = tax
		p.Monthly.Social = social
	}
	w.Labor.RevenueCNY = 1_000_000
	w.Labor.WageBillCNY = 120_000

	before := w.Treasury.Cash
	total := w.Treasury.CollectMonthTax(w)
	c := w.Treasury.MonthlyCounter

	wantIncome := int64(12) * tax
	wantSocial := int64(12) * social
	wantVat := int64(60_000)   // 1,000,000 × 6% = 60,000(+0.5 截断)
	wantCorp := int64(132_000) // (1,000,000−120,000) × 15% = 132,000
	if c.IncomeTaxTotal != wantIncome {
		t.Errorf("income tax total = %d, want %d", c.IncomeTaxTotal, wantIncome)
	}
	if c.SocialSecurityTot != wantSocial {
		t.Errorf("social security total = %d, want %d", c.SocialSecurityTot, wantSocial)
	}
	if c.VatTotal != wantVat {
		t.Errorf("vat = %d, want %d", c.VatTotal, wantVat)
	}
	if c.CorporateTaxTotal != wantCorp {
		t.Errorf("corporate tax = %d, want %d", c.CorporateTaxTotal, wantCorp)
	}
	wantTotal := wantIncome + wantSocial + wantVat + wantCorp
	if total != wantTotal {
		t.Errorf("total tax = %d, want %d", total, wantTotal)
	}
	if w.Treasury.Cash != before+wantTotal {
		t.Errorf("treasury cash %d, want %d (+%d)", w.Treasury.Cash, before+wantTotal, wantTotal)
	}
	// 二次调用清零重算(月度聚合器不跨月累积)。
	if again := w.Treasury.CollectMonthTax(w); again != wantTotal {
		t.Errorf("re-collect should recompute from zero: %d, want %d", again, wantTotal)
	}
}

// 3. TestTransferUnemployment_WaitingPeriod 等待期(R4-3):累计失业 1–3 月不发。
// 计发语义:payUnemployment 先推进计数(+1)再按**新计数**计系数 ——
// 第 1/2/3 个失业月(预置 0/1/2 → 计发时 1/2/3)待遇为 0。
func TestTransferUnemployment_WaitingPeriod(t *testing.T) {
	for pre := 0; pre < UnemploymentWaitMonths; pre++ {
		w := treasuryWorld(3, 1)
		p := w.Players[0]
		p.UnemployedMonths = 4 // 仍处失业期
		p.UnemployedAccumMonths = pre
		cashBefore := w.Treasury.Cash
		if got := w.Treasury.payUnemployment(w, p); got != 0 {
			t.Errorf("month %d of spell: waiting period must pay 0, got %d", pre+1, got)
		}
		if w.Treasury.Cash != cashBefore {
			t.Errorf("month %d of spell: treasury cash changed during waiting period", pre+1)
		}
		if p.UnemployedAccumMonths != pre+1 {
			t.Errorf("month %d of spell: accum should advance to %d, got %d", pre+1, pre+1, p.UnemployedAccumMonths)
		}
	}
	// 在职玩家:不累计不计发。
	w := treasuryWorld(3, 1)
	p := w.Players[0]
	p.UnemployedMonths = 0
	if got := w.Treasury.payUnemployment(w, p); got != 0 {
		t.Errorf("employed player must get 0, got %d", got)
	}
	if p.UnemployedAccumMonths != 0 {
		t.Errorf("employed player accum must stay 0, got %d", p.UnemployedAccumMonths)
	}
}

// 4. TestTransferUnemployment_DecayAndFullBand 系数带:4–12 月 = 1000(全额);
// >12 月 = 700(0.7 递减)。
func TestTransferUnemployment_DecayAndFullBand(t *testing.T) {
	full := int64(1000) // 基本生活成本 2000 × 50%(+0.5 截断)
	// 第 4–12 个失业月:全额 1000(预置 3/7/11 → 计发时 4/8/12,抽样首尾)。
	for pre := range map[int]bool{3: true, 7: true, 11: true} {
		w := treasuryWorld(3, 1)
		p := w.Players[0]
		p.UnemployedMonths = 2
		p.UnemployedAccumMonths = pre
		if got := w.Treasury.payUnemployment(w, p); got != full {
			t.Errorf("month %d of spell: want full %d, got %d", pre+1, full, got)
		}
	}
	// 第 13/14 个失业月(>12):递减 0.7 → 700。
	for pre := UnemploymentDecayFrom; pre <= UnemploymentDecayFrom+1; pre++ {
		w := treasuryWorld(3, 1)
		p := w.Players[0]
		p.UnemployedMonths = 2
		p.UnemployedAccumMonths = pre
		want := int64(float64(full) * UnemploymentDecayFactor)
		if got := w.Treasury.payUnemployment(w, p); got != want {
			t.Errorf("month %d of spell: want decayed %d, got %d", pre+1, want, got)
		}
	}
	// 累计计数推进:仍失业 → accum+1。
	w := treasuryWorld(3, 1)
	p := w.Players[0]
	p.UnemployedMonths = 5
	p.UnemployedAccumMonths = 4
	w.Treasury.payUnemployment(w, p)
	if p.UnemployedAccumMonths != 5 {
		t.Errorf("accum should advance to 5, got %d", p.UnemployedAccumMonths)
	}
}

// 5. TestTransferLowIncome 低保:补差 = max(0, 1200 − 收入) × 50%;
// R4-3 门槛:累计失业 ≥ 3 月;收入 ≥ 贫困线 → 0。
func TestTransferLowIncome(t *testing.T) {
	// 门槛未到(accum=2):收入再低也不发。
	w := treasuryWorld(5, 1)
	p := w.Players[0]
	p.UnemployedAccumMonths = LowIncomeMinUnemployed - 1
	p.Monthly.Income = 0
	if got := w.Treasury.payLowIncome(w, p); got != 0 {
		t.Errorf("below unemployment threshold must pay 0, got %d", got)
	}
	// 补差:收入 400 → (1200−400)×50% = 400。
	p.UnemployedAccumMonths = LowIncomeMinUnemployed
	p.Monthly.Income = 400
	if got := w.Treasury.payLowIncome(w, p); got != 400 {
		t.Errorf("low income subsidy = %d, want 400", got)
	}
	// 收入 0 → 600;收入 ≥ 1200 → 0。
	p.Monthly.Income = 0
	if got := w.Treasury.payLowIncome(w, p); got != 600 {
		t.Errorf("zero income subsidy = %d, want 600", got)
	}
	p.Monthly.Income = PovertyLineCNY
	if got := w.Treasury.payLowIncome(w, p); got != 0 {
		t.Errorf("income at poverty line must pay 0, got %d", got)
	}
}

// 6. TestTransferChildAllowance 儿童津贴:200/孩/月,仅 6 岁以下(<72 月)。
func TestTransferChildAllowance(t *testing.T) {
	w := treasuryWorld(6, 1)
	p := w.Players[0]
	w.Month = 100
	// 1 孩 10 个月大 → 200。
	p.BirthMonths = []int{90}
	if got := w.Treasury.payChildAllowance(w, p); got != ChildAllowanceCNY {
		t.Errorf("1 child under 6: got %d, want %d", got, ChildAllowanceCNY)
	}
	// 2 孩:10 月 + 70 月(都 <72)→ 400。
	p.BirthMonths = []int{90, 30}
	if got := w.Treasury.payChildAllowance(w, p); got != 2*ChildAllowanceCNY {
		t.Errorf("2 children under 6: got %d, want %d", got, 2*ChildAllowanceCNY)
	}
	// 超 6 岁(80 月前出生)→ 0。
	p.BirthMonths = []int{20}
	if got := w.Treasury.payChildAllowance(w, p); got != 0 {
		t.Errorf("child over 6 must pay 0, got %d", got)
	}
	// 无生育记录 → 0。
	p.BirthMonths = nil
	if got := w.Treasury.payChildAllowance(w, p); got != 0 {
		t.Errorf("no birth record must pay 0, got %d", got)
	}
}

// 7. TestFiscalBudgetAllocation 五类预算比例:和 = 1.0,每类 > 0。
func TestFiscalBudgetAllocation(t *testing.T) {
	sum := FiscalBudget.Education + FiscalBudget.Healthcare + FiscalBudget.Pension +
		FiscalBudget.Infrastructure + FiscalBudget.UnemploymentBenefit
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("FiscalBudget sum = %f, want 1.0", sum)
	}
	shares := map[FiscalCategory]float64{
		FiscalEdu: 0.30, FiscalHealth: 0.25, FiscalPension: 0.20,
		FiscalInfra: 0.15, FiscalUnemployment: 0.10,
	}
	for cat, want := range shares {
		if got := fiscalShare(cat); math.Abs(got-want) > 1e-12 {
			t.Errorf("fiscalShare(%s) = %f, want %f", cat, got, want)
		}
	}
	// 哨兵计数 = 5(防新增类别漏配份额)。
	if fiscalCategoryCount != 5 {
		t.Errorf("fiscalCategoryCount = %d, want 5", fiscalCategoryCount)
	}
}

// 8. TestFiscalSpendingCap 支出上限:min(现金×5%, 上月收入×110%);
// 紧缩(现金 < 100 万)减半;无直发对象时仅公共服务 70% 落地。
func TestFiscalSpendingCap(t *testing.T) {
	w := treasuryWorld(9, 2)
	tr := w.Treasury
	// 场景①:现金 500 万,上月收入 10 万 → 预算 = min(25 万, 11 万) = 11 万;
	// 无 60 岁/失业座位 → 只有教育/医疗/基建 70% 实际支出 = 7.7 万。
	tr.Cash = 5_000_000
	tr.LastMonthRevenue = 100_000
	got := tr.ExecuteFiscalSpending(w)
	want := int64(float64(110_000) * (FiscalBudget.Education + FiscalBudget.Healthcare + FiscalBudget.Infrastructure))
	if got != want {
		t.Errorf("scenario1 spent = %d, want %d (70%% of 110000)", got, want)
	}
	// 场景②:现金 90 万(< 100 万紧缩线)→ 预算 = min(4.5 万, 11 万) 再减半
	// = 2.25 万 → 实际 70% = 1.575 万。
	tr.Cash = 900_000
	tr.LastMonthRevenue = 100_000
	got = tr.ExecuteFiscalSpending(w)
	want = int64(float64(22_500) * (FiscalBudget.Education + FiscalBudget.Healthcare + FiscalBudget.Infrastructure))
	if got != want {
		t.Errorf("austerity spent = %d, want %d (70%% of 22500)", got, want)
	}
	// 场景③:现金 0 → 不支出。
	tr.Cash = 0
	if got := tr.ExecuteFiscalSpending(w); got != 0 {
		t.Errorf("zero cash must spend 0, got %d", got)
	}
	// 公共服务累计投入随支出增长。
	if tr.EduCapital <= 0 || tr.HealthCapital <= 0 || tr.InfraCapital <= 0 {
		t.Errorf("public capital should accumulate: edu=%d health=%d infra=%d",
			tr.EduCapital, tr.HealthCapital, tr.InfraCapital)
	}
}

// 9. TestSovereignBondIssue 发行触发:现金 < 3×月支出 → 发行 = 缺口×80%,
// clamp [10 万, 500 万];现金充足不发行。
func TestSovereignBondIssue(t *testing.T) {
	w := treasuryWorld(11, 1)
	tr := w.Treasury
	// 现金充足(500 万 ≥ 3×10 万):不发行。
	tr.LastMonthExpense = 100_000
	if got := tr.IssueBonds(w); got != 0 {
		t.Errorf("healthy treasury must not issue, got %d", got)
	}
	// 现金 10 万 < 3×10 万:缺口 20 万 → 发行 16 万。
	tr.Cash = 100_000
	got := tr.IssueBonds(w)
	if got != 160_000 {
		t.Errorf("issue amount = %d, want 160000 (200000×0.8)", got)
	}
	if tr.BondsOutstanding != 160_000 || len(tr.Bonds) != 1 {
		t.Errorf("bonds outstanding=%d n=%d, want 160000/1", tr.BondsOutstanding, len(tr.Bonds))
	}
	b := tr.Bonds[0]
	if b.TenureMonths != BondDefaultTenureMonths || b.IssueMonth != w.Month {
		t.Errorf("bond tenure/issue month = %d/%d, want %d/%d", b.TenureMonths, b.IssueMonth, BondDefaultTenureMonths, w.Month)
	}
	wantCoupon := w.CB.LPR5Y() + BondCouponSpread
	if math.Abs(b.CouponRate-wantCoupon) > 1e-12 {
		t.Errorf("coupon = %f, want %f (5Y LPR + 0.20%%)", b.CouponRate, wantCoupon)
	}
	// 自持记账:发行不产生现金流入(R4-1:国债不是印钞通道)。
	if tr.Cash != 100_000 {
		t.Errorf("issuance must not change cash, got %d", tr.Cash)
	}
	// 缺口过小(计算值 < 10 万):不发行。
	tr2 := NewTreasury()
	tr2.LastMonthExpense = 100_000
	tr2.Cash = 299_000 // 阈值 30 万,缺口 1000 → 800 < 10 万
	if got := tr2.IssueBonds(w); got != 0 {
		t.Errorf("tiny gap must not issue, got %d", got)
	}
}

// 10. TestSovereignBondNoCBOverdraft R4-1:国债月度处理不触达央行 ——
// CentralBankState 关键字段零变化;gov:treasury 流水只指向座位。
func TestSovereignBondNoCBOverdraft(t *testing.T) {
	w := treasuryWorld(13, 4)
	tr := w.Treasury
	// 造一只存量国债 + 低现金触发新一轮发行,含转移支付全链路。
	tr.Bonds = append(tr.Bonds, &SovereignBond{
		ID: "GB1-1", IssueMonth: 1, TenureMonths: 60,
		FaceValue: 2_000_000, CouponRate: 0.037,
	})
	tr.BondsOutstanding = 2_000_000
	tr.Cash = 50_000  // 低现金 → 触发发行
	cbBefore := *w.CB // 值拷贝快照

	tr.SettleTreasuryMonth(w)

	cbAfter := *w.CB
	for _, f := range []struct {
		name          string
		before, after float64
	}{
		{"M0", cbBefore.M0, cbAfter.M0},
		{"BaseMoney", cbBefore.BaseMoney, cbAfter.BaseMoney},
		{"GovBondsHeld", cbBefore.GovBondsHeld, cbAfter.GovBondsHeld},
		{"LoansToBanks", cbBefore.LoansToBanks, cbAfter.LoansToBanks},
		{"Reserves", cbBefore.Reserves, cbAfter.Reserves},
		{"PolicyRate", cbBefore.PolicyRate, cbAfter.PolicyRate},
	} {
		if f.before != f.after {
			t.Errorf("R4-1 violated: CB.%s changed %f → %f (treasury must never touch central bank)",
				f.name, f.before, f.after)
		}
	}
	// 流水侧:所有 gov:treasury 条目 to ∈ {seat:*},category = CatWelfare。
	for _, e := range w.Ledger.Entries {
		if e.From != EntityGovernment {
			continue
		}
		if _, ok := IsSeatEntity(e.To); !ok {
			t.Errorf("gov:treasury paid non-seat entity %q (R4-1 channel violation)", e.To)
		}
		if e.Category != CatWelfare {
			t.Errorf("gov:treasury category = %q, want %q", e.Category, CatWelfare)
		}
	}
	// 付息真实扣减现金:200 万 × 3.7%/12 = 6167。
	wantInterest := int64(6167) // 2,000,000 × 3.7% ÷ 12 = 6166.67(+0.5 截断)
	if got := tr.History[0].BondService; got < wantInterest {
		t.Errorf("bond service = %d, want >= %d (interest must be real cash outflow)", got, wantInterest)
	}
}

// 11. TestSettleMonth_TreasuryWired §130 接线验证:SettleMonth 后国库已月结
// (History 追加 + 税收入库 + 视图下发 treasury 字段)。
func TestSettleMonth_TreasuryWired(t *testing.T) {
	w := treasuryWorld(21, 12)
	month := w.Month
	w.SettleMonth()

	tr := w.Treasury
	if len(tr.History) != 1 {
		t.Fatalf("treasury history len = %d, want 1", len(tr.History))
	}
	rec := tr.History[0]
	if rec.Month != month {
		t.Errorf("history month = %d, want %d", rec.Month, month)
	}
	// 12 人 salary 10000 已月结:个税+社保必为正。
	if tr.MonthlyCounter.IncomeTaxTotal <= 0 || tr.MonthlyCounter.SocialSecurityTot <= 0 {
		t.Errorf("taxes not collected: income=%d social=%d",
			tr.MonthlyCounter.IncomeTaxTotal, tr.MonthlyCounter.SocialSecurityTot)
	}
	if tr.LastMonthRevenue != rec.Revenue {
		t.Errorf("LastMonthRevenue %d != history revenue %d", tr.LastMonthRevenue, rec.Revenue)
	}
	// economy_enabled=false:财政月结整体跳过(P0 回归零偏移)。
	w2 := treasuryWorld(21, 12)
	w2.EconomyEnabled = false
	w2.SettleMonth()
	if len(w2.Treasury.History) != 0 {
		t.Errorf("economy disabled must skip treasury settle, got %d records", len(w2.Treasury.History))
	}
	// view 接线:BuildClientState 透出 treasury。
	cs := BuildClientState("room-test", 0, w, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil)
	if cs.Treasury == nil {
		t.Fatal("view must expose treasury snapshot (§130 wiring)")
	}
	if cs.Treasury.CashCNY != tr.Cash {
		t.Errorf("view treasury cash = %d, want %d", cs.Treasury.CashCNY, tr.Cash)
	}
}

// 12. TestTreasuryDeterministic 确定性:同种子同月数 → 国库现金/历史逐条一致
// (引擎财政路径零 rand 消费)。
func TestTreasuryDeterministic(t *testing.T) {
	run := func(seed int64) *TreasuryState {
		w := treasuryWorld(seed, 8)
		for i := 0; i < 6; i++ {
			w.SettleMonth()
		}
		return w.Treasury
	}
	a, b := run(20260921), run(20260921)
	if a.Cash != b.Cash {
		t.Errorf("cash diverged: %d vs %d", a.Cash, b.Cash)
	}
	if a.BondsOutstanding != b.BondsOutstanding {
		t.Errorf("bonds outstanding diverged: %d vs %d", a.BondsOutstanding, b.BondsOutstanding)
	}
	if len(a.History) != len(b.History) {
		t.Fatalf("history length diverged: %d vs %d", len(a.History), len(b.History))
	}
	for i := range a.History {
		if a.History[i] != b.History[i] {
			t.Errorf("history[%d] diverged: %+v vs %+v", i, a.History[i], b.History[i])
		}
	}
	// History 环形缓冲:25 次月结只保留最近 24 条。
	w := treasuryWorld(1, 2)
	for i := 0; i < TreasuryHistoryLen+1; i++ {
		w.SettleMonth()
	}
	if len(w.Treasury.History) != TreasuryHistoryLen {
		t.Errorf("history ring buffer len = %d, want %d", len(w.Treasury.History), TreasuryHistoryLen)
	}
}
