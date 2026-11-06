// Package virtual_city — event_lifecycle_test.go: 批次86 事件生命周期留痕单测
// (2026-11-06 §可靠性与可观测性 §3.1)。
//
// 覆盖(契约 §7.1 硬性断言):
//   - 4 阶段留痕完整(触发/决策/执行/结果逐字段非空);
//   - 留痕环 cap 50,超出丢最旧;
//   - panic ⇒ 快照恢复 + rolled_back 标记(注入 panic 探针);
//   - 回放:恢复快照 → 重放注入 → StateHashAfter 与原 trace 一致;
//   - nil trace ⇒ 零回归(不写留痕环);
//   - 房间层查询:limit / 精确查询 / 副本语义 / nil 安全;
//   - 房间层安全阀逐闸门留痕(幂等 / 每月条数 / 强度上限)。
package virtual_city

import (
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// elGateNames 是 applyScenarioToWorldTraced 的 5 道决策闸门(契约 §3.1.3 表)。
var elGateNames = []string{
	EventGateUnknownKind,   // scenarioDefFor 命中
	EventGateMagnitudeLimit, // 1..5
	EventGateParams,         // 持续月数 1..24
	EventGateActiveLimit,    // < scenarioMaxActive
	EventGateParams,         // target 合法
}

// TestApplyScenarioTraced_FourPhases 一次成功注入的留痕必须四阶段完整。
func TestApplyScenarioTraced_FourPhases(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "el-phases", 20261106)
	rec, effects, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "four-phases")
	if e != nil {
		t.Fatalf("inject: %v", e)
	}
	if rec == nil || len(effects) == 0 {
		t.Fatalf("inject returned no record/effects: rec=%v effects=%d", rec, len(effects))
	}
	traces := r.EventLifecycleTraces(0)
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	tr := traces[0]
	// ① 触发
	if tr.TraceID == "" || tr.TriggeredAt == 0 {
		t.Errorf("trigger identity empty: %+v", tr)
	}
	if tr.TriggerSource != TriggerSourceManual {
		t.Errorf("trigger_source = %q, want %q", tr.TriggerSource, TriggerSourceManual)
	}
	if tr.TriggerDetail == "" {
		t.Errorf("trigger_detail empty")
	}
	if tr.ScenarioKind != ScenarioEarthquake {
		t.Errorf("scenario_kind = %q", tr.ScenarioKind)
	}
	// ② 决策
	if tr.DecisionOutcome != EventDecisionPass {
		t.Errorf("decision_outcome = %q", tr.DecisionOutcome)
	}
	if len(tr.DecisionGates) != len(elGateNames) {
		t.Fatalf("gates = %d, want %d: %+v", len(tr.DecisionGates), len(elGateNames), tr.DecisionGates)
	}
	for i, g := range tr.DecisionGates {
		if g.Gate != elGateNames[i] {
			t.Errorf("gate[%d] = %q, want %q", i, g.Gate, elGateNames[i])
		}
		if !g.Passed {
			t.Errorf("gate[%d](%s) must pass", i, g.Gate)
		}
	}
	// ③ 执行
	if tr.ExecutionMagnitude != 3 {
		t.Errorf("execution_magnitude = %d, want 3", tr.ExecutionMagnitude)
	}
	if tr.ExecutionDuration != 6 {
		t.Errorf("execution_duration = %d, want 6", tr.ExecutionDuration)
	}
	if tr.ExecutionTarget != "" {
		t.Errorf("execution_target = %q, want empty", tr.ExecutionTarget)
	}
	if len(tr.ExecutionEffects) != len(effects) {
		t.Errorf("execution_effects = %d, want %d", len(tr.ExecutionEffects), len(effects))
	}
	// ④ 结果
	if tr.ResultOutcome != EventResultSuccess {
		t.Errorf("result_outcome = %q", tr.ResultOutcome)
	}
	if tr.ResultErrCode != 0 {
		t.Errorf("result_err_code = %d, want 0", tr.ResultErrCode)
	}
	if tr.StateHashBefore == "" || tr.StateHashAfter == "" {
		t.Errorf("state hash before/after empty: %q / %q", tr.StateHashBefore, tr.StateHashAfter)
	}
	if tr.SnapshotID == "" {
		t.Errorf("snapshot_id empty (auto_rollback snapshot must be taken)")
	}
}

