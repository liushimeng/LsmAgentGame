// Package citysystem — sanitize_test.go: 出站消息修整单测(2026-11-03 §批次81)。
//
// 覆盖 §14.1 的四类真实破损:相邻同角色 / 孤儿 tool_result / 悬空 tool_use /
// 非白名单块(thinking)。assertWireValid 同时被裁剪 / Compact / ReAct 用例复用。
package citysystem

import (
	"strings"
	"testing"

	llmtypes "LsmAgentGame/llm/types"
)

// toolUseMsg 构造携带 tool_use 的 assistant 消息。
func toolUseMsg(text string, ids ...string) llmtypes.Message {
	blocks := make([]llmtypes.ContentBlock, 0, len(ids)+1)
	if text != "" {
		blocks = append(blocks, llmtypes.ContentBlock{Type: "text", Text: text})
	}
	for _, id := range ids {
		blocks = append(blocks, llmtypes.ContentBlock{
			Type: "tool_use", ID: id, Name: "t", Input: map[string]any{},
		})
	}
	return llmtypes.Message{Role: "assistant", Content: blocks}
}

// toolResultMsg 构造携带 tool_result 的 user 消息。
func toolResultMsg(ids ...string) llmtypes.Message {
	blocks := make([]llmtypes.ContentBlock, 0, len(ids))
	for _, id := range ids {
		blocks = append(blocks, llmtypes.ContentBlock{
			Type: "tool_result", ToolUseID: id,
			Content: []llmtypes.ContentBlock{{Type: "text", Text: "ok"}},
		})
	}
	return llmtypes.Message{Role: "user", Content: blocks}
}

// assertWireValid 校验 messages 满足 Anthropic wire 纪律(§14.1):
// 首条 user、严格交替、tool_use/tool_result 紧邻成对、无空消息、
// 内容块只含 text/tool_use/tool_result。
func assertWireValid(t *testing.T, msgs []llmtypes.Message) {
	t.Helper()
	if len(msgs) == 0 {
		return
	}
	if msgs[0].Role != "user" {
		t.Fatalf("first message role = %s, want user", msgs[0].Role)
	}
	for i, m := range msgs {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		if m.Role != want {
			t.Fatalf("msgs[%d].Role = %s, want %s(严格交替)", i, m.Role, want)
		}
		if len(m.Content) == 0 {
			t.Fatalf("msgs[%d] empty content", i)
		}
		ids := map[string]bool{}
		for _, b := range m.Content {
			switch b.Type {
			case "text", "tool_use", "tool_result":
			default:
				t.Fatalf("msgs[%d] illegal block type %q", i, b.Type)
			}
			if b.Type == "tool_use" {
				ids[b.ID] = true
				// tool_use 必须在下一条 user 里有 tool_result 收尾。
				if i+1 >= len(msgs) {
					t.Fatalf("msgs[%d] dangling tool_use %s(无后续消息)", i, b.ID)
				}
				found := false
				for _, nb := range msgs[i+1].Content {
					if nb.Type == "tool_result" && nb.ToolUseID == b.ID {
						found = true
					}
				}
				if !found {
					t.Fatalf("msgs[%d] dangling tool_use %s(下一条无对应 tool_result)", i, b.ID)
				}
			}
		}
		if m.Role == "user" {
			for _, b := range m.Content {
				if b.Type != "tool_result" {
					continue
				}
				if i == 0 {
					t.Fatalf("msgs[0] orphan tool_result %s", b.ToolUseID)
				}
				ok := false
				for _, pb := range msgs[i-1].Content {
					if pb.Type == "tool_use" && pb.ID == b.ToolUseID {
						ok = true
					}
				}
				if !ok {
					t.Fatalf("msgs[%d] orphan tool_result %s(上一条无对应 tool_use)", i, b.ToolUseID)
				}
			}
		}
	}
}

// TestSanitizeForWire_MergesAdjacentUsers 相邻 user 合并(失败轮残留场景)。
func TestSanitizeForWire_MergesAdjacentUsers(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "第一句"),
		msg("user", "第二句"),
		msg("assistant", "答复"),
	}
	out := sanitizeForWire(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Role != "user" || len(out[0].Content) != 2 {
		t.Fatalf("merged user = %+v", out[0])
	}
	assertWireValid(t, out)
}

// TestSanitizeForWire_MergesAdjacentAssistant 相邻 assistant 合并。
func TestSanitizeForWire_MergesAdjacentAssistant(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "问"),
		msg("assistant", "答 1"),
		msg("assistant", "答 2"),
	}
	out := sanitizeForWire(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if len(out[1].Content) != 2 {
		t.Fatalf("merged assistant blocks = %d, want 2", len(out[1].Content))
	}
	assertWireValid(t, out)
}

// TestSanitizeForWire_PreservesValidPairs 合法的 tool 对原样保留。
func TestSanitizeForWire_PreservesValidPairs(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "查状态"),
		toolUseMsg("调工具", "tu-1"),
		toolResultMsg("tu-1"),
		msg("assistant", "最终答复"),
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4(合法会话不得被裁)", len(out))
	}
}

// TestSanitizeForWire_DropsOrphanToolResult 无主的 tool_result 剔除。
func TestSanitizeForWire_DropsOrphanToolResult(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "问"),
		toolResultMsg("tu-ghost"),
		msg("assistant", "答"),
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2(orphan tool_result 应随空消息剔除)", len(out))
	}
}

