// Package wealth — civic_election.go: 市长选举(R8-2 完全可选,默认关闭)
// 2026-09-21 §城市扩张v2.12 阶段8。
//
// 得票模型(30/40/30):
//
//	综合得分 = 财富排名分×30% + 人脉 Network×10×40% + 社会满意度×30%
//	  - 财富排名分:存活玩家净资产降序排名归一(第 1 名 100 分,末名 0 分;
//	    并列按座位升序 tie-break;n=1 → 100);
//	  - 人脉分:Network(0..10)×10;
//	  - 社会满意度 = 100 − 基尼×100(基尼低 = 满意度高;基尼取
//	    World.Society.Gini —— settlement ④.6 已在同月更早时点算好;
//	    Society 为 nil(未开 economy)→ 基尼 0 → 满意度 100)。
//
// 当选者特权:每月 500 元津贴,走 Treasury 转移支付通道(gov:treasury →
// seat,CatWelfare —— Ledger 白名单唯一合法国库→座位 category;
// 国库现金不足 → 当月不发,不透支,R4-1 同款纪律)。
//
// 确定性:纯排序 + 算术,零 rand;并列得分按座位升序 tie-break。
// Enabled=false(默认)时 MonthlyStep 完全 no-op —— 固定种子存量对局
// 回归零偏移(§R8-2"完全可选"的硬约束)。
package wealth

import (
	"fmt"
	"sort"
)

// 市长选举常量(阶段8 新定)。
const (
	// ElectionIntervalMonths 选举间隔(48 月 = 4 年一届)。
	ElectionIntervalMonths = 48
	// MayorStipendCNY 市长月津贴(元;走 Treasury 转移支付通道)。
	MayorStipendCNY int64 = 500
	// 得票权重(和 = 1.0;TestCivicElection_VoteWeights 锁定)。
	VoteWeightWealth       = 0.30
	VoteWeightNetwork      = 0.40
	VoteWeightSatisfaction = 0.30
)

// ElectionVote 单座位得票明细(快照下发 + 测试锁定 30/40/30 权重)。
type ElectionVote struct {
	Seat         int     `json:"seat"`
	Score        float64 `json:"score"`         // 综合得分 0..100
	WealthScore  float64 `json:"wealth_score"`  // 财富排名分 0..100
	NetworkScore float64 `json:"network_score"` // 人脉分 0..100
	Satisfaction float64 `json:"satisfaction"`  // 社会满意度 0..100(全城同值)
}

// CivicElection 市长选举状态(挂 World.Election;阶段8)。
type CivicElection struct {
	Enabled           bool // 默认 false(R8-2 完全可选)
	IntervalMonths    int  // 48(4 年一届);≤0 回落 ElectionIntervalMonths
	LastElectionMonth int  // 最近一次选举的主钟月份(0 = 从未)
	MayorSeat         int  // 当选者座位(-1 = 无市长)
	MonthsRun         int  // Enabled 期间累计月步次数(§130 接线验证)
	LastStepMonth     int  // 最近一次 MonthlyStep 的主钟月份
	LastVotes         []ElectionVote // 最近一次得票明细(得分降序;view 下发)
	// StipendStopped 上月津贴是否断发(批次20 文档3 A3:「市长津贴停发」
	// event 只在 停→停 转换沿播一次,恢复发放后重新计)。
	StipendStopped bool
}

// NewCivicElection 构造:默认关闭、48 月一届、无市长。
func NewCivicElection() *CivicElection {
	return &CivicElection{
		Enabled:        false,
		IntervalMonths: ElectionIntervalMonths,
		MayorSeat:      -1,
	}
}

// MonthlyStep 选举月步(settlement ⑨G;Enabled=false 完全 no-op):
//   - 市长在任校验:座位空/死亡 → 卸任(MayorSeat=-1);
//   - 任期届满(w.Month − LastElectionMonth ≥ IntervalMonths)→ RunElection;
//   - 津贴:在任市长每月 500 元,Treasury 现金充足才发(不足跳过,不透支)。
//
// 顺序:卸任校验 → 选举 → 津贴(新市长当选当月即领首月津贴)。
func (ce *CivicElection) MonthlyStep(w *World) {
	if ce == nil || w == nil || !ce.Enabled {
		return // R8-2:默认关闭,零副作用(不动现金/不动事件/不动状态)
	}
	ce.MonthsRun++
	ce.LastStepMonth = w.Month

	// ① 在任校验(死亡/离座卸任)。
	if ce.MayorSeat >= 0 {
		p := w.Players[ce.MayorSeat]
		if p == nil || !p.Alive {
			w.emitEvent("policy", ce.MayorSeat, fmt.Sprintf("市长(%d 号位)离任,职位空缺", ce.MayorSeat))
			ce.MayorSeat = -1
		}
	}

	// ② 任期届满选举。
	interval := ce.IntervalMonths
	if interval <= 0 {
		interval = ElectionIntervalMonths
	}
	if w.Month-ce.LastElectionMonth >= interval {
		ce.RunElection(w)
	}

	// ③ 市长津贴(gov:treasury → seat,CatWelfare;国库不足不发)。
	// 批次20 文档3 A3:津贴月度不单独播报(Ledger 可查);**断发**(国库
	// 不足)播报一条 event,恢复发放后允许再次播报(StipendStopped 转换沿)。
	if ce.MayorSeat >= 0 {
		if w.Treasury != nil && w.Treasury.Cash >= MayorStipendCNY {
			w.Pay(ce.MayorSeat, EntityGovernment, SeatEntity(ce.MayorSeat),
				MayorStipendCNY, CatWelfare, "市长津贴")
			w.Treasury.Cash -= MayorStipendCNY
			ce.StipendStopped = false
		} else if !ce.StipendStopped {
			ce.StipendStopped = true
			w.emitEvent("policy", ce.MayorSeat, "市长津贴停发(国库不足)")
		}
	}
}

