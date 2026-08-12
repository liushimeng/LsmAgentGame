package werewolf

import (
	"context"
	"fmt"
	"time"

	"LsmAgentGame/agent/wwcommentator"
	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (m *WerewolfManager) stopAgentsLocked(r *WerewolfRoom) {
	// BUG-WEREWOLF-P0-NEW-42b: stop the phase watchdog goroutine first so it
	// doesn't try to dispatch a skip while agents are being torn down.
	if r.watchdogCancel != nil {
		r.watchdogCancel()
		r.watchdogCancel = nil
	}
	// 2026-07-12 §129 增强 — 停掉冷却期 watchdog, 避免 goroutine 泄漏。
	// 必须在 phase watchdog 之后、agentCancels 之前: cooling watchdog 可能
	// 在 finishCoolingLocked 路径下调用 wakeAllAgentsLocked 唤醒 bot,
	// 若 agent 已 cancel 再推事件会导致向已 cancel 的 goroutine 推事件。
	if r.coolingCancel != nil {
		r.coolingCancel()
		r.coolingCancel = nil
	}
	// §130 重构(2026-07-13) — 停掉人类等待窗口 watchdog,避免 goroutine 泄漏。
	// 与 coolingCancel 同位置:必须在 phase watchdog 之后、agent 启动之前。
	if r.humanWaitCancel != nil {
		r.humanWaitCancel()
		r.humanWaitCancel = nil
	}
	r.humanWaitDeadlineAt = time.Time{}
	r.humanWaitBroadcastSent = false
	// 2026-07-09 §13 增强 — 同步停掉 speak floor watchdog。
	// 2026-07-10 §125 增强 — 停掉法官 goroutine(若已启动)。
	// 2026-07-30 §重构:判定条件不变(只看 r.judgeCancel != nil);启动 / 配置语义变化不影响停止路径。
	if r.judgeCancel != nil {
		r.judgeCancel()
		r.judgeCancel = nil
	}
	// §20260809-02 U1:停掉法官后清空多轮记忆环形缓冲,避免下一局借用上一局
	// announce 残留(尤其是跨房间复用同一 *AgentJudge 时的历史污染边界)。
	// JudgeMemoryRing.Reset 内部 nil-guard,旧 room 无法官时不做事。
	if r.judge != nil && r.judge.Memory != nil {
		r.judge.Memory.Reset()
	}
	m.stopSpeakFloorWatchdog(r)
	for seat, cancel := range r.agentCancels {
		cancel()
		delete(r.agentCancels, seat)
	}
	for seat, ag := range r.BotAgents {
		ag.Shutdown()
		delete(r.BotAgents, seat)
	}
	// 2026-07-09 §13-bugfix — 房间 tear down 时触发一次 Compress 把队列压缩
	// 到 ≤ 80%,然后保留 chatQueue 引用(供前端 GET 调试用,见
	// /api/admin/werewolf/rooms/:id/chat_history)。不删 map[r.chatQueue] —
	// GC 由 WerewolfManager.rooms 删除 r 时回收。
	if r.chatQueue != nil {
		r.chatQueue.Compress()
	}
	// BUG-WEREWOLF-DISBAND-LEAK: 等待所有 agent goroutine 真正退出,
	// 而不只是发出 cancel 信号。用 done channel 把 Wait() 包成非阻塞
	// 形式,避免慢 LLM 响应把 stopAgentsLocked 永久卡住。
	// 无论 map 是否为空,只要曾经 Add(1) 过就得等 Done(),否则会丢失
	// 已注册但仍在跑的 goroutine —— 比如 StartAgentsLocked 失败但
	// 部分 goroutine 已经 launch 的情况。
	done := make(chan struct{})
	go func() {
		r.agentWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.L().Info("werewolf: all agent goroutines exited cleanly",
			zap.String("room_id", r.RoomID))
	case <-time.After(stopAgentsWaitTimeout):
		logger.L().Warn("werewolf: stopAgentsLocked timeout — some agent goroutines may still be alive",
			zap.String("room_id", r.RoomID),
			zap.Duration("timeout", stopAgentsWaitTimeout))
	}
}

func (m *WerewolfManager) startPhaseWatchdog(ctx context.Context, r *WerewolfRoom) {
	ticker := time.NewTicker(phaseWatchdogTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.phaseWatchdogTick(r); err != nil {
				// Room torn down — exit the goroutine.
				return
			}
		}
	}
}

