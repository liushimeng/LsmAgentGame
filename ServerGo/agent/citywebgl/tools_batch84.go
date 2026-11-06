// Package citywebgl — tools_batch84.go: 批次84 新增的 8 个显示原语
// (2026-11-05 §批次84,方案文档 §4.5.1 #7–#14)。
//
// 新增原语覆盖既有 6 个缺失的维度:时间(时段/流速)、天气、实体聚焦、
// 图层显隐、类别筛选、机位预设、飞行路径 —— 需求 2「自动渲染显示、
// 控制各种物体 / 物品 / 3D 模型相关的显示行为」。
//
// 派发纪律(与批次81 一致,批次84 追加枚举硬校验):
//   - 缺参 / 非法枚举 / 越界数值 ⇒ isErr=true 回灌模型,**不产指令**;
//   - 成功 ⇒ mk(cmd) 统一补 trace_id + 「已下发显示指令 <type>」回执。
package citywebgl

import (
	"fmt"
	"math"
	"strings"

	llmtypes "LsmAgentGame/llm/types"
)

// flyPathMinPoints / flyPathMaxPoints 是 fly_path 的路径点数量边界(§4.5.1 ≤8)。
const (
	flyPathMinPoints = 2
	flyPathMaxPoints = 8
)

// hourMin / hourMax 是 set_time_of_day 的 hour 取值区间(含端点)。
const (
	hourMin = 0.0
	hourMax = 24.0
)

// buildBatch84Tools 返回批次84 新增的 8 个原语 schema。
func buildBatch84Tools() []llmtypes.ToolDef {
	return []llmtypes.ToolDef{
		{
			Name:        ToolSetTimeOfDay,
			Description: "设置城市时段(产出 set_time_of_day 指令):dawn 黎明 / day 白天 / dusk 黄昏 / night 夜晚;可附 hour(0-24)精确定位小时。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"phase": map[string]any{"type": "string", "enum": DisplayPhases, "description": "时段(必填;只能 dawn|day|dusk|night)"},
					"hour":  map[string]any{"type": "number", "description": "小时(可选,0-24;省略则由前端按时段自行取代表时刻)"},
				},
				"required": []string{"phase"},
			},
		},
		{
			Name:        ToolSetWeather,
			Description: "设置天气(产出 set_weather 指令):clear 晴 / rain 雨 / snow 雪 / fog 雾 / haze 霾 / sandstorm 沙尘 / storm 风暴。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"weather": map[string]any{"type": "string", "enum": DisplayWeathers, "description": "天气(必填;只能 clear|rain|snow|fog|haze|sandstorm|storm)"},
				},
				"required": []string{"weather"},
			},
		},
		{
			Name:        ToolSetTimeSpeed,
			Description: "设置时间流速(产出 set_time_speed 指令):pause 暂停 / slow 慢 / normal 常速 / fast 快 / xfast 极快。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"speed": map[string]any{"type": "string", "enum": DisplaySpeeds, "description": "时间流速(必填;只能 pause|slow|normal|fast|xfast)"},
				},
				"required": []string{"speed"},
			},
		},
		{
			Name:        ToolFocusEntity,
			Description: "聚焦一个具体实体(产出 focus_entity 指令):建筑 / 居民 / 车辆。entity_id 必须来自上下文给出的真实 id,严禁臆造。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{"type": "string", "description": "实体 id(必填;只能来自请求参数或上下文中出现过的 id,不得编造;形态如 resident:seat:2 / building:<区>:<序> / vehicle:<id>)"},
					"kind":      map[string]any{"type": "string", "enum": DisplayEntityKinds, "description": "实体类别(可选;building|resident|vehicle)"},
				},
				"required": []string{"entity_id"},
			},
		},
		{
			Name:        ToolSetLayerVisibility,
			Description: "设置某个图层的显示与否(产出 set_layer_visibility 指令):建筑 / 树木 / 车辆 / 行人 / 道具 / 标牌 / 水体。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"layer":   map[string]any{"type": "string", "enum": DisplayLayers, "description": "图层(必填;只能 buildings|trees|vehicles|pedestrians|props|labels|water)"},
					"visible": map[string]any{"type": "boolean", "description": "是否显示(必填,true 显示 / false 隐藏)"},
				},
				"required": []string{"layer", "visible"},
			},
		},
		{
			Name:        ToolSetFilter,
			Description: "按类别筛选并高亮(产出 set_filter 指令);category 留空 = 取消筛选。mode 决定呈现:highlight 高亮 / dim 变暗。category 支持图层名 / 物件类别 / 车辆类型。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": map[string]any{"type": "string", "description": "类别(可选;留空 = 取消筛选;支持图层名 / 物件类别(如 park、fire-station 等物件类别) / 车辆类型(sedan|truck|bus|taxi))"},
					"mode":     map[string]any{"type": "string", "enum": DisplayFilterModes, "description": "呈现方式(可选,默认 highlight;只能 highlight|dim)"},
				},
			},
		},
		{
			Name:        ToolCameraPreset,
			Description: "切换机位预设(产出 camera_preset 指令):bird 鸟瞰 / eye 平视 / street 街道 / top 正俯视 / free 自由。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"preset": map[string]any{"type": "string", "enum": DisplayCameraPresets, "description": "机位预设(必填;只能 bird|eye|street|top|free)"},
				},
				"required": []string{"preset"},
			},
		},
		{
			Name:        ToolFlyPath,
			Description: "沿折线路径飞行(产出 fly_path 指令):给 2-8 个世界坐标点,相机依次掠过。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"points": map[string]any{
						"type":        "array",
						"minItems":    flyPathMinPoints,
						"maxItems":    flyPathMaxPoints,
						"description": "路径点数组(必填,2-8 个;每点为 {x, z} 世界坐标)",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"x": map[string]any{"type": "number", "description": "x 坐标"},
								"z": map[string]any{"type": "number", "description": "z 坐标"},
							},
							"required": []string{"x", "z"},
						},
					},
				},
				"required": []string{"points"},
			},
		},
	}
}

