package werewolf

// engine_r212_p1_test.go — BUG-R212-P1-01 回归测试。
//
// 缺陷:13 人局创建者 seat 0 (真人) 在创建房间后立即 leave/spectate,但
// room_service_crud.LeaveRoom 只清 DB 不调 werewolfMgr.RemovePlayer →
// r.State.RemovePlayer 未被调用 → gs.Seats[0] 残留 userID,gs.Roles[0]
// 仍 = RoleWerewolf → firstLivingWolf 返回 0 → night_wolves acting seat
// = 0 (无 bot agent 可唤醒) → 阶段卡死至 watchdog 90s 兜底(实测 364s 才
// skip + 120s 后 force-tally,night_wolves 总耗 8 分钟)。
//
// 修复:在 firstLivingWolf / firstLivingSeer / firstLivingHunter /
// firstLivingDemonHunter 四个选择器加 HasActorAt 守卫,跳过无演员座位。
// HandleDisconnect 中 WitchSeat / GuardSeat 分支同步补 HasActorAt。
//
// 覆盖矩阵:
//
//	R-01 firstLivingWolf 跳过 Seats[i]=="" 但 Roles[i]==RoleWerewolf 的幽灵座位
//	R-02 firstLivingSeer 同上(RoleSeer)
//	R-03 firstLivingHunter 同上(RoleHunter)
//	R-04 firstLivingDemonHunter 同上(RoleDemonHunter)
//	R-05 全部狼座都是幽灵 → 返回 NoSeat(驱动 endWolfPhase)
//	R-06 多幽灵座位 → 返回首个"既有演员 + 存活 + 角色匹配"的座位
//	R-07 真人作为女巫 → firstLivingSeer 跳过(无女巫 NoSeat)
//	R-08 真人作为守卫 → firstLivingSeer 跳过
//	R-09 HasActorAt 边界:负数 / 越界 / 空座位 / 入座后
//	R-10 startNight 后 PhaseNightWolves 的 TurnActingSeat 跳过幽灵狼

import (
	"testing"
)

// newR212P1Room 构造一个 13 人局:seat 0 = 真人玩家(可指定角色),
// 其他 12 位是 bot;全部都已 AddPlayer 后再设角色。
func newR212P1Room(t *testing.T, humanSeat int, humanRole Role) *GameState {
	t.Helper()
	gs := NewGame(20260731)
	for i := 0; i < 13; i++ {
		uid := ""
		if i == humanSeat {
			uid = "human-creator"
		} else {
			uid = "bot-" + string(rune('a'+i))
		}
		if _, e := gs.AddPlayer(uid); e != nil {
			t.Fatalf("AddPlayer[%d] %s: %v", i, uid, e)
		}
	}
	roles := []Role{
		RoleWerewolf, RoleWerewolf, RoleWerewolf, RoleWerewolf, // 4 狼
		RoleSeer, RoleWitch, RoleHunter, RoleGuard, RoleDemonHunter, // 5 神
		RoleVillager, RoleVillager, RoleVillager, RoleVillager, // 4 民
	}
	if humanRole != RoleUnknown {
		roles[humanSeat] = humanRole
	}
	for i, r := range roles {
		gs.Roles[i] = r
	}
	return gs
}

// markGhostHumanSeatLocked 模拟「真人 leave → r.State.RemovePlayer 已调用
// 但 Roles[seat] 仍残留」的状态。这是修复后端 leave wiring 之后,服务端
// 调用顺序的正常路径:
//
//	room_service_crud.LeaveRoom → werewolfMgr.RemovePlayer → r.State.RemovePlayer
//	  → gs.Seats[seat]="" + gs.Players[seat]=Player{} + PlayerByID 删除
//	  → 但 Roles[seat] 保留(用于终局展示),Alive=false。
//
// 这个状态必须被 firstLivingWolf / 等选择器识别为「无演员」并跳过,
// 否则 night_wolves acting seat 会被错误指向该座位,导致 phase 卡死。
func markGhostHumanSeatLocked(gs *GameState, seat Seat) {
	if seat < 0 || seat >= MaxPlayers {
		return
	}
	delete(gs.PlayerByID, gs.Seats[seat])
	gs.Seats[seat] = ""       // r.State.RemovePlayer 的实际效果
	gs.Players[seat] = Player{} // 同上
	// 故意保留 gs.Roles[seat](用于终局展示);Alive 已因 Player{} 归零。
}

// ──────────────────── 单元测试 ────────────────────

func TestR212P1_FirstLivingWolf_SkipsGhostHuman(t *testing.T) {
	gs := newR212P1Room(t, 0, RoleWerewolf)
	markGhostHumanSeatLocked(gs, 0)
	got := firstLivingWolf(gs)
	if got == 0 {
		t.Fatalf("firstLivingWolf returned ghost seat 0 (human left but Seats[0] still set); want next living wolf")
	}
	if got != 1 {
		t.Fatalf("firstLivingWolf = %d, want 1 (first non-ghost wolf)", got)
	}
}

func TestR212P1_FirstLivingSeer_SkipsGhostHuman(t *testing.T) {
	gs := newR212P1Room(t, 4, RoleSeer)
	markGhostHumanSeatLocked(gs, 4)
	got := firstLivingSeer(gs)
	if got != NoSeat {
		t.Fatalf("firstLivingSeer = %d, want NoSeat (only seater was human ghost)", got)
	}
}

