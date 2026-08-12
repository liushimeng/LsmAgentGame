// Package ws — prop_frame_validation_test.go: 验证 game.werewolf_use_prop 帧
// 的严格 JSON 校验逻辑(§84b DisallowUnknownFields)与字段边界。
//
// 2026-07-21 §道具系统:仅测试 payload 解码与字段边界,不依赖 hub / manager /
// game state(避免引入沉重 fixtures);端到端逻辑在 game_service 集成测试覆盖。
package ws

import (
	"encoding/json"
	"testing"
)

// TestDecodeWerewolfUseProp_StrictOK 验证正常 payload 可通过严格解码。
func TestDecodeWerewolfUseProp_StrictOK(t *testing.T) {
	payload := json.RawMessage(`{"room_id":"abc","prop_key":"reveal","target":3,"payload":"hi"}`)
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(payload, &req); err != nil {
		t.Fatalf("strict decode failed on valid payload: %v", err)
	}
	if req.RoomID != "abc" || req.PropKey != "reveal" || req.Target != 3 || req.Payload != "hi" {
		t.Errorf("decoded values mismatch: %+v", req)
	}
}

// TestDecodeWerewolfUseProp_StrictRejectsUnknownField 验证拼错字段会被严格拒绝(§84b)。
// 场景:前端把 prop_key 写成 propID,严格解码必须返回错误而非静默接受空 prop_key。
func TestDecodeWerewolfUseProp_StrictRejectsUnknownField(t *testing.T) {
	payload := json.RawMessage(`{"room_id":"abc","propID":"reveal"}`)
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(payload, &req); err == nil {
		t.Fatalf("strict decode should reject unknown field propID")
	}
}

// TestDecodeWerewolfUseProp_TrailingDataRejected 验证同一帧内 JSON 拼接
// (trailing data)会被解码器拒绝(防误把两帧粘成一帧)。
func TestDecodeWerewolfUseProp_TrailingDataRejected(t *testing.T) {
	payload := json.RawMessage(`{"room_id":"abc","prop_key":"x"}{"extra":"yes"}`)
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(payload, &req); err == nil {
		t.Fatalf("strict decode should reject trailing data")
	}
}

// TestDecodeWerewolfUseProp_MissingRequired 验证缺少 room_id / prop_key 时
// 严格解码成功,但调用方在后续 req.RoomID == "" 守卫处判失败(语义正确)。
func TestDecodeWerewolfUseProp_MissingRequired(t *testing.T) {
	payload := json.RawMessage(`{"prop_key":"x"}`)
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(payload, &req); err != nil {
		t.Fatalf("decode should succeed even with missing room_id (caller validates): %v", err)
	}
	if req.RoomID != "" || req.PropKey != "x" {
		t.Errorf("expected room_id='', prop_key='x', got %+v", req)
	}
}

// TestDecodeWerewolfUseProp_DefaultsOK 验证 target=-1 (AOE) 与省略 payload 字段
// 都能正确解码为默认值。
func TestDecodeWerewolfUseProp_DefaultsOK(t *testing.T) {
	payload := json.RawMessage(`{"room_id":"abc","prop_key":"shield","target":-1}`)
	var req struct {
		RoomID  string `json:"room_id"`
		PropKey string `json:"prop_key"`
		Target  int    `json:"target"`
		Payload string `json:"payload,omitempty"`
	}
	if err := decodeJSONStrictFromBytes(payload, &req); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if req.Target != -1 {
		t.Errorf("target should be -1 (AOE), got %d", req.Target)
	}
	if req.Payload != "" {
		t.Errorf("payload should be empty when omitted, got %q", req.Payload)
	}
}