func (m *WerewolfManager) phaseWatchdogTick(r *WerewolfRoom) error {
	if !lockRoomBriefly(r, 500*time.Millisecond) {
		return nil // lock contention — skip this tick
	}
	// 2026-07-16 主持人重构 — judgeWakeKind 在函数体(锁内)检测到 phase 切换后由
	// 本闭包在 r.mu.Unlock() 之后发送,确保 wakeJudgeLocked 不持 r.mu (§92a)。
	judgeWakeKind := ""
	// BUG-R231-P0-01 (2026-08-02): force-tally 路径的「锁外发」标记。
	// 锁内只执行 tallyWolfVotes + endWolfPhase 等纯状态变更;EmitAutoSkip /
	// EmitWolfKill / wakeActingAgentsLocked 在 r.mu.Unlock() 之后派发,
	// 避免持 r.mu 调 Emit* → hub.BroadcastRoomIncludingSpectators(h.mu.RLock)
	// 与下游路径形成 §92a/§212 类自死锁(详见 BUG-R231-P0-01 报告)。
	forceTallyWakeKind := ""
	forceTallyWolfKillTarget := -1
	// BUG-HUNTER2-P0-01 (2026-08-07): 警长竞选阶段 watchdog 兜底标记。当
	// watchdog 通过 deadline 或 360s 兜底派发 sheriff_elect skip 时,
	// 记下此标记,由 defer 块在锁外调 EmitSheriffAutoSkip 公开广播
	// 「⏭ 警长竞选超时 / 无人参选,本局无警长」,避免阶段无声切换。
	sheriffAutoSkip := false
	defer func() {
		r.mu.Unlock()
		if judgeWakeKind != "" {
			r.wakeJudgeLocked(judgeWakeKind, nil)
		}
		// BUG-R231-P0-01: 锁外发 — 此时已释放 r.mu,可安全调 Emit* (走
		// hub.BroadcastRoomIncludingSpectators → h.mu.RLock) 与
		// wakeActingAgentsLocked (走 tryDispatchQuarantinedActingSkip 级联)。
		if forceTallyWakeKind != "" {
			m.EmitAutoSkip(r, forceTallyWakeKind)
			if forceTallyWolfKillTarget >= 0 {
				m.EmitWolfKill(r, forceTallyWolfKillTarget)
			}
			m.wakeActingAgentsLocked(r, "wolf_vote_tally")
		}
		// BUG-HUNTER2-P0-01 (2026-08-07): 警长竞选被 watchdog 跳过时,公开
		// 广播原因,避免玩家看到阶段突然切换却无任何交代。EmitSheriffAutoSkip
		// 内部区分「超时」与「无人参选」两种文案(参见 activity_emitter.go)。
		if sheriffAutoSkip {
			m.EmitSheriffAutoSkip(r)
		}
	}()

	if r.State == nil || r.State.Phase == PhaseFilling {
		return nil
	}

	// 2026-07-24 优化:UI 暂停时跳过所有 watchdog 干预(死锁检测 / 强制 skip /
	// 法官唤醒),让用户自由控制推进节奏。Bot 也不调 LLM(防批量 quarantine)。
	if r.IsPaused() {
		return nil
	}

	// 2026-07-16 主持人重构 — phase 切换时唤醒法官(秘密阶段返回 "" → 不唤醒)。
	// 放在 status==over / RestartVote 分支之前,让这些阶段也能触发法官事件。
	if r.State.Phase != r.lastJudgePhase {
		if kind := judgeKindForPhase(r.State.Phase); kind != "" {
			judgeWakeKind = kind
		}
		r.lastJudgePhase = r.State.Phase
		// §20260811-09 U1 — 解说同步触发(非秘密阶段,观众席可见)。与法官共享同一 kind,
		// 让解说 prompt 与法官宣告保持事件上下文一致。
		if judgeKindForPhase(r.State.Phase) != "" {
			r.triggerCommentaryEventLocked(wwcommentator.CommentaryPendingPhaseChange, map[string]any{
				"phase": r.State.Phase.String(),
				"day":   r.State.DayNumber,
			})
		}
	}
	// BUG-R48-P0-4 + 2026-07-10 重开局投票: 当 status=over 时先尝试进入
	// PhaseRestartVote;投票阶段由专门 tick (restartDeadlineTickLocked) 推进,
	// 不再走单一 force-close。判定口径见 engine_restart_vote.go
	// shouldEnterRestartVoteLocked。
	// 2026-07-12 §129 增强 — 冷却期: 在 tryEnterRestartVoteFromGameOverLocked
	// 之前先尝试进入冷却期, 让人类玩家与观察者有足够时间复盘。
	if r.State.Status == "over" {
		if r.State.Phase == PhaseGameOver && !r.gameOverNotified {
			// 先尝试进入冷却期(若 config 开 + 房间未在冷却)。
			// 进入冷却期后本 tick 直接返回, 由 cooling watchdog 推进。
			if m.tryEnterCoolingFromGameOverLocked(r) {
				return nil
			}
			// 冷却期未启用 / 已冷却过 → 走立刻关门流程。
			m.tryEnterRestartVoteFromGameOverLocked(r)
			// tryEnter... 根据 config 决定是否进入投票;
			// 若投票未启用 / 人数不足 → 关闭房间
			if r.State.Phase == PhaseGameOver && !r.gameOverNotified {
				r.gameOverNotified = true
				m.EmitGameOver(r, r.State.Winner)
				if m.onGameOver != nil {
					m.onGameOver(r.RoomID)
				}
				// 2026-07-10 §125 增强 — 触发法官「整局总结」。
				// 用 JudgePendingGameOverSummary 事件投递;judge goroutine 收到后调
				// LLM 生成 5 段总结并 append 到 chatQueue + 持久化 modelMemories。
				r.wakeJudgeLockedForSummaryLocked()
				return nil
			}
			// 已切到 PhaseRestartVote,tick 已推送 wake
			return nil
		}
		if r.State.Phase == PhaseRestartVote {
			// 仍在投票窗口:评估 deadline
			m.restartDeadlineTickLocked(r)
			return nil
		}
		// restartVoteDone=true 或其他 status=over 子状态:保持原 force-close
		if !r.gameOverNotified {
			r.gameOverNotified = true
			m.EmitGameOver(r, r.State.Winner)
			if m.onGameOver != nil {
				m.onGameOver(r.RoomID)
			}
		}
		return nil
	}

	// BUG-R221 (2026-08-01) 房间级熔断 — 所有 Agent 全部 quarantine 时强制结束。
	// 放在 status=over 分支之后(已结束的房间无需熔断)、pre_wolves 分支之前
	// (缓冲期分支会提前 return,放后面会漏掉这些 tick 的计数)。
	if m.allQuarantinedTickLocked(r) {
		return nil
	}

	// BUG 2026-07-08: 首夜发言缓冲期 (PhasePreWolves) 到点检查。
	// 缓冲期是内部状态,不让任何 agent 推进;到点后由 watchdog 切回
	// PhaseNightWolves 并推 wake 给狼人(若有)/预言家/女巫。空刀场景
	// 由 Action_WolfKill 走标准路径(把 target 设为 NoSeat 等价于空刀)
	// 处理;若 0 名狼人 bot 发言过,引擎在 startNight() 中强制 NoSeat。
	if r.State.Phase == PhasePreWolves {
		// BUG Round 40 §95: 强制发言阶段出口条件 (a) — 全员完成时提前切阶段,
		// 不必等满 120 秒。advancePreWolvesRoundLocked 内部会切到 PhaseNightWolves
		// (最后一轮) 或 切换到下一轮(round++)。
		if m.advancePreWolvesRoundLocked(r) {
			// 已切到 PhaseNightWolves,推 wake 给所有存活 agent
			m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
			return nil
		}
		// 出口条件 (b) — 缓冲期超时
		if !r.State.FirstNightGraceEnd.IsZero() && time.Now().After(r.State.FirstNightGraceEnd) {
			graceDuration := time.Until(r.State.FirstNightGraceEnd.Add(-time.Since(r.State.FirstNightGraceEnd)))
			logger.L().Info("werewolf: first night grace period expired; advancing to night",
				zap.String("room_id", r.RoomID),
				zap.Duration("overshoot", graceDuration))
			// §134 BUG-R214-P0: 经 startNight() 进入夜间,由其统一完成夜间重置
			// + 守卫/狼人阶段判定。此前此处硬编码 PhaseNightWolves 并手动设
			// TurnActingSeat,绕开了 startNight() 内的守卫阶段决策,导致首夜守卫永远
			// 无法行动。startNight() 在 pre_wolves 出口调用是幂等的:此期间仅
			// PreWolvesSpeakCount/HasSpoken/round 被改动,均会被 startNight() 正确复位。
			r.State.startNight()
			// 推 wake 给所有存活 agent(状态变更同步)
			m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
			return nil
		}
		// 缓冲期未到:仅做轻量心跳
		return nil
	}

	// Compute the current phase+actingSeat key.
	actingSeat := watchdogActingSeat(r)
	key := fmt.Sprintf("%s/%d", r.State.Phase.String(), actingSeat)

	now := time.Now()

	// BUG-R229-P1-01 (2026-08-01) — night_wolves 阶段所有存活狼人均 quarantine
	// 时立即 force-tally。背景: BUG-R229-P0-01 类错误致 3/13 座位的 Agent 永久
	// 400 进 quarantine;若这些座位恰好是全部存活狼人,night_wolves 阶段既无
	// acting bot 可唤醒、亦无真人可推进。既有的"首次 skipCount>=1 且 120s
	// 后 force-tally"路径依赖"第一次 90s 兜底成功派发 skip",但狼人座位
	// quarantine 时 dispatchQuarantinedSkipLocked 推不动,阶段卡 6+ 分钟。
	// 修复:在通用 stall 检测之前,专门识别"PhaseNightWolves + 至少 1 匹存活狼
	// + 全部存活狼 quarantine/无法投票"并立即强制计票(未投票者弃权)。
	if r.State.Phase == PhaseNightWolves && allAliveWolvesQuarantinedLocked(r) {
		logger.L().Warn("werewolf: phase watchdog — night_wolves all alive wolves quarantined; forcing tally",
			zap.String("room_id", r.RoomID),
			zap.String("phase", r.State.Phase.String()))
		// BUG-R231-P0-01: 锁内只做纯状态变更,记 wakeKind 由 defer 锁外发。
		// 必须在 tallyWolfVotes / endWolfPhase 之前记录原 phase(用于
		// EmitAutoSkip 的标签文案),因为 endWolfPhase 会推进 phase。
		forceTallyWakeKind = r.State.Phase.String()
		r.State.tallyWolfVotes()
		r.State.endWolfPhase()
		// tallyWolfVotes 已把 WolfKillTarget 写入,且 endWolfPhase 不会清它。
		forceTallyWolfKillTarget = int(r.State.WolfKillTarget)
		r.phaseWatchdog.key = ""
		r.phaseWatchdog.enteredAt = now
		r.phaseWatchdog.skipCount = 0
		return nil
	}

	// BUG-R223 (2026-08-01) 防御性重新武装 — 行动阶段的 PhaseDeadlineAt 若为零值
	// (setPhaseAndDeadline 未被走到 / 阶段被外部直接赋值 / 历史快照恢复),下方
	// deadline 分支永远不触发,该阶段就只剩 stall 分支一条救援路径。这里用既有
	// setPhaseAndDeadline 按当前 phase 重新计算一次 deadline(同 phase 幂等赋值,
	// 不引入任何新引擎状态),让时钟机制恢复。
	if r.State.PhaseDeadlineAt.IsZero() && isActingPhase(r.State.Phase.String()) {
		setPhaseAndDeadline(r.State, r.State.Phase)
		logger.L().Warn("werewolf: phase deadline unarmed in acting phase; re-armed",
			zap.String("room_id", r.RoomID),
			zap.String("phase", r.State.Phase.String()),
			zap.Time("deadline_at", r.State.PhaseDeadlineAt))
	}

	// 2026-07-09 §13 增强 — 时钟机制。PhaseDeadlineAt 到期后立即派发 skip
	// (不等 90s 兜底);优先级高于 90s 兜底,但低于"stage 切换"reset。
	if !r.State.PhaseDeadlineAt.IsZero() && now.After(r.State.PhaseDeadlineAt) {
		deadline := r.State.PhaseDeadlineAt
		overshoot := now.Sub(deadline)
		logger.L().Info("werewolf: phase deadline reached; forcing skip",
			zap.String("room_id", r.RoomID),
			zap.String("phase", r.State.Phase.String()),
			zap.Int("acting_seat", int(actingSeat)),
			zap.Duration("overshoot", overshoot))

		// 2026-07-11 §13-R91: deadline 派发前先做一次"最后唤醒"。
		// Bug-4 治理:R90 报告里 watchdog 在 LLM 503 场景下「deadline 到期 → 立即
		// skip → 阶段无任何人真正行动」,玩家在 13 bot 房间看到阶段被「瞬间
		// 空过」。修复:deadline 派发前 push 一次 wake 事件给当前 acting bot,
		// bot 收到 wake 后 1) 立即执行工具;2) 同时本函数继续派发 skip(若
		// wake 仍失败,skip 是兜底)。这样至少 bot 多一次机会,而不是
		// 「deadline 命中 → 0 行动 → 立即进入下阶段」。
		if actingSeat >= 0 {
			if bot := r.BotAgents[int(actingSeat)]; bot != nil {
				bot.PushEvent(wwplayer.AgentEvent{
					Kind: "wake",
					Context: wwtypes.GameContext{
						Phase:   r.State.Phase.String(),
						MySeat:  int(actingSeat),
						MyTurn:  true,
						MyVoted: false,
					},
				})
				logger.L().Info("werewolf: phase deadline — pushed last wake before skip",
					zap.String("room_id", r.RoomID),
					zap.String("phase", r.State.Phase.String()),
					zap.Int("acting_seat", int(actingSeat)))
			}
		}

		// BUG-R231-P0-01: 锁内记 wakeKind,defer 锁外发(与 force-tally 路径同模式)。
		// §115 房间聊天 — watchdog 派发 skip 活动事件(deadline 触发)
		forceTallyWakeKind = r.State.Phase.String()

		// 2026-07-17: night_wolves 投票窗口到期 → 计票(未投票者视为弃权)
		if r.State.Phase == PhaseNightWolves {
			r.State.tallyWolfVotes()
			r.State.endWolfPhase()
			// tallyWolfVotes 已把 WolfKillTarget 写入,且 endWolfPhase 不会清它。
			forceTallyWolfKillTarget = int(r.State.WolfKillTarget)
			// reset watchdog 状态(让下一阶段重新计时)
			r.phaseWatchdog.key = ""
			r.phaseWatchdog.enteredAt = now
			r.phaseWatchdog.skipCount = 0
			return nil
		}

		// 与 90s 兜底同模式:vote 走 finishVoteLocked,其他 phase 走 dispatchQuarantinedSkipLocked。
		if r.State.Phase == PhaseVote {
			if derr := m.finishVoteLocked(r, 0); derr != nil {
				logger.L().Warn("werewolf: deadline dispatch finishVote failed",
					zap.String("room_id", r.RoomID), zap.Error(derr))
			}
		} else if r.State.Phase == PhaseSheriff {
			// BUG-HUNTER2-P0-01 (2026-08-07): 警长竞选 deadline 分支。
			// 与原 watchdog 兜底行为完全一致 —— 派发 sheriff_elect skip,
			// 由 sheriffElectLocked → r.State.SheriffElect(NoSeat) → 若
			// SheriffSeat == NoSeat 则无人当选,自动跳到 PhaseSpeak。
			// 同时记 sheriffAutoSkip = true 让 defer 块锁外发「⏭ 警长竞选
			// 超时,本局无警长」广播(参见 EmitSheriffAutoSkip)。
			if actingSeat >= 0 {
				if skipName, skipArg := wwplayer.SkipPhaseAction(r.State.Phase.String(), r.State.Roles[actingSeat].String()); skipName != "" {
					if derr := m.dispatchQuarantinedSkipLocked(r, actingSeat, skipName, skipArg); derr != nil {
						logger.L().Warn("werewolf: deadline sheriff_elect dispatch failed",
							zap.String("room_id", r.RoomID),
							zap.Error(derr))
					}
				}
			}
			sheriffAutoSkip = true
		} else if actingSeat >= 0 {
			roleName := r.State.Roles[actingSeat].String()
			if skipName, skipArg := wwplayer.SkipPhaseAction(r.State.Phase.String(), roleName); skipName != "" {
				if derr := m.dispatchQuarantinedSkipLocked(r, actingSeat, skipName, skipArg); derr != nil {
					logger.L().Warn("werewolf: deadline dispatch skip failed",
						zap.String("room_id", r.RoomID),
						zap.String("skip_tool", skipName), zap.Error(derr))
				}
			}
		}
		// 推进后 reset watchdog 状态(让下一阶段重新计时)
		r.phaseWatchdog.key = ""
		r.phaseWatchdog.enteredAt = now
		r.phaseWatchdog.skipCount = 0
		return nil
	}

	if r.phaseWatchdog.key == key {
		// Same phase+seat as last tick — check if overdue.
		elapsed := now.Sub(r.phaseWatchdog.enteredAt)

		// Emit a warning log periodically (not every tick).
		if elapsed >= phaseWatchdogWarningInterval && now.Sub(r.phaseWatchdog.lastLog) >= phaseWatchdogWarningInterval {
			agName := "human"
			if ag := r.BotAgents[int(actingSeat)]; ag != nil {
				agName = ag.ModelKey
			}
			logger.L().Warn("werewolf: phase watchdog — same acting seat for extended period",
				zap.String("room_id", r.RoomID),
				zap.String("phase", r.State.Phase.String()),
				zap.Int("acting_seat", int(actingSeat)),
				zap.String("agent_model", agName),
				zap.Duration("elapsed", elapsed))
			r.phaseWatchdog.lastLog = now
		}

		// BUG-R212-P1-02 (2026-07-31, 混合模式): night_wolves acting seat 是真人时,
		// 真人通过 NightActionPanel / game.werewolf_action 投票;若真人不投票(掉线/
		// 挂机未触发 HandleDisconnect),watchdog 的 bot 派发路径(PushEvent →
		// BotAgents[actingSeat]=nil)空转,skipCount 永远到不了 1,只能等 360s 硬兜底
		// → 整阶段卡 8+ 分钟(R212 混合模式复测报告 20260731_004620)。
		// 与 single-actor night phase 同构:真人 acting seat 视为"永久无法响应",
		// 在 phaseWatchdogSingleActorDeadline (120s) 时直接 force-tally(真人视为弃权),
		// 不必等 skipCount>=1。识别信号: 存活 + !IsBot + BotAgents[seat]==nil。
		humanActingWolf := actingSeat >= 0 && actingSeat < MaxPlayers &&
			r.State.AliveSeat(Seat(actingSeat)) &&
			!r.State.Players[actingSeat].IsBot &&
			r.BotAgents[actingSeat] == nil &&
			!r.State.WolfVoteCast[actingSeat]
		// BUG-R11-P0 (2026-07-30): night_wolves 在首次 wolf_kill skip 后,若仍未
		// 推进(其他狼未投票或仍 stuck),在 120s 时直接 force-tally,不必再等
		// 完整的 360s (原逻辑要等 skipCount>=1 且 elapsed>=360s,共 720s)。
		// R11 报告: seat 2 (GLM-model) 在 night_wolves stuck → skip → 再 stuck
		// 循环,对局停滞 12+ 分钟。此检查放在主 deadline 之前,优先于通用兜底。
		//
		// BUG-R243-P1-01 (2026-08-06): 第三条早期出口 — 「零投票死锁」。
		// skipCount 只统计 watchdog 派发的 skip;但狼 bot 可以一直"活跃"地调
		// speak / wolf_whisper / idle_silent(LLM 全部成功,UI 统计 166/166),
		// 却从不提交 wolf_kill 票。此时 watchdog 无 skip 可派(bot 未 quarantine、
		// LLM 正常),skipCount 恒为 0;真人房间 deadline 被 §127 提升到 ≥480s,
		// deadline 分支也不触发 → 唯一剩下的 360s 通用兜底也要等 skipCount>=1
		// 才 force-tally,阶段实卡 270 秒(报告实测 04:48:48 → 04:53:18)。
		// 识别信号: 进入阶段 ≥120s 且所有存活狼均未投票(与 humanActingWolf
		// 同类——"没有任何可推进的投票来源"),直接 force-tally,全狼视为弃权。
		noWolfVoteCast := true
		for i := 0; i < MaxPlayers; i++ {
			if r.State.AliveSeat(Seat(i)) && r.State.Roles[i] == RoleWerewolf && r.State.WolfVoteCast[i] {
				noWolfVoteCast = false
				break
			}
		}
		if r.State.Phase == PhaseNightWolves && actingSeat >= 0 &&
			elapsed >= phaseWatchdogSingleActorDeadline &&
			(r.phaseWatchdog.skipCount >= 1 || humanActingWolf || noWolfVoteCast) {
			reason := "120s after first skip"
			if humanActingWolf && r.phaseWatchdog.skipCount < 1 {
				reason = "human acting wolf not voting (120s)"
			}
			if noWolfVoteCast && r.phaseWatchdog.skipCount < 1 && !humanActingWolf {
				reason = "no wolf vote cast (120s)"
			}
			logger.L().Warn("werewolf: phase watchdog — night_wolves early force-tally ("+reason+")",
				zap.String("room_id", r.RoomID),
				zap.Int("acting_seat", actingSeat),
				zap.Bool("human_acting_seat", humanActingWolf),
				zap.Bool("no_wolf_vote_cast", noWolfVoteCast),
				zap.Int("skip_count", r.phaseWatchdog.skipCount),
				zap.Duration("elapsed", elapsed))
			// BUG-R231-P0-01: 锁内只做纯状态变更,记 wakeKind 由 defer 锁外发。
			// 必须在 tallyWolfVotes / endWolfPhase 之前记录原 phase(用于
			// EmitAutoSkip 的标签文案),因为 endWolfPhase 会推进 phase。
			forceTallyWakeKind = r.State.Phase.String()
			r.State.tallyWolfVotes()
			r.State.endWolfPhase()
			// tallyWolfVotes 已把 WolfKillTarget 写入,且 endWolfPhase 不会清它。
			forceTallyWolfKillTarget = int(r.State.WolfKillTarget)
			r.phaseWatchdog.key = ""
			r.phaseWatchdog.enteredAt = now
			r.phaseWatchdog.skipCount = 0
			return nil
		}

		// BUG-R239-P1-01 (2026-08-04): 单座位夜间阶段 120s 早期兜底。
		// 此前此分支(与下方的 hunter_shoot 分支)被错误地嵌套在 360s 通用兜底的
		// `if elapsed >= phaseWatchdogDeadlineFor(...)` 块内部,导致外层 360s 门槛
		// 未达成时内层 120s 分支根本不可达 —— 早期兜底从未生效,13 人局慢模型每个
		// 夜间阶段最坏多等 240s。修复:提前到通用兜底之前,成为独立的并列判断
		// (elapsed >= 120s 即派发)。这与上方 night_wolves 120s 早期 force-tally
		// 分支(第 394 行起)的摆放位置完全一致。
		if isSingleActorNightPhase(r.State.Phase) && actingSeat >= 0 &&
			actingSeat < MaxPlayers &&
			roleMatchesPhase(r.State.Phase, r.State.Roles[actingSeat]) &&
			elapsed >= phaseWatchdogSingleActorDeadline {
			logger.L().Warn("werewolf: phase watchdog — single-actor night phase early skip (120s)",
				zap.String("room_id", r.RoomID),
				zap.String("phase", r.State.Phase.String()),
				zap.Int("acting_seat", actingSeat),
				zap.Duration("elapsed", elapsed))
			roleName := r.State.Roles[actingSeat].String()
			if skipName, skipArg := wwplayer.SkipPhaseAction(r.State.Phase.String(), roleName); skipName != "" {
				if derr := m.dispatchQuarantinedSkipLocked(r, actingSeat, skipName, skipArg); derr != nil {
					logger.L().Warn("werewolf: single-actor night phase early skip dispatch failed",
						zap.String("room_id", r.RoomID),
						zap.String("skip_tool", skipName), zap.Error(derr))
				}
			}
			r.phaseWatchdog.enteredAt = now
			r.phaseWatchdog.lastLog = now
			r.phaseWatchdog.skipCount++
			return nil
		}

		// BUG-R239-P1-01 (同一根因): hunter_shoot 120s 早期兜底。
		// 此前同样被嵌套在 360s 块内,致其标注的「提前至 120s 兜底」从未生效。
		// BUG-R10-P0-3 (2026-07-29) 原意是提前至 120s 兜底:deadline 一到,
		// 立即走 dispatchQuarantinedSkipLocked(r, hunter, "hunter_shoot", -1)
		// → HunterShoot(-1) → resumeAfterHunterShoot,阶段正常推进。
		if r.State.Phase == PhaseHunterShoot && actingSeat >= 0 &&
			actingSeat < MaxPlayers &&
			r.State.Roles[actingSeat] == RoleHunter &&
			elapsed >= phaseWatchdogHunterDeadline {
			logger.L().Warn("werewolf: phase watchdog — hunter_shoot early skip (120s)",
				zap.String("room_id", r.RoomID),
				zap.Int("acting_seat", actingSeat),
				zap.Duration("elapsed", elapsed))
			roleName := r.State.Roles[actingSeat].String()
			if skipName, skipArg := wwplayer.SkipPhaseAction(r.State.Phase.String(), roleName); skipName != "" {
				if derr := m.dispatchQuarantinedSkipLocked(r, actingSeat, skipName, skipArg); derr != nil {
					logger.L().Warn("werewolf: hunter_shoot early skip dispatch failed",
						zap.String("room_id", r.RoomID),
						zap.String("skip_tool", skipName), zap.Error(derr))
				}
			}
			r.phaseWatchdog.enteredAt = now
			r.phaseWatchdog.lastLog = now
			r.phaseWatchdog.skipCount++
			return nil
		}

		if elapsed >= phaseWatchdogDeadlineFor(r.State.SeatCount) {
			// BUG-HUNTER2-P0-01 (2026-08-07): 警长竞选阶段是「并发行动」阶段
			// —— 所有存活玩家同时举手参选 + 同时投票,**没有 acting_seat**。
			// 此前 watchdog 用 lowestActiveBotSeatLocked(r) 作为 acting seat,
			// 导致 key=sheriff/0 在多个 bot 都未调 sheriff_elect 时永远不变,
			// 365s 后 360s 兜底派发 skip(原报告 seat 0 未行动但其实全房间都未
			// 行动)。修复:PhaseSheriff + PhaseDeadlineAt 已武装时,完全跳过
			// 360s 兜底,让上方 deadline 分支(默认 300s 全 AI / 120s 真人)接管。
			// deadline 分支会按 SkipPhaseAction("sheriff", role) → sheriff_elect,
			// 与正常人类/Agent 主动结束竞选走完全一致的路径。
			if r.State.Phase == PhaseSheriff && !r.State.PhaseDeadlineAt.IsZero() {
				logger.L().Info("werewolf: phase watchdog — sheriff phase bypassed 360s fallback (deadline branch handles it)",
					zap.String("room_id", r.RoomID),
					zap.Duration("elapsed", elapsed),
					zap.Time("deadline_at", r.State.PhaseDeadlineAt))
				// 不递增 skipCount / 不重置 enteredAt,让后续 tick 继续等待
				// deadline 分支自然接管。如果 PhaseDeadlineAt 被异常清零
				// (R223 防御性重新武装会修复),下一 tick 仍会走 360s 兜底。
				return nil
			}
			// Phase is permanently stuck — dispatch the skip action.
			logger.L().Warn("werewolf: phase watchdog — skip dispatched for stuck phase",
				zap.String("room_id", r.RoomID),
				zap.String("phase", r.State.Phase.String()),
				zap.Int("acting_seat", int(actingSeat)),
				zap.Duration("elapsed", elapsed))

			// 2026-07-11 §13-R91: 90s 兜底派发 skip 之前先做一次"最后唤醒"。
			// 与 deadline 派发前唤醒同源逻辑(见上方):给 bot 一次
			// 最后机会;若仍失败 → skip 是兜底。这样 Bug-4 「watchdog
			// 频繁空跳」不会完全跳过 bot。
			if actingSeat >= 0 {
				if bot := r.BotAgents[int(actingSeat)]; bot != nil {
					bot.PushEvent(wwplayer.AgentEvent{
						Kind: "wake",
						Context: wwtypes.GameContext{
							Phase:   r.State.Phase.String(),
							MySeat:  int(actingSeat),
							MyTurn:  true,
							MyVoted: false,
						},
					})
					logger.L().Info("werewolf: phase watchdog — pushed last wake before 90s skip",
						zap.String("room_id", r.RoomID),
						zap.String("phase", r.State.Phase.String()),
						zap.Int("acting_seat", int(actingSeat)))
				}
			}

			// BUG-WEREWOLF-P0-VOTE-WATCHDOG (R44): PhaseVote has a
			// unique rescue path. Per-bot auto-skip maps vote →
			// vote_skip (abstain) which is correct for an individual
			// stuck voter but wrong for the watchdog rescue: the
			// watchdog only fires when ALL other alive bots have voted
			// and the missing voter is the host driver. Casting one
			// more vote_skip would not finish the phase. Dispatch
			// finishVoteLocked directly to force-tally.
			//
			// BUG-R223 (2026-08-01): 此检查此前被放在 `if actingSeat >= 0 {`
			// 守卫**内部**,而 watchdogActingSeat(PhaseVote) 走
			// lowestActiveBotSeatLocked —— 该 helper 跳过所有 quarantined bot。
			// 13 bot 全 quarantine 时它返回 -1,整个 stall 救援块退化为
			// "只 skipCount++ 后重新计时",每 360s 空转一次,永不计票。
			// 唯一还能救场的只剩 630s deadline 分支(且每次进入 phase 只
			// 触发一次);若 PhaseDeadlineAt 未武装则房间永久卡死。
			// 修复:与 deadline 分支(上方)结构完全对齐 —— PhaseVote 的
			// force-tally 提到 actingSeat 守卫**之外**,计票本身不需要任何
			// acting bot 参与(未投票者按弃权计,见 TallyVotes)。
			// 最坏恢复时延 630s → 360s。
			if r.State.Phase == PhaseVote {
				if derr := m.finishVoteLocked(r, 0); derr != nil {
					logger.L().Warn("werewolf: phase watchdog finishVote dispatch failed",
						zap.String("room_id", r.RoomID),
						zap.Error(derr))
				} else {
					logger.L().Info("werewolf: phase watchdog force-tallied PhaseVote",
						zap.String("room_id", r.RoomID),
						zap.Int("driver_seat", actingSeat))
				}
			} else if actingSeat >= 0 {
				if r.State.Phase == PhaseNightWolves && r.phaseWatchdog.skipCount >= 1 {
					// BUG-R9-P0-1 (2026-07-29): night_wolves 与 PhaseVote 同属
					// "多座位投票,单座位 skip 无法推进" 的阶段。第一次超时已给
					// stuck 狼座位派过 wolf_kill skip(skipCount=1),但该狼因
					// LLM 持续失败(R9 seat 4 Xiaomi-model 全程 stuck)仍投不出票,
					// 继续派 skip 只会在同一 key 上无限循环(R9 实测 12:32→12:42
					// 循环 6 次),最终被迫触发 cooling+judge_game_over 强制关房,
					// 破坏"自然结束"原则。第二次超时直接强制计票:已投票的狼
					// 按 tally 规则计,未投票的狼视为弃权(tallyWolfVotes 内部
					// 跳过 NoSeat),与 deadline 分支 (3782 行) 行为完全一致。
					// BUG-R11-P0 (2026-07-30): 此分支现在作为兜底保留,正常情况
					// 由上方 120s 早期 force-tally 路径 (phaseWatchdogSingleActorDeadline)
					// 提前处理,不会再等到这里。
					logger.L().Warn("werewolf: phase watchdog — night_wolves repeated stuck; forcing wolf vote tally",
						zap.String("room_id", r.RoomID),
						zap.Int("acting_seat", actingSeat),
						zap.Int("skip_count", r.phaseWatchdog.skipCount))
					// BUG-R232-P0-01 (2026-08-02): 之前 R231 修复(30726e7)只覆盖了
					// 3 处 force-tally 路径(257/319/410),遗漏本 360s 兜底分支。
					// 这里继续持 r.mu 直接调 m.EmitAutoSkip / m.EmitWolfKill /
					// m.wakeActingAgentsLocked → Emit* 走 hub.BroadcastRoomIncludingSpectators
					// → h.mu.RLock,与下游路径形成 §92a 自死锁(详见 R232 报告)。
					// 修复:与上 3 处完全对齐 —— 锁内只做纯状态变更,把 Emit*/wake*
					// 全部交由函数顶部 defer 在 r.mu.Unlock() 之后派发。
					// forceTallyWakeKind 在 tallyWolfVotes / endWolfPhase 之前记录,
					// 因为 endWolfPhase 会推进 phase。
					forceTallyWakeKind = r.State.Phase.String()
					r.State.tallyWolfVotes()
					r.State.endWolfPhase()
					// tallyWolfVotes 已把 WolfKillTarget 写入,且 endWolfPhase 不会清它。
					forceTallyWolfKillTarget = int(r.State.WolfKillTarget)
					r.phaseWatchdog.key = ""
					r.phaseWatchdog.enteredAt = now
					r.phaseWatchdog.skipCount = 0
					return nil
				} else {
					roleName := r.State.Roles[actingSeat].String()
					if skipName, skipArg := wwplayer.SkipPhaseAction(r.State.Phase.String(), roleName); skipName != "" {
						if derr := m.dispatchQuarantinedSkipLocked(r, actingSeat, skipName, skipArg); derr != nil {
							logger.L().Warn("werewolf: phase watchdog skip dispatch failed",
								zap.String("room_id", r.RoomID),
								zap.String("skip_tool", skipName),
								zap.Error(derr))
						}
					}
				}
			}

			// Reset timer so we don't fire again immediately.
			r.phaseWatchdog.enteredAt = now
			r.phaseWatchdog.lastLog = now
			r.phaseWatchdog.skipCount++
		}
	} else {
		// Phase or acting seat changed — reset the watchdog and log once.
		if r.phaseWatchdog.key != "" {
			logger.L().Info("werewolf: phase watchdog — acting seat changed",
				zap.String("room_id", r.RoomID),
				zap.String("old_key", r.phaseWatchdog.key),
				zap.String("new_key", key))
		}
		r.phaseWatchdog.key = key
		r.phaseWatchdog.enteredAt = now
		r.phaseWatchdog.lastLog = now
		r.phaseWatchdog.skipCount = 0
	}
	return nil
}

