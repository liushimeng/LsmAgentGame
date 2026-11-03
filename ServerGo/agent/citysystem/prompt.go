// Package citysystem — prompt.go: 系统提示词角色段 + 场景目录内联文本
// (2026-11-03 §批次81)。
//
// 契约: 批次81 方案文档 §3.2「系统提示词」。
//
// 场景目录**内联进 system**(ScenarioCatalogText),让 Agent 不必先调
// list_scenarios 才知道有哪些场景 —— 省一次模型往返,同时保留
// list_scenarios 工具供按需刷新(目录扩展只改 scenario_catalog_batch81.go
// 与本文件,两处同源)。
package citysystem

import (
	"fmt"
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

【场景目录(内联;完整参数以 list_scenarios 工具返回为准)】`

// ScenarioCatalogText 把 virtual_city.ScenarioCatalog() 渲染成紧凑文本:
// 分组 → kind / 名称 / 默认强度 / 描述。
//
// 目录为空时返回明确提示(不返回空串,避免 system 段出现空块)。
func ScenarioCatalogText() string {
	catalog := virtual_city.ScenarioCatalog()
	if len(catalog) == 0 {
		return "(场景目录暂不可用)"
	}
	var b strings.Builder
	groupOrder := make([]string, 0, 4)
	byGroup := make(map[string][]virtual_city.ScenarioSpec)
	for _, spec := range catalog {
		if _, ok := byGroup[spec.Group]; !ok {
			groupOrder = append(groupOrder, spec.Group)
		}
		byGroup[spec.Group] = append(byGroup[spec.Group], spec)
	}
	for _, group := range groupOrder {
		fmt.Fprintf(&b, "\n[%s]\n", group)
		for _, spec := range byGroup[group] {
			fmt.Fprintf(&b, "- %s %s(默认 L%d):%s",
				spec.Kind, spec.Name, spec.DefaultMagnitude, spec.Description)
			if len(spec.Params) > 0 {
				fmt.Fprintf(&b, " 参数:%s", strings.Join(spec.Params, "|"))
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// BuildSystemBlocks 构造出站 system 段:角色段 + 步数纪律 + 场景目录,再经
// sysprompt.EnsureHead 注入三段式标准头(§14.3,含计费元数据 /
// 基础身份 / 核心行为规则;幂等,重复调用不会叠加)。
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
	body := roleText + discipline + "\n" + ScenarioCatalogText()
	return sysprompt.EnsureHead([]llmtypes.SystemBlock{
		{Type: "text", Text: body},
	}, string(agentroot.AgentClassCitySystem))
}
