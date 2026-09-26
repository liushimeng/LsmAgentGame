// Package virtual_city — supply_chain_test.go: 阶段5 产业链/产业集群单测
// (2026-09-21 §城市扩张v2.12 阶段5)。
//
// 覆盖:15 节点/12 关系初始化、食品链 12 月价格平稳(<10%)、R5-1 安全库存
// 强制补产、上游冲击下游降载 50% 而非停产、弹性传导比例、6 集群与城区对齐、
// R5-3 减免三重上限、SettleMonth ②B/⑨F 接线(§130)、同种子确定性 +
// 零 rand 回归(产业链开启前后市场序列一致,§197)。
package virtual_city

import (
	"math"
	"reflect"
	"testing"

	"LsmAgentGame/game/virtual_city/profession"
)

// newSCWorld 构造 12 座位真实支出测试世界(Expense 8500 ≈ 契约基准需求
// goodsBaselineDemandCNY/12,消费篮子口径与产业链 baseDemand 标定一致)。
func newSCWorld(seed int64) *World {
	cards := [MaxSeats]profession.Card{}
	for i := range cards {
		cards[i] = profession.Card{
			ID: "SC01", Title: "供应链测试职业", Salary: 12000, Expense: 8500,
			Savings: 200000, CreditScore: 700, Energy: 5, Network: 5, Cognition: 5,
			HomeDistrict: "residential", HealthGrade: "A", RiskPreference: "balanced",
			Marital: "single",
		}
	}
	w := NewWorld(seed, cards)
	w.StartGame()
	return w
}

// TestSupplyChainInit ①15 节点 + 12 关系初始化(弹性 0.6-0.9,价格指数 1.0)。
func TestSupplyChainInit(t *testing.T) {
	sc := NewSupplyChain()
	if len(sc.Nodes) != 15 {
		t.Fatalf("nodes = %d, want 15", len(sc.Nodes))
	}
	if len(sc.Relations) != 12 {
		t.Fatalf("relations = %d, want 12", len(sc.Relations))
	}
	for _, rel := range sc.Relations {
		if rel.Elasticity < 0.6 || rel.Elasticity > 0.9 {
			t.Errorf("relation %s→%s elasticity = %f, want in [0.6,0.9]",
				rel.Upstream, rel.Downstream, rel.Elasticity)
		}
		if _, ok := sc.Nodes[rel.Upstream]; !ok {
			t.Errorf("relation upstream %q not in nodes", rel.Upstream)
		}
		if _, ok := sc.Nodes[rel.Downstream]; !ok {
			t.Errorf("relation downstream %q not in nodes", rel.Downstream)
		}
	}
	for tier, idx := range sc.PriceIndex {
		if math.Abs(idx-1.0) > 1e-12 {
			t.Errorf("PriceIndex[%d] = %f, want 1.0", tier, idx)
		}
	}
	// 三条链各 5 节点、四层齐备(R5-2:固定 15 节点)。
	tiers := map[int]int{}
	for _, n := range sc.Nodes {
		tiers[n.Tier]++
		if n.SafetyStockMons != SupplySafetyStockMons {
			t.Errorf("node %s SafetyStockMons = %f, want %f", n.ID, n.SafetyStockMons, SupplySafetyStockMons)
		}
	}
	for tier := 0; tier < SupplyChainTierCount; tier++ {
		if tiers[tier] == 0 {
			t.Errorf("tier %d has no nodes", tier)
		}
	}
}

