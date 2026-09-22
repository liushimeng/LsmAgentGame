// Package wealthplayer — tools_sense.go: 感知与行动工具(2026-09-22 §CityHuman重构)。
//
// 契约: lag_docs/虚拟城市/已实现/12-CityHuman重构/虚拟城市-CityHuman-Agent合并与感知系统设计-v1.md §4。
// 5 个工具:see(视觉≈500m)/hear(听觉≈100m)/smell(嗅觉≈50m) 不耗动作预算、
// 每月各限 2 次(Agent 侧计数);move(walk|run 区内 / bus|metro|taxi 跨城区)
// 耗 1 次动作预算;speak scope=private 的派发在 tools.go(与 area 合一)。
package wealthplayer

import (
	"encoding/json"
	"fmt"

	"LsmAgentGame/agent/wealthtypes"
	"LsmAgentGame/errcode"
	llmtypes "LsmAgentGame/llm/types"
)

// senseToolNames 感知与行动工具名(供 DispatchTool default 分支路由)。
var senseToolNames = []string{ToolSee, ToolHear, ToolSmell, ToolMove}

// senseMonthlyLimit 感知工具每月限次(设计 §4.1:see/hear/smell 各 ≤2)。
const senseMonthlyLimit = 2

// SenseToolDefinitions 感知与行动工具的 Anthropic wire 定义。
func SenseToolDefinitions() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name: ToolSee,
			Description: "看见(视觉≈500米,限同城区,不耗动作预算,每月最多 2 次):" +
				"看见周围的人(邻居姓名/职业/状态)、物(地标建筑/本区挂牌出售)、事(本区本月正在发生的事件)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolHear,
			Description: "听见(听觉≈100米,限同城区近处,不耗动作预算,每月最多 2 次):" +
				"听见附近居民的公开发言、城市之声、环境声与本月事件动静。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolSmell,
			Description: "闻(嗅觉≈50米,不耗动作预算,每月最多 2 次):" +
				"闻到所在城区的气味画像——城区基底气味叠加当月事件气味(如失业潮的焦虑汗味、婚礼的喜糖甜香)。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: ToolMove,
			Description: "移动(耗 1 次动作预算):destination 为空或当前城区时为区内移动(walk 步行精力−1 / run 跑步精力−2);" +
				"destination 为其他城区 id 时乘坐交通工具跨城区:bus 公交¥500精力−2 / metro 地铁¥1500精力−1 / taxi 出租车¥3000精力−1(价格随 CPI 浮动)," +
				"跨城区语义等同原 move_district(迁区后若目标区有自有住宅自动自住)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"destination": strSchema("目标城区 id(区内移动可省略)"),
					"mode":        strSchema("walk|run(区内) / bus|metro|taxi(跨城区)"),
				},
				"required": []string{"mode"},
			},
		},
	}
}

// dispatchSenseTool 派发感知与行动工具(DispatchTool default 分支调用)。
// getStr/getInt 复用 tools.go 的入参解包闭包。
func (a *Agent) dispatchSenseTool(name string, inputJSON json.RawMessage, getStr func(string) string, getInt func(string) int64) dispatchToolResult {
	res := dispatchToolResult{Name: name, Input: string(inputJSON)}
	fail := func(err error) dispatchToolResult {
		res.IsErr = true
		res.Text = "失败:" + err.Error()
		return res
	}

	// 感知三件套:每月各限 2 次(Agent 侧计数,随 OnMonthStart 重置)。
	if name == ToolSee || name == ToolHear || name == ToolSmell {
		a.mu.Lock()
		used := a.senseUsed[name]
		if used >= senseMonthlyLimit {
			a.mu.Unlock()
			e := errcode.Code(errcode.ErrWealthSenseLimit)
			res.IsErr = true
			res.Text = fmt.Sprintf("失败:[%d] %s(本月 %s 已用 %d 次)", e.Code, e.Message, name, used)
			return res
		}
		a.senseUsed[name] = used + 1
		a.mu.Unlock()

		var (
			sr  *wealthtypes.SenseResult
			err error
		)
		switch name {
		case ToolSee:
			sr, err = a.runner.See(a.MySeat)
		case ToolHear:
			sr, err = a.runner.Hear(a.MySeat)
		default:
			sr, err = a.runner.Smell(a.MySeat)
		}
		if err != nil {
			return fail(err)
		}
		if sr == nil {
			res.IsErr = true
			res.Text = "失败:感知结果为空"
			return res
		}
		b, _ := json.Marshal(sr)
		res.Text = string(b)
		return res
	}

	// move:耗 1 次动作预算(引擎侧校验)。
	if name == ToolMove {
		return failOr(a.runner.Move(a.MySeat, getStr("destination"), getStr("mode")), "移动完成", res)
	}

	res.IsErr = true
	res.Text = "未知感知工具: " + name
	return res
}
