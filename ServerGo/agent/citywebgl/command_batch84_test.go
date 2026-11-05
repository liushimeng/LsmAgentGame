// Package citywebgl — command_batch84_test.go: 批次84 wire 契约扩展单测
// (2026-11-05 §批次84 §4.5.2)。
//
// 核心不变量:批次84 新增的 11 个字段的 **JSON 键名逐字** 与方案文档
// §4.5.2 一致(且与 citysystem/display.go 的镜像结构逐字一致)。
package citywebgl

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestDisplayCommandWireShapeBatch84 批次84 新增字段的 JSON 键名锚点。
func TestDisplayCommandWireShapeBatch84(t *testing.T) {
	hour := 12.0
	visible := true
	cmd := DisplayCommand{
		Type: DisplaySetTimeOfDay, Phase: "dusk", Hour: &hour,
		Weather: "rain", Speed: "fast", EntityID: "b-12", Kind: "building",
		Layer: "trees", Visible: &visible, Category: "商业", Preset: "bird",
		Points: []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}},
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var literal map[string]any
	if err := json.Unmarshal(raw, &literal); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// 键名逐字锚点(方案文档 §4.5.2)。
	wantKeys := []string{
		"phase", "hour", "weather", "speed", "entity_id", "kind",
		"layer", "visible", "category", "preset", "points",
	}
	for _, k := range wantKeys {
		if _, ok := literal[k]; !ok {
			t.Fatalf("missing key %q in %s", k, raw)
		}
	}
	// 不得出现「嵌套化」的键(只加字段、不加嵌套)。
	for _, bad := range []string{"hour_range", "layer_visibility", "path", "filters"} {
		if _, ok := literal[bad]; ok {
			t.Fatalf("unexpected nested key %q in %s", bad, raw)
		}
	}
	// 与镜像锚点逐字一致(含字段顺序 ⇒ 字节一致)。
	anchor := displayCommandAnchor{
		Type: DisplaySetTimeOfDay, Phase: "dusk", Hour: &hour,
		Weather: "rain", Speed: "fast", EntityID: "b-12", Kind: "building",
		Layer: "trees", Visible: &visible, Category: "商业", Preset: "bird",
		Points: []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}},
	}
	anchorRaw, _ := json.Marshal(anchor)
	if string(raw) != string(anchorRaw) {
		t.Fatalf("wire mismatch:\n got %s\nwant %s", raw, anchorRaw)
	}
}

// TestDisplayCommandWireShapeBatch84_Sparse 稀疏形态:新字段 omitempty
// 后不出现(旧前端可安全忽略未知键)。
func TestDisplayCommandWireShapeBatch84_Sparse(t *testing.T) {
	raw, _ := json.Marshal(DisplayCommand{Type: DisplayFocusDistrict, District: "d1"})
	var literal map[string]any
	_ = json.Unmarshal(raw, &literal)
	if len(literal) != 2 {
		t.Fatalf("sparse keys = %v, want {type,district}", literal)
	}
	for _, k := range []string{"phase", "hour", "weather", "speed", "entity_id",
		"kind", "layer", "visible", "category", "preset", "points"} {
		if _, ok := literal[k]; ok {
			t.Fatalf("unexpected key %q in sparse %s", k, raw)
		}
	}
}

// TestDisplayRequestWireShapeBatch84 请求侧新增字段的键名锚点。
func TestDisplayRequestWireShapeBatch84(t *testing.T) {
	hour := 7.5
	visible := false
	req := DisplayRequest{
		Text: "切到夜里", Phase: "night", Hour: &hour, Weather: "fog",
		Speed: "slow", EntityID: "r-3", Kind: "resident", Layer: "labels",
		Visible: &visible, Category: "医疗", Preset: "top",
		Points: []DisplayPoint{{X: 0, Z: 0}, {X: 5, Z: 5}},
	}
	raw, _ := json.Marshal(req)
	var literal map[string]any
	_ = json.Unmarshal(raw, &literal)
	for _, k := range []string{"phase", "hour", "weather", "speed", "entity_id",
		"kind", "layer", "visible", "category", "preset", "points"} {
		if _, ok := literal[k]; !ok {
			t.Fatalf("missing key %q in %s", k, raw)
		}
	}
	anchor := displayRequestAnchor{
		Text: "切到夜里", Phase: "night", Hour: &hour, Weather: "fog",
		Speed: "slow", EntityID: "r-3", Kind: "resident", Layer: "labels",
		Visible: &visible, Category: "医疗", Preset: "top",
		Points: []DisplayPoint{{X: 0, Z: 0}, {X: 5, Z: 5}},
	}
	anchorRaw, _ := json.Marshal(anchor)
	if string(raw) != string(anchorRaw) {
		t.Fatalf("request wire mismatch:\n got %s\nwant %s", raw, anchorRaw)
	}
}

