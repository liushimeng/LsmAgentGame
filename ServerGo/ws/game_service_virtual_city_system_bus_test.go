// Package ws — game_service_virtual_city_system_bus_test.go: 批次84 装配层
// 接线单测(2026-11-05 §批次84 §9.1)。
//
// 覆盖:
//   - City-WebGL-Show 房间级 handler 按 msg.Type 路由:message → 对话式翻译
//     回中文摘要;run_display/"" → 显示路径(仅带 phase/weather 的请求走
//     Structured 零 LLM,断言 HasStructuredParams 生效);未知 Type → 明确
//     中文失败说明;
//   - City-System RegisterBusHandler 上行来信:收 "message" 被接收
//     (回执 + agent_message 事件);
//   - removeCitySystemAgent 后房间级注册已清(Describe 不再列出)。
package ws

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/agent/citysystem"
	"LsmAgentGame/agent/citywebgl"
	llmtypes "LsmAgentGame/llm/types"
)

// busTestProvider 是按脚本应答的 fake LLM provider(记录调用次数)。
// 未脚本化时统一回 end_turn 文本(不产 tool_use)。
type busTestProvider struct {
	calls  int64
	script []llmtypes.LLMResponse
}

func (p *busTestProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	n := atomic.AddInt64(&p.calls, 1)
	idx := int(n) - 1
	if idx < len(p.script) {
		return p.script[idx], nil
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}

func (p *busTestProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *busTestProvider) ProviderType() string { return "fake-bus" }

// newBusTestGame 构造已开局房间 + 注入 City-System 运行时的 GameService,
// 并完成房间级 bus 注册(getOrCreateSystemRoomAgent)。
func newBusTestGame(t *testing.T, provider llmtypes.LLMProvider) (*GameService, string) {
	t.Helper()
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	r := gs.vcMgr.Get(roomID)
	if r == nil {
		t.Fatalf("room %s not found", roomID)
	}
	gs.getOrCreateSystemRoomAgent(roomID, r)
	return gs, roomID
}

// busSend 经 citybus.Default() 向指定 Agent 发一条消息(模拟 ask_agent /
// send_agent_message 的总线投递)。
func busSend(roomID, from, to, typ string, payload any) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return citybus.Default().Send(ctx, roomID, from, to, typ, payload)
}

// TestCityWebGLBusHandler_MessageSummary "message" 分支:把自然语言翻译成
// 显示指令并回**非空中文摘要**(不 panic、不 JSON 类型错误 —— §2.5b 回归锚)。
func TestCityWebGLBusHandler_MessageSummary(t *testing.T) {
	// 脚本:第 1 次 LLM 调用产 1 条 set_weather 指令,第 2 次收尾。
	provider := &busTestProvider{script: []llmtypes.LLMResponse{
		{StopReason: "tool_use", Content: []llmtypes.ContentBlock{{
			Type: "tool_use", ID: "t1", Name: citywebgl.ToolSetWeather,
			Input: map[string]any{"weather": "rain"},
		}}},
		{StopReason: "end_turn", Content: []llmtypes.ContentBlock{{Type: "text", Text: "done"}}},
	}}
	gs, roomID := newBusTestGame(t, provider)
	_ = gs

	for _, payload := range []any{
		map[string]any{"text": "把镜头拉到商业区并下雨"},
		"让画面下点雨", // 裸字符串载荷(与其它 Agent 一致)
	} {
		res, err := busSend(roomID, string(agentroot.AgentClassCitySystem),
			string(agentroot.AgentClassCityWebGLShow), "message", payload)
		if err != nil {
			t.Fatalf("message payload %v: err = %v, want summary", payload, err)
		}
		reply, ok := res.(string)
		if !ok || strings.TrimSpace(reply) == "" {
			t.Fatalf("message payload %v: reply = %#v, want non-empty string", payload, res)
		}
		if !strings.Contains(reply, "显示指令") {
			t.Fatalf("message payload %v: reply = %q, want 中文摘要含「显示指令」", payload, reply)
		}
	}
}

