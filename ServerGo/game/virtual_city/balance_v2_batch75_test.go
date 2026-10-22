// Package virtual_city — balance_v2_batch75_test.go: 批次75 P0 经济平衡 v2
// 第二步「三阶段破产」(B75-1)单测(设计文档 §5.1 T6–T8 / T13–T14)。
//
// 全部断言只读引擎既有产出,不新增 rand 消费点;现金夹具经账外登记保持 I1 守恒。
// 旧口径(BalanceV2Enabled=false)由 T14 钉死;新口径状态机由 T6/T7/T8/T13 覆盖。
package virtual_city

import (
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// b75Card 批次75 三阶段破产内联夹具卡(金额整千,便于固定数值断言)。
func b75Card(salary, expense, savings int64, district string) profession.Card {
	return profession.Card{
		ID: "B75", Title: "批次75夹具", Salary: salary, Expense: expense,
		Savings: savings, StartAge: 30, Energy: 8, Network: 5, Cognition: 5,
		CreditScore: 650, HomeDistrict: district, RiskPreference: "balanced",
		HealthGrade: "B", Marital: "single",
		OpeningHook: "这是一张批次75三阶段破产测试用的内联夹具卡,用于固定数值断言。",
	}
}

// b75CardFull 带婚姻家庭参数的夹具卡。
func b75CardFull(salary, expense, savings int64, district, marital string, children int) profession.Card {
	c := b75Card(salary, expense, savings, district)
	c.Marital = marital
	c.ChildrenCount = children
	return c
}

// b75World 单座夹具世界(v2 开启;StartGame 后未推进)。
func b75World(seed int64, card profession.Card) *World {
	w := NewWorld(seed, [MaxSeats]profession.Card{card})
	w.StartGame()
	return w
}

// b75WorldLegacy 单座夹具世界(v2 关闭;旧口径初始现金 = 卡面 Savings)。
func b75WorldLegacy(seed int64, card profession.Card) *World {
	w := NewWorld(seed, [MaxSeats]profession.Card{card})
	w.BalanceV2Enabled = false
	w.StartGame()
	return w
}

// b75SetCash 直接设定座位现金并登记账外调整(保持 I1 守恒)。
func b75SetCash(w *World, p *Player, cash int64) {
	delta := cash - p.Cash
	p.Cash = cash
	if delta != 0 {
		w.offLedgerRecord(w.Month, p.Seat, "test_fixture_cash", delta, "批次75测试夹具现金设定")
	}
}

// b75AddHouse 追加一套住宅并返回市值。
func b75AddHouse(w *World, p *Player, district string, selfOccupied bool) int64 {
	price := w.Market.HousePrice(district)
	p.Assets = append(p.Assets, Asset{
		Kind: AssetKindHouse(district), Units: 1, CostCNY: price, OpenMonth: w.Month,
		Extra: map[string]any{"district": district, "self_occupied": selfOccupied},
	})
	return price
}

// b75ConsumerLoan 追加一笔消费贷并返回月供。
func b75ConsumerLoan(p *Player, amount int64, rate float64, periods int) int64 {
	payment := AnnuityPayment(amount, rate, periods)
	p.Loans = append(p.Loans, Loan{
		ID: p.nextLoanID(), Kind: LoanConsumer, Principal: amount, Balance: amount,
		AnnualRate: rate, MonthlyPayment: payment, TermN: periods, MonthsLeft: periods,
	})
	return payment
}

// b75HasDetailKey 本月明细是否含指定 key。
func b75HasDetailKey(p *Player, key string) bool {
	for _, d := range p.Monthly.Detail {
		if d.Key == key {
			return true
		}
	}
	return false
}

// ── T6 TestBalanceV2_HardshipGrace ───────────────────────────────────
// 现金 −20,000 且净资产为正 ⇒ 6 月内不出局、不清空房产、不扣逾期信用分;
// H1 内消费贷只付息(本金延期)、无 5% 罚息;H1 内禁止加班/新增贷款/买入风险资产。
func TestBalanceV2_HardshipGrace(t *testing.T) {
	// 工资 8500 ≈ 生活 8000 + 物业 500 ⇒ 月净流 ≈ 0,现金稳定在 −20000,
	// 保证 6 月内持续处 H1(不提前恢复)。
	w := b75World(7501, b75Card(8500, 8000, 30000, "residential"))
	p := w.Players[0]
	houseValue := b75AddHouse(w, p, "residential", true) // 自住房 ⇒ 免租
	b75ConsumerLoan(p, 50000, 0.10, 36)
	b75SetCash(w, p, -20000)

	if nw := p.NetWorth(w.Market); nw <= 0 {
		t.Fatalf("夹具净资产 = %d,期望 > 0(房 %d − 贷 50000 − 现金)", nw, houseValue)
	}
	if gf := w.graceFloor(p); -20000 < gf {
		t.Fatalf("夹具现金 −20000 < GraceFloor %d,无法进入 H1", gf)
	}

	for m := 1; m <= 6; m++ {
		w.SettleMonth()
		if !p.Alive {
			t.Fatalf("M%d 出局(期望 6 月内存活)", m)
		}
		if len(p.Assets) == 0 {
			t.Fatalf("M%d 房产被清空(期望 H1 不清产)", m)
		}
		if p.CreditScore != 650 {
			t.Fatalf("M%d 信用分 = %d,期望 650(H1 不记逾期)", m, p.CreditScore)
		}
		if b75HasDetailKey(p, "penalty") || b75HasDetailKey(p, "loan_overdue") {
			t.Fatalf("M%d 出现逾期/罚息(H1 只付息无罚息)", m)
		}
	}
	if !p.InHardship {
		t.Fatalf("6 月后未处 H1(InHardship=%v)", p.InHardship)
	}
	loan := p.loanByID("L1")
	if loan == nil {
		t.Fatal("消费贷消失")
	}
	if loan.Balance != 50000 {
		t.Errorf("H1 内消费贷余额 = %d,期望 50000(本金延期)", loan.Balance)
	}
	if loan.MonthsLeft != 36 {
		t.Errorf("H1 内消费贷剩余期数 = %d,期望 36(本金延期不消耗期数)", loan.MonthsLeft)
	}
	// H1 动作守卫:停加班 / 禁新增贷款 / 禁买入非债券风险资产。
	if _, e := w.ApplyAction(0, Action{Type: ActWorkOvertime}); e == nil {
		t.Error("H1 内 work_overtime 应被拒绝")
	}
	if _, e := w.ApplyAction(0, Action{Type: ActTakeLoan, Kind: "consumer", AmountCNY: 10000}); e == nil {
		t.Error("H1 内新增贷款应被拒绝")
	}
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 1000}); e == nil {
		t.Error("H1 内买入股票(非债券风险资产)应被拒绝")
	}
	t.Logf("T6:H1 6 月存活;房产保留;信用分 650;消费贷 50000/36 期(只付息);H1 守卫生效")
}

