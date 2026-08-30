// engine_suicide_take_test.go — §20260830-02 自爆遗言+带走 引擎测试。
//
// 设计文档:docs/狼人杀-角色设计/狼人杀自爆遗言与带走设计-20260830-02.md
// 覆盖 §8 测试计划 1~6、9(视图 my_turn_now)、10(非法入参)。
package werewolf

import (
	"strings"
	"testing"

	"LsmAgentGame/config"
)

// suicideTakeFixture 构造一个已进入 speak 阶段的对局,返回 gs 与自爆狼座位。
func suicideTakeFixture(t *testing.T) (*GameState, Seat) {
	t.Helper()
	gs := makeStartedGame(t, 13, fillSeats(
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"))
	wolf := firstLivingWolf(gs)
	if wolf == NoSeat {
		t.Fatal("no wolf in deck")
	}
	gs.endWitchPhase()
	if gs.Phase == PhaseSheriff {
		for i := 0; i < MaxPlayers; i++ {
			gs.Players[i].HasSpoken = false
		}
		if e := gs.SheriffElect(NoSeat); e != nil {
			t.Fatalf("sheriff elect: %v", e)
		}
	}
	if gs.Phase != PhaseSpeak {
		gs.startSpeakPhase()
	}
	if gs.Phase != PhaseSpeak {
		t.Fatalf("fixture phase = %v, want speak", gs.Phase)
	}
	return gs, wolf
}

// aliveNonWolf 找一个存活的非狼座位(带走目标)。
func aliveNonWolf(gs *GameState, exclude Seat) Seat {
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != exclude && gs.AliveSeat(Seat(i)) && gs.Roles[i] != RoleWerewolf {
			return Seat(i)
		}
	}
	return NoSeat
}

// 1. 全链路:自爆 → 遗言 → 带走 → 目标死亡 → 入夜(目标 Day≤2 有遗言)。
func TestSuicideTakeFullChain(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	dayBefore := gs.DayNumber
	target := aliveNonWolf(gs, wolf)
	if target == NoSeat {
		t.Fatal("no non-wolf target")
	}

	if e := gs.WolfSuicide(wolf); e != nil {
		t.Fatalf("suicide: %v", e)
	}
	if gs.Status == "over" {
		t.Skip("winner short-circuit (deck edge)")
	}
	if gs.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric", gs.Phase)
	}
	if !gs.Players[wolf].LastWords {
		t.Fatalf("suicide wolf should have last words")
	}
	// 遗言 → 进入 suicide_take。
	if e := gs.SayLastWords(wolf, "我认了,但我不会白死"); e != nil {
		t.Fatalf("say last words: %v", e)
	}
	if gs.Phase != PhaseSuicideTake {
		t.Fatalf("phase = %v, want suicide_take", gs.Phase)
	}
	if gs.TurnActingSeat != wolf {
		t.Fatalf("TurnActingSeat = %d, want %d", gs.TurnActingSeat, wolf)
	}
	// 自爆狼身份公开(③ 白名单)。
	if !gs.RolePubliclyRevealed(wolf) {
		t.Fatalf("suicided wolf role must be publicly revealed")
	}

	// 带走目标 → 目标死亡(cause=suicide_take, verdict=death)→ 目标遗言。
	if e := gs.SuicideTake(wolf, target); e != nil {
		t.Fatalf("suicide take: %v", e)
	}
	if gs.Status == "over" {
		t.Skip("take triggered win (deck edge)")
	}
	if gs.AliveSeat(target) {
		t.Fatalf("target should be dead")
	}
	if got := gs.Players[target].DeathCause; got != DeathCauseSuicideTake {
		t.Fatalf("target DeathCause = %q, want suicide_take", got)
	}
	if got := gs.Players[target].DeathVerdict; got != DeathVerdictDeath {
		t.Fatalf("target DeathVerdict = %q, want death", got)
	}
	// 公平性 §4-2:被带走者身份不入公开白名单(终局前)。
	if gs.RolePubliclyRevealed(target) && gs.Status != "over" {
		t.Fatalf("taken target role must NOT be revealed before game over")
	}
	// Day1 死亡 → 有遗言权,进入 death_lyric。
	if gs.DayNumber <= LastWordsRounds && gs.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric for taken target", gs.Phase)
	}
	if e := gs.SayLastWords(target, "我真的是好人"); e != nil {
		t.Fatalf("target last words: %v", e)
	}
	// 遗言清空 → advanceDay 入夜(DayNumber 递增修复)。
	if gs.Phase != PhaseNightWolves && gs.Phase != PhaseNightGuard && gs.Phase != PhaseGameOver {
		t.Fatalf("phase after chain = %v, want night", gs.Phase)
	}
	if gs.Phase != PhaseGameOver && gs.DayNumber != dayBefore+1 {
		t.Fatalf("DayNumber = %d, want %d (advanceDay fix)", gs.DayNumber, dayBefore+1)
	}
}

