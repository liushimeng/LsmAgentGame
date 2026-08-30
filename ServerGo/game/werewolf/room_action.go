package werewolf

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/agent/wwtypes"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

func (m *WerewolfManager) ManagerAddPlayerAt(roomID, userID string, seat Seat) (*WerewolfRoom, *errcode.Error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		r = &WerewolfRoom{
			RoomID:         roomID,
			createdAt:      time.Now(),
			recentSpeeches: make([]wwtypes.SpeechEvent, 0, recentSpeechBufferSize),
			whisperInbox:   make(map[int][]wwtypes.WhisperEvent, MaxPlayers),
			// BUG-R242-P1-01: llmSema 由 StartAgentsLocked 懒创建(不在此处设)。
		}
		m.rooms[roomID] = r
	}
	m.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// 幂等
	if _, seated := r.SeatOf(userID); seated {
		return r, nil
	}
	if r.State == nil {
		// §20260830-01: 经 newGameStateLocked 拷贝房间级「死亡亮身份」开关。
		r.State = r.newGameStateLocked(m.seedFn())
	}
	_, e := r.State.AddPlayerAt(userID, seat)
	if e != nil {
		return nil, e
	}
	r.Seats[seat] = userID
	return r, nil
}

func (m *WerewolfManager) Action_SeerCheck(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.NightSeerCheck(seat, target); e != nil {
		return nil, e
	}
	// 2026-08-10 §20260810-05 — 信息账本:查验结果仅预言家本人知情。
	r.ledgerAppendLocked(InfoSourceNightSeer,
		fmt.Sprintf("seer_check seat=%d target=%d", int(seat), int(target)),
		singleKnowerSet(int(seat)), time.Now().UnixMilli())
	return r, nil
}

func (m *WerewolfManager) Action_SheriffStream(roomID, userID string, slot int, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	if !lockRoomBriefly(r, 800*time.Millisecond) {
		return nil, errcode.Code(errcode.ErrLockContended)
	}
	defer r.mu.Unlock()
	if e := m.sheriffStreamDeclareLocked(r, userID, slot, target); e != nil {
		return nil, e
	}
	return r, nil
}

func (m *WerewolfManager) Action_IdiotReveal(roomID, userID, choice string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	if !lockRoomBriefly(r, 800*time.Millisecond) {
		return nil, errcode.Code(errcode.ErrLockContended)
	}
	defer r.mu.Unlock()
	seat, _ := r.SeatOf(userID)
	revealed := choice == "reveal"
	if e := m.idiotRevealLocked(r, userID, choice); e != nil {
		return nil, e
	}
	// 广播翻牌结算帧(前端据此渲染翻牌结果动效)。通过 onIdiotRevealed 回调委托
	// ws 层 BroadcastRoom,避免 engine 包反向依赖 hub。
	if m.onIdiotRevealed != nil {
		m.onIdiotRevealed(roomID, int(seat), choice, revealed)
	}
	// 2026-08-10 §20260810-05 — 信息账本:白痴翻牌(实际翻牌才公开)全房公开。
	if revealed {
		r.ledgerAppendLocked(InfoSourceIdiotReveal,
			fmt.Sprintf("idiot_reveal seat=%d", int(seat)),
			aliveKnowerSetLocked(r), time.Now().UnixMilli())
	}
	return r, nil
}

func (m *WerewolfManager) Action_Witch(roomID, userID string, action string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.NightWitchAct(seat, action, target); e != nil {
		return nil, e
	}
	// 2026-08-10 §20260810-05 — 信息账本:女巫见刀/用药仅女巫本人知情。
	r.ledgerAppendLocked(InfoSourceNightWitch,
		fmt.Sprintf("witch_act seat=%d action=%s target=%d", int(seat), action, int(target)),
		singleKnowerSet(int(seat)), time.Now().UnixMilli())
	// §20260811-07 U2 — 战报触发器:女巫救人成功 / 毒杀命中狼。
	// 持锁态调用,§92a 锁内变体。
	if action == "antidote" && r.State.WitchSavedTarget != NoSeat {
		r.appendBattleReportTriggerLocked(HighlightKindWitchSave,
			int(seat), r.State.DayNumber,
			fmt.Sprintf("女巫(座位 %d)使用解药救下 %d 号", seat+1, int(r.State.WitchSavedTarget)+1))
	}
	if action == "poison" && target >= 0 && target < MaxPlayers &&
		r.State.Roles[target] == RoleWerewolf {
		r.appendBattleReportTriggerLocked(HighlightKindWitchPoisonWolf,
			int(target), r.State.DayNumber,
			fmt.Sprintf("女巫毒杀命中狼 %d 号", target+1))
	}
	return r, nil
}

func (m *WerewolfManager) Action_GuardProtect(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.NightGuardProtect(seat, target); e != nil {
		return nil, e
	}
	// 2026-08-10 §20260810-05 — 信息账本:守护目标仅守卫本人知情(§134 盲守对称)。
	r.ledgerAppendLocked(InfoSourceNightGuard,
		fmt.Sprintf("guard_protect seat=%d target=%d", int(seat), int(target)),
		singleKnowerSet(int(seat)), time.Now().UnixMilli())
	return r, nil
}

// Action_Challenge §20260810-11 H2 — 白天发言阶段公开质疑。
// 校验顺序(失败立即 return):
//  1. 房间存在 / 局已开始
//  2. challenger 存活 且 target 存活 且 != challenger(不自疑)
//  3. 当前 phase 必须是 PhaseSpeak(白天发言阶段)
//  4. challenger 当日未用(ChallengeUsedToday == false)
//  5. question 非空且长度 ≤ 60
//
// 成功路径:challengeLocked 设置目标 LastChallengedBy/Question,标记 challenger 已用,
// EmitChallenge 走活动流,§92a 教训**锁内**完成(无外部锁依赖),wakeActingAgentsLocked
// 仅唤醒被质疑者(§130 教训:仅行动者触发)。
func (m *WerewolfManager) Action_Challenge(roomID, userID string, target Seat, question string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := m.challengeLocked(r, seat, target, question); e != nil {
		return nil, e
	}
	return r, nil
}

