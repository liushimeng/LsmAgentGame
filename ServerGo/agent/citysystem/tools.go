// Package citysystem — tools.go: 17 个工具的 Anthropic wire 定义
// (2026-11-03 §批次81;2026-11-05 §批次84 工具面 11 → 17)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
//       01-方案设计.md §4.2(工具表) / §4.1.4(目录检索 API)。
//
// 工具名常量集中在本文件,ToolNames() 供单测钉死 **17** 个
// (防「声明了却从不接线」§130);BuildTools() 与 ToolNames() **顺序逐位对齐**
// (单测断言 defs[i].Name == names[i])。
//
// ⚠ 批次84 关键改动(契约 §2.1 / §2.2 阻塞点 A、B):
//   - `inject_scenario.kind` 的 **enum 移除** —— 目录可达 10000 条,枚举会
//     把请求体撑到数百 KB;改为普通 string + 描述引导「先用 search_scenarios 查」。
//   - `list_scenarios` 无参 ⇒ 返回**维度索引**(维度名 · 条目数 · 代表 kind),
//     恒定体积;传 group ⇒ 该组条目(带 limit)。
package citysystem

import (
	llmtypes "LsmAgentGame/llm/types"
)

// 工具名常量(17 个)。
const (
	ToolInjectScenario       = "inject_scenario"
	ToolSearchScenarios      = "search_scenarios"
	ToolListScenarios        = "list_scenarios"
	ToolQueryCityState       = "query_city_state"
	ToolQueryMetricHistory   = "query_metric_history"
	ToolQueryDistricts       = "query_districts"
	ToolQueryWorldEvents     = "query_world_events"
	ToolQueryPrediction      = "query_prediction_report"
	ToolQuerySurveys         = "query_surveys"
	ToolControlSimulation    = "control_simulation"
	ToolLaunchSurvey         = "launch_survey"
	ToolFocusCamera          = "focus_camera"
	ToolApplyDisplay         = "apply_display"
	ToolCollectRealworldData = "collect_realworld_data"
	ToolAskAgent             = "ask_agent"
	ToolSendAgentMessage     = "send_agent_message"
	ToolListAgents           = "list_agents"
)

// queryCityStateScopes 是 query_city_state 的 scope 枚举。
//
// 批次84 扩展 6 → 13(契约 §4.2 #4):新增 7 个维度作用域对应场景目录的
// 7 个新分组(公共卫生 / 基础设施 / 交通物流 / 文化教育 / 环境生态 /
// 公共安全 / 民生日常)。**对应子系统未启用时明确返回「未启用」,绝不编造**。
var queryCityStateScopes = []string{
	"overview", "market", "labor", "treasury", "election", "all",
	"infrastructure", "public_health", "environment", "security",
	"transport", "culture", "daily",
}

// extendedCityStateScopes 是批次84 新增的 7 个 scope(既有 6 个仍由房间侧
// SystemSummaryText 渲染,零回归)。
var extendedCityStateScopes = map[string]bool{
	"infrastructure": true,
	"public_health":  true,
	"environment":    true,
	"security":       true,
	"transport":      true,
	"culture":        true,
	"daily":          true,
}

// metricHistoryMetrics 是 query_metric_history 的 metric 枚举
// (与房间侧指标历史环的采样口径一一对应)。
var metricHistoryMetrics = []string{
	"cpi", "unemployment", "wage", "stock", "gold", "house", "treasury", "satisfaction",
}

// controlSimulationActions 是 control_simulation 的 action 枚举。
var controlSimulationActions = []string{"pause", "resume", "status"}

// displayModes 是 focus_camera 的 mode 枚举(与 citywebgl 显示原语一致)。
var displayModes = []string{"orbit", "fly"}

// realworldCategories 是 collect_realworld_data 的 category 枚举
// (与 citychrome 采集类目一致;空 = 由采集 Agent 自行选择)。
var realworldCategories = []string{
	"a_share", "hk", "us", "metal", "futures", "crypto", "macro", "news",
}

// scenarioSearchLimitDefault / scenarioSearchLimitMax 是目录检索的默认条数
// 与硬上限(契约 §4.1.4;防 tool_result_limit 回灌爆)。
const (
	scenarioSearchLimitDefault = 30
	scenarioSearchLimitMax     = 200
	// scenarioGroupListLimitDefault 是 list_scenarios 单组默认条数。
	scenarioGroupListLimitDefault = 60
)

