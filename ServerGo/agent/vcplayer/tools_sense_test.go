// Package vcplayer — tools_sense_test.go: City-Human 感知与行动工具单测
// (2026-09-22 §CityHuman重构)。
//
// 覆盖:4 个新工具 wire 定义齐备 + 派发路由(see/hear/smell/move) +
// 感知每月限次(35101) + move 参数透传 + speak scope=private 路由 SpeakTo。
package vcplayer

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSenseTools_Present 感知与行动工具已并入 BuildTools / ToolNames(§130 接线)。
func TestSenseTools_Present(t *testing.T) {
	tools := BuildTools()
	want := map[string]bool{ToolSee: false, ToolHear: false, ToolSmell: false, ToolMove: false}
	for _, td := range tools {
		if _, ok := want[td.Name]; ok {
			want[td.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("BuildTools missing sense tool %q", name)
		}
	}
	names := ToolNames()
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for name := range want {
		if !nameSet[name] {
			t.Errorf("ToolNames missing sense tool %q", name)
		}
	}
	// move 工具 mode 必填。
	for _, td := range tools {
		if td.Name != ToolMove {
			continue
		}
		req, _ := td.InputSchema["required"].([]string)
		ok := false
		for _, r := range req {
			if r == "mode" {
				ok = true
			}
		}
		if !ok {
			t.Error("move tool must require mode")
		}
	}
}

// newSenseAgent 构造绑定 fake runner 的 Agent(感知测试夹具)。
func newSenseAgent(f *fakeTradeRunner) *Agent {
	a := NewAgent("r1", "u1", "", "", 0, 3, 0)
	a.BindRunner(f)
	a.senseUsed = map[string]int{}
	return a
}

// TestDispatchSenseTools_SeeHearSmell 感知三件套派发路由 + 结果 JSON 序列化。
func TestDispatchSenseTools_SeeHearSmell(t *testing.T) {
	f := &fakeTradeRunner{}
	a := newSenseAgent(f)
	for _, name := range []string{ToolSee, ToolHear, ToolSmell} {
		res := a.DispatchTool(name, map[string]any{})
		if res.IsErr {
			t.Fatalf("%s dispatch failed: %s", name, res.Text)
		}
		if f.lastTool != name || f.lastSeat != 0 {
			t.Errorf("%s: lastTool=%q lastSeat=%d", name, f.lastTool, f.lastSeat)
		}
		if !strings.Contains(res.Text, `"district"`) {
			t.Errorf("%s: result not SenseResult JSON: %s", name, res.Text)
		}
	}
}

// TestDispatchSenseTools_MonthlyLimit 感知每月限 2 次,第 3 次返回 35101。
func TestDispatchSenseTools_MonthlyLimit(t *testing.T) {
	f := &fakeTradeRunner{}
	a := newSenseAgent(f)
	for i := 0; i < senseMonthlyLimit; i++ {
		if res := a.DispatchTool(ToolSee, map[string]any{}); res.IsErr {
			t.Fatalf("see #%d unexpected error: %s", i+1, res.Text)
		}
	}
	res := a.DispatchTool(ToolSee, map[string]any{})
	if !res.IsErr {
		t.Fatal("3rd see in one month must be rejected")
	}
	if !strings.Contains(res.Text, "35101") {
		t.Errorf("limit error must carry 35101, got %q", res.Text)
	}
	// 限次按工具独立计数:hear 仍可用。
	if res := a.DispatchTool(ToolHear, map[string]any{}); res.IsErr {
		t.Errorf("hear should be independent of see limit: %s", res.Text)
	}
	// 月初重置(OnMonthStart 行为):清零后可用。
	a.senseUsed = map[string]int{}
	if res := a.DispatchTool(ToolSee, map[string]any{}); res.IsErr {
		t.Errorf("see after monthly reset failed: %s", res.Text)
	}
}

// TestDispatchSenseTool_Move move 参数透传(destination + mode)。
func TestDispatchSenseTool_Move(t *testing.T) {
	f := &fakeTradeRunner{}
	a := newSenseAgent(f)
	res := a.DispatchTool(ToolMove, map[string]any{"destination": "riverside", "mode": "taxi"})
	if res.IsErr {
		t.Fatalf("move dispatch failed: %s", res.Text)
	}
	if f.moveDestination != "riverside" || f.moveMode != "taxi" {
		t.Errorf("move args: got (%q,%q), want (riverside,taxi)", f.moveDestination, f.moveMode)
	}
	// 区内步行。
	res = a.DispatchTool(ToolMove, map[string]any{"mode": "walk"})
	if res.IsErr {
		t.Fatalf("walk dispatch failed: %s", res.Text)
	}
	if f.moveMode != "walk" || f.moveDestination != "" {
		t.Errorf("walk args: got (%q,%q)", f.moveDestination, f.moveMode)
	}
}

// TestDispatchSpeak_Private speak scope=private 路由到 SpeakTo。
func TestDispatchSpeak_Private(t *testing.T) {
	f := &fakeTradeRunner{}
	a := newSenseAgent(f)
	res := a.DispatchTool(ToolSpeak, map[string]any{
		"scope": "private", "target_seat": 5, "text": "晚上河边见",
	})
	if res.IsErr {
		t.Fatalf("private speak failed: %s", res.Text)
	}
	if f.whisperTarget != 5 || f.whisperText != "晚上河边见" {
		t.Errorf("whisper args: got (%d,%q)", f.whisperTarget, f.whisperText)
	}
	// area(默认)仍路由公屏 Speak(lastTool 记录为 speak 由 fake 决定;
	// 此处断言不报错即可)。
	res = a.DispatchTool(ToolSpeak, map[string]any{"text": "大家好"})
	if res.IsErr {
		t.Fatalf("area speak failed: %s", res.Text)
	}
}

// TestMoveDistrictAlias move_district 兼容别名改写为 move+bus(设计 §4.2)。
func TestMoveDistrictAlias(t *testing.T) {
	f := &fakeTradeRunner{}
	a := newSenseAgent(f)
	res := a.DispatchTool(ToolMoveDistrict, map[string]any{"district": "suburb"})
	if res.IsErr {
		t.Fatalf("move_district alias failed: %s", res.Text)
	}
	if f.lastTool != ToolMove || f.moveDestination != "suburb" || f.moveMode != "bus" {
		t.Errorf("alias rewrite: got tool=%q dest=%q mode=%q, want move/suburb/bus",
			f.lastTool, f.moveDestination, f.moveMode)
	}
}

// TestSenseResultJSONShape SenseResult JSON 键名与设计契约 §4.3 一致。
func TestSenseResultJSONShape(t *testing.T) {
	f := &fakeTradeRunner{}
	a := newSenseAgent(f)
	res := a.DispatchTool(ToolSee, map[string]any{})
	var m map[string]any
	if err := json.Unmarshal([]byte(res.Text), &m); err != nil {
		t.Fatalf("sense result not JSON: %v", err)
	}
	if m["district"] != "finance" {
		t.Errorf("district key: got %v", m["district"])
	}
}
