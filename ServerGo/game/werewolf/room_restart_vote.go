// Package werewolf — room_restart_vote.go: Room manager 侧的重开局投票派发路径。
// 详见 docs/狼人杀-Agent与系统/狼人杀重开局投票设计.md。
//
// 关键约束:
//   - 所有 *Locked 函数假定 caller 已持 r.mu(见 §92a sync.Mutex 不可重入)。
//   - restartGameLocked 必须保留 r.Seats + r.chatQueue + r.chatQueue.readPointers;
//     仅重置 r.State 的对局字段然后调 StartGame(seed)。
package werewolf

import (
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// ─────────────────── Manager 入口 ───────────────────

// Action_RestartVote 是人类玩家(WS)调用的入口。manager 持锁调用
// restartVoteLocked 完成投票动作 + 评估 quorum,通过则触发 restartGameLocked。
func (m *WerewolfManager) Action_RestartVote(roomID, userID, choice string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	if !lockRoomBriefly(r, 800*time.Millisecond) {
		return nil, errcode.Code(errcode.ErrLockContended)
	}
	defer r.mu.Unlock()

	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := restartVoteLocked(r, seat, choice); e != nil {
		return nil, e
	}
	// 评估并(必要时)推进
	m.evaluateAndApplyRestartVoteLocked(r)
	return r, nil
}

// RestartVoteBotLocked 供 agentRunner.RestartVote 调用的 lock-held 内部版本。
// manager 派发路径已经持锁,不通过 Action_RestartVote 入口。
func (m *WerewolfManager) RestartVoteBotLocked(r *WerewolfRoom, seat Seat, choice string) *errcode.Error {
	if r == nil || r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if e := restartVoteLocked(r, seat, choice); e != nil {
		return e
	}
	m.evaluateAndApplyRestartVoteLocked(r)
	return nil
}

// ─────────────────── Fast Restart (即刻原班重开) ───────────────────

// Action_FastRestartVote 是人类玩家(WS)调用的「即刻原班重开」入口。
// 功能:
//   - 若 Status != "over": 返回错误(对局仍在进行)。
//   - 若 PhaseRestartVote: 标记 FastRestart=true + 投一票 yes + 评估。
//   - 若 PhaseGameOver 或冷却期: 取消冷却期 → 强制进入 PhaseRestartVote
//     (FastRestart=true) + 投一票 yes + 评估。
//   - 阈值由 EvaluateRestartVoteLocked 按 FastRestart 标记动态降为简单多数。
func (m *WerewolfManager) Action_FastRestartVote(roomID, userID string) *errcode.Error {
	r := m.getRoom(roomID)
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	if !lockRoomBriefly(r, 800*time.Millisecond) {
		return errcode.Code(errcode.ErrLockContended)
	}
	defer r.mu.Unlock()

	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}

	switch r.State.Phase {
	case PhaseRestartVote:
		// 已在投票中: 标记 FastRestart + 投 yes + 评估。
		r.State.FastRestart = true
		if e := restartVoteLocked(r, seat, "yes"); e != nil {
			return e
		}
		m.evaluateAndApplyRestartVoteLocked(r)

	case PhaseGameOver:
		// 游戏结束或冷却期: 取消冷却期 → 强制进入投票(FastRestart) + 投 yes。
		m.forceEnterRestartVoteFastLocked(r)
		if e := restartVoteLocked(r, seat, "yes"); e != nil {
			return e
		}
		m.evaluateAndApplyRestartVoteLocked(r)

	default:
		// 对局仍在进行中(或其他非终局 phase)。
		return errcode.CodeMsg(errcode.ErrValidationFailed, "game is still in progress")
	}

	return nil
}