// TestParseDisplayRequestBatch84 三种载荷形态(map / JSON 字节 / 本包结构)
// 都能解出全部批次84 新字段。
func TestParseDisplayRequestBatch84(t *testing.T) {
	// 1) map 形态(citybus JSON 化后的通用形态)。
	m := map[string]any{
		"text":      "下雨",
		"phase":     "night",
		"hour":      21.0,
		"weather":   "storm",
		"speed":     "xfast",
		"entity_id": "v-7",
		"kind":      "vehicle",
		"layer":     "vehicles",
		"visible":   false,
		"category":  "交通",
		"preset":    "street",
		"points": []any{
			map[string]any{"x": 1.0, "z": 2.0},
			map[string]any{"x": 3.0, "z": 4.0},
		},
	}
	got, err := ParseDisplayRequest(m)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	assertBatch84Request(t, got)

	// 2) JSON 字节形态。
	raw, _ := json.Marshal(m)
	gotBytes, err := ParseDisplayRequest(raw)
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	assertBatch84Request(t, gotBytes)

	// 3) 本包结构形态(直通)。
	hour := 21.0
	vis := false
	direct := DisplayRequest{
		Phase: "night", Hour: &hour, Weather: "storm", Speed: "xfast",
		EntityID: "v-7", Kind: "vehicle", Layer: "vehicles", Visible: &vis,
		Category: "交通", Preset: "street",
		Points: []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}},
	}
	gotDirect, err := ParseDisplayRequest(direct)
	if err != nil {
		t.Fatalf("direct: %v", err)
	}
	assertBatch84Request(t, gotDirect)
}

// assertBatch84Request 断言请求的全部批次84 新字段都已解出。
func assertBatch84Request(t *testing.T, r DisplayRequest) {
	t.Helper()
	if r.Phase != "night" || r.Hour == nil || *r.Hour != 21 {
		t.Fatalf("phase/hour = %q/%v", r.Phase, r.Hour)
	}
	if r.Weather != "storm" || r.Speed != "xfast" {
		t.Fatalf("weather/speed = %q/%q", r.Weather, r.Speed)
	}
	if r.EntityID != "v-7" || r.Kind != "vehicle" {
		t.Fatalf("entity_id/kind = %q/%q", r.EntityID, r.Kind)
	}
	if r.Layer != "vehicles" || r.Visible == nil || *r.Visible {
		t.Fatalf("layer/visible = %q/%v", r.Layer, r.Visible)
	}
	if r.Category != "交通" || r.Preset != "street" {
		t.Fatalf("category/preset = %q/%q", r.Category, r.Preset)
	}
	want := []DisplayPoint{{X: 1, Z: 2}, {X: 3, Z: 4}}
	if !reflect.DeepEqual(r.Points, want) {
		t.Fatalf("points = %+v, want %+v", r.Points, want)
	}
}

// TestNormalizeEnum 枚举归一化:小写 + 去空格命中 allowed;否则空。
func TestNormalizeEnum(t *testing.T) {
	if got := NormalizeEnum(" DUSK ", DisplayPhases); got != "dusk" {
		t.Fatalf("NormalizeEnum(DUSK) = %q", got)
	}
	for _, bad := range []string{"", "midnight", "sunny"} {
		if got := NormalizeEnum(bad, DisplayPhases); got != "" {
			t.Fatalf("NormalizeEnum(%q) = %q, want empty", bad, got)
		}
	}
	if got := NormalizeEnum("x", nil); got != "" {
		t.Fatalf("nil allowed must reject, got %q", got)
	}
}

// TestHasStructuredParamsBatch84 新参数也算「结构化齐备」。
func TestHasStructuredParamsBatch84(t *testing.T) {
	x := 1.0
	vis := true
	cases := []struct {
		req  DisplayRequest
		want bool
	}{
		{DisplayRequest{Text: "hi"}, false},
		{DisplayRequest{Phase: "night"}, true},
		{DisplayRequest{Hour: &x}, true},
		{DisplayRequest{Weather: "rain"}, true},
		{DisplayRequest{Speed: "fast"}, true},
		{DisplayRequest{EntityID: "b1"}, true},
		{DisplayRequest{Kind: "building"}, true},
		{DisplayRequest{Layer: "trees"}, true},
		{DisplayRequest{Visible: &vis}, true},
		{DisplayRequest{Category: "商业"}, true},
		{DisplayRequest{Preset: "bird"}, true},
		{DisplayRequest{Points: []DisplayPoint{{X: 0, Z: 0}}}, true},
	}
	for _, tc := range cases {
		if got := tc.req.HasStructuredParams(); got != tc.want {
			t.Fatalf("%+v: HasStructuredParams=%v want %v", tc.req, got, tc.want)
		}
	}
}

// TestNormalizeMode_ZeroRegression 批次81 行为零回归。
func TestNormalizeMode_ZeroRegression(t *testing.T) {
	if NormalizeMode("Orbit") != "orbit" || NormalizeMode(" fly ") != "fly" {
		t.Fatal("orbit/fly must normalize")
	}
	for _, bad := range []string{"", "spin", "highlight", "dim"} {
		if NormalizeMode(bad) != "" {
			t.Fatalf("NormalizeMode(%q) must be empty", bad)
		}
	}
}
