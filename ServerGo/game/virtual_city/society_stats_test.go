// Package virtual_city — society_stats_test.go: 社会结构指标体系单测
// (阶段7,2026-09-21 §城市扩张v2.12)。
//
// 覆盖:基尼边界(完全平等/一人独占)/五等分守恒/Top-Bottom 单调/金字塔
// 4 层分类/月度流动性/settlement+view 接线(§130)/同种子确定性。
package virtual_city

import (
	"math"
	"reflect"
	"testing"
)

// newSocietyWorld 构造 n 名存活玩家、按 cashFn 设定现金的测试世界
// (SalaryBase 置 0 排除工资噪声;无资产/负债 → NetWorth == Cash)。
func newSocietyWorld(seed int64, n int, cashFn func(seat int) int64) *World {
	w := NewWorld(seed, emptyCardsFor(n))
	for s := 0; s < n; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 0))
		w.Players[s].Cash = cashFn(s)
		w.Players[s].SalaryBase = 0
	}
	return w
}

// TestGini_PerfectEquality 完全平等:所有玩家财富相等 → 基尼=0。
func TestGini_PerfectEquality(t *testing.T) {
	// 公式级:giniCoefficient 直接验证。
	if g := giniCoefficient([]float64{5, 5, 5, 5}); math.Abs(g) > 1e-12 {
		t.Errorf("giniCoefficient equal sample: got %f, want 0", g)
	}
	// 快照级:12 人等财富 → GiniWealth=0。
	w := newSocietyWorld(7, 12, func(seat int) int64 { return 100_000 })
	snap := ComputeSocietySnapshot(w)
	if math.Abs(snap.GiniWealth) > 1e-12 {
		t.Errorf("GiniWealth under perfect equality: got %f, want 0", snap.GiniWealth)
	}
	if len(snap.Ranks) != 12 {
		t.Errorf("ranks: got %d entries, want 12", len(snap.Ranks))
	}
}

// TestGini_OnePlayerOwnsAll 一人独占:基尼 → 1−1/n(12 人 ≈ 0.917,接近 1);
// 同时 Top1/Top10 = 100%、Bottom50 = 0。
func TestGini_OnePlayerOwnsAll(t *testing.T) {
	w := newSocietyWorld(7, 12, func(seat int) int64 {
		if seat == 0 {
			return 1_000_000
		}
		return 0
	})
	snap := ComputeSocietySnapshot(w)
	if snap.GiniWealth < 0.90 {
		t.Errorf("GiniWealth one-owns-all: got %f, want ≥0.90 (=1−1/12≈0.917)", snap.GiniWealth)
	}
	if math.Abs(snap.Top1Pct-1.0) > 1e-9 {
		t.Errorf("Top1Pct one-owns-all: got %f, want 1.0", snap.Top1Pct)
	}
	if math.Abs(snap.Top10Pct-1.0) > 1e-9 {
		t.Errorf("Top10Pct one-owns-all: got %f, want 1.0", snap.Top10Pct)
	}
	if snap.Bottom50Pct > 1e-9 {
		t.Errorf("Bottom50Pct one-owns-all: got %f, want 0", snap.Bottom50Pct)
	}
	if snap.Ranks[0] != 1 {
		t.Errorf("rank of sole owner: got %d, want 1", snap.Ranks[0])
	}
}

