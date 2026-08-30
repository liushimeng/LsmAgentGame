package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// helpers
// fillSeats 接受 1..MaxPlayers 个 userID,按序填入座位 0..n-1,其余留空。
func fillSeats(users ...string) [MaxPlayers]string {
	if len(users) > MaxPlayers {
		panic("too many seats")
	}
	var s [MaxPlayers]string
	for i, u := range users {
		s[i] = u
	}
	return s
}

// makeStartedGame 启动一局已发牌的游戏(默认按实际入座人数选择 7 / 12 人牌组)。
// BUG 2026-07-08: 测试通常需要直接进入 night_wolves 操作 NightWolfKill,
// 跳过首夜发言缓冲期 (PhasePreWolves)。makeStartedGame 保留 PhasePreWolves
// 阶段(用于其他测试覆盖缓冲期逻辑),需要 night_wolves 的测试请在拿到 gs
// 后调 gs.startNight()。
func makeStartedGame(t *testing.T, seed int64, users [MaxPlayers]string) *GameState {
	t.Helper()
	gs := NewGame(seed)
	occupied := 0
	for _, u := range users {
		if u == "" {
			continue
		}
		occupied++
		if _, e := gs.AddPlayer(u); e != nil {
			t.Fatalf("add player: %v", e)
		}
	}
	// 按入座人数设定本局人数:7 → werewolf_7 兼容;其余 → 12 人标准竞技局。
	if occupied == 7 {
		gs.SeatCount = 7
	} else {
		gs.SeatCount = MaxPlayers
	}
	// 注入 seats 给 AssignRoles / view 用
	for i, u := range users {
		gs.Seats[i] = u
	}
	if e := gs.StartGame(); e != nil {
		t.Fatalf("start: %v", e)
	}
	gs.Seats = users
	return gs
}

// makeStartedGame12 启动一局 12 人标准竞技局(helper for new 12-player tests)。
// 显式设 SeatCount=12(不依赖 MaxPlayers,因为 MaxPlayers 已扩到 13)。
func makeStartedGame12(t *testing.T, seed int64, users [MaxPlayers]string) *GameState {
	t.Helper()
	gs := NewGame(seed)
	for _, u := range users {
		if u == "" {
			continue
		}
		if _, e := gs.AddPlayer(u); e != nil {
			t.Fatalf("add player: %v", e)
		}
	}
	gs.SeatCount = 12
	for i, u := range users {
		gs.Seats[i] = u
	}
	if e := gs.StartGame(); e != nil {
		t.Fatalf("start: %v", e)
	}
	gs.Seats = users
	return gs
}

// makeStartedGame13 启动一局 13 人标准竞技局(默认人数)。
// 显式设 SeatCount=13(MaxPlayers)。
func makeStartedGame13(t *testing.T, seed int64, users [MaxPlayers]string) *GameState {
	t.Helper()
	gs := NewGame(seed)
	for _, u := range users {
		if u == "" {
			continue
		}
		if _, e := gs.AddPlayer(u); e != nil {
			t.Fatalf("add player: %v", e)
		}
	}
	gs.SeatCount = MaxPlayers
	for i, u := range users {
		gs.Seats[i] = u
	}
	if e := gs.StartGame(); e != nil {
		t.Fatalf("start: %v", e)
	}
	gs.Seats = users
	return gs
}

// TestStandardDeck 历史 7 人标准局:2 狼 + 1预言家 + 1女巫 + 1猎人 + 2 平民
func TestStandardDeck(t *testing.T) {
	d := StandardDeck()
	if len(d) != 7 {
		t.Fatalf("deck size %d != 7", len(d))
	}
	counts := map[Role]int{}
	for _, r := range d {
		counts[r]++
	}
	if counts[RoleWerewolf] != 2 ||
		counts[RoleSeer] != 1 ||
		counts[RoleWitch] != 1 ||
		counts[RoleHunter] != 1 ||
		counts[RoleVillager] != 2 {
		t.Fatalf("counts wrong: %+v", counts)
	}
}

// TestStandardDeck12 12 人标准竞技局:4 狼 + 预言家 + 女巫 + 猎人 + 白痴 + 4 平民
func TestStandardDeck12(t *testing.T) {
	d := StandardDeck12()
	if len(d) != 12 {
		t.Fatalf("deck size %d != 12", len(d))
	}
	counts := map[Role]int{}
	for _, r := range d {
		counts[r]++
	}
	if counts[RoleWerewolf] != 4 ||
		counts[RoleSeer] != 1 ||
		counts[RoleWitch] != 1 ||
		counts[RoleHunter] != 1 ||
		counts[RoleIdiot] != 1 ||
		counts[RoleVillager] != 4 {
		t.Fatalf("counts wrong: %+v", counts)
	}
}

// TestStandardDeck13 13 人标准竞技局(默认):4 狼 + 预言家 + 女巫 + 猎人 + 白痴 + 5 平民
func TestStandardDeck13(t *testing.T) {
	d := StandardDeck13()
	if len(d) != 13 {
		t.Fatalf("deck size %d != 13", len(d))
	}
	counts := map[Role]int{}
	for _, r := range d {
		counts[r]++
	}
	if counts[RoleWerewolf] != 4 ||
		counts[RoleSeer] != 1 ||
		counts[RoleWitch] != 1 ||
		counts[RoleHunter] != 1 ||
		counts[RoleIdiot] != 1 ||
		counts[RoleVillager] != 5 {
		t.Fatalf("counts wrong: %+v", counts)
	}
}

