// Package virtual_city — goods_test.go: 消费品市场 + 内生 CPI + 恩格尔分配单测
// (2026-09-16 §财商流P1-2,契约文档 §10.1)。
package virtual_city

import (
	"math"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestGoods_BasketWeights 权重和恒为 1.00(§2.1)。
func TestGoods_BasketWeights(t *testing.T) {
	g := NewGoodsMarket()
	sum := 0.0
	for _, id := range goodsOrder {
		it, ok := g.Items[id]
		if !ok || it == nil {
			t.Fatalf("missing goods item: %s", id)
		}
		if it.Weight != goodsWeights[id] {
			t.Errorf("%s weight: got %f, want %f", id, it.Weight, goodsWeights[id])
		}
		if it.PriceIdx != 100 || g.SupplyIdx[id] != 100 {
			t.Errorf("%s initial idx: price=%f supply=%f, want 100/100", id, it.PriceIdx, g.SupplyIdx[id])
		}
		sum += it.Weight
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("weights sum: got %f, want 1.00", sum)
	}
}

// TestGoods_PriceRatioClamp 手算边界:DemandIdx/SupplyIdx(同为基期 100 指数)
// 的 0.4 次幂 clamp 到 [0.96, 1.05](§10.1「价格更新确定性」的 clamp 边界复算)。
func TestGoods_PriceRatioClamp(t *testing.T) {
	// 需求 = 供给 → ratio = 1(均衡)。
	if r := goodsPriceRatio(100, 100); math.Abs(r-1.0) > 1e-9 {
		t.Errorf("ratio(100,100): got %f, want 1.0", r)
	}
	// 需求暴涨(300 vs 100)→ 3^0.4≈1.55 → 上限 1.05。
	if r := goodsPriceRatio(300, 100); math.Abs(r-1.05) > 1e-9 {
		t.Errorf("ratio(300,100): got %f, want 1.05 (clamped)", r)
	}
	// 需求枯竭 → 下限 0.96。
	if r := goodsPriceRatio(0, 100); math.Abs(r-0.96) > 1e-9 {
		t.Errorf("ratio(0,100): got %f, want 0.96 (clamped)", r)
	}
	// 手算中间点:demand/supply=1.21 → 1.21^0.4 ≈ 1.0793 → clamp 1.05。
	if r := goodsPriceRatio(121, 100); r != 1.05 {
		t.Errorf("ratio(121,100): got %f, want 1.05", r)
	}
	// 未触界:demand/supply=1.1 → 1.1^0.4 ≈ 1.039。
	if r := goodsPriceRatio(110, 100); math.Abs(r-math.Pow(1.1, 0.4)) > 1e-9 {
		t.Errorf("ratio(110,100): got %f, want %f", r, math.Pow(1.1, 0.4))
	}
}

// TestGoods_MoneyEffectClamp 货币传导:M2GrowthYoY−RealGrowth = ±0.2 →
// 命中 ±clamp(§10.1)。
func TestGoods_MoneyEffectClamp(t *testing.T) {
	cb := NewCentralBank()
	cb.RealGDPGrowth = 0.05
	// +0.2 差口:0.2×0.05 = +0.01 → 恰在上限。
	cb.M2GrowthYoY = 0.25
	if e := goodsMoneyEffect(cb); math.Abs(e-0.01) > 1e-12 {
		t.Errorf("money effect +0.2 gap: got %f, want +0.01", e)
	}
	// −0.2 差口:−0.01 → clamp 到 −0.005。
	cb.M2GrowthYoY = -0.15
	if e := goodsMoneyEffect(cb); math.Abs(e+0.005) > 1e-12 {
		t.Errorf("money effect -0.2 gap: got %f, want -0.005", e)
	}
	// 零差口 → 0;CB nil → 0。
	cb.M2GrowthYoY = 0.05
	if e := goodsMoneyEffect(cb); e != 0 {
		t.Errorf("money effect zero gap: got %f, want 0", e)
	}
	if e := goodsMoneyEffect(nil); e != 0 {
		t.Errorf("money effect nil CB: got %f, want 0", e)
	}
}

// TestGoods_MonthStepDeterministic_CPIFormulas 固定 seed 连续 24 月:
// (1) 同 seed 双世界对拍完全一致;(2) CPIMom = Σ weight×MomChange;
// (3) 12 月后 CPIYoYReady 且 CPIYoY = Π(1+mom, 最近 12)−1。
func TestGoods_MonthStepDeterministic_CPIFormulas(t *testing.T) {
	run := func() *World {
		w := NewWorld(20260916, emptyCardsFor(0))
		// 均衡货币差口 → moneyEffect = 0,便于手算。
		w.CB.M2GrowthYoY = 0.05
		w.CB.RealGDPGrowth = 0.05
		for i := 0; i < 24; i++ {
			w.GoodsMonthStep()
		}
		return w
	}
	w1, w2 := run(), run()
	for _, id := range goodsOrder {
		a, b := w1.Goods.Items[id], w2.Goods.Items[id]
		if a.PriceIdx != b.PriceIdx || a.MomChange != b.MomChange {
			t.Errorf("%s not deterministic: %+v vs %+v", id, a, b)
		}
	}

	g := w1.Goods
	// CPIMom = Σ weight × MomChange。
	wantMom := 0.0
	for _, id := range goodsOrder {
		wantMom += g.Items[id].Weight * g.Items[id].MomChange
	}
	if math.Abs(g.CPIMom-wantMom) > 1e-12 {
		t.Errorf("CPIMom: got %f, want %f", g.CPIMom, wantMom)
	}
	// 24 月 → Ready;CPIYoY = Π(1+mom, 最近 12) − 1。
	if !g.CPIYoYReady() {
		t.Fatal("CPIYoYReady should be true after 24 months")
	}
	yoy := 1.0
	for _, m := range g.history[len(g.history)-12:] {
		yoy *= 1 + m
	}
	if math.Abs(g.CPIYoY-(yoy-1)) > 1e-12 {
		t.Errorf("CPIYoY: got %f, want %f", g.CPIYoY, yoy-1)
	}
	// 单月环比在合理区间(clamp × moneyEffect)。
	for _, id := range goodsOrder {
		m := g.Items[id].MomChange
		if m < -0.05 || m > 0.06 {
			t.Errorf("%s mom out of range: %f", id, m)
		}
	}
}

// TestGoods_CPIYoYNotReadyBefore12 不足 12 月:值保留但 Ready=false(§2.3)。
func TestGoods_CPIYoYNotReadyBefore12(t *testing.T) {
	w := NewWorld(7, emptyCardsFor(0))
	for i := 0; i < 11; i++ {
		w.GoodsMonthStep()
	}
	if w.Goods.CPIYoYReady() {
		t.Error("CPIYoYReady should be false before 12 months")
	}
	if w.Goods.CPIYoY == 0 && w.Goods.CPIMom == 0 {
		t.Error("CPI values should be retained even when not ready")
	}
	w.GoodsMonthStep()
	if !w.Goods.CPIYoYReady() {
		t.Error("CPIYoYReady should be true after 12 months")
	}
}

// TestGoods_HousingLinkage 手动抬升 DistrictIdx 1% → MomChange_housing ≈ 0.3%
// (§10.1 housing 联动;货币差口置 0 以隔离联动系数)。
func TestGoods_HousingLinkage(t *testing.T) {
	w := NewWorld(3, emptyCardsFor(0))
	w.CB.M2GrowthYoY = 0.05
	w.CB.RealGDPGrowth = 0.05
	w.GoodsMonthStep() // 捕获各区基准快照
	for _, d := range DistrictDefs {
		w.Market.DistrictIdx[d.ID] *= 1.01
	}
	w.GoodsMonthStep()
	mom := w.Goods.Items["housing"].MomChange
	if math.Abs(mom-0.003) > 1e-9 {
		t.Errorf("housing mom after +1%% district idx: got %f, want 0.003 (0.3×1%%)", mom)
	}
	// 首月(无基准快照)housing 环比应为 0 × moneyEffect = 0。
	w2 := NewWorld(3, emptyCardsFor(0))
	w2.CB.M2GrowthYoY = 0.05
	w2.CB.RealGDPGrowth = 0.05
	w2.GoodsMonthStep()
	if m := w2.Goods.Items["housing"].MomChange; math.Abs(m) > 1e-12 {
		t.Errorf("first-month housing mom should be 0, got %f", m)
	}
}

// TestEngelShares_HighIncomeFoodFloor 高收入玩家 foodShare 落 0.15 下限(§10.1)。
func TestEngelShares_HighIncomeFoodFloor(t *testing.T) {
	shares := engelShares(200000, 1, 0, 30)
	if math.Abs(shares["food"]-0.15) > 1e-9 {
		t.Errorf("high-income food share: got %f, want 0.15 (floor)", shares["food"])
	}
	sum := 0.0
	for _, v := range shares {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("shares sum: got %f, want 1.0", sum)
	}
}

// TestEngelShares_FourKidsScalingTriggered 4 孩低收入家庭:四类(食品/居住/
// 教育/医疗)份额和 > 0.90 → 等比缩放至 0.90;其余四类瓜分 0.10(§10.1)。
func TestEngelShares_FourKidsScalingTriggered(t *testing.T) {
	shares := engelShares(5000, 6, 4, 30) // 家庭 6 人(夫妻+4 孩),月入 5000
	big4 := shares["food"] + shares["housing"] + shares["education"] + shares["healthcare"]
	if math.Abs(big4-0.90) > 1e-9 {
		t.Errorf("big-4 share sum: got %f, want 0.90 (scaled)", big4)
	}
	rest := shares["clothing"] + shares["household"] + shares["transport"] + shares["misc"]
	if math.Abs(rest-0.10) > 1e-9 {
		t.Errorf("rest share sum: got %f, want 0.10", rest)
	}
	sum := 0.0
	for _, v := range shares {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("shares sum: got %f, want 1.0", sum)
	}
	// 教育份额接近上限:0.08+0.16=0.24 缩放后 0.24×(0.9/0.99)≈0.218。
	if shares["education"] < 0.20 || shares["education"] > 0.25 {
		t.Errorf("education share for 4 kids: got %f, want ~0.218", shares["education"])
	}
}

// TestSplitEngel_Conservation Σ ConsumptionByGoods == living+familyLiving
// (金额守恒,§10.1;含家庭表拆分口径)。
func TestSplitEngel_Conservation(t *testing.T) {
	p := newPlayerFromCard(0, synthCard(0, 100000))
	p.Family.Marital = "married"
	p.Family.Children = 2
	p.Card.EldersDependent = 1
	living := int64(6000)
	familyLiving := int64(livingSpouseCNY) + 2*int64(livingChildCNY) + 1*int64(livingElderCNY)
	m := splitEngel(living+familyLiving, p, 40)
	var sum int64
	for _, v := range m {
		sum += int64(v)
	}
	if sum != living+familyLiving {
		t.Errorf("engel conservation: sum=%d, want %d (living %d + family %d)", sum, living+familyLiving, living, familyLiving)
	}
	// 家庭表:教育 = 2 孩 × 5000 × 0.55 = 5500 起步(再加本人份额)。
	if m["education"] < 5500 {
		t.Errorf("education split: got %f, want >= 5500 (2 kids × 0.55)", m["education"])
	}
	// 医疗 = 老人 1000×0.60 = 600 起步。
	if m["healthcare"] < 600 {
		t.Errorf("healthcare split: got %f, want >= 600 (elder × 0.60)", m["healthcare"])
	}
	// 8 类俱全。
	for _, id := range goodsOrder {
		if _, ok := m[id]; !ok {
			t.Errorf("missing goods category in split: %s", id)
		}
	}
}

// TestSplitEngel_ZeroTotal 零消费 → 空映射(Σ=0 守恒)。
func TestSplitEngel_ZeroTotal(t *testing.T) {
	p := newPlayerFromCard(0, synthCard(0, 0))
	m := splitEngel(0, p, 30)
	if len(m) != 0 {
		t.Errorf("splitEngel(0): got %v, want empty map", m)
	}
}

// TestConsumptionLevelSafe 兜底:map nil 或档位越界 → 1;0 是合法档位(§3.1 零值陷阱)。
func TestConsumptionLevelSafe(t *testing.T) {
	p := newPlayerFromCard(0, profession.Card{ID: "P01"})
	if p.ConsumptionLevel != 1 {
		t.Errorf("newPlayerFromCard default level: got %d, want 1", p.ConsumptionLevel)
	}
	if got := p.ConsumptionLevelSafe(); got != 1 {
		t.Errorf("nil map safe level: got %d, want 1", got)
	}
	// 0 是合法档位(节俭):map 已初始化时必须原样返回。
	p.ConsumptionByGoods = map[string]float64{"food": 1}
	p.ConsumptionLevel = 0
	if got := p.ConsumptionLevelSafe(); got != 0 {
		t.Errorf("level 0 must be legal: got %d, want 0", got)
	}
	p.ConsumptionLevel = 4 // 越界
	if got := p.ConsumptionLevelSafe(); got != 1 {
		t.Errorf("out-of-range level: got %d, want 1", got)
	}
}
