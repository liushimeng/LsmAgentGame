package wwplayer

import (
	"strings"
	"testing"
	"unicode/utf8"

	"LsmAgentGame/llm"
)

// §20260813-04 U6 —— tool_result 独立剪枝测试。
//
// 借鉴 Hermes ContextEngine.prune_tool_results_only。
//
// **最重要的断言是配对完整性**（§82b）：只截断 tool_result 的内层文本，
// 绝不删除整个块、绝不改动 tool_use_id —— 否则切断 tool_use/tool_result 配对，
// 触发 Anthropic 400 "tool result's tool id not found"。

// buildToolPairMessages 构造 n 组 (assistant tool_use, user tool_result) 配对，
// 每个 tool_result 的文本为 payload。
func buildToolPairMessages(n int, payload string) []llm.Message {
	var msgs []llm.Message
	// messages[0] 是 identity（永久保留），与生产形状一致
	msgs = append(msgs, llm.Message{
		Role:    "user",
		Content: []llm.ContentBlock{{Type: "text", Text: "identity"}},
	})
	for i := 0; i < n; i++ {
		id := "tu_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		msgs = append(msgs,
			llm.Message{
				Role: "assistant",
				Content: []llm.ContentBlock{{
					Type:  "tool_use",
					ID:    id,
					Name:  "chat_recall",
					Input: map[string]any{"n": i},
				}},
			},
			llm.Message{
				Role: "user",
				Content: []llm.ContentBlock{{
					Type:      "tool_result",
					ToolUseID: id,
					Content:   []llm.ContentBlock{{Type: "text", Text: payload}},
				}},
			},
		)
	}
	return msgs
}

// collectToolIDs 分别收集 tool_use 与 tool_result 的 id 集合。
func collectToolIDs(msgs []llm.Message) (uses, results map[string]bool) {
	uses, results = map[string]bool{}, map[string]bool{}
	for _, m := range msgs {
		for _, b := range m.Content {
			switch b.Type {
			case "tool_use":
				uses[b.ID] = true
			case "tool_result":
				results[b.ToolUseID] = true
			}
		}
	}
	return
}

// TestU6_PreservesToolUsePairing 断言剪枝**不破坏** tool_use/tool_result 配对。
//
// 这是 U6 的核心安全断言 —— 破坏配对会让 Anthropic 直接 400。
func TestU6_PreservesToolUsePairing(t *testing.T) {
	big := strings.Repeat("道具历史明细", 500) // 远超默认 512 字节阈值
	m := &Memory{messages: buildToolPairMessages(20, big)}

	beforeUses, beforeResults := collectToolIDs(m.messages)
	beforeMsgCount := len(m.messages)

	pruned := m.PruneToolResultsOnly(0, 0)
	if pruned == 0 {
		t.Fatal("20 组配对 + 超长 payload 应产生裁剪，实际 0")
	}

	afterUses, afterResults := collectToolIDs(m.messages)

	if len(m.messages) != beforeMsgCount {
		t.Errorf("消息条数不应变化（只截文本不删块）: %d → %d", beforeMsgCount, len(m.messages))
	}
	if len(afterUses) != len(beforeUses) {
		t.Errorf("tool_use 数量不应变化: %d → %d", len(beforeUses), len(afterUses))
	}
	if len(afterResults) != len(beforeResults) {
		t.Errorf("tool_result 数量不应变化: %d → %d", len(beforeResults), len(afterResults))
	}
	// 每个 tool_result 必须仍有对应的 tool_use（配对完整）
	for id := range afterResults {
		if !afterUses[id] {
			t.Errorf("tool_result %q 失去了配对的 tool_use —— 会触发 Anthropic 400", id)
		}
	}
}

// TestU6_KeepsRecentIntact 断言最近 keepRecent 条不被裁剪。
// 近期工具返回是当前决策的直接依据，裁掉会让 LLM 失去刚查到的信息。
func TestU6_KeepsRecentIntact(t *testing.T) {
	big := strings.Repeat("X", 2000)
	const keepRecent = 6
	m := &Memory{messages: buildToolPairMessages(20, big)}

	m.PruneToolResultsOnly(keepRecent, 512)

	// 检查最后 keepRecent 条里的 tool_result 文本仍是原长
	tail := m.messages[len(m.messages)-keepRecent:]
	for i, msg := range tail {
		for _, b := range msg.Content {
			if b.Type != "tool_result" {
				continue
			}
			for _, inner := range b.Content {
				if inner.Type != "text" {
					continue
				}
				if len(inner.Text) != len(big) {
					t.Errorf("最近 %d 条内的 tool_result（tail[%d]）被裁剪了: %d → %d 字节",
						keepRecent, i, len(big), len(inner.Text))
				}
			}
		}
	}
}

