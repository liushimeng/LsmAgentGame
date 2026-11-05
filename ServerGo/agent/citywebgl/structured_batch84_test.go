// Package citywebgl — structured_batch84_test.go: 结构化快路径的新参数
// 组合规则单测(2026-11-05 §批次84)。
package citywebgl

import (
	"reflect"
	"testing"
)

// TestStructured_Batch84Params 新参数各自产出一条指令(语义与 LLM 路径一致)。
func TestStructured_Batch84Params(t *testing.T) {
	hour := 21.0
	visible := false
	cases := []struct {
		name string
		req  DisplayRequest
		want []DisplayCommand
	}{
		{"phase", DisplayRequest{Phase: "dusk"},
			[]DisplayCommand{{Type: DisplaySetTimeOfDay, Phase: "dusk"}}},
		{"phase+hour", DisplayRequest{Phase: "night", Hour: &hour},
			[]DisplayCommand{{Type: DisplaySetTimeOfDay, Phase: "night", Hour: &hour}}},
		{"phase invalid", DisplayRequest{Phase: "midnight"}, []DisplayCommand{}},
		{"phase hour out of range", DisplayRequest{Phase: "night", Hour: floatPtr(30)},
			[]DisplayCommand{{Type: DisplaySetTimeOfDay, Phase: "night"}}},
		{"weather", DisplayRequest{Weather: "rain"},
			[]DisplayCommand{{Type: DisplaySetWeather, Weather: "rain"}}},
		{"weather invalid", DisplayRequest{Weather: "sunny"}, []DisplayCommand{}},
		{"speed", DisplayRequest{Speed: "fast"},
			[]DisplayCommand{{Type: DisplaySetTimeSpeed, Speed: "fast"}}},
		{"entity", DisplayRequest{EntityID: "b-12", Kind: "building"},
			[]DisplayCommand{{Type: DisplayFocusEntity, EntityID: "b-12", Kind: "building"}}},
		{"entity no kind", DisplayRequest{EntityID: "b-12"},
			[]DisplayCommand{{Type: DisplayFocusEntity, EntityID: "b-12"}}},
		{"entity invalid kind ignored", DisplayRequest{EntityID: "b-12", Kind: "drone"},
			[]DisplayCommand{{Type: DisplayFocusEntity, EntityID: "b-12"}}},
		{"layer+visible", DisplayRequest{Layer: "trees", Visible: &visible},
			[]DisplayCommand{{Type: DisplaySetLayerVisibility, Layer: "trees", Visible: &visible}}},
		{"layer without visible", DisplayRequest{Layer: "trees"}, []DisplayCommand{}},
		{"layer invalid", DisplayRequest{Layer: "sky", Visible: &visible}, []DisplayCommand{}},
		{"category default highlight", DisplayRequest{Category: "商业"},
			[]DisplayCommand{{Type: DisplaySetFilter, Category: "商业", Mode: "highlight"}}},
		{"category dim mode", DisplayRequest{Category: "商业", Mode: "dim"},
			[]DisplayCommand{{Type: DisplaySetFilter, Category: "商业", Mode: "dim"}}},
		{"preset", DisplayRequest{Preset: "bird"},
			[]DisplayCommand{{Type: DisplayCameraPreset, Preset: "bird"}}},
		{"preset invalid", DisplayRequest{Preset: "drone"}, []DisplayCommand{}},
		{"points", DisplayRequest{Points: []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}}},
			[]DisplayCommand{{Type: DisplayFlyPath, Points: []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}}}}},
		{"points too few", DisplayRequest{Points: []DisplayPoint{{X: 1, Z: 2}}}, []DisplayCommand{}},
	}
	a := NewAgent(Config{}, nil, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a.Structured(tc.req)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d cmds %+v, want %d %+v", len(got), got, len(tc.want), tc.want)
			}
			for i := range tc.want {
				if !reflect.DeepEqual(got[i], tc.want[i]) {
					t.Fatalf("cmd[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestStructured_Batch84Combination 组合顺序:聚焦 → 相机 → 环境 → 实体/图层/筛选 → 预设/路径。
func TestStructured_Batch84Combination(t *testing.T) {
	x, z := 10.0, -20.0
	req := DisplayRequest{
		District: "d1", X: &x, Z: &z, Mode: "fly",
		Phase: "dusk", Weather: "fog", Speed: "normal",
		EntityID: "b-1", Kind: "building",
		Layer: "trees", Visible: boolPtr(true),
		Category: "商业", Preset: "top",
	}
	want := []DisplayCommand{
		{Type: DisplayFocusDistrict, District: "d1"},
		{Type: DisplaySetMode, Mode: "fly"},
		{Type: DisplaySetTimeOfDay, Phase: "dusk"},
		{Type: DisplaySetWeather, Weather: "fog"},
		{Type: DisplaySetTimeSpeed, Speed: "normal"},
	}
	a := NewAgent(Config{}, nil, nil)
	got := a.Structured(req)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cmds = %+v, want %+v", got, want)
	}
	// 截断生效:上限 5 条(entity/layer/filter/preset 被裁掉)。
	if len(got) != maxStructuredCommands {
		t.Fatalf("len = %d, want %d", len(got), maxStructuredCommands)
	}
}

// TestStructured_Batch84Cap 指令条数硬上限 5(防指令风暴)。
func TestStructured_Batch84Cap(t *testing.T) {
	x, z := 1.0, 2.0
	req := DisplayRequest{
		District: "d1", X: &x, Z: &z, Mode: "fly",
		Phase: "day", Weather: "clear", Speed: "fast",
		EntityID: "e1", Layer: "trees", Visible: boolPtr(false),
		Category: "c", Preset: "eye",
		Points: []DisplayPoint{{X: 0, Z: 0}, {X: 1, Z: 1}},
	}
	a := NewAgent(Config{}, nil, nil)
	got := a.Structured(req)
	if len(got) > maxStructuredCommands {
		t.Fatalf("len = %d, want ≤ %d (%+v)", len(got), maxStructuredCommands, got)
	}
}

// TestStructured_Batch84ZeroRegression 批次81 组合规则零回归。
func TestStructured_Batch84ZeroRegression(t *testing.T) {
	x, z := 10.0, -20.0
	a := NewAgent(Config{}, nil, nil)
	got := a.Structured(DisplayRequest{District: "d1", X: &x, Z: &z})
	want := []DisplayCommand{{Type: DisplayFocusDistrict, District: "d1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cmds = %+v, want %+v", got, want)
	}
	got = a.Structured(DisplayRequest{Mode: "fly"})
	want = []DisplayCommand{{Type: DisplaySetMode, Mode: "fly"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cmds = %+v, want %+v", got, want)
	}
	got = a.Structured(DisplayRequest{Mode: "spin"})
	if len(got) != 0 {
		t.Fatalf("invalid mode must produce nothing, got %+v", got)
	}
}

// floatPtr 返回浮点指针(测试辅助)。
func floatPtr(f float64) *float64 { return &f }
