// Package citysystem — prompt_test.go: 系统提示词与工具清单单测
// (2026-11-03 §批次81)。
package citysystem

import (
	"strings"
	"testing"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/game/virtual_city"
)

// TestScenarioCatalogText 目录文本非空、含分组与已知 kind。
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
	// 场景目录已内联(不必先调 list_scenarios)。
	if !strings.Contains(all, virtual_city.ScenarioGoodsPriceShock) {
		t.Error("system blocks missing inline scenario catalog")
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
// (40 个场景全覆盖,目录扩缩两侧行同步)。
func TestInjectScenarioKindEnumCoversCatalog(t *testing.T) {
	catalog := virtual_city.ScenarioCatalog()
	if len(catalog) != 40 {
		t.Fatalf("catalog size = %d, want 40", len(catalog))
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
