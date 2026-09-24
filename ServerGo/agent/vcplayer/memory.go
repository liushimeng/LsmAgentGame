// Package vcplayer — memory.go: 月度决策记忆(2026-09-14 §财商流P0)。
//
// P0 范围(Agent 设计文档 §8):
//   - 局内滚动 24 月月度决策 + KeyFacts(20 条上限)
//   - 超过 24 月由规则式压缩生成 CompactSummary(不调 LLM,P2)
//   - 组装 prompt 时注入:最近 6 条 MonthDecision 全文 + CompactSummary + KeyFacts
package vcplayer

import "sync"

// Memory 是单 Agent 的局内记忆。
type Memory struct {
	mu             sync.Mutex
	Monthly        []MonthDecision // 最近 24 月滚动
	KeyFacts       []string        // 上限 20 条
	CompactSummary string          // 规则式压缩(>24 月)
}

const (
	memoryMonthlyCap = 24
	memoryKeyFactsCap = 20
	memoryPromptWindow = 6
)

// MonthDecision 单月决策摘要。
type MonthDecision struct {
	Month      int
	Actions    []string // 人读动作列表(≤3 条)
	SpeakText  string
	CashAfter  int64
	NetAfter   int64
	FI         float64
	Summary    string // ≤80 字
}

// AppendDecision 追加本月决策;满 24 月时压缩最早 6 月 → CompactSummary。
func (m *Memory) AppendDecision(d MonthDecision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Monthly = append(m.Monthly, d)
	if len(m.Monthly) > memoryMonthlyCap {
		old := m.Monthly[:len(m.Monthly)-memoryMonthlyCap]
		m.Monthly = m.Monthly[len(m.Monthly)-memoryMonthlyCap:]
		m.compactLocked(old)
	}
}

// compactLocked 规则式压缩:把最早 n 条拼成简短摘要(>24 月时)。
func (m *Memory) compactLocked(older []MonthDecision) {
	if len(older) == 0 {
		return
	}
	var b []byte
	b = append(b, "更早记忆摘要:\n"...)
	for _, d := range older {
		// b = append(b, fmt.Sprintf("[月%d] %s\n", d.Month, d.Summary)...)
		b = append(b, "["...)
		b = append(b, []byte(itoa(d.Month))...)
		b = append(b, "] "...)
		b = append(b, d.Summary...)
		b = append(b, '\n')
	}
	m.CompactSummary = string(b)
}

// AddKeyFact 记录大事记;满 20 条覆盖最旧。
func (m *Memory) AddKeyFact(fact string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.KeyFacts) >= memoryKeyFactsCap {
		m.KeyFacts = m.KeyFacts[1:]
	}
	m.KeyFacts = append(m.KeyFacts, fact)
}

// RenderForPrompt 返回注入 user prompt 的记忆段(最近 6 月 + 压缩摘要 + KeyFacts)。
func (m *Memory) RenderForPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Monthly) == 0 && len(m.KeyFacts) == 0 && m.CompactSummary == "" {
		return ""
	}
	var b []byte
	if m.CompactSummary != "" {
		b = append(b, m.CompactSummary...)
		b = append(b, '\n')
	}
	start := 0
	if len(m.Monthly) > memoryPromptWindow {
		start = len(m.Monthly) - memoryPromptWindow
	}
	for _, d := range m.Monthly[start:] {
		b = append(b, "["...)
		b = append(b, []byte(itoa(d.Month))...)
		b = append(b, "] "...)
		b = append(b, d.Summary...)
		if len(d.Actions) > 0 {
			b = append(b, "(动作:"...)
			for i, a := range d.Actions {
				if i > 0 {
					b = append(b, ',')
				}
				b = append(b, a...)
			}
			b = append(b, ")\n"...)
		} else {
			b = append(b, '\n')
		}
	}
	if len(m.KeyFacts) > 0 {
		b = append(b, "大事:"...)
		for i, f := range m.KeyFacts {
			if i > 0 {
				b = append(b, ';')
			}
			b = append(b, f...)
		}
		b = append(b, '\n')
	}
	return string(b)
}

// NewMemory 构造空记忆。
func NewMemory() *Memory { return &Memory{} }

// itoa 小型 int→string(避免 strconv 引入模板包)。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}