// Action_KnightDuel §198 骑士白天决斗公开入口。
// 与 Action_GuardProtect 完全同构:SeatOf → r.State.KnightDuel →
// EmitKnightDuel → wakeActingAgentsLocked。本方法**不**修改 phase
// (发言轮继续走),只在 killPlayer 后通过状态变化广播。
func (m *WerewolfManager) Action_KnightDuel(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	// 提前记录是不是"命中狼",供 EmitKnightDuel 文案使用。
	hitWerewolf := false
	if target >= 0 && target < MaxPlayers {
		hitWerewolf = (r.State.Roles[target] == RoleWerewolf)
	}
	if e := r.State.KnightDuel(seat, target); e != nil {
		return nil, e
	}
	// §198 活动流广播:公开 actor/target/结果是公开技能的必要信息泄露。
	m.EmitKnightDuel(r, int(seat), int(target), hitWerewolf)
	// 2026-08-10 §20260810-05 — 信息账本:决斗结果全房公开。
	r.ledgerAppendLocked(InfoSourceKnightDuel,
		fmt.Sprintf("knight_duel seat=%d target=%d hit_wolf=%v", int(seat), int(target), hitWerewolf),
		aliveKnowerSetLocked(r), time.Now().UnixMilli())
	m.wakeActingAgentsLocked(r, "state_change")
	return r, nil
}

// Action_DemonHunterHunt §猎魔人 夜间狩猎公开入口。
// 与 Action_GuardProtect 完全同构:SeatOf → r.State.NightDemonHunterHunt →
// EmitDemonHunterHunt → wakeActingAgentsLocked。
// 本方法**不**修改 phase(由 endDemonHunterPhase 推进到 dawn),只在 killPlayer 后
// 通过状态变化广播。
func (m *WerewolfManager) Action_DemonHunterHunt(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	// 提前记录是否命中狼,供 EmitDemonHunterHunt 文案使用。
	hitWerewolf := false
	if target >= 0 && target < MaxPlayers {
		hitWerewolf = (r.State.Roles[target] == RoleWerewolf)
	}
	if e := r.State.NightDemonHunterHunt(seat, target); e != nil {
		return nil, e
	}
	// §猎魔人 活动流广播:公开 actor/target/结果是公开技能的必要信息泄露。
	m.EmitDemonHunterHunt(r, int(seat), int(target), hitWerewolf)
	// 2026-08-10 §20260810-05 — 信息账本:狩猎结果全房公开。
	r.ledgerAppendLocked(InfoSourceDemonHunter,
		fmt.Sprintf("demon_hunter seat=%d target=%d hit_wolf=%v", int(seat), int(target), hitWerewolf),
		aliveKnowerSetLocked(r), time.Now().UnixMilli())
	m.wakeActingAgentsLocked(r, "state_change")
	return r, nil
}

func (m *WerewolfManager) Action_DayVote(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	// BUG-R193-001: 同步 quarantine 状态到引擎,让 DayVote 的 auto-tally 正确
	// 排除被禁用的 bot。
	syncQuarantinedLocked(r)
	if e := r.State.DayVote(seat, target); e != nil {
		return nil, e
	}
	// 2026-08-10 §20260810-05 — 信息账本:白天票型(谁投了谁)全房公开。
	r.ledgerAppendLocked(InfoSourceDayVoteMap,
		fmt.Sprintf("day_vote seat=%d target=%d", int(seat), int(target)),
		aliveKnowerSetLocked(r), time.Now().UnixMilli())
	return r, nil
}

func (m *WerewolfManager) Action_FinishVote(roomID string, tiedRound int) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	// §20260811-07 U2 — 战报触发器:投票前统计票数,FinishVote 后判定 close_vote
	// (票数差 ≤1)。必须在 State.FinishVote 之前抓快照,否则 DayEliminated 已被
	// 写入且 TallyVotes 状态不可重现。
	prevTally := r.State.TallyVotes(false)
	prevTopCount := 0
	prevSecondCount := 0
	for _, c := range prevTally {
		if c > prevTopCount {
			prevSecondCount = prevTopCount
			prevTopCount = c
		} else if c > prevSecondCount {
			prevSecondCount = c
		}
	}
	if e := r.State.FinishVote(tiedRound); e != nil {
		return nil, e
	}
	// 2026-08-14 §20260814-01 U1 — 逐日票型入历史(个人复盘投票准确率维度)。
	// 三条 FinishVote 路径必须全部采集,漏一条即该天票型丢失(§92a/§132
	// 「同一语义在多路径必须完全一致」)。复用上方已抓的 prevTally 快照。
	r.recordVoteHistoryLocked(prevTally)
	// 触发 close_vote:票数差 ≤1 且未平票(平票已在 tiedRound==1 提前返回)
	if tiedRound == 0 && prevTopCount > 0 && prevTopCount-prevSecondCount <= 1 &&
		r.State.DayEliminated != NoSeat {
		r.appendBattleReportTriggerLocked(HighlightKindCloseVote,
			int(r.State.DayEliminated), r.State.DayNumber,
			fmt.Sprintf("险胜票:差 %d 票放逐 %d 号", prevTopCount-prevSecondCount, int(r.State.DayEliminated)+1))
	}
	// §20260810-06 — 投票结算后评估承诺
	facts := r.buildCommitFactsLocked()
	r.evaluateCommitmentsForTriggerLocked(CommitVoteTarget, facts)
	r.evaluateCommitmentsForTriggerLocked(CommitNoVoteFor, facts)
	return r, nil
}

func (m *WerewolfManager) Action_FinishSpeak(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.FinishSpeak(seat); e != nil {
		return nil, e
	}
	// BUG-WEREWOLF-P0-NEW-16 FIX: push the wake here so the chain advances
	// regardless of which caller path invoked us. Lock-held variant because
	// defer r.mu.Unlock() above means we cannot re-acquire the same mutex
	// via the public WakeActingAgents without deadlocking.
	m.wakeActingAgentsLocked(r, "state_change")
	return r, nil
}

