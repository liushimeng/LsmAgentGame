package werewolf

import (
	"time"

	"LsmAgentGame/errcode"
)

func (gs *GameState) StartDay() *errcode.Error {
	if gs.Phase != PhaseDawn {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not at dawn")
	}
	// DayNumber 在夜晚递增;首夜结束后 DayNumber=1,第一天后 DayNumber=2...
	// 第一晚 startNight 已设为 1;这里首次从 PhaseDawn 跳到白天应递增到 1 还是保留 1?
	// 设定:DayNumber = 当前"白天轮数";首夜后第一天的 DayNumber = 1。
	// → 故 startNight 时 day=1 → StartDay 仍 1;之后 startNight 递增。
	if gs.DayNumber == 0 {
		gs.DayNumber = 1
	}
	// 第一天白天且还没警长 → 先警长竞选
	if gs.SheriffSeat == NoSeat && gs.DayNumber == 1 {
		setPhaseAndDeadline(gs, PhaseSheriff)
		gs.TurnActingSeat = NoSeat
		// §报告-20260804-03 BUG-03: 警长竞选首轮**没有**轮流发言顺序 ——
		// 所有存活玩家同时举手参选 + 同时投票(见 view.go fillMyTurnExtra
		// 的 PhaseSheriff 分支: myTurn = !HasSpoken)。SpeakTurnSeat 必须显式
		// 保持 NoSeat,前端 sheriff 面板据此**不渲染**「当前发言:#N」。
		// (此前依赖隐式初值,前端裸 `speak_turn_seat + 1` 渲染出不存在的 #0。)
		gs.SpeakTurnSeat = NoSeat
		return nil
	}
	// §20260810-09 — 警长定序阶段。仅当**警长存活**时启用;警长已死或无警长
	// 走回退默认顺序(等同于§130 既有接线不被破坏)。该阶段
	// watchdog 在 30s 内未收到 sheriff_set_speak_order 也会按"顺时针 + 警长
	// 首发言"兜底进入 PhaseSpeak,见 §97 五处同步中的 dispatchQuarantinedSkipLocked。
	if gs.SheriffSeat != NoSeat && gs.AliveSeat(gs.SheriffSeat) {
		setPhaseAndDeadline(gs, PhaseSheriffOrder)
		gs.TurnActingSeat = gs.SheriffSeat
		gs.SpeakTurnSeat = NoSeat
		// 重置本局定序状态(若上局警长定过序,本局清零;每次白天独立生效)
		gs.SheriffOrderSet = false
		gs.SheriffSpeakDirection = ""
		gs.SheriffSpeakSelfPos = ""
		return nil
	}
	gs.startSpeakPhase()
	// §20260811-06 U5 — 黎明流言系统在 speak 阶段开始前广播 1-2 条流言。
	// 复用 emitActivity 链路;§130 接线验证:本调用点 + resumeAfterHunterShoot(from=wolf) 是
	// 唯一两个 emitDayRumorsLocked 入口(都在 startDay 完成后)。
	return nil
}

// sheriffSpeakDirectionEnum 校验 direction 取值。
const (
	SheriffDirectionCW  = "cw"  // 顺时针
	SheriffDirectionCCW = "ccw" // 逆时针

	SheriffSelfPosFirst = "first" // 警长先发言
	SheriffSelfPosLast  = "last"  // 警长后发言

	// SheriffOrderDefaultDirection/Pos 是 §97 watchdog 兜底默认值。
	SheriffOrderDefaultDirection = SheriffDirectionCW
	SheriffOrderDefaultSelfPos   = SheriffSelfPosFirst
)

// applySheriffOrderLocked 由 Action_SheriffSetSpeakOrder(manager 路径,持锁态)
// 与 dispatchQuarantinedSkipLocked(watchdog 路径)共用。direction/pos 已校验,
// 直接写入状态并切到 PhaseSpeak。§92a *Locked 双变体 —— 调用方必须持 r.mu。
func (gs *GameState) applySheriffOrderLocked(direction, selfPos string) {
	gs.SheriffOrderSet = true
	gs.SheriffSpeakDirection = direction
	gs.SheriffSpeakSelfPos = selfPos
	gs.TurnActingSeat = NoSeat
	gs.startSpeakPhaseWithOrder()
}

