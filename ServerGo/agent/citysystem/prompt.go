// Package citysystem — prompt.go: 系统提示词角色段 + 场景目录内联文本
// (2026-11-03 §批次81;2026-11-04 §批次83 目录索引化改造)。
//
// 契约: 批次81 方案文档 §3.2「系统提示词」;批次83 方案文档 §4.2
// 「目录内联改紧凑索引 + list_scenarios(group)」。
//
// 场景目录以**紧凑索引**内联进 system(ScenarioCatalogIndex:kind/名称/
// 默认强度/参数,不含描述),让 Agent 不必先调 list_scenarios 才知道有哪些
// 场景;完整描述与参数经 list_scenarios(group=组名) 按组获取
// (ScenarioCatalogDetail,单组体量 < tool_result_limit)。目录再扩容只改
// scenario_catalog_batch8X.go 与本文件两处同源,不触碰三段式头(§14.3)。
package citysystem

import (
	"fmt"
	"strconv"
	"strings"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/game/virtual_city"
	"LsmAgentGame/llm/sysprompt"
	llmtypes "LsmAgentGame/llm/types"
)

// roleText 是 City-System Agent 角色段(方案文档 §3.2 逐条落地)。
const roleText = `你是 LsmAgentGame-City-System,虚拟城市系统主 Agent。

【城市背景】
虚拟城市是**全 Agent 真实城市模拟系统**(非游戏):由 LsmAgentGame-City-Human
居民 Agent 自主决策、真实世界数据由 LsmAgentGame-City-Chrome 采集、
3D 画面由 LsmAgentGame-City-WebGL-Show 渲染。人类是**观察者 + 条件触发者**,
通过与你对话来控制这座城市。

【能力边界(硬约束)】
1. 你**只能**通过工具影响城市:不得凭空描述未由工具返回的指标。
2. **禁止编造数据**:所有数字必须来自工具结果(城市状态 / 预测报告 /
   世界事件 / 场景目录),没有依据就说「暂未取到」。
3. 注入场景前必须确认 **kind 与强度档**:kind 从下方场景目录选,强度 L1-L5
   (L1 轻微 / L3 中等 / L5 极端);用户没说强度时按目录默认强度并**先告知**
   再注入,不要自行放大。
4. 每次工具调用前后用中文简述:先说**要做什么**,再调工具;最后说
   **做了什么 + 关键指标**(如 CPI / 失业率 / 股指 / 国库)。
5. 你不得代替人类做不可逆操作;本批工具均为可逆或可观测。
6. 输出一律中文、简洁,避免复述工具原始 JSON。
7. 查询类信息一律经工具获取(query_city_state / query_prediction_report /
   query_world_events / list_scenarios);要拉真实世界数据用
   collect_realworld_data,要看 3D 视角用 focus_camera。

【场景目录(紧凑索引;完整描述与参数用 list_scenarios(group=组名) 按组获取)】`

// scenarioGroupOrder 六个分组的固定展示顺序(与场景目录文件注释及
// ScenarioGroup* 常量一致);list_scenarios 的 group 枚举同源。
var scenarioGroupOrder = []string{
	virtual_city.ScenarioGroupMarket,
	virtual_city.ScenarioGroupPolicy,
	virtual_city.ScenarioGroupSociety,
	virtual_city.ScenarioGroupNature,
	virtual_city.ScenarioGroupUnknown,
	virtual_city.ScenarioGroupTech,
}

// scenarioGroupKnown 报告 group 是否合法(空串 = 全部,恒合法)。
func scenarioGroupKnown(group string) bool {
	if group == "" {
		return true
	}
	for _, g := range scenarioGroupOrder {
		if g == group {
			return true
		}
	}
	return false
}

// groupSpecsByOrder 把目录按固定分组顺序聚组;不在六常量内的未知分组
// (防御未来扩组)按首次出现顺序追加在末尾,保证条目不丢失。
func groupSpecsByOrder(catalog []virtual_city.ScenarioSpec) []struct {
	group string
	specs []virtual_city.ScenarioSpec
} {
	byGroup := make(map[string][]virtual_city.ScenarioSpec, len(scenarioGroupOrder))
	extra := make([]string, 0, 2)
	for _, spec := range catalog {
		known := false
		for _, g := range scenarioGroupOrder {
			if g == spec.Group {
				known = true
				break
			}
		}
		if !known {
			extra = append(extra, spec.Group)
		}
		byGroup[spec.Group] = append(byGroup[spec.Group], spec)
	}
	order := append([]string(nil), scenarioGroupOrder...)
	order = append(order, extra...)
	out := make([]struct {
		group string
		specs []virtual_city.ScenarioSpec
	}, 0, len(order))
	for _, g := range order {
		if specs := byGroup[g]; len(specs) > 0 {
			out = append(out, struct {
				group string
				specs []virtual_city.ScenarioSpec
			}{g, specs})
		}
	}
	return out
}

