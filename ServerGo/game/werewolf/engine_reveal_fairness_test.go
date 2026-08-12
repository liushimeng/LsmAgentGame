package werewolf

// engine_reveal_fairness_test.go — §135 身份公开公平性测试。
//
// 目标规则(线上 APP / 线下标准竞技局统一):
//
//	普通死亡出局,所有人**不会**自动知道死者身份,死者身份牌全程不翻开。
//	法官只公布「几号玩家死亡」,不报角色。仅 4 类事件公开身份:
//	  ① 终局复盘  ② 白痴白天翻牌  ③ 狼人白天自爆  ④ 猎人实际开枪
//
// 覆盖矩阵:
//
//	R-01 狼刀普通死亡      → 不公开
//	R-02 女巫毒杀          → 不公开
//	R-03 白天投票放逐      → 不公开
//	R-04 猎人枪杀的目标    → 不公开(被带走者身份仍保密)
//	R-05 狼人自爆          → 公开
//	R-06 白痴白天翻牌      → 公开
//	R-07 猎人实际开枪      → 公开(开枪者自己)
//	R-08 猎人选择不开枪    → 不公开
//	R-09 终局              → 全员公开
//	R-10 猎人夜间被狼刀    → 获得开枪机会(HunterPendingFrom=="wolf")
//	R-11 猎人被女巫毒杀    → 不获得开枪机会,身份保密
//	R-12 死亡列表脱敏      → all_dead_list_verbose 不带未公开身份
//	R-13 死亡事实仍公开    → alive=false 始终下发

import (
	"testing"
)

// ensureHunterSeat 确保存在猎人座位(13 人随机牌组可能不发猎人)。
// 找一个非狼人 / 非女巫 / 非预言家 / 非守卫的存活座位改成猎人。
func ensureHunterSeat(t *testing.T, gs *GameState) Seat {
	t.Helper()
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleHunter {
			return Seat(i)
		}
	}
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) {
			continue
		}
		switch gs.Roles[i] {
		case RoleWerewolf, RoleWitch, RoleSeer, RoleGuard:
			continue
		}
		gs.Roles[i] = RoleHunter
		gs.Players[i].Role = RoleHunter
		return Seat(i)
	}
	return NoSeat
}

// anyLivingWolf 返回任一存活狼人座位。
func anyLivingWolf(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			return Seat(i)
		}
	}
	return NoSeat
}

// newFairnessGame 起一局 13 人标准局。
func newFairnessGame(t *testing.T, seed int64) *GameState {
	t.Helper()
	return makeStartedGame(t, seed,
		fillSeats("u1", "u2", "u3", "u4", "u5", "u6", "u7",
			"u8", "u9", "u10", "u11", "u12", "u13"))
}

// ── 不公开身份的 4 类普通死亡 ────────────────────────────────

func TestReveal_R01_WolfKillDoesNotRevealRole(t *testing.T) {
	gs := newFairnessGame(t, 4101)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if gs.RolePubliclyRevealed(victim) {
		t.Fatalf("狼刀普通死亡不得公开身份")
	}
}

func TestReveal_R02_WitchPoisonDoesNotRevealRole(t *testing.T) {
	gs := newFairnessGame(t, 4102)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseWitchPoison); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if gs.RolePubliclyRevealed(victim) {
		t.Fatalf("女巫毒杀不得公开身份")
	}
}

func TestReveal_R03_VoteExileDoesNotRevealRole(t *testing.T) {
	gs := newFairnessGame(t, 4103)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseVote); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if gs.RolePubliclyRevealed(victim) {
		t.Fatalf("白天投票放逐不得公开身份(只有白痴翻牌才公开)")
	}
}

func TestReveal_R04_HunterShotVictimStaysHidden(t *testing.T) {
	gs := newFairnessGame(t, 4104)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseHunter); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if gs.RolePubliclyRevealed(victim) {
		t.Fatalf("被猎人枪杀者的身份不得公开(只有开枪的猎人自己亮身份)")
	}
}

// ── 公开身份的 4 类例外 ──────────────────────────────────────

func TestReveal_R05_WolfSuicideRevealsRole(t *testing.T) {
	gs := newFairnessGame(t, 4105)
	wolf := anyLivingWolf(gs)
	if wolf == NoSeat {
		t.Skip("no living wolf")
	}
	if e := gs.killPlayer(wolf, DeathCauseSuicide); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if !gs.RolePubliclyRevealed(wolf) {
		t.Fatalf("狼人自爆必须公开身份")
	}
}

