// Package wealth — distribution.go: 资金流向聚合引擎(P2 v2 §13.2.2)。
// 2026-09-19 §财商流P2 v2。
//
// 契约: lag_docs/财商流游戏/已实现/07-P2财富可视化/财商流游戏-P2-财富流动可视化仪表盘-v1.md §13.2。
//
// 设计要点:
//   - 固定节点(salary/market/bank/gov/firms/player 6 个);玩家间互转跳过(走 P2-1 挂单簿)
//   - 聚合扫描本月 Ledger,按 category → (from, to) 映射累加;
//   - 月结末尾 (SettleMonth ⑨ 之后) 调用 RecordFlowStat 刷新 World.LastFlowStat;
//   - 纯函数 ComputeFlowStat(world, month) 也可单独调用(单测/复盘)。
package wealth

import "fmt"

// 资金流向节点 ID(§13.2.3 映射表的"主体";固定集合不引入新节点)。
const (
	FlowNodeSalary = "salary" // 工资来源(从 bank 出来到玩家)
	FlowNodeFirms  = "firms"  // 企业部门(消费接收方)
	FlowNodeMarket = "market" // 资产/租金/分红
	FlowNodeBank   = "bank"   // 银行(贷款/还款)
	FlowNodeGov    = "gov"    // 政府(税收/养老金)
	FlowNodePlayer = "player" // 玩家聚合(所有 seat:N 合计)
	FlowNodeWorld  = "world"  // 系统外注入
)

// FlowNode 节点(2026-09-19 §P2 v2 §13.2.2)。
type FlowNode struct {
	ID        string `json:"id"`         // 节点 ID
	Label     string `json:"label"`      // i18n key(由前端映射)
	Kind      string `json:"kind"`       // "source" 注入 / "sink" 接收 / "pass" 中转
	AmountCNY int64  `json:"amount_cny"` // 本月流量(绝对值,节点层级 sum)
}

// FlowLink 边(2026-09-19 §P2 v2 §13.2.2)。
type FlowLink struct {
	From      string  `json:"from"`       // 节点 ID
	To        string  `json:"to"`         // 节点 ID
	AmountCNY int64    `json:"amount_cny"` // 流量(恒正)
	Pct       float64 `json:"pct"`        // 占总流量比(最大边为基准)
}

// FlowStat 月度资金流向统计(2026-09-19 §P2 v2 §13.2.2)。
type FlowStat struct {
	Period      string     `json:"period"`       // "M<month>"
	PeriodLabel string     `json:"period_label"` // "第 42 月"
	Nodes       []FlowNode `json:"nodes"`
	Links       []FlowLink `json:"links"`
	TotalInCNY  int64      `json:"total_in_cny"`  // 流入玩家合计(所有 to=seat:*)
	TotalOutCNY int64      `json:"total_out_cny"` // 流出玩家合计(所有 from=seat:*)
}

// flowCategoryMap category → (源节点, 目标节点, link kind)。
// 仅列玩家↔系统的边;玩家间 trade/p2p_* 跳过(P2-1 挂单簿单独可视化)。
var flowCategoryMap = map[string]struct {
	from, to string
}{
	CatSalary:       {FlowNodeBank, FlowNodePlayer},   // bank 工资代发 → player
	CatSpouse:       {FlowNodeBank, FlowNodePlayer},   // 同上(配偶)
	CatSide:         {FlowNodeBank, FlowNodePlayer},   // 副业收入
	CatLiving:       {FlowNodePlayer, FlowNodeFirms},  // 消费 → 企业
	CatConsume:      {FlowNodePlayer, FlowNodeFirms},
	CatRentPay:      {FlowNodePlayer, FlowNodeFirms},
	CatProperty:     {FlowNodePlayer, FlowNodeFirms},
	CatStudy:        {FlowNodePlayer, FlowNodeFirms},
	CatSocialEv:     {FlowNodePlayer, FlowNodeFirms},
	CatMoving:       {FlowNodePlayer, FlowNodeFirms},
	CatMedical:      {FlowNodePlayer, FlowNodeFirms},
	CatWedding:      {FlowNodePlayer, FlowNodeFirms},
	CatTax:          {FlowNodePlayer, FlowNodeGov},    // 税收
	CatSocial:       {FlowNodePlayer, FlowNodeGov},    // 社保
	CatPension:      {FlowNodeGov, FlowNodePlayer},    // 养老金
	CatMortgage:     {FlowNodePlayer, FlowNodeBank},   // 房贷还款
	CatInterest:     {FlowNodePlayer, FlowNodeBank},   // 利息
	CatPrincipal:    {FlowNodePlayer, FlowNodeBank},   // 还本
	CatRepay:        {FlowNodePlayer, FlowNodeBank},   // 还款
	CatLoan:         {FlowNodeBank, FlowNodePlayer},   // 贷款放出
	CatBuy:          {FlowNodePlayer, FlowNodeMarket}, // 买入资产
	CatSell:         {FlowNodeMarket, FlowNodePlayer}, // 卖出资产
	CatRent:         {FlowNodeMarket, FlowNodePlayer}, // 房租收入
	CatBondInterest: {FlowNodeMarket, FlowNodePlayer}, // 债券利息/股息
	CatFee:          {FlowNodePlayer, FlowNodeMarket}, // 手续费
	CatInject:       {FlowNodeWorld, FlowNodePlayer},  // 系统注入
	// P2-1 玩家间互转跳过:CatTrade / CatTradeFee / CatP2PInterest / CatP2PRepay /
	// CatAuctionFee / CatInfoTrade / CatNegotiateFee → 不进入聚合。
	// CatDonate / CatOvertime → 暂不进聚合(P0 极小流量;留 v3 补)
}