// dispatchToolBatch84 派发批次84 新增的 8 个原语。
// 未知工具名 ⇒ isErr(不产指令)。
func dispatchToolBatch84(name string, input map[string]any, mk cmdMaker) (*DisplayCommand, string, bool) {
	switch name {
	case ToolSetTimeOfDay:
		phase := NormalizeEnum(strArg(input, "phase"), DisplayPhases)
		if phase == "" {
			return nil, "失败:phase 只能 dawn|day|dusk|night", true
		}
		cmd := DisplayCommand{Type: DisplaySetTimeOfDay, Phase: phase}
		if argPresent(input, "hour") {
			hour, numOK := numArg(input, "hour")
			if !numOK || !isFinite(hour) || hour < hourMin || hour > hourMax {
				return nil, "失败:hour 必须是 0-24 之间的有限数", true
			}
			h := hour
			cmd.Hour = &h
		}
		return mk(cmd)
	case ToolSetWeather:
		weather := NormalizeEnum(strArg(input, "weather"), DisplayWeathers)
		if weather == "" {
			return nil, "失败:weather 只能 clear|rain|snow|fog|haze|sandstorm|storm", true
		}
		return mk(DisplayCommand{Type: DisplaySetWeather, Weather: weather})
	case ToolSetTimeSpeed:
		speed := NormalizeEnum(strArg(input, "speed"), DisplaySpeeds)
		if speed == "" {
			return nil, "失败:speed 只能 pause|slow|normal|fast|xfast", true
		}
		return mk(DisplayCommand{Type: DisplaySetTimeSpeed, Speed: speed})
	case ToolFocusEntity:
		entityID := strings.TrimSpace(strArg(input, "entity_id"))
		if entityID == "" {
			return nil, "失败:缺少 entity_id(实体 id)", true
		}
		cmd := DisplayCommand{Type: DisplayFocusEntity, EntityID: entityID}
		if argPresent(input, "kind") {
			kind := NormalizeEnum(strArg(input, "kind"), DisplayEntityKinds)
			if kind == "" {
				return nil, "失败:kind 只能 building|resident|vehicle", true
			}
			cmd.Kind = kind
		}
		return mk(cmd)
	case ToolSetLayerVisibility:
		layer := NormalizeEnum(strArg(input, "layer"), DisplayLayers)
		if layer == "" {
			return nil, "失败:layer 只能 buildings|trees|vehicles|pedestrians|props|labels|water", true
		}
		visible, ok := boolArg(input, "visible")
		if !ok {
			return nil, "失败:缺少 visible(布尔值,true 显示 / false 隐藏)", true
		}
		v := visible
		return mk(DisplayCommand{Type: DisplaySetLayerVisibility, Layer: layer, Visible: &v})
	case ToolSetFilter:
		mode := DisplayFilterModes[0] // 默认 highlight
		if argPresent(input, "mode") {
			m := NormalizeEnum(strArg(input, "mode"), DisplayFilterModes)
			if m == "" {
				return nil, "失败:mode 只能 highlight|dim", true
			}
			mode = m
		}
		// category 允许为空(空 = 取消筛选),仍产出一条 set_filter 指令。
		return mk(DisplayCommand{Type: DisplaySetFilter,
			Category: strings.TrimSpace(strArg(input, "category")), Mode: mode})
	case ToolCameraPreset:
		preset := NormalizeEnum(strArg(input, "preset"), DisplayCameraPresets)
		if preset == "" {
			return nil, "失败:preset 只能 bird|eye|street|top|free", true
		}
		return mk(DisplayCommand{Type: DisplayCameraPreset, Preset: preset})
	case ToolFlyPath:
		points, ok := parsePoints(input["points"])
		if !ok {
			return nil, fmt.Sprintf("失败:points 必须是 %d-%d 个 {x,z} 路径点(坐标须为有限数)",
				flyPathMinPoints, flyPathMaxPoints), true
		}
		return mk(DisplayCommand{Type: DisplayFlyPath, Points: points})
	default:
		return nil, fmt.Sprintf("失败:未知工具 %s", name), true
	}
}

