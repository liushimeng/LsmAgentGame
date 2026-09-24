// Package virtual_city — society_stats.go: 社会结构指标体系(阶段7,2026-09-21 §城市扩张v2.12)。
//
// 职责:月度 SocietySnapshot(财富/收入基尼 + Top/Bottom 占比 + 财富五等分 +
// 4 层绝对门槛财富金字塔 + 30 岁以下月度社会流动性)与 24 月环形历史。
//
// 与 labor.go::ComputeSociety 的分工(补全而非重写):
//   - labor.go SocietyStats  : 财富基尼(全量)/可支配收入五等分/圈层/分位线/
//                              洛伦兹曲线/3 层圈层金字塔 —— 月度缓存,view 直读;
//   - 本文件 SocietySnapshot : 收入基尼/Top1/Top10/Bottom50/财富五等分/
//                              4 层绝对门槛金字塔/月度流动性 + 历史趋势。
//                              财富基尼在本文件按 R7-1 截尾口径独立计算
//                              (12 人局 n×0.1% 不足 1 人 → 自动退化为全量,
//                              数值与 SocietyStats.Gini 完全一致)。
//
// 接线:settlement.go ④.6B(SettleMonth 内,ComputeSociety 之后)入栈;
// view.go society 子结构下发当前快照 + 最近 12 月趋势。
//
// 确定性:纯排序+算术,零 rand 消费 —— 固定种子存量对局回归零偏移
// (阶段4-6 同款纪律)。并列财富按 seat 升序 tie-break,排名稳定可复现。
package virtual_city

import (
	"math"
	"sort"
)

// 阶段7 常量。
const (
	// societyHistoryCap 社会结构历史容量(环形保留最近 24 月)。
	societyHistoryCap = 24
	// societyTrimFrac R7-1:基尼计算前剔除两端极值的比例(各 0.1%)。
	// 12 人局 n×0.001 = 0.012 不足 1 人 → trimCount=0,自动退化为全量计算
	// (与 labor.go SocietyStats.Gini 口径一致);未来人数扩容后自动生效。
	societyTrimFrac = 0.001
	// societyMobilityAge 流动性统计年龄上限:主时钟 w.Age() < 30 的玩家计入
	// (含 29 岁;30 岁起退出样本,体现"年轻世代向上流动")。
	societyMobilityAge = 30
	// 财富金字塔 4 层绝对门槛(净资产,元):
	//   层0 <10万(生存) / 层1 10万-100万(积累) / 层2 100万-1000万(富裕) /
	//   层3 ≥1000万(富豪)。自下而上。
	pyramidTier1CNY = 100_000
	pyramidTier2CNY = 1_000_000
	pyramidTier3CNY = 10_000_000
)

// SocietySnapshot 月度社会结构快照(阶段7)。
// 只统计 alive 玩家;字段均为月度终值(月结 ④.6B 时点,即 w.Month 递增之前)。
type SocietySnapshot struct {
	Month       int     `json:"month"`        // 主钟月份(结算月)
	GiniWealth  float64 `json:"gini_wealth"`  // 财富基尼(R7-1:剔除 top/bottom 0.1% 后;12 人不足 1 人 → 全量)
	GiniIncome  float64 `json:"gini_income"`  // 收入基尼(当月总收入 Income,升序加权公式)
	Top1Pct     float64 `json:"top1_pct"`     // 前 1% 玩家财富占比(12 人 → ceil(0.12)=1 人)
	Top10Pct    float64 `json:"top10_pct"`    // 前 10% 玩家财富占比(12 人 → ceil(1.2)=2 人)
	Bottom50Pct float64 `json:"bottom50_pct"` // 后 50% 玩家财富占比(12 人 → 6 人)
	Quintiles   [5]float64 `json:"quintiles"` // 财富五等分占比(净资产升序均分 5 组,低→高,和=1;Σ=0 时全 0)
	// Pyramid 财富金字塔各层人数(4 层,自下而上):
	// [0]<10万 / [1]10万-100万 / [2]100万-1000万 / [3]≥1000万。
	Pyramid [4]int `json:"pyramid"`
	// MobilityYoung 月度社会流动性:主时钟 <30 岁玩家中,财富排名较上月
	// 上升(rank 变小)的比例 ∈ [0,1];无有效样本(首月/全员 ≥30 岁)→ 0。
	MobilityYoung float64 `json:"mobility_young"`

	// Ranks 排名缓存(seat → 财富降序排名,1=最富;并列按 seat 升序 tie-break)。
	// 下月快照计算 MobilityYoung 的对比基准;不下发客户端。
	Ranks map[int]int `json:"-"`
}

