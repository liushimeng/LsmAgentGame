// Package agent — tools_registry_test.go: ToolRegistry v5 测试。
//
// 覆盖：
//  1. RegisterTool 覆盖语义
//  2. MountTools 按 phase 过滤 + MountIf 谓词
//  3. DispatchToolByName 派发表
//  4. BuildAnthropicToolDefs wire 字段顺序
//  5. mountFromRegistry 自动拼装 add(name, desc, schema)
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"encoding/json"
	"strings"
	"testing"
)

// TestRegisterTool_OverridesByName 验证同名重复注册覆盖旧条目。
func TestRegisterTool_OverridesByName(t *testing.T) {
	// 备份并恢复(避免污染其他测试的 registry 状态)。
	orig := AllRegistered()
	defer func() {
		UnregisterAll()
		for _, s := range orig {
			RegisterTool(s)
		}
	}()

	called1 := false
	called2 := false
	RegisterTool(&ToolSpec{
		Name:     "test_tool",
		Phase:    ToolPhaseSpeak,
		Category: "test",
		Builder:  func(_ *wwtypes.GameContext) map[string]any { return nil },
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			called1 = true
			return "v1", nil
		},
	})
	RegisterTool(&ToolSpec{
		Name:     "test_tool",
		Phase:    ToolPhaseSpeak,
		Category: "test",
		Builder:  func(_ *wwtypes.GameContext) map[string]any { return nil },
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			called2 = true
			return "v2", nil
		},
	})

	d, ok := DispatchToolByName("test_tool")
	if !ok {
		t.Fatal("DispatchToolByName should find test_tool")
	}
	_, _ = d(nil, nil, nil)
	if called1 {
		t.Fatal("old dispatcher should be replaced")
	}
	if !called2 {
		t.Fatal("new dispatcher should be invoked")
	}
}

// TestMountTools_PhaseFilter 验证 MountTools 按 phase 过滤。
func TestMountTools_PhaseFilter(t *testing.T) {
	orig := AllRegistered()
	defer func() {
		UnregisterAll()
		for _, s := range orig {
			RegisterTool(s)
		}
	}()

	RegisterTool(&ToolSpec{
		Name:     "speak_only",
		Phase:    ToolPhaseSpeak,
		Builder:  func(_ *wwtypes.GameContext) map[string]any { return nil },
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			return "speak_only", nil
		},
	})
	RegisterTool(&ToolSpec{
		Name:     "night_only",
		Phase:    ToolPhaseNight,
		Builder:  func(_ *wwtypes.GameContext) map[string]any { return nil },
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			return "night_only", nil
		},
	})
	RegisterTool(&ToolSpec{
		Name:     "any_phase",
		Phase:    ToolPhaseAny,
		Builder:  func(_ *wwtypes.GameContext) map[string]any { return nil },
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			return "any_phase", nil
		},
	})

	speakTools := MountTools(ToolPhaseSpeak, nil)
	nightTools := MountTools(ToolPhaseNight, nil)
	voteTools := MountTools(ToolPhaseVote, nil)

	if !containsName(speakTools, "speak_only") || !containsName(speakTools, "any_phase") {
		t.Fatalf("speak mount should include speak_only + any_phase, got %v", namesOf(speakTools))
	}
	if containsName(speakTools, "night_only") {
		t.Fatalf("speak mount should exclude night_only, got %v", namesOf(speakTools))
	}
	if !containsName(nightTools, "night_only") || !containsName(nightTools, "any_phase") {
		t.Fatalf("night mount should include night_only + any_phase, got %v", namesOf(nightTools))
	}
	if !containsName(voteTools, "any_phase") {
		t.Fatalf("vote mount should include any_phase, got %v", namesOf(voteTools))
	}
}

// TestMountTools_MountIfFilter 验证 MountIf 谓词拒绝时不挂载。
func TestMountTools_MountIfFilter(t *testing.T) {
	orig := AllRegistered()
	defer func() {
		UnregisterAll()
		for _, s := range orig {
			RegisterTool(s)
		}
	}()

	RegisterTool(&ToolSpec{
		Name:     "wolf_only",
		Phase:    ToolPhaseSpeak,
		MountIf:  func(gc *wwtypes.GameContext) bool { return gc != nil && gc.Faction == "wolf" },
		Builder:  func(_ *wwtypes.GameContext) map[string]any { return nil },
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			return "wolf_only", nil
		},
	})

	// good faction → MountIf 拒绝 → 不挂载
	good := MountTools(ToolPhaseSpeak, &wwtypes.GameContext{Faction: "good"})
	if containsName(good, "wolf_only") {
		t.Fatalf("wolf_only should not mount for good faction")
	}
	// wolf faction → MountIf 接受 → 挂载
	wolf := MountTools(ToolPhaseSpeak, &wwtypes.GameContext{Faction: "wolf"})
	if !containsName(wolf, "wolf_only") {
		t.Fatalf("wolf_only should mount for wolf faction")
	}
	// nil gc → MountIf nil-safe(跳过谓词),默认挂载。
	// 这是设计决策:BuildTools 阶段若 gc 未填充,MountIf 被跳过,工具按
	// Phase 过滤可见性单独决定。验证 wolf_only 在 nil gc 下挂载。
	nilGC := MountTools(ToolPhaseSpeak, nil)
	if !containsName(nilGC, "wolf_only") {
		t.Fatalf("nil gc should mount (MountIf nil-safe:跳过谓词,按 Phase 过滤)")
	}
}

