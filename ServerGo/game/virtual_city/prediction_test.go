// Package virtual_city — prediction_test.go: 预测运行引擎单测
// (2026-10-21 §预测运行,批次68)。
//
// 覆盖(方案 §6-7):①零 rand 双跑一致 + Rand 流零消耗;②T0 基线先于冲击;
// ③horizon 内并入 / horizon 外新 run + 环形 3;④trajectory 上限 36;
// ⑤report 必填章节非空 + R1 边界声明;⑥recovery_month 判定(收敛序列);
// ⑦无 run → report nil 不 panic;另:35058 run_id 未命中。
package virtual_city

import (
	"testing"

	"LsmAgentGame/errcode"
)

// applyPredScenario 在指定 world 上注入地震(engine 级;room 层测试另走
// ApplyScenario)。校验失败返回 errcode,否则 nil。
func applyPredScenario(t *testing.T, w *World) *errcode.Error {
	t.Helper()
	_, _, _, e := applyScenarioToWorld(w, ScenarioEarthquake, 3, 6, "", "det", 1730000000000, 12345)
	return e
}

// TestPrediction_ZeroRand 双跑一致 + 预测路径零 Rand 消费。
func TestPrediction_ZeroRand(t *testing.T) {
	// ①a 同 seed 双世界,同一注入 + 同一逐月采样序列 → 轨迹逐位一致。
	w1 := newScenarioTestRoom(t, "pred-det-a", 20261021).World
	w2 := newScenarioTestRoom(t, "pred-det-b", 20261021).World
	if e := applyPredScenario(t, w1); e != nil {
		t.Fatalf("inject w1: %v", e)
	}
	if e := applyPredScenario(t, w2); e != nil {
		t.Fatalf("inject w2: %v", e)
	}
	for i := 0; i < 5; i++ {
		w1.predictionMonthStep()
		w2.predictionMonthStep()
	}
	t1 := w1.Prediction.Runs[len(w1.Prediction.Runs)-1].Trajectory
	t2 := w2.Prediction.Runs[len(w2.Prediction.Runs)-1].Trajectory
	if len(t1) != len(t2) || len(t1) != 6 {
		t.Fatalf("trajectory len = %d/%d, want 6/6", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Fatalf("trajectory[%d] diverged: %+v vs %+v", i, t1[i], t2[i])
		}
	}
	if w1.Prediction.Runs[0].Baseline != w2.Prediction.Runs[0].Baseline {
		t.Fatal("baseline diverged")
	}

	// ①b 预测路径零 Rand 消费:同 seed 双世界,一侧走完整预测路径,随后两侧
	// 首笔 Rand 消费必须一致(若 prediction 消耗 rand 则流错位)。
	r1 := PlaceholderWorld(4242)
	r2 := PlaceholderWorld(4242)
	r1.predictionBeginOrJoin()
	for i := 0; i < 8; i++ {
		r1.predictionMonthStep()
	}
	if r1.Rand.Int63() != r2.Rand.Int63() {
		t.Fatal("prediction path consumed w.Rand (stream diverged)")
	}
}