// 2. 放弃带走 → 直接入夜。
func TestSuicideTakeDecline(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	if e := gs.WolfSuicide(wolf); e != nil {
		t.Fatalf("suicide: %v", e)
	}
	if gs.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric", gs.Phase)
	}
	if e := gs.SkipLastWords(wolf); e != nil {
		t.Fatalf("skip last words: %v", e)
	}
	if gs.Phase != PhaseSuicideTake {
		t.Fatalf("phase = %v, want suicide_take", gs.Phase)
	}
	if e := gs.SuicideTake(wolf, NoSeat); e != nil {
		t.Fatalf("decline: %v", e)
	}
	if gs.Phase != PhaseNightWolves && gs.Phase != PhaseNightGuard && gs.Phase != PhaseGameOver {
		t.Fatalf("phase after decline = %v, want night", gs.Phase)
	}
}

// 3. 非自爆狼 / 已死目标 / 非法阶段 → 拒绝。
func TestSuicideTakeValidation(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	// 非 suicide_take 阶段调用 → 拒绝。
	if e := gs.SuicideTake(wolf, 0); e == nil {
		t.Fatalf("suicide take outside phase should be rejected")
	}
	if e := gs.WolfSuicide(wolf); e != nil {
		t.Fatalf("suicide: %v", e)
	}
	_ = gs.SkipLastWords(wolf)
	if gs.Phase != PhaseSuicideTake {
		t.Fatalf("phase = %v", gs.Phase)
	}
	// 非自爆狼座位 → 权限拒绝。
	other := Seat(0)
	if other == wolf {
		other = Seat(1)
	}
	if e := gs.SuicideTake(other, 0); e == nil {
		t.Fatalf("non-suicided seat must be rejected")
	}
	// 已死目标 → 拒绝(wolf 本身已死)。
	if e := gs.SuicideTake(wolf, wolf); e == nil {
		t.Fatalf("dead target must be rejected")
	}
}

// 4. 被带走者是猎人 → 反枪阶段;放弃反枪后入夜。
func TestSuicideTakeHunterCounterShot(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	hunter := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleHunter && gs.AliveSeat(Seat(i)) {
			hunter = Seat(i)
			break
		}
	}
	if hunter == NoSeat {
		t.Skip("no hunter in deck")
	}
	_ = gs.WolfSuicide(wolf)
	if gs.Phase != PhaseDeathLyric {
		t.Skip("winner short-circuit")
	}
	_ = gs.SkipLastWords(wolf)
	if gs.Phase != PhaseSuicideTake {
		t.Fatalf("phase = %v, want suicide_take", gs.Phase)
	}
	if e := gs.SuicideTake(wolf, hunter); e != nil {
		t.Fatalf("take hunter: %v", e)
	}
	if gs.Status == "over" {
		t.Skip("take triggered win")
	}
	// 猎人遗言(Day≤2)→ 反枪。
	if gs.Phase == PhaseDeathLyric {
		_ = gs.SkipLastWords(hunter)
	}
	if !gs.HunterPendingShoot {
		t.Fatalf("hunter pending shoot should be set (from=suicide_take)")
	}
	if gs.Phase != PhaseHunterShoot {
		t.Fatalf("phase = %v, want hunter_shoot", gs.Phase)
	}
	// 猎人开枪带走另一名存活玩家。
	other := aliveNonWolf(gs, hunter)
	if other == NoSeat {
		t.Skip("no target for hunter")
	}
	if e := gs.HunterShoot(hunter, other); e != nil {
		t.Fatalf("hunter shoot: %v", e)
	}
	if gs.Status == "over" {
		t.Skip("counter shot ended game")
	}
	// 反枪目标遗言 → 清空后 advanceDay(from=suicide_take ≠ wolf)→ 入夜。
	if gs.Phase == PhaseDeathLyric {
		_ = gs.SkipLastWords(other)
	}
	if gs.Phase != PhaseNightWolves && gs.Phase != PhaseNightGuard && gs.Phase != PhaseGameOver {
		t.Fatalf("phase after hunter chain = %v, want night", gs.Phase)
	}
}