// TestSocietyQuintiles 财富五等分:占比和=1.0;等差递增样本下最低组最小、
// 最高组最大。(注:12 人下组大小为 2,2,3,2,3(labor.go 五等份同构,末组吃
// 余数),组内人数不均可使中间组非严格单调 —— 仅断言两端语义。)
func TestSocietyQuintiles(t *testing.T) {
	w := newSocietyWorld(7, 12, func(seat int) int64 { return int64(seat+1) * 10_000 })
	snap := ComputeSocietySnapshot(w)
	var sum float64
	for _, v := range snap.Quintiles {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("quintiles sum: got %f, want 1.0", sum)
	}
	if snap.Quintiles[0] > 0.10 {
		t.Errorf("bottom quintile share too large: %f (want ≤0.10 for arithmetic sample)", snap.Quintiles[0])
	}
	if snap.Quintiles[4] < 0.30 {
		t.Errorf("top quintile share too small: %f (want ≥0.30 for arithmetic sample)", snap.Quintiles[4])
	}
	for q := 1; q < 4; q++ {
		if snap.Quintiles[q] < snap.Quintiles[0]-1e-12 || snap.Quintiles[q] > snap.Quintiles[4]+1e-12 {
			t.Errorf("middle quintile out of [bottom, top] band: q%d=%f", q, snap.Quintiles[q])
		}
	}
}

// TestSocietyTopShares Top1/Top10/Bottom50 单调关系:
// Top1 ≤ Top10;均匀分布时各自 ≈ 人口占比(1/12、2/12、6/12);
// 集中分布时 Top 份额升高、Bottom50 下降。
func TestSocietyTopShares(t *testing.T) {
	equal := newSocietyWorld(7, 12, func(seat int) int64 { return 50_000 })
	snapE := ComputeSocietySnapshot(equal)
	if snapE.Top1Pct > snapE.Top10Pct+1e-12 {
		t.Errorf("equal: Top1Pct(%f) > Top10Pct(%f), monotonicity broken", snapE.Top1Pct, snapE.Top10Pct)
	}
	// 均匀:Top1 = 1/12(1 人);Top10 = 2/12(ceil(1.2)=2 人);Bottom50 = 6/12。
	if math.Abs(snapE.Top1Pct-1.0/12) > 1e-9 {
		t.Errorf("equal: Top1Pct got %f, want %f", snapE.Top1Pct, 1.0/12)
	}
	if math.Abs(snapE.Top10Pct-2.0/12) > 1e-9 {
		t.Errorf("equal: Top10Pct got %f, want %f", snapE.Top10Pct, 2.0/12)
	}
	if math.Abs(snapE.Bottom50Pct-0.5) > 1e-9 {
		t.Errorf("equal: Bottom50Pct got %f, want 0.5", snapE.Bottom50Pct)
	}

	// 集中(两人分大头):Top 份额较均匀场景上升,Bottom50 下降。
	skew := newSocietyWorld(7, 12, func(seat int) int64 {
		switch seat {
		case 0:
			return 900_000
		case 1:
			return 90_000
		default:
			return 10_000
		}
	})
	snapS := ComputeSocietySnapshot(skew)
	if snapS.Top1Pct <= snapE.Top1Pct {
		t.Errorf("skew: Top1Pct(%f) should exceed equal(%f)", snapS.Top1Pct, snapE.Top1Pct)
	}
	if snapS.Top10Pct <= snapE.Top10Pct {
		t.Errorf("skew: Top10Pct(%f) should exceed equal(%f)", snapS.Top10Pct, snapE.Top10Pct)
	}
	if snapS.Bottom50Pct >= snapE.Bottom50Pct {
		t.Errorf("skew: Bottom50Pct(%f) should be below equal(%f)", snapS.Bottom50Pct, snapE.Bottom50Pct)
	}
	if snapS.Top1Pct > snapS.Top10Pct+1e-12 {
		t.Errorf("skew: Top1Pct(%f) > Top10Pct(%f), monotonicity broken", snapS.Top1Pct, snapS.Top10Pct)
	}
}

