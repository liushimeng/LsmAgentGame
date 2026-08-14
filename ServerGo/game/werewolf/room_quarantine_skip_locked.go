// Package werewolf - room_quarantine_skip_locked.go: lock-held variants of the
// public Action_* methods used by dispatchQuarantinedSkipLocked.
//
// BUG-WEREWOLF-P0-NEW-42 (Round 37): the dispatchQuarantinedSkipLocked cases
// (wolf_kill / seer_check / witch_act_skip / vote_skip / sheriff_elect /
// start_day) previously routed through the public Action_* methods, each of
// which does `r.mu.Lock()`. But dispatchQuarantinedSkipLocked is ALWAYS called
// with r.mu already held (callers wakeActingAgentsLocked / wakeAllAgentsLocked
// / notifyQuarantine all acquire r.mu before invoking
// tryDispatchQuarantinedActingSkip). Go's sync.Mutex is NOT reentrant, so the
// re-Lock self-deadlocked the goroutine - and since it held r.mu, the whole
// room froze. R37 hit this on the speak->vote transition: the last speaker's
// agent-side finish_speak (Action_FinishSpeak, holding r.mu) ->
// wakeActingAgentsLocked -> quarantined seat 3 (MyTurn in PhaseVote) ->
// dispatchQuarantinedSkipLocked("vote_skip") -> Action_DayVote -> r.mu.Lock()
// => permanent deadlock, no further log lines for the room.
//
// Fix: lock-held *Locked variants of every Action_* used by
// dispatchQuarantinedSkipLocked, mirroring the pre-existing finishSpeakLocked
// (which lives in room.go next to Action_FinishSpeak). They skip the r.mu.Lock
// (caller holds it), run the same validation + engine mutation, and wake the
// next acting seat via wakeActingAgentsLocked so the notifyQuarantine path
// (which has no outer wake loop) still advances the chain.
package werewolf

import (
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
	"unicode/utf8"
)

// autoVoteStuckWolvesLocked scans all living wolves that haven't voted and
// auto-marks them as abstained (target=NoSeat, WolfVoteCast=true) if their
// agent is permanently broken (quarantined or has crossed the
// permanentQuarantineThreshold). Returns true if after this scan
// allWolvesVoted() holds — caller should then force-tally immediately
// instead of waiting another watchdog tick.
//
// BUG-R228-P0 (2026-08-01): in the 13-AI full-room with one wolf bot
// permanently broken (e.g. MinMax-model in model_400_circuit), the watchdog
// fires wolf_kill skip for the stuck wolf, but other wolves that ALSO happen
// to be broken get wake events they can never answer. allWolvesVoted()
// stays false, the phase stalls until the next 120s tick — adding 4-8 min
// per stuck night. This helper shortens that to "the same watchdog tick":
// once the primary stuck wolf's vote is cast, scan the rest, decide if all
// remaining non-voters are unfixable, and tally on the spot.
//
// Caller must hold r.mu.
func (r *WerewolfRoom) autoVoteStuckWolvesLocked(actingSeat Seat) bool {
	if r.State == nil || r.State.Phase != PhaseNightWolves {
		return false
	}
	// Threshold for "permanently broken": same constant used in agent package
	// to mark a bot as quarantine-worthy on permanent errors. We import it
	// indirectly via agent.PermanentQuarantineThreshold to keep the two
	// sites in sync; if the constant moves, the import path is the only edit.
	const permThreshold = 6 // permanentQuarantineThreshold in agent/run.go
	autoMarked := 0
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) == actingSeat {
			continue
		}
		if !r.State.AliveSeat(Seat(i)) || r.State.Roles[i] != RoleWerewolf {
			continue
		}
		if r.State.WolfVoteCast[i] {
			continue
		}
		// Determine if this wolf is permanently broken.
		bot := r.BotAgents[i]
		if bot == nil {
			continue
		}
		if bot.IsQuarantined() {
			r.State.WolfVoteCast[i] = true
			r.State.WolfVotes[i] = NoSeat
			autoMarked++
			logger.L().Info("werewolf: auto-vote stuck wolf as abstained (quarantined)",
				zap.String("room_id", r.RoomID),
				zap.Int("seat", i),
				zap.String("model", bot.ModelKey))
			continue
		}
		if bot.ConsecutiveFailures() >= permThreshold {
			r.State.WolfVoteCast[i] = true
			r.State.WolfVotes[i] = NoSeat
			autoMarked++
			logger.L().Warn("werewolf: auto-vote stuck wolf as abstained (>= permanent threshold)",
				zap.String("room_id", r.RoomID),
				zap.Int("seat", i),
				zap.String("model", bot.ModelKey),
				zap.Int("consecutive_failures", bot.ConsecutiveFailures()))
		}
	}
	if autoMarked == 0 {
		return false
	}
	return r.State.allWolvesVoted()
}

