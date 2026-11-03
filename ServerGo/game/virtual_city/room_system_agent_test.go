// Package virtual_city — room_system_agent_test.go: 批次81 只读桥单测
// (SystemSummaryText / WorldEventLogView / CitySystemLine)。
//
// 覆盖(契约 §7.1):各 scope 非空、不 panic(World 为 nil / 各子系统 nil)。
package virtual_city

import (
	"strings"
	"testing"
)

// TestSystemSummaryText_Scopes 已开局房间:每个 scope 都返回非空可读文本。
func TestSystemSummaryText_Scopes(t *testing.T) {
	r := newScenarioTestRoom(t, "sys-summary", 20261103)
	scopes := []string{"", "overview", "market", "labor", "treasury", "election", "all", "unknown-scope"}
	for _, scope := range scopes {
		got := r.SystemSummaryText(scope)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("scope %q returned empty text", scope)
		}
		if !strings.Contains(got, "城市状态") {
			t.Fatalf("scope %q missing header: %q", scope, got)
		}
	}
	// overview/all 必须含活动场景段。
	for _, scope := range []string{"overview", "all"} {
		if !strings.Contains(r.SystemSummaryText(scope), "活动场景") {
			t.Fatalf("scope %q missing active scenarios: %q", scope, r.SystemSummaryText(scope))
		}
	}
}

// TestSystemSummaryText_NilWorld 未开局房间(World 为 nil)不 panic,返回提示。
func TestSystemSummaryText_NilWorld(t *testing.T) {
	r := NewVirtualCityRoom("sys-nil", 3000, 1, 4)
	for _, scope := range []string{"", "overview", "market", "labor", "treasury", "election", "all"} {
		got := r.SystemSummaryText(scope)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("nil-world scope %q returned empty", scope)
		}
	}
}

// TestSystemSummaryText_NilSubsystems World 存在但各子系统为 nil 时不 panic。
func TestSystemSummaryText_NilSubsystems(t *testing.T) {
	r := NewVirtualCityRoom("sys-nil-sub", 3000, 1, 4)
	r.World = &World{Month: 3}
	scopes := []string{"overview", "market", "labor", "treasury", "election", "all"}
	for _, scope := range scopes {
		if got := r.SystemSummaryText(scope); strings.TrimSpace(got) == "" {
			t.Fatalf("nil-subsystem scope %q returned empty", scope)
		}
	}
}

// TestWorldEventLogView_NilWorld 未开局返回空切片(非 nil,JSON 序列化为 [])。
func TestWorldEventLogView_NilWorld(t *testing.T) {
	r := NewVirtualCityRoom("sys-we-nil", 3000, 1, 4)
	got := r.WorldEventLogView()
	if got == nil {
		t.Fatal("WorldEventLogView must return non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("nil world log len = %d, want 0", len(got))
	}
}

// TestCitySystemLine 房间级线路策略 set/get(与批次79 CityChromeLine 同构)。
func TestCitySystemLine(t *testing.T) {
	r := NewVirtualCityRoom("sys-line", 3000, 1, 4)
	if got := r.CitySystemLine(); got != "" {
		t.Fatalf("default line = %q, want empty", got)
	}
	r.SetCitySystemLine("  random  ")
	if got := r.CitySystemLine(); got != "random" {
		t.Fatalf("line = %q, want random (trimmed)", got)
	}
	r.SetCitySystemLine("DeepSeek-model")
	if got := r.CitySystemLine(); got != "DeepSeek-model" {
		t.Fatalf("line = %q, want DeepSeek-model", got)
	}
}
