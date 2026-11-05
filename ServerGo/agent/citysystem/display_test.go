// Package citysystem — display_test.go: display.go 中性 wire 结构的
// JSON 锚点测试 + apply_display 的校验单测(2026-11-05 §批次84)。
//
// 契约: 批次84 方案文档 §4.5.2 —— `citysystem/display.go` 与
// `citywebgl/command.go` 各持一份**逐字一致**的镜像结构(两包互不 import,
// 经 citybus JSON 往返)。本测试用**字面 JSON 锚点**钉死字段名 / tag /
// 顺序,任一侧改动都会红(两侧各有同口径测试)。
package citysystem

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestDisplayRequestJSONAnchor displayRequest 序列化为逐字节锚点 JSON。
//
// 字段顺序 = struct 声明顺序(批次81 的 5 个 + 批次84 的 11 个);
// omitempty 语义:未设字段不出现在 JSON。
func TestDisplayRequestJSONAnchor(t *testing.T) {
	x, z := 12.5, -30.0
	hour := 21.0
	visible := false
	req := displayRequest{
		Text:     "看中心",
		District: "d1",
		Mode:     "orbit",
		X:        &x,
		Z:        &z,
		Phase:    "night",
		Hour:     &hour,
		Weather:  "rain",
		Speed:    "fast",
		EntityID: "b-9",
		Kind:     "building",
		Layer:    "trees",
		Visible:  &visible,
		Category: "hospital",
		Preset:   "bird",
		Points:   []displayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"text":"看中心","district":"d1","mode":"orbit","x":12.5,"z":-30,` +
		`"phase":"night","hour":21,"weather":"rain","speed":"fast",` +
		`"entity_id":"b-9","kind":"building","layer":"trees","visible":false,` +
		`"category":"hospital","preset":"bird","points":[{"x":1,"z":2},{"x":3,"z":4}]}`
	if string(raw) != want {
		t.Fatalf("displayRequest JSON mismatch\n got: %s\nwant: %s", raw, want)
	}
}

// TestDisplayRequestJSONOmitEmpty 未设字段不出现在 JSON(omitted)。
func TestDisplayRequestJSONOmitEmpty(t *testing.T) {
	req := displayRequest{District: "d1"}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"district":"d1"}`
	if string(raw) != want {
		t.Fatalf("omitempty mismatch\n got: %s\nwant: %s", raw, want)
	}
}

