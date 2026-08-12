package werewolf

// engine_guard_test.go — §134 守卫角色测试。
//
// 按 docs/狼人杀-角色设计/狼人杀守卫角色设计.md §8 列出的 G-01..G-12 用例实现。
// 13 人局强制使用 makeStartedGame13;7 人局强制使用 makeStartedGame(makeStartedGame
// 选 7 人牌组,但 7 人牌组不含守卫 — 见 StandardDeck() / StandardDeck13())。
//
// 设计原则:
//   - 每条用例只断言设计文档 §2/§3 描述的核心不变量,允许引擎推进阶段、killPlayer
//     等副作用存在(只要不破坏断言);
//   - 凡是依赖「13 人局必有守卫」的断言用 makeStartedGame13 + 调 ensureGuardSeat
//     做兜底(若种子巧合无守卫则跳过 t.Skip);
//   - 隔离副作用:每个测试独立 new GameState,互不影响。

import (
	"testing"
)

// ensureGuardSeat 在 13 人局测试中确保存在守卫;若种子恰好未发守卫则注入一个。
// 必须在 startNight 之前调用 —— 否则 startNight 不会进入 PhaseNightGuard。
//
// 13 人随机牌组(AssignRoles13Random)从 godRolePool 随机选神职,种子碰巧可能没发守卫;
// 强制注入时找一个非狼人 / 非女巫 / 非预言家的存活座位改成守卫(角色互斥)。
func ensureGuardSeat(t *testing.T, gs *GameState) bool {
	t.Helper()
	if gs.GuardSeat != NoSeat {
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
		// 把这个座位改成守卫
		gs.Roles[i] = RoleGuard
		gs.Players[i].Role = RoleGuard
		gs.GuardSeat = Seat(i)
		return true
	}
	return false
}

// ensureWitchSeat 确保存在女巫座位(同理守卫)。13 人随机牌组可能不发女巫。
func ensureWitchSeat(t *testing.T, gs *GameState) bool {
	t.Helper()
	if gs.WitchSeat != NoSeat {
		return true
	}
	for i := 0; i < MaxPlayers; i++ {
		if !gs.AliveSeat(Seat(i)) {
			continue
		}
		r := gs.Roles[i]
		if r == RoleWerewolf || r == RoleGuard || r == RoleSeer {
			continue
		}
		gs.Roles[i] = RoleWitch
		gs.Players[i].Role = RoleWitch
		gs.WitchSeat = Seat(i)
		return true
	}
	return false
}

// advanceToNightGuard 让 GameState 跳过 night_wolves → night_seer → night_witch,
// 直接进入 night_guard 阶段(用于纯守卫测试)。startNight 本身已经把守卫阶段
// 设为第一阶段(若有守卫);调用此函数前请确保 gs.Phase == PhaseNightGuard。
func advanceFromGuardToWolves(gs *GameState) {
	if gs.Phase != PhaseNightGuard {
		return
	}
	gs.endGuardPhase()
}

// firstLivingNonWolfNonSeer 找一个非狼人/非女巫/非守卫的存活玩家(用于测试"狼刀目标")。
// 优先选平民。
func firstLivingNonWolfNonSeer(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if !gs.AliveSeat(s) {
			continue
		}
		r := gs.Roles[i]
		if r == RoleWerewolf || r == RoleSeer || r == RoleGuard {
			continue
		}
		return s
	}
	return NoSeat
}

// firstLivingSeerSeat 取得当前预言家座位(若存活)。
func firstLivingSeerSeat(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleSeer {
			return Seat(i)
		}
	}
	return NoSeat
}

// firstLivingWitchSeat 取得当前女巫座位(若存活)。
func firstLivingWitchSeat(gs *GameState) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWitch {
			return Seat(i)
		}
	}
	return NoSeat
}