// forceEnterRestartVoteFastLocked 在 PhaseGameOver(含冷却期)时强制切入
// PhaseRestartVote 并标记 FastRestart=true。与 tryEnterRestartVoteFromGameOverLocked
// 类似,但跳过冷却期等待且始终置 FastRestart。
//
// caller 必须持 r.mu。
func (m *WerewolfManager) forceEnterRestartVoteFastLocked(r *WerewolfRoom) {
	if r.State.Phase != PhaseGameOver {
		return
	}
	// 取消冷却期(若已进入)。
	if r.coolingCancel != nil {
		r.coolingCancel()
		r.coolingCancel = nil
	}
	r.coolingDone = true

	// 触发结算(与 tryEnterRestartVoteFromGameOverLocked 相同的收口逻辑)。
	if !r.gameOverNotified {
		r.gameOverNotified = true
		facts := r.buildCommitFactsLocked()
		facts.IsGameOver = true
		r.evaluateCommitmentsForTriggerLocked(CommitApologyIfGood, facts)
		m.EmitGameOver(r, r.State.Winner)
		if m.onGameOver != nil {
			m.onGameOver(r.RoomID)
		}
		r.wakeJudgeLockedForSummaryLocked()
	}

	// 进入投票阶段。
	r.State.RestartLastWinner = r.State.Winner
	r.State.RestartVoteYes = map[Seat]bool{}
	r.State.RestartVoteNo = map[Seat]bool{}
	r.State.RestartVoteAbstain = map[Seat]bool{}
	r.State.RestartVoteDone = false
	r.State.RestartVoteResult = ""
	r.State.FastRestart = true

	deadline := restartVoteDeadlineSec()
	r.State.SetPhaseDeadline(r.State.Phase.String(), deadline)
	r.State.Phase = PhaseRestartVote
	r.State.TurnActingSeat = NoSeat
	r.State.SpeakTurnSeat = NoSeat
	logger.L().Info("werewolf: room force-entered fast restart vote",
		zap.String("room_id", r.RoomID),
		zap.Int("deadline_sec", deadline))
	m.wakeAllAgentsLocked(r, "state_change", buildAgentContextLocked(r, -1, lowestActiveBotSeatLocked(r)))
}

// ─────────────────── 内部 lock-held helper ───────────────────

// restartVoteLocked 写入投票(覆盖写)。caller 必须持 r.mu。
func restartVoteLocked(r *WerewolfRoom, seat Seat, choice string) *errcode.Error {
	return CastRestartVoteLocked(r, seat, choice)
}

// evaluateAndApplyRestartVoteLocked 在投票写入后 / deadline tick 后由 manager 调
// 用。逻辑:
//   - "passed"   → restartGameLocked → broadcast state → 新一局开始
//   - "rejected" / "timeout" → forceCloseRoomLocked(emit activity + 走原有 onGameOver)
//
// caller 必须持 r.mu。
func (m *WerewolfManager) evaluateAndApplyRestartVoteLocked(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	outcome := EvaluateRestartVoteLocked(r, false)
	if outcome == "pending" {
		return
	}
	r.State.RestartLastWinner = r.State.Winner // 缓存给前端展示
	logger.L().Info("werewolf: restart vote decided",
		zap.String("room_id", r.RoomID),
		zap.String("outcome", outcome),
		zap.Int("yes", len(r.State.RestartVoteYes)),
		zap.Int("no", len(r.State.RestartVoteNo)),
		zap.Int("abstain", len(r.State.RestartVoteAbstain)))

	switch outcome {
	case "passed":
		if err := m.restartGameLocked(r); err != nil {
			logger.L().Error("werewolf: restartGameLocked failed",
				zap.String("room_id", r.RoomID), zap.Error(err))
			// fallback: 关闭房间
			m.forceCloseRoomLocked(r, "restart_failed")
			return
		}
		// 推 wake 让所有 bot 重新看见 PhasePreWolves + 上一局 chat 作为上下文
		m.wakeAllAgentsLocked(r, "state_change", buildAgentContextLocked(r, -1, lowestActiveBotSeatLocked(r)))
	case "rejected":
		m.forceCloseRoomLocked(r, "rejected")
	case "timeout":
		m.forceCloseRoomLocked(r, "timeout")
	}
}

