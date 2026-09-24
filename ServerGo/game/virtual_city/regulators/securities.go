// Package regulators — securities.go: 证监会(信息披露 + 内幕交易监管)
// (2026-09-21 §城市扩张v2.12 阶段8)。
//
// 职责:
//   - 内幕交易检测(R8-3):单座位**大额开仓**(≥50 万)后**次月内**出现同座位
//     大额反向操作 → 认定"短线 insider",罚款 = 反向金额 × 5%,封顶当月
//     月收入 × 5%(无收入 → 罚 0,见头注释取舍);
//   - 信息披露月数(DisclosureMonths):月度例行披露计数(快照/审计面)。
//
// 解耦约束(阶段8 子包设计):本包**不得 import wealth 主包**(防循环依赖);
// 所有输入经 TradeRecord / 回调函数注入,由 wealth 主包
// (public_services.go::RegulatorBundle.MonthlyStep)从 Ledger 适配。
//
// 纯引擎层:无锁、无 goroutine、无 IO、零 rand —— 输入相同输出恒定。
package regulators

import "sort"

// 证监会常量(阶段8 新定)。
const (
	// InsiderLargeTradeCNY 大额交易认定线(元;"7 日"在月粒度引擎中的
	// 代理口径 = 当月或次月内反向,见 MonthlyStep 头注释)。
	InsiderLargeTradeCNY int64 = 500_000
	// InsiderFineRate 罚款率 = 反向操作金额 × 5%。
	InsiderFineRate = 0.05
	// InsiderFineCapRate R8-3:罚款封顶 = 当月月收入 × 5%。
	InsiderFineCapRate = 0.05
	// InsiderReverseWindowMonths 反向窗口(月):大额锚交易之后多少个月内
	// 的反向操作仍计入检测(月粒度对"7 日"的近似,取 1 = 当月 + 次月)。
	InsiderReverseWindowMonths = 1
)

// TradeSide 交易方向。
type TradeSide int

const (
	TradeBuy  TradeSide = iota // 买入 / 做空开仓(保证金,CatBuy 口径)
	TradeSell                  // 卖出 / 做空平仓(CatSell 口径)
)

// TradeRecord 单座位单笔证券类交易(wealth 侧从 Ledger CatBuy/CatSell 适配;
// 融券开仓保证金(CatBuy)/ 平仓结算(CatSell)天然落入同一检测口径)。
type TradeRecord struct {
	Seat       int
	Month      int
	Side       TradeSide
	AmountCNY int64
}

// IsLarge 是否达到大额认定线。
func (t TradeRecord) IsLarge() bool { return t.AmountCNY >= InsiderLargeTradeCNY }

// InsiderFine 单笔内幕交易处罚(月度检测产出;wealth 侧执行扣款)。
type InsiderFine struct {
	Seat     int
	Month    int
	BaseCNY  int64 // 计罚基数(反向操作金额)
	FineCNY  int64 // 应罚金额(5% + R8-3 收入封顶后;wealth 侧再按现金可收性钳制)
	AnchorCNY int64 // 触发锚(大额开仓金额;展示用)
}

// SecuritiesRegulator 证监会状态(挂 wealth.RegulatorBundle.Securities)。
type SecuritiesRegulator struct {
	InsiderTradeFines int64 // 累计罚款(应罚口径,未扣现金可收性钳制)
	InsiderCases      int  // 累计认定笔数(含应罚为 0 的低收入座位)
	DisclosureMonths  int  // 信息披露月数(月度例行计数)

	// prevLarge 上月大额锚交易(seat → 记录;按月滚动)。
	prevLarge map[int][]TradeRecord
	// lastStepMonth 最近一次 MonthlyStep 的月份(§130 接线验证)。
	LastStepMonth int
}

// NewSecuritiesRegulator 构造。
func NewSecuritiesRegulator() *SecuritiesRegulator {
	return &SecuritiesRegulator{prevLarge: map[int][]TradeRecord{}}
}

