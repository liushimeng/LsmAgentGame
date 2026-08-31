// Package debateplayer — Memory / 压缩校验 / 消息清洗测试(2026-08-31 §20260831-02/03)。
//
// 覆盖:
//   - sanitizeDebateMessages: 悬空 tool_result 剔除 / 相邻同 role 合并 / assistant 开头丢弃
//   - IsValidCompactSummary: 8 段结构 / 长度 / 黑名单关键词(§05 §5.4)
//   - Memory.ShouldCompact 触发阈值(消息数)
//   - Memory.Replace / LastCompactSummary / CompactCount
package debateplayer

import (
	"strings"
	"testing"

	"LsmAgentGame/llm"
)

// TestSanitizeDropsOrphanToolResult 悬空 tool_result(配对 tool_use 已不在)必须剔除。
func TestSanitizeDropsOrphanToolResult(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "ghost_id", Content: []llm.ContentBlock{{Type: "text", Text: "orphan"}}},
			{Type: "text", Text: "当前轮提示"},
		}},
	}
	out := sanitizeDebateMessages(msgs)
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	for _, c := range out[0].Content {
		if c.Type == "tool_result" {
			t.Error("悬空 tool_result 未被剔除")
		}
	}
}

// TestSanitizeKeepsPairedToolResult 配对完整的 tool_use/tool_result 必须保留。
func TestSanitizeKeepsPairedToolResult(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "轮到你发言"}}},
		{Role: "assistant", Content: []llm.ContentBlock{
			{Type: "tool_use", ID: "tu_1", Name: "speech", Input: map[string]any{"content": "..."}},
		}},
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_1", Content: []llm.ContentBlock{{Type: "text", Text: "ok: speech accepted"}}},
		}},
	}
	out := sanitizeDebateMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3(配对完整不应删减)", len(out))
	}
	found := false
	for _, c := range out[2].Content {
		if c.Type == "tool_result" && c.ToolUseID == "tu_1" {
			found = true
		}
	}
	if !found {
		t.Error("配对的 tool_result 被误删")
	}
}

// TestSanitizeMergesAdjacentUser 相邻 user 消息(fallback 路径可能产生)必须合并。
func TestSanitizeMergesAdjacentUser(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "第1轮提示"}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "第2轮提示"}}},
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "发言"}}},
	}
	out := sanitizeDebateMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2(两条 user 合并为一条)", len(out))
	}
	if out[0].Role != "user" || len(out[0].Content) != 2 {
		t.Errorf("合并后的首条 user 异常: role=%s blocks=%d", out[0].Role, len(out[0].Content))
	}
}

// TestSanitizeDropsLeadingAssistant 首条 assistant 必须丢弃(对话须 user 开头)。
func TestSanitizeDropsLeadingAssistant(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "悬空回复"}}},
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "提示"}}},
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "回复"}}},
	}
	out := sanitizeDebateMessages(msgs)
	if len(out) != 2 || out[0].Role != "user" {
		t.Fatalf("首条 assistant 未被丢弃: len=%d first=%s", len(out), out[0].Role)
	}
}

// validCompactSummary 构造合法 8 段摘要。
func validCompactSummary() string {
	return "## S1. 辩题与立场\n人性本善;我方正方。\n" +
		"## S2. 我方核心论点与论据\n恻隐之心;道德直觉。\n" +
		"## S3. 对方核心论点与论据\n对方一辩:恶行证明性恶。\n" +
		"## S4. 关键交锋点\n善恶定义之争。\n" +
		"## S5. 我方发言摘要\n一辩立论完成。\n" +
		"## S6. 对方发言摘要\n对方立论完成。\n" +
		"## S7. 当前局势\n进入驳论阶段。\n" +
		"## S8. 上次压缩以来的新增\n新增交锋点一处。"
}

// TestIsValidCompactSummaryOK 合法摘要应通过。
func TestIsValidCompactSummaryOK(t *testing.T) {
	if ok, reason := IsValidCompactSummary(validCompactSummary()); !ok {
		t.Errorf("合法摘要被误判失败: %s", reason)
	}
}