func TestReveal_R06_IdiotRevealFlagRevealsRole(t *testing.T) {
	gs := newFairnessGame(t, 4106)
	seat := Seat(-1)
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] != RoleWerewolf {
			seat = Seat(i)
			break
		}
	}
	if seat == NoSeat {
		t.Skip("no seat")
	}
	gs.Roles[seat] = RoleIdiot
	gs.Players[seat].IdiotRevealed = true
	if !gs.RolePubliclyRevealed(seat) {
		t.Fatalf("白痴白天翻牌必须公开身份")
	}
}

func TestReveal_R07_HunterFiredRevealsRole(t *testing.T) {
	gs := newFairnessGame(t, 4107)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat")
	}
	if gs.RolePubliclyRevealed(hunter) {
		t.Fatalf("开枪前猎人身份不应公开")
	}
	gs.Players[hunter].HunterFired = true
	if !gs.RolePubliclyRevealed(hunter) {
		t.Fatalf("猎人实际开枪后必须公开身份")
	}
}

func TestReveal_R08_HunterDeclinedToShootStaysHidden(t *testing.T) {
	gs := newFairnessGame(t, 4108)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat")
	}
	// 猎人被放逐 → 待开枪 → 选择不开枪。
	if e := gs.killPlayer(hunter, DeathCauseVote); e != nil {
		t.Fatalf("kill: %v", e)
	}
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "vote"
	setPhaseAndDeadline(gs, PhaseHunterShoot)
	if e := gs.HunterShoot(hunter, NoSeat); e != nil {
		t.Fatalf("hunter shoot(none): %v", e)
	}
	if gs.Players[hunter].HunterFired {
		t.Fatalf("选择不开枪不得置 HunterFired")
	}
	if gs.RolePubliclyRevealed(hunter) {
		t.Fatalf("猎人主动选择不开枪 → 未亮身份,身份必须保持隐藏")
	}
}

func TestReveal_R09_GameOverRevealsEveryone(t *testing.T) {
	gs := newFairnessGame(t, 4109)
	gs.Status = "over"
	for i := 0; i < MaxPlayers; i++ {
		if gs.Seats[i] == "" {
			continue
		}
		if !gs.RolePubliclyRevealed(Seat(i)) {
			t.Fatalf("终局后座位 %d 身份必须公开(复盘)", i)
		}
	}
}

// ── 猎人夜间开枪链路(§135 新接线) ───────────────────────────

func TestReveal_R10_HunterKilledAtNightGetsToShoot(t *testing.T) {
	gs := newFairnessGame(t, 4110)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat")
	}
	// 直接构造"狼刀猎人"的夜间结算。
	gs.WolfKillTarget = hunter
	gs.WitchSavedTarget = NoSeat
	gs.GuardProtectTarget = NoSeat
	gs.endWitchPhase()

	if gs.AliveSeat(hunter) {
		t.Fatalf("猎人应已被狼刀击杀")
	}
	if !gs.HunterPendingShoot {
		t.Fatalf("猎人夜间被狼刀必须获得开枪机会(HunterPendingShoot)")
	}
	if gs.HunterPendingFrom != "wolf" {
		t.Fatalf("HunterPendingFrom = %q, want \"wolf\"", gs.HunterPendingFrom)
	}
}

func TestReveal_R11_PoisonedHunterCannotShootAndStaysHidden(t *testing.T) {
	gs := newFairnessGame(t, 4111)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat")
	}
	if e := gs.killPlayer(hunter, DeathCauseWitchPoison); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if gs.HunterPendingShoot {
		t.Fatalf("被女巫毒杀的猎人不得获得开枪机会")
	}
	if gs.RolePubliclyRevealed(hunter) {
		t.Fatalf("被毒死的猎人身份必须保持隐藏")
	}
}

// ── 视图层脱敏 ───────────────────────────────────────────────

func TestReveal_R12_DeadListsHideUnrevealedRoles(t *testing.T) {
	gs := newFairnessGame(t, 4112)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
		t.Fatalf("kill: %v", e)
	}

	// 每个存活玩家视角 + 观战者视角都不得看到死者身份。
	viewers := []int{-1, 0, 1, 2}
	for _, v := range viewers {
		cs := BuildClientState("r", gs.Seats, v, gs)
		if int(victim) != v {
			if cs.Players[victim].Role != "" || cs.Players[victim].RoleRevealed {
				t.Fatalf("viewer=%d 不得看到死者 players[].role", v)
			}
		}
		for _, d := range cs.AllDeadListVerbose {
			if d.Seat == int(victim) && d.Role != "" {
				t.Fatalf("viewer=%d: all_dead_list_verbose 泄露死者身份 %q", v, d.Role)
			}
		}
	}
}

