// Package virtual_city — room_world_event_test.go: 批次80 房间层自动注入单测
// (2026-10-23 §真实世界事件自动注入引擎)。
//
// 覆盖:零回归(无事件源 / 空源 ⇒ 两次 SettleMonth 逐位相等)/ 注入链路
// (ScenarioApplied +1 且 WorldEventLog +1)/ 幂等(同 key 不重复注入)/
// 安全阀(条数上限 / 强度上限 / 同 kind 本月去重 / 活动场景满额 ⇒
// 跳过且 Injected=false)/ prompt 段有·无。
package virtual_city

import (
	"fmt"
	"testing"
	"time"
)

// weRoom 构造开局中的 10 bot 房间(与 scenario_test 同款装配)。
func weRoom(t *testing.T, roomID string, seed int64) *VirtualCityRoom {
	t.Helper()
	r := NewVirtualCityRoom(roomID, 3000, seed, 4)
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = fmt.Sprintf("bot-%d", seat)
		botModels[seat] = "TestModel"
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start room: %v", e)
	}
	return r
}

// weSource 构造恒定返回指定输入的事件源(纯内存快照,不回调房间)。
func weSource(in []WorldEventInput) func() []WorldEventInput {
	return func() []WorldEventInput { return in }
}

// weCrashInput 构造触发 R1(股市剧震)的行情输入。
func weCrashInput(now int64) []WorldEventInput {
	return []WorldEventInput{{
		Quotes: []WorldEventQuote{
			{Category: "a_share", Symbol: "000001", Name: "上证", ChangePct: -3.0, UpdatedAt: now},
			{Category: "us", Symbol: "SPX", Name: "标普", ChangePct: -3.0, UpdatedAt: now},
		},
	}}
}

// weStateDigest 世界状态摘要(逐位比较用)。
func weStateDigest(w *World) string {
	s := fmt.Sprintf("month=%d stock=%.10f gold=%.10f house=%.10f unemp=%.10f cpi=%.10f cash0=%d",
		w.Month, w.Market.StockIndex, w.Market.GoldPrice, w.Market.DistrictIdx["finance"],
		w.Labor.Unemployment, w.Goods.CPIYoY, w.Players[0].Cash)
	for _, p := range w.Players {
		if p == nil {
			continue
		}
		s += fmt.Sprintf("|p%d:%d:%d", p.Seat, p.Cash, p.NetWorth(w.Market))
	}
	return s
}

// TestWorldEvent_ZeroRegression_NoSource 无事件源 ⇒ 固定 seed 两次 SettleMonth
// 逐位相等(零回归硬契约)。
func TestWorldEvent_ZeroRegression_NoSource(t *testing.T) {
	r1 := weRoom(t, "we-zr-a", 20261023)
	r2 := weRoom(t, "we-zr-b", 20261023)
	for i := 0; i < 3; i++ {
		r1.World.SettleMonth()
		r2.World.SettleMonth()
	}
	if weStateDigest(r1.World) != weStateDigest(r2.World) {
		t.Fatalf("zero regression broken (no source):\n%s\n%s",
			weStateDigest(r1.World), weStateDigest(r2.World))
	}
	if len(r1.World.WorldEventLog) != 0 {
		t.Errorf("no source must leave event log empty: %d", len(r1.World.WorldEventLog))
	}
	if len(r1.World.ScenarioApplied) != 0 {
		t.Errorf("no source must not inject any scenario: %d", len(r1.World.ScenarioApplied))
	}
}

// TestWorldEvent_ZeroRegression_EmptySource 源返回空 ⇒ 与无源逐位相等。
func TestWorldEvent_ZeroRegression_EmptySource(t *testing.T) {
	r1 := weRoom(t, "we-zr-c", 20261023)
	r2 := weRoom(t, "we-zr-d", 20261023)
	r2.SetWorldEventSource(weSource(nil))
	for i := 0; i < 3; i++ {
		r1.World.SettleMonth()
		r2.World.SettleMonth()
		r2.refreshWorldEventsLocked()
	}
	if weStateDigest(r1.World) != weStateDigest(r2.World) {
		t.Fatalf("zero regression broken (empty source):\n%s\n%s",
			weStateDigest(r1.World), weStateDigest(r2.World))
	}
	if len(r2.World.WorldEventLog) != 0 {
		t.Errorf("empty source must leave event log empty: %d", len(r2.World.WorldEventLog))
	}
}

