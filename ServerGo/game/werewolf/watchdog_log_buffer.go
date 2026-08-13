// Package werewolf — watchdog_log_buffer.go: 内存 ring buffer 记录最近
// 100 条 watchdog tick / skip / wake / quarantine 事件,供前端调试面板拉取。
//
// 2026-08-13 §20260813-01 优化: 借鉴 agent-studio `TriggerExecutionLogDB`
// (docs/其他Agent代码分析/agent-studio_意图识别与任务分解分析.md §3),
// 替代当前仅依赖 logger 文件的"看不见历史"模式,提供 per-room 内存级
// 时间线(进程重启即丢失,符合"内存态"定位)。
//
// 设计要点:
//   - 容量固定 100,覆盖最近 ~8 分钟(5s tick × 100)的关键事件。
//   - 线程安全(RWMutex),phaseWatchdogTick 内 Append 频率低(≤ 1/s),
//     RWMutex 性能足够。
//   - 零值即可用,无外部依赖。
//   - 与 §130 教训兼容:本结构是纯日志缓冲,不参与"声明了却从不接线"路径
//     的任何决策分支。
package werewolf

import (
	"sort"
	"sync"
	"time"
)

// WatchdogLogEntryKind 事件类型常量(便于前端按 type 渲染/过滤)。
const (
	WatchdogLogKindTick        = "tick"        // 5s tick 触发
	WatchdogLogKindSkip        = "skip"        // 强制 skip(deadlock/stall)
	WatchdogLogKindWake        = "wake"        // 唤醒 bot
	WatchdogLogKindQuarantine  = "quarantine"  // bot 进入 quarantine
	WatchdogLogKindResume      = "resume"      // bot 从 quarantine 恢复
	WatchdogLogKindPhaseChange = "phase_change" // phase 切换
	WatchdogLogKindError       = "error"       // watchdog 自身错误
)

// WatchdogLogEntry 是单条 watchdog 日志。
type WatchdogLogEntry struct {
	At         int64  `json:"at"`         // unix ms
	Kind       string `json:"kind"`       // WatchdogLogKindXxx
	Phase      string `json:"phase"`      // 当时 phase
	ActingSeat int    `json:"acting_seat"` // 当前应行动 seat(-1 = N/A)
	Reason     string `json:"reason"`     // ≤ 120 字理由
	BotID      string `json:"bot_id,omitempty"` // 涉及 bot 时填
	Round      int    `json:"round"`      // 当时局/日
	Extra      string `json:"extra,omitempty"`  // 额外上下文(≤ 200 字)
}

// WatchdogLogMaxEntries ring buffer 容量上限(100 条 ≈ 8 分钟 5s tick)。
const WatchdogLogMaxEntries = 100

// WatchdogLogBuffer 是 per-room 内存 ring buffer。
//
// 零值即可用,所有方法线程安全。
type WatchdogLogBuffer struct {
	mu      sync.RWMutex
	entries [WatchdogLogMaxEntries]WatchdogLogEntry
	head    int // 下一个写入位置
	size    int // 当前条数
}

// Append 追加一条日志。容量满则覆盖最旧(简化 LRU)。
func (b *WatchdogLogBuffer) Append(e WatchdogLogEntry) {
	if b == nil {
		return
	}
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.head] = e
	b.head = (b.head + 1) % WatchdogLogMaxEntries
	if b.size < WatchdogLogMaxEntries {
		b.size++
	}
}

// Snapshot 返回按时间升序的所有日志副本(新切片,调用方可自由修改)。
func (b *WatchdogLogBuffer) Snapshot() []WatchdogLogEntry {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]WatchdogLogEntry, b.size)
	// 从最旧一条开始拷贝
	start := 0
	if b.size == WatchdogLogMaxEntries {
		start = b.head
	}
	for i := 0; i < b.size; i++ {
		out[i] = b.entries[(start+i)%WatchdogLogMaxEntries]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Size 返回当前条数。
func (b *WatchdogLogBuffer) Size() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// Clear 清空 buffer(房间重启时调)。
func (b *WatchdogLogBuffer) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.size = 0
	for i := range b.entries {
		b.entries[i] = WatchdogLogEntry{}
	}
}
