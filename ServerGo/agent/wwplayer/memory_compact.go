// Package wwplayer — memory_compact.go: LLM 驱动的记忆压缩。
// 灵感来源: PI Agent 的 compaction/compaction.ts — 结构化摘要 + 增量更新。
//
// 与 PI 的差异:
//   - PI 用独立 LLM 调用做摘要;狼人杀复用 bot 自己的 provider (节省 HTTP)
//   - PI 增量更新 (previousSummary + new);狼人杀全量重建 (单局生命周期短)
//   - PI 追踪文件操作;狼人杀追踪游戏事实 (已确认/待验证)
//   - PI 使用 contextWindow - reserveTokens 阈值;狼人杀用消息数 + 字节数
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"LsmWebGame/llm"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// COMPACT_SYSTEM_PROMPT 压缩任务的 system prompt。
// 与 PI 的 SUMMARIZATION_SYSTEM_PROMPT 对齐,但适配狼人杀游戏语境。
const COMPACT_SYSTEM_PROMPT = `你是一个游戏历史摘要助手。你的任务是将狼人杀游戏的对话历史压缩为结构化摘要，供后续 LLM 决策使用。

不要继续对话。不要回答对话中的问题。只输出结构化摘要。

摘要必须包含以下段落(可为空段写"暂无")：
## 本局概况
## 已确认信息
## 关键决策
## 待验证信息

保持简洁。保留具体的座位号、角色名、行动时间点。`

// COMPACT_PROMPT 是压缩任务的 user prompt 模板。
const COMPACT_PROMPT = `以下是狼人杀游戏第 %d 局、座位 %d 号(%s/%s)的对话历史。请压缩为结构化摘要。

<conversation>
%s
</conversation>

请按照 system prompt 中的格式输出摘要。重点关注：
1. 本局基本态势（已知角色、存活人数、当前阶段）
2. 已确认的事实（查验结果、已知身份、已发生的行动）
3. 关键决策及其理由
4. 需要继续关注的疑点`

// CompactConfig 控制记忆压缩行为。
type CompactConfig struct {
	// Enabled 是否启用 LLM 压缩。
	Enabled bool
	// MinMessages 触发压缩的最小消息数。
	MinMessages int
	// MaxTokens LLM 压缩调用的 max_tokens。
	MaxTokens int
	// TimeoutSec 压缩调用的超时秒数。
	TimeoutSec int
}

// DefaultCompactConfig 返回默认压缩配置。
func DefaultCompactConfig() CompactConfig {
	return CompactConfig{
		Enabled:     true,
		MinMessages: 40,
		MaxTokens:   1024,
		TimeoutSec:  30,
	}
}

// CompactResult 压缩操作的结果。
type CompactResult struct {
	Success      bool
	Summary      string
	MessagesBefore int
	MessagesAfter  int
	TokensUsed     int
	DurationMs     int64
	Error          error
}

