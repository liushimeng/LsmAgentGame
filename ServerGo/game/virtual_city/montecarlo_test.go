// Package virtual_city — montecarlo_test.go: 批次72 蒙特卡洛验收测试
// (方案 §9 T-MC1~T-MC6 + 单局时延测量)。
//
// 硬纪律:本文件零真实 LLM / 零网络 / 零 DB / 零定时器;策略代理即 LLM 替身。
// TestMain 把运行记录写盘目录重定向到临时目录(测试二进制内全局生效,
// 含 calibration_test.go 的 finalize 路径),避免污染仓库工作目录。
package virtual_city

import (
	"math"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/game/virtual_city/profession"
)

// TestMain 重定向 RunStore 写盘目录(测试专用;LSM_MC_RUNS_DIR 可覆盖)。
func TestMain(m *testing.M) {
	dir := os.Getenv("LSM_MC_RUNS_DIR")
	cleanup := false
	if dir == "" {
		d, err := os.MkdirTemp("", "lsm-vc-mc-runs-")
		if err != nil {
			dir = os.TempDir() // 兜底:绝不因测试环境 fail-fast
		} else {
			dir, cleanup = d, true
		}
	}
	SetCalibrationRunsDir(dir)
	code := m.Run()
	if cleanup {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

// mcSmokeRunsDefault T10 默认局数(方案 §4.4:由实测单局时延定,
// 公式 min(200, max(5, ⌊60s÷单局时延⌋ ÷5×5)))。
//
// 实测(2026-10-21,本机 4 核,IsSeatEntity 热路径修复后):
//   - 单局时延:balanced 281ms / stress 183ms(TestMCSingleRunTiming);
//   - 公式:⌊60s÷0.281s⌋ = 213 ⇒ ÷5×5 = 210 ⇒ min(200, 210) = 200;
//   - 实跑 50 局(并行 4 worker)墙钟 6.2s ⇒ 200 局估算 ≈ 25s(≤60s 预算,
//     120s 硬护栏之内;慢一倍的 CI 机器仍 ~50s)。
//
// LSM_MC_SMOKE_RUNS 环境变量可覆盖(≥5,向下取 5 的倍数)。
const mcSmokeRunsDefault = 200

// mcSmokeRuns T10 局数(≥5;向下取 5 的倍数保证 5 策略均分)。
func mcSmokeRuns() int {
	if v := os.Getenv("LSM_MC_SMOKE_RUNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 {
			return 5 * (n / 5)
		}
	}
	return mcSmokeRunsDefault
}

// mcWorld 构造与 MCRunOne 同口径的世界(卡源 rng 独立;测试驱动复用)。
func mcWorld(seed int64) *World {
	cardRng := rand.New(rand.NewSource(seed))
	cards := [MaxSeats]profession.Card{}
	for i, c := range profession.SyntheticCards(MaxSeats, cardRng) {
		if i >= MaxSeats {
			break
		}
		cards[i] = c
	}
	w := NewWorld(seed, cards)
	w.StartGame()
	return w
}

// ── T-MC1 策略确定性:同种子双跑 MCRunStats 深相等(含 StateHash 终值)──

func TestMCDeterminism_SameSeedTwice(t *testing.T) {
	for _, s := range MCStrategies() {
		s := s
		t.Run(s, func(t *testing.T) {
			seed := MCSeedFor(s, 3)
			a := MCRunOne(seed, s)
			b := MCRunOne(seed, s)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("同种子双跑不一致:months %d/%d, hash %s/%s",
					a.Months, b.Months, a.StateHash, b.StateHash)
			}
			// ⚠ 经济基线现状(批次72 首跑):全桌出局会在 M420 前终局
			//(SettleMonth:alivePlayers==0 ⇒ finished),故不硬性要求 420 月;
			// 只要求推进合法且收口信息齐备。
			if a.Months < 1 || a.Months > mcMaxMonths || !a.Finished {
				t.Errorf("终局月数异常:months=%d finished=%v(应 1..%d 且终局)",
					a.Months, a.Finished, mcMaxMonths)
			}
			if a.StateHash == "" {
				t.Error("StateHash 终值为空(finalize 收口信息缺失)")
			}
		})
	}
}

// TestMCRunOne_UnknownStrategyNoPanic 未知策略零值返回,不 panic。
func TestMCRunOne_UnknownStrategyNoPanic(t *testing.T) {
	stats := MCRunOne(12345, "no_such_strategy")
	if stats.Months != 0 || stats.Finished {
		t.Errorf("未知策略应返回零值统计:got %+v", stats)
	}
}

