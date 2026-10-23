// Package virtual_city — world_event_log_test.go: 批次80 事件反馈层单测
// (2026-10-23 §真实世界事件自动注入引擎)。
//
// 覆盖:日志环(容量/新→旧/同 key 原地更新)/ 幂等集合 / outcome 回填
// (因果链终点字段非零)/ 看板快照 JSON 契约。
package virtual_city

import (
	"strings"
	"testing"
)

// TestWorldEventLog_RingAndOrder 日志环:新→旧、容量 ≤50、同 key 原地更新。
func TestWorldEventLog_RingAndOrder(t *testing.T) {
	w := newTestWorld(20261023)
	for i := 0; i < 60; i++ {
		w.worldEventLogAdd(WorldEventRecord{Key: "k" + itoaInt(i), Title: "t" + itoaInt(i)})
	}
	if len(w.WorldEventLog) != worldEventLogCap {
		t.Fatalf("log len = %d, want %d", len(w.WorldEventLog), worldEventLogCap)
	}
	// 新→旧:最后写入的在头部。
	if w.WorldEventLog[0].Key != "k59" {
		t.Errorf("head key = %q, want k59 (newest first)", w.WorldEventLog[0].Key)
	}
	// 同 key ⇒ 原地更新,不新增行。
	before := len(w.WorldEventLog)
	w.worldEventLogUpsert(WorldEventRecord{Key: "k59", Injected: true, InjectedMonth: 7})
	if len(w.WorldEventLog) != before {
		t.Fatalf("upsert must not grow log: %d -> %d", before, len(w.WorldEventLog))
	}
	if !w.WorldEventLog[0].Injected || w.WorldEventLog[0].InjectedMonth != 7 {
		t.Errorf("upsert did not update in place: %+v", w.WorldEventLog[0])
	}
}

// TestWorldEventSeen_IdempotentGate 幂等集合:标记后同 key 不再处理。
func TestWorldEventSeen_IdempotentGate(t *testing.T) {
	w := newTestWorld(20261023)
	if w.worldEventSeen("rw:R1:x") {
		t.Fatal("fresh world must have empty seen set")
	}
	w.worldEventMarkSeen("rw:R1:x")
	if !w.worldEventSeen("rw:R1:x") {
		t.Fatal("seen key must be marked")
	}
	if w.worldEventSeen("rw:R1:y") {
		t.Fatal("other key must not be marked")
	}
}

// TestFillWorldEventOutcomes 注入后一次月结 ⇒ outcome 回填且字段非零。
func TestFillWorldEventOutcomes(t *testing.T) {
	w := newTestWorld(20261023)
	w.Players[0].Cash = 50000
	w.WorldEventLog = []WorldEventRecord{
		{Key: "k1", Rule: "R1", Injected: true, InjectedMonth: 1, ScenarioKind: ScenarioMarketCrash},
	}
	w.SettleMonth()
	if len(w.WorldEventLog) != 1 {
		t.Fatalf("log len changed: %d", len(w.WorldEventLog))
	}
	o := w.WorldEventLog[0].Outcome
	if o == nil {
		t.Fatal("outcome must be filled after a month settle")
	}
	if o.Month != w.Month {
		t.Errorf("outcome month = %d, want %d (次月编号)", o.Month, w.Month)
	}
	// 因果链终点字段非零(除可能为 0 的商品价格指数外均须有值)。
	if o.StockIndex == 0 || o.GoldPrice == 0 || o.ResidentCashTotal == 0 {
		t.Errorf("outcome fields must be non-zero: %+v", o)
	}
	if o.Unemployment == 0 {
		t.Errorf("outcome unemployment must be non-zero: %+v", o)
	}
	if o.GoodsPriceIdx == 0 {
		t.Errorf("outcome goods price index must be non-zero: %+v", o)
	}
	// 已回填的事件不再被二次回填(月度推进后 outcome.Month 不变)。
	before := *o
	w.SettleMonth()
	if w.WorldEventLog[0].Outcome.Month != before.Month {
		t.Errorf("outcome must not be rewritten: %+v -> %+v", before, *w.WorldEventLog[0].Outcome)
	}
}

// TestFillWorldEventOutcomes_NotInjectedSkipped 未注入事件不回填 outcome。
func TestFillWorldEventOutcomes_NotInjectedSkipped(t *testing.T) {
	w := newTestWorld(20261023)
	w.WorldEventLog = []WorldEventRecord{{Key: "k1", Injected: false}}
	w.SettleMonth()
	if w.WorldEventLog[0].Outcome != nil {
		t.Errorf("not-injected event must not carry outcome: %+v", w.WorldEventLog[0].Outcome)
	}
}