// TestRefreshCounts13_FivePlain 13 人局: 2026-07-11 随机牌组后,神职数 3-4(1 seer + 2-3 random gods)。
func TestRefreshCounts13_FivePlain(t *testing.T) {
	gs := makeStartedGame13(t, 400, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11", "p12"))
	gs.refreshCounts()
	if gs.DivineCnt < 3 || gs.DivineCnt > 4 {
		t.Fatalf("DivineCnt = %d, want 3 or 4", gs.DivineCnt)
	}
	expectedPlain := 13 - 4 - gs.DivineCnt
	if gs.PlainCnt != expectedPlain {
		t.Fatalf("PlainCnt = %d, want %d", gs.PlainCnt, expectedPlain)
	}
	if gs.WolfAliveCnt != 4 {
		t.Fatalf("WolfAliveCnt = %d, want 4", gs.WolfAliveCnt)
	}
	if gs.GoodAliveCnt != gs.DivineCnt+gs.PlainCnt {
		t.Fatalf("GoodAliveCnt = %d, want %d", gs.GoodAliveCnt, gs.DivineCnt+gs.PlainCnt)
	}
}

// TestCheckWinner13_PlainWipe 13 人局屠民:5 民死光 → 狼人胜(屠神阈值 4 不变)。
func TestCheckWinner13_PlainWipe(t *testing.T) {
	gs := makeStartedGame13(t, 401, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11", "p12"))
	// 杀掉全部 5 平民(留 4 神+4 狼)。
	for i, r := range gs.Roles {
		if r == RoleVillager {
			gs.Players[i].Alive = false
		}
	}
	if !gs.checkWinner() {
		t.Fatalf("should have a winner after plain wipe")
	}
	if gs.Winner != "wolf" {
		t.Fatalf("winner = %q, want wolf", gs.Winner)
	}
}

// TestCheckWinner13_DivineWipe 13 人局屠神:所有神职死光 → 狼人胜(2026-07-11 适配随机牌组)。
func TestCheckWinner13_DivineWipe(t *testing.T) {
	gs := makeStartedGame13(t, 402, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11", "p12"))
	for i, r := range gs.Roles {
		if IsGodRole(r) {
			gs.Players[i].Alive = false
		}
	}
	if !gs.checkWinner() {
		t.Fatalf("should have a winner after divine wipe")
	}
	if gs.Winner != "wolf" {
		t.Fatalf("winner = %q, want wolf", gs.Winner)
	}
}

// TestRoleIdiotFaction 白痴属于好人阵营神职。
func TestRoleIdiotFaction(t *testing.T) {
	if FactionOf(RoleIdiot) != FactionGood {
		t.Fatalf("Idiot faction = %v, want good", FactionOf(RoleIdiot))
	}
	if RoleIdiot.String() != "idiot" {
		t.Fatalf("Idiot.String = %q, want idiot", RoleIdiot.String())
	}
	if RoleDisplayName(RoleIdiot) != "白痴" {
		t.Fatalf("Idiot displayName = %q, want 白痴", RoleDisplayName(RoleIdiot))
	}
}

func TestAssignRolesDeterministicBySeed(t *testing.T) {
	u := fillSeats("u1", "u2", "u3", "u4", "u5", "u6", "u7")
	r1 := AssignRoles(42, u)
	r2 := AssignRoles(42, u)
	for i := 0; i < MaxPlayers; i++ {
		if r1[i] != r2[i] {
			t.Fatalf("seed=42 not deterministic")
		}
	}
	r3 := AssignRoles(43, u)
	diff := false
	for i := 0; i < MaxPlayers; i++ {
		if r1[i] != r3[i] {
			diff = true
			break
		}
	}
	if !diff {
		t.Fatalf("different seeds should produce different orders")
	}
}

func TestStartGameAssignsRolesAndStartsNight(t *testing.T) {
	gs := makeStartedGame(t, 100, fillSeats("u1", "u2", "u3", "u4", "u5", "u6", "u7"))
	// BUG 2026-07-08: StartGame 现在先进入 PhasePreWolves(首夜发言缓冲期),
	// 而不是直接 PhaseNightWolves。测试通过 startNight() 切到 night_wolves。
	if gs.Phase != PhasePreWolves {
		t.Fatalf("phase = %v (expected pre_wolves)", gs.Phase)
	}
	gs.startNight()
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("phase after startNight = %v", gs.Phase)
	}
	roles := map[Role]int{}
	for _, r := range gs.Roles {
		roles[r]++
	}
	if roles[RoleWerewolf] != 2 {
		t.Fatalf("expected 2 wolves, got %v", roles)
	}
}

func TestVictory_CivilianSideRemoved(t *testing.T) {
	// 屠民:2 狼剩 1 神 1 平民→杀 2 平民就胜利
	gs := makeStartedGame(t, 1, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	// 手动定位 2 个狼 + 2 个平民 + 1 个任意,杀掉 2 个平民 → 屠民胜
	wolves := []Seat{}
	villagers := []Seat{}
	for i, r := range gs.Roles {
		if r == RoleWerewolf { wolves = append(wolves, Seat(i)) }
		if r == RoleVillager { villagers = append(villagers, Seat(i)) }
	}
	// 屠民:要求狼杀光所有平民即可胜 → 我们手动调 killPlayer 仿狼刀 + 女巫不用药
	for _, v := range villagers {
		if e := gs.killPlayer(v, "wolf"); e != nil {
			t.Fatalf("kill villager: %v", e)
		}
	}
	gs.refreshCounts()
	if !gs.checkWinner() || gs.Winner != "wolf" {
		t.Fatalf("expected wolf win, got winner=%s", gs.Winner)
	}
	if gs.Status != "over" || gs.Phase != PhaseGameOver {
		t.Fatalf("expected over, got status=%s phase=%v", gs.Status, gs.Phase)
	}
}

func TestVictory_GodSideRemoved(t *testing.T) {
	// 屠神:杀光所有神职
	gs := makeStartedGame(t, 1, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	divine := []Seat{}
	for i, r := range gs.Roles {
		if r == RoleSeer || r == RoleWitch || r == RoleHunter {
			divine = append(divine, Seat(i))
		}
	}
	for _, v := range divine {
		if e := gs.killPlayer(v, "wolf"); e != nil {
			t.Fatalf("kill divine: %v", e)
		}
	}
	if !gs.checkWinner() || gs.Winner != "wolf" {
		t.Fatalf("expected wolf win (屠神), got winner=%s", gs.Winner)
	}
}

func TestVictory_AllWolvesEliminated(t *testing.T) {
	gs := makeStartedGame(t, 1, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	wolves := []Seat{}
	for i, r := range gs.Roles {
		if r == RoleWerewolf { wolves = append(wolves, Seat(i)) }
	}
	for _, w := range wolves {
		if e := gs.killPlayer(w, "vote"); e != nil {
			t.Fatalf("kill wolf: %v", e)
		}
	}
	if !gs.checkWinner() || gs.Winner != "good" {
		t.Fatalf("expected good win, got winner=%s", gs.Winner)
	}
}

func TestWitchAntidoteSavesTarget(t *testing.T) {
	gs := makeStartedGame(t, 1, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight() // BUG 2026-07-08: 跳过首夜发言缓冲期进入 night_wolves
	// 找一个非狼人活人当 victim
	var victim Seat = -1
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] != RoleWerewolf && gs.AliveSeat(Seat(i)) {
			victim = Seat(i)
			break
		}
	}
	// 2026-07-17: 所有存活狼人投票给同一目标,触发计票推进
	if e := voteAllWolves(gs, victim); e != nil {
		t.Fatalf("wolf kill: %v", e)
	}
	if gs.Phase != PhaseNightSeer && gs.Phase != PhaseNightWitch {
		t.Fatalf("expected night_witch, got %v", gs.Phase)
	}
	if gs.Phase == PhaseNightSeer {
		// 让预言家跳过(查验一个非自己的合法目标)
		seer := firstLivingSeer(gs)
		if seer != NoSeat {
			var checkTarget Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					checkTarget = Seat(i)
					break
				}
			}
			if checkTarget != NoSeat {
				if e := gs.NightSeerCheck(seer, checkTarget); e != nil {
					t.Fatalf("seer check: %v", e)
				}
			}
		}
	}
	witch := gs.WitchSeat
	if witch == NoSeat { t.Skip("no witch") }
	if e := gs.NightWitchAct(witch, "antidote", -1); e != nil {
		t.Fatalf("witch antidote: %v", e)
	}
	// victim 应仍活
	if !gs.AliveSeat(victim) {
		t.Fatalf("victim should be alive after antidote")
	}
}

func TestWitchPoisonKillsIndependently(t *testing.T) {
	gs := makeStartedGame(t, 2, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight() // BUG 2026-07-08: 跳过首夜发言缓冲期进入 night_wolves
	// 跳过前序:直接强制进入女巫
	var victim Seat = -1
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] != RoleWitch && gs.AliveSeat(Seat(i)) {
			victim = Seat(i); break
		}
	}
	// 2026-07-17: 所有存活狼人弃权(空刀),触发计票推进(全弃权→随机)
	if e := voteAllWolves(gs, NoSeat); e != nil {
		t.Fatalf("wolf empty: %v", e)
	}
	// 处理预言家(若有)
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeer(gs)
		if seer != NoSeat {
			// 任意查验一个存活非自己
			var target Seat = -1
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
					target = Seat(i); break
				}
			}
			if target != -1 {
				if e := gs.NightSeerCheck(seer, target); e != nil {
					t.Fatalf("seer: %v", e)
				}
			}
		}
	}
	witch := gs.WitchSeat
	if e := gs.NightWitchAct(witch, "poison", victim); e != nil {
		t.Fatalf("witch poison: %v", e)
	}
	if gs.AliveSeat(victim) {
		t.Fatalf("poison victim should be dead")
	}
}

