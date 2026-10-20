package virtual_city

import (
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city/profession"
)

// biasTestWorld 构造一个单座位测试世界(座位 0,现金充足,认知可调)。
func biasTestWorld(t *testing.T, cash int64) *World {
	t.Helper()
	w := NewWorld(62, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, profession.Card{ID: "P09", Savings: cash, Salary: 20000, Cognition: 5})
	p.Cash = cash
	p.ActionBudget = 3
	assignBiasProfile(0, p)
	w.Players[0] = p
	w.StartGame()
	return w
}

// TestBias_CognitionCheck_BothBranches §4.1:阈值判定(非掷骰)。
// 通过 ⇒ 效果减弱 50% 且 detected=true;未通过 ⇒ 全额(Exaggerated)且
// detected=false(**不得**暴露偏差名)。
func TestBias_CognitionCheck_BothBranches(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	def, ok := biasByCode("loss_aversion")
	if !ok {
		t.Fatal("loss_aversion missing from catalog")
	}
	// 未通过(认知 4 < 阈值 5)⇒ 用加重值 0.85,不识破。
	p.Cognition = 4
	mul, detected := applyBias(p, def)
	if detected {
		t.Errorf("cognition 4 < threshold 5 must NOT be detected")
	}
	if mul != 0.85 {
		t.Errorf("unchecked multiplier: got %.2f, want 0.85 (Exaggerated)", mul)
	}
	// 通过(认知 5 ≥ 阈值 5)⇒ 偏离 1 的部分砍半:(0.92−1)/2 ⇒ 0.96,识破。
	p.Cognition = 5
	mul, detected = applyBias(p, def)
	if !detected {
		t.Errorf("cognition 5 >= threshold 5 must be detected")
	}
	if mul < 0.9599 || mul > 0.9601 {
		t.Errorf("checked multiplier: got %.4f, want 0.96 (Mul effect halved)", mul)
	}
	// 检定本身不消耗认知点。
	if p.Cognition != 5 {
		t.Errorf("cognition check must not consume cognition: got %d", p.Cognition)
	}
}

// TestBias_Reflect_ThreeModesAndLimits §4.3:三档反思 + 每回合 1 次 + 认知≤1 禁用。
func TestBias_Reflect_ThreeModesAndLimits(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	p.Cognition = 6

	// 轻度:认知 −1,解除 1 项。
	if _, err := w.ApplyAction(0, Action{Type: ActReflect, ReflectMode: ReflectModeLight}); err != nil {
		t.Fatalf("light reflect: %v", err)
	}
	if p.Cognition != 5 {
		t.Errorf("light reflect cognition: got %d, want 5", p.Cognition)
	}
	if !p.biasResolved(p.BiasProfile[0]) {
		t.Errorf("light reflect must resolve first profile bias")
	}
	// 本月第二次 ⇒ 35048。
	if _, err := w.ApplyAction(0, Action{Type: ActReflect, ReflectMode: ReflectModeDeep}); err == nil ||
		err.Code != errcode.ErrVirtualCityReflectLimit {
		t.Errorf("second reflect must be 35048, got %v", err)
	}
	// 认知 ≤1 ⇒ 35047。
	p.ActionBudget = 3
	p.ReflectCountThisMonth = 0
	p.Cognition = 1
	if _, err := w.ApplyAction(0, Action{Type: ActReflect, ReflectMode: ReflectModeLight}); err == nil ||
		err.Code != errcode.ErrVirtualCityReflectCognitionLow {
		t.Errorf("reflect at cognition 1 must be 35047, got %v", err)
	}
	// 非法模式 ⇒ 35049。
	p.Cognition = 6
	if _, err := w.ApplyAction(0, Action{Type: ActReflect, ReflectMode: "nope"}); err == nil ||
		err.Code != errcode.ErrVirtualCityReflectModeInvalid {
		t.Errorf("illegal reflect mode must be 35049, got %v", err)
	}
}