// flowNodeKind 节点种类映射(用于前端配色)。
var flowNodeKind = map[string]string{
	FlowNodeSalary: "source",
	FlowNodeFirms:  "sink",
	FlowNodeMarket: "pass",
	FlowNodeBank:   "pass",
	FlowNodeGov:    "sink",
	FlowNodePlayer: "pass",
	FlowNodeWorld:  "source",
}

// ComputeFlowStat 计算指定月份资金流向(纯函数,§13.2.3 算法)。
// 跳过 P2-1 玩家间交易类目;只画宏观系统↔玩家流向。
func ComputeFlowStat(w *World, month int) *FlowStat {
	if w == nil {
		return &FlowStat{}
	}
	fs := &FlowStat{
		Period:      fmt.Sprintf("M%d", month),
		PeriodLabel: fmt.Sprintf("第 %d 月", month),
	}
	if w.Ledger == nil {
		return fs
	}
	// 边聚合:key = from+"|"+to
	type edgeKey struct{ from, to string }
	edgeSum := map[edgeKey]int64{}

	for _, e := range w.Ledger.Entries {
		if month > 0 && e.Month != month {
			continue
		}
		// 跳过玩家间互转(seat:* ↔ seat:*)
		_, fromSeat := IsSeatEntity(e.From)
		_, toSeat := IsSeatEntity(e.To)
		if fromSeat && toSeat {
			continue
		}
		mapping, ok := flowCategoryMap[e.Category]
		if !ok {
			continue
		}
		// 把 seat:* 替换为 "player"(聚合节点)
		fromID := mapping.from
		toID := mapping.to
		if _, isSeat := IsSeatEntity(e.From); isSeat {
			fromID = FlowNodePlayer
		}
		if _, isSeat := IsSeatEntity(e.To); isSeat {
			toID = FlowNodePlayer
		}
		key := edgeKey{from: fromID, to: toID}
		edgeSum[key] += e.AmountCNY
		// 玩家流入/流出合计
		if toID == FlowNodePlayer {
			fs.TotalInCNY += e.AmountCNY
		}
		if fromID == FlowNodePlayer {
			fs.TotalOutCNY += e.AmountCNY
		}
	}

	// 边 → FlowLink(计算 Pct,基准 = 最大边)
	var maxEdge int64
	for _, v := range edgeSum {
		if v > maxEdge {
			maxEdge = v
		}
	}
	if maxEdge == 0 {
		return fs // 本月无流水;空态兜底
	}
	for k, v := range edgeSum {
		if v == 0 {
			continue
		}
		fs.Links = append(fs.Links, FlowLink{
			From: k.from, To: k.to, AmountCNY: v,
			Pct: float64(v) / float64(maxEdge),
		})
	}

	// 节点:从所有出现的 (from, to) 收集
	nodeSeen := map[string]int64{}
	for k, v := range edgeSum {
		if v == 0 {
			continue
		}
		nodeSeen[k.from] += v
		nodeSeen[k.to] += v
	}
	// 固定顺序(避免随机顺序影响前端 diff)
	order := []string{FlowNodeSalary, FlowNodeBank, FlowNodeMarket, FlowNodeWorld, FlowNodePlayer, FlowNodeFirms, FlowNodeGov}
	for _, id := range order {
		amt, ok := nodeSeen[id]
		if !ok {
			continue
		}
		fs.Nodes = append(fs.Nodes, FlowNode{
			ID:        id,
			Label:     "wealth.dashboard.flow.node." + id,
			Kind:      flowNodeKind[id],
			AmountCNY: amt,
		})
	}
	return fs
}

// RecordFlowStat 在月结末尾刷新 World.LastFlowStat(§13.2.2 缓存机制)。
// 调用时机:SettleMonth ⑨ 之后,广播前。EconomyEnabled=false 也允许(P0 路径)。
func (w *World) RecordFlowStat() *FlowStat {
	if w == nil {
		return nil
	}
	fs := ComputeFlowStat(w, w.Month)
	w.LastFlowStat = fs
	return fs
}