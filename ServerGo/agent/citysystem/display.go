// Package citysystem — display.go: 3D 显示指令的**中性 wire 契约**
// (2026-11-03 §批次81;2026-11-05 §批次84 扩展 6 → 14 原语)。
//
// 背景:批次81 方案文档 §3.3 把「看哪里 / 怎么显示」的翻译职责交给
// City-WebGL-Show Agent(独立 SubAgent 实现,不在本包内)。为避免
// citysystem → citywebgl 的硬依赖(两个可独立演进的包),这里定义
// **中性** 的请求 / 指令结构:经 citybus 传输时统一 JSON 往返,
// 字段名与方案文档 §3.3 / §6.1 / §4.5.2 的 wire 契约逐字一致。
//
// ⚠ **镜像契约**:`citysystem/display.go` 与 `citywebgl/command.go` 各持一份
// **逐字一致**的 displayRequest / displayCommand 结构(两包互不 import,
// 经 citybus JSON 往返)。字段名 / JSON tag / **字段顺序** 任一侧改动都
// 必须双侧同步 —— 两侧各有 JSON 锚点单测钉死。
//
// 字段口径:
//
//	{"type":"focus_district|focus_point|set_mode|highlight_district|annotate|clear
//	          |set_time_of_day|set_weather|set_time_speed|focus_entity
//	          |set_layer_visibility|set_filter|camera_preset|fly_path",
//	 "district":"d1","x":0.0,"z":0.0,"mode":"orbit|fly",
//	 "text":"...","color":"...","trace_id":"tr-...",
//	 "phase":"dawn|day|dusk|night","hour":12,
//	 "weather":"clear|rain|snow|fog|haze|sandstorm|storm",
//	 "speed":"pause|slow|normal|fast|xfast",
//	 "entity_id":"...","kind":"building|resident|vehicle",
//	 "layer":"buildings|trees|vehicles|pedestrians|props|labels|water",
//	 "visible":true,"category":"...","preset":"bird|eye|street|top|free",
//	 "points":[{"x":0,"z":0}]}
//
// ⚠ **只加字段、不加嵌套**:一条指令 = 一个 type + 一组扁平字段。旧的
// 前端 adapter 遇到未知 type 会静默忽略(向后兼容)。
package citysystem

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// agentClassCityWebGLShow 是渲染显示 Agent 的 AgentClassName(跨包协作的
// 字面量镜像)。该常量由 citywebgl 包的责任线在 class_names.go 正式登记
// (AgentClassCityWebGLShow);此处仅作为 citybus 寻址的 to 值,避免
// citysystem → citywebgl 的编译期依赖。
const agentClassCityWebGLShow = "LsmAgentGame-City-WebGL-Show"

// displayPoint 是 fly_path 的路径点(世界坐标 x/z)。
type displayPoint struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
}

// displayRequest 是发给渲染显示 Agent 的结构化请求(中性 wire 形状)。
type displayRequest struct {
	Text     string   `json:"text,omitempty"`
	District string   `json:"district,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Z        *float64 `json:"z,omitempty"`
	// ── 批次84 新增(与 citywebgl 镜像逐字一致)──
	Phase    string         `json:"phase,omitempty"`
	Hour     *float64       `json:"hour,omitempty"`
	Weather  string         `json:"weather,omitempty"`
	Speed    string         `json:"speed,omitempty"`
	EntityID string         `json:"entity_id,omitempty"`
	Kind     string         `json:"kind,omitempty"`
	Layer    string         `json:"layer,omitempty"`
	Visible  *bool          `json:"visible,omitempty"`
	Category string         `json:"category,omitempty"`
	Preset   string         `json:"preset,omitempty"`
	Points   []displayPoint `json:"points,omitempty"`
}

// displayCommand 是一条结构化显示指令(中性 wire 形状,前端 3D 引擎消费)。
type displayCommand struct {
	Type     string  `json:"type"`
	District string  `json:"district,omitempty"`
	X        float64 `json:"x,omitempty"`
	Z        float64 `json:"z,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	Text     string  `json:"text,omitempty"`
	Color    string  `json:"color,omitempty"`
	TraceID  string  `json:"trace_id,omitempty"`
	// ── 批次84 新增(与 citywebgl 镜像逐字一致)──
	Phase    string         `json:"phase,omitempty"`
	Hour     *float64       `json:"hour,omitempty"`
	Weather  string         `json:"weather,omitempty"`
	Speed    string         `json:"speed,omitempty"`
	EntityID string         `json:"entity_id,omitempty"`
	Kind     string         `json:"kind,omitempty"`
	Layer    string         `json:"layer,omitempty"`
	Visible  *bool          `json:"visible,omitempty"`
	Category string         `json:"category,omitempty"`
	Preset   string         `json:"preset,omitempty"`
	Points   []displayPoint `json:"points,omitempty"`
}