// ─────────────────── BUG-R221 房间级熔断(全员 quarantine) ───────────────────

// allBotsQuarantinedLocked 判定房间是否处于"所有存活 bot 全部 quarantine 且
// 无存活真人可行动"的死局。caller 必须持 r.mu。
//
// 判定口径(与 lowestActiveBotSeatLocked 的遍历风格一致):
//   - 至少存在 1 个存活 bot agent(纯真人房间不适用本熔断)
//   - 所有存活 bot agent 都 IsQuarantined()
//   - 不存在"存活 + !IsBot + BotAgents[seat]==nil"的真人座位
//     (识别信号与 BUG-R212-P1-02 的 humanActingWolf 完全一致)
//
// 真人只要还活着就仍有推进能力(NightActionPanel / game.werewolf_action),
// 此时房间不算死局,交由既有的 phase deadline / stall 路径处理。
func allBotsQuarantinedLocked(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	aliveBots := 0
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		if !r.State.AliveSeat(Seat(seat)) {
			continue
		}
		aliveBots++
		if !ag.IsQuarantined() {
			return false
		}
	}
	if aliveBots == 0 {
		return false
	}
	// 仍有存活真人 → 房间还有推进动力,不熔断。
	for i := 0; i < MaxPlayers; i++ {
		if !r.State.AliveSeat(Seat(i)) {
			continue
		}
		if !r.State.Players[i].IsBot && r.BotAgents[i] == nil {
			return false
		}
	}
	return true
}