// CompactWithLLM 使用 LLM 将旧消息压缩为结构化游戏摘要。
//
// 流程:
//  1. 检查消息数是否达到阈值
//  2. 分离: 旧消息 (待压缩,前 1/3) + 近端消息 (保留)
//  3. 构建压缩 prompt (旧消息序列化 + 游戏上下文)
//  4. LLM 调用 (短超时,低 token)
//  5. 替换: identity + compact摘要 + 近端消息
//
// 失败时不影响正常流程 (返回 error,调用方忽略即可)。
func (m *Memory) CompactWithLLM(
	ctx context.Context,
	provider llm.LLMProvider,
	apiKey string,
	modelKey string,
	gc *wwtypes.GameContext,
	cfg CompactConfig,
) CompactResult {
	if !cfg.Enabled {
		return CompactResult{Success: false, Error: fmt.Errorf("compact disabled")}
	}

	m.mu.RLock()
	msgCount := len(m.messages)
	m.mu.RUnlock()

	if msgCount < cfg.MinMessages {
		return CompactResult{Success: false, Error: fmt.Errorf("messages %d < min %d", msgCount, cfg.MinMessages)}
	}

	startTime := time.Now()

	// 1. 分离: 旧消息 (前 1/3) + 近端消息 (后 2/3)
	m.mu.Lock()
	msgs := make([]llm.Message, len(m.messages))
	copy(msgs, m.messages)
	m.mu.Unlock()

	splitIdx := msgCount / 3
	if splitIdx < 5 {
		splitIdx = 5 // 至少保留 5 条旧消息用于压缩
	}
	if splitIdx > msgCount-10 {
		splitIdx = msgCount - 10 // 至少保留 10 条近端消息
	}

	oldMsgs := msgs[1:splitIdx] // 跳过 identity (index 0)
	recentMsgs := msgs[splitIdx:]

	// 2. 序列化旧消息
	conversationText := serializeMessagesForCompact(oldMsgs)
	if len(conversationText) > 8000 {
		conversationText = conversationText[:8000] + "\n...(截断)"
	}

	// 3. 构建压缩 prompt
	seat := 0
	role := "unknown"
	faction := "unknown"
	round := 0
	if gc != nil {
		seat = gc.MySeat
		round = gc.Round
		if gc.Role != "" {
			role = gc.Role
		}
		if gc.Faction != "" {
			faction = gc.Faction
		}
	}
	userPrompt := fmt.Sprintf(COMPACT_PROMPT, round, seat, role, faction, conversationText)

	// 4. LLM 调用
	compactCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	req := llm.LLMRequest{
		Model:     modelKey,
		MaxTokens: cfg.MaxTokens,
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}}},
		},
		System:         []llm.SystemBlock{{Type: "text", Text: COMPACT_SYSTEM_PROMPT}},
		AgentClassName: "LsmWebGame-Werewolf-MemoryCompact",
	}

	resp, err := provider.Chat(compactCtx, apiKey, req)
	if err != nil {
		return CompactResult{
			Success:        false,
			MessagesBefore: msgCount,
			Error:          fmt.Errorf("compact LLM call failed: %w", err),
		}
	}

	// 5. 提取摘要文本
	summary := extractCompactSummary(&resp)
	if summary == "" {
		return CompactResult{
			Success:      false,
			MessagesBefore: msgCount,
			Error:          fmt.Errorf("compact LLM returned empty summary"),
		}
	}

	// 6. 构建压缩摘要消息
	compactMsg := llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("【游戏历史摘要】\n%s", summary),
		}},
	}

	// 7. 替换: identity + compact + recent
	m.mu.Lock()
	m.messages = append([]llm.Message{msgs[0], compactMsg}, recentMsgs...)
	newCount := len(m.messages)
	m.mu.Unlock()

	duration := time.Since(startTime).Milliseconds()

	return CompactResult{
		Success:        true,
		Summary:        summary,
		MessagesBefore: msgCount,
		MessagesAfter:  newCount,
		TokensUsed:     resp.Usage.InputTokens + resp.Usage.OutputTokens,
		DurationMs:     duration,
	}
}

// extractCompactSummary 从 LLM 响应中提取摘要文本。
func extractCompactSummary(resp *llm.LLMResponse) string {
	if resp == nil {
		return ""
	}
	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// serializeMessagesForCompact 将消息序列化为可读文本 (用于压缩 prompt)。
func serializeMessagesForCompact(msgs []llm.Message) string {
	var sb strings.Builder
	for _, msg := range msgs {
		role := msg.Role
		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					sb.WriteString(fmt.Sprintf("[%s] %s\n", role, truncateCompact(block.Text, 200)))
				}
			case "tool_use":
				inputStr := "{}"
				if block.Input != nil {
					if b, err := json.Marshal(block.Input); err == nil {
						inputStr = string(b)
					}
				}
				sb.WriteString(fmt.Sprintf("[%s] tool_use: %s(%s)\n", role, block.Name, truncateCompact(inputStr, 100)))
			case "tool_result":
				// tool_result 的 Content 是 []ContentBlock,需要提取文本
				contentStr := extractTextFromBlocks(block.Content)
				if block.IsError {
					contentStr = "ERROR: " + contentStr
				}
				sb.WriteString(fmt.Sprintf("[%s] tool_result[%s]: %s\n", role, block.ToolUseID, truncateCompact(contentStr, 200)))
			}
		}
	}
	return sb.String()
}

// extractTextFromBlocks 从 ContentBlock 切片中提取所有文本。
func extractTextFromBlocks(blocks []llm.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// truncateCompact 截断字符串到指定长度。
func truncateCompact(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