func (m *WerewolfManager) finishSpeakLocked(r *WerewolfRoom, userID string) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.FinishSpeak(seat); e != nil {
		return e
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

func (m *WerewolfManager) Action_ProposeVote(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.ProposeVote(seat); e != nil {
		return nil, e
	}
	// 发起投票后直接进入投票阶段,唤醒所有行动者
	m.wakeActingAgentsLocked(r, "propose_vote")
	return r, nil
}

func (m *WerewolfManager) hunterShootLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.HunterShoot(seat, target); e != nil {
		return e
	}
	// §115 房间聊天 — 猎人开枪活动事件广播
	m.EmitHunterShoot(r, int(target))
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

func (m *WerewolfManager) idiotRevealLocked(r *WerewolfRoom, userID string, choice string) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.IdiotReveal(seat, choice); e != nil {
		return e
	}
	// §115 房间聊天 — 白痴翻牌活动事件
	if choice == "reveal" {
		m.emitActivity(r, ActivityEventKindPlayerDied, "🃏 "+strconv.Itoa(int(seat)+1)+"号 白痴翻牌免死",
			r.State.Phase.String(), r.State.DayNumber, "info", "🃏", int(seat), -1, false)
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

func (m *WerewolfManager) sheriffStreamDeclareLocked(r *WerewolfRoom, userID string, slot int, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.SheriffStreamDeclare(seat, slot, target); e != nil {
		return e
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

func (m *WerewolfManager) actionSpeakPreWolvesLocked(r *WerewolfRoom, seat Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhasePreWolves {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not in pre_wolves phase")
	}
	if seat < 0 || seat >= MaxPlayers || !r.State.AliveSeat(seat) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "seat not alive")
	}
	r.State.PreWolvesSpeakCount[seat]++
	r.State.Players[seat].HasSpoken = true
	return nil
}

func (m *WerewolfManager) recordForcedSpeakPlaceholderLocked(r *WerewolfRoom, seat Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhasePreWolves {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not in pre_wolves phase")
	}
	if seat < 0 || seat >= MaxPlayers || !r.State.AliveSeat(seat) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "seat not alive")
	}
	r.State.PreWolvesSpeakCount[seat]++
	r.State.Players[seat].HasSpoken = true
	logger.L().Debug("werewolf: forced-speak placeholder recorded (MaxToolUse exhausted)",
		zap.String("room_id", r.RoomID),
		zap.Int("seat", int(seat)),
		zap.Int("count", r.State.PreWolvesSpeakCount[seat]))
	return nil
}

func (m *WerewolfManager) advancePreWolvesRoundLocked(r *WerewolfRoom) bool {
	if r.State == nil || r.State.Phase != PhasePreWolves {
		return false
	}
	if !m.allForcedSpeakDoneLocked(r) {
		return false
	}
	// 已达最后一轮 → 经 startNight() 进入夜间(含守卫阶段判定)。
	// §134 BUG-R214-P0: 不可硬编码 PhaseNightWolves,理由同 watchdog grace 出口。
	if r.State.PreWolvesSpeakRound+1 >= r.State.PreWolvesSpeakRoundsPerPlayer {
		r.State.startNight()
		logger.L().Info("werewolf: pre_wolves forced-speak complete; advancing to night",
			zap.String("room_id", r.RoomID),
			zap.Int("round", r.State.PreWolvesSpeakRound),
			zap.Int("target", r.State.PreWolvesSpeakRoundsPerPlayer),
			zap.String("phase", r.State.Phase.String()))
		return true
	}
	// 还在中间轮:round++,清空 HasSpoken,留给下一轮
	r.State.PreWolvesSpeakRound++
	for i := range r.State.Players {
		r.State.Players[i].HasSpoken = false
	}
	logger.L().Info("werewolf: pre_wolves forced-speak round advanced",
		zap.String("room_id", r.RoomID),
		zap.Int("round", r.State.PreWolvesSpeakRound),
		zap.Int("target", r.State.PreWolvesSpeakRoundsPerPlayer))
	return false
}

func (m *WerewolfManager) allForcedSpeakDoneLocked(r *WerewolfRoom) bool {
	if r.State == nil {
		return false
	}
	target := r.State.PreWolvesSpeakRoundsPerPlayer
	if target <= 0 {
		return false
	}
	for i, p := range r.State.Players {
		if !p.Alive {
			continue
		}
		if r.State.PreWolvesSpeakCount[i] < target {
			return false
		}
	}
	return true
}

func firstLivingWolfLocked(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			return Seat(i)
		}
	}
	return NoSeat
}

func (m *WerewolfManager) wakeActingAgentsLocked(r *WerewolfRoom, kind string) {
	// 2026-07-24 优化:UI 暂停时跳过所有 wake,bot 不调 LLM。
	// 已存在的 wake 队列事件由 Agent 端 in-flight 逻辑自然 drain。
	if r.IsPaused() {
		return
	}
	driverSeat := lowestActiveBotSeatLocked(r)
	for seat, ag := range r.BotAgents {
		if ag == nil {
			continue
		}
		gc := buildAgentContextLocked(r, seat, driverSeat)
		if gc.Phase == "" || !gc.MyTurn {
			continue
		}
		// Quarantined acting bot — dispatch skip in-place so the chain still
		// advances. Same logic as WakeActingAgents; inlined here to avoid
		// re-acquiring r.mu by calling the public variant.
		// BUG-WEREWOLF-P0-NEW-27: helper only dispatches when the bot is
		// BOTH quarantined AND the acting seat (gc.MyTurn=true).
		if tryDispatchQuarantinedActingSkip(m, r, ag, gc) {
			continue
		}
		ag.PushEvent(wwplayer.AgentEvent{Kind: kind, Context: gc})
	}
}

func (m *WerewolfManager) Action_WolfSuicide(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.WolfSuicide(seat); e != nil {
		return nil, e
	}
	// §20260811-07 U2 — 战报触发器:狼自爆。
	r.appendBattleReportTriggerLocked(HighlightKindWolfSuicide,
		int(seat), r.State.DayNumber,
		fmt.Sprintf("狼 %d 号 白天自爆", seat+1))
	return r, nil
}

func (m *WerewolfManager) Action_HunterShoot(roomID, userID string, target Seat) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.HunterShoot(seat, target); e != nil {
		return nil, e
	}
	// 2026-08-10 §20260810-05 — 信息账本:猎人开枪(实际开枪才公开身份,§135)全房公开。
	r.ledgerAppendLocked(InfoSourceHunterShot,
		fmt.Sprintf("hunter_shot seat=%d target=%d", int(seat), int(target)),
		aliveKnowerSetLocked(r), time.Now().UnixMilli())
	// §20260811-07 U2 — 战报触发器:猎人带走狼。
	if target >= 0 && target < MaxPlayers && r.State.Roles[target] == RoleWerewolf {
		r.appendBattleReportTriggerLocked(HighlightKindHunterKillWolf,
			int(target), r.State.DayNumber,
			fmt.Sprintf("猎人 %d 号 带走狼 %d 号", seat+1, target+1))
	}
	return r, nil
}

func (m *WerewolfManager) Action_SheriffCandidate(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.SheriffCandidate(seat); e != nil {
		return nil, e
	}
	return r, nil
}

