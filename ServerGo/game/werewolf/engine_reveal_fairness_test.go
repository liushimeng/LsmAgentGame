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

// ─────────────────────────────────────────────────────────────
// §20260830-01 「死亡亮身份」模式(设计文档 §10.1 R-14~R-23 后端部分)。
//
// 命名说明:本文件已有历史 R-14/R-15(自爆狼视图 / 猎人夜枪回白天),故本批
// 用 TestRevealOn_Rxx(开启模式)与 TestRevealMode_Rxx(开/关对照)前缀,
// 编号仍对齐设计文档 §10.1。
// ─────────────────────────────────────────────────────────────

// newRevealModeGame 起一局并显式开启「死亡亮身份」开关。
func newRevealModeGame(t *testing.T, seed int64) *GameState {
	t.Helper()
	gs := newFairnessGame(t, seed)
	gs.RevealRoleOnDeath = true
	return gs
}

// TestRevealOn_R14_WolfKillRevealsRole 开启:狼刀死亡 → 公开,角色名正确。
func TestRevealOn_R14_WolfKillRevealsRole(t *testing.T) {
	gs := newRevealModeGame(t, 4201)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if !gs.RolePubliclyRevealed(victim) {
		t.Fatalf("死亡亮身份开启:狼刀死亡必须公开身份")
	}
	if got := publicRoleName(gs, victim); got != gs.Roles[victim].String() {
		t.Fatalf("公开角色名 = %q, want %q", got, gs.Roles[victim].String())
	}
}

// TestRevealOn_R15_WitchPoisonRevealsRole 开启:女巫毒杀 → 公开。
func TestRevealOn_R15_WitchPoisonRevealsRole(t *testing.T) {
	gs := newRevealModeGame(t, 4202)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseWitchPoison); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if !gs.RolePubliclyRevealed(victim) {
		t.Fatalf("死亡亮身份开启:毒杀死亡必须公开身份")
	}
}

// TestRevealOn_R16_VoteExecutionRevealsRole 开启:白天投票放逐 → 公开。
func TestRevealOn_R16_VoteExecutionRevealsRole(t *testing.T) {
	gs := newRevealModeGame(t, 4203)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseVote); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if !gs.RolePubliclyRevealed(victim) {
		t.Fatalf("死亡亮身份开启:投票放逐必须公开身份")
	}
}

// TestRevealOn_R17_HunterTargetRevealsRole 开启:被猎人带走者 → 公开
// (现行 R-04 的镜像:关闭时被带走者身份保密)。
func TestRevealOn_R17_HunterTargetRevealsRole(t *testing.T) {
	gs := newRevealModeGame(t, 4204)
	hunter := ensureHunterSeat(t, gs)
	if hunter == NoSeat {
		t.Skip("no hunter seat")
	}
	// 目标任取一个存活非猎人座位(狼也可被带走,HunterShoot 仅要求 target 存活;
	// ensureHunterSeat 可能已把唯一的非神职座位改成猎人,故不用 firstLivingNonWolfNonSeer)。
	var victim Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != hunter && gs.HasActorAt(Seat(i)) && gs.AliveSeat(Seat(i)) {
			victim = Seat(i)
			break
		}
	}
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	// HunterShoot 前置:猎人处于待开枪状态(夜间被刀)。
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "wolf"
	if e := gs.HunterShoot(hunter, victim); e != nil {
		t.Fatalf("hunter shoot: %v", e)
	}
	if gs.AliveSeat(victim) {
		t.Fatalf("被猎人带走者应已死亡")
	}
	if !gs.RolePubliclyRevealed(victim) {
		t.Fatalf("死亡亮身份开启:被猎人带走者必须公开身份")
	}
}

// TestRevealOn_R18_IdiotNightKilledRevealsRole 开启:白痴夜间被刀 → 公开
// (身份=白痴;夜间死亡不触发 ② 翻牌,但 ⑦ 覆盖)。
func TestRevealOn_R18_IdiotNightKilledRevealsRole(t *testing.T) {
	gs := newRevealModeGame(t, 4205)
	// 找/造一个白痴座位。
	idiot := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleIdiot {
			idiot = Seat(i)
			break
		}
	}
	if idiot == NoSeat {
		for i := 0; i < MaxPlayers; i++ {
			if gs.AliveSeat(Seat(i)) && gs.Roles[i] != RoleWerewolf && gs.Roles[i] != RoleSeer &&
				gs.Roles[i] != RoleWitch && gs.Roles[i] != RoleHunter && gs.Roles[i] != RoleGuard {
				gs.Roles[i] = RoleIdiot
				gs.Players[i].Role = RoleIdiot
				idiot = Seat(i)
				break
			}
		}
	}
	if idiot == NoSeat {
		t.Skip("no idiot seat")
	}
	if e := gs.killPlayer(idiot, DeathCauseWolf); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if !gs.RolePubliclyRevealed(idiot) {
		t.Fatalf("死亡亮身份开启:白痴夜间被刀必须公开身份")
	}
	if got := publicRoleName(gs, idiot); got != "idiot" {
		t.Fatalf("公开角色名 = %q, want \"idiot\"", got)
	}
}