// ── T15 TestBalanceV2_HardshipUnpaidCounterSeatLevel ──────────────────
// 「连续两月无法付息 ⇒ R1」按**座位**口径判定(方案 §3.6.2 / §3.6.1 触发条件):
// 任一笔结构性付息不能即计数,**不得在贷款循环内逐笔重置** —— 否则多笔贷款时
// 后一笔可付息即抹掉前一笔计数,座位滞留 H1 超出契约时长。
//
// 夹具:大额消费贷(利息 > 常规收入 ⇒ 结构性付息不能)在前,小额消费贷(利息可付)在后;
// 若计数被逐笔重置,末笔会把计数抹成 0 ⇒ 连续两月无法付息永不触发 R1。
func TestBalanceV2_HardshipUnpaidCounterSeatLevel(t *testing.T) {
	w := b75World(7515, b75Card(5000, 2000, 30000, "residential"))
	p := w.Players[0]
	// 大额贷:月息 = 1,000,000 × 10% / 12 ≈ 8,333 > 常规收入 5,000 ⇒ 结构性付息不能。
	b75ConsumerLoan(p, 1000000, 0.10, 36)
	// 小额贷:月息 ≈ 8 ⇒ 现金可付(用于验证「后一笔正常不得抹掉前一笔计数」)。
	b75ConsumerLoan(p, 1000, 0.10, 36)
	b75SetCash(w, p, -20000)

	if gf := w.graceFloor(p); -20000 < gf {
		t.Fatalf("夹具现金 −20000 < GraceFloor %d,无法进入 H1", gf)
	}

	w.SettleMonth()
	if !p.InHardship {
		t.Fatalf("M1 未进入 H1(InHardship=%v)", p.InHardship)
	}
	if p.HardshipUnpaidMonths != 1 {
		t.Fatalf("M1 HardshipUnpaidMonths = %d,期望 1(座位级计数:大额贷结构性付息不能,"+
			"不得被后一笔可付息贷款抹掉)", p.HardshipUnpaidMonths)
	}
	if b75HasDetailKey(p, "penalty") || b75HasDetailKey(p, "loan_overdue") {
		t.Fatal("H1 内出现逾期/罚息(只付息不罚息)")
	}

	w.SettleMonth()
	if !p.InR1 {
		t.Fatalf("M2 未进入 R1(连续两月无法付息应触发重组);InHardship=%v HardshipUnpaidMonths=%d",
			p.InHardship, p.HardshipUnpaidMonths)
	}
	if !p.Alive {
		t.Fatal("M2 出局(期望 H1→R1 而非直接出局)")
	}
	t.Logf("T15:座位级计数生效 —— M1 计数=1,M2 触发 R1(连续两月无法付息)")
}