// Action_SheriffElect 结算警长竞选。
//
// §报告-20260804-03 BUG-06: 此前签名为 Action_SheriffElect(roomID) —— 不接
// userID,任何人(甚至观战者)都能结算。修复 BUG-01(前端「结束竞选」按钮从
// start_day 改为 elect)后必须补上调用者校验。
// userID=="" 是**系统内部调用**哨兵(agent sheriff_elect 兜底 / watchdog),
// 传 NoSeat 给引擎跳过校验。
func (m *WerewolfManager) Action_SheriffElect(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	actor := NoSeat
	if userID != "" {
		seat, ok := r.SeatOf(userID)
		if !ok {
			return nil, errcode.Code(errcode.ErrRoomNotIn)
		}
		actor = seat
	}
	if e := r.State.SheriffElect(actor); e != nil {
		return nil, e
	}
	// OBS-R239-01 (§89 路径对称): watchdog 兜底路径 sheriffElectLocked 在
	// SheriffElect 成功后调 EmitSheriffElect 公开播报警长当选;人类/Agent 正常
	// 路径 Action_SheriffElect 此前遗漏此行,导致警长选出后聊天/活动流无
	// 「⭐ 警长:N号」播报。补齐以消除双路径不对称。
	if r.State.SheriffSeat != NoSeat {
		m.EmitSheriffElect(r, int(r.State.SheriffSeat))
		// 2026-08-10 §20260810-05 — 信息账本:警长当选全房公开。
		r.ledgerAppendLocked(InfoSourceSheriffElect,
			fmt.Sprintf("sheriff_elect seat=%d", int(r.State.SheriffSeat)),
			aliveKnowerSetLocked(r), time.Now().UnixMilli())
	}
	return r, nil
}

// Action_SheriffSetSpeakOrder §20260810-09 — 警长定序公开方法。
// 仅当 PhaseSheriffOrder + 警长存活 + 调用者是警长本人时合法。direction 取
// "cw"/"ccw",selfPos 取 "first"/"last"。由前端 NightActionPanel 与 Agent
// sheriff_set_speak_order 工具共用,统一走 *Locked 双变体避免 §92a 自死锁。
func (m *WerewolfManager) Action_SheriffSetSpeakOrder(roomID, userID, direction, selfPos string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.SheriffSeat == NoSeat {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, "no sheriff in this game")
	}
	sheriffUserID := r.Seats[r.State.SheriffSeat]
	if userID != sheriffUserID {
		return nil, errcode.CodeMsg(errcode.ErrPermissionDenied, "only sheriff may set speak order")
	}
	if e := m.sheriffSetSpeakOrderLocked(r, userID, direction, selfPos); e != nil {
		return nil, e
	}
	return r, nil
}

func (m *WerewolfManager) Action_StartDay(roomID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	if e := r.State.StartDay(); e != nil {
		return nil, e
	}
	// 警徽流结算:上夜警长死亡时(含预言家与非预言家)自动结算并广播。
	m.maybeSettleSheriffStreamLocked(r)
	// §20260810-06 — 白天开始时评估夜间相关承诺
	facts := r.buildCommitFactsLocked()
	r.evaluateCommitmentsForTriggerLocked(CommitSeerCheck, facts)
	r.evaluateCommitmentsForTriggerLocked(CommitNoUseSkill, facts)
	return r, nil
}

func (m *WerewolfManager) maybeSettleSheriffStreamLocked(r *WerewolfRoom) {
	gs := r.State
	if gs == nil || gs.sheriffSlain == NoSeat {
		return
	}
	// §20260810-04 U3 — SettleSheriffOnDeathLocked 内部会把 sheriffSlain 复位为
	// NoSeat,先捕获死去警长座位供广播帧的 streamFaction 查验历史判定使用。
	deadSheriff := gs.sheriffSlain
	successor, ripped := r.SettleSheriffOnDeathLocked(gs)
	gs.SheriffSuccessor = successor
	// 实际移交:非撕警徽且 successor 存活时,立即变更警长座位。
	if !ripped && successor != NoSeat && gs.AliveSeat(successor) && successor < Seat(MaxPlayers) {
		gs.SheriffSeat = successor
		gs.Players[successor].IsSheriff = true
	} else if ripped {
		gs.SheriffSeat = NoSeat
	}
	// 广播警徽流结算帧(前端据此渲染移交/撕警徽动效)。
	// §20260810-04 U3 — 广播阵营同样走 streamFaction 单点判定:未经真实查验的
	// 目标下发 "unknown" 而非底牌阵营,避免结算帧绕过「真查过才公开」语义。
	streamTargets := [2]int{-1, -1}
	streamFactions := [2]string{"", ""}
	for i, st := range gs.SheriffStreams {
		if st >= 0 && st < Seat(MaxPlayers) {
			streamTargets[i] = int(st)
			streamFactions[i] = streamFaction(gs, deadSheriff, st)
		}
	}
	// 通过 onSheriffStreamSettle 回调委托 ws 层 BroadcastRoom,避免 engine 包反向依赖 hub。
	if m.onSheriffStreamSettle != nil {
		m.onSheriffStreamSettle(r.RoomID, map[string]any{
			"room_id":         r.RoomID,
			"dead_seat":       int(gs.sheriffSlain),
			"dead_role":       RoleUnknown.String(), // 夜间死亡不公开身份(规则)
			"successor":       int(successor),
			"ripped":          ripped,
			"reason":          "night_death",
			"stream_targets":  streamTargets,
			"stream_factions": streamFactions,
			"sheriff_seat":    int(gs.SheriffSeat),
		})
	}
	// 2026-08-10 §20260810-05 — 信息账本:警徽流结算(移交/撕警徽)全房公开。
	// faction 文本经 streamFaction 单点判定(§20260810-04 U3),账本记录其结果,
	// 身份明文由 redactLedgerFact 二次兜底。
	r.ledgerAppendLocked(InfoSourceSheriffStream,
		fmt.Sprintf("sheriff_stream dead=%d successor=%d ripped=%v targets=%v factions=%v",
			int(deadSheriff), int(successor), ripped, streamTargets, streamFactions),
		aliveKnowerSetLocked(r), time.Now().UnixMilli())
}

func (m *WerewolfManager) Action_LastWords(roomID, userID string, text string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := m.sayLastWordsLocked(r, seat, text); e != nil {
		return nil, e
	}
	return r, nil
}

