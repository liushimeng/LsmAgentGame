// Package wwjudge — 法官 Agent 多轮记忆环形缓冲 (§20260809-02 U1)。
//
// 设计动机:
//   AgentJudge 当前每次唤醒都是单轮无状态调用(judge.go:336-341),
//   导致法官不可能说出"正如昨天所说""这已是本局第三次平票"
//   这类承上启下的话。本文件实现一个轻量环形缓冲:
//
//   - 仅存法官自己的输出文本(announce / summary),不存 GameSnapshot
//     (含 WolfSeats / SeerCheckResults 等全知视野)——§119 协议层隔离约束。
//   - 容量默认 20 条;超过自动覆盖最早。
//   - 写入/读取全程加锁(简单 sync.Mutex,无 r.mu 跨包冲突,
//     不涉及 WerewolfRoom 锁路径)。
//   - 历史回填仅放 assistant 角色消息(Anthropic 协议要求严格交替),
//     复用 llm.SanitizeMessagesForAnthropic 即可。
//
// 复用模式:
//   - startJudgeGoroutine 启动时 Memory = NewJudgeMemoryRing(20)
//   - 每次 announce / summary 成功后 Append(r, kind, text)
//   - judgeChatOrFallback 构造 Messages 前 PrependHistory(req.Messages)
//   - stopAgentsLocked / reset 时 Memory.Reset()
package wwjudge

import (
	"sync"
	"time"
)

// JudgeMemoryRing 是 AgentJudge 的轻量环形缓冲。
type JudgeMemoryRing struct {
	mu       sync.Mutex
	capacity int
	entries  []JudgeMemoryEntry
}

// JudgeMemoryEntry 是环形缓冲里的一条历史记录。
// 只存"已广播的公开文本",绝不存 GameSnapshot(§119)。
type JudgeMemoryEntry struct {
	Round     int    `json:"round"`
	Phase     string `json:"phase"`
	WakeKind  string `json:"wake_kind"` // announce / prompt_actor / summary / declare_cause
	Text      string `json:"text"`      // 法官本次 announce 的原文
	Timestamp int64  `json:"ts"`
}

// NewJudgeMemoryRing 构造一个容量为 capacity 的环形缓冲。
// capacity <= 0 走默认 20。
func NewJudgeMemoryRing(capacity int) *JudgeMemoryRing {
	if capacity <= 0 {
		capacity = 20
	}
	return &JudgeMemoryRing{
		capacity: capacity,
		entries:  make([]JudgeMemoryEntry, 0, capacity),
	}
}

// Append 在持锁态追加一条历史。容量满则覆盖最早一条(FIFO)。
func (m *JudgeMemoryRing) Append(entry JudgeMemoryEntry) {
	if m == nil {
		return
	}
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().Unix()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= m.capacity {
		// FIFO:丢弃最旧的,腾出位置。
		m.entries = append(m.entries[1:], entry)
		return
	}
	m.entries = append(m.entries, entry)
}

// Snapshot 返回当前历史的快照(最近 capacity 条,FIFO 顺序)。
// 不返回内部 slice,避免外部修改污染。
func (m *JudgeMemoryRing) Snapshot() []JudgeMemoryEntry {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) == 0 {
		return nil
	}
	out := make([]JudgeMemoryEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

// Len 返回当前历史条数(用于测试断言)。
func (m *JudgeMemoryRing) Len() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// Reset 清空历史(用于 stopAgentsLocked / 房间销毁 / 冷却期重置)。
func (m *JudgeMemoryRing) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = m.entries[:0]
}

// Capacity 返回当前容量上限(测试用)。
func (m *JudgeMemoryRing) Capacity() int {
	if m == nil {
		return 0
	}
	return m.capacity
}
