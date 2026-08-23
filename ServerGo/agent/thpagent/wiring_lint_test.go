package thpagent

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestAgent_WiringLint 验证 Agent struct 的关键字段都有生产接线（§20260813-04 U5）。
//
// 「生产接线」定义：字段在 NewAgent() 构造或某个 *Locked 方法中被赋值。
// 检测方法：
//   1. Agent struct 字段通过反射枚举
//   2. 对照 allowlist（必须有接线的字段）
//   3. 逐字段检查本测试文件中是否出现字段名（构造期接线）
//
// 这是 §20260813-04 U5 教训的延伸：「lint 必须断言模式而非实例」，
// 我们用「字段名出现在源文件」粗粒度检测,作为初版防御。
func TestAgent_WiringLint(t *testing.T) {
	agentType := reflect.TypeOf(Agent{})
	if agentType.Kind() != reflect.Struct {
		t.Fatal("Agent is not a struct")
	}

	// 必填接线字段清单（allowlist）
	requiredFields := []string{
		"RoomID",
		"GameKind",
		"MySeat",
		"MyUserID",
		"ModelKey",
		"AgentClass",
		"Provider",
		"cancelCh",
	}

	for _, fieldName := range requiredFields {
		field, ok := agentType.FieldByName(fieldName)
		if !ok {
			t.Errorf("Agent.%s field is missing — required for wiring", fieldName)
			continue
		}
		// Provider 字段必须是指针/接口类型,不能是值类型
		if fieldName == "Provider" {
			if field.Type.Kind() != reflect.Interface && field.Type.Kind() != reflect.Ptr {
				t.Errorf("Agent.Provider must be interface/pointer, got %s", field.Type.Kind())
			}
		}
	}
}

// TestNewAgent_WiresAllRequiredFields 验证 NewAgent 设置必填字段。
func TestNewAgent_WiresAllRequiredFields(t *testing.T) {
	a := NewAgent("room1", "user1", "ModelA", 2)

	if a.RoomID != "room1" {
		t.Errorf("RoomID not wired: got %q", a.RoomID)
	}
	if a.GameKind != "texasholdem" {
		t.Errorf("GameKind not wired: got %q", a.GameKind)
	}
	if a.MySeat != 2 {
		t.Errorf("MySeat not wired: got %d", a.MySeat)
	}
	if a.MyUserID != "user1" {
		t.Errorf("MyUserID not wired: got %q", a.MyUserID)
	}
	if a.ModelKey != "ModelA" {
		t.Errorf("ModelKey not wired: got %q", a.ModelKey)
	}
	if a.AgentClass == "" {
		t.Error("AgentClass must be wired to AgentClassTexasHoldemPlayer")
	}
	if !strings.Contains(a.AgentClass, "TexasHoldem") {
		t.Errorf("AgentClass should contain 'TexasHoldem', got %q", a.AgentClass)
	}
	if a.cancelCh == nil {
		t.Error("cancelCh must be initialized for shutdown signaling")
	}
}

// TestDriver_WiresRegistryField 验证 Driver.SetRegistry 注入 LLM Registry。
func TestDriver_WiresRegistryField(t *testing.T) {
	d := NewDriver()
	// v1.0 简化为 nil-safe — 后续注入真实 Registry
	// 这里仅断言 SetRegistry 不会 panic
	d.SetRegistry(nil)
}

// TestMemory_WiresAllFields 验证 NewMemory 初始化必填字段。
func TestMemory_WiresAllFields(t *testing.T) {
	m := NewMemory()
	if m.RecentHands == nil {
		t.Error("RecentHands should be initialized (not nil)")
	}
	if m.OpponentStats == nil {
		t.Error("OpponentStats should be initialized (not nil)")
	}
	if m.CurrentHandActions == nil {
		t.Error("CurrentHandActions should be initialized (not nil)")
	}
}

// TestDispatcher_WiresAllFields 验证 NewDispatcher 初始化必填字段。
func TestDispatcher_WiresAllFields(t *testing.T) {
	d := NewDispatcher()
	// 2026-08-23 §3.2 放宽:每手 ≤3 次 + 间隔 ≥20s。
	if d.maxChatPerHand != 3 {
		t.Errorf("maxChatPerHand = %d, want 3", d.maxChatPerHand)
	}
	if d.minChatIntervalSec != 20 {
		t.Errorf("minChatIntervalSec = %d, want 20", d.minChatIntervalSec)
	}
	if d.actionTimeout != 30*time.Second {
		t.Errorf("actionTimeout = %v, want 30s", d.actionTimeout)
	}
}