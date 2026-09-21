// Package wealth — phillips_curve.go: 菲利普斯曲线接入
// (2026-09-21 §城市扩张v2.12 阶段3)。
//
// 简化菲利普斯规则:以通胀缺口与失业缺口推断央行政策倾向(dovish/neutral/
// hawkish)与利率调整建议 RateBias(±5% clamp)。输入取自 World.CB(内生 CPI)
// 与 World.Labor(内生失业率/工资增速),nil 安全(回落自然率/目标值)。
// 纯函数(§92a 无锁):只读 World,不改任何状态。
// 阶段 4 央行行长 Bot(AgentClassCityBanker)消费本输出驱动 PolicyToolbox。
package wealth

import "fmt"

// 菲利普斯规则阈值常量(v2.12 阶段3 新定)。
const (
	// PhillipsHawkCPI CPI 高于 5% → 鹰派(加息),RateBias = +3%。
	PhillipsHawkCPI = 0.05
	// PhillipsHawkBias 鹰派规则利率建议 +3%(0.03)。
	PhillipsHawkBias = 0.03
	// PhillipsDovishUnemploy 失业率高于 8% → 鸽派(降息),RateBias = −3%。
	PhillipsDovishUnemploy = 0.08
	// PhillipsDovishBias 鸽派规则利率建议 −3%(−0.03)。
	PhillipsDovishBias = -0.03
	// PhillipsNeutralCPILo/Hi 中性通胀带 [2%,3%]。
	PhillipsNeutralCPILo = 0.02
	PhillipsNeutralCPIHi = 0.03
	// PhillipsNeutralUnempLo/Hi 中性失业带 [4%,6%]。
	PhillipsNeutralUnempLo = 0.04
	PhillipsNeutralUnempHi = 0.06
	// PhillipsBiasMax 利率建议幅度 clamp ±5%。
	PhillipsBiasMax = 0.05
	// PhillipsStanceBand 线性插值分支的倾向判定带宽 ±0.5%。
	PhillipsStanceBand = 0.005
)

// PhillipsInput 菲利普斯曲线输入(月度宏观快照;利率/比率均为小数)。
type PhillipsInput struct {
	UnemploymentRate    float64 // 内生失业率(World.Labor.Unemployment)
	CPIYoY              float64 // CPI 同比(World.CB.CPI)
	CapacityUtilization float64 // 产能利用率代理 = 货币乘数/乘数上限
	WageGrowth          float64 // 工资年化增速(World.Labor.WageGrowthYoY)
}

// PhillipsOutput 菲利普斯曲线输出:政策倾向 + 利率调整建议。
type PhillipsOutput struct {
	Stance      string  // dovish / neutral / hawkish
	RateBias    float64 // 利率调整建议(小数;+ 加息 / − 降息;clamp ±5%)
	Description string  // 人读摘要(事件流 / 央行行长 Bot 上下文)
}

// CollectPhillipsInput 从 World 收集菲利普斯输入(nil 安全)。
//
//	失业率    : w.Labor.Unemployment(nil → 自然率 5%)
//	CPI YoY  : w.CB.CPI(nil → 目标 2%)
//	产能利用率: w.CB.MoneyMultiplier/MultiplierCeiling(nil → 85% 目标利用率)
//	工资增速  : w.Labor.WageGrowthYoY(nil → 3% 基线)
func CollectPhillipsInput(w *World) PhillipsInput {
	in := PhillipsInput{
		UnemploymentRate:    UnemployNatural,
		CPIYoY:              TargetCPI,
		CapacityUtilization: TargetUtil,
		WageGrowth:          0.03,
	}
	if w == nil {
		return in
	}
	if w.Labor != nil {
		in.UnemploymentRate = w.Labor.Unemployment
		in.WageGrowth = w.Labor.WageGrowthYoY
	}
	if w.CB != nil {
		in.CPIYoY = w.CB.CPI
		if w.CB.MultiplierCeiling > 0 {
			in.CapacityUtilization = w.CB.MoneyMultiplier / w.CB.MultiplierCeiling
		}
	}
	return in
}

// EvaluatePhillips 简化菲利普斯曲线(纯函数,只读 World):
//
//	r1  CPI > 5%                    → hawkish, RateBias = +3%
//	r2  失业率 > 8%                 → dovish,  RateBias = −3%
//	r3  CPI∈[2%,3%] 且 U∈[4%,6%]   → neutral, RateBias = 0
//	r4  其余线性插值: RateBias = (CPI−2%) − (U−5%),clamp ±5%;
//	    |RateBias| ≤ 0.5% → neutral,> +0.5% → hawkish,< −0.5% → dovish
//
// 通胀目标优先(r1 先于 r2):滞胀情形按通胀目标制央行惯例先稳物价。
func EvaluatePhillips(w *World) PhillipsOutput {
	in := CollectPhillipsInput(w)

	var out PhillipsOutput
	switch {
	case in.CPIYoY > PhillipsHawkCPI:
		out.Stance = StanceHawkish
		out.RateBias = PhillipsHawkBias
	case in.UnemploymentRate > PhillipsDovishUnemploy:
		out.Stance = StanceDovish
		out.RateBias = PhillipsDovishBias
	case in.CPIYoY >= PhillipsNeutralCPILo && in.CPIYoY <= PhillipsNeutralCPIHi &&
		in.UnemploymentRate >= PhillipsNeutralUnempLo && in.UnemploymentRate <= PhillipsNeutralUnempHi:
		out.Stance = StanceNeutral
		out.RateBias = 0
	default:
		// 线性插值:通胀缺口推动加息,失业缺口推动降息(单位增益,直接小数相减)。
		bias := (in.CPIYoY - TargetCPI) - (in.UnemploymentRate - UnemployNatural)
		out.RateBias = clampF(bias, -PhillipsBiasMax, PhillipsBiasMax)
		switch {
		case out.RateBias > PhillipsStanceBand:
			out.Stance = StanceHawkish
		case out.RateBias < -PhillipsStanceBand:
			out.Stance = StanceDovish
		default:
			out.Stance = StanceNeutral
		}
	}

	out.Description = fmt.Sprintf(
		"菲利普斯: CPI %.1f%% / 失业率 %.1f%% / 产能利用率 %.0f%% / 工资增速 %.1f%% → %s, 利率建议 %+.2f%%",
		in.CPIYoY*100, in.UnemploymentRate*100, in.CapacityUtilization*100,
		in.WageGrowth*100, out.Stance, out.RateBias*100)
	return out
}