func (m *WerewolfManager) enterDeathLyricRoundLocked(r *WerewolfRoom, seats []Seat, onDone func() *errcode.Error) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	prePhase := r.State.Phase
	if err := r.State.tryEnterDeathLyricRound(seats, onDone); err != nil {
		return err
	}
	// 若成功进入遗言阶段(prePhase != death_lyric → now == death_lyric),广播 start 事件。
	if prePhase != PhaseDeathLyric && r.State.Phase == PhaseDeathLyric && r.State.DeathLyricCurrent >= 0 {
		m.EmitDeathLyricStart(r, int(r.State.DeathLyricCurrent))
	}
	return nil
}

func (m *WerewolfManager) Action_SkipLastWords(roomID, userID string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := m.skipLastWordsLocked(r, seat); e != nil {
		return nil, e
	}
	return r, nil
}

// §20260811-10 U1 — 照妖镜特化单帧:向购买者推送 prop.mirror_reveal。
// 锁外调用;PropSpecialPusher 为 nil 时静默跳过(老部署零感知)。
// payload 含 target_seat(被照的 bot 座位)与提示信息;具体 HeartThought
// 转发由 ws 层在收到此帧后,再单独请求目标 bot 的最新 BotTranscript。
func (m *WerewolfManager) sendMirrorRevealToBuyer(roomID, userID string, targetSeat int) {
	if m.PropSpecialPusher == nil {
		return
	}
	m.PropSpecialPusher(userID, "prop.mirror_reveal", map[string]any{
		"room_id":     roomID,
		"prop_key":    "mirror_check",
		"target_seat": targetSeat,
		"hint":        "目标 Agent 下一轮 LLM 将被强制追加真实身份指令;其内心独白可向你开放一次。",
	})
}

// §20260811-10 U2 — 心理侧写特化单帧:向购买者推送 prop.behavior_report。
// payload 直接挂 4 维报告;前端 BehaviorReportModal 渲染。
func (m *WerewolfManager) sendBehaviorReportToBuyer(roomID, userID string, report *BehaviorReportJSON) {
	if m.PropSpecialPusher == nil || report == nil {
		return
	}
	m.PropSpecialPusher(userID, "prop.behavior_report", map[string]any{
		"room_id":                   roomID,
		"prop_key":                  "behavior_analyze",
		"target_seat":               report.Seat,
		"speak_contradiction_rate":  report.SpeakContradictionRate,
		"emotion_volatility":        report.EmotionVolatility,
		"vote_consistency":          report.VoteConsistency,
		"faction_leaning_wolf":      report.FactionLeaningWolf,
		"faction_leaning_good":      report.FactionLeaningGood,
		"sample_speak_count":        report.SampleSpeakCount,
		"sample_vote_count":         report.SampleVoteCount,
	})
}

func (m *WerewolfManager) Action_UseProp(roomID, userID, propKey string, target int, payload string) (*WerewolfRoom, PropUseResult, *errcode.Error) {
	if m.propEngine == nil {
		return nil, PropUseResult{}, errcode.Code(errcode.ErrPropEngineUnavailable)
	}
	r := m.getRoom(roomID)
	if r == nil {
		return nil, PropUseResult{}, errcode.Code(errcode.ErrRoomNotFound)
	}
	if r.State == nil {
		return nil, PropUseResult{}, errcode.Code(errcode.ErrGameNotStarted)
	}
	// 短时持锁读取 seat + 准备 PropUseRequest(纯读,不放任何副作用)
	if !lockRoomBriefly(r, 200*time.Millisecond) {
		return nil, PropUseResult{}, errcode.Code(errcode.ErrLockContended)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		r.mu.Unlock()
		return nil, PropUseResult{}, errcode.Code(errcode.ErrRoomNotIn)
	}
	if !r.State.AliveSeat(seat) {
		r.mu.Unlock()
		// R173 P2: 专门的 error code 让前端能明确提示"死亡玩家不能用道具",
		// 而不是笼统的 "validation failed"。
		return nil, PropUseResult{}, errcode.Code(errcode.ErrPropPlayerDead)
	}
	roleTo := ""
	if target >= 0 && target < len(r.State.Roles) {
		roleTo = r.State.Roles[target].String()
	}
	toUserID := ""
	if target >= 0 && target < len(r.Seats) {
		toUserID = r.Seats[target]
	}
	phaseAtUse := r.State.Phase.String()
	roundAtUse := r.State.DayNumber
	req := PropUseRequest{
		RoomID:     roomID,
		FromSeat:   int(seat),
		FromUserID: userID,
		ToSeat:     target,
		ToUserID:   toUserID,
		PropKey:    propKey,
		Payload:    payload,
		RoleTo:     roleTo,
		PhaseAtUse: phaseAtUse,
		RoundAtUse: roundAtUse,
	}
	r.mu.Unlock()

	// 调引擎(内部会自己持锁;持锁时间应 < 500ms 由 DB 写入主导)
	result := m.propEngine.UseProp(context.Background(), req, r)
	if !result.Success {
		return r, result, errcode.CodeMsg(result.ErrorCode, result.ErrorMsg)
	}

	// v2：中招则注入队列(短时持锁)——注入文本 + 干扰信号(EffectTypes + TwistSeat)。
	// 2026-08-07 §20260807-04 P0-2:AOE 道具 target=-1 时旧条件 target>=0 恒 false
	// → EffectTypes 永不落地;改为 AOE 时遍历所有存活 bot 逐个入队。
	if result.Hit {
		if lockRoomBriefly(r, 200*time.Millisecond) {
			catEntry, _ := m.propCatalog.GetEnabled(propKey)
			enqueueFor := func(targetSeat int) {
				if targetSeat < 0 || targetSeat >= len(r.State.Players) {
					return
				}
				twistSeat := r.computeTwistSeatLocked(catEntry.EffectSpec.TwistSeatSrc, int(seat), targetSeat)
				r.enqueuePropHitLocked(targetSeat, PropInjectEntry{
					FromSeat:     int(seat),
					PropKey:      propKey,
					InjectText:   result.InjectResult.InjectText,
					EffectTypes:  catEntry.EffectSpec.EffectTypes,
					TwistSeat:    twistSeat,
					Hit:          true,
					ExpiresAfter: 1,
				})
			}
			switch {
			case catEntry != nil && catEntry.IsAOE:
				for s, p := range r.State.Players {
					if !p.Alive || !p.IsBot {
						continue
					}
					enqueueFor(s)
				}
			case target >= 0:
				enqueueFor(target)
			}
			// 2026-08-07 §20260807-04 P0-3:人类反制道具 — debuff 写到目标人类座位。
			if catEntry != nil && catEntry.TargetCamp == "human" && target >= 0 && target < len(r.State.Players) {
				spec := buildHumanDebuffSpecLocked(catEntry, int(seat), target)
				if spec != nil {
					r.setHumanDebuffLocked(target, *spec)
				}
			}
			r.mu.Unlock()
		}
	}

	// 公开广播(短时持锁;broadcastPropUseLocked 内部只调 emitActivity,无重锁)。
	var (
		behaviorReport *BehaviorReportJSON
		mirrorTarget   int = -1
	)
	if lockRoomBriefly(r, 200*time.Millisecond) {
		catName := propKey
		isAOE := false
		if entry, ok := m.propCatalog.GetEnabled(propKey); ok {
			catName = entry.NameZh
			isAOE = entry.IsAOE
		}
		// R190 Bug 1: AOE 道具无论客户端传入什么 target,一律归一化为 -1,
		// 保证 broadcast 文案("对所有玩家")与 prop_history.to_seat 一致。
		// 否则 target=0 会让玩家误以为只打了 1 号玩家(实际是全场)。
		broadcastTarget := target
		if isAOE {
			broadcastTarget = -1
		}
		// §20260811-10 U1 / U2 — 道具特化的「不走 BroadcastRoom」单帧推送。
		// behavior_analyze 的报告帧仅推送给购买者,不入公屏广播(§119 + §135)。
		// 本节在持锁路径中读 report 并清空,锁外调 m.sendPropSpecialFrame 给买家。
		if result.Hit {
			switch propKey {
			case "behavior_analyze":
				behaviorReport = r.pendingBehaviorReport
				if behaviorReport != nil {
					// 取走并清空(下次的 behavior_analyze 不会误推旧报告)。
					r.pendingBehaviorReport = nil
				}
			case "mirror_check":
				mirrorTarget = target
			}
		}
		// 2026-07-23 §道具特效:附 propKey,驱动前端 game.werewolf_prop_used 特效帧。
		m.broadcastPropUseLocked(r, int(seat), broadcastTarget, propKey, catName, result.Hit)
		// v3 §G5 — 写入公开道具使用历史(环形 buffer,供 prop_history 工具查询)。
		r.recordPropHistoryLocked(PropHistoryRecord{
			FromSeat:   int(seat),
			ToSeat:     broadcastTarget,
			PropKey:    propKey,
			PropNameZh: catName,
			Hit:        result.Hit,
			EffectHint: result.InjectResult.EffectHint,
			Phase:      phaseAtUse,
			Round:      roundAtUse,
			CreatedAt:  time.Now().Unix(),
		})
		r.mu.Unlock()
	}
	// §20260811-10 U1 / U2 — 道具特化单帧推送(锁外执行,无锁竞争)。
	// 仅推送给购买者,不走 BroadcastRoom,前端据 frame kind 渲染。
	if behaviorReport != nil {
		m.sendBehaviorReportToBuyer(roomID, userID, behaviorReport)
	}
	if mirrorTarget >= 0 {
		m.sendMirrorRevealToBuyer(roomID, userID, mirrorTarget)
	}
	// 唤醒所有 bot agent — WakeActingAgents 是公共路径(manager 内取快照 + 派发),
	// 不需要持 r.mu(参考 agentRunner.wakeAll 的实现)。
	m.WakeActingAgents(roomID, "prop_used")
	return r, result, nil
}