// ── T7 TestBalanceV2_Reorganization ──────────────────────────────────
// 首次消费贷余额 ×0.5、60 期免息、信用分 ≥ 400、动作冻结仅 1 月且
// 工资/被动收入继续。
func TestBalanceV2_Reorganization(t *testing.T) {
	w := b75World(7502, b75Card(8000, 7000, 20000, "residential"))
	p := w.Players[0]
	b75ConsumerLoan(p, 50000, 0.10, 36)
	b75SetCash(w, p, -20000)

	// H1 首次 6 月 ⇒ 第 7 个月结进入 R1。
	for m := 1; m <= 7; m++ {
		w.SettleMonth()
	}
	if !p.InR1 {
		t.Fatalf("7 月后未进入 R1(InR1=%v, InHardship=%v, HardshipMonths=%d)",
			p.InR1, p.InHardship, p.HardshipMonths)
	}
	loan := p.loanByID("L1")
	if loan == nil {
		t.Fatal("消费贷消失")
	}
	if loan.Balance != 25000 {
		t.Errorf("R1 首次重组后消费贷余额 = %d,期望 25000(×0.5)", loan.Balance)
	}
	if !loan.FreeInterest || loan.MonthsLeft != 60 || loan.TermN != 60 {
		t.Errorf("R1 重组条款:FreeInterest=%v MonthsLeft=%d TermN=%d,期望 true/60/60",
			loan.FreeInterest, loan.MonthsLeft, loan.TermN)
	}
	if p.CreditScore < CreditScoreFloorV2 {
		t.Errorf("R1 信用分 = %d,期望 ≥ %d", p.CreditScore, CreditScoreFloorV2)
	}
	if p.BankruptCount == 0 {
		t.Error("R1 未记清算事件(BankruptCount=0)")
	}
	// 动作冻结:冻结期内仅还款/保险;work_overtime 拒绝。
	if p.R1FreezeMonths <= 0 {
		t.Fatalf("R1 冻结期 = %d,期望 > 0", p.R1FreezeMonths)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActWorkOvertime}); e == nil {
		t.Error("R1 冻结期内 work_overtime 应被拒绝")
	}
	// 冻结仅 1 月:下一月结后解冻,且工资/被动收入继续发放。
	w.SettleMonth()
	if p.R1FreezeMonths != 0 {
		t.Errorf("R1 冻结期未在 1 月内解除(R1FreezeMonths=%d)", p.R1FreezeMonths)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActWorkOvertime}); e != nil {
		t.Errorf("R1 冻结期后 work_overtime 应恢复:%v", e)
	}
	if p.Monthly.Income <= 0 {
		t.Errorf("R1 冻结期后收入 = %d,期望 > 0(工资/被动收入不被冻结)", p.Monthly.Income)
	}
	t.Logf("T7:R1 首次重组 50000→25000 / 60 期免息;信用分 %d;冻结 1 月;工资继续",
		p.CreditScore)
}

