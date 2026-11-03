// Package citywebgl — tools_test.go: 6 个显示原语的 schema 与派发单测
// (2026-11-03 §批次81)。每个工具至少 1 正 1 负(§130 防「声明了却从不接线」)。
package citywebgl

import (
	"testing"
)

// TestBuildTools_Schema 工具集完整性:6 工具、名实一致、schema 形状合法。
func TestBuildTools_Schema(t *testing.T) {
	tools := BuildTools()
	if len(tools) != len(ToolNames()) {
		t.Fatalf("BuildTools=%d, ToolNames=%d", len(tools), len(ToolNames()))
	}
	names := make(map[string]bool, len(tools))
	for _, td := range tools {
		names[td.Name] = true
		if td.Description == "" {
			t.Fatalf("tool %s: empty description", td.Name)
		}
		schema, ok := td.InputSchema["type"].(string)
		if !ok || schema != "object" {
			t.Fatalf("tool %s: input_schema.type must be object", td.Name)
		}
		if _, ok := td.InputSchema["properties"].(map[string]any); !ok {
			t.Fatalf("tool %s: missing properties", td.Name)
		}
	}
	for _, n := range ToolNames() {
		if !names[n] {
			t.Fatalf("tool %s in ToolNames but not in BuildTools", n)
		}
	}
	// 必填参数钉死(缺一即回归)。
	wantRequired := map[string][]string{
		ToolFocusDistrict:     {"district"},
		ToolFocusPoint:        {"x", "z"},
		ToolSetMode:           {"mode"},
		ToolHighlightDistrict: {"district"},
		ToolAnnotate:          {"text"},
	}
	for _, td := range tools {
		want, ok := wantRequired[td.Name]
		if !ok {
			continue
		}
		req, _ := td.InputSchema["required"].([]string)
		if len(req) != len(want) {
			t.Fatalf("tool %s required = %v, want %v", td.Name, req, want)
		}
		for i := range want {
			if req[i] != want[i] {
				t.Fatalf("tool %s required = %v, want %v", td.Name, req, want)
			}
		}
	}
}

// TestDispatchTool_Positive 6 工具正例:产出的 DisplayCommand 字段精确匹配。
func TestDispatchTool_Positive(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  DisplayCommand
	}{
		{ToolFocusDistrict, map[string]any{"district": "d1"},
			DisplayCommand{Type: DisplayFocusDistrict, District: "d1"}},
		{ToolFocusPoint, map[string]any{"x": 1.5, "z": -2.0},
			DisplayCommand{Type: DisplayFocusPoint, X: 1.5, Z: -2.0}},
		{ToolSetMode, map[string]any{"mode": "fly"},
			DisplayCommand{Type: DisplaySetMode, Mode: "fly"}},
		{ToolSetMode, map[string]any{"mode": "Orbit"},
			DisplayCommand{Type: DisplaySetMode, Mode: "orbit"}},
		{ToolHighlightDistrict, map[string]any{"district": "d2", "color": "#00ff00"},
			DisplayCommand{Type: DisplayHighlightDistrict, District: "d2", Color: "#00ff00"}},
		{ToolHighlightDistrict, map[string]any{"district": "d2"},
			DisplayCommand{Type: DisplayHighlightDistrict, District: "d2"}},
		{ToolAnnotate, map[string]any{"text": "拥堵", "district": "d3"},
			DisplayCommand{Type: DisplayAnnotate, Text: "拥堵", District: "d3"}},
		{ToolAnnotate, map[string]any{"text": "全局注意"},
			DisplayCommand{Type: DisplayAnnotate, Text: "全局注意"}},
		{ToolClearDisplay, map[string]any{},
			DisplayCommand{Type: DisplayClear}},
		// clear_display 工具名 ≠ 指令类型 clear(契约常量)。
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, text, isErr := dispatchTool(tc.name, tc.input, "tr-9")
			if isErr || cmd == nil {
				t.Fatalf("unexpected error: %q (cmd=%v)", text, cmd)
			}
			tc.want.TraceID = "tr-9"
			if *cmd != tc.want {
				t.Fatalf("cmd = %+v, want %+v", *cmd, tc.want)
			}
			if text == "" {
				t.Fatal("empty tool_result text")
			}
		})
	}
}

// TestDispatchTool_Negative 缺参/非法参数/未知工具 → isErr 且不产指令。
func TestDispatchTool_Negative(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
	}{
		{ToolFocusDistrict, map[string]any{}},
		{ToolFocusDistrict, map[string]any{"district": "  "}},
		{ToolFocusPoint, map[string]any{"x": 1.0}},               // 缺 z
		{ToolFocusPoint, map[string]any{"z": 1.0}},               // 缺 x
		{ToolFocusPoint, map[string]any{"x": "1", "z": "2"}},     // 非数值
		{ToolSetMode, map[string]any{"mode": "spin"}},            // 非法 mode
		{ToolSetMode, map[string]any{}},                          // 缺 mode
		{ToolHighlightDistrict, map[string]any{"color": "#fff"}}, // 缺 district
		{ToolAnnotate, map[string]any{"district": "d1"}},         // 缺 text
		{ToolAnnotate, map[string]any{"text": "   "}},            // 空白 text
		{"no_such_tool", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, text, isErr := dispatchTool(tc.name, tc.input, "")
			if !isErr || cmd != nil {
				t.Fatalf("expected error, got cmd=%+v text=%q", cmd, text)
			}
		})
	}
}

// TestDispatchTool_AnnotateTruncation annotate 文本超 40 字截断(纪律口径)。
func TestDispatchTool_AnnotateTruncation(t *testing.T) {
	long := ""
	for i := 0; i < 50; i++ {
		long += "字"
	}
	cmd, _, isErr := dispatchTool(ToolAnnotate, map[string]any{"text": long}, "")
	if isErr || cmd == nil {
		t.Fatalf("unexpected error: isErr=%v", isErr)
	}
	if n := len([]rune(cmd.Text)); n != annotateTextLimit {
		t.Fatalf("annotate text runes = %d, want %d", n, annotateTextLimit)
	}
}

// TestDispatchTool_NilInput nil input 视作空 map(缺参路径,不 panic)。
func TestDispatchTool_NilInput(t *testing.T) {
	if _, _, isErr := dispatchTool(ToolFocusDistrict, nil, ""); !isErr {
		t.Fatal("nil input must error for focus_district")
	}
	if cmd, _, isErr := dispatchTool(ToolClearDisplay, nil, ""); isErr || cmd == nil {
		t.Fatalf("clear_display accepts nil input, got isErr=%v cmd=%v", isErr, cmd)
	}
}
