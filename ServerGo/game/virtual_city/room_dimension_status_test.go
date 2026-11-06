// Package virtual_city — room_dimension_status_test.go: 批次85 全维度状态视图
// 单测(2026-11-05 §批次85 §7.1)。
//
// 覆盖:SystemDimensionStatus 只读(调用前后世界状态逐字段相等)/ nil 安全
// (未开局不 panic)/ 返回副本 / 未知 dim 空视图 / 世界事件与场景归集
// (相关计入、无关不计入、归不到不计入)。
package virtual_city

import (
	"encoding/json"
	"strings"
	"testing"
)

// worldJSON 世界状态的确定性 JSON 快照(测试断言用;map 键由
// encoding/json 排序,输出稳定)。
func worldJSON(t *testing.T, w *World) string {
	t.Helper()
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("world must be JSON-marshalable for snapshot: %v", err)
	}
	return string(b)
}

// TestSystemDimensionStatus_ReadOnly 只读:调用前后世界状态逐字段相等
// (JSON 快照全量对比;§92a 纪律的可执行验证)。
func TestSystemDimensionStatus_ReadOnly(t *testing.T) {
	r := newScenarioTestRoom(t, "dim-ro", 20261105)
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "只读验证"); e != nil {
		t.Fatalf("apply scenario: %v", e)
	}
	before := worldJSON(t, r.World)
	for _, g := range scenarioGroupOrder {
		_ = r.SystemDimensionStatus(g)
	}
	_ = r.SystemDimensionStatus("不存在的维度")
	after := worldJSON(t, r.World)
	if before != after {
		t.Fatalf("SystemDimensionStatus must be read-only:\nbefore=%s\nafter =%s", before, after)
	}
}

// TestSystemDimensionStatus_NilWorld 未开局房间(World 为 nil)不 panic,
// 返回可读空视图(维度名在场)。
func TestSystemDimensionStatus_NilWorld(t *testing.T) {
	r := NewVirtualCityRoom("dim-nil", 3000, 1, 4)
	v := r.SystemDimensionStatus(ScenarioGroupNature)
	if v.Group != ScenarioGroupNature {
		t.Fatalf("group = %q, want %q", v.Group, ScenarioGroupNature)
	}
	if len(v.ActiveScenarios) != 0 || len(v.RecentEvents) != 0 {
		t.Fatalf("nil-world view must be empty: %+v", v)
	}
}

// TestSystemDimensionStatus_ReturnsCopy 返回副本:调用方修改不污染内部状态。
func TestSystemDimensionStatus_ReturnsCopy(t *testing.T) {
	r := newScenarioTestRoom(t, "dim-copy", 20261105)
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "副本验证"); e != nil {
		t.Fatalf("apply scenario: %v", e)
	}
	r.MuLock()
	w := r.EngineLocked()
	w.worldEventLogAdd(WorldEventRecord{Key: "copy-k", Title: "地震速报",
		Kind: WorldEventKindEarthquake, ScenarioKind: ScenarioEarthquake, Injected: true})
	r.MuUnlock()

	v := r.SystemDimensionStatus(ScenarioGroupNature)
	if len(v.ActiveScenarios) == 0 || len(v.RecentEvents) == 0 {
		t.Fatalf("fixture must produce scenarios+events, got %+v", v)
	}
	v.ActiveScenarios[0].Name = "被篡改"
	v.RecentEvents[0].Title = "被篡改"
	v.ActiveScenarios = v.ActiveScenarios[:0]
	v.RecentEvents = v.RecentEvents[:0]

	v2 := r.SystemDimensionStatus(ScenarioGroupNature)
	if len(v2.ActiveScenarios) == 0 || len(v2.RecentEvents) == 0 {
		t.Fatal("internal state must not be polluted by caller mutation")
	}
	if v2.ActiveScenarios[0].Name == "被篡改" || v2.RecentEvents[0].Title == "被篡改" {
		t.Fatalf("caller mutation leaked into world state: %+v", v2)
	}
}

// TestSystemDimensionStatus_UnknownDim 未知维度(含空串)返回空视图。
func TestSystemDimensionStatus_UnknownDim(t *testing.T) {
	r := newScenarioTestRoom(t, "dim-unknown", 20261105)
	for _, dim := range []string{"", "不存在", "infrastructure"} {
		v := r.SystemDimensionStatus(dim)
		if v.Group != "" {
			t.Errorf("dim %q: group = %q, want empty view", dim, v.Group)
		}
		if len(v.ActiveScenarios) != 0 || len(v.RecentEvents) != 0 {
			t.Errorf("dim %q: unknown dim must return empty view, got %+v", dim, v)
		}
	}
}

