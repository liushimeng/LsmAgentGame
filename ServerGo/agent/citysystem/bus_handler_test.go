// Package citysystem — bus_handler_test.go: 「Agent 来信」上行通道单测
// (2026-11-05 §批次84 §4.3.2)。
//
// 覆盖:上行 message ⇒ agent_message 事件 + 来信入队 + 下一轮 system 含来信段;
// 自我发信 / 非 message 类型 / 空正文的防御分支。
package citysystem

import (
	"context"
	"strings"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	llmtypes "LsmAgentGame/llm/types"
)

// TestBusHandler_IncomingMessage 上行来信:事件 + 入队 + 下一轮 system 注入。
func TestBusHandler_IncomingMessage(t *testing.T) {
	room := newTestRoom(t, "R-bus")
	reg := &fakeRegistry{p: &fakeProvider{responses: []llmtypes.LLMResponse{
		textResponse("已参考来信。"),
	}}}
	a, events := newTestAgent(t, room, reg)
	hub := a.hub

	if err := a.RegisterBusHandler(hub); err != nil {
		t.Fatalf("RegisterBusHandler: %v", err)
	}

	// 另一个 Agent(Post 投递,模拟 citychrome 的 NotifySystem)。
	if err := hub.Post(context.Background(), room.RoomID,
		string(agentroot.AgentClassCityChrome), string(agentroot.AgentClassCitySystem),
		BusTypeMessage, map[string]any{"text": "已采集到今日 A 股行情"}); err != nil {
		t.Fatalf("post: %v", err)
	}
	// 等待 postLoop 投递(异步)。
	deadline := time.Now().Add(2 * time.Second)
	for a.store.PendingIncoming(incomingKey(room.RoomID)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	// 1) agent_message 事件。
	got := collectEvents(events)
	if !hasEventKind(got, EventAgentMessage) {
		t.Fatalf("missing %s event in %v", EventAgentMessage, kinds(got))
	}
	for _, e := range got {
		if e.Kind != EventAgentMessage {
			continue
		}
		data, _ := e.Data.(map[string]any)
		if data["from"] != string(agentroot.AgentClassCityChrome) {
			t.Errorf("from = %v", data["from"])
		}
		if data["text"] != "已采集到今日 A 股行情" {
			t.Errorf("text = %v", data["text"])
		}
		if ts, _ := data["ts"].(int64); ts == 0 {
			t.Errorf("ts = %v", data["ts"])
		}
	}

	// 2) 来信入队(未消费)。
	if n := a.store.PendingIncoming(incomingKey(room.RoomID)); n != 1 {
		t.Fatalf("pending incoming = %d, want 1", n)
	}

	// 3) 下一轮 Run 构造 system 时注入来信段(注入后出队)。
	if err := a.Run(context.Background(), "u1", "城市怎么样?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := a.store.PendingIncoming(incomingKey(room.RoomID)); n != 0 {
		t.Fatalf("pending incoming after run = %d, want 0", n)
	}
}

// TestBusHandler_SystemInjectedInSection 上行来信注入下一轮 system 提示词。
func TestBusHandler_SystemInjectedInSection(t *testing.T) {
	room := newTestRoom(t, "R-bus2")
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{textResponse("ok")}}
	a, _ := newTestAgent(t, room, &fakeRegistry{p: fp})

	// 直接投递来信(不经 postLoop,确定性)。
	a.handleBusMessage(citybus.Message{
		From: string(agentroot.AgentClassCityChrome),
		To:   string(agentroot.AgentClassCitySystem),
		Type: BusTypeMessage,
		Payload: map[string]any{"text": "已采集到今日 A 股行情"},
	})
	if err := a.Run(context.Background(), "u1", "城市怎么样?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	fp.mu.Lock()
	req := fp.reqs[len(fp.reqs)-1]
	fp.mu.Unlock()
	all := ""
	for _, b := range req.System {
		all += b.Text
	}
	if !strings.Contains(all, "【来自其它 Agent 的消息】") {
		t.Fatalf("system must contain the incoming section, got %q", all)
	}
	if !strings.Contains(all, "已采集到今日 A 股行情") {
		t.Fatalf("system must contain the incoming text, got %q", all)
	}
	if !strings.Contains(all, string(agentroot.AgentClassCityChrome)) {
		t.Fatalf("system must contain the incoming sender, got %q", all)
	}
	// 同一条来信只注入一次:再跑一轮不得重复。
	if err := a.Run(context.Background(), "u1", "再看一眼"); err != nil {
		t.Fatalf("run2: %v", err)
	}
	fp.mu.Lock()
	req2 := fp.reqs[len(fp.reqs)-1]
	fp.mu.Unlock()
	all2 := ""
	for _, b := range req2.System {
		all2 += b.Text
	}
	if strings.Contains(all2, "已采集到今日 A 股行情") {
		t.Fatal("same incoming must not be injected twice")
	}
}

// TestBusHandler_Defense 自我发信 / 非 message / 空正文的防御分支。
func TestBusHandler_Defense(t *testing.T) {
	room := newTestRoom(t, "R-bus3")
	a, events := newTestAgent(t, room, &fakeRegistry{p: &fakeProvider{}})

	// 非 message 类型 ⇒ 不入队。
	reply := a.handleBusMessage(citybus.Message{
		From: "A", Type: "run_display", Payload: "x",
	})
	if !strings.Contains(asString(reply), "仅支持") {
		t.Errorf("non-message reply = %v", reply)
	}
	if a.store.PendingIncoming(incomingKey(room.RoomID)) != 0 {
		t.Error("non-message must not enqueue")
	}

	// 自我发信 ⇒ 忽略。
	reply = a.handleBusMessage(citybus.Message{
		From: string(agentroot.AgentClassCitySystem), Type: BusTypeMessage,
		Payload: map[string]any{"text": "自问"},
	})
	if !strings.Contains(asString(reply), "防回环") {
		t.Errorf("self reply = %v", reply)
	}
	if a.store.PendingIncoming(incomingKey(room.RoomID)) != 0 {
		t.Error("self message must not enqueue")
	}

	// 空正文 ⇒ 忽略。
	reply = a.handleBusMessage(citybus.Message{
		From: "A", Type: BusTypeMessage, Payload: map[string]any{"text": "  "},
	})
	if !strings.Contains(asString(reply), "正文为空") {
		t.Errorf("empty text reply = %v", reply)
	}
	if a.store.PendingIncoming(incomingKey(room.RoomID)) != 0 {
		t.Error("empty text must not enqueue")
	}

	// 正常 ⇒ 回执 + 入队 + 事件。
	reply = a.handleBusMessage(citybus.Message{
		From: "A", Type: BusTypeMessage, Payload: map[string]any{"text": "hi"},
	})
	if !strings.Contains(asString(reply), "已收到") {
		t.Errorf("normal reply = %v", reply)
	}
	if a.store.PendingIncoming(incomingKey(room.RoomID)) != 1 {
		t.Error("normal message must enqueue")
	}
	if !hasEventKind(collectEvents(events), EventAgentMessage) {
		t.Error("normal message must emit agent_message")
	}
}

// TestBusMessageText 兼容 map{"text":...} / 裸字符串 / JSON 往返。
func TestBusMessageText(t *testing.T) {
	if got := busMessageText(map[string]any{"text": " hi "}); got != "hi" {
		t.Errorf("map text = %q", got)
	}
	if got := busMessageText(" hi "); got != "hi" {
		t.Errorf("string text = %q", got)
	}
	if got := busMessageText(map[string]any{"other": 1}); got != "" {
		t.Errorf("missing text = %q", got)
	}
	if got := busMessageText(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
}

// asString 把 handler 返回值转 string(测试辅助)。
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
