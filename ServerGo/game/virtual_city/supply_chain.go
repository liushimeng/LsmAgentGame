// Package virtual_city — supply_chain.go: 企业产业链上下游系统 (2026-09-21 §城市扩张v2.12 阶段5)。
//
// 三条产业链 × 5 节点 = 15 个虚拟企业节点(非玩家企业 —— 玩家企业经营不变,
// 本文件不改动 labor.go 的 FirmSector 口径,只做产业链层的产能/库存/价格推演):
//
//	食品链    : 农业原材料(T0) → 食品加工(T1) → 物流仓储(T1) → 批发零售(T2) → 餐饮服务(T3)
//	能源链    : 采矿原材料(T0) → 能源生产(T1) → 化工新材料(T1) → 制造业(T2) → 终端消费(T3)
//	消费电子链: 电子半导体(T0) → 元器件制造(T1) → 整机组装(T2) → 品牌零售(T2) → 售后服务(T3)
//
// 月度调度(MonthlyTick,SettleMonth ②B 调用):
//  1. 需求自下而上传导:零售销售取自 Goods 消费篮子上月口径 → 订单 ×inputCoef 向上游传导;
//  2. 每节点 targetProduction = demand;actual = min(Capacity×Utilization, target)(利用率平滑);
//  3. 库存调整:Inventory += actual − shipped;低于安全库存(SafetyStockMons 月)时强制补产;
//  4. 价格传导:上游价格环比 × Elasticity 传导到下游 PriceIndex(4 层);
//  5. 产出影响 Goods 市场价格(链内紧缺→涨价 ≤5%,过剩→降价 ≤3%)。
//
// R5-1 断供防护:每节点强制保留 SafetyStockMons(默认 2)个月安全库存;上游断供
// (fillRatio<1)时下游有效需求 = demand × (0.5 + 0.5×fillRatio) —— 利用率最多
// 降 50% 而非停产(多供应商简化)。
//
// R5-2 性能:仅 15 个虚拟节点参与月度调度(不随玩家数扩展),全遍历 O(1)。
//
// 纯引擎层:无锁、无 goroutine、无 IO、**零 rand 消费**(固定种子存量对局回归
// 零偏移,§197/treasury.go 同款纪律)。数值单位:**万元**(Capacity/Inventory/
// baseDemand);与引擎其余部分(元)的换算只发生在 chainDemand 边界(×FirmScale/1e4,
// FirmScale=2.5 表示消费篮子之外的投资+政府+外需)。
package virtual_city

import "sort"

// 产业链常量(阶段5 新定)。
const (
	// SupplyChainTierCount 产业链层级数(T0 原材料 … T3 终端服务)。
	SupplyChainTierCount = 4
	// SupplyInputCoef 下游订单向上游传导的需求系数(1 元下游产值需要 0.6 元上游投入)。
	SupplyInputCoef = 0.6
	// SupplyUtilSmoothAlpha 产能利用率月度平滑系数(防单月需求脉冲直接打满/打穿)。
	SupplyUtilSmoothAlpha = 0.35
	// SupplyUtilFloor 利用率下限(惨淡但不停产;与 R5-1「降 50% 而非停产」配套)。
	SupplyUtilFloor = 0.15
	// SupplySafetyStockMons 默认安全库存月数(R5-1)。
	SupplySafetyStockMons = 2.0
	// SupplyInventoryCapMult 库存上限 = Capacity × 3(防无限堆积导致价格单边下跌)。
	SupplyInventoryCapMult = 3.0
	// SupplyShortagePriceHike 链内紧缺时单月最大涨价幅度(R5 契约:紧缺→涨价 5%)。
	SupplyShortagePriceHike = 0.05
	// SupplySurplusPriceCut 链内过剩时单月最大降价幅度(R5 契约:过剩→降价 3%)。
	SupplySurplusPriceCut = 0.03
	// SupplyMomMin / SupplyMomMax 单层价格环比 clamp(传导+自失衡合成后的总界)。
	SupplyMomMin = -0.05
	SupplyMomMax = 0.08
)

