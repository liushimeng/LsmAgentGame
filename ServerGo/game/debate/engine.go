// Package debate — DebateEngine 阶段机主循环 + watchdog。
//
// 2026-08-31 §20260831-01 — DebateEngine 首期实现:
//
//   - 每房间一实例,由 DebateManager.StartGame 启动
//   - 主循环:监听 ctx.Done → 阶段超时 → 推进阶段
//   - 阶段推进回调:PhaseOpening / PhaseRebuttal / ... 由 engine_phase.go 实现
//   - watchdog:每阶段 PhaseConfig.PreparationSec 时长,超时自动切下一阶段
//
// 详细设计见 docs/辩论比赛/01-辩论比赛游戏流程设计.md §3。
package debate

import (
	"context"
	"sync"
	"time"
)

// DebateEngine 单房间引擎。
type DebateEngine struct {
	room    *DebateRoom
	manager *DebateManager

	ctx    context.Context
	cancel context.CancelFunc

	// 启动时间(用于 watchdog 初始化)
	startedAt int64

	// 同步
	mu sync.Mutex

	// Bot 启动状态:每个 (teamID, seat) 一个 chan,供 engine_phase 唤醒对应 Bot。
	botEvents    map[string]chan struct{} // key = "team:seat"
	botEventsMu  sync.RWMutex

	// 裁判启动状态
	judgeEvents    map[int]chan struct{}
	judgeEventsMu  sync.RWMutex
}

// NewDebateEngine 构造引擎(由 DebateManager.StartGame 调用)。
func NewDebateEngine(room *DebateRoom, mgr *DebateManager) *DebateEngine {
	return &DebateEngine{
		room:        room,
		manager:     mgr,
		startedAt:   WallNow(),
		botEvents:   make(map[string]chan struct{}),
		judgeEvents: make(map[int]chan struct{}),
	}
}

// Room 返回关联的房间(供阶段实现访问)。
func (e *DebateEngine) Room() *DebateRoom { return e.room }

// Manager 返回关联的 manager(供阶段实现访问)。
func (e *DebateEngine) Manager() *DebateManager { return e.manager }

// BotEventChan 返回指定 (teamID, seat) Bot 的事件通道(用于唤醒该 Bot)。
//
// 当前实现:每阶段进入时,engine_phase 会触发对应 Bot 的通道,
// Bot driver 监听通道并发起 LLM 调用。
func (e *DebateEngine) BotEventChan(teamID, seat int) chan struct{} {
	key := SeatKey(teamID, seat)
	e.botEventsMu.Lock()
	defer e.botEventsMu.Unlock()
	if ch, ok := e.botEvents[key]; ok {
		return ch
	}
	ch := make(chan struct{}, 4) // buffered 防阻塞
	e.botEvents[key] = ch
	return ch
}

// JudgeEventChan 返回指定裁判的事件通道。
func (e *DebateEngine) JudgeEventChan(idx int) chan struct{} {
	e.judgeEventsMu.Lock()
	defer e.judgeEventsMu.Unlock()
	if ch, ok := e.judgeEvents[idx]; ok {
		return ch
	}
	ch := make(chan struct{}, 4)
	e.judgeEvents[idx] = ch
	return ch
}

// TriggerBot 唤醒指定 Bot(driver 监听后发起 LLM 调用)。
func (e *DebateEngine) TriggerBot(teamID, seat int) {
	ch := e.BotEventChan(teamID, seat)
	select {
	case ch <- struct{}{}:
	default:
	}
}

// TriggerJudge 唤醒指定裁判。
func (e *DebateEngine) TriggerJudge(idx int) {
	ch := e.JudgeEventChan(idx)
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Stop 关闭引擎(由 DebateManager.Remove / StopGame 调用)。
func (e *DebateEngine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// Run 主循环(watchdog + 阶段推进)。
//
// 简化版实现:按 PhaseConfig 中各阶段时长顺序推进,不做 watchdog 实时性优化
// (每阶段 SetPhase 时已记录 deadline,后续可优化为 ticker-based)。
//
// 本 Run 启动后:
//   1. 进入 PhasePreparation 倒计时
//   2. 倒计时结束 → 进入 PhaseOpeningArgument
//   3. 通知一辩发言(TriggerBot)
//   4. 等待一辩发言完成(speech 通知)
//   5. 进入下一位发言者 / 下一阶段
//
// 完整推进逻辑详见 engine_phase.go 的各阶段实现。
func (e *DebateEngine) Run() {
	defer func() {
		// 引擎退出时清理
		e.Stop()
	}()

	// 阶段推进循环
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}

		phase := e.room.Phase()
		e.handlePhase(phase)

		// 阶段超时后由 handlePhase 内部推进;若已 GameOver,退出
		if e.room.IsGameOver() {
			return
		}
	}
}

// handlePhase 当前阶段实现入口(由 engine_phase.go 中的具体方法分发)。
//
// Run() → handlePhase(phase) → 进入对应阶段逻辑 → 完成 → 推进下一阶段。
func (e *DebateEngine) handlePhase(phase Phase) {
	// 简化的占位逻辑:
	//   - PhaseFilling:等待房主点击开始(由 StartGame 触发)
	//   - PhasePreparation:30s 准备
	//   - 各发言阶段:由 Agent Bot 完成发言 → 引擎收到 speech → 推进
	//   - PhaseGameOver:退出

	switch phase {
	case PhaseFilling:
		// 等待 StartGame(由 manager 调用)
		return
	case PhasePreparation:
		e.runPreparationPhase()
	case PhaseOpeningArgument:
		e.runOpeningArgumentPhase()
	case PhaseRebuttal:
		e.runRebuttalPhase()
	case PhaseCrossExamination:
		e.runCrossExamPhase()
	case PhaseCrossExamSummary:
		e.runCrossExamSummaryPhase()
	case PhaseFreeDebate:
		e.runFreeDebatePhase()
	case PhaseClosingArgument:
		e.runClosingArgumentPhase()
	case PhaseJudging:
		e.runJudgingPhase()
	case PhaseResult:
		e.runResultPhase()
	case PhaseGameOver:
		// 退出
		return
	default:
		// 未知阶段:兜底跳过到下一阶段
		time.Sleep(time.Second)
	}
}

// runPreparationPhase 赛前准备阶段(本版本简化:固定时间后推进)。
func (e *DebateEngine) runPreparationPhase() {
	deadline := e.room.PhaseDeadline()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(time.Second):
			if WallNow() >= deadline {
				e.advanceTo(PhaseOpeningArgument)
				return
			}
		}
	}
}

// advanceTo 推进到指定阶段(写房间状态 + 触发对应阶段逻辑)。
func (e *DebateEngine) advanceTo(next Phase) {
	e.room.SetPhase(next)
	// 广播 phase 变化(由 ws 层完成)
	if e.manager.onPhaseChange != nil {
		go e.manager.onPhaseChange(e.room.RoomID, next)
	}
}