// ── T8 TestBalanceV2_FormalBankruptcyOnlyInsolvent ───────────────────
// 流动性危机但净资产为正 ⇒ 存活(不清产);清算后资不抵债且预测现金流为负
// ⇒ 才 Alive=false。
func TestBalanceV2_FormalBankruptcyOnlyInsolvent(t *testing.T) {
	// Case A:流动性危机 + 净资产为正 ⇒ 存活。
	t.Run("LiquidButSolvent", func(t *testing.T) {
		w := b75World(7503, b75Card(5000, 4000, 20000, "residential"))
		p := w.Players[0]
		b75AddHouse(w, p, "residential", true)
		b75ConsumerLoan(p, 100000, 0.10, 36)
		b75SetCash(w, p, -20000)
		if nw := p.NetWorth(w.Market); nw <= 0 {
			t.Fatalf("CaseA 净资产 = %d,期望 > 0", nw)
		}
		for m := 1; m <= 8; m++ {
			w.SettleMonth()
			if !p.Alive {
				t.Fatalf("CaseA M%d 出局(期望存活)", m)
			}
		}
		if len(p.Assets) == 0 {
			t.Error("CaseA 房产被清空(期望存活不清产)")
		}
	})

	// Case B:清算后资不抵债且预测现金流为负 ⇒ 才出局。
	t.Run("InsolventAndNegativeFlow", func(t *testing.T) {
		w := b75World(7504, b75Card(3000, 8000, 5000, "residential"))
		p := w.Players[0]
		b75ConsumerLoan(p, 100000, 0.10, 36)
		b75SetCash(w, p, -20000)
		if got := w.predictedBaseCashFlow(p); got >= 0 {
			t.Fatalf("CaseB 预测现金流 = %d,期望 < 0", got)
		}
		if w.postLiquidationSolvent(p) {
			t.Fatal("CaseB 清算后应为资不抵债")
		}
		for m := 1; m <= 8; m++ {
			w.SettleMonth()
			if !p.Alive {
				if p.Ending != EndingBankrupt {
					t.Errorf("CaseB 出局结局 = %q,期望 %q", p.Ending, EndingBankrupt)
				}
				t.Logf("CaseB M%d 正式出局(资不抵债 + 现金流为负)", m)
				return
			}
		}
		t.Fatal("CaseB 8 月内未出局(期望资不抵债 + 现金流为负 ⇒ B1)")
	})
}