// TestCityWebGLBusHandler_MessageInvalidPayload 非法载荷(缺 text / text
// 非字符串 / text 空 / 形态不对)⇒ 明确中文失败说明,不 panic。
func TestCityWebGLBusHandler_MessageInvalidPayload(t *testing.T) {
	provider := &busTestProvider{}
	gs, roomID := newBusTestGame(t, provider)
	_ = gs
	for _, payload := range []any{
		map[string]any{"other": "x"},
		map[string]any{"text": 42},
		map[string]any{"text": "   "},
		123,
		nil,
	} {
		res, err := busSend(roomID, string(agentroot.AgentClassCitySystem),
			string(agentroot.AgentClassCityWebGLShow), "message", payload)
		if err == nil {
			t.Fatalf("payload %v: err = nil (res=%#v), want 失败说明", payload, res)
		}
		if !strings.Contains(err.Error(), "消息") {
			t.Fatalf("payload %v: err = %q, want 中文失败说明", payload, err.Error())
		}
	}
}

// TestCityWebGLBusHandler_StructuredNoLLM run_display / 空 Type:仅带
// phase/weather 等新参数的请求必须走 Structured(零 LLM)—— 断言
// HasStructuredParams 已替换旧内联判据(批次84 修复项)。同时钉死
// **单发射点纪律**:run_display 分支不得自行 emit(指令由 citysystem 侧
// emitCommands 统一下发,ws 层双发会出重复帧)。
func TestCityWebGLBusHandler_StructuredNoLLM(t *testing.T) {
	provider := &busTestProvider{}
	gs, roomID := newBusTestGame(t, provider)
	c := sysClient("u-structured")
	gs.hub.SubscribeRoom(roomID, c)

	for _, typ := range []string{"run_display", ""} {
		res, err := busSend(roomID, string(agentroot.AgentClassCitySystem),
			string(agentroot.AgentClassCityWebGLShow), typ,
			map[string]any{"phase": "night", "weather": "rain"})
		if err != nil {
			t.Fatalf("type %q: err = %v, want display commands", typ, err)
		}
		cmds, ok := res.([]citywebgl.DisplayCommand)
		if !ok {
			t.Fatalf("type %q: res type = %T, want []citywebgl.DisplayCommand", typ, res)
		}
		got := map[string]bool{}
		for _, cmd := range cmds {
			got[cmd.Type] = true
		}
		if !got[citywebgl.DisplaySetTimeOfDay] || !got[citywebgl.DisplaySetWeather] {
			t.Fatalf("type %q: commands = %+v, want 含 %s 与 %s",
				typ, cmds, citywebgl.DisplaySetTimeOfDay, citywebgl.DisplaySetWeather)
		}
	}
	if n := atomic.LoadInt64(&provider.calls); n != 0 {
		t.Fatalf("LLM called %d times, want 0 (Structured 必须零 LLM)", n)
	}
	// run_display 分支零 emit:show_command 帧只允许来自 citysystem 侧
	// emitCommands(此处没有 citysystem 参与 ⇒ 恰 0 条)。
	for {
		select {
		case ev := <-c.send:
			if ev.Type == "game.virtual_city_show_command" {
				t.Fatalf("run_display 分支不得自行 emit show_command 帧(双发帧回归): %s", string(ev.Payload))
			}
		default:
			return
		}
	}
}

// TestCityWebGLBusHandler_MessageEmitsShowCommands message 分支产出的指令
// 必须逐条下发前端(批次84 需求 2「自动渲染显示」/ 需求 5「实时显示正在
// 干什么」):game.virtual_city_show_command 帧条数与产出指令数一致,
// 且恰为 N 条(不得双发帧)。busSend 同步返回 ⇒ 发帧均已完成(hub 广播
// 同步入列),非阻塞排空计数即可。
func TestCityWebGLBusHandler_MessageEmitsShowCommands(t *testing.T) {
	provider := &busTestProvider{script: []llmtypes.LLMResponse{
		{StopReason: "tool_use", Content: []llmtypes.ContentBlock{
			{Type: "tool_use", ID: "t1", Name: citywebgl.ToolSetWeather,
				Input: map[string]any{"weather": "rain"}},
			{Type: "tool_use", ID: "t2", Name: citywebgl.ToolAnnotate,
				Input: map[string]any{"text": "测试标注"}},
		}},
		{StopReason: "end_turn", Content: []llmtypes.ContentBlock{{Type: "text", Text: "done"}}},
	}}
	gs, roomID := newBusTestGame(t, provider)
	c := sysClient("u-showcmd")
	gs.hub.SubscribeRoom(roomID, c)

	res, err := busSend(roomID, string(agentroot.AgentClassCitySystem),
		string(agentroot.AgentClassCityWebGLShow), "message",
		map[string]any{"text": "下雨并在画面加个标注"})
	if err != nil {
		t.Fatalf("message: err = %v", err)
	}
	reply, _ := res.(string)
	if !strings.Contains(reply, "已翻译为 2 条显示指令") {
		t.Fatalf("reply = %q, want 「已翻译为 2 条显示指令」", reply)
	}

	showCmds := 0
	types := map[string]bool{}
	drained := false
	for !drained {
		select {
		case ev := <-c.send:
			if ev.Type != "game.virtual_city_show_command" {
				continue
			}
			showCmds++
			m := sysPayloadMap(t, ev)
			if cmd, ok := m["command"].(map[string]any); ok {
				if typ, ok := cmd["type"].(string); ok {
					types[typ] = true
				}
			}
		default:
			drained = true
		}
	}
	if showCmds != 2 {
		t.Fatalf("show_command 帧 = %d 条,期望恰 2 条(与产出指令数一致,不得双发)", showCmds)
	}
	if !types[citywebgl.DisplaySetWeather] || !types[citywebgl.DisplayAnnotate] {
		t.Fatalf("show_command types = %v, want 含 %s 与 %s",
			types, citywebgl.DisplaySetWeather, citywebgl.DisplayAnnotate)
	}
}