// SupplyChainNode 供应链节点(虚拟企业,非玩家企业 —— 玩家企业经营不变)。
type SupplyChainNode struct {
	ID          string  // 节点 id(如 "food_agri")
	Industry    string  // 行业名(26 L1 域规范名,对齐 city/calibration.go l1DomainNames)
	NameCN      string  // 中文节点名(快照/警告展示)
	Tier        int     // 产业链层级: 0=原材料 1=中游制造 2=下游零售 3=终端服务
	Capacity    float64 // 月产能(万元)
	Inventory   float64 // 库存(万元成本 = 万元 × UnitCost 口径;字段本身为万元量)
	UnitCost    float64 // 单位成本系数(0..1,占产值比;库存估值用)
	Utilization float64 // 产能利用率 0..1
	// SafetyStockMons 安全库存月数(强制保留,防断供 R5-1;默认 2.0)。
	SafetyStockMons float64

	// ── 动态状态(MonthlyTick 维护;快照/测试用)──
	baseDemand  float64 // 稳态月需求(万元;NewSupplyChain 播种,之后不再改写)
	LastDemand  float64 // 上月需求(万元,含下游订单传导)
	LastShipped float64 // 上月出货(万元)
	AvgShip     float64 // 月出货 EMA(α=0.2;安全库存基准)
	FillRatio   float64 // 上月履约率 0..1(= shipped/demand;上游断供判定输入)
	SupplyRatio float64 // 上游供给比 0..1(本月有效需求折算系数来源)
}

// safetyLine 安全库存线(万元)= SafetyStockMons × max(AvgShip, Capacity×5%)。
// floor 防节点长期零出货时安全线退化为 0(永不在意库存)。
func (n *SupplyChainNode) safetyLine() float64 {
	base := n.AvgShip
	if floor := n.Capacity * 0.05; base < floor {
		base = floor
	}
	return n.SafetyStockMons * base
}

// inventoryMonths 库存可用月数(快照口径;AvgShip≈0 时按安全线月数计)。
func (n *SupplyChainNode) inventoryMonths() float64 {
	d := n.AvgShip
	if d < 1e-9 {
		d = n.safetyLine() / n.SafetyStockMons
	}
	if d < 1e-9 {
		return 0
	}
	return n.Inventory / d
}

// IndustryRelation 行业上下游关系。
type IndustryRelation struct {
	Upstream   string  // 上游节点 id
	Downstream string  // 下游节点 id
	Elasticity float64 // 传导弹性 0..1(上游价格变动传导到下游的比例)
}

// SupplyChain 全城供应链网络(挂 World.SupplyChain;引擎纯状态,无锁)。
type SupplyChain struct {
	Nodes     map[string]*SupplyChainNode
	Relations []IndustryRelation
	// PriceIndex 各层级价格指数(月度,基于 1.0)。
	PriceIndex [SupplyChainTierCount]float64 // Tier 0-3

	// nodeOrder 稳定遍历序(map 迭代序不确定;聚合求和按此序保证浮点确定性)。
	nodeOrder []string
	// lastMom 上月各层环比(价格传导输入;内部)。
	lastMom [SupplyChainTierCount]float64
	// LastTickMonth 最近一次 MonthlyTick 的 w.Month(§130 接线验证)。
	LastTickMonth int
}

// supplyNodeSpec 节点静态规格(NewSupplyChain 表驱动)。
type supplyNodeSpec struct {
	id, industry, nameCN string
	tier                 int
	capacity, baseDemand float64 // 万元/月
}