// ── T-MC2 策略零 rand:决策与注入路径不消费 w.Rand ────────────────────

func TestMCStrategies_ZeroRandConsumption(t *testing.T) {
	for _, s := range MCStrategies() {
		s := s
		t.Run(s, func(t *testing.T) {
			w := mcWorld(20261021)
			defer func() { _ = recover() }()
			probe := rand.New(rand.NewSource(4242))
			w.Rand = probe // 探针流:策略阶段任何 rand 消费都会被捕获
			for m := 1; m <= 3; m++ {
				mcResetMonthBudgets(w)
				for seat := range w.Players {
					acts := MCStrategyActions(w, s, seat, m)
					for i := 0; i < len(acts) && i < mcMonthlyActionCap; i++ {
						_, _ = w.ApplyAction(seat, acts[i])
					}
				}
				MCStressPreSettle(w, m)
				MCStressPostSettle(w)
			}
			fresh := rand.New(rand.NewSource(4242))
			for i := 0; i < 64; i++ {
				if probe.Int63() != fresh.Int63() {
					t.Fatalf("策略 %s 消费了 w.Rand(第 %d 次对拍分歧)", s, i)
				}
			}
		})
	}
}

// ── T-MC3 stress 注入:相位覆盖 / 失业冲击 / 医疗冲击 ─────────────────

func TestMCStressInjections(t *testing.T) {
	w := mcWorld(20261021)
	aliveAt := func() int {
		n := 0
		for _, p := range w.Players {
			if p != nil && p.Alive {
				n++
			}
		}
		return n
	}
	medicalCount := func() int {
		n := 0
		for _, e := range w.Ledger.Entries {
			if e.Category == CatMedical && e.Note == stressMedicalNote {
				n++
			}
		}
		return n
	}

	var aliveAt62 int
	for m := 1; m <= 63; m++ {
		mcResetMonthBudgets(w)
		for seat := range w.Players {
			acts := MCStrategyActions(w, MCStrategyStress, seat, m)
			for i := 0; i < len(acts) && i < mcMonthlyActionCap; i++ {
				_, _ = w.ApplyAction(seat, acts[i])
			}
		}
		if m == stressMedicalMonth {
			aliveAt62 = aliveAt()
		}
		MCStressPreSettle(w, m)
		switch m {
		case stressUnemploymentMonth:
			for seat, p := range w.Players {
				if p == nil || !p.Alive {
					continue
				}
				if p.UnemployedMonths != 2 {
					t.Fatalf("M26 注入后座位 %d UnemployedMonths = %d,期望 2", seat, p.UnemployedMonths)
				}
			}
		case stressMedicalMonth:
			if got := medicalCount(); got != aliveAt62 {
				t.Fatalf("M62 医疗支出条数 = %d,期望 = 存活座位数 %d", got, aliveAt62)
			}
			for _, p := range w.Players {
				if p == nil || !p.Alive {
					continue
				}
				if p.Energy < -3 {
					t.Fatalf("M62 注入后 Energy = %d,应 clamp ≥ -3", p.Energy)
				}
				if !p.HadIllnessYear {
					t.Fatal("M62 注入后 HadIllnessYear 应为 true")
				}
			}
		}
		finished, _ := w.SettleMonth()
		MCStressPostSettle(w)
		switch m {
		case 24: // SettleMonth 后 w.Month = 25 ⇒ 下月(25)强制衰退
			if w.Market.CyclePhase != PhaseRecession || w.Market.CycleMonthsLeft != 1 {
				t.Fatalf("M25 相位 = %s(left %d),期望 recession/1", w.Market.CyclePhase, w.Market.CycleMonthsLeft)
			}
		case 25:
			if w.Market.CyclePhase != PhaseRecession {
				t.Fatalf("M26 相位 = %s,期望仍被强制为 recession", w.Market.CyclePhase)
			}
		case 60: // w.Month = 61 ⇒ 强制萧条
			if w.Market.CyclePhase != PhaseDepression || w.Market.CycleMonthsLeft != 1 {
				t.Fatalf("M61 相位 = %s(left %d),期望 depression/1", w.Market.CyclePhase, w.Market.CycleMonthsLeft)
			}
		case 73: // w.Month = 74 ⇒ 窗口外,不再强制(此处只验证不 panic)
		}
		if finished {
			break
		}
	}
}

