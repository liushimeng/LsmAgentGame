// Package citysystem — display.go: 3D 显示指令的**中性 wire 契约**
// (2026-11-03 §批次81)。
//
// 背景:批次81 方案文档 §3.3 把「看哪里 / 怎么显示」的翻译职责交给
// City-WebGL-Show Agent(独立 SubAgent 实现,不在本包内)。为避免
// citysystem → citywebgl 的硬依赖(两个可独立演进的包),这里定义
// **中性** 的请求 / 指令结构:经 citybus 传输时统一 JSON 往返,
// 字段名与方案文档 §3.3 / §6.1 的 wire 契约逐字一致。
//
// 字段口径:
//
//	{"type":"focus_district|focus_point|set_mode|highlight_district|annotate|clear",
//	 "district":"d1","x":0.0,"z":0.0,"mode":"orbit|fly",
//	 "text":"...","color":"...","trace_id":"tr-..."}
package citysystem

import (
	"encoding/json"
	"fmt"
	"strings"
)

// agentClassCityWebGLShow 是渲染显示 Agent 的 AgentClassName(跨包协作的
// 字面量镜像)。该常量由 citywebgl 包的责任线在 class_names.go 正式登记
// (AgentClassCityWebGLShow);此处仅作为 citybus 寻址的 to 值,避免
// citysystem → citywebgl 的编译期依赖。
const agentClassCityWebGLShow = "LsmAgentGame-City-WebGL-Show"

// displayRequest 是发给渲染显示 Agent 的结构化请求(中性 wire 形状)。
type displayRequest struct {
	Text     string   `json:"text,omitempty"`
	District string   `json:"district,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Z        *float64 `json:"z,omitempty"`
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
