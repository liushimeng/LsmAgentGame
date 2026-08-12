// Package werewolf — room_cooling.go: 狼人杀一局结束后的「冷却期」机制。
// 详见 docs/狼人杀冷却期设计.md(待写) + CLAUDE.md §129。
//
// 设计要点:
//   - 所有 *Locked 函数假定 caller 已持 r.mu(见 §92a sync.Mutex 不可重入)。
//   - 一局结束 (Status="over" + Phase=PhaseGameOver) 后, 不立刻走
//     onGameOver / forceCloseRoomLocked / tryEnterRestartVoteFromGameOverLocked,
//     而是先进入冷却期, 让人类玩家与观察者有足够时间复盘。
//   - cooling watchdog 每 coolingTickInterval 秒探测一次人类存在:
//     - 有人类在线 → 持续延长冷却窗口(清零 coolingEmptySince)
//     - 无人类 → 记录 coolingEmptySince(首次) 或检查是否已超 CoolingSec
//   - 冷却期结束 → 走原逻辑(tryEnterRestartVoteFromGameOverLocked 或
//     forceCloseRoomLocked)。
//   - coolingSec=0 时禁用冷却期, 走立刻关门的原行为。
package werewolf

import (
	"context"
	"time"

	"LsmWebGame/agent/wwplayer"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// coolingTickInterval 是 cooling watchdog 轮询人类存在的间隔。
// 30s — 与 phaseWatchdogTickInterval(5s) 相比更宽松, 冷却期是分钟级,
// 不需要秒级精度; 30s 足以在 30 分钟窗口内及时响应人类离开。
const coolingTickInterval = 30 * time.Second

// ─────────────────── Phase Watchdog 入口修改 ───────────────────

// tryEnterCoolingFromGameOverLocked 在 phaseWatchdogTick 检测到 Status="over" +
// Phase=PhaseGameOver 时调用, 判定是否进入冷却期。
//
// 进入条件(全部满足):
//   - cfgWerewolfCoolingSec() > 0
//   - 房间未在冷却期(!r.coolingDone && r.coolingCancel == nil)
//   - 房间未进入 PhaseRestartVote(那是冷却期结束后的下一阶段)
//
// 进入后:
//   - 记录 coolingSince = now
//   - 立即探测一次人类存在, 设置 coolingEmptySince
//   - 启动 cooling watchdog goroutine
//
// caller 必须持 r.mu。
func (m *WerewolfManager) tryEnterCoolingFromGameOverLocked(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	coolingSec := cfgWerewolfCoolingSec()
	if coolingSec <= 0 {
		return false
	}
	if r.State.Phase != PhaseGameOver {
		return false
	}
	if r.coolingDone {
		return false
	}
	if r.coolingCancel != nil {
		// 已经在冷却期中(不应发生, 防御性)
		return true
	}
	now := time.Now()
	r.coolingSince = now
	// 立即探测一次人类存在, 决定 coolingEmptySince 起点。
	if m.coolingHumanPresence != nil && !m.coolingHumanPresence(r.RoomID) {
		r.coolingEmptySince = now
	} else {
		r.coolingEmptySince = time.Time{} // 有人类: 清零
	}
	// 2026-07-30 解决和设计方案-20260730-03 Fix-A1: 终局清场 + 终局广播。
	// 冷却期会把 EmitGameOver/gameOverNotified 推迟到冷却结束(人类在线时
	// 可能永远不结束),若不在这里收编,在房玩家/观众只能看到「进行中的
	// 尸体字段」: bot_contexts 的 LLMCallPhase=calling/streaming 残留、
	// 过期 phase_deadline、旧法官语 —— 渲染出「⌛ 等待阶段推进… /
	// 🧠 N 名 Agent 响应中」与 header「对局结束」并存的矛盾 UI。
	// (a) 所有 bot 的 LLM 相位清场为 idle(quarantined 保留,它本身就是终态);
	// (b) 经 onGameOverBroadcast 回调广播一次权威终局帧(phase=over,
	//     status=over, bot_contexts 已清场),复用既有 game.state 路径。
	for _, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		if ag.LLMCallPhase() != wwplayer.PhaseQuarantined {
			ag.ResetLLMCallPhase(wwplayer.PhaseIdle)
		}
	}
	if m.onGameOverBroadcast != nil {
		m.onGameOverBroadcast(r.RoomID, r.State.Winner)
	}
	// 启动 cooling watchdog goroutine。
	ctx, cancel := context.WithCancel(context.Background())
	r.coolingCancel = cancel
	go m.coolingWatchdog(ctx, r)
	logger.L().Info("werewolf: room entered cooling period",
		zap.String("room_id", r.RoomID),
		zap.Int("cooling_sec", coolingSec),
		zap.Time("since", now))
	return true
}

// ─────────────────── Cooling Watchdog Goroutine ───────────────────