// TestPrediction_BaselineBeforeShock T0 基线先于冲击:注入后指标已偏离但
// run.Baseline 仍是冲击前值(方案 §3:先拍基线再生效冲击)。
func TestPrediction_BaselineBeforeShock(t *testing.T) {
	r := newScenarioTestRoom(t, "pred-t0", 2026)
	w := r.World
	base := collectPredictionIndicators(w)
	if _, _, e := r.ApplyScenario(ScenarioGoodsPriceShock, 5, 6, "", "shock"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	if w.Prediction == nil || len(w.Prediction.Runs) != 1 {
		t.Fatalf("prediction runs = %+v, want 1 run", w.Prediction)
	}
	run := w.Prediction.Runs[0]
	if run.ID != "PR1" || run.StartMonth != base.Month {
		t.Fatalf("run meta = %s@%d, want PR1@%d", run.ID, run.StartMonth, base.Month)
	}
	// 基线未被冲击污染(冲击前拍下)。
	if run.Baseline.GoodsPriceIdx != base.GoodsPriceIdx {
		t.Fatalf("baseline goods_price_idx = %v, want pre-shock %v", run.Baseline.GoodsPriceIdx, base.GoodsPriceIdx)
	}
	// 当前世界已偏离基线(商品价格冲击 L5 +3%×5)。
	now := collectPredictionIndicators(w)
	if now.GoodsPriceIdx <= run.Baseline.GoodsPriceIdx {
		t.Fatalf("current goods_price_idx %v did not deviate from baseline %v", now.GoodsPriceIdx, run.Baseline.GoodsPriceIdx)
	}
	// T0 轨迹点 = 基线(方案 §2:T0 也记一点)。
	if len(run.Trajectory) != 1 || run.Trajectory[0] != run.Baseline {
		t.Fatalf("trajectory[0] = %+v, want T0 == baseline", run.Trajectory)
	}
	if len(run.Injections) != 1 || run.Injections[0].ID != "SC1" {
		t.Fatalf("injections = %+v, want [SC1]", run.Injections)
	}
}

// TestPrediction_JoinAndNewRun horizon 内并入 / horizon 外新 run + 环形 3。
func TestPrediction_JoinAndNewRun(t *testing.T) {
	r := newScenarioTestRoom(t, "pred-join", 7)
	w := r.World
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "a"); e != nil {
		t.Fatalf("inject1: %v", e)
	}
	// horizon 内并入同 run。
	if _, _, e := r.ApplyScenario(ScenarioMeteorite, 4, 6, "", "b"); e != nil {
		t.Fatalf("inject2: %v", e)
	}
	if w.Prediction == nil || len(w.Prediction.Runs) != 1 {
		t.Fatalf("runs = %+v, want 1", w.Prediction)
	}
	run := w.Prediction.Runs[0]
	if run.ID != "PR1" || len(run.Injections) != 2 {
		t.Fatalf("run %s injections = %+v, want PR1 x2", run.ID, run.Injections)
	}
	if run.Injections[0].ID != "SC1" || run.Injections[1].ID != "SC2" {
		t.Fatalf("injection ids = %s/%s", run.Injections[0].ID, run.Injections[1].ID)
	}
	// horizon 外 → 新 run(T0 基线取当月)。
	w.Month = run.StartMonth + predictionHorizonMonths
	if _, _, e := r.ApplyScenario(ScenarioMarketCrash, 3, 4, "", "c"); e != nil {
		t.Fatalf("inject3: %v", e)
	}
	if len(w.Prediction.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(w.Prediction.Runs))
	}
	nr := w.Prediction.Runs[1]
	if nr.ID != "PR2" || nr.StartMonth != w.Month || len(nr.Injections) != 1 {
		t.Fatalf("new run = %+v, want PR2@%d x1", nr, w.Month)
	}
	// 环形容量 3:再开 2 个 run,最旧 PR1 挤出。
	for i := 0; i < 2; i++ {
		w.Month += predictionHorizonMonths
		if _, _, e := r.ApplyScenario(ScenarioTsunami, 2, 4, "", "ring"); e != nil {
			t.Fatalf("inject ring %d: %v", i, e)
		}
	}
	if len(w.Prediction.Runs) != predictionRunsCap {
		t.Fatalf("runs = %d, want cap %d", len(w.Prediction.Runs), predictionRunsCap)
	}
	if w.Prediction.Runs[0].ID != "PR2" {
		t.Fatalf("oldest kept = %s, want PR1 evicted", w.Prediction.Runs[0].ID)
	}
}

