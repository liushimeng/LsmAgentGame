// Package debateplayer — memory_compact.go: LLM 驱动的记忆压缩(§20260831-03)。
//
// 落地 docs/辩论比赛/05-辩论比赛工具与记忆系统设计.md §5:
//
//	触发条件: 消息数 > 50 或 字节数 > 300KB(轮次开始时检查)
//	压缩策略: 8 段结构化摘要(立场锁定声明)
//	   S1 辩题与立场        S2 我方核心论点与论据
//	   S3 对方核心论点与论据  S4 关键交锋点
//	   S5 我方发言摘要       S6 对方发言摘要
//	   S7 当前局势           S8 上次压缩以来的新增
//	增量模式: 有上次摘要 → PRESERVE/ADD/DELETE 增量更新
//	校验:     IsValidCompactSummary(8 段 + 长度 + 黑名单)失败保留原记忆
//
// 与狼人杀 wwplayer/memory_compact.go 同源模式(全量重建 + 视角锁定),
// 差异:辩论信息全对称,黑名单防的是摘要幻觉出「对方内部策略」,
// 而非阵营私有情报泄漏。
package debateplayer

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 压缩触发阈值(§05 §5.1)。
const (
	compactMsgThreshold   = 50      // 消息数阈值
	compactBytesThreshold = 300000  // 字节数阈值(≈300KB)
	compactKeepTailMsgs   = 8       // 压缩后保留的最近消息条数
	compactConvMaxBytes   = 24000   // 送压缩的对话文本截断上限
	compactTimeoutSec     = 60      // 压缩 LLM 调用超时
)

// COMPACT_SYSTEM_PROMPT 压缩任务 system prompt(§05 §5.3,立场锁定版)。
const COMPACT_SYSTEM_PROMPT = `你是辩论比赛的记忆压缩助手。你的任务是把给定的对话历史压缩为 8 段结构化摘要,供该辩手后续发言决策使用。

不要继续对话。不要回答对话中的问题。只输出结构化摘要。

**身份锁定声明**:
- 当前辩手: %s(%s/%s)
- 辩题: %s
- 严格只输出该辩位在公开辩论流程内可见的事实与论点

**摘要必须包含以下 8 段**(段名固定,每段 ≤ 80 字):
## S1. 辩题与立场
## S2. 我方核心论点与论据
## S3. 对方核心论点与论据
## S4. 关键交锋点
## S5. 我方发言摘要
## S6. 对方发言摘要
## S7. 当前局势
## S8. 上次压缩以来的新增

**强制格式要求**:
- 保持简洁,总长度 ≤ 600 字
- 论点引用必须注明来源(对方 X 辩)
- 不得编造未出现在对话中的内容`

// COMPACT_USER_PROMPT 全量压缩 user prompt。
const COMPACT_USER_PROMPT = `以下是辩论比赛的对话历史。请压缩为 8 段结构化摘要。

<conversation>
%s
</conversation>

请按照 system prompt 中的格式输出 8 段摘要。`

// COMPACT_UPDATE_PROMPT 增量更新 user prompt(§05 §5.5)。
const COMPACT_UPDATE_PROMPT = `以下是辩论比赛的对话历史,以及上一次压缩得到的 8 段摘要。

<previous_summary>
%s
</previous_summary>

<conversation>
%s
</conversation>

请输出一份**更新后**的 8 段摘要,要求:

1. **PRESERVE 优先级**:
   - S1 辩题与立场(不可丢失)
   - S2 我方核心论点(不可丢失)
   - S3 对方核心论点(不可丢失)
   - S4 关键交锋点
2. **ADD**: 把新事实按 S1-S7 分类追加,新增内容同时写入 S8
3. **DELETE/REWRITE**: 仅在旧要点被新事实明确推翻时
4. **保持简洁**: 总长度 ≤ 600 字`

// compactForbiddenKeywords 摘要黑名单(§05 §5.4 立场校验):
// 辩论信息全对称,不存在对方私有情报;出现这些词说明 LLM 在幻觉
// 「偷看对方内部」,视为压缩失败。
var compactForbiddenKeywords = []string{"对方策略", "对方计划", "对方内部"}

// ShouldCompact 判断是否触发压缩(消息数或字节数超阈值)。
func (m *Memory) ShouldCompact() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.messages) > compactMsgThreshold {
		return true
	}
	return approxMessagesBytes(m.messages) > compactBytesThreshold
}

// approxMessagesBytes 估算 messages 总字节数(text / tool_use input / tool_result 文本)。
func approxMessagesBytes(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		for _, c := range m.Content {
			total += len(c.Text) + len(c.Name) + len(c.ToolUseID)
			for k, v := range c.Input {
				total += len(k) + len(fmt.Sprint(v))
			}
			for _, sub := range c.Content {
				total += len(sub.Text)
			}
		}
	}
	return total
}

// LastCompactSummary 返回上次压缩摘要(可能为空)。
func (m *Memory) LastCompactSummary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCompactSummary
}

// CompactCount 返回累计压缩次数。
func (m *Memory) CompactCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.compactCount
}