// TestApplyScenarioToWorld_NilTraceZeroRegression nil trace ⇒ 与既有
// applyScenarioToWorld 逐字一致(不写留痕环)。
func TestApplyScenarioToWorld_NilTraceZeroRegression(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "el-niltrace", 20261106)
	if _, _, _, e := applyScenarioToWorld(r.World, ScenarioEarthquake, 2, 6, "", "nil-trace",
		time.Now().UnixMilli(), r.seed); e != nil {
		t.Fatalf("inject: %v", e)
	}
	if len(r.World.EventLifecycleLog) != 0 {
		t.Fatalf("nil trace must not write lifecycle log: %d", len(r.World.EventLifecycleLog))
	}
}

// TestEventLifecycleLog_RingCap50 留痕环 cap 50,超出丢最旧(新→旧)。
func TestEventLifecycleLog_RingCap50(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "el-cap", 20261106)
	const n = eventLifecycleLogCap + 5
	for i := 0; i < n; i++ {
		if _, _, e := r.ApplyScenario(ScenarioSurveyPulse, 1, 1, "", "cap"); e != nil {
			t.Fatalf("inject #%d: %v", i+1, e)
		}
		// 持续 1 月 ⇒ 本次即到期,腾出活动名额(否则第 9 次起被 active_limit 拦)。
		r.MuLock()
		r.World.scenarioMonthStep()
		r.MuUnlock()
	}
	traces := r.EventLifecycleTraces(0)
	if len(traces) != eventLifecycleLogCap {
		t.Fatalf("traces = %d, want cap %d", len(traces), eventLifecycleLogCap)
	}
	// 新→旧:首条 TriggeredAt 应 ≥ 末条。
	if traces[0].TriggeredAt < traces[len(traces)-1].TriggeredAt {
		t.Errorf("ring order broken: first=%d last=%d", traces[0].TriggeredAt, traces[len(traces)-1].TriggeredAt)
	}
	seen := map[string]bool{}
	for _, tr := range traces {
		if seen[tr.TraceID] {
			t.Fatalf("duplicate trace id %q", tr.TraceID)
		}
		seen[tr.TraceID] = true
	}
}

// TestEventLifecycleTraceQueries 查询:limit / 精确命中 / 副本 / nil 安全。
func TestEventLifecycleTraceQueries(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "el-query", 20261106)
	for i := 0; i < 3; i++ {
		if _, _, e := r.ApplyScenario(ScenarioSurveyPulse, 1, 1, "", "q"); e != nil {
			t.Fatalf("inject #%d: %v", i+1, e)
		}
		r.MuLock()
		r.World.scenarioMonthStep()
		r.MuUnlock()
	}
	all := r.EventLifecycleTraces(0)
	if len(all) != 3 {
		t.Fatalf("traces = %d, want 3", len(all))
	}
	limited := r.EventLifecycleTraces(2)
	if len(limited) != 2 {
		t.Fatalf("limit=2 got %d", len(limited))
	}
	if limited[0].TraceID != all[0].TraceID {
		t.Errorf("limit must keep 新→旧 order")
	}
	// 精确查询 + 副本语义。
	got := r.EventLifecycleTraceByID(all[1].TraceID)
	if got == nil || got.TraceID != all[1].TraceID {
		t.Fatalf("byID miss: %v", got)
	}
	got.ResultOutcome = "mutated"
	got.DecisionGates[0].Gate = "mutated"
	again := r.EventLifecycleTraceByID(all[1].TraceID)
	if again.ResultOutcome == "mutated" || again.DecisionGates[0].Gate == "mutated" {
		t.Fatalf("byID must return a deep copy")
	}
	if r.EventLifecycleTraceByID("no-such-trace") != nil {
		t.Errorf("unknown id must return nil")
	}
	// nil 安全:未开局房间返回非 nil 空切片。
	empty := NewVirtualCityRoom("el-empty", 3000, 1, 4)
	if got := empty.EventLifecycleTraces(0); got == nil || len(got) != 0 {
		t.Errorf("empty room traces must be non-nil [], got %#v", got)
	}
	if empty.EventLifecycleTraceByID("x") != nil {
		t.Errorf("empty room byID must be nil")
	}
}

