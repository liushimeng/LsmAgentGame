// Package wealth — labor.go: 劳动力市场 + 社会结构统计(2026-09-16 §财商流P1-2)。
//
// 契约: docs/财商流游戏/已实现/05-P1扩展/财商流游戏-P1-真实经济循环引擎-v1.md §4/§5。
// 纯引擎层:无锁、无 goroutine、无 IO;月度更新由 SettleMonth 持房间锁调用
// (LaborMonthStep 本步确定性,个体裁员掷骰仍在 events.go 经 w.Rand)。
package wealth

import (
	"sort"
)

// 劳动力市场常量(§4.1,P1 新定,标定理由见契约文档 §12.1)。
const (
	FirmScale       = 2.5  // 居民消费 → GDP 的倍数(投资+政府+外需)
	LaborShare      = 0.52 // 劳动报酬份额(国民收入口径)
	UnemployFloor   = 0.02 // 失业率下限
	UnemployCeil    = 0.35 // 失业率上限
	UnemployNatural = 0.05 // 自然失业率(个体概率基准)
	// laborSmoothAlpha 失业率月度平滑系数(α=0.25,防 12 人小样本发散)。
	laborSmoothAlpha = 0.25
)

// FirmSector 企业部门月度状态(挂 World.Labor;引擎纯状态,无锁)。
type FirmSector struct {
	RevenueCNY    int64   // 上月企业营收 = Σ当月 to-firms 流水 × FirmScale(2.5)
	WageBillCNY   int64   // 上月全城工资总额(在职玩家 SalaryBase 之和)
	AvgWageCNY    int64   // 滚动平均工资 = WageBill ÷ 在职人数
	Unemployment  float64 // 内生失业率 0-1(clamp [0.02,0.35])
	Employment    float64 // 就业率 = 1 − Unemployment
	WageGrowthYoY float64 // Phillips 年化工资增长(小数,AnnualAdjust 时更新)
	LayoffWave    int     // 裁员潮强度 0-3(0 平静/1 观察/2 裁员潮)
}

// NewFirmSector 构造初始状态(Unemployment=0.05 自然率)。
func NewFirmSector() *FirmSector {
	return &FirmSector{
		Unemployment:  UnemployNatural,
		Employment:    1 - UnemployNatural,
		WageGrowthYoY: 0.03,
	}
}

// SocietyStats 社会结构统计(§5,月度计算后缓存于 World.Society)。
type SocietyStats struct {
	Gini      float64    // 存活玩家净资产基尼系数 [0,1]
	Quintiles [5]float64 // 可支配收入五等份各组收入占比(低→高,和=1)
	Circles   [3]int     // 圈层人数:{生存圈, 积累圈, 自由圈}
}

// sumFirmsInflow 统计指定月份 to=EntityFirms 的流水合计(§4.2,O(条目数))。
func (l *Ledger) sumFirmsInflow(month int) int64 {
	var sum int64
	for _, e := range l.Entries {
		if month > 0 && e.Month != month {
			continue
		}
		if e.To == EntityFirms {
			sum += e.AmountCNY
		}
	}
	return sum
}

// LaborMonthStep 劳动力市场月度更新(§4.2,P1 新定)。
// 调用时机:SettleMonth ② MonthlyEvents 之前(个体失业概率消费最新 Unemployment)。
// 随机性:无(本步确定性)。economy_enabled=false 时跳过。
//
// ⚠️ 口径说明:本步在 ②.0 执行时,当月(w.Month)的 to=firms 生活支出尚未入账
// (步骤③ 才记账),故企业内需扫描**上一个完整结算月(w.Month−1)** —— 否则
// 占大头的 living 永远进不了营收,§12.1 标定(常态失业率 4-6%)不成立。
func (w *World) LaborMonthStep() {
	if !w.EconomyEnabled {
		return
	}
	if w.Labor == nil {
		w.Labor = NewFirmSector()
	}
	l := w.Labor

	month := w.Month - 1 // 刚结算完成的月份;SettleMonth(1) 时为 0(空账)
	conFirms := w.Ledger.sumFirmsInflow(month)
	l.RevenueCNY = int64(float64(conFirms)*FirmScale + 0.5)

	var wageBill int64
	employed := 0
	for _, p := range w.Players {
		if p == nil || !p.Alive || p.UnemployedMonths != 0 {
			continue // 失业者不计入工资总额
		}
		wageBill += p.SalaryBase
		employed++
	}
	l.WageBillCNY = wageBill
	if employed > 0 {
		l.AvgWageCNY = wageBill / int64(employed)
	} else {
		l.AvgWageCNY = 0
	}

	capacityWage := float64(l.RevenueCNY) * LaborShare
	denom := float64(wageBill)
	if denom < 1 {
		denom = 1
	}
	targetEmployment := clampF(capacityWage/denom, 0.60, 1.0)
	l.Unemployment = clampF(
		l.Unemployment+laborSmoothAlpha*((1-targetEmployment)-l.Unemployment),
		UnemployFloor, UnemployCeil)
	l.Employment = 1 - l.Unemployment

	switch {
	case l.Unemployment > 0.12:
		l.LayoffWave = 2
	case l.Unemployment > 0.08:
		l.LayoffWave = 1
	default:
		l.LayoffWave = 0
	}
}

