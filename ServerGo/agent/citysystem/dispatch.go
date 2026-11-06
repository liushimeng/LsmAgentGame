// Package citysystem — dispatch.go: 工具派发(2026-11-03 §批次81)。
//
// 契约: 批次81 方案文档 §3.2 工具表。每个工具只调用**既有房间 API**
// (ApplyScenario / Pause / LaunchSurvey / PredictionReport / ScenarioCatalog)
// 或经 citybus 转发(WebGL-Show / Chrome / 其它 Agent),不新增平行账本、
// 不改经济语义(方案 §2 关键决策 2)。
//
// 错误处理:一律 isErr=true + "失败:" + err.Error();*errcode.Error 的
// 350xx 码随 Error() 保留在文本里回灌给模型(如 [35055] 未知场景)。
// 总线目标未注册 → [35061](ErrVirtualCityAgentNotFound)同样透传。
//
// 身份:userID 随调用链显式传递(操作者 = 触发本轮对话的用户,
// control_simulation 的房主校验用;不共享 Agent 字段,避免并发轮次串号)。
package citysystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
)

// Dispatch 派发单个工具调用。input 为 Anthropic tool_use.input(map);
// userID 是本轮对话的操作者(control_simulation 权限校验)。
// 返回 (人读结果, 是否错误)。
func (a *Agent) Dispatch(ctx context.Context, userID, name string, input map[string]any) (string, bool) {
	getStr := func(k string) string { s, _ := input[k].(string); return s }
	getInt := func(k string) int {
		switch v := input[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		default:
			return 0
		}
	}
	switch name {
	case ToolInjectScenario:
		return a.injectScenario(getStr("kind"), getInt("magnitude"),
			getInt("duration_months"), getStr("target"), getStr("note"))
	case ToolSearchScenarios:
		return searchScenarios(getStr("query"), getStr("group"), getInt("limit"))
	case ToolListScenarios:
		// 批次84 §4.2:缺省返回**维度索引**(恒定体积);传合法组名返回该组
		// 条目(limit 缺省 60 上限 200);未知组名返回失败文本(35064)。
		return listScenarios(getStr("group"), getInt("limit"))
	case ToolQueryCityState:
		// 批次85 E2E F1:scope 归一(7 个英文维度键别名 → 13 个维度名,
		// 维度名与 ScenarioGroup* 同值,模型可直接传「自然灾害」等组名)。
		scope := canonicalCityStateScope(strings.ToLower(strings.TrimSpace(getStr("scope"))))
		if extendedCityStateScopes[scope] {
			return a.queryCityStateExtended(scope)
		}
		text, isErr := a.systemSummary(scope)
		if isErr {
			return text, true
		}
		if scope == "all" {
			// "all" = 既有房间摘要 + 批次84 新增 7 个子系统(全维度口径)。
			if ext := a.allExtendedScopes(); ext != "" {
				text += "\n\n" + ext
			}
		}
		// 批次85 §3.1.2:全部 scope 末尾追加观察闭环两段
		// (all 与 5 个无专属维度 scope 走全局聚合口径)。
		if sec := a.dimensionObservation(scope); sec != "" {
			text += "\n" + sec
		}
		return text, false
	case ToolQueryMetricHistory:
		return a.queryMetricHistory(getStr("metric"), getInt("months"))
	case ToolQueryDistricts:
		return a.queryDistricts(getStr("district"))
	case ToolQueryWorldEvents:
		// 批次85 §3.1.3:dimension 可选过滤(空 = 全部)。
		return a.worldEventText(getStr("dimension"))
	case ToolQueryPrediction:
		return a.queryPrediction(getStr("run_id"))
	case ToolQuerySurveys:
		return a.querySurveys()
	case ToolControlSimulation:
		return a.controlSimulation(getStr("action"), userID)
	case ToolLaunchSurvey:
		return a.launchSurvey(getStr("question"), stringSlice(input["options"]))
	case ToolFocusCamera:
		return a.focusCamera(ctx, getStr("district"), getStr("mode"), input)
	case ToolApplyDisplay:
		return a.applyDisplay(ctx, input)
	case ToolCollectRealworldData:
		return a.collectRealworldData(ctx, getStr("category"))
	case ToolAskAgent:
		return a.askAgent(ctx, getStr("target_agent"), getStr("message"))
	case ToolSendAgentMessage:
		return a.sendAgentMessage(ctx, getStr("target_agent"), getStr("message"))
	case ToolListAgents:
		return a.listAgentsExtended()
	default:
		return "失败:未知工具 " + name, true
	}
}