// ── T-MC4 门禁边界:合成 MCRunStats 压线 ±ε 逐条翻转 ─────────────────

// mcSynthRun 合成一局统计(逐月序列自动铺底;锚点月快照零值,由 tune 填)。
func mcSynthRun(strategy string, months int, tune func(*MCRunStats)) MCRunStats {
	r := MCRunStats{Seed: 1, Strategy: strategy, Months: months, InvariantFailsByID: map[string]int{}}
	r.AliveByMonth = make([]int, months)
	r.EliminatedByMonth = make([]int, months)
	r.LiquidatedByMonth = make([]int, months)
	for i := range r.AliveByMonth {
		r.AliveByMonth[i] = MaxSeats
	}
	r.AnchorM1.Month = 1
	if months >= 97 {
		r.AnchorM97.Month = 97
	}
	if months >= 120 {
		r.AnchorM120.Month = 120
	}
	if months >= 360 {
		r.AnchorM360.Month = 360
	}
	r.AnchorFinal.Month = months
	for seat := 0; seat < MaxSeats; seat++ {
		r.AnchorM1.Alive[seat] = true
		r.AnchorM120.Alive[seat] = true
		r.AnchorM360.Alive[seat] = true
		r.AnchorFinal.Alive[seat] = true
		r.AnchorM1.MonthlyNet[seat] = 100
	}
	if tune != nil {
		tune(&r)
	}
	return r
}

