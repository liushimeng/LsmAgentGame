// Package citywebgl — tools.go: 显示原语的 Anthropic wire 定义与派发
// (2026-11-03 §批次81 建立 6 个;2026-11-05 §批次84 扩到 14 个)。
//
// 每个工具调用即产出一条 DisplayCommand(emit 实时下发 + 回灌 tool_result);
// 派发是纯函数(不触引擎),失败以 isErr 回灌让模型自行纠正。
// 工具名常量集中在本文件,ToolNames() 供单测钉死(防「声明了却从不接线」§130)。
//
// 批次84 的 8 个新原语(schema + 派发)放在 tools_batch84.go,避免单文件膨胀。
package citywebgl

import (
	"fmt"
	"strings"

	llmtypes "LsmAgentGame/llm/types"
)

// 显示原语工具名(14 个)。注意 clear_display 的工具名与产出指令类型
// clear(DisplayClear)不同 —— 前端契约的指令类型以 Display* 常量为准。
const (
	// ── 批次81 的 6 个(取值不变,零回归)──
	ToolFocusDistrict     = "focus_district"
	ToolFocusPoint        = "focus_point"
	ToolSetMode           = "set_mode"
	ToolHighlightDistrict = "highlight_district"
	ToolAnnotate          = "annotate"
	ToolClearDisplay      = "clear_display"
	// ── 批次84 新增的 8 个(与产出指令 type 同名)──
	ToolSetTimeOfDay       = "set_time_of_day"
	ToolSetWeather         = "set_weather"
	ToolSetTimeSpeed       = "set_time_speed"
	ToolFocusEntity        = "focus_entity"
	ToolSetLayerVisibility = "set_layer_visibility"
	ToolSetFilter          = "set_filter"
	ToolCameraPreset       = "camera_preset"
	ToolFlyPath            = "fly_path"
)

// annotateTextLimit 是 annotate 文本上限(prompt 纪律同口径;超限截断)。
const annotateTextLimit = 40

// cmdMaker 是产出成功指令的闭包(统一补 trace_id + 人读回执)。
type cmdMaker func(DisplayCommand) (*DisplayCommand, string, bool)

// BuildTools 返回全部 14 个显示原语的 Anthropic tools[] 定义。
func BuildTools() []llmtypes.ToolDef {
	tools := make([]llmtypes.ToolDef, 0, 14)
	tools = append(tools, buildBaseTools()...)
	tools = append(tools, buildBatch84Tools()...)
	return tools
}

