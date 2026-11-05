// Package citysystem — prompt.go: 系统提示词角色段 + 维度索引内联 + 来信段
// (2026-11-03 §批次81;2026-11-05 §批次84「目录内联改维度索引」)。
//
// 契约: 批次81 方案文档 §3.2「系统提示词」;批次83 方案文档 §4.2
// 「目录内联改紧凑索引 + list_scenarios(group)」;
// 批次84 方案文档 §2.2(阻塞点 B)/ §4.2 / §4.3.2(来信段)。
//
// 批次83 的紧凑索引(ScenarioCatalogIndex)把**全目录**内联进 system ——
// 目录 101 条时 ≈3 KB 尚可,批次84 扩到 1000+ 条后 ≈30 KB 直接撑爆请求体
// 且与 §14.3「三段式头逐字节稳定」冲突(目录一变前缀就变,cache 全失效)。
//
// 批次84 改为**维度索引**(ScenarioDimensionIndexText):每维度一行
// 「维度名 · 条目数 · 代表 kind」,**体积恒定**(13 行 ≈800 B,目录怎么扩容
// 都不变);具体 kind 一律经 `search_scenarios` 检索,不再内联。
//
// 来信段(§4.3.2):下一轮构造 system 时注入「【来自其它 Agent 的消息】」
// (≤5 条,时间倒序);来信为空不加该段。
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

// roleText 是 City-System Agent 角色段(方案文档 §3.2 逐条落地;
// 批次84 更新为「维度索引 + 检索工具」口径)。
const roleText = `你是 LsmAgentGame-City-System,虚拟城市系统主 Agent。

【城市背景】
虚拟城市是**全 Agent 真实城市模拟系统**(非游戏):由 LsmAgentGame-City-Human
居民 Agent 自主决策、真实世界数据由 LsmAgentGame-City-Chrome 采集、
3D 画面由 LsmAgentGame-City-WebGL-Show 渲染。人类是**观察者 + 条件触发者**,
通过与你对话来控制这座城市。

【能力边界(硬约束)】
1. 你**只能**通过工具影响城市:不得凭空描述未由工具返回的指标。
2. **禁止编造数据**:所有数字必须来自工具结果(城市状态 / 指标历史 / 预测报告 /
   世界事件 / 场景目录),没有依据就说「暂未取到」。
3. 注入场景前必须确认 **kind 与强度档**:kind 必须先用 search_scenarios
   **检索**得到(目录上万条,禁止凭记忆写 kind),强度 L1-L5
   (L1 轻微 / L3 中等 / L5 极端);用户没说强度时按目录默认强度并**先告知**
   再注入,不要自行放大。
4. 每次工具调用前后用中文简述:先说**要做什么**,再调工具;最后说
   **做了什么 + 关键指标**(如 CPI / 失业率 / 股指 / 国库)。
5. 你不得代替人类做不可逆操作;本批工具均为可逆或可观测。
6. 输出一律中文、简洁,避免复述工具原始 JSON。
7. 查询类信息一律经工具获取(query_city_state / query_metric_history /
   query_districts / query_surveys / query_prediction_report / query_world_events /
   list_scenarios / search_scenarios);要拉真实世界数据用 collect_realworld_data,
   要看 3D 视角用 focus_camera / apply_display。
8. 与其它 Agent 协作:问问题用 ask_agent(等待回复,超时 30s 回「未在时限内
   回应」),发通知用 send_agent_message(不等回复);对方上行的来信会以
   「Agent 来信」段出现在下方,供你在下一轮作答时参考。

【场景维度索引(目录规模可达万条;具体 kind 一律用 search_scenarios 检索)】`

// scenarioGroupOrder 六个分组的固定展示顺序(与场景目录文件注释及
// ScenarioGroup* 常量一致);list_scenarios 的 group 校验同源。
//
// 批次84:分组集由 virtual_city.ScenarioGroupOrder() 动态给出(13 组,
// 既有 6 组 + 新增 7 组);本变量仅保留**既有 6 组**常量引用以免误改。
var scenarioGroupOrder = []string{
	virtual_city.ScenarioGroupMarket,
	virtual_city.ScenarioGroupPolicy,
	virtual_city.ScenarioGroupSociety,
	virtual_city.ScenarioGroupNature,
	virtual_city.ScenarioGroupUnknown,
	virtual_city.ScenarioGroupTech,
}

// knownScenarioGroups 返回当前全部合法分组(批次84 起 13 组;
// 来自 virtual_city.ScenarioGroupOrder() 的稳定副本)。
func knownScenarioGroups() []string {
	if g := virtual_city.ScenarioGroupOrder(); len(g) > 0 {
		return g
	}
	// 防御:上游未给出分组表时回退既有 6 组。
	return append([]string(nil), scenarioGroupOrder...)
}