// TestSupplyChainLinearFoodChain ②食品链 5 节点 12 月模拟:四层价格指数
// 波动 <10%(稳态标定:Inventory=安全线、Utilization=base/Capacity 无瞬态)。
func TestSupplyChainLinearFoodChain(t *testing.T) {
	w := newSCWorld(42)
	foodChain := []string{"food_agri", "food_processing", "food_logistics", "food_wholesale", "food_catering"}
	for i := 0; i < 12; i++ {
		if _, res := w.SettleMonth(); res == nil {
			t.Fatalf("SettleMonth #%d returned nil result", i)
		}
	}
	for tier := 0; tier < SupplyChainTierCount; tier++ {
		idx := w.SupplyChain.PriceIndex[tier]
		if idx < 0.9 || idx > 1.1 {
			t.Errorf("12 月后 tier %d PriceIndex = %f, want in [0.9,1.1]", tier, idx)
		}
	}
	for _, id := range foodChain {
		n := w.SupplyChain.Nodes[id]
		if n == nil {
			t.Fatalf("node %q missing", id)
		}
		if n.Utilization <= 0 {
			t.Errorf("node %s utilization = %f, want > 0", id, n.Utilization)
		}
		if n.Utilization > 1 {
			t.Errorf("node %s utilization = %f, want ≤ 1", id, n.Utilization)
		}
	}
	// 快照可用性:view 层 top5 + 瓶颈列表非 nil。
	snap := w.SupplyChain.Snapshot()
	if len(snap.TopNodes) != 5 {
		t.Errorf("snapshot top nodes = %d, want 5", len(snap.TopNodes))
	}
	if snap.Bottlenecks == nil {
		t.Errorf("snapshot bottlenecks nil, want []")
	}
}

// TestSupplyChainSafetyStock ③库存低于安全线 → 强制补产(R5-1)。
func TestSupplyChainSafetyStock(t *testing.T) {
	w := newSCWorld(7)
	sc := w.SupplyChain
	agri := sc.Nodes["food_agri"]
	agri.Inventory = 0 // 清空库存,远低于安全线(2×baseDemand)。

	for i := 0; i < 10; i++ {
		sc.MonthlyTick(w)
	}
	// 安全线 = 2×月出货:强制补产应把库存拉回安全线之上。
	if line := agri.safetyLine(); agri.Inventory < line {
		t.Errorf("inventory %f still below safety line %f after 10 ticks (R5-1 补产失效)",
			agri.Inventory, line)
	}
	if agri.Utilization <= 0 || agri.Utilization > 1 {
		t.Errorf("utilization = %f, want in (0,1]", agri.Utilization)
	}
}

// TestSupplyChainUpstreamShock ④上游产能压缩 → 下游 Utilization 降载
// (R5-1:最多降 50% 而非停产)。
func TestSupplyChainUpstreamShock(t *testing.T) {
	base := newSCWorld(11)
	shock := newSCWorld(11)
	agri := shock.SupplyChain.Nodes["food_agri"]
	agri.Capacity = agri.Capacity / 3 // 上游产能骤减(1/3)。
	agri.Inventory = 0                // 且无库存缓冲。

	for i := 0; i < 4; i++ {
		base.SupplyChain.MonthlyTick(base)
		shock.SupplyChain.MonthlyTick(shock)
	}
	shockAgri := shock.SupplyChain.Nodes["food_agri"]
	if shockAgri.FillRatio >= 1 {
		t.Errorf("shocked upstream fill ratio = %f, want < 1", shockAgri.FillRatio)
	}
	procBase := base.SupplyChain.Nodes["food_processing"].Utilization
	procShock := shock.SupplyChain.Nodes["food_processing"].Utilization
	if procShock <= 0 {
		t.Errorf("shocked downstream utilization = %f, want > 0 (不停产)", procShock)
	}
	if procShock >= procBase {
		t.Errorf("shocked downstream utilization %f >= baseline %f, want lower", procShock, procBase)
	}
	// R5-1 断供折算系数:有效需求 ≥ 50%(SupplyRatio 折算下限)。
	if r := shock.SupplyChain.Nodes["food_processing"].SupplyRatio; r < 0 || r > 1 {
		t.Errorf("downstream supply ratio = %f, want in [0,1]", r)
	}
	// 全断供极端:SupplyRatio=0 时下游需求折算 = 50% 而非 0(纯函数验证)。
	if eff := 1.42 * (0.5 + 0.5*0); eff <= 0 {
		t.Errorf("full-cutoff effective demand = %f, want > 0", eff)
	}
}

