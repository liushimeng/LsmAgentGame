// Package thpagent — memory_compact_test.go: 德扑版 LLM 驱动记忆压缩测试
// (2026-08-24 §2.1 移植 wwplayer memory_compact)。
package thpagent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/llm/types"
)

// mockProvider 实现 types.LLMProvider 用于单测,直接返回预设文本。
type mockCompactProvider struct {
	response string
	err      error
}

func (m *mockCompactProvider) Chat(_ context.Context, _ string, _ types.LLMRequest) (types.LLMResponse, error) {
	if m.err != nil {
		return types.LLMResponse{}, m.err
	}
	return types.LLMResponse{
		Content: []types.ContentBlock{{Type: "text", Text: m.response}},
	}, nil
}

// ChatStream 实现 types.LLMProvider(单测不需要,返回错误即可)。
func (m *mockCompactProvider) ChatStream(_ context.Context, _ string, _ types.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("mockCompactProvider: stream not implemented")
}

// ProviderType 实现 types.LLMProvider。
func (m *mockCompactProvider) ProviderType() string { return "anthropic-messages" }

func TestIsValidTexasCompactSummary(t *testing.T) {
	valid := "# Title\n\n## S1 风格画像\n积极的桌牌偏好,本局累计 bluff 命中率 40%,弃牌率偏紧。\n\n## S2 对手笔记\n1号位: 紧凶型,共8手,弃牌率70%。\n\n## S3 关键决策与理由\n上次 bluff 被 4 号位读出后收手,教训 — 注意对手读牌模式。\n\n## S4 当前局势提示\n筹码中位,大盲在 3 号,需要激进一次争取主动权。\n"
	ok, reason := IsValidTexasCompactSummary(valid)
	if !ok {
		t.Errorf("expected valid summary, got failure: %s", reason)
	}
	// 缺 S3 段
	missing := "# Title\n\n## S1 风格画像\nx\n\n## S2 对手笔记\nx\n\n## S4 当前局势提示\nx\n" + strings.Repeat("x", 80)
	ok, reason = IsValidTexasCompactSummary(missing)
	if ok {
		t.Error("expected invalid due to missing S3 section")
	}
	if !strings.Contains(reason, "S3") {
		t.Errorf("expected reason to mention S3, got %q", reason)
	}
	// 太短
	ok, _ = IsValidTexasCompactSummary("## S1 风格画像\nx\n")
	if ok {
		t.Error("expected invalid due to too short")
	}
}

func TestSerializeMemoryForCompact(t *testing.T) {
	mem := NewMemory()
	for i := 0; i < 3; i++ {
		mem.AppendHand(HandRecord{
			HandNumber:   i + 1,
			MyHole:       [2]int{12, 25},
			CommunityLen: 5,
			Winners:      []int{0},
			NetChipDelta: 100 * (i + 1),
		}, 5)
	}
	mem.UpdateOpponentStat("opp-1", 1, "fold")
	mem.UpdateOpponentStat("opp-1", 1, "raise")
	mem.IncrementHandsPlayed("opp-1")
	text := serializeMemoryForCompact(mem.RecentHandsSnapshot(), mem.AllOpponentStats(), 8)
	if !strings.Contains(text, "手牌回顾") {
		t.Error("expected 手牌回顾 section in serialized text")
	}
	if !strings.Contains(text, "对手统计") {
		t.Error("expected 对手统计 section in serialized text")
	}
	if !strings.Contains(text, "净盈亏+300") {
		t.Errorf("expected last hand net delta +300, got: %s", text)
	}
}

func TestTexasMemoryCompactWithLLM_Success(t *testing.T) {
	mem := NewMemory()
	for i := 0; i < 4; i++ {
		mem.AppendHand(HandRecord{
			HandNumber:   i + 1,
			CommunityLen: 5,
			NetChipDelta: 50 * (i + 1),
		}, 5)
	}
	mem.UpdateOpponentStat("opp-1", 1, "fold")
	mem.IncrementHandsPlayed("opp-1")

	summary := "# 经验库\n\n## S1 风格画像\n本局紧凶,bluff 频率较高,需关注位置选择。\n\n## S2 对手笔记\n1号位: 紧,共8手,弃牌率70%。\n\n## S3 关键决策与理由\n关键 bluff 成功,继续保持节奏。\n\n## S4 当前局势提示\n筹码中位,大盲在 3 号。"
	provider := &mockCompactProvider{response: summary}
	cfg := DefaultTexasCompactConfig()
	res := mem.CompactWithLLM(context.Background(), provider, "fake-key", "model-x", cfg)
	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if res.HandsBefore != 4 {
		t.Errorf("expected HandsBefore=4, got %d", res.HandsBefore)
	}
	if mem.LastCompactSummary() != summary {
		t.Errorf("LastCompactSummary not persisted: got len=%d, want len=%d", len(mem.LastCompactSummary()), len(summary))
	}
}

