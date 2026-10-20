// Package virtual_city — bubble.go: 集体杠杆 → 泡沫 → 破裂(批次62 §20261020-62)。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市社交资本与信用体系设计.md §11
//   §11.1 三项集体指标(市场杠杆率 >60% / 资产换手率 >30% / 现金占比 <10%)
//   §11.2 状态机(复苏 → 过热 → 破裂 → 衰退 → 复苏)
//   §11.3 破裂播报话术(把"市场周期是运气"扭转为"市场周期是集体行为的结果")
//
// 与既有 market.go 四阶段(复苏/繁荣/衰退/萧条)**正交**:市场周期由
// 转移矩阵 + 骰子掷出,泡沫状态由**居民集体行为**驱动 —— 这正是本模块
// 存在的意义:让居民的选择反作用于宏观(反身性)。
package virtual_city

import (
	"fmt"
	"math"
)

// BubbleState 泡沫状态(与 CyclePhase 正交)。
type BubbleState string

const (
	BubbleNone      BubbleState = "none"      // 常态
	BubbleOverheat  BubbleState = "overheat"  // 过热(信贷过热)
	BubbleBurst     BubbleState = "burst"     // 破裂(一次性冲击月)
	BubbleRecession BubbleState = "recession" // 泡沫后衰退(强制 2~4 月)
)

// 泡沫阈值(§11.1)。
const (
	bubbleLeverageOverheat = 0.60 // 市场杠杆率 > 60% ⇒ 信贷过热
	bubbleLeverageBurst    = 0.80 // 杠杆率 > 80% ⇒ 立即破裂
	bubbleTurnoverHot      = 0.30 // 资产换手率 > 30% ⇒ 投机过热
	bubbleCashTight        = 0.10 // 现金占比 < 10% ⇒ 流动性紧张
)

// 泡沫期的价格与利率参数(§11.2 状态表)。
const (
	bubbleOverheatDriftMult = 1.50 // 过热期:资产价格月度涨幅 ×1.5
	bubbleOverheatRatePush  = 0.0020 // 过热期:政策利率一次性上行压力 +0.20 个百分点
	bubbleRecessionRateCut  = 0.0020 // 泡沫后衰退期:政策利率一次性下调 −0.20 个百分点
	bubbleRecessionDriftAdd = -0.02 // 泡沫后衰退期:资产价格阴跌 −2%/月
	bubbleBurstDropMin      = 0.20 // 破裂:资产价格一次性下跌下限(杠杆率 0.60)
	bubbleBurstDropMax      = 0.40 // 破裂:资产价格一次性下跌上限(杠杆率 ≥0.80)
	bubbleBurstGoldFactor   = 0.40 // 破裂时黄金跌幅系数(避险资产少跌)
)

// bubbleBurstDrop 破裂跌幅(按杠杆率线性映射:0.60→20%,0.80+→40%)。
func bubbleBurstDrop(leverage float64) float64 {
	if leverage <= bubbleLeverageOverheat {
		return bubbleBurstDropMin
	}
	if leverage >= bubbleLeverageBurst {
		return bubbleBurstDropMax
	}
	t := (leverage - bubbleLeverageOverheat) / (bubbleLeverageBurst - bubbleLeverageOverheat)
	return bubbleBurstDropMin + t*(bubbleBurstDropMax-bubbleBurstDropMin)
}

// bubbleStateCN 泡沫状态中文名。
func bubbleStateCN(s BubbleState) string {
	switch s {
	case BubbleOverheat:
		return "信贷过热"
	case BubbleBurst:
		return "泡沫破裂"
	case BubbleRecession:
		return "泡沫后衰退"
	}
	return "常态"
}

// BubbleSnapshot 泡沫状态快照(view 层下发)。
type BubbleSnapshot struct {
	State          BubbleState `json:"state"`
	LeverageRatio  float64     `json:"leverage_ratio"`
	TurnoverRatio  float64     `json:"turnover_ratio"`
	CashRatio      float64     `json:"cash_ratio"`
	MonthsInState  int         `json:"months_in_state"`
	Message        string      `json:"message"`
}

