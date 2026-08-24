// Package thpagent — memory.go: 德州扑克 Bot 短期记忆（2026-08-19）。
//
// 设计原则：
//   - 短期 Memory 仅本局生效（HandRecord 数组, 最多保留最近 5 手牌）
//   - 持久化记忆（MEMORY.md）由 thpagent/memory_persist.go 单独处理（v1.1 再实现）
//   - 与狼人杀 Memory 平行：接口类似但仅 HandRecord + OpponentStats
//
// 锁语义：所有公开方法线程安全（内部 mu 保护）。
package thpagent

import (
	"sync"
)

// Memory 是德州扑克 Bot 的短期记忆。
type Memory struct {
	mu sync.Mutex

	// RecentHands 最近 N 手牌回顾（v1.0 默认保留 5 手）
	RecentHands []HandRecord

	// OpponentStats 每个对手的累计统计（按 UserID 索引）
	OpponentStats map[string]*OpponentStat

	// CurrentHandActions 本手牌所有动作（按时间顺序,包括自己的）
	CurrentHandActions []ActionRecordForMemory

	// LastDecisionSummary 最近决策摘要（驱动 LastDecisionSummary 字段）
	LastDecisionSummary string
}

// HandRecord 是单手牌回顾记录（与 thptypes.HandRecord 平行, 避免循环 import）。
type HandRecord struct {
	HandNumber   int
	MySeat       int
	MyHole       [2]int
	Community    [5]int
	CommunityLen int
	Winners      []int
	NetChipDelta int
}

// ActionRecordForMemory 是单个动作记录（与 thptypes.ActionRecord 平行）。
type ActionRecordForMemory struct {
	Seat       int
	ActionType string
	Amount     int
	Street     string
}

// OpponentStat 是单个对手的累计统计。
type OpponentStat struct {
	UserID         string
	Seat           int
	HandsPlayed    int  // 共同参与的手牌数
	HandsWon       int  // 对手胜出的手牌数
	TotalFold      int  // 对手弃牌次数
	TotalCall      int  // 对手跟注次数
	TotalRaise     int  // 对手加注次数
	TotalAllIn     int  // 对手全押次数
	NetChips       int  // 对手净盈亏
	FoldRate       float64 // 弃牌率 = TotalFold / 参与押注轮次数
}

// NewMemory 构造一个空的 Memory。
func NewMemory() *Memory {
	return &Memory{
		RecentHands:    make([]HandRecord, 0, 5),
		OpponentStats:  make(map[string]*OpponentStat),
		CurrentHandActions: make([]ActionRecordForMemory, 0, 20),
	}
}

// AppendHand 追加一手牌回顾。保留最近 maxHands 手。
func (m *Memory) AppendHand(h HandRecord, maxHands int) {
	if maxHands <= 0 {
		maxHands = 5
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecentHands = append(m.RecentHands, h)
	if len(m.RecentHands) > maxHands {
		// 移除最早的记录
		m.RecentHands = m.RecentHands[len(m.RecentHands)-maxHands:]
	}
}

// RecentHandsSnapshot 返回最近 N 手牌快照（线程安全）。
func (m *Memory) RecentHandsSnapshot() []HandRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]HandRecord, len(m.RecentHands))
	copy(out, m.RecentHands)
	return out
}

// RecentHandsSnapshotLength 返回 RecentHands 当前长度(避免调用方再走 Snapshot
// 仅取 len 时付出整片切片拷贝成本,§20260824-01)。
func (m *Memory) RecentHandsSnapshotLength() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.RecentHands)
}

// RecordAction 记录本手牌的一个动作。
func (m *Memory) RecordAction(act ActionRecordForMemory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CurrentHandActions = append(m.CurrentHandActions, act)
}

// ResetCurrentHand 清空本手牌动作记录（每手牌开始时调用）。
func (m *Memory) ResetCurrentHand() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CurrentHandActions = make([]ActionRecordForMemory, 0, 20)
}

// CurrentHandActionsSnapshot 返回本手牌所有动作快照。
func (m *Memory) CurrentHandActionsSnapshot() []ActionRecordForMemory {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ActionRecordForMemory, len(m.CurrentHandActions))
	copy(out, m.CurrentHandActions)
	return out
}

