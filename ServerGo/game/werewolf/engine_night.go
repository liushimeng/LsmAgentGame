package werewolf

import (
	"LsmWebGame/errcode"
)

func (gs *GameState) startNight() {
	gs.WolfKillTarget = NoSeat
	// 2026-07-17: 重置夜间投票状态
	for i := range gs.WolfVotes {
		gs.WolfVotes[i] = NoSeat
		gs.WolfVoteCast[i] = false
		gs.WolfVoteReasons[i] = "" // §20260810-04 U2: 刀人理由与投票同生命周期
	}
	gs.WolfVoteTally = nil
	gs.NightDeaths = gs.NightDeaths[:0]
	gs.SuicidedWolfSeat = NoSeat
	gs.DayEliminated = NoSeat
	gs.DayTiedPlayers = nil
	gs.sheriffSlain = NoSeat
	// §134 重置当晚守卫临时状态(GuardLastProtect 跨夜保留,是 G1 连守校验的依据)。
	gs.GuardProtectTarget = NoSeat
	gs.GuardSavedTarget = NoSeat
	gs.SameGuardSameSave = NoSeat
	gs.WitchSavedTarget = NoSeat
	// §猎魔人 重置当晚猎魔人临时状态(无跨夜保留字段,DH5 每晚限一次仅限当晚)。
	gs.DemonHunterHuntTarget = NoSeat
	for i := range gs.Players {
		gs.Players[i].Voted = false
		gs.Players[i].HasSpoken = false
		gs.Players[i].VoteTarget = NoSeat
	}
	// §134: 守卫阶段插在狼刀之前(盲守)。守卫存活则 PhaseNightGuard + TurnActingSeat=GuardSeat;
	// 否则直接走 PhaseNightWolves 旧路径。
	if gs.GuardSeat != NoSeat && gs.AliveSeat(gs.GuardSeat) {
		setPhaseAndDeadline(gs, PhaseNightGuard)
		gs.TurnActingSeat = gs.GuardSeat
	} else {
		setPhaseAndDeadline(gs, PhaseNightWolves)
		gs.TurnActingSeat = firstLivingWolf(gs)
	}
	// 第一晚女巫没死,但跳过预言家(alive 但未唤醒)也算合法
	gs.refreshCounts()
}

// WolfVoteReasonMaxRunes 是 wolf_kill 刀人理由的 rune 上限(§20260810-04 U2)。
// 理由只进狼 bot GameContext,但截断防 LLM 超长文本灌爆队友上下文。
const WolfVoteReasonMaxRunes = 30

// truncateWolfVoteReason 把刀人理由 rune 安全截断到 WolfVoteReasonMaxRunes。
func truncateWolfVoteReason(reason string) string {
	r := []rune(reason)
	if len(r) <= WolfVoteReasonMaxRunes {
		return reason
	}
	return string(r[:WolfVoteReasonMaxRunes])
}

func (gs *GameState) NightWolfKill(actor Seat, target Seat, reason string) *errcode.Error {
	if gs.Phase != PhaseNightWolves {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not night_wolves phase")
	}
	if !gs.AliveSeat(actor) || gs.Roles[actor] != RoleWerewolf {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	// 2026-07-24 R196-P1: 拒绝重复投票。已投(含弃权)的狼人再调 wolf_kill
	// 一律 ErrAlreadyWolfVoted,LLM 收到 error 后自然收敛,不再循环。
	if gs.WolfVoteCast[actor] {
		return errcode.CodeMsg(errcode.ErrAlreadyWolfVoted, "you have already voted this round")
	}
	// 校验 target: NoSeat(弃权) 或 合法目标(存活非狼人)
	if target != NoSeat {
		if target < 0 || target >= MaxPlayers {
			return errcode.Code(errcode.ErrValidationFailed)
		}
		if !gs.AliveSeat(target) {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
		}
		if gs.Roles[target] == RoleWerewolf {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "wolves cannot kill each other")
		}
	}
	// 记录投票(一次性)
	gs.WolfVotes[actor] = target
	gs.WolfVoteCast[actor] = true
	// §20260810-04 U2 — 刀人理由(≤30 字,rune 截断;弃权清理由)。
	if target == NoSeat {
		gs.WolfVoteReasons[actor] = ""
	} else {
		gs.WolfVoteReasons[actor] = truncateWolfVoteReason(reason)
	}

	// 全部存活狼人已投票 → 立即计票并推进
	if gs.allWolvesVoted() {
		gs.tallyWolfVotes()
		gs.endWolfPhase()
	}
	return nil
}

