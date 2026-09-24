// Package wealth — side_market.go: 副业竞争比价与定价战(批次20 文档2)。
//
// 机制(上游:《虚拟城市交易拍卖与企业经营设计》§8.3/§8.4,FCM 定价战极简版):
// 每位居民的副业新增定价档位;同品类多名经营者按客群份额切分市场收入:
//
//	档位 t ∈ {低价 1, 中价 0(默认), 高价 2}
//	客群权重 w(t) = {低 0.50, 中 0.30, 高 0.20}(低价档覆盖最大客群)
//	收入乘数 m(t) = {低 0.75, 中 1.00, 高 1.25}(§8.3 收入区间中值比取整)
//	份额 s_i = w(t_i) / Σ_{j∈O_k} w(t_j)(O_k = 同品类存活经营者;独占 = 1.0)
//	月副业收入 = round(Base × m × s × BrassSide × variance)(variance 既有
//	rand 消耗形态不变 → 全员默认中价 + 无竞争时与批次20 前逐分不差)。
//
// 囚徒困境锚点(n=2 双中各 0.50,单低 0.469 vs 守中 0.375,双低各 0.375;
// 高价利基:对手中价时切高 → 份额 0.2/0.5=0.25×1.25=0.3125 < 守中 0.5)。
// 本文件全部纯函数:零 rand、零副作用、份额/门槛/文案可单测。
package wealth

import (
	"fmt"
	"sort"

	"LsmAgentGame/errcode"
)

// 定价档位常量(文档2 §2;0 = 中价,兼容旧档零值 = 旧行为)。
const (
	PriceTierMid  = 0 // 中价(默认)
	PriceTierLow  = 1 // 低价
	PriceTierHigh = 2 // 高价
)

// sideTierWeight 档位客群权重(§8.4 50/30/20)。
func sideTierWeight(tier int) float64 {
	switch tier {
	case PriceTierLow:
		return 0.50
	case PriceTierHigh:
		return 0.20
	default: // PriceTierMid;非法值入口已拒绝,此处按中价兜底
		return 0.30
	}
}

// sideTierMultiplier 档位收入乘数(§8.3 收入区间中值比取整)。
func sideTierMultiplier(tier int) float64 {
	switch tier {
	case PriceTierLow:
		return 0.75
	case PriceTierHigh:
		return 1.25
	default:
		return 1.00
	}
}

// sideTierCN 档位中文名(事件/结算文案/view 共用)。
func sideTierCN(tier int) string {
	switch tier {
	case PriceTierLow:
		return "低价"
	case PriceTierHigh:
		return "高价"
	default:
		return "中价"
	}
}

// sideTierValid 档位入参合法性(set_side_price / start_side_business 共用)。
func sideTierValid(tier int) bool {
	return tier >= PriceTierMid && tier <= PriceTierHigh
}

// sideMarketShares 同品类客群份额:kind → seat → s_i(文档2 §1 份额公式)。
// 经营者集合 = Alive 且 SideBusiness.Kind == k 的座位;k 独占 → s=1.0
// (无竞争不惩罚)。仅返回有经营者的品类;seat 输出序由调用方按升序遍历。
// 纯函数零副作用零 rand;全员默认中价 + 每品类 ≤1 经营者时全部为 1.0。
func sideMarketShares(w *World) map[string]map[int]float64 {
	type operator struct {
		seat int
		weight float64
	}
	byKind := map[string][]operator{}
	for _, p := range w.Players {
		if p == nil || !p.Alive || p.SideBusiness == nil {
			continue
		}
		k := p.SideBusiness.Kind
		byKind[k] = append(byKind[k], operator{seat: p.Seat, weight: sideTierWeight(p.SideBusiness.PriceTier)})
	}
	out := make(map[string]map[int]float64, len(byKind))
	for k, ops := range byKind {
		sum := 0.0
		for _, o := range ops {
			sum += o.weight
		}
		if sum <= 0 {
			continue
		}
		shares := make(map[int]float64, len(ops))
		for _, o := range ops {
			shares[o.seat] = o.weight / sum
		}
		out[k] = shares
	}
	return out
}

// sideOperatorSeats 某品类经营者座位升序列表(view 下发确定性排序用)。
func sideOperatorSeats(shares map[string]map[int]float64, kind string) []int {
	out := make([]int, 0, len(shares[kind]))
	for s := range shares[kind] {
		out = append(out, s)
	}
	sort.Ints(out)
	return out
}

// sideTierGate 档位品类门槛(文档2 §1 规则表;全部确定性):
//   - tutoring / freelance:高价要求 Cognition ≥ def.GateCognition+1,否则拒绝;
//   - delivery(高价 → 月结额外精力 −1)、content(高价且认知 <3 → 收入 ×0.5)
//     不设动作门槛(效果在 settlement 内生效,不拒绝定价);
//   - 低价/中价全品类无门槛。
func sideTierGate(p *Player, def sideBizDef, tier int) *errcode.Error {
	if tier != PriceTierHigh {
		return nil
	}
	switch def.Kind {
	case "tutoring", "freelance":
		if p.Cognition < def.GateCognition+1 {
			return errcode.CodeMsg(errcode.ErrWealthGateFailed,
				fmt.Sprintf("%s高价档需认知 ≥ %d(小众溢价的前提是专业度)",
					sideBizCN(def.Kind), def.GateCognition+1))
		}
	}
	return nil
}

// sidePriceTextSuffix 结算文案定价括号(文档2 §3:「副业收入(高价×125%·份额62%)」)。
// 中价 + 独占(份额 ≥1.0)返回空串 —— 与批次20 前旧文案逐字一致(回归零偏移)。
func sidePriceTextSuffix(tier int, share float64) string {
	if tier == PriceTierMid && share >= 1.0 {
		return ""
	}
	return fmt.Sprintf("(%s×%d%%·份额%d%%)", sideTierCN(tier),
		int(sideTierMultiplier(tier)*100+0.5), int(share*100+0.5))
}

// stepSideMarketEvents 品类首次出现 2+ 经营者 → 播报价格竞争
// (文档2 §3;每月每品类 ≤1 条,竞争持续期不重复播报,断档后再现会重新播报)。
// 事件不影响任何金额/.rand 消耗 —— 旧 seed 对局结算金额零偏移。
func (w *World) stepSideMarketEvents(shares map[string]map[int]float64) {
	if w.Market == nil {
		return
	}
	if w.Market.SideCompetitionTracked == nil {
		w.Market.SideCompetitionTracked = map[string]bool{}
	}
	if w.Market.SideCompetitionEventMonth == nil {
		w.Market.SideCompetitionEventMonth = map[string]int{}
	}
	// 品类序确定性:按 kind 升序遍历。
	kinds := make([]string, 0, len(shares))
	for k := range shares {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		competing := len(shares[k]) >= 2
		was := w.Market.SideCompetitionTracked[k]
		if competing && !was && w.Market.SideCompetitionEventMonth[k] != w.Month {
			w.Market.SideCompetitionEventMonth[k] = w.Month
			w.emitEvent("market", -1, fmt.Sprintf("%s市场出现价格竞争:%d 名经营者", sideBizCN(k), len(shares[k])))
		}
		w.Market.SideCompetitionTracked[k] = competing
	}
}
