// Package citywebgl — command.go: 3D 显示指令的中性 wire 契约
// (2026-11-03 §批次81;2026-11-05 §批次84 扩展 6 → 14 原语)。
//
// 背景:批次81 方案文档 §3.3 把「看哪里 / 怎么显示」的翻译职责交给本包
// (City-WebGL-Show 渲染显示 Agent)。citysystem/display.go 有一份**逐字
// 一致**的镜像结构(displayRequest / displayCommand),两包互不 import、
// 经 citybus JSON 往返通信 —— 字段名任何一侧改动都必须双侧同步
// (command_test.go 用字面 JSON 锚点钉死)。
//
// 字段口径(与 citysystem/display.go / 方案文档 §4.5.2 逐字一致):
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
// ⚠ **字段顺序也必须双侧一致**:Go 的 encoding/json 按 struct 字段声明
// 顺序序列化,两侧顺序不同 ⇒ JSON 字节不同 ⇒ 跨包往返虽能解析但字节比对
// (锚点测试)会失败。批次84 新增字段统一**追加在既有字段之后**。
//
// ⚠ **只加字段、不加嵌套**:一条指令 = 一个 type + 一组扁平字段。旧的
// 前端 adapter 遇到未知 type 会静默忽略(向后兼容)。
//
// ⚠ `mode` 字段是**上下文相关**的单一扁平字段:
//   - 指令 type = set_mode     ⇒ mode ∈ orbit|fly(相机运动风格);
//   - 指令 type = set_filter   ⇒ mode ∈ highlight|dim(筛选呈现方式)。
//     两者由 type 区分,不新增字段、不加嵌套(§4.5.2 硬约束)。
package citywebgl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DisplayPoint 是 fly_path 的路径点(世界坐标 x/z)。
type DisplayPoint struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
}

// DisplayRequest 是显示请求(中性 wire 形状,与 citysystem/display.go
// 的 displayRequest 一致)。citybus Message.Payload 承载,经 JSON 往返解码
// (ParseDisplayRequest)。
type DisplayRequest struct {
	Text     string   `json:"text,omitempty"`
	District string   `json:"district,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Z        *float64 `json:"z,omitempty"`
	// ── 批次84 新增(与 citysystem 镜像逐字一致)──
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
	Points   []DisplayPoint `json:"points,omitempty"`
}

// DisplayCommand 是一条结构化显示指令(中性 wire 形状,前端 3D 引擎消费)。
// ⚠ JSON 字段名/顺序必须与 citysystem/display.go::displayCommand 逐字一致。
type DisplayCommand struct {
	Type     string  `json:"type"`
	District string  `json:"district,omitempty"`
	X        float64 `json:"x,omitempty"`
	Z        float64 `json:"z,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	Text     string  `json:"text,omitempty"`
	Color    string  `json:"color,omitempty"`
	TraceID  string  `json:"trace_id,omitempty"`
	// ── 批次84 新增(与 citysystem 镜像逐字一致)──
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
	Points   []DisplayPoint `json:"points,omitempty"`
}

// 显示指令类型常量(与方案文档 §4.5.1 / 前端 cityShowAdapter 契约一致)。
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

// DisplayModes 是 set_mode 的合法取值(mode 只能 orbit|fly)。
var DisplayModes = []string{"orbit", "fly"}

// ── 批次84 新增的枚举取值域(与方案文档 §4.5.1 逐字一致)──

// DisplayPhases 是 set_time_of_day 的合法时段取值。
var DisplayPhases = []string{"dawn", "day", "dusk", "night"}

// DisplayWeathers 是 set_weather 的合法天气取值。
var DisplayWeathers = []string{"clear", "rain", "snow", "fog", "haze", "sandstorm", "storm"}

// DisplaySpeeds 是 set_time_speed 的合法时间流速取值。
var DisplaySpeeds = []string{"pause", "slow", "normal", "fast", "xfast"}

// DisplayEntityKinds 是 focus_entity 的合法实体类别取值。
var DisplayEntityKinds = []string{"building", "resident", "vehicle"}

// DisplayLayers 是 set_layer_visibility 的合法图层取值。
var DisplayLayers = []string{"buildings", "trees", "vehicles", "pedestrians", "props", "labels", "water"}

// DisplayFilterModes 是 set_filter 的合法呈现方式取值(写 Mode 字段)。
var DisplayFilterModes = []string{"highlight", "dim"}

// DisplayCameraPresets 是 camera_preset 的合法机位预设取值。
var DisplayCameraPresets = []string{"bird", "eye", "street", "top", "free"}

// NormalizeMode 归一化相机模式:仅接受 orbit|fly(大小写不敏感),
// 其它值返回 ""(调用方丢弃,不产指令)。
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "orbit":
		return "orbit"
	case "fly":
		return "fly"
	default:
		return ""
	}
}

// NormalizeEnum 归一化枚举取值:小写 + 去首尾空格后必须命中 allowed,
// 否则返回 ""(调用方丢弃 / 报错,不产指令)。
//
// allowed 为 nil 时返回 ""(空取值域 = 一切都非法)。
func NormalizeEnum(value string, allowed []string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return ""
	}
	for _, a := range allowed {
		if v == a {
			return a
		}
	}
	return ""
}

// ParseDisplayRequest 把 citybus Message.Payload 解码为 DisplayRequest。
//
// 兼容两种载荷形态(跨包不 import citysystem,统一走 JSON 往返):
//   - 本包 / 镜像结构体(字段名一致的任意 struct);
//   - map[string]any / JSON 文本(citysystem 侧重构后的通用形态)。
func ParseDisplayRequest(payload any) (DisplayRequest, error) {
	if payload == nil {
		return DisplayRequest{}, fmt.Errorf("citywebgl: empty display request payload")
	}
	if r, ok := payload.(DisplayRequest); ok {
		return r, nil
	}
	if raw, ok := payload.([]byte); ok {
		var req DisplayRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return DisplayRequest{}, fmt.Errorf("citywebgl: unmarshal display request: %w", err)
		}
		return req, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return DisplayRequest{}, fmt.Errorf("citywebgl: marshal display request: %w", err)
	}
	var req DisplayRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return DisplayRequest{}, fmt.Errorf("citywebgl: unmarshal display request: %w", err)
	}
	return req, nil
}

// HasStructuredParams 报告请求是否齐备结构化参数,调用方据此选
// Structured(零延迟)而非 Run(LLM)路径。
//
// 批次84 扩展:除 district / x,z 外,新增的 11 个参数(phase/hour/weather/
// speed/entity_id/kind/layer/visible/category/preset/points)齐备任意一项
// 也算「结构化齐备」—— 它们同样可以**不过 LLM** 直译成显示指令。
//
// ⚠ ws 层当前用的是**内联**判据(`req.District != "" || req.X != nil ||
// req.Z != nil`),未覆盖批次84 新参数(见本批实施报告的「待接线」项)。
func (r DisplayRequest) HasStructuredParams() bool {
	if r.District != "" || r.X != nil || r.Z != nil {
		return true
	}
	if r.Phase != "" || r.Hour != nil || r.Weather != "" || r.Speed != "" {
		return true
	}
	if r.EntityID != "" || r.Kind != "" || r.Layer != "" || r.Visible != nil {
		return true
	}
	if r.Category != "" || r.Preset != "" || len(r.Points) > 0 {
		return true
	}
	return false
}
