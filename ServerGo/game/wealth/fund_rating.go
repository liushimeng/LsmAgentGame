// Package wealth — fund_rating.go: 基金评级引擎(5 星)(2026-09-21 §城市扩张v2.12 阶段6)。
//
// 5 只虚拟公募基金(玩家不可直接申赎,评级进 view 快照影响决策信息面):
//
//	F01 稳债增益   equity 10% —— 债性为主,波动最小
//	F02 均衡混合   equity 40%
//	F03 成长先锋   equity 80%
//	F04 全市场指数 equity 100% —— 与 Market.StockIndex 同涨跌(扣费)
//	F05 量化对冲   equity 50% + 量化引擎 alpha 联动(QuantEngine.LastMonthReturn)
//
// 月度评级(⑥B 末位执行,量化引擎先跑 → F05 可消费本月量化收益):
//
//	夏普(年化,rf=2%) > 2 且回撤 < 10% → 5 星
//	夏普 > 1.5                       → 4 星
//	夏普 > 1                         → 3 星
//	夏普 > 0.5                       → 2 星
//	其余                              → 1 星
//	评级漂移限制:单月最多 ±1 星(防评级跳水,真实评级机构同款约束)。
//
// 收益生成:零 rand —— r = equityW×股票环比 + (1−equityW)×债券票息/12 − 管理费/12
// (+ F05 量化联动项)。确定性公式 ⇒ 固定种子存量对局回归零偏移(treasury/供应链
// 同款纪律)。World 无 JSON 持久化路径,未导出 profile 字段仅存内存安全。
package wealth

import "math"

// 基金评级常量(阶段6 新定)。
const (
	// FundRatingMinHistory 评级最小样本月数(<3 月不评,星数维持初值 3)。
	FundRatingMinHistory = 3
	// FundSharpe5 / 4 / 3 / 2 星阈值(契约:2.0/1.5/1.0/0.5)。
	FundSharpe5 = 2.0
	FundSharpe4 = 1.5
	FundSharpe3 = 1.0
	FundSharpe2 = 0.5
	// FundDD5StarLimit 5 星最大回撤上限(<10%)。
	FundDD5StarLimit = 0.10
	// FundQuantAlphaWeight F05 量化联动系数(对冲腿吸收 60% 量化月收益)。
	FundQuantAlphaWeight = 0.60
)

// FundRating 单只基金评级状态(挂 World.FundRatings;view 下发 top5)。
type FundRating struct {
	FundID        string  `json:"fund_id"`
	Name          string  `json:"name"`
	Stars         int     `json:"stars"`          // 1-5(初值 3)
	Sharpe        float64 `json:"sharpe"`         // 年化夏普(小数)
	MaxDrawdown   float64 `json:"max_drawdown"`   // 近 12 月最大回撤(正数)
	AUM           float64 `json:"aum_wan"`        // 管理规模(万元)
	MonthlyReturn [12]float64 `json:"monthly_return"` // 近 12 月收益率(旧→新)

	// ── 未导出 profile(引擎内;构造于 NewFundRatings,不序列化)──
	equityW     float64 // 权益仓位 0..1
	feeAnnual   float64 // 管理费率(年化)
	lastStockIdx float64 // 上月末股指(环比基准)
	filled      int     // 已积累样本月数(≤12)
}

// fundProfileSpec 基金静态规格(NewFundRatings 表驱动)。
type fundProfileSpec struct {
	id, name string
	equityW  float64
	fee      float64
	aumWan   float64
}

// fundProfileSpecs 5 只基金静态表(顺序固定 = FundRatings 下发序)。
var fundProfileSpecs = []fundProfileSpec{
	{"F01", "稳债增益", 0.10, 0.005, 8000},
	{"F02", "均衡混合", 0.40, 0.008, 12000},
	{"F03", "成长先锋", 0.80, 0.012, 20000},
	{"F04", "全市场指数", 1.00, 0.005, 30000},
	{"F05", "量化对冲", 0.50, 0.015, 15000},
}

// NewFundRatings 构造 5 只基金初值(星数 3;lastStockIdx = 当前股指,首月环比 0)。
func NewFundRatings(stockIdx float64) []FundRating {
	out := make([]FundRating, 0, len(fundProfileSpecs))
	for _, s := range fundProfileSpecs {
		if stockIdx <= 0 {
			stockIdx = InitialStockIndex
		}
		out = append(out, FundRating{
			FundID: s.id, Name: s.name, Stars: 3, AUM: s.aumWan,
			equityW: s.equityW, feeAnnual: s.fee, lastStockIdx: stockIdx,
		})
	}
	return out
}

// fundStars 评级分层(纯函数,契约阈值):
// 夏普>2且回撤<10% → 5;夏普>1.5 → 4;>1 → 3;>0.5 → 2;其余 1。
func fundStars(sharpe, maxDD float64) int {
	switch {
	case sharpe > FundSharpe5 && maxDD < FundDD5StarLimit:
		return 5
	case sharpe > FundSharpe4:
		return 4
	case sharpe > FundSharpe3:
		return 3
	case sharpe > FundSharpe2:
		return 2
	default:
		return 1
	}
}