// ── T13 TestBalanceV2_FamilyHealthStress ─────────────────────────────
// 失业 + 两孩 + 医疗事件叠加 ⇒ 先 H1 而非直接 B1;无后续人为 world 注资。
func TestBalanceV2_FamilyHealthStress(t *testing.T) {
	w := b75World(7505, b75CardFull(8000, 6000, 30000, "residential", "married", 2))
	p := w.Players[0]
	// 复合冲击:失业 2 月 + 医疗支出(经 Pay 走 Ledger,非 world 注资)。
	p.UnemployedMonths = 2
	w.Pay(0, SeatEntity(0), w.consumerPayTo(), 30000, CatMedical, "压力测试医疗冲击")
	b75SetCash(w, p, -5000)

	countInject := func() int {
		n := 0
		for _, e := range w.Ledger.Entries {
			if e.From == EntityWorld && e.To == SeatEntity(0) && e.Category == CatInject {
				n++
			}
		}
		return n
	}
	injectBefore := countInject()

	// M1:复合冲击下**先进入 H1**(而非直接 B1),且存活。
	w.SettleMonth()
	if !p.Alive {
		t.Fatalf("M1 直接出局(期望先 H1 而非 B1)")
	}
	if !p.InHardship && p.HardshipCount == 0 {
		t.Fatalf("M1 未进入 H1(InHardship=%v, HardshipCount=%d, InR1=%v)",
			p.InHardship, p.HardshipCount, p.InR1)
	}
	// 无后续人为 world 注资(仅初始注入)。
	if after := countInject(); after != injectBefore {
		t.Errorf("复合冲击后 world 注资条目 %d → %d,期望不变(无人为注资)",
			injectBefore, after)
	}
	t.Logf("T13:复合冲击先 H1(InHardship=%v, HardshipCount=%d, InR1=%v),无人为 world 注资",
		p.InHardship, p.HardshipCount, p.InR1)
}

// ── H1 房贷展期(方案 §3.6.2)──────────────────────────────────────────

// TestBalanceV2_MortgageExtension H1 内房贷月供 > 家庭常规收入 35% ⇒ 一次性
// 展期 12 月,展期后月供 ≤ 35%。
func TestBalanceV2_MortgageExtension(t *testing.T) {
	w := b75World(7510, b75Card(5000, 3000, 100000, "residential"))
	p := w.Players[0]
	pay0 := AnnuityPayment(1000000, 0.042, 360)
	p.Loans = append(p.Loans, Loan{
		ID: "L1", Kind: LoanMortgage, Principal: 1000000, Balance: 1000000,
		AnnualRate: 0.042, MonthlyPayment: pay0, TermN: 360, MonthsLeft: 360,
	})
	b75SetCash(w, p, -5000)
	w.SettleMonth()
	if !p.InHardship && p.HardshipCount == 0 {
		t.Fatal("未进入 H1")
	}
	loan := p.loanByID("L1")
	if loan == nil {
		t.Fatal("房贷消失")
	}
	if loan.TermN != 360+MortgageExtensionMonthsV2 {
		t.Errorf("房贷展期后期数 = %d,期望 %d(+12)", loan.TermN, 360+MortgageExtensionMonthsV2)
	}
	capPayment := int64(float64(householdRegularGrossIncome(p))*MortgagePaymentIncomeRatioV2 + 0.5)
	if loan.MonthlyPayment > capPayment {
		t.Errorf("展期后月供 = %d,期望 ≤ %d(35%%)", loan.MonthlyPayment, capPayment)
	}
}

// ── R1 信用分回补 / 24 月杠杆禁令(方案 §3.6.3-4)──────────────────────

// TestBalanceV2_R1CreditRecovery R1 内按时还款每 6 月信用分 +10(上限 550)。
func TestBalanceV2_R1CreditRecovery(t *testing.T) {
	w := b75World(7508, b75Card(20000, 8000, 200000, "residential"))
	p := w.Players[0]
	// 直接置 R1 状态(跳过 H1);无贷款 ⇒ 无逾期,OnTimeStreak 递增。
	p.InR1 = true
	p.R1Months = 60
	p.CreditScore = CreditScoreFloorV2
	p.OnTimeStreak = CreditScoreOnTimeEveryV2 - 1
	w.SettleMonth()
	if p.CreditScore != CreditScoreFloorV2+CreditScoreOnTimeStepV2 {
		t.Errorf("R1 按时还款 6 月信用分 = %d,期望 %d(+10)",
			p.CreditScore, CreditScoreFloorV2+CreditScoreOnTimeStepV2)
	}
}