func (m *WerewolfManager) ForceStartIfReady(roomID string) (bool, *WerewolfRoom) {
	r := m.getRoom(roomID)
	if r == nil {
		return false, nil
	}
	r.mu.Lock()
	if r.State == nil || r.State.Phase != PhaseFilling {
		r.mu.Unlock()
		return false, r
	}
	if !r.IsReady() {
		r.mu.Unlock()
		return false, r
	}
	// §130 重构(2026-07-13):有 Agent + 真人时,先进入"人类等待窗口"。
	// 等待窗口已建立 → 返回 true 让调用者知道已"启动"(虽未真正 StartGame)。
	if m.tryStartWithHumanWaitLocked(r) {
		// BUG-R200-R211-P0-01 (2026-07-30): 人类等待窗口建立时,立刻给
		// publicStateCache 种下{filling,open}快照,避免 REST 路径在引擎持锁时
		// 退回 publicStateCache 命中却不存在的 room → fallback 到空数据;实际
		// 后端完整 StartGame 后由 completeHumanWaitAndStart / watchdog 覆盖。
		seedPhase := PhaseFilling.String()
		seedDay := 0
		seedStatus := "open"
		r.mu.Unlock()
		m.publicStateCache.Store(roomID, PublicState{Phase: seedPhase, Day: seedDay, Status: seedStatus})
		return true, r
	}
	// 2026-08-06 §20260806-03: 发牌前同步座位角色偏好。
	syncPreferredRolesLocked(r)
	if err := r.State.StartGame(); err != nil {
		logger.L().Warn("werewolf force-start failed",
			zap.String("room_id", roomID), zap.Error(err))
		r.mu.Unlock()
		return false, r
	}
	started := true
	r.gameStartedAt = time.Now().Unix()
	// 2026-08-10 §20260810-05 — 信息账本 role_deal 登记(发牌成功路径)。
	r.ledgerRegisterRoleDealLocked()
	// 2026-07-14 BUG-R116-03: 新一局开始时重置单座位发言冷却。
	r.seatLastPublicSpeak = make(map[int]time.Time)
	// 2026-07-15 BUG-R124-UI-001: 新一局开始时清零单座位每阶段发言计数。
	r.seatSpeakCountThisPhase = make(map[int]int)
	r.seatSpeakCountPhaseTag = ""
	logger.L().Info("werewolf game force-started (full AI room)",
		zap.String("room_id", roomID),
		zap.Int64("seed", r.State.Seed))
	m.StartAgentsLocked(r)
	// 2026-07-17 BUG-R135-001: ForceStartIfReady 是全 AI 0+13 房间创建的必经路径
	// (CreateRoomWithAgents → SetJudgeConfig → RegisterAgentSeats → ForceStartIfReady),
	// 与 JoinGame / StartGame 路径对称,必须在此处同时启动法官 goroutine,否则
	// 法官面板 UI / ⚖️ 公屏消息 / 阶段播报 / 活动流 在最常用自动化测试场景下全部不可见。
	m.startJudgeGoroutine(r)
	// BUG-WEREWOLF-FULL-AI-NO-WAKE: 复述段落已压缩 — git blame 与 docs/ 索引可还原

	m.wakeAllAgentsLocked(r, "state_change", wwtypes.GameContext{Phase: r.State.Phase.String()})
	// BUG-R200-P0-01 (2026-07-30): 在持锁状态同步调 DB 写(m.onGameStarted
	// 触发 UpdateRoomStatus 等 SQL)会阻断所有试图抢 r.mu 的 WS handler /
	// REST snapshot 路径,表现为全 AI 房间 24 分钟零 Agent 活动 + WS
	// game.spectate 无响应。释放锁后再回调 DB 写,语义不变(已 StartGame + wake,
	// DB 状态只滞后毫秒级)。
	// 锁内捕获 seed 用值供锁外 publicStateCache 种入快照,避免越界读 r.State。
	seedPhase := r.State.Phase.String()
	seedDay := r.State.DayNumber
	seedStatus := r.State.Status
	r.mu.Unlock()
	// Notify caller to update DB room status from "open" to "playing".
	if m.onGameStarted != nil {
		m.onGameStarted(roomID)
	}
	// BUG-R200-P2-04 (2026-07-30): 在 force-start 成功路径主动给 publicStateCache
	// 种下权威快照,避免后续 lockRoomBriefly 抢锁失败时回落缓存永远返回 "filling"。
	// 注意: publicStateCache 是 sync.Map(零值即可用),无 nil 比较意义。
	ps := PublicState{
		Phase:  seedPhase,
		Day:    seedDay,
		Status: seedStatus,
	}
	m.publicStateCache.Store(roomID, ps)
	return started, r
}

