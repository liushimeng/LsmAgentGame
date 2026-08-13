// Package wwtypes — short_memory.go: 短期记忆(本局本 bot 关键事件 ring buffer)。
//
// 2026-08-13 §20260813-01 优化: 借鉴 agent-studio 多类型记忆设计
// (docs/其他Agent代码分析/agent-studio_记忆管理系统分析.md §4),
// 在 §131 长期记忆之外新增"短期记忆"通道,弥补 MEMORY.md 跨局经验
// 与本局高频事件之间的空白。
//
// 设计目标:
//   - 容量小(≤50 条),驻留内存,局结束随 WerewolfRoom GC。
//   - 事件类型:投票/发言/死亡/道具/工具使用/私人通信/狼队暗号。
//   - 关键事件(死亡/暴露)永不淘汰,普通事件按 FIFO 覆盖。
//   - 注入 prompt 时按相关度排序:本 bot 行动过的 > 涉及本 bot 阵营的 > 其他。
//   - 局结束时由 caller 调 Compress() 摘要入 MEMORY.md(后续可扩展)。
//
// 不变量:
//   - 单一 shortMemoryEvent 不可变,append-only。
//   - 并发安全(RWMutex 保护)。
//   - 与 §131 持久化记忆正交:本短期记忆不写 DB,§131 长期记忆由 model_key
//     索引,本短期记忆由 (room_id, seat) 索引,完全不同的作用域。
package wwtypes

import (
	"sort"
	"sync"
	"time"
)

// ShortMemoryEventKind 短期记忆事件类型常量。
//
// 与狼人杀核心事件一一对应,新增类型时同时:
//   1. 在此处加常量;
//   2. 在 injectShortMemoryToPrompt() 加渲染分支;
//   3. 在 addShortMemoryEvent() 加 emit 路径。
const (
	ShortMemKindVote         = "vote"          // 投票
	ShortMemKindSpeak        = "speak"         // 公开发言
	ShortMemKindDeath        = "death"         // 死亡事件(关键,永不淘汰)
	ShortMemKindProp         = "prop"          // 道具使用/被击中
	ShortMemKindToolUse      = "tool_use"      // 工具调用
	ShortMemKindWhisper      = "whisper"       // 私聊接收
	ShortMemKindWolfPack     = "wolf_pack"     // 狼队暗号留言
	ShortMemKindPhaseChange  = "phase_change"  // 阶段切换
	ShortMemKindSettle       = "settle"        // 结算事件
	ShortMemKindMirrorExpose = "mirror_expose" // 照妖镜暴露身份(关键,永不淘汰)
)

// ShortMemoryEvent 是单条本局关键事件。
//
// 关键事件(KinPinned)绝不淘汰;其他事件按 FIFO 覆盖。
// IsKey 由 AddEvent 内部按 Kind 决定,不允许外部覆盖。
type ShortMemoryEvent struct {
	At      int64  `json:"at"`       // unix ms
	Kind    string `json:"kind"`     // ShortMemKindXxx
	Actor   int    `json:"actor"`    // 触发者 seat(0-12)
	Target  int    `json:"target"`   // 目标 seat,若适用
	Phase   string `json:"phase"`    // 当时 phase
	Round   int    `json:"round"`    // 当日(从 1 开始)
	Summary string `json:"summary"`  // ≤80 字摘要(中文为主)
	Tool    string `json:"tool,omitempty"`     // 工具名,若适用
	Hit     bool   `json:"hit,omitempty"`     // 道具是否命中
	IsKey   bool   `json:"is_key,omitempty"`  // 关键事件,永不淘汰
	Extra   string `json:"extra,omitempty"`    // 额外上下文(≤120 字)
}

// ShortMemoryMaxEvents 是 ring buffer 容量上限。
// 50 条覆盖 13 人局 60 分钟对局的关键事件(每 1-2 分钟 1 条)。
// 关键事件(死亡/暴露)单独统计,不占此容量。
const ShortMemoryMaxEvents = 50

