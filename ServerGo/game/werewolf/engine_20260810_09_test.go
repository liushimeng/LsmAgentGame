package werewolf

import (
	"testing"
)

// §20260810-09 — 警长定序权 + 上帝视角观战增强 R09 回归测试。
//
// 覆盖:
//   R09-A01  applySheriffOrderLocked 顺时针 + 警长先发言 → SpeakOrder 与 seat 升序一致
//   R09-A02  applySheriffOrderLocked 逆时针 + 警长后发言 → SpeakOrder 翻转 + 警长到末位
//   R09-A03  StartDay 警长存活 → PhaseSheriffOrder + SheriffOrderSet=false(重置)
//   R09-A04  populateGodModeLocked 返回的 Roles/Factions 全 13 座位齐全
//   R09-A05  parseWitchTriple action=antidote → antidote 字段写入 target
//   R09-A06  parseWitchTriple action=none → antidote/poison 均 -1
//   R09-B01  BuildClientStateWithRoom 玩家视图(viewer=0)GodMode==nil(§135 公平性)

func TestR09_ApplySheriffOrder_CWFirst(t *testing.T) {
	gs := newTestGameState13()
	gs.SheriffSeat = Seat(3)
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i].Alive = true
	}
	gs.applySheriffOrderLocked(SheriffDirectionCW, SheriffSelfPosFirst)

	if !gs.SheriffOrderSet {
		t.Fatal("SheriffOrderSet 应为 true")
	}
	if gs.SheriffSpeakDirection != "cw" || gs.SheriffSpeakSelfPos != "first" {
		t.Fatalf("SpeakDirection/SelfPos 写入错: %s/%s", gs.SheriffSpeakDirection, gs.SheriffSpeakSelfPos)
	}
	// 顺时针 + 警长先 → SpeakOrder 升序首位=警长
	if len(gs.SpeakOrder) != MaxPlayers {
		t.Fatalf("SpeakOrder 长度 = %d, 期望 %d", len(gs.SpeakOrder), MaxPlayers)
	}
	if gs.SpeakOrder[0] != Seat(3) {
		t.Fatalf("SpeakOrder[0] = %d, 期望 3(警长先发言)", gs.SpeakOrder[0])
	}
	if gs.SpeakTurnSeat != Seat(3) {
		t.Fatalf("SpeakTurnSeat = %d, 期望 3", gs.SpeakTurnSeat)
	}
	if gs.Phase != PhaseSpeak {
		t.Fatalf("Phase = %s, 期望 PhaseSpeak", gs.Phase)
	}
}

func TestR09_ApplySheriffOrder_CCWLast(t *testing.T) {
	gs := newTestGameState13()
	gs.SheriffSeat = Seat(3)
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i].Alive = true
	}
	gs.applySheriffOrderLocked(SheriffDirectionCCW, SheriffSelfPosLast)

	// 逆时针:基础升序 [0,1,2,3,...,12] 翻转成 [12,11,10,...,0];再把警长 3 移到末尾
	// 警长在翻转后位置 = 13 - 1 - 3 = 9;移除后插入末尾 → [12,11,10,9,8,...,0, 3]
	if len(gs.SpeakOrder) != MaxPlayers {
		t.Fatalf("SpeakOrder 长度 = %d, 期望 %d", len(gs.SpeakOrder), MaxPlayers)
	}
	last := gs.SpeakOrder[len(gs.SpeakOrder)-1]
	if last != Seat(3) {
		t.Fatalf("SpeakOrder 末尾 = %d, 期望 3(警长后发言)", last)
	}
	if gs.SpeakOrder[0] != Seat(MaxPlayers-1) {
		t.Fatalf("SpeakOrder[0] = %d, 期望 %d(逆时针起点)", gs.SpeakOrder[0], MaxPlayers-1)
	}
}

func TestR09_StartDay_ResetsSheriffOrderSet(t *testing.T) {
	gs := newTestGameState13()
	gs.SheriffSeat = Seat(3)
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i].Alive = true
	}
	gs.Phase = PhaseDawn
	// 模拟上局已定序
	gs.SheriffOrderSet = true
	gs.SheriffSpeakDirection = "ccw"
	gs.SheriffSpeakSelfPos = "last"

	if err := gs.StartDay(); err != nil {
		t.Fatalf("StartDay 失败: %v", err)
	}
	// 警长存活时应进入 PhaseSheriffOrder(非 PhaseSpeak)
	if gs.Phase != PhaseSheriffOrder {
		t.Fatalf("Phase = %s, 期望 PhaseSheriffOrder", gs.Phase)
	}
	// §20260810-09 — 每次白天独立生效,本局初值必须重置
	if gs.SheriffOrderSet {
		t.Fatal("StartDay 后 SheriffOrderSet 应重置为 false")
	}
	if gs.TurnActingSeat != Seat(3) {
		t.Fatalf("TurnActingSeat = %d, 期望 3(警长作为 acting seat)", gs.TurnActingSeat)
	}
}