func strSchema(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

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

func boolSchema(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func numSchema(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func strArraySchema(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"description": desc,
	}
}

// pointsSchema 是 fly_path 的 points 参数 schema(2-8 个 {x,z} 世界坐标点)。
func pointsSchema(desc string) map[string]any {
	return map[string]any{
		"type":        "array",
		"minItems":    flyPathMinPoints,
		"maxItems":    flyPathMaxPoints,
		"description": desc,
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"x": numSchema("x 坐标"),
				"z": numSchema("z 坐标"),
			},
			"required": []string{"x", "z"},
		},
	}
}

// BuildTools 返回全部 17 个工具定义。
//
// ⚠ 与 ToolNames() **顺序逐位对齐**(单测钉死)。
func BuildTools() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name: ToolInjectScenario,
			Description: "向运行中的城市注入一个场景(条件触发)。kind 必填 —— 目录规模很大," +
				"先用 search_scenarios 按关键词/分组检索出确切 kind 再注入;" +
				"magnitude 强度 L1-L5(缺省用目录默认强度),duration_months 持续月数(1-24)," +
				"target 作用范围(城区/行业/商品类目,可空=全域),note 触发者备注(≤60字)。" +
				"返回即时效果列表与注入记录。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					// 批次84:enum 移除(契约 §2.1);kind 由 search_scenarios 提供。
					"kind":            strSchema("场景 kind(先用 search_scenarios 检索目录得到的确切 kind)"),
					"magnitude":       intSchema(1, 5, "强度档 L1-L5(1 最轻 / 5 最重;缺省用目录默认)"),
					"duration_months": intSchema(1, 24, "持续月数(1-24;缺省用目录默认)"),
					"target":          strSchema("作用范围:城区 / 行业 / 商品类目(可空 = 全域)"),
					"note":            strSchema("触发者备注(≤60 字)"),
				},
				"required": []string{"kind"},
			},
		},
		{
			Name: ToolSearchScenarios,
			Description: "检索场景目录(只读)。query 关键词(可空:只按 group 过滤);" +
				"group 分组名(可空:全目录);limit 返回条数(缺省 30,上限 200)。" +
				"返回每个命中条目的 kind / 名称 / 分组 / 默认强度 / 可调参数 / 描述摘要。" +
				"注入场景前先用本工具确定 kind。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": strSchema("关键词(匹配 kind / 名称 / 描述 / 分组;可空)"),
					"group": strSchema("分组名(可空 = 全部);可用分组见系统提示词的场景维度索引"),
					"limit": intSchema(1, scenarioSearchLimitMax, "返回条数(缺省 30,上限 200)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolListScenarios,
			Description: "列出场景目录。不传 group ⇒ 返回**维度索引**(每个维度:条目数 + 代表 kind)," +
				"体积恒定,用于了解目录全貌;传 group ⇒ 返回该分组的条目清单(带 limit,缺省 60,上限 200)。" +
				"需要精确 kind 时用 search_scenarios。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group": strSchema("分组名(可空 = 返回维度索引);可用分组见系统提示词的场景维度索引"),
					"limit": intSchema(1, scenarioSearchLimitMax, "传 group 时的返回条数(缺省 60,上限 200)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolQueryCityState,
			Description: "查询城市当前状态摘要(只读,不消耗任何资源)。" +
				"scope=overview 总览 / market 市场 / labor 劳动力 / treasury 财政 / " +
				"election 选举 / all 全部 / infrastructure 基础设施 / public_health 公共卫生 / " +
				"environment 环境生态 / security 公共安全 / transport 交通物流 / " +
				"culture 文化教育 / daily 民生日常。子系统未启用时明确返回「未启用」。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope": enumStrSchema(queryCityStateScopes, "查询范围(缺省 overview)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolQueryMetricHistory,
			Description: "查询城市指标的**时间序列**(只读;读房间侧指标历史环)。" +
				"metric=cpi 物价 / unemployment 失业率 / wage 工资 / stock 股指 / " +
				"gold 黄金 / house 房价 / treasury 国库现金 / satisfaction 满意度;" +
				"months 回看月数(1-60,缺省 12)。历史不足时返回实际条数。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"metric": enumStrSchema(metricHistoryMetrics, "指标名(缺省 cpi)"),
					"months": intSchema(1, 60, "回看月数(1-60;缺省 12)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolQueryDistricts,
			Description: "查询城区目录(只读):城区 id / 名称 / 人口(按当前所在区统计的存活居民)/ " +
				"房价指数 / 主导产业 / 设施说明。传 district 则只看该城区(id 或名称前缀均可)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"district": strSchema("城区 id 或名称前缀(可空 = 全部城区)"),
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
			Name: ToolQuerySurveys,
			Description: "查询社会调研(只读):调研问题 / 状态 / 截止月 / 各选项的得票数与占比 / " +
				"居民理由摘录。进行中的调研只有问题与选项,结算后才有占比。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
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
			Name: ToolLaunchSurvey,
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
					"x":        numSchema("目标点 x 坐标(与 z 同给)"),
					"z":        numSchema("目标点 z 坐标(与 x 同给)"),
					"mode":     enumStrSchema(displayModes, "相机模式:orbit 环绕 / fly 飞行(可空)"),
				},
				"required": []string{},
			},
		},
		{
			Name: ToolApplyDisplay,
			Description: "高级显示控制(经 City-WebGL-Show 翻译为结构化显示指令):" +
				"phase 时段(dawn 黎明/day 白天/dusk 黄昏/night 夜晚)+ hour 小时(0-24)、" +
				"weather 天气(clear 晴/rain 雨/snow 雪/fog 雾/haze 霾/sandstorm 沙尘/storm 风暴)、" +
				"speed 时间流速(pause 暂停/slow 慢/normal 常速/fast 快/xfast 极快)、" +
				"layer+visible 图层显隐(buildings 建筑/trees 树/vehicles 车/pedestrians 行人/" +
				"props 道具/labels 标牌/water 水体)、entity_id+kind 实体聚焦(建筑/居民/车辆)、" +
				"preset 机位预设(bird 鸟瞰/eye 平视/street 街道/top 正俯视/free 自由)、" +
				"points 折线飞行路径(2-8 个 {x,z} 世界坐标点)。可组合多个参数。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"phase":     enumStrSchema(DisplayPhases, "时段(dawn|day|dusk|night)"),
					"hour":      numSchema("小时(0-24;省略则由前端按时段自行取代表时刻)"),
					"weather":   enumStrSchema(DisplayWeathers, "天气(clear|rain|snow|fog|haze|sandstorm|storm)"),
					"speed":     enumStrSchema(DisplaySpeeds, "时间流速(pause|slow|normal|fast|xfast)"),
					"layer":     enumStrSchema(DisplayLayers, "图层(buildings|trees|vehicles|pedestrians|props|labels|water)"),
					"visible":   boolSchema("是否显示该图层(true 显示 / false 隐藏)"),
					"entity_id": strSchema("实体 id(必须来自上下文中出现过的真实 id,严禁臆造)"),
					"kind":      enumStrSchema(DisplayEntityKinds, "实体类别(building|resident|vehicle)"),
					"category":  strSchema("按类别筛选高亮的类目(可空)"),
					"preset":    enumStrSchema(DisplayCameraPresets, "机位预设(bird|eye|street|top|free)"),
					"points":    pointsSchema("折线飞行路径点(2-8 个 {x,z} 世界坐标)"),
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
			Name: ToolAskAgent,
			Description: "**向某个 Agent 提问并等待回复**(双向)。target_agent 必须是 list_agents " +
				"返回的 AgentClassName;message 是问题正文。等待上限 30 秒,超时回「对方未在时限内回应」;" +
				"回包超长会截断。适合问居民视角、问采集结果。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target_agent": strSchema("目标 Agent 类名(见 list_agents)"),
					"message":      strSchema("向对方提出的问题(正文)"),
				},
				"required": []string{"target_agent", "message"},
			},
		},
		{
			Name: ToolSendAgentMessage,
			Description: "向指定 Agent 发送一条消息(单向通知,不等待回复)。" +
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
			Name: ToolListAgents,
			Description: "列出当前已注册的 Agent 目录:类名 / 作用域(类级|房间级)/ " +
				"支持的消息类型 / 一句话能力描述。用于选择 ask_agent / send_agent_message 的目标。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// ToolNames 返回全部工具名(测试钉死;合计 **17**)。
//
// ⚠ 与 BuildTools() 顺序逐位对齐(单测断言)。
func ToolNames() []string {
	return []string{
		ToolInjectScenario, ToolSearchScenarios, ToolListScenarios, ToolQueryCityState,
		ToolQueryMetricHistory, ToolQueryDistricts, ToolQueryWorldEvents, ToolQueryPrediction,
		ToolQuerySurveys, ToolControlSimulation, ToolLaunchSurvey, ToolFocusCamera,
		ToolApplyDisplay, ToolCollectRealworldData, ToolAskAgent, ToolSendAgentMessage,
		ToolListAgents,
	}
}