// restartDeadlineTickLocked 由 phaseWatchdog goroutine 周期性(每 5s)调用,
// 检查 PhaseRestartVote 是否到期。caller 持 r.mu。
func (m *WerewolfManager) restartDeadlineTickLocked(r *WerewolfRoom) {
	if r == nil || r.State == nil {
		return
	}
	if r.State.Phase != PhaseRestartVote || r.State.RestartVoteDone {
		return
	}
	if r.State.PhaseDeadlineAt.IsZero() {
		return
	}
	if !time.Now().After(r.State.PhaseDeadlineAt) {
		return
	}
	// 强制 quorum 评估(以 deadlineExpired=true 调用)
	outcome := EvaluateRestartVoteLocked(r, true)
	if outcome == "pending" {
		// 不应发生: deadline 到 + pending → 仍 pending 的场景,强制设为 timeout
		r.State.RestartVoteResult = "timeout"
		r.State.RestartVoteDone = true
		r.State.Phase = PhaseGameOver
		outcome = "timeout"
	}
	r.State.RestartLastWinner = r.State.Winner
	logger.L().Info("werewolf: restart vote deadline reached",
		zap.String("room_id", r.RoomID),
		zap.String("outcome", outcome))
	switch outcome {
	case "passed":
		if err := m.restartGameLocked(r); err != nil {
			logger.L().Error("werewolf: restartGameLocked failed (deadline)",
				zap.String("room_id", r.RoomID), zap.Error(err))
			m.forceCloseRoomLocked(r, "restart_failed")
			return
		}
		m.wakeAllAgentsLocked(r, "state_change", buildAgentContextLocked(r, -1, lowestActiveBotSeatLocked(r)))
	default:
		m.forceCloseRoomLocked(r, outcome) // "rejected" | "timeout"
	}
}

// ─────────────────── RestartGame 核心 ───────────────────