// startSpeakPhase 生成发言顺序的主入口。优先按 §20260810-09 警长定序配置;
// 无警长 / 警长已死 / 未定序时回退到 §130 既有接线(座位升序)。
func (gs *GameState) startSpeakPhase() {
	// §20260810-09 兼容:StartDay 已在 SheriffOrderSet=true 时调 startSpeakPhaseWithOrder;
	// 此函数本身保持 §130 既有行为不变 —— 警长定序状态在 StartDay 阶段已被读走。
	gs.startSpeakPhaseWithOrder()
}

// startSpeakPhaseWithOrder 实际生成发言顺序的函数。考虑警长定序状态。
func (gs *GameState) startSpeakPhaseWithOrder() {
	alive := make([]Seat, 0, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) {
			alive = append(alive, Seat(i))
		}
	}
	if len(alive) == 0 {
		setPhaseAndDeadline(gs, PhaseGameOver)
		gs.Status = "over"
		gs.Winner = "good"
		return
	}
	// 默认顺序:座位升序
	gs.SpeakOrder = alive
	// §20260810-09 警长定序生效时,按 direction 翻转 + 按 selfPos 移动警长到首/末
	if gs.SheriffOrderSet && gs.SheriffSeat != NoSeat && gs.AliveSeat(gs.SheriffSeat) {
		if gs.SheriffSpeakDirection == SheriffDirectionCCW {
			// 逆时针:反转顺序
			for i, j := 0, len(gs.SpeakOrder)-1; i < j; i, j = i+1, j-1 {
				gs.SpeakOrder[i], gs.SpeakOrder[j] = gs.SpeakOrder[j], gs.SpeakOrder[i]
			}
		}
		// 警长先发言:把警长座位从当前位置移到首位
		// 警长后发言:把警长座位从当前位置移到末尾
		// 顺序:先做 direction 翻转,再做 selfPos 移动。
		for i, s := range gs.SpeakOrder {
			if s != gs.SheriffSeat {
				continue
			}
			if gs.SheriffSpeakSelfPos == SheriffSelfPosFirst {
				gs.SpeakOrder = append([]Seat{s}, append(gs.SpeakOrder[:i], gs.SpeakOrder[i+1:]...)...)
			} else { // SheriffSelfPosLast
				gs.SpeakOrder = append(append(gs.SpeakOrder[:i], gs.SpeakOrder[i+1:]...), s)
			}
			break
		}
	}
	gs.SpeakTurnSeat = gs.SpeakOrder[0]
	setPhaseAndDeadline(gs, PhaseSpeak)
	gs.TurnActingSeat = NoSeat
}

func (gs *GameState) NextSpeaker() Seat {
	if gs.Phase != PhaseSpeak {
		return NoSeat
	}
	idx := -1
	for i, s := range gs.SpeakOrder {
		if s == gs.SpeakTurnSeat {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(gs.SpeakOrder) {
		setPhaseAndDeadline(gs, PhaseVote)
		gs.SpeakTurnSeat = NoSeat
		return NoSeat
	}
	gs.SpeakTurnSeat = gs.SpeakOrder[idx+1]
	return gs.SpeakTurnSeat
}

func (gs *GameState) FinishSpeak(actor Seat) *errcode.Error {
	if gs.Phase != PhaseSpeak {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not speaking phase")
	}
	if actor != gs.SpeakTurnSeat {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not current speaker")
	}
	gs.Players[actor].HasSpoken = true
	gs.NextSpeaker()
	return nil
}

func (gs *GameState) ProposeVote(actor Seat) *errcode.Error {
	if gs.Phase != PhaseSpeak {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not speaking phase")
	}
	if !gs.AliveSeat(actor) {
		// R176 报告 P1: 使用 ErrDeadPlayerAction (40112) 专属 code,前端可据此
		// 明确提示玩家「已死亡,不能发起投票」(而非通用 validation failed)。
		return errcode.CodeMsg(errcode.ErrDeadPlayerAction, "dead player cannot propose vote")
	}
	if gs.Roles[actor] != RoleSeer {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "only seer can propose vote")
	}
	if gs.VoteProposed {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "vote already proposed")
	}
	gs.VoteProposed = true
	gs.VoteProposer = actor
	// 直接进入投票阶段
	setPhaseAndDeadline(gs, PhaseVote)
	gs.SpeakTurnSeat = NoSeat
	return nil
}