// computeBubbleMetrics 计算三项集体指标(§11.1)。
//
// 市场杠杆率 = 全体居民总负债 / 总净资产(净资产 ≤0 的居民不计入分母);
// 资产换手率 = 本回合成交资产数 / 全体持有资产数;
// 现金占比   = 全体现金 / 全体总资产(现金 + 资产市值)。
func (w *World) computeBubbleMetrics() (leverage, turnover, cashRatio float64) {
	var totalDebt, totalNetWorth, totalCash, totalAssets int64
	assetCount := 0
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		var debt int64
		for i := range p.Loans {
			debt += p.Loans[i].Balance
		}
		totalDebt += debt
		nw := p.NetWorth(w.Market)
		if nw > 0 {
			totalNetWorth += nw
		}
		totalCash += p.Cash
		for i := range p.Assets {
			totalAssets += AssetValue(&p.Assets[i], w.Market)
			assetCount++
		}
	}
	if totalNetWorth > 0 {
		leverage = float64(totalDebt) / float64(totalNetWorth)
	}
	if assetCount > 0 {
		turnover = float64(w.TurnTradeCount) / float64(assetCount)
	}
	grand := totalCash + totalAssets
	if grand > 0 {
		cashRatio = float64(totalCash) / float64(grand)
	}
	return leverage, turnover, cashRatio
}

// bubbleMonthlyStep 泡沫状态机月度推进(§11.2)。
//
// 状态迁移:
//
//	none ──杠杆率>0.60──→ overheat ──持续≥2月 或 杠杆率>0.80──→ burst
//	                                                              │
//	                        none ←── 衰退锁定 2~4 月后自然恢复 ── recession
//
// burst 为一次性冲击月(当月完成价格冲击),次月进入 recession。
func (w *World) bubbleMonthlyStep() {
	w.BubbleLeverage, w.BubbleTurnover, w.BubbleCash = w.computeBubbleMetrics()
	switch w.BubbleState {
	case BubbleNone:
		if w.BubbleLeverage > bubbleLeverageOverheat {
			w.BubbleState = BubbleOverheat
			w.BubbleMonths = 1
			w.emitEvent("market", -1, fmt.Sprintf(
				"信贷过热:全场居民平均杠杆率达到 %.0f%%,资产价格被信贷推高",
				w.BubbleLeverage*100))
		}
	case BubbleOverheat:
		w.BubbleMonths++
		if w.BubbleMonths >= 2 || w.BubbleLeverage > bubbleLeverageBurst {
			w.bubbleBurst()
		}
	case BubbleBurst:
		// burst 为一次性冲击月:次月强制进入衰退期。
		w.BubbleState = BubbleRecession
		w.BubbleMonths = 1
		// 衰退月数确定性取 2~4(按当前月份取模,禁用全局 rand)。
		w.BubbleRecessionMonths = 2 + (w.Month % 3)
		w.emitEvent("market", -1, "泡沫破裂后的强制衰退期开始(2~4 个月)")
	case BubbleRecession:
		w.BubbleMonths++
		if w.BubbleMonths > w.BubbleRecessionMonths {
			w.BubbleState = BubbleNone
			w.BubbleMonths = 0
			w.BubbleRecessionMonths = 0
			w.emitEvent("market", -1, "泡沫后衰退期结束,市场回归常态")
		}
	}
	// 泡沫期对月度价格漂移的乘子 / 加成(在 Market.MonthStep 之前设置)。
	w.applyBubbleDrift()
	// 本回合成交笔数用完即清(换手率的分子是"本回合"口径)。
	w.TurnTradeCount = 0
}

// applyBubbleDrift 把泡沫状态映射到市场月度漂移参数。
//
//	过热期:涨幅 ×1.5(信贷推高资产价格)
//	衰退期:阴跌 −2%/月(泡沫破裂后的余波)
//
// ⚠ 只作用于**杠杆资产**(股票 + 房产指数);黄金由既有 PhaseTable 决定
// (避险属性),不在此处干预 —— 见 bubbleBurst 的黄金单独处理。
func (w *World) applyBubbleDrift() {
	m := w.Market
	if m == nil {
		return
	}
	m.DriftMultiplier = 1.0
	m.DriftExtra = 0
	switch w.BubbleState {
	case BubbleOverheat:
		m.DriftMultiplier = bubbleOverheatDriftMult
	case BubbleRecession:
		m.DriftExtra = bubbleRecessionDriftAdd
	}
}