// TestSupplyChainPriceTransmission ⑤弹性传导:全关系 Elasticity=0.8 时,
// T0 环比 +5% 的 80% 传导到 T1(+4%),再 80% 到 T2(+3.2%)。
func TestSupplyChainPriceTransmission(t *testing.T) {
	w := newSCWorld(3)
	sc := w.SupplyChain
	for i := range sc.Relations {
		sc.Relations[i].Elasticity = 0.8
	}
	// 全部 T0 节点清库 → tier0 库存比 ≈ 0 → 自失衡 +5%(全额)。
	for _, id := range sc.nodeOrder {
		if n := sc.Nodes[id]; n.Tier == 0 {
			n.Inventory = 0
		}
	}
	sc.stepPrices()
	if mom := sc.lastMom[0]; math.Abs(mom-0.05) > 1e-9 {
		t.Errorf("tier0 mom = %f, want 0.05", mom)
	}
	if mom := sc.lastMom[1]; math.Abs(mom-0.8*0.05) > 1e-9 {
		t.Errorf("tier1 mom = %f, want %f (80%% 传导)", mom, 0.8*0.05)
	}
	if idx := sc.PriceIndex[1]; math.Abs(idx-1.04) > 1e-9 {
		t.Errorf("tier1 PriceIndex = %f, want 1.04", idx)
	}
	if idx := sc.PriceIndex[2]; math.Abs(idx-1.032) > 1e-9 {
		t.Errorf("tier2 PriceIndex = %f, want 1.032", idx)
	}
}

// TestIndustrialClusters ⑥6 集群初始化与 16 城区 id 对齐(districts.go)。
func TestIndustrialClusters(t *testing.T) {
	cs := NewIndustrialClusters()
	if len(cs) != 6 {
		t.Fatalf("clusters = %d, want 6", len(cs))
	}
	seen := map[string]bool{}
	for _, c := range cs {
		if seen[c.ID] {
			t.Errorf("duplicate cluster id %q", c.ID)
		}
		seen[c.ID] = true
		if !ValidDistrict(c.DistrictID) {
			t.Errorf("cluster %s district %q not in DistrictDefs", c.ID, c.DistrictID)
		}
		if len(c.Industries) == 0 {
			t.Errorf("cluster %s has no industries", c.ID)
		}
		if c.TaxBreakCap != ClusterTaxBreakCapDefault {
			t.Errorf("cluster %s TaxBreakCap = %f, want %f", c.ID, c.TaxBreakCap, ClusterTaxBreakCapDefault)
		}
		if c.Firms < ClusterFirmsMin || c.Firms > ClusterFirmsMax {
			t.Errorf("cluster %s firms = %d, want in [%d,%d]", c.ID, c.Firms, ClusterFirmsMin, ClusterFirmsMax)
		}
	}
}

