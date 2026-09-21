// Package wealth — quant_fund.go: 量化基金引擎(虚拟做市商,非玩家实体)
// (2026-09-21 §城市扩张v2.12 阶段6 / R6-2)。
//
// 5 策略加权组合,权重随市场周期阶段自适应(R6-2):
//
//	繁荣/复苏:trend + momentum 权重升(趋势行情吃贝塔)
//	衰退/萧条:mean_reversion + event_driven 权重升(震荡/困境反转行情)
//
// 月度(⑥B 首位执行 —— F05 基金评级联动消费本月量化收益):
//
//	① 各策略原始收益 = 确定性函数(股指环比 / 周期相位),零 rand
//	② 组合收益 = Σ w_i × r_i,clamp [−8%, +8%]/月
//	③ QuantIndex ×= (1+r)(基 100,进 view 快照,影响市场情绪)
//	④ 权重重归一化:score_i = 相位亲和 + 2×上月单策略收益 → softmax(τ=0.35)
//
// 纯引擎层:无锁、无 IO、零 rand 消费 —— 固定种子存量对局回归零偏移。
package wealth

import "math"

// 量化策略名(QuantStrategy.Name 取值)。
const (
	QuantTrend         = "trend"          // 趋势跟踪
	QuantMeanReversion = "mean_reversion" // 均值回归
	QuantStatArb       = "stat_arb"       // 统计套利
	QuantMomentum      = "momentum"       // 动量
	QuantEventDriven   = "event_driven"   // 事件驱动
)

// 量化引擎常量(阶段6 新定)。
const (
	// QuantIndexBase 量化基金指数基点。
	QuantIndexBase = 100.0
	// QuantMonthlyClamp 单月组合收益 clamp [−8%, +8%]。
	QuantMonthlyClamp = 0.08
	// QuantSoftmaxTau 权重 softmax 温度(越小越尖锐)。
	QuantSoftmaxTau = 0.35
	// QuantPerfFeedback 业绩反馈系数(score += 2 × 上月单策略收益)。
	QuantPerfFeedback = 2.0
)

// QuantStrategy 量化策略(R6-2 权重随市场状态自适应)。
type QuantStrategy struct {
	Name       string  // trend/mean_reversion/stat_arb/momentum/event_driven
	Weight     float64 // 权重 0..1,Σ 恒 1(softmax 重归一化)
	LastReturn float64 // 上月单策略收益(小数)
}

// QuantFundEngine 量化基金引擎(虚拟做市商;挂 World.QuantEngine)。
type QuantFundEngine struct {
	Strategies      []*QuantStrategy // 5 策略(固定序)
	Index           float64          // 量化基金指数(基 100)
	LastMonthReturn float64          // 上月组合收益(clamp 后;F05 评级联动消费)
	LastRawReturn   float64          // 上月组合原始收益(clamp 前;诊断)
	MonthsRun       int              // 累计运行月数(§130 接线验证)

	lastStockIdx float64 // 上月末股指(环比基准)
}

// quantStrategySpecs 策略静态表(固定序;初始权重 = 等权)。
var quantStrategySpecs = []string{
	QuantTrend, QuantMeanReversion, QuantStatArb, QuantMomentum, QuantEventDriven,
}

// NewQuantFundEngine 构造(等权 0.2;指数基 100;环比锚当前股指)。
func NewQuantFundEngine() *QuantFundEngine {
	e := &QuantFundEngine{Index: QuantIndexBase, lastStockIdx: InitialStockIndex}
	for _, name := range quantStrategySpecs {
		e.Strategies = append(e.Strategies, &QuantStrategy{Name: name, Weight: 0.2})
	}
	return e
}

// quantPhaseAffinity 策略×周期相位亲和度(softmax score 基座;R6-2):
// 繁荣期动量/趋势亲和高,萧条期均值回归/事件驱动亲和高。
func quantPhaseAffinity(name string, phase CyclePhase) float64 {
	switch phase {
	case PhaseBoom:
		switch name {
		case QuantTrend:
			return 1.0
		case QuantMeanReversion:
			return 0.2
		case QuantStatArb:
			return 0.4
		case QuantMomentum:
			return 1.2
		default:
			return 0.3
		}
	case PhaseRecovery:
		switch name {
		case QuantTrend:
			return 1.0
		case QuantMeanReversion:
			return 0.4
		case QuantStatArb:
			return 0.5
		case QuantMomentum:
			return 0.9
		default:
			return 0.4
		}
	case PhaseRecession:
		switch name {
		case QuantTrend:
			return 0.3
		case QuantMeanReversion:
			return 1.1
		case QuantStatArb:
			return 0.7
		case QuantMomentum:
			return 0.1
		default:
			return 1.0
		}
	default: // PhaseDepression
		switch name {
		case QuantTrend:
			return 0.2
		case QuantMeanReversion:
			return 1.2
		case QuantStatArb:
			return 0.8
		case QuantMomentum:
			return 0.0
		default:
			return 1.1
		}
	}
}

