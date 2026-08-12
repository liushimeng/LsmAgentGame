// Package agent — round26_test.go: regression tests for Round 26 bug fixes.
//
//   - BUG-WEREWOLF-P0-NEW-10: identity mismatch — the agent's identity turn
//     and per-turn user prompt must surface the *1-indexed* 玩家编号 (UI
//     label #1..#7) in addition to the 0-indexed seat number, so the LLM
//     says "我是N号" with N=seat+1 instead of "我是N号" with N=seat.
//
//   - BUG-WEREWOLF-P1-LLM-TOOL: tool_use/tool_result protocol —
//     SanitizeMessagesForAnthropic must synthesise tool_result blocks for
//     any orphan tool_use (assistant message followed by something other
//     than a user tool_result referencing the same id), so DeepSeek/GLM
//     strict proxies don't reject the request with HTTP 400.
//
//   - BUG-WEREWOLF-P1-LLM-TOOL-ORPHAN-RESULT (Round 70+): symmetric
//     defense — the Anthropic protocol also rejects a user tool_result
//     whose tool_use_id does not appear in any preceding assistant turn
//     with HTTP 400 `tool result's tool id … not found (2013)`. This
//     typically happens when Memory.Prune() cuts between the assistant
//     turn and its tool_result. The sanitizer must drop such orphans, and
//     Prune must also advance past leading orphan tool_results in the
//     kept slice.
package wwplayer

import (
	"strings"
	"testing"

	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/llm"
)

// TestIdentityPrompt_OneIndexedPlayerNumber asserts the identity turn for
// seat=3 (player #4) explicitly says 玩家编号 4 号, so the LLM is reminded
// to introduce itself as "我是4号" rather than "我是3号".
func TestIdentityPrompt_OneIndexedPlayerNumber(t *testing.T) {
	prompt := identityPrompt("villager", "good", "vote out wolves", 3)
	if !strings.Contains(prompt, "玩家编号: 4 号") {
		t.Fatalf("identityPrompt missing 1-indexed 玩家编号; got: %s", prompt)
	}
	if !strings.Contains(prompt, "座位号: 3") {
		t.Fatalf("identityPrompt missing 0-indexed 座位号; got: %s", prompt)
	}
	if !strings.Contains(prompt, "我是4号") {
		t.Fatalf("identityPrompt missing hard-constraint '我是4号' template; got: %s", prompt)
	}
}

// TestBuildUserPrompt_RemindsPlayerNumberEveryTurn asserts the per-turn user
// prompt explicitly surfaces the 1-indexed player number, even when the
// bot has no recent context yet. This is the just-in-time reminder that
// stops the LLM from regressing to 0-indexed self-introduction across turns.
func TestBuildUserPrompt_RemindsPlayerNumberEveryTurn(t *testing.T) {
	got := BuildUserPrompt(wwtypes.GameContext{
		Round:  1,
		Phase:  "speak",
		MySeat: 2, // seat 2 → 玩家编号 3 号
	})
	if !strings.Contains(got, "你的玩家编号是 3 号") {
		t.Fatalf("BuildUserPrompt missing 1-indexed 玩家编号 reminder; got: %s", got)
	}
}

// TestSanitizeMessages_OrphanToolUse_IsPatched covers the canonical
// DeepSeek-failure case: an assistant turn with a tool_use, then a user
// text-only turn (no tool_result) — protocol violation. Sanitizer must
// insert a synthetic tool_result user message between them.
func TestSanitizeMessages_OrphanToolUse_IsPatched(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "hi"}}},
		{
			Role:    "assistant",
			Content: []llm.ContentBlock{{Type: "text", Text: ""}, {Type: "tool_use", ID: "tu-1", Name: "vote", Input: map[string]any{"target": 3}}},
		},
		// missing tool_result for tu-1 — protocol violation
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "next turn"}}},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	// 1 orphan tool_use patch + 1 consecutive-user merge (synthetic tool_result
	// user msg merges with the following "next turn" user text) = 2 patches.
	if n != 2 {
		t.Fatalf("expected 2 patches (1 tool_use + 1 user-merge), got %d", n)
	}
	// 3 original + 1 synthetic - 1 merge = 3 messages out.
	if len(patched) != 3 {
		t.Fatalf("expected 3 messages after patch+merge, got %d", len(patched))
	}
	// The merged tail user message (idx 2) must contain a tool_result
	// referencing tu-1 (is_error=true) alongside the "next turn" text block.
	tail := patched[2]
	if tail.Role != "user" {
		t.Fatalf("merged tail at idx 2 must be user; got role=%s", tail.Role)
	}
	foundTR := false
	for _, c := range tail.Content {
		if c.Type == "tool_result" && c.ToolUseID == "tu-1" && c.IsError {
			foundTR = true
		}
	}
	if !foundTR {
		t.Fatalf("merged tail user message missing synthetic tool_result tu-1; got %+v", tail.Content)
	}
}

