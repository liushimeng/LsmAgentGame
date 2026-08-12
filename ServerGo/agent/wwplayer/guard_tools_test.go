// 2026-07-29 §134 — 守卫 (Guard) 工具 / Prompt / 过滤 helper 测试。
//
// 覆盖范围:
//  1. BuildTools:guard_protect 工具仅在 role=="guard" + phase==night_guard/PhaseNightGuard 时暴露;
//     其他角色 / 阶段不暴露。
//  2. enum 过滤:filterGuardTargets 剔除自己 + 上晚守护目标(GuardLastProtect),
//     并始终保留 -1(空守出口);gc==nil 时退化为只剔除自己。
//  3. SkipPhaseAction:night_guard/PhaseNightGuard → ("guard_protect_skip", 0)。
//  4. DispatchTool:guard_protect 派发到 runner.GuardProtect(target);
//     guard_protect_skip 派发到 runner.GuardProtect(-1)(空守)。
//  5. wwtypes.GameContext.GuardLastProtect 字段存在(-1 默认)。
package wwplayer_test

import (
	"testing"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
)

// toolByName returns the tool with the given name from a list, or nil.
func toolByName(tools []llm.ToolDef, name string) *llm.ToolDef {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

// enumHas reports whether the given integer value is present in []any enum.
func enumHas(enum []any, want int) bool {
	for _, v := range enum {
		switch n := v.(type) {
		case float64:
			if int(n) == want {
				return true
			}
		case int:
			if n == want {
				return true
			}
		}
	}
	return false
}

// TestBuildTools_GuardProtect_OnlyGuardAndNightGuard §134
//   - role=="guard" + phase=="night_guard" → guard_protect 暴露
//   - 其他角色 / 阶段 → guard_protect 不暴露
func TestBuildTools_GuardProtect_OnlyGuardAndNightGuard(t *testing.T) {
	aliveAll := []int{0, 1, 2, 3, 4, 5, 6}
	// 1) guard + night_guard → guard_protect 暴露
	tools := wwplayer.BuildTools("night_guard", "guard", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") == nil {
		t.Errorf("guard + night_guard: guard_protect tool should be exposed, got %+v", toolNames(tools))
	}
	// 2) guard + night_wolves (非 night_guard) → guard_protect 不暴露
	tools = wwplayer.BuildTools("night_wolves", "guard", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") != nil {
		t.Errorf("guard + night_wolves: guard_protect should NOT be exposed, got %+v", toolNames(tools))
	}
	// 3) werewolf + night_guard → guard_protect 不暴露
	tools = wwplayer.BuildTools("night_guard", "werewolf", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") != nil {
		t.Errorf("werewolf + night_guard: guard_protect should NOT be exposed, got %+v", toolNames(tools))
	}
	// 4) seer + night_guard → guard_protect 不暴露
	tools = wwplayer.BuildTools("night_guard", "seer", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") != nil {
		t.Errorf("seer + night_guard: guard_protect should NOT be exposed, got %+v", toolNames(tools))
	}
	// 5) witch + night_guard → guard_protect 不暴露
	tools = wwplayer.BuildTools("night_guard", "witch", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") != nil {
		t.Errorf("witch + night_guard: guard_protect should NOT be exposed, got %+v", toolNames(tools))
	}
	// 6) villager + night_guard → guard_protect 不暴露
	tools = wwplayer.BuildTools("night_guard", "villager", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") != nil {
		t.Errorf("villager + night_guard: guard_protect should NOT be exposed, got %+v", toolNames(tools))
	}
	// 7) canonical spelling PhaseNightGuard
	tools = wwplayer.BuildTools("PhaseNightGuard", "guard", 0, aliveAll, -1, nil)
	if toolByName(tools, "guard_protect") == nil {
		t.Errorf("guard + PhaseNightGuard: guard_protect should be exposed, got %+v", toolNames(tools))
	}
}

// TestBuildTools_GuardProtect_EnumFiltering §134
//   - enum 剔除自己(seat) + 上晚守护目标(GuardLastProtect) + 始终保留 -1
//   - gc 包含 GuardLastProtect=4 时,4 必须从 enum 剔除
func TestBuildTools_GuardProtect_EnumFiltering(t *testing.T) {
	aliveAll := []int{0, 1, 2, 3, 4, 5, 6}
	// seat=0, GuardLastProtect=4 → enum 应剔除 0 和 4,保留 1,2,3,5,6 + -1
	gc := &wwtypes.GameContext{GuardLastProtect: 4}
	tools := wwplayer.BuildTools("night_guard", "guard", 0, aliveAll, -1, gc)
	tl := toolByName(tools, "guard_protect")
	if tl == nil {
		t.Fatalf("guard_protect tool missing")
	}
	props, ok := tl.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("guard_protect properties missing: %+v", tl.InputSchema)
	}
	targetSchema, ok := props["target"].(map[string]any)
	if !ok {
		t.Fatalf("guard_protect target schema missing: %+v", props)
	}
	enum, ok := targetSchema["enum"].([]any)
	if !ok {
		t.Fatalf("guard_protect target enum missing or wrong type: %T %v", targetSchema["enum"], targetSchema["enum"])
	}
	if enumHas(enum, 0) {
		t.Errorf("enum should NOT include self (seat=0); got %v", enum)
	}
	if enumHas(enum, 4) {
		t.Errorf("enum should NOT include GuardLastProtect=4; got %v", enum)
	}
	if !enumHas(enum, -1) {
		t.Errorf("enum should always include -1 (空守 sentinel); got %v", enum)
	}
	for _, must := range []int{1, 2, 3, 5, 6} {
		if !enumHas(enum, must) {
			t.Errorf("enum should include alive seat %d; got %v", must, enum)
		}
	}
	// 关键:guard_protect 也应该带 emotion_switch_speak(夜间阶段均暴露)。
	// 2026-08-04 §重构 — emotion_switch 旧名删除,合并工具是 emotion_switch_speak。
	if toolByName(tools, "emotion_switch_speak") == nil {
		t.Errorf("emotion_switch_speak should be exposed in night_guard phase")
	}
}

