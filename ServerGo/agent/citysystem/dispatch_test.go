// Package citysystem — dispatch_test.go: 11 个工具派发单测
// (2026-11-03 §批次81)。
//
// 每个工具至少 1 正 1 负:正例走真实房间 API 或已注册的总线目标;
// 负例覆盖未知 kind / 非法参数 / 非房主 / 未注册 Agent / nil room 等边界。
package citysystem

import (
	"context"
	"strings"
	"testing"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/game/virtual_city"
)

// dispatchFixture 是派发测试夹具(真实房间 + 已注册总线目标)。
type dispatchFixture struct {
	agent  *Agent
	room   *virtual_city.VirtualCityRoom
	events *[]SystemEvent
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	room := newTestRoom(t, "R-dispatch")
	room.OwnerID = "owner-1"
	reg := &fakeRegistry{p: &fakeProvider{}}
	a, events := newTestAgent(t, room, reg)

	// City-WebGL-Show(房间级):返回一条结构化显示指令。
	if err := a.hub.RegisterRoom(room.RoomID, agentClassCityWebGLShow,
		func(_ context.Context, msg citybus.Message) (any, error) {
			return []displayCommand{{Type: DisplayFocusDistrict, District: "d1"}}, nil
		}); err != nil {
		t.Fatalf("register webgl: %v", err)
	}
	// City-Chrome(类级)。
	if err := a.hub.Register(string(agentroot.AgentClassCityChrome),
		func(context.Context, citybus.Message) (any, error) {
			return "采集完成:3 条行情", nil
		}); err != nil {
		t.Fatalf("register chrome: %v", err)
	}
	// City-Human(类级)。
	if err := a.hub.Register(string(agentroot.AgentClassCityHuman),
		func(context.Context, citybus.Message) (any, error) {
			return "已收到", nil
		}); err != nil {
		t.Fatalf("register human: %v", err)
	}
	// Dispatch 的 control_simulation 需要操作者身份(userID 显式传参,
	// 与 Run 调用链一致;本夹具的正例统一用房主 owner-1)。
	return &dispatchFixture{agent: a, room: room, events: events}
}

func TestDispatch_Positive(t *testing.T) {
	f := newDispatchFixture(t)
	cases := []struct {
		name string
		in   map[string]any
		want string
	}{
		// 预测报告用例必须排在注入之前(该房尚无 run,否则会返回真实报告)。
		{ToolQueryPrediction, map[string]any{}, "暂无预测运行报告"},
		{ToolInjectScenario, map[string]any{
			"kind": virtual_city.ScenarioEarthquake, "magnitude": 3, "duration_months": 6,
		}, "已注入场景"},
		{ToolListScenarios, map[string]any{}, virtual_city.ScenarioGoodsPriceShock},
		{ToolQueryCityState, map[string]any{"scope": "overview"}, "城市状态"},
		{ToolQueryWorldEvents, map[string]any{}, "暂无真实世界事件"},
		{ToolControlSimulation, map[string]any{"action": "status"}, "playing"},
		{ToolControlSimulation, map[string]any{"action": "pause"}, "已暂停模拟"},
		{ToolLaunchSurvey, map[string]any{
			"question": "你满意当前物价吗?", "options": []any{"满意", "一般", "不满意"},
		}, "已发起调研"},
		{ToolFocusCamera, map[string]any{"district": "d1"}, DisplayFocusDistrict},
		{ToolCollectRealworldData, map[string]any{"category": "macro"}, "采集完成"},
		{ToolSendAgentMessage, map[string]any{
			"target_agent": string(agentroot.AgentClassCityHuman), "message": "hi",
		}, "已向 " + string(agentroot.AgentClassCityHuman) + " 发送消息"},
		{ToolListAgents, map[string]any{}, string(agentroot.AgentClassCityChrome)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := f.agent.Dispatch(context.Background(), "owner-1", tc.name, tc.in)
			if isErr {
				t.Fatalf("unexpected error: %s", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want substring %q", text, tc.want)
			}
		})
	}
}

