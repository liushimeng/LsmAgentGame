// Package citysystem — dispatch_dimension_test.go: 批次85 维度状态视图
// 观察闭环单测(2026-11-05 §批次85 §7.1)。
//
// 覆盖:query_city_state 全 13 scope 观察闭环(无「未启用」空答复)/
// 注入场景后对应维度 scope 命中场景名 + 「本维度进行中」段 / environment /
// security 两段在场 / 既有 5 个实质 scope 零回归 / query_world_events 的
// dimension 过滤与未知维度失败。
package citysystem

import (
	"context"
	"strings"
	"testing"

	"LsmAgentGame/game/virtual_city"
)

// dimScopePairs 是 7 个维度 scope 与其维度分组的对照(与 scopeDimension 同源,
// 测试独立列出防映射表自证)。
var dimScopePairs = []struct{ scope, group string }{
	{"infrastructure", virtual_city.ScenarioGroupInfrastructure},
	{"public_health", virtual_city.ScenarioGroupPublicHealth},
	{"environment", virtual_city.ScenarioGroupEnvironment},
	{"security", virtual_city.ScenarioGroupSecurity},
	{"transport", virtual_city.ScenarioGroupTransport},
	{"culture", virtual_city.ScenarioGroupCulture},
	{"daily", virtual_city.ScenarioGroupDailyLife},
}

// firstKindOfGroup 取某维度目录里的第一个真实 kind(目录扩容不脆)。
func firstKindOfGroup(t *testing.T, group string) virtual_city.ScenarioSpec {
	t.Helper()
	specs := virtual_city.ScenarioCatalogSearch("", group, 5)
	if len(specs) == 0 {
		t.Fatalf("catalog must contain group %q", group)
	}
	return specs[0]
}

// seedWorldEvent 直接向世界事件日志写一条记录(测试夹具;真实字段,不编造)。
func seedWorldEvent(t *testing.T, room *virtual_city.VirtualCityRoom,
	key, title, kind, scenarioKind string, injected bool) {
	t.Helper()
	room.MuLock()
	defer room.MuUnlock()
	w := room.EngineLocked()
	if w == nil {
		t.Fatal("room must be started")
	}
	w.WorldEventLog = append(w.WorldEventLog, virtual_city.WorldEventRecord{
		Key: key, Title: title, Kind: kind, ScenarioKind: scenarioKind,
		Source: "测试源", Injected: injected,
	})
}

// TestQueryCityState_AllScopes_ObservationLoop 13 个 scope 逐个调用 ⇒
// 返回文本均带观察闭环回执,且 environment / security 不含「未启用」字样
// (批次85 §3.1.2:删除「子系统未启用」空答复)。
func TestQueryCityState_AllScopes_ObservationLoop(t *testing.T) {
	f := newExtFixture(t)
	// 注入一个场景 + 一条世界事件,让观察闭环有真实内容可回。
	spec := firstKindOfGroup(t, virtual_city.ScenarioGroupNature)
	if _, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolInjectScenario,
		map[string]any{"kind": spec.Kind, "magnitude": 3, "duration_months": 6}); isErr {
		t.Fatal("inject must succeed")
	}
	seedWorldEvent(t, f.room, "loop-e1", "测试事件", "zzz_unresolvable", "zzz_unresolvable", false)

	for _, scope := range queryCityStateScopes {
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": scope})
		if isErr {
			t.Fatalf("scope %s: unexpected error: %s", scope, text)
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("scope %s: empty text", scope)
		}
		// 观察闭环回执:两段之一,或明确的空态回执(绝不空白)。
		marker := strings.Contains(text, "【本维度进行中】") ||
			strings.Contains(text, "【全局进行中】") ||
			strings.Contains(text, "【相关世界事件】") ||
			strings.Contains(text, "当前该维度无进行中场景、无相关事件")
		if !marker {
			t.Errorf("scope %s missing observation loop: %q", scope, text)
		}
		// environment / security 严格不含「未启用」(批次85 重写)。
		if scope == "environment" || scope == "security" {
			if strings.Contains(text, "未启用") {
				t.Errorf("scope %s must not contain 未启用 (batch85 rewrite): %q", scope, text)
			}
		}
	}
}

