// Package virtual_city — market.go: 市场周期引擎 + 资产价格月度演化(2026-09-14 §财商流P0)。
//
// 数值出处: 后端架构文档 §5(阶段参数表 / 转移矩阵 / 月度漂移公式)。
// 所有随机性经注入的 *rand.Rand 驱动(确定性测试,§16)。
package virtual_city

import (
	"math"
	"math/rand"
)

// CyclePhase 市场周期阶段 id。
type CyclePhase string

const (
	PhaseRecovery   CyclePhase = "recovery"   // 复苏
	PhaseBoom       CyclePhase = "boom"       // 繁荣
	PhaseRecession  CyclePhase = "recession"  // 衰退
	PhaseDepression CyclePhase = "depression" // 萧条
)

// PhaseParams 单阶段的宏观参数(《规则》§7.1/§7.3/§7.4)。
type PhaseParams struct {
	LPR           float64 // 1Y LPR(小数)
	CPI           float64
	StockAnnual   float64 // 股票年化
	HouseAnnual   float64 // 房产年化
	GoldAnnual    float64 // 黄金年化
	BondRate      float64 // 新购债券票面年化(P0 反向联动取舍,后端架构 §5.1 ⚠️)
	EmploymentPct float64 // 就业基线(展示/事件参考)
}

// PhaseTable 四阶段参数表(后端架构 §5.1)。
var PhaseTable = map[CyclePhase]PhaseParams{
	PhaseRecovery:   {LPR: 0.035, CPI: 0.02, StockAnnual: 0.15, HouseAnnual: 0.05, GoldAnnual: -0.05, BondRate: 0.032, EmploymentPct: 0.95},
	PhaseBoom:       {LPR: 0.045, CPI: 0.035, StockAnnual: 0.30, HouseAnnual: 0.15, GoldAnnual: -0.10, BondRate: 0.027, EmploymentPct: 0.95},
	PhaseRecession:  {LPR: 0.058, CPI: 0.02, StockAnnual: -0.25, HouseAnnual: -0.05, GoldAnnual: 0.10, BondRate: 0.037, EmploymentPct: 0.80},
	PhaseDepression: {LPR: 0.028, CPI: 0.00, StockAnnual: -0.40, HouseAnnual: -0.15, GoldAnnual: 0.25, BondRate: 0.042, EmploymentPct: 0.65},
}

// 转移矩阵(《规则》§7.2;行 = 当前阶段,列数组按序累计)。
var transitionTable = map[CyclePhase][]struct {
	To   CyclePhase
	P    float64
}{
	PhaseRecovery:   {{PhaseBoom, 0.5}, {PhaseRecovery, 0.3}, {PhaseRecession, 0.2}},
	PhaseBoom:       {{PhaseBoom, 0.2}, {PhaseRecession, 0.6}, {PhaseDepression, 0.2}},
	PhaseRecession:  {{PhaseRecession, 0.4}, {PhaseDepression, 0.4}, {PhaseRecovery, 0.2}},
	PhaseDepression: {{PhaseRecovery, 0.7}, {PhaseDepression, 0.3}},
}

// 价格初值(后端架构 §5.3)。
const (
	InitialStockIndex = 3.50 // 元/份
	InitialGoldPrice  = 750  // 元/克
)

// 月度漂移 σ(后端架构 §5.3 / §4)。
const (
	stockMonthlySigma = 0.03
	goldMonthlySigma  = 0.015
	houseMonthlySigma = 0.008
)

