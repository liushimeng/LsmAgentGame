// Package virtual_city — herding.go: 羊群效应(批次62 §20261020-62)。
//
// 唯一事实来源:lag_docs/虚拟城市/虚拟城市行为金融与投资心理机制设计.md §3.4
//   §3.4.1 实时持仓可见性:每回合末广播"本回合操作摘要"(买入/卖出/持有,
//           **不含具体金额** —— 金额属于私有信息,广播即泄密)
//   §3.4.2 AI 玩家带节奏行为:繁荣期 80% 跟随多数买入 / 萧条期 70% 跟随
//           多数抛售 / 复苏期 50% 独立决策
//   §3.4.3 逆势奖励:同向人数 ≥3 时,逆势且获利者认知 +2
//   §3.4.4 羊群指数 = 跟风次数 / 总决策次数
//
// 与社交资本文档《虚拟城市社交资本与信用体系设计.md》§7.2/§7.3 的
// 流动性价差表联动(见 liquidity.go):跟风带来额外滑点成本。
package virtual_city

import "fmt"

// TurnSummary 一回合的全体操作摘要(不含金额)。
type TurnSummary struct {
	Month   int    `json:"month"`
	Buy     int    `json:"buy"`
	Sell    int    `json:"sell"`
	Hold    int    `json:"hold"`
	Majority string `json:"majority"` // buy|sell|hold|flat
}

// 多数方向常量。
const (
	MajorityBuy  = "buy"
	MajoritySell = "sell"
	MajorityHold = "hold"
	MajorityFlat = "flat"
)

// 羊群指数判定用的回看窗口(§3.4.2 "近几回合的市场多数方向")。
const herdLookbackTurns = 3

// 逆势奖励门槛(§3.4.3):同向人数 ≥3。
const counterTrendMinPeers = 3

// 逆势奖励认知点数(§3.4.3:获得认知 +2,而非通常的 +1)。
const counterTrendCognitionGain = 2

// 羊群跟风概率(§3.4.2):按市场周期给出 AI 跟随多数方向的概率。
func herdFollowProbability(phase CyclePhase) float64 {
	switch phase {
	case PhaseBoom:
		return 0.80 // 繁荣期:80% 跟随多数买入
	case PhaseDepression:
		return 0.70 // 萧条期:70% 跟随多数抛售
	case PhaseRecovery:
		return 0.50 // 复苏期:50% 独立决策(聪明钱先行)
	case PhaseRecession:
		return 0.60 // 衰退期:60% 跟随抛售(文档未单列,取保守中值)
	}
	return 0.50
}

// closeTurnSummary 回合末结算操作摘要(广播 + 入历史)。
//
// 必须由 SettleMonth 在**逐座位月结之前**调用 —— 逆势奖励判定依赖本回合
// 的同向人数(§3.4.3),而买入/卖出计数在行动阶段已完整。
func (w *World) closeTurnSummary() TurnSummary {
	sum := TurnSummary{
		Month: w.Month,
		Buy:   w.TurnBuyCount,
		Sell:  w.TurnSellCount,
	}
	// 观望人数 = 在场且未交易的人数(买入/卖出之外的全部在场居民)。
	alive := len(w.alivePlayers())
	hold := alive - sum.Buy - sum.Sell
	if hold < 0 {
		hold = 0
	}
	sum.Hold = hold
	w.TurnHoldCount = hold
	sum.Majority = majorityOf(sum.Buy, sum.Sell, sum.Hold)
	w.TurnSummaries = append(w.TurnSummaries, sum)
	// 只保留最近 12 回合(羊群指数与回看窗口够用;防无界增长)。
	if len(w.TurnSummaries) > 12 {
		w.TurnSummaries = w.TurnSummaries[len(w.TurnSummaries)-12:]
	}
	// 广播:只含方向与人数,绝不含金额(§3.4.1)。
	w.emitEvent("market", -1, fmt.Sprintf(
		"本回合操作摘要:买入 %d 人 / 卖出 %d 人 / 观望 %d 人(多数方向:%s)",
		sum.Buy, sum.Sell, sum.Hold, majorityCN(sum.Majority)))
	// 重置本回合计数(下回合重新累计)。
	w.TurnBuyCount = 0
	w.TurnSellCount = 0
	w.TurnHoldCount = 0
	return sum
}

// majorityOf 三个计数里的多数方向(平票 / 全零 ⇒ flat)。
func majorityOf(buy, sell, hold int) string {
	max := buy
	dir := MajorityBuy
	if sell > max {
		max = sell
		dir = MajoritySell
	}
	if hold > max {
		dir = MajorityHold
	}
	if max <= 0 {
		return MajorityFlat
	}
	return dir
}

// majorityCN 多数方向中文名。
func majorityCN(dir string) string {
	switch dir {
	case MajorityBuy:
		return "买入"
	case MajoritySell:
		return "卖出"
	case MajorityHold:
		return "观望"
	}
	return "无明显方向"
}

// marketMajorityDirection 近 herdLookbackTurns 回合的多数方向(累计票数口径)。
// 返回 MajorityFlat 表示无明显方向(此时既不算跟风也不算逆势)。
func (w *World) marketMajorityDirection() string {
	if len(w.TurnSummaries) == 0 {
		return MajorityFlat
	}
	buy, sell, hold := 0, 0, 0
	start := len(w.TurnSummaries) - herdLookbackTurns
	if start < 0 {
		start = 0
	}
	for _, s := range w.TurnSummaries[start:] {
		buy += s.Buy
		sell += s.Sell
		hold += s.Hold
	}
	return majorityOf(buy, sell, hold)
}

// recordTradeDirection 记录一次买入/卖出方向并判定跟风/逆势/中性(§3.4.4)。
//
// dir = "buy"|"sell";由动作层在成交后调用。
func (w *World) recordTradeDirection(p *Player, dir string) {
	if p == nil {
		return
	}
	majority := w.marketMajorityDirection()
	switch {
	case majority == MajorityFlat || majority == MajorityHold:
		p.HerdNeutralCount++
	case majority == dir:
		p.HerdFollowCount++
	default:
		p.HerdCounterCount++
		p.HerdCounterMovesThisMonth++
	}
}

// counterTrendReward 逆势奖励(§3.4.3):本回合同向人数 ≥3、本人逆势操作、
// 且本回合净资产上升 ⇒ 认知 +2。返回是否发放。
//
// ⚠ 引擎内**不存在**"普通获利认知 +1"的既有机制(全仓 grep:认知只在
// 学习进修 / 生育 / 教育增值三处增加),因此本模块直接发放 §3.4.3 规定的
// "+2",不存在重复发放问题(见实施报告「登记项」)。
func (w *World) counterTrendReward(p *Player, netBefore, netAfter int64, summary TurnSummary) bool {
	if p == nil || p.HerdCounterMovesThisMonth <= 0 {
		return false
	}
	// 同向人数:按本回合多数方向统计(≥3 人才算有"群体共识")。
	peers := summary.Buy
	if summary.Majority == MajoritySell {
		peers = summary.Sell
	}
	if peers < counterTrendMinPeers {
		return false
	}
	if netAfter <= netBefore {
		return false
	}
	p.Cognition += counterTrendCognitionGain
	if p.Cognition > 10 {
		p.Cognition = 10
	}
	w.emitEvent("settle", p.Seat, fmt.Sprintf(
		"%d 号位逆势操作并获利,认知 +%d(独立思考奖励)", p.Seat, counterTrendCognitionGain))
	return true
}
