// Package citysystem — dispatch_display_test.go: apply_display 类别筛选
// (批次85 E2E 缺口 C7)单测。
//
// 覆盖:显式空 category = 取消筛选(本侧直产 set_filter 空 category 指令,
// 不再报「至少给出一个显示参数」)/ 空 category 与其它参数组合 / 无参数仍
// 拒绝(零回归)/ 非空 category 仍经总线正常产出(零回归)/ buildDisplayRequest
// 的 category 存在性判据(键存在 vs 值非空)。
package citysystem

import (
	"context"
	"strings"
	"sync"
	"testing"

	"LsmAgentGame/agent/citybus"
)

// displayFixture 是 apply_display 的夹具:真实房间 + 记录总线转发的渲染
// Agent 假件(返回固定指令序列;nil = 空返回)。
type displayFixture struct {
	agent  *Agent
	events *[]capturedEvent
	mu     sync.Mutex
	busReq []displayRequest
}

func newDisplayFixture(t *testing.T, ret []displayCommand) *displayFixture {
	t.Helper()
	room := newTestRoom(t, "R-disp")
	a, events := newTestAgent(t, room, &fakeRegistry{p: &fakeProvider{}})
	f := &displayFixture{agent: a, events: events}
	if err := a.hub.RegisterRoomWith(room.RoomID, agentClassCityWebGLShow,
		[]string{"run_display"}, "渲染显示 Agent",
		func(_ context.Context, msg citybus.Message) (any, error) {
			req, ok := msg.Payload.(displayRequest)
			if !ok {
				return []displayCommand{}, nil
			}
			f.mu.Lock()
			f.busReq = append(f.busReq, req)
			f.mu.Unlock()
			return ret, nil
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return f
}

// busCalls 返回总线转发记录快照。
func (f *displayFixture) busCalls() []displayRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]displayRequest(nil), f.busReq...)
}

// collectShowCommands 提取事件流里的 show_command 显示指令。
func collectShowCommands(events []capturedEvent) []displayCommand {
	out := []displayCommand{}
	for _, e := range events {
		if e.Kind != EventShowCommand {
			continue
		}
		if cmd, ok := e.Data.(displayCommand); ok {
			out = append(out, cmd)
		}
	}
	return out
}

// TestApplyDisplay_CategoryEmpty_CancelFilter 修 C7:apply_display{category:""}
// ⇒ 成功产出 set_filter 空 category 指令(取消筛选),不再报「至少给出一个
// 显示参数」。渲染侧 wire 表达不了「显式空 category」(omitempty 丢空值 +
// citywebgl.Structured 丢空 category),指令由本侧直产、经 emitCommands
// 下发,**不经过总线**。
func TestApplyDisplay_CategoryEmpty_CancelFilter(t *testing.T) {
	f := newDisplayFixture(t, nil)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolApplyDisplay,
		map[string]any{"category": ""})
	if isErr {
		t.Fatalf("category:\"\" must succeed (cancel filter), got %q", text)
	}
	if strings.Contains(text, "至少给出一个显示参数") {
		t.Fatalf("must not report missing params: %q", text)
	}
	if !strings.Contains(text, DisplaySetFilter) {
		t.Fatalf("text = %q, want set_filter", text)
	}
	if calls := f.busCalls(); len(calls) != 0 {
		t.Fatalf("cancel filter must not hit the bus, got %d calls: %+v", len(calls), calls)
	}
	cmds := collectShowCommands(collectEvents(f.events))
	if len(cmds) != 1 {
		t.Fatalf("cmds = %+v, want exactly 1 set_filter", cmds)
	}
	if cmds[0].Type != DisplaySetFilter || cmds[0].Category != "" {
		t.Fatalf("cmd = %+v, want set_filter with empty category", cmds[0])
	}
	if cmds[0].Mode != DisplayFilterModes[0] {
		t.Fatalf("cmd mode = %q, want %q", cmds[0].Mode, DisplayFilterModes[0])
	}
}