// MonthlyStep 月度检测:输入当月全量证券类交易(任意金额;本方法内部过滤
// 大额),对照上月大额锚 + 当月内先开后平的配对,产出罚款清单。
//
// 检测规则(两腿都须 ≥ 50 万):
//   - 跨月:上月大额买入(锚) + 当月大额卖出(反向)→ 罚;对称同理;
//   - 当月:同月内先大额买入后大额卖出(按传入切片顺序)→ 罚;对称同理;
//   - 一笔锚只计一次(命中即 flagged,防同锚重复计罚)。
//
// incomeOf(seat) 返回该座位当月月收入(元);nil 或返回 ≤0 → R8-3 封顶为 0
// (无收入者免罚 —— 已知取舍:失业座位暂不受罚,阶段8 不引入"罚到负"路径)。
// 返回罚款清单(仅应罚 > 0 的条目;顺序按 seat 升序,确定性)。
func (r *SecuritiesRegulator) MonthlyStep(month int, current []TradeRecord, incomeOf func(seat int) int64) []InsiderFine {
	if r == nil {
		return nil
	}
	r.DisclosureMonths++
	r.LastStepMonth = month

	// 当月交易按座位分组(保持传入顺序 —— Ledger 顺序即时间序)。
	bySeat := map[int][]TradeRecord{}
	for _, t := range current {
		bySeat[t.Seat] = append(bySeat[t.Seat], t)
	}

	// prevLarge 锚(跨月检测)逐座位配对;配对完成锚即弃(单锚单罚)。
	var fines []InsiderFine
	emit := func(seat int, base, anchor int64) {
		fine := int64(float64(base)*InsiderFineRate + 0.5)
		if incomeOf != nil {
			if cap := int64(float64(incomeOf(seat))*InsiderFineCapRate + 0.5); fine > cap {
				fine = cap
			}
		}
		r.InsiderCases++
		if fine <= 0 {
			return // 认定但免罚(低收入);计入案件数,不出罚款单。
		}
		r.InsiderTradeFines += fine
		fines = append(fines, InsiderFine{Seat: seat, Month: month, BaseCNY: base, FineCNY: fine, AnchorCNY: anchor})
	}

	for _, seat := range sortedSeats(bySeat) {
		cur := bySeat[seat]
		// ① 跨月:上月大额锚 × 当月反向大额。
		for _, anchor := range r.prevLarge[seat] {
			rev := firstLargeReverse(cur, anchor.Side)
			if rev == nil {
				continue
			}
			emit(seat, rev.AmountCNY, anchor.AmountCNY)
		}
		// ② 当月:同月内先大额开仓后大额反向(切片顺序即先后)。
		for i, a := range cur {
			if !a.IsLarge() {
				continue
			}
			for j := i + 1; j < len(cur); j++ {
				b := cur[j]
				if b.IsLarge() && b.Side != a.Side {
					emit(seat, b.AmountCNY, a.AmountCNY)
					break
				}
			}
		}
	}

	// 滚动锚:当月大额交易留作下月检测基准。
	next := map[int][]TradeRecord{}
	for _, seat := range sortedSeats(bySeat) {
		for _, t := range bySeat[seat] {
			if t.IsLarge() {
				next[seat] = append(next[seat], t)
			}
		}
	}
	r.prevLarge = next
	return fines
}

// firstLargeReverse 找到 cur 中第一条与 anchorSide 反向的大额交易(无则 nil)。
func firstLargeReverse(cur []TradeRecord, anchorSide TradeSide) *TradeRecord {
	for i := range cur {
		if cur[i].IsLarge() && cur[i].Side != anchorSide {
			return &cur[i]
		}
	}
	return nil
}

// sortedSeats map 键排序(确定性遍历)。
func sortedSeats(m map[int][]TradeRecord) []int {
	out := make([]int, 0, len(m))
	for seat := range m {
		out = append(out, seat)
	}
	sort.Ints(out)
	return out
}