// NextElectionMonth 下届选举主钟月 = LastElectionMonth + Interval(从未选举
// → Interval;批次20 文档3 A3,view 下发 next_election_month)。
// Enabled=false 返回 0(view 字段 omitempty 不下发 —— 未启用无「下届」)。
func (ce *CivicElection) NextElectionMonth() int {
	if ce == nil || !ce.Enabled {
		return 0
	}
	interval := ce.IntervalMonths
	if interval <= 0 {
		interval = ElectionIntervalMonths
	}
	return ce.LastElectionMonth + interval
}

// RunElection 举行一次选举:ComputeVotes 得分降序取首名(并列按座位升序,
// ComputeVotes 内部已排);无存活玩家 → 空缺。记录 LastElectionMonth 与
// LastVotes,并播报当选事件。返回当选座位(-1 = 空缺)。
func (ce *CivicElection) RunElection(w *World) int {
	if ce == nil || w == nil {
		return -1
	}
	ce.LastElectionMonth = w.Month
	votes := ce.ComputeVotes(w)
	ce.LastVotes = votes
	if len(votes) == 0 {
		ce.MayorSeat = -1
		w.emitEvent("policy", -1, "市长选举:无有效候选人,职位空缺")
		return -1
	}
	winner := votes[0]
	ce.MayorSeat = winner.Seat
	w.emitEvent("policy", winner.Seat, fmt.Sprintf(
		"市长选举:%d 号位当选市长(综合 %.1f = 财富 %.1f×30%% + 人脉 %.1f×40%% + 满意度 %.1f×30%%)",
		winner.Seat, winner.Score, winner.WealthScore, winner.NetworkScore, winner.Satisfaction))
	return winner.Seat
}

// ComputeVotes 计算全部存活玩家得票(纯函数,零 rand):
// 排名口径 = 净资产降序、并列座位升序;输出按综合得分降序、并列座位升序。
func (ce *CivicElection) ComputeVotes(w *World) []ElectionVote {
	if w == nil {
		return nil
	}
	type row struct {
		seat int
		nw   float64
		net  int
	}
	var rows []row
	for _, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		rows = append(rows, row{seat: p.Seat, nw: float64(p.NetWorth(w.Market)), net: p.Network})
	}
	n := len(rows)
	if n == 0 {
		return nil
	}
	// 财富排名(降序,并列座位升序 —— 确定性 tie-break)。
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].nw != rows[j].nw {
			return rows[i].nw > rows[j].nw
		}
		return rows[i].seat < rows[j].seat
	})
	// 社会满意度(全城同值):100 − 基尼×100。
	gini := 0.0
	if w.Society != nil {
		gini = clamp01(w.Society.Gini)
	}
	satisfaction := 100 - gini*100

	votes := make([]ElectionVote, 0, n)
	for rank, r := range rows {
		wealthScore := 100.0
		if n > 1 {
			wealthScore = float64(n-1-rank) / float64(n-1) * 100
		}
		net := r.net
		if net < 0 {
			net = 0
		}
		if net > 10 {
			net = 10
		}
		v := ElectionVote{
			Seat:         r.seat,
			WealthScore:  wealthScore,
			NetworkScore: float64(net) * 10,
			Satisfaction: satisfaction,
		}
		v.Score = VoteWeightWealth*v.WealthScore +
			VoteWeightNetwork*v.NetworkScore +
			VoteWeightSatisfaction*v.Satisfaction
		votes = append(votes, v)
	}
	// 输出排序:得分降序,并列座位升序。
	sort.SliceStable(votes, func(i, j int) bool {
		if votes[i].Score != votes[j].Score {
			return votes[i].Score > votes[j].Score
		}
		return votes[i].Seat < votes[j].Seat
	})
	return votes
}
