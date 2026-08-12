// Package wwplayer — wolfpack_assign_tools_test.go: §20260810-10 U1
// wolfpack_assign 工具注册 + 分工 prompt 段渲染单测。
package wwplayer

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwtypes"
)

// TestWolfpackAssign_Registration 验证工具已注册且 schema 合法。
func TestWolfpackAssign_Registration(t *testing.T) {
	spec := FindTool("wolfpack_assign")
	if spec == nil {
		t.Fatal("wolfpack_assign not registered")
	}
	schema := spec.Builder(nil)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing properties: %+v", schema)
	}
	roleProp, ok := props["role"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing role property: %+v", props)
	}
	enumVals, ok := roleProp["enum"].([]string)
	if !ok || len(enumVals) != 4 {
		t.Fatalf("role enum should have 4 values, got: %+v", roleProp["enum"])
	}
}

// TestWolfpackAssign_MountGating 验证仅轮值狼王可见。
func TestWolfpackAssign_MountGating(t *testing.T) {
	spec := FindTool("wolfpack_assign")
	if spec == nil || spec.MountIf == nil {
		t.Fatal("wolfpack_assign MountIf missing")
	}
	// 非狼 → 不挂载。
	if spec.MountIf(&wwtypes.GameContext{Faction: "good", MySeat: 2, WolfKingSeat: 2}) {
		t.Fatal("good faction should not mount wolfpack_assign")
	}
	// 狼但非狼王 → 不挂载。
	if spec.MountIf(&wwtypes.GameContext{Faction: "wolf", MySeat: 5, WolfKingSeat: 2}) {
		t.Fatal("non-king wolf should not mount wolfpack_assign")
	}
	// 狼王 → 挂载。
	if !spec.MountIf(&wwtypes.GameContext{Faction: "wolf", MySeat: 2, WolfKingSeat: 2}) {
		t.Fatal("wolf king should mount wolfpack_assign")
	}
	// 无狼王(-1) → 不挂载。
	if spec.MountIf(&wwtypes.GameContext{Faction: "wolf", MySeat: 2, WolfKingSeat: -1}) {
		t.Fatal("no king should not mount wolfpack_assign")
	}
}

// TestWolfpackAssign_Phases 验证 speak + night 双阶段挂载(与 wolf_whisper 一致)。
func TestWolfpackAssign_Phases(t *testing.T) {
	spec := FindTool("wolfpack_assign")
	gc := &wwtypes.GameContext{Faction: "wolf", MySeat: 2, WolfKingSeat: 2}
	if !specMatchesPhase(spec, ToolPhaseSpeak) || !specMatchesPhase(spec, ToolPhaseNight) {
		t.Fatal("wolfpack_assign should mount in speak + night phases")
	}
	if specMatchesPhase(spec, ToolPhaseVote) {
		t.Fatal("wolfpack_assign should NOT mount in vote phase")
	}
	// MountTools 端到端:狼王在 speak 阶段应能看到。
	mounted := MountTools(ToolPhaseSpeak, gc)
	found := false
	for _, s := range mounted {
		if s.Name == "wolfpack_assign" {
			found = true
		}
	}
	if !found {
		t.Fatal("wolfpack_assign should be in MountTools(speak) for wolf king")
	}
}

// TestWolfPackPromptBlock_RoleSection 验证分工段渲染(仅狼 + 分工表非空)。
func TestWolfPackPromptBlock_RoleSection(t *testing.T) {
	gc := &wwtypes.GameContext{
		Faction:      "wolf",
		MySeat:       2,
		WolfKingSeat: 2,
		WolfPackRole: "hype",
		WolfPackRoleTable: map[int]string{
			2: "hype", 5: "charger", 8: "hook", 11: "deep",
		},
	}
	out := WolfPackPromptBlock(gc)
	for _, want := range []string{"狼队战术分工", "悍跳位", "冲锋位", "倒钩位", "深水位", "本轮狼王: 你(3号)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("role section missing %q: %s", want, out)
		}
	}
	// 座位按升序渲染(确定性)。
	idx3 := strings.Index(out, "3号=")
	idx6 := strings.Index(out, "6号=")
	idx9 := strings.Index(out, "9号=")
	idx12 := strings.Index(out, "12号=")
	if !(idx3 >= 0 && idx3 < idx6 && idx6 < idx9 && idx9 < idx12) {
		t.Fatalf("role table should be ordered by seat asc: %s", out)
	}
}

// TestWolfPackPromptBlock_NonWolfEmpty 非狼不渲染。
func TestWolfPackPromptBlock_NonWolfEmpty(t *testing.T) {
	gc := &wwtypes.GameContext{
		Faction:           "good",
		WolfPackRoleTable: map[int]string{2: "hype"},
	}
	if out := WolfPackPromptBlock(gc); out != "" {
		t.Fatalf("good faction should render empty, got: %s", out)
	}
}

// TestWolfPackPromptBlock_NonKingHint 非狼王狼看到的狼王提示。
func TestWolfPackPromptBlock_NonKingHint(t *testing.T) {
	gc := &wwtypes.GameContext{
		Faction:      "wolf",
		MySeat:       5,
		WolfKingSeat: 2,
		WolfPackRole: "charger",
		WolfPackRoleTable: map[int]string{
			2: "hype", 5: "charger",
		},
	}
	out := WolfPackPromptBlock(gc)
	if !strings.Contains(out, "本轮狼王: 3号") {
		t.Fatalf("non-king should see king seat hint, got: %s", out)
	}
	if strings.Contains(out, "wolfpack_assign 工具") {
		t.Fatalf("non-king should NOT see tool hint, got: %s", out)
	}
}