func TestR212P1_FirstLivingHunter_SkipsGhostHuman(t *testing.T) {
	gs := newR212P1Room(t, 6, RoleHunter)
	markGhostHumanSeatLocked(gs, 6)
	got := firstLivingHunter(gs)
	if got != NoSeat {
		t.Fatalf("firstLivingHunter = %d, want NoSeat (only hunter was human ghost)", got)
	}
}

func TestR212P1_FirstLivingDemonHunter_SkipsGhostHuman(t *testing.T) {
	gs := newR212P1Room(t, 8, RoleDemonHunter)
	markGhostHumanSeatLocked(gs, 8)
	got := firstLivingDemonHunter(gs)
	if got != NoSeat {
		t.Fatalf("firstLivingDemonHunter = %d, want NoSeat (only demon hunter was human ghost)", got)
	}
}

func TestR212P1_AllGhostWolves_ReturnsNoSeat(t *testing.T) {
	// 全部 4 个狼座都是真人 + leave → firstLivingWolf 必须 NoSeat,
	// 否则会进入 endWolfPhase 死循环或选错座位。
	gs := NewGame(20260731)
	for i := 0; i < 13; i++ {
		if _, e := gs.AddPlayer("ghost-wolf-" + string(rune('a'+i))); e != nil {
			t.Fatalf("AddPlayer: %v", e)
		}
	}
	for i := 0; i < 4; i++ {
		gs.Roles[i] = RoleWerewolf
		markGhostHumanSeatLocked(gs, Seat(i))
	}
	if got := firstLivingWolf(gs); got != NoSeat {
		t.Fatalf("firstLivingWolf with all wolves ghost = %d, want NoSeat (drive endWolfPhase)", got)
	}
}

func TestR212P1_MultipleGhosts_PicksNextRealActor(t *testing.T) {
	// seat 0/1 都是真人狼(都 leave),seat 2/3 是 bot 狼 → 应选 seat 2
	gs := newR212P1Room(t, 0, RoleWerewolf)
	gs.Roles[1] = RoleWerewolf
	markGhostHumanSeatLocked(gs, 0)
	markGhostHumanSeatLocked(gs, 1)
	if got := firstLivingWolf(gs); got != 2 {
		t.Fatalf("firstLivingWolf with 2 ghosts at start = %d, want 2", got)
	}
}

func TestR212P1_GhostWitch_NoSeer(t *testing.T) {
	// 真人作为女巫 → 女巫没演员 → firstLivingSeer NoSeat(预言家本来就是 bot)。
	// 这条测试只确认修复不会影响"角色座位"和"演员"两层语义。
	gs := newR212P1Room(t, 5, RoleWitch)
	markGhostHumanSeatLocked(gs, 5)
	if got := firstLivingSeer(gs); got != 4 {
		t.Fatalf("firstLivingSeer = %d, want 4 (bot seater at seat 4)", got)
	}
}

func TestR212P1_GhostGuard_NoDemonHunter(t *testing.T) {
	gs := newR212P1Room(t, 7, RoleGuard)
	markGhostHumanSeatLocked(gs, 7)
	if got := firstLivingDemonHunter(gs); got != 8 {
		t.Fatalf("firstLivingDemonHunter = %d, want 8 (bot demon hunter)", got)
	}
}

func TestR212P1_HasActorAt_Boundaries(t *testing.T) {
	gs := NewGame(20260731)
	if gs.HasActorAt(-1) {
		t.Errorf("HasActorAt(-1) = true, want false")
	}
	if gs.HasActorAt(MaxPlayers) {
		t.Errorf("HasActorAt(MaxPlayers) = true, want false")
	}
	if gs.HasActorAt(0) {
		t.Errorf("HasActorAt(0) on fresh room = true, want false")
	}
	if _, e := gs.AddPlayer("u1"); e != nil {
		t.Fatalf("AddPlayer: %v", e)
	}
	if !gs.HasActorAt(0) {
		t.Errorf("HasActorAt(0) after AddPlayer = false, want true")
	}
}

// R-10 startNight → PhaseNightWolves + TurnActingSeat 跳过幽灵狼
func TestR212P1_StartNight_SkipsGhostWolfForTurnActingSeat(t *testing.T) {
	gs := newR212P1Room(t, 0, RoleWerewolf)
	markGhostHumanSeatLocked(gs, 0)
	gs.startNight()
	if gs.Phase != PhaseNightWolves {
		t.Fatalf("startNight with no guard → Phase=%s, want PhaseNightWolves", gs.Phase)
	}
	if gs.TurnActingSeat == 0 {
		t.Fatalf("TurnActingSeat = 0 (ghost human wolf), want 1 (next bot wolf)")
	}
	if gs.TurnActingSeat != 1 {
		t.Fatalf("TurnActingSeat = %d, want 1", gs.TurnActingSeat)
	}
}

// R-11 全部幽灵狼时 startNight 应能继续推进(endWolfPhase 路径依赖 NoSeat)
func TestR212P1_StartNight_AllGhostWolves_TurnActingSeatNoSeat(t *testing.T) {
	gs := NewGame(20260731)
	for i := 0; i < 13; i++ {
		if _, e := gs.AddPlayer("ghost-" + string(rune('a'+i))); e != nil {
			t.Fatalf("AddPlayer: %v", e)
		}
	}
	for i := 0; i < 4; i++ {
		gs.Roles[i] = RoleWerewolf
		markGhostHumanSeatLocked(gs, Seat(i))
	}
	gs.startNight()
	if gs.TurnActingSeat != NoSeat {
		t.Fatalf("TurnActingSeat with all ghost wolves = %d, want NoSeat (NoSeat means engine will endWolfPhase)", gs.TurnActingSeat)
	}
}