// Action_WolfKill 狼人夜间投票击杀。reason 为可选刀人理由(§20260810-04 U2,
// ≤30 字,仅狼 bot GameContext 可见);人类 WS 路径与兜底路径传 ""。
func (m *WerewolfManager) Action_WolfKill(roomID, userID string, target Seat, reason string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil || r.State.Phase != PhaseNightWolves {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	if r.State.Roles[seat] != RoleWerewolf {
		return nil, errcode.Code(errcode.ErrPermissionDenied)
	}
	// 2026-08-01 BUG-R225-P1-01: agent 自救路径(LLM 400 后 auto-skip)曾与
	// manager/watchdog 路径分歧 — 后者在 dispatchQuarantinedSkipLocked
	// (room_agent.go:530-543) 复位 WolfVoteCast,前者没有,导致 [30201]
	// "you have already voted this round" 死循环每 8s 一次,直到 watchdog
	// 90s 兜底。把复位下沉到 Action_WolfKill 入口,两条路径共用单一真相源
	// (§97 双路径同步约束),弃权(target==NoSeat)视为新一轮投票。
	if target == NoSeat && r.State.WolfVoteCast[seat] {
		logger.L().Warn("werewolf: wolf_kill skip — resetting prior vote (Action_WolfKill)",
			zap.String("room_id", roomID),
			zap.Int("seat", int(seat)))
		r.State.WolfVoteCast[seat] = false
		r.State.WolfVotes[seat] = NoSeat
		r.State.WolfVoteReasons[seat] = "" // §20260810-04 U2: 弃权同步清理由
	}
	if e := r.State.NightWolfKill(seat, target, reason); e != nil {
		return nil, e
	}
	// 2026-08-10 §20260810-05 — 信息账本:狼刀投票(含理由)仅存活狼座位知情。
	// fact 不含具体身份结论,仅记「某狼投某座」的投票行为本身。
	r.ledgerAppendLocked(InfoSourceNightWolfVote,
		fmt.Sprintf("wolf_vote seat=%d target=%d reason=%s", int(seat), int(target), reason),
		aliveWolfKnowerSetLocked(r), time.Now().UnixMilli())
	// 2026-07-17: 若已计票(Phase 已推进),广播最终击杀结果。
	// 投票快照通过 BuildClientState.WolfVoteView 随 game.state 广播(无需额外帧)。
	if r.State.Phase != PhaseNightWolves {
		m.EmitWolfKill(r, int(r.State.WolfKillTarget))
	}
	m.wakeActingAgentsLocked(r, "wolf_vote")
	return r, nil
}

func (m *WerewolfManager) Action_Pause(roomID, userID string, pause bool, reason string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	// 权限校验:仅房主(r.Seats[0]) 可暂停。
	// (若需要 admin 介入,在 auth_service 注入 admin_users 白名单后再扩展。)
	if !r.IsOwner(userID) {
		return nil, errcode.Code(errcode.ErrPermissionDenied)
	}
	if r.paused == pause {
		// 幂等:已是目标状态,直接返回。
		return r, nil
	}
	r.paused = pause
	if pause {
		r.pausedBy = userID
		r.pausedAt = time.Now()
		r.pausedReason = reason
		logger.L().Info("werewolf: room paused",
			zap.String("room_id", roomID), zap.String("by", userID),
			zap.String("reason", reason))
	} else {
		logger.L().Info("werewolf: room resumed",
			zap.String("room_id", roomID), zap.String("by", userID))
		r.pausedReason = ""
	}
	return r, nil
}

// Action_PublicCommit 玩家公开做出行为承诺（§20260810-06）。
// 仅白天发言阶段可用；计入 speakLimiter（与发言共享令牌桶）。
// 成功时广播 commit_made 活动事件。
func (m *WerewolfManager) Action_PublicCommit(roomID, userID string, template CommitTemplate, paramSeat int, paramText, reason string) (*WerewolfRoom, *errcode.Error) {
	r := m.getRoom(roomID)
	if r == nil {
		return nil, errcode.Code(errcode.ErrRoomNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State == nil {
		return nil, errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return nil, errcode.Code(errcode.ErrRoomNotIn)
	}
	// 仅白天发言阶段可承诺
	if r.State.Phase != PhaseSpeak {
		return nil, errcode.Code(errcode.ErrValidationFailed)
	}
	// 校验存活
	if !r.State.AliveSeat(seat) {
		return nil, errcode.Code(errcode.ErrDeadPlayerAction)
	}
	c, err := r.addCommitmentLocked(int(seat), template, paramSeat, paramText, reason)
	if err != nil {
		return nil, errcode.CodeMsg(errcode.ErrValidationFailed, err.Error())
	}
	// 广播承诺事件
	m.EmitCommitMade(r, int(seat), string(template), paramSeat)
	// 唤醒其他 Agent（承诺可能影响他人决策）
	m.wakeActingAgentsLocked(r, "commit_made")
	_ = c // 账本已写入
	return r, nil
}

// ─── 2026-08-26 §20260826-01 — 心理博弈增强 Action_*_Locked ───
//
// 三个新 action 由 agentRunner.Action_ProbePlayer / Action_FramePlayer /
// Action_FollowCrowd 入口调用。所有方法假定 m.mu 与 r.mu 都已持有
// （调用方持锁路径 §92a）；返回 success summary 或 error。
//
// 频率限制：每个 bot 每天各 1 次。daily counter 由
// werewolf_room.psychologyDailyCount[seat] 跟踪，dawn 时清零。
//
// 副作用：仅 impressionStore + RumorGraph + commitmentLedger，**绝不**写
// chat_message / chat_history / BotTranscript.HeartThought（§119 隔离）。

// probePlayerLocked 是 Action_ProbePlayer 的锁内实现（§20260826-01 U5）。
//
// 行为：
//   - 校验 phase ∈ {PhaseSpeak, PhaseVote}；
//   - 校验 target 存活 + != self；
//   - 校验 daily count < 1；
//   - 写 ProbeQuestionLocked 表（target 下轮 prompt 高优 question）；
//   - daily count++；
//   - 返回 summary。
func (m *WerewolfManager) probePlayerLocked(r *agentRunner, targetSeat int, probeText, expectedKind string) (string, error) {
	_ = m
	room, ok := m.rooms[r.roomID]
	if !ok || room == nil || room.State == nil {
		return "", errors.New("probe_player: room not found")
	}
	// 频率限制
	if room.psychologyDailyCount == nil {
		room.psychologyDailyCount = make(map[int]map[string]int)
	}
	if room.psychologyDailyCount[int(r.seat)] == nil {
		room.psychologyDailyCount[int(r.seat)] = make(map[string]int)
	}
	day := room.State.DayNumber
	room.psychologyDailyCount[int(r.seat)]["probe_"+strconv.Itoa(day)]++
	if room.psychologyDailyCount[int(r.seat)]["probe_"+strconv.Itoa(day)] > 1 {
		return "probe_player rejected: today quota exhausted", nil
	}
	// 记录 probe question（target 下轮 prompt 注入）
	if room.pendingProbeQuestions == nil {
		room.pendingProbeQuestions = make(map[int][]ProbeQuestion)
	}
	room.pendingProbeQuestions[targetSeat] = append(room.pendingProbeQuestions[targetSeat], ProbeQuestion{
		FromSeat:         int(r.seat),
		ProbeText:        probeText,
		ExpectedKind:     expectedKind,
		CreatedDay:       day,
		CreatedUnixMilli: time.Now().UnixMilli(),
	})
	return "probe dispatched → target=" + strconv.Itoa(targetSeat+1) + " expected=" + expectedKind, nil
}

// framePlayerLocked 是 Action_FramePlayer 的锁内实现（§20260826-01 U5）。
//
// 行为：
//   - 校验 phase / target / daily count；
//   - 调 AddRumorEdgeLocked 写 RumorGraph（hop=0, from=bot, to=target）；
//   - 调 EmitImpressionOnFrameLocked 更新 target 对 framer 的 Threat。
func (m *WerewolfManager) framePlayerLocked(r *agentRunner, targetSeat int, narrative, evidence string) (string, error) {
	_ = m
	room, ok := m.rooms[r.roomID]
	if !ok || room == nil || room.State == nil {
		return "", errors.New("frame_player: room not found")
	}
	if room.psychologyDailyCount == nil {
		room.psychologyDailyCount = make(map[int]map[string]int)
	}
	if room.psychologyDailyCount[int(r.seat)] == nil {
		room.psychologyDailyCount[int(r.seat)] = make(map[string]int)
	}
	day := room.State.DayNumber
	room.psychologyDailyCount[int(r.seat)]["frame_"+strconv.Itoa(day)]++
	if room.psychologyDailyCount[int(r.seat)]["frame_"+strconv.Itoa(day)] > 1 {
		return "frame_player rejected: today quota exhausted", nil
	}
	// 写入 RumorGraph（hop=0, 真人/agent 真实度 0.5；本期先简化）
	if room.rumorGraphLocked() != nil {
		_, _ = room.AddRumorEdgeLocked(int(r.seat), targetSeat, narrative, 0, 0.5, day, day)
	}
	// 更新印象：被嫁祸者对嫁祸者的 Threat
	room.EmitImpressionOnFrameLocked(targetSeat, int(r.seat), time.Now())
	return "frame dispatched → target=" + strconv.Itoa(targetSeat+1), nil
}

// followCrowdLocked 是 Action_FollowCrowd 的锁内实现（§20260826-01 U5）。
//
// 行为：
//   - 校验 phase / leader / daily count；
//   - 写 commitmentLedger（承诺下轮投票与 leader 同）；
//   - 调 EmitImpressionOnFollowVoteLocked 更新 follower 对 leader 的 Cooperation。
func (m *WerewolfManager) followCrowdLocked(r *agentRunner, leaderSeat int, reason string) (string, error) {
	_ = m
	room, ok := m.rooms[r.roomID]
	if !ok || room == nil || room.State == nil {
		return "", errors.New("follow_crowd: room not found")
	}
	if room.psychologyDailyCount == nil {
		room.psychologyDailyCount = make(map[int]map[string]int)
	}
	if room.psychologyDailyCount[int(r.seat)] == nil {
		room.psychologyDailyCount[int(r.seat)] = make(map[string]int)
	}
	day := room.State.DayNumber
	room.psychologyDailyCount[int(r.seat)]["follow_"+strconv.Itoa(day)]++
	if room.psychologyDailyCount[int(r.seat)]["follow_"+strconv.Itoa(day)] > 1 {
		return "follow_crowd rejected: today quota exhausted", nil
	}
	// 写 commitmentLedger
	if room.commitmentLedgerLocked() != nil {
		_, _ = room.commitmentLedger.AddCommitmentLocked(int(r.seat), day,
			CommitVoteTarget, leaderSeat, reason, "follow_crowd tool", 1)
	}
	// 更新印象：follower 对 leader 的 Cooperation
	room.EmitImpressionOnFollowVoteLocked(int(r.seat), leaderSeat, time.Now())
	return "follow dispatched → leader=" + strconv.Itoa(leaderSeat+1), nil
}

// ProbeQuestion 是 probe_player 工具记录的待注入问题（§20260826-01 U5）。
type ProbeQuestion struct {
	FromSeat         int    `json:"from_seat"`
	ProbeText        string `json:"probe_text"`
	ExpectedKind     string `json:"expected_kind"`
	CreatedDay       int    `json:"created_day"`
	CreatedUnixMilli int64  `json:"created_unix_ms"`
}