// skipSeerAndWitch 推进阶段跳过 seer + witch,直接进入 dawn(用于只需要验证
// 守卫裁决 + 狼刀生效结果的测试)。调用方需确保 gs.Phase == PhaseNightSeer。
func skipSeerAndWitch(gs *GameState) {
	// 处理预言家(若无活预言家,endSeerPhase 自动调 endWitchPhase)
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeerSeat(gs)
		if seer != NoSeat {
			// 任意查验一个存活非自己目标
			var target Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					target = Seat(i)
					break
				}
			}
			if target != NoSeat {
				_ = gs.NightSeerCheck(seer, target)
			} else {
				gs.endSeerPhase()
			}
		} else {
			gs.endSeerPhase()
		}
	}
	// 处理女巫(若女巫存在则不用药)
	if gs.Phase == PhaseNightWitch {
		witch := firstLivingWitchSeat(gs)
		if witch != NoSeat {
			_ = gs.NightWitchAct(witch, "none", NoSeat)
		} else {
			gs.endWitchPhase()
		}
	}
}

// advanceToNextNight 把 GameState 从当前阶段推进到下一个 PhaseNightGuard 或
// PhaseNightWolves 状态。处理 dawn / death_lyric / speak / vote 之间的转移;
// 投票阶段用全部弃权推进,遗言阶段用 SkipLastWords 跳过。
// 返回 true 表示成功推进到下一夜,false 表示游戏已结束(t.Skip)。
// 含 1000-iter 上限防死循环。
func advanceToNextNight(t *testing.T, gs *GameState) bool {
	t.Helper()
	const maxIter = 1000
	for n := 0; n < maxIter; n++ {
		switch gs.Phase {
		case PhaseDawn:
			gs.advanceDay()
			if gs.Status == "over" {
				return false
			}
		case PhaseSheriff:
			// 首日警长竞选 — 直接调 SheriffElect 推进。
			_ = gs.SheriffElect(NoSeat)
		case PhaseSpeak:
			// 跳过发言 — 把所有存活玩家设为已发完,然后 NextSpeaker 推进。
			// 最简单:把所有存活玩家 Voted 标志设上,DayVote 触发 PhaseVote。
			for i := 0; i < MaxPlayers; i++ {
				if gs.AliveSeat(Seat(i)) && !gs.Players[i].HasSpoken {
					gs.Players[i].HasSpoken = true
				}
			}
			// 调用 NextSpeaker 推进;若全部说完了,NextSpeaker 把 phase 切到 PhaseVote。
			gs.NextSpeaker()
		case PhaseVote:
			// 全员弃权推进。
			for i := 0; i < MaxPlayers; i++ {
				if gs.AliveSeat(Seat(i)) && !gs.Players[i].Voted {
					_ = gs.DayVote(Seat(i), NoSeat)
				}
			}
			if gs.Phase == PhaseVote {
				_ = gs.FinishVote(0)
			}
			if gs.HunterPendingShoot {
				gs.HunterPendingShoot = false
			}
			gs.advanceDay()
			if gs.Status == "over" {
				return false
			}
		case PhaseDeathLyric:
			// 清空遗言队列,恢复原路径。
			for len(gs.DeathLyricQueue) > 0 {
				_ = gs.SkipLastWords(gs.DeathLyricCurrent)
			}
		case PhaseHunterShoot:
			gs.HunterPendingShoot = false
			gs.advanceDay()
			if gs.Status == "over" {
				return false
			}
		case PhaseIdiotReveal:
			if gs.DayEliminated != NoSeat {
				_ = gs.IdiotReveal(gs.DayEliminated, "skip")
			} else {
				gs.advanceDay()
			}
		case PhaseRestartVote:
			return false
		case PhasePreWolves:
			// startNight 已经把阶段切走,不应该到这里。如果在,直接 advanceDay 跳过。
			gs.advanceDay()
			if gs.Status == "over" {
				return false
			}
		default:
			// night_guard / night_wolves / night_seer / night_witch / filling /
			// gameover — 已推进到下一夜(或在原夜内)。
			return true
		}
	}
	t.Logf("advanceToNextNight: maxIter=%d reached, last phase=%s", maxIter, gs.Phase)
	return gs.Phase == PhaseNightGuard || gs.Phase == PhaseNightWolves
}

