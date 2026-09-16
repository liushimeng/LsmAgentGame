// Package wealth — minsky_test.go: 明斯基金融不稳定引擎单测(2026-09-16 §财商流P1)。
//
// 实现设计文档 §8.1 验收清单。
package wealth

import (
	"math"
	"testing"

	"LsmAgentGame/game/wealth/profession"
)

// TestClassifyMinsky_Boundary 三档边界(v2.60 N11-4)。
func TestClassifyMinsky_Boundary(t *testing.T) {
	// 对冲:月供 ≤ 收入 × 40%。
	if got := ClassifyMinsky(3000, 10000, false); got != MinskyHedge {
		t.Errorf("ClassifyMinsky(3000,10000): got %s, want %s", got, MinskyHedge)
	}
	if got := ClassifyMinsky(4000, 10000, false); got != MinskyHedge {
		t.Errorf("ClassifyMinsky(4000,10000): got %s, want %s", got, MinskyHedge)
	}
	// 投机:40% < 月供/收入 ≤ 70%。
	if got := ClassifyMinsky(4001, 10000, false); got != MinskySpeculative {
		t.Errorf("ClassifyMinsky(4001,10000): got %s, want %s", got, MinskySpeculative)
	}
	if got := ClassifyMinsky(5000, 10000, false); got != MinskySpeculative {
		t.Errorf("ClassifyMinsky(5000,10000): got %s, want %s", got, MinskySpeculative)
	}
	if got := ClassifyMinsky(7000, 10000, false); got != MinskySpeculative {
		t.Errorf("ClassifyMinsky(7000,10000): got %s, want %s", got, MinskySpeculative)
	}
	// 庞氏:月供/收入 > 70%。
	if got := ClassifyMinsky(7001, 10000, false); got != MinskyPonzi {
		t.Errorf("ClassifyMinsky(7001,10000): got %s, want %s", got, MinskyPonzi)
	}
	if got := ClassifyMinsky(8000, 10000, false); got != MinskyPonzi {
		t.Errorf("ClassifyMinsky(8000,10000): got %s, want %s", got, MinskyPonzi)
	}
	// 收入为 0 → 庞氏。
	if got := ClassifyMinsky(1000, 0, false); got != MinskyPonzi {
		t.Errorf("ClassifyMinsky(1000,0): got %s, want %s", got, MinskyPonzi)
	}
	// 收入负数 → 庞氏。
	if got := ClassifyMinsky(1000, -500, false); got != MinskyPonzi {
		t.Errorf("ClassifyMinsky(1000,-500): got %s, want %s", got, MinskyPonzi)
	}
}

// TestMinskyRateAdjustment 利率调整(诱人陷阱)。
func TestMinskyRateAdjustment(t *testing.T) {
	if got := MinskyRateAdjustment(MinskyHedge); got != 0 {
		t.Errorf("Hedge adjustment: got %.4f, want 0", got)
	}
	if got := MinskyRateAdjustment(MinskySpeculative); math.Abs(got-0.005) > 1e-9 {
		t.Errorf("Speculative adjustment: got %.4f, want 0.005", got)
	}
	if got := MinskyRateAdjustment(MinskyPonzi); math.Abs(got+0.005) > 1e-9 {
		t.Errorf("Ponzi adjustment: got %.4f, want -0.005", got)
	}
}

// TestTriggerMinskyMoment_TriggerAndCooldown 庞氏 >30% 触发 + 冷却。
func TestTriggerMinskyMoment_TriggerAndCooldown(t *testing.T) {
	w := newTestWorld(42)
	// 构造:12 座位中 4 庞氏(33.3% > 30%)。
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		p.Loans = nil
		p.MinskyByLoan = map[string]*MinskyStatus{}
		// 4 庞氏,4 投机,4 对冲。
		var tier MinskyTier
		switch {
		case seat < 4:
			tier = MinskyPonzi
		case seat < 8:
			tier = MinskySpeculative
		default:
			tier = MinskyHedge
		}
		p.MinskyByLoan["L1"] = &MinskyStatus{Tier: tier, DebtToIncome: 0.8}
	}
	initialStock := w.Market.StockIndex
	res := &SettleResult{Month: w.Month, Events: []EventRecord{}}
	w.triggerMinskyMoment(res)

	// 触发后冷却 12 月。
	if w.MinskyMomentCooldown != 12 {
		t.Errorf("MinskyMomentCooldown: got %d, want 12", w.MinskyMomentCooldown)
	}
	if w.MinskyMomentCount != 1 {
		t.Errorf("MinskyMomentCount: got %d, want 1", w.MinskyMomentCount)
	}
	// 市场指数 -50%。
	if math.Abs(w.Market.StockIndex-initialStock*0.5) > 1e-6 {
		t.Errorf("StockIndex: got %.4f, want %.4f", w.Market.StockIndex, initialStock*0.5)
	}
	// 触发事件已 emit。
	found := false
	for _, e := range w.Events {
		if e.Type == "minsky" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected minsky event not emitted")
	}

	// 冷却期内不重复触发。
	before := w.MinskyMomentCount
	w.MinskyMomentCooldown = 5
	w.triggerMinskyMoment(res)
	if w.MinskyMomentCount != before {
		t.Errorf("Should not trigger during cooldown: got count %d, want %d", w.MinskyMomentCount, before)
	}
}

