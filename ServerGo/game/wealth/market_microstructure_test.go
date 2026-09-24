// Package wealth — market_microstructure_test.go: 股票交易微观结构单测
// (批次20 文档3 B5)。
//
// 覆盖:涨跌停 clamp 表(±20% 原始 → ±10% 截断)/ 四阶段价差取数 /
// T+1 冻结·解冻·可卖量边界 / 熔断触发·延长·到期·仅股票作用域 /
// 35043·35044 返回路径 / Ledger 双式平衡不回归。
package wealth

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth/profession"
)

// microWorld 单玩家现金充足世界(微观结构动作测试夹具)。
func microWorld(t *testing.T, cash int64) *World {
	t.Helper()
	w := NewWorld(11, [MaxSeats]profession.Card{})
	p := newPlayerFromCard(0, profession.Card{ID: "M", Savings: cash})
	p.Cash = cash
	p.ActionBudget = 3
	w.Players[0] = p
	w.StartGame()
	return w
}

// TestStockLimitBand_Clamp B2-1 涨跌停截断表:±20% 原始 → ±10% 保号;带内逐位不变。
func TestStockLimitBand_Clamp(t *testing.T) {
	cases := []struct {
		old, raw, want float64
	}{
		{100, 120, 110}, // +20% → +10%
		{100, 80, 90},   // −20% → −10%
		{100, 111, 110}, // +11% → +10%(边界外一位也截断)
		{100, 89, 90},   // −11% → −10%
		{100, 110, 110}, // 恰好 +10% 保留
		{100, 90, 90},   // 恰好 −10% 保留
		{100, 105, 105}, // 带内原样
		{3.5, 3.507, 3.507},
	}
	for _, c := range cases {
		// 容忍浮点表示误差(100×1.10 = 110.00000000000001)。
		if got := clampStockMonthlyLimit(c.old, c.raw); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("clamp(%v,%v): got %v, want %v", c.old, c.raw, got, c.want)
		}
	}
	// MonthStep 集成:1000 步随机漂移,单月 |Δ| 恒 ≤10%+ε(涨跌停刚性)。
	m := NewMarket(rand.New(rand.NewSource(3)))
	for i := 0; i < 1000; i++ {
		old := m.StockIndex
		m.MonthStep(rand.New(rand.NewSource(int64(i))))
		d := m.StockIndex/old - 1
		if d > 0.1000001 || d < -0.1000001 {
			t.Fatalf("step %d escaped limit band: Δ=%f (idx %f→%f)", i, d, old, m.StockIndex)
		}
		if m.StockIndex < 0.01 {
			t.Fatalf("step %d floor broken: %f", i, m.StockIndex)
		}
	}
}

// TestStockLimitBand_ZeroOffsetInsideBand B2-1 零偏移论证:带内月份新乘式
// 与旧实现 `m *= 1+drift` 逐位一致(clamp 不改变带内值)。
func TestStockLimitBand_ZeroOffsetInsideBand(t *testing.T) {
	m := NewMarket(rand.New(rand.NewSource(9)))
	old := m.StockIndex
	p := m.Params()
	drift := 1 + p.StockAnnual/12
	// 小幅漂移(模拟 |Δ|<10%)两条路径逐位相等。
	rawNew := clampStockMonthlyLimit(old, old*drift)
	rawOld := old * drift
	if rawNew != rawOld {
		t.Errorf("in-band bitwise: new=%v old=%v", rawNew, rawOld)
	}
}

// TestStockSpreadByPhase B2-2 四阶段价差取数表 + 双边价对称性。
func TestStockSpreadByPhase(t *testing.T) {
	cases := []struct {
		phase CyclePhase
		bps   int
	}{
		{PhaseBoom, 20},       // overheated 过热 0.20%
		{PhaseRecovery, 4},    // expansion 扩张 0.04%
		{PhaseRecession, 8},   // recession 衰退 0.08%
		{PhaseDepression, 15}, // panic 恐慌 0.15%
	}
	for _, c := range cases {
		m := &MarketState{CyclePhase: c.phase, StockIndex: 100}
		if got := m.StockSpread(); int(got*10000+0.5) != c.bps {
			t.Errorf("spread(%s): got %f bps want %d", c.phase, got*10000, c.bps)
		}
		buy, sell := m.StockBuyUnit(), m.StockSellUnit()
		// 双边价围绕中值对称:buy+sell = 2×idx;买 > 中 > 卖。
		if diff := (buy + sell) - 2*100.0; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s mid symmetry: buy+sell=%f want 200", c.phase, buy+sell)
		}
		if !(buy > 100 && sell < 100) {
			t.Errorf("%s ordering: buy=%f idx=100 sell=%f", c.phase, buy, sell)
		}
	}
}