// SocietyHistory 社会结构环形历史(最近 societyHistoryCap=24 月,时间升序)。
// 挂 World.SocietyHist;settlement ④.6B Push,view 下发最近 12 月趋势。
type SocietyHistory struct {
	Snapshots []SocietySnapshot
}

// Push 追加快照;超容量时保留最近 24 条(copy 到新数组,防底层数组无限增长)。
// nil 接收者安全 no-op(引擎接线处已保证初始化,双保险)。
func (h *SocietyHistory) Push(s SocietySnapshot) {
	if h == nil {
		return
	}
	h.Snapshots = append(h.Snapshots, s)
	if len(h.Snapshots) > societyHistoryCap {
		kept := make([]SocietySnapshot, societyHistoryCap)
		copy(kept, h.Snapshots[len(h.Snapshots)-societyHistoryCap:])
		h.Snapshots = kept
	}
}

// Latest 返回最近一条快照(无历史 → nil;nil 接收者安全)。
func (h *SocietyHistory) Latest() *SocietySnapshot {
	if h == nil || len(h.Snapshots) == 0 {
		return nil
	}
	return &h.Snapshots[len(h.Snapshots)-1]
}

// Last 返回最近 n 条快照(时间升序;不足 n 条 → 全量副本;nil/非正 → nil)。
func (h *SocietyHistory) Last(n int) []SocietySnapshot {
	if h == nil || n <= 0 {
		return nil
	}
	if len(h.Snapshots) <= n {
		out := make([]SocietySnapshot, len(h.Snapshots))
		copy(out, h.Snapshots)
		return out
	}
	out := make([]SocietySnapshot, n)
	copy(out, h.Snapshots[len(h.Snapshots)-n:])
	return out
}

// societyRow 快照中间行(单存活玩家)。
type societyRow struct {
	seat int
	nw   float64 // 净资产(现金+资产-负债,Market 即时估值)
	inc  float64 // 当月总收入(Monthly.Income)
}

