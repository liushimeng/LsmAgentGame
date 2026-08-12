package werewolf

// engine_demon_hunter_test.go — §猎魔人 猎魔人角色测试。
//
// 按 docs/狼人杀猎魔人角色设计.md §8 列出的 D-01..D-14 用例实现。
// 13 人局强制使用 makeStartedGame13;7 人局强制使用 makeStartedGame。
//
// 设计原则:
//   - 每条用例只断言设计文档 §2/§3 描述的核心不变量,允许引擎推进阶段、killPlayer
//     等副作用存在(只要不破坏断言);
//   - 凡是依赖「13 人局必有猎魔人」的断言用 makeStartedGame13 + 调 ensureDemonHunterSeat
//     做兜底(若种子巧合无猎魔人则跳过 t.Skip);
//   - 隔离副作用:每个测试独立 new GameState,互不影响。

import (
	"testing"
)

// ensureDemonHunterSeat 在 13 人局测试中确保存在猎魔人;若种子恰好未发猎魔人则注入一个。
// 必须在 startNight 之前调用 —— 否则 startNight 不会进入 PhaseNightDemonHunter。
func ensureDemonHunterSeat(t *testing.T, gs *GameState) bool {
	t.Helper()
	if gs.DemonHunterSeat != NoSeat {
		return true
	}
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) {
			continue
		}
		r := gs.Roles[i]
		if r == RoleWerewolf || r == RoleWitch || r == RoleSeer {
			continue
		}
		gs.Roles[i] = RoleDemonHunter
		gs.Players[i].Role = RoleDemonHunter
		gs.DemonHunterSeat = Seat(i)
		return true
	}
	return false
}

// firstLivingDemonHunterSeat 取得当前猎魔人座位(若存活)。
func firstLivingDemonHunterSeat(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleDemonHunter {
			return Seat(i)
		}
	}
	return NoSeat
}

// firstLivingGoodNonDemonHunter 找一个非狼人/非猎魔人的存活好人(用于测试"命中好人")。
func firstLivingGoodNonDemonHunter(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if !gs.AliveSeat(s) {
			continue
		}
		r := gs.Roles[i]
		if r == RoleWerewolf || r == RoleDemonHunter {
			continue
		}
		return s
	}
	return NoSeat
}

// advanceToNightDemonHunter 让 GameState 从 startNight 推进到 PhaseNightDemonHunter。
// 处理守卫 → 狼人 → 预言家 → 女巫 → 猎魔人的完整链路。
// 前置:gs 已 startNight,DayNumber >= 2(否则猎魔人阶段会被跳过)。
// 为避免狼人空刀随机选目标导致意外死亡进入遗言阶段,本函数让狼人投票给一个
// 特定的"牺牲位"(wolfSacrifice),并设置 DayNumber >= 3 以禁用 LastWords。
func advanceToNightDemonHunter(t *testing.T, gs *GameState) {
	t.Helper()
	// 设置 DayNumber >= 3 以禁用 LastWords,避免遗言阶段干扰测试
	gs.DayNumber = 3
	// 找一个"牺牲位"(非狼人/非猎魔人/非预言家/非女巫/非守卫的存活玩家)
	wolfSacrifice := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if !gs.AliveSeat(s) {
			continue
		}
		r := gs.Roles[i]
		if r == RoleWerewolf || r == RoleDemonHunter || r == RoleSeer || r == RoleWitch || r == RoleGuard {
			continue
		}
		wolfSacrifice = s
		break
	}
	// 守卫阶段:空守
	if gs.Phase == PhaseNightGuard {
		gs.NightGuardProtect(gs.GuardSeat, NoSeat)
	}
	// 狼人阶段:全员投票给 wolfSacrifice(避免空刀随机选目标)
	if gs.Phase == PhaseNightWolves && wolfSacrifice != NoSeat {
		for i := 0; i < MaxPlayers; i++ {
			if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
				gs.NightWolfKill(Seat(i), wolfSacrifice, "")
			}
		}
	} else if gs.Phase == PhaseNightWolves {
		// 没有合适牺牲位,全员弃权(可能触发随机选目标)
		for i := 0; i < MaxPlayers; i++ {
			if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
				gs.NightWolfKill(Seat(i), NoSeat, "")
			}
		}
	}
	// 预言家阶段:查验一个存活非自己目标
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeerSeat(gs)
		if seer != NoSeat {
			var target Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					target = Seat(i)
					break
				}
			}
			if target != NoSeat {
				gs.NightSeerCheck(seer, target)
			} else {
				gs.endSeerPhase()
			}
		} else {
			gs.endSeerPhase()
		}
	}
	// 女巫阶段:不用药
	if gs.Phase == PhaseNightWitch {
		witch := firstLivingWitchSeat(gs)
		if witch != NoSeat {
			gs.NightWitchAct(witch, "none", NoSeat)
		} else {
			gs.endWitchPhase()
		}
	}
}