func (gs *GameState) DayVote(actor Seat, target Seat) *errcode.Error {
	if gs.Phase != PhaseVote && gs.Phase != PhaseSheriff {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not vote phase")
	}
	if !gs.AliveSeat(actor) {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	if target == NoSeat {
		gs.Players[actor].Voted = true
		gs.Players[actor].VoteTarget = NoSeat
	} else {
		if target < 0 || target >= MaxPlayers {
			return errcode.Code(errcode.ErrValidationFailed)
		}
		if gs.Phase == PhaseVote {
			if !gs.AliveSeat(target) {
				return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
			}
			if target == actor {
				return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot vote self")
			}
			if gs.Roles[target] == RoleIdiot && gs.Players[target].IdiotRevealed { // 翻牌后失去被投票权
				return errcode.CodeMsg(errcode.ErrValidationFailed, "idiot already revealed, cannot be voted")
			}
		}
		gs.Players[actor].Voted = true
		gs.Players[actor].VoteTarget = target
	}
	// BUG-WEREWOLF-P0-4: auto-tally the vote once every alive player has
	// voted, so full-AI rooms with no human GM still progress. We only do
	// this in PhaseVote — PhaseSheriff keeps requiring an explicit
	// SheriffElect call (which actually selects the sheriff), matching the
	// existing TestSheriffVoteTieFirstRound contract.
	// BUG-R193-001: use allActiveVoted() so quarantined (LLM-broken) bots do
	// not block the auto-tally — they are alive but can never set Voted=true.
	if gs.Phase == PhaseVote && gs.allActiveVoted() {
		_ = gs.FinishVote(0)
	}
	return nil
}

func (gs *GameState) allAliveVoted() bool {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && !gs.Players[i].Voted {
			return false
		}
	}
	return true
}

func (gs *GameState) allActiveVoted() bool {
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) || gs.QuarantinedSeats[i] {
			continue
		}
		if !gs.Players[i].Voted {
			return false
		}
	}
	return true
}

func (gs *GameState) TallyVotes(sheriffMode bool) map[Seat]int {
	tally := make(map[Seat]int, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) || !gs.Players[i].Voted {
			continue
		}
		if gs.Roles[i] == RoleIdiot && gs.Players[i].IdiotRevealed { // 翻牌后失去投票权
			continue
		}
		t := gs.Players[i].VoteTarget
		if t == NoSeat {
			continue
		}
		weight := 1
		if !sheriffMode && Seat(i) == gs.SheriffSeat {
			weight = 3 // 1.5 → 3;只影响整数比值比较,不影响比例
		}
		tally[t] += weight
	}
	return tally
}