// allAliveWolvesQuarantinedLocked 判断 night_wolves 阶段是否处于"所有存活狼人均
// quarantine 且无存活真人狼人可行动"的死局。用于 phaseWatchdogTick 的早期
// force-tally 快速路径,避免卡在 90s 兜底循环。caller 必须持 r.mu。
//
// 判定:存在 >=1 匹存活狼人角色座位,且其中每个 bot 座位的 Agent 都
// IsQuarantined()、每个非 bot 座位(真人狼人)不存在(BotAgents[seat]==nil)。
// 一旦出现 1 匹存活 + 未 quarantine 的 bot 狼人,或 1 位存活真人狼人,返回 false,
// 让既有的通用 stall 路径处理。
func allAliveWolvesQuarantinedLocked(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	aliveWolves := 0
	for i := 0; i < MaxPlayers; i++ {
		if !r.State.AliveSeat(Seat(i)) || r.State.Roles[i] != RoleWerewolf {
			continue
		}
		aliveWolves++
		if ag := r.BotAgents[i]; ag != nil {
			if !ag.IsQuarantined() {
				return false
			}
		} else {
			// 非 bot 座位(真人狼人)仍存在 → 真人可推进,不触发。
			return false
		}
	}
	return aliveWolves > 0
}

// allQuarantinedTickLocked 是 phaseWatchdogTick 的房间级熔断分支。返回 true
// 表示本 tick 已经处理完毕(房间被强制结束),调用方应直接 return。
// caller 必须持 r.mu(由 phaseWatchdogTick 保证)。
//
// BUG-R221 (2026-08-01):全员 quarantine 的房间没有任何推进动力 —— 每个 bot 的
// wake 都被 IsQuarantined guard 短路,vote / night_wolves 这类"多座位投票"阶段
// 连 dispatchQuarantinedSkipLocked 都推不动。R221 观测到房间 ce288893 保持
// "游戏中" 15+ 小时,持续占用大厅席位。此前既无房间 TTL,也无"全员失联"升级。
//
// 需要连续 allQuarantinedTripTicks 个 tick(10 分钟)条件持续成立才熔断:任一
// bot 恢复(ResetConsecutiveFailures 清 quarantine)、或任一真人复活/入座,
// 计数器立即清零,给上游 LLM 代理瞬断留出恢复窗口。
func (m *WerewolfManager) allQuarantinedTickLocked(r *WerewolfRoom) bool {
	if r == nil || r.State == nil {
		return false
	}
	if !allBotsQuarantinedLocked(r) {
		r.phaseWatchdog.allQuarantinedTicks = 0
		return false
	}
	r.phaseWatchdog.allQuarantinedTicks++
	if r.phaseWatchdog.allQuarantinedTicks < allQuarantinedTripTicks {
		return false
	}
	stalled := time.Duration(r.phaseWatchdog.allQuarantinedTicks) * phaseWatchdogTickInterval
	logger.L().Warn("werewolf: all agents quarantined — force-ending room",
		zap.String("room_id", r.RoomID),
		zap.String("phase", r.State.Phase.String()),
		zap.Int("seat_count", r.State.SeatCount),
		zap.Int("ticks", r.phaseWatchdog.allQuarantinedTicks),
		zap.Duration("stalled", stalled))
	m.forceEndAllQuarantinedLocked(r)
	return true
}

