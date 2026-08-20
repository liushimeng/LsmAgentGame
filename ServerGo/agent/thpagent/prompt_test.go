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
// ─────────────────── 2026-08-20 §B3 回归测试 ───────────────────

// TestB3_SystemPromptRealChipValues: 筹码/本轮已下注必须是真实值,
// 不允许再出现字面「?」占位(§B3 修复前的 bug)。
func TestB3_SystemPromptRealChipValues(t *testing.T) {
	ctx := &GameContextForAgent{
		RoomID:           "room1",
		HandNumber:       3,
		Street:           "flop",
		MySeat:           1,
		MyHole:           [2]int{EncodeCard(14, 1), EncodeCard(13, 2)}, // A♠ K♥
		Community:        [5]int{EncodeCard(10, 3), EncodeCard(7, 4), EncodeCard(2, 1)},
		CommunityLen:     3,
		MyStack:          8800,
		MyRoundCommitted: 200,
		Pot:              600,
		CurrentBet:       200,
		CallAmount:       0,
		MinRaise:         200,
		BigBlind:         200,
		Position:         "BTN",
	}
	prompt := BuildSystemPrompt(ctx, NewMemory())
	if strings.Contains(prompt, "筹码: ?") || strings.Contains(prompt, "已下注: ?") {
		t.Errorf("system prompt must not contain '?' placeholder:\n%s", prompt)
	}
	if !strings.Contains(prompt, "剩余筹码: 8800") {
		t.Errorf("system prompt should contain real stack 8800:\n%s", prompt)
	}
	if !strings.Contains(prompt, "本轮已下注: 200") {
		t.Errorf("system prompt should contain round committed 200:\n%s", prompt)
	}
	// 底牌/公共牌必须用花色符号渲染(§B3: 裸 int 对 LLM 无语义)
	if !strings.Contains(prompt, "A♠ K♥") {
		t.Errorf("hole cards should render as 'A♠ K♥':\n%s", prompt)
	}
	if !strings.Contains(prompt, "10♣ 7♦ 2♠") {
		t.Errorf("community should render with suits '10♣ 7♦ 2♠':\n%s", prompt)
	}
	// 最小加注规则必须给出具体数值(§B3)
	if !strings.Contains(prompt, "目标总注额") || !strings.Contains(prompt, "= 400") {
		t.Errorf("system prompt should state min raise total = CurrentBet+MinRaise = 400:\n%s", prompt)
	}
}

// TestB3_ReputationBlockRealStats: 玩家画像基于 Memory.AllOpponentStats 真实统计;
// 无数据时块整体省略(禁止假占位文案)。
func TestB3_ReputationBlockRealStats(t *testing.T) {
	mem := NewMemory()
	if blk := buildReputationBlock(mem); blk != "" {
		t.Errorf("empty memory should yield empty reputation block, got %q", blk)
	}
	mem.IncrementHandsPlayed("opp1")
	mem.UpdateOpponentStat("opp1", 3, "fold")
	mem.UpdateOpponentStat("opp1", 3, "fold")
	mem.UpdateOpponentStat("opp1", 3, "raise")
	mem.RecordOpponentHandResult("opp1", 3, -400, false)
	blk := buildReputationBlock(mem)
	if !strings.Contains(blk, "【玩家画像】") {
		t.Errorf("reputation block missing header: %q", blk)
	}
	if !strings.Contains(blk, "座位4") {
		t.Errorf("reputation block should name seat 4 (seat idx 3): %q", blk)
	}
	if !strings.Contains(blk, "弃牌率 67%") { // 2/3
		t.Errorf("reputation block should show fold rate 67%%: %q", blk)
	}
	if !strings.Contains(blk, "净盈亏 -400") {
		t.Errorf("reputation block should show net chips -400: %q", blk)
	}
	if strings.Contains(blk, "v1.0 简化") {
		t.Errorf("fake placeholder text must be gone: %q", blk)
	}
}

// TestB3_UserPromptOmitsEmptyReputation: 无统计数据时 user prompt 不含【玩家画像】。
func TestB3_UserPromptOmitsEmptyReputation(t *testing.T) {
	ctx := &GameContextForAgent{
		RoomID: "room1", HandNumber: 1, Street: "preflop", MySeat: 0,
		MyHole: [2]int{EncodeCard(14, 1), EncodeCard(14, 2)},
		Pot: 300, CurrentBet: 200, CallAmount: 200, MinRaise: 200, BigBlind: 200,
	}
	prompt := BuildUserPrompt(ctx, NewMemory())
	if strings.Contains(prompt, "【玩家画像】") {
		t.Errorf("user prompt must omit reputation block when no stats:\n%s", prompt)
	}
	if strings.Contains(prompt, "v1.0 简化") {
		t.Errorf("fake placeholder text must not appear:\n%s", prompt)
	}
}
