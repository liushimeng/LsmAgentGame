// Package virtual_city — liquidity.go: 资产流动性价差表(批次62 §20261020-62)。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市社交资本与信用体系设计.md §7
//   §7.2 买卖价差表(现金 0% / 股票 2% / 债券理财 3% / 贵金属 5% /
//         房产 8%~12% / 企业股权 15%~25% / 收藏品另类 20%~30%)
//   §7.3 流动性折价的极端情形(繁荣 ×0.7 / 衰退 ×1.8 / 破产清算额外 −30% /
//         挂单超 5 回合未成交自动降价 10%)
//
// 教育意图:流动性是资产的**隐性成本** —— 房产涨 20%,买卖一次要吃掉
// 10% 的价差 + 税费;新手最容易只看"涨幅"不看"能不能变现"。
//
// 与 market_microstructure.go 的股票**微观结构阶段价差**(过热 0.20% /
// 扩张 0.04% …)是两回事:前者是交易所撮合的 tick 价差,后者是**资产类别
// 的流动性溢价**。本模块的股票价差与微观结构价差**叠加**(不覆盖),
// 见 §4「股票:二者叠加」。
package virtual_city

import "math"

// AssetClass 资产类别(价差表键)。
type AssetClass string

const (
	ClassCash        AssetClass = "cash"        // 现金
	ClassStock       AssetClass = "stock"       // 股票(流通)
	ClassBond        AssetClass = "bond"        // 债券 / 理财
	ClassGold        AssetClass = "gold"        // 贵金属
	ClassHouse       AssetClass = "house"       // 房产(住宅 + 商铺)
	ClassEquity      AssetClass = "equity"      // 企业股权
	ClassCollectible AssetClass = "collectible" // 收藏品 / 另类
)

// liquiditySpreadTable 买卖价差表(§7.2;区间取中值)。
var liquiditySpreadTable = map[AssetClass]float64{
	ClassCash:        0.00,
	ClassStock:       0.02,
	ClassBond:        0.03,
	ClassGold:        0.05,
	ClassHouse:       0.10, // 8%~12% 取 10%
	ClassEquity:      0.20, // 15%~25% 取 20%
	ClassCollectible: 0.25, // 20%~30% 取 25%
}

// liquidityPhaseMultiplier 周期修正系数(§7.3)。
func liquidityPhaseMultiplier(phase CyclePhase) float64 {
	switch phase {
	case PhaseBoom:
		return 0.70 // 繁荣:好卖,价差收窄
	case PhaseRecession, PhaseDepression:
		return 1.80 // 衰退/萧条:无人接盘,价差放大
	}
	return 1.0
}

// LiquiditySpread 当前某资产类别的买卖价差(小数;已含周期修正)。
//
// ⚠ 股票最终成交价差 = 本表(2%) **叠加** 微观结构阶段价差
// (market_microstructure.go::stockSpreadForPhase),二者口径不同:
// 前者=资产流动性溢价,后者=交易所撮合价差。
func (w *World) LiquiditySpread(class AssetClass) float64 {
	base := liquiditySpreadTable[class]
	if base <= 0 {
		return 0
	}
	phase := PhaseRecovery
	if w != nil && w.Market != nil {
		phase = w.Market.CyclePhase
	}
	return base * liquidityPhaseMultiplier(phase)
}

// liquidityBuyPrice 买入成交价 = 公允价 × (1 + 价差/2)(中点成交口径)。
func (w *World) liquidityBuyPrice(fair int64, class AssetClass) int64 {
	if fair <= 0 {
		return fair
	}
	return int64(math.Round(float64(fair) * (1 + w.LiquiditySpread(class)/2)))
}

// liquiditySellPrice 卖出成交价 = 公允价 × (1 − 价差/2)(中点成交口径)。
func (w *World) liquiditySellPrice(fair int64, class AssetClass) int64 {
	if fair <= 0 {
		return fair
	}
	return int64(math.Round(float64(fair) * (1 - w.LiquiditySpread(class)/2)))
}

// liquiditySpreadSnapshot view 层下发的价差快照(§7.2 原始表 + 周期系数)。
type LiquiditySpreadSnapshot struct {
	Stock           float64 `json:"stock"`
	Bond            float64 `json:"bond"`
	Gold            float64 `json:"gold"`
	House           float64 `json:"house"`
	Equity          float64 `json:"equity"`
	Collectible     float64 `json:"collectible"`
	PhaseMultiplier float64 `json:"phase_multiplier"`
}

// liquiditySpreadSnapshotOf 构造价差快照。
func (w *World) liquiditySpreadSnapshotOf() LiquiditySpreadSnapshot {
	phase := PhaseRecovery
	if w != nil && w.Market != nil {
		phase = w.Market.CyclePhase
	}
	return LiquiditySpreadSnapshot{
		Stock:           liquiditySpreadTable[ClassStock] * liquidityPhaseMultiplier(phase),
		Bond:            liquiditySpreadTable[ClassBond] * liquidityPhaseMultiplier(phase),
		Gold:            liquiditySpreadTable[ClassGold] * liquidityPhaseMultiplier(phase),
		House:           liquiditySpreadTable[ClassHouse] * liquidityPhaseMultiplier(phase),
		Equity:          liquiditySpreadTable[ClassEquity] * liquidityPhaseMultiplier(phase),
		Collectible:     liquiditySpreadTable[ClassCollectible] * liquidityPhaseMultiplier(phase),
		PhaseMultiplier: liquidityPhaseMultiplier(phase),
	}
}