// TestBias_ReflectDeep_ImmunityAndPreview 深度反思解除全部 + 本回合免疫;
// 复盘预习让下一回合偏差一律视为"通过"。
func TestBias_ReflectDeep_ImmunityAndPreview(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	p.Cognition = 8
	if _, err := w.ApplyAction(0, Action{Type: ActReflect, ReflectMode: ReflectModeDeep}); err != nil {
		t.Fatalf("deep reflect: %v", err)
	}
	if p.Cognition != 6 {
		t.Errorf("deep reflect cognition: got %d, want 6", p.Cognition)
	}
	for _, code := range p.BiasProfile {
		if !p.biasResolved(code) {
			t.Errorf("deep reflect must resolve %s", code)
		}
	}
	if !p.BiasImmuneThisTurn {
		t.Errorf("deep reflect must grant turn immunity")
	}
	// 预览:认知 −1 + 下一回合一律视为通过(即使认知不达标)。
	// (每月限 1 次反思 ⇒ 模拟跨月:计数复位。)
	p.ActionBudget = 3
	p.ReflectCountThisMonth = 0
	p.BiasImmuneThisTurn = false // 深度反思的回合免疫已在上一回合结束
	p.Cognition = 4
	if _, err := w.ApplyAction(0, Action{Type: ActReflect, ReflectMode: ReflectModePreview}); err != nil {
		t.Fatalf("preview reflect: %v", err)
	}
	def, _ := biasByCode("loss_aversion")
	p.Cognition = 0 // 远低于阈值
	mul, detected := applyBias(p, def)
	if !detected {
		t.Errorf("preview must force detected=true")
	}
	if mul >= 1 {
		t.Errorf("preview must halve the deviation, got %.3f", mul)
	}
}

// TestBias_SavingsReward_StreakAndInterruption §3.3.2:连续 3 个月正储蓄 ⇒
// 第 4 个月起奖励 = 月结余 × 2%;中断即重置。
func TestBias_SavingsReward_StreakAndInterruption(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	// 直接驱动 settleBehaviorMonth(避免依赖完整月结的随机性)。
	// 正结余 1000:连续 3 个月后第 4 个月起每次奖励 20。
	for i := 0; i < 3; i++ {
		inc, _, _ := w.settleBehaviorMonth(p, 1000)
		if inc != 0 {
			t.Fatalf("month %d: reward must be 0 before streak reaches 4, got %d", i+1, inc)
		}
	}
	if p.SavingsStreakMonths != 3 {
		t.Errorf("streak: got %d, want 3", p.SavingsStreakMonths)
	}
	inc, _, _ := w.settleBehaviorMonth(p, 1000)
	if inc != 20 {
		t.Errorf("month 4 reward: got %d, want 20 (surplus 1000 x 2%%)", inc)
	}
	// 中断(结余 ≤0)⇒ 计数归零。
	w.settleBehaviorMonth(p, -500)
	if p.SavingsStreakMonths != 0 {
		t.Errorf("streak must reset on negative surplus, got %d", p.SavingsStreakMonths)
	}
	inc, _, _ = w.settleBehaviorMonth(p, 1000)
	if inc != 0 {
		t.Errorf("reward must stop after interruption, got %d", inc)
	}
}

// TestBias_DeferredPayment_DueWithPenalty §3.3.1:本月免付,3 个月后 ×1.3。
func TestBias_DeferredPayment_DueWithPenalty(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	// 画像必须含双曲贴现,否则 35051。
	p.BiasProfile = []string{"hyperbolic_discount"}
	if _, err := w.ApplyAction(0, Action{Type: ActDeferPayment, AmountCNY: 1000}); err != nil {
		t.Fatalf("defer: %v", err)
	}
	if p.Cash != 100000 {
		t.Errorf("defer must not deduct now: cash=%d, want 100000", p.Cash)
	}
	if len(p.DeferredPayments) != 1 {
		t.Fatalf("deferred payments: got %d, want 1", len(p.DeferredPayments))
	}
	if p.DeferredPayments[0].DueMonth != w.Month+3 {
		t.Errorf("due month: got %d, want %d", p.DeferredPayments[0].DueMonth, w.Month+3)
	}
	// 推进到到期月:扣 1000×1.3 = 1300。
	w.Month = p.DeferredPayments[0].DueMonth
	cashBefore := p.Cash
	w.settleDeferredPayments(p)
	if p.Cash != cashBefore-1300 {
		t.Errorf("due deduction: cash=%d, want %d (1000 x 1.3)", p.Cash, cashBefore-1300)
	}
	if len(p.DeferredPayments) != 0 {
		t.Errorf("deferred list must be cleared after due, got %d", len(p.DeferredPayments))
	}
	// 无双曲贴现画像 ⇒ 35051。
	p2 := w.Players[0]
	p2.BiasProfile = []string{"loss_aversion"}
	p2.ActionBudget = 3
	if _, err := w.ApplyAction(0, Action{Type: ActDeferPayment, AmountCNY: 100}); err == nil ||
		err.Code != errcode.ErrVirtualCityDeferNotAvailable {
		t.Errorf("defer without hyperbolic profile must be 35051, got %v", err)
	}
}