// TestBalanceV2_R1LeverageBan R1 内与 R1 后 24 月内禁止新增杠杆;超 24 月恢复。
func TestBalanceV2_R1LeverageBan(t *testing.T) {
	w := b75World(7509, b75Card(20000, 8000, 200000, "residential"))
	p := w.Players[0]
	loan := Action{Type: ActTakeLoan, Kind: "consumer", AmountCNY: 10000}

	p.InR1 = true
	p.R1Months = 60
	if _, e := w.ApplyAction(0, loan); e == nil {
		t.Error("R1 内新增贷款应被拒绝")
	}
	p.InR1 = false
	w.Month = 40
	p.LastR1EndMonth = 20 // 40−20=20 ≤ 24 ⇒ 拒绝
	if _, e := w.ApplyAction(0, loan); e == nil {
		t.Error("R1 后 20 月内新增贷款应被拒绝")
	}
	p.LastR1EndMonth = 14 // 40−14=26 > 24 ⇒ 恢复
	if _, e := w.ApplyAction(0, loan); e != nil {
		t.Errorf("R1 后 %d 月新增贷款应恢复:%v", ReorgRepeatWindowMonths+1, e)
	}
}

// ── T14 TestBalanceV2_OldConfigCompatibility ─────────────────────────
// BalanceV2Enabled=false:旧随机序列与旧数值不变(双跑逐字段一致);
// 旧口径破产路径(NegativeCashMonths ≥ 3 ⇒ 清算,信用分归零)保持。
func TestBalanceV2_OldConfigCompatibility(t *testing.T) {
	// 旧口径双跑逐字段一致(含账本摘要)。
	build := func() *World { return b75WorldLegacy(7506, b75Card(10000, 8000, 30000, "residential")) }
	a, b := build(), build()
	for m := 1; m <= 12; m++ {
		a.SettleMonth()
		b.SettleMonth()
		pa, pb := a.Players[0], b.Players[0]
		if pa.Cash != pb.Cash || pa.NegativeCashMonths != pb.NegativeCashMonths ||
			pa.InReorganization != pb.InReorganization || pa.BankruptCount != pb.BankruptCount ||
			pa.CreditScore != pb.CreditScore || len(pa.Loans) != len(pb.Loans) ||
			pa.Alive != pb.Alive {
			t.Fatalf("M%d 旧口径双跑不一致", m)
		}
		if a.Ledger.Digest != b.Ledger.Digest {
			t.Fatalf("M%d 旧口径账本摘要不一致", m)
		}
	}
	// 旧口径新字段恒零(不接线)。
	pa := a.Players[0]
	if pa.InHardship || pa.InR1 || pa.HardshipCount != 0 || pa.R1Months != 0 || pa.R1FreezeMonths != 0 {
		t.Errorf("旧口径不应使用新字段:InHardship=%v InR1=%v HardshipCount=%d R1Months=%d R1FreezeMonths=%d",
			pa.InHardship, pa.InR1, pa.HardshipCount, pa.R1Months, pa.R1FreezeMonths)
	}

	// 旧口径破产路径:NegativeCashMonths ≥ 3 ⇒ 清算(信用分归零 + 重组标记)。
	w := b75WorldLegacy(7507, b75Card(5000, 3000, 1000, "commerce"))
	p := w.Players[0]
	b75ConsumerLoan(p, 50000, 0.10, 36)
	b75SetCash(w, p, -1)
	p.NegativeCashMonths = 3
	w.SettleMonth()
	if p.BankruptCount == 0 {
		t.Error("旧口径未触发清算(BankruptCount=0)")
	}
	if !p.InReorganization {
		t.Error("旧口径清算后应置 InReorganization")
	}
	if p.CreditScore != 0 {
		t.Errorf("旧口径清算后信用分 = %d,期望 0(旧口径归零)", p.CreditScore)
	}
	t.Logf("T14:旧口径双跑一致;旧破产路径生效(BankruptCount=%d, 信用分归零)", p.BankruptCount)
}
