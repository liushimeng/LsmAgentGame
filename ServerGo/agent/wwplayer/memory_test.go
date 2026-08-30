// Package agent — memory_test.go: PickWolfTeammateHint + identityPromptWithWolfHint 的可测性测试。
//
// 测试目标(2026-07-21 §5.2):
//  1. PickWolfTeammateHint 在概率 / 角色 / 狼人数边界条件下行为正确
//     (非狼人 / 单狼 / 概率 0 / 概率 100 / 种子可复现)
//  2. identityPromptWithWolfHint 在注入 / 不注入 / 文本完整性三档下输出正确
//  3. NewMemoryWithWolfHint / ReplaceIdentity 保留后续对话(防 LLM 上下文污染)
package wwplayer

import (
	"math/rand"
	"strings"
	"testing"

	"LsmAgentGame/llm"
)

// TestPickWolfTeammateHint_NonWolf 验证非狼人 bot 永远不会被注入(返回 -1)。
func TestPickWolfTeammateHint_NonWolf(t *testing.T) {
	allWolves := []int{1, 5, 9}
	rng := rand.New(rand.NewSource(42))
	for _, role := range []string{"villager", "seer", "witch", "hunter", "idiot"} {
		got := PickWolfTeammateHint(role, "good", 0, allWolves, 100, rng)
		if got != -1 {
			t.Errorf("non-wolf role=%s should return -1, got %d", role, got)
		}
	}
}

// TestPickWolfTeammateHint_SingleWolf 验证只有自己一个狼时返回 -1
// (len(allWolfSeats) < 2 → 没有"队友"可言)。
func TestPickWolfTeammateHint_SingleWolf(t *testing.T) {
	got := PickWolfTeammateHint("werewolf", "wolf", 3, []int{3}, 100, rand.New(rand.NewSource(1)))
	if got != -1 {
		t.Errorf("single wolf should return -1, got %d", got)
	}
}

// TestPickWolfTeammateHint_RateZero 验证 ratePercent=0 时永远返回 -1(设计关闭)。
func TestPickWolfTeammateHint_RateZero(t *testing.T) {
	for trial := 0; trial < 50; trial++ {
		got := PickWolfTeammateHint("werewolf", "wolf", 1, []int{1, 5, 9}, 0, rand.New(rand.NewSource(int64(trial))))
		if got != -1 {
			t.Errorf("rate=0 should always return -1, got %d", got)
		}
	}
}

// TestPickWolfTeammateHint_RateHundred 验证 ratePercent=100 时永远命中,
// 且返回的座位 ∈ allWolfSeats 减去自己。
func TestPickWolfTeammateHint_RateHundred(t *testing.T) {
	allWolves := []int{2, 7}
	mySeat := 2
	for trial := 0; trial < 30; trial++ {
		got := PickWolfTeammateHint("werewolf", "wolf", mySeat, allWolves, 100, rand.New(rand.NewSource(int64(trial))))
		if got != 7 {
			t.Errorf("rate=100 should return the only other wolf=7, got %d", got)
		}
	}
}

// TestPickWolfTeammateHint_Distribution 验证默认概率(30%)下,~30% 命中。
// 用 1000 次种子复现实验做粗粒度校验(25%-35% 区间即可,避免 flaky)。
func TestPickWolfTeammateHint_Distribution(t *testing.T) {
	const N = 1000
	hits := 0
	for trial := 0; trial < N; trial++ {
		got := PickWolfTeammateHint("werewolf", "wolf", 3, []int{3, 7, 11}, 30, rand.New(rand.NewSource(int64(trial))))
		if got >= 0 {
			hits++
		}
	}
	pct := float64(hits) * 100.0 / float64(N)
	if pct < 25.0 || pct > 35.0 {
		t.Errorf("rate=30 over %d trials: hits=%d (%.1f%%), expected 25-35%% range", N, hits, pct)
	}
}

// TestPickWolfTeammateHint_Deterministic 验证相同 seed 永远返回相同结果。
func TestPickWolfTeammateHint_Deterministic(t *testing.T) {
	allWolves := []int{1, 5, 9, 11}
	rng1 := rand.New(rand.NewSource(1234))
	rng2 := rand.New(rand.NewSource(1234))
	got1 := PickWolfTeammateHint("werewolf", "wolf", 1, allWolves, 30, rng1)
	got2 := PickWolfTeammateHint("werewolf", "wolf", 1, allWolves, 30, rng2)
	if got1 != got2 {
		t.Errorf("same seed should give same result, got %d vs %d", got1, got2)
	}
}

