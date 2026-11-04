// Package citysystem — prompt_test.go: 系统提示词与工具清单单测
// (2026-11-03 §批次81)。
package citysystem

import (
	"strings"
	"testing"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/game/virtual_city"
)

// TestScenarioCatalogText 目录文本非空、含分组与已知 kind
// (批次83 起为 ScenarioCatalogDetail("") 薄包装,锚点保持)。
func TestScenarioCatalogText(t *testing.T) {
	text := ScenarioCatalogText()
	if text == "" {
		t.Fatal("ScenarioCatalogText must not be empty")
	}
	for _, want := range []string{
		virtual_city.ScenarioGoodsPriceShock,
		virtual_city.ScenarioEarthquake,
		virtual_city.ScenarioAlienInvasion,
		"[",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("catalog text missing %q", want)
		}
	}
}

// TestScenarioCatalogIndex 紧凑索引(批次83 §4.2):组头带计数、行含
// kind/名称/强度/参数、**不含描述**;带参条目含「参数:」段,无参条目
// 以「(Lx)」收尾。
func TestScenarioCatalogIndex(t *testing.T) {
	idx := ScenarioCatalogIndex()
	if idx == "" {
		t.Fatal("ScenarioCatalogIndex must not be empty")
	}
	for _, want := range []string{
		"[" + virtual_city.ScenarioGroupMarket + "] 21 个",
		"[" + virtual_city.ScenarioGroupTech + "] 13 个",
		"- " + virtual_city.ScenarioGoodsPriceShock + " 商品价格冲击(L3;参数:goods)",
		"- " + virtual_city.ScenarioEarthquake + " 地震(L3)",
		"- " + virtual_city.ScenarioNuclearBrinkmanship + " 核冲突边缘(L5)",
	} {
		if !strings.Contains(idx, want) {
			t.Errorf("index missing %q", want)
		}
	}
	// 体量护栏:101 条索引应 ≈3k rune,远小于全描述内联(§4.2 动机);
	// 上限 5k 防目录扩容后索引失控撑爆 system 前缀。
	if n := len([]rune(idx)); n > 5000 {
		t.Errorf("index too large: %d runes (want <= 5000)", n)
	}
	// 索引不含任何条目描述片段(描述只能经 Detail/list_scenarios(group) 取)。
	for _, banned := range []string{
		"城区建筑受损",     // earthquake 描述片段
		"央行连续加息抑制通胀", // interest_rate_hike 描述片段
		"地缘核对抗白热化",   // nuclear_brinkmanship 描述片段
	} {
		if strings.Contains(idx, banned) {
			t.Errorf("index must not contain description fragment %q", banned)
		}
	}
}

// TestScenarioCatalogDetail 单组详情只含该组条目且含描述;空串 = 全部分组
// (兼容旧语义);未知组名返回错误并列出可用分组。
func TestScenarioCatalogDetail(t *testing.T) {
	detail, err := ScenarioCatalogDetail(virtual_city.ScenarioGroupNature)
	if err != nil {
		t.Fatalf("detail(nature) error: %v", err)
	}
	for _, want := range []string{
		"[" + virtual_city.ScenarioGroupNature + "]",
		virtual_city.ScenarioBlizzard,
		"极端暴雪阻断物流干线", // blizzard 描述片段(Detail 含描述)
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail(nature) missing %q", want)
		}
	}
	for _, banned := range []string{
		"[" + virtual_city.ScenarioGroupMarket + "]",
		virtual_city.ScenarioGoodsPriceShock,
		virtual_city.ScenarioLiquidityCrisis,
	} {
		if strings.Contains(detail, banned) {
			t.Errorf("detail(nature) must not contain %q", banned)
		}
	}
	// 空串 = 全部分组(六个组头齐全,与旧 ScenarioCatalogText 语义一致)。
	all, err := ScenarioCatalogDetail("")
	if err != nil {
		t.Fatalf("detail(\"\") error: %v", err)
	}
	for _, g := range scenarioGroupOrder {
		if !strings.Contains(all, "["+g+"]") {
			t.Errorf("detail(\"\") missing group header [%s]", g)
		}
	}
	// 未知组名 → 错误(调用方转「失败:未知分组 …」回灌)。
	if _, err := ScenarioCatalogDetail("不存在"); err == nil {
		t.Fatal("detail(unknown group) must return error")
	} else if !strings.Contains(err.Error(), "未知分组") || !strings.Contains(err.Error(), "可用分组") {
		t.Errorf("error text = %q, want 未知分组 + 可用分组", err.Error())
	}
}