// TestBuildTools_GuardProtect_EnumFilterNilGC §134
//   - gc=nil 时退化为只剔除自己,保留 -1
func TestBuildTools_GuardProtect_EnumFilterNilGC(t *testing.T) {
	aliveAll := []int{0, 1, 2, 3}
	tools := wwplayer.BuildTools("night_guard", "guard", 0, aliveAll, -1, nil)
	tl := toolByName(tools, "guard_protect")
	if tl == nil {
		t.Fatalf("guard_protect tool missing")
	}
	props := tl.InputSchema["properties"].(map[string]any)
	targetSchema := props["target"].(map[string]any)
	enum := targetSchema["enum"].([]any)
	if enumHas(enum, 0) {
		t.Errorf("nil gc: enum should still exclude self (seat=0); got %v", enum)
	}
	if !enumHas(enum, -1) {
		t.Errorf("nil gc: enum should include -1; got %v", enum)
	}
	// other alive seats should be present
	for _, must := range []int{1, 2, 3} {
		if !enumHas(enum, must) {
			t.Errorf("nil gc: enum should include alive seat %d; got %v", must, enum)
		}
	}
}

// TestBuildTools_GuardProtect_EnumFilterLastProtectIsSelf §134
//   - 极端 case:GuardLastProtect 与 seat 同时恰好命中 — 仍只剔除一次,enum 正确
func TestBuildTools_GuardProtect_EnumFilterLastProtectIsSelf(t *testing.T) {
	// seat=2, GuardLastProtect=2 → enum 剔除 2 (一次),保留 0,1,3,4,5,6 + -1
	gc := &wwtypes.GameContext{GuardLastProtect: 2}
	aliveAll := []int{0, 1, 2, 3, 4, 5, 6}
	tools := wwplayer.BuildTools("night_guard", "guard", 2, aliveAll, -1, gc)
	tl := toolByName(tools, "guard_protect")
	if tl == nil {
		t.Fatalf("guard_protect tool missing")
	}
	enum := tl.InputSchema["properties"].(map[string]any)["target"].(map[string]any)["enum"].([]any)
	if enumHas(enum, 2) {
		t.Errorf("enum should NOT include self (seat=2) which equals GuardLastProtect=2; got %v", enum)
	}
	if !enumHas(enum, -1) {
		t.Errorf("enum should include -1; got %v", enum)
	}
}