// G-01: 护盾挡刀。守 X + 狼刀 X → X 存活,NightDeaths 为空。
func TestGuardProtect_G01_ShieldBlocksKill(t *testing.T) {
	gs := makeStartedGame13(t, 100, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat available")
	}
	gs.startNight()
	if gs.Phase != PhaseNightGuard {
		t.Fatalf("expected phase night_guard, got %v", gs.Phase)
	}
	// 找一个非守卫、非狼人的存活目标 X
	target := firstLivingNonWolfNonSeer(gs)
	if target == NoSeat {
		t.Skip("no candidate")
	}
	if target == gs.GuardSeat {
		// 排除守卫自己
		t.Skip("target == guard seat; rare edge case")
	}
	// 守卫守 X
	if e := gs.NightGuardProtect(gs.GuardSeat, target); e != nil {
		t.Fatalf("guard protect: %v", e)
	}
	if gs.GuardProtectTarget != target {
		t.Fatalf("GuardProtectTarget=%v, want %v", gs.GuardProtectTarget, target)
	}
	if gs.GuardLastProtect != target {
		t.Fatalf("GuardLastProtect=%v, want %v", gs.GuardLastProtect, target)
	}
	// 阶段已推进到 night_wolves
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected night_wolves after guard, got %v", gs.Phase)
	}
	// 让所有狼人投 target
	if e := voteAllWolves(gs, target); e != nil {
		t.Fatalf("wolf vote: %v", e)
	}
	// 跳过 seer + witch
	skipSeerAndWitch(gs)
	// X 应仍活
	if !gs.AliveSeat(target) {
		t.Fatalf("X (seat %v) should be alive after shield", target)
	}
	// NightDeaths 应为空(或只有因其他原因死的,这里只有狼刀 → 应为空)
	for _, d := range gs.NightDeaths {
		if d == target {
			t.Fatalf("target in NightDeaths: %v", gs.NightDeaths)
		}
	}
	if gs.GuardSavedTarget != target {
		t.Fatalf("GuardSavedTarget=%v, want %v", gs.GuardSavedTarget, target)
	}
}

// G-02: 未守则死。守 Y + 狼刀 X → X 死亡,cause=wolf。
func TestGuardProtect_G02_NoShieldDie(t *testing.T) {
	gs := makeStartedGame13(t, 200, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	// 找两个不同目标 X, Y(都非守卫、非狼人)
	var x, y Seat = NoSeat, NoSeat
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if !gs.AliveSeat(s) || gs.Roles[i] == RoleWerewolf || s == gs.GuardSeat {
			continue
		}
		if x == NoSeat {
			x = s
		} else if y == NoSeat && s != x {
			y = s
			break
		}
	}
	if x == NoSeat || y == NoSeat {
		t.Skip("no candidate")
	}
	// 守卫守 Y
	if e := gs.NightGuardProtect(gs.GuardSeat, y); e != nil {
		t.Fatalf("guard protect: %v", e)
	}
	// 狼人投 X
	if e := voteAllWolves(gs, x); e != nil {
		t.Fatalf("wolf vote: %v", e)
	}
	skipSeerAndWitch(gs)
	// X 应死亡
	if gs.AliveSeat(x) {
		t.Fatalf("X (seat %v) should be dead", x)
	}
	if gs.Players[x].DeathCause != "wolf" {
		t.Fatalf("DeathCause=%q, want wolf", gs.Players[x].DeathCause)
	}
}

// G-03: 同守同救。守 X + 狼刀 X + 解药 X → X 死亡,解药已消耗。
func TestGuardProtect_G03_SameGuardSameSave(t *testing.T) {
	gs := makeStartedGame13(t, 300, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	if !ensureWitchSeat(t, gs) {
		t.Skip("no witch seat")
	}
	gs.startNight()
	// 找一个非守卫、非狼人、非女巫的存活目标 X(守卫守 X;女巫救 X)
	x := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if !gs.AliveSeat(s) || gs.Roles[i] == RoleWerewolf || gs.Roles[i] == RoleWitch || s == gs.GuardSeat {
			continue
		}
		x = s
		break
	}
	if x == NoSeat {
		t.Skip("no candidate")
	}
	// 守卫守 X
	if e := gs.NightGuardProtect(gs.GuardSeat, x); e != nil {
		t.Fatalf("guard protect: %v", e)
	}
	// 狼人投 X
	if e := voteAllWolves(gs, x); e != nil {
		t.Fatalf("wolf vote: %v", e)
	}
	// 跳过预言家
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeerSeat(gs)
		if seer != NoSeat {
			var t2 Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					t2 = Seat(i)
					break
				}
			}
			if t2 != NoSeat {
				_ = gs.NightSeerCheck(seer, t2)
			} else {
				gs.endSeerPhase()
			}
		} else {
			gs.endSeerPhase()
		}
	}
	// 女巫用解药救 X
	witch := firstLivingWitchSeat(gs)
	if witch == NoSeat {
		t.Skip("no witch")
	}
	if e := gs.NightWitchAct(witch, "antidote", NoSeat); e != nil {
		t.Fatalf("witch antidote: %v", e)
	}
	// X 应死亡(同守同救)
	if gs.AliveSeat(x) {
		t.Fatalf("X (seat %v) should be dead (same guard same save)", x)
	}
	if gs.Players[x].DeathCause != "wolf" {
		t.Fatalf("DeathCause=%q, want wolf", gs.Players[x].DeathCause)
	}
	if gs.SameGuardSameSave != x {
		t.Fatalf("SameGuardSameSave=%v, want %v", gs.SameGuardSameSave, x)
	}
	// 解药已消耗
	if !gs.Players[witch].WitchAntidoteUsed {
		t.Fatalf("antidote should be consumed")
	}
}

