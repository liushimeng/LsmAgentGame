// Package debateplayer — 辩方 Agent 的 Memory + Context。
//
// 2026-08-31 §20260831-01 — 简化版 Memory 实现:
//   - 线性追加 messages(本局全程保留)
//   - DebateContext 由发言历史推导(每次发言后追加)
//
// 不实现:压缩 / 跨局 MEMORY.md 迭代(后续版本)。
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

	room   *debate.DebateRoom
	teamID int
	seat   int
	role   debate.Role
	stance debate.Stance
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