// TestSanitizeForWire_PrunesDanglingToolUse 悬空 tool_use 剔除;
// 消息清空则整条剔除(尾部 user 合并回来)。
func TestSanitizeForWire_PrunesDanglingToolUse(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "问"),
		toolUseMsg("", "tu-1"), // 只有 tool_use、无收尾 → 整条剔除
		msg("user", "继续"),
		msg("assistant", "答"),
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	// user(问) + user(继续) 合并 + assistant(答) = 2 条。
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
}

// TestSanitizeForWire_KeepsTextWhenPruning 悬空 tool_use 剔除时保留同条文本。
func TestSanitizeForWire_KeepsTextWhenPruning(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "问"),
		toolUseMsg("我先查一下", "tu-1"), // 无收尾
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	if len(out) != 2 || len(out[1].Content) != 1 || out[1].Content[0].Text != "我先查一下" {
		t.Fatalf("out = %+v, want 文本保留 / tool_use 剔除", out)
	}
}

// TestSanitizeForWire_RequiresImmediatePairing tool_result 必须紧跟持有
// tool_use 的 assistant;中间隔了 user/assistant 即双双剔除。
func TestSanitizeForWire_RequiresImmediatePairing(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "问"),
		toolUseMsg("", "tu-1"),
		msg("user", "插一句"), // 隔断配对
		msg("assistant", "答"),
		toolResultMsg("tu-1"), // 迟到的收尾 → 孤儿
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	// user(问+插一句) + assistant(答) = 2 条(tu-1 对被整体剔除)。
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
}

// TestSanitizeForWire_DropsNonWhitelistBlocks thinking 等非白名单块剔除。
func TestSanitizeForWire_DropsNonWhitelistBlocks(t *testing.T) {
	in := []llmtypes.Message{
		{Role: "assistant", Content: []llmtypes.ContentBlock{
			{Type: "thinking", ThinkingBudget: 1024},
			{Type: "text", Text: "答"},
		}},
		msg("user", "问"),
		msg("assistant", "答 2"),
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	// 前置 assistant(只余 text)仍因「首条必须 user」被丢弃。
	if len(out) != 2 || out[0].Role != "user" {
		t.Fatalf("out = %+v, want [user, assistant]", out)
	}
	for _, m := range out {
		for _, b := range m.Content {
			if b.Type == "thinking" {
				t.Fatal("thinking block must be stripped")
			}
		}
	}
}

// TestSanitizeForWire_MultiToolUsePairing 多工具并行:逐一配对,重复应答剔除。
func TestSanitizeForWire_MultiToolUsePairing(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "查两个"),
		toolUseMsg("", "tu-a", "tu-b"),
		{Role: "user", Content: []llmtypes.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu-a",
				Content: []llmtypes.ContentBlock{{Type: "text", Text: "A"}}},
			{Type: "tool_result", ToolUseID: "tu-a", // 重复应答
				Content: []llmtypes.ContentBlock{{Type: "text", Text: "A2"}}},
			{Type: "tool_result", ToolUseID: "tu-b",
				Content: []llmtypes.ContentBlock{{Type: "text", Text: "B"}}},
		}},
		msg("assistant", "总结"),
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4", len(out))
	}
	if got := len(out[2].Content); got != 2 {
		t.Fatalf("tool_result blocks = %d, want 2(重复应答剔除)", got)
	}
}

// TestSanitizeForWire_PrunesPartiallyDangling 多工具部分收尾:
// 未收尾的 tool_use 剔除,已收尾的保留。
func TestSanitizeForWire_PrunesPartiallyDangling(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "查两个"),
		toolUseMsg("", "tu-a", "tu-b"),
		toolResultMsg("tu-a"), // tu-b 无收尾
		msg("assistant", "总结"),
	}
	out := sanitizeForWire(in)
	assertWireValid(t, out)
	// assistant 只剩 tu-a → 配对保留;tu-b 剔除。
	found := false
	for _, b := range out[1].Content {
		if b.Type == "tool_use" && b.ID == "tu-b" {
			t.Fatal("dangling tu-b must be pruned")
		}
		if b.Type == "tool_use" && b.ID == "tu-a" {
			found = true
		}
	}
	if !found {
		t.Fatal("paired tu-a must be kept")
	}
}

// TestSanitizeForWire_EmptyInput 空输入不 panic。
func TestSanitizeForWire_EmptyInput(t *testing.T) {
	if out := sanitizeForWire(nil); len(out) != 0 {
		t.Fatalf("nil in → %v", out)
	}
	if out := sanitizeForWire([]llmtypes.Message{}); len(out) != 0 {
		t.Fatalf("empty in → %v", out)
	}
}

// TestSanitizeForWire_NoMutation 输入不被就地修改(存储留痕不污染)。
func TestSanitizeForWire_NoMutation(t *testing.T) {
	in := []llmtypes.Message{
		msg("user", "问"),
		toolUseMsg("查", "tu-1"),
		msg("user", "再来一句"),
	}
	_ = sanitizeForWire(in)
	if len(in) != 3 || in[1].Content[1].ID != "tu-1" {
		t.Fatalf("input mutated: %+v", in)
	}
	if !strings.Contains(in[0].Content[0].Text, "问") {
		t.Fatalf("input text mutated: %+v", in[0])
	}
}