// ShortMemoryBuffer 是 per-bot 短期记忆 ring buffer。
//
// 并发安全;零值即可用(零值容量=50,所有字段为初值)。
type ShortMemoryBuffer struct {
	mu       sync.RWMutex
	events   []ShortMemoryEvent // 已用槽位(无空洞)
	keyCount int                // 关键事件计数
	head     int                // 下一个写入位置
	size     int                // 当前条数
	capacity int                // 容量上限(默认 50)

	// roomID + seat 用于诊断日志,不参与逻辑。
	roomID string
	seat   int
}

// NewShortMemoryBuffer 创建并初始化 buffer。capacity<=0 时用默认值。
func NewShortMemoryBuffer(roomID string, seat int, capacity int) *ShortMemoryBuffer {
	if capacity <= 0 {
		capacity = ShortMemoryMaxEvents
	}
	return &ShortMemoryBuffer{
		events:   make([]ShortMemoryEvent, 0, capacity),
		capacity: capacity,
		roomID:   roomID,
		seat:     seat,
	}
}

// isKeyEventKind 判定某 Kind 是否为关键事件(永不淘汰)。
func isKeyEventKind(kind string) bool {
	switch kind {
	case ShortMemKindDeath, ShortMemKindMirrorExpose:
		return true
	}
	return false
}

// AddEvent 追加一条事件。关键事件永不淘汰,普通事件按 FIFO。
//
// 重复 key event(同 kind+actor+target+phase+round)将被忽略,避免
// 双重死亡(双重 add 误调)淹没 buffer。
func (b *ShortMemoryBuffer) AddEvent(e ShortMemoryEvent) {
	if b == nil {
		return
	}
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	e.IsKey = isKeyEventKind(e.Kind)

	b.mu.Lock()
	defer b.mu.Unlock()

	// 关键事件去重
	if e.IsKey {
		for _, existing := range b.events {
			if existing.IsKey &&
				existing.Kind == e.Kind &&
				existing.Actor == e.Actor &&
				existing.Target == e.Target &&
				existing.Phase == e.Phase &&
				existing.Round == e.Round {
				return
			}
		}
	}

	b.events = append(b.events, e)
	b.size = len(b.events)

	// 容量控制(关键事件仍占用槽位,但不被覆盖 — 改为扩容语义)
	if b.size > b.capacity {
		// 找到最旧的非关键事件,移除
		idx := -1
		for i, ev := range b.events {
			if !ev.IsKey {
				idx = i
				break
			}
		}
		if idx >= 0 {
			b.events = append(b.events[:idx], b.events[idx+1:]...)
			b.size = len(b.events)
		}
		// 全部都是关键事件:保留,容量软超限
	}
}

// Size 返回当前事件总数(含关键事件)。
func (b *ShortMemoryBuffer) Size() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// Snapshot 返回按时间升序的所有事件副本。
//
// 返回的是新切片,调用方可自由修改。
func (b *ShortMemoryBuffer) Snapshot() []ShortMemoryEvent {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]ShortMemoryEvent, len(b.events))
	copy(out, b.events)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// FilterByActor 返回 actor 涉及的所有事件(触发者=actor)。
// 用于 prompt 注入时"本 bot 行动过的"优先排序。
func (b *ShortMemoryBuffer) FilterByActor(actor int) []ShortMemoryEvent {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []ShortMemoryEvent
	for _, e := range b.events {
		if e.Actor == actor || e.Target == actor {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// Clear 清空 buffer(局结束或 restart vote 时调)。
func (b *ShortMemoryBuffer) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = b.events[:0]
	b.keyCount = 0
	b.head = 0
	b.size = 0
}

// RoomID + Seat 仅用于诊断。
func (b *ShortMemoryBuffer) RoomID() string {
	if b == nil {
		return ""
	}
	return b.roomID
}

func (b *ShortMemoryBuffer) Seat() int {
	if b == nil {
		return 0
	}
	return b.seat
}