// TestSkipPhaseAction_NightGuard §134
//   - SkipPhaseAction("night_guard") → ("guard_protect_skip", 0)
//   - SkipPhaseAction("PhaseNightGuard") → ("guard_protect_skip", 0)
func TestSkipPhaseAction_NightGuard(t *testing.T) {
	cases := []struct {
		phase    string
		wantName string
		wantArg  int
	}{
		{"night_guard", "guard_protect_skip", 0},
		{"PhaseNightGuard", "guard_protect_skip", 0},
	}
	for _, c := range cases {
		name, arg := wwplayer.SkipPhaseAction(c.phase, "guard")
		if name != c.wantName || arg != c.wantArg {
			t.Errorf("SkipPhaseAction(%q, guard) = (%q, %d), want (%q, %d)",
				c.phase, name, arg, c.wantName, c.wantArg)
		}
	}
}

// TestDispatchTool_GuardProtect §134
//   - guard_protect 派发到 runner.GuardProtect(target)
//   - guard_protect_skip 派发到 runner.GuardProtect(-1)(空守)
//
// 关键工程约束:guard_protect_skip 在 agent 路径和 manager 路径必须映射到
// 完全相同的引擎调用(§89/§92b)。fakeRunner 记录 "guard_protect" 调用,
// 我们只能断言到"guard_protect 被调用";真实 / -1 校验由 engine 单元测试覆盖。
func TestDispatchTool_GuardProtect(t *testing.T) {
	// 1) guard_protect 带 target=3
	f := &fakeRunner{}
	res, err := wwplayer.DispatchTool("guard_protect", map[string]any{"target": float64(3)}, f)
	if err != nil {
		t.Fatalf("guard_protect dispatch error: %v", err)
	}
	if res != "ok" {
		t.Errorf("guard_protect res = %q, want ok", res)
	}
	if len(f.calls) != 1 || f.calls[0] != "guard_protect" {
		t.Errorf("guard_protect calls = %v, want [guard_protect]", f.calls)
	}

	// 2) guard_protect_skip 不带参数(走空守)
	f = &fakeRunner{}
	res, err = wwplayer.DispatchTool("guard_protect_skip", map[string]any{}, f)
	if err != nil {
		t.Fatalf("guard_protect_skip dispatch error: %v", err)
	}
	if res != "ok" {
		t.Errorf("guard_protect_skip res = %q, want ok", res)
	}
	if len(f.calls) != 1 || f.calls[0] != "guard_protect" {
		t.Errorf("guard_protect_skip calls = %v, want [guard_protect] (空守映射到相同方法)", f.calls)
	}
}

// TestGameContext_GuardLastProtectField §134
//   - wwtypes.GameContext 新增 GuardLastProtect 字段,int 零值 0
//   - 可被构造方正常写入
//   - "默认 -1" 是业务语义(wwtypes.GameContext 填充方负责把未守护的座位写成 -1),
//     不是 Go 零值;filterGuardTargets 在读到这个字段时若为 0 也会按"非上晚
//     守护目标"处理(0 == seat 时才被剔除,与 self 移除去重)。
func TestGameContext_GuardLastProtectField(t *testing.T) {
	// int 零值 = 0
	gc := &wwtypes.GameContext{}
	if gc.GuardLastProtect != 0 {
		t.Errorf("default GuardLastProtect int zero-value should be 0; got %d", gc.GuardLastProtect)
	}
	// 可写入
	gc.GuardLastProtect = 5
	if gc.GuardLastProtect != 5 {
		t.Errorf("GuardLastProtect = 5; got %d", gc.GuardLastProtect)
	}
}