// TestSystemDimensionStatus_Grouping 归集判据(§3.1.1):
//   - 场景按 scenarioDefFor(kind).spec.Group 归集(L1 kind 直连目录);
//   - 世界事件按 ScenarioKind / Kind 归集;
//   - 相关事件计入、无关事件**不计入**、归不到维度的事件**不计入**(宁缺勿滥)。
func TestSystemDimensionStatus_Grouping(t *testing.T) {
	r := newScenarioTestRoom(t, "dim-group", 20261105)
	// 两个维度各注入一个场景:地震(自然灾害)/ 商品价格冲击(市场与经济)。
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "t"); e != nil {
		t.Fatalf("apply earthquake: %v", e)
	}
	if _, _, e := r.ApplyScenario(ScenarioGoodsPriceShock, 2, 6, "", "t"); e != nil {
		t.Fatalf("apply goods shock: %v", e)
	}
	r.MuLock()
	w := r.EngineLocked()
	// 三条事件:相关(地震)/ 无关(市场崩盘)/ 归不到任何维度(伪 kind)。
	w.worldEventLogAdd(WorldEventRecord{Key: "e1", Title: "地震速报",
		Kind: WorldEventKindEarthquake, ScenarioKind: ScenarioEarthquake, Injected: true})
	w.worldEventLogAdd(WorldEventRecord{Key: "e2", Title: "股市剧震",
		Kind: WorldEventKindEquityCrash, ScenarioKind: ScenarioMarketCrash})
	w.worldEventLogAdd(WorldEventRecord{Key: "e3", Title: "神秘信号",
		Kind: "zzz_no_such_kind", ScenarioKind: "zzz_no_such_scenario"})
	r.MuUnlock()

	nature := r.SystemDimensionStatus(ScenarioGroupNature)
	if nature.Group != ScenarioGroupNature {
		t.Fatalf("group = %q", nature.Group)
	}
	if len(nature.ActiveScenarios) != 1 || nature.ActiveScenarios[0].Kind != ScenarioEarthquake {
		t.Fatalf("nature scenarios = %+v, want only earthquake", nature.ActiveScenarios)
	}
	if len(nature.RecentEvents) != 1 || nature.RecentEvents[0].Title != "地震速报" {
		t.Fatalf("nature events = %+v, want only 地震速报(相关计入、无关/归不到不计入)", nature.RecentEvents)
	}
	if !nature.RecentEvents[0].Injected {
		t.Error("earthquake event must carry Injected flag")
	}

	market := r.SystemDimensionStatus(ScenarioGroupMarket)
	if len(market.ActiveScenarios) != 1 || market.ActiveScenarios[0].Kind != ScenarioGoodsPriceShock {
		t.Fatalf("market scenarios = %+v, want only goods_price_shock", market.ActiveScenarios)
	}
	if len(market.RecentEvents) != 1 || market.RecentEvents[0].Title != "股市剧震" {
		t.Fatalf("market events = %+v, want only 股市剧震", market.RecentEvents)
	}

	// 归不到维度的 e3 不计入任何维度(宁缺勿滥)。
	for _, g := range scenarioGroupOrder {
		for _, ev := range r.SystemDimensionStatus(g).RecentEvents {
			if ev.Title == "神秘信号" {
				t.Fatalf("unresolvable event must not appear in dim %q", g)
			}
		}
	}
}

// TestWorldEventDimensionOf 归集函数:ScenarioKind 优先、Kind 兜底、
// 双盲返回空串。
func TestWorldEventDimensionOf(t *testing.T) {
	cases := []struct {
		name string
		rec  WorldEventRecord
		want string
	}{
		{"scenario_kind wins", WorldEventRecord{ScenarioKind: ScenarioEarthquake, Kind: WorldEventKindEquityCrash}, ScenarioGroupNature},
		{"kind fallback", WorldEventRecord{ScenarioKind: "", Kind: WorldEventKindEarthquake}, ScenarioGroupNature},
		{"unresolvable", WorldEventRecord{ScenarioKind: "zzz", Kind: "zzz2"}, ""},
		{"both empty", WorldEventRecord{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorldEventDimensionOf(tc.rec); got != tc.want {
				t.Fatalf("WorldEventDimensionOf(%+v) = %q, want %q", tc.rec, got, tc.want)
			}
		})
	}
	// 一致性锚:kind 直连目录分组。
	if got := ScenarioDimensionOf(ScenarioMarketCrash); got != ScenarioGroupMarket {
		t.Fatalf("ScenarioDimensionOf(market_crash) = %q, want %q", got, ScenarioGroupMarket)
	}
	if got := ScenarioDimensionOf("zzz"); got != "" {
		t.Fatalf("ScenarioDimensionOf(unknown) = %q, want empty", got)
	}
}

// TestCashDamageScenarioBriefs 居民现金损失类场景(CashDamageFrac>0)
// 真实 profile 字段读出;返回副本、新→旧。
func TestCashDamageScenarioBriefs(t *testing.T) {
	r := newScenarioTestRoom(t, "dim-cash", 20261105)
	if _, _, e := r.ApplyScenario(ScenarioEarthquake, 3, 6, "", "t"); e != nil {
		t.Fatalf("apply earthquake: %v", e)
	}
	r.MuLock()
	w := r.EngineLocked()
	got := CashDamageScenarioBriefs(w)
	r.MuUnlock()
	// 地震 profile 的 CashDamageFrac>0(居民现金损失类)⇒ 必须在列。
	found := false
	for _, s := range got {
		if s.Kind == ScenarioEarthquake {
			found = true
		}
	}
	if !found {
		t.Fatalf("earthquake has CashDamageFrac>0, must be listed: %+v", got)
	}
	// nil 安全。
	if out := CashDamageScenarioBriefs(nil); len(out) != 0 {
		t.Fatalf("nil world must return empty, got %+v", out)
	}
	// 返回副本。
	if len(got) > 0 {
		got[0].Name = "被篡改"
		r.MuLock()
		again := CashDamageScenarioBriefs(r.EngineLocked())
		r.MuUnlock()
		if len(again) == 0 || again[0].Name == "被篡改" {
			t.Fatal("CashDamageScenarioBriefs must return copies")
		}
	}
	// 名称取目录(不编造)。
	if len(got) > 0 && strings.TrimSpace(got[0].Name) == "" {
		t.Fatal("brief name must not be empty")
	}
}