// TestTriggerMinskyMoment_PonziLiquidate 庞氏强制平仓所有杠杆资产。
func TestTriggerMinskyMoment_PonziLiquidate(t *testing.T) {
	w := newTestWorld(42)
	// 单一庞氏玩家 + 若干对冲玩家(>30% 触发需要 >30% 庞氏,所以 1/4 = 25% 不触发)。
	// 构造 4 庞氏 / 12 总 = 33.3% > 30% 触发。
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		if seat < 4 {
			p.MinskyByLoan["L1"] = &MinskyStatus{Tier: MinskyPonzi, DebtToIncome: 0.9}
			p.Assets = []Asset{
				{Kind: AssetStockIndex, Units: 100, CostCNY: 350, OpenMonth: 1},
				{Kind: AssetSideBusiness, Units: 1, CostCNY: 0, OpenMonth: 1},
				{Kind: AssetBond, Units: 100, CostCNY: 100, OpenMonth: 1},
			}
		} else {
			p.MinskyByLoan["L1"] = &MinskyStatus{Tier: MinskyHedge, DebtToIncome: 0.2}
			p.Assets = []Asset{
				{Kind: AssetStockIndex, Units: 100, CostCNY: 350, OpenMonth: 1},
			}
		}
	}
	stockBefore := w.Market.StockIndex
	res := &SettleResult{Month: w.Month, Events: []EventRecord{}}
	w.triggerMinskyMoment(res)

	// 庞氏玩家杠杆资产(stock_index/side_business)归零,债券保留。
	for seat := 0; seat < 4; seat++ {
		p := w.Players[seat]
		for _, a := range p.Assets {
			if a.Kind == AssetStockIndex || a.Kind == AssetSideBusiness {
				t.Errorf("Ponzi seat %d should have no %s, got %.2f units", seat, a.Kind, a.Units)
			}
		}
		// 债券保留。
		if bond := p.assetOf(AssetBond); bond == nil || bond.Units != 100 {
			t.Errorf("Ponzi seat %d bond should be preserved", seat)
		}
	}

	// 对冲玩家不受影响。
	for seat := 4; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		if stock := p.assetOf(AssetStockIndex); stock == nil || stock.Units != 100 {
			t.Errorf("Hedge seat %d stock_index should be preserved", seat)
		}
	}

	// 市场指数 -5%? 实际上是 -50%。
	if math.Abs(w.Market.StockIndex-stockBefore*0.5) > 1e-6 {
		t.Errorf("StockIndex: got %.4f, want %.4f", w.Market.StockIndex, stockBefore*0.5)
	}
}

// TestTriggerMinskyMoment_NoTrigger 庞氏占比 ≤30% 不触发。
func TestTriggerMinskyMoment_NoTrigger(t *testing.T) {
	w := newTestWorld(42)
	// 12 座位中 3 庞氏(25% ≤ 30%)。
	for seat := 0; seat < MaxSeats; seat++ {
		p := w.Players[seat]
		if p == nil {
			continue
		}
		tier := MinskyHedge
		if seat < 3 {
			tier = MinskyPonzi
		}
		p.MinskyByLoan["L1"] = &MinskyStatus{Tier: tier, DebtToIncome: 0.9}
	}
	stockBefore := w.Market.StockIndex
	res := &SettleResult{Month: w.Month, Events: []EventRecord{}}
	w.triggerMinskyMoment(res)

	if w.MinskyMomentCount != 0 {
		t.Errorf("Should not trigger below 30%%: got count %d, want 0", w.MinskyMomentCount)
	}
	if w.Market.StockIndex != stockBefore {
		t.Errorf("StockIndex should be unchanged: got %.4f, want %.4f", w.Market.StockIndex, stockBefore)
	}
}

// TestPlayerDominantMinskyTier 主导等级取最差。
func TestPlayerDominantMinskyTier(t *testing.T) {
	w := newTestWorld(42)
	p := w.Players[0]
	if p == nil {
		t.Fatal("seat 0 nil")
	}
	// 单一对冲。
	p.MinskyByLoan["L1"] = &MinskyStatus{Tier: MinskyHedge}
	if got := w.playerDominantMinskyTier(p); got != MinskyHedge {
		t.Errorf("Single Hedge: got %s, want %s", got, MinskyHedge)
	}
	// 对冲 + 投机 → 投机。
	p.MinskyByLoan["L2"] = &MinskyStatus{Tier: MinskySpeculative}
	if got := w.playerDominantMinskyTier(p); got != MinskySpeculative {
		t.Errorf("Hedge+Spec: got %s, want %s", got, MinskySpeculative)
	}
	// 对冲 + 投机 + 庞氏 → 庞氏。
	p.MinskyByLoan["L3"] = &MinskyStatus{Tier: MinskyPonzi}
	if got := w.playerDominantMinskyTier(p); got != MinskyPonzi {
		t.Errorf("Hedge+Spec+Ponzi: got %s, want %s", got, MinskyPonzi)
	}
}

// newTestWorld 构造最小测试世界(12 座位,交替职业卡)。
func newTestWorld(seed int64) *World {
	cards := [MaxSeats]profession.Card{}
	// 使用最小化职业卡填充(引擎内只读字段)。
	for i := 0; i < MaxSeats; i++ {
		cards[i] = profession.Card{
			ID:             "test_card",
			Title:          "测试职业",
			Name:           "测试",
			Salary:         10000,
			Expense:        3000,
			Savings:        50000,
			CreditScore:    700,
			Energy:         5,
			Network:        5,
			Cognition:      5,
			HomeDistrict:   "finance",
			HealthGrade:    "A",
			RiskPreference: "balanced",
			Marital:        "single",
		}
	}
	w := NewWorld(seed, cards)
	w.StartGame()
	return w
}