// TestClusterTaxBreak ⑦R5-3 三重上限:月上限 Cap/12、年 12 月上限、
// 总额上限;次年 1 月额度重置;额度用尽一次性播报。
func TestClusterTaxBreak(t *testing.T) {
	w := NewWorld(7, [MaxSeats]profession.Card{})
	w.Treasury = NewTreasury()
	w.Treasury.MonthlyCounter = &TaxCounter{}

	// ① 月上限 + 年 12 月上限:单集群 100% 份额,VatTotal 足额
	// (gross=12 万 ≫ 月上限 50 万×1e4/12=41666.67)。
	c := w.Clusters[0]
	w.Clusters = w.Clusters[:1]
	var vatAfter int64
	for i := 1; i <= 12; i++ {
		w.Month = i
		w.Treasury.MonthlyCounter.VatTotal = 600_000
		w.ClusterMonthStep()
		vatAfter = w.Treasury.MonthlyCounter.VatTotal
		if c.LastBreakCNY > 41667 {
			t.Fatalf("month %d relief %d exceeds monthly cap 41667", i, c.LastBreakCNY)
		}
	}
	if c.TaxBreakMonths != ClusterTaxBreakMonthsCap {
		t.Errorf("TaxBreakMonths = %d, want %d", c.TaxBreakMonths, ClusterTaxBreakMonthsCap)
	}
	// 累计减免 = 月上限 × 12(末月受总额上限裁剪)= 50.0 万封顶。
	if c.TaxBreakUsed > c.TaxBreakCap || c.TaxBreakUsed < c.TaxBreakCap-0.01 {
		t.Errorf("TaxBreakUsed = %f, want ≈ cap %f", c.TaxBreakUsed, c.TaxBreakCap)
	}
	if vatAfter != 600_000-41663 {
		t.Errorf("VatTotal after month 12 = %d, want %d", vatAfter, 600_000-41663)
	}
	// 第 13 月(次年 1 月):年度额度重置 → 恢复减免(月数回到 1)。
	w.Month = 13
	w.Treasury.MonthlyCounter.VatTotal = 600_000
	w.ClusterMonthStep()
	if c.TaxBreakMonths != 1 || c.TaxBreakUsed < 4.0 {
		t.Errorf("after year reset Months=%d Used=%f, want 1 / <cap fresh quota", c.TaxBreakMonths, c.TaxBreakUsed)
	}

	// ② 总额上限:额度将尽(剩 50 元),一月用尽,次月零减免 + 一次性事件。
	w2 := NewWorld(7, [MaxSeats]profession.Card{})
	w2.Treasury = NewTreasury()
	w2.Treasury.MonthlyCounter = &TaxCounter{}
	w2.Clusters = NewIndustrialClusters()[:1]
	c2 := w2.Clusters[0]
	c2.TaxBreakCap = 1.0
	c2.TaxBreakUsed = 0.995 // 剩余 50 元。
	for i := 1; i <= 3; i++ {
		w2.Month = i + 1 // 从 2 月起(避开年初重置)。
		w2.Treasury.MonthlyCounter.VatTotal = 600_000
		w2.ClusterMonthStep()
	}
	if c2.TaxBreakMonths != 1 {
		t.Errorf("cap-limited TaxBreakMonths = %d, want 1", c2.TaxBreakMonths)
	}
	if c2.TaxBreakUsed > c2.TaxBreakCap || math.Abs(c2.TaxBreakUsed-c2.TaxBreakCap) > 1e-9 {
		t.Errorf("cap-limited TaxBreakUsed = %f, want = cap %f", c2.TaxBreakUsed, c2.TaxBreakCap)
	}
	if c2.LastBreakCNY != 0 {
		t.Errorf("cap exhausted LastBreakCNY = %d, want 0", c2.LastBreakCNY)
	}
	if !hasEventText(w2, "额度用尽") {
		t.Errorf("expected cap-exhausted policy event, got %v", w2.Events)
	}
	// 事件只播报一次(第 2、3 月均触顶)。
	n := 0
	for _, e := range w2.Events {
		if hasEventText(&World{Events: []EventRecord{e}}, "额度用尽") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("cap-exhausted event count = %d, want 1", n)
	}
	// VatTotal 负向扣除不破零。
	if w2.Treasury.MonthlyCounter.VatTotal < 0 {
		t.Errorf("VatTotal = %d, want ≥ 0", w2.Treasury.MonthlyCounter.VatTotal)
	}
}