// mcGateByID 取门禁结果 by id。
func mcGateByID(t *testing.T, gates []GateResult, id string) GateResult {
	t.Helper()
	for _, g := range gates {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("门禁 %s 不在评估结果中", id)
	return GateResult{}
}

// TestMCGates_OrderAndCount 恒 16 条、顺序固定。
func TestMCGates_OrderAndCount(t *testing.T) {
	gates := MCEvaluateGates(nil)
	want := []string{GateG1, GateG2, GateG3, GateG4, GateG5, GateG6, GateG7,
		GateG8, GateG9, GateG10, GateG11, GateG12, GateA1, GateA2, GateA3, GateA4}
	if len(gates) != len(want) {
		t.Fatalf("门禁条数 = %d,期望 %d", len(gates), len(want))
	}
	for i, id := range want {
		if gates[i].ID != id {
			t.Errorf("门禁[%d] = %s,期望 %s", i, gates[i].ID, id)
		}
	}
	for _, id := range []string{GateA3, GateA4} {
		g := mcGateByID(t, gates, id)
		if !g.Passed || g.Detail == "" {
			t.Errorf("%s 缺数据项应 Passed=true + 非空 detail:%+v", id, g)
		}
	}
}

// TestMCGates_G1Boundary M1 月结非负(波动卡豁免)。
func TestMCGates_G1Boundary(t *testing.T) {
	bad := mcSynthRun(MCStrategyBalanced, 420, func(r *MCRunStats) {
		r.AnchorM1.MonthlyNet[3] = -1
	})
	if g := MCEvaluateGates([]MCRunStats{bad})[0]; g.Passed {
		t.Errorf("非波动座位 M1 月结 -1 应失败:%+v", g)
	}
	volatile := mcSynthRun(MCStrategyBalanced, 420, func(r *MCRunStats) {
		r.AnchorM1.Volatile[3] = true
		r.AnchorM1.MonthlyNet[3] = -1
	})
	if g := MCEvaluateGates([]MCRunStats{volatile})[0]; !g.Passed {
		t.Errorf("波动卡负月结不判失败:%+v", g)
	}
}

// TestMCGates_G2Boundary M97 存活(均值 ≥10 且 P05 ≥9)。
func TestMCGates_G2Boundary(t *testing.T) {
	pass := []MCRunStats{
		mcSynthRun("a", 420, func(r *MCRunStats) { r.AliveByMonth[96] = 11 }),
		mcSynthRun("b", 420, func(r *MCRunStats) { r.AliveByMonth[96] = 9 }),
	}
	if g := MCEvaluateGates(pass)[1]; !g.Passed {
		t.Errorf("均值 10 + P05 9 应通过:%+v", g)
	}
	fail := []MCRunStats{
		mcSynthRun("a", 420, func(r *MCRunStats) { r.AliveByMonth[96] = 10 }),
		mcSynthRun("b", 420, func(r *MCRunStats) { r.AliveByMonth[96] = 9 }),
	}
	if g := MCEvaluateGates(fail)[1]; g.Passed {
		t.Errorf("均值 9.5 < 10 应失败:%+v", g)
	}
	p05 := []MCRunStats{
		mcSynthRun("a", 420, func(r *MCRunStats) { r.AliveByMonth[96] = 12 }),
		mcSynthRun("b", 420, func(r *MCRunStats) { r.AliveByMonth[96] = 8 }),
	}
	if g := MCEvaluateGates(p05)[1]; g.Passed {
		t.Errorf("P05 8 < 9 应失败:%+v", g)
	}
}

// TestMCGates_G3Boundary M120 出局率(均值严格 <15%;CI 上界 <20%;单局 ≤4/12)。
func TestMCGates_G3Boundary(t *testing.T) {
	mk := func(elim int) MCRunStats {
		return mcSynthRun("a", 420, func(r *MCRunStats) { r.EliminatedByMonth[119] = elim })
	}
	if g := MCEvaluateGates([]MCRunStats{mk(1)})[2]; !g.Passed {
		t.Errorf("单局 1/12 应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(2)})[2]; g.Passed { // 均值 1/6≈16.7%
		t.Errorf("均值 16.7%% ≥ 15%% 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(5)})[2]; g.Passed { // 单局 5/12
		t.Errorf("单局 5/12 > 4/12 应失败:%+v", g)
	}
	// CI 上界单独爆:两局 {0.05, 0.23},均值 0.14 < 0.15 但 sd 大 ⇒ CI > 0.20。
	ci := []MCRunStats{
		mcSynthRun("a", 420, func(r *MCRunStats) { r.EliminatedByMonth[119] = 0 }),
		mcSynthRun("b", 420, func(r *MCRunStats) { r.EliminatedByMonth[119] = 3 }),
	}
	g := MCEvaluateGates(ci)[2]
	if g.Passed {
		t.Errorf("95%%CI 上界 > 20%% 应失败:%+v", g)
	}
}

// TestMCGates_G4Boundary M120 清算率 ∈ [8%,25%] 且 G3 成立。
func TestMCGates_G4Boundary(t *testing.T) {
	mk := func(liq int, elim int) MCRunStats {
		return mcSynthRun("a", 420, func(r *MCRunStats) {
			r.LiquidatedByMonth[119] = liq
			r.EliminatedByMonth[119] = elim
		})
	}
	if g := MCEvaluateGates([]MCRunStats{mk(1, 0)})[3]; !g.Passed {
		t.Errorf("清算率恰 8%% 边界应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0, 0)})[3]; g.Passed {
		t.Errorf("清算率 0 < 8%% 应失败:%+v", g)
	}
	// 精确压线:3/12 = 25% 恰在闭区间内应通过。
	if g := MCEvaluateGates([]MCRunStats{mk(3, 0)})[3]; !g.Passed {
		t.Errorf("清算率恰 25%% 边界应通过:%+v", g)
	}
	// G3 不成立 ⇒ G4 联动失败。
	if g := MCEvaluateGates([]MCRunStats{mk(1, 5)})[3]; g.Passed {
		t.Errorf("G3 失败时 G4 应联动失败:%+v", g)
	}
}

// TestMCGates_G5Boundary 终局出局 ∈ [5%,15%];平均存活 ≥ 10.2。
func TestMCGates_G5Boundary(t *testing.T) {
	mk := func(elim int) MCRunStats {
		return mcSynthRun("a", 420, func(r *MCRunStats) {
			r.EliminatedByMonth[419] = elim
			for i := range r.AliveByMonth {
				r.AliveByMonth[i] = MaxSeats - elim
			}
		})
	}
	if g := MCEvaluateGates([]MCRunStats{mk(1)})[4]; !g.Passed { // 1/12≈8.3%
		t.Errorf("出局 8.3%% 在区间应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(2)})[4]; g.Passed { // 16.7% > 15%
		t.Errorf("出局 16.7%% > 15%% 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0)})[4]; g.Passed { // 0% < 5%
		t.Errorf("出局 0%% < 5%% 应失败:%+v", g)
	}
	// 出局达标但平均存活 10 < 10.2 ⇒ 失败。
	alive := mcSynthRun("a", 420, func(r *MCRunStats) {
		r.EliminatedByMonth[419] = 1
		for i := range r.AliveByMonth {
			r.AliveByMonth[i] = 10
		}
	})
	if g := MCEvaluateGates([]MCRunStats{alive})[4]; g.Passed {
		t.Errorf("平均存活 10 < 10.2 应失败:%+v", g)
	}
}

// TestMCGates_G6G7Boundary 条件 FI 门禁。
func TestMCGates_G6G7Boundary(t *testing.T) {
	// G6:6 存活座 FI>0 + median 0.05 恰压线通过
	//(其余 6 座已出局不进 FI 分布)。
	g6pass := mcSynthRun(MCStrategyMinimumEffort, 420, func(r *MCRunStats) {
		for seat := 0; seat < 6; seat++ {
			r.AnchorM120.FI[seat] = 0.05
		}
		for seat := 6; seat < MaxSeats; seat++ {
			r.AnchorM120.Alive[seat] = false
		}
	})
	if g := MCEvaluateGates([]MCRunStats{g6pass})[5]; !g.Passed {
		t.Errorf("G6 6 座 + median 0.05 恰压线应通过:%+v", g)
	}
	g6fail := mcSynthRun(MCStrategyMinimumEffort, 420, func(r *MCRunStats) {
		for seat := 0; seat < 5; seat++ {
			r.AnchorM120.FI[seat] = 0.05 // 只 5 座 >0
		}
		for seat := 5; seat < MaxSeats; seat++ {
			r.AnchorM120.Alive[seat] = false
		}
	})
	if g := MCEvaluateGates([]MCRunStats{g6fail})[5]; g.Passed {
		t.Errorf("G6 5 座 < 6 应失败:%+v", g)
	}
	g6median := mcSynthRun(MCStrategyMinimumEffort, 420, func(r *MCRunStats) {
		for seat := 0; seat < MaxSeats; seat++ {
			r.AnchorM120.FI[seat] = 0.049 // 全部 >0 但 median 不足
		}
	})
	if g := MCEvaluateGates([]MCRunStats{g6median})[5]; g.Passed {
		t.Errorf("G6 median 0.049 < 0.05 应失败:%+v", g)
	}

	// G7:9 座 FI>0 + median 0.80 压线通过;0.79 失败。
	g7 := func(median float64) MCRunStats {
		return mcSynthRun(MCStrategyBalanced, 420, func(r *MCRunStats) {
			for seat := 0; seat < 9; seat++ { // 9 存活座 FI>0(其余 3 座出局)
				r.AnchorM360.FI[seat] = median
			}
			for seat := 9; seat < MaxSeats; seat++ {
				r.AnchorM360.Alive[seat] = false
			}
		})
	}
	if g := MCEvaluateGates([]MCRunStats{g7(0.80)})[6]; !g.Passed {
		t.Errorf("G7 9 座 + median 0.80 恰压线应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{g7(0.79)})[6]; g.Passed {
		t.Errorf("G7 median 0.79 < 0.80 应失败:%+v", g)
	}
}

// TestMCGates_G8G9Boundary 分布门禁闭区间压线。
func TestMCGates_G8G9Boundary(t *testing.T) {
	mk := func(g60, g120, t120, gf, tf float64) MCRunStats {
		return mcSynthRun("a", 420, func(r *MCRunStats) {
			r.GiniM60, r.GiniM120, r.Top20M120 = g60, g120, t120
			r.GiniFinal, r.Top20Final = gf, tf
		})
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.20, 0.25, 0.75, 0.30, 0.50)})[7]; !g.Passed {
		t.Errorf("G8 下边界恰压线应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.45, 0.50, 0.75, 0.60, 0.80)})[7]; !g.Passed {
		t.Errorf("G8 上边界恰压线应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.19, 0.30, 0.70, 0.40, 0.60)})[7]; g.Passed {
		t.Errorf("G8 M60 0.19 < 0.20 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.30, 0.51, 0.70, 0.40, 0.60)})[7]; g.Passed {
		t.Errorf("G8 M120 0.51 > 0.50 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.30, 0.30, 0.76, 0.40, 0.60)})[7]; g.Passed {
		t.Errorf("G8 top20 76%% > 75%% 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.30, 0.30, 0.70, 0.29, 0.60)})[8]; g.Passed {
		t.Errorf("G9 Gini 0.29 < 0.30 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.30, 0.30, 0.70, 0.40, 0.49)})[8]; g.Passed {
		t.Errorf("G9 top20 49%% < 50%% 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(0.30, 0.30, 0.70, 0.40, 0.81)})[8]; g.Passed {
		t.Errorf("G9 top20 81%% > 80%% 应失败:%+v", g)
	}
}

// TestMCGates_G10Boundary 排名粘性:过粘(ρ=1)与过散(ρ=−1)同判失败。
func TestMCGates_G10Boundary(t *testing.T) {
	ident := mcSynthRun("a", 420, func(r *MCRunStats) {
		r.NetWorthM1 = make([]float64, MaxSeats)
		r.NetWorthM120 = make([]float64, MaxSeats)
		for i := 0; i < MaxSeats; i++ {
			r.NetWorthM1[i] = float64(i + 1)
			r.NetWorthM120[i] = float64(i + 1) // 完全保持 ⇒ ρ=1
		}
	})
	if g := MCEvaluateGates([]MCRunStats{ident})[9]; g.Passed {
		t.Errorf("G10 ρ=1 过粘应失败:%+v", g)
	}
	rev := mcSynthRun("a", 420, func(r *MCRunStats) {
		r.NetWorthM1 = make([]float64, MaxSeats)
		r.NetWorthM120 = make([]float64, MaxSeats)
		for i := 0; i < MaxSeats; i++ {
			r.NetWorthM1[i] = float64(i + 1)
			r.NetWorthM120[i] = float64(MaxSeats - i)
		}
	})
	if g := MCEvaluateGates([]MCRunStats{rev})[9]; g.Passed {
		t.Errorf("G10 ρ=−1 过散应失败:%+v", g)
	}
	// 中间档:半数保持 + 半数反转的确定性构造,ρ 与工具一致裁决。
	mid := mcSynthRun("a", 420, func(r *MCRunStats) {
		r.NetWorthM1 = make([]float64, MaxSeats)
		r.NetWorthM120 = make([]float64, MaxSeats)
		for i := 0; i < MaxSeats; i++ {
			r.NetWorthM1[i] = float64(i + 1)
			r.NetWorthM120[i] = float64(i + 1)
			if i%2 == 0 {
				r.NetWorthM120[i] = float64(MaxSeats - i)
			}
		}
	})
	rho := mcSpearman(mid.NetWorthM1, mid.NetWorthM120)
	g := MCEvaluateGates([]MCRunStats{mid})[9]
	if g.Passed != (rho >= 0.35 && rho <= 0.80) {
		t.Errorf("G10 与 Spearman 工具裁决不一致:ρ=%.4f passed=%v", rho, g.Passed)
	}
	if math.Abs(rho) > 1 {
		t.Errorf("ρ 越界:%f", rho)
	}
}

// TestMCGates_G11G12Boundary 杠杆出局与不变量。
func TestMCGates_G11G12Boundary(t *testing.T) {
	mk := func(elim int, peak int) MCRunStats {
		return mcSynthRun(MCStrategyAggressiveLeverage, 420, func(r *MCRunStats) {
			r.EliminatedByMonth[419] = elim
			r.ChainBankruptcyPeak = peak
		})
	}
	if g := MCEvaluateGates([]MCRunStats{mk(3, 5)})[10]; !g.Passed {
		t.Errorf("G11 25%% + 峰值 5 恰压线应通过:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(4, 3)})[10]; g.Passed {
		t.Errorf("G11 出局 33%% > 25%% 应失败:%+v", g)
	}
	if g := MCEvaluateGates([]MCRunStats{mk(1, 6)})[10]; g.Passed {
		t.Errorf("G11 连锁峰值 6 > 5 应失败:%+v", g)
	}
	// G12:I3/I5 不在判定面;I1 任一违例即失败。
	ok := mcSynthRun("a", 420, func(r *MCRunStats) {
		r.InvariantFailsByID[InvariantI3] = 7
		r.InvariantFailsByID[InvariantI5] = 3
	})
	if g := MCEvaluateGates([]MCRunStats{ok})[11]; !g.Passed {
		t.Errorf("G12 I3/I5 违例不判失败:%+v", g)
	}
	bad := mcSynthRun("a", 420, func(r *MCRunStats) {
		r.InvariantFailsByID[InvariantI1] = 1
	})
	if g := MCEvaluateGates([]MCRunStats{bad})[11]; g.Passed {
		t.Errorf("G12 I1 违例应失败:%+v", g)
	}
}

// TestMCGates_A1A2Boundary 附加护栏(可采部分)。
func TestMCGates_A1A2Boundary(t *testing.T) {
	ok := mcSynthRun("a", 420, nil)
	if g := MCEvaluateGates([]MCRunStats{ok})[12]; !g.Passed {
		t.Errorf("A1 全月存活 12 应通过:%+v", g)
	}
	dip := mcSynthRun("a", 420, func(r *MCRunStats) { r.AliveByMonth[30] = 10 })
	if g := MCEvaluateGates([]MCRunStats{dip})[12]; g.Passed {
		t.Errorf("A1 单月存活 10 < 10.5 应失败:%+v", g)
	}
	nw := mcSynthRun(MCStrategySafetyNetOnly, 420, func(r *MCRunStats) {
		for seat := 0; seat < MaxSeats; seat++ {
			r.AnchorM120.NetWorth[seat] = float64(seat) * 1000 // 全正
		}
	})
	if g := MCEvaluateGates([]MCRunStats{nw})[13]; !g.Passed {
		t.Errorf("A2 中位净资产 > 0 应通过:%+v", g)
	}
	neg := mcSynthRun(MCStrategySafetyNetOnly, 420, func(r *MCRunStats) {
		for seat := 0; seat < MaxSeats; seat++ {
			r.AnchorM120.NetWorth[seat] = -float64(seat+1) * 1000
		}
	})
	if g := MCEvaluateGates([]MCRunStats{neg})[13]; g.Passed {
		t.Errorf("A2 中位净资产 < 0 应失败:%+v", g)
	}
}

// ── T-MC5 数学工具 ────────────────────────────────────────────────────

func TestMCMathTools(t *testing.T) {
	// Spearman:完美单调 / 逆序 / 含并列(手算 0.5)。
	xs := []float64{1, 2, 3, 4, 5}
	if r := mcSpearman(xs, []float64{2, 4, 6, 8, 10}); math.Abs(r-1) > 1e-9 {
		t.Errorf("完美单调 ρ = %f,期望 1", r)
	}
	if r := mcSpearman(xs, []float64{10, 8, 6, 4, 2}); math.Abs(r+1) > 1e-9 {
		t.Errorf("逆序 ρ = %f,期望 -1", r)
	}
	if r := mcSpearman([]float64{1, 1, 2}, []float64{1, 2, 2}); math.Abs(r-0.5) > 1e-9 {
		t.Errorf("并列平均秩 ρ = %f,期望 0.5", r)
	}
	if r := mcSpearman(xs, []float64{1}); r != 0 {
		t.Errorf("n<2 应返回 0,got %f", r)
	}

	// Gini:均等 = 0;单人独占 = (n−1)/n。
	eq := make([]float64, MaxSeats)
	for i := range eq {
		eq[i] = 100
	}
	if g := mcGini(eq); math.Abs(g) > 1e-9 {
		t.Errorf("均等 Gini = %f,期望 0", g)
	}
	one := make([]float64, MaxSeats)
	one[MaxSeats-1] = 100
	if g := mcGini(one); math.Abs(g-11.0/12.0) > 1e-9 {
		t.Errorf("单人独占 Gini = %f,期望 11/12≈0.9167", g)
	}

	// top20:12 座 top3 份额。
	t20 := make([]float64, MaxSeats)
	for i := 0; i < 3; i++ {
		t20[i] = 30
	}
	for i := 3; i < MaxSeats; i++ {
		t20[i] = 3 // 9×3=27 ⇒ 90/117
	}
	if s := mcTop20Share(t20); math.Abs(s-90.0/117.0) > 1e-9 {
		t.Errorf("top20 = %f,期望 90/117≈0.769", s)
	}

	// 正态 CI:{1..5} 均值 3,样本 sd = √2.5。
	ci := mcNormalCIUpper95([]float64{1, 2, 3, 4, 5})
	want := 3 + mcZ95*math.Sqrt(2.5)/math.Sqrt(5)
	if math.Abs(ci-want) > 1e-12 {
		t.Errorf("CI 上界 = %f,期望 %f", ci, want)
	}
	if math.Abs(want-4.385903) > 1e-5 {
		t.Errorf("CI 手算对照 = %f,期望 ≈4.385903", want)
	}
	if c := mcNormalCIUpper95([]float64{7}); c != 7 {
		t.Errorf("n=1 CI 上界应 = 均值,got %f", c)
	}

	// 分位数:1..100 线性插值。
	full := make([]float64, 100)
	for i := range full {
		full[i] = float64(i + 1)
	}
	if p := mcPercentile(full, 0.05); math.Abs(p-5.95) > 1e-9 {
		t.Errorf("P05 = %f,期望 5.95", p)
	}
	if p := mcPercentile(full, 0.5); math.Abs(p-50.5) > 1e-9 {
		t.Errorf("P50 = %f,期望 50.5", p)
	}
	if p := mcPercentile(full, 0.95); math.Abs(p-95.05) > 1e-9 {
		t.Errorf("P95 = %f,期望 95.05", p)
	}
}

// ── 单局时延测量(方案 §4.4:先测再定 T10 局数)───────────────────────

// TestMCSingleRunTiming 单局时延(信息性输出;断言仅防退化上限 10s)。
func TestMCSingleRunTiming(t *testing.T) {
	for _, s := range []string{MCStrategyBalanced, MCStrategyStress} {
		start := time.Now()
		stats := MCRunOne(MCSeedFor(s, 0), s)
		elapsed := time.Since(start)
		if elapsed > 10*time.Second {
			t.Errorf("单局(%s)时延 %v 超退化上限 10s", s, elapsed)
		}
		if stats.Months != mcMaxMonths {
			t.Errorf("单局应推进满 %d 月,got %d", mcMaxMonths, stats.Months)
		}
		t.Logf("单局时延 strategy=%s seed=%d months=%d elapsed=%v", s, MCSeedFor(s, 0), stats.Months, elapsed)
	}
}

// ── T-MC6 smoke:harness + 门禁评估器无 panic;G12 类 0 failure ────────

// mcRunBatch 并行批量跑 total 局(5 策略均分;结果按任务序对齐)。
func mcRunBatch(t *testing.T, total int) []MCRunStats {
	t.Helper()
	per := total / len(mcStrategyOrder)
	type job struct {
		strategy string
		idx      int
	}
	jobs := make([]job, 0, total)
	for _, s := range mcStrategyOrder {
		for i := 0; i < per; i++ {
			jobs = append(jobs, job{s, i})
		}
	}
	results := make([]MCRunStats, len(jobs))
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	ch := make(chan int, len(jobs))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				j := jobs[idx]
				results[idx] = MCRunOne(MCSeedFor(j.strategy, j.idx), j.strategy)
			}
		}()
	}
	for i := range jobs {
		ch <- i
	}
	close(ch)
	wg.Wait()
	return results
}

func TestMCSmoke(t *testing.T) {
	runs := mcSmokeRuns()
	start := time.Now()
	stats := mcRunBatch(t, runs)
	elapsed := time.Since(start)

	if len(stats) != runs {
		t.Fatalf("批量结果数 = %d,期望 %d", len(stats), runs)
	}
	for i, s := range stats {
		if s.Months == 0 {
			t.Errorf("第 %d 局未推进任何月份", i)
		}
	}
	gates := MCEvaluateGates(stats)
	if len(gates) != 16 {
		t.Fatalf("门禁条数 = %d,期望 16", len(gates))
	}
	// 不变量类(G12)必须 0 failure —— 框架自身正确性。
	g12 := mcGateByID(t, gates, GateG12)
	if !g12.Passed {
		t.Errorf("G12 不变量门禁失败(框架自身正确性问题):%+v", g12)
		for _, s := range stats {
			if len(s.InvariantFailsByID) > 0 {
				t.Errorf("seed=%d strategy=%s fails=%v", s.Seed, s.Strategy, s.InvariantFailsByID)
			}
		}
	}
	// 其余 G 门禁:数值问题归批次 73 裁决,只记录不 fail。
	for _, g := range gates {
		if g.ID == GateG12 {
			continue
		}
		if !g.Passed {
			t.Logf("[smoke-基线] 门禁 %s(%s)未通过:actual=%.4f threshold=%.4f detail=%s",
				g.ID, g.Name, g.Actual, g.Threshold, g.Detail)
		}
	}
	// 失败样本输出(五元组)。
	samples := 0
	for _, s := range stats {
		for _, fs := range s.EliminationSamples {
			if samples < 10 {
				t.Logf("失败样本:seed=%d month=%d seat=%d cash=%d net_worth=%d",
					fs.Seed, fs.Month, fs.Seat, fs.Cash, fs.NetWorth)
			}
			samples++
		}
	}
	t.Logf("T10 smoke:%d 局(%d×5 策略)总耗时 %v(单局均值 %.1fms);失败样本 %d 条",
		runs, runs/5, elapsed, float64(elapsed.Milliseconds())/float64(runs), samples)
	if elapsed > 120*time.Second {
		t.Errorf("T10 总耗时 %v 超过 120s 上限(CI 回归防护)", elapsed)
	}
}