// busFailure 把总线错误转成回灌文本:目标未注册 → [35061] 透传
// (ErrVirtualCityAgentNotFound;citybus 的错误文案见 bus.go 契约)。
func busFailure(err error) string {
	if err == nil {
		return "失败:未知错误"
	}
	if strings.Contains(err.Error(), "agent not registered") {
		return "失败:" + errcode.CodeMsg(errcode.ErrVirtualCityAgentNotFound, err.Error()).Error()
	}
	return "失败:" + err.Error()
}

// injectScenario 注入场景并返回含 effects 的摘要文本。
func (a *Agent) injectScenario(kind string, magnitude, durationMonths int, target, note string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "失败:缺少场景 kind(请从场景目录选择)", true
	}
	rec, effects, e := a.room.ApplyScenarioFromAgent(kind, magnitude, durationMonths, target, note)
	if e != nil {
		return "失败:" + e.Error(), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已注入场景 %s(%s)L%d,持续 %d 个月,作用范围 %s。",
		rec.Kind, rec.ID, rec.Magnitude, rec.DurationMonths, scenarioTargetLabel(rec.Target))
	if rec.Note != "" {
		fmt.Fprintf(&b, "备注:%s。", rec.Note)
	}
	if len(effects) > 0 {
		parts := make([]string, 0, len(effects))
		for _, ef := range effects {
			parts = append(parts, ef.Text)
		}
		fmt.Fprintf(&b, "\n即时效果:%s", strings.Join(parts, "; "))
	}
	return b.String(), false
}

// systemSummary 查询城市状态摘要(持锁只读,见 room.SystemSummaryText)。
func (a *Agent) systemSummary(scope string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	return a.room.SystemSummaryText(scope), false
}

// worldEventText 查询真实世界事件流文本(持锁只读快照)。
//
// 批次85 §3.1.3:dimension 可选过滤(空 = 全部;取 13 个分组名),
// 复用维度归集判据(virtual_city.WorldEventDimensionOf —— ScenarioKind
// 优先、Kind 兜底;归不到该维度的事件不计入)。未知维度 ⇒ 35064 失败文本
// 回灌模型(与 list_scenarios 未知分组同口径)。
func (a *Agent) worldEventText(dimension string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	dimension = strings.TrimSpace(dimension)
	if dimension != "" && !scenarioGroupKnown(dimension) {
		return "失败:" + errcode.CodeMsg(errcode.ErrVirtualCityScenarioGroupUnknown,
			fmt.Sprintf("未知维度 %s,可用维度:%s", dimension,
				strings.Join(knownScenarioGroups(), "|"))).Error(), true
	}
	records := a.room.WorldEventLogView()
	if dimension != "" {
		filtered := make([]virtual_city.WorldEventRecord, 0, len(records))
		for _, rec := range records {
			if virtual_city.WorldEventDimensionOf(rec) == dimension {
				filtered = append(filtered, rec)
			}
		}
		if len(filtered) == 0 {
			return fmt.Sprintf("维度 %s 暂无相关世界事件(归不到该维度的事件不计入)。", dimension), false
		}
		records = filtered
	}
	return worldEventText(records), false
}

// scenarioTargetLabel 目标标签(空 = 全域)。
func scenarioTargetLabel(target string) string {
	if strings.TrimSpace(target) == "" {
		return "全域"
	}
	return target
}

// queryPrediction 查询预测报告:文本摘要 + 报告卡片数据(下发给前端渲染)。
func (a *Agent) queryPrediction(runID string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	rep, e := a.room.PredictionReport(runID)
	if e != nil {
		return "失败:" + e.Error(), true
	}
	if rep == nil {
		return "暂无预测运行报告。", false
	}
	// 报告卡片数据随事件流下发(前端渲染完整报告)。
	a.emit(SystemEvent{Kind: EventReport, Data: rep})
	return predictionReportText(rep), false
}

// controlSimulation 暂停 / 恢复 / 状态查询。pause/resume 的房主校验
// 使用本轮操作者 userID(35011 原样回灌)。
func (a *Agent) controlSimulation(action, userID string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	switch action {
	case "pause", "resume":
		if strings.TrimSpace(userID) == "" {
			return "失败:无法确认操作者身份,不能暂停/恢复模拟", true
		}
		if e := a.room.Pause(userID, action == "pause"); e != nil {
			return "失败:" + e.Error(), true
		}
		if action == "pause" {
			return "已暂停模拟(月结时钟冻结,恢复后继续)。", false
		}
		return "已恢复模拟。", false
	case "status":
		return fmt.Sprintf("房间状态:%s", a.room.GetStatus()), false
	default:
		return "失败:action 必须是 pause / resume / status", true
	}
}