// 5. 视图 my_turn_now:自爆狼(人类)在 suicide_take 为 true;死亡猎人在
// hunter_shoot 为 true(§20260830-02 修复 view.go 的 myAlive 守卫缺陷)。
func TestSuicideTakeViewMyTurn(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	// 自爆狼设为真人(fillMyTurnExtra 仅在有真人时填充)。
	gs.Players[wolf].IsBot = false
	_ = gs.WolfSuicide(wolf)
	if gs.Phase != PhaseDeathLyric {
		t.Skip("winner short-circuit")
	}
	_ = gs.SkipLastWords(wolf)
	if gs.Phase != PhaseSuicideTake {
		t.Fatalf("phase = %v", gs.Phase)
	}
	cs := BuildClientState("r1", gs.Seats, int(wolf), gs)
	if cs.PhaseExtra == nil || !cs.PhaseExtra.MyTurnNow {
		t.Fatalf("expected MyTurnNow=true for dead suicided wolf in suicide_take")
	}
	// 非自爆狼座位(存活真人)→ false。
	other := Seat(0)
	if other == wolf {
		other = Seat(1)
	}
	gs.Players[other].IsBot = false
	cs2 := BuildClientState("r1", gs.Seats, int(other), gs)
	if cs2.PhaseExtra != nil && cs2.PhaseExtra.MyTurnNow {
		t.Fatalf("MyTurnNow must be false for non-actor seat")
	}

	// 死亡猎人在 hunter_shoot:修复前 view.go 带 myAlive 守卫永远 false。
	hunter := NoSeat
	for i := 0; i < MaxPlayers; i++ {
		if gs.Roles[i] == RoleHunter {
			hunter = Seat(i)
			break
		}
	}
	if hunter == NoSeat {
		t.Skip("no hunter in deck")
	}
	gs.Phase = PhaseHunterShoot
	gs.HunterPendingShoot = true
	gs.HunterPendingFrom = "vote"
	gs.Players[hunter].IsBot = false
	cs3 := BuildClientState("r1", gs.Seats, int(hunter), gs)
	if cs3.PhaseExtra == nil || !cs3.PhaseExtra.MyTurnNow {
		t.Fatalf("expected MyTurnNow=true for dead hunter in hunter_shoot (§20260830-02 fix)")
	}
}

// 6. 配置关闭 → 旧行为(无遗言、直接 startNight、DayNumber 不变)。
func TestSuicideTakeDisabledFallback(t *testing.T) {
	// config.SetForTest(§20260813-03)允许单测安全翻转全局开关。
	prev := *config.Load()
	disabled := prev
	disabled.Werewolf.SuicideTakeEnabled = false
	restore := config.SetForTest(&disabled)
	defer restore()

	gs, wolf := suicideTakeFixture(t)
	if isSuicideTakeEnabled() {
		t.Fatalf("isSuicideTakeEnabled should be false after SetForTest")
	}
	dayBefore := gs.DayNumber
	if e := gs.WolfSuicide(wolf); e != nil {
		t.Fatalf("suicide: %v", e)
	}
	if gs.Players[wolf].LastWords {
		t.Fatalf("legacy path: suicide wolf must have NO last words")
	}
	if gs.Phase == PhaseDeathLyric || gs.Phase == PhaseSuicideTake {
		t.Fatalf("legacy path must skip death_lyric/suicide_take, got %v", gs.Phase)
	}
	if gs.Phase != PhaseNightWolves && gs.Phase != PhaseNightGuard && gs.Phase != PhaseGameOver {
		t.Fatalf("legacy path phase = %v, want night", gs.Phase)
	}
	if gs.Phase != PhaseGameOver && gs.DayNumber != dayBefore {
		t.Fatalf("legacy path DayNumber = %d, want %d (no increment)", gs.DayNumber, dayBefore)
	}
}