// TestWorldEventInjectedThisMonth 本月注入计数 + 同 kind 集合。
func TestWorldEventInjectedThisMonth(t *testing.T) {
	w := newTestWorld(20261023)
	w.Month = 5
	// 计数器口径:注入发生在月结之前 ⇒ 计数器归属**生效月**(= w.Month+1)。
	if got := w.worldEventEffectiveMonth(); got != 6 {
		t.Fatalf("effective month = %d, want 6", got)
	}
	w.worldEventAutoCounterReset()
	w.worldEventAutoMarkInjected(ScenarioMarketCrash)
	w.worldEventAutoMarkInjected(ScenarioMarketCrash)
	if got := w.worldEventAutoCount(); got != 2 {
		t.Errorf("auto count = %d, want 2", got)
	}
	if !w.worldEventAutoKindInjected(ScenarioMarketCrash) {
		t.Error("market_crash must be marked as injected this month")
	}
	if w.worldEventAutoKindInjected(ScenarioTsunami) {
		t.Error("not-injected kind must not be marked")
	}
	// 同生效月再触发 ⇒ 不重置。
	w.worldEventAutoCounterReset()
	if got := w.worldEventAutoCount(); got != 2 {
		t.Errorf("counter must survive within same effective month: %d", got)
	}
	// 跨月(生效月变化)⇒ 计数器归零。
	w.Month = 6
	w.worldEventAutoCounterReset()
	if got := w.worldEventAutoCount(); got != 0 {
		t.Errorf("counter must reset on new month: %d", got)
	}
	if w.worldEventAutoKindInjected(ScenarioMarketCrash) {
		t.Error("kind set must reset on new month")
	}
}

// TestWorldEventInjectedEffectiveThisMonth 看板口径:生效于当前月的注入数
// (日志环回查路径 —— 计数器已跨月归零时用日志)。
func TestWorldEventInjectedEffectiveThisMonth(t *testing.T) {
	w := newTestWorld(20261023)
	w.Month = 6
	w.WorldEventLog = []WorldEventRecord{
		{Key: "a", Injected: true, InjectedMonth: 6, ScenarioKind: ScenarioMarketCrash},
		{Key: "b", Injected: true, InjectedMonth: 6, ScenarioKind: ScenarioMarketCrash},
		{Key: "c", Injected: true, InjectedMonth: 5, ScenarioKind: ScenarioEarthquake},
		{Key: "d", Injected: false, InjectedMonth: 6, ScenarioKind: ScenarioTsunami},
	}
	if got := w.worldEventInjectedEffectiveThisMonth(); got != 2 {
		t.Errorf("effective this month = %d, want 2", got)
	}
}

// TestBuildWorldEventsJSON 看板快照契约:auto_enabled / items(≤20,新→旧)。
func TestBuildWorldEventsJSON(t *testing.T) {
	w := newTestWorld(20261023)
	w.WorldEventAutoEnabled = true
	w.Month = 3
	for i := 0; i < 30; i++ {
		w.worldEventLogAdd(WorldEventRecord{
			Key: "k" + itoaInt(i), ObservedAt: int64(1000 + i),
			Injected: i%2 == 0, InjectedMonth: 3,
		})
	}
	js := BuildWorldEventsJSON(w)
	if js == nil {
		t.Fatal("world events json must not be nil")
	}
	if !js.AutoEnabled {
		t.Error("auto_enabled must mirror world flag")
	}
	if len(js.Items) != worldEventViewCap {
		t.Errorf("items len = %d, want %d", len(js.Items), worldEventViewCap)
	}
	if js.Items[0].Key != "k29" {
		t.Errorf("items[0] = %q, want k29 (newest first)", js.Items[0].Key)
	}
	if js.Items[0].Effects == nil {
		t.Error("items effects must be [] not null")
	}
	// 生效月 = 当前月(w.Month=3)⇒ 生效于本月的注入数 = 15(偶数下标注入)。
	if js.InjectedThisMonth != 15 {
		t.Errorf("injected_this_month = %d, want 15", js.InjectedThisMonth)
	}
	// nil world ⇒ nil(未开局不下发)。
	if BuildWorldEventsJSON(nil) != nil {
		t.Error("nil world must yield nil json")
	}
}

// TestWorldEventBriefLocked prompt 小节:有已注入事件 ⇒ 非空;无 ⇒ 空串。
func TestWorldEventBriefLocked(t *testing.T) {
	w := newTestWorld(20261023)
	if s := worldEventBriefLocked(w); s != "" {
		t.Errorf("empty log must yield empty brief: %q", s)
	}
	w.worldEventLogAdd(WorldEventRecord{
		Key: "k1", Title: "股市剧震", Injected: true, InjectedMonth: 1,
		ScenarioKind: ScenarioMarketCrash, ScenarioMag: 1,
		Effects: []ScenarioEffect{{Metric: "stock_index", Delta: -6, Text: "股指 -6.00%"}},
	})
	s := worldEventBriefLocked(w)
	if s == "" {
		t.Fatal("injected event must produce a brief")
	}
	if !strings.Contains(s, "股市剧震") {
		t.Errorf("brief must mention the event title: %q", s)
	}
	// 未注入的事件不进 brief。
	w2 := newTestWorld(20261023)
	w2.worldEventLogAdd(WorldEventRecord{Key: "k1", Title: "股市剧震", Injected: false})
	if s := worldEventBriefLocked(w2); s != "" {
		t.Errorf("not-injected event must not produce a brief: %q", s)
	}
}