// TestWealthPyramid_4Layers 4 层绝对门槛金字塔:各层人数分类正确。
func TestWealthPyramid_4Layers(t *testing.T) {
	// 8 人:2 人 <10万 / 2 人 10万-100万 / 3 人 100万-1000万 / 1 人 ≥1000万。
	cash := map[int]int64{
		0: 50_000, 1: 99_999, // 层0
		2: 100_000, 3: 900_000, // 层1(10万边界含)
		4: 1_000_000, 5: 5_000_000, 6: 9_999_999, // 层2(100万边界含)
		7: 10_000_000, // 层3(1000万边界含)
	}
	w := newSocietyWorld(7, 8, func(seat int) int64 { return cash[seat] })
	snap := ComputeSocietySnapshot(w)
	want := [4]int{2, 2, 3, 1}
	if snap.Pyramid != want {
		t.Errorf("pyramid: got %v, want %v", snap.Pyramid, want)
	}
	var total int
	for _, c := range snap.Pyramid {
		total += c
	}
	if total != 8 {
		t.Errorf("pyramid total headcount: got %d, want 8", total)
	}
}

// TestSocietyMobility 月度社会流动性:排名变动 → 流动性 ∈ [0,1] 且数值精确。
func TestSocietyMobility(t *testing.T) {
	w := newSocietyWorld(7, 4, func(seat int) int64 {
		return []int64{400_000, 300_000, 200_000, 100_000}[seat]
	})
	// 首月:无对比基准 → 0。
	snap1 := ComputeSocietySnapshot(w)
	if snap1.MobilityYoung != 0 {
		t.Errorf("first month mobility: got %f, want 0 (no baseline)", snap1.MobilityYoung)
	}
	if snap1.Ranks[0] != 1 || snap1.Ranks[3] != 4 {
		t.Errorf("initial ranks wrong: %v", snap1.Ranks)
	}
	w.SocietyHist.Push(snap1)

	// 重排:seat1 40万→1(2→1 升)、seat3 30万→2(4→2 升)、seat2 20万→3(3→3 平)、
	// seat0 10万→4(1→4 降)→ 上升 2/4 = 0.5(全员 25 岁 <30 岁计入)。
	w.Players[0].Cash, w.Players[1].Cash = 100_000, 400_000
	w.Players[3].Cash = 300_000
	snap2 := ComputeSocietySnapshot(w)
	if math.Abs(snap2.MobilityYoung-0.5) > 1e-9 {
		t.Errorf("mobility after reshuffle: got %f, want 0.5 (2 of 4 risen)", snap2.MobilityYoung)
	}
	if snap2.MobilityYoung < 0 || snap2.MobilityYoung > 1 {
		t.Errorf("mobility out of [0,1]: %f", snap2.MobilityYoung)
	}

	// ≥30 岁:主时钟推进到 30 岁后样本清空 → 恒 0(年龄口径 = w.Age())。
	w2 := newSocietyWorld(7, 4, func(seat int) int64 {
		return []int64{400_000, 300_000, 200_000, 100_000}[seat]
	})
	w2.Month = 61 // Age = 25 + (61-1)/12 = 30 → 不计入
	base := ComputeSocietySnapshot(w2)
	w2.SocietyHist.Push(base)
	w2.Players[0].Cash, w2.Players[1].Cash = 100_000, 400_000
	w2.Players[3].Cash = 300_000
	if snap := ComputeSocietySnapshot(w2); snap.MobilityYoung != 0 {
		t.Errorf("mobility at age 30+: got %f, want 0 (sample excluded)", snap.MobilityYoung)
	}
}