// argPresent 报告参数是否「有值」(键存在、非 nil、且不是空字符串)。
//
// 用于「可选参数给了就校验」的场景:空串视作**未给**(走缺省值),
// 而不是「给了非法值」—— 避免模型多传一个空参数就判失败。
func argPresent(input map[string]any, key string) bool {
	raw, ok := input[key]
	if !ok || raw == nil {
		return false
	}
	if s, isStr := raw.(string); isStr && strings.TrimSpace(s) == "" {
		return false
	}
	return true
}

// parsePoints 解析 fly_path 的 points 参数。
// 兼容 []any / []map[string]any / []DisplayPoint 三种形态(JSON 往返后
// 通常是 []any[map[string]any])。点数须落在 [2,8],每点 x/z 须为有限数。
func parsePoints(raw any) ([]DisplayPoint, bool) {
	var list []DisplayPoint
	switch v := raw.(type) {
	case nil:
		return nil, false
	case []DisplayPoint:
		list = append(list, v...)
	case []map[string]any:
		for _, m := range v {
			x, xOK := numArg(m, "x")
			z, zOK := numArg(m, "z")
			if !xOK || !zOK || !isFinite(x) || !isFinite(z) {
				return nil, false
			}
			list = append(list, DisplayPoint{X: x, Z: z})
		}
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			x, xOK := numArg(m, "x")
			z, zOK := numArg(m, "z")
			if !xOK || !zOK || !isFinite(x) || !isFinite(z) {
				return nil, false
			}
			list = append(list, DisplayPoint{X: x, Z: z})
		}
	default:
		return nil, false
	}
	if len(list) < flyPathMinPoints || len(list) > flyPathMaxPoints {
		return nil, false
	}
	return list, true
}

// isFinite 判定浮点数是否为有限值(排除 NaN / ±Inf)。
func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