// TestQueryCityState_DimensionObservation 注入场景后,对应维度的 scope 结果
// 必须含该场景名与「本维度进行中」段(观察闭环验收 §7.2-1)。
func TestQueryCityState_DimensionObservation(t *testing.T) {
	f := newExtFixture(t)
	for _, p := range dimScopePairs {
		spec := firstKindOfGroup(t, p.group)
		if _, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolInjectScenario,
			map[string]any{"kind": spec.Kind, "magnitude": 3, "duration_months": 6}); isErr {
			t.Fatalf("inject %s must succeed", spec.Kind)
		}
	}
	for _, p := range dimScopePairs {
		spec := firstKindOfGroup(t, p.group)
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": p.scope})
		if isErr {
			t.Fatalf("scope %s: unexpected error: %s", p.scope, text)
		}
		if !strings.Contains(text, "【本维度进行中】") {
			t.Errorf("scope %s missing 【本维度进行中】 section: %q", p.scope, text)
		}
		if !strings.Contains(text, spec.Name) {
			t.Errorf("scope %s missing scenario name %q: %q", p.scope, spec.Name, text)
		}
		if !strings.Contains(text, "L3") || !strings.Contains(text, "注入于 M") {
			t.Errorf("scope %s scenario line must carry magnitude/applied month: %q", p.scope, text)
		}
	}
}

// TestQueryCityState_EnvironmentSecurity_Sections environment / security
// 含活动场景 + 世界事件两段(即使指标缺失也必须给;§7.1 硬性断言)。
func TestQueryCityState_EnvironmentSecurity_Sections(t *testing.T) {
	f := newExtFixture(t)
	for _, p := range []struct{ scope, group string }{
		{"environment", virtual_city.ScenarioGroupEnvironment},
		{"security", virtual_city.ScenarioGroupSecurity},
	} {
		spec := firstKindOfGroup(t, p.group)
		if _, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolInjectScenario,
			map[string]any{"kind": spec.Kind, "magnitude": 2, "duration_months": 4}); isErr {
			t.Fatalf("inject %s must succeed", spec.Kind)
		}
		seedWorldEvent(t, f.room, "es-"+p.scope, "维度事件:"+p.scope,
			virtual_city.WorldEventKindEarthquake, spec.Kind, true)
	}
	for _, p := range []struct{ scope, group string }{
		{"environment", virtual_city.ScenarioGroupEnvironment},
		{"security", virtual_city.ScenarioGroupSecurity},
	} {
		spec := firstKindOfGroup(t, p.group)
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": p.scope})
		if isErr {
			t.Fatalf("scope %s: unexpected error: %s", p.scope, text)
		}
		for _, want := range []string{"【本维度进行中】", "【相关世界事件】", spec.Name, "维度事件:" + p.scope} {
			if !strings.Contains(text, want) {
				t.Errorf("scope %s missing %q: %q", p.scope, want, text)
			}
		}
		if strings.Contains(text, "未启用") {
			t.Errorf("scope %s must not contain 未启用: %q", p.scope, text)
		}
	}
}

// TestQueryCityState_ZeroRegression 既有 5 个实质 scope 的既有内容零回归:
// 关键指标仍一字在场(批次85 只在末尾追加,既有内容不动)。
func TestQueryCityState_ZeroRegression(t *testing.T) {
	f := newExtFixture(t)
	cases := []struct {
		scope string
		want  []string
	}{
		{"infrastructure", []string{"基础设施(M", "产业链节点", "层级价格指数", "产业集群"}},
		{"public_health", []string{"公共卫生(M", "医疗质量", "教育质量", "养老水平", "公共医疗累积投入"}},
		{"transport", []string{"交通物流(M", "交通类商品价格指数", "物流依托产业链"}},
		{"culture", []string{"文化教育(M", "教育质量", "公共教育累积投入", "教育类商品价格指数"}},
		{"daily", []string{"民生日常(M", "CPI 同比", "价格指数"}},
	}
	for _, tc := range cases {
		text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryCityState,
			map[string]any{"scope": tc.scope})
		if isErr {
			t.Fatalf("scope %s: unexpected error: %s", tc.scope, text)
		}
		for _, want := range tc.want {
			if !strings.Contains(text, want) {
				t.Errorf("scope %s regressed, missing %q: %q", tc.scope, want, text)
			}
		}
	}
}