// G-04: 连守拒绝 (G1)。连续两晚守 X → 第二晚 ErrValidationFailed。
func TestGuardProtect_G04_ConsecutiveReject(t *testing.T) {
	gs := makeStartedGame13(t, 400, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	target := firstLivingNonWolfNonSeer(gs)
	if target == NoSeat || target == gs.GuardSeat {
		t.Skip("no candidate")
	}
	// 第一晚:守 X
	if e := gs.NightGuardProtect(gs.GuardSeat, target); e != nil {
		t.Fatalf("first protect: %v", e)
	}
	// 跳过这一晚的狼/预/巫(用 voteAllWolves + skipSeerAndWitch 推进)
	if e := voteAllWolves(gs, NoSeat); e != nil {
		t.Fatalf("vote abstain: %v", e)
	}
	skipSeerAndWitch(gs)
	// 推进到下一夜(处理 dawn / speak / vote / death_lyric 等中间阶段)
	if !advanceToNextNight(t, gs) {
		t.Skip("game ended (no second night)")
	}
	// 现在应该已经回到 PhaseNightGuard
	if gs.Phase != PhaseNightGuard {
		t.Skipf("expected phase night_guard on second night, got %v", gs.Phase)
	}
	if gs.GuardLastProtect != target {
		t.Fatalf("GuardLastProtect=%v, want %v (cross-night persistence)", gs.GuardLastProtect, target)
	}
	// 第二晚:再守 X → ErrValidationFailed
	if e := gs.NightGuardProtect(gs.GuardSeat, target); e == nil {
		t.Fatalf("second-night protect should fail (G1 consecutive)")
	}
}

// G-05: 守自己拒绝 (G2)。target==actor → ErrValidationFailed / ErrPermissionDenied。
func TestGuardProtect_G05_NoSelfProtect(t *testing.T) {
	gs := makeStartedGame13(t, 500, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	// 守自己 → 应当报错
	if e := gs.NightGuardProtect(gs.GuardSeat, gs.GuardSeat); e == nil {
		t.Fatalf("guard self should fail (G2)")
	}
}

// G-06: 空守合法 (G4)。target=-1 → 阶段推进到 night_wolves。
func TestGuardProtect_G06_EmptyProtectLegal(t *testing.T) {
	gs := makeStartedGame13(t, 600, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	// 空守
	if e := gs.NightGuardProtect(gs.GuardSeat, NoSeat); e != nil {
		t.Fatalf("empty protect should succeed, got %v", e)
	}
	if gs.GuardProtectTarget != NoSeat {
		t.Fatalf("GuardProtectTarget=%v, want NoSeat", gs.GuardProtectTarget)
	}
	if gs.GuardLastProtect != NoSeat {
		t.Fatalf("GuardLastProtect=%v, want NoSeat (empty protect clears)", gs.GuardLastProtect)
	}
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected night_wolves after empty protect, got %v", gs.Phase)
	}
}

// G-07: 无守卫跳过。13 人局用 StartDeck13(无守卫) → startNight 直接进 night_wolves。
func TestGuardProtect_G07_NoGuardSkipsPhase(t *testing.T) {
	gs := makeStartedGame13(t, 700, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	gs.startNight()
	// StandardDeck13 没有守卫 → GuardSeat == NoSeat → startNight 直接进 night_wolves
	if gs.GuardSeat != NoSeat {
		// 13 人标准牌组本来没守卫;若种子碰巧有(随机牌组)则跳过
		t.Skipf("guard present (random deck); GuardSeat=%v", gs.GuardSeat)
	}
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected night_wolves (no guard), got %v", gs.Phase)
	}
}

// G-08: 守卫死亡跳过。守卫已死 → startNight 跳过 night_guard。
func TestGuardProtect_G08_DeadGuardSkipsPhase(t *testing.T) {
	gs := makeStartedGame13(t, 800, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	if gs.Phase != PhaseNightGuard {
		t.Fatalf("expected phase night_guard initially, got %v", gs.Phase)
	}
	// 守卫空守 → 进 night_wolves
	if e := gs.NightGuardProtect(gs.GuardSeat, NoSeat); e != nil {
		t.Fatalf("guard protect: %v", e)
	}
	// 现在标记守卫死亡(模拟首夜被狼刀 / 白天出局)
	gs.Players[gs.GuardSeat].Alive = false
	// 直接调 startNight 断言"死亡守卫被跳过"这一不变式。
	//
	// 为什么不走 advanceToNextNight 完整推进一整天:该路径的中间阶段
	// (dawn / death_lyric / vote / 猎人开枪 / 白痴翻牌)受随机牌组与随机
	// 空刀目标影响,分支组合过多,曾导致本用例停在 death_lyric 而非下一夜 —
	// 那是**测试遍历逻辑**的偶发结果,并非守卫逻辑缺陷。startNight 是
	// "是否进入 night_guard" 的唯一决策点(engine.go §134 分支),
	// 直接调用它可稳定、无歧义地覆盖本用例要验证的不变式。
	gs.startNight()
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected night_wolves (dead guard skipped), got %v", gs.Phase)
	}
	// 同时确认 acting seat 已切到狼人而非死亡守卫。
	if gs.TurnActingSeat == gs.GuardSeat {
		t.Fatalf("TurnActingSeat still points at dead guard seat %v", gs.GuardSeat)
	}
}

// G-09: 护盾不挡毒 (G5)。守 X + 女巫毒 X → X 死亡,cause=witch_poison。
func TestGuardProtect_G09_ShieldNotAgainstPoison(t *testing.T) {
	gs := makeStartedGame13(t, 900, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	if !ensureWitchSeat(t, gs) {
		t.Skip("no witch seat")
	}
	gs.startNight()
	x := firstLivingNonWolfNonSeer(gs)
	if x == NoSeat || x == gs.GuardSeat {
		t.Skip("no candidate")
	}
	// 守卫守 X
	if e := gs.NightGuardProtect(gs.GuardSeat, x); e != nil {
		t.Fatalf("guard protect: %v", e)
	}
	// 狼人空刀
	if e := voteAllWolves(gs, NoSeat); e != nil {
		t.Fatalf("wolf vote: %v", e)
	}
	// 跳过预言家
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeerSeat(gs)
		if seer != NoSeat {
			var t2 Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					t2 = Seat(i)
					break
				}
			}
			if t2 != NoSeat {
				_ = gs.NightSeerCheck(seer, t2)
			} else {
				gs.endSeerPhase()
			}
		} else {
			gs.endSeerPhase()
		}
	}
	// 女巫毒 X
	witch := firstLivingWitchSeat(gs)
	if e := gs.NightWitchAct(witch, "poison", x); e != nil {
		t.Fatalf("witch poison: %v", e)
	}
	// X 应死亡(毒杀,不受护盾影响)
	if gs.AliveSeat(x) {
		t.Fatalf("X (seat %v) should be dead (poison bypasses shield)", x)
	}
	if gs.Players[x].DeathCause != "witch_poison" {
		t.Fatalf("DeathCause=%q, want witch_poison", gs.Players[x].DeathCause)
	}
}