// ComputeSocietySnapshot 遍历存活玩家计算月度社会结构快照(纯函数,零 rand)。
//
// 算法(2026-09-21 §城市扩张v2.12 阶段7):
//  1. 排序净资产(降序,并列按 seat 升序)→ Ranks 排名缓存;
//  2. 财富基尼:升序 + R7-1 截尾(两端各 floor(n×0.1%)人;不足 1 人 → 全量),
//     加权公式 G = 2Σi·x_i/(n·Σx_i) − (n+1)/n,clamp [0,1](负净资产防越界);
//  3. 收入基尼:当月 Income 升序同公式;
//  4. Top1/Top10/Bottom50:降序前 ceil(n×1%)/ceil(n×10%) 人与升序后 n/2 人
//     财富占比(小样本向上取整保证 Top1 ≥1 人、Top10 ≥2 人有区分度);
//  5. 财富五等分:升序均分 5 组(末组吃余数)占比,与 labor.go 收入五等份同构;
//  6. 金字塔 4 层:绝对门槛 10万/100万/1000万 计数;
//  7. 流动性:主时钟 <30 岁且上月快照有排名记录的玩家中,排名上升比例。
func ComputeSocietySnapshot(w *World) SocietySnapshot {
	if w == nil {
		return SocietySnapshot{}
	}
	snap := SocietySnapshot{Month: w.Month, Ranks: map[int]int{}}

	// 收集存活玩家行。
	rows := make([]societyRow, 0, len(w.Players))
	for seat, p := range w.Players {
		if p == nil || !p.Alive {
			continue
		}
		rows = append(rows, societyRow{
			seat: seat,
			nw:   float64(p.NetWorth(w.Market)),
			inc:  float64(p.Monthly.Income),
		})
	}
	n := len(rows)
	if n == 0 {
		return snap
	}

	// 1. 排名(降序;并列按 seat 升序 tie-break —— 零 rand 下确定性稳定)。
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].nw != rows[j].nw {
			return rows[i].nw > rows[j].nw
		}
		return rows[i].seat < rows[j].seat
	})
	for i, r := range rows {
		snap.Ranks[r.seat] = i + 1
	}
	// asc:同一批净资产升序(rows 降序的反转;后续基尼/五等分/Bottom50 用)。
	asc := make([]float64, n)
	for i, r := range rows {
		asc[n-1-i] = r.nw
	}
	var total float64
	for _, v := range asc {
		total += v
	}

	// 2. 财富基尼(R7-1 截尾:两端各 trim 人;12 人 → 0 → 全量)。
	trim := int(float64(n) * societyTrimFrac)
	snap.GiniWealth = clamp01(giniCoefficient(asc[trim : n-trim]))

	// 3. 收入基尼(全量;ΣIncome ≤ 0 → 0)。
	incs := make([]float64, n)
	for i, r := range rows {
		incs[i] = r.inc
	}
	sort.Float64s(incs)
	snap.GiniIncome = clamp01(giniCoefficient(incs))

	// 4. Top/Bottom 占比(总财富 ≤ 0 → 全 0 兜底,极端负债场景)。
	if total > 0 {
		top1 := max(1, int(math.Ceil(float64(n)*0.01)))
		top10 := max(top1, int(math.Ceil(float64(n)*0.10)))
		bot := n / 2
		var s1, s10, sb float64
		for i := 0; i < top1 && i < n; i++ {
			s1 += rows[i].nw // rows 降序 → 前 k 人即最富 k 人
		}
		for i := 0; i < top10 && i < n; i++ {
			s10 += rows[i].nw
		}
		for i := 0; i < bot && i < n; i++ {
			sb += asc[i] // asc 升序头部 bot 人即最穷一半
		}
		snap.Top1Pct = clamp01(s1 / total)
		snap.Top10Pct = clamp01(s10 / total)
		snap.Bottom50Pct = clamp01(sb / total)
	}

	// 5. 财富五等分(升序均分 5 组,末组吃余数;与 labor.go 收入五等份同构)。
	for i := range snap.Quintiles {
		snap.Quintiles[i] = 0.2
	}
	if n >= 5 && total > 0 {
		for q := 0; q < 5; q++ {
			lo := q * n / 5
			hi := (q + 1) * n / 5
			if q == 4 {
				hi = n
			}
			var group float64
			for _, v := range asc[lo:hi] {
				group += v
			}
			snap.Quintiles[q] = clamp01(group / total)
		}
	}

	// 6. 财富金字塔 4 层(绝对门槛计数,自下而上)。
	for _, r := range rows {
		switch {
		case r.nw >= pyramidTier3CNY:
			snap.Pyramid[3]++
		case r.nw >= pyramidTier2CNY:
			snap.Pyramid[2]++
		case r.nw >= pyramidTier1CNY:
			snap.Pyramid[1]++
		default:
			snap.Pyramid[0]++
		}
	}

	// 7. 月度流动性:与上月快照排名对比(主时钟 <30 岁样本)。
	// 年龄用 w.Age() 主时钟(与 settlePlayer/结算口径一致;混龄卡个人 age
	// 仅展示)。全员 ≥30 岁/首月无对比 → 保持 0。
	if prev := w.SocietyHist.Latest(); prev != nil && len(prev.Ranks) > 0 &&
		w.Age() < societyMobilityAge {
		young, risen := 0, 0
		for seat, curRank := range snap.Ranks { // map 迭代序无关(纯计数)
			prevRank, ok := prev.Ranks[seat]
			if !ok {
				continue // 上月不在场(中途入座/破产出局后复活等)不计入
			}
			young++
			if curRank < prevRank {
				risen++
			}
		}
		if young > 0 {
			snap.MobilityYoung = clamp01(float64(risen) / float64(young))
		}
	}
	return snap
}

// giniCoefficient 升序样本的基尼系数(加权公式,labor.go::ComputeSociety
// 同款;独立成函数供财富/收入两处复用,不改 labor.go 已有逻辑):
//
//	G = 2Σ(i·x_i)/(n·Σx_i) − (n+1)/n   (x 升序,i 从 1 起)
//
// n<2 或 Σ≤0 → 0;负净资产样本结果可能越界,调用方 clamp01。
func giniCoefficient(sortedAsc []float64) float64 {
	n := len(sortedAsc)
	if n < 2 {
		return 0
	}
	var sum, weighted float64
	for i, v := range sortedAsc {
		sum += v
		weighted += float64(i+1) * v
	}
	if sum <= 0 {
		return 0
	}
	return 2*weighted/(float64(n)*sum) - float64(n+1)/float64(n)
}

// clamp01 夹取 [0,1](占比/基尼防越界;goods.go::clampF 的 [0,1] 特化)。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