// TestDispatchToolByName_NotFound 验证未注册工具返回 (nil, false)。
func TestDispatchToolByName_NotFound(t *testing.T) {
	d, ok := DispatchToolByName("not_registered_tool_xyz")
	if ok || d != nil {
		t.Fatalf("not registered tool should return (nil, false), got (%T, %v)", d, ok)
	}
}

// TestBuildAnthropicToolDefs_FieldOrder 验证 wire 序列化字段顺序固定为
// name → description → input_schema(CLAUDE.md §14.1 强约束)。
//
// 序列化为 raw JSON bytes,断言字段 key 顺序。
func TestBuildAnthropicToolDefs_FieldOrder(t *testing.T) {
	raw, err := json.Marshal(BuildAnthropicToolDefs([]*ToolSpec{
		{
			Name:        "test_tool",
			Description: "test desc",
			Phase:       ToolPhaseSpeak,
			Builder: func(_ *wwtypes.GameContext) map[string]any {
				return map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				}
			},
			Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
				return "", nil
			},
		},
	}))
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	s := string(raw)
	nameIdx := strings.Index(s, `"name"`)
	descIdx := strings.Index(s, `"description"`)
	schemaIdx := strings.Index(s, `"input_schema"`)
	if nameIdx < 0 || descIdx < 0 || schemaIdx < 0 {
		t.Fatalf("missing required fields, got: %s", s)
	}
	if !(nameIdx < descIdx && descIdx < schemaIdx) {
		t.Fatalf("field order must be name < description < input_schema, got: %s", s)
	}
}

// TestMountFromRegistry_CallsAdd 验证 mountFromRegistry 装配工具时调用 add 闭包,
// 并能识别新注册的工具(与既有 prop/wolf 工具共存)。
func TestMountFromRegistry_CallsAdd(t *testing.T) {
	orig := AllRegistered()
	defer func() {
		UnregisterAll()
		for _, s := range orig {
			RegisterTool(s)
		}
	}()

	RegisterTool(&ToolSpec{
		Name:        "test_mount_tool",
		Description: "test mount desc",
		Phase:       ToolPhaseSpeak,
		Builder: func(_ *wwtypes.GameContext) map[string]any {
			return map[string]any{"type": "object", "properties": map[string]any{}}
		},
		Dispatcher: func(_ map[string]any, _ *wwtypes.GameContext, _ ToolRunner) (string, error) {
			return "", nil
		},
	})

	mounted := []string{}
	descs := []string{}
	add := func(name, desc string, _ map[string]any) {
		mounted = append(mounted, name)
		descs = append(descs, desc)
	}
	mountFromRegistry(add, ToolPhaseSpeak, nil)
	// v5 设计:registry 装配会同时挂载 prop/wolf 等所有 PhaseSpeak 工具 +
	// 本测试新加的 test_mount_tool。验证 test_mount_tool 在列表中,以及
	// desc 字段正确。
	if !contains(mounted, "test_mount_tool") {
		t.Fatalf("mountFromRegistry should include test_mount_tool, got %v", mounted)
	}
	// 验证新增工具的 desc 是测试预期值(其他工具的 desc 由各自 Builder 保证,
	// 这里不强制断言)。
	idx := indexOfStr(mounted, "test_mount_tool")
	if idx < 0 || descs[idx] != "test mount desc" {
		t.Fatalf("desc at test_mount_tool position should be 'test mount desc', got %v", descs)
	}
}

// helpers (continued)
func contains(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}

func indexOfStr(arr []string, s string) int {
	for i, x := range arr {
		if x == s {
			return i
		}
	}
	return -1
}

// helpers
func containsName(specs []*ToolSpec, name string) bool {
	for _, s := range specs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func namesOf(specs []*ToolSpec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}