// TestSanitizeMessages_PairedToolUse_NoChange covers the well-formed case:
// assistant(tool_use) immediately followed by user(tool_result). Patcher
// must leave the slice untouched.
func TestSanitizeMessages_PairedToolUse_NoChange(t *testing.T) {
	msgs := []llm.Message{
		{
			Role:    "assistant",
			Content: []llm.ContentBlock{{Type: "tool_use", ID: "tu-1", Name: "vote", Input: map[string]any{}}},
		},
		{
			Role:    "user",
			Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "tu-1", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}}},
		},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	if n != 0 {
		t.Fatalf("expected 0 patches for paired tool_use, got %d", n)
	}
	if len(patched) != len(msgs) {
		t.Fatalf("expected no message count change, got %d → %d", len(msgs), len(patched))
	}
}

// TestSanitizeMessages_OrphanToolResult_Dropped covers the Round 70+
// regression: a user tool_result whose tool_use_id has no preceding
// assistant tool_use (typically produced by Memory.Prune() cutting in
// the middle of an (assistant, user) pair). Anthropic's proxy returns
// HTTP 400 `tool result's tool id … not found (2013)`. The sanitizer
// must drop the orphan block.
func TestSanitizeMessages_OrphanToolResult_Dropped(t *testing.T) {
	msgs := []llm.Message{
		// identity-style opening user message
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "identity"}}},
		// orphan tool_result — no preceding tool_use with this id
		{Role: "user", Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "call_uUU83zrvPl122c8KAlHmYnxY", Content: []llm.ContentBlock{{Type: "text", Text: "sent"}}}}},
		// a normal user text turn afterwards
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "next state push"}}},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	// 1 orphan tool_result drop + 1 consecutive-user merge (the surviving
	// opening "identity" user msg merges with the trailing "next state push"
	// user msg) = 2 patches.
	if n != 2 {
		t.Fatalf("expected 2 patches (1 orphan-drop + 1 user-merge), got %d", n)
	}
	// 3 messages in, 1 dropped + 1 merge → 1 merged user message out.
	if len(patched) != 1 {
		t.Fatalf("expected 1 merged user message after drop+merge, got %d", len(patched))
	}
	for _, m := range patched {
		for _, c := range m.Content {
			if c.Type == "tool_result" && c.ToolUseID == "call_uUU83zrvPl122c8KAlHmYnxY" {
				t.Fatalf("orphan tool_result must be dropped; still present in role=%s", m.Role)
			}
		}
	}
	// The merged single user message must preserve both surviving text blocks.
	if len(patched[0].Content) != 2 {
		t.Fatalf("expected 2 text blocks in merged user message, got %d", len(patched[0].Content))
	}
}

// TestSanitizeMessages_OrphanToolResultMixedWithText_DropOnlyBlock covers
// the case where a user message contains BOTH a text block AND an
// orphan tool_result. We keep the text and drop the orphan tool_result
// block; the user message survives.
func TestSanitizeMessages_OrphanToolResultMixedWithText_DropOnlyBlock(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: "user",
			Content: []llm.ContentBlock{
				{Type: "text", Text: "shared info"},
				{Type: "tool_result", ToolUseID: "call_unknown_xyz", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}},
			},
		},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	if n != 1 {
		t.Fatalf("expected 1 patched block, got %d", n)
	}
	if len(patched) != 1 {
		t.Fatalf("user message must survive (mixed text + orphan tool_result), got %d messages", len(patched))
	}
	if len(patched[0].Content) != 1 || patched[0].Content[0].Type != "text" {
		t.Fatalf("only the text block must remain; got %+v", patched[0].Content)
	}
	if patched[0].Content[0].Text != "shared info" {
		t.Fatalf("text must be preserved verbatim; got %q", patched[0].Content[0].Text)
	}
}