// TestListScenariosGroupSchema list_scenarios 的 group 枚举 = 六个组名,
// 描述说明缺省紧凑索引 / 传组名全量详情(批次83 §4.2)。
func TestListScenariosGroupSchema(t *testing.T) {
	for _, d := range BuildTools() {
		if d.Name != ToolListScenarios {
			continue
		}
		props, ok := d.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatal("list_scenarios missing properties")
		}
		group, ok := props["group"].(map[string]any)
		if !ok {
			t.Fatal("list_scenarios missing group schema")
		}
		enum, _ := group["enum"].([]string)
		if len(enum) != 6 {
			t.Fatalf("group enum = %v, want 6 groups", enum)
		}
		for _, g := range scenarioGroupOrder {
			found := false
			for _, e := range enum {
				if e == g {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("group enum missing %q", g)
			}
		}
		if !strings.Contains(d.Description, "紧凑索引") {
			t.Errorf("description must mention 紧凑索引: %q", d.Description)
		}
		return
	}
	t.Fatal("list_scenarios tool not found")
}

// TestBuildSystemBlocks 三段式头 + 角色段 + 场景目录。
func TestBuildSystemBlocks(t *testing.T) {
	blocks := BuildSystemBlocks(8)
	if len(blocks) == 0 {
		t.Fatal("BuildSystemBlocks must not be empty")
	}
	all := ""
	for _, b := range blocks {
		all += b.Text
	}
	// ① 计费头含 AgentClassName(§14.3)。
	if !strings.Contains(blocks[0].Text, "cc_agent_name="+string(agentroot.AgentClassCitySystem)) {
		t.Errorf("billing head missing agent class: %q", blocks[0].Text)
	}
	// ②③ 标准头 + 角色段身份。
	for _, want := range []string{
		"LsmAgentGame-City-System",
		"LsmAgentGame-City-Human",
		"LsmAgentGame-City-Chrome",
		"LsmAgentGame-City-WebGL-Show",
		"禁止编造数据",
		"虚拟城市是**全 Agent 真实城市模拟系统**(非游戏)",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("system blocks missing %q", want)
		}
	}
	// 场景目录以**紧凑索引**内联(批次83):含 kind 行、不含描述;
	// 完整描述经 list_scenarios(group=组名) 按组获取。
	if !strings.Contains(all, "- "+virtual_city.ScenarioGoodsPriceShock+" 商品价格冲击(L3;参数:goods)") {
		t.Error("system blocks missing inline scenario catalog index")
	}
	if strings.Contains(all, "城区建筑受损") {
		t.Error("system blocks must inline index without descriptions")
	}
	if !strings.Contains(all, "list_scenarios(group=组名)") {
		t.Error("role text must guide per-group detail via list_scenarios(group)")
	}
}

// TestBuildSystemBlocks_Idempotent EnsureHead 幂等(重复调用不叠加标准头)。
func TestBuildSystemBlocks_Idempotent(t *testing.T) {
	a := BuildSystemBlocks(8)
	b := BuildSystemBlocks(8)
	if len(a) != len(b) {
		t.Fatalf("block count differs: %d vs %d", len(a), len(b))
	}
	if len(a) < 2 {
		t.Fatalf("expected >= 2 blocks (head + role), got %d", len(a))
	}
}

// TestToolNames 工具清单 = 11,且与 BuildTools 一一对应。
func TestToolNames(t *testing.T) {
	names := ToolNames()
	if len(names) != 11 {
		t.Fatalf("ToolNames len = %d, want 11", len(names))
	}
	defs := BuildTools()
	if len(defs) != len(names) {
		t.Fatalf("BuildTools len = %d, want %d", len(defs), len(names))
	}
	seen := map[string]bool{}
	for i, name := range names {
		if name == "" {
			t.Fatalf("empty tool name at %d", i)
		}
		if seen[name] {
			t.Fatalf("duplicate tool name %q", name)
		}
		seen[name] = true
		if defs[i].Name != name {
			t.Fatalf("BuildTools[%d].Name = %q, want %q", i, defs[i].Name, name)
		}
		if defs[i].InputSchema == nil {
			t.Fatalf("tool %q missing input schema", name)
		}
	}
}