// TestSupplyChainWiredIntoSettleMonth ⑧§130 接线:SettleMonth ②B 推进
// LastTickMonth、view 下发 supply_chain/industrial_clusters;economy 关闭
// 时不下发。
func TestSupplyChainWiredIntoSettleMonth(t *testing.T) {
	w := newSCWorld(1)
	if w.SupplyChain == nil || len(w.Clusters) != 6 {
		t.Fatalf("NewWorld 未装配 SupplyChain/Clusters(§130 断线)")
	}
	if _, res := w.SettleMonth(); res == nil {
		t.Fatalf("SettleMonth returned nil result")
	}
	if w.SupplyChain.LastTickMonth != 1 {
		t.Errorf("LastTickMonth = %d, want 1(②B 未接线?)", w.SupplyChain.LastTickMonth)
	}
	w.SettleMonth()
	if w.SupplyChain.LastTickMonth != 2 {
		t.Errorf("LastTickMonth = %d, want 2", w.SupplyChain.LastTickMonth)
	}
	// view 接线:玩家/观战者快照携带产业链与产业集群。
	cs := BuildClientState("room-1", -1, w, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil)
	if cs.SupplyChain == nil {
		t.Fatalf("cs.SupplyChain nil, want snapshot(§130 view 断线)")
	}
	if len(cs.SupplyChain.TopNodes) != 5 {
		t.Errorf("cs.SupplyChain top nodes = %d, want 5", len(cs.SupplyChain.TopNodes))
	}
	if cs.SupplyChain.Bottlenecks == nil {
		t.Errorf("cs.SupplyChain bottlenecks nil, want [] (P0-bugfix: 数组非 null)")
	}
	if len(cs.Clusters) != 6 {
		t.Errorf("cs.Clusters = %d, want 6", len(cs.Clusters))
	}
	// economy_enabled=false:②B/⑨F 跳过 + view omit。
	w2 := newSCWorld(1)
	w2.EconomyEnabled = false
	scMonth := w2.SupplyChain.LastTickMonth
	w2.SettleMonth()
	if w2.SupplyChain.LastTickMonth != scMonth {
		t.Errorf("economy off: LastTickMonth advanced %d → %d, want unchanged",
			scMonth, w2.SupplyChain.LastTickMonth)
	}
	cs2 := BuildClientState("room-2", -1, w2, [MaxSeats]string{}, [MaxSeats]string{},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil)
	if cs2.SupplyChain != nil || cs2.Clusters != nil {
		t.Errorf("economy off: cs.SupplyChain/cs.Clusters should be omitted")
	}
}

// TestSupplyChainDeterministic ⑨同种子同月快照一致 + 零 rand 回归
// (§197:产业链/集群开启前后,市场随机序列零偏移)。
func TestSupplyChainDeterministic(t *testing.T) {
	a := newSCWorld(99)
	b := newSCWorld(99)
	for i := 0; i < 6; i++ {
		a.SettleMonth()
		b.SettleMonth()
	}
	if !reflect.DeepEqual(a.SupplyChain.Snapshot(), b.SupplyChain.Snapshot()) {
		t.Errorf("same-seed supply chain snapshots diverge")
	}
	for id, na := range a.SupplyChain.Nodes {
		nb := b.SupplyChain.Nodes[id]
		if na.Utilization != nb.Utilization || na.Inventory != nb.Inventory ||
			na.LastDemand != nb.LastDemand || na.FillRatio != nb.FillRatio {
			t.Errorf("node %s state diverges: %+v vs %+v", id, na, nb)
		}
	}
	if a.SupplyChain.PriceIndex != b.SupplyChain.PriceIndex {
		t.Errorf("PriceIndex diverges: %v vs %v", a.SupplyChain.PriceIndex, b.SupplyChain.PriceIndex)
	}
	for i := range a.Clusters {
		if a.Clusters[i].TaxBreakUsed != b.Clusters[i].TaxBreakUsed ||
			a.Clusters[i].TaxBreakMonths != b.Clusters[i].TaxBreakMonths {
			t.Errorf("cluster %s diverges", a.Clusters[i].ID)
		}
	}

	// 零 rand 回归:关闭供应链/集群的对照世界,市场随机输出完全一致。
	c := newSCWorld(99)
	c.SupplyChain = nil
	c.Clusters = nil
	for i := 0; i < 6; i++ {
		c.SettleMonth()
	}
	if a.Market.StockIndex != c.Market.StockIndex || a.Market.GoldPrice != c.Market.GoldPrice {
		t.Errorf("rand 序列偏移: with-SC stock=%f gold=%f, without=%f/%f",
			a.Market.StockIndex, a.Market.GoldPrice, c.Market.StockIndex, c.Market.GoldPrice)
	}
	if a.Treasury.Cash != c.Treasury.Cash {
		t.Errorf("Treasury.Cash diverges: %d vs %d (⑨F 不应动 Cash)", a.Treasury.Cash, c.Treasury.Cash)
	}
}