func TestSameTarget_KnifeAndPoison(t *testing.T) {
	gs := makeStartedGame(t, 3, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight() // BUG 2026-07-08: 跳过首夜发言缓冲期进入 night_wolves
	var victim Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] != RoleWerewolf && gs.Roles[i] != RoleWitch && gs.AliveSeat(Seat(i)) {
			victim = Seat(i); break
		}
	}
	// 2026-07-17: 所有存活狼人投票给同一目标
	if e := voteAllWolves(gs, victim); e != nil { t.Fatalf("wolf: %v", e) }
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeer(gs)
		if seer != NoSeat {
			var target Seat = NoSeat
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) { target = Seat(i); break }
			}
			if target != NoSeat { _ = gs.NightSeerCheck(seer, target) }
		}
	}
	witch := gs.WitchSeat
	if e := gs.NightWitchAct(witch, "poison", victim); e != nil { t.Fatalf("witch: %v", e) }
	if gs.AliveSeat(victim) {
		t.Fatalf("same-target victim should be dead")
	}
}

func TestHunterCanShootAfterVoteButNotPoison(t *testing.T) {
	gs := makeStartedGame(t, 4, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	// 找到猎人 + 投票路径
	hunter := firstLivingHunter(gs)
	if hunter == NoSeat { t.Skip("no hunter") }
	// 强推到猎人开枪阶段
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "vote"
	if e := gs.HunterShoot(hunter, 0); e != nil { t.Fatalf("hunter shoot vote: %v", e) }
	if gs.AliveSeat(0) {
		t.Fatalf("victim should be dead")
	}

	// 同一 hunter 但 poison 不能再开枪
	gs2 := makeStartedGame(t, 5, fillSeats("h1", "h2", "h3", "h4", "h5", "h6", "h7"))
	hunter2 := firstLivingHunter(gs2)
	if hunter2 == NoSeat { t.Skip("no hunter") }
	gs2.HunterPendingShoot = true
	gs2.HunterPendingFrom = "poison"
	if e := gs2.HunterShoot(hunter2, 0); e == nil {
		t.Fatalf("poison hunter should NOT be able to shoot")
	}
}

func TestWolfSuicideImmediatelyToNight(t *testing.T) {
	gs := makeStartedGame(t, 6, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	wolf := firstLivingWolf(gs)
	if wolf == NoSeat { t.Fatal("no wolf") }
	// BUG 2026-07-09: endWitchPhase 现在会自动调 StartDay()(通过遗言阶段哨兵
	// 直接恢复),不再需要手动调 StartDay。
	gs.endWitchPhase()
	// 若进入 sheriff → 跳过到 speak
	if gs.Phase == PhaseSheriff {
		// 没候选 → 无警长
		for i := 0; i < MaxPlayers; i++ { gs.Players[i].HasSpoken = false }
		if e := gs.SheriffElect(NoSeat); e != nil { t.Fatalf("sheriff elect: %v", e) }
	}
	if gs.Phase != PhaseSpeak {
		// 强制一下:从 dusk/phases 切换 → 简化,直接调 startSpeak
		gs.startSpeakPhase()
	}
	if e := gs.WolfSuicide(wolf); e != nil { t.Fatalf("suicide: %v", e) }
	if gs.AliveSeat(wolf) {
		t.Fatalf("wolf should be dead")
	}
	// §20260830-02 — 新链路:自爆 → 遗言(death_lyric)→ 带走(suicide_take)
	// → …→ night_wolves。此处只断言"不再直接进夜";完整链路见
	// engine_suicide_take_test.go。
	if gs.Phase == PhaseDeathLyric {
		if !gs.Players[wolf].LastWords {
			t.Fatalf("suicide wolf should have last words (§20260830-02)")
		}
		if e := gs.SayLastWords(wolf, "我是狼,认了"); e != nil {
			t.Fatalf("say last words: %v", e)
		}
	}
	if gs.Phase != PhaseSuicideTake && gs.Phase != PhaseNightWolves && gs.Phase != PhaseGameOver {
		t.Fatalf("phase after suicide chain = %v", gs.Phase)
	}
	if gs.Phase == PhaseSuicideTake {
		// 放弃带走 → 直接入夜。
		if e := gs.SuicideTake(wolf, NoSeat); e != nil {
			t.Fatalf("suicide take decline: %v", e)
		}
	}
	if gs.Phase != PhaseNightWolves && gs.Phase != PhaseGameOver {
		t.Fatalf("phase after decline = %v", gs.Phase)
	}
}

func TestSheriffVoteTieFirstRound(t *testing.T) {
	gs := makeStartedGame(t, 7, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	// BUG 2026-07-09: endWitchPhase 现在自动调 StartDay()(通过遗言阶段哨兵恢复)。
	gs.endWitchPhase()
	if gs.Phase != PhaseSheriff { t.Fatalf("phase = %v", gs.Phase) }
	// 所有人投同一人 + 另一人同票 → 平票
	p1 := Seat(0)
	p2 := Seat(1)
	for i := 0; i < MaxPlayers; i++ {
		s := Seat(i)
		if gs.AliveSeat(s) {
			if i%2 == 0 {
				_ = gs.DayVote(s, p1)
			} else {
				_ = gs.DayVote(s, p2)
			}
		}
	}
	if e := gs.SheriffElect(NoSeat); e != nil { t.Fatalf("sheriff elect: %v", e) }
	if len(gs.DayTiedPlayers) > 0 && gs.SheriffSeat != NoSeat {
		t.Fatalf("should be tie or no sheriff: sheriff=%v tied=%v", gs.SheriffSeat, gs.DayTiedPlayers)
	}
}

func TestBuildClientState_SpectatorHasNoRoles(t *testing.T) {
	// 8 人局(viewer=-1 = 观察者): 所有存活玩家角色必须剥离。
	gs := makeStartedGame(t, 8, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	state := BuildClientState("r1", gs.Seats, -1, gs)
	for _, p := range state.Players {
		if p.RoleRevealed {
			t.Fatalf("spectator should not see role revealed for seat=%d", p.Seat)
		}
		if p.Role != "" {
			t.Fatalf("spectator should not see role string for seat=%d, got=%q", p.Seat, p.Role)
		}
		if p.Faction != "" {
			t.Fatalf("spectator should not see faction for seat=%d, got=%q", p.Seat, p.Faction)
		}
	}
}

// BUG-R204-SEC-01 回归测试: 13 人局观察者视图必须剥离所有存活玩家角色/阵营,
// 且 my_seat/my_role/my_faction 均为空(无自身身份)。
func TestBuildClientState_Spectator13_NoRoleLeak(t *testing.T) {
	gs := makeStartedGame13(t, 99, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	state := BuildClientState("r1", gs.Seats, -1, gs)
	for i, p := range state.Players {
		if p.RoleRevealed {
			t.Fatalf("13p spectator: seat %d RoleRevealed=true (LEAK)", i)
		}
		if p.Role != "" {
			t.Fatalf("13p spectator: seat %d role=%q (LEAK)", i, p.Role)
		}
		if p.Faction != "" {
			t.Fatalf("13p spectator: seat %d faction=%q (LEAK)", i, p.Faction)
		}
	}
	if state.MySeat != -1 {
		t.Fatalf("spectator MySeat should be -1, got %d", state.MySeat)
	}
	if state.MyRole != "" || state.MyFaction != "" {
		t.Fatalf("spectator should have empty MyRole/MyFaction, got role=%q faction=%q", state.MyRole, state.MyFaction)
	}
}

// §135 身份公开公平性:本测试原名 TestBuildClientState_RevealsDeadPlayers,
// 断言"任何玩家一死就对全场公开身份"。该行为违反标准竞技局规则(普通死亡出局
// 身份牌全程不翻开),已于 §135 移除 —— 断言随之**反转**。
func TestBuildClientState_HidesOrdinaryDeadPlayerRole(t *testing.T) {
	gs := makeStartedGame(t, 9, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	victim := Seat(0)
	if e := gs.killPlayer(victim, "wolf"); e != nil {
		t.Fatalf("kill: %v", e)
	}
	state := BuildClientState("r1", gs.Seats, 1, gs)
	if state.Players[0].RoleRevealed {
		t.Fatalf("普通死亡(狼刀)不得公开身份, role_revealed 应为 false")
	}
	if state.Players[0].Role != "" {
		t.Fatalf("普通死亡不得下发 role, got %q", state.Players[0].Role)
	}
	// 但"已死亡"这一事实仍然公开(法官公布几号死亡)。
	if state.Players[0].Alive {
		t.Fatalf("死亡事实必须公开: alive 应为 false")
	}
}

func TestBuildWolfPeerView(t *testing.T) {
	gs := makeStartedGame(t, 10, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight() // BUG 2026-07-08: 跳过首夜发言缓冲期进入 night_wolves
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("phase = %v", gs.Phase)
	}
	v := BuildWolfPeerView(gs)
	if v == nil { t.Fatal("nil") }
	// 狼人的 wolf_seats 必有 2 个
	count := 0
	for _, r := range gs.Roles {
		if r == RoleWerewolf { count++ }
	}
	if len(v.WolfSeats) != count {
		t.Fatalf("wolf_seats size = %d, want %d", len(v.WolfSeats), count)
	}
}

func TestBuildWitchInform(t *testing.T) {
	gs := makeStartedGame(t, 11, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	wolf := firstLivingWolf(gs)
	var victim Seat = -1
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] != RoleWerewolf && gs.AliveSeat(Seat(i)) {
			victim = Seat(i); break
		}
	}
	_ = gs.NightWolfKill(wolf, victim, "")
	if gs.Phase == PhaseNightSeer {
		seer := firstLivingSeer(gs)
		if seer != NoSeat {
			var target Seat = -1
			for i := 0; i < MaxPlayers; i++ {
				if Seat(i) != seer && gs.AliveSeat(Seat(i)) { target = Seat(i); break }
			}
			if target != -1 { _ = gs.NightSeerCheck(seer, target) }
		}
	}
	info := BuildWitchInform(gs)
	if info == nil { t.Skip("no witch") }
	if info.WolfTarget != int(victim) {
		t.Fatalf("witch target = %d, want %d", info.WolfTarget, victim)
	}
	if !info.AntidoteAvailable || !info.PoisonAvailable {
		t.Fatalf("fresh witch should have both potions")
	}
}

// ─────────────────── 遗言 (Last Words) 引擎单元测试 —— BUG 2026-07-09 ───────────────────

// TestDeathLyric_DawnMultiFilter 多死时仅 LastWords=true 的座位入遗言队列。
// 用更直接的方式构造:手动设置 LastNightDeaths 后调 endWitchPhase,验证队列
// 过滤与遗言发言流程。
func TestDeathLyric_DawnMultiFilter(t *testing.T) {
	gs := makeStartedGame(t, 101, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	// 跳过首夜缓冲到 dawn → death_lyric 流程
	gs.Phase = PhaseNightWolves
	if firstLivingWolf(gs) == NoSeat {
		t.Skip("no wolf")
	}
	// 2026-07-17: 所有存活狼人投票给 seat 2(LastWords=true for Day 1)
	victim := Seat(2)
	if e := voteAllWolves(gs, victim); e != nil {
		t.Fatalf("wolf kill: %v", e)
	}
	// 预言家查验
	seer := firstLivingSeer(gs)
	if seer != NoSeat {
		for i := 0; i < MaxPlayers; i++ {
			if Seat(i) != seer && gs.AliveSeat(Seat(i)) {
				_ = gs.NightSeerCheck(seer, Seat(i))
				break
			}
		}
	}
	// 女巫直接结束(不用药)
	if gs.WitchSeat != NoSeat && gs.AliveSeat(gs.WitchSeat) {
		if e := gs.NightWitchAct(gs.WitchSeat, "none", NoSeat); e != nil {
			t.Fatalf("witch none: %v", e)
		}
	} else {
		gs.endWitchPhase()
	}
	// dawn → death_lyric:仅 victim(LastWords=true)入队
	if gs.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric", gs.Phase)
	}
	if len(gs.DeathLyricQueue) != 1 || gs.DeathLyricQueue[0] != victim {
		t.Fatalf("queue = %v, want [2]", gs.DeathLyricQueue)
	}
	if gs.DeathLyricCurrent != victim {
		t.Fatalf("current = %d, want 2", gs.DeathLyricCurrent)
	}
	// seat 2 发遗言 → 队列清空 → 自动进入 sheriff/speak
	if e := gs.SayLastWords(victim, "我是预言家,查了 3 号好人"); e != nil {
		t.Fatalf("say last words: %v", e)
	}
	if gs.Phase != PhaseSheriff && gs.Phase != PhaseSpeak {
		t.Fatalf("after death_lyric phase = %v, want sheriff/speak", gs.Phase)
	}
}

// TestDeathLyric_Day3NoLastWords Day 3 出局无遗言,不进遗言阶段。
func TestDeathLyric_Day3NoLastWords(t *testing.T) {
	gs := makeStartedGame(t, 102, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.DayNumber = 3
	prePhase := gs.Phase
	// killPlayer wolf → LastWords=false (Day>2)
	if e := gs.killPlayer(Seat(2), "wolf"); e != nil {
		t.Fatalf("kill: %v", e)
	}
	if gs.Players[2].LastWords {
		t.Fatalf("Day 3 kill should have LastWords=false")
	}
	// tryEnterDeathLyricRound 应直接调 onDone(返回哨兵)
	called := false
	if e := gs.tryEnterDeathLyricRound([]Seat{Seat(2)}, func() *errcode.Error {
		called = true
		return nil
	}); e != nil {
		t.Fatalf("tryEnter: %v", e)
	}
	if !called {
		t.Fatalf("on Day 3, onDone should be called directly (no death_lyric phase)")
	}
	// phase 不应变(仍与 prePhase 一致)
	if gs.Phase != prePhase {
		t.Fatalf("phase changed: pre=%v post=%v, want unchanged", prePhase, gs.Phase)
	}
}

// TestDeathLyric_HunterOrder 猎人死于 Day 2 投票 → 遗言 → hunter_shoot → advanceDay。
func TestDeathLyric_HunterOrder(t *testing.T) {
	gs := makeStartedGame(t, 103, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.DayNumber = 2
	hunter := firstLivingHunter(gs)
	if hunter == NoSeat {
		t.Skip("no hunter")
	}
	// 投票放逐 hunter
	gs.Phase = PhaseVote
	for i := range gs.Players {
		gs.Players[i].Voted = false
		gs.Players[i].VoteTarget = NoSeat
	}
	// 全员投 hunter
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) {
			_ = gs.DayVote(Seat(i), hunter)
		}
	}
	if e := gs.FinishVote(0); e != nil {
		t.Fatalf("finish vote: %v", e)
	}
	// 应进入遗言阶段(LastWords=true for Day 2 vote kill)
	if gs.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric", gs.Phase)
	}
	if gs.HunterPendingShoot {
		t.Fatalf("HunterPendingShoot should be false until death_lyric completes")
	}
	// hunter 遗言 → 队列清空 → onDone 触发 hunter_shoot 阶段
	if e := gs.SayLastWords(hunter, "我是猎人,带走 3 号"); e != nil {
		t.Fatalf("say last words: %v", e)
	}
	if gs.Phase != PhaseHunterShoot {
		t.Fatalf("after death_lyric phase = %v, want hunter_shoot", gs.Phase)
	}
	if !gs.HunterPendingShoot {
		t.Fatalf("HunterPendingShoot should be true")
	}
	// 猎人开枪
	if e := gs.HunterShoot(hunter, NoSeat); e != nil {
		t.Fatalf("hunter shoot: %v", e)
	}
	if gs.DayNumber != 3 {
		t.Fatalf("DayNumber = %d, want 3 after advanceDay", gs.DayNumber)
	}
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("phase = %v, want night_wolves", gs.Phase)
	}
}

// TestDeathLyric_SayAndSkip 队列多座:seat 2 发言 + seat 4 跳过 → 队列清空。
func TestDeathLyric_SayAndSkip(t *testing.T) {
	gs := makeStartedGame(t, 104, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.DayNumber = 1
	// 手动构造遗言队列,绕过 killPlayer 的 LastWords 逻辑
	gs.Phase = PhaseDeathLyric
	gs.DeathLyricQueue = []Seat{2, 4}
	gs.DeathLyricDone = make(map[Seat]bool)
	gs.DeathLyricCurrent = 2
	done := false
	gs.DeathLyricOnDone = func() *errcode.Error {
		done = true
		gs.Phase = PhaseSpeak
		return nil
	}
	// seat 2 发遗言
	if e := gs.SayLastWords(2, "first words"); e != nil {
		t.Fatalf("say: %v", e)
	}
	if gs.DeathLyricCurrent != 4 {
		t.Fatalf("current = %d, want 4", gs.DeathLyricCurrent)
	}
	if len(gs.DeathLyricQueue) != 1 {
		t.Fatalf("queue = %v, want [4]", gs.DeathLyricQueue)
	}
	// seat 4 跳过
	if e := gs.SkipLastWords(4); e != nil {
		t.Fatalf("skip: %v", e)
	}
	if !done {
		t.Fatalf("onDone should be called")
	}
	if gs.Phase != PhaseSpeak {
		t.Fatalf("phase = %v, want speak", gs.Phase)
	}
	if gs.DeathLyricQueue != nil {
		t.Fatalf("queue should be nil after round ends")
	}
}

// TestDeathLyric_GameOverNoLyric 出局同时游戏结束 → 无遗言阶段。
func TestDeathLyric_GameOverNoLyric(t *testing.T) {
	gs := makeStartedGame(t, 105, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	// 设置:好人仅剩 2 人(seat 0,1), 狼人 seat 2; 放逐 seat 0 后好人=1, 还没结束。
	// 改为 killPlayer 触发 Status=over。
	gs.DayNumber = 1
	gs.Status = "over"
	called := false
	if e := gs.tryEnterDeathLyricRound([]Seat{Seat(2)}, func() *errcode.Error {
		called = true
		return nil
	}); e != nil {
		t.Fatalf("err: %v", e)
	}
	if !called {
		t.Fatalf("status=over, onDone should be called directly")
	}
}

// TestSetPhaseAndDeadline_WiresDeadlineOnAllPhaseTransitions (BUG-R70-P1)
//
// 验证:所有 `gs.Phase = PhaseX` 切换后立即挂上 PhaseDeadlineAt,PhaseDeadlineAt
// 与 cfgPhaseDeadlineSec(phase, seatCount) 一致,且 ≤ 30s (death_lyric ≤ 45s)。
// 之前的 SetPhaseDeadline 只定义未被调用,导致 watchdog 兜底 90s,R70 Day 1
// death_lyric 95s 才 skip。
func TestSetPhaseAndDeadline_WiresDeadlineOnAllPhaseTransitions(t *testing.T) {
	phases := []Phase{
		PhasePreWolves, PhaseNightWolves, PhaseNightSeer, PhaseNightWitch,
		PhaseDawn, PhaseSheriff, PhaseSpeak, PhaseVote,
		PhaseHunterShoot, PhaseDeathLyric, PhaseGameOver,
	}
	for _, p := range phases {
		gs := &GameState{}
		setPhaseAndDeadline(gs, p)
		if gs.Phase != p {
			t.Errorf("Phase not set: got %v, want %v", gs.Phase, p)
		}
		if gs.PhaseDeadlineAt.IsZero() {
			t.Errorf("PhaseDeadlineAt zero for %v — setPhaseAndDeadline should populate it", p)
		}
		want := cfgPhaseDeadlineSec(p.String(), gs.SeatCount)
		got := int(time.Until(gs.PhaseDeadlineAt).Seconds())
		// 允许 ±2s 测试执行偏差
		if abs(got-want) > 2 {
			t.Errorf("%v: deadline %ds, config wants %ds", p, got, want)
		}
	}
}

// TestSetPhaseAndDeadline_DeathLyricIsShort (BUG-R70-P1)
//
// 专项验证:death_lyric 阶段挂的 deadline 必须 ≤ 35s(默认 30s),
// 防止 R70 Day 1 95s 才 skip 的回归。
func TestSetPhaseAndDeadline_DeathLyricIsShort(t *testing.T) {
	gs := &GameState{}
	setPhaseAndDeadline(gs, PhaseDeathLyric)
	got := int(time.Until(gs.PhaseDeadlineAt).Seconds())
	if got > 35 {
		t.Fatalf("death_lyric deadline too long: %ds (want ≤ 35s)", got)
	}
	if got < 5 {
		t.Fatalf("death_lyric deadline too short: %ds (want ≥ 5s)", got)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ─────────────────── 12 人标准竞技局:白痴翻牌测试 ───────────────────

// TestIdiotReveal_Reveal_SurvivesAndLosesVote 白痴翻牌免死,失去投票权,直接进入黑夜。
func TestIdiotReveal_Reveal_SurvivesAndLosesVote(t *testing.T) {
	gs := makeStartedGame12(t, 200, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	// 把 seat 3 设为白痴(确保存在)。找到白痴座位。
	idiotSeat := Seat(-1)
	for i, r := range gs.Roles {
		if r == RoleIdiot {
			idiotSeat = Seat(i)
			break
		}
	}
	if idiotSeat < 0 {
		t.Fatalf("no idiot in roles: %+v", gs.Roles)
	}
	// 进入投票并制造「最高票为白痴」局面。
	gs.DayNumber = 1
	setPhaseAndDeadline(gs, PhaseVote)
	gs.DayEliminated = idiotSeat
	setPhaseAndDeadline(gs, PhaseIdiotReveal)

	if err := gs.IdiotReveal(idiotSeat, "reveal"); err != nil {
		t.Fatalf("IdiotReveal reveal: %v", err)
	}
	if !gs.Players[idiotSeat].IdiotRevealed {
		t.Fatalf("IdiotRevealed not set")
	}
	if !gs.Players[idiotSeat].Alive {
		t.Fatalf("idiot should survive reveal")
	}
	if gs.DayEliminated != NoSeat {
		t.Fatalf("DayEliminated should be cleared after reveal")
	}
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("phase = %v, want night_wolves", gs.Phase)
	}
	// 翻牌后白痴失去投票权:不可再被投,票不计入 tally。
	if err := gs.DayVote(Seat(0), idiotSeat); err == nil {
		t.Fatalf("should reject voting a revealed idiot")
	}
	gs.Players[0].Voted = false
	gs.Players[0].VoteTarget = NoSeat
	_ = gs.DayVote(Seat(0), idiotSeat) // returns err, ignore
	if tally := gs.TallyVotes(false); tally[idiotSeat] != 0 {
		t.Fatalf("revealed idiot should not receive votes, tally=%d", tally[idiotSeat])
	}
}

// TestIdiotReveal_Skip_NormalElimination 白痴放弃翻牌 → 正常放逐 + 遗言。
func TestIdiotReveal_Skip_NormalElimination(t *testing.T) {
	gs := makeStartedGame12(t, 201, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	idiotSeat := Seat(-1)
	for i, r := range gs.Roles {
		if r == RoleIdiot {
			idiotSeat = Seat(i)
			break
		}
	}
	if idiotSeat < 0 {
		t.Fatalf("no idiot in roles")
	}
	gs.DayNumber = 1
	setPhaseAndDeadline(gs, PhaseVote)
	gs.DayEliminated = idiotSeat
	setPhaseAndDeadline(gs, PhaseIdiotReveal)

	if err := gs.IdiotReveal(idiotSeat, "skip"); err != nil {
		t.Fatalf("IdiotReveal skip: %v", err)
	}
	if gs.Players[idiotSeat].Alive {
		t.Fatalf("idiot should be dead after skip")
	}
	// 放弃翻牌不设置 IdiotRevealed(只有 reveal 才设置)。
	if gs.Players[idiotSeat].IdiotRevealed {
		t.Fatalf("skip should NOT set IdiotRevealed")
	}
}

// TestIdiotReveal_OnlyEliminatedMayAct 仅最高票白痴可翻牌。
func TestIdiotReveal_OnlyEliminatedMayAct(t *testing.T) {
	gs := makeStartedGame12(t, 202, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	idiotSeat := Seat(-1)
	for i, r := range gs.Roles {
		if r == RoleIdiot {
			idiotSeat = Seat(i)
			break
		}
	}
	if idiotSeat < 0 {
		t.Fatalf("no idiot in roles")
	}
	gs.DayNumber = 1
	setPhaseAndDeadline(gs, PhaseVote)
	gs.DayEliminated = idiotSeat
	setPhaseAndDeadline(gs, PhaseIdiotReveal)

	// 其他座位不可触发翻牌。
	other := Seat(0)
	if other == idiotSeat {
		other = Seat(1)
	}
	if err := gs.IdiotReveal(other, "reveal"); err == nil {
		t.Fatalf("non-eliminated seat should not be allowed to reveal")
	}
}

// TestFinishVote_IdiotTopVote_GoesToIdiotReveal 最高票为存活白痴 → 不进放逐,进翻牌阶段。
func TestFinishVote_IdiotTopVote_GoesToIdiotReveal(t *testing.T) {
	gs := makeStartedGame12(t, 203, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	idiotSeat := Seat(-1)
	for i, r := range gs.Roles {
		if r == RoleIdiot {
			idiotSeat = Seat(i)
			break
		}
	}
	if idiotSeat < 0 {
		t.Fatalf("no idiot in roles")
	}
	gs.DayNumber = 1
	setPhaseAndDeadline(gs, PhaseVote)
	// 全员把票投给白痴 → 唯一最高票。
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) == idiotSeat {
			continue
		}
		gs.Players[i].Voted = true
		gs.Players[i].VoteTarget = idiotSeat
	}
	if err := gs.FinishVote(0); err != nil {
		t.Fatalf("FinishVote: %v", err)
	}
	if gs.Phase != PhaseIdiotReveal {
		t.Fatalf("phase = %v, want idiot_reveal", gs.Phase)
	}
	if gs.DayEliminated != idiotSeat {
		t.Fatalf("DayEliminated = %d, want %d", gs.DayEliminated, idiotSeat)
	}
	if !gs.Players[idiotSeat].Alive {
		t.Fatalf("idiot should still be alive before reveal")
	}
}

// ─────────────────── 12 人标准竞技局:警徽流测试 ───────────────────

// TestSheriffStreamDeclare_OnlySeerSheriff 仅预言家警长可声明警徽流。
func TestSheriffStreamDeclare_OnlySeerSheriff(t *testing.T) {
	gs := makeStartedGame12(t, 300, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	seer := Seat(-1)
	for i, r := range gs.Roles {
		if r == RoleSeer {
			seer = Seat(i)
			break
		}
	}
	if seer < 0 {
		t.Fatalf("no seer in roles")
	}
	gs.SheriffSeat = seer
	setPhaseAndDeadline(gs, PhaseSpeak)
	// 非法 slot。
	if err := gs.SheriffStreamDeclare(seer, 3, Seat(5)); err == nil {
		t.Fatalf("should reject slot=3")
	}
	// 正常声明第一警徽流。
	if err := gs.SheriffStreamDeclare(seer, 1, Seat(5)); err != nil {
		t.Fatalf("declare slot1: %v", err)
	}
	if gs.SheriffStreams[0] != Seat(5) {
		t.Fatalf("stream[0] = %d, want 5", gs.SheriffStreams[0])
	}
	// 撤回第二槽(未声明)也合法(target=-1)。
	if err := gs.SheriffStreamDeclare(seer, 2, NoSeat); err != nil {
		t.Fatalf("revoke slot2: %v", err)
	}
	// 不可声明自己。
	if err := gs.SheriffStreamDeclare(seer, 2, seer); err == nil {
		t.Fatalf("should reject stream self")
	}
	// 非警长不可声明。
	if err := gs.SheriffStreamDeclare(Seat(0), 1, Seat(5)); err == nil {
		t.Fatalf("non-sheriff should not declare")
	}
}

// TestSettleSheriffOnDeath_DoubleGold_DoubleKill 警徽流结算:双金水移交第一槽,双查杀撕。
// §20260810-04 U3 — 警徽流结算以「真实验人历史」为准;测试必须在 SheriffStreams
// 设置前,把对应目标写入 SeerCheckHistory,否则 streamFaction 返回 unknown → 撕警徽。
func TestSettleSheriffOnDeath_DoubleGold_DoubleKill(t *testing.T) {
	gs := makeStartedGame12(t, 301, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	seer := Seat(-1)
	var good1, good2, wolf1 Seat = -1, -1, -1
	for i, r := range gs.Roles {
		switch r {
		case RoleSeer:
			if seer < 0 {
				seer = Seat(i)
			}
		case RoleWerewolf:
			if wolf1 < 0 {
				wolf1 = Seat(i)
			}
		default:
			if good1 < 0 {
				good1 = Seat(i)
			} else if good2 < 0 {
				good2 = Seat(i)
			}
		}
	}
	if seer < 0 || good1 < 0 || good2 < 0 || wolf1 < 0 {
		t.Fatalf("missing roles: seer=%d good1=%d good2=%d wolf1=%d", seer, good1, good2, wolf1)
	}
	gs.SheriffSeat = seer
	// 构造房间用于结算。
	m := &WerewolfManager{}
	r := &WerewolfRoom{State: gs}

	// §20260810-04 U3 — 填充预言家的查验历史:本测试假设 seer 已真查过 good1/good2/wolf1
	// (历史包含这三个目标);否则 streamFaction 返回 unknown → 撕警徽。
	gs.Players[seer].SeerCheckHistory = []Seat{good1, good2, wolf1}

	// 双金水:应移交第一槽(good1)。
	gs.SheriffStreams = [2]Seat{good1, good2}
	gs.sheriffSlain = seer
	successor, ripped := r.SettleSheriffOnDeathLocked(gs)
	if ripped || successor != good1 {
		t.Fatalf("double-gold: successor=%d ripped=%v, want %d false", successor, ripped, good1)
	}

	// 双查杀:应撕警徽。
	var wolf2 Seat = -1
	for i, rr := range gs.Roles {
		if rr == RoleWerewolf && Seat(i) != wolf1 {
			wolf2 = Seat(i)
			break
		}
	}
	gs.SheriffStreams = [2]Seat{wolf1, wolf2}
	gs.sheriffSlain = seer
	successor, ripped = r.SettleSheriffOnDeathLocked(gs)
	if !ripped || successor != NoSeat {
		t.Fatalf("double-kill: successor=%d ripped=%v, want -1 true", successor, ripped)
	}

	// 一金一查杀:移交金水。
	gs.SheriffStreams = [2]Seat{good1, wolf1}
	gs.sheriffSlain = seer
	successor, ripped = r.SettleSheriffOnDeathLocked(gs)
	if ripped || successor != good1 {
		t.Fatalf("gold+kill: successor=%d ripped=%v, want %d false", successor, ripped, good1)
	}
	_ = m
}

// TestRefreshCounts_IdiotIsDivine 白痴计入神职数。
func TestRefreshCounts_IdiotIsDivine(t *testing.T) {
	gs := makeStartedGame12(t, 302, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	gs.refreshCounts()
	// 12 人局:4 神(预+女巫+猎+白痴)+4 民+4 狼。
	if gs.DivineCnt != 4 {
		t.Fatalf("DivineCnt = %d, want 4", gs.DivineCnt)
	}
	if gs.PlainCnt != 4 {
		t.Fatalf("PlainCnt = %d, want 4", gs.PlainCnt)
	}
	if gs.WolfAliveCnt != 4 {
		t.Fatalf("WolfAliveCnt = %d, want 4", gs.WolfAliveCnt)
	}
	if gs.GoodAliveCnt != 8 {
		t.Fatalf("GoodAliveCnt = %d, want 8", gs.GoodAliveCnt)
	}
}

// TestCheckWinner12_DivineWipe 屠神:4 神死光 → 狼人胜。
func TestCheckWinner12_DivineWipe(t *testing.T) {
	gs := makeStartedGame12(t, 303, fillSeats("p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9", "p10", "p11"))
	// 杀掉全部 4 神职。
	for i, r := range gs.Roles {
		switch r {
		case RoleSeer, RoleWitch, RoleHunter, RoleIdiot:
			gs.Players[i].Alive = false
		}
	}
	if !gs.checkWinner() {
		t.Fatalf("should have a winner after divine wipe")
	}
	if gs.Winner != "wolf" {
		t.Fatalf("winner = %q, want wolf", gs.Winner)
	}
}

// voteAllWolves 让所有存活狼人投票给 target(2026-07-17 投票机制 helper)。
// 用于测试中快速完成夜间投票并触发计票推进。
func voteAllWolves(gs *GameState, target Seat) *errcode.Error {
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			if e := gs.NightWolfKill(Seat(i), target, ""); e != nil {
				return e
			}
		}
	}
	return nil
}

// TestWolfVoteTally_Majority 验证多数决:2 狼均投同一目标 → majority。
func TestWolfVoteTally_Majority(t *testing.T) {
	gs := makeStartedGame(t, 200, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight()
	// 找一个合法目标
	var target Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] != RoleWerewolf && gs.AliveSeat(Seat(i)) {
			target = Seat(i)
			break
		}
	}
	// 所有狼人投同一目标 → 多数决
	if e := voteAllWolves(gs, target); e != nil {
		t.Fatalf("vote: %v", e)
	}
	if gs.WolfVoteTally == nil {
		t.Fatalf("expected tally after all wolves voted")
	}
	if gs.WolfKillTarget != target {
		t.Fatalf("kill target = %d, want %d", gs.WolfKillTarget, target)
	}
	if gs.WolfVoteTally.Reason != "majority" {
		t.Fatalf("reason = %q, want majority", gs.WolfVoteTally.Reason)
	}
	// 投票后应推进到下一阶段
	if gs.Phase != PhaseNightSeer && gs.Phase != PhaseNightWitch {
		t.Fatalf("phase = %v, want night_seer/night_witch", gs.Phase)
	}
}

// TestWolfVoteTally_AllAbstain 验证全弃权:所有狼人弃权 → 随机选择。
func TestWolfVoteTally_AllAbstain(t *testing.T) {
	gs := makeStartedGame(t, 201, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight()
	if e := voteAllWolves(gs, NoSeat); e != nil {
		t.Fatalf("vote: %v", e)
	}
	if gs.WolfVoteTally == nil {
		t.Fatalf("expected tally")
	}
	if gs.WolfVoteTally.Reason != "random_all_abstain" {
		t.Fatalf("reason = %q, want random_all_abstain", gs.WolfVoteTally.Reason)
	}
	// 最终目标应是存活非狼人
	if gs.WolfKillTarget == NoSeat {
		t.Fatalf("expected a target, got NoSeat")
	}
	if gs.Roles[gs.WolfKillTarget] == RoleWerewolf {
		t.Fatalf("target %d should not be a wolf", gs.WolfKillTarget)
	}
	if !gs.AliveSeat(gs.WolfKillTarget) {
		t.Fatalf("target %d should be alive", gs.WolfKillTarget)
	}
}

// TestWolfVoteView 验证 BuildWolfPeerView 正确反映投票状态。
func TestWolfVoteView(t *testing.T) {
	gs := makeStartedGame(t, 202, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight()
	// 找一个合法目标
	var target Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] != RoleWerewolf && gs.AliveSeat(Seat(i)) {
			target = Seat(i)
			break
		}
	}
	// 收集存活狼人
	wolves := []Seat{}
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			wolves = append(wolves, Seat(i))
		}
	}
	// 第一个狼人投票,其余弃权
	if e := gs.NightWolfKill(wolves[0], target, ""); e != nil {
		t.Fatalf("vote: %v", e)
	}
	// 投票中视图
	view := BuildWolfPeerView(gs)
	if view == nil {
		t.Fatalf("view should not be nil during voting")
	}
	if !view.Voting {
		t.Fatalf("expected voting=true")
	}
	if view.VotesCast != 1 {
		t.Fatalf("votes_cast = %d, want 1", view.VotesCast)
	}
	if view.TotalWolves != len(wolves) {
		t.Fatalf("total_wolves = %d, want %d", view.TotalWolves, len(wolves))
	}
	if view.Votes[int(wolves[0])] != int(target) {
		t.Fatalf("wolf %d vote = %d, want %d", wolves[0], view.Votes[int(wolves[0])], target)
	}
	// 其余狼人弃权 → 计票
	for i := 1; i < len(wolves); i++ {
		if e := gs.NightWolfKill(wolves[i], NoSeat, ""); e != nil {
			t.Fatalf("abstain: %v", e)
		}
	}
	// 结算后视图
	view2 := BuildWolfPeerView(gs)
	if view2 == nil {
		// 阶段已推进,视图为 nil 是正常的
		return
	}
	if view2.Voting {
		t.Fatalf("expected voting=false after tally")
	}
}

// TestNightWolfKill_DuplicateVoteRejected R196-P1 回归测试:
// 验证已投票(含弃权)的狼人再次调用 wolf_kill 一律被拒绝,防止 LLM 看不到
// 反馈陷入循环。原代码仅覆盖 WolfVotes[actor] 不报错,Bot 8 (GLM-5.2)
// 反复投票 15+ 次而无人察觉。
func TestNightWolfKill_DuplicateVoteRejected(t *testing.T) {
	gs := makeStartedGame(t, 202, fillSeats("a", "b", "c", "d", "e", "f", "g"))
	gs.startNight()

	// 找一个狼人 + 一个非狼目标
	var wolf Seat = NoSeat
	var target Seat = NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if wolf == NoSeat && gs.Roles[i] == RoleWerewolf && gs.AliveSeat(Seat(i)) {
			wolf = Seat(i)
		}
		if target == NoSeat && gs.Roles[i] != RoleWerewolf && gs.AliveSeat(Seat(i)) {
			target = Seat(i)
		}
		if wolf != NoSeat && target != NoSeat {
			break
		}
	}
	if wolf == NoSeat || target == NoSeat {
		t.Fatalf("setup failed: wolf=%d target=%d", wolf, target)
	}

	// 第一次投票必须成功
	if e := gs.NightWolfKill(wolf, target, ""); e != nil {
		t.Fatalf("first vote: %v", e)
	}
	if !gs.WolfVoteCast[wolf] {
		t.Fatalf("expected WolfVoteCast[%d]=true", wolf)
	}

	// 第二次投票必须被拒绝(ErrAlreadyWolfVoted)
	e := gs.NightWolfKill(wolf, target, "")
	if e == nil {
		t.Fatalf("second vote should fail")
	}
	if e.Code != errcode.ErrAlreadyWolfVoted {
		t.Fatalf("second vote err code = %d, want ErrAlreadyWolfVoted=%d", e.Code, errcode.ErrAlreadyWolfVoted)
	}

	// 换目标再投也应被拒绝
	e2 := gs.NightWolfKill(wolf, NoSeat, "")
	if e2 == nil {
		t.Fatalf("abstain after vote should fail")
	}
	if e2.Code != errcode.ErrAlreadyWolfVoted {
		t.Fatalf("abstain after vote err code = %d, want ErrAlreadyWolfVoted=%d", e2.Code, errcode.ErrAlreadyWolfVoted)
	}

	// 确认 WolfVotes[wolf] 没被覆盖
	if gs.WolfVotes[wolf] != target {
		t.Fatalf("WolfVotes[%d] = %d, want %d (must not be overwritten)", wolf, gs.WolfVotes[wolf], target)
	}
}

// TestWatchdogForceTally_R9P01 BUG-R9-P0-1 (2026-07-29):night_wolves 阶段
// 单座位 stuck(如 seat 4 Xiaomi-model 全程 LLM 失败)时,第一次 watchdog 超时
// 派发 wolf_kill skip 只能让该座位补一票,若该座位继续失败,反复 skip 会陷入
// 同一 phase+seat 的无限循环(R9 12:32→12:42 循环 6 次后触发 cooling 强制关房)。
// 修复:watchdog 第二次超时直接 tallyWolfVotes + endWolfPhase 强制推进 —— 与
// deadline 分支 (room.go:3782) 行为完全一致。
//
// 引擎层等价性验证:模拟"3 狼中 2 狼已投票 + 1 狼 stuck 未投票"的中间态,
// 调 tallyWolfVotes + endWolfPhase,断言:
//   1. WolfKillTarget 取已投票数的多数目标;
//   2. Phase 推进到 night_seer(或之后);
//   3. 不再卡在 night_wolves。
func TestWatchdogForceTally_R9P01(t *testing.T) {
	gs := makeStartedGame13(t, 4242, fillSeats("a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	gs.startNight()
	// 强制切到 night_wolves(若守卫在场会先经过 guard phase)。
	if gs.Phase == PhaseNightGuard {
		// 直接结束守卫阶段,等价于 guard_protect_skip。
		gs.GuardProtectTarget = NoSeat
		gs.GuardLastProtect = NoSeat
		gs.endGuardPhase()
	}
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("expected night_wolves, got %v", gs.Phase)
	}

	// 找出所有存活狼。
	wolves := []Seat{}
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] == RoleWerewolf {
			wolves = append(wolves, Seat(i))
		}
	}
	if len(wolves) < 2 {
		t.Skipf("need ≥2 wolves to simulate partial vote, got %d", len(wolves))
	}

	// 挑一个合法击杀目标(非狼、存活)。
	victim := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.AliveSeat(Seat(i)) && gs.Roles[i] != RoleWerewolf {
			victim = Seat(i)
			break
		}
	}
	if victim == NoSeat {
		t.Skip("no legal victim")
	}

	// 模拟:第一头狼正常投票;其余狼 stuck 未投票。
	// 等价于 watchdog 第一次 timeout 后 wolf_kill skip 已派发但仍未推进。
	gs.WolfVotes[wolves[0]] = victim
	gs.WolfVoteCast[wolves[0]] = true
	for _, w := range wolves[1:] {
		gs.WolfVotes[w] = NoSeat
		gs.WolfVoteCast[w] = false
	}

	// watchdog 第二次 timeout 触发:强制 tally + 推进。
	gs.tallyWolfVotes()
	gs.endWolfPhase()

	// 断言 1:最终击杀目标 = 第一头狼投的目标(唯一一票,多数胜出)。
	if gs.WolfKillTarget != victim {
		t.Fatalf("WolfKillTarget=%v, want %v (only one cast vote wins)", gs.WolfKillTarget, victim)
	}
	// 断言 2:阶段离开 night_wolves。
	if gs.Phase == PhaseNightWolves {
		t.Fatalf("phase still night_wolves after force tally — watchdog would loop again")
	}
	// 断言 3:进入合法下一阶段(seer / witch / dawn 都可能,取决于存活)。
	switch gs.Phase {
	case PhaseNightSeer, PhaseNightWitch, PhaseDawn, PhaseSpeak:
		// ok
	default:
		t.Fatalf("unexpected phase after force tally: %v", gs.Phase)
	}
}