func TestReveal_R13_DeathFactRemainsPublic(t *testing.T) {
	gs := newFairnessGame(t, 4113)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
		t.Fatalf("kill: %v", e)
	}
	cs := BuildClientState("r", gs.Seats, 1, gs)
	if cs.Players[victim].Alive {
		t.Fatalf("死亡事实必须公开(法官公布几号死亡)")
	}
	// verdict / cause 仍然下发(处决 vs 死亡的二分语义 §123)。
	found := false
	for _, d := range cs.AllDeadListVerbose {
		if d.Seat == int(victim) {
			found = true
			if d.Verdict == "" {
				t.Fatalf("verdict 必须保留(§123 处决/死亡二分)")
			}
		}
	}
	if !found {
		t.Fatalf("死者必须出现在 all_dead_list_verbose 中(座位号公开)")
	}
}

// TestReveal_R14_SuicidedWolfRoleVisibleInView 端到端:自爆狼在视图层可见身份。
func TestReveal_R14_SuicidedWolfRoleVisibleInView(t *testing.T) {
	gs := newFairnessGame(t, 4114)
	wolf := anyLivingWolf(gs)
	if wolf == NoSeat {
		t.Skip("no living wolf")
	}
	if e := gs.killPlayer(wolf, DeathCauseSuicide); e != nil {
		t.Fatalf("kill: %v", e)
	}
	cs := BuildClientState("r", gs.Seats, 1, gs)
	if !cs.Players[wolf].RoleRevealed || cs.Players[wolf].Role != "werewolf" {
		t.Fatalf("自爆狼身份必须在视图层公开, got revealed=%v role=%q",
			cs.Players[wolf].RoleRevealed, cs.Players[wolf].Role)
	}
}

// TestReveal_R15_HunterNightShootResumesDay 端到端:猎人夜间被刀 → 开枪 →
// 回到白天流程(而不是被 advanceDay 直接吞掉整个白天跳进下一夜)。
func TestReveal_R15_HunterNightShootResumesDay(t *testing.T) {
	gs := newFairnessGame(t, 4115)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat")
	}
	dayBefore := gs.DayNumber

	gs.WolfKillTarget = hunter
	gs.WitchSavedTarget = NoSeat
	gs.GuardProtectTarget = NoSeat
	gs.endWitchPhase()

	if !gs.HunterPendingShoot {
		t.Fatalf("猎人夜死应待开枪")
	}
	// 推进遗言队列直到进入开枪阶段。
	for i := 0; i < 20 && gs.Phase == PhaseDeathLyric; i++ {
		cur := gs.DeathLyricCurrent
		if cur == NoSeat {
			break
		}
		_ = gs.SkipLastWords(cur)
	}
	if gs.Status == "over" {
		t.Skip("game ended early")
	}
	if gs.Phase != PhaseHunterShoot {
		t.Fatalf("phase = %v, want hunter_shoot", gs.Phase)
	}

	// 开枪带走一个存活目标。
	var target Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) {
			target = Seat(i)
			break
		}
	}
	if target == NoSeat {
		t.Skip("no target")
	}
	if e := gs.HunterShoot(hunter, target); e != nil {
		t.Fatalf("hunter shoot: %v", e)
	}
	if !gs.Players[hunter].HunterFired {
		t.Fatalf("开枪后必须置 HunterFired")
	}
	if !gs.RolePubliclyRevealed(hunter) {
		t.Fatalf("开枪后猎人身份必须公开")
	}
	if gs.Status == "over" {
		return
	}
	// 推完遗言后应停在白天(sheriff/speak),而**不是**跳进下一夜。
	for i := 0; i < 20 && gs.Phase == PhaseDeathLyric; i++ {
		cur := gs.DeathLyricCurrent
		if cur == NoSeat {
			break
		}
		_ = gs.SkipLastWords(cur)
	}
	switch gs.Phase {
	case PhaseSheriff, PhaseSpeak, PhaseDawn:
		// 正确:仍在本白天流程内。
	default:
		t.Fatalf("猎人夜间开枪后应回到白天流程, got phase=%v", gs.Phase)
	}
	if gs.DayNumber != dayBefore {
		t.Fatalf("不应递增 DayNumber(那意味着白天被吞掉): %d → %d", dayBefore, gs.DayNumber)
	}
}
