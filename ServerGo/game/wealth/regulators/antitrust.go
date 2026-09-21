// Package regulators — antitrust.go: 反垄断监管(2026-09-21 §城市扩张v2.12 阶段8)。
//
// 职责:单"企业"(supply_chain.go 产业节点代理)产能市占 > 40% 连续 3 月
// → 立案调查期满,执行**分拆**:Capacity 减半(一次性、结构性,不回滚;
// 已分拆节点不再二次分拆,防 40% 线反复触发无限减半)。
//
// 解耦约束:本包不 import wealth 主包 —— 产能数据经 CapacitySource 接口
// 注入,由 wealth 主包(public_services.go::supplyCapacityView)适配
// SupplyChain.Nodes[*].Capacity(万元)。
//
// 现状标定(阶段5 15 节点):最大节点 food_wholesale 产能占比 ≈ 23%
// < 40% —— 常态对局本监管为**休眠护栏**(只有外部冲击改写 Capacity 才触发),
// 与 R8-3"监管不干扰正常市场"的取向一致。
//
// 纯引擎层:无锁、无 IO、零 rand;节点遍历序由调用方保证(nodeIDs 升序)。
package regulators

// 反垄断常量(阶段8 新定)。
const (
	// AntitrustShareTrigger 市占率触发线(单节点产能 ÷ 全城总产能)。
	AntitrustShareTrigger = 0.40
	// AntitrustConsecutiveMonths 连续超标月数(调查期;连续 N 月 > 40% 才分拆)。
	AntitrustConsecutiveMonths = 3
	// AntitrustSplitFactor 分拆系数:Capacity × 0.5。
	AntitrustSplitFactor = 0.5
)

// CapacitySource 产能数据源(wealth::SupplyChain 适配;万元口径)。
// SetCapacity 仅在分拆裁决时被本监管调用(减半写回)。
type CapacitySource interface {
	CapacityOf(id string) float64
	TotalCapacity() float64
	SetCapacity(id string, capacity float64)
}

// SplitAction 单次分拆裁决(wealth 侧据此 emit 事件;写回已在 MonthlyStep 内完成)。
type SplitAction struct {
	NodeID      string  `json:"node_id"`
	Month       int     `json:"month"`
	OldCapacity float64 `json:"old_capacity"` // 万元
	NewCapacity float64 `json:"new_capacity"` // 万元
	MarketShare float64 `json:"market_share"` // 分拆前市占(0-1)
}

// AntitrustRegulator 反垄断监管状态(挂 wealth.RegulatorBundle.Antitrust)。
type AntitrustRegulator struct {
	Investigations int    // 累计立案数(首次进入"连续超标"计数即 +1)
	Splits         int    // 累计分拆执行次数
	LastSplitNode  string // 最近一次分拆节点 id(快照展示;空 = 从未)

	// watch 节点级调查状态(nodeID → 连续超标月数/是否已分拆)。
	watch map[string]*antitrustWatch
	// LastStepMonth 最近一次 MonthlyStep 的月份(§130 接线验证)。
	LastStepMonth int
}

// antitrustWatch 单节点调查状态。
type antitrustWatch struct {
	excessRun int  // 连续市占超标月数(断档清零)
	notified  bool // 是否已因"开始连续超标"计过立案(notified 仅一次性)
	splitDone bool // 已执行分拆(结构性,不再二次分拆)
}

// NewAntitrustRegulator 构造。
func NewAntitrustRegulator() *AntitrustRegulator {
	return &AntitrustRegulator{watch: map[string]*antitrustWatch{}}
}

// MonthlyStep 月度市占检查:遍历 nodeIDs(调用方按 id 升序传入,保证确定性),
// 市占 = CapacityOf(id) ÷ TotalCapacity()。连续 AntitrustConsecutiveMonths 月
// > AntitrustShareTrigger 且未分拆过 → SetCapacity(id, cap×0.5) 并返回
// SplitAction(同月多节点可并行分拆;返回序 = nodeIDs 序)。
// TotalCapacity ≤ 0 或 CapacityOf ≤ 0 时跳过该节点(防除零)。
func (a *AntitrustRegulator) MonthlyStep(month int, src CapacitySource, nodeIDs []string) []SplitAction {
	if a == nil || src == nil {
		return nil
	}
	a.LastStepMonth = month
	var actions []SplitAction
	for _, id := range nodeIDs {
		cap := src.CapacityOf(id)
		total := src.TotalCapacity()
		if cap <= 0 || total <= 0 {
			continue
		}
		st := a.watch[id]
		if st == nil {
			st = &antitrustWatch{}
			a.watch[id] = st
		}
		if cap/total > AntitrustShareTrigger {
			st.excessRun++
			if st.excessRun == 1 && !st.notified {
				a.Investigations++
				st.notified = true
			}
			if st.excessRun >= AntitrustConsecutiveMonths && !st.splitDone {
				newCap := cap * AntitrustSplitFactor
				src.SetCapacity(id, newCap)
				st.splitDone = true
				st.excessRun = 0
				a.Splits++
				a.LastSplitNode = id
				actions = append(actions, SplitAction{
					NodeID: id, Month: month,
					OldCapacity: cap, NewCapacity: newCap,
					MarketShare: cap / total,
				})
			}
		} else {
			st.excessRun = 0
		}
	}
	return actions
}