// TestApplyScenarioTraced_PanicRollback 注入 panic ⇒ 快照恢复 + rolled_back
// (契约 §7.1「panic ⇒ 快照恢复 + rolled_back 标记」)。
//
// panic 探针:把 World.Goods 换成「Items 有 food 项、SupplyIdx 为 nil」的
// 损坏态 ⇒ 商品供给冲击落到 nil map 赋值,确定性 panic(模拟引擎内部异常
// 导致的半改写状态)。
func TestApplyScenarioTraced_PanicRollback(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "el-panic", 20261106)
	w := r.World
	w.Goods = &GoodsMarket{Items: map[string]*GoodsItem{
		"food": {ID: "food", Weight: 0.2, PriceIdx: 100},
	}}
	before := w.StateHash()
	mcBefore := len(w.MCEvents)
	dropBefore := w.EventDropped
	predBefore := w.Prediction

	trace := newScenarioInjectionTrace(r.RoomID, TriggerSourceManual, "panic-probe")
	var panicked any
	var injectErr *errcode.Error
	func() {
		defer func() { panicked = recover() }()
		_, _, _, injectErr = applyScenarioToWorldTraced(w, ScenarioGoodsPriceShock, 2, 6, "", "probe",
			time.Now().UnixMilli(), r.seed, trace)
	}()
	if panicked == nil {
		t.Fatalf("expected panic from nil SupplyIdx map (err=%v)", injectErr)
	}
	// 结果标记
	if trace.ResultOutcome != EventResultRolledBack {
		t.Errorf("result_outcome = %q, want %q", trace.ResultOutcome, EventResultRolledBack)
	}
	// 状态已回滚
	if got := w.StateHash(); got != before {
		t.Errorf("state not rolled back: before=%s after=%s", before, got)
	}
	if len(w.MCEvents) != mcBefore {
		t.Errorf("MCEvents not rolled back: %d → %d", mcBefore, len(w.MCEvents))
	}
	if w.EventDropped != dropBefore {
		t.Errorf("EventDropped not rolled back: %d → %d", dropBefore, w.EventDropped)
	}
	if w.Prediction != predBefore {
		t.Errorf("Prediction not rolled back: %v → %v", predBefore, w.Prediction)
	}
	if len(w.Scenarios) != 0 {
		t.Errorf("scenario must be rolled back: %d", len(w.Scenarios))
	}
	// panic 路径也留痕(环内首条)
	if len(w.EventLifecycleLog) != 1 {
		t.Fatalf("panic path must append trace: %d", len(w.EventLifecycleLog))
	}
	got := w.EventLifecycleLog[0]
	if got.TraceID != trace.TraceID {
		t.Errorf("ring trace id = %q, want %q", got.TraceID, trace.TraceID)
	}
	if got.ResultOutcome != EventResultRolledBack {
		t.Errorf("ring trace outcome = %q", got.ResultOutcome)
	}
	if got.SnapshotID == "" {
		t.Errorf("ring trace snapshot_id empty")
	}
}

// TestReplayScenarioTrace 回放:恢复快照 → 重放注入 → StateHashAfter 一致
// (契约 §3.1.5 / §7.1「快照 round-trip 后 StateHash 一致」)。
func TestReplayScenarioTrace(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := newScenarioTestRoom(t, "el-replay", 20261106)
	before := r.World.StateHash()
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "orig"); e != nil {
		t.Fatalf("inject: %v", e)
	}
	after := r.World.StateHash()
	if before == after {
		t.Fatal("injection must change state hash")
	}
	traces := r.EventLifecycleTraces(0)
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	orig := traces[0]
	replayed, match, e := r.ReplayScenarioTrace(orig.TraceID)
	if e != nil {
		t.Fatalf("replay: %v", e)
	}
	if !match {
		t.Errorf("replay hash mismatch: original=%s replayed=%s", orig.StateHashAfter, replayed.StateHashAfter)
	}
	if replayed.TriggerSource != TriggerSourceReplay {
		t.Errorf("replay source = %q, want %q", replayed.TriggerSource, TriggerSourceReplay)
	}
	if got := r.World.StateHash(); got != after {
		t.Errorf("world after replay = %s, want %s", got, after)
	}
	if _, _, e := r.ReplayScenarioTrace("no-such-trace"); e == nil ||
		e.Code != errcode.ErrVirtualCityEventTraceNotFound {
		t.Errorf("unknown trace: got %v, want code %d", e, errcode.ErrVirtualCityEventTraceNotFound)
	}
}

