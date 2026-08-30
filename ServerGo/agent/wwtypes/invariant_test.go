// Package wwtypes — invariant_test.go: 12 条不变量独立触发 + 双向验证。
//
// 2026-08-13 §20260813-05 U2。
package wwtypes

import (
	"testing"

	"LsmAgentGame/llm"
)

func TestInvariant_I1_SeerCheckRequiresFaction(t *testing.T) {
	gc := &GameContext{Role: "seer", MySeerCheckFaction: ""}
	v := CheckGameContextInvariant(gc)
	if len(v) == 0 || v[0].Code != "I1" {
		t.Fatalf("期望 I1 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I1_OKWhenSeerHasFaction(t *testing.T) {
	gc := &GameContext{Role: "seer", MySeerCheckFaction: "wolf"}
	v := CheckGameContextInvariant(gc)
	for _, x := range v {
		if x.Code == "I1" {
			t.Fatalf("seer 有 faction 时不应触发 I1，实际=%v", v)
		}
	}
}

func TestInvariant_I2_WitchAntidoteWithNoWolfTarget(t *testing.T) {
	gc := &GameContext{WolfTarget: -1, WitchAntidoteUsed: true}
	v := CheckGameContextInvariant(gc)
	if len(v) == 0 || v[0].Code != "I2" {
		t.Fatalf("期望 I2 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I2_OKWhenAntidoteWithWolfTarget(t *testing.T) {
	gc := &GameContext{WolfTarget: 3, WitchAntidoteUsed: true}
	v := CheckGameContextInvariant(gc)
	for _, x := range v {
		if x.Code == "I2" {
			t.Fatalf("Antidote+WolfTarget 一致时不应触发 I2，实际=%v", v)
		}
	}
}

func TestInvariant_I3_WolfTeammateRequiresWerewolfRole(t *testing.T) {
	gc := &GameContext{WolfTeammateSeats: []int{4}, Role: "villager"}
	v := CheckGameContextInvariant(gc)
	if len(v) == 0 || v[0].Code != "I3" {
		t.Fatalf("期望 I3 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I3_OKWhenWerewolf(t *testing.T) {
	gc := &GameContext{WolfTeammateSeats: []int{0, 1, 2}, Role: "werewolf"}
	v := CheckGameContextInvariant(gc)
	for _, x := range v {
		if x.Code == "I3" {
			t.Fatalf("werewolf 有 teammate 不应触发 I3，实际=%v", v)
		}
	}
}

func TestInvariant_I4_WolfPackRequiresWolfFaction(t *testing.T) {
	gc := &GameContext{
		Faction:         "good",
		WolfTeammateSeats: nil,
		WolfPackSnapshot: []WolfPackMsg{{FromSeat: 1, Text: "hi"}},
	}
	v := CheckGameContextInvariant(gc)
	if len(v) == 0 || v[0].Code != "I4" {
		t.Fatalf("期望 I4 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I5_HumanDebuffRequiresHuman(t *testing.T) {
	gc := &GameContext{
		HumanDebuff:     &HumanDebuffSpec{},
		WolfTeammateSeats: nil,
		Static: &StaticContext{
			MySeat: 0,
			AllPlayers: []PlayerBrief{
				{Seat: 0, IsBot: true},
			},
		},
	}
	v := CheckGameContextInvariant(gc)
	if len(v) == 0 || v[0].Code != "I5" {
		t.Fatalf("期望 I5 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I5_OKWhenHuman(t *testing.T) {
	gc := &GameContext{
		HumanDebuff: &HumanDebuffSpec{},
		Static: &StaticContext{
			MySeat: 0,
			AllPlayers: []PlayerBrief{
				{Seat: 0, IsBot: false},
			},
		},
	}
	v := CheckGameContextInvariant(gc)
	for _, x := range v {
		if x.Code == "I5" {
			t.Fatalf("人类持有 HumanDebuff 不应触发 I5，实际=%v", v)
		}
	}
}

func TestInvariant_I6_SeerHistoryLengthCap(t *testing.T) {
	gc := &GameContext{
		Round:              2,
		WolfTeammateSeats:   nil,
		MySeerCheckHistory: make([]SeerCheckRecord, 5), // > Round+1=3
	}
	v := CheckGameContextInvariant(gc)
	if len(v) == 0 || v[0].Code != "I6" {
		t.Fatalf("期望 I6 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

// ---- B 组：消息配对 ----

func TestInvariant_I7_UnpairedToolUse(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentBlock{
			{Type: "tool_use", ID: "call_1", Name: "wolf_kill", Input: map[string]any{"target": 3}},
		}},
		// 缺 tool_result
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "text", Text: "ok"},
		}},
	}
	v := CheckMessagePairingInvariant(msgs)
	if len(v) == 0 || v[0].Code != "I7" {
		t.Fatalf("期望 I7 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I7_OKWhenPaired(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentBlock{
			{Type: "tool_use", ID: "call_1", Name: "wolf_kill", Input: map[string]any{"target": 3}},
		}},
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "call_1", Content: []llm.ContentBlock{
				{Type: "text", Text: "kill accepted"},
			}},
		}},
	}
	v := CheckMessagePairingInvariant(msgs)
	for _, x := range v {
		if x.Code == "I7" {
			t.Fatalf("已配对的 tool_use 不应触发 I7，实际=%v", v)
		}
	}
}

func TestInvariant_I8_ConsecutiveUserMessages(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "a"}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "b"}}},
	}
	v := CheckMessagePairingInvariant(msgs)
	if len(v) == 0 || v[0].Code != "I8" {
		t.Fatalf("期望 I8 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I9_NilToolUseInput(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentBlock{
			{Type: "tool_use", ID: "x", Name: "wolf_kill", Input: nil},
		}},
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "x", Content: []llm.ContentBlock{
				{Type: "text", Text: "ok"},
			}},
		}},
	}
	v := CheckMessagePairingInvariant(msgs)
	foundI9 := false
	for _, x := range v {
		if x.Code == "I9" {
			foundI9 = true
			break
		}
	}
	if !foundI9 {
		t.Fatalf("期望 I9 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

// ---- C 组：请求重建一致性 ----

func TestInvariant_I10_RequestMessagesByteDrift(t *testing.T) {
	// 构造一个 messages 远大于 memoryBytes 的情况
	msgs := make([]llm.Message, 100)
	for i := range msgs {
		msgs[i] = llm.Message{Role: "user", Content: []llm.ContentBlock{
			{Type: "text", Text: "this is a long text payload "},
		}}
	}
	v := CheckRequestReconstructabilityInvariant(llm.LLMRequest{Messages: msgs}, 256, nil)
	if len(v) == 0 || v[0].Code != "I10" {
		t.Fatalf("期望 I10 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I11_SystemBytesMismatch(t *testing.T) {
	req := llm.LLMRequest{
		System: []llm.SystemBlock{{Type: "text", Text: "hello"}},
	}
	v := CheckRequestReconstructabilityInvariant(req, 0, []byte("goodbye"))
	if len(v) == 0 || v[0].Code != "I11" {
		t.Fatalf("期望 I11 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_I12_AgentClassNameRequired(t *testing.T) {
	req := llm.LLMRequest{AgentClassName: ""}
	v := CheckRequestReconstructabilityInvariant(req, 0, nil)
	if len(v) == 0 || v[0].Code != "I12" {
		t.Fatalf("期望 I12 违反，实际=%v", v)
	}
	ResetInvariantViolationCounters()
}

func TestInvariant_CounterBumpOnViolation(t *testing.T) {
	ResetInvariantViolationCounters()
	gc := &GameContext{Role: "seer", MySeerCheckFaction: ""}
	CheckGameContextInvariant(gc)
	if InvariantViolationCount("I1") != 1 {
		t.Fatalf("期望 I1 计数器=1，实际=%d", InvariantViolationCount("I1"))
	}
	// 再次违反
	CheckGameContextInvariant(gc)
	if InvariantViolationCount("I1") != 2 {
		t.Fatalf("期望 I1 计数器=2，实际=%d", InvariantViolationCount("I1"))
	}
	ResetInvariantViolationCounters()
}