// 显示指令类型常量(与方案文档 §3.3 / §4.5.1 / 前端 cityShowAdapter 契约一致)。
// 批次81 的 6 个常量**取值不变**(零回归),批次84 追加 8 个。
const (
	DisplayFocusDistrict     = "focus_district"
	DisplayFocusPoint        = "focus_point"
	DisplaySetMode           = "set_mode"
	DisplayHighlightDistrict = "highlight_district"
	DisplayAnnotate          = "annotate"
	DisplayClear             = "clear"
	// ── 批次84 新增 8 个 ──
	DisplaySetTimeOfDay       = "set_time_of_day"
	DisplaySetWeather         = "set_weather"
	DisplaySetTimeSpeed       = "set_time_speed"
	DisplayFocusEntity        = "focus_entity"
	DisplaySetLayerVisibility = "set_layer_visibility"
	DisplaySetFilter          = "set_filter"
	DisplayCameraPreset       = "camera_preset"
	DisplayFlyPath            = "fly_path"
)

// 批次84 新增的枚举取值域(与 citywebgl/command.go 逐字一致)。
var (
	// DisplayPhases 是 set_time_of_day 的合法时段取值。
	DisplayPhases = []string{"dawn", "day", "dusk", "night"}
	// DisplayWeathers 是 set_weather 的合法天气取值。
	DisplayWeathers = []string{"clear", "rain", "snow", "fog", "haze", "sandstorm", "storm"}
	// DisplaySpeeds 是 set_time_speed 的合法时间流速取值。
	DisplaySpeeds = []string{"pause", "slow", "normal", "fast", "xfast"}
	// DisplayEntityKinds 是 focus_entity 的合法实体类别取值。
	DisplayEntityKinds = []string{"building", "resident", "vehicle"}
	// DisplayLayers 是 set_layer_visibility 的合法图层取值。
	DisplayLayers = []string{"buildings", "trees", "vehicles", "pedestrians", "props", "labels", "water"}
	// DisplayFilterModes 是 set_filter 的合法呈现方式取值(写 Mode 字段)。
	DisplayFilterModes = []string{"highlight", "dim"}
	// DisplayCameraPresets 是 camera_preset 的合法机位预设取值。
	DisplayCameraPresets = []string{"bird", "eye", "street", "top", "free"}
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

// normalizeEnum 归一化枚举取值:小写 + 去首尾空格后必须命中 allowed,
// 否则返回 ""(调用方丢弃 / 报错,不产指令)。
//
// allowed 为 nil 时返回 ""(空取值域 = 一切都非法)。
func normalizeEnum(value string, allowed []string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return ""
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return ""
}

// isFinite 判定浮点数是否为有限值(排除 NaN / ±Inf;与 citywebgl 同名实现同义)。
func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// parseDisplayCommands 把总线返回值反序列化为中性显示指令。
//
// 兼容三种返回形态(房间级 handler 的实现细节不泄漏到本包):
//   - []displayCommand(本包类型,直接返回);
//   - []map[string]any / []any(JSON 解码后的通用形态);
//   - 单个 displayCommand / map[string]any(包成一条)。
//
// 统一走 JSON 往返,确保字段名口径唯一(避免类型断言失败即报「类型异常」)。
func parseDisplayCommands(res any) ([]displayCommand, error) {
	switch v := res.(type) {
	case nil:
		return nil, nil
	case []displayCommand:
		return v, nil
	case displayCommand:
		return []displayCommand{v}, nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("marshal display result: %w", err)
	}
	// 先按数组解;失败再按单对象解。
	var list []displayCommand
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var one displayCommand
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("unmarshal display result: %w", err)
	}
	return []displayCommand{one}, nil
}

// describeDisplayCommands 工具结果里的指令摘要。
func describeDisplayCommands(cmds []displayCommand) string {
	parts := make([]string, 0, len(cmds))
	for _, c := range cmds {
		parts = append(parts, c.Type)
	}
	return strings.Join(parts, ", ")
}