func (gs *GameState) allWolvesVoted() bool {
	total := 0
	cast := 0
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			total++
			if gs.WolfVoteCast[i] {
				cast++
			}
		}
	}
	return total > 0 && cast >= total
}

func (gs *GameState) tallyWolfVotes() {
	counts := map[int]int{}
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) || gs.Roles[i] != RoleWerewolf {
			continue
		}
		target := gs.WolfVotes[i]
		if target == NoSeat {
			continue
		}
		counts[int(target)]++
	}

	tally := &WolfVoteTally{Counts: counts}

	if len(counts) == 0 {
		// 全弃权 → 从合法目标中随机选择
		tally.Tied = nil
		tally.Reason = "random_all_abstain"
		tally.Final = gs.randomAliveNonWolf()
	} else {
		// 找最高票
		maxV := 0
		for _, v := range counts {
			if v > maxV {
				maxV = v
			}
		}
		tied := []int{}
		for t, v := range counts {
			if v == maxV {
				tied = append(tied, t)
			}
		}
		tally.Tied = tied
		if len(tied) == 1 {
			tally.Reason = "majority"
			tally.Final = tied[0]
		} else {
			tally.Reason = "random_tie_break"
			tally.Final = tied[gs.rng.Intn(len(tied))]
		}
	}

	gs.WolfKillTarget = Seat(tally.Final)
	gs.WolfVoteTally = tally
}

func (gs *GameState) randomAliveNonWolf() int {
	candidates := []int{}
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] != RoleWerewolf {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return int(NoSeat)
	}
	return candidates[gs.rng.Intn(len(candidates))]
}

func (gs *GameState) endWolfPhase() {
	setPhaseAndDeadline(gs, PhaseNightSeer, hasHumanPlayer(gs))
	gs.TurnActingSeat = firstLivingSeer(gs)
	if gs.TurnActingSeat == NoSeat {
		gs.endSeerPhase()
	}
}

func (gs *GameState) NightSeerCheck(actor Seat, target Seat) *errcode.Error {
	if gs.Phase != PhaseNightSeer {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not night_seer phase")
	}
	if !gs.AliveSeat(actor) || gs.Roles[actor] != RoleSeer {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	if target < 0 || target >= MaxPlayers {
		return errcode.Code(errcode.ErrValidationFailed)
	}
	if target == actor {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot check self")
	}
	if !gs.AliveSeat(target) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
	}
	gs.Players[actor].LastSeerCheck = target
	// §20260810-04 U3 — 查验历史记账:警徽流结算以「是否真查过」为准。
	gs.Players[actor].SeerCheckHistory = append(gs.Players[actor].SeerCheckHistory, target)
	gs.endSeerPhase()
	return nil
}

func (gs *GameState) endSeerPhase() {
	setPhaseAndDeadline(gs, PhaseNightWitch)
	if gs.WitchSeat == NoSeat || !gs.AliveSeat(gs.WitchSeat) {
		gs.endWitchPhase()
		return
	}
	gs.TurnActingSeat = gs.WitchSeat
}

func (gs *GameState) NightWitchAct(actor Seat, action string, target Seat) *errcode.Error {
	if gs.Phase != PhaseNightWitch {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not night_witch phase")
	}
	if actor != gs.WitchSeat || !gs.AliveSeat(actor) {
		return errcode.Code(errcode.ErrPermissionDenied)
	}

	switch action {
	case "none":
		// 不使用任何药
	case "antidote":
		if gs.Players[actor].WitchAntidoteUsed {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "antidote used")
		}
		// 解药只对当晚狼刀有效
		if gs.WolfKillTarget == NoSeat {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "wolves did not kill, cannot use antidote")
		}
		gs.Players[actor].WitchAntidoteUsed = true
		// §134: 必须先记录 WitchSavedTarget 再破坏性写 WolfKillTarget=NoSeat。
		// 同守同救裁决依赖 WitchSavedTarget 与 GuardProtectTarget 比较;
		// 若先清空 WolfKillTarget 再读它则同守同救永远无法触发。
		gs.WitchSavedTarget = gs.WolfKillTarget
		gs.WolfKillTarget = NoSeat
	case "poison":
		if gs.Players[actor].WitchPoisonUsed {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "poison used")
		}
		if target == NoSeat || target < 0 || target >= MaxPlayers {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "need poison target")
		}
		if !gs.AliveSeat(target) {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
		}
		// 同刀同毒依然毒杀成功(规则)
		gs.Players[actor].WitchPoisonUsed = true
		if err := gs.killPlayer(target, "witch_poison"); err != nil {
			return errcode.CodeMsg(errcode.ErrValidationFailed, err.Message)
		}
		gs.NightDeaths = appendUniqueSeat(gs.NightDeaths, target)
	default:
		return errcode.CodeMsg(errcode.ErrValidationFailed, "unknown witch action")
	}

	gs.endWitchPhase()
	return nil
}