// MarketState 市场状态(view 层映射到 game.state.market/cycle)。
type MarketState struct {
	CyclePhase      CyclePhase
	CycleMonthsLeft int // 阶段剩余月
	StockIndex      float64
	GoldPrice       float64
	DistrictIdx     map[string]float64 // district id → idx_d(初值恒 1.0)

	// 批次20 文档3 B2:股票微观结构状态(仅 stock_index 作用域)。
	// BreakerUntilMonth 熔断「最后一个禁止月」(0=从未;月结判定见
	// market_microstructure.go checkStockCircuitBreaker)。
	BreakerUntilMonth int
	// LastStockRawDelta 最近一次 MonthStep 涨跌停 clamp **前**的原始月涨跌
	// (熔断判定输入;每次 MonthStep 覆写,零持久化语义)。
	LastStockRawDelta float64

	// 批次20 文档2 §3:副业品类价格竞争播报状态(kind → 是否已在竞争 /
	// 最近播报月;仅事件去重,不影响任何金额与 rand)。
	SideCompetitionTracked    map[string]bool
	SideCompetitionEventMonth map[string]int

	// ── 批次62 泡沫反身性(2026-10-20):价格漂移的宏观乘子/加成 ──
	// DriftMultiplier 作用于**漂移项**(阶段年化/12),默认 1.0;
	//   过热期 = 1.5(信贷推高资产价格,§11.2 状态表)。
	// DriftExtra 作用于月度漂移的**加法项**(默认 0);
	//   泡沫后衰退期 = −0.02(阴跌 2%/月)。
	// ⚠ 只作用于杠杆资产(股票 + 房产指数);黄金由 PhaseTable 决定(避险属性)。
	DriftMultiplier float64
	DriftExtra      float64

	// ── 批次66 外盘情绪锚(2026-10-20):真实行情 → 游戏内月漂移微偏置 ──
	// exogenous.go::ComputeExoSignal 计算,room.trySettle 每月结前写入;
	// Active=false ⇒ 两 bias 恒 0.0(零回归:x+0.0 IEEE 恒等)。
	// 与 DriftMultiplier/DriftExtra 语义正交:bubble 内生(覆写链),外盘外生(独立字段)。
	Exo ExoSignal

	// ── 批次73 O5:A/B 参数覆盖(MCOverride;nil = 引擎默认,零回归)──
	// BoomStockMonthlyDrift 非 nil 且当前相位 = boom 时,替换 MonthStep
	// 股票漂移项「PhaseTable[boom].StockAnnual/12」(覆盖值优先于 const)。
	BoomStockMonthlyDrift *float64
	// RecessionMinMonths 非 nil 时,RerollPhase 进入衰退相位的时长取
	// max(随机 U(24,48), 覆盖值)(「衰退期最短持续月数」下限 clamp)。
	RecessionMinMonths *int
}

// NewMarket 构造初始市场(开局默认 recovery,阶段时长 U(24,48) 月)。
func NewMarket(rng *rand.Rand) *MarketState {
	m := &MarketState{
		CyclePhase:  PhaseRecovery,
		StockIndex:  InitialStockIndex,
		GoldPrice:   InitialGoldPrice,
		DistrictIdx: make(map[string]float64, DistrictCount),
		// 批次20 文档2:副业竞争播报状态(直接构造的旧 World 由
		// stepSideMarketEvents 惰性初始化兜底)。
		SideCompetitionTracked:    map[string]bool{},
		SideCompetitionEventMonth: map[string]int{},
		// 批次62:泡沫漂移乘子默认 1.0(常态)。
		DriftMultiplier: 1.0,
		DriftExtra:      0,
	}
	for _, d := range DistrictDefs {
		m.DistrictIdx[d.ID] = 1.0
	}
	m.CycleMonthsLeft = phaseDurationMonths(rng)
	return m
}

// phaseDurationMonths 阶段持续 U(24,48) 月(后端架构 §5.2,P0 放宽《规则》§7.1 的 2–4 年)。
func phaseDurationMonths(rng *rand.Rand) int {
	return 24 + rng.Intn(25) // [24, 48]
}

// Params 返回当前阶段参数。
func (m *MarketState) Params() PhaseParams {
	if p, ok := PhaseTable[m.CyclePhase]; ok {
		return p
	}
	return PhaseTable[PhaseRecovery]
}

// HousePrice 当前某城区住宅单价(元/套) = base_wan×10000×beta×idx(后端架构 §4)。
func (m *MarketState) HousePrice(districtID string) int64 {
	d := districtByID(districtID)
	if d == nil {
		return 0
	}
	idx := m.DistrictIdx[districtID]
	if idx <= 0 {
		idx = 1.0
	}
	return int64(math.Round(d.BasePriceWan * 10000 * d.Beta * idx))
}

// HouseRent 当前某城区住宅月租(元)。
func (m *MarketState) HouseRent(districtID string) int64 {
	return int64(math.Round(float64(m.HousePrice(districtID)) * HouseRentFactor))
}

// ShopPrice 商铺单价与同区住宅一致(后端架构 §6:按 §4 公式,同区房价)。
func (m *MarketState) ShopPrice(districtID string) int64 {
	return m.HousePrice(districtID)
}

// ShopRent 商铺月租(元)。
func (m *MarketState) ShopRent(districtID string) int64 {
	return int64(math.Round(float64(m.ShopPrice(districtID)) * ShopRentFactor))
}