func (gs *GameState) FinishVote(tiedRound int) *errcode.Error {
	if gs.Phase != PhaseVote {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not vote phase")
	}
	tally := gs.TallyVotes(false)
	maxVal := -1
	var leaders []Seat
	for s, c := range tally {
		if c > maxVal {
			maxVal = c
			leaders = []Seat{s}
		} else if c == maxVal {
			leaders = append(leaders, s)
		}
	}
	if len(leaders) == 0 {
		// 没人投票 → 当天没人出局
		gs.DayEliminated = NoSeat
		gs.DayTiedPlayers = nil
		gs.advanceDay()
		return nil
	}
	if len(leaders) == 1 {
		target := leaders[0]
		gs.DayEliminated = target
		gs.DayTiedPlayers = nil
		// 最高票为存活未翻牌白痴 → 进入翻牌结算,不直接放逐。
		if gs.Roles[target] == RoleIdiot && gs.AliveSeat(target) && !gs.Players[target].IdiotRevealed {
			setPhaseAndDeadline(gs, PhaseIdiotReveal)
			gs.TurnActingSeat = target
			return nil
		}
		gs.Players[target].LastWords = gs.DayNumber <= LastWordsRounds
		if err := gs.killPlayer(target, "vote"); err != nil {
			return errcode.CodeMsg(errcode.ErrValidationFailed, err.Message)
		}
		gs.checkWinner()
		if gs.Status == "over" {
			return nil
		}
		// BUG 2026-07-09: 放逐后,先遗言(若 LastWords=true),再走猎人开枪 / 进下一夜。
		gs.tryEnterDeathLyricRound([]Seat{target}, func() *errcode.Error {
			if gs.Roles[target] == RoleHunter {
				gs.HunterPendingShoot = true
				gs.HunterPendingFrom = "vote"
				setPhaseAndDeadline(gs, PhaseHunterShoot)
				// BUG-R10-P0-3: 把 TurnActingSeat 设为猎人座位,与
				// engine_night.go 夜间入口保持一致(详见
				// hunterSeatForPhaseLocked 注释)。
				gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
				return nil
			}
			gs.advanceDay()
			return nil
		})
		return nil
	}
	// 平票
	if tiedRound == 1 {
		gs.DayTiedPlayers = leaders
		gs.SpeakOrder = append([]Seat{}, leaders...)
		gs.SpeakTurnSeat = leaders[0]
		setPhaseAndDeadline(gs, PhaseSpeak)
		for i := range gs.Players {
			gs.Players[i].Voted = false
			gs.Players[i].VoteTarget = NoSeat
			gs.Players[i].HasSpoken = false
			// §20260810-11 H2 — 每日重置 challenge 状态。
			gs.Players[i].ChallengeUsedToday = false
			gs.Players[i].LastChallengedBy = -1
			gs.Players[i].LastChallengeQuestion = ""
		}
		return nil
	}
	gs.DayTiedPlayers = nil
	gs.DayEliminated = NoSeat
	gs.advanceDay()
	return nil
}

func (gs *GameState) advanceDay() {
	if gs.HunterPendingShoot {
		setPhaseAndDeadline(gs, PhaseHunterShoot)
		// BUG-R10-P0-3: 把 TurnActingSeat 设为猎人座位(详见
		// hunterSeatForPhaseLocked 注释)。
		gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
		return
	}
	if gs.Status == "over" {
		return
	}
	gs.DayNumber++
	gs.startNight()
}

// resumeAfterHunterShoot §135 猎人开枪结算完毕后的阶段恢复。
//
// 猎人开枪有两个触发来源,恢复路径**不同**:
//   - from == "wolf"(夜间被狼刀,开枪发生在黎明遗言之后)→ 回到 dawn 并 StartDay,
//     继续本白天流程;若走 advanceDay 会直接跳进下一夜,整个白天被吞掉。
//   - from == "vote"(白天被放逐)→ 走 advanceDay 进入下一夜(原有语义)。
func (gs *GameState) resumeAfterHunterShoot(from string) *errcode.Error {
	if gs.Status == "over" {
		return nil
	}
	if from == "wolf" {
		setPhaseAndDeadline(gs, PhaseDawn)
		gs.TurnActingSeat = NoSeat
		return gs.StartDay()
	}
	gs.advanceDay()
	return nil
}

func (gs *GameState) WolfSuicide(actor Seat) *errcode.Error {
	if gs.Phase != PhaseSpeak {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "suicide only in speak phase")
	}
	if !gs.AliveSeat(actor) || gs.Roles[actor] != RoleWerewolf {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	gs.SuicidedWolfSeat = actor
	gs.Players[actor].LastWords = false
	if err := gs.killPlayer(actor, "suicide"); err != nil {
		return errcode.CodeMsg(errcode.ErrValidationFailed, err.Message)
	}
	if gs.checkWinner() {
		return nil
	}
	gs.startNight()
	return nil
}