// TestBias_DeferredPayment_DefaultOnInsufficientCash 现金不足 ⇒ 扣到 0 不判负数。
func TestBias_DeferredPayment_DefaultOnInsufficientCash(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	p.BiasProfile = []string{"hyperbolic_discount"}
	if _, err := w.ApplyAction(0, Action{Type: ActDeferPayment, AmountCNY: 5000}); err != nil {
		t.Fatalf("defer: %v", err)
	}
	p.Cash = 100
	w.Month = p.DeferredPayments[0].DueMonth
	w.settleDeferredPayments(p)
	if p.Cash < 0 {
		t.Errorf("cash must never go negative on default: got %d", p.Cash)
	}
	if p.DeferredDefaultCount != 1 {
		t.Errorf("default count: got %d, want 1", p.DeferredDefaultCount)
	}
}

// TestBubble_MetricsAndStateMachine §11.1/§11.2:三项集体指标 + 状态迁移。
func TestBubble_MetricsAndStateMachine(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	// 造杠杆:净资产 = 现金 100000 − 负债 38000 = 62000 ⇒ 杠杆率 = 38000/62000 ≈ 0.613
	// (>0.60 阈值 ⇒ 触发信贷过热)。
	p.Loans = append(p.Loans, Loan{ID: "L1", Kind: LoanConsumer, Principal: 38000, Balance: 38000,
		AnnualRate: 0.12, MonthlyPayment: 3000, TermN: 12, MonthsLeft: 12})
	lev, turn, cash := w.computeBubbleMetrics()
	if lev < 0.60 || lev > 0.63 {
		t.Errorf("leverage ratio: got %.4f, want ~0.613", lev)
	}
	if turn != 0 {
		t.Errorf("turnover ratio must be 0 with no trades, got %.3f", turn)
	}
	if cash < 0.9 || cash > 1.0 {
		t.Errorf("cash ratio: got %.3f, want ~1.0 (all cash)", cash)
	}
	// 杠杆率 0.6 ⇒ 进入过热。
	w.bubbleMonthlyStep()
	if w.BubbleState != BubbleOverheat {
		t.Fatalf("state: got %s, want overheat", w.BubbleState)
	}
	if w.Market.DriftMultiplier != bubbleOverheatDriftMult {
		t.Errorf("overheat drift multiplier: got %.2f, want %.2f", w.Market.DriftMultiplier, bubbleOverheatDriftMult)
	}
	// 过热持续 2 月 ⇒ 破裂(资产一次性下跌)。
	idxBefore := w.Market.StockIndex
	w.bubbleMonthlyStep()
	if w.BubbleState != BubbleBurst {
		t.Fatalf("state after 2 months: got %s, want burst", w.BubbleState)
	}
	if w.Market.StockIndex >= idxBefore {
		t.Errorf("burst must drop stock index: %.4f -> %.4f", idxBefore, w.Market.StockIndex)
	}
	if w.BubbleMessage == "" {
		t.Errorf("burst must broadcast the teaching message")
	}
	// 破裂 ⇒ 次月强制衰退,2~4 月后回常态。
	w.bubbleMonthlyStep()
	if w.BubbleState != BubbleRecession {
		t.Fatalf("state after burst: got %s, want recession", w.BubbleState)
	}
	if w.Market.DriftExtra != bubbleRecessionDriftAdd {
		t.Errorf("recession drift extra: got %.3f, want %.3f", w.Market.DriftExtra, bubbleRecessionDriftAdd)
	}
	// 撤掉杠杆(去杠杆是真实的"泡沫后修复"路径)⇒ 衰退期结束后回常态。
	p.Loans = nil
	for i := 0; i < 8; i++ {
		w.bubbleMonthlyStep()
	}
	if w.BubbleState != BubbleNone {
		t.Errorf("state must return to none, got %s", w.BubbleState)
	}
	if w.Market.DriftMultiplier != 1.0 || w.Market.DriftExtra != 0 {
		t.Errorf("drift params must reset: mult=%.2f extra=%.3f", w.Market.DriftMultiplier, w.Market.DriftExtra)
	}
}

// TestBubble_DropMapping 破裂跌幅按杠杆率线性映射(0.60→20%,0.80+→40%)。
func TestBubble_DropMapping(t *testing.T) {
	cases := []struct{ lev, want float64 }{
		{0.50, 0.20},
		{0.60, 0.20},
		{0.70, 0.30},
		{0.80, 0.40},
		{0.95, 0.40},
	}
	for _, c := range cases {
		got := bubbleBurstDrop(c.lev)
		if got < c.want-1e-9 || got > c.want+1e-9 {
			t.Errorf("bubbleBurstDrop(%.2f) = %.3f, want %.3f", c.lev, got, c.want)
		}
	}
}