// TestQueryWorldEvents_Dimension dimension 过滤生效;无关/归不到的事件不计入;
// 未知维度 ⇒ 失败文本(35064);无命中维度 ⇒ 明确「暂无」(不编造)。
func TestQueryWorldEvents_Dimension(t *testing.T) {
	f := newExtFixture(t)
	seedWorldEvent(t, f.room, "we-1", "地震速报",
		virtual_city.WorldEventKindEarthquake, virtual_city.ScenarioEarthquake, true)
	seedWorldEvent(t, f.room, "we-2", "股市剧震",
		virtual_city.WorldEventKindEquityCrash, virtual_city.ScenarioMarketCrash, false)
	seedWorldEvent(t, f.room, "we-3", "神秘信号", "zzz", "zzz", false)

	// 空 dimension = 全部(含归不到的,如实列出)。
	all, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryWorldEvents,
		map[string]any{})
	if isErr {
		t.Fatalf("unexpected error: %s", all)
	}
	for _, want := range []string{"地震速报", "股市剧震", "神秘信号"} {
		if !strings.Contains(all, want) {
			t.Errorf("unfiltered list missing %q: %q", want, all)
		}
	}

	// 按维度过滤:只留归入该维度的。
	nature, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryWorldEvents,
		map[string]any{"dimension": virtual_city.ScenarioGroupNature})
	if isErr {
		t.Fatalf("unexpected error: %s", nature)
	}
	if !strings.Contains(nature, "地震速报") {
		t.Errorf("nature filter missing 地震速报: %q", nature)
	}
	for _, absent := range []string{"股市剧震", "神秘信号"} {
		if strings.Contains(nature, absent) {
			t.Errorf("nature filter must exclude %q (无关/归不到不计入): %q", absent, nature)
		}
	}

	// 未知维度 ⇒ 失败文本(35064)。
	bad, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryWorldEvents,
		map[string]any{"dimension": "不存在"})
	if !isErr {
		t.Fatalf("unknown dimension must fail, got %q", bad)
	}
	if !strings.Contains(bad, "35064") || !strings.Contains(bad, "未知维度") {
		t.Fatalf("unknown dimension text = %q, want 35064 + 未知维度", bad)
	}

	// 合法维度但无相关事件 ⇒ 明确「暂无」。
	tech := firstKindOfGroup(t, virtual_city.ScenarioGroupTech)
	empty, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolQueryWorldEvents,
		map[string]any{"dimension": tech.Group})
	if isErr {
		t.Fatalf("empty dimension must not fail: %s", empty)
	}
	if !strings.Contains(empty, "暂无相关世界事件") {
		t.Fatalf("empty dimension text = %q", empty)
	}
}

// TestQueryWorldEvents_DimensionSchema §130 接线验证:query_world_events 的
// dimension 属性已在 InputSchema 声明(枚举 = 13 个分组名)。
func TestQueryWorldEvents_DimensionSchema(t *testing.T) {
	for _, d := range BuildTools() {
		if d.Name != ToolQueryWorldEvents {
			continue
		}
		props, ok := d.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatal("query_world_events missing properties")
		}
		dim, ok := props["dimension"].(map[string]any)
		if !ok {
			t.Fatal("query_world_events missing dimension schema (batch85 §3.1.3)")
		}
		if dim["type"] != "string" {
			t.Fatalf("dimension type = %v, want string", dim["type"])
		}
		enum, ok := dim["enum"].([]string)
		if !ok {
			t.Fatalf("dimension enum = %T, want []string", dim["enum"])
		}
		if len(enum) != len(knownScenarioGroups()) {
			t.Fatalf("dimension enum size = %d, want %d", len(enum), len(knownScenarioGroups()))
		}
		// 工具面仍是 17 个(不新增工具)。
		if len(ToolNames()) != 17 {
			t.Fatalf("tool count = %d, want 17", len(ToolNames()))
		}
		return
	}
	t.Fatal("query_world_events tool not found")
}
