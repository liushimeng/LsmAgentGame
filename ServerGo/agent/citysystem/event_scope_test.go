// Package citysystem — event_scope_test.go: 批次87 P1-6「会话事件按发起
// 用户定向投递」的包内单测。
//
// 背景:会话**存储**按 roomID/userID 隔离(context.go 的 key = roomID+"/"+userID),
// 但**事件流**原先是全房广播 ⇒ 同房 A 发起对话,B 的面板会把 A 的流式回复
// 并进自己的助手气泡(前端 ensureStreamingAssistant 只认「最后一条 streaming
// 助手消息」),且 A 的对话内容经线缆外泄给全房。
//
// 本文件锁死 emit 回调的归属语义:
//   - 会话类事件(agent_start/text/thinking/tool_start/tool_result/report/
//     error/agent_end)必须带**发起用户**的 userID;
//   - 房间级事件(其它 Agent 来信 agent_message)必须带**空** userID。
package citysystem

import (
	"context"
	"testing"

	llmtypes "LsmAgentGame/llm/types"
)

// eventsFor 返回指定 kind 的事件列表。
func eventsFor(events *[]capturedEvent, kind string) []capturedEvent {
	out := make([]capturedEvent, 0, 4)
	for _, e := range collectEvents(events) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// TestEmitScope_SessionEventsCarryInitiator 会话类事件的归属必须是发起用户。
func TestEmitScope_SessionEventsCarryInitiator(t *testing.T) {
	room := newTestRoom(t, "R-scope")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolResponse("tu-1", ToolQueryCityState, map[string]any{"scope": "overview"}),
		textResponse("城市状态如下。"),
	}}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u-initiator", "现在城市怎么样?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	// 会话类事件清单(P1-6 定向投递的 8 类)。
	for _, kind := range []string{
		EventAgentStart, EventToolStart, EventToolResult, EventAgentEnd,
	} {
		list := eventsFor(events, kind)
		if len(list) == 0 {
			t.Fatalf("missing event %q", kind)
		}
		for _, e := range list {
			if e.UserID != "u-initiator" {
				t.Fatalf("event %q owner = %q, want %q", kind, e.UserID, "u-initiator")
			}
		}
	}
}

// TestEmitScope_AgentMessageIsRoomLevel 其它 Agent 来信是**房间级**事件
// (入队 incomingKey(roomID),注入所有用户的下一轮)⇒ userID 必须为空。
func TestEmitScope_AgentMessageIsRoomLevel(t *testing.T) {
	room := newTestRoom(t, "R-scope-msg")
	a, events := newTestAgent(t, room, &fakeRegistry{p: &fakeProvider{}})
	if err := a.RegisterBusHandler(a.hub); err != nil {
		t.Fatalf("register bus handler: %v", err)
	}
	if _, err := a.hub.Send(context.Background(), a.RoomID,
		"LsmAgentGame-City-Human", "LsmAgentGame-City-System",
		BusTypeMessage, map[string]any{"text": "居民区出事了"}); err != nil {
		t.Fatalf("bus send: %v", err)
	}
	list := eventsFor(events, EventAgentMessage)
	if len(list) == 0 {
		t.Fatal("missing agent_message event")
	}
	for _, e := range list {
		if e.UserID != "" {
			t.Fatalf("agent_message owner = %q, want 空(房间级)", e.UserID)
		}
	}
}

// TestEmitScope_ShowCommandCarriesInitiator 由 Agent 工具(focus_camera /
// apply_display)派发的 show_command 归属发起用户 —— 3D 相机是客户端本地
// 状态,全房强拉相机是骚扰。
func TestEmitScope_ShowCommandCarriesInitiator(t *testing.T) {
	room := newTestRoom(t, "R-scope-show")
	a, events := newTestAgent(t, room, &fakeRegistry{p: &fakeProvider{}})
	// apply_display 直产一条取消筛选指令(不依赖总线目标)。
	if _, isErr := a.Dispatch(context.Background(), "u-initiator", ToolApplyDisplay,
		map[string]any{"category": ""}); isErr {
		t.Fatal("apply_display must succeed")
	}
	list := eventsFor(events, EventShowCommand)
	if len(list) == 0 {
		t.Fatal("missing show_command event")
	}
	for _, e := range list {
		if e.UserID != "u-initiator" {
			t.Fatalf("show_command owner = %q, want %q", e.UserID, "u-initiator")
		}
	}
}

// TestEmitScope_ConcurrentRunsDoNotCrossTalk 两个用户并发跑同一 Agent:
// 事件归属不得串号(A 的事件不能带 B 的 userID)。
func TestEmitScope_ConcurrentRunsDoNotCrossTalk(t *testing.T) {
	room := newTestRoom(t, "R-scope-conc")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		textResponse("ok-1"), textResponse("ok-2"),
	}}
	a, events := newTestAgent(t, room, &fakeRegistry{p: fp})
	if err := a.Run(context.Background(), "u-a", "A 的问题"); err != nil {
		t.Fatalf("run A: %v", err)
	}
	if err := a.Run(context.Background(), "u-b", "B 的问题"); err != nil {
		t.Fatalf("run B: %v", err)
	}
	owners := map[string]int{}
	for _, e := range collectEvents(events) {
		switch e.Kind {
		case EventAgentStart, EventAgentEnd:
			owners[e.UserID]++
		}
	}
	if owners["u-a"] < 2 || owners["u-b"] < 2 {
		t.Fatalf("event owners = %v, want u-a/u-b 各 ≥2 条(start+end)", owners)
	}
}