// TestWorldEvent_InjectsOnSettle 命中事件 ⇒ 注入 1 条场景 + 日志 1 条。
func TestWorldEvent_InjectsOnSettle(t *testing.T) {
	r := weRoom(t, "we-inject", 20261023)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	appliedBefore := len(r.World.ScenarioApplied)
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != appliedBefore+1 {
		t.Fatalf("scenario applied = %d, want %d", len(r.World.ScenarioApplied), appliedBefore+1)
	}
	if len(r.World.WorldEventLog) != 1 {
		t.Fatalf("event log len = %d, want 1", len(r.World.WorldEventLog))
	}
	rec := r.World.WorldEventLog[0]
	if !rec.Injected {
		t.Errorf("event must be injected: %+v", rec)
	}
	if rec.Rule != "R1" || rec.ScenarioKind != ScenarioMarketCrash {
		t.Errorf("event mapping wrong: %+v", rec)
	}
	if rec.InjectedMonth != r.World.worldEventEffectiveMonth() {
		t.Errorf("injected month = %d, want %d (effective month)",
			rec.InjectedMonth, r.World.worldEventEffectiveMonth())
	}
	if len(rec.Effects) == 0 {
		t.Errorf("injected event must carry immediate effects: %+v", rec)
	}
}

// TestWorldEvent_Idempotent 同一 key 二次派生 ⇒ 不重复注入。
func TestWorldEvent_Idempotent(t *testing.T) {
	r := weRoom(t, "we-idem", 20261023)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != 1 {
		t.Fatalf("same key must inject only once: %d scenarios", len(r.World.ScenarioApplied))
	}
	if len(r.World.WorldEventLog) != 1 {
		t.Fatalf("same key must log only once: %d records", len(r.World.WorldEventLog))
	}
}

// TestWorldEvent_SafetyValve_PerMonthLimit 每月条数上限 ⇒ 跳过且 Injected=false。
func TestWorldEvent_SafetyValve_PerMonthLimit(t *testing.T) {
	r := weRoom(t, "we-valve-cnt", 20261023)
	r.SetAutoScenarioLimits(true, 1, 3)
	now := time.Now().Unix()
	// 两条不同 kind 的事件(股市剧震 + 大宗通胀)⇒ 只注入 1 条。
	in := []WorldEventInput{{
		Quotes: []WorldEventQuote{
			{Category: "a_share", ChangePct: -3.0, UpdatedAt: now},
			{Category: "futures", ChangePct: 5.0, UpdatedAt: now},
		},
	}}
	r.SetWorldEventSource(weSource(in))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != 1 {
		t.Fatalf("per-month limit broken: %d scenarios injected", len(r.World.ScenarioApplied))
	}
	if len(r.World.WorldEventLog) != 2 {
		t.Fatalf("event log len = %d, want 2 (both observed)", len(r.World.WorldEventLog))
	}
	injected := 0
	for _, rec := range r.World.WorldEventLog {
		if rec.Injected {
			injected++
		}
	}
	if injected != 1 {
		t.Errorf("injected count = %d, want 1 (second blocked by limit)", injected)
	}
}

// TestWorldEvent_SafetyValve_MagnitudeLimit 强度上限 ⇒ 跳过且 Injected=false。
func TestWorldEvent_SafetyValve_MagnitudeLimit(t *testing.T) {
	r := weRoom(t, "we-valve-mag", 20261023)
	// 强度上限 1 ⇒ R7 的 L2 事件超限 ⇒ 跳过。
	r.SetAutoScenarioLimits(true, 2, 1)
	now := time.Now().Unix()
	in := []WorldEventInput{{
		News: []WorldEventNews{
			{ID: "n1", Title: "某地发生6级地震", Source: "测试", PublishedAt: now},
		},
	}}
	r.SetWorldEventSource(weSource(in))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != 0 {
		t.Fatalf("magnitude limit broken: %d scenarios injected", len(r.World.ScenarioApplied))
	}
	if len(r.World.WorldEventLog) != 1 {
		t.Fatalf("event log len = %d, want 1", len(r.World.WorldEventLog))
	}
	if r.World.WorldEventLog[0].Injected {
		t.Errorf("over-magnitude event must be recorded as not injected: %+v", r.World.WorldEventLog[0])
	}
}