// TestLiquidity_SpreadTableAndPhaseCorrection §7.2/§7.3:价差表 + 周期修正。
func TestLiquidity_SpreadTableAndPhaseCorrection(t *testing.T) {
	w := biasTestWorld(t, 100000)
	w.Market.CyclePhase = PhaseRecovery
	base := map[AssetClass]float64{
		ClassCash: 0, ClassStock: 0.02, ClassBond: 0.03, ClassGold: 0.05,
		ClassHouse: 0.10, ClassEquity: 0.20, ClassCollectible: 0.25,
	}
	for class, want := range base {
		got := w.LiquiditySpread(class)
		if got < want-1e-9 || got > want+1e-9 {
			t.Errorf("recovery spread %s: got %.4f, want %.4f", class, got, want)
		}
	}
	// 繁荣期 ×0.7,衰退期 ×1.8。
	w.Market.CyclePhase = PhaseBoom
	if got := w.LiquiditySpread(ClassHouse); got < 0.0699 || got > 0.0701 {
		t.Errorf("boom house spread: got %.4f, want 0.07", got)
	}
	w.Market.CyclePhase = PhaseRecession
	if got := w.LiquiditySpread(ClassHouse); got < 0.1799 || got > 0.1801 {
		t.Errorf("recession house spread: got %.4f, want 0.18", got)
	}
	// 中点成交口径:买方付 ×(1+spread/2),卖方收 ×(1−spread/2)。
	w.Market.CyclePhase = PhaseRecovery
	fair := int64(100000)
	buy := w.liquidityBuyPrice(fair, ClassStock)
	sell := w.liquiditySellPrice(fair, ClassStock)
	if buy != 101000 {
		t.Errorf("buy price: got %d, want 101000 (100000 x 1.01)", buy)
	}
	if sell != 99000 {
		t.Errorf("sell price: got %d, want 99000 (100000 x 0.99)", sell)
	}
}

// TestLiquidity_SpreadAppliedToTrade 价差必须真的作用在成交价上(否则是"声明了却从不接线")。
func TestLiquidity_SpreadAppliedToTrade(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	p.BiasProfile = nil // 排除偏差影响,只验价差
	before := p.Cash
	if _, err := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 10000}); err != nil {
		t.Fatalf("buy: %v", err)
	}
	at := p.assetOf(AssetStockIndex)
	if at == nil || at.Units <= 0 {
		t.Fatal("stock not held")
	}
	// 无价差时的份数 = 10000/ask;有 2% 价差 ⇒ 份数必须更少。
	ask := w.Market.StockBuyUnit()
	naive := float64(int64(10000 / ask))
	if at.Units >= naive {
		t.Errorf("liquidity spread not applied: units %v >= naive %v", at.Units, naive)
	}
	if p.Cash >= before {
		t.Errorf("cash must decrease: %d -> %d", before, p.Cash)
	}
}

// TestHerding_IndexAndCounterTrendReward §3.4.3/§3.4.4:羊群指数 + 逆势奖励。
func TestHerding_IndexAndCounterTrendReward(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	// 制造"多数人买入"的过去 3 回合。
	w.TurnSummaries = []TurnSummary{
		{Month: 1, Buy: 5, Sell: 0, Hold: 0, Majority: MajorityBuy},
		{Month: 2, Buy: 4, Sell: 1, Hold: 0, Majority: MajorityBuy},
	}
	if dir := w.marketMajorityDirection(); dir != MajorityBuy {
		t.Fatalf("majority direction: got %s, want buy", dir)
	}
	// 顺势买入 ⇒ 跟风 +1。
	w.recordTradeDirection(p, "buy")
	if p.HerdFollowCount != 1 {
		t.Errorf("follow count: got %d, want 1", p.HerdFollowCount)
	}
	// 逆势卖出 ⇒ 逆势 +1。
	w.recordTradeDirection(p, "sell")
	if p.HerdCounterCount != 1 {
		t.Errorf("counter count: got %d, want 1", p.HerdCounterCount)
	}
	if idx := p.herdingIndex(); idx < 0.49 || idx > 0.51 {
		t.Errorf("herding index: got %.3f, want 0.5", idx)
	}
	// 逆势奖励:同向 ≥3 人 + 净资产上升 ⇒ 认知 +2。
	p.Cognition = 4
	p.HerdCounterMovesThisMonth = 1
	summary := TurnSummary{Month: 3, Buy: 4, Sell: 0, Hold: 0, Majority: MajorityBuy}
	if !w.counterTrendReward(p, 1000, 2000, summary) {
		t.Fatal("countertrend reward must be granted")
	}
	if p.Cognition != 6 {
		t.Errorf("countertrend cognition: got %d, want 6 (+2)", p.Cognition)
	}
	// 净资产未上升 ⇒ 不发。
	p.Cognition = 4
	if w.counterTrendReward(p, 2000, 1000, summary) {
		t.Error("countertrend reward must not be granted when net worth fell")
	}
	if p.Cognition != 4 {
		t.Errorf("cognition must be unchanged, got %d", p.Cognition)
	}
	// 同向人数 <3 ⇒ 不发。
	p.HerdCounterMovesThisMonth = 1
	small := TurnSummary{Month: 3, Buy: 2, Sell: 0, Hold: 0, Majority: MajorityBuy}
	if w.counterTrendReward(p, 1000, 2000, small) {
		t.Error("countertrend reward must require >=3 same-direction peers")
	}
}

