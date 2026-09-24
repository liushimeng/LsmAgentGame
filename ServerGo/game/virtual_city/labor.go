// Package virtual_city — labor.go: 劳动力市场 + 社会结构统计(2026-09-16 §财商流P1-2)。
//
// 契约: lag_docs/虚拟城市/已实现/05-P1扩展/虚拟城市-P1-真实经济循环引擎-v1.md §4/§5。
// 纯引擎层:无锁、无 goroutine、无 IO;月度更新由 SettleMonth 持房间锁调用
// (LaborMonthStep 本步确定性,个体裁员掷骰仍在 events.go 经 w.Rand)。
package virtual_city

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
// 2026-09-19 §P2 v2 增量:增加 TotalWealth/MedianWealth/MeanWealth/Percentiles/
// LorenzPoints/PyramidLayers 字段;ComputeSociety 同步填充。
type SocietyStats struct {
	Gini      float64    // 存活玩家净资产基尼系数 [0,1]
	Quintiles [5]float64 // 可支配收入五等份各组收入占比(低→高,和=1)
	Circles   [3]int     // 圈层人数:{生存圈, 积累圈, 自由圈}

	// v2 新增(P2 财富可视化 §13.2.1)
	TotalWealth   int64                    // Σ存活玩家净资产(分母)
	MedianWealth  int64                    // 中位数(n 奇→中点;偶→中两点平均)
	MeanWealth    int64                    // 总/人数
	P10           int64                    // 分位线 P10/P25/P50/P75/P90
	P25           int64
	P50           int64
	P75           int64
	P90           int64
	LorenzPoints  [][2]float64             // 洛伦兹曲线 {(人口累计比例, 财富累计比例)};n+1 点
	PyramidLayers []WealthLayer            // 金字塔分层(自下而上):生存/积累/自由
}

// WealthLayer 金字塔单层(2026-09-19 §P2 v2 新定)。
type WealthLayer struct {
	Name        string  // "survival" / "accumulation" / "freedom"
	Count       int     // 人数
	TotalWealth int64   // 总财富
	AvgWealth   int64   // 平均财富
	WealthPct   float64 // 占总财富比例 0-1
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
//	v2 增量  :TotalWealth/Median/Mean/Percentiles/LorenzPoints/PyramidLayers
//	         (2026-09-19 §P2 v2 §13.2.1)
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

	// v2 增量:总/中位/均值 + 分位线 + 洛伦兹 + 金字塔(2026-09-19 §P2 v2)。
	if n > 0 {
		var total float64
		for _, v := range netWorths {
			total += v
		}
		s.TotalWealth = int64(total + 0.5)
		s.MeanWealth = int64(total/float64(n) + 0.5)
		s.P50 = percentileInt64(netWorths, 0.50)
		s.MedianWealth = s.P50
		s.P10 = percentileInt64(netWorths, 0.10)
		s.P25 = percentileInt64(netWorths, 0.25)
		s.P75 = percentileInt64(netWorths, 0.75)
		s.P90 = percentileInt64(netWorths, 0.90)

		// 洛伦兹曲线:n+1 点;(0,0) → 累加 → (1,1);总财富 0 时退化为对角线。
		s.LorenzPoints = make([][2]float64, n+1)
		s.LorenzPoints[0] = [2]float64{0, 0}
		if total > 0 {
			var cum float64
			for i, v := range netWorths {
				cum += v
				s.LorenzPoints[i+1] = [2]float64{
					float64(i+1) / float64(n),
					cum / total,
				}
			}
		} else {
			for i := 0; i < n; i++ {
				s.LorenzPoints[i+1] = [2]float64{
					float64(i+1) / float64(n),
					float64(i+1) / float64(n),
				}
			}
		}

		// 财富金字塔:三层,自下而上(生存 → 积累 → 自由);按 Circles 顺序映射。
		// 注意:Circles 是 [生存, 积累, 自由],但 WealthLayer 输出顺序按"自下而上"
		// 习惯(底层=生存,顶层=自由),与 Circles 索引一致。
		layerNames := [3]string{"survival", "accumulation", "freedom"}
		s.PyramidLayers = make([]WealthLayer, 3)
		for li := 0; li < 3; li++ {
			s.PyramidLayers[li] = WealthLayer{Name: layerNames[li], Count: s.Circles[li]}
		}
		// 金字塔每层的 TotalWealth/AvgWealth/WealthPct:按 Circle 切 netWorths
		// (按 r=被动/支出 升序切片;但 Circles 已按座位遍历顺序累计,无法直接
		// 映射回 netWorths,这里走简化近似:按资产升序分位切)。
		if total > 0 {
			// 简化映射:每层占 1/3 人数(向上递进),与 Circles 实际分布存在偏差,
			// 但对 12 人小样本足够直观;若需要精确切分,留 v3 接 r 阈值分组函数。
			perLayer := n / 3
			rem := n - perLayer*3
			idx := 0
			for li := 0; li < 3; li++ {
				size := perLayer
				if li == 2 {
					size += rem // 末层吃余数
				}
				if size <= 0 {
					continue
				}
				var sum float64
				for j := 0; j < size && idx < n; j++ {
					sum += netWorths[idx]
					idx++
				}
				s.PyramidLayers[li].TotalWealth = int64(sum + 0.5)
				if size > 0 {
					s.PyramidLayers[li].AvgWealth = int64(sum/float64(size) + 0.5)
				}
				s.PyramidLayers[li].WealthPct = sum / total
			}
		}
	}
	return s
}

// percentileInt64 计算升序 netWorths 的 p 分位(线性插值,n<2 → 0)。
// p ∈ [0,1];索引 floor((n-1)*p)。
func percentileInt64(sortedVals []float64, p float64) int64 {
	n := len(sortedVals)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return int64(sortedVals[0] + 0.5)
	}
	if p <= 0 {
		return int64(sortedVals[0] + 0.5)
	}
	if p >= 1 {
		return int64(sortedVals[n-1] + 0.5)
	}
	pos := p * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return int64(sortedVals[n-1] + 0.5)
	}
	frac := pos - float64(lo)
	v := sortedVals[lo]*(1-frac) + sortedVals[hi]*frac
	return int64(v + 0.5)
}

// max64 int64 最大值辅助(go 版本未定,避免依赖内建 max 的泛型推导差异)。
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