// wolfKillLocked is the lock-held variant of Action_WolfKill. Caller must hold
// r.mu. BUG-WEREWOLF-P0-NEW-42.
// 2026-07-17: 适配投票语义(与 Action_WolfKill 同步)。
// 2026-08-01 BUG-R225-P1-01: 同样在弃权(target==NoSeat)时复位 WolfVoteCast,
// 与 Action_WolfKill 行为对称,manager/agent 双路径共用同一真相源。
func (m *WerewolfManager) wolfKillLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil || r.State.Phase != PhaseNightWolves {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if r.State.Roles[seat] != RoleWerewolf {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	// 2026-08-01 BUG-R225-P1-01: 与 Action_WolfKill 对称的弃权复位,
	// 见 room_action.go 注释。本块同时替换了原
	// dispatchQuarantinedSkipLocked(room_agent.go) 里的复位置位,使
	// manager/watchdog 与 agent 自救两条路径走完全相同的入口语义。
	if target == NoSeat && r.State.WolfVoteCast[seat] {
		logger.L().Warn("werewolf: wolf_kill skip — resetting prior vote (wolfKillLocked)",
			zap.String("room_id", r.RoomID),
			zap.Int("seat", int(seat)))
		r.State.WolfVoteCast[seat] = false
		r.State.WolfVotes[seat] = NoSeat
		r.State.WolfVoteReasons[seat] = "" // §20260810-04 U2: 弃权同步清理由
	}
	// §20260810-04 U2 — quarantine/watchdog 兜底路径无 LLM 理由,传 ""。
	if e := r.State.NightWolfKill(seat, target, ""); e != nil {
		return e
	}
	// 2026-07-17: 若已计票(Phase 已推进),广播最终击杀结果。
	// 投票快照通过 BuildClientState.WolfVoteView 随 game.state 广播(无需额外帧)。
	if r.State.Phase != PhaseNightWolves {
		m.EmitWolfKill(r, int(r.State.WolfKillTarget))
		m.wakeActingAgentsLocked(r, "wolf_vote_tally")
		return nil
	}
	// BUG-R228-P0 (2026-08-01): 当 watchdog 路径给 stuck 狼投票后,
	// 仍有"其余存活狼均 quarantined / circuit-open"的可能 —— 它们即使被
	// wake 也不会响应 LLM 调用,等下一次 watchdog tick 才 force-tally,
	// 而下一次 tick 又要 120s 才到,本局每个夜晚因此积压 ~4-8 分钟(R228
	// 13-AI 全 AI 房间实测:MinMax-model wolf 永久 stuck,Night 1 救援
	// 用时 4m29s,Night 2 用时 8m)。在 watchdog skip 路径的 wolf_kill
	// 已成功落票后,若 allWolvesVoted() 仍未满足但其它狼都已经永久坏掉,
	// 把它们一并标记为弃权(target=NoSeat)立即计票推进,而不是等下一次
	// watchdog tick。
	allQuarantinedWolvesCanTally := r.autoVoteStuckWolvesLocked(seat)
	if allQuarantinedWolvesCanTally {
		r.State.tallyWolfVotes()
		r.State.endWolfPhase()
		m.EmitWolfKill(r, int(r.State.WolfKillTarget))
		m.wakeActingAgentsLocked(r, "wolf_vote_tally")
		return nil
	}
	m.wakeActingAgentsLocked(r, "wolf_vote")
	return nil
}

// seerCheckLocked is the lock-held variant of Action_SeerCheck. Caller must
// hold r.mu. BUG-WEREWOLF-P0-NEW-42.
func (m *WerewolfManager) seerCheckLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.NightSeerCheck(seat, target); e != nil {
		return e
	}
	// §115 房间聊天 — 活动事件广播
	if target >= 0 && target < MaxPlayers {
		m.EmitSeerCheck(r, int(target))
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// witchLocked is the lock-held variant of Action_Witch. Caller must hold r.mu.
// BUG-WEREWOLF-P0-NEW-42.
func (m *WerewolfManager) witchLocked(r *WerewolfRoom, userID, action string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.NightWitchAct(seat, action, target); e != nil {
		return e
	}
	// §115 房间聊天 — 活动事件广播
	m.EmitWitchAct(r, action)
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// guardProtectLocked §134 守卫守护的 lock-held 变体。Caller 必须持有 r.mu。
// 与 witchLocked 完全同构:SeatOf → r.State.NightGuardProtect → m.EmitGuardProtect
// → m.wakeActingAgentsLocked。target=-1(N.NoSeat)表示空守,用于
// dispatchQuarantinedSkipLocked 的 "guard_protect_skip" 分支。
//
// BUG-R9-P0-2 (2026-07-29): 守卫是"行动失败 = 死亡"的脆弱角色 —— R9 实测
// seat 5 守卫(MinMax-model)LLM 400 失败 → auto-skip 空守 → 当夜被狼刀死,
// 好人直接少一个神职,触发屠边。提供 guardProtectFallbackLocked 供 watchdog
// 派发路径使用:在规则允许范围内挑一个兜底守护目标(G1 不连守、G2 不守自己、
// G3 只守存活)让护盾仍有概率挡刀;找不到合法目标才退化为空守。
// 注意:agent 侧 runner.GuardProtect(-1) → Action_GuardProtect(公开入口) →
// NightGuardProtect(-1) = 显式空守,这是 §134 设计的合法选择,不做 fallback —
// 仅"系统代打"路径(dispatchQuarantinedSkipLocked)使用 fallback。
func (m *WerewolfManager) guardProtectLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	if e := r.State.NightGuardProtect(seat, target); e != nil {
		return e
	}
	// §115 房间聊天 — 活动事件广播(target 绝不包含进活动文案,守卫盲守隐私)
	m.EmitGuardProtect(r)
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// challengeLocked §20260810-11 H2 — 白天发言阶段公开质疑(锁内变体)。
// 与 guardProtectLocked 同构:SeatOf 已在外层完成,这里只做状态校验 + 写状态 + EmitChallenge。
// §92a 双变体:外层 Action_Challenge 持 r.mu → 这里不能再次 Lock;caller 必须已持 r.mu。
//
// 校验:
//  1. 阶段必须是 PhaseSpeak(白天发言阶段)
//  2. challenger 与 target 都存活,且 != 同一座位(不自疑)
//  3. target 在合法座位范围
//  4. challenger ChallengeUsedToday == false(每人每天 1 次)
//  5. question 非空且长度 ≤ 60
//  6. 上一晚的状态(DayNumber)在有效范围
//
// 写状态:target.LastChallengedBy = challenger + target.LastChallengeQuestion = q;
// challenger.ChallengeUsedToday = true;EmitChallenge 走活动流(不调用 wake,见下)。
//
// 注意:质疑是社交动作,不触发 wake,被质疑者下一轮 speaking 时 buildAgentContextLocked
// 会注入 GameContext.LastChallenge 块,LLM 自行决定是否回应。这是 §130「仅行动者触发」
// 的反向应用 —— 被质疑者**当前不是行动者**,所以不主动 wake。
func (m *WerewolfManager) challengeLocked(r *WerewolfRoom, challenger, target Seat, question string) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhaseSpeak {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "challenge requires PhaseSpeak")
	}
	if challenger < 0 || challenger >= MaxPlayers {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "invalid challenger seat")
	}
	if target < 0 || target >= MaxPlayers {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "invalid target seat")
	}
	if challenger == target {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot challenge self")
	}
	if !r.State.AliveSeat(challenger) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "challenger is dead")
	}
	if !r.State.AliveSeat(target) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "target is dead")
	}
	if question == "" {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "empty question")
	}
	// 限长 60 字(§121 严格校验,UI 同步)。
	if utf8.RuneCountInString(question) > 60 {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "question too long (max 60)")
	}
	if r.State.Players[challenger].ChallengeUsedToday {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "challenge already used today")
	}
	// 写状态。
	r.State.Players[challenger].ChallengeUsedToday = true
	r.State.Players[target].LastChallengedBy = int(challenger)
	r.State.Players[target].LastChallengeQuestion = question
	// 活动广播(§119:question 公开,仅走活动流,不入 chat_message 表)。
	m.EmitChallenge(r, int(challenger), int(target), question)
	return nil
}