// TestSocietyWiredIntoSettleMonth §130 接线:settlement ④.6B 入栈 + view 下发
// (society 快照字段 + 12 月趋势);economy_enabled=false 不入栈。
func TestSocietyWiredIntoSettleMonth(t *testing.T) {
	w := NewWorld(23, emptyCardsFor(3))
	for s := 0; s < 3; s++ {
		w.Players[s] = newPlayerFromCard(s, synthCard(s, 200_000))
	}
	w.StartGame()
	if _, res := w.SettleMonth(); res == nil {
		t.Fatal("nil settle result")
	}
	// 引擎接线:历史非空,快照月份 = 结算月(步骤⑤ Month++ 之前生成)。
	if w.SocietyHist == nil || len(w.SocietyHist.Snapshots) != 1 {
		t.Fatalf("SocietyHist after 1 settle: %v, want exactly 1 snapshot", w.SocietyHist)
	}
	if got := w.SocietyHist.Snapshots[0].Month; got != 1 {
		t.Errorf("snapshot month: got %d, want 1 (generated before Month++)", got)
	}
	// view 下发:当前快照字段 + 趋势首点对齐。
	cs := BuildClientState("room-r7", 0, w, [MaxSeats]string{"u:0"}, [MaxSeats]string{"玩家0"},
		[MaxSeats]bool{}, [MaxSeats]string{}, [MaxSeats]BotTranscript{}, 0, 0, 0, nil)
	snap := w.SocietyHist.Snapshots[0]
	if len(cs.Society.Trend) != 1 || cs.Society.Trend[0].Month != 1 {
		t.Fatalf("view trend: got %+v, want single M1 point", cs.Society.Trend)
	}
	if cs.Society.Trend[0].GiniWealth != snap.GiniWealth ||
		cs.Society.Trend[0].GiniIncome != snap.GiniIncome ||
		cs.Society.Trend[0].Top10Pct != snap.Top10Pct {
		t.Errorf("view trend point mismatch: %+v vs snapshot %+v", cs.Society.Trend[0], snap)
	}
	if cs.Society.Top1Pct != snap.Top1Pct || cs.Society.Bottom50Pct != snap.Bottom50Pct ||
		cs.Society.MobilityYoung != snap.MobilityYoung || cs.Society.GiniIncome != snap.GiniIncome {
		t.Errorf("view current snapshot fields mismatch: %+v", cs.Society)
	}
	if cs.Society.Pyramid != snap.Pyramid || cs.Society.WealthQuintiles != snap.Quintiles {
		t.Errorf("view pyramid/quintiles mismatch: pyramid %v vs %v", cs.Society.Pyramid, snap.Pyramid)
	}
	// economy_enabled=false:完整跳过(历史保持空)。
	w2 := NewWorld(23, emptyCardsFor(3))
	for s := 0; s < 3; s++ {
		w2.Players[s] = newPlayerFromCard(s, synthCard(s, 200_000))
	}
	w2.EconomyEnabled = false
	w2.StartGame()
	w2.SettleMonth()
	if w2.SocietyHist == nil || len(w2.SocietyHist.Snapshots) != 0 {
		t.Errorf("disabled economy should not push snapshots: %v", w2.SocietyHist)
	}
}

// TestSocietyDeterministic 同种子双世界逐月结算 → 全部快照逐字段一致(零 rand 消费)。
func TestSocietyDeterministic(t *testing.T) {
	run := func() *SocietyHistory {
		w := NewWorld(99, emptyCardsFor(4))
		for s := 0; s < 4; s++ {
			w.Players[s] = newPlayerFromCard(s, synthCard(s, 100_000+int64(s)*50_000))
		}
		w.StartGame()
		for i := 0; i < 3; i++ {
			w.SettleMonth()
		}
		return w.SocietyHist
	}
	ha, hb := run(), run()
	if len(ha.Snapshots) != 3 || len(hb.Snapshots) != 3 {
		t.Fatalf("snapshot count: %d / %d, want 3", len(ha.Snapshots), len(hb.Snapshots))
	}
	for i := 0; i < 3; i++ {
		a, b := ha.Snapshots[i], hb.Snapshots[i]
		if a.Month != b.Month || a.GiniWealth != b.GiniWealth || a.GiniIncome != b.GiniIncome ||
			a.Top1Pct != b.Top1Pct || a.Top10Pct != b.Top10Pct || a.Bottom50Pct != b.Bottom50Pct ||
			a.Quintiles != b.Quintiles || a.Pyramid != b.Pyramid || a.MobilityYoung != b.MobilityYoung {
			t.Errorf("snapshot[%d] diverged: %+v vs %+v", i, a, b)
		}
		if !reflect.DeepEqual(a.Ranks, b.Ranks) {
			t.Errorf("snapshot[%d] ranks diverged: %v vs %v", i, a.Ranks, b.Ranks)
		}
	}
}