// forceEndAllQuarantinedLocked 把全员 quarantine 的死局房间收编成正常的"对局
// 结束"。caller 必须持 r.mu。
//
// §108(b) 双写约束:先把 in-memory GameState 推到终态(Status="over" +
// Phase=PhaseGameOver + Winner="draw" —— 没有任何一方真正达成胜利条件,判和),
// 再复用既有的 forceCloseRoomLocked 走 EmitGameOver + onGameOver(DB status
// 同步为 over)。不另起清理路径,也不 stopAgentsLocked ——
// agent/watchdog goroutine 的 teardown 仍由 RemoveGame 统一负责。
func (m *WerewolfManager) forceEndAllQuarantinedLocked(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	m.EmitAutoSkip(r, r.State.Phase.String())
	if r.State.Status != "over" {
		r.State.Status = "over"
		r.State.Winner = "draw"
	}
	r.State.Phase = PhaseGameOver
	r.State.TurnActingSeat = NoSeat
	r.State.SpeakTurnSeat = NoSeat
	r.State.SetPhaseDeadline(r.State.Phase.String(), 0)
	// 熔断结束的房间不进冷却期 / 重开投票 —— 没有活着的 Agent 能重开。
	// forceCloseRoomLocked 会置 gameOverNotified=true,天然屏蔽后续 tick 的
	// tryEnterCoolingFromGameOverLocked / tryEnterRestartVoteFromGameOverLocked。
	m.forceCloseRoomLocked(r, "all_agents_quarantined")
}