// TestU6_TruncatedTextCarriesMarker 断言被裁剪的文本带可观测标记。
// 静默截断会让「工具返回被裁短」与「工具本来返回很少」同形（§20260812-04 教训 4）。
func TestU6_TruncatedTextCarriesMarker(t *testing.T) {
	m := &Memory{messages: buildToolPairMessages(20, strings.Repeat("Y", 3000))}

	if n := m.PruneToolResultsOnly(4, 256); n == 0 {
		t.Fatal("应有裁剪发生")
	}

	var found bool
	for _, msg := range m.messages[:len(m.messages)-4] {
		for _, b := range msg.Content {
			for _, inner := range b.Content {
				if strings.Contains(inner.Text, toolResultTruncMarker) {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("被裁剪的 tool_result 应含标记 %q", toolResultTruncMarker)
	}
}

// TestU6_NoOpWhenNothingToPrune 断言无可裁剪内容时返回 0（安全 no-op）。
func TestU6_NoOpWhenNothingToPrune(t *testing.T) {
	cases := []struct {
		name string
		m    *Memory
	}{
		{"空 Memory", &Memory{}},
		{"只有 identity", &Memory{messages: buildToolPairMessages(0, "")}},
		{"短 tool_result（未超阈值）", &Memory{messages: buildToolPairMessages(20, "短返回")}},
		{"消息数少于 keepRecent", &Memory{messages: buildToolPairMessages(2, strings.Repeat("Z", 5000))}},
	}
	for _, c := range cases {
		if n := c.m.PruneToolResultsOnly(0, 0); n != 0 {
			t.Errorf("%s: 应返回 0（no-op），实际裁剪 %d 块", c.name, n)
		}
	}
}

// TestU6_TruncationRespectsUTF8Boundary 断言按 rune 边界截断。
//
// 中文 UTF-8 是 3 字节/字，按字节硬切必然切出无效 UTF-8，
// 会导致 JSON 序列化产出非法字符串 → 上游 400。
func TestU6_TruncationRespectsUTF8Boundary(t *testing.T) {
	// 用中文构造，保证截断点大概率落在多字节字符中间
	chinese := strings.Repeat("狼人杀推理", 300)
	for _, truncTo := range []int{100, 256, 511, 512, 1000} {
		m := &Memory{messages: buildToolPairMessages(20, chinese)}
		m.PruneToolResultsOnly(4, truncTo)

		for _, msg := range m.messages {
			for _, b := range msg.Content {
				for _, inner := range b.Content {
					if inner.Type != "text" {
						continue
					}
					if !utf8.ValidString(inner.Text) {
						t.Errorf("truncTo=%d 产出无效 UTF-8 —— 会导致上游 400", truncTo)
					}
				}
			}
		}
	}
}

// TestU6_TotalPayloadBytesShrinks 断言裁剪确实减小了 payload。
//
// 这是「只测转换函数、不测转换结果」的反面（§20260811-08 教训 5）：
// 断言裁剪的**效果**，而非只断言函数返回了非零计数。
func TestU6_TotalPayloadBytesShrinks(t *testing.T) {
	m := &Memory{messages: buildToolPairMessages(30, strings.Repeat("明细", 800))}

	before := m.TotalPayloadBytes()
	if n := m.PruneToolResultsOnly(0, 0); n == 0 {
		t.Fatal("应有裁剪发生")
	}
	after := m.TotalPayloadBytes()

	if after >= before {
		t.Errorf("裁剪后 payload 应减小: %d → %d", before, after)
	}
	// 30 组 × 4800 字节 payload，裁到 512 应至少省下一半
	if after > before/2 {
		t.Errorf("裁剪幅度不足: %d → %d（期望至少减半）", before, after)
	}
}

// TestU6_IdempotentSecondPass 断言二次裁剪不再产生变化（幂等）。
func TestU6_IdempotentSecondPass(t *testing.T) {
	m := &Memory{messages: buildToolPairMessages(20, strings.Repeat("W", 3000))}

	first := m.PruneToolResultsOnly(0, 0)
	if first == 0 {
		t.Fatal("首次应有裁剪")
	}
	afterFirst := m.TotalPayloadBytes()

	second := m.PruneToolResultsOnly(0, 0)
	if second != 0 {
		t.Errorf("二次裁剪应为 no-op，实际又裁了 %d 块（说明标记未生效或反复截断）", second)
	}
	if m.TotalPayloadBytes() != afterFirst {
		t.Error("二次裁剪改变了 payload —— 非幂等")
	}
}