// supplyChainNodeSpecs 15 节点静态表(3 链 × 5;capacity ≈ baseDemand × 1.4 头寸)。
// baseDemand 标定:基准消费篮子(goodsBaselineDemandCNY=102000 元/月)按
// goodsWeights 拆类 × FirmScale(2.5)/1e4 折万元,再按链内份额分配(见 chainDemand)。
var supplyChainNodeSpecs = []supplyNodeSpec{
	// 食品链(food 篮子 30600 元 → 零售 0.65 / 餐饮 0.35)。
	{"food_agri", "A-农林牧渔", "农业原材料", 0, 2.0, 1.42},
	{"food_processing", "C-食品饮料与烟草", "食品加工", 1, 3.4, 2.37},
	{"food_logistics", "N-交通运输与物流仓储", "物流仓储", 1, 5.5, 3.95},
	{"food_wholesale", "M-批发零售与商贸流通", "批发零售", 2, 9.2, 6.58},
	{"food_catering", "O-住宿与餐饮", "餐饮服务", 3, 3.8, 2.68},
	// 能源链(transport 篮子 13260 元 → 终端;household ×0.6 → 制造)。
	{"energy_mining", "B-采矿与冶金", "采矿原材料", 0, 1.2, 0.56},
	{"energy_production", "K-能源与电力", "能源生产", 1, 2.0, 0.94},
	{"energy_chemical", "G-化工与新材料", "化工新材料", 1, 3.0, 1.56},
	{"energy_manufacturing", "H-金属制品与通用机械", "制造业", 2, 5.0, 2.60},
	{"energy_terminal", "Y-居民生活服务", "终端消费", 3, 6.0, 3.32},
	// 消费电子链(household×0.4 + misc×0.5 → 品牌零售;household×0.1 → 售后)。
	{"elec_semiconductor", "I-电子半导体与仪器仪表", "电子半导体", 0, 0.5, 0.29},
	{"elec_components", "I-电子半导体与仪器仪表", "元器件制造", 1, 0.8, 0.48},
	{"elec_assembly", "H-金属制品与通用机械", "整机组装", 2, 1.3, 0.80},
	{"elec_retail", "M-批发零售与商贸流通", "品牌零售", 2, 1.8, 1.25},
	{"elec_service", "Y-居民生活服务", "售后服务", 3, 0.5, 0.15},
}

// supplyChainRelations 12 条上下游关系(弹性 0.6-0.9;顺序固定)。
var supplyChainRelations = []IndustryRelation{
	// 食品链。
	{"food_agri", "food_processing", 0.85},
	{"food_processing", "food_logistics", 0.75},
	{"food_logistics", "food_wholesale", 0.70},
	{"food_wholesale", "food_catering", 0.80},
	// 能源链。
	{"energy_mining", "energy_production", 0.90},
	{"energy_production", "energy_chemical", 0.80},
	{"energy_chemical", "energy_manufacturing", 0.75},
	{"energy_manufacturing", "energy_terminal", 0.65},
	// 消费电子链。
	{"elec_semiconductor", "elec_components", 0.85},
	{"elec_components", "elec_assembly", 0.80},
	{"elec_assembly", "elec_retail", 0.70},
	{"elec_retail", "elec_service", 0.60},
}

// supplyUnitCostByTier 各层单位成本系数(下游增值率高;库存估值用)。
var supplyUnitCostByTier = [SupplyChainTierCount]float64{0.70, 0.75, 0.80, 0.85}

// NewSupplyChain 初始化 15 节点 + 12 关系。节点以稳态播种:Inventory =
// 2×baseDemand(= 安全线)、AvgShip = baseDemand、Utilization = base/Capacity
// —— 首月即均衡(价格指数 1.0 无瞬态),需求变化才产生有界瞬态。
func NewSupplyChain() *SupplyChain {
	sc := &SupplyChain{
		Nodes:     make(map[string]*SupplyChainNode, len(supplyChainNodeSpecs)),
		Relations: append([]IndustryRelation{}, supplyChainRelations...),
	}
	for _, s := range supplyChainNodeSpecs {
		sc.Nodes[s.id] = &SupplyChainNode{
			ID: s.id, Industry: s.industry, NameCN: s.nameCN, Tier: s.tier,
			Capacity: s.capacity, baseDemand: s.baseDemand,
			UnitCost:        supplyUnitCostByTier[s.tier],
			Utilization:     s.baseDemand / s.capacity,
			SafetyStockMons: SupplySafetyStockMons,
			Inventory:       SupplySafetyStockMons * s.baseDemand,
			AvgShip:         s.baseDemand,
			FillRatio:       1,
			SupplyRatio:     1,
		}
		sc.nodeOrder = append(sc.nodeOrder, s.id)
	}
	for t := range sc.PriceIndex {
		sc.PriceIndex[t] = 1.0
	}
	return sc
}