// TestRevealOn_R19_DuelAndDemonHunterRevealsRole 开启:骑士决斗 /
// 猎魔人误杀死亡 → 公开(⑥ 白名单之外的死者在 ⑦ 下也公开)。
func TestRevealOn_R19_DuelAndDemonHunterRevealsRole(t *testing.T) {
	gs := newRevealModeGame(t, 4206)
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	// duel 死因(verdict=execution)。
	if e := gs.killPlayer(victim, DeathCauseDuel); e != nil {
		t.Fatalf("kill duel: %v", e)
	}
	if !gs.RolePubliclyRevealed(victim) {
		t.Fatalf("死亡亮身份开启:决斗死亡必须公开身份")
	}
	// demon_hunter_misjudge 死因(verdict=execution)。
	other := firstLivingNonWolfNonSeer(gs)
	if other == NoSeat {
		t.Skip("no second victim")
	}
	if e := gs.killPlayer(other, DeathCauseDemonHunterMisjudge); e != nil {
		t.Fatalf("kill misjudge: %v", e)
	}
	if !gs.RolePubliclyRevealed(other) {
		t.Fatalf("死亡亮身份开启:猎魔人误杀死亡必须公开身份")
	}
}

// TestRevealMode_R20_Disabled_ZeroRegression 显式关闭(gs.RevealRoleOnDeath=false)
// 时普通死亡仍不公开 —— 与 R-01~R-03/R-12 断言完全一致,零回归。
func TestRevealMode_R20_Disabled_ZeroRegression(t *testing.T) {
	for _, tc := range []struct {
		cause string
		name  string
	}{
		{DeathCauseWolf, "wolf"},
		{DeathCauseWitchPoison, "witch_poison"},
		{DeathCauseVote, "vote"},
		{DeathCauseHunter, "hunter"},
	} {
		gs := newFairnessGame(t, 4207)
		gs.RevealRoleOnDeath = false
		victim := firstLivingNonWolfNonSeer(gs)
		if victim == NoSeat {
			t.Skip("no suitable victim")
		}
		if e := gs.killPlayer(victim, tc.cause); e != nil {
			t.Fatalf("kill %s: %v", tc.name, e)
		}
		if gs.RolePubliclyRevealed(victim) {
			t.Fatalf("关闭开关:%s 普通死亡不得公开身份(§135 竞技规则零回归)", tc.name)
		}
		cs := BuildClientState("r", gs.Seats, 0, gs)
		if int(victim) != 0 && (cs.Players[victim].Role != "" || cs.Players[victim].RoleRevealed) {
			t.Fatalf("关闭开关:%s 死者 players[].role 不得泄露", tc.name)
		}
	}
}

// TestRevealMode_R21_AliveIdiotNotDoubleRevealed 白痴翻牌免死存活
// (Alive=true && DeathCause=="")不进 ⑦,由 ② 覆盖;关闭 ⑦ 后 ② 仍独立生效。
func TestRevealMode_R21_AliveIdiotNotDoubleRevealed(t *testing.T) {
	gs := newFairnessGame(t, 4208)
	idiot := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleIdiot {
			idiot = Seat(i)
			break
		}
	}
	if idiot == NoSeat {
		for i := 0; i < MaxPlayers; i++ {
			if gs.AliveSeat(Seat(i)) && gs.Roles[i] != RoleWerewolf && gs.Roles[i] != RoleSeer &&
				gs.Roles[i] != RoleWitch && gs.Roles[i] != RoleHunter && gs.Roles[i] != RoleGuard {
				gs.Roles[i] = RoleIdiot
				gs.Players[i].Role = RoleIdiot
				idiot = Seat(i)
				break
			}
		}
	}
	if idiot == NoSeat {
		t.Skip("no idiot seat")
	}
	// 白天翻牌免死:Alive 保持 true,DeathCause 为空。
	gs.Players[idiot].IdiotRevealed = true
	if !gs.RolePubliclyRevealed(idiot) {
		t.Fatalf("白痴翻牌(②)必须独立于 ⑦ 生效")
	}
	if gs.Players[idiot].DeathCause != "" || !gs.Players[idiot].Alive {
		t.Fatalf("白痴翻牌免死:Alive=true 且 DeathCause==\"\" 前置失败")
	}
	// 死亡公开 drain(W03 同款语义)必须排除该存活座位 —— 由 wiring 测试覆盖;
	// 此处断言判定面:开启 ⑦ 也不改变(仍由 ② 覆盖,无重复副作用)。
	gs.RevealRoleOnDeath = true
	if !gs.RolePubliclyRevealed(idiot) || publicRoleName(gs, idiot) != "idiot" {
		t.Fatalf("白痴翻牌公开身份不应被 ⑦ 干扰")
	}
}