// compactMemory 执行一次压缩(同步,须在 LLM 槽位持有期内调用)。
//
// 成功:memory 重建为 [user: 摘要头] + 最近 compactKeepTailMsgs 条消息,
// lastCompactSummary 更新,compactCount++。
// 失败:原样保留(下次触发再试),仅记日志。
func (a *Agent) compactMemory() {
	if a.provider == nil {
		return
	}
	snapshot := a.memory.Snapshot()
	if len(snapshot) == 0 {
		return
	}

	// 保留尾部消息不参与压缩(当前轮上下文)
	var head, tail []llm.Message
	if len(snapshot) > compactKeepTailMsgs {
		head = snapshot[:len(snapshot)-compactKeepTailMsgs]
		tail = snapshot[len(snapshot)-compactKeepTailMsgs:]
	} else {
		// 消息太少(理论上不应触发,字节超限时可能)——只压 head 为空则无意义
		head = snapshot
		tail = nil
	}

	conv := serializeForCompact(head)
	if strings.TrimSpace(conv) == "" {
		return
	}

	roleCN := debate.RoleCN(a.Role)
	stanceCN := debate.StanceLabel(a.Stance)
	system := fmt.Sprintf(COMPACT_SYSTEM_PROMPT, roleCN, stanceCN, string(a.Role), a.topicText())

	var userPrompt string
	if prev := a.memory.LastCompactSummary(); prev != "" {
		userPrompt = fmt.Sprintf(COMPACT_UPDATE_PROMPT, prev, conv)
	} else {
		userPrompt = fmt.Sprintf(COMPACT_USER_PROMPT, conv)
	}

	ctx, cancel := context.WithTimeout(a.ctx, compactTimeoutSec*time.Second)
	defer cancel()

	resp, err := a.provider.Chat(ctx, a.apiKey, llm.LLMRequest{
		AgentClassName: string(agentroot.AgentClassDebateMemoryCompact),
		Model:          a.ModelKey,
		System:         []llm.SystemBlock{{Type: "text", Text: system}},
		Messages: []llm.Message{
			{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: userPrompt}}},
		},
		MaxTokens: 1024,
	})
	if err != nil {
		logger.L().Warn("debate agent: memory compact LLM call failed, keep original",
			zap.String("room_id", a.RoomID),
			zap.Int("team", a.TeamID),
			zap.Int("seat", a.Seat),
			zap.Error(err))
		return
	}

	summary := compactSummaryText(resp)
	if summary == "" {
		logger.L().Warn("debate agent: memory compact returned empty, keep original",
			zap.String("room_id", a.RoomID))
		return
	}
	if ok, reason := IsValidCompactSummary(summary); !ok {
		logger.L().Warn("debate agent: memory compact summary invalid, keep original",
			zap.String("room_id", a.RoomID),
			zap.String("reason", reason),
			zap.Int("summary_len", len(summary)))
		return
	}

	// 重建:摘要头(user)+ 尾部消息。尾部若以悬空 tool_result 开头,
	// 请求期 sanitizeDebateMessages 会再兜底。
	headMsg := llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{Type: "text", Text: fmt.Sprintf(
			"【记忆摘要】以下是本局此前辩论的结构化摘要(第 %d 次压缩):\n%s\n【摘要结束】",
			a.memory.CompactCount()+1, summary)}},
	}
	rebuilt := append([]llm.Message{headMsg}, tail...)
	a.memory.Replace(rebuilt)
	a.memory.setLastCompactSummary(summary)
	a.memory.incCompactCount()

	logger.L().Info("debate agent: memory compacted",
		zap.String("room_id", a.RoomID),
		zap.Int("team", a.TeamID),
		zap.Int("seat", a.Seat),
		zap.Int("msgs_before", len(snapshot)),
		zap.Int("msgs_after", len(rebuilt)),
		zap.Int("summary_len", len(summary)))
}

// compactSummaryText 从压缩响应提取纯文本摘要。
func compactSummaryText(resp llm.LLMResponse) string {
	var b strings.Builder
	for _, blk := range resp.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// IsValidCompactSummary 校验压缩摘要合法性(§05 §5.4)。
//
// 规则:
//  1. 非空且长度 ≤ 2000
//  2. 必须包含全部 8 个段标题(## S1. ~ ## S8.)
//  3. 不得包含黑名单关键词(对方策略/对方计划/对方内部)
//
// 返回 (是否有效, 失败原因)。
func IsValidCompactSummary(summary string) (bool, string) {
	if strings.TrimSpace(summary) == "" {
		return false, "empty summary"
	}
	if len(summary) > 2000 {
		return false, fmt.Sprintf("summary too long: %d > 2000", len(summary))
	}
	for i := 1; i <= 8; i++ {
		section := fmt.Sprintf("## S%d.", i)
		if !strings.Contains(summary, section) {
			return false, "missing section " + section
		}
	}
	for _, kw := range compactForbiddenKeywords {
		if strings.Contains(summary, kw) {
			return false, "forbidden keyword: " + kw
		}
	}
	return true, ""
}

// serializeForCompact 把消息序列化为压缩用对话文本(每块截断,总量截断)。
func serializeForCompact(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			b.WriteString("[观众/主持→你]\n")
		case "assistant":
			b.WriteString("[你]\n")
		default:
			b.WriteString("[" + m.Role + "]\n")
		}
		for _, c := range m.Content {
			switch c.Type {
			case "text":
				b.WriteString("  " + truncateRunes(c.Text, 400) + "\n")
			case "tool_use":
				b.WriteString("  [tool_use " + c.Name + "] " + truncateRunes(fmt.Sprint(c.Input), 200) + "\n")
			case "tool_result":
				var inner strings.Builder
				for _, sub := range c.Content {
					if sub.Type == "text" {
						inner.WriteString(sub.Text)
					}
				}
				b.WriteString("  [tool_result] " + truncateRunes(inner.String(), 120) + "\n")
			}
		}
		if b.Len() > compactConvMaxBytes {
			break
		}
	}
	out := b.String()
	if len(out) > compactConvMaxBytes {
		out = out[:compactConvMaxBytes]
	}
	return out
}

// topicText 返回辩题文本(空安全)。
func (a *Agent) topicText() string {
	if r := a.engine.Room(); r != nil {
		return r.Config.Topic.Text
	}
	return ""
}