// MonthlyTick 产业链月度调度(SettleMonth ②B;确定性,零 rand)。
func (sc *SupplyChain) MonthlyTick(w *World) {
	if sc == nil || w == nil {
		return
	}
	sc.computeDemand(w)    // ① 需求自下而上(含下游订单传导)。
	sc.stepProduction()    // ② 生产/利用率/库存(R5-1 断供防护 + 强制补产)。
	sc.stepPrices()        // ③ 价格逐层传导(PriceIndex)。
	sc.applyGoodsEffect(w) // ④ 消费品市场价格联动(紧缺+5% / 过剩−3%)。
	sc.LastTickMonth = w.Month
}

// chainGoodsMap 链 id → 受其价格影响的消费篮子类别(goodsOrder 子集)。
var chainGoodsMap = map[string]string{
	"food":   "food",      // 食品链 → 食品烟酒
	"energy": "transport", // 能源链 → 交通通信(油价/能源)
	"elec":   "household", // 消费电子链 → 生活用品及服务
}

// chainOfNode 节点 id 前缀(链 id):"food_agri" → "food"。
func chainOfNode(nodeID string) string {
	for i := 0; i < len(nodeID); i++ {
		if nodeID[i] == '_' {
			return nodeID[:i]
		}
	}
	return nodeID
}

// computeDemand ①需求自下而上:零售/终端需求取自 Goods 消费篮子上月口径
// (②B 在步骤③ 之前执行,当月消费尚未入账 —— 与 LaborMonthStep 扫描
// w.Month−1 同理,ConsumptionByGoods 保存的是上一个结算月的拆分);
// 上游需求 = 下游订单 × SupplyInputCoef,自 T3 → T0 逐层累加。
func (sc *SupplyChain) computeDemand(w *World) {
	// 消费篮子上月合计(元);全房无消费数据(首月/全员阵亡)→ 契约基准播种。
	consumed := make(map[string]float64, len(goodsOrder))
	total := 0.0
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		for id, v := range p.ConsumptionByGoods {
			consumed[id] += v
			total += v
		}
	}
	wan := func(id string, share float64) float64 {
		v := consumed[id]
		if total <= 0 {
			v = goodsBaselineDemandCNY * goodsWeights[id] // 基准需求(元/月)
		}
		return v * share * FirmScale / 10000.0
	}

	// 终端/零售节点基础需求(万元/月)。
	base := map[string]float64{
		"food_wholesale":       wan("food", 0.65),                        // 商超零售份额
		"food_catering":        wan("food", 0.35),                        // 外食份额
		"energy_terminal":      wan("transport", 1.0),                    // 能源终端消费
		"energy_manufacturing": wan("household", 0.6),                    // 制造业用能/用品
		"elec_retail":          wan("household", 0.4) + wan("misc", 0.5), // 电子零售
		"elec_service":         wan("household", 0.1),                    // 售后服务
	}
	for _, id := range sc.nodeOrder {
		sc.Nodes[id].LastDemand = base[id] // map 未命中 → 0(纯上游节点)
	}
	// 订单自下而上传导:每条关系恰好累加一次(下游 LastDemand → 上游订单)。
	// Relations 表按「每链上游在前」列出(链内拓扑序);**逆序遍历**保证处理
	// 某关系时其下游节点的 LastDemand 已含全部更下游订单 —— 同层上下游
	// (食品加工 T1 → 物流仓储 T1)不受 tier 分组迭代顺序影响。
	for i := len(sc.Relations) - 1; i >= 0; i-- {
		rel := sc.Relations[i]
		d, u := sc.Nodes[rel.Downstream], sc.Nodes[rel.Upstream]
		if d != nil && u != nil {
			u.LastDemand += d.LastDemand * SupplyInputCoef
		}
	}
}