// TestInjectScenarioKindEnum kind 枚举来自动态目录(含既有场景)。
func TestInjectScenarioKindEnum(t *testing.T) {
	defs := BuildTools()
	for _, d := range defs {
		if d.Name != ToolInjectScenario {
			continue
		}
		props, ok := d.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatal("inject_scenario missing properties")
		}
		kind, ok := props["kind"].(map[string]any)
		if !ok {
			t.Fatal("inject_scenario missing kind schema")
		}
		enum, _ := kind["enum"].([]string)
		if len(enum) == 0 {
			t.Fatal("inject_scenario kind enum must not be empty")
		}
		found := false
		for _, k := range enum {
			if k == virtual_city.ScenarioGoodsPriceShock {
				found = true
			}
		}
		if !found {
			t.Fatalf("kind enum missing %q: %v", virtual_city.ScenarioGoodsPriceShock, enum)
		}
		return
	}
	t.Fatal("inject_scenario tool not found")
}

// TestAgentClassCitySystem_Wired §24 登记合规(常量 + AllAgentClassNames)。
func TestAgentClassCitySystem_Wired(t *testing.T) {
	assertAgentClassWired(t)
}

// TestInjectScenarioKindEnumCoversCatalog kind 枚举 = 场景目录全量
// (批次83 起 101 个场景全覆盖,目录扩缩两侧行同步)。
func TestInjectScenarioKindEnumCoversCatalog(t *testing.T) {
	catalog := virtual_city.ScenarioCatalog()
	if len(catalog) != 101 {
		t.Fatalf("catalog size = %d, want 101", len(catalog))
	}
	enum := scenarioKindEnum()
	if len(enum) != len(catalog) {
		t.Fatalf("kind enum size = %d, want %d", len(enum), len(catalog))
	}
	inEnum := make(map[string]bool, len(enum))
	for _, k := range enum {
		inEnum[k] = true
	}
	for _, spec := range catalog {
		if !inEnum[spec.Kind] {
			t.Errorf("kind enum missing catalog kind %q", spec.Kind)
		}
	}
}

// TestToolSchemas_Bounds inject_scenario 数值参数带准确上下界
// (magnitude 1-5 / duration_months 1-24)。
func TestToolSchemas_Bounds(t *testing.T) {
	for _, d := range BuildTools() {
		if d.Name != ToolInjectScenario {
			continue
		}
		props := d.InputSchema["properties"].(map[string]any)
		mag := props["magnitude"].(map[string]any)
		if mag["minimum"] != 1 || mag["maximum"] != 5 {
			t.Fatalf("magnitude bounds = %v/%v, want 1/5", mag["minimum"], mag["maximum"])
		}
		dur := props["duration_months"].(map[string]any)
		if dur["minimum"] != 1 || dur["maximum"] != 24 {
			t.Fatalf("duration bounds = %v/%v, want 1/24", dur["minimum"], dur["maximum"])
		}
		return
	}
	t.Fatal("inject_scenario tool not found")
}

// TestBuildSystemBlocks_StepDiscipline 步数纪律内联(含 [step i/N] 约定)。
func TestBuildSystemBlocks_StepDiscipline(t *testing.T) {
	all := ""
	for _, b := range BuildSystemBlocks(12) {
		all += b.Text
	}
	for _, want := range []string{"步数纪律", "最多 12 步", "[step i/N]"} {
		if !strings.Contains(all, want) {
			t.Errorf("system blocks missing %q", want)
		}
	}
	// maxSteps 零值兜底 8。
	all = ""
	for _, b := range BuildSystemBlocks(0) {
		all += b.Text
	}
	if !strings.Contains(all, "最多 8 步") {
		t.Error("BuildSystemBlocks(0) must default to 8 steps")
	}
}
