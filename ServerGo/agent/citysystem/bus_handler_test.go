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

// TestBusHandler_DescribeAllAgents 批次85 §3.3.3 收尾(跨线交接):hub.Describe
// 对**全部 4 个 Agent**给出非空 Description 与 MessageTypes(含 City-System
// 自己的 RegisterRoom → RegisterRoomWith 迁移),list_agents 文本不再出现
// 「未登记描述」。
func TestBusHandler_DescribeAllAgents(t *testing.T) {
	room := newTestRoom(t, "R-bus-desc")
	a, _ := newTestAgent(t, room, &fakeRegistry{p: &fakeProvider{}})
	hub := a.hub

	// City-System 自己(本批迁移:RegisterRoomWith 带 types + desc)。
	if err := a.RegisterBusHandler(hub); err != nil {
		t.Fatalf("RegisterBusHandler: %v", err)
	}
	// 其余 3 个 Agent 与装配层同款 RegisterWith / RegisterRoomWith(带描述;
	// 职责线 3 已迁 main.go / ws 侧)。
	if err := hub.RegisterRoomWith(room.RoomID, agentClassCityWebGLShow,
		[]string{"run_display"}, "渲染显示 Agent",
		func(context.Context, citybus.Message) (any, error) {
			return []displayCommand{}, nil
		}); err != nil {
		t.Fatalf("register webgl: %v", err)
	}
	if err := hub.RegisterWith(string(agentroot.AgentClassCityChrome),
		[]string{"collect", "message"}, "采集真实世界行情与新闻",
		func(context.Context, citybus.Message) (any, error) { return "ok", nil }); err != nil {
		t.Fatalf("register chrome: %v", err)
	}
	if err := hub.RegisterWith(string(agentroot.AgentClassCityHuman),
		[]string{"message"}, "城市居民 Agent",
		func(context.Context, citybus.Message) (any, error) { return "ok", nil }); err != nil {
		t.Fatalf("register human: %v", err)
	}

	// hub.Describe:4 个 Agent 全部非空 Description / MessageTypes。
	list := hub.Describe(room.RoomID)
	if len(list) != 4 {
		t.Fatalf("describe count = %d, want 4: %+v", len(list), list)
	}
	sawSystem := false
	for _, d := range list {
		if strings.TrimSpace(d.Description) == "" {
			t.Errorf("agent %s has empty description (§3.3.3)", d.ClassName)
		}
		if len(d.MessageTypes) == 0 {
			t.Errorf("agent %s has empty message types (§3.3.3)", d.ClassName)
		}
		if d.ClassName == string(agentroot.AgentClassCitySystem) {
			sawSystem = true
			// MessageTypes = 实测口径(handleBusMessage 仅接受 message 分支)。
			if len(d.MessageTypes) != 1 || d.MessageTypes[0] != BusTypeMessage {
				t.Errorf("City-System message types = %v, want [%q]",
					d.MessageTypes, BusTypeMessage)
			}
			if d.Description != BusCapabilityDesc {
				t.Errorf("City-System desc = %q, want BusCapabilityDesc", d.Description)
			}
		}
	}
	if !sawSystem {
		t.Fatal("City-System must be described (RegisterRoomWith migration)")
	}

	// list_agents 文本不得再出现「未登记描述」。
	text, isErr := a.Dispatch(context.Background(), "owner-1", ToolListAgents, map[string]any{})
	if isErr {
		t.Fatalf("list_agents: %s", text)
	}
	if strings.Contains(text, "未登记描述") {
		t.Fatalf("list_agents still shows 未登记描述: %q", text)
	}
	if !strings.Contains(text, BusCapabilityDesc) {
		t.Fatalf("list_agents must show City-System desc: %q", text)
	}
}

// TestBusMessageTypes_Measured BusMessageTypes 与 handleBusMessage 实测口径
// 一致:["message"](其余类型不入队,防御用例已钉)。
func TestBusMessageTypes_Measured(t *testing.T) {
	if len(BusMessageTypes) != 1 || BusMessageTypes[0] != BusTypeMessage {
		t.Fatalf("BusMessageTypes = %v, want [%q] (handleBusMessage 实测口径)",
			BusMessageTypes, BusTypeMessage)
	}
	if strings.TrimSpace(BusCapabilityDesc) == "" {
		t.Fatal("BusCapabilityDesc must be non-empty")
	}
}