// guardProtectFallbackLocked R9-P0-2: watchdog / quarantine skip 代打路径专用。
// 在 G1/G2/G3 约束下挑一个合法目标;找不到时退化为空守。Caller 必须持有 r.mu。
func (m *WerewolfManager) guardProtectFallbackLocked(r *WerewolfRoom, userID string) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	fallback := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		cand := Seat(i)
		if cand == seat { // G2 不能守自己
			continue
		}
		if !r.State.AliveSeat(cand) { // G3 只能守存活
			continue
		}
		if cand == r.State.GuardLastProtect { // G1 不能连守
			continue
		}
		fallback = cand
		break
	}
	if fallback != NoSeat {
		logger.L().Info("werewolf: guard protect fallback — guarding first legal target instead of empty",
			zap.String("room_id", r.RoomID),
			zap.Int("guard_seat", int(seat)),
			zap.Int("fallback_target", int(fallback)))
	}
	return m.guardProtectLocked(r, userID, fallback)
}

// knightDuelLocked §198 骑士决斗的 lock-held 变体 (§92a 双变体约束)。
// 与 guardProtectLocked 完全同构:SeatOf → r.State.KnightDuel →
// m.EmitKnightDuel → m.wakeActingAgentsLocked。
//
// §198 重要决策: 骑士决斗**不**有 `_skip` 派发路径 —— 白天发言阶段
// knight_bot LLM 沉默时,PhaseSpeak 自然按 SpeakTurnSeat 推进,
// LLM 在发言轮(speak/finish_speak)中正常表达,knight_duel 工具是
// 嵌入发言的"可选附加动作",不是阻断性技能。故
// dispatchQuarantinedSkipLocked 在 "PhaseSpeak" + "knight" 路径
// 默认走 finish_speak(详见 run.go SkipPhaseAction "speak" → "finish_speak")
// —— 沉默 bot 自动跳过发言,技能以"未使用"形态自然收尾,不需强制发动。
func (m *WerewolfManager) knightDuelLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	// 提前记录是否命中狼,以便 EmitKnightDuel 文案正确拼后缀
	// (Action_KnightDuel 公开入口也做了同样的预检;两路径必须同源)。
	hitWolf := false
	if int(target) >= 0 && int(target) < MaxPlayers {
		hitWolf = (r.State.Roles[target] == RoleWerewolf)
	}
	if e := r.State.KnightDuel(seat, target); e != nil {
		return e
	}
	// §115 房间聊天 — 活动流广播(公开 actor 与 target;命中/未命中文案分流)
	m.EmitKnightDuel(r, int(seat), int(target), hitWolf)
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// demonHunterHuntLocked §猎魔人 夜间狩猎的 lock-held 变体(§92a 双变体约束)。
// 与 guardProtectLocked 完全同构:SeatOf → r.State.NightDemonHunterHunt →
// m.EmitDemonHunterHunt → m.wakeActingAgentsLocked。
//
// §猎魔人 重要决策: 猎魔人狩猎**不**有 `_skip` 派发路径 —— 夜间狩猎阶段
// LLM 沉默时,PhaseNightDemonHunter 自然由 watchdog 兜底(空过 = target=-1),
// 通过 WatchdogSkipQuarantinePath 进入 dispatchQuarantinedSkipLocked 调用本函数。
func (m *WerewolfManager) demonHunterHuntLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	// 提前记录是否命中狼,以便 EmitDemonHunterHunt 文案正确拼后缀。
	hitWolf := false
	if int(target) >= 0 && int(target) < MaxPlayers {
		hitWolf = (r.State.Roles[target] == RoleWerewolf)
	}
	if e := r.State.NightDemonHunterHunt(seat, target); e != nil {
		return e
	}
	// §猎魔人 活动流广播(公开 actor 与 target;命中/未命中文案分流)
	m.EmitDemonHunterHunt(r, int(seat), int(target), hitWolf)
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// dayVoteLocked is the lock-held variant of Action_DayVote. Caller must hold
// r.mu. DayVote may auto-tally (FinishVote) once every alive player has voted,
// transitioning the phase; wakeActingAgentsLocked then nudges the next phase's
// acting seat. BUG-WEREWOLF-P0-NEW-42.
//
// BUG-WEREWOLF-P0-NEW-43 (Round 38): if the quarantined voter is the host
// driver, DayVote alone leaves MyTurn=true for that seat (the vote-phase
// MyTurn formula is `!MyVoted || seat == driverSeat`) and the next
// wakeActingAgentsLocked cycle recurses through
// tryDispatchQuarantinedActingSkip -> dispatchQuarantinedSkipLocked ->
// dayVoteLocked again, ad infinitum (logged as 1427747+ frame stack
// overflow before this fix). When the driver's skip makes allAliveVoted()
// true, fall through to finishVoteLocked so the phase actually transitions
// and the recursion is broken on the next wake.
func (m *WerewolfManager) dayVoteLocked(r *WerewolfRoom, userID string, target Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	seat, ok := r.SeatOf(userID)
	if !ok {
		return errcode.Code(errcode.ErrRoomNotIn)
	}
	// BUG-R193-001: 同步 quarantine 状态到引擎,确保 DayVote auto-tally 与本函数
	// 末尾 driver-tally 分支都能正确排除被禁用的 bot。
	syncQuarantinedLocked(r)
	if e := r.State.DayVote(seat, target); e != nil {
		return e
	}
	// §115 房间聊天 — 投票动作广播
	if target >= 0 && target < MaxPlayers {
		m.EmitVoteCast(r, int(seat), int(target))
	}
	// BUG-WEREWOLF-P0-NEW-43: Break the driver self-loop. If every alive seat
	// has now voted (i.e. the driver's own skip was the deciding one),
	// auto-tally via the lock-held FinishVote variant so PhaseVote transitions
	// and wakeActingAgentsLocked sees a different acting seat.
	// BUG-R193-001: use allActiveVoted() so quarantined bots don't stall the
	// driver-tally branch either.
	if r.State.Phase == PhaseVote && r.State.allActiveVoted() {
		// 2026-08-14 §20260814-01 U1 — 票型快照必须在 FinishVote **之前**抓
		// (之后 TallyVotes 状态不可重现),历史记录必须在 FinishVote **之后**写
		// (DayEliminated 才有值)。这是三条 FinishVote 路径中唯一原先没有
		// tally 快照的一条,故此处新抓。
		autoTally := r.State.TallyVotes(false)
		if e := r.State.FinishVote(0); e != nil {
			logger.L().Warn("werewolf: driver auto-tally FinishVote failed (skip-only path)",
				zap.String("room_id", r.RoomID), zap.Int("seat", int(seat)),
				zap.String("err", e.Message))
		} else {
			r.recordVoteHistoryLocked(autoTally)
		}
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// finishVoteLocked is the lock-held variant of Action_FinishVote. Caller must
// hold r.mu. Mirrors finishSpeakLocked's relationship to Action_FinishSpeak —
// dispatchQuarantinedSkipLocked used to call Action_FinishVote directly, but
// every public Action_* re-acquires r.mu, which self-deadlocks when called
// from the lock-held dispatch path. BUG-WEREWOLF-P0-NEW-43 also uses this
// from dayVoteLocked's driver-tally branch (same lock-held caller, can't
// call Action_FinishVote there either).
func (m *WerewolfManager) finishVoteLocked(r *WerewolfRoom, tiedRound int) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	// 记录投票结果快照(FinishVote 内会推进 phase,所以要提前抓 seats)。
	prePhase := r.State.Phase
	preTally := r.State.TallyVotes(false)
	// §20260809-02 U2:投票结算时抓一份「谁投了谁」快照,供下一轮 Agent prompt。
	// 在 FinishVote 之前抓 — 之后 phase 推进但 VoteTarget 字段保留(只清 Voted 标记)。
	// 弃权者(NoSeat)不进入 Map。
	if prePhase == PhaseVote {
		m.fillDayVoteMapLocked(r)
	}
	if e := r.State.FinishVote(tiedRound); e != nil {
		return e
	}
	// 2026-08-14 §20260814-01 U1 — 逐日票型入历史(个人复盘投票准确率维度)。
	// 见 recordVoteHistoryLocked:必须在 FinishVote 之后(DayEliminated 才有值)。
	if prePhase == PhaseVote {
		r.recordVoteHistoryLocked(preTally)
	}
	// §115 房间聊天 — 投票结果广播
	if prePhase == PhaseVote {
		// 找出票数最多者(平票/无票)
		topSeat := NoSeat
		topCount := 0
		tied := false
		for seat, n := range preTally {
			switch {
			case n > topCount:
				topSeat = seat
				topCount = n
				tied = false
			case n == topCount && n > 0:
				tied = true
			}
		}
		if topSeat < 0 || topCount == 0 {
			m.EmitVoteResult(r, -1, false)
		} else if tied {
			m.EmitVoteResult(r, -1, true)
		} else {
			m.EmitVoteResult(r, int(topSeat), false)
		}
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// sheriffElectLocked is the lock-held variant of Action_SheriffElect. Caller
// must hold r.mu. SheriffElect transitions to PhaseSpeak (or a tied speak
// round); wakeActingAgentsLocked nudges the first speaker. BUG-WEREWOLF-P0-NEW-42.
func (m *WerewolfManager) sheriffElectLocked(r *WerewolfRoom) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	// §报告-20260804-03 BUG-06: NoSeat = 系统内部调用哨兵。本函数是
	// quarantine-skip / watchdog 的兜底路径,调用者不是某个具体玩家,
	// 故跳过存活入座校验(否则 driver 被隔离后阶段永久卡死)。
	if e := r.State.SheriffElect(NoSeat); e != nil {
		return e
	}
	// §115 房间聊天 — 警长选出广播
	if r.State.SheriffSeat != NoSeat {
		m.EmitSheriffElect(r, int(r.State.SheriffSeat))
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// sheriffSetSpeakOrderLocked is the lock-held variant of
// Action_SheriffSetSpeakOrder. Caller must hold r.mu. §20260810-09 警长定序权:
// 警长决定发言方向(顺/逆时针) + 自位置(首/末),引擎按其选择生成 SpeakOrder。
// §92a *Locked 双变体硬约束 — Action_SheriffSetSpeakOrder(manager 路径)与
// dispatchQuarantinedSkipLocked(watchdog 路径)都走本入口,避免 §130 「声明了
// 却从不接线」+ §92a 自死锁双重雷区。
//
// §135 公平性:本动作只影响发言顺序生成,**不**揭示身份;`RolePubliclyRevealed`
// 白名单不增加新分支。
func (m *WerewolfManager) sheriffSetSpeakOrderLocked(r *WerewolfRoom, userID, direction, selfPos string) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if r.State.Phase != PhaseSheriffOrder {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not at sheriff order phase")
	}
	if r.State.SheriffSeat == NoSeat {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "no sheriff in this game")
	}
	if !r.State.AliveSeat(r.State.SheriffSeat) {
		return errcode.CodeMsg(errcode.ErrDeadPlayerAction, "sheriff is dead")
	}
	// 校验取值白名单(§84b 严格校验)
	if direction != SheriffDirectionCW && direction != SheriffDirectionCCW {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "invalid sheriff speak direction")
	}
	if selfPos != SheriffSelfPosFirst && selfPos != SheriffSelfPosLast {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "invalid sheriff self position")
	}
	// 锁定为警长本人操作(userID == SheriffSeat 对应 userID)
	sheriffUserID := r.Seats[r.State.SheriffSeat]
	if userID != sheriffUserID {
		return errcode.CodeMsg(errcode.ErrPermissionDenied, "only sheriff may set speak order")
	}
	r.State.applySheriffOrderLocked(direction, selfPos)
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// startDayLocked is the lock-held variant of Action_StartDay. Caller must hold
// r.mu. StartDay transitions dawn -> sheriff/speak; wakeActingAgentsLocked
// nudges the driver / first speaker. BUG-WEREWOLF-P0-NEW-42.
func (m *WerewolfManager) startDayLocked(r *WerewolfRoom) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	// §20260811-06 U5 — 黎明流言系统在 startDay 完成后立即广播。
	// §130 接线验证:本调用点是 EmitDayRumorsLocked 的唯一生产注入点
	// (resumeAfterHunterShoot 走 gs.StartDay 也会触发 — 同一路径)。
	m.emitDayRumorsLocked(r)
	if e := r.State.StartDay(); e != nil {
		return e
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// sayLastWordsLocked 是 last_words 工具的 lock-held 派发。调用方必须持有 r.mu。
// BUG 2026-07-09: 遗言 actor 提交遗言 → 引擎推进队列 → 广播遗言内容 → 活动事件。
// 队列清空后 EndDeathLyricRound 自动调 onDone 恢复原路径(start_day / hunter / advanceDay)。
func (m *WerewolfManager) sayLastWordsLocked(r *WerewolfRoom, seat Seat, text string) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if e := r.State.SayLastWords(seat, text); e != nil {
		return e
	}
	// §115 房间聊天 — 遗言广播(走 chat.send 路径,前端标记 💀 遗言)
	m.EmitDeathLyricSpoken(r, int(seat), text)
	// 遗言计入 speak floor,避免 floor watchdog 在遗言阶段误 wake
	if ag := r.BotAgents[int(seat)]; ag != nil {
		ag.NoteIfSpeaking()
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}

// skipLastWordsLocked 是 last_words_skip 工具的 lock-held 派发。调用方必须持有 r.mu。
// BUG 2026-07-09: 遗言 actor 放弃遗言 → 引擎推进队列 → 活动事件。
func (m *WerewolfManager) skipLastWordsLocked(r *WerewolfRoom, seat Seat) *errcode.Error {
	if r.State == nil {
		return errcode.Code(errcode.ErrGameNotStarted)
	}
	if e := r.State.SkipLastWords(seat); e != nil {
		return e
	}
	m.EmitDeathLyricSkipped(r, int(seat))
	if ag := r.BotAgents[int(seat)]; ag != nil {
		ag.NoteIfSpeaking()
	}
	m.wakeActingAgentsLocked(r, "state_change")
	return nil
}