// quantStrategyReturn 单策略本月原始收益(零 rand,确定性公式):
//
//	trend          = 1.6 × mom   (趋势放大)
//	mean_reversion = −0.9 × mom  (反趋势)
//	stat_arb       = 0.4%/月 + 0.05 × mom(低波动稳定收益)
//	momentum       = 1.3 × mom + 相位增益(繁荣 +1%/萧条 −1%)
//	event_driven   = 0.3%/月 + 困境增益(衰退/萧条 +0.4%)
func quantStrategyReturn(name string, phase CyclePhase, stockMom float64) float64 {
	switch name {
	case QuantTrend:
		return 1.6 * stockMom
	case QuantMeanReversion:
		return -0.9 * stockMom
	case QuantStatArb:
		return 0.004 + 0.05*stockMom
	case QuantMomentum:
		boost := 0.0
		switch phase {
		case PhaseBoom:
			boost = 0.010
		case PhaseDepression:
			boost = -0.010
		}
		return 1.3*stockMom + boost
	case QuantEventDriven:
		boost := 0.0
		if phase == PhaseRecession || phase == PhaseDepression {
			boost = 0.004
		}
		return 0.003 + boost
	}
	return 0
}

// quantSoftmax softmax(s/τ) 归一化(数值稳定:减最大值)。
func quantSoftmax(scores []float64, tau float64) []float64 {
	if len(scores) == 0 {
		return nil
	}
	if tau <= 1e-9 {
		tau = 1e-9
	}
	maxS := scores[0]
	for _, s := range scores {
		if s > maxS {
			maxS = s
		}
	}
	out := make([]float64, len(scores))
	var sum float64
	for i, s := range scores {
		e := math.Exp((s - maxS) / tau)
		out[i] = e
		sum += e
	}
	if sum <= 1e-12 {
		for i := range out {
			out[i] = 1 / float64(len(out))
		}
		return out
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// MonthlyStep 月度推进(⑥B 首位;见头注释 ①-④)。nil 安全;零 rand。
func (e *QuantFundEngine) MonthlyStep(w *World) {
	if e == nil || w == nil || w.Market == nil {
		return
	}
	if len(e.Strategies) == 0 {
		*e = *NewQuantFundEngine()
	}
	if e.Index <= 0 {
		e.Index = QuantIndexBase
	}
	if e.lastStockIdx <= 0 {
		e.lastStockIdx = w.Market.StockIndex
	}

	// ① 各策略原始收益(环比 = 当期股指/上月锚 − 1)。
	stockMom := w.Market.StockIndex/e.lastStockIdx - 1
	if stockMom > 10 {
		stockMom = 10 // 防御:外部改写股指的极端注入。
	}
	if stockMom < -0.99 {
		stockMom = -0.99
	}
	phase := w.Market.CyclePhase
	for _, s := range e.Strategies {
		s.LastReturn = quantStrategyReturn(s.Name, phase, stockMom)
	}

	// ② 组合收益 + clamp ±8%(R6-2)。
	var raw float64
	for _, s := range e.Strategies {
		raw += s.Weight * s.LastReturn
	}
	e.LastRawReturn = raw
	r := clampF(raw, -QuantMonthlyClamp, QuantMonthlyClamp)
	e.LastMonthReturn = r

	// ③ 指数推进。
	e.Index *= 1 + r
	if e.Index < 1 {
		e.Index = 1
	}
	e.MonthsRun++

	// ④ 权重重归一化:score = 相位亲和 + 业绩反馈 → softmax。
	scores := make([]float64, len(e.Strategies))
	for i, s := range e.Strategies {
		scores[i] = quantPhaseAffinity(s.Name, phase) + QuantPerfFeedback*s.LastReturn
	}
	weights := quantSoftmax(scores, QuantSoftmaxTau)
	for i, s := range e.Strategies {
		s.Weight = weights[i]
	}

	// 环比锚滚动到本月(下月环比基准)。
	e.lastStockIdx = w.Market.StockIndex
}

// QuantWeight 策略权重查询(测试/快照用;找不到返回 0)。
func (e *QuantFundEngine) QuantWeight(name string) float64 {
	if e == nil {
		return 0
	}
	for _, s := range e.Strategies {
		if s.Name == name {
			return s.Weight
		}
	}
	return 0
}