// coolingWatchdog 是冷却期的后台 goroutine。每 coolingTickInterval 秒
// 探测一次人类存在, 决定是继续冷却还是结束冷却期并走关门流程。
//
// 退出条件:
//   - ctx 被 cancel(stopAgentsLocked 调 r.coolingCancel)
//   - 房间已被 RemoveGame(下一轮 tick 会发现 r.mu 抢不到或房间不在 m.rooms)
//   - 冷却期结束(调 finishCoolingLocked)
//
// 注意: 本 goroutine 不持 r.mu, 每次 tick 通过 lockRoomBriefly 抢锁;
// 抢不到就跳过这一 tick(与 phaseWatchdogTick 同模式)。
func (m *WerewolfManager) coolingWatchdog(ctx context.Context, r *WerewolfRoom) {
	ticker := time.NewTicker(coolingTickInterval)
	defer ticker.Stop()
	logger.L().Info("werewolf: cooling watchdog started",
		zap.String("room_id", r.RoomID))
	for {
		select {
		case <-ctx.Done():
			logger.L().Info("werewolf: cooling watchdog cancelled",
				zap.String("room_id", r.RoomID))
			return
		case <-ticker.C:
			if m.coolingTick(r) {
				// 冷却期结束, 退出 goroutine。
				return
			}
		}
	}
}

// coolingTick 执行一次冷却期探测。返回 true 表示冷却期已结束, 调用方
// 应退出 cooling watchdog goroutine。
//
// 内部逻辑:
//   - 抢 r.mu(500ms 兜底)
//   - 检查房间是否仍在 PhaseGameOver + Status="over"(否则冷却期已被外部取消)
//   - 探测人类存在:
//     - 有人类 → 清零 coolingEmptySince, 继续冷却
//     - 无人类 → 记录 coolingEmptySince(首次) 或检查是否已超 CoolingSec
//   - 超时 → finishCoolingLocked → 返回 true
func (m *WerewolfManager) coolingTick(r *WerewolfRoom) bool {
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return false // 抢锁失败, 下一 tick 再试
	}
	defer r.mu.Unlock()

	// 防御: 房间状态已不是 game_over, 冷却期已被外部取消。
	if r.State == nil || r.State.Status != "over" || r.State.Phase != PhaseGameOver {
		return true
	}
	// 防御: coolingCancel 已被外部清空(不应发生, 但避免 goroutine 泄漏)。
	if r.coolingCancel == nil {
		return true
	}

	now := time.Now()
	humanPresent := m.coolingHumanPresence != nil && m.coolingHumanPresence(r.RoomID)

	if humanPresent {
		// 有人类在线: 清零 coolingEmptySince, 持续延长冷却窗口。
		if !r.coolingEmptySince.IsZero() {
			r.coolingEmptySince = time.Time{}
			logger.L().Info("werewolf: cooling period extended — human present",
				zap.String("room_id", r.RoomID))
		}
		return false
	}

	// 无人类在线。
	if r.coolingEmptySince.IsZero() {
		// 首次探测到无人类, 记录时间。
		r.coolingEmptySince = now
		logger.L().Info("werewolf: cooling period — no human detected, starting empty timer",
			zap.String("room_id", r.RoomID),
			zap.Int("cooling_sec", cfgWerewolfCoolingSec()))
		return false
	}

	// 已无人类一段时间, 检查是否超时。
	elapsed := now.Sub(r.coolingEmptySince)
	if elapsed < time.Duration(cfgWerewolfCoolingSec())*time.Second {
		return false // 冷却期未结束
	}

	// 冷却期结束, 走关门流程。
	logger.L().Info("werewolf: cooling period expired — closing room",
		zap.String("room_id", r.RoomID),
		zap.Duration("elapsed", elapsed),
		zap.Duration("since", now.Sub(r.coolingSince)))
	m.finishCoolingLocked(r)
	return true
}

// finishCoolingLocked 结束冷却期, 走原有关门流程:
//   - 标记 coolingDone = true
//   - 调 tryEnterRestartVoteFromGameOverLocked(若 config 开 + 人数够 → PhaseRestartVote)
//   - 否则 forceCloseRoomLocked
//
// caller 必须持 r.mu。
func (m *WerewolfManager) finishCoolingLocked(r *WerewolfRoom) {
	if r.coolingDone {
		return
	}
	r.coolingDone = true
	// coolingCancel 保留, 让 cooling watchdog 下一轮 tick 发现 coolingDone=true
	// 后退出; 这里不 cancel, 避免与 cooling watchdog 竞争。

	// 走原有关门流程:
	//   - tryEnterRestartVoteFromGameOverLocked 内部会先判断
	//     shouldEnterRestartVoteLocked (config 开 + 人数够) → PhaseRestartVote;
	//     否则 forceCloseRoomLocked。
	//   - 无论走哪条路, 本函数都不再推进投票阶段(让 phaseWatchdogTick
	//     在下一轮 5s tick 继续推进)。
	m.tryEnterRestartVoteFromGameOverLocked(r)
}

// ─────────────────── 冷却期状态清理 ───────────────────

// resetCoolingStateLocked 在房间重开新一局时调用, 重置冷却期状态。
// caller 必须持 r.mu。
func resetCoolingStateLocked(r *WerewolfRoom) {
	if r == nil {
		return
	}
	if r.coolingCancel != nil {
		r.coolingCancel()
		r.coolingCancel = nil
	}
	r.coolingSince = time.Time{}
	r.coolingEmptySince = time.Time{}
	r.coolingDone = false
}