// scenarioGroupKnown 报告 group 是否合法(空串 = 全部,恒合法)。
func scenarioGroupKnown(group string) bool {
	if group == "" {
		return true
	}
	for _, g := range knownScenarioGroups() {
		if g == group {
			return true
		}
	}
	return false
}

// groupSpecsByOrder 把目录按固定分组顺序聚组;不在分组表内的未知分组
// (防御未来扩组)按首次出现顺序追加在末尾,保证条目不丢失。
func groupSpecsByOrder(catalog []virtual_city.ScenarioSpec) []struct {
	group string
	specs []virtual_city.ScenarioSpec
} {
	order := knownScenarioGroups()
	byGroup := make(map[string][]virtual_city.ScenarioSpec, len(order))
	extra := make([]string, 0, 2)
	for _, spec := range catalog {
		known := false
		for _, g := range order {
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
	order = append(append([]string(nil), order...), extra...)
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

// ScenarioDimensionIndexText 渲染**维度索引**(批次84 §4.2):每维度一行
// 「- 维度名 · 条目数 · 代表 kind:kind1 / kind2 / kind3」。
//
// **体积恒定**:每组最多 3 个代表 kind(来自 ScenarioDimensionIndex),
// 13 组 ≈800 B —— 目录扩容到万条也不变,是 system 前缀稳定的关键。
//
// 目录为空时返回明确提示(不返回空串,避免 system 段出现空块)。
func ScenarioDimensionIndexText() string {
	dims := virtual_city.ScenarioDimensionIndex()
	if len(dims) == 0 {
		return "(场景目录暂不可用)"
	}
	var b strings.Builder
	total := virtual_city.ScenarioCatalogCount()
	fmt.Fprintf(&b, "共 %d 个场景 · %d 个维度;每行「维度 · 条目数 · 代表 kind」:\n",
		total, len(dims))
	for _, d := range dims {
		line := "- " + d.Group + " · " + strconv.Itoa(d.Count) + " 个"
		if len(d.Samples) > 0 {
			line += " · 代表:" + strings.Join(d.Samples, " / ")
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("需要某个具体 kind 时用 search_scenarios(query=关键词,group=维度名) 检索。")
	return b.String()
}

// ScenarioCatalogIndex 渲染**全目录紧凑索引**(批次83 §4.2)。
//
// ⚠ **已废弃,不再进 system 提示词**(批次84 §2.2 阻塞点 B:目录扩容到
// 1000+ 条后索引会撑爆请求体并破坏 §14.3 的 prompt cache 前缀)。保留为
// 导出函数仅供**调试 / 单测**对照全目录用;BuildSystemBlocks 不再调用它,
// system 提示词里也不会出现全目录 kind(单测钉死)。
//
// 组头 `[组名] N 个` + 每行 `- kind 名称(L3;参数:goods)`,不含描述。
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
		return "", fmt.Errorf("未知分组 %s,可用分组:%s", group, strings.Join(knownScenarioGroups(), "|"))
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

// incomingSectionText 渲染「【来自其它 Agent 的消息】」段(批次84 §4.3.2)。
//
// 每行「- 来自 <AgentClassName>:<正文摘要>」;正文按 rune 截断到
// incomingTextMax 防单条来信撑爆 system。msgs 为空返回空串(调用方不加段)。
func incomingSectionText(msgs []IncomingMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n【来自其它 Agent 的消息】\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "- 来自 %s:%s\n", m.From, truncateRunes(m.Text, incomingTextMax))
	}
	b.WriteString("(以上消息来自其它 Agent 的主动上行,供你参考;需要追问请用 ask_agent。)")
	return b.String()
}

// incomingTextMax 是单条来信正文注入 system 的截断长度(rune)。
const incomingTextMax = 200

// BuildSystemBlocks 构造出站 system 段(批次84):角色段 + 步数纪律 +
// **维度索引**(恒定体积,不再内联全目录)+ 可选**来信段**,再经
// sysprompt.EnsureHead 注入三段式标准头(§14.3,幂等)。
//
// incoming 为 nil / 空时不加来信段(§4.3.2)。
//
// 三段式头只在 sysprompt.EnsureHead 注入(§14.3 单一注入点),本函数
// **不自己拼**标准头。maxSteps <= 0 按 8(与 Config 缺省一致)。
func BuildSystemBlocks(maxSteps int, incoming []IncomingMessage) []llmtypes.SystemBlock {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	discipline := fmt.Sprintf(
		"\n【步数纪律】单轮最多 %d 步工具调用;每个工具结果带 [step i/N] 进度前缀。"+
			"剩余步数不多时直接总结作答,不要铺开新调查。", maxSteps)
	body := roleText + discipline + "\n" + ScenarioDimensionIndexText()
	if s := incomingSectionText(incoming); s != "" {
		body += "\n" + s
	}
	return sysprompt.EnsureHead([]llmtypes.SystemBlock{
		{Type: "text", Text: body},
	}, string(agentroot.AgentClassCitySystem))
}