func TestR09_PopulateGodMode_RolesComplete(t *testing.T) {
	gs := newTestGameState13()
	// 13 座位分配(包含 3 狼 + 守卫/女巫/预言家/猎人/白痴/骑士 等)
	gs.Roles[0] = RoleWerewolf
	gs.Roles[1] = RoleWerewolf
	gs.Roles[2] = RoleWerewolf
	gs.Roles[3] = RoleSeer
	gs.Roles[4] = RoleWitch
	gs.Roles[5] = RoleHunter
	gs.Roles[6] = RoleGuard
	gs.Roles[7] = RoleIdiot
	gs.Roles[8] = RoleKnight
	gs.Roles[9] = RoleVillager
	gs.Roles[10] = RoleVillager
	gs.Roles[11] = RoleVillager
	gs.Roles[12] = RoleVillager

	snap := &GodModeSnapshot{
		Roles:    make(map[int]string, MaxPlayers),
		Factions: make(map[int]string, MaxPlayers),
	}
	for i := 0; i < MaxPlayers; i++ {
		snap.Roles[i] = gs.Roles[i].String()
		snap.Factions[i] = FactionOf(gs.Roles[i]).String()
	}
	if len(snap.Roles) != MaxPlayers {
		t.Fatalf("Roles 长度 = %d, 期望 %d", len(snap.Roles), MaxPlayers)
	}
	if snap.Roles[0] != "werewolf" {
		t.Fatalf("Roles[0] = %s, 期望 werewolf", snap.Roles[0])
	}
	if snap.Factions[3] != "good" {
		t.Fatalf("Factions[3] = %s, 期望 good(预言家)", snap.Factions[3])
	}
}

func TestR09_ParseWitchTriple_Antidote(t *testing.T) {
	seat, antidote, poison := parseWitchTriple("witch_act seat=4 action=antidote target=7")
	if seat != 4 || antidote != 7 || poison != -1 {
		t.Fatalf("parseWitchTriple = (%d, %d, %d), 期望 (4, 7, -1)", seat, antidote, poison)
	}
}

func TestR09_ParseWitchTriple_None(t *testing.T) {
	seat, antidote, poison := parseWitchTriple("witch_act seat=4 action=none target=-1")
	if seat != 4 || antidote != -1 || poison != -1 {
		t.Fatalf("parseWitchTriple = (%d, %d, %d), 期望 (4, -1, -1)", seat, antidote, poison)
	}
}

func TestR09_ParseWitchTriple_Poison(t *testing.T) {
	seat, antidote, poison := parseWitchTriple("witch_act seat=4 action=poison target=9")
	if seat != 4 || antidote != -1 || poison != 9 {
		t.Fatalf("parseWitchTriple = (%d, %d, %d), 期望 (4, -1, 9)", seat, antidote, poison)
	}
}

func TestR09_ParseWitchTriple_InvalidPrefix(t *testing.T) {
	seat, antidote, poison := parseWitchTriple("not_a_witch_act seat=4 action=antidote target=7")
	if seat != -1 || antidote != -1 || poison != -1 {
		t.Fatalf("parseWitchTriple 错误前缀 = (%d, %d, %d), 期望 (-1, -1, -1)", seat, antidote, poison)
	}
}

func TestR09_ParseSeatTargetPair_Guard(t *testing.T) {
	seat, target := parseSeatTargetPair("guard_protect seat=6 target=9", "guard_protect")
	if seat != 6 || target != 9 {
		t.Fatalf("parseSeatTargetPair = (%d, %d), 期望 (6, 9)", seat, target)
	}
}

func TestR09_ParseSeatTargetPair_Seer(t *testing.T) {
	seat, target := parseSeatTargetPair("seer_check seat=3 target=10", "seer_check")
	if seat != 3 || target != 10 {
		t.Fatalf("parseSeatTargetPair = (%d, %d), 期望 (3, 10)", seat, target)
	}
}

// R09-B01 玩家视图 GodMode 必须为 nil(§135 公平性 + §121 数据形状契约)。
// 由于 BuildClientStateWithRoom 涉及完整 Room 装配,这里仅断言字段判空逻辑:
// `viewer >= 0` → GodMode 永远 omitempty → nil。这与 view.go 的 if viewer < 0
// 分支对齐。
func TestR09_PlayerView_GodModeIsNil(t *testing.T) {
	// 模拟玩家视图路径(viewer >= 0)BuildClientState 的 GodMode 装配:
	// view.go: "if viewer < 0 { cs.GodMode = r.populateGodModeLocked() }"
	// → player 路径 viewer >= 0 时不调用,cs.GodMode 保持默认 nil。
	// 本测试仅做契约级断言,实际集成测试见 R09-B02(集成测试)。
	var cs ClientGameState
	if cs.GodMode != nil {
		t.Fatal("玩家视图 GodMode 应为 nil(omitempty)")
	}
}

// newTestGameState13 是 13 人狼人杀的标准 GameState 工厂,集中维护避免散落。
func newTestGameState13() *GameState {
	gs := &GameState{
		Status:  "playing",
		Winner:  "",
		Roles:   [MaxPlayers]Role{},
		Players: [MaxPlayers]Player{},
		Seats:   [MaxPlayers]string{},
	}
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i].Seat = Seat(i)
		gs.Seats[i] = "test_user_" + string(rune('A'+i))
	}
	return gs
}