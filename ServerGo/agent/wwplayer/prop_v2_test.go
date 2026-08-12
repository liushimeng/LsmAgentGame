// Package agent — prop_v2_test.go: 道具系统 v2 重设计的 agent 侧单测。
//
// 覆盖：
//   · use_prop 工具 schema 从 wwtypes.GameContext.wwtypes.PropSnapshot 动态生成。
//   · 空 snapshot → 不暴露 use_prop 工具（节省 tool slot）。
//   · propKeyToEmoji 本地副本与 werewolf 包一致。
//
// 2026-07-21 道具系统 v2 重设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"strings"
	"testing"
)

// TestAddUsePropTool_DynamicSchema 验证：snapshot 动态生成 tool schema 且空 snapshot 不暴露工具。
func TestAddUsePropTool_DynamicSchema(t *testing.T) {
	snaps := []wwtypes.PropSnapshot{
		{PropKey: "markdown_bomb", NameZh: "紧急公告", Price: 150, BaseHitRate: 30, IsAOE: false},
		{PropKey: "long_swear", NameZh: "长篇废话", Price: 250, BaseHitRate: 35, IsAOE: true},
	}
	gc := &wwtypes.GameContext{PropSnapshot: snaps}
	var capturedName string
	var capturedSchema map[string]any
	add := func(name, desc string, s map[string]any) {
		capturedName = name
		capturedSchema = s
	}
	addUsePropTool(add, gc, []int{1, 2, 3}, 0)
	if capturedName == "" {
		t.Fatal("非空 snapshot 应生成 use_prop 工具")
	}
	if capturedName != "use_prop" {
		t.Errorf("工具名应为 use_prop, got %s", capturedName)
	}
	// 动态生成的 description 应包含每个道具的名称。
	// 验证 enum 含两个 prop_key。
	props, ok := capturedSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema 应含 properties")
	}
	propID, ok := props["prop_id"].(map[string]any)
	if !ok {
		t.Fatal("properties 应含 prop_id")
	}
	enum, ok := propID["enum"].([]string)
	if !ok || len(enum) != 2 {
		t.Errorf("prop_id enum 应含 2 个 key, got %#v", propID["enum"])
	}
	if !strings.Contains(enum[0], "markdown_bomb") {
		t.Errorf("第一个 enum 应为 markdown_bomb, got %s", enum[0])
	}

	// 空 snapshot → 不应生成 use_prop 工具。
	capturedName = ""
	addUsePropTool(add, &wwtypes.GameContext{}, []int{1, 2, 3}, 0)
	if capturedName != "" {
		t.Error("空 snapshot 不应生成 use_prop 工具")
	}
}

// TestPropUserPromptBlock_DynamicList 验证 user prompt 中展示动态可购买清单。
func TestPropUserPromptBlock_DynamicList(t *testing.T) {
	gc := &wwtypes.GameContext{
		PropCooldownRemainingSec: 0,
		PropUsedThisGame:         0,
		PropMaxPerGame:           3,
		RoomPropBudget:           900,
		RoomPropBudgetUsed:       0,
		PropSnapshot: []wwtypes.PropSnapshot{
			{PropKey: "long_swear", NameZh: "长篇废话", Price: 250, BaseHitRate: 35, IsAOE: true},
		},
	}
	block := PropUserPromptBlock(gc)
	if !strings.Contains(block, "长篇废话") {
		t.Errorf("可购买清单应包含道具名，got: %s", block)
	}
	if !strings.Contains(block, "全局预算") {
		t.Error("应展示全局预算")
	}
}

// TestPropEffectSignalBlock_AllSignals 验证干扰信号段渲染。
func TestPropEffectSignalBlock_AllSignals(t *testing.T) {
	gc := &wwtypes.GameContext{
		EffectExpose:            true,
		EffectAttentionScatter:  true,
		ToolUseMaxOverride:      2,
		EffectTargetTwistSeat:   4,
		EffectForceEmotion:      "confused",
	}
	block := PropEffectSignalBlock(gc)
	if !strings.Contains(block, "可疑") {
		t.Error("EffectExpose 应渲染可疑提示")
	}
	if !strings.Contains(block, "直觉") {
		t.Error("EffectTargetTwistSeat 应渲染直觉引导")
	}
	if !strings.Contains(block, "5 号") {
		t.Error("EffectTargetTwistSeat=4 应渲染为 5 号(1-indexed)")
	}
	if !strings.Contains(block, "困惑") {
		t.Error("EffectForceEmotion=confused 应渲染困惑提示")
	}
	// 无任何信号时返回空串（显式置 -1 = 无引导，与 buildAgentContextLocked 的重置语义一致）。
	if PropEffectSignalBlock(&wwtypes.GameContext{EffectTargetTwistSeat: -1}) != "" {
		t.Error("无信号时应返回空串")
	}
}

// TestPropKeyToEmoji_LocalCopy 验证 agent 本地 emoji 副本不 panic 且覆盖常用 key。
func TestPropKeyToEmoji_LocalCopy(t *testing.T) {
	if propKeyToEmoji("long_swear") != "📜" {
		t.Errorf("long_swear emoji got %s", propKeyToEmoji("long_swear"))
	}
	if propKeyToEmoji("unknown_xyz") != "❓" {
		t.Error("未知 key 应返回默认 emoji")
	}
}

// TestSystemPromptV2 验证 v2 生存 prompt 含关键信息。
func TestSystemPromptV2(t *testing.T) {
	p := PropSystemPrompt()
	for _, want := range []string{"社会性死亡", "期望值", "共用本局道具池", "绝对不要使用道具"} {
		if !strings.Contains(p, want) {
			t.Errorf("PropSystemPrompt 应含 %q", want)
		}
	}
}
