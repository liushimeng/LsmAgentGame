// Package citywebgl — prompt_batch84_test.go: 角色段与结构化提示的单测
// (2026-11-05 §批次84)。
//
// 钉死「提示词 ↔ 代码」不漂移:roleText 必须列出 14 个原语与 7 组枚举;
// structuredHint 必须列出 11 个批次84 新参数。
package citywebgl

import (
	"strings"
	"testing"
)

// TestRoleText_ListsAllPrimitives roleText 必须逐字列出全部 14 个原语名。
func TestRoleText_ListsAllPrimitives(t *testing.T) {
	for _, name := range ToolNames() {
		if !strings.Contains(roleText, name) {
			t.Fatalf("roleText missing tool %q", name)
		}
	}
}

// TestRoleText_ListsAllEnums roleText 必须逐字列出各枚举的每个取值。
func TestRoleText_ListsAllEnums(t *testing.T) {
	enums := [][]string{
		DisplayPhases, DisplayWeathers, DisplaySpeeds,
		DisplayEntityKinds, DisplayLayers, DisplayFilterModes,
		DisplayCameraPresets,
	}
	for _, e := range enums {
		for _, v := range e {
			if !strings.Contains(roleText, v) {
				t.Fatalf("roleText missing enum value %q", v)
			}
		}
	}
	// 相机模式枚举也要在提示词里。
	for _, v := range DisplayModes {
		if !strings.Contains(roleText, v) {
			t.Fatalf("roleText missing mode value %q", v)
		}
	}
}

// TestRoleText_DisciplineAnchors 纪律锚点:条数上限 + 禁止臆造实体/图层。
func TestRoleText_DisciplineAnchors(t *testing.T) {
	anchors := []string{"≤3 条指令", "≤5 条指令", "entity_id", "不编造实体与图层"}
	for _, a := range anchors {
		if !strings.Contains(roleText, a) {
			t.Fatalf("roleText missing discipline anchor %q", a)
		}
	}
}

// TestStructuredHint_Batch84 structuredHint 必须列出全部新参数。
func TestStructuredHint_Batch84(t *testing.T) {
	hour := 12.0
	visible := true
	x, z := 3.0, 4.0
	req := DisplayRequest{
		Text: "切夜", District: "d1", Mode: "fly", X: &x, Z: &z,
		Phase: "night", Hour: &hour, Weather: "rain", Speed: "fast",
		EntityID: "b-1", Kind: "building", Layer: "trees", Visible: &visible,
		Category: "商业", Preset: "bird",
		Points: []DisplayPoint{{X: 0, Z: 0}, {X: 1, Z: 1}},
	}
	hint := structuredHint(req)
	for _, want := range []string{
		"district=d1", "point=(", "mode=fly", "phase=night", "hour=12",
		"weather=rain", "speed=fast", "entity_id=b-1", "kind=building",
		"layer=trees", "visible=true", "category=商业", "preset=bird", "points=",
	} {
		if !strings.Contains(hint, want) {
			t.Fatalf("structuredHint missing %q:\n%s", want, hint)
		}
	}
	// 全空 → 空串(不产生空提示块)。
	if got := structuredHint(DisplayRequest{}); got != "" {
		t.Fatalf("empty request hint = %q, want empty", got)
	}
}

// TestUserPrompt_Batch84 user 消息含请求文本 + 新参数提示行。
func TestUserPrompt_Batch84(t *testing.T) {
	s := userPrompt(DisplayRequest{Text: "下雨了", Weather: "rain"})
	if !strings.Contains(s, "下雨了") || !strings.Contains(s, "weather=rain") {
		t.Fatalf("userPrompt = %q", s)
	}
}
