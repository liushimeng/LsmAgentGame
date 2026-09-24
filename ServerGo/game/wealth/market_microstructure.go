// Package wealth — market_microstructure.go: 股票交易微观结构(批次20 文档3 B2)。
//
// 四件套(作用域 = stock_index 一类资产,不影响 gold/bond/fund/可转债):
//   - B2-1 涨跌停板:月涨跌 ±10% 保号截断(clamp 在 MonthStep 内,
//     未越界的月份乘式与旧实现逐位一致);
//   - B2-2 买卖价差:按市场阶段 bid/ask 双边价(过热 0.20% / 扩张 0.04% /
//     衰退 0.08% / 恐慌 0.15%);
//   - B2-3 T+1:当月买入冻结(Player.StockT1Locked,settlement 开头清零;
//     月粒度模拟下「当日」= 当月的投影);
//   - B2-4 熔断:clamp 前原始单月跌幅 ≤ −15% → 下全月禁股票买卖
//     (BreakerUntilMonth 存「最后一个禁止月」,同月重复触发只延长不叠加)。
//
// 全部确定性或既有 rand 通道 —— 无新增 rand 消耗;历史 seed 对局在
// ±10% 带内(σ=3% 月波动 p99 <10%)预期绝大多数零偏移,但涨跌停为政策类
// 变更,不承诺逐分不差(验收以统计带内断言,批次 11 先例)。
package wealth

import (
	"fmt"
)

// 微观结构常量(文档3 B2-1/B2-4)。
const (
	// stockLimitRate 月涨跌停板幅度(±10%)。
	stockLimitRate = 0.10
	// stockBreakerRate 熔断触发线:clamp 前原始单月跌幅 ≤ −15%。
	stockBreakerRate = -0.15
)

// stockSpreadForPhase 阶段买卖价差表(文档3 B2-2)。
// 阶段名映射:overheated 过热 = PhaseBoom 繁荣、expansion 扩张 =
// PhaseRecovery 复苏(取流动性最好 0.04%)、recession 衰退 = PhaseRecession、
// panic 恐慌 = PhaseDepression 萧条。
func stockSpreadForPhase(p CyclePhase) float64 {
	switch p {
	case PhaseBoom:
		return 0.0020 // 过热 0.20%
	case PhaseRecovery:
		return 0.0004 // 扩张 0.04%
	case PhaseRecession:
		return 0.0008 // 衰退 0.08%
	case PhaseDepression:
		return 0.0015 // 恐慌 0.15%
	default:
		return 0.0008 // 未知阶段兜底(同衰退)
	}
}

// StockSpread 当前阶段双边价差(小数;bps = ×10000)。
func (m *MarketState) StockSpread() float64 {
	if m == nil {
		return 0
	}
	return stockSpreadForPhase(m.CyclePhase)
}

// StockBuyUnit 股票买入单价 = idx × (1 + sp/2)(每份,面值 1 元口径)。
func (m *MarketState) StockBuyUnit() float64 {
	if m == nil {
		return 0
	}
	return m.StockIndex * (1 + m.StockSpread()/2)
}

// StockSellUnit 股票卖出单价 = idx × (1 − sp/2)。
func (m *MarketState) StockSellUnit() float64 {
	if m == nil {
		return 0
	}
	return m.StockIndex * (1 - m.StockSpread()/2)
}

// StockBreakerActive month 是否处于熔断禁止期。BreakerUntilMonth 存
// 「最后一个禁止月」,w.Month > BreakerUntilMonth 即自动解禁;0 = 从未触发。
func (m *MarketState) StockBreakerActive(month int) bool {
	return m != nil && m.BreakerUntilMonth > 0 && month <= m.BreakerUntilMonth
}

// clampStockMonthlyLimit 涨跌停截断(B2-1):newIdx 越出
// [old×(1−10%), old×(1+10%)] 时保号取边界;带内原样返回(逐位不变)。
func clampStockMonthlyLimit(oldIdx, newIdx float64) float64 {
	if oldIdx <= 0 {
		return newIdx
	}
	lo := oldIdx * (1 - stockLimitRate)
	hi := oldIdx * (1 + stockLimitRate)
	if newIdx > hi {
		return hi
	}
	if newIdx < lo {
		return lo
	}
	return newIdx
}

// checkStockCircuitBreaker settlement ④ MonthStep 之后调用(B2-4):
// clamp 前原始 Δ_raw ≤ −15% → BreakerUntilMonth = 下月(只延长不叠加)+
// event 播报。仅股票作用域;其它资产不受影响。LastStockRawDelta 每次
// MonthStep 覆写,本函数每月结至多一次判定。
func (w *World) checkStockCircuitBreaker() {
	if w.Market == nil || w.Market.LastStockRawDelta > stockBreakerRate {
		return
	}
	next := w.Month + 1
	if next > w.Market.BreakerUntilMonth {
		w.Market.BreakerUntilMonth = next
	}
	dropPct := -w.Market.LastStockRawDelta * 100
	w.emitEvent("market", -1, fmt.Sprintf(
		"熔断:指数单月下跌 %.1f%%,全市场股票交易暂停 1 个月(第 %d 月)", dropPct, next))
}
