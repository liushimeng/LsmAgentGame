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

	// §20260831-02 — turn-done 同步:engine 触发 Bot 后必须等该 Bot 本轮
	// 结束(发言已提交 / fallback / 放弃)才能推进阶段;否则引擎瞬间跑完
	// 所有阶段,Bot 的 LLM 调用回来时 SubmitSpeech 全部被阶段校验拒绝。
	// key = "team:seat",每轮 beginTurn 换新 chan(吸收上一轮残留信号)。
	turnDoneMu sync.Mutex
	turnDone   map[string]chan struct{}
}

// NewDebateEngine 构造引擎(由 DebateManager.StartGame 调用)。
func NewDebateEngine(room *DebateRoom, mgr *DebateManager) *DebateEngine {
	return &DebateEngine{
		room:        room,
		manager:     mgr,
		startedAt:   WallNow(),
		botEvents:   make(map[string]chan struct{}),
		judgeEvents: make(map[int]chan struct{}),
		turnDone:    make(map[string]chan struct{}),
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

// ============================================================================
// §20260831-02 turn-done 同步(engine ↔ Bot driver)
// ============================================================================

// beginTurn 为指定 Bot 开启新一轮等待,返回新鲜的 done chan。
//
// 每次 driveSpeaker 触发 Bot 前调用;旧 chan 直接丢弃(即使其上还有残留
// 信号也不会误唤醒新的一轮)。
func (e *DebateEngine) beginTurn(key string) chan struct{} {
	ch := make(chan struct{}, 1)
	e.turnDoneMu.Lock()
	e.turnDone[key] = ch
	e.turnDoneMu.Unlock()
	return ch
}

// NotifyTurnDone Bot driver 通知引擎「本轮已完成」。
//
// 由 debateplayer.Agent 在 runTurn 结束(defer)时调用 —— 无论成功发言、
// fallback、放弃还是失败都必须调,否则引擎只能等阶段超时。
func (e *DebateEngine) NotifyTurnDone(teamID, seat int) {
	key := SeatKey(teamID, seat)
	e.turnDoneMu.Lock()
	ch := e.turnDone[key]
	e.turnDoneMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// perSpeakerWaitBudget 单个 Bot 一轮的等待预算。
//
// §20260831-02 实测教训:真 LLM 单次调用 20~90s( DouBao 曾命中 90s 上限),
// 一个 2 人发言阶段实际需要 40~180s。若用「阶段 deadline」做等待上限,
// 后发言的 Bot 完成时阶段已被引擎推进,SubmitSpeech 全部被阶段校验拒绝
// (首期 e2e:rebuttal 0 发言 / closing 1/2 发言)。
// 因此发言阶段的等待预算固定为 110s(LLM 超时 90s + 派发余量),
// 与阶段 deadline 解耦 —— 阶段时长是名义节奏,「发言必须完成」优先。
// deadline 仍约束:准备期倒计时 / 自由辩循环 / 评审收集。
const perSpeakerWaitBudget = 110 * time.Second

// waitTurnDone 阻塞等待 Bot 本轮完成。
//
// 返回是否正常等到(done);false = 超时或引擎 ctx 取消。
func (e *DebateEngine) waitTurnDone(ch chan struct{}) bool {
	t := time.NewTimer(perSpeakerWaitBudget)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-e.ctx.Done():
		return false
	case <-t.C:
		return false
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