// TestWorldEvent_SafetyValve_SameKindPerMonth 同 kind 本月只注入一次
// (R1/R2/R3 同映射 market_crash)。
func TestWorldEvent_SafetyValve_SameKindPerMonth(t *testing.T) {
	r := weRoom(t, "we-valve-kind", 20261023)
	now := time.Now().Unix()
	// 同时触发 R1(股市)与 R2(黄金)⇒ 同映射 market_crash ⇒ 只注入 1 条。
	in := []WorldEventInput{{
		Quotes: []WorldEventQuote{
			{Category: "a_share", ChangePct: -3.0, UpdatedAt: now},
			{Category: "metal", ChangePct: 3.0, UpdatedAt: now},
		},
	}}
	r.SetWorldEventSource(weSource(in))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != 1 {
		t.Fatalf("same-kind valve broken: %d scenarios injected", len(r.World.ScenarioApplied))
	}
	if len(r.World.WorldEventLog) != 2 {
		t.Fatalf("event log len = %d, want 2", len(r.World.WorldEventLog))
	}
	injected := 0
	for _, rec := range r.World.WorldEventLog {
		if rec.Injected {
			injected++
		}
	}
	if injected != 1 {
		t.Errorf("injected count = %d, want 1 (same kind blocked)", injected)
	}
}

// TestWorldEvent_SafetyValve_ActiveScenarioLimit 活动场景满额 ⇒ 跳过。
func TestWorldEvent_SafetyValve_ActiveScenarioLimit(t *testing.T) {
	r := weRoom(t, "we-valve-active", 20261023)
	// 预填满额活动场景(场景引擎上限 8)。
	for i := 0; i < scenarioMaxActive; i++ {
		r.World.Scenarios = append(r.World.Scenarios, &ScenarioActive{
			ID: "SC" + itoaInt(i), Kind: ScenarioSurveyPulse, Magnitude: 1,
			TotalMonths: 6, RemainingMonths: 6, AppliedMonth: 1,
		})
	}
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != 0 {
		t.Fatalf("active-scenario valve broken: %d scenarios injected", len(r.World.ScenarioApplied))
	}
	if len(r.World.WorldEventLog) != 1 || r.World.WorldEventLog[0].Injected {
		t.Errorf("blocked event must be logged as not injected: %+v", r.World.WorldEventLog)
	}
}

// TestWorldEvent_DisabledSwitch 自动注入总开关关闭 ⇒ 不派生、不注入。
func TestWorldEvent_DisabledSwitch(t *testing.T) {
	r := weRoom(t, "we-off", 20261023)
	r.SetAutoScenarioLimits(false, 2, 3)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.ScenarioApplied) != 0 {
		t.Fatalf("disabled switch must not inject: %d", len(r.World.ScenarioApplied))
	}
	if len(r.World.WorldEventLog) != 0 {
		t.Errorf("disabled switch must not log: %d", len(r.World.WorldEventLog))
	}
}

// TestWorldEvent_BlockedEventRetriesWhenCapacityFrees 被安全阀拦下的事件
// 不写幂等集合 ⇒ 下月额度释放后可补注入;日志同 key 原地更新(不重复行)。
func TestWorldEvent_BlockedEventRetriesWhenCapacityFrees(t *testing.T) {
	r := weRoom(t, "we-retry", 20261023)
	// 每月上限 1 条 ⇒ 两条候选中只有第一条注入,第二条被拦下。
	r.SetAutoScenarioLimits(true, 1, 3)
	now := time.Now().Unix()
	in := []WorldEventInput{{
		Quotes: []WorldEventQuote{
			{Category: "a_share", ChangePct: -3.0, UpdatedAt: now},
			{Category: "futures", ChangePct: 5.0, UpdatedAt: now},
		},
	}}
	r.SetWorldEventSource(weSource(in))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.WorldEventLog) != 2 {
		t.Fatalf("event log len = %d, want 2", len(r.World.WorldEventLog))
	}
	blocked := 0
	for _, rec := range r.World.WorldEventLog {
		if !rec.Injected {
			blocked++
		}
	}
	if blocked != 1 {
		t.Fatalf("blocked count = %d, want 1", blocked)
	}
	// 下月(额度重置)⇒ 被拦下的一条补注入,日志仍 2 行(同 key 原地更新)。
	r.World.Month++
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.WorldEventLog) != 2 {
		t.Fatalf("event log must not grow on retry: %d", len(r.World.WorldEventLog))
	}
	if len(r.World.ScenarioApplied) != 2 {
		t.Fatalf("blocked event must be injected after capacity frees: %d",
			len(r.World.ScenarioApplied))
	}
	injected := 0
	for _, rec := range r.World.WorldEventLog {
		if rec.Injected {
			injected++
		}
	}
	if injected != 2 {
		t.Errorf("injected count = %d, want 2", injected)
	}
}

