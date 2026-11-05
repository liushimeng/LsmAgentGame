// Package citywebgl — tools_batch84_test.go: 批次84 新增 8 个显示原语的
// schema 与派发单测(2026-11-05 §批次84 §4.5.1 #7–#14)。
//
// 每个新原语至少 1 正 1 负(§130 防「声明了却从不接线」)。
package citywebgl

import (
	"reflect"
	"testing"

	llmtypes "LsmAgentGame/llm/types"
)

// TestToolNames_Count14 工具名清单恰为 14 个(方案文档 §10.1 硬性断言)。
func TestToolNames_Count14(t *testing.T) {
	names := ToolNames()
	if len(names) != 14 {
		t.Fatalf("ToolNames() = %d, want 14 (%v)", len(names), names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" {
			t.Fatal("empty tool name")
		}
		if seen[n] {
			t.Fatalf("duplicated tool name %q", n)
		}
		seen[n] = true
	}
	// 批次81 的 6 个必须仍在(零回归)。
	for _, n := range []string{ToolFocusDistrict, ToolFocusPoint, ToolSetMode,
		ToolHighlightDistrict, ToolAnnotate, ToolClearDisplay} {
		if !seen[n] {
			t.Fatalf("batch81 tool %q missing", n)
		}
	}
	// 批次84 的 8 个必须齐备。
	for _, n := range []string{ToolSetTimeOfDay, ToolSetWeather, ToolSetTimeSpeed,
		ToolFocusEntity, ToolSetLayerVisibility, ToolSetFilter,
		ToolCameraPreset, ToolFlyPath} {
		if !seen[n] {
			t.Fatalf("batch84 tool %q missing", n)
		}
	}
}

// TestBuildTools_Count14 BuildTools 与 ToolNames 数量一致。
func TestBuildTools_Count14(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 14 || len(tools) != len(ToolNames()) {
		t.Fatalf("BuildTools=%d ToolNames=%d, want 14", len(tools), len(ToolNames()))
	}
	names := map[string]bool{}
	for _, td := range tools {
		names[td.Name] = true
	}
	for _, n := range ToolNames() {
		if !names[n] {
			t.Fatalf("tool %q in ToolNames but not in BuildTools", n)
		}
	}
}

// TestBuildTools_Batch84Schemas 批次84 新工具的 schema 形状与枚举钉死。
func TestBuildTools_Batch84Schemas(t *testing.T) {
	tools := BuildTools()
	byName := make(map[string]llmtypes.ToolDef, len(tools))
	for _, td := range tools {
		byName[td.Name] = td
	}
	// required 钉死。
	wantRequired := map[string][]string{
		ToolSetTimeOfDay:       {"phase"},
		ToolSetWeather:         {"weather"},
		ToolSetTimeSpeed:       {"speed"},
		ToolFocusEntity:        {"entity_id"},
		ToolSetLayerVisibility: {"layer", "visible"},
		ToolSetFilter:          nil, // 无必填
		ToolCameraPreset:       {"preset"},
		ToolFlyPath:            {"points"},
	}
	// enum 钉死。
	wantEnum := map[string]struct {
		prop string
		enum []string
	}{
		ToolSetTimeOfDay:       {"phase", DisplayPhases},
		ToolSetWeather:         {"weather", DisplayWeathers},
		ToolSetTimeSpeed:       {"speed", DisplaySpeeds},
		ToolFocusEntity:        {"kind", DisplayEntityKinds},
		ToolSetLayerVisibility: {"layer", DisplayLayers},
		ToolSetFilter:          {"mode", DisplayFilterModes},
		ToolCameraPreset:       {"preset", DisplayCameraPresets},
	}
	for _, td := range tools {
		if td.Description == "" {
			t.Fatalf("tool %s: empty description", td.Name)
		}
		if s, ok := td.InputSchema["type"].(string); !ok || s != "object" {
			t.Fatalf("tool %s: input_schema.type must be object", td.Name)
		}
		props, ok := td.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s: missing properties", td.Name)
		}
		// required 名单。
		if want, ok := wantRequired[td.Name]; ok {
			req, _ := td.InputSchema["required"].([]string)
			if want == nil {
				if len(req) != 0 {
					t.Fatalf("tool %s: required = %v, want none", td.Name, req)
				}
			} else if !reflect.DeepEqual(req, want) {
				t.Fatalf("tool %s: required = %v, want %v", td.Name, req, want)
			}
		}
		// enum 取值。
		if spec, ok := wantEnum[td.Name]; ok {
			p, ok := props[spec.prop].(map[string]any)
			if !ok {
				t.Fatalf("tool %s: missing property %q", td.Name, spec.prop)
			}
			enum, ok := p["enum"].([]string)
			if !ok {
				t.Fatalf("tool %s: property %q missing enum", td.Name, spec.prop)
			}
			if !reflect.DeepEqual(enum, spec.enum) {
				t.Fatalf("tool %s: %s enum = %v, want %v", td.Name, spec.prop, enum, spec.enum)
			}
		}
	}
	// fly_path 的 points 是数组且有 2-8 的边界。
	fp := byName[ToolFlyPath]
	points, ok := fp.InputSchema["properties"].(map[string]any)["points"].(map[string]any)
	if !ok {
		t.Fatal("fly_path: missing points schema")
	}
	if points["type"] != "array" {
		t.Fatalf("fly_path: points type = %v, want array", points["type"])
	}
	if points["minItems"] != flyPathMinPoints || points["maxItems"] != flyPathMaxPoints {
		t.Fatalf("fly_path: min/maxItems = %v/%v", points["minItems"], points["maxItems"])
	}
	// set_layer_visibility 的 visible 必须是 boolean。
	lv := byName[ToolSetLayerVisibility]
	visible, ok := lv.InputSchema["properties"].(map[string]any)["visible"].(map[string]any)
	if !ok || visible["type"] != "boolean" {
		t.Fatalf("set_layer_visibility: visible must be boolean, got %v", visible)
	}
}

