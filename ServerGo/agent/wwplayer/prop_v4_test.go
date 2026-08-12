// Package agent — prop_v4_test.go: v4 新增道具相关 prompt block 单测。
//
// 覆盖：
//  1. WolfPackPromptBlock — 仅狼 bot + 有快照时渲染；非狼 / 无快照返回空
//  2. EconTierFeedbackBlock — 各档位渲染
//  3. addWolfWhisperTool 工具挂载规则（faction=wolf + WolfTeammateSeat>=0 才挂）
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"strings"
	"testing"
)

func TestWolfPackPromptBlock_NonWolfReturnsEmpty(t *testing.T) {
	gc := &wwtypes.GameContext{Faction: "good", WolfPackSnapshot: []wwtypes.WolfPackMsg{
		{FromSeat: 1, Text: "刀 7 号"},
	}}
	if got := WolfPackPromptBlock(gc); got != "" {
		t.Fatalf("good faction should return empty, got: %s", got)
	}
}

func TestWolfPackPromptBlock_EmptySnapshotReturnsEmpty(t *testing.T) {
	gc := &wwtypes.GameContext{Faction: "wolf"}
	if got := WolfPackPromptBlock(gc); got != "" {
		t.Fatalf("empty snapshot should return empty, got: %s", got)
	}
}

func TestWolfPackPromptBlock_WolfWithSnapshot(t *testing.T) {
	gc := &wwtypes.GameContext{Faction: "wolf", WolfPackSnapshot: []wwtypes.WolfPackMsg{
		{FromSeat: 1, Text: "今晚刀 7 号"},
		{FromSeat: 5, Text: "我先悍跳预言家"},
	}}
	got := WolfPackPromptBlock(gc)
	if got == "" {
		t.Fatal("wolf with snapshot should render prompt block")
	}
	if !strings.Contains(got, "狼小队留言") {
		t.Fatalf("should contain block title, got: %s", got)
	}
	if !strings.Contains(got, "今晚刀 7 号") {
		t.Fatalf("should contain seat-1 message, got: %s", got)
	}
	if !strings.Contains(got, "6号") {
		t.Fatalf("should contain seat-5 1-indexed label (5+1=6号), got: %s", got)
	}
}

func TestEconTierFeedbackBlock_EmptyGC(t *testing.T) {
	if got := EconTierFeedbackBlock(nil); got != "" {
		t.Fatalf("nil gc should return empty, got: %s", got)
	}
}

func TestEconTierFeedbackBlock_NoPropSnapshotEmpty(t *testing.T) {
	gc := &wwtypes.GameContext{EconTier: "danger"}
	if got := EconTierFeedbackBlock(gc); got != "" {
		t.Fatalf("without wwtypes.PropSnapshot should return empty, got: %s", got)
	}
}

func TestEconTierFeedbackBlock_Health(t *testing.T) {
	gc := &wwtypes.GameContext{EconTier: "health", PropSnapshot: []wwtypes.PropSnapshot{{PropKey: "markdown_bomb"}}}
	got := EconTierFeedbackBlock(gc)
	if !strings.Contains(got, "🟢 Health") {
		t.Fatalf("Health should show green badge, got: %s", got)
	}
	if !strings.Contains(got, "30%") {
		t.Fatalf("Health absorb rate should be 30%%, got: %s", got)
	}
}

func TestEconTierFeedbackBlock_Caution(t *testing.T) {
	gc := &wwtypes.GameContext{EconTier: "caution", PropSnapshot: []wwtypes.PropSnapshot{{PropKey: "markdown_bomb"}}}
	got := EconTierFeedbackBlock(gc)
	if !strings.Contains(got, "🟡 Caution") {
		t.Fatalf("Caution should show yellow badge, got: %s", got)
	}
	if !strings.Contains(got, "40%") {
		t.Fatalf("Caution absorb rate should be 40%%, got: %s", got)
	}
}

func TestEconTierFeedbackBlock_Danger(t *testing.T) {
	gc := &wwtypes.GameContext{EconTier: "danger", PropSnapshot: []wwtypes.PropSnapshot{{PropKey: "markdown_bomb"}}}
	got := EconTierFeedbackBlock(gc)
	// v5: Danger 档改 🟠 + 销毁率 45%(v4 是 🔴 + 50%)。
	if !strings.Contains(got, "🟠 Danger") {
		t.Fatalf("Danger should show orange badge (v5), got: %s", got)
	}
	if !strings.Contains(got, "45%") {
		t.Fatalf("Danger absorb rate should be 45%% (v5), got: %s", got)
	}
}

func TestEconTierFeedbackBlock_EmptyTierDefaultsHealth(t *testing.T) {
	gc := &wwtypes.GameContext{EconTier: "", PropSnapshot: []wwtypes.PropSnapshot{{PropKey: "markdown_bomb"}}}
	got := EconTierFeedbackBlock(gc)
	if !strings.Contains(got, "30%") {
		t.Fatalf("empty tier should default to Health (30%%), got: %s", got)
	}
}

// TestAddWolfWhisperTool_Gating 验证 wolf_whisper 工具挂载规则：
//   - nil gc → 不挂载
//   - faction=good → 不挂载
//   - faction=wolf + WolfTeammateSeat=-1 → 不挂载
//   - faction=wolf + WolfTeammateSeat>=0 → 挂载
func TestAddWolfWhisperTool_Gating(t *testing.T) {
	tools := make([]string, 0)
	add := func(name, desc string, _ map[string]any) {
		tools = append(tools, name)
	}
	// Case 1: nil gc
	tools = tools[:0]
	addWolfWhisperTool(add, nil)
	if len(tools) != 0 {
		t.Fatalf("nil gc should not mount wolf_whisper, got: %v", tools)
	}

	// Case 2: good faction
	tools = tools[:0]
	addWolfWhisperTool(add, &wwtypes.GameContext{Faction: "good"})
	if len(tools) != 0 {
		t.Fatalf("good faction should not mount wolf_whisper, got: %v", tools)
	}

	// Case 3: wolf but no teammate
	tools = tools[:0]
	addWolfWhisperTool(add, &wwtypes.GameContext{Faction: "wolf", WolfTeammateSeat: -1})
	if len(tools) != 0 {
		t.Fatalf("wolf without teammate should not mount wolf_whisper, got: %v", tools)
	}

	// Case 4: wolf with teammate
	tools = tools[:0]
	addWolfWhisperTool(add, &wwtypes.GameContext{Faction: "wolf", WolfTeammateSeat: 3})
	if len(tools) != 1 || tools[0] != "wolf_whisper" {
		t.Fatalf("wolf with teammate should mount wolf_whisper, got: %v", tools)
	}
}