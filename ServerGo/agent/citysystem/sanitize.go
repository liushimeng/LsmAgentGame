// Package citysystem — sanitize.go: 出站消息的 Anthropic wire 修整
// (2026-11-03 §批次81)。
//
// 契约: CLAUDE.md §14.1「messages 数组 user/assistant 严格交替;
// tool_result 与 tool_use 成对」。llm/anthropic 层只做 tool_use.input
// 归一与 assistant 文本兜底,**不修交替与配对** —— 本包必须自证合法:
//
//   - 失败轮只留 user 消息 → 下一轮追加 user 即相邻 user(上游 400);
//   - ConversationStore 裁剪 / Compact 的切口可能拆散 tool_use/tool_result 对;
//   - 历史里回放 thinking 等非白名单块会被严格代理拒绝。
//
// 出站唯一闸口:callLLM 入口调用 sanitizeForWire;存储保留原始留痕
// (前端历史展示不受影响)。
package citysystem

import (
	llmtypes "LsmAgentGame/llm/types"
)

// sanitizeForWire 把会话消息修整为合法的 Anthropic messages[] 形状:
//  1. 内容块白名单:只保留 text / tool_use / tool_result(§14.1 权威用例
//     的 messages[].content[] 只含这三种块);
//  2. 相邻同角色消息合并(拼接 content blocks);
//  3. 丢弃开头的 assistant 消息(第一条必须是 user);
//  4. tool_use / tool_result 成对校验:tool_result 只在**紧跟**持有其
//     tool_use_id 的 assistant 消息时保留(每 id 一次);没有 tool_result
//     收尾的 tool_use 从 assistant 消息剔除(消息变空则整条剔除);
//  5. 消息级空内容剔除;剔除造成的相邻同角色再合并一次。
func sanitizeForWire(msgs []llmtypes.Message) []llmtypes.Message {
	// 1. 块级白名单 + 空消息剔除。
	filtered := make([]llmtypes.Message, 0, len(msgs))
	for _, m := range msgs {
		blocks := make([]llmtypes.ContentBlock, 0, len(m.Content))
		for _, b := range m.Content {
			switch b.Type {
			case "text", "tool_use", "tool_result":
				blocks = append(blocks, b)
			}
		}
		if len(blocks) == 0 {
			continue
		}
		filtered = append(filtered, llmtypes.Message{Role: m.Role, Content: blocks})
	}
	// 2. 相邻同角色合并。
	merged := mergeAdjacentRoles(filtered)
	// 3. 开头必须是 user:丢弃前置 assistant(其 tool_result 在第 4 步按孤儿剔除)。
	for len(merged) > 0 && merged[0].Role == "assistant" {
		merged = merged[1:]
	}
	// 4. tool_use / tool_result 成对校验。
	out := make([]llmtypes.Message, 0, len(merged))
	pending := map[string]bool{} // 上一条 assistant 未被应答的 tool_use id
	owner := -1                  // 该 assistant 消息在 out 中的下标
	for _, m := range merged {
		if m.Role == "assistant" {
			// 到达新的 assistant:上一条的未应答 tool_use 即悬空,剔除。
			out = pruneDanglingToolUse(out, owner, pending)
			pending = map[string]bool{}
			for _, b := range m.Content {
				if b.Type == "tool_use" {
					pending[b.ID] = true
				}
			}
			owner = len(out)
			out = append(out, m)
			continue
		}
		// user:tool_result 只在命中上一条 assistant 的未应答 id 时保留。
		blocks := make([]llmtypes.ContentBlock, 0, len(m.Content))
		for _, b := range m.Content {
			if b.Type == "tool_result" {
				if pending[b.ToolUseID] {
					delete(pending, b.ToolUseID)
					blocks = append(blocks, b)
				}
				continue // 孤儿 / 重复 tool_result 剔除
			}
			blocks = append(blocks, b)
		}
		if len(blocks) == 0 {
			continue
		}
		out = append(out, llmtypes.Message{Role: "user", Content: blocks})
	}
	out = pruneDanglingToolUse(out, owner, pending)
	// 5. 剔除可能造成的相邻同角色再合并一次。
	return mergeAdjacentRoles(out)
}

// mergeAdjacentRoles 合并相邻同角色消息(拼接 content blocks;不改动输入)。
func mergeAdjacentRoles(msgs []llmtypes.Message) []llmtypes.Message {
	out := make([]llmtypes.Message, 0, len(msgs))
	for _, m := range msgs {
		if n := len(out); n > 0 && out[n-1].Role == m.Role {
			out[n-1].Content = append(out[n-1].Content, m.Content...)
			continue
		}
		out = append(out, llmtypes.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// pruneDanglingToolUse 从 msgs[owner] 剔除 pending 中的悬空 tool_use 块;
// 消息变空则整条剔除(owner 之后的下标会前移,调用方随后重算)。
func pruneDanglingToolUse(msgs []llmtypes.Message, owner int, pending map[string]bool) []llmtypes.Message {
	if owner < 0 || owner >= len(msgs) || len(pending) == 0 {
		return msgs
	}
	kept := make([]llmtypes.ContentBlock, 0, len(msgs[owner].Content))
	for _, b := range msgs[owner].Content {
		if b.Type == "tool_use" && pending[b.ID] {
			continue
		}
		kept = append(kept, b)
	}
	if len(kept) == 0 {
		return append(msgs[:owner], msgs[owner+1:]...)
	}
	msgs[owner].Content = kept
	return msgs
}