// TestStockT1_BuyLockAndThaw B2-3:买入即冻结、月结开头清零、可卖量边界。
func TestStockT1_BuyLockAndThaw(t *testing.T) {
	w := microWorld(t, 100000)
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 20000}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	at := w.Players[0].assetOf(AssetStockIndex)
	if w.Players[0].StockT1Locked != int64(at.Units) {
		t.Fatalf("lock: got %d want %d", w.Players[0].StockT1Locked, int64(at.Units))
	}
	// 边界:可卖量 = 持仓 − 冻结 = 0;卖 1 份即 35044。
	if s := w.Players[0].stockSellableUnits(); s != 0 {
		t.Fatalf("sellable at full lock: got %v, want 0", s)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 1}); e == nil ||
		e.Code != errcode.ErrWealthStockT1Locked {
		t.Fatalf("locked sell must 35044, got %v", e)
	}
	// 混合:再买(全部仍冻结)+ 手动解冻 100 模拟上月持仓 → 卖 100 放行、101 拒绝。
	w.Players[0].StockT1Locked = int64(at.Units) - 100
	if s := w.Players[0].stockSellableUnits(); s != 100 {
		t.Fatalf("sellable mixed: got %v want 100", s)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 101}); e == nil ||
		e.Code != errcode.ErrWealthStockT1Locked {
		t.Errorf("over-sell by 1 must 35044, got %v", e)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 100}); e != nil {
		t.Errorf("exact sellable must pass: %v", e)
	}
	// 月结开头清零。
	w.Players[0].StockT1Locked = 500
	w.SettleMonth()
	if w.Players[0].StockT1Locked != 0 {
		t.Fatalf("T1 must thaw at settle head: %d", w.Players[0].StockT1Locked)
	}
}

// TestStockT1_ListingFrozen B2-3 评审口径:挂牌侧股票过户同受 T+1 约束。
func TestStockT1_ListingFrozen(t *testing.T) {
	w := microWorld(t, 100000)
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 10000}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	at := w.Players[0].assetOf(AssetStockIndex)
	// 全部冻结 → 挂牌被拒 35044。
	if _, e := w.CreateListing(0, ListingAsset, ListingPayload{Asset: &AssetPayload{Asset: *at}}, 5000); e == nil ||
		e.Code != errcode.ErrWealthStockT1Locked {
		t.Fatalf("listing frozen stock must 35044, got %v", e)
	}
	// 解冻一半 → 可挂(快照量 ≤ 可卖量)。
	w.Players[0].StockT1Locked = int64(at.Units) - 50
	partial := *at
	partial.Units = 50
	if _, e := w.CreateListing(0, ListingAsset, ListingPayload{Asset: &AssetPayload{Asset: partial}}, 500); e != nil {
		t.Errorf("listing unfrozen part must pass: %v", e)
	}
}

// TestStockCircuitBreaker_TriggerScopeExpiry B2-4:触发/仅股票/延长/到期。
func TestStockCircuitBreaker_TriggerScopeExpiry(t *testing.T) {
	w := microWorld(t, 100000)
	m := w.Market
	// 触发:注入 clamp 前原始跌幅 −16%(< −15%)→ BreakerUntilMonth = 当月+1 + event。
	w.Month = 10
	m.LastStockRawDelta = -0.16
	w.checkStockCircuitBreaker()
	if m.BreakerUntilMonth != 11 {
		t.Fatalf("breaker month: got %d, want 11", m.BreakerUntilMonth)
	}
	// BreakerUntilMonth 存「最后一个禁止月」=11:月 ≤11 均判禁(触发月 10 的
	// 动作窗口实际已关,判定无副作用);月 12 起自动解禁。
	if !m.StockBreakerActive(10) || !m.StockBreakerActive(11) || m.StockBreakerActive(12) {
		t.Fatalf("breaker window: active(10)=%v active(11)=%v active(12)=%v",
			m.StockBreakerActive(10), m.StockBreakerActive(11), m.StockBreakerActive(12))
	}
	last := w.Events[len(w.Events)-1]
	if !strings.Contains(last.Text, "熔断") || !strings.Contains(last.Text, "暂停 1 个月") {
		t.Fatalf("breaker event missing: %q", last.Text)
	}
	// 期内:股票买/卖 → 35043;其它资产不受影响。
	w.Month = 11
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 5000}); e == nil ||
		e.Code != errcode.ErrWealthMarketCircuitBreak {
		t.Errorf("breaker buy must 35043, got %v", e)
	}
	// 先建仓再验证卖出路径(绕过 gate 直接造仓)。
	w.Players[0].Assets = append(w.Players[0].Assets, Asset{Kind: AssetStockIndex, Units: 1000, OpenMonth: 1})
	if _, e := w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 10}); e == nil ||
		e.Code != errcode.ErrWealthMarketCircuitBreak {
		t.Errorf("breaker sell must 35043, got %v", e)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetBond, AmountCNY: 5000}); e != nil {
		t.Errorf("bond must be unaffected by breaker: %v", e)
	}
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetGold, AmountCNY: 5000}); e != nil {
		t.Errorf("gold must be unaffected by breaker: %v", e)
	}
	// 同一结算月内重复触发只延长不叠加:两次 check 均指向 Month+1(不会 +2)。
	w.Month = 11
	m.LastStockRawDelta = -0.16
	w.checkStockCircuitBreaker()
	if m.BreakerUntilMonth != 12 {
		t.Fatalf("extension at month 11: got %d, want 12", m.BreakerUntilMonth)
	}
	w.checkStockCircuitBreaker()
	if m.BreakerUntilMonth != 12 {
		t.Fatalf("same-month re-trigger must not stack beyond +1: got %d", m.BreakerUntilMonth)
	}
	// 到期自动解禁:12 月仍禁,13 月起放行。
	if !m.StockBreakerActive(12) {
		t.Fatal("month 12 must still be forbidden")
	}
	w.Month = 13
	if m.StockBreakerActive(13) {
		t.Fatal("month 13 must be free of month-11 triggers")
	}
	// 无新原始跌幅注入 → 不再延长。
	m.LastStockRawDelta = -0.05
	w.checkStockCircuitBreaker()
	if m.BreakerUntilMonth != 12 {
		t.Errorf("−5%% must not extend breaker: %d", m.BreakerUntilMonth)
	}
	// 阈值边界:−15% 整即触发(≤),−14.9% 不触发。
	m2 := microWorld(t, 1000).Market
	m2.BreakerUntilMonth = 0
	m2.LastStockRawDelta = -0.149
	w.Market = m2
	w.Month = 20
	w.checkStockCircuitBreaker()
	if m2.BreakerUntilMonth != 0 {
		t.Errorf("−14.9%% must not trigger: %d", m2.BreakerUntilMonth)
	}
	m2.LastStockRawDelta = -0.15
	w.checkStockCircuitBreaker()
	if m2.BreakerUntilMonth != 21 {
		t.Errorf("−15.0%% must trigger: %d", m2.BreakerUntilMonth)
	}
}