func (gs *GameState) NightGuardProtect(actor Seat, target Seat) *errcode.Error {
	if gs.Phase != PhaseNightGuard {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not night_guard phase")
	}
	if actor != gs.GuardSeat || !gs.AliveSeat(actor) || gs.Roles[actor] != RoleGuard {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	// 空守 (G4):合法,直接推进;GuardLastProtect 置 NoSeat(下一晚可守任何人)。
	if target == NoSeat {
		gs.GuardProtectTarget = NoSeat
		gs.GuardLastProtect = NoSeat
		gs.endGuardPhase()
		return nil
	}
	// G2:不能守自己
	if target == actor {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot guard self")
	}
	// G3:只能守存活
	if !gs.AliveSeat(target) {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
	}
	// G1:不能连续两晚守同一人
	if target == gs.GuardLastProtect {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot guard the same player twice in a row")
	}
	gs.GuardProtectTarget = target
	gs.GuardLastProtect = target
	gs.endGuardPhase()
	return nil
}

func (gs *GameState) endGuardPhase() {
	setPhaseAndDeadline(gs, PhaseNightWolves, hasHumanPlayer(gs))
	gs.TurnActingSeat = firstLivingWolf(gs)
	if gs.TurnActingSeat == NoSeat {
		gs.endWolfPhase()
	}
}

func (gs *GameState) endWitchPhase() {
	// §134 守卫护盾 / 同守同救裁决。两个分支互斥。
	switch {
	case gs.WitchSavedTarget != NoSeat && gs.GuardProtectTarget == gs.WitchSavedTarget:
		// 同守同救 → 两者抵消,该玩家仍然死亡(解药已消耗,不退还)。
		// 裁决:把 WolfKillTarget 复活为 WitchSavedTarget,以便下方统一 killPlayer 流程。
		gs.SameGuardSameSave = gs.WitchSavedTarget
		gs.WolfKillTarget = gs.WitchSavedTarget
	case gs.WolfKillTarget != NoSeat && gs.GuardProtectTarget == gs.WolfKillTarget:
		// 护盾单独生效 → 挡下狼刀。
		gs.GuardSavedTarget = gs.WolfKillTarget
		gs.WolfKillTarget = NoSeat
	}
	if gs.WolfKillTarget != NoSeat {
		if err := gs.killPlayer(gs.WolfKillTarget, "wolf"); err == nil {
			gs.NightDeaths = appendUniqueSeat(gs.NightDeaths, gs.WolfKillTarget)
			// §135 猎人夜间开枪接线。此前 HunterPendingShoot 只在白天投票放逐
			// 两条路径置位(engine_day.go),狼刀死的猎人**从未**获得开枪机会 ——
			// HunterPendingFrom=="wolf" 分支、view.go 的 !="poison" 守卫、
			// HunterShoot 的 poison 拒绝分支全都是为一条永不执行的路径写的死代码
			// (§130/§134「声明了却从不接线」教训的又一次复现)。
			//
			// 女巫毒杀路径(NightWitchAct)显式**不**置位 —— 规则:被毒不能开枪,
			// 且身份依旧隐藏(见 RolePubliclyRevealed 的 HunterFired 判定)。
			if gs.Roles[gs.WolfKillTarget] == RoleHunter {
				gs.HunterPendingShoot = true
				gs.HunterPendingFrom = "wolf"
			}
		}
	}
	// §猎魔人 女巫阶段结束后,串接猎魔人狩猎阶段(DH5):
	//   - DH1 首夜禁用(DayNumber<2)→ 跳过该阶段,直接进 dawn
	//   - 本局无猎魔人 / 猎魔人已死 → 跳过
	//   - 否则进入 PhaseNightDemonHunter,TurnActingSeat=firstLivingDemonHunter
	// 守卫护盾 / 同守同救裁决已在女巫后完成,猎魔人狩猎不影响 WolfKillTarget。
	if gs.shouldEnterDemonHunterPhase() {
		setPhaseAndDeadline(gs, PhaseNightDemonHunter, hasHumanPlayer(gs))
		gs.TurnActingSeat = firstLivingDemonHunter(gs)
		return
	}
	setPhaseAndDeadline(gs, PhaseDawn)
	gs.TurnActingSeat = NoSeat
	gs.LastNightDeaths = append([]Seat{}, gs.NightDeaths...)
	gs.checkWinner()
	if gs.Status == "over" {
		return
	}
	// BUG 2026-07-09: 遗言:LastNightDeaths 中 LastWords=true 的座位入队;队列清空
	// 后恢复 dawn 阶段并调 StartDay 进入 sheriff/speak。注意 StartDay 仅接受
	// PhaseDawn,所以闭包内先复位 phase 到 dawn 再调 StartDay。
	gs.tryEnterDeathLyricRound(gs.LastNightDeaths, func() *errcode.Error {
		// §135 遗言结束后,若夜死的是猎人则先进开枪阶段;开枪结算完毕由
		// resumeAfterHunterShoot 走回 dawn → StartDay(见 engine_day.go)。
		//
		// BUG-R10-P0-3 (2026-07-29): PhaseHunterShoot 必须把 TurnActingSeat
		// 设为猎人座位,而非 NoSeat。原因:
		//   - agent 侧 ShouldAutoSkip 在非 speak phase 用 currentTurnActing
		//     与 seat 比较;NoSeat → currentTurnActing=-1 → "falls through to true"
		//     在 ShouldAutoSkip 真实被调时才生效。
		//   - manager 侧 tryDispatchQuarantinedActingSkip / notifyQuarantine
		//     链路大量依赖 gc.MyTurn 派生;PhaseHunterShoot 走 hunter_pending
		//     分支正确填 true,但 engine 内 TurnActingSeat 仍为 NoSeat,导致
		//     旁观者视图 cs.MyTurn(view.go:411)与 actor 视图 gc.MyTurn 出现
		//     不一致,watchdog 与 agent skip 派发路径在某些竞态下错位。
		// 修复:把进入 PhaseHunterShoot 的入口(本处夜间 + 投票放逐 + 白痴
		// 翻牌后猎人 + advanceDay)统一在 setPhaseAndDeadline 之后立即把
		// TurnActingSeat 设为猎人座位。hunterSeatForPhaseLocked 是单一查找
		// 入口,避免四处分叉。
		if gs.HunterPendingShoot {
			setPhaseAndDeadline(gs, PhaseHunterShoot)
			gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
			return nil
		}
		setPhaseAndDeadline(gs, PhaseDawn)
		gs.TurnActingSeat = NoSeat
		return gs.StartDay()
	})
}

// shouldEnterDemonHunterPhase §猎魔人 判定女巫结束后是否需要进入猎魔人狩猎阶段:
//   - DH1 首夜禁用(DayNumber<2)→ false
//   - 本局无猎魔人 / 猎魔人已死 → false
//   - 否则 true(进入 PhaseNightDemonHunter)
func (gs *GameState) shouldEnterDemonHunterPhase() bool {
	if gs.DemonHunterSeat == NoSeat {
		return false
	}
	if !gs.AliveSeat(gs.DemonHunterSeat) {
		return false
	}
	if gs.DayNumber < 2 {
		// DH1 首夜不可用 —— 直接空过,无 NocthDeaths / 状态变更
		return false
	}
	return true
}

// NightDemonHunterHunt §猎魔人 夜间狩猎决策。
// 校验链(DH-A..DH-G):
//   DH-A Phase == PhaseNightDemonHunter
//   DH-B DayNumber >= 2 (否则 ErrValidationFailed;首夜不可用)
//   DH-C actor == gs.DemonHunterSeat && AliveSeat(actor)
//   DH-D target == NoSeat(空过) OR (target ∈ [0, MaxPlayers) && AliveSeat(target) && target != actor)
//   DH-E gs.DemonHunterHuntTarget = target
//   DH-F gs.Players[actor].DemonHunterHuntUsed = true(公开身份)
//   DH-G endDemonHunterPhase() 结算
//
// 结算:
//   - target == NoSeat(空过): 无死亡,直接进 dawn(由 endDemonHunterPhase 推进)
//   - target 是狼(RoleWerewolf): killPlayer(target, "wolf") 复用狼 cause,
//                                  NightDeaths 追加 target,verdict=death
//   - target 是好人(FactionGood): killPlayer(actor, "demon_hunter_misjudge")
//                                  NightDeaths 追加 actor,verdict=execution(自决)
//   - target FactionUnknown(防御性兜底): 走误杀路径,保守处理
func (gs *GameState) NightDemonHunterHunt(actor Seat, target Seat) *errcode.Error {
	if gs.Phase != PhaseNightDemonHunter {
		return errcode.CodeMsg(errcode.ErrNotYourTurn, "not night_demon_hunter phase")
	}
	// DH-B 首夜禁用
	if gs.DayNumber < 2 {
		return errcode.CodeMsg(errcode.ErrValidationFailed, "demon hunter first night unavailable")
	}
	// DH-C actor 必须是存活猎魔人
	if actor != gs.DemonHunterSeat || !gs.AliveSeat(actor) || gs.Roles[actor] != RoleDemonHunter {
		return errcode.Code(errcode.ErrPermissionDenied)
	}
	// DH-D target 校验
	if target != NoSeat {
		if target < 0 || target >= MaxPlayers {
			return errcode.Code(errcode.ErrValidationFailed)
		}
		if !gs.AliveSeat(target) {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "target dead")
		}
		if target == actor {
			return errcode.CodeMsg(errcode.ErrValidationFailed, "cannot hunt self")
		}
	}
	// DH-E / DH-F 写状态
	gs.DemonHunterHuntTarget = target
	gs.Players[actor].DemonHunterHuntUsed = true
	// DH-G 结算
	gs.endDemonHunterPhase()
	return nil
}

