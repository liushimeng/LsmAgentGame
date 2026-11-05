// Package citybus — bus_describe_test.go: 批次84 §4.3.1 的 Describe /
// RegisterWith / RegisterRoomWith 单测(2026-11-05)。
//
// 覆盖:Describe 类级 + 房间级目录 / 元信息登记 / 房间隔离 /
// 旧 Register 零回归 / 注销后元信息清理。
package citybus

import (
	"strings"
	"testing"
)

// ─────────────────────────── 批次84 §4.3.1:Describe / RegisterWith ───────────────────────────

// TestDescribe_ClassAndRoom Describe 覆盖类级 + 房间级两类;排序稳定。
func TestDescribe_ClassAndRoom(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.RegisterWith("Beta", []string{"message"}, "乙", echoHandler("b", nil)); err != nil {
		t.Fatalf("register Beta: %v", err)
	}
	if err := h.RegisterWith("Alpha", []string{"collect", "message"}, "甲", echoHandler("a", nil)); err != nil {
		t.Fatalf("register Alpha: %v", err)
	}
	if err := h.RegisterRoomWith("R1", "RoomOnly", []string{"run_display"}, "丙", echoHandler("r", nil)); err != nil {
		t.Fatalf("register RoomOnly: %v", err)
	}
	got := h.Describe("R1")
	if len(got) != 3 {
		t.Fatalf("Describe len = %d, want 3", len(got))
	}
	// 类级在前、房间级在后;各自按 ClassName 升序。
	want := []AgentDescriptor{
		{ClassName: "Alpha", Scope: ScopeClass, MessageTypes: []string{"collect", "message"}, Description: "甲"},
		{ClassName: "Beta", Scope: ScopeClass, MessageTypes: []string{"message"}, Description: "乙"},
		{ClassName: "RoomOnly", Scope: ScopeRoom, RoomID: "R1", MessageTypes: []string{"run_display"}, Description: "丙"},
	}
	for i, w := range want {
		if got[i].ClassName != w.ClassName || got[i].Scope != w.Scope || got[i].RoomID != w.RoomID {
			t.Fatalf("Describe[%d] = %+v, want %+v", i, got[i], w)
		}
		if got[i].Description != w.Description {
			t.Fatalf("Describe[%d].Description = %q, want %q", i, got[i].Description, w.Description)
		}
		if strings.Join(got[i].MessageTypes, ",") != strings.Join(w.MessageTypes, ",") {
			t.Fatalf("Describe[%d].MessageTypes = %v, want %v", i, got[i].MessageTypes, w.MessageTypes)
		}
	}
	// 房间隔离:其它房间只看类级 + 该房间的房间级。
	got2 := h.Describe("R2")
	if len(got2) != 2 {
		t.Fatalf("Describe(R2) len = %d, want 2 (类级 only)", len(got2))
	}
	for _, d := range got2 {
		if d.Scope != ScopeClass {
			t.Fatalf("R2 must not see R1's room-level agent: %+v", d)
		}
	}
}

// TestDescribe_WithoutMeta 旧 Register / RegisterRoom 注册的 Agent:
// MessageTypes 为空、Description 为空(不影响发现)。
func TestDescribe_WithoutMeta(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.Register("A", echoHandler("a", nil)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := h.RegisterRoom("R1", "B", echoHandler("b", nil)); err != nil {
		t.Fatalf("register room: %v", err)
	}
	got := h.Describe("R1")
	if len(got) != 2 {
		t.Fatalf("Describe len = %d, want 2", len(got))
	}
	for _, d := range got {
		if d.MessageTypes != nil {
			t.Errorf("MessageTypes = %v, want nil", d.MessageTypes)
		}
		if d.Description != "" {
			t.Errorf("Description = %q, want empty", d.Description)
		}
	}
}

// TestRegisterWith_ZeroRegression RegisterWith 不影响 Agents() 既有行为:
// Agents() 仍只返回**类级**且排序;重复注册仍报错。
func TestRegisterWith_ZeroRegression(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.RegisterWith("Zeta", []string{"message"}, "z", echoHandler("z", nil)); err != nil {
		t.Fatalf("register Zeta: %v", err)
	}
	if err := h.RegisterWith("Alpha", nil, "", echoHandler("a", nil)); err != nil {
		t.Fatalf("register Alpha: %v", err)
	}
	// Agents() 只含类级,排序。
	got := h.Agents()
	want := []string{"Alpha", "Zeta"}
	if len(got) != len(want) {
		t.Fatalf("Agents() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Agents() = %v, want %v", got, want)
		}
	}
	// 重复注册 ⇒ error(与 Register 一致)。
	if err := h.RegisterWith("Alpha", nil, "", echoHandler("x", nil)); err == nil {
		t.Fatal("duplicate RegisterWith must fail")
	}
	if err := h.RegisterRoomWith("R1", "RoomOnly", nil, "", echoHandler("r", nil)); err != nil {
		t.Fatalf("register room: %v", err)
	}
	if err := h.RegisterRoomWith("R1", "RoomOnly", nil, "", echoHandler("r2", nil)); err == nil {
		t.Fatal("duplicate RegisterRoomWith must fail")
	}
	// UnregisterRoom 清理元信息(再注册成功)。
	h.UnregisterRoom("R1", "RoomOnly")
	if err := h.RegisterRoomWith("R1", "RoomOnly", []string{"message"}, "again", echoHandler("r3", nil)); err != nil {
		t.Fatalf("re-register after unregister: %v", err)
	}
	got2 := h.Describe("R1")
	found := false
	for _, d := range got2 {
		if d.ClassName == "RoomOnly" && d.Scope == ScopeRoom && d.Description == "again" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Describe after re-register = %+v", got2)
	}
}

// TestDescribe_NilRoomID 空 roomID ⇒ 只返回类级(不 panic)。
func TestDescribe_NilRoomID(t *testing.T) {
	h := New()
	defer h.Close()
	if err := h.RegisterWith("A", []string{"message"}, "甲", echoHandler("a", nil)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := h.RegisterRoom("R1", "B", echoHandler("b", nil)); err != nil {
		t.Fatalf("register room: %v", err)
	}
	got := h.Describe("")
	if len(got) != 1 || got[0].ClassName != "A" {
		t.Fatalf("Describe(\"\") = %+v, want only class-level A", got)
	}
}