// TestApplyDisplay_CategoryEmptyCombined 空 category + 其它参数组合:
// 取消筛选指令本侧直产,其余参数照常经总线翻译,合并下发。
func TestApplyDisplay_CategoryEmptyCombined(t *testing.T) {
	f := newDisplayFixture(t, []displayCommand{{Type: DisplaySetWeather, Weather: "rain"}})
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolApplyDisplay,
		map[string]any{"category": "", "weather": "rain"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	calls := f.busCalls()
	if len(calls) != 1 || calls[0].Weather != "rain" {
		t.Fatalf("bus calls = %+v, want 1 call with weather=rain", calls)
	}
	cmds := collectShowCommands(collectEvents(f.events))
	if len(cmds) != 2 {
		t.Fatalf("cmds = %+v, want set_filter + set_weather", cmds)
	}
	if cmds[0].Type != DisplaySetFilter || cmds[0].Category != "" {
		t.Fatalf("cmd0 = %+v, want cancel set_filter", cmds[0])
	}
	if cmds[1].Type != DisplaySetWeather || cmds[1].Weather != "rain" {
		t.Fatalf("cmd1 = %+v, want set_weather", cmds[1])
	}
	if !strings.Contains(text, DisplaySetFilter) || !strings.Contains(text, DisplaySetWeather) {
		t.Fatalf("text = %q, want both command types", text)
	}
}

// TestApplyDisplay_NoParams_StillFails 零回归:不带任何参数仍报
// 「至少给出一个显示参数」,不转发、不产指令。
func TestApplyDisplay_NoParams_StillFails(t *testing.T) {
	f := newDisplayFixture(t, []displayCommand{{Type: DisplaySetWeather, Weather: "rain"}})
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolApplyDisplay,
		map[string]any{})
	if !isErr {
		t.Fatalf("expected error, got %q", text)
	}
	if !strings.Contains(text, "至少给出一个显示参数") {
		t.Fatalf("text = %q, want 至少给出一个显示参数", text)
	}
	if calls := f.busCalls(); len(calls) != 0 {
		t.Fatalf("must not forward empty request, got %+v", calls)
	}
	if cmds := collectShowCommands(collectEvents(f.events)); len(cmds) != 0 {
		t.Fatalf("must not emit commands, got %+v", cmds)
	}
}

// TestApplyDisplay_CategoryNonEmpty_StillForwarded 零回归:非空 category
// 照常经总线转发(渲染 Agent 产出 set_filter),不受 C7 改动影响。
func TestApplyDisplay_CategoryNonEmpty_StillForwarded(t *testing.T) {
	f := newDisplayFixture(t, []displayCommand{{Type: DisplaySetFilter, Category: "park", Mode: "highlight"}})
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolApplyDisplay,
		map[string]any{"category": "park"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	calls := f.busCalls()
	if len(calls) != 1 || calls[0].Category != "park" {
		t.Fatalf("bus calls = %+v, want 1 call with category=park", calls)
	}
	cmds := collectShowCommands(collectEvents(f.events))
	if len(cmds) != 1 || cmds[0].Category != "park" {
		t.Fatalf("cmds = %+v, want set_filter park", cmds)
	}
	if !strings.Contains(text, DisplaySetFilter) {
		t.Fatalf("text = %q, want set_filter", text)
	}
}

// TestBuildDisplayRequest_CategoryPresence category 的「已给出」判据 =
// 键是否存在(逗号 ok 形式),而非取值是否非空:「没传键」是缺参,
// 「传了空值」是取消筛选(JSON null 同取消语义)。
func TestBuildDisplayRequest_CategoryPresence(t *testing.T) {
	cases := []struct {
		name       string
		input      map[string]any
		wantCancel bool
		wantCat    string
		wantErr    string
	}{
		{"empty string = cancel", map[string]any{"category": ""}, true, "", ""},
		{"whitespace = cancel", map[string]any{"category": "   "}, true, "", ""},
		{"json null = cancel", map[string]any{"category": nil}, true, "", ""},
		{"non-empty = filter", map[string]any{"category": "park"}, false, "park", ""},
		{"absent key = missing", map[string]any{}, false, "", "至少给出一个显示参数"},
		{"non-string rejected", map[string]any{"category": 123}, false, "", "category 必须是字符串"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, cancel, err := buildDisplayRequest(tc.input)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cancel != tc.wantCancel {
				t.Errorf("cancelFilter = %v, want %v", cancel, tc.wantCancel)
			}
			if req.Category != tc.wantCat {
				t.Errorf("category = %q, want %q", req.Category, tc.wantCat)
			}
		})
	}
}