// restartGameLocked 原地复用本房间的座位,重置 GameState 字段 → 重新发牌开新一局。
// 发牌按 r.SeatCount 分支:7 人 = StandardDeck;12 人 = StandardDeck12;13 人 = StandardDeck13。
//
// 必须保留(不在本函数重置):
//   - r.Seats                       (7 座位与 userID 绑定)
//   - r.chatQueue + readPointers    (500K 队列 + 各 bot 序号;reset 计数但保留 messages)
//   - r.BotAgents + r.AgentNames    (Agent 引用;其内部 transcript 不会清空,
//                                    让 Agent 仍能看见上一局最后一轮 thinking)
//   - r.SeatModelKeys               (bot 模型绑定)
//   - r.watchdogCancel / r.watchdogActive
//
// caller 必须持 r.mu。
func (m *WerewolfManager) restartGameLocked(r *WerewolfRoom) *errcode.Error {
	if r == nil {
		return errcode.Code(errcode.ErrRoomNotFound)
	}
	oldSeats := r.Seats
	oldSeed := time.Now().UnixNano()
	// 缓存上一局的 winner + 上一次重开的次数到新 state(完成后再覆盖)
	prevWinner := r.State.Winner
	prevRestartCount := r.State.RestartCount
	prevSeatCount := r.State.SeatCount // 保留本局人数(7 / 12 / 13)

	// 重新分配角色 + 进 PhasePreWoles(StartGame)
	// §20260830-01: 经 newGameStateLocked 拷贝房间级「死亡亮身份」开关,
	// 同时清零死亡公开幂等簿记(新一局重新累计)。
	newState := r.newGameStateLocked(oldSeed)
	r.resetDeathRevealBookkeepingLocked()
	// 复用上一局人数(发牌选择 7 / 12 / 13 人牌组)
	switch prevSeatCount {
	case 7:
		newState.SeatCount = 7
	case 12:
		newState.SeatCount = 12
	default:
		newState.SeatCount = MaxPlayers
	}
	// 复用 seat/userID 绑定
	for i, uid := range oldSeats {
		newState.Seats[i] = uid
		if uid != "" {
			newState.PlayerByID[uid] = Seat(i)
		}
	}
	// 承接上一局的累计数据
	newState.RestartLastWinner = prevWinner
	newState.RestartCount = prevRestartCount + 1
	r.State = newState
	// 2026-08-13 §20260813-01 U2 — 重开时失效上下文缓存(角色/玩家列表已变)。
	invalidateContextCaches(r)
	// 2026-08-06 §20260806-03: 重开局沿用创建房间时的座位角色偏好
	// (seatPreferredRoles 生命周期与 seatModelKeys 同级,跨局保留)。
	syncPreferredRolesLocked(r)
	if err := r.State.StartGame(); err != nil {
		return err
	}

	// 2026-08-10 §20260810-05 — 信息账本:重开一局时清零(新一局信息重新累计),
	// 然后按发牌结果登记 role_deal(仅本人知情,§134/§135 身份隔离)。
	// 账本按「局」隔离 —— 角色已重发,跨局知情集合无意义。
	r.infoLedger = NewInformationLedger()
	// 2026-08-10 §20260810-08：新一局同步清空派生的说漏嘴缓存。
	r.leakCache = nil
	r.leakCacheSeq = 0
	r.ledgerRegisterRoleDealLocked()
	// 2026-08-11 §20260811-02 U1 — 影响力按「局」隔离:新一局票型/发言全部重来,
	// 沿用上一局分数会让开局即出现虚高影响力。
	r.resetInfluenceLocked()

	// chatQueue 不动;state.go 的字段已重置,但 readPointers 保留 → 投票期间写入的
	// 活动事件 "restart_vote_update" 等会作为新一局首轮的 chat 上下文(被 bot 在
	// pre_wolves 阶段读到)
	//
	// 2026-07-12 §129 增强 — 重置冷却期状态。新一局开始时冷却期清零,
	// 让下一局结束后能再次进入冷却期(若 config 开)。
	resetCoolingStateLocked(r)
	// 2026-07-14 BUG-R116-03: 新一局开始时重置单座位发言冷却。
	r.seatLastPublicSpeak = make(map[int]time.Time)
	// §20260811-07 U1 — 幽灵语音 1 次上限跨局重置。
	r.ResetGhostVoiceEmittedLocked()
	// §20260811-07 U2 — 战报触发器 + 高光跨局重置。
	r.ResetBattleReportTriggersLocked()
	// §20260811-08 U2 — 终局奖励发放标志跨局重置。
	// 漏掉此行会让原地重开的第二局起永不发奖(幂等守卫恒为 true)。
	r.settlementRewarded = false
	// §20260812-03 U2 — 暗线信件跨局重置(信件内容属于上一局,新一局清零)。
	if r.secretLetter != nil {
		r.secretLetter.reset()
	}
	// §20260814-01 U1 — 逐日票型 + 法官信任度轨迹跨局重置。
	// 与上方 settlementRewarded 同理:任何「整局累积」状态漏清零,
	// 第二局的个人复盘就会把第一局的票算进去。
	r.resetVoteHistoryLocked()
	// 复盘缓存同样属于上一局(30min TTL 会横跨重开局)。
	ClearReviewCacheForRoom(r.RoomID)
	logger.L().Info("werewolf: room restarted in-place",
		zap.String("room_id", r.RoomID),
		zap.Int64("seed", oldSeed),
		zap.Int("restart_count", r.State.RestartCount),
		zap.Int("kept_chat_messages", len(r.chatQueue.Snapshot())))
	return nil
}