// TestIdentityPromptWithWolfHint_NoHint 验证未注入提示时与原 identityPrompt 行为一致。
func TestIdentityPromptWithWolfHint_NoHint(t *testing.T) {
	base := identityPrompt("werewolf", "wolf", "屠边", 0)
	explicit := identityPromptWithWolfHint("werewolf", "wolf", "屠边", 0, nil)
	if base != explicit {
		t.Errorf("wolfTeammateSeats=nil should produce identical text to identityPrompt")
	}
	// 非狼人:即使 WolfTeammateSeats 非空也不注入。
	withHint := identityPromptWithWolfHint("villager", "good", "放逐全部狼人", 0, []int{5})
	if strings.Contains(withHint, "开局互认所有狼队友") {
		t.Errorf("non-wolf should not get the hint section, got: %s", withHint)
	}
}

// TestIdentityPromptWithWolfHint_HasHint 验证狼人 + wolfTeammateSeat >= 0 时
// 正确注入提示(包含 1-indexed 玩家编号与 0-indexed 座位)。
func TestIdentityPromptWithWolfHint_HasHint(t *testing.T) {
	out := identityPromptWithWolfHint("werewolf", "wolf", "屠边", 4, []int{0, 1, 7})
	if !strings.Contains(out, "开局互认所有狼队友") {
		t.Errorf("wolf should get the §5.2 hint section, got: %s", out)
	}
	// 提示应包含 1-indexed 玩家编号(8)与 0-indexed 座位(7)。
	if !strings.Contains(out, "8 号") {
		t.Errorf("hint should reference 1-indexed player #8, got: %s", out)
	}
	if !strings.Contains(out, "座位 7") {
		t.Errorf("hint should reference 0-indexed seat 7, got: %s", out)
	}
}

// TestReplaceIdentity_PreservesHistory 验证 ReplaceIdentity 仅替换首条
// identity,保留后续已发生的对话(防止 LLM 上下文污染)。
func TestReplaceIdentity_PreservesHistory(t *testing.T) {
	m := NewMemory("werewolf", "wolf", "屠边", 0)
	m.Push(llm.Message{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "首夜: 狼队决定击杀 5 号"}}})
	m.Push(llm.Message{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "wolf_chat: 收到,确认"}}})

	m.ReplaceIdentity("werewolf", "wolf", "屠边", 0, []int{0, 1, 7})
	msgs, _ := m.Snapshot()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (identity + 2 turns), got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content[0].Text, "开局互认所有狼队友") {
		t.Errorf("first msg should now contain wolf hint, got: %s", msgs[0].Content[0].Text)
	}
	if msgs[1].Role != "assistant" || !strings.Contains(msgs[1].Content[0].Text, "首夜") {
		t.Errorf("second msg should be preserved, got: %+v", msgs[1])
	}
	if msgs[2].Role != "user" || !strings.Contains(msgs[2].Content[0].Text, "wolf_chat") {
		t.Errorf("third msg should be preserved, got: %+v", msgs[2])
	}
}

// TestReplaceIdentity_EmptyMemoryIsNoop 验证 m.messages 为空时不 panic、不写入。
func TestReplaceIdentity_EmptyMemoryIsNoop(t *testing.T) {
	m := &Memory{}
	m.ReplaceIdentity("werewolf", "wolf", "屠边", 0, []int{0, 1, 7})
	msgs, _ := m.Snapshot()
	if len(msgs) != 0 {
		t.Errorf("empty memory should remain empty after ReplaceIdentity, got %d msgs", len(msgs))
	}
}

// TestNewMemoryWithWolfHint_HintText 验证工厂函数产出含提示的 identity。
func TestNewMemoryWithWolfHint_HintText(t *testing.T) {
	m := NewMemoryWithWolfHint("werewolf", "wolf", "屠边", 4, []int{0, 1, 7})
	msgs, _ := m.Snapshot()
	if len(msgs) != 1 {
		t.Fatalf("NewMemoryWithWolfHint should produce 1 identity message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("identity should be role=user, got %s", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content[0].Text, "开局互认所有狼队友") {
		t.Errorf("NewMemoryWithWolfHint with seat=7 should embed hint, got: %s", msgs[0].Content[0].Text)
	}
}

// TestNewMemory_NoHint 验证 NewMemory (= NewMemoryWithWolfHint(-1)) 不含提示。
func TestNewMemory_NoHint(t *testing.T) {
	m := NewMemory("werewolf", "wolf", "屠边", 4)
	msgs, _ := m.Snapshot()
	if len(msgs) != 1 {
		t.Fatalf("NewMemory should produce 1 identity message, got %d", len(msgs))
	}
	if strings.Contains(msgs[0].Content[0].Text, "开局互认所有狼队友") {
		t.Errorf("NewMemory without hint should NOT embed hint, got: %s", msgs[0].Content[0].Text)
	}
}