// TestStockSpreadExecution_LedgerParity B2-2:买卖改双边价后 Ledger 金额 =
// 实付/实收(双式平衡,科目不变)。
func TestStockSpreadExecution_LedgerParity(t *testing.T) {
	w := microWorld(t, 100000)
	ask := w.Market.StockBuyUnit()
	cashBefore := w.Players[0].Cash
	if _, e := w.ApplyAction(0, Action{Type: ActBuyAsset, Asset: AssetStockIndex, AmountCNY: 10000}); e != nil {
		t.Fatalf("buy: %v", e)
	}
	at := w.Players[0].assetOf(AssetStockIndex)
	wantCost := int64(at.Units*ask + 0.5)
	feeWant := commissionStock(wantCost)
	var buy *Entry
	for i := range w.Ledger.Entries {
		e := &w.Ledger.Entries[i]
		if e.Category == CatBuy && e.To == EntityMarket {
			buy = e
		}
	}
	if buy == nil {
		t.Fatal("buy ledger entry missing")
	}
	if buy.AmountCNY != wantCost+feeWant {
		t.Errorf("ledger buy amount: got %d, want %d(ask-cost+fee)", buy.AmountCNY, wantCost+feeWant)
	}
	// I1 守恒:Ledger 金额 = 实付(现金变化与流水一致)。
	if w.Players[0].Cash != cashBefore-(wantCost+feeWant) {
		t.Errorf("cash parity: got %d, want %d", w.Players[0].Cash, cashBefore-(wantCost+feeWant))
	}
	// 解冻后卖出 → bid 单边价;Ledger = 实收毛额,佣金另记。
	w.Players[0].StockT1Locked = 0
	bid := w.Market.StockSellUnit()
	cashMid := w.Players[0].Cash
	if _, e := w.ApplyAction(0, Action{Type: ActSellAsset, Asset: AssetStockIndex, Units: 100}); e != nil {
		t.Fatalf("sell: %v", e)
	}
	var sell *Entry
	for i := range w.Ledger.Entries {
		e := &w.Ledger.Entries[i]
		if e.Category == CatSell && e.From == EntityMarket {
			sell = e
		}
	}
	wantGross := int64(100*bid + 0.5)
	if sell == nil || sell.AmountCNY != wantGross {
		t.Errorf("ledger sell amount: got %+v want %d (bid=%f)", sell, wantGross, bid)
	}
	wantFee := commissionStock(wantGross)
	if w.Players[0].Cash != cashMid+wantGross-wantFee {
		t.Errorf("sell cash parity: got %d, want %d", w.Players[0].Cash, cashMid+wantGross-wantFee)
	}
}

// TestErrcodes_Microstructure B3:35043/35044 常量与默认消息登记。
func TestErrcodes_Microstructure(t *testing.T) {
	if errcode.ErrWealthMarketCircuitBreak != 35043 || errcode.ErrWealthStockT1Locked != 35044 {
		t.Fatalf("codes: got %d/%d, want 35043/35044", errcode.ErrWealthMarketCircuitBreak, errcode.ErrWealthStockT1Locked)
	}
	for _, c := range []int{errcode.ErrWealthMarketCircuitBreak, errcode.ErrWealthStockT1Locked} {
		if m, ok := errcode.DefaultMessages[c]; !ok || m == "" {
			t.Errorf("DefaultMessages[%d] missing", c)
		}
	}
}