func (gs *GameState) HunterShoot(actor Seat, target Seat) *errcode.Error {
	if !gs.HunterPendingShoot {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "hunter not pending")
	}
	if gs.Roles[actor] != RoleHunter {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	if gs.HunterPendingFrom == "poison" {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "hunter cannot shoot after poison")
	}
	if target == NoSeat {
		// §135 主动选择"不开枪" → **未亮身份**,HunterFired 保持 false,
		// 身份继续隐藏。这给猎人一个真实的战术选择(藏身份 vs 带人)。
		from := gs.HunterPendingFrom
		gs.HunterPendingShoot = false
		gs.HunterPendingFrom = ""
		return gs.resumeAfterHunterShoot(from)
	}
	if target < 0 || target >= MaxPlayers {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	if !gs.AliveSeat(target) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
	}
	if err := gs.killPlayer(target, "hunter"); err != nil {
		return errcode.CodeMsg(errcode.ErrValidationFailed, err.Message)
	}
	from := gs.HunterPendingFrom
	// §135 开枪带人 = 主动亮身份,全场立刻知道 actor 是猎人。
	// 这是 RolePubliclyRevealed 的 4 类公开事件之一。
	gs.Players[actor].HunterFired = true
	gs.HunterPendingShoot = false
	gs.HunterPendingFrom = ""
	gs.checkWinner()
	if gs.Status == "over" {
		return nil
	}
	// 遗言:被枪杀者 LastWords=true 则入队;队列清空后按触发来源恢复阶段。
	gs.tryEnterDeathLyricRound([]Seat{target}, func() *errcode.Error {
		return gs.resumeAfterHunterShoot(from)
	})
	return nil
}