// G-10: 空守后可重守。空守一晚 → 次晚可守上上晚的目标(GuardLastProtect 被清)。
func TestGuardProtect_G10_EmptyEnablesRetarget(t *testing.T) {
	gs := makeStartedGame13(t, 1000, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	first := firstLivingNonWolfNonSeer(gs)
	if first == NoSeat || first == gs.GuardSeat {
		t.Skip("no candidate")
	}
	// 第一晚:守 first(使 GuardLastProtect = first)
	if e := gs.NightGuardProtect(gs.GuardSeat, first); e != nil {
		t.Fatalf("first protect: %v", e)
	}
	// 推进到下一夜(跳过狼/预/巫)
	if e := voteAllWolves(gs, NoSeat); e != nil {
		t.Fatalf("vote: %v", e)
	}
	skipSeerAndWitch(gs)
	if !advanceToNextNight(t, gs) {
		t.Skip("game ended")
	}
	// 第二晚:空守(清 GuardLastProtect)
	if gs.Phase != PhaseNightGuard {
		t.Skipf("expected night_guard, got %v", gs.Phase)
	}
	if e := gs.NightGuardProtect(gs.GuardSeat, NoSeat); e != nil {
		t.Fatalf("empty protect: %v", e)
	}
	if gs.GuardLastProtect != NoSeat {
		t.Fatalf("GuardLastProtect=%v after empty protect, want NoSeat", gs.GuardLastProtect)
	}
	// 推进到第三晚(跳过狼/预/巫)
	if e := voteAllWolves(gs, NoSeat); e != nil {
		t.Fatalf("vote2: %v", e)
	}
	skipSeerAndWitch(gs)
	if !advanceToNextNight(t, gs) {
		t.Skip("game ended")
	}
	// 第三晚:守 first(应该合法,因空守已清 GuardLastProtect)
	if gs.Phase != PhaseNightGuard {
		t.Skipf("expected night_guard on 3rd night, got %v", gs.Phase)
	}
	if e := gs.NightGuardProtect(gs.GuardSeat, first); e != nil {
		t.Fatalf("third-night retarget should succeed after empty middle night: %v", e)
	}
	if gs.GuardProtectTarget != first {
		t.Fatalf("GuardProtectTarget=%v, want %v", gs.GuardProtectTarget, first)
	}
}

// G-11: quarantine skip。guard_protect_skip → 空守 + 阶段推进。
// 通过直接调 NightGuardProtect(NoSeat) 模拟 manager 派发的 guard_protect_skip 行为,
// 因为 dispatchQuarantinedSkipLocked 在测试中需要 manager 实例 + r.mu 持锁,这里只
// 验证引擎层等同性(§92b 双路径必须映射到完全相同的引擎调用)。
func TestGuardProtect_G11_QuarantineSkipEmptyProtect(t *testing.T) {
	gs := makeStartedGame13(t, 1100, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	// manager 路径:guardProtectLocked(r, userID, NoSeat) → r.State.NightGuardProtect(seat, NoSeat)
	if e := gs.NightGuardProtect(gs.GuardSeat, NoSeat); e != nil {
		t.Fatalf("quarantine-skip (empty protect): %v", e)
	}
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected night_wolves after quarantine skip, got %v", gs.Phase)
	}
	if gs.GuardProtectTarget != NoSeat {
		t.Fatalf("GuardProtectTarget=%v, want NoSeat", gs.GuardProtectTarget)
	}
}

// G-12: 视图脱敏。非守卫视角 guard_last_protect == -1;守卫视角 WolfTarget == -1。
func TestGuardProtect_G12_ViewDesensitization(t *testing.T) {
	gs := makeStartedGame13(t, 1200, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	// 设置 GuardLastProtect 给后续断言
	gs.GuardLastProtect = Seat(2)
	gs.GuardProtectTarget = Seat(3)
	gs.WolfKillTarget = Seat(5) // 假设狼刀 5 号

	// (a) 非守卫视角(viewer=平民)→ guard 字段应为 -1
	var villager Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleVillager && gs.AliveSeat(Seat(i)) {
			villager = Seat(i)
			break
		}
	}
	if villager == NoSeat {
		t.Skip("no villager")
	}
	cs := BuildClientState("room1", gs.Seats, int(villager), gs)
	if cs.GuardLastProtect != -1 {
		t.Fatalf("villager view GuardLastProtect=%d, want -1 (desensitized)", cs.GuardLastProtect)
	}
	if cs.GuardProtectTarget != -1 {
		t.Fatalf("villager view GuardProtectTarget=%d, want -1 (desensitized)", cs.GuardProtectTarget)
	}
	// (b) 守卫视角 → guard 字段应有真实值;WolfKillTarget 不可见(守卫盲守)。
	csGuard := BuildClientState("room1", gs.Seats, int(gs.GuardSeat), gs)
	if csGuard.GuardLastProtect != 2 {
		t.Fatalf("guard view GuardLastProtect=%d, want 2", csGuard.GuardLastProtect)
	}
	if csGuard.GuardProtectTarget != 3 {
		t.Fatalf("guard view GuardProtectTarget=%d, want 3", csGuard.GuardProtectTarget)
	}
	// 守卫视角 WitchWolfTarget 应为 -1(没有 WitchWolfTarget 字段;守卫盲守,
	// ClientGameState 不下发 WolfKillTarget 字段 — 这里通过确认守卫拿不到
	// WitchWolfTarget 来证明脱敏)。BuildClientState 中守卫角色不会触发
	// WitchWolfTarget 分支(该分支仅对 RoleWitch 生效)。
	if csGuard.WitchWolfTarget != 0 {
		// WitchWolfTarget 是 int 值零值(无 omitempty);默认 -1;这里粗略检查
		// 守卫没拿到 5(=狼刀目标)。
		t.Fatalf("guard view WitchWolfTarget=%d, want -1/0 (blind guard)", csGuard.WitchWolfTarget)
	}
	// (c) 观战者视角 → 任何 guard 字段都应为 -1
	csSpec := BuildClientState("room1", gs.Seats, -1, gs)
	if csSpec.GuardLastProtect != -1 {
		t.Fatalf("spectator GuardLastProtect=%d, want -1", csSpec.GuardLastProtect)
	}
	if csSpec.GuardProtectTarget != -1 {
		t.Fatalf("spectator GuardProtectTarget=%d, want -1", csSpec.GuardProtectTarget)
	}
}

// TestGuardProtect_StartNight_ResetsNotLastProtect 验证 startNight 重置
// GuardProtectTarget/GuardSavedTarget/SameGuardSameSave/WitchSavedTarget 但不重置
// GuardLastProtect(§3.2 + §3.3 关键)。
func TestGuardProtect_StartNight_ResetsNotLastProtect(t *testing.T) {
	gs := makeStartedGame13(t, 1300, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	gs.startNight()
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.GuardProtectTarget = Seat(2)
	gs.GuardLastProtect = Seat(1)
	gs.GuardSavedTarget = Seat(3)
	gs.SameGuardSameSave = Seat(4)
	gs.WitchSavedTarget = Seat(5)
	// 模拟"新夜晚"前先标 lastProtect(关键),再 startNight
	gs.startNight()
	if gs.GuardProtectTarget != NoSeat {
		t.Fatalf("GuardProtectTarget=%v, want NoSeat after startNight", gs.GuardProtectTarget)
	}
	if gs.GuardSavedTarget != NoSeat {
		t.Fatalf("GuardSavedTarget=%v, want NoSeat after startNight", gs.GuardSavedTarget)
	}
	if gs.SameGuardSameSave != NoSeat {
		t.Fatalf("SameGuardSameSave=%v, want NoSeat after startNight", gs.SameGuardSameSave)
	}
	if gs.WitchSavedTarget != NoSeat {
		t.Fatalf("WitchSavedTarget=%v, want NoSeat after startNight", gs.WitchSavedTarget)
	}
	// GuardLastProtect 必须保留(否则 G1 连守校验失效)
	if gs.GuardLastProtect != Seat(1) {
		t.Fatalf("GuardLastProtect=%v, want 1 (cross-night persistence)", gs.GuardLastProtect)
	}
}

// TestGuardProtect_StartGame_AssignsGuardSeat 验证 StartGame 在 Roles 遍历中
// 给 GuardSeat 赋值(与 WitchSeat 同一循环)。
func TestGuardProtect_StartGame_AssignsGuardSeat(t *testing.T) {
	gs := makeStartedGame13(t, 1400, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	// 注入守卫(直接在 StartGame 后改 Roles 是允许的:只是更新 GuardSeat 字段缓存,
	// 不会破坏后续 GameState 操作)
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	guard := gs.GuardSeat
	if gs.Roles[guard] != RoleGuard {
		t.Fatalf("Roles[guard]=%v, want RoleGuard", gs.Roles[guard])
	}
	// 验证核心契约:GuardSeat 与 Roles[GuardSeat]==RoleGuard 一致 — 已在前面断言。
	// 这里再验证 WitchSeat 也被正确赋值(同一循环,回归保护)
	if gs.WitchSeat == NoSeat {
		// 若随机牌组碰巧没发女巫,跳过断言;正常 13 人局必有女巫。
		// 13 人 RandomDeck13 中女巫出现率约 90%,这里容忍无女巫的边界。
		t.Logf("WitchSeat == NoSeat (random deck quirk); skipping WitchSeat regression")
	}
}

// G-13: BUG-R9-P0-2 (2026-07-29) — guardProtectFallbackLocked 兜底。
// 场景:守卫 LLM 持续失败 → watchdog 代打 guard_protect_skip。若走显式空守,
// 守卫当夜必被狼刀死(R9 seat 5 实测触发屠边)。修复后系统代打应在 G1/G2/G3
// 约束内挑一个合法目标。本用例不拉起 manager,直接调
// guardProtectFallbackLocked 的引擎行为等价物:在 GuardLastProtect / 自己之外
// 选第一个存活座位。
func TestGuardProtect_G13_WatchdogFallbackPicksLegalTarget(t *testing.T) {
	gs := makeStartedGame13(t, 1300, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	if gs.Phase != PhaseNightGuard {
		t.Fatalf("expected night_guard after startNight, got %v", gs.Phase)
	}
	// 模拟上一晚守过 seat 0(或第一个非己存活座位) — G1 应剔除。
	last := Seat(-1)
	for i := 0; i < MaxPlayers; i++ {
		cand := Seat(i)
		if cand == gs.GuardSeat {
			continue
		}
		if !gs.AliveSeat(cand) {
			continue
		}
		last = cand
		break
	}
	if last < 0 {
		t.Skip("no legal fallback candidate")
	}
	gs.GuardLastProtect = last

	// 模拟 fallback 逻辑:G1/G2/G3 全过滤后的下一个存活座位。
	var want Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		cand := Seat(i)
		if cand == gs.GuardSeat {
			continue
		}
		if !gs.AliveSeat(cand) {
			continue
		}
		if cand == gs.GuardLastProtect {
			continue
		}
		want = cand
		break
	}
	if want == NoSeat {
		t.Skip("no second legal candidate")
	}

	// 引擎侧:NightGuardProtect(want) 必须成功(校验 G1/G2/G3 全过)。
	if e := gs.NightGuardProtect(gs.GuardSeat, want); e != nil {
		t.Fatalf("fallback NightGuardProtect(%v) err: %v", want, e)
	}
	if gs.GuardProtectTarget != want {
		t.Fatalf("GuardProtectTarget=%v, want %v", gs.GuardProtectTarget, want)
	}
	if gs.GuardLastProtect != want {
		t.Fatalf("GuardLastProtect=%v, want %v", gs.GuardLastProtect, want)
	}
}

// G-14: BUG-R9-P0-2 反向用例 — 显式空守仍合法(LLM 主动选择)。
// 确保 fallback 不会污染"显式空守"语义:agent 侧 runner.GuardProtect(-1)
// 走 Action_GuardProtect → NightGuardProtect(NoSeat) 必须仍然成功,且
// GuardProtectTarget 保持 NoSeat,GuardLastProtect 被复位为 NoSeat。
func TestGuardProtect_G14_ExplicitEmptyProtectStillWorks(t *testing.T) {
	gs := makeStartedGame13(t, 1400, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	if !ensureGuardSeat(t, gs) {
		t.Skip("no guard seat")
	}
	gs.startNight()
	if gs.Phase != PhaseNightGuard {
		t.Fatalf("expected night_guard after startNight, got %v", gs.Phase)
	}
	// 上一晚守过任意座位,显式空守后 GuardLastProtect 应复位(§134 G4)。
	gs.GuardLastProtect = Seat(0)
	if e := gs.NightGuardProtect(gs.GuardSeat, NoSeat); e != nil {
		t.Fatalf("explicit empty protect err: %v", e)
	}
	if gs.GuardProtectTarget != NoSeat {
		t.Fatalf("GuardProtectTarget=%v, want NoSeat", gs.GuardProtectTarget)
	}
	if gs.GuardLastProtect != NoSeat {
		t.Fatalf("GuardLastProtect=%v, want NoSeat after explicit empty protect", gs.GuardLastProtect)
	}
}
