// Package ws — game_service_virtual_city_system_scope_test.go: 批次87 P1-6
// 「会话事件按发起用户定向投递」的 WS 层单测(2026-11-06)。
//
// 场景:同一房间内 A 与 B 两位用户。A 发起一轮系统对话 ⇒
//   - A 收到 game.virtual_city_system_event(agent_start/text/agent_end…);
//   - B **不**收到任何会话事件(否则 B 的面板会把 A 的流式回复并进自己的
//     助手气泡,且 A 的对话内容经线缆外泄给全房);
//   - A 收到的事件载荷**结构不变**(room_id/kind/data 三字段)。
//
// 兼容性:旧前端连新后端只是事件变少(语义正确);新前端连旧后端保持批次
// 81–86 的全房广播行为。
package ws

import (
	"encoding/json"
	"testing"
	"time"

	"LsmAgentGame/agent/citysystem"
)

// collectSystemEvents 排空客户端通道里指定类型的帧。
//
// untilKind 非空时,收到该 kind 即提前返回(免固定等待);
// 超时 wait 返回已收集到的(空 = 什么都没收到)。
func collectSystemEvents(t *testing.T, c *Client, frameType string, wait time.Duration, untilKind string) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, 8)
	deadline := time.After(wait)
	for {
		select {
		case ev := <-c.send:
			if ev.Type != frameType {
				continue
			}
			m := sysPayloadMap(t, ev)
			out = append(out, m)
			if untilKind != "" && m["kind"] == untilKind {
				return out
			}
		case <-deadline:
			return out
		}
	}
}

// TestCitySystemEvent_ScopedToInitiator A 发起 ⇒ 只有 A 收到会话事件。
func TestCitySystemEvent_ScopedToInitiator(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	cA := sysClient("u-A")
	cB := sysClient("u-B")
	// 两人都入房(A 玩家 / B 观战者),确保 B 本来就能收到全房广播。
	gs.hub.SubscribeRoom(roomID, cA)
	gs.hub.SpectateRoom(roomID, cB)

	gs.handleVirtualCitySystemMessage(cA, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"查看城市状态"}`)})

	// A 至少应收到 agent_start 与 agent_end。
	aEvents := collectSystemEvents(t, cA, "game.virtual_city_system_event", 3*time.Second, citysystem.EventAgentEnd)
	sawStart, sawEnd := false, false
	for _, m := range aEvents {
		switch m["kind"] {
		case citysystem.EventAgentStart:
			sawStart = true
		case citysystem.EventAgentEnd:
			sawEnd = true
		}
		// 载荷结构不变:room_id / kind / data 三字段。
		if m["room_id"] != roomID {
			t.Fatalf("event payload room_id = %v, want %q", m["room_id"], roomID)
		}
		if _, ok := m["data"]; !ok {
			t.Fatalf("event payload missing data: %v", m)
		}
	}
	if !sawStart || !sawEnd {
		t.Fatalf("initiator missed events (start=%v end=%v), got %v", sawStart, sawEnd, aEvents)
	}

	// B 不得收到任何会话事件。
	bEvents := collectSystemEvents(t, cB, "game.virtual_city_system_event", 500*time.Millisecond, "")
	if len(bEvents) > 0 {
		t.Fatalf("non-initiator received %d system events, want 0: %v", len(bEvents), bEvents)
	}
}

// TestCitySystemEvent_ShowCommandScopedToInitiator A 触发的显示指令
// (Agent 工具 focus_camera / apply_display 派发)只发给 A。
//
// 用 City-WebGL-Show 房间级 bus handler 派发的 show_command 仍是**全房**
// (批次84「自动渲染显示」既有语义)—— 本测试只锁「Agent 工具派发」这一侧。
func TestCitySystemEvent_ShowCommandScopedToInitiator(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	cA := sysClient("u-A2")
	cB := sysClient("u-B2")
	gs.hub.SubscribeRoom(roomID, cA)
	gs.hub.SpectateRoom(roomID, cB)

	// 直接触发 Agent 工具链:apply_display 直产「取消筛选」指令(不依赖总线)。
	sa := gs.getOrCreateSystemRoomAgent(roomID, gs.vcMgr.Get(roomID))
	if _, isErr := sa.agent.Dispatch(t.Context(), "u-A2", citysystem.ToolApplyDisplay,
		map[string]any{"category": ""}); isErr {
		t.Fatal("apply_display must succeed")
	}

	aCmds := collectSystemEvents(t, cA, "game.virtual_city_show_command", 2*time.Second, "")
	if len(aCmds) == 0 {
		t.Fatal("initiator received no show_command frame")
	}
	bCmds := collectSystemEvents(t, cB, "game.virtual_city_show_command", 500*time.Millisecond, "")
	if len(bCmds) > 0 {
		t.Fatalf("non-initiator received %d show_command frames, want 0: %v", len(bCmds), bCmds)
	}
}

// TestCitySystemEvent_AgentMessageStillRoomLevel 其它 Agent 来信
// (agent_message)保持**全房**投递 —— 来信是房间级事件,入队
// incomingKey(roomID)后注入所有用户的下一轮。
func TestCitySystemEvent_AgentMessageStillRoomLevel(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	cA := sysClient("u-A3")
	cB := sysClient("u-B3")
	gs.hub.SubscribeRoom(roomID, cA)
	gs.hub.SpectateRoom(roomID, cB)
	gs.getOrCreateSystemRoomAgent(roomID, gs.vcMgr.Get(roomID))

	// 经总线向 City-System 发一条上行消息(模拟其它 Agent 来信)。
	if _, err := busSend(roomID, "LsmAgentGame-City-Human", "LsmAgentGame-City-System",
		"message", map[string]any{"text": "居民区出事了"}); err != nil {
		t.Fatalf("bus send: %v", err)
	}

	for _, c := range []*Client{cA, cB} {
		events := collectSystemEvents(t, c, "game.virtual_city_system_event", 2*time.Second, citysystem.EventAgentMessage)
		saw := false
		for _, m := range events {
			if m["kind"] == citysystem.EventAgentMessage {
				saw = true
			}
		}
		if !saw {
			t.Fatalf("user %s did not receive agent_message (room-level event must stay room-wide)", c.UserID)
		}
	}
}