// phillipsBase 年化基础工资增长 = 3% + 0.5×(5% − U)%(§4.4,P1 新定)。
// U=5% → 3%(与 P0 基线一致);U=10% → 0.5%;U=2% → 4.5%。
func (w *World) phillipsBase() float64 {
	u := UnemployNatural
	if w.Labor != nil {
		u = w.Labor.Unemployment
	}
	return 0.03 + 0.5*(0.05-u)
}

// sampleRehireRatio 再就业工资比率抽样(§4.3,裁员时点调用;随机经 w.Rand):
//
//	U > 0.10 → 0.7 + 0.2×rand   // 0.7–0.9(就业市场冷,降薪再就业)
//	U < 0.04 → 1.0 + 0.2×rand   // 1.0–1.2(用工紧俏,跳槽涨薪)
//	其他     → 0.8 + 0.4×rand   // 0.8–1.2(P0 原区间)
func (w *World) sampleRehireRatio() float64 {
	if !w.EconomyEnabled || w.Labor == nil {
		return 0.8 + w.Rand.Float64()*0.4
	}
	switch u := w.Labor.Unemployment; {
	case u > 0.10:
		return 0.7 + w.Rand.Float64()*0.2
	case u < 0.04:
		return 1.0 + w.Rand.Float64()*0.2
	default:
		return 0.8 + w.Rand.Float64()*0.4
	}
}

// ComputeSociety 月度社会结构统计(§5,纯函数;只统计 alive 玩家)。
//
//	基尼    :净资产升序 G = (2Σi·x_i)/(n·Σx_i) − (n+1)/n;Σx=0 或 n<2 → 0
//	五等份  :可支配收入(Income−Tax−Social,clamp ≥0)升序均分 5 组占比;
//	         n<5 或 Σ=0 → 全 0.2
//	圈层    :r = 月被动收入 ÷ max(1, Monthly.Expense);
//	         r<1 生存圈 / 1≤r<2 积累圈 / r≥2 自由圈(《总体设计》§6)
func ComputeSociety(w *World) *SocietyStats {
	s := &SocietyStats{}
	if w == nil {
		return s
	}
	age := w.Age()
	var netWorths []float64
	var incomes []float64
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		netWorths = append(netWorths, float64(p.NetWorth(w.Market)))
		disp := p.Monthly.Income - p.Monthly.Tax - p.Monthly.Social
		if disp < 0 {
			disp = 0
		}
		incomes = append(incomes, float64(disp))
		r := float64(p.MonthlyPassiveIncome(w.Market, age)) / float64(max64(p.Monthly.Expense, 1))
		switch {
		case r >= 2:
			s.Circles[2]++ // 自由圈
		case r >= 1:
			s.Circles[1]++ // 积累圈
		default:
			s.Circles[0]++ // 生存圈
		}
	}

	// 基尼系数。
	sort.Float64s(netWorths)
	n := len(netWorths)
	if n >= 2 {
		var sum, weighted float64
		for i, v := range netWorths {
			sum += v
			weighted += float64(i+1) * v
		}
		if sum > 0 {
			s.Gini = 2*weighted/(float64(n)*sum) - float64(n+1)/float64(n)
		}
	}

	// 五等份(升序均分;末组吃余数)。
	for i := range s.Quintiles {
		s.Quintiles[i] = 0.2
	}
	if len(incomes) >= 5 {
		sort.Float64s(incomes)
		var total float64
		for _, v := range incomes {
			total += v
		}
		if total > 0 {
			np := len(incomes)
			for q := 0; q < 5; q++ {
				lo := q * np / 5
				hi := (q + 1) * np / 5
				if q == 4 {
					hi = np
				}
				var group float64
				for _, v := range incomes[lo:hi] {
					group += v
				}
				s.Quintiles[q] = group / total
			}
		}
	}
	return s
}

// max64 int64 最大值辅助(go 版本未定,避免依赖内建 max 的泛型推导差异)。
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