// stepProduction ②生产/库存:按 tier 0→3(上游先结算,下游才能读到其
// FillRatio 施加断供约束)。R5-1:上游 fillRatio<1 时下游有效需求 =
// demand × (0.5 + 0.5×fillRatio) —— 最多降 50%,不停产;库存低于安全线
// 时 targetProduction 追加减产缺口(强制补产);库存超 2×安全线(中性带
// 上限)时压产去库存(超限部分每月消化 30%,≤40% 骨架需求)—— 防需求
// 回落后库存滞留导致价格单边下跌。
func (sc *SupplyChain) stepProduction() {
	for tier := 0; tier < SupplyChainTierCount; tier++ {
		for _, id := range sc.nodeOrder {
			n := sc.Nodes[id]
			if n == nil || n.Tier != tier {
				continue
			}
			// 上游供给约束:取该节点全部上游关系的最小履约率(无上游 → 1)。
			supply := 1.0
			for _, rel := range sc.Relations {
				if rel.Downstream != id {
					continue
				}
				if u := sc.Nodes[rel.Upstream]; u != nil && u.FillRatio < supply {
					supply = u.FillRatio
				}
			}
			n.SupplyRatio = supply
			demand := n.LastDemand * (0.5 + 0.5*supply) // R5-1 断供折算。

			target := demand
			if replenish := n.safetyLine() - n.Inventory; replenish > 0 {
				target += replenish // R5-1 强制补产缺口。
			} else if glut := n.Inventory - 2*n.safetyLine(); glut > 0 {
				destock := 0.3 * glut
				if destockCap := 0.4 * demand; destock > destockCap {
					destock = destockCap
				}
				target -= destock
			}
			targetUtil := clampF(target/n.Capacity, SupplyUtilFloor, 1.0)
			n.Utilization += SupplyUtilSmoothAlpha * (targetUtil - n.Utilization)
			actual := n.Capacity * n.Utilization

			avail := n.Inventory + actual
			shipped := clampF(demand, 0, avail)
			n.Inventory = clampF(avail-shipped, 0, n.Capacity*SupplyInventoryCapMult)
			n.AvgShip = 0.8*n.AvgShip + 0.2*shipped
			n.LastShipped = shipped
			if demand > 1e-9 {
				n.FillRatio = clampF(shipped/demand, 0, 1)
			} else {
				n.FillRatio = 1
			}
		}
	}
}

// tierInvRatio 层内库存/安全线聚合比(价格自失衡的基准;稳态 ≈ 1 中性)。
func (sc *SupplyChain) tierInvRatio(tier int) float64 {
	var inv, safety float64
	for _, id := range sc.nodeOrder {
		n := sc.Nodes[id]
		if n == nil || n.Tier != tier {
			continue
		}
		inv += n.Inventory
		safety += n.safetyLine()
	}
	if safety < 1e-9 {
		return 1
	}
	return inv / safety
}

// invRatioPriceEffect 库存比 → 单月价格效应:紧缺(<1)涨价 ≤+5%,
// 过剩(>2)降价 ≤−3%,中性带 [1,2] 为 0(稳态价格平稳的保证)。
func invRatioPriceEffect(ratio float64) float64 {
	switch {
	case ratio < 1:
		return SupplyShortagePriceHike * clampF((1-ratio)/0.5, 0, 1)
	case ratio > 2:
		return -SupplySurplusPriceCut * clampF((ratio-2)/2.0, 0, 1)
	default:
		return 0
	}
}