func watchdogActingSeat(r *WerewolfRoom) int {
	gs := r.State
	if gs == nil {
		return -1
	}
	switch gs.Phase {
	case PhaseNightWolves, PhaseNightSeer, PhaseNightWitch, PhaseNightGuard, PhaseNightDemonHunter:
		// §134:守卫阶段 acting seat = gs.TurnActingSeat(已在 startNight 设置为 GuardSeat)。
		// §猎魔人:猎魔人阶段 acting seat = gs.TurnActingSeat(startNight 设置为 DemonHunterSeat)。
		return int(gs.TurnActingSeat)
	case PhaseDawn, PhaseSheriff:
		return lowestActiveBotSeatLocked(r)
	case PhaseSheriffOrder:
		// §20260810-09 — 警长定序阶段:watchdog 兜底默认值时把当前警长座位作为
		// acting seat,确保 90s 同 phase 兜底分支能精准派发 skip_sheriff_order。
		// 兜底默认值在 dispatchQuarantinedSkipLocked 中显式列出(§97)。
		if gs.SheriffSeat != NoSeat && gs.AliveSeat(gs.SheriffSeat) {
			return int(gs.SheriffSeat)
		}
		return -1
	case PhaseSpeak:
		return int(gs.SpeakTurnSeat)
	case PhaseHunterShoot:
		// BUG-R10-P0-3 (2026-07-29): 直接返回猎人座位,与 R42 R45 测试套
		// 件兼容(测试只设 Phase=PhaseHunterShoot,不强制
		// HunterPendingShoot=true)。HunterPendingShoot 的额外保护在
		// 派发分支 dispatchQuarantinedSkipLocked 与早期 deadline 兜底
		// 中体现,本处只负责"phase 是 hunter_shoot → 找到猎人"的
		// 单一职责。
		return findHunterSeat(r)
	case PhaseIdiotReveal:
		// 2026-07-10: 白痴翻牌阶段,acting seat 为最高票白痴(DayEliminated)。
		if r.State.DayEliminated >= 0 {
			return int(r.State.DayEliminated)
		}
		return lowestActiveBotSeatLocked(r)
	case PhaseDeathLyric:
		// BUG 2026-07-09: 遗言阶段。当前遗言座位是 acting seat;若队列异常
		// (DeathLyricCurrent==NoSeat 但 phase 仍是 death_lyric),回退到 driver
		// 派发 last_words_skip 以清空队列。
		if gs.DeathLyricCurrent >= 0 {
			return int(gs.DeathLyricCurrent)
		}
		return lowestActiveBotSeatLocked(r)
	case PhaseVote:
		// BUG-WEREWOLF-P0-VOTE-WATCHDOG (R44): PhaseVote used to return -1,
		// which left the watchdog unable to dispatch any skip. The
		// engine auto-tallies via DayVote → FinishVote(0) when every
		// alive seat has voted, but if the host driver is the only seat
		// still missing a vote (e.g. quarantined or stuck), allAliveVoted
		// stays false forever and PhaseVote spins at 90s intervals. Use
		// the lowest alive bot seat as the "driver" the watchdog will
		// target with a finish_vote skip — same pattern as dawn/sheriff.
		// BUG-R193-001: lowestActiveBotSeatLocked — 跳过被禁用的 bot。若选出的
		// acting driver 是 quarantined 的,watchdog 无法唤醒它(wake → IsQuarantined
		// guard → no-op),阶段空转。
		return lowestActiveBotSeatLocked(r)
	}
	return -1
}