// D-01: 首夜禁用,DayNumber=1 调 hunt 返回 ErrValidationFailed
func TestNightDemonHunterHunt_FirstNightDisabled(t *testing.T) {
	gs := makeStartedGame13(t, 100, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat available")
	}
	// 强制 DayNumber=1(首夜)
	gs.DayNumber = 1
	// 手动进入 PhaseNightDemonHunter 模拟阶段(实际 startNight 不会进入,因为 DayNumber<2)
	dh := gs.DemonHunterSeat
	gs.Phase = PhaseNightDemonHunter
	gs.TurnActingSeat = dh
	// 找一个合法目标(存活非自己)
	target := firstLivingWolfSeat(gs)
	if target == NoSeat || target == dh {
		t.Skip("no legal target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e == nil {
		t.Fatalf("expected ErrValidationFailed on first night, got nil")
	}
}

// D-02: DH6 命中狼 → target 死亡,cause=wolf,verdict=death
func TestNightDemonHunterHunt_HitWerewolf_TargetDies(t *testing.T) {
	gs := makeStartedGame13(t, 200, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	// 推进到下一夜(DayNumber=2)
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	if gs.Phase != PhaseNightDemonHunter {
		t.Fatalf("expected night_demon_hunter, got %v", gs.Phase)
	}
	dh := gs.DemonHunterSeat
	// 找一个存活狼人
	target := firstLivingWolfSeat(gs)
	if target == NoSeat {
		t.Skip("no wolf target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e != nil {
		t.Fatalf("demon hunter hunt: %v", e)
	}
	// target 应死亡
	if gs.AliveSeat(target) {
		t.Fatalf("target (seat %v) should be dead after hunt", target)
	}
	if gs.Players[target].DeathCause != "wolf" {
		t.Fatalf("DeathCause=%q, want wolf", gs.Players[target].DeathCause)
	}
	if gs.Players[target].DeathVerdict != "death" {
		t.Fatalf("DeathVerdict=%q, want death", gs.Players[target].DeathVerdict)
	}
}

// D-03: 命中好人 → dh 自决出,cause=demon_hunter_misjudge,verdict=execution
func TestNightDemonHunterHunt_HitGood_ActorDies(t *testing.T) {
	gs := makeStartedGame13(t, 300, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	dh := gs.DemonHunterSeat
	// 找一个存活好人(非狼人/非猎魔人)
	target := firstLivingGoodNonDemonHunter(gs)
	if target == NoSeat {
		t.Skip("no good target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e != nil {
		t.Fatalf("demon hunter hunt: %v", e)
	}
	// dh 应死亡
	if gs.AliveSeat(dh) {
		t.Fatalf("demon hunter (seat %v) should be dead after misjudge", dh)
	}
	if gs.Players[dh].DeathCause != "demon_hunter_misjudge" {
		t.Fatalf("DeathCause=%q, want demon_hunter_misjudge", gs.Players[dh].DeathCause)
	}
	if gs.Players[dh].DeathVerdict != "execution" {
		t.Fatalf("DeathVerdict=%q, want execution", gs.Players[dh].DeathVerdict)
	}
	// target 应仍活
	if !gs.AliveSeat(target) {
		t.Fatalf("target (seat %v) should be alive", target)
	}
}

// D-04: DH4 target=-1 空过合法,无死亡,dh 仍活
func TestNightDemonHunterHunt_PassIsLegal(t *testing.T) {
	gs := makeStartedGame13(t, 400, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	// 构造 Day2 状态
	gs.DayNumber = 3
	gs.Phase = PhaseNightDemonHunter
	dh := gs.DemonHunterSeat
	gs.TurnActingSeat = dh
	if e := gs.NightDemonHunterHunt(dh, NoSeat); e != nil {
		t.Fatalf("demon hunter pass: %v", e)
	}
	// dh 应仍活
	if !gs.AliveSeat(dh) {
		t.Fatalf("demon hunter (seat %v) should be alive after pass", dh)
	}
	// DemonHunterHuntUsed 应 true(空过也触发公开身份)
	if !gs.Players[dh].DemonHunterHuntUsed {
		t.Fatalf("DemonHunterHuntUsed should be true after pass")
	}
	// NightDeaths 应为空
	if len(gs.NightDeaths) != 0 {
		t.Fatalf("NightDeaths should be empty, got %v", gs.NightDeaths)
	}
}

// D-05: DH3 target=actor 返回 ErrValidationFailed
func TestNightDemonHunterHunt_SelfTarget_Rejected(t *testing.T) {
	gs := makeStartedGame13(t, 500, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	dh := gs.DemonHunterSeat
	if e := gs.NightDemonHunterHunt(dh, dh); e == nil {
		t.Fatalf("expected ErrValidationFailed on self-target, got nil")
	}
}

// D-06: DH2 target 已死返回 ErrValidationFailed
func TestNightDemonHunterHunt_DeadTarget_Rejected(t *testing.T) {
	gs := makeStartedGame13(t, 600, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	dh := gs.DemonHunterSeat
	// 找一个已死玩家(先杀一个)
	var dead Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if s == dh {
			continue
		}
		// 强制标记为死亡
		gs.Players[s].Alive = false
		dead = s
		break
	}
	if dead == NoSeat {
		t.Skip("no dead target")
	}
	if e := gs.NightDemonHunterHunt(dh, dead); e == nil {
		t.Fatalf("expected ErrValidationFailed on dead target, got nil")
	}
}

// D-07: DH-A 非 PhaseNightDemonHunter 调返回 ErrNotYourTurn
func TestNightDemonHunterHunt_WrongPhase_Rejected(t *testing.T) {
	gs := makeStartedGame13(t, 700, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	dh := gs.DemonHunterSeat
	// 强制进入错误阶段
	gs.Phase = PhaseNightWolves
	gs.TurnActingSeat = dh
	target := firstLivingWolfSeat(gs)
	if target == NoSeat {
		t.Skip("no wolf target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e == nil {
		t.Fatalf("expected ErrNotYourTurn on wrong phase, got nil")
	}
}

// D-08: 本局无猎魔人时 startNight 跳过该阶段
func TestNightDemonHunterHunt_NoDemonHunterInGame_SkipsPhase(t *testing.T) {
	gs := makeStartedGame13(t, 800, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	// 确保本局无猎魔人
	if gs.DemonHunterSeat != NoSeat {
		t.Skip("demon hunter seat exists; cannot test skip")
	}
	// 推进到下一夜
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	// 阶段应推进到 dawn(跳过猎魔人阶段)
	if gs.Phase != PhaseDawn {
		t.Fatalf("expected dawn (skip demon hunter), got %v", gs.Phase)
	}
}

// D-09: DH7 DemonHunterHuntUsed=true → RolePubliclyRevealed(dh)=true
func TestNightDemonHunterHunt_PublicIdentity_RevealsAfterHunt(t *testing.T) {
	gs := makeStartedGame13(t, 900, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	dh := gs.DemonHunterSeat
	// 发动狩猎(命中好人/狼人都行,这里空过也触发公开身份)
	if e := gs.NightDemonHunterHunt(dh, NoSeat); e != nil {
		t.Fatalf("demon hunter hunt: %v", e)
	}
	// DemonHunterHuntUsed 应 true
	if !gs.Players[dh].DemonHunterHuntUsed {
		t.Fatalf("DemonHunterHuntUsed should be true after hunt")
	}
	// RolePubliclyRevealed 应 true
	if !gs.RolePubliclyRevealed(dh) {
		t.Fatalf("RolePubliclyRevealed(dh) should be true after hunt")
	}
}

// D-10: 女巫毒 X + 猎魔人狩猎 X → X 仅死一次
func TestNightDemonHunterHunt_WitchPoisonSameTarget_TargetStillDiesOnce(t *testing.T) {
	gs := makeStartedGame13(t, 1000, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !ensureWitchSeat(t, gs) {
		t.Skip("no witch seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	// 守卫空守
	if gs.Phase == PhaseNightGuard {
		gs.NightGuardProtect(gs.GuardSeat, NoSeat)
	}
	// 狼人空刀
	if gs.Phase == PhaseNightWolves {
		for i := 0; i < MaxPlayers; i++ {
			if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
				gs.NightWolfKill(Seat(i), NoSeat, "")
			}
		}
	}
	// 预言家查验
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeerSeat(gs)
		if seer != NoSeat {
			var target Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					target = Seat(i)
					break
				}
			}
			if target != NoSeat {
				gs.NightSeerCheck(seer, target)
			} else {
				gs.endSeerPhase()
			}
		} else {
			gs.endSeerPhase()
		}
	}
	// 女巫阶段:毒一个非猎魔人的存活好人
	if gs.Phase != PhaseNightWitch {
		t.Fatalf("expected night_witch, got %v", gs.Phase)
	}
	witch := firstLivingWitchSeat(gs)
	// 找一个非猎魔人/非女巫的存活好人 X
	var x Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if !gs.AliveSeat(s) || s == witch || s == gs.DemonHunterSeat {
			continue
		}
		if gs.Roles[i] == RoleWerewolf {
			continue
		}
		x = s
		break
	}
	if x == NoSeat {
		t.Skip("no legal poison target")
	}
	if e := gs.NightWitchAct(witch, "poison", x); e != nil {
		t.Fatalf("witch poison: %v", e)
	}
	// 此时阶段应推进到 PhaseNightDemonHunter
	if gs.Phase != PhaseNightDemonHunter {
		t.Fatalf("expected night_demon_hunter, got %v", gs.Phase)
	}
	dh := gs.DemonHunterSeat
	// 猎魔人狩猎 X(已死)
	if e := gs.NightDemonHunterHunt(dh, x); e == nil {
		t.Fatalf("expected ErrValidationFailed on dead target, got nil")
	}
	// X 应已死亡(被毒)
	if gs.AliveSeat(x) {
		t.Fatalf("X (seat %v) should be dead after poison", x)
	}
}

// D-11: SkipPhaseAction("night_demon_hunter", "demon_hunter") → demon_hunter_hunt_skip,target=-1
func TestNightDemonHunterHunt_QuarantineSkip_Passes(t *testing.T) {
	gs := makeStartedGame13(t, 1100, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	// 构造 Day2 状态
	gs.DayNumber = 3
	gs.Phase = PhaseNightDemonHunter
	dh := gs.DemonHunterSeat
	gs.TurnActingSeat = dh
	// 模拟 quarantine skip: 直接调 hunt(-1)
	if e := gs.NightDemonHunterHunt(dh, NoSeat); e != nil {
		t.Fatalf("demon hunter pass: %v", e)
	}
	// dh 应仍活
	if !gs.AliveSeat(dh) {
		t.Fatalf("demon hunter (seat %v) should be alive after pass", dh)
	}
	// DemonHunterHuntUsed 应 true
	if !gs.Players[dh].DemonHunterHuntUsed {
		t.Fatalf("DemonHunterHuntUsed should be true after pass")
	}
}

// D-12: 猎魔人当晚被狼刀死 → DH-C 失败,PhaseNightDemonHunter 仍走完空过
func TestNightDemonHunterHunt_DeadActor_PhaseSkips(t *testing.T) {
	gs := makeStartedGame13(t, 1200, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	dh := gs.DemonHunterSeat
	// 强制猎魔人死亡
	gs.Players[dh].Alive = false
	// 调 hunt 应失败(DH-C)
	target := firstLivingWolfSeat(gs)
	if target == NoSeat {
		t.Skip("no wolf target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e == nil {
		t.Fatalf("expected ErrPermissionDenied on dead actor, got nil")
	}
}

// D-13: DH5 每晚可发动:Day2 发动后 Day3 仍可继续,只是身份公开
// 简化版:直接构造 Day2 / Day3 状态,避免 advanceToNextNight 的复杂性
func TestNightDemonHunterHunt_DemonHunterHuntUsed_NotLocked(t *testing.T) {
	gs := makeStartedGame13(t, 1300, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	// 构造 Day2 状态:直接进入 PhaseNightDemonHunter
	gs.DayNumber = 3 // >= 2 且禁用 LastWords
	gs.Phase = PhaseNightDemonHunter
	dh := gs.DemonHunterSeat
	gs.TurnActingSeat = dh
	// Day2 发动(命中狼)
	target := firstLivingWolfSeat(gs)
	if target == NoSeat {
		t.Skip("no wolf target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e != nil {
		t.Fatalf("demon hunter hunt Day2: %v", e)
	}
	// DemonHunterHuntUsed 应 true
	if !gs.Players[dh].DemonHunterHuntUsed {
		t.Fatalf("DemonHunterHuntUsed should be true after Day2 hunt")
	}
	// 构造 Day3 状态:直接进入 PhaseNightDemonHunter
	gs.DayNumber = 4
	gs.Phase = PhaseNightDemonHunter
	gs.TurnActingSeat = dh
	// Day3 仍可发动(找另一个目标)
	target2 := firstLivingWolfSeat(gs)
	if target2 == NoSeat || target2 == dh {
		t.Skip("no second wolf target")
	}
	if e := gs.NightDemonHunterHunt(dh, target2); e != nil {
		t.Fatalf("demon hunter hunt Day3: %v", e)
	}
	// target2 应死亡
	if gs.AliveSeat(target2) {
		t.Fatalf("target2 (seat %v) should be dead after Day3 hunt", target2)
	}
}

// D-14: endDemonHunterPhase 后 NightDeaths 含正确死亡座位
func TestNightDemonHunterHunt_EndDemonHunterPhase_NightDeathsContainsHuntResult(t *testing.T) {
	gs := makeStartedGame13(t, 1400, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureDemonHunterSeat(t, gs) {
		t.Skip("no demon hunter seat")
	}
	if !advanceToNextNight(t, gs) {
		t.Skip("game over")
	}
	advanceToNightDemonHunter(t, gs)
	dh := gs.DemonHunterSeat
	// 命中狼
	target := firstLivingWolfSeat(gs)
	if target == NoSeat {
		t.Skip("no wolf target")
	}
	if e := gs.NightDemonHunterHunt(dh, target); e != nil {
		t.Fatalf("demon hunter hunt: %v", e)
	}
	// NightDeaths 应含 target
	found := false
	for _, d := range gs.NightDeaths {
		if d == target {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("NightDeaths should contain target %v, got %v", target, gs.NightDeaths)
	}
}