// UpdateOpponentStat 更新单个对手的统计（fold/call/raise/allin 各 +1）。
func (m *Memory) UpdateOpponentStat(userID string, seat int, actionType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.OpponentStats[userID]
	if !ok {
		stat = &OpponentStat{UserID: userID, Seat: seat}
		m.OpponentStats[userID] = stat
	}
	switch actionType {
	case "fold":
		stat.TotalFold++
	case "call", "check":
		stat.TotalCall++
	case "raise", "bet":
		stat.TotalRaise++
	case "allin":
		stat.TotalAllIn++
	}
	totalBets := stat.TotalFold + stat.TotalCall + stat.TotalRaise + stat.TotalAllIn
	if totalBets > 0 {
		stat.FoldRate = float64(stat.TotalFold) / float64(totalBets)
	}
}

// IncrementHandsPlayed 增加对手参与手牌计数（每手牌 StartHand 时调用所有存活座位）。
func (m *Memory) IncrementHandsPlayed(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.OpponentStats[userID]
	if !ok {
		stat = &OpponentStat{UserID: userID}
		m.OpponentStats[userID] = stat
	}
	stat.HandsPlayed++
}

// RecordOpponentHandResult 手牌结束时更新对手的净盈亏与胜出计数（2026-08-20 §B2）。
// userID 不存在时建行（HandsPlayed 由 IncrementHandsPlayed 维护,此处不累加）。
func (m *Memory) RecordOpponentHandResult(userID string, seat, netDelta int, won bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.OpponentStats[userID]
	if !ok {
		stat = &OpponentStat{UserID: userID, Seat: seat}
		m.OpponentStats[userID] = stat
	}
	stat.Seat = seat
	stat.NetChips += netDelta
	if won {
		stat.HandsWon++
	}
}

// OpponentFoldRate 返回指定对手的弃牌率（0.0-1.0）。无记录返回 0.5（中性）。
func (m *Memory) OpponentFoldRate(userID string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.OpponentStats[userID]
	if !ok || stat.HandsPlayed == 0 {
		return 0.5 // 中性默认值
	}
	return stat.FoldRate
}

// OpponentStatSnapshot 返回指定对手的统计快照。
func (m *Memory) OpponentStatSnapshot(userID string) *OpponentStat {
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.OpponentStats[userID]
	if !ok {
		return nil
	}
	out := *stat // 复制
	return &out
}

// AllOpponentStats 返回所有对手的统计快照（用于 Profile 迭代）。
func (m *Memory) AllOpponentStats() map[string]*OpponentStat {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*OpponentStat, len(m.OpponentStats))
	for k, v := range m.OpponentStats {
		copy := *v
		out[k] = &copy
	}
	return out
}

// TruncateRecentHands 只保留最近 n 手牌(§3.4 压缩梯度 Tier60=3 / Tier100,400=1)。
// n ≤ 0 时清空。线程安全。
func (m *Memory) TruncateRecentHands(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if len(m.RecentHands) > n {
		m.RecentHands = m.RecentHands[len(m.RecentHands)-n:]
	}
}

// PruneOpponentStats 只保留「交手最多」的 maxN 个对手统计(§3.4 Tier80:
// OpponentStats 收敛到同桌主要对手 —— 德扑 Memory 不感知座位表,以
// HandsPlayed 降序近似)。maxN ≤ 0 时 no-op。线程安全。
func (m *Memory) PruneOpponentStats(maxN int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxN <= 0 || len(m.OpponentStats) <= maxN {
		return
	}
	type kv struct {
		id string
		hp int
	}
	pairs := make([]kv, 0, len(m.OpponentStats))
	for id, st := range m.OpponentStats {
		pairs = append(pairs, kv{id, st.HandsPlayed})
	}
	// HandsPlayed 降序(简单选择排序,n ≤ 6)
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].hp > pairs[i].hp {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}
	keep := make(map[string]bool, maxN)
	for i := 0; i < maxN && i < len(pairs); i++ {
		keep[pairs[i].id] = true
	}
	for id := range m.OpponentStats {
		if !keep[id] {
			delete(m.OpponentStats, id)
		}
	}
}

// SetLastDecisionSummary 设置最近决策摘要。
func (m *Memory) SetLastDecisionSummary(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastDecisionSummary = s
}

// GetLastDecisionSummary 返回最近决策摘要。
func (m *Memory) GetLastDecisionSummary() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.LastDecisionSummary
}