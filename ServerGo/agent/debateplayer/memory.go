// Package debateplayer — 辩方 Agent 的 Memory + Context。
//
// 2026-08-31 §20260831-01 — Memory 实现:
//   - 线性追加 messages(assistant / tool_result 全量保留,供多轮 tool_use 循环)
//   - DebateContext 由发言历史推导(每次发言后追加)
//
// 2026-08-31 §20260831-02/03 — 增强:
//   - sanitizeDebateMessages:请求期消息清洗(§14.1 交替 + tool 配对安全网)
//   - 记忆压缩触发 / 状态字段(见 memory_compact.go,8 段结构化摘要)
//
// 不实现:跨局 MEMORY.md 迭代(后续版本)。
package debateplayer

import (
	"sync"

	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"
)

// Memory 辩方 Bot 的多轮记忆。
type Memory struct {
	mu       sync.RWMutex
	messages []llm.Message

	// §20260831-03 — 压缩状态(§05 §5)
	lastCompactSummary string
	compactCount       int

	room   *debate.DebateRoom
	teamID int
	seat   int
	role   debate.Role
	stance debate.Stance
}

// setLastCompactSummary 更新压缩摘要(仅 memory_compact.go 调用)。
func (m *Memory) setLastCompactSummary(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCompactSummary = s
}

// incCompactCount 压缩次数 +1。
func (m *Memory) incCompactCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.compactCount++
}

// NewMemory 构造记忆(注入身份信息)。
func NewMemory(room *debate.DebateRoom, teamID, seat int, role debate.Role, stance debate.Stance) *Memory {
	return &Memory{
		room:   room,
		teamID: teamID,
		seat:   seat,
		role:   role,
		stance: stance,
	}
}

// Append 追加一条消息。
func (m *Memory) Append(msg llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
}

// Replace 整体替换消息(记忆压缩后重建用,§20260831-03)。
func (m *Memory) Replace(msgs []llm.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = msgs
}

// Snapshot 返回消息快照。
func (m *Memory) Snapshot() []llm.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]llm.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

// Length 返回消息数。
func (m *Memory) Length() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messages)
}

// DebateContext 当前可见上下文(供 buildUserPrompt 使用)。
type DebateContext struct {
	Topic         string
	TopicType     string
	MyStance      string
	MyRole        string
	Phase         debate.Phase
	TimeRemaining int

	MyArguments  []string
	OppArguments []string

	RecentSpeeches []SpeechSummary

	// PendingQuestion §20260831-02:若我是当前质询对的被质询方,
	// 这里携带待答问题(id + 文本),prompt 会引导正面回答。
	PendingQuestionID   string
	PendingQuestionText string
}

// SpeechSummary 简化发言信息。
type SpeechSummary struct {
	Stance  string
	Role    string
	Content string
}

// collectContext 从 DebateRoom 构造 DebateContext。
func (a *Agent) collectContext() DebateContext {
	room := a.engine.Room()
	if room == nil {
		return DebateContext{}
	}

	gc := DebateContext{
		Topic:         room.Config.Topic.Text,
		TopicType:     room.Config.Topic.Type,
		MyStance:      string(a.Stance),
		MyRole:        string(a.Role),
		Phase:         room.Phase(),
		TimeRemaining: room.PhaseTimeRemainingSec(),
	}

	// 提取论点(从历史发言中提炼)
	gc.MyArguments = extractArguments(room, a.TeamID, true)
	gc.OppArguments = extractArguments(room, a.TeamID, false)

	// 最近 5 条发言
	speeches := room.RecentSpeeches(5)
	gc.RecentSpeeches = make([]SpeechSummary, 0, len(speeches))
	for _, s := range speeches {
		gc.RecentSpeeches = append(gc.RecentSpeeches, SpeechSummary{
			Stance:  string(s.Stance),
			Role:    string(s.Role),
			Content: s.Content,
		})
	}

	// §20260831-02 — 若我是当前质询对的被质询方,取待答问题(id + 文本)。
	// 修复:此前回答方 Bot 既不知道问题文本也不知道 question_id,
	// LLM 只能瞎编参数导致「有问无答」。
	if qKey, aKey, ok := room.CrossExamActive(); ok {
		if mt, ms, p1 := debate.ParseSeatKey(aKey); p1 && mt == a.TeamID && ms == a.Seat {
			// 找提问方最新一条未回答的问题
			entries := room.CrossExamEntries()
			for i := len(entries) - 1; i >= 0; i-- {
				e := entries[i]
				if !e.IsAnswer && e.Questioner == qKey {
					gc.PendingQuestionID = e.ID
					gc.PendingQuestionText = e.Question
					break
				}
			}
		}
	}

	return gc
}

// extractArguments 从发言中提取论点(简化版:取每条发言的前 80 字)。
func extractArguments(room *debate.DebateRoom, myTeamID int, isMyTeam bool) []string {
	out := []string{}
	speeches := room.Speeches()
	for _, s := range speeches {
		if (s.TeamID == myTeamID) != isMyTeam {
			continue
		}
		if s.Content == "" {
			continue
		}
		// 取每条发言第一句
		first := firstSentence(s.Content, 80)
		if first != "" {
			out = append(out, first)
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// firstSentence 提取第一句(简化版:找第一个句号/问号/感叹号)。
func firstSentence(s string, max int) string {
	for i, r := range s {
		if i >= max {
			return s[:i] + "..."
		}
		if r == '。' || r == '!' || r == '?' {
			return s[:i+1]
		}
	}
	return s
}

// ============================================================================
// §20260831-02 请求期消息清洗(对齐 CLAUDE.md §14.1 Anthropic 协议约束)
// ============================================================================

// sanitizeDebateMessages 把 memory 快照清洗为可直接下发的 messages:
//
//  1. 收集 assistant 消息中的全部 tool_use id;
//  2. user 消息里引用未知 id 的 tool_result 块(悬空孤儿)直接剔除;
//  3. 相邻同 role 消息合并(user+user / assistant+assistant → 单条拼接);
//     —— fallback 路径只追加 assistant 文本、不入 tool_use,不会破坏配对;
//  4. 剔除后为空的消息丢弃;首条若为 assistant 则丢弃(对话必须 user 开头)。
//
// 该函数是 §14.1「messages 严格交替 + tool_use/tool_result 配对」的
// 请求期安全网(与狼人杀 SanitizeMessagesForAnthropic 同职责的辩论轻量版)。
func sanitizeDebateMessages(msgs []llm.Message) []llm.Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Pass 1: 收集已知 tool_use id
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

	// Pass 2: 剔除悬空 tool_result 块 + 过滤空消息
	cleaned := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		var content []llm.ContentBlock
		for _, c := range m.Content {
			if c.Type == "tool_result" && c.ToolUseID != "" && !knownUseIDs[c.ToolUseID] {
				continue // 悬空孤儿:其配对的 tool_use 已不在快照内
			}
			content = append(content, c)
		}
		if len(content) == 0 {
			continue
		}
		cm := m
		cm.Content = content
		cleaned = append(cleaned, cm)
	}

	// Pass 3: 相邻同 role 合并
	merged := make([]llm.Message, 0, len(cleaned))
	for _, m := range cleaned {
		if n := len(merged); n > 0 && merged[n-1].Role == m.Role {
			merged[n-1].Content = append(merged[n-1].Content, m.Content...)
			continue
		}
		merged = append(merged, m)
	}

	// Pass 4: 对话必须以 user 开头
	for len(merged) > 0 && merged[0].Role != "user" {
		merged = merged[1:]
	}
	return merged
}