// TestCityWebGLBusHandler_UnknownType 未知 Type ⇒ 明确中文失败说明。
func TestCityWebGLBusHandler_UnknownType(t *testing.T) {
	provider := &busTestProvider{}
	gs, roomID := newBusTestGame(t, provider)
	_ = gs
	res, err := busSend(roomID, string(agentroot.AgentClassCitySystem),
		string(agentroot.AgentClassCityWebGLShow), "bogus", map[string]any{"text": "x"})
	if err == nil {
		t.Fatalf("res = %#v, want 失败说明", res)
	}
	if !strings.Contains(err.Error(), "不支持") {
		t.Fatalf("err = %q, want 含「不支持」的中文失败说明", err.Error())
	}
}

// TestCitySystemBusHandler_IncomingMessage RegisterBusHandler 后,向
// LsmAgentGame-City-System 发 "message" 能被接收:回执非空 + agent_message
// 事件随 game.virtual_city_system_event 下发。
func TestCitySystemBusHandler_IncomingMessage(t *testing.T) {
	provider := &busTestProvider{}
	gs, roomID := newBusTestGame(t, provider)
	c := sysClient("u-incoming")
	gs.hub.SubscribeRoom(roomID, c)

	res, err := busSend(roomID, string(agentroot.AgentClassCityHuman),
		string(agentroot.AgentClassCitySystem), "message", map[string]any{"text": "居民区出事了"})
	if err != nil {
		t.Fatalf("send to City-System: err = %v, want 回执", err)
	}
	reply, ok := res.(string)
	if !ok || !strings.Contains(reply, "已收到") {
		t.Fatalf("reply = %#v, want 含「已收到」的回执", res)
	}
	// agent_message 事件应经 WS 广播到房间(前端「Agent 来信」卡片)。
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-c.send:
			if ev.Type != "game.virtual_city_system_event" {
				continue
			}
			if sysPayloadMap(t, ev)["kind"] == citysystem.EventAgentMessage {
				return
			}
		case <-deadline:
			t.Fatalf("%s 事件未随 game.virtual_city_system_event 下发", citysystem.EventAgentMessage)
		}
	}
}

// TestRemoveCitySystemAgent_UnregistersRoomBus removeCitySystemAgent 后
// City-System 与 City-WebGL-Show 的房间级注册均已清(Describe 不再列出)。
func TestRemoveCitySystemAgent_UnregistersRoomBus(t *testing.T) {
	provider := &busTestProvider{}
	gs, roomID := newBusTestGame(t, provider)

	want := map[string]bool{
		string(agentroot.AgentClassCitySystem):    false,
		string(agentroot.AgentClassCityWebGLShow): false,
	}
	for _, d := range citybus.Default().Describe(roomID) {
		if d.Scope == citybus.ScopeRoom && d.RoomID == roomID {
			if _, ok := want[d.ClassName]; ok {
				want[d.ClassName] = true
			}
		}
	}
	for class, seen := range want {
		if !seen {
			t.Fatalf("注册后 Describe 缺房间级 %s", class)
		}
	}

	gs.removeCitySystemAgent(roomID)
	for _, d := range citybus.Default().Describe(roomID) {
		if d.Scope == citybus.ScopeRoom && d.RoomID == roomID {
			t.Fatalf("removeCitySystemAgent 后 Describe 仍列 %s(注册未清)", d.ClassName)
		}
	}
}
