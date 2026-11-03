// Package ws — game_service_virtual_city_system_test.go: 批次81 City-System
// 主 Agent WS 帧处理单测(2026-11-03 §批次81)。
//
// 覆盖:载荷校验 / 未启用 35058 / 房间不存在 35001 / accepted + 事件流 +
// 会话持久化 / 并发限流 35060 / reset / history 空会话。
package ws

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/agent/citysystem"
	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
)

// fakeSystemProvider 是 citysystem 的 fake LLM provider(直接文本作答)。
type fakeSystemProvider struct {
	block chan struct{}
	calls chan struct{} // 非 nil 时记录调用次数
}

func (p *fakeSystemProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	if p.calls != nil {
		select {
		case p.calls <- struct{}{}:
		default:
		}
	}
	if p.block != nil {
		<-p.block
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}

func (p *fakeSystemProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (p *fakeSystemProvider) ProviderType() string { return "fake-system" }

// fakeSystemRegistry 实现 citysystem.LLMRegistry。
type fakeSystemRegistry struct{ provider llmtypes.LLMProvider }

func (r fakeSystemRegistry) Get(string) (llmtypes.LLMProvider, string, error) {
	return r.provider, "fake-key", nil
}
func (r fakeSystemRegistry) GetThinkingEnabled(string) (bool, int) { return false, 0 }
func (r fakeSystemRegistry) ListEnabled() []llmtypes.ModelInfo     { return nil }

// newSystemTestGame 构造已开局房间 + 注入 City-System 运行时的 GameService。
func newSystemTestGame(t *testing.T, enabled bool, reg citysystem.LLMRegistry) (*GameService, string) {
	t.Helper()
	manager := virtual_city.NewManager(virtual_city.Config{
		MonthMs: 3000, AgentEnabled: false, AgentConcurrency: 4,
	}, nil)
	room := manager.CreateRoom("room-sys-test")
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < virtual_city.MinSeats; seat++ {
		botUsers[seat] = "sys-bot-" + strconv.Itoa(seat)
		botModels[seat] = "TestModel"
	}
	room.RegisterBotSeats(botUsers, botModels)
	if e := room.Start(nil); e != nil {
		room.Close()
		t.Fatalf("start room: %v", e)
	}
	t.Cleanup(room.Close)
	gs := &GameService{hub: NewHub(), vcMgr: manager}
	gs.SetCitySystemRuntime(enabled, config.ResolvedCitySystemConfig{
		ModelKey: "FakeModel", MaxSteps: 4, MaxMessages: 20,
		RequestTimeout: 5 * time.Second, ToolResultLimit: 1000,
	}, reg)
	return gs, room.RoomID
}

func sysClient(userID string) *Client {
	return &Client{UserID: userID, send: make(chan Envelope, 32)}
}

func nextSysEnvelope(t *testing.T, c *Client) Envelope {
	t.Helper()
	select {
	case env := <-c.send:
		return env
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for envelope")
		return Envelope{}
	}
}

func sysEnvelopeCode(t *testing.T, env Envelope) int {
	t.Helper()
	var p struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload %s: %v", string(env.Payload), err)
	}
	return p.Code
}

func sysPayloadMap(t *testing.T, env Envelope) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload %s: %v", string(env.Payload), err)
	}
	return p
}

// TestVirtualCitySystemMessage_Disabled 未启用 → 35058。
func TestVirtualCitySystemMessage_Disabled(t *testing.T) {
	gs, roomID := newSystemTestGame(t, false, fakeSystemRegistry{provider: &fakeSystemProvider{}})
	c := sysClient("u1")
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"hi"}`)})
	env := nextSysEnvelope(t, c)
	if env.Type != "game.error" || sysEnvelopeCode(t, env) != errcode.ErrVirtualCitySystemAgentDisabled {
		t.Fatalf("got type=%s code=%d, want error %d", env.Type, sysEnvelopeCode(t, env), errcode.ErrVirtualCitySystemAgentDisabled)
	}
}

// TestVirtualCitySystemMessage_InvalidPayload 空 room_id / 空 text → 校验失败。
func TestVirtualCitySystemMessage_InvalidPayload(t *testing.T) {
	gs, _ := newSystemTestGame(t, true, fakeSystemRegistry{provider: &fakeSystemProvider{}})
	for _, payload := range []string{`{"room_id":"","text":"hi"}`, `{"room_id":"r1","text":"  "}`} {
		c := sysClient("u1")
		gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
			Payload: json.RawMessage(payload)})
		env := nextSysEnvelope(t, c)
		if sysEnvelopeCode(t, env) != errcode.ErrValidationFailed {
			t.Fatalf("payload %s: code=%d, want %d", payload, sysEnvelopeCode(t, env), errcode.ErrValidationFailed)
		}
	}
}

// TestVirtualCitySystemMessage_RoomNotFound 房间不存在 → 35001。
func TestVirtualCitySystemMessage_RoomNotFound(t *testing.T) {
	gs, _ := newSystemTestGame(t, true, fakeSystemRegistry{provider: &fakeSystemProvider{}})
	c := sysClient("u1")
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"nope","text":"hi"}`)})
	env := nextSysEnvelope(t, c)
	if sysEnvelopeCode(t, env) != errcode.ErrVirtualCityRoomNotFound {
		t.Fatalf("code=%d, want %d", sysEnvelopeCode(t, env), errcode.ErrVirtualCityRoomNotFound)
	}
}

