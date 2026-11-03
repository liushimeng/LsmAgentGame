// Package citysystem — tools.go: 11 个工具的 Anthropic wire 定义
// (2026-11-03 §批次81)。
//
// 契约: 批次81 方案文档 §3.2「10 个工具」表 + list_agents 元工具。
// 工具名常量集中在本文件,ToolNames() 供单测钉死(防「声明了却从不接线」§130)。
//
// kind 的 enum 从 virtual_city.ScenarioCatalog() **动态生成** —— 批次81
// 场景目录扩到 40 个后,无需同步改 wire 定义。
package citysystem

import (
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
)

// 工具名常量(11 个)。
const (
	ToolInjectScenario       = "inject_scenario"
	ToolListScenarios        = "list_scenarios"
	ToolQueryCityState       = "query_city_state"
	ToolQueryWorldEvents     = "query_world_events"
	ToolQueryPrediction      = "query_prediction_report"
	ToolControlSimulation    = "control_simulation"
	ToolLaunchSurvey         = "launch_survey"
	ToolFocusCamera          = "focus_camera"
	ToolCollectRealworldData = "collect_realworld_data"
	ToolSendAgentMessage     = "send_agent_message"
	ToolListAgents           = "list_agents"
)

// queryCityStateScopes 是 query_city_state 的 scope 枚举。
var queryCityStateScopes = []string{"overview", "market", "labor", "treasury", "election", "all"}

// controlSimulationActions 是 control_simulation 的 action 枚举。
var controlSimulationActions = []string{"pause", "resume", "status"}

// displayModes 是 focus_camera 的 mode 枚举(与 citywebgl 显示原语一致)。
var displayModes = []string{"orbit", "fly"}

// realworldCategories 是 collect_realworld_data 的 category 枚举
// (与 citychrome 采集类目一致;空 = 由采集 Agent 自行选择)。
var realworldCategories = []string{
	"a_share", "hk", "us", "metal", "futures", "crypto", "macro", "news",
}

func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// intSchema 带上下界的整数 schema(maximum <= 0 表示不设上界)。
func intSchema(minimum, maximum int, desc string) map[string]any {
	s := map[string]any{"type": "integer", "minimum": minimum, "description": desc}
	if maximum > 0 {
		s["maximum"] = maximum
	}
	return s
}

func enumStrSchema(values []string, desc string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}

func strArraySchema(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": desc,
	}
}

// scenarioKindEnum 从场景目录动态生成 kind 枚举(目录为空时返回空切片,
// 由 ApplyScenario 侧报 35055)。
func scenarioKindEnum() []string {
	catalog := virtual_city.ScenarioCatalog()
	out := make([]string, 0, len(catalog))
	for _, spec := range catalog {
		out = append(out, spec.Kind)
	}
	return out
}

// BuildTools 返回全部 11 个工具定义(每次调用重新生成 kind 枚举)。
func BuildTools() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name: ToolInjectScenario,
			Description: "向运行中的城市注入一个场景(条件触发)。kind 必填(见场景目录)," +
				"magnitude 强度 L1-L5(缺省用目录默认强度),duration_months 持续月数(1-24)," +
				"target 作用范围(城区/行业/商品类目,可空=全域),note 触发者备注(≤60字)。" +
				"返回即时效果列表与注入记录。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":            enumStrSchema(scenarioKindEnum(), "场景 kind(必须来自场景目录)"),
					"magnitude":       intSchema(1, 5, "强度档 L1-L5(1 最轻 / 5 最重;缺省用目录默认)"),
					"duration_months": intSchema(1, 24, "持续月数(1-24;缺省用目录默认)"),
					"target":          strSchema("作用范围:城区 / 行业 / 商品类目(可空 = 全域)"),
					"note":            strSchema("触发者备注(≤60 字)"),
				},
				"required": []string{"kind"},
			},
		},
		{
			Name: ToolListScenarios,
			Description: "列出全部可注入场景(分组 / kind / 名称 / 默认强度 / 描述 / 参数)。" +
				"系统提示词已内联目录,仅在需要完整参数或目录可能过期时调用。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolQueryCityState,
			Description: "查询城市当前状态摘要(只读,不消耗任何资源)。" +
				"scope=overview 总览 / market 市场 / labor 劳动力 / treasury 财政 / " +
				"election 选举 / all 全部。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": enumStrSchema(queryCityStateScopes, "查询范围(缺省 overview)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolQueryWorldEvents,
			Description: "查询真实世界事件流(批次80 自动注入引擎的反馈链):" +
				"事件标题 / 来源 / 是否已注入 / 即时效果 / 次月城市结果。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolQueryPrediction,
			Description: "查询预测运行报告(批次68)。run_id 空 = 最新一次运行;" +
				"无任何运行返回「暂无预测运行」。返回文本摘要并同时下发报告卡片数据。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id": strSchema("预测运行 id(可空 = 最新)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolControlSimulation,
			Description: "控制模拟运行:action=pause 暂停月结 / resume 恢复 / " +
				"status 查询当前状态。暂停权限仅限房主(35011)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": enumStrSchema(controlSimulationActions, "pause=暂停 / resume=恢复 / status=查询"),
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        ToolLaunchSurvey,
			Description: "向全城居民发起一次社会调研(2-6 个选项,每房同时至多 1 个进行中之调研)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": strSchema("调研问题(≤100 字)"),
					"options":  strArraySchema("选项列表(2-6 个,每个 ≤40 字)"),
				},
				"required": []string{"question", "options"},
			},
		},
		{
			Name: ToolFocusCamera,
			Description: "把 3D 视角拉到指定位置(经 City-WebGL-Show 翻译为结构化显示指令)。" +
				"district 与 (x,z) 二选一;mode 可指定 orbit 环绕 / fly 飞行。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"district": strSchema("目标城区 id(与 x/z 二选一)"),
					"x":        map[string]any{"type": "number", "description": "目标点 x 坐标(与 z 同给)"},
					"z":        map[string]any{"type": "number", "description": "目标点 z 坐标(与 x 同给)"},
					"mode":     enumStrSchema(displayModes, "相机模式:orbit 环绕 / fly 飞行(可空)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolCollectRealworldData,
			Description: "触发一次真实世界数据采集(经 City-Chrome 采集 Agent 执行)。" +
				"category 可空,由采集 Agent 自行选择类目。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": enumStrSchema(realworldCategories, "采集类目(可空 = 自动选择)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolSendAgentMessage,
			Description: "向指定 Agent 发送一条消息(单向投递)。" +
				"target_agent 必须是 list_agents 返回的 AgentClassName。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_agent": strSchema("目标 Agent 类名(见 list_agents)"),
					"message":      strSchema("消息文本"),
				},
				"required": []string{"target_agent", "message"},
			},
		},
		{
			Name:        ToolListAgents,
			Description: "列出当前已注册的 Agent 类名(不含房间级实例),用于 send_agent_message 选目标。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// ToolNames 返回全部工具名(测试钉死;合计 11)。
func ToolNames() []string {
	return []string{
		ToolInjectScenario, ToolListScenarios, ToolQueryCityState, ToolQueryWorldEvents,
		ToolQueryPrediction, ToolControlSimulation, ToolLaunchSurvey, ToolFocusCamera,
		ToolCollectRealworldData, ToolSendAgentMessage, ToolListAgents,
	}
}