// TestRevealMode_R22_AllViewChannelsConsistent 同一 gs 下五通道一致(开/关两轮):
// players[].role / dead_list / all_dead_list_verbose / last_night_deaths_verbose
// 全部经 publicRoleName → RolePubliclyRevealed 单点判定;REST PublicPlayerState
// 亦走同一判定(room_state.go GetPublicPlayerStates,本测试以同一函数断言替代)。
func TestRevealMode_R22_AllViewChannelsConsistent(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		gs := newFairnessGame(t, 4209)
		gs.RevealRoleOnDeath = enabled
		victim := firstLivingNonWolfNonSeer(gs)
		if victim == NoSeat {
			t.Skip("no suitable victim")
		}
		if e := gs.killPlayer(victim, DeathCauseWolf); e != nil {
			t.Fatalf("kill: %v", e)
		}
		gs.LastNightDeaths = []Seat{victim}

		wantRole := ""
		if enabled {
			wantRole = gs.Roles[victim].String()
		}
		// 通道 1:players[].role(每个非本人 viewer 视角)。
		for _, v := range []int{-1, 0, 1, 2} {
			cs := BuildClientState("r", gs.Seats, v, gs)
			if int(victim) == v {
				continue
			}
			if cs.Players[victim].Role != wantRole {
				t.Fatalf("enabled=%v viewer=%d players[].role=%q want %q",
					enabled, v, cs.Players[victim].Role, wantRole)
			}
		}
		// 通道 2:dead_list(遗言阶段)。
		for _, d := range buildDeadListLocked(gs) {
			if d.Seat == int(victim) && d.Role != wantRole {
				t.Fatalf("enabled=%v dead_list role=%q want %q", enabled, d.Role, wantRole)
			}
		}
		// 通道 3:all_dead_list_verbose。
		for _, d := range buildAllDeadListLocked(gs) {
			if d.Seat == int(victim) && d.Role != wantRole {
				t.Fatalf("enabled=%v all_dead_list_verbose role=%q want %q", enabled, d.Role, wantRole)
			}
		}
		// 通道 4:last_night_deaths_verbose(黎明公告)。
		for _, d := range buildDeadListForSeatsLocked(gs, gs.LastNightDeaths) {
			if d.Seat == int(victim) && d.Role != wantRole {
				t.Fatalf("enabled=%v last_night_deaths_verbose role=%q want %q", enabled, d.Role, wantRole)
			}
		}
		// 通道 5:单点判定(REST PublicPlayerState 的同一事实来源)。
		if got := gs.RolePubliclyRevealed(victim); got != enabled {
			t.Fatalf("enabled=%v RolePubliclyRevealed=%v", enabled, got)
		}
	}
}

// TestRevealMode_R23_DeathFactIndependence 关闭时死亡事实(alive=false /
// cause / verdict)仍照常下发,仅 role 脱敏。
func TestRevealMode_R23_DeathFactIndependence(t *testing.T) {
	gs := newFairnessGame(t, 4210)
	gs.RevealRoleOnDeath = false
	victim := firstLivingNonWolfNonSeer(gs)
	if victim == NoSeat {
		t.Skip("no suitable victim")
	}
	if e := gs.killPlayer(victim, DeathCauseVote); e != nil {
		t.Fatalf("kill: %v", e)
	}
	cs := BuildClientState("r", gs.Seats, 2, gs)
	if cs.Players[victim].Alive {
		t.Fatalf("死亡事实必须公开")
	}
	found := false
	for _, d := range cs.AllDeadListVerbose {
		if d.Seat == int(victim) {
			found = true
			if d.Cause != DeathCauseVote || d.Verdict != DeathVerdictExecution {
				t.Fatalf("cause/verdict 必须保留, got %q/%q", d.Cause, d.Verdict)
			}
			if d.Role != "" {
				t.Fatalf("关闭开关:role 必须脱敏")
			}
		}
	}
	if !found {
		t.Fatalf("死者必须出现在 all_dead_list_verbose")
	}
}
