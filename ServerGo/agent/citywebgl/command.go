// Package citywebgl — command.go: 3D 显示指令的中性 wire 契约
// (2026-11-03 §批次81)。
//
// 背景:批次81 方案文档 §3.3 把「看哪里 / 怎么显示」的翻译职责交给本包
// (City-WebGL-Show 渲染显示 Agent)。citysystem/display.go 有一份**逐字
// 一致**的镜像结构(displayRequest / displayCommand),两包互不 import、
// 经 citybus JSON 往返通信 —— 字段名任何一侧改动都必须双侧同步
// (command_test.go 用字面 JSON 锚点钉死)。
//
// 字段口径(与 citysystem/display.go / 方案文档 §3.3 / §6.1 逐字一致):
//
//	{"type":"focus_district|focus_point|set_mode|highlight_district|annotate|clear",
//	 "district":"d1","x":0.0,"z":0.0,"mode":"orbit|fly",
//	 "text":"...","color":"...","trace_id":"tr-..."}
package citywebgl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DisplayRequest 是显示请求(中性 wire 形状,与 citysystem/display.go
// 的 displayRequest 一致)。citybus Message.Payload 承载,经 JSON 往返解码
// (ParseDisplayRequest)。
type DisplayRequest struct {
	Text     string   `json:"text,omitempty"`
	District string   `json:"district,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Z        *float64 `json:"z,omitempty"`
}

// DisplayCommand 是一条结构化显示指令(中性 wire 形状,前端 3D 引擎消费)。
// ⚠ JSON 字段名必须与 citysystem/display.go::displayCommand 逐字一致。
type DisplayCommand struct {
	Type     string  `json:"type"`
	District string  `json:"district,omitempty"`
	X        float64 `json:"x,omitempty"`
	Z        float64 `json:"z,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	Text     string  `json:"text,omitempty"`
	Color    string  `json:"color,omitempty"`
	TraceID  string  `json:"trace_id,omitempty"`
}

// 显示指令类型常量(与方案文档 §3.3 / 前端 cityShowAdapter 契约一致)。
const (
	DisplayFocusDistrict     = "focus_district"
	DisplayFocusPoint        = "focus_point"
	DisplaySetMode           = "set_mode"
	DisplayHighlightDistrict = "highlight_district"
	DisplayAnnotate          = "annotate"
	DisplayClear             = "clear"
)

// DisplayModes 是 set_mode 的合法取值(mode 只能 orbit|fly)。
var DisplayModes = []string{"orbit", "fly"}

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

// HasStructuredParams 报告请求是否齐备结构化参数(district 或 x/z),
// 调用方据此选 Structured(零延迟)而非 Run(LLM)路径。
func (r DisplayRequest) HasStructuredParams() bool {
	return r.District != "" || r.X != nil || r.Z != nil
}