// TestSanitizeMessages_OrphanToolUse_StillPatched ensures the
// Round-26 forward patch (orphan tool_use gets a synthetic
// tool_result) still works after the orphan-result defense is added.
func TestSanitizeMessages_OrphanToolUse_StillPatched(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "hi"}}},
		{
			Role:    "assistant",
			Content: []llm.ContentBlock{{Type: "text", Text: ""}, {Type: "tool_use", ID: "tu-orphan", Name: "vote", Input: map[string]any{"target": 3}}},
		},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "next turn"}}},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	// 1 orphan tool_use patch + 1 consecutive-user merge = 2 patches.
	if n != 2 {
		t.Fatalf("expected 2 patches (1 tool_use + 1 user-merge), got %d", n)
	}
	if len(patched) != 3 {
		t.Fatalf("expected 3 messages after patch+merge, got %d", len(patched))
	}
	// The merged tail user message (idx 2) must carry the synthetic tool_result.
	tail := patched[2]
	if tail.Role != "user" {
		t.Fatalf("merged tail at idx 2 must be user; got role=%s", tail.Role)
	}
	found := false
	for _, c := range tail.Content {
		if c.Type == "tool_result" && c.ToolUseID == "tu-orphan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("synthetic tool_result tu-orphan missing from merged tail; got %+v", tail.Content)
	}
}

// TestPrune_OrphanToolResultAtBoundary_AdvancesPast covers the
// long-term-memory cleanup: when Memory.Prune cuts in the middle of an
// (assistant, user) pair, the kept slice must NOT start with a
// tool_result-only user message. Prune must advance past it so the
// stored transcript stays clean (the request-time sanitizer is the
// outer safety net, but the UI AgentThoughtPanel renders the stored
// transcript directly).
func TestPrune_OrphanToolResultAtBoundary_AdvancesPast(t *testing.T) {
	// Build a memory with identity + 130 messages, where the
	// 120-message keep window cuts between assistant(tool_use) and
	// user(tool_result) at the head of the kept slice.
	mem := NewMemory("villager", "good", "vote out wolves", 2)
	// 60 turns * 2 = 120 messages to push past the 120 boundary.
	for i := 0; i < 60; i++ {
		// user turn
		mem.Push(llm.Message{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "tick"}}})
		// assistant turn with a tool_use — id must be unique per turn
		// so the orphan is detectable.
		mem.Push(llm.Message{
			Role: "assistant",
			Content: []llm.ContentBlock{
				{Type: "tool_use", ID: "tu_" + itoa(i), Name: "speak", Input: map[string]any{"text": "x"}},
			},
		})
	}
	// Now the tail is assistant(tool_use tu_59). Push the matching
	// user(tool_result tu_59) so the pair exists, but make sure the
	// next pushed message (which will be the 121st → pruned off the
	// tail) takes the tool_use with it.
	mem.Push(llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_59", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}},
		},
	})

	beforeSnapshot, _ := mem.Snapshot()
	if len(beforeSnapshot) <= 121 {
		t.Fatalf("test fixture invalid: need > 121 messages to trigger prune, got %d", len(beforeSnapshot))
	}

	// Prune to 60 turns (keep = 120 messages plus identity = 121).
	// The cutoff will land in the middle: the assistant(tool_use tu_58)
	// gets dropped while its user(tool_result tu_58) stays, producing
	// an orphan tool_result at the head of the kept slice.
	mem.Prune(60)

	msgs, _ := mem.Snapshot()
	if len(msgs) == 0 {
		t.Fatal("snapshot empty after prune")
	}
	// Walk the kept slice (after identity at index 0) and assert no
	// message is a tool_result-only user message whose id is unknown.
	knownUseIDs := map[string]bool{}
	for _, m := range msgs {
		if m.Role != "assistant" {
			continue
		}
		for _, c := range m.Content {
			if c.Type == "tool_use" && c.ID != "" {
				knownUseIDs[c.ID] = true
			}
		}
	}
	for i, m := range msgs {
		if m.Role != "user" {
			continue
		}
		for _, c := range m.Content {
			if c.Type == "tool_result" && c.ToolUseID != "" && !knownUseIDs[c.ToolUseID] {
				t.Fatalf("orphan tool_result survived Prune at msg[%d]: tool_use_id=%q", i, c.ToolUseID)
			}
		}
	}
}