// buildBaseTools 返回批次81 的 6 个原语 schema(批次84 零改动)。
func buildBaseTools() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name:        ToolFocusDistrict,
			Description: "把 3D 视角聚焦到指定城区(产出 focus_district 指令)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"district": map[string]any{"type": "string", "description": "目标城区 id(必填;地名只能来自请求方给出的 district,不得编造)"},
				},
				"required": []string{"district"},
			},
		},
		{
			Name:        ToolFocusPoint,
			Description: "把 3D 视角聚焦到世界坐标 (x, z) 一点(产出 focus_point 指令;x/z 必须同给)。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{"type": "number", "description": "目标点 x 坐标(必填,与 z 同给)"},
					"z": map[string]any{"type": "number", "description": "目标点 z 坐标(必填,与 x 同给)"},
				},
				"required": []string{"x", "z"},
			},
		},
		{
			Name:        ToolSetMode,
			Description: "设置相机运动模式(产出 set_mode 指令):orbit 环绕 / fly 飞行。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{"type": "string", "enum": DisplayModes, "description": "相机模式(只能 orbit|fly)"},
				},
				"required": []string{"mode"},
			},
		},
		{
			Name:        ToolHighlightDistrict,
			Description: "高亮指定城区(产出 highlight_district 指令);color 可选,缺省由前端用主题色。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"district": map[string]any{"type": "string", "description": "目标城区 id(必填)"},
					"color":    map[string]any{"type": "string", "description": "高亮色(CSS 颜色字面量,可空)"},
				},
				"required": []string{"district"},
			},
		},
		{
			Name:        ToolAnnotate,
			Description: "在指定城区(或全局)上叠一条简短标注(产出 annotate 指令);text ≤40 字。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text":     map[string]any{"type": "string", "description": "标注文本(必填,≤40 字)"},
					"district": map[string]any{"type": "string", "description": "挂载城区 id(可空 = 全局标注)"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        ToolClearDisplay,
			Description: "清除全部显示效果(高亮 / 标注,产出 clear 指令)。不移动视角。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// ToolNames 返回全部工具名(测试/lint 用;合计 14)。
func ToolNames() []string {
	return []string{
		ToolFocusDistrict, ToolFocusPoint, ToolSetMode,
		ToolHighlightDistrict, ToolAnnotate, ToolClearDisplay,
		ToolSetTimeOfDay, ToolSetWeather, ToolSetTimeSpeed,
		ToolFocusEntity, ToolSetLayerVisibility, ToolSetFilter,
		ToolCameraPreset, ToolFlyPath,
	}
}

// dispatchTool 把一次 tool_use 派发为 DisplayCommand + 人读 tool_result。
// 返回 (cmd, resultText, isErr):isErr 时 cmd 恒 nil(不 emit 不收集);
// trace 为空时指令不带 trace_id(由下游统一补)。
func dispatchTool(name string, input map[string]any, trace string) (*DisplayCommand, string, bool) {
	if input == nil {
		input = map[string]any{}
	}
	mk := func(c DisplayCommand) (*DisplayCommand, string, bool) {
		c.TraceID = trace
		return &c, fmt.Sprintf("已下发显示指令 %s", c.Type), false
	}
	switch name {
	case ToolFocusDistrict:
		district := strings.TrimSpace(strArg(input, "district"))
		if district == "" {
			return nil, "失败:缺少 district(城区 id)", true
		}
		return mk(DisplayCommand{Type: DisplayFocusDistrict, District: district})
	case ToolFocusPoint:
		x, xOK := numArg(input, "x")
		z, zOK := numArg(input, "z")
		if !xOK || !zOK {
			return nil, "失败:焦点坐标 x/z 必须同给", true
		}
		return mk(DisplayCommand{Type: DisplayFocusPoint, X: x, Z: z})
	case ToolSetMode:
		mode := NormalizeMode(strArg(input, "mode"))
		if mode == "" {
			return nil, "失败:mode 只能 orbit|fly", true
		}
		return mk(DisplayCommand{Type: DisplaySetMode, Mode: mode})
	case ToolHighlightDistrict:
		district := strings.TrimSpace(strArg(input, "district"))
		if district == "" {
			return nil, "失败:缺少 district(城区 id)", true
		}
		cmd := DisplayCommand{Type: DisplayHighlightDistrict, District: district}
		if color := strings.TrimSpace(strArg(input, "color")); color != "" {
			cmd.Color = color
		}
		return mk(cmd)
	case ToolAnnotate:
		text := strings.TrimSpace(strArg(input, "text"))
		if text == "" {
			return nil, "失败:缺少 text(标注文本)", true
		}
		text = truncateRunes(text, annotateTextLimit)
		cmd := DisplayCommand{Type: DisplayAnnotate, Text: text}
		if district := strings.TrimSpace(strArg(input, "district")); district != "" {
			cmd.District = district
		}
		return mk(cmd)
	case ToolClearDisplay:
		return mk(DisplayCommand{Type: DisplayClear})
	default:
		// 批次84 新增的 8 个原语(tools_batch84.go)。
		return dispatchToolBatch84(name, input, mk)
	}
}

// strArg 取字符串参数(缺失/非字符串 → "")。
func strArg(input map[string]any, key string) string {
	s, _ := input[key].(string)
	return s
}

// numArg 取数值参数(接受 float64 / int / json.Number;缺失或非数值 → false)。
func numArg(input map[string]any, key string) (float64, bool) {
	switch v := input[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case jsonNumber:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// boolArg 取布尔参数(仅接受真正的 bool;缺失/非布尔 → false)。
//
// ⚠ 刻意**不**接受 "true"/"1" 等字符串形态:set_layer_visibility 的
// visible 缺失即失败(§批次84 派发纪律),字符串形态视为非法输入,
// 由 isErr 回灌让模型自行纠正(比静默猜值安全)。
func boolArg(input map[string]any, key string) (bool, bool) {
	b, ok := input[key].(bool)
	return b, ok
}

// jsonNumber 是 encoding/json.Number 的窄接口镜像(避免本包引 json 依赖图
// 之外的类型断言失败;实际值即 json.Number)。
type jsonNumber interface{ Float64() (float64, error) }

// truncateRunes 按 rune 截断(标注文本上限)。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}