// MonthStep 月度漂移(后端架构 §5.3):股票/黄金 + 各区房价指数。
// 在月结完成后、进入下一月前调用(§14 时序步骤③)。
func (m *MarketState) MonthStep(rng *rand.Rand) {
	p := m.Params()
	// 批次62 泡沫反身性:漂移乘子(过热 ×1.5)/ 加法项(衰退阴跌 −2%/月)。
	// ⚠ 零回归契约:常态下 mult=1.0 且 extra=0 ⇒ 本函数与批次 61 之前的
	// 实现**逐位一致**(固定种子存量对局零偏移)。
	mult := m.DriftMultiplier
	if mult <= 0 {
		mult = 1.0
	}
	extra := m.DriftExtra
	// 批次73 O5:A/B 覆盖 —— 繁荣相位月收益率「覆盖值优先于 const」;
	// nil 时 stockDrift == p.StockAnnual/12 ⇒ 逐位零回归。
	stockDrift := p.StockAnnual / 12
	if m.BoomStockMonthlyDrift != nil && m.CyclePhase == PhaseBoom {
		stockDrift = *m.BoomStockMonthlyDrift
	}
	// 股票(批次20 文档3 B2-1/B2-4:记录 clamp 前原始 Δ 供熔断判定,再做
	// ±10% 保号截断。带内月份乘式与旧实现 `m *= 1+drift` 逐位一致 →
	// 旧 seed 对局零偏移;σ=3% 月波动 p99 <10%,绝大多数月份走此路径)。
	oldIdx := m.StockIndex
	raw := oldIdx * (1 + stockDrift*mult + extra + m.Exo.StockBias + stockMonthlySigma*normFloat64(rng))
	if oldIdx > 0 {
		m.LastStockRawDelta = raw/oldIdx - 1
	}
	next := clampStockMonthlyLimit(oldIdx, raw)
	if next < 0.01 {
		next = 0.01
	}
	m.StockIndex = next
	// 黄金(避险属性 ⇒ 不受泡沫漂移乘子影响,只吃 PhaseTable 的年化)。
	m.GoldPrice *= 1 + p.GoldAnnual/12 + m.Exo.GoldBias + goldMonthlySigma*normFloat64(rng)
	if m.GoldPrice < 1 {
		m.GoldPrice = 1
	}
	// 房产: idx_d(m) = idx_d(m-1) × (1 + house_annual×beta/12 + 0.008×beta×N(0,1))
	for _, d := range DistrictDefs {
		idx := m.DistrictIdx[d.ID]
		next := idx * (1 + p.HouseAnnual*d.Beta/12*mult + extra + houseMonthlySigma*d.Beta*normFloat64(rng))
		if next < 0.05 {
			next = 0.05
		}
		m.DistrictIdx[d.ID] = next
	}
}

// CycleStep 阶段到期重掷:剩余月 -1;归零时按转移矩阵重掷并重置 U(24,48) 月。
// 返回是否发生了切换(切换即产生 game.event type:"market")。
func (m *MarketState) CycleStep(rng *rand.Rand) (switched bool) {
	if m.CycleMonthsLeft > 0 {
		m.CycleMonthsLeft--
	}
	if m.CycleMonthsLeft > 0 {
		return false
	}
	return m.RerollPhase(rng)
}

// RerollPhase 按转移矩阵重掷阶段(钟声强制重掷与此同源)。
func (m *MarketState) RerollPhase(rng *rand.Rand) (switched bool) {
	old := m.CyclePhase
	rows, ok := transitionTable[m.CyclePhase]
	if !ok || len(rows) == 0 {
		rows = transitionTable[PhaseRecovery]
	}
	draw := rng.Float64()
	var next CyclePhase
	for _, r := range rows {
		if draw < r.P {
			next = r.To
			break
		}
		draw -= r.P
	}
	if next == "" {
		next = rows[len(rows)-1].To
	}
	m.CyclePhase = next
	// 批次73 O5:RecessionMinMonths 覆盖 = 进入衰退时长的下限 clamp
	// (nil ⇒ 恒走原 phaseDurationMonths,零回归;覆盖只抬不压 ⇒ 不移动
	// 既有随机消费序列)。
	d := phaseDurationMonths(rng)
	if m.RecessionMinMonths != nil && next == PhaseRecession && d < *m.RecessionMinMonths {
		d = *m.RecessionMinMonths
	}
	m.CycleMonthsLeft = d
	return next != old
}

// normFloat64 标准正态 N(0,1)(Box-Muller;确定性:只消费注入的 rng)。
func normFloat64(rng *rand.Rand) float64 {
	u1 := rng.Float64()
	u2 := rng.Float64()
	if u1 < 1e-12 {
		u1 = 1e-12
	}
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}