// TestSanitizeMessages_ConsecutiveUser_Merged is the regression guard for the
// Anthropic protocol's hard constraint: user/assistant messages MUST alternate
// — two or more consecutive role=user messages trigger HTTP 400. The real
// driver produces this shape every turn (recordToolResult pushes a
// tool_result user message, then the next handleEvent/game-state push adds a
// game-state user message on top). The sanitizer must merge adjacent user
// messages into one (concatenating content blocks) so the wire payload
// alternates strictly.
func TestSanitizeMessages_ConsecutiveUser_Merged(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "identity"}}},
		// 3 consecutive user messages (tool_result + game_state + another push)
		{Role: "user", Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "tu-1", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "game state A"}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "game state B"}}},
		// an assistant turn then more consecutive users
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "tool_use", ID: "tu-2", Name: "speak", Input: map[string]any{"text": "hi"}}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "tu-2", Content: []llm.ContentBlock{{Type: "text", Text: "spoken"}}}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "game state C"}}},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	if n == 0 {
		t.Fatalf("expected at least 1 user-merge patch, got 0")
	}
	// No two adjacent messages may share the same role.
	for i := 1; i < len(patched); i++ {
		if patched[i].Role == patched[i-1].Role {
			t.Fatalf("adjacent same-role messages at idx %d,%d (role=%s): payload violates Anthropic alternation rule", i-1, i, patched[i].Role)
		}
	}
	// The merged head user message should carry 3 content blocks: the
	// tool_result(tu-1) is an orphan (no preceding assistant emitted tu-1),
	// so the orphan-result defense drops it first; the identity text + "game
	// state A" + "game state B" then merge into one user message.
	head := patched[0]
	if head.Role != "user" {
		t.Fatalf("head must be user; got %s", head.Role)
	}
	if len(head.Content) != 3 {
		t.Fatalf("merged head user message expected 3 content blocks, got %d (%+v)", len(head.Content), head.Content)
	}
	// First block must remain the identity text (ordering preserved).
	if head.Content[0].Type != "text" || head.Content[0].Text != "identity" {
		t.Fatalf("first block must be identity text; got %+v", head.Content[0])
	}
	// The orphan tool_result tu-1 must NOT survive.
	for _, c := range head.Content {
		if c.Type == "tool_result" && c.ToolUseID == "tu-1" {
			t.Fatalf("orphan tool_result tu-1 must be dropped, but survived in merged head")
		}
	}
	// The tail merged user message must carry tool_result(tu-2) + "game state C".
	tail := patched[len(patched)-1]
	if tail.Role != "user" || len(tail.Content) != 2 {
		t.Fatalf("tail expected user with 2 blocks, got role=%s n=%d", tail.Role, len(tail.Content))
	}
	if tail.Content[0].Type != "tool_result" || tail.Content[0].ToolUseID != "tu-2" {
		t.Fatalf("tail block 0 must be tool_result tu-2; got %+v", tail.Content[0])
	}
	if tail.Content[1].Type != "text" || tail.Content[1].Text != "game state C" {
		t.Fatalf("tail block 1 must be 'game state C'; got %+v", tail.Content[1])
	}
}