// TestDispatchTool_Batch84Positive 批次84 新原语正例:产出字段精确匹配。
func TestDispatchTool_Batch84Positive(t *testing.T) {
	hour := 21.0
	visible := false
	cases := []struct {
		name  string
		input map[string]any
		want  DisplayCommand
	}{
		{ToolSetTimeOfDay, map[string]any{"phase": "dusk"},
			DisplayCommand{Type: DisplaySetTimeOfDay, Phase: "dusk"}},
		{ToolSetTimeOfDay, map[string]any{"phase": "NIGHT", "hour": 21.0},
			DisplayCommand{Type: DisplaySetTimeOfDay, Phase: "night", Hour: &hour}},
		{ToolSetWeather, map[string]any{"weather": "sandstorm"},
			DisplayCommand{Type: DisplaySetWeather, Weather: "sandstorm"}},
		{ToolSetTimeSpeed, map[string]any{"speed": "xfast"},
			DisplayCommand{Type: DisplaySetTimeSpeed, Speed: "xfast"}},
		{ToolFocusEntity, map[string]any{"entity_id": "b-12"},
			DisplayCommand{Type: DisplayFocusEntity, EntityID: "b-12"}},
		{ToolFocusEntity, map[string]any{"entity_id": "r-3", "kind": "resident"},
			DisplayCommand{Type: DisplayFocusEntity, EntityID: "r-3", Kind: "resident"}},
		{ToolSetLayerVisibility, map[string]any{"layer": "trees", "visible": true},
			DisplayCommand{Type: DisplaySetLayerVisibility, Layer: "trees", Visible: boolPtr(true)}},
		{ToolSetLayerVisibility, map[string]any{"layer": "labels", "visible": false},
			DisplayCommand{Type: DisplaySetLayerVisibility, Layer: "labels", Visible: &visible}},
		{ToolSetFilter, map[string]any{"category": "商业"},
			DisplayCommand{Type: DisplaySetFilter, Category: "商业", Mode: "highlight"}},
		{ToolSetFilter, map[string]any{"category": "商业", "mode": "dim"},
			DisplayCommand{Type: DisplaySetFilter, Category: "商业", Mode: "dim"}},
		{ToolSetFilter, map[string]any{},
			DisplayCommand{Type: DisplaySetFilter, Mode: "highlight"}},
		{ToolCameraPreset, map[string]any{"preset": "street"},
			DisplayCommand{Type: DisplayCameraPreset, Preset: "street"}},
		{ToolFlyPath, map[string]any{"points": []any{
			map[string]any{"x": 1.0, "z": 2.0},
			map[string]any{"x": 3.0, "z": 4.0},
		}}, DisplayCommand{Type: DisplayFlyPath, Points: []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}}}},
		// 空串 / nil 的**可选**参数视作「未给」(走缺省),不是非法值。
		{ToolSetFilter, map[string]any{"category": "x", "mode": ""},
			DisplayCommand{Type: DisplaySetFilter, Category: "x", Mode: "highlight"}},
		{ToolFocusEntity, map[string]any{"entity_id": "b1", "kind": ""},
			DisplayCommand{Type: DisplayFocusEntity, EntityID: "b1"}},
		{ToolSetTimeOfDay, map[string]any{"phase": "day", "hour": nil},
			DisplayCommand{Type: DisplaySetTimeOfDay, Phase: "day"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, text, isErr := dispatchTool(tc.name, tc.input, "tr-84")
			if isErr || cmd == nil {
				t.Fatalf("unexpected error: %q (cmd=%v)", text, cmd)
			}
			tc.want.TraceID = "tr-84"
			if !reflect.DeepEqual(*cmd, tc.want) {
				t.Fatalf("cmd = %+v, want %+v", *cmd, tc.want)
			}
			if text == "" {
				t.Fatal("empty tool_result text")
			}
		})
	}
}