// endDemonHunterPhase §猎魔人 关闭猎魔人狩猎阶段 → 进入 dawn(公布死亡)。
// 逻辑:
//   - 空过(target==NoSeat) → 无死亡
//   - 命中狼(target 是 RoleWerewolf) → killPlayer(target, "wolf")
//   - 命中好人(target 是 FactionGood) → killPlayer(actor, "demon_hunter_misjudge")
//   - 防御性兜底(目标 FactionUnknown) → 走误杀路径(保守)
// 走完后与 endWitchPhase 同构:lastNightDeaths = NightDeaths,checkWinner,
// 若 gameover 直接 return,否则 tryEnterDeathLyricRound → StartDay。
func (gs *GameState) endDemonHunterPhase() {
	actor := gs.DemonHunterSeat
	target := gs.DemonHunterHuntTarget
	if target != NoSeat && gs.AliveSeat(target) {
		// 区分命中狼 / 命中好人
		switch gs.Roles[target] {
		case RoleWerewolf:
			// DH-G-1 命中狼:target 出局(cause=wolf,verdict=death)
			if err := gs.killPlayer(target, DeathCauseWolf); err == nil {
				gs.NightDeaths = appendUniqueSeat(gs.NightDeaths, target)
			}
		default:
			// DH-G-2 命中好人(含 RoleVillager / RoleSeer / RoleWitch / RoleHunter /
			// RoleIdiot / RoleGuard / RoleKnight / RoleDemonHunter):actor 自决出
			// (cause=demon_hunter_misjudge,verdict=execution)。
			//
			// FactionUnknown 防御性兜底:保守按"误杀好人"处理,避免狼利用
			// FactionUnknown 角色规避猎魔人狩猎后果。
			if err := gs.killPlayer(actor, DeathCauseDemonHunterMisjudge); err == nil {
				gs.NightDeaths = appendUniqueSeat(gs.NightDeaths, actor)
			}
		}
	}
	// 进入 dawn(与 endWitchPhase 同构)
	setPhaseAndDeadline(gs, PhaseDawn)
	gs.TurnActingSeat = NoSeat
	gs.LastNightDeaths = append([]Seat{}, gs.NightDeaths...)
	gs.checkWinner()
	if gs.Status == "over" {
		return
	}
	gs.tryEnterDeathLyricRound(gs.LastNightDeaths, func() *errcode.Error {
		if gs.HunterPendingShoot {
			setPhaseAndDeadline(gs, PhaseHunterShoot)
			gs.TurnActingSeat = hunterSeatForPhaseLocked(gs)
			return nil
		}
		setPhaseAndDeadline(gs, PhaseDawn)
		gs.TurnActingSeat = NoSeat
		return gs.StartDay()
	})
}

func appendUniqueSeat(s []Seat, x Seat) []Seat {
	for _, v := range s {
		if v == x {
			return s
		}
	}
	return append(s, x)
}