// launchSurvey 发起社会调研。
func (a *Agent) launchSurvey(question string, options []string) (string, bool) {
	if a.room == nil {
		return "失败:房间未绑定", true
	}
	q := strings.TrimSpace(question)
	if q == "" {
		return "失败:调研问题不能为空", true
	}
	if len(options) < 2 || len(options) > 6 {
		return fmt.Sprintf("失败:调研选项须 2-6 个(收到 %d 个)", len(options)), true
	}
	sv, e := a.room.LaunchSurvey(q, options)
	if e != nil {
		return "失败:" + e.Error(), true
	}
	return fmt.Sprintf("已发起调研 %s:%s(选项 %d 个,截止月 M%d)。",
		sv.ID, sv.Question, len(sv.Options), sv.DeadlineMonth), false
}

// focusCamera 经总线调用 City-WebGL-Show,把返回的显示指令逐条下发。
//
// 显示指令的 wire 契约由本包的**中性结构** displayRequest / displayCommand
// 承载(display.go),不 import citywebgl —— 渲染显示 Agent 是可独立演进的
// 责任线,其实现细节不得泄漏进 System Agent。
func (a *Agent) focusCamera(ctx context.Context, district, mode string, input map[string]any) (string, bool) {
	if a.hub == nil {
		return "失败:Agent 总线未绑定", true
	}
	req := displayRequest{District: district, Mode: mode}
	if v, ok := input["x"]; ok && v != nil {
		if f, ok := toFloat64(v); ok {
			req.X = &f
		}
	}
	if v, ok := input["z"]; ok && v != nil {
		if f, ok := toFloat64(v); ok {
			req.Z = &f
		}
	}
	res, err := a.hub.Send(ctx, a.RoomID, string(agentroot.AgentClassCitySystem),
		agentClassCityWebGLShow, "run_display", req)
	if err != nil {
		return busFailure(err), true
	}
	cmds, err := parseDisplayCommands(res)
	if err != nil {
		return "失败:" + err.Error(), true
	}
	if len(cmds) == 0 {
		return "显示 Agent 未返回任何指令(请检查参数或稍后再试)。", false
	}
	a.emitCommands(cmds)
	return "已下发视角指令:" + describeDisplayCommands(cmds), false
}

// emitCommands 逐条下发显示指令(前端 3D 引擎消费)。
func (a *Agent) emitCommands(cmds []displayCommand) {
	for _, cmd := range cmds {
		if cmd.TraceID == "" {
			cmd.TraceID = NewTraceID()
		}
		a.emit(SystemEvent{Kind: EventShowCommand, Data: cmd})
	}
}

// collectRealworldData 经总线触发 City-Chrome 采集。
func (a *Agent) collectRealworldData(ctx context.Context, category string) (string, bool) {
	if a.hub == nil {
		return "失败:Agent 总线未绑定", true
	}
	payload := map[string]any{}
	if category != "" {
		payload["category"] = category
	}
	res, err := a.hub.Send(ctx, a.RoomID, string(agentroot.AgentClassCitySystem),
		string(agentroot.AgentClassCityChrome), "collect", payload)
	if err != nil {
		return busFailure(err), true
	}
	if s, ok := res.(string); ok && s != "" {
		return s, false
	}
	return "已触发真实数据采集(结果由采集 Agent 异步产出,可在实时看板查看)。", false
}

// sendAgentMessage 经总线向目标 Agent **单向通知**(批次84:改用 hub.Post,
// 不等回复 —— 对方 handler 的返回值被丢弃,也不阻塞本轮)。
//
// 目标未注册仍回 [35061](先经 Describe 查注册表;Post 对未注册目标是静默
// 丢弃,不能给出确定反馈)。载荷与批次81 一致(裸字符串)。
func (a *Agent) sendAgentMessage(ctx context.Context, targetAgent, message string) (string, bool) {
	if a.hub == nil {
		return "失败:Agent 总线未绑定", true
	}
	targetAgent = strings.TrimSpace(targetAgent)
	if targetAgent == "" {
		return "失败:缺少目标 Agent(target_agent)", true
	}
	if strings.TrimSpace(message) == "" {
		return "失败:消息内容为空", true
	}
	if !a.agentRegistered(targetAgent) {
		return busFailure(fmt.Errorf("agent not registered: %s", targetAgent)), true
	}
	if err := a.hub.Post(ctx, a.RoomID, string(agentroot.AgentClassCitySystem),
		targetAgent, BusTypeMessage, message); err != nil {
		return "失败:" + err.Error(), true
	}
	return fmt.Sprintf("已向 %s 发送消息(单向通知,不等待回复)。", targetAgent), false
}

// agentRegistered 报告 target 是否在本房可达(类级或本房房间级)。
func (a *Agent) agentRegistered(target string) bool {
	for _, d := range a.hub.Describe(a.RoomID) {
		if d.ClassName == target {
			return true
		}
	}
	return false
}

// stringSlice 把 []any / []string 统一成 []string。
func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// toFloat64 宽松数值转换(JSON number / int)。
func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