// TestDispatchTool_Batch84Negative 非法输入 → isErr 且 cmd == nil。
func TestDispatchTool_Batch84Negative(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
	}{
		{ToolSetTimeOfDay, map[string]any{}},
		{ToolSetTimeOfDay, map[string]any{"phase": "midnight"}},
		{ToolSetTimeOfDay, map[string]any{"phase": "day", "hour": 25.0}},
		{ToolSetTimeOfDay, map[string]any{"phase": "day", "hour": -1.0}},
		{ToolSetTimeOfDay, map[string]any{"phase": "day", "hour": "noon"}},
		{ToolSetWeather, map[string]any{}},
		{ToolSetWeather, map[string]any{"weather": "sunny"}},
		{ToolSetTimeSpeed, map[string]any{}},
		{ToolSetTimeSpeed, map[string]any{"speed": "warp"}},
		{ToolFocusEntity, map[string]any{}},
		{ToolFocusEntity, map[string]any{"entity_id": "  "}},
		{ToolFocusEntity, map[string]any{"entity_id": "b1", "kind": "drone"}},
		{ToolSetLayerVisibility, map[string]any{"layer": "trees"}},
		{ToolSetLayerVisibility, map[string]any{"visible": true}},
		{ToolSetLayerVisibility, map[string]any{"layer": "sky", "visible": true}},
		{ToolSetLayerVisibility, map[string]any{"layer": "trees", "visible": "yes"}},
		{ToolSetFilter, map[string]any{"category": "x", "mode": "flat"}},
		{ToolCameraPreset, map[string]any{}},
		{ToolCameraPreset, map[string]any{"preset": "drone"}},
		{ToolFlyPath, map[string]any{}},
		{ToolFlyPath, map[string]any{"points": []any{map[string]any{"x": 1.0, "z": 2.0}}}},
		{ToolFlyPath, map[string]any{"points": []any{
			map[string]any{"x": 1.0, "z": 2.0},
			map[string]any{"x": 3.0, "z": 4.0},
			map[string]any{"x": 5.0, "z": 6.0},
			map[string]any{"x": 7.0, "z": 8.0},
			map[string]any{"x": 9.0, "z": 10.0},
			map[string]any{"x": 11.0, "z": 12.0},
			map[string]any{"x": 13.0, "z": 14.0},
			map[string]any{"x": 15.0, "z": 16.0},
			map[string]any{"x": 17.0, "z": 18.0},
		}}},
		{ToolFlyPath, map[string]any{"points": []any{
			map[string]any{"x": 1.0},
			map[string]any{"x": 3.0, "z": 4.0},
		}}},
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

// boolPtr 返回布尔指针(测试辅助)。
func boolPtr(b bool) *bool { return &b }

// TestDisplayTypesAllProducible 每个 Display* 指令类型常量都必须有工具
// 能产出(防「声明了却从不接线」§130);反之每个工具都必须产出已知类型。
func TestDisplayTypesAllProducible(t *testing.T) {
	// 每个工具的合法最小输入。
	inputs := map[string]map[string]any{
		ToolFocusDistrict:      {"district": "d1"},
		ToolFocusPoint:         {"x": 1.0, "z": 2.0},
		ToolSetMode:            {"mode": "orbit"},
		ToolHighlightDistrict:  {"district": "d1"},
		ToolAnnotate:           {"text": "hi"},
		ToolClearDisplay:       {},
		ToolSetTimeOfDay:       {"phase": "day"},
		ToolSetWeather:         {"weather": "clear"},
		ToolSetTimeSpeed:       {"speed": "normal"},
		ToolFocusEntity:        {"entity_id": "e1"},
		ToolSetLayerVisibility: {"layer": "trees", "visible": true},
		ToolSetFilter:          {"category": "c"},
		ToolCameraPreset:       {"preset": "bird"},
		ToolFlyPath: {"points": []any{
			map[string]any{"x": 0.0, "z": 0.0},
			map[string]any{"x": 1.0, "z": 1.0},
		}},
	}
	produced := map[string]bool{}
	for _, name := range ToolNames() {
		input, ok := inputs[name]
		if !ok {
			t.Fatalf("tool %q has no canonical input in the test table", name)
		}
		cmd, text, isErr := dispatchTool(name, input, "")
		if isErr || cmd == nil {
			t.Fatalf("tool %q canonical input failed: %q", name, text)
		}
		produced[cmd.Type] = true
	}
	want := []string{
		DisplayFocusDistrict, DisplayFocusPoint, DisplaySetMode,
		DisplayHighlightDistrict, DisplayAnnotate, DisplayClear,
		DisplaySetTimeOfDay, DisplaySetWeather, DisplaySetTimeSpeed,
		DisplayFocusEntity, DisplaySetLayerVisibility, DisplaySetFilter,
		DisplayCameraPreset, DisplayFlyPath,
	}
	for _, ty := range want {
		if !produced[ty] {
			t.Fatalf("display type %q is not producible by any tool", ty)
		}
	}
	if len(produced) != len(want) {
		t.Fatalf("produced types = %d, want %d (%v)", len(produced), len(want), produced)
	}
}
