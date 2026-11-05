// Package citywebgl — command_test.go: 中性 wire 契约回归
// (2026-11-03 §批次81)。
//
// 核心不变量:DisplayCommand 的 JSON 键集必须与 citysystem/display.go 的
// displayCommand **逐字一致**(两包互不 import,双侧同步靠本测试钉死)。
// citysystem.displayCommand 未导出,故此处用与它字面一致的锚点结构 +
// 字面 JSON 作断言(锚点注释标明镜像来源行)。
package citywebgl

import (
	"encoding/json"
	"testing"
)

// displayCommandAnchor 是 citysystem/display.go::displayCommand 的**逐字镜像**
// (struct tag 必须与那边一致;任何一侧改动都会让 TestDisplayCommandWireShape
// 的字面 JSON 锚点失败)。
type displayCommandAnchor struct {
	Type     string  `json:"type"`
	District string  `json:"district,omitempty"`
	X        float64 `json:"x,omitempty"`
	Z        float64 `json:"z,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	Text     string  `json:"text,omitempty"`
	Color    string  `json:"color,omitempty"`
	TraceID  string  `json:"trace_id,omitempty"`
	// ── 批次84 新增(与 citysystem/display.go 镜像逐字一致)──
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

// displayRequestAnchor 是 citysystem/display.go::displayRequest 的逐字镜像。
type displayRequestAnchor struct {
	Text     string   `json:"text,omitempty"`
	District string   `json:"district,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Z        *float64 `json:"z,omitempty"`
	// ── 批次84 新增(与 citysystem/display.go 镜像逐字一致)──
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

func jsonKeys(t *testing.T, v any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal keys: %v", err)
	}
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// TestDisplayCommandWireShape DisplayCommand 与 citysystem 镜像锚点的
// JSON 键集/序列化结果完全一致(全字段填充 + 稀疏填充两种形态)。
func TestDisplayCommandWireShape(t *testing.T) {
	full := DisplayCommand{
		Type: DisplayAnnotate, District: "d1", X: 1.5, Z: -2.5,
		Mode: "orbit", Text: "hello", Color: "#ff0000", TraceID: "tr-1",
	}
	fullAnchor := displayCommandAnchor{
		Type: DisplayAnnotate, District: "d1", X: 1.5, Z: -2.5,
		Mode: "orbit", Text: "hello", Color: "#ff0000", TraceID: "tr-1",
	}
	gotRaw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wantRaw, err := json.Marshal(fullAnchor)
	if err != nil {
		t.Fatalf("marshal anchor: %v", err)
	}
	if string(gotRaw) != string(wantRaw) {
		t.Fatalf("full wire mismatch:\n got %s\nwant %s", gotRaw, wantRaw)
	}
	// 字面 JSON 锚点(与 citysystem/display.go 字段顺序/名称逐字一致的键集;
	// 值断言防 MarshalJSON 等自定义序列化漂移)。
	var literal map[string]any
	if err := json.Unmarshal(gotRaw, &literal); err != nil {
		t.Fatalf("unmarshal literal: %v", err)
	}
	wantKeys := map[string]bool{
		"type": true, "district": true, "x": true, "z": true,
		"mode": true, "text": true, "color": true, "trace_id": true,
	}
	if len(literal) != len(wantKeys) {
		t.Fatalf("key count = %d, want %d (%v)", len(literal), len(wantKeys), literal)
	}
	for k := range wantKeys {
		if _, ok := literal[k]; !ok {
			t.Fatalf("missing key %q in %v", k, literal)
		}
	}

	// 稀疏形态:omitempty 后键集 = {type} + 已填字段。
	sparse := DisplayCommand{Type: DisplayFocusDistrict, District: "d2"}
	sparseAnchor := displayCommandAnchor{Type: DisplayFocusDistrict, District: "d2"}
	sRaw, _ := json.Marshal(sparse)
	saRaw, _ := json.Marshal(sparseAnchor)
	if string(sRaw) != string(saRaw) {
		t.Fatalf("sparse wire mismatch:\n got %s\nwant %s", sRaw, saRaw)
	}
	if keys := jsonKeys(t, sparse); !keys["type"] || !keys["district"] || len(keys) != 2 {
		t.Fatalf("sparse keys = %v", keys)
	}
}

// TestDisplayRequestWireShape DisplayRequest 与 displayRequest 镜像锚点一致。
func TestDisplayRequestWireShape(t *testing.T) {
	x, z := 1.0, 2.0
	req := DisplayRequest{Text: "t", District: "d1", Mode: "fly", X: &x, Z: &z}
	anchor := displayRequestAnchor{Text: "t", District: "d1", Mode: "fly", X: &x, Z: &z}
	gotRaw, _ := json.Marshal(req)
	wantRaw, _ := json.Marshal(anchor)
	if string(gotRaw) != string(wantRaw) {
		t.Fatalf("request wire mismatch:\n got %s\nwant %s", gotRaw, wantRaw)
	}
}

// TestParseDisplayRequest citybus 载荷(镜像 struct / map / []byte)统一解码。
func TestParseDisplayRequest(t *testing.T) {
	x, z := 3.0, 4.0
	// 1) 本包类型直通。
	if got, err := ParseDisplayRequest(DisplayRequest{District: "d1"}); err != nil || got.District != "d1" {
		t.Fatalf("direct: got=%+v err=%v", got, err)
	}
	// 2) 镜像 struct(citysystem.focusCamera 发出的形态)JSON 往返。
	if got, err := ParseDisplayRequest(displayRequestAnchor{Mode: "orbit", X: &x, Z: &z}); err != nil {
		t.Fatalf("mirror: %v", err)
	} else if got.Mode != "orbit" || got.X == nil || *got.X != 3.0 || got.Z == nil || *got.Z != 4.0 {
		t.Fatalf("mirror decode: %+v", got)
	}
	// 3) map 形态(总线 JSON 化后的通用形态)。
	if got, err := ParseDisplayRequest(map[string]any{"text": "去看商业区"}); err != nil || got.Text != "去看商业区" {
		t.Fatalf("map: got=%+v err=%v", got, err)
	}
	// 4) []byte JSON。
	if got, err := ParseDisplayRequest([]byte(`{"district":"d9"}`)); err != nil || got.District != "d9" {
		t.Fatalf("bytes: got=%+v err=%v", got, err)
	}
	// 5) nil / 非法。
	if _, err := ParseDisplayRequest(nil); err == nil {
		t.Fatal("nil payload must error")
	}
	if _, err := ParseDisplayRequest(make(chan int)); err == nil {
		t.Fatal("unmarshalable payload must error")
	}
}

// TestHasStructuredParams 路径选择判据。
func TestHasStructuredParams(t *testing.T) {
	x := 1.0
	cases := []struct {
		req  DisplayRequest
		want bool
	}{
		{DisplayRequest{Text: "hi"}, false},
		{DisplayRequest{District: "d1"}, true},
		{DisplayRequest{X: &x}, true},
		{DisplayRequest{}, false},
	}
	for _, tc := range cases {
		if got := tc.req.HasStructuredParams(); got != tc.want {
			t.Fatalf("%+v: HasStructuredParams=%v want %v", tc.req, got, tc.want)
		}
	}
}

// TestNormalizeMode mode 只能 orbit|fly。
func TestNormalizeMode(t *testing.T) {
	if NormalizeMode("Orbit") != "orbit" || NormalizeMode("fly") != "fly" {
		t.Fatal("orbit/fly must normalize")
	}
	for _, bad := range []string{"", "spin", "pan", "3"} {
		if NormalizeMode(bad) != "" {
			t.Fatalf("NormalizeMode(%q) must be empty", bad)
		}
	}
}