// TestWorldEvent_FeedbackChain 注入后一次月结 ⇒ outcome 回填且字段非零。
func TestWorldEvent_FeedbackChain(t *testing.T) {
	r := weRoom(t, "we-feedback", 20261023)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	if len(r.World.WorldEventLog) != 1 || !r.World.WorldEventLog[0].Injected {
		t.Fatalf("setup failed: %+v", r.World.WorldEventLog)
	}
	r.World.SettleMonth()
	o := r.World.WorldEventLog[0].Outcome
	if o == nil {
		t.Fatal("outcome must be filled after a month settle")
	}
	if o.StockIndex == 0 || o.GoldPrice == 0 || o.Unemployment == 0 {
		t.Errorf("outcome fields must be non-zero: %+v", o)
	}
	if o.Month != r.World.Month {
		t.Errorf("outcome month = %d, want %d", o.Month, r.World.Month)
	}
}

// TestWorldEvent_PromptSegment 有事件 ⇒ prompt 含「世界事件」段;无 ⇒ 整段省略。
func TestWorldEvent_PromptSegment(t *testing.T) {
	r := weRoom(t, "we-prompt", 20261023)
	// 无事件 ⇒ brief 为空串。
	if s := worldEventBriefLocked(r.World); s != "" {
		t.Errorf("no event must yield empty brief: %q", s)
	}
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	r.mu.Lock()
	r.refreshWorldEventsLocked()
	r.mu.Unlock()
	s := worldEventBriefLocked(r.World)
	if s == "" {
		t.Fatal("injected event must yield a prompt brief")
	}
}

// TestWorldEvent_EndToEnd_TrySettle 端到端:真实触发点(trySettle 月结前)⇒
// 注入 + 日志 + outcome 回填 + 看板快照 + 居民 prompt 段全链路贯通。
func TestWorldEvent_EndToEnd_TrySettle(t *testing.T) {
	r := weRoom(t, "we-e2e", 20261023)
	now := time.Now().Unix()
	r.SetWorldEventSource(weSource(weCrashInput(now)))
	// 推进到月结窗口(trySettle 的真实前置条件)。
	r.mu.Lock()
	r.NextMonthAt = time.Now().Add(-time.Millisecond)
	r.mu.Unlock()
	if !r.trySettle(nil) {
		t.Fatal("trySettle did not advance month")
	}
	w := r.Engine()
	if len(w.WorldEventLog) != 1 {
		t.Fatalf("event log len = %d, want 1", len(w.WorldEventLog))
	}
	rec := w.WorldEventLog[0]
	if !rec.Injected {
		t.Fatalf("event must be injected via trySettle: %+v", rec)
	}
	if len(w.ScenarioApplied) != 1 {
		t.Fatalf("scenario applied = %d, want 1", len(w.ScenarioApplied))
	}
	// outcome 回填(注入后的那次月结结束 ⇒ 已回填)。
	if rec.Outcome == nil {
		t.Fatalf("outcome must be filled after the settling month: %+v", rec)
	}
	if rec.Outcome.StockIndex == 0 || rec.Outcome.Unemployment == 0 {
		t.Errorf("outcome fields must be non-zero: %+v", rec.Outcome)
	}
	if rec.Outcome.Month != rec.InjectedMonth {
		t.Errorf("outcome month = %d, want %d (same effective month)",
			rec.Outcome.Month, rec.InjectedMonth)
	}
	// 看板快照。
	js := BuildWorldEventsJSON(w)
	if js == nil || len(js.Items) != 1 {
		t.Fatalf("world events json broken: %+v", js)
	}
	if js.Items[0].Key != rec.Key {
		t.Errorf("json item key mismatch: %q vs %q", js.Items[0].Key, rec.Key)
	}
	// 居民 prompt 段。
	if s := worldEventBriefLocked(w); s == "" {
		t.Error("resident prompt brief must be non-empty after injection")
	}
}