// TestDisplayCommandJSONAnchor displayCommand 序列化为逐字节锚点 JSON。
func TestDisplayCommandJSONAnchor(t *testing.T) {
	hour := 6.5
	visible := true
	cmd := displayCommand{
		Type:     DisplaySetTimeOfDay,
		District: "d1",
		X:        1.5,
		Z:        -2.5,
		Mode:     "fly",
		Text:     "备注",
		Color:    "#ff0000",
		TraceID:  "tr-1",
		Phase:    "dawn",
		Hour:     &hour,
		Weather:  "fog",
		Speed:    "slow",
		EntityID: "r-1",
		Kind:     "resident",
		Layer:    "water",
		Visible:  &visible,
		Category: "park",
		Preset:   "top",
		Points:   []displayPoint{{X: 0, Z: 0}},
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"set_time_of_day","district":"d1","x":1.5,"z":-2.5,` +
		`"mode":"fly","text":"备注","color":"#ff0000","trace_id":"tr-1",` +
		`"phase":"dawn","hour":6.5,"weather":"fog","speed":"slow",` +
		`"entity_id":"r-1","kind":"resident","layer":"water","visible":true,` +
		`"category":"park","preset":"top","points":[{"x":0,"z":0}]}`
	if string(raw) != want {
		t.Fatalf("displayCommand JSON mismatch\n got: %s\nwant: %s", raw, want)
	}
}

// TestDisplayCommandTypes_All14 批次84 §4.5.1:显示指令类型常量共 14 个,
// 取值逐字与 citywebgl 侧一致(镜像)。
func TestDisplayCommandTypes_All14(t *testing.T) {
	all := []string{
		DisplayFocusDistrict, DisplayFocusPoint, DisplaySetMode,
		DisplayHighlightDistrict, DisplayAnnotate, DisplayClear,
		DisplaySetTimeOfDay, DisplaySetWeather, DisplaySetTimeSpeed,
		DisplayFocusEntity, DisplaySetLayerVisibility, DisplaySetFilter,
		DisplayCameraPreset, DisplayFlyPath,
	}
	want := map[string]string{
		"focus_district": "1", "focus_point": "1", "set_mode": "1",
		"highlight_district": "1", "annotate": "1", "clear": "1",
		"set_time_of_day": "1", "set_weather": "1", "set_time_speed": "1",
		"focus_entity": "1", "set_layer_visibility": "1", "set_filter": "1",
		"camera_preset": "1", "fly_path": "1",
	}
	if len(all) != 14 {
		t.Fatalf("display command types = %d, want 14", len(all))
	}
	for _, c := range all {
		if want[c] == "" {
			t.Errorf("unexpected command type %q", c)
		}
		delete(want, c)
	}
	for k := range want {
		t.Errorf("missing command type %q", k)
	}
}

// TestBuildDisplayRequest_Valid 合法参数归一化:枚举命中 / 数值区间 /
// layer+visible 同给 / points 2-8 点。
func TestBuildDisplayRequest_Valid(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		check func(t *testing.T, r displayRequest)
	}{
		{"phase", map[string]any{"phase": "Night"}, func(t *testing.T, r displayRequest) {
			if r.Phase != "night" {
				t.Errorf("phase = %q, want night (枚举须小写归一)", r.Phase)
			}
		}},
		{"hour", map[string]any{"hour": 21.5}, func(t *testing.T, r displayRequest) {
			if r.Hour == nil || *r.Hour != 21.5 {
				t.Errorf("hour = %v, want 21.5", r.Hour)
			}
		}},
		{"weather", map[string]any{"weather": "sandstorm"}, func(t *testing.T, r displayRequest) {
			if r.Weather != "sandstorm" {
				t.Errorf("weather = %q", r.Weather)
			}
		}},
		{"speed", map[string]any{"speed": "xfast"}, func(t *testing.T, r displayRequest) {
			if r.Speed != "xfast" {
				t.Errorf("speed = %q", r.Speed)
			}
		}},
		{"layer+visible", map[string]any{"layer": "trees", "visible": true}, func(t *testing.T, r displayRequest) {
			if r.Layer != "trees" || r.Visible == nil || !*r.Visible {
				t.Errorf("layer/visible = %q/%v", r.Layer, r.Visible)
			}
		}},
		{"entity+kind", map[string]any{"entity_id": "b-1", "kind": "building"}, func(t *testing.T, r displayRequest) {
			if r.EntityID != "b-1" || r.Kind != "building" {
				t.Errorf("entity/kind = %q/%q", r.EntityID, r.Kind)
			}
		}},
		{"preset", map[string]any{"preset": "bird"}, func(t *testing.T, r displayRequest) {
			if r.Preset != "bird" {
				t.Errorf("preset = %q", r.Preset)
			}
		}},
		{"points", map[string]any{"points": []any{
			map[string]any{"x": 1, "z": 2}, map[string]any{"x": 3, "z": 4},
		}}, func(t *testing.T, r displayRequest) {
			if len(r.Points) != 2 || r.Points[0].X != 1 || r.Points[1].Z != 4 {
				t.Errorf("points = %+v", r.Points)
			}
		}},
		{"category", map[string]any{"category": "hospital"}, func(t *testing.T, r displayRequest) {
			if r.Category != "hospital" {
				t.Errorf("category = %q", r.Category)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := buildDisplayRequest(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, r)
		})
	}
}

// TestBuildDisplayRequest_Invalid 非法参数 ⇒ error(apply_display 会转
// isErr 失败文本,不转发给渲染 Agent)。
func TestBuildDisplayRequest_Invalid(t *testing.T) {
	bigPoints := make([]any, flyPathMaxPoints+1)
	for i := range bigPoints {
		bigPoints[i] = map[string]any{"x": float64(i), "z": 0}
	}
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"unknown phase", map[string]any{"phase": "midnight"}, "phase 只能"},
		{"unknown weather", map[string]any{"weather": "sunny"}, "weather 只能"},
		{"unknown speed", map[string]any{"speed": "warp"}, "speed 只能"},
		{"unknown layer", map[string]any{"layer": "clouds", "visible": true}, "layer 只能"},
		{"unknown preset", map[string]any{"preset": "sky"}, "preset 只能"},
		{"unknown kind", map[string]any{"entity_id": "b-1", "kind": "tree"}, "kind 只能"},
		{"hour out of range", map[string]any{"hour": 25}, "hour 必须是"},
		{"hour NaN", map[string]any{"hour": math.NaN()}, "hour 必须是"},
		{"points too many", map[string]any{"points": bigPoints}, "点数须在 2-8 之间"},
		{"points too few", map[string]any{"points": []any{map[string]any{"x": 1, "z": 2}}}, "点数须在 2-8 之间"},
		{"points bad coord", map[string]any{"points": []any{
			map[string]any{"x": 1, "z": 2}, map[string]any{"x": math.NaN(), "z": 4},
		}}, "points 必须是"},
		{"layer without visible", map[string]any{"layer": "trees"}, "layer 与 visible 必须同时给出"},
		{"visible without layer", map[string]any{"visible": true}, "layer 与 visible 必须同时给出"},
		{"no params", map[string]any{}, "至少给出一个显示参数"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildDisplayRequest(tc.input)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestParseDisplayCommands_RoundTrip 中性结构经 JSON 往返可解析
// (总线跨包传输的契约)。
func TestParseDisplayCommands_RoundTrip(t *testing.T) {
	// 模拟 citywebgl 侧返回的通用 JSON 形态(经总线 map/[]any 往返)。
	res := []any{
		map[string]any{"type": "set_weather", "weather": "rain"},
		map[string]any{"type": "set_time_of_day", "phase": "night", "hour": 21.0},
	}
	cmds, err := parseDisplayCommands(res)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("len = %d, want 2", len(cmds))
	}
	if cmds[0].Type != DisplaySetWeather || cmds[0].Weather != "rain" {
		t.Fatalf("cmd0 = %+v", cmds[0])
	}
	if cmds[1].Type != DisplaySetTimeOfDay || cmds[1].Hour == nil || *cmds[1].Hour != 21.0 {
		t.Fatalf("cmd1 = %+v", cmds[1])
	}
	if got := describeDisplayCommands(cmds); got != "set_weather, set_time_of_day" {
		t.Fatalf("describe = %q", got)
	}
	// 单对象形态(非数组)也兼容。
	one, err := parseDisplayCommands(map[string]any{"type": "clear"})
	if err != nil || len(one) != 1 || one[0].Type != DisplayClear {
		t.Fatalf("single object parse = %+v / %v", one, err)
	}
}