// fundSharpe 年化夏普 = (mean×12 − rf) / (std×√12)。
// rf 取当期无风险基准(PhaseTable.BondRate)—— 零方差债腿的超额收益≈0,
// 权益仓位才是分层驱动(否则纯债基金因零波动反得虚高夏普)。
// std 下限 1e-6:全常数收益列(零方差)时夏普取大数 → clamp 封顶 9。
func fundSharpe(returns []float64, rf float64) float64 {
	n := len(returns)
	if n == 0 {
		return 0
	}
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(n)
	var sq float64
	for _, r := range returns {
		d := r - mean
		sq += d * d
	}
	std := math.Sqrt(sq / float64(n))
	if std < 1e-6 {
		std = 1e-6
	}
	sharpe := (mean*12 - rf) / (std * math.Sqrt(12))
	// 展示 clamp:极端零方差路径夏普爆炸 → 封顶 9(仍稳拿 5 星,不污染快照)。
	if sharpe > 9 {
		sharpe = 9
	}
	if sharpe < -9 {
		sharpe = -9
	}
	return sharpe
}

// fundMaxDrawdown 近窗口最大回撤(正数小数;峰值到谷值)。
func fundMaxDrawdown(returns []float64) float64 {
	cum, peak, dd := 1.0, 1.0, 0.0
	for _, r := range returns {
		cum *= 1 + r
		if cum > peak {
			peak = cum
		}
		if d := (peak - cum) / peak; d > dd {
			dd = d
		}
	}
	if dd < 0 {
		return 0
	}
	return dd
}

// fundMonthlyReturn 单基金本月收益率(零 rand,确定性公式):
// r = equityW×stockMom + (1−equityW)×bondYield/12 − fee/12 [+ 量化联动(F05)]。
func (f *FundRating) fundMonthlyReturn(stockMom, bondYield, quantAlpha float64) float64 {
	r := f.equityW*stockMom + (1-f.equityW)*bondYield/12 - f.feeAnnual/12
	if f.FundID == "F05" {
		r += FundQuantAlphaWeight * quantAlpha
	}
	return r
}

// rateFundUpdate 单基金月度推进:记收益 → 更新夏普/回撤/AUM → 星数(漂移 ±1)。
// rf 为当期无风险基准(PhaseTable.BondRate)。
func (f *FundRating) rateFundUpdate(stockMom, bondYield, rf, quantAlpha float64) {
	r := f.fundMonthlyReturn(stockMom, bondYield, quantAlpha)

	// 滚动窗口:未满 12 尾接;满 12 左移。
	if f.filled < 12 {
		f.MonthlyReturn[f.filled] = r
		f.filled++
	} else {
		copy(f.MonthlyReturn[:], f.MonthlyReturn[1:])
		f.MonthlyReturn[11] = r
	}

	window := make([]float64, f.filled)
	copy(window, f.MonthlyReturn[:f.filled])

	// 样本不足:维持星数(初值 3),仅更新 AUM。
	if f.filled >= FundRatingMinHistory {
		f.Sharpe = fundSharpe(window, rf)
		f.MaxDrawdown = fundMaxDrawdown(window)
		raw := fundStars(f.Sharpe, f.MaxDrawdown)
		// 评级漂移限制:单月最多 ±1 星。
		f.Stars = clamp(raw, f.Stars-1, f.Stars+1)
		if f.Stars < 1 {
			f.Stars = 1
		}
		if f.Stars > 5 {
			f.Stars = 5
		}
	}
	f.AUM *= 1 + r
	if f.AUM < 0 {
		f.AUM = 0
	}
}

// rateFundsMonthly 全房基金评级月度推进(SettleMonth ⑥B 末位)。
// 空表惰性初始化(旧档防御);零 rand。
func rateFundsMonthly(w *World) {
	if w == nil || w.Market == nil {
		return
	}
	if len(w.FundRatings) == 0 {
		// 首月播种后**继续**推进(环比 0,当月即积累样本;星数守 3)。
		w.FundRatings = NewFundRatings(w.Market.StockIndex)
	}
	stockMom := 0.0
	if w.Market.StockIndex > 0 {
		stockMom = w.Market.StockIndex/w.FundRatings[0].lastStockIdx - 1
	}
	if stockMom > 10 {
		stockMom = 10 // 防御:外部改写股指的极端注入。
	}
	if stockMom < -0.99 {
		stockMom = -0.99
	}
	bondYield := w.Market.Params().BondRate
	quantAlpha := 0.0
	if w.QuantEngine != nil {
		quantAlpha = w.QuantEngine.LastMonthReturn
	}
	for i := range w.FundRatings {
		f := &w.FundRatings[i]
		if f.lastStockIdx <= 0 {
			f.lastStockIdx = w.Market.StockIndex
			continue
		}
		f.rateFundUpdate(stockMom, bondYield, bondYield, quantAlpha)
		f.lastStockIdx = w.Market.StockIndex
	}
}