// TestIsValidCompactSummaryMissingSection 缺段必须失败。
func TestIsValidCompactSummaryMissingSection(t *testing.T) {
	s := strings.Replace(validCompactSummary(), "## S4. 关键交锋点\n善恶定义之争。\n", "", 1)
	if ok, _ := IsValidCompactSummary(s); ok {
		t.Error("缺 S4 段的摘要应校验失败")
	}
}

// TestIsValidCompactSummaryForbiddenKeyword 黑名单关键词必须失败(§05 §5.4)。
func TestIsValidCompactSummaryForbiddenKeyword(t *testing.T) {
	for _, kw := range []string{"对方策略", "对方计划", "对方内部"} {
		s := strings.Replace(validCompactSummary(), "## S7. 当前局势\n",
			"## S7. 当前局势\n我方掌握"+kw+"机密。\n", 1)
		if ok, reason := IsValidCompactSummary(s); ok {
			t.Errorf("含黑名单词 %q 的摘要应校验失败", kw)
		} else if !strings.Contains(reason, kw) {
			t.Errorf("失败原因应包含关键词 %q, got %q", kw, reason)
		}
	}
}

// TestIsValidCompactSummaryTooLong 超长必须失败。
func TestIsValidCompactSummaryTooLong(t *testing.T) {
	s := validCompactSummary() + "\n## 附注\n" + strings.Repeat("长", 2000)
	if ok, _ := IsValidCompactSummary(s); ok {
		t.Error("超长摘要(>2000 字节)应校验失败")
	}
}

// TestIsValidCompactSummaryEmpty 空摘要必须失败。
func TestIsValidCompactSummaryEmpty(t *testing.T) {
	if ok, _ := IsValidCompactSummary(""); ok {
		t.Error("空摘要应校验失败")
	}
	if ok, _ := IsValidCompactSummary("   \n  "); ok {
		t.Error("纯空白摘要应校验失败")
	}
}

// TestMemoryShouldCompact 消息数超阈值触发。
func TestMemoryShouldCompact(t *testing.T) {
	m := &Memory{}
	if m.ShouldCompact() {
		t.Error("空记忆不应触发压缩")
	}
	for i := 0; i < compactMsgThreshold+1; i++ {
		m.Append(llm.Message{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "x"}}})
	}
	if !m.ShouldCompact() {
		t.Errorf("消息数 %d > %d 应触发压缩", m.Length(), compactMsgThreshold)
	}
}

// TestMemoryCompactStateAccessors Replace / LastCompactSummary / CompactCount。
func TestMemoryCompactStateAccessors(t *testing.T) {
	m := &Memory{}
	m.setLastCompactSummary("summary-v1")
	m.incCompactCount()
	if m.LastCompactSummary() != "summary-v1" {
		t.Errorf("LastCompactSummary = %q, want summary-v1", m.LastCompactSummary())
	}
	if m.CompactCount() != 1 {
		t.Errorf("CompactCount = %d, want 1", m.CompactCount())
	}

	replacement := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "摘要头"}}},
		{Role: "assistant", Content: []llm.ContentBlock{{Type: "text", Text: "ok"}}},
	}
	m.Replace(replacement)
	if m.Length() != 2 {
		t.Errorf("Replace 后 Length = %d, want 2", m.Length())
	}
}

// TestSerializeForCompact 序列化输出应包含 tool_use / tool_result 标记并截断。
func TestSerializeForCompact(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "轮到你"}}},
		{Role: "assistant", Content: []llm.ContentBlock{
			{Type: "tool_use", ID: "tu_9", Name: "speech", Input: map[string]any{"content": "正文"}},
		}},
		{Role: "user", Content: []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "tu_9", Content: []llm.ContentBlock{{Type: "text", Text: "ok: speech accepted"}}},
		}},
	}
	out := serializeForCompact(msgs)
	if !strings.Contains(out, "tool_use speech") {
		t.Error("序列化应包含 tool_use 标记")
	}
	if !strings.Contains(out, "tool_result") {
		t.Error("序列化应包含 tool_result 标记")
	}
}