// TestWorldEvent_SafetyValveTraces 房间层安全阀逐闸门留痕(批次86 §3.1.4)。
func TestWorldEvent_SafetyValveTraces(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := weRoom(t, "el-valve", 20261106)
	// 强度上限 1 ⇒ R7 地震事件(L2)被 magnitude_limit 拦截。
	r.SetAutoScenarioLimits(true, 2, 1)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource([]WorldEventInput{{
		News: []WorldEventNews{
			{ID: "n1", Title: "某地发生6级地震", Source: "测试", PublishedAt: now},
		},
	}}))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	traces := r.EventLifecycleTraces(0)
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	tr := traces[0]
	if tr.TriggerSource != TriggerSourceRealWorldRule {
		t.Errorf("trigger_source = %q, want %q", tr.TriggerSource, TriggerSourceRealWorldRule)
	}
	if tr.DecisionOutcome != EventDecisionReject {
		t.Errorf("decision_outcome = %q, want reject", tr.DecisionOutcome)
	}
	if tr.ResultOutcome != EventResultFailed {
		t.Errorf("result_outcome = %q, want failed", tr.ResultOutcome)
	}
	if len(tr.DecisionGates) != 1 || tr.DecisionGates[0].Gate != EventGateMagnitudeLimit {
		t.Fatalf("gates = %+v, want single magnitude_limit", tr.DecisionGates)
	}
	if tr.DecisionGates[0].Passed {
		t.Errorf("magnitude gate must be failed")
	}
	if tr.EventKey == "" {
		t.Errorf("event_key empty")
	}
}

// TestWorldEvent_PassTraceHasRoomAndEngineGates 通过安全阀的事件:房间层 4 道
// 通过闸门 + 引擎 5 道闸门,共 9 道,全部 passed。
func TestWorldEvent_PassTraceHasRoomAndEngineGates(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := weRoom(t, "el-valve-pass", 20261106)
	r.SetAutoScenarioLimits(true, 2, 3)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	traces := r.EventLifecycleTraces(0)
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	tr := traces[0]
	if tr.ResultOutcome != EventResultSuccess {
		t.Fatalf("result_outcome = %q, want success", tr.ResultOutcome)
	}
	if len(tr.DecisionGates) != 9 {
		t.Fatalf("gates = %d, want 9 (4 room + 5 engine): %+v", len(tr.DecisionGates), tr.DecisionGates)
	}
	for i, g := range tr.DecisionGates {
		if !g.Passed {
			t.Errorf("gate[%d](%s) must pass", i, g.Gate)
		}
	}
}

// TestWorldEvent_IdempotencyTrace 幂等闸门留痕:同一事件第二次评估被拦。
func TestWorldEvent_IdempotencyTrace(t *testing.T) {
	SetSnapshotDir(t.TempDir())
	r := weRoom(t, "el-valve-idem", 20261106)
	r.SetAutoScenarioLimits(true, 2, 3)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.refreshWorldEventsLocked() // 第二次:同一指纹 ⇒ 幂等拦截
	r.mu.Unlock()
	traces := r.EventLifecycleTraces(0)
	if len(traces) != 2 {
		t.Fatalf("traces = %d, want 2", len(traces))
	}
	// 环为新→旧 ⇒ 首条是第二次(幂等拦截)。
	if traces[0].DecisionGates[0].Gate != EventGateIdempotency {
		t.Errorf("latest trace gate = %q, want idempotency", traces[0].DecisionGates[0].Gate)
	}
	if traces[0].DecisionGates[0].Passed {
		t.Errorf("idempotency gate must be failed")
	}
	if traces[1].ResultOutcome != EventResultSuccess {
		t.Errorf("first trace outcome = %q, want success", traces[1].ResultOutcome)
	}
}