// bubbleBurst 泡沫破裂:一次性价格冲击 + 爆仓判定 + 教学播报(§11.2/§11.3)。
func (w *World) bubbleBurst() {
	drop := bubbleBurstDrop(w.BubbleLeverage)
	m := w.Market
	// 股票:全额下跌。
	m.StockIndex = math.Max(0.01, m.StockIndex*(1-drop))
	// 房产指数:全额下跌(各区同步)。
	for id, idx := range m.DistrictIdx {
		m.DistrictIdx[id] = math.Max(0.05, idx*(1-drop))
	}
	// 黄金:跌幅按 bubbleBurstGoldFactor 折算(避险资产少跌)。
	m.GoldPrice = math.Max(1, m.GoldPrice*(1-drop*bubbleBurstGoldFactor))

	w.BubbleState = BubbleBurst
	w.BubbleMonths = 1
	w.BubbleBurstMonth = w.Month
	w.BubbleMessage = fmt.Sprintf(
		"本次市场破裂的直接原因是:全场居民平均杠杆率达到 %.0f%%,资产价格被信贷推高至脱离基本面。"+
			"最先加杠杆的居民获得了最高收益,最后加杠杆的居民承担了最大损失。"+
			"这就是真实世界中每一轮泡沫的共同结构。", w.BubbleLeverage*100)
	w.emitEvent("market", -1, w.BubbleMessage)
	w.emitEvent("market", -1, fmt.Sprintf("资产价格一次性下跌 %.0f%%(杠杆率 %.0f%% 映射)",
		drop*100, w.BubbleLeverage*100))
}

// bubbleBustCheck 破裂当月的爆仓判定:负债 / 总资产 > 1.0 的居民进入清算。
//
// 复用既有清算流程(settlement.go::liquidate)—— 不新写一套清算规则。
// 仅在「本月发生破裂」时触发(避免每帧清算)。
func (w *World) bubbleBustCheck(p *Player) bool {
	if w.BubbleState != BubbleBurst || w.BubbleBurstMonth != w.Month {
		return false
	}
	var debt, assets int64
	for i := range p.Loans {
		debt += p.Loans[i].Balance
	}
	assets = p.Cash
	for i := range p.Assets {
		assets += AssetValue(&p.Assets[i], w.Market)
	}
	if assets <= 0 {
		return false
	}
	return float64(debt)/float64(assets) > 1.0
}

// applyBubbleRateShift 泡沫对政策利率的影响(在央行月度决策入口叠加)。
//
//	过热期首次进入:政策利率一次性 +0.20 个百分点(信贷过热 → 央行加息压力)
//	泡沫后衰退期首次进入:政策利率一次性 −0.20 个百分点(需求萎缩 → 降息)
//
// 用 BubbleRateShiftApplied 标记保证**每个泡沫周期只施加一次**(水平位移,
// 不是逐月累加 —— 逐月 +20bp 会在 3 个月内把政策利率推高 60bp,失真)。
func (w *World) applyBubbleRateShift(cb *CentralBankState) {
	if cb == nil {
		return
	}
	switch w.BubbleState {
	case BubbleOverheat:
		if w.BubbleRateShiftApplied {
			return
		}
		cb.PolicyRate += bubbleOverheatRatePush
		if cb.PolicyRate < 0.005 {
			cb.PolicyRate = 0.005
		}
		cb.RediscountRate = cb.PolicyRate + 0.005
		w.BubbleRateShiftApplied = true
	case BubbleRecession:
		if w.BubbleRateShiftApplied {
			return
		}
		cb.PolicyRate -= bubbleRecessionRateCut
		if cb.PolicyRate < 0.005 {
			cb.PolicyRate = 0.005
		}
		cb.RediscountRate = cb.PolicyRate + 0.005
		w.BubbleRateShiftApplied = true
	default:
		w.BubbleRateShiftApplied = false
	}
}

// bubbleSnapshot 构造 view 层快照。
func (w *World) bubbleSnapshot() *BubbleSnapshot {
	return &BubbleSnapshot{
		State:         w.BubbleState,
		LeverageRatio: w.BubbleLeverage,
		TurnoverRatio: w.BubbleTurnover,
		CashRatio:     w.BubbleCash,
		MonthsInState: w.BubbleMonths,
		Message:       w.BubbleMessage,
	}
}