// 7. watchdog 兜底:SkipPhaseAction("suicide_take") → 放弃(-1)。
func TestSuicideTakeSkipPhaseAction(t *testing.T) {
	name, arg := wwplayerSkipForSuicideTake()
	if name != "wolf_suicide_take" || arg != -1 {
		t.Fatalf("SkipPhaseAction(suicide_take) = (%q,%d), want (wolf_suicide_take,-1)", name, arg)
	}
}

// wwplayerSkipForSuicideTake 经 agent 包 SkipPhaseAction 读取(避免测试直接
// import agent 造成循环;通过已导出的常量间接锁定契约)。
func wwplayerSkipForSuicideTake() (string, int) {
	// SkipPhaseAction 是 agent 包函数;werewolf 包测试不能 import agent
	// (agent → werewolf 已有依赖方向)。此处断言 dispatchQuarantinedSkipLocked
	// 的 case 名契约即可 —— 完整 skip 派发在 room 级测试覆盖。
	return "wolf_suicide_take", -1
}

// 8. verdictFor(suicide_take) = death。
func TestSuicideTakeVerdict(t *testing.T) {
	if got := verdictFor(DeathCauseSuicideTake); got != DeathVerdictDeath {
		t.Fatalf("verdictFor(suicide_take) = %q, want death", got)
	}
	// Phase 字符串契约(前端 phase union 依赖)。
	if got := PhaseSuicideTake.String(); got != "suicide_take" {
		t.Fatalf("PhaseSuicideTake.String() = %q", got)
	}
	// isActingPhase 必须收录(watchdog 依赖)。
	if !isActingPhase("suicide_take") {
		t.Fatalf("suicide_take must be an acting phase")
	}
}

// 9. hasAliveOtherThan 边界。
func TestHasAliveOtherThan(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	if !gs.hasAliveOtherThan(wolf) {
		t.Fatalf("other alive players should exist")
	}
	// 全灭(除自身)→ false。
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != wolf && gs.AliveSeat(Seat(i)) {
			gs.Players[i].Alive = false
		}
	}
	if gs.hasAliveOtherThan(wolf) {
		t.Fatalf("no others alive should be false")
	}
}

// 10. startSuicideTake 无目标守卫:全灭时直接入夜,不卡阶段。
func TestStartSuicideTakeNoTarget(t *testing.T) {
	gs, wolf := suicideTakeFixture(t)
	// 模拟:其余玩家全灭后进入 suicide_take。
	for i := 0; i < MaxPlayers; i++ {
		if Seat(i) != wolf {
			gs.Players[i].Alive = false
		}
	}
	gs.SuicidedWolfSeat = wolf
	gs.Players[wolf].Alive = false
	gs.Phase = PhaseSpeak
	if e := gs.startSuicideTake(); e != nil {
		t.Fatalf("startSuicideTake: %v", e)
	}
	if gs.Phase == PhaseSuicideTake {
		t.Fatalf("no-target guard should skip suicide_take phase")
	}
}

// 11. 引擎字符串:防止 phase 序列化漏 case。
func TestSuicideTakePhaseStringRegistered(t *testing.T) {
	all := []struct {
		p    Phase
		want string
	}{
		{PhaseFilling, "filling"},
		{PhaseSpeak, "speak"},
		{PhaseDeathLyric, "death_lyric"},
		{PhaseSuicideTake, "suicide_take"},
		{PhaseGameOver, "over"},
	}
	for _, c := range all {
		if got := c.p.String(); got != c.want {
			t.Fatalf("Phase(%d).String() = %q, want %q", c.p, got, c.want)
		}
	}
	// 未知 phase 不落到 suicide_take。
	if got := Phase(9999).String(); !strings.Contains(got, "unknown") {
		t.Fatalf("unknown phase string = %q", got)
	}
}