// forceCloseRoomLocked 在 refused / timeout / restart_failed 等"重开投票未通过"
// 路径下被调,等价于 EmitGameOver + onGameOver,把 DB status 改为 over。
//
// caller 必须持 r.mu。函数会保证 gameOverNotified=true (防止 watchdog tick 重复触发)。
func (m *WerewolfManager) forceCloseRoomLocked(r *WerewolfRoom, outcome string) {
	if r == nil || r.State == nil {
		return
	}
	if r.gameOverNotified {
		return
	}
	r.gameOverNotified = true
	// §20260811-08 U2 — 原 grantSettlementRewardsLocked 调用已收口进 EmitGameOver
	// (那里覆盖全部 4 条终局路径)。此处保留调用会造成同一路径连调两次 ——
	// settlementRewarded 幂等守卫虽能挡住,但留着是误导,故删除。
	m.EmitGameOver(r, r.State.Winner)
	if m.onGameOver != nil {
		m.onGameOver(r.RoomID)
	}
	logger.L().Info("werewolf: room closed after restart vote",
		zap.String("room_id", r.RoomID),
		zap.String("outcome", outcome))
}

// tryEnterRestartVoteFromGameOverLocked 在 watchdog 检测到 Status=over + 还在
// PhaseGameOver 时调用,判定是否切到 PhaseRestartVote。设计 doc §5.1.
//
// caller 必须持 r.mu。函数在原 1782 行 gameOverNotified 块之后调用,
// 若不进入投票则保持原行为(force-close 房间)。
//
// 2026-07-16 BUG-R128-01 修复: 进入 PhaseRestartVote 时立即触发 EmitGameOver,
// 把 DB 状态同步为 over + 写入金币结算。此前 EmitGameOver 被延迟到投票结束
// (forceCloseRoomLocked) 才触发,导致:
//   - DB status 在冷却期+投票期间仍为 "playing",与 in-memory phase 脱钩
//   - 若进程在投票窗口内重启,结算完全丢失
// 进入投票即视为"对局已结束",后续投票结果只影响是否原地重开,不影响结算。
func (m *WerewolfManager) tryEnterRestartVoteFromGameOverLocked(r *WerewolfRoom) {
	if !shouldEnterRestartVoteLocked(r) {
		return
	}
	// BUG-R128-01: 进入投票前先把上一局 winner 缓存,再触发结算。
	r.State.RestartLastWinner = r.State.Winner
	r.State.RestartVoteYes = map[Seat]bool{}
	r.State.RestartVoteNo = map[Seat]bool{}
	r.State.RestartVoteAbstain = map[Seat]bool{}
	r.State.RestartVoteDone = false
	r.State.RestartVoteResult = ""

	// BUG-R128-01: 立即触发结算 + 同步 DB status,并置 gameOverNotified=true
	// 防止后续 forceCloseRoomLocked / restartGameLocked 路径重复结算。
	if !r.gameOverNotified {
		r.gameOverNotified = true
		// §20260810-06 — 终局时评估赛后道歉承诺
		facts := r.buildCommitFactsLocked()
		facts.IsGameOver = true
		r.evaluateCommitmentsForTriggerLocked(CommitApologyIfGood, facts)
		m.EmitGameOver(r, r.State.Winner)
		if m.onGameOver != nil {
			m.onGameOver(r.RoomID)
		}
		// 触发法官「整局总结」(原在 phaseWatchdogTick EmitGameOver 之后调用)。
		r.wakeJudgeLockedForSummaryLocked()
	}

	deadline := restartVoteDeadlineSec()
	r.State.SetPhaseDeadline(r.State.Phase.String(), deadline)
	r.State.Phase = PhaseRestartVote
	r.State.TurnActingSeat = NoSeat
	r.State.SpeakTurnSeat = NoSeat
	logger.L().Info("werewolf: room entered restart_vote phase",
		zap.String("room_id", r.RoomID),
		zap.Int("deadline_sec", deadline))
	// 立刻唤醒所有 bot,让其 system prompt 看到 phase=restart_vote + 投票面板
	m.wakeAllAgentsLocked(r, "state_change", buildAgentContextLocked(r, -1, lowestActiveBotSeatLocked(r)))
}

func restartVoteDeadlineSec() int {
	defer func() { _ = recover() }()
	cfg := config.Load()
	if cfg != nil && cfg.Werewolf.RestartVote.DeadlineSec > 0 {
		return cfg.Werewolf.RestartVote.DeadlineSec
	}
	return 300
}