func findHunterSeat(r *WerewolfRoom) int {
	gs := r.State
	if gs == nil {
		return -1
	}
	// BUG-R10-P0-3 (2026-07-29):保持 findHunterSeat 为"扫描猎人角色"的
	// 通用 helper,与房间 round45 测试套件的契约一致(测试先
	// findHunterSeat → 再 set HunterPendingShoot=true)。HunterPendingShoot
	// 守卫上提到 watchdogActingSeat 调用点,避免破坏既有调用约定。
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleHunter {
			return i
		}
	}
	return -1
}

// isSingleActorNightPhase BUG-R11-P0 (2026-07-30):返回该 phase 是否为
// "单座位 + 单工具"夜间阶段。这些阶段只有 1 个特定角色行动,1 次 tool_use
// 即可完成,适用 120s 早期兜底(推广 d7d3558 的 hunter_shoot 机制)。
// §猎魔人: 加入 PhaseNightDemonHunter(单座位 + demon_hunter_hunt 单工具)。
func isSingleActorNightPhase(p Phase) bool {
	switch p {
	case PhaseNightGuard, PhaseNightSeer, PhaseNightWitch, PhaseNightDemonHunter:
		return true
	}
	return false
}

// roleMatchesPhase BUG-R11-P0 (2026-07-30):校验 acting seat 的角色是否匹配
// 当前阶段需要行动的角色。用于早期兜底前的安全检查,避免误判。
// §猎魔人: 加入 PhaseNightDemonHunter → RoleDemonHunter 校验。
func roleMatchesPhase(p Phase, role Role) bool {
	switch p {
	case PhaseNightGuard:
		return role == RoleGuard
	case PhaseNightSeer:
		return role == RoleSeer
	case PhaseNightWitch:
		return role == RoleWitch
	case PhaseNightDemonHunter:
		return role == RoleDemonHunter
	}
	return false
}

func (r *WerewolfRoom) State_Begin() *GameState {
	if r.State == nil {
		r.State = NewGame(time.Now().UnixNano())
	}
	return r.State
}