// TestPrediction_TrajectoryCap 轨迹环形上限 36(方案 §3)。
func TestPrediction_TrajectoryCap(t *testing.T) {
	r := newScenarioTestRoom(t, "pred-cap", 11)
	w := r.World
	if _, _, e := r.ApplyScenario(ScenarioSurveyPulse, 2, 6, "", "cap"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	for i := 0; i < predictionTrajectoryCap+10; i++ {
		w.predictionMonthStep()
	}
	run := w.Prediction.Runs[0]
	if len(run.Trajectory) != predictionTrajectoryCap {
		t.Fatalf("trajectory len = %d, want %d", len(run.Trajectory), predictionTrajectoryCap)
	}
}

// TestPrediction_ReportSections 报告必填章节非空 + R1 边界声明存在(方案 §4)。
func TestPrediction_ReportSections(t *testing.T) {
	r := newScenarioTestRoom(t, "pred-report", 20261021)
	w := r.World
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 4, 6, "", "report"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	// 造 3 个月轨迹(含偏离 + 收敛,供 peak/recovery/extrapolation 有料)。
	w.Labor.Unemployment = w.Labor.Unemployment + 0.02
	w.predictionMonthStep()
	w.Labor.Unemployment = w.Labor.Unemployment + 0.01
	w.predictionMonthStep()
	w.Labor.Unemployment = w.Labor.Unemployment - 0.025
	w.predictionMonthStep()
	rep, e := r.PredictionReport("")
	if e != nil {
		t.Fatalf("report err: %v", e)
	}
	if rep == nil {
		t.Fatal("report nil")
	}
	// meta。
	if rep.Meta.RunID != "PR1" || rep.Meta.Tier != PredictionTierR1 {
		t.Fatalf("meta = %+v", rep.Meta)
	}
	if rep.Meta.BaselineSnapshotID == "" || len(rep.Meta.BaselineSnapshotID) != len("b-0123456789abcdef") {
		t.Fatalf("baseline_snapshot_id = %q", rep.Meta.BaselineSnapshotID)
	}
	if rep.Meta.Seed != 20261021 || rep.Meta.StartMonth != 1 {
		t.Fatalf("meta seed/month = %d/%d", rep.Meta.Seed, rep.Meta.StartMonth)
	}
	if rep.Meta.MonthsObserved != 3 {
		t.Fatalf("months_observed = %d, want 3", rep.Meta.MonthsObserved)
	}
	if len(rep.Meta.Injections) != 1 {
		t.Fatalf("meta injections = %+v", rep.Meta.Injections)
	}
	// scenarios。
	if len(rep.Scenarios) != 1 || rep.Scenarios[0].Kind != ScenarioEarthquake ||
		rep.Scenarios[0].Name != "地震" || rep.Scenarios[0].Group != ScenarioGroupNature ||
		rep.Scenarios[0].Decay == "" {
		t.Fatalf("scenarios = %+v", rep.Scenarios)
	}
	// first_month_impact(有偏离的指标非空;T0+1 vs 基线)。
	if len(rep.FirstMonthImpact) == 0 {
		t.Fatal("first_month_impact empty")
	}
	foundUnemp := false
	for _, im := range rep.FirstMonthImpact {
		if im.Metric == "unemployment" && im.DeltaPct > 0 && im.Text != "" && im.Unit == "ratio" {
			foundUnemp = true
		}
	}
	if !foundUnemp {
		t.Fatalf("first_month_impact missing unemployment: %+v", rep.FirstMonthImpact)
	}
	// trajectory(双线:values + baseline;T0 点 delta 0)。
	if len(rep.Trajectory) != 4 {
		t.Fatalf("trajectory = %d points, want 4", len(rep.Trajectory))
	}
	if len(rep.Trajectory[0].Values) != len(predictionMetrics) || len(rep.Trajectory[0].Baseline) != len(predictionMetrics) {
		t.Fatalf("trajectory values/baseline keys = %d/%d, want %d",
			len(rep.Trajectory[0].Values), len(rep.Trajectory[0].Baseline), len(predictionMetrics))
	}
	// peak_recovery(失业率偏离非零)。
	if len(rep.PeakRecovery) == 0 {
		t.Fatal("peak_recovery empty")
	}
	// transmission(地震链路 + 归一贡献 + dominant)。
	if len(rep.Transmission.Chains) == 0 {
		t.Fatal("transmission chains empty")
	}
	sum := 0.0
	dominant := 0
	for _, ch := range rep.Transmission.Chains {
		sum += ch.Contribution
		if ch.Dominant {
			dominant++
		}
		if ch.Chain == "" || ch.Evidence == "" {
			t.Fatalf("chain missing text: %+v", ch)
		}
	}
	if dominant != 1 {
		t.Fatalf("dominant count = %d, want 1", dominant)
	}
	if sum < 0.99 || sum > 1.01 {
		t.Fatalf("contribution sum = %v, want ~1", sum)
	}
	if rep.Transmission.Note == "" {
		t.Fatal("transmission note empty")
	}
	// distribution / extrapolation / resilience / boundaries。
	if rep.Distribution.Text == "" {
		t.Fatal("distribution text empty")
	}
	if len(rep.Extrapolation) != 12 {
		t.Fatalf("extrapolation = %d, want 12 (3 horizons x 4 metrics)", len(rep.Extrapolation))
	}
	for _, ex := range rep.Extrapolation {
		if ex.Lo > ex.Point || ex.Point > ex.Hi {
			t.Fatalf("extrapolation band broken: %+v", ex)
		}
	}
	if !rep.Resilience.Applicable {
		t.Fatal("earthquake run must set resilience.applicable")
	}
	if rep.Resilience.RecoveryScore < 0 || rep.Resilience.RecoveryScore > 100 ||
		rep.Resilience.AdaptationScore < 0 || rep.Resilience.AdaptationScore > 100 ||
		rep.Resilience.TransformScore < 0 || rep.Resilience.TransformScore > 100 {
		t.Fatalf("resilience scores out of range: %+v", rep.Resilience)
	}
	if len(rep.Resilience.Basis) == 0 {
		t.Fatal("resilience basis empty")
	}
	// R1 边界声明必须存在(方案 §1:显式声明观测对照)。
	foundR1 := false
	for _, b := range rep.Boundaries {
		if len(b) >= 2 && b[:2] == "R1" {
			foundR1 = true
		}
	}
	if !foundR1 {
		t.Fatalf("boundaries missing R1 declaration: %v", rep.Boundaries)
	}
	// 市场类场景 resilience 不适用。
	r2 := newScenarioTestRoom(t, "pred-report2", 20261021)
	if _, _, e := r2.ApplyScenario(ScenarioMarketCrash, 3, 4, "", ""); e != nil {
		t.Fatalf("inject2: %v", e)
	}
	rep2, _ := r2.PredictionReport("")
	if rep2 == nil || rep2.Resilience.Applicable {
		t.Fatalf("market_crash resilience must not apply: %+v", rep2.Resilience)
	}
}

// TestPrediction_RecoveryMonth 收敛序列的恢复月判定(方案 §4 规则3)。
func TestPrediction_RecoveryMonth(t *testing.T) {
	m := predictionMetricFor("unemployment")
	base := PredictionIndicators{Month: 40, Unemployment: 0.05}
	run := &PredictionRun{
		ID: "PR1", StartMonth: 40, Baseline: base,
		Trajectory: []PredictionIndicators{
			base,                              // T0
			{Month: 41, Unemployment: 0.062},  // +24% 峰值
			{Month: 42, Unemployment: 0.056},  // +12%
			{Month: 43, Unemployment: 0.052},  // +4% 入带
			{Month: 44, Unemployment: 0.049},  // 带内
			{Month: 45, Unemployment: 0.0505}, // 带内(±5%)
		},
		Injections: []PredictionInjection{{
			ID: "SC1", Kind: ScenarioEarthquake, Name: "地震", Group: ScenarioGroupNature,
			Magnitude: 3, DurationMonths: 6, AppliedMonth: 40,
		}},
	}
	peak := predictionPeakFor(run, *m)
	if peak.PeakMonth != 41 || peak.PeakDevPct != 24 {
		t.Fatalf("peak = %+v, want month 41 dev +24", peak)
	}
	if !peak.Recovered || peak.RecoveryMonth == nil {
		t.Fatalf("peak not recovered: %+v", peak)
	}
	if *peak.RecoveryMonth != 43 {
		t.Fatalf("recovery_month = %d, want 43(入带后不再出带)", *peak.RecoveryMonth)
	}

	// 末点出带 → 未恢复(recovery_month = null)。
	run2 := &PredictionRun{
		ID: "PR1", StartMonth: 40, Baseline: base,
		Trajectory: []PredictionIndicators{
			base,
			{Month: 41, Unemployment: 0.062},
			{Month: 42, Unemployment: 0.070}, // 仍在峰值外
		},
	}
	peak2 := predictionPeakFor(run2, *m)
	if peak2.Recovered || peak2.RecoveryMonth != nil {
		t.Fatalf("expect unrecovered: %+v", peak2)
	}
}

// TestPrediction_NoRun 无 run → report nil 不 panic;run_id 未命中 → 35058。
func TestPrediction_NoRun(t *testing.T) {
	r := newScenarioTestRoom(t, "pred-norun", 5)
	// 未注入:runs 空数组、report null、错误 nil(方案 §5:不报错)。
	runs := r.PredictionRuns()
	if runs == nil || len(runs) != 0 {
		t.Fatalf("runs = %#v, want []", runs)
	}
	rep, e := r.PredictionReport("")
	if rep != nil || e != nil {
		t.Fatalf("report = %+v err = %v, want (nil, nil)", rep, e)
	}
	// 指定不存在的 run_id → 35058(方案 §5)。
	if _, e := r.PredictionReport("PR99"); e == nil || e.Code != errcode.ErrVirtualCityPredictionRunNotFound {
		t.Fatalf("PR99 err = %v, want 35058", e)
	}
	// 有 run 后:未命中的 run_id 仍 35058;命中/缺省出报告。
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", ""); e != nil {
		t.Fatalf("inject: %v", e)
	}
	if _, e := r.PredictionReport("PR77"); e == nil || e.Code != errcode.ErrVirtualCityPredictionRunNotFound {
		t.Fatalf("PR77 err = %v, want 35058", e)
	}
	rep, e = r.PredictionReport("PR1")
	if e != nil || rep == nil || rep.Meta.RunID != "PR1" {
		t.Fatalf("PR1 report = %+v err = %v", rep, e)
	}
	// 注入失败(未知 kind)不得开 run(基线只在有效注入时拍)。
	r2 := newScenarioTestRoom(t, "pred-norun2", 5)
	if _, _, e := r2.ApplyScenario("no_such_kind", 3, 6, "", ""); e == nil {
		t.Fatal("unknown kind must fail")
	}
	if len(r2.PredictionRuns()) != 0 {
		t.Fatal("failed injection must not open a prediction run")
	}
}
