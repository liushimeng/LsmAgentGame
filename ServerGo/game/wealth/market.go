// Package wealth — market.go: 市场周期引擎 + 资产价格月度演化(2026-09-14 §财商流P0)。
//
// 数值出处: 后端架构文档 §5(阶段参数表 / 转移矩阵 / 月度漂移公式)。
// 所有随机性经注入的 *rand.Rand 驱动(确定性测试,§16)。
package wealth

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
	// 股票(批次20 文档3 B2-1/B2-4:记录 clamp 前原始 Δ 供熔断判定,再做
	// ±10% 保号截断。带内月份乘式与旧实现 `m *= 1+drift` 逐位一致 →
	// 旧 seed 对局零偏移;σ=3% 月波动 p99 <10%,绝大多数月份走此路径)。
	oldIdx := m.StockIndex
	raw := oldIdx * (1 + p.StockAnnual/12 + stockMonthlySigma*normFloat64(rng))
	if oldIdx > 0 {
		m.LastStockRawDelta = raw/oldIdx - 1
	}
	next := clampStockMonthlyLimit(oldIdx, raw)
	if next < 0.01 {
		next = 0.01
	}
	m.StockIndex = next
	// 黄金
	m.GoldPrice *= 1 + p.GoldAnnual/12 + goldMonthlySigma*normFloat64(rng)
	if m.GoldPrice < 1 {
		m.GoldPrice = 1
	}
	// 房产: idx_d(m) = idx_d(m-1) × (1 + house_annual×beta/12 + 0.008×beta×N(0,1))
	for _, d := range DistrictDefs {
		idx := m.DistrictIdx[d.ID]
		next := idx * (1 + p.HouseAnnual*d.Beta/12 + houseMonthlySigma*d.Beta*normFloat64(rng))
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
	m.CycleMonthsLeft = phaseDurationMonths(rng)
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