func (gs *GameState) IdiotReveal(actor Seat, choice string) *errcode.Error {
	if gs.Phase != PhaseIdiotReveal {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not idiot_reveal phase")
	}
	if gs.DayEliminated != actor {
		return errcode.CodeMsg(errcode.ErrPermissionDenied, "only the eliminated idiot may reveal")
	}
	if gs.Roles[actor] != RoleIdiot || !gs.AliveSeat(actor) || gs.Players[actor].IdiotRevealed {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	target := actor
	switch choice {
	case "reveal":
		gs.Players[target].IdiotRevealed = true
		gs.Players[target].LastWords = false
		gs.DayEliminated = NoSeat // 翻牌后失去投票权,当天无人出局
		gs.checkWinner()
		if gs.Status == "over" {
			return nil
		}
		// 直接进入黑夜。
		gs.DayNumber++
		gs.startNight()
		return nil
	case "skip":
		// 放弃翻牌:正常放逐。复用投票放逐的后续路径。
		gs.DayEliminated = NoSeat
		gs.Players[target].LastWords = gs.DayNumber <= LastWordsRounds
		_ = gs.killPlayer(target, "vote")
		gs.checkWinner()
		if gs.Status == "over" {
			return nil
		}
		gs.tryEnterDeathLyricRound([]Seat{target}, func() *errcode.Error {
			if gs.Roles[target] == RoleHunter {
				gs.HunterPendingShoot = true
				gs.HunterPendingFrom = "vote"
				setPhaseAndDeadline(gs, PhaseHunterShoot)
				// BUG-R10-P0-3: 把 TurnActingSeat 设为猎人座位(详见
				// hunterSeatForPhaseLocked 注释)。
				gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
				return nil
			}
			gs.advanceDay()
			return nil
		})
		return nil
	default:
		return errcode.CodeMsg(errcode.ErrValidationFailed, "idiot_reveal choice must be 'reveal' or 'skip'")
	}
}

// KnightDuel §198 骑士公开决斗。白天发言阶段发动;每局限一次。
//
// 校验链(按顺序):
//   K3 Phase == PhaseSpeak
//   K4 AliveSeat(actor)
//   K5 Roles[actor] == RoleKnight
//   K2 !Players[actor].KnightDuelUsed
//   K6 target != NoSeat  / K7 AliveSeat(target)
//   K8 target != actor
//
// 结算:
//   命中狼 (Roles[target]==Werewolf) → killPlayer(target, "vote") [execution]
//                                → DayEliminated=target(★ §198 选择)
//   未命中   → killPlayer(actor, "duel") [execution + 自决死因]
//
// 副作用:
//   Players[actor].KnightDuelUsed = true   // K1/K2 锁定本局
//   // RolePubliclyRevealed 通过 KnightDuelUsed 触发(K8 §135 白名单第 ⑤ 类)
//
// 注意:KnightDuel **不修改 Phase**,发言轮按 SpeakTurnSeat 顺序继续走;
// 不触发遗言 / 放逐 / 警长结算流程。设计师对 §135 公平性的扩展:
//   - 命中狼:狼的执行语义与投死一致 → verdictFor("vote")=execution
//     (不能复用 "wolf" 否则 verdict=death 模糊因果断言)。
//   - 自决:死因 duel → verdictFor → execution。
func (gs *GameState) KnightDuel(actor Seat, target Seat) *errcode.Error {
	if gs.Phase != PhaseSpeak {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "knight duel only in speak phase")
	}
	if !gs.AliveSeat(actor) || gs.Roles[actor] != RoleKnight {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	if gs.Players[actor].KnightDuelUsed {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "knight duel already used this game")
	}
	if target == NoSeat {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "knight duel target required (-1 only allowed by LLM enum to express 'skip this turn')")
	}
	if target == actor {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot duel self")
	}
	if !gs.AliveSeat(target) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
	}
	// 锁定本局技能(K1/K2)+ 公开身份(K8)
	gs.Players[actor].KnightDuelUsed = true

	if gs.Roles[target] == RoleWerewolf {
		// 命中狼 → 目标出局(verdict=execution,"vote" 因果与白天放逐对齐)。
		// 不写 gs.DayEliminated(避免与 PhaseVote 的 auto-tally 路径混淆);
		// killPlayer 单独填死法。
		gs.killPlayer(target, "vote")
		gs.checkWinner()
		// 注:PhaseSpeak 继续;不回退、不触发遗言(归入"决斗技能"语义,
		// 与白天放逐完整遗言流程分离 —— K9 与 K9'.
		// 若 DeathLyric 流程需要在决斗后启动,把下面的 tryEnterDeathLyricRound
		// 解开注释。
		// gs.tryEnterDeathLyricRound([]Seat{target}, func() *errcode.Error { return nil })
	} else {
		// 未命中狼 → 骑士自己出局(verdict=execution,cause="duel" 自决)。
		gs.killPlayer(actor, DeathCauseDuel)
		gs.checkWinner()
	}
	return nil
}

func (gs *GameState) SheriffStreamDeclare(actor Seat, slot int, target Seat) *errcode.Error {
	if gs.SheriffSeat != actor {
		return errcode.CodeMsg(errcode.ErrPermissionDenied, "only the sheriff may declare streams")
	}
	if gs.Roles[actor] != RoleSeer { // 仅预言家警长可生效警徽流
		return errcode.CodeMsg(errcode.ErrPermissionDenied, "only a seer sheriff may declare streams")
	}
	if gs.Phase != PhaseSpeak && gs.Phase != PhaseSheriff && gs.Phase != PhaseDawn {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "sheriff_stream not allowed in this phase")
	}
	if slot != 1 && slot != 2 {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "slot must be 1 or 2")
	}
	if target != NoSeat {
		if target < 0 || target >= MaxPlayers {
			return errcode.Code(errcode.ErrValidationFailed)
		}
		if target == actor {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot stream self")
		}
		if !gs.AliveSeat(target) {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
		}
	}
	gs.SheriffStreams[slot-1] = target
	if target == NoSeat {
		gs.SheriffStreamsAt[slot-1] = 0
		// §20260811-10 U5 — 撤回警徽流时同时清零声明轮次,避免下一次声明
		// 时 AgeRounds 仍按上一次的轮次计算。
		gs.SheriffStreamRounds[slot-1] = 0
	} else {
		gs.SheriffStreamsAt[slot-1] = time.Now().Unix()
		// §20260811-10 U5 — 记录声明轮次,view.go 据此计算 AgeRounds / IsStale。
		gs.SheriffStreamRounds[slot-1] = gs.DayNumber
	}
	return nil
}

