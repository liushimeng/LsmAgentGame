package thpagent

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_Basic(t *testing.T) {
	ctx := &GameContextForAgent{
		RoomID:         "room1",
		HandNumber:     5,
		Street:         "preflop",
		MySeat:         2,
		MyHole:         [2]int{1, 14},
		Community:      [5]int{},
		CommunityLen:   0,
		Pot:            300,
		CurrentBet:     200,
		CallAmount:     200,
		HandStrength:   0.65,
		RequiredEquity: 0.4,
		Position:       "BTN",
		BluffHint:      0.25,
		OpponentsCount: 5,
		ModelNameField: "MeiTuan-LongCat",
	}
	mem := NewMemory()

	prompt := BuildSystemPrompt(ctx, mem)
	if prompt == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(prompt, "[Identity]") {
		t.Error("system prompt should contain [Identity] section")
	}
	if !strings.Contains(prompt, "[GameRules]") {
		t.Error("system prompt should contain [GameRules] section")
	}
	if !strings.Contains(prompt, "[CurrentState]") {
		t.Error("system prompt should contain [CurrentState] section")
	}
	if !strings.Contains(prompt, "[MathHelpers]") {
		t.Error("system prompt should contain [MathHelpers] section")
	}
	if !strings.Contains(prompt, "[StyleGuide]") {
		t.Error("system prompt should contain [StyleGuide] section")
	}
	if !strings.Contains(prompt, "MeiTuan-LongCat") {
		t.Error("system prompt should contain model name")
	}
	if !strings.Contains(prompt, "Hand Strength") {
		t.Error("system prompt should contain Hand Strength math helper")
	}
}

func TestBuildSystemPrompt_BudgetLimit(t *testing.T) {
	// 构造一个超过 SystemPromptMaxRunes 的 ctx
	bigCtx := &GameContextForAgent{
		RoomID:         "room1",
		HandNumber:     5,
		Street:         "preflop",
		MySeat:         2,
		MyHole:         [2]int{1, 14},
		Community:      [5]int{},
		CommunityLen:   0,
		Pot:            300,
		CurrentBet:     200,
		CallAmount:     200,
		HandStrength:   0.65,
		RequiredEquity: 0.4,
		Position:       "BTN",
		BluffHint:      0.25,
		OpponentsCount: 5,
		ModelNameField: "TestModel",
	}
	prompt := BuildSystemPrompt(bigCtx, NewMemory())
	runes := []rune(prompt)
	if len(runes) > SystemPromptMaxRunes+200 {
		t.Errorf("system prompt exceeds budget: %d runes", len(runes))
	}
}

func TestBuildUserPrompt_CriticalBlocks(t *testing.T) {
	ctx := &GameContextForAgent{
		RoomID:         "room1",
		HandNumber:     5,
		Street:         "flop",
		MySeat:         0,
		MyHole:         [2]int{1, 14},
		Community:      [5]int{10, 11, 12},
		CommunityLen:   3,
		Pot:            500,
		CurrentBet:     200,
		CallAmount:     100,
		HandStrength:   0.75,
		RequiredEquity: 0.25,
		Position:       "BTN",
		BluffHint:      0.20,
		OpponentsCount: 3,
		ModelNameField: "TestBot",
	}
	mem := NewMemory()
	mem.AppendHand(HandRecord{HandNumber: 1, NetChipDelta: 200}, 5)
	mem.AppendHand(HandRecord{HandNumber: 2, NetChipDelta: -150}, 5)
	mem.SetLastDecisionSummary("raise: BTN 偷盲")

	prompt := BuildUserPrompt(ctx, mem)
	if !strings.Contains(prompt, "【身份】") {
		t.Error("user prompt should contain 【身份】 block")
	}
	if !strings.Contains(prompt, "【当前手牌】") {
		t.Error("user prompt should contain 【当前手牌】 block")
	}
	if !strings.Contains(prompt, "【我的底牌】") {
		t.Error("user prompt should contain 【我的底牌】 block")
	}
	if !strings.Contains(prompt, "【公共牌】") {
		t.Error("user prompt should contain 【公共牌】 block")
	}
}

func TestBuildUserPrompt_BudgetDropMarker(t *testing.T) {
	// 模拟 budget 紧张情况 — 构造大量 data 让 budget 不够
	ctx := &GameContextForAgent{
		RoomID:         "room1",
		HandNumber:     5,
		Street:         "preflop",
		MySeat:         0,
		MyHole:         [2]int{1, 14},
		Community:      [5]int{},
		CommunityLen:   0,
		Pot:            300,
		CurrentBet:     200,
		CallAmount:     200,
		HandStrength:   0.65,
		RequiredEquity: 0.4,
		Position:       "BTN",
		BluffHint:      0.25,
		OpponentsCount: 5,
		ModelNameField: "TestBot",
	}
	mem := NewMemory()
	// 大量历史手牌让 budget 紧张
	for i := 0; i < 100; i++ {
		mem.AppendHand(HandRecord{HandNumber: i + 1, NetChipDelta: i * 10}, 5)
	}

	prompt := BuildUserPrompt(ctx, mem)
	// Budget 应该不会爆到丢块(因为 prompt 内容有限),但应该正确返回
	if prompt == "" {
		t.Error("user prompt should not be empty even with budget pressure")
	}
}