// tierAvgElasticityInto 流入该层的关系的平均传导弹性(价格传导权重)。
func (sc *SupplyChain) tierAvgElasticityInto(tier int) float64 {
	var sum float64
	n := 0
	for _, rel := range sc.Relations {
		d := sc.Nodes[rel.Downstream]
		if d == nil || d.Tier != tier {
			continue
		}
		sum += rel.Elasticity
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// stepPrices ③价格传导:层 mom = 自失衡 + 上游本月 mom × 平均弹性(自 T0 → T3)。
func (sc *SupplyChain) stepPrices() {
	var cur [SupplyChainTierCount]float64
	for tier := 0; tier < SupplyChainTierCount; tier++ {
		mom := invRatioPriceEffect(sc.tierInvRatio(tier))
		if tier > 0 {
			mom += cur[tier-1] * sc.tierAvgElasticityInto(tier)
		}
		mom = clampF(mom, SupplyMomMin, SupplyMomMax)
		sc.PriceIndex[tier] *= 1 + mom
		cur[tier] = mom
	}
	sc.lastMom = cur
}

// applyGoodsEffect ④链内库存失衡影响 Goods 市场价格:紧缺→涨价 ≤5%,
// 过剩→降价 ≤3%(直接乘 PriceIdx;本月稍后的 GoodsMonthStep(④.5)会以
// 调整后的值为基计算 MomChange —— 链条冲击体现在当月环比,不二次计价)。
func (sc *SupplyChain) applyGoodsEffect(w *World) {
	if w == nil || w.Goods == nil {
		return
	}
	for chain, goodsID := range chainGoodsMap {
		it := w.Goods.Items[goodsID]
		if it == nil {
			continue
		}
		var inv, safety float64
		for _, id := range sc.nodeOrder {
			if chainOfNode(id) != chain {
				continue
			}
			n := sc.Nodes[id]
			inv += n.Inventory
			safety += n.safetyLine()
		}
		if safety < 1e-9 {
			continue
		}
		it.PriceIdx *= 1 + invRatioPriceEffect(inv/safety)
	}
}

// Snapshot 视图快照(view.go BuildClientState 调用):利用率 top5 节点 +
// 4 层价格指数 + 瓶颈警告(≤5 条,按严重度降序)。
func (sc *SupplyChain) Snapshot() SupplyChainJSON {
	out := SupplyChainJSON{
		TopNodes:    make([]SupplyNodeJSON, 0, 5),
		Bottlenecks: make([]BottleneckJSON, 0),
	}
	if sc == nil {
		return out
	}
	out.PriceIdx = sc.PriceIndex
	// top5:按利用率降序,nodeOrder 序为稳定 tie-break(确定性)。
	ranked := make([]*SupplyChainNode, 0, len(sc.nodeOrder))
	for _, id := range sc.nodeOrder {
		if n := sc.Nodes[id]; n != nil {
			ranked = append(ranked, n)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Utilization > ranked[j].Utilization })
	for i, n := range ranked {
		if i >= 5 {
			break
		}
		out.TopNodes = append(out.TopNodes, SupplyNodeJSON{
			ID: n.ID, Industry: n.Industry, NameCN: n.NameCN, Tier: n.Tier,
			Utilization: n.Utilization, InventoryMons: n.inventoryMonths(),
		})
	}
	// 瓶颈警告:供货缺口(sev 2)> 低于安全库存(sev 1)> 满负荷(sev 1)。
	type warn struct {
		node     *SupplyChainNode
		reason   string
		severity int
	}
	var warns []warn
	for _, id := range sc.nodeOrder {
		n := sc.Nodes[id]
		if n == nil {
			continue
		}
		switch {
		case n.FillRatio < 0.95 && n.LastDemand > 1e-9:
			warns = append(warns, warn{n, "supply_gap", 2})
		case n.Inventory < n.safetyLine():
			warns = append(warns, warn{n, "below_safety_stock", 1})
		case n.Utilization >= 0.95:
			warns = append(warns, warn{n, "full_capacity", 1})
		}
	}
	sort.SliceStable(warns, func(i, j int) bool { return warns[i].severity > warns[j].severity })
	for i, wn := range warns {
		if i >= 5 {
			break
		}
		out.Bottlenecks = append(out.Bottlenecks, BottleneckJSON{
			NodeID: wn.node.ID, Industry: wn.node.Industry, NameCN: wn.node.NameCN,
			Reason: wn.reason, Severity: wn.severity,
		})
	}
	return out
}
