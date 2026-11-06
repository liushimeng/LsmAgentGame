// Package citysystem — prompt_test.go: 系统提示词与工具清单单测
// (2026-11-03 §批次81;2026-11-05 §批次84 维度索引 + 17 工具 + 来信段)。
package citysystem

import (
	"strings"
	"testing"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
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

// TestScenarioDimensionIndexText 维度索引(批次84 §4.2):每维度一行
// 「维度名 · 条目数 · 代表 kind」,**体积恒定**(≤4KB,不随目录条目数膨胀)。
func TestScenarioDimensionIndexText(t *testing.T) {
	idx := ScenarioDimensionIndexText()
	if idx == "" {
		t.Fatal("ScenarioDimensionIndexText must not be empty")
	}
	// 13 个维度各一行。
	for _, g := range virtual_city.ScenarioGroupOrder() {
		if !strings.Contains(idx, g) {
			t.Errorf("dimension index missing group %q", g)
		}
	}
	// 体积恒定:不随目录条目数膨胀(契约 §4.1.4 单测钉死 ≤ 4KB)。
	if n := len([]rune(idx)); n > 4000 {
		t.Errorf("dimension index too large: %d runes (want <= 4000)", n)
	}
	// 不含描述片段(描述只能经 search_scenarios / list_scenarios(group) 取)。
	for _, banned := range []string{
		"城区建筑受损",     // earthquake 描述片段
		"央行连续加息抑制通胀", // interest_rate_hike 描述片段
		"地缘核对抗白热化",   // nuclear_brinkmanship 描述片段
	} {
		if strings.Contains(idx, banned) {
			t.Errorf("dimension index must not contain description fragment %q", banned)
		}
	}
}

// TestScenarioCatalogIndex_Deprecated 全目录紧凑索引**不再进 system**
// (批次84 §2.2 阻塞点 B)。函数保留仅供调试/对照,本测试钉死其注释口径:
// 它必须能渲染全目录(对照用),但 BuildSystemBlocks 不再调用它。
func TestScenarioCatalogIndex_Deprecated(t *testing.T) {
	idx := ScenarioCatalogIndex()
	if idx == "" {
		t.Fatal("ScenarioCatalogIndex must not be empty")
	}
	if !strings.Contains(idx, "- "+virtual_city.ScenarioGoodsPriceShock) {
		t.Error("legacy index must still render kind lines (for debugging)")
	}
	// 关键:它**不得**出现在 system 提示词里(见 TestBuildSystemBlocks_NoFullCatalog)。
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
	// 空串 = 全部分组(13 个组头齐全,与旧 ScenarioCatalogText 语义一致)。
	all, err := ScenarioCatalogDetail("")
	if err != nil {
		t.Fatalf("detail(\"\") error: %v", err)
	}
	for _, g := range virtual_city.ScenarioGroupOrder() {
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

// TestListScenariosGroupSchema list_scenarios 的 group 描述说明缺省维度索引 /
// 传组名该组条目(批次84 §4.2);不再硬编码分组枚举(分组随目录扩容可变)。
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
		if _, hasEnum := group["enum"]; hasEnum {
			t.Fatal("list_scenarios group must not hardcode an enum (groups grow with catalog)")
		}
		if !strings.Contains(d.Description, "维度索引") {
			t.Errorf("description must mention 维度索引: %q", d.Description)
		}
		return
	}
	t.Fatal("list_scenarios tool not found")
}

// TestBuildSystemBlocks 三段式头 + 角色段 + **维度索引**(不再内联全目录)。
func TestBuildSystemBlocks(t *testing.T) {
	blocks := BuildSystemBlocks(8, nil)
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
	// 批次84:内联**维度索引**(恒定体积),不再内联全目录条目行。
	if !strings.Contains(all, "维度") {
		t.Error("system blocks must inline the scenario dimension index")
	}
	if !strings.Contains(all, "search_scenarios") {
		t.Error("role text must guide kind lookup via search_scenarios")
	}
	// 无来信时不出现来信段。
	if strings.Contains(all, "来自其它 Agent 的消息") {
		t.Error("system must not contain the incoming section when no incoming messages")
	}
}

// TestBuildSystemBlocks_NoFullCatalog 批次84 核心断言(§2.2 阻塞点 B):
// system 全文 ≤ 8 KB,且**不含全目录 kind** —— 用一个明显的哨兵 kind
// 断言「不出现」(全目录索引里的行会带 "- <kind> <名称>(L…)" 形状)。
func TestBuildSystemBlocks_NoFullCatalog(t *testing.T) {
	blocks := BuildSystemBlocks(8, nil)
	all := ""
	for _, b := range blocks {
		all += b.Text
	}
	// ① 体积上限 8KB。
	if n := len([]byte(all)); n > 8*1024 {
		t.Fatalf("system text %d bytes, want <= 8KB", n)
	}
	// ② 哨兵 kind 不出现(目录里真实存在,但**不是**维度索引的代表 kind)。
	catalog := virtual_city.ScenarioCatalog()
	if len(catalog) == 0 {
		t.Skip("catalog empty")
	}
	samples := map[string]bool{}
	for _, d := range virtual_city.ScenarioDimensionIndex() {
		for _, s := range d.Samples {
			samples[s] = true
		}
	}
	sentinel := ""
	for _, spec := range catalog {
		if !samples[spec.Kind] {
			sentinel = spec.Kind
			break
		}
	}
	if sentinel == "" {
		t.Fatal("no non-sample kind found to use as sentinel")
	}
	if strings.Contains(all, sentinel) {
		t.Fatalf("system must not inline full catalog: found sentinel kind %q", sentinel)
	}
	// ③ 全目录索引形状不得出现(组头 + 条目计数)。
	for _, g := range virtual_city.ScenarioGroupOrder() {
		if strings.Contains(all, "["+g+"]") {
			t.Errorf("system must not contain catalog group header [%s]", g)
		}
	}
}

// TestBuildSystemBlocks_IncomingSection 来信段(批次84 §4.3.2):
// 有来信时注入「【来自其它 Agent 的消息】」段,正文截断;无来信时不加。
func TestBuildSystemBlocks_IncomingSection(t *testing.T) {
	incoming := []IncomingMessage{
		{From: "LsmAgentGame-City-Chrome", Text: "已采集到今日 A 股行情", Ts: 1730000000000},
		{From: "LsmAgentGame-City-Human", Text: "居民报告物价上涨", Ts: 1730000001000},
	}
	blocks := BuildSystemBlocks(8, incoming)
	all := ""
	for _, b := range blocks {
		all += b.Text
	}
	if !strings.Contains(all, "【来自其它 Agent 的消息】") {
		t.Fatal("system missing incoming section")
	}
	if !strings.Contains(all, "LsmAgentGame-City-Chrome") ||
		!strings.Contains(all, "已采集到今日 A 股行情") {
		t.Error("incoming section must render from + text")
	}
	// 空来信 ⇒ 无来信段。
	empty := BuildSystemBlocks(8, nil)
	emptyAll := ""
	for _, b := range empty {
		emptyAll += b.Text
	}
	if strings.Contains(emptyAll, "【来自其它 Agent 的消息】") {
		t.Error("empty incoming must not render the section")
	}
}

// TestIncomingSectionText_Truncate 单条来信正文按 rune 截断(防撑爆 system)。
func TestIncomingSectionText_Truncate(t *testing.T) {
	long := strings.Repeat("长", incomingTextMax+100)
	s := incomingSectionText([]IncomingMessage{{From: "A", Text: long}})
	if len([]rune(s)) > incomingTextMax+200 {
		t.Errorf("incoming section too long: %d runes", len([]rune(s)))
	}
	if incomingSectionText(nil) != "" {
		t.Error("nil incoming must render empty string")
	}
}

// TestBuildSystemBlocks_Idempotent EnsureHead 幂等(重复调用不叠加标准头)。
func TestBuildSystemBlocks_Idempotent(t *testing.T) {
	a := BuildSystemBlocks(8, nil)
	b := BuildSystemBlocks(8, nil)
	if len(a) != len(b) {
		t.Fatalf("block count differs: %d vs %d", len(a), len(b))
	}
	if len(a) < 2 {
		t.Fatalf("expected >= 2 blocks (head + role), got %d", len(a))
	}
}

// TestToolNames 工具清单 = **17**(批次84 §4.2),且与 BuildTools 一一对应。
func TestToolNames(t *testing.T) {
	names := ToolNames()
	if len(names) != 17 {
		t.Fatalf("ToolNames len = %d, want 17", len(names))
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
	// 17 个工具名逐个在场(防「声明了却从不接线」§130)。
	for _, want := range []string{
		ToolInjectScenario, ToolSearchScenarios, ToolListScenarios, ToolQueryCityState,
		ToolQueryMetricHistory, ToolQueryDistricts, ToolQueryWorldEvents, ToolQueryPrediction,
		ToolQuerySurveys, ToolControlSimulation, ToolLaunchSurvey, ToolFocusCamera,
		ToolApplyDisplay, ToolCollectRealworldData, ToolAskAgent, ToolSendAgentMessage,
		ToolListAgents,
	} {
		if !seen[want] {
			t.Errorf("ToolNames missing %q", want)
		}
	}
}

// TestInjectScenarioKindNoEnum 批次84 §2.1 阻塞点 A:inject_scenario 的
// kind **不得**带 enum(目录可达万条,枚举会撑爆请求体)。
func TestInjectScenarioKindNoEnum(t *testing.T) {
	for _, d := range BuildTools() {
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
		if _, hasEnum := kind["enum"]; hasEnum {
			t.Fatal("inject_scenario.kind must NOT have enum (batch84 §2.1)")
		}
		if kind["type"] != "string" {
			t.Fatalf("kind type = %v, want string", kind["type"])
		}
		if !strings.Contains(d.Description, "search_scenarios") {
			t.Error("inject_scenario description must guide search_scenarios")
		}
		return
	}
	t.Fatal("inject_scenario tool not found")
}

// TestAgentClassCitySystem_Wired §24 登记合规(常量 + AllAgentClassNames)。
func TestAgentClassCitySystem_Wired(t *testing.T) {
	assertAgentClassWired(t)
}

// TestToolSchemas_Bounds inject_scenario 数值参数带准确上下界
// (magnitude 1-5 / duration_months 1-24);query_metric_history months 1-60。
func TestToolSchemas_Bounds(t *testing.T) {
	defs := BuildTools()
	byName := map[string]llmtypes.ToolDef{}
	for _, d := range defs {
		byName[d.Name] = d
	}
	inj, ok := byName[ToolInjectScenario]
	if !ok {
		t.Fatal("inject_scenario tool not found")
	}
	props := inj.InputSchema["properties"].(map[string]any)
	mag := props["magnitude"].(map[string]any)
	if mag["minimum"] != 1 || mag["maximum"] != 5 {
		t.Fatalf("magnitude bounds = %v/%v, want 1/5", mag["minimum"], mag["maximum"])
	}
	dur := props["duration_months"].(map[string]any)
	if dur["minimum"] != 1 || dur["maximum"] != 24 {
		t.Fatalf("duration bounds = %v/%v, want 1/24", dur["minimum"], dur["maximum"])
	}
	mh, ok := byName[ToolQueryMetricHistory]
	if !ok {
		t.Fatal("query_metric_history tool not found")
	}
	mhProps := mh.InputSchema["properties"].(map[string]any)
	months := mhProps["months"].(map[string]any)
	if months["minimum"] != 1 || months["maximum"] != 60 {
		t.Fatalf("months bounds = %v/%v, want 1/60", months["minimum"], months["maximum"])
	}
}

// TestQueryCityStateScopes 19 个 scope(批次85 E2E F1:6 个既有 +
// 13 个维度名全集)。
func TestQueryCityStateScopes(t *testing.T) {
	if len(queryCityStateScopes) != 19 {
		t.Fatalf("scopes = %v, want 19", queryCityStateScopes)
	}
	for _, want := range []string{
		"overview", "market", "labor", "treasury", "election", "all",
		"市场与经济", "政策与治理", "社会与舆情", "自然灾害", "未知灾难", "科技与新文明",
		"公共卫生", "基础设施", "交通物流", "文化教育", "环境生态", "公共安全", "民生日常",
	} {
		found := false
		for _, s := range queryCityStateScopes {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("query_city_state scope missing %q", want)
		}
	}
	// 维度 scope 集合 = 13 个(批次85 E2E F1 补齐 6 个)。
	if len(extendedCityStateScopes) != 13 {
		t.Fatalf("extended scopes = %d, want 13", len(extendedCityStateScopes))
	}
}

// TestBuildSystemBlocks_StepDiscipline 步数纪律内联(含 [step i/N] 约定)。
func TestBuildSystemBlocks_StepDiscipline(t *testing.T) {
	all := ""
	for _, b := range BuildSystemBlocks(12, nil) {
		all += b.Text
	}
	for _, want := range []string{"步数纪律", "最多 12 步", "[step i/N]"} {
		if !strings.Contains(all, want) {
			t.Errorf("system blocks missing %q", want)
		}
	}
	// maxSteps 零值兜底 8。
	all = ""
	for _, b := range BuildSystemBlocks(0, nil) {
		all += b.Text
	}
	if !strings.Contains(all, "最多 8 步") {
		t.Error("BuildSystemBlocks(0) must default to 8 steps")
	}
}