// TestVirtualCitySystemMessage_AcceptedAndRun 正常一轮:accepted 帧 → 事件流
// (agent_start/agent_end)→ 会话持久化到 store。
func TestVirtualCitySystemMessage_AcceptedAndRun(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	c := sysClient("u1")
	gs.hub.SubscribeRoom(roomID, c)
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 7,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"查看城市状态"}`)})

	// 第一帧 = accepted。
	env := nextSysEnvelope(t, c)
	if env.Type != "game.virtual_city_system_accepted" || env.Seq != 7 {
		t.Fatalf("first frame = %s seq=%d, want accepted seq=7", env.Type, env.Seq)
	}
	// 后续事件:至少出现 agent_start 与 agent_end。
	sawStart, sawEnd := false, false
	deadline := time.After(3 * time.Second)
	for !sawEnd {
		select {
		case ev := <-c.send:
			if ev.Type != "game.virtual_city_system_event" {
				continue
			}
			m := sysPayloadMap(t, ev)
			switch m["kind"] {
			case citysystem.EventAgentStart:
				sawStart = true
			case citysystem.EventAgentEnd:
				sawEnd = true
			}
		case <-deadline:
			t.Fatalf("agent_end not seen (start=%v end=%v)", sawStart, sawEnd)
		}
	}
	if !sawStart {
		t.Fatal("agent_start not seen")
	}
	// 会话已持久化:user + assistant 两条。
	msgs := gs.SystemAgentHistory(roomID, "u1")
	if len(msgs) < 2 {
		t.Fatalf("history len = %d, want >= 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[len(msgs)-1].Role != "assistant" {
		t.Fatalf("history roles = %q..%q, want user..assistant", msgs[0].Role, msgs[len(msgs)-1].Role)
	}
}

// TestVirtualCitySystemMessage_Busy 并发限流:同一用户第二轮 → 35060。
func TestVirtualCitySystemMessage_Busy(t *testing.T) {
	block := make(chan struct{})
	provider := &fakeSystemProvider{block: block, calls: make(chan struct{}, 4)}
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	c := sysClient("u1")
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"first"}`)})
	if env := nextSysEnvelope(t, c); env.Type != "game.virtual_city_system_accepted" {
		t.Fatalf("first frame = %s, want accepted", env.Type)
	}
	// 等待 provider 被调用(说明 Run 已进入 LLM 调用)。
	select {
	case <-provider.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("provider not called")
	}
	// 第二轮 → busy。
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 2,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"second"}`)})
	env := nextSysEnvelope(t, c)
	if sysEnvelopeCode(t, env) != errcode.ErrVirtualCitySystemAgentBusy {
		t.Fatalf("code=%d, want %d", sysEnvelopeCode(t, env), errcode.ErrVirtualCitySystemAgentBusy)
	}
	close(block) // 放行第一轮
}

// TestVirtualCitySystemReset 清空会话 → cleared 帧 + 广播事件 + store 清空。
func TestVirtualCitySystemReset(t *testing.T) {
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: &fakeSystemProvider{}})
	c := sysClient("u1")
	gs.hub.SubscribeRoom(roomID, c)
	// 先造一条会话。
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"hi"}`)})
	nextSysEnvelope(t, c) // accepted
	deadline := time.Now().Add(3 * time.Second)
	for len(gs.SystemAgentHistory(roomID, "u1")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// 排空第一轮的事件流(agent_start / text / agent_end),避免与 reset 帧混淆。
	drainDeadline := time.After(2 * time.Second)
	drained := false
	for !drained {
		select {
		case ev := <-c.send:
			if ev.Type == "game.virtual_city_system_event" && sysPayloadMap(t, ev)["kind"] == citysystem.EventAgentEnd {
				drained = true
			}
		case <-drainDeadline:
			drained = true
		}
	}
	// reset。
	gs.handleVirtualCitySystemReset(c, Envelope{Type: "game.virtual_city_system_reset", Seq: 2,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `"}`)})
	env := nextSysEnvelope(t, c)
	if env.Type != "game.virtual_city_system_cleared" {
		t.Fatalf("reset frame = %s, want cleared", env.Type)
	}
	if len(gs.SystemAgentHistory(roomID, "u1")) != 0 {
		t.Fatal("history not cleared")
	}
	// 广播事件 kind=cleared。
	select {
	case ev := <-c.send:
		if ev.Type != "game.virtual_city_system_event" || sysPayloadMap(t, ev)["kind"] != citysystem.EventCleared {
			t.Fatalf("broadcast = %s kind=%v, want cleared", ev.Type, sysPayloadMap(t, ev)["kind"])
		}
	case <-time.After(time.Second):
		t.Fatal("cleared broadcast not seen")
	}
}

// TestVirtualCitySystemHistory_Empty 无会话 → 空数组(非 null)。
func TestVirtualCitySystemHistory_Empty(t *testing.T) {
	gs, roomID := newSystemTestGame(t, true, fakeSystemRegistry{provider: &fakeSystemProvider{}})
	c := sysClient("u1")
	gs.handleVirtualCitySystemHistory(c, Envelope{Type: "game.virtual_city_system_history", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `"}`)})
	env := nextSysEnvelope(t, c)
	if env.Type != "game.virtual_city_system_history" {
		t.Fatalf("frame = %s payload=%s, want history", env.Type, string(env.Payload))
	}
	raw, _ := json.Marshal(sysPayloadMap(t, env)["messages"])
	if string(raw) != "[]" {
		t.Fatalf("messages = %s, want []", raw)
	}
}