// TestBias_AnchoringIndex 锚定指数:有样本才非零;分母趋零的样本被跳过。
func TestBias_AnchoringIndex(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	if idx := p.anchoringIndex(); idx != 0 {
		t.Errorf("anchoring index with no samples: got %.3f, want 0", idx)
	}
	// 公式 §3.1.4:Σ(|决策价 − 公允价| / |买入价 − 公允价|) / 样本数
	//           = |3.0−3.5| / |3.6−3.5| = 0.5/0.1 = 5.0
	p.recordAnchorSample(AnchorSample{Asset: AssetStockIndex, DecisionPrice: 3.0, FairPrice: 3.5, BuyPrice: 3.6})
	if idx := p.anchoringIndex(); idx < 4.99 || idx > 5.01 {
		t.Errorf("anchoring index: got %.3f, want 5.0 (|3.0-3.5| / |3.6-3.5|)", idx)
	}
	// 分母为 0 的样本被跳过(不计入均值)。
	p.AnchorSamples = append(p.AnchorSamples, AnchorSample{Asset: AssetStockIndex, DecisionPrice: 3.5, FairPrice: 3.5, BuyPrice: 3.5})
	if idx := p.anchoringIndex(); idx < 4.99 || idx > 5.01 {
		t.Errorf("degenerate sample must be skipped, got %.3f", idx)
	}
}

// TestBias_MentalAccountTags 心理账户:收入打标,不影响购买力;意外之财触发放大。
func TestBias_MentalAccountTags(t *testing.T) {
	w := biasTestWorld(t, 100000)
	p := w.Players[0]
	p.tagIncome(MoneyTagWindfall, 5000)
	p.tagIncome(MoneyTagSalary, 1000)
	if p.MentalAccount[MoneyTagWindfall] != 5000 {
		t.Errorf("windfall tag: got %d, want 5000", p.MentalAccount[MoneyTagWindfall])
	}
	// 意外之财存在 ⇒ 心理账户偏差生效(消费金额放大)。
	p.BiasProfile = []string{"mental_account"}
	p.Cognition = 0 // 未通过检定 ⇒ 用 Exaggerated 2.0
	plan := planConsume(p, 1000)
	if plan.Amount != 2000 {
		t.Errorf("mental account consume: got %d, want 2000 (1000 x 2.0)", plan.Amount)
	}
	// 无意外之财 ⇒ Guard 不通过 ⇒ 不放大。
	p.MentalAccount[MoneyTagWindfall] = 0
	plan = planConsume(p, 1000)
	if plan.Amount != 1000 {
		t.Errorf("no windfall ⇒ no amplification, got %d", plan.Amount)
	}
}

// TestBias_ProfileDeterministic 画像按 seat 确定性采样(同 seat 每次一致)。
func TestBias_ProfileDeterministic(t *testing.T) {
	for seat := 0; seat < MaxSeats; seat++ {
		a := pickBiasProfile(seat)
		b := pickBiasProfile(seat)
		if len(a) != len(b) {
			t.Fatalf("seat %d: profile length differs between calls", seat)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("seat %d: profile differs between calls: %v vs %v", seat, a, b)
			}
		}
		if len(a) < 2 || len(a) > 3 {
			t.Errorf("seat %d: profile size %d, want 2..3", seat, len(a))
		}
		for _, code := range a {
			if _, ok := biasByCode(code); !ok {
				t.Errorf("seat %d: unknown bias code %q", seat, code)
			}
		}
	}
}
