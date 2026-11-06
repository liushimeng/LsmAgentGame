// Package ws — game_service_virtual_city_system_error_room_test.go: 批次87
// P1-2/P2-8「City-System 错误帧带 room_id」的 WS 层单测(2026-11-06)。
//
// 背景:通用 sendError 只发 {code,message} ⇒ 前端收到 game.error 时无法
// 判断「是哪个房间/哪次操作被拒」。City-System 三个 handler 的错误都发生在
// 某个具体房间内 ⇒ 错误帧补 room_id(帧结构只**增**字段,旧前端忽略即可)。
//
// 覆盖:message / reset / history 三个 handler 的错误路径均带 room_id,
// 且 room_id 等于请求里的房间号。
package ws

import (
	"encoding/json"
	"testing"

	"LsmAgentGame/errcode"
)

// sysErrorRoomID 取错误帧载荷里的 room_id。
func sysErrorRoomID(t *testing.T, env Envelope) string {
	t.Helper()
	if env.Type != "game.error" {
		t.Fatalf("frame type = %s, want game.error (payload=%s)", env.Type, string(env.Payload))
	}
	m := sysPayloadMap(t, env)
	raw, ok := m["room_id"]
	if !ok {
		t.Fatalf("error payload missing room_id: %s", string(env.Payload))
	}
	s, ok := raw.(string)
	if !ok {
		t.Fatalf("room_id type = %T, want string", raw)
	}
	return s
}

// TestCitySystemErrorFrame_CarriesRoomID 三个 handler 的错误帧都带 room_id。
func TestCitySystemErrorFrame_CarriesRoomID(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, _ := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})

	// 1) message:房间不存在 → 35001 + room_id。
	c2 := sysClient("u-err2")
	gs.handleVirtualCitySystemMessage(c2, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"no-such-room","text":"hi"}`)})
	env := nextSysEnvelope(t, c2)
	if code := sysEnvelopeCode(t, env); code != errcode.ErrVirtualCityRoomNotFound {
		t.Fatalf("message: code = %d, want %d", code, errcode.ErrVirtualCityRoomNotFound)
	}
	if got := sysErrorRoomID(t, env); got != "no-such-room" {
		t.Fatalf("message: room_id = %q, want %q", got, "no-such-room")
	}

	// 2) reset:房间不存在 → 35001 + room_id。
	c3 := sysClient("u-err3")
	gs.handleVirtualCitySystemReset(c3, Envelope{Type: "game.virtual_city_system_reset", Seq: 2,
		Payload: json.RawMessage(`{"room_id":"no-such-room2"}`)})
	env = nextSysEnvelope(t, c3)
	if code := sysEnvelopeCode(t, env); code != errcode.ErrVirtualCityRoomNotFound {
		t.Fatalf("reset: code = %d, want %d", code, errcode.ErrVirtualCityRoomNotFound)
	}
	if got := sysErrorRoomID(t, env); got != "no-such-room2" {
		t.Fatalf("reset: room_id = %q, want %q", got, "no-such-room2")
	}

	// 3) history:房间不存在 → 35001 + room_id。
	c4 := sysClient("u-err4")
	gs.handleVirtualCitySystemHistory(c4, Envelope{Type: "game.virtual_city_system_history", Seq: 3,
		Payload: json.RawMessage(`{"room_id":"no-such-room3"}`)})
	env = nextSysEnvelope(t, c4)
	if code := sysEnvelopeCode(t, env); code != errcode.ErrVirtualCityRoomNotFound {
		t.Fatalf("history: code = %d, want %d", code, errcode.ErrVirtualCityRoomNotFound)
	}
	if got := sysErrorRoomID(t, env); got != "no-such-room3" {
		t.Fatalf("history: room_id = %q, want %q", got, "no-such-room3")
	}
}

// TestCitySystemErrorFrame_DisabledCarriesRoomID 停用(35059)时同样带
// room_id —— 前端据此把「停用」状态归属到正确房间。
func TestCitySystemErrorFrame_DisabledCarriesRoomID(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, roomID := newSystemTestGame(t, false, fakeSystemRegistry{provider: provider})
	c := sysClient("u-err5")
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"` + roomID + `","text":"hi"}`)})
	env := nextSysEnvelope(t, c)
	if code := sysEnvelopeCode(t, env); code != errcode.ErrVirtualCitySystemAgentDisabled {
		t.Fatalf("code = %d, want %d", code, errcode.ErrVirtualCitySystemAgentDisabled)
	}
	if got := sysErrorRoomID(t, env); got != roomID {
		t.Fatalf("room_id = %q, want %q", got, roomID)
	}
}

// TestCitySystemErrorFrame_InvalidPayloadKeepsRoomID 载荷非法时若 room_id
// 本身可解析,错误帧仍应带上它(前端才能归属房间)。
func TestCitySystemErrorFrame_InvalidPayloadKeepsRoomID(t *testing.T) {
	provider := &fakeSystemProvider{}
	gs, _ := newSystemTestGame(t, true, fakeSystemRegistry{provider: provider})
	c := sysClient("u-err6")
	gs.handleVirtualCitySystemMessage(c, Envelope{Type: "game.virtual_city_system_message", Seq: 1,
		Payload: json.RawMessage(`{"room_id":"room-xyz","text":"   "}`)})
	env := nextSysEnvelope(t, c)
	if code := sysEnvelopeCode(t, env); code != errcode.ErrValidationFailed {
		t.Fatalf("code = %d, want %d", code, errcode.ErrValidationFailed)
	}
	if got := sysErrorRoomID(t, env); got != "room-xyz" {
		t.Fatalf("room_id = %q, want %q", got, "room-xyz")
	}
}

// TestCitySystemErrorFrame_OtherGamesUnchanged 其它游戏的 sendError 调用点
// **不带** room_id(契约零变更,避免影响既有前端)。
func TestCitySystemErrorFrame_OtherGamesUnchanged(t *testing.T) {
	gs := &GameService{hub: NewHub()}
	c := sysClient("u-err7")
	gs.sendError(c, 1, errcode.ErrValidationFailed, "boom")
	env := nextSysEnvelope(t, c)
	m := sysPayloadMap(t, env)
	if _, ok := m["room_id"]; ok {
		t.Fatalf("generic sendError must not carry room_id: %s", string(env.Payload))
	}
}