func (gs *GameState) SheriffCandidate(actor Seat) *errcode.Error {
	if gs.Phase != PhaseSheriff {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not sheriff phase")
	}
	if !gs.AliveSeat(actor) {
		// §报告-20260804-03: 与 ProposeVote 一致改用 ErrDeadPlayerAction (40112)
		// 专属 code,前端可据此明确提示「已阵亡,不能参选警长」。
		return errcode.CodeMsg(errcode.ErrDeadPlayerAction, "dead player cannot run for sheriff")
	}
	gs.Players[actor].HasSpoken = true // 复用为"已参选"
	return nil
}

// SheriffCandidates 返回当前所有已参选(存活)座位。
//
// §报告-20260804-03 BUG-04: 参选状态复用 Player.HasSpoken 存储,该字段在
// PhaseSpeak 语义是「白天已发言」。本函数**只在 PhaseSheriff 下有意义**,
// 调用方(view.go / tools.go)必须自行确认 phase,否则会把「已发言」误读为
// 「已参选」。
func (gs *GameState) SheriffCandidates() []Seat {
	if gs.Phase != PhaseSheriff {
		return nil
	}
	var out []Seat
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Players[i].HasSpoken {
			out = append(out, Seat(i))
		}
	}
	return out
}

// SheriffElect 结算警长竞选。actor 是发起结算的座位。
//
// §报告-20260804-03 BUG-06: 此前签名为 SheriffElect() 且无任何调用者校验 ——
// 修复 BUG-01(前端结束按钮从 start_day 改为 elect)后,越权问题会从
// 「永远失败」变成「永远成功且可滥用」,故此处补上存活入座校验。
// actor==NoSeat 是**系统内部调用**的哨兵(watchdog / quarantine-skip /
// agent sheriff_elect 兜底),跳过校验。
func (gs *GameState) SheriffElect(actor Seat) *errcode.Error {
	if gs.Phase != PhaseSheriff {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "not sheriff phase")
	}
	if actor != NoSeat && !gs.AliveSeat(actor) {
		return errcode.CodeMsg(errcode.ErrDeadPlayerAction, "dead player cannot settle sheriff election")
	}
	tally := gs.TallyVotes(true)
	if len(tally) == 0 {
		// 没人参选或没人投票 → 本局无警长
		gs.SheriffSeat = NoSeat
		gs.startSpeakPhase()
		return nil
	}
	maxVal := -1
	var leaders []Seat
	for s, c := range tally {
		if c > maxVal {
			maxVal = c
			leaders = []Seat{s}
		} else if c == maxVal {
			leaders = append(leaders, s)
		}
	}
	if len(leaders) == 1 {
		gs.SheriffSeat = leaders[0]
		gs.Players[leaders[0]].IsSheriff = true
		gs.startSpeakPhase()
		return nil
	}
	// 平票:对 leader 二次发言+投票;我们用 SpeakOrder 限定平票者,Phase 切回 Speak
	gs.SpeakOrder = append([]Seat{}, leaders...)
	gs.SpeakTurnSeat = leaders[0]
	// 调用方再调一次 SheriffElect 即可;投票后清空原有 votes 以便二次投票
	for i := range gs.Players {
		gs.Players[i].Voted = false
		gs.Players[i].VoteTarget = NoSeat
	}
	// Phase 留给 caller 决定:这里仍保持 Speak;调用方在 sheriff 平票时应再次
	// 让玩家投,然后再调 SheriffElect。
	setPhaseAndDeadline(gs, PhaseSpeak)
	return nil
}