func TestTexasMemoryCompactWithLLM_BelowMinHands(t *testing.T) {
	mem := NewMemory()
	for i := 0; i < 2; i++ { // 2 手 < MinHands 4
		mem.AppendHand(HandRecord{HandNumber: i + 1}, 5)
	}
	provider := &mockCompactProvider{response: "unused"}
	cfg := DefaultTexasCompactConfig()
	res := mem.CompactWithLLM(context.Background(), provider, "fake-key", "model-x", cfg)
	if res.Success {
		t.Error("expected failure when hands < MinHands")
	}
	if mem.LastCompactSummary() != "" {
		t.Error("LastCompactSummary must remain empty on skipped compact")
	}
}

func TestTexasMemoryCompactWithLLM_InvalidSummary(t *testing.T) {
	mem := NewMemory()
	for i := 0; i < 4; i++ {
		mem.AppendHand(HandRecord{HandNumber: i + 1}, 5)
	}
	// 缺 S3
	bad := "# t\n\n## S1 风格画像\nx\n\n## S2 对手笔记\nx\n\n## S4 当前局势提示\nx\n"
	provider := &mockCompactProvider{response: bad}
	cfg := DefaultTexasCompactConfig()
	res := mem.CompactWithLLM(context.Background(), provider, "fake-key", "model-x", cfg)
	if res.Success {
		t.Error("expected failure for invalid summary")
	}
	if mem.LastCompactSummary() != "" {
		t.Error("LastCompactSummary must remain empty on invalid summary")
	}
}

func TestTexasMemoryCompactWithLLM_AgentClassName(t *testing.T) {
	mem := NewMemory()
	for i := 0; i < 4; i++ {
		mem.AppendHand(HandRecord{HandNumber: i + 1}, 5)
	}
	capture := &agentClassCapture{}
	provider := &compactCaptureProvider{
		provider:  &mockCompactProvider{response: "# Title\n\n## S1 风格画像\n积极的桌牌偏好,本局累计 bluff 命中率 40%。\n\n## S2 对手笔记\n1号位: 紧,共8手。\n\n## S3 关键决策与理由\n关键 bluff 成功,继续保持节奏。\n\n## S4 当前局势提示\n筹码中位,大盲在 3 号。\n"},
		captured:  capture,
	}
	cfg := DefaultTexasCompactConfig()
	res := mem.CompactWithLLM(context.Background(), provider, "fake-key", "model-x", cfg)
	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Error)
	}
	if capture.lastAgentClass != string(agentroot.AgentClassTexasHoldemMemoryCompact) {
		t.Errorf("expected AgentClassName=%q, got %q",
			agentroot.AgentClassTexasHoldemMemoryCompact, capture.lastAgentClass)
	}
}

// compactCaptureProvider 包装 mockProvider 并捕获 AgentClassName。
type compactCaptureProvider struct {
	provider *mockCompactProvider
	captured *agentClassCapture
}

type agentClassCapture struct {
	lastAgentClass string
}

func (p *compactCaptureProvider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	p.captured.lastAgentClass = req.AgentClassName
	return p.provider.Chat(ctx, key, req)
}

func (p *compactCaptureProvider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	return p.provider.ChatStream(ctx, key, req)
}

func (p *compactCaptureProvider) ProviderType() string {
	return p.provider.ProviderType()
}

func TestAgentClassTexasHoldemMemoryCompact_Wired(t *testing.T) {
	if agentroot.AgentClassTexasHoldemMemoryCompact == "" {
		t.Fatal("AgentClassTexasHoldemMemoryCompact must be non-empty (§24)")
	}
	if string(agentroot.AgentClassTexasHoldemMemoryCompact) != "LsmAgentGame-TexasHoldem-MemoryCompact" {
		t.Errorf("unexpected AgentClassName: %q", agentroot.AgentClassTexasHoldemMemoryCompact)
	}
	found := false
	for _, c := range agentroot.AllAgentClassNames() {
		if c == agentroot.AgentClassTexasHoldemMemoryCompact {
			found = true
			break
		}
	}
	if !found {
		t.Error("AgentClassTexasHoldemMemoryCompact must be registered in AllAgentClassNames()")
	}
	if !agentroot.IsValidAgentClassName(string(agentroot.AgentClassTexasHoldemMemoryCompact)) {
		t.Error("AgentClassTexasHoldemMemoryCompact must pass IsValidAgentClassName")
	}
}