// TestSanitizeMessages_AlreadyAlternating_NoChange asserts the sanitizer is a
// no-op on a well-formed payload (strict alternation already holds) — no
// spurious merges or patches.
func TestSanitizeMessages_AlreadyAlternating_NoChange(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "identity"}}},
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "tool_use", ID: "tu-1", Name: "vote", Input: map[string]any{"target": 1}}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "tu-1", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}}}},
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "thinking..."}}},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	if n != 0 {
		t.Fatalf("expected 0 patches for well-formed payload, got %d", n)
	}
	if len(patched) != len(msgs) {
		t.Fatalf("expected message count unchanged, got %d → %d", len(msgs), len(patched))
	}
}

// TestSanitizeMessages_DuplicateToolResult_Dropped reproduces BUG-R234-P2-01:
// concurrent wake paths can append the same result twice, and the subsequent
// consecutive-user merge puts both blocks in one user turn. Anthropic accepts
// exactly one result for each tool_use id, so the sanitizer must retain only
// the first block in wire order.
func TestSanitizeMessages_DuplicateToolResult_Dropped(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "tool_use", ID: "call_dup", Name: "speak", Input: map[string]any{"text": "hello"}}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "tool_result", ToolUseID: "call_dup", Content: []llm.ContentBlock{{Type: "text", Text: "first"}}}}},
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "call_dup", Content: []llm.ContentBlock{{Type: "text", Text: "duplicate"}}},
			{Type: "text", Text: "next state"},
		}},
	}

	got, patched := SanitizeMessagesForAnthropic(msgs)
	if patched != 2 { // one adjacent-user merge + one duplicate result drop
		t.Fatalf("expected 2 patches, got %d", patched)
	}
	if len(got) != 2 || got[1].Role != "user" {
		t.Fatalf("expected assistant/user pair, got %+v", got)
	}
	resultCount := 0
	resultText := ""
	for _, c := range got[1].Content {
		if c.Type == "tool_result" && c.ToolUseID == "call_dup" {
			resultCount++
			if len(c.Content) > 0 {
				resultText = c.Content[0].Text
			}
		}
	}
	if resultCount != 1 {
		t.Fatalf("expected exactly one tool_result for call_dup, got %d: %+v", resultCount, got[1].Content)
	}
	if resultText != "first" {
		t.Fatalf("first tool_result must win, got %q", resultText)
	}
}

// TestSanitizeMessages_StripsThinkingContentBlocks (2026-08-02 §14.1) — when a
// model has extended thinking enabled, its streaming response contains a
// {type:"thinking"} content block. If that block is persisted to Memory and
// replayed verbatim on the next request, the wire shape degenerates to
// {"type":"thinking"} (budget lost via finalizeBlock + omitempty), which
// strict proxies (DouBao/DeepSeek/GLM) reject with HTTP 400
// "missing messages.content.thinking parameter". The sanitizer must strip
// thinking content blocks from assistant turns — thinking is the model's
// transient reasoning, not replayable dialogue history (§14.1 权威用例:
// messages[].content[] 只含 text/tool_use/tool_result)。
func TestSanitizeMessages_StripsThinkingContentBlocks(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "game_state"}}},
		{Role: "assistant", Content: []llm.ContentBlock{
			{Type: "text", Text: "我分析一下"},
			{Type: "thinking", ThinkingBudget: 0}, // 流式回显的退化 thinking 块
			{Type: "tool_use", ID: "tu-1", Name: "speak", Input: map[string]any{"text": "发言"}},
		}},
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu-1", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}},
			{Type: "text", Text: "continue"},
		}},
	}
	patched, n := SanitizeMessagesForAnthropic(msgs)
	if n != 1 {
		t.Fatalf("expected 1 patch (thinking strip), got %d", n)
	}
	// assistant turn (idx 1) must no longer contain any thinking block.
	for _, c := range patched[1].Content {
		if c.Type == "thinking" {
			t.Fatalf("thinking content block not stripped from assistant turn: %+v", patched[1].Content)
		}
	}
	// text + tool_use must survive intact.
	var hasText, hasToolUse bool
	for _, c := range patched[1].Content {
		if c.Type == "text" {
			hasText = true
		}
		if c.Type == "tool_use" && c.ID == "tu-1" {
			hasToolUse = true
		}
	}
	if !hasText || !hasToolUse {
		t.Fatalf("text/tool_use blocks must be preserved, got %+v", patched[1].Content)
	}
}