func TestDispatch_Negative(t *testing.T) {
	f := newDispatchFixture(t)
	cases := []struct {
		name   string
		in     map[string]any
		want   string
		userID string
	}{
		// 未知 kind / 缺 kind → 35055。
		{ToolInjectScenario, map[string]any{"kind": "not_a_scenario"}, "35055", ""},
		{ToolInjectScenario, map[string]any{}, "缺少场景 kind", ""},
		// 强度越界 → 35056。
		{ToolInjectScenario, map[string]any{
			"kind": virtual_city.ScenarioEarthquake, "magnitude": 99,
		}, "35056", ""},
		// 指定不存在的预测 run → 35058。
		{ToolQueryPrediction, map[string]any{"run_id": "PR999"}, "35058", ""},
		// 非房主暂停 → 35011。
		{ToolControlSimulation, map[string]any{"action": "resume"}, "35011", "intruder"},
		// 未知 action。
		{ToolControlSimulation, map[string]any{"action": "explode"}, "pause / resume / status", ""},
		// 调研参数非法。
		{ToolLaunchSurvey, map[string]any{
			"question": "q", "options": []any{"only-one"},
		}, "2-6", ""},
		{ToolLaunchSurvey, map[string]any{
			"question": "  ", "options": []any{"a", "b"},
		}, "问题不能为空", ""},
		// 未注册的总线目标。
		{ToolFocusCamera, map[string]any{"district": "d1"}, "agent not registered", ""},
		{ToolCollectRealworldData, map[string]any{}, "agent not registered", ""},
		{ToolSendAgentMessage, map[string]any{
			"target_agent": "LsmAgentGame-City-Nobody", "message": "hi",
		}, "agent not registered", ""},
		{ToolSendAgentMessage, map[string]any{"target_agent": "  ", "message": "hi"}, "缺少目标 Agent", ""},
		{ToolSendAgentMessage, map[string]any{
			"target_agent": string(agentroot.AgentClassCityHuman), "message": " ",
		}, "消息内容为空", ""},
		// 未知工具。
		{"no_such_tool", map[string]any{}, "未知工具", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.want, func(t *testing.T) {
			// focus_camera / collect / send_agent_message 需要未注册目标:
			// 用独立 hub(不注册任何目标)。
			ag := f.agent
			if tc.want == "agent not registered" {
				ag = NewAgent(f.room, Config{ModelKey: "TestModel"},
					&fakeRegistry{p: &fakeProvider{}}, newTestHub(t), NewConversationStore(40), nil)
			}
			text, isErr := ag.Dispatch(context.Background(), tc.userID, tc.name, tc.in)
			if !isErr {
				t.Fatalf("expected error, got text %q", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want substring %q", text, tc.want)
			}
		})
	}
}

// TestDispatch_NilRoom nil room 边界:房间相关工具返回 isErr,不 panic。
func TestDispatch_NilRoom(t *testing.T) {
	a := NewAgent(nil, Config{ModelKey: "TestModel"}, &fakeRegistry{p: &fakeProvider{}},
		newTestHub(t), NewConversationStore(40), nil)
	for _, name := range []string{
		ToolInjectScenario, ToolQueryCityState, ToolQueryWorldEvents,
		ToolQueryPrediction, ToolControlSimulation, ToolLaunchSurvey,
	} {
		text, isErr := a.Dispatch(context.Background(), "u1", name, map[string]any{})
		if !isErr {
			t.Errorf("%s: expected error with nil room, got %q", name, text)
		}
		if !strings.Contains(text, "房间未绑定") {
			t.Errorf("%s: text = %q, want 房间未绑定", name, text)
		}
	}
}

// TestDispatch_FocusCameraEmitsShowCommand focus_camera 命中时下发 show_command。
func TestDispatch_FocusCameraEmitsShowCommand(t *testing.T) {
	f := newDispatchFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", ToolFocusCamera,
		map[string]any{"district": "d1", "mode": "orbit"})
	if isErr {
		t.Fatalf("unexpected error: %s", text)
	}
	events := collectEvents(f.events)
	if !hasEventKind(events, EventShowCommand) {
		t.Fatalf("expected show_command event, got %v", kinds(events))
	}
	// 显示指令带 trace_id(便于前端与事件流串联)。
	found := false
	for _, e := range events {
		if e.Kind != EventShowCommand {
			continue
		}
		cmd, ok := e.Data.(displayCommand)
		if !ok {
			t.Fatalf("show_command data type = %T, want displayCommand", e.Data)
		}
		if cmd.Type != DisplayFocusDistrict || cmd.District != "d1" {
			t.Fatalf("unexpected command: %+v", cmd)
		}
		if cmd.TraceID == "" {
			t.Fatal("show_command must carry trace_id")
		}
		found = true
	}
	if !found {
		t.Fatal("no show_command event")
	}
}

// TestDispatch_UnknownTool 未知工具不回灌成功。
func TestDispatch_UnknownTool(t *testing.T) {
	f := newDispatchFixture(t)
	text, isErr := f.agent.Dispatch(context.Background(), "owner-1", "nope", nil)
	if !isErr || !strings.Contains(text, "未知工具") {
		t.Fatalf("text = %q, isErr = %v", text, isErr)
	}
}

// TestDispatch_ErrcodePassthrough 350xx 错误码原样回灌给模型(不吞成泛化
// 错误):房间侧 [35055] / [35056] / [35058] / [35011],总线未注册 [35061]。
func TestDispatch_ErrcodePassthrough(t *testing.T) {
	f := newDispatchFixture(t)
	// 未注册的总线目标 → [35061](ErrVirtualCityAgentNotFound)。
	ag := NewAgent(f.room, Config{ModelKey: "TestModel"},
		&fakeRegistry{p: &fakeProvider{}}, newTestHub(t), NewConversationStore(40), nil)
	cases := []struct {
		agent *Agent
		name  string
		in    map[string]any
		user  string
		want  string
	}{
		{ag, ToolSendAgentMessage,
			map[string]any{"target_agent": "LsmAgentGame-City-Nobody", "message": "hi"}, "", "[35061]"},
		{ag, ToolFocusCamera, map[string]any{"district": "d1"}, "", "[35061]"},
		{ag, ToolCollectRealworldData, map[string]any{}, "", "[35061]"},
		{f.agent, ToolInjectScenario, map[string]any{"kind": "not_a_scenario"}, "", "[35055]"},
		{f.agent, ToolInjectScenario,
			map[string]any{"kind": virtual_city.ScenarioEarthquake, "magnitude": 99}, "", "[35056]"},
		{f.agent, ToolQueryPrediction, map[string]any{"run_id": "PR999"}, "", "[35058]"},
		{f.agent, ToolControlSimulation, map[string]any{"action": "pause"}, "intruder", "[35011]"},
	}
	for _, tc := range cases {
		t.Run(tc.want+"/"+tc.name, func(t *testing.T) {
			text, isErr := tc.agent.Dispatch(context.Background(), tc.user, tc.name, tc.in)
			if !isErr {
				t.Fatalf("expected error, got %q", text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want errcode %s 原样回灌", text, tc.want)
			}
		})
	}
}

// ─────────────────────────── 辅助 ───────────────────────────

func kinds(events []SystemEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}