// ScenarioCatalogIndex 把全目录渲染成**紧凑索引**(批次83 §4.2):
// 组头 `[组名] N 个` + 每行 `- kind 名称(L3;参数:goods)`,
// **不含描述**(101 个场景全描述内联 ≈ 10k+ rune 会撑爆 system 前缀;
// 索引化后 ≈ 3k rune 且随目录扩容线性可控)。
//
// 目录为空时返回明确提示(不返回空串,避免 system 段出现空块)。
func ScenarioCatalogIndex() string {
	catalog := virtual_city.ScenarioCatalog()
	if len(catalog) == 0 {
		return "(场景目录暂不可用)"
	}
	var b strings.Builder
	for _, g := range groupSpecsByOrder(catalog) {
		fmt.Fprintf(&b, "\n[%s] %d 个\n", g.group, len(g.specs))
		for _, spec := range g.specs {
			b.WriteString("- " + spec.Kind + " " + spec.Name + "(L" + strconv.Itoa(spec.DefaultMagnitude))
			if len(spec.Params) > 0 {
				b.WriteString(";参数:" + strings.Join(spec.Params, "|"))
			}
			b.WriteString(")\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ScenarioCatalogDetail 渲染指定分组的**完整条目**(kind/名称/默认强度/
// 描述/参数,格式沿用批次81 的全量渲染)。group 为空串返回全部分组详情
// (兼容旧 ScenarioCatalogText 语义);未知组名返回错误(调用方转失败
// 文本回灌模型)。目录为空时返回明确提示。
func ScenarioCatalogDetail(group string) (string, error) {
	if !scenarioGroupKnown(group) {
		return "", fmt.Errorf("未知分组 %s,可用分组:%s", group, strings.Join(scenarioGroupOrder, "|"))
	}
	catalog := virtual_city.ScenarioCatalog()
	if len(catalog) == 0 {
		return "(场景目录暂不可用)", nil
	}
	var b strings.Builder
	for _, g := range groupSpecsByOrder(catalog) {
		if group != "" && g.group != group {
			continue
		}
		fmt.Fprintf(&b, "\n[%s]\n", g.group)
		for _, spec := range g.specs {
			fmt.Fprintf(&b, "- %s %s(默认 L%d):%s",
				spec.Kind, spec.Name, spec.DefaultMagnitude, spec.Description)
			if len(spec.Params) > 0 {
				fmt.Fprintf(&b, " 参数:%s", strings.Join(spec.Params, "|"))
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ScenarioCatalogText 批次83 前的唯一渲染入口;现保留为 Detail 的薄包装
// (group="" = 全部分组详情),供既有测试锚点与兼容调用使用。
func ScenarioCatalogText() string {
	text, _ := ScenarioCatalogDetail("")
	return text
}

// BuildSystemBlocks 构造出站 system 段:角色段 + 步数纪律 + 场景目录紧凑
// 索引(批次83 §4.2),再经 sysprompt.EnsureHead 注入三段式标准头(§14.3,
// 含计费元数据 / 基础身份 / 核心行为规则;幂等,重复调用不会叠加)。
//
// 三段式头只在 sysprompt.EnsureHead 注入(§14.3 单一注入点),本函数
// **不自己拼**标准头。maxSteps <= 0 按 8(与 Config 缺省一致)。
func BuildSystemBlocks(maxSteps int) []llmtypes.SystemBlock {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	discipline := fmt.Sprintf(
		"\n【步数纪律】单轮最多 %d 步工具调用;每个工具结果带 [step i/N] 进度前缀。"+
			"剩余步数不多时直接总结作答,不要铺开新调查。", maxSteps)
	body := roleText + discipline + "\n" + ScenarioCatalogIndex()
	return sysprompt.EnsureHead([]llmtypes.SystemBlock{
		{Type: "text", Text: body},
	}, string(agentroot.AgentClassCitySystem))
}
