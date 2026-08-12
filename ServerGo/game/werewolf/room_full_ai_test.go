// Package werewolf — full-AI-room regression tests for
// BUG-WEREWOLF-FULL-AI-WAITING.
//
// §65a CreateRoomWithAgents downgrades the human creator to a spectator row
// when len(agentSeats) == 7. The server force-starts the room BEFORE the
// creator's WS subscribes; the create → join → broadcast race means the
// creator's `game.join` envelope arrives AFTER Phase has already left Filling.
//
// These tests pin down the contract that the in-memory WerewolfManager enforces
// (and the WS layer's handleWerewolfJoin downgrade piggybacks on):
//
//   - JoinGame on a Phase != Filling room MUST return ErrRoomFull so the WS
//     layer can pattern-match it and fall back to SpectateGame.
//   - SpectateGame on the SAME room MUST accept the user as a spectator and
//     leave them out of Seats (no seat stealing).
//   - The two calls together (= what handleWerewolfJoin does after the bug
//     fix) MUST end up with the user in Spectators and Not in Seats.
//
// We avoid DB dependencies by injecting a fixed seedFn so the test is
// deterministic and the agent goroutines aren't actually started (we never
// call StartAgentsLocked — see WerewolfRoom vs StartGame interaction below).
package werewolf

import (
	"fmt"
	"testing"

	"LsmWebGame/errcode"
)

// stubWWMgr builds a WerewolfManager with seedFn = func() int64 { return 1 }
// so tests are deterministic. agentFactory stays nil → JoinGame calls
// StartAgentsLocked but no goroutines actually run; the lock is still acquired
// and released, and the test never blocks.
func stubWWMgr() *WerewolfManager {
	m := NewWerewolfManager()
	m.seedFn = func() int64 { return 1 }
	return m
}

// fillAndStart pre-fills all 7 seats with bot userIDs and starts the game,
// returning the roomID and a *WerewolfRoom whose State.Phase is no longer
// PhaseFilling.按 werewolf_7 兼容模式(SeatCount=7)发牌。
func fillAndStart(t *testing.T, m *WerewolfManager) (string, *WerewolfRoom) {
	t.Helper()
	const roomID = "test-room-full-ai"
	// 7 人兼容模式:显式设定 SeatCount=7,使 IsReady 在 7 人时触发开局。
	// 在房间首次创建时同步 JoinGame 自动开局路径中的 State.SeatCount。
	m.SetSeatCount(roomID, 7)
	// Bot IDs sit in Seats 0..6; creator arrives later as the 8th player.
	bots := []string{"bot_0", "bot_1", "bot_2", "bot_3", "bot_4", "bot_5", "bot_6"}
	for i, b := range bots {
		_, _, _ = m.JoinGame(roomID, b)
		// 首次 JoinGame 创建房间后,确保 SeatCount=7 同步到房间与 State。
		if i == 0 {
			m.SetSeatCount(roomID, 7)
		}
	}
	r := m.getRoom(roomID)
	if r == nil {
		t.Fatalf("room %s not initialised by JoinGame", roomID)
	}
	if r.State == nil || r.State.Phase == PhaseFilling {
		t.Fatalf("expected Phase != PhaseFilling after 7 joins, got %v", r.State)
	}
	return roomID, r
}

// TestActionUseProp_DeadPlayerReturnsSpecificErrorCode 验证 R173 P2 修复:
// 死亡玩家调 Action_UseProp 必须返回 ErrPropPlayerDead(40111) 而非泛化
// ErrValidationFailed(20001),让前端能明确提示"死亡玩家不能使用道具"。
func TestActionUseProp_DeadPlayerReturnsSpecificErrorCode(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)
	// 把 seat 0 标记为已死亡(AliveSeat 检查 Seats!="" && Players[seat].Alive)。
	r.mu.Lock()
	if r.State.Seats[0] == "" {
		t.Fatal("seat 0 should be occupied by bot_0")
	}
	r.State.Players[0].Alive = false
	r.mu.Unlock()
	// 引擎注入(m.propEngine 可能 nil,但 AliveSeat 检查在引擎调用前)
	m.SetPropEngine(&PropEngine{walletSvc: nil})
	_, _, em := m.Action_UseProp(roomID, "bot_0", "char_confuse", 5, "")
	if em == nil {
		t.Fatal("Action_UseProp on dead player should return error")
	}
	if em.Code != errcode.ErrPropPlayerDead {
		t.Errorf("dead player Action_UseProp code = %d, want %d (ErrPropPlayerDead); msg=%q",
			em.Code, errcode.ErrPropPlayerDead, em.Message)
	}
}

// fillAndStart12 pre-fills all 12 seats with bot userIDs in 12 人标准竞技局
// 模式(SeatCount=12)并开局。
func fillAndStart12(t *testing.T, m *WerewolfManager) (string, *WerewolfRoom) {
	t.Helper()
	const roomID = "test-room-full-ai-12"
	m.SetSeatCount(roomID, 12)
	bots := make([]string, 12)
	for i := 0; i < 12; i++ {
		bots[i] = fmt.Sprintf("bot_%d", i)
	}
	for _, b := range bots {
		_, _, _ = m.JoinGame(roomID, b)
	}
	r := m.getRoom(roomID)
	if r == nil {
		t.Fatalf("room %s not initialised by JoinGame", roomID)
	}
	if r.State == nil || r.State.Phase == PhaseFilling {
		t.Fatalf("expected Phase != PhaseFilling after 12 joins, got %v", r.State)
	}
	return roomID, r
}

// TestJoinGame_AfterStartReturnsRoomFull is the core contract: a 8th
// JoinGame on a started werewolf room MUST return ErrRoomFull so the WS
// layer can branch into the spectate fallback path (BUG-WEREWOLF-FULL-AI-WAITING
// fix in handleWerewolfJoin).
func TestJoinGame_AfterStartReturnsRoomFull(t *testing.T) {
	m := stubWWMgr()
	roomID, _ := fillAndStart(t, m)

	room, started, e := m.JoinGame(roomID, "human-creator")
	if e == nil {
		t.Fatalf("expected error, got success (room=%v started=%v)", room, started)
	}
	if e.Code != errcode.ErrRoomFull {
		t.Fatalf("expected ErrRoomFull (%d), got %d (%s)",
			errcode.ErrRoomFull, e.Code, e.Message)
	}
	if started {
		t.Fatalf("expected started=false on ErrRoomFull, got true")
	}
	if room != nil {
		t.Fatalf("expected nil room on ErrRoomFull, got %v", room)
	}
}

// TestSpectateGame_AfterStartAcceptsUser verifies the fallback path: the
// user who was rejected by JoinGame can attach as a spectator to the same
// room (no DB touched, no seats stolen).
func TestSpectateGame_AfterStartAcceptsUser(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	room, e := m.SpectateGame(roomID, "human-creator")
	if e != nil {
		t.Fatalf("expected success from SpectateGame after JoinGame rejection, got %d (%s)",
			e.Code, e.Message)
	}
	if room == nil {
		t.Fatalf("expected non-nil room from SpectateGame")
	}

	// Crucially: the user must NOT occupy a seat (they're observing, not playing).
	for seat, uid := range r.Seats {
		if uid == "human-creator" {
			t.Fatalf("spectator leaked into Seats[%d]", seat)
		}
	}
	// And they MUST be tracked in Spectators.
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Spectators["human-creator"]; !ok {
		t.Fatalf("expected 'human-creator' in Spectators map, got %v",
			spectatorKeysLocked(r))
	}
}

// TestJoinThenSpectate_MatchesHandleWerewolfJoinFallback mirrors the handleWerewolfJoin
// downgrade path end-to-end: a JoinGame rejection followed by SpectateGame
// must leave the user as a spectator with the live state intact.
//
// This is what runs in production for the §65a full-AI creator scenario
// after the handleWerewolfJoin downgrade lands.
func TestJoinThenSpectate_MatchesHandleWerewolfJoinFallback(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	// Step 1: creator's join envelope arrives AFTER the game has already started.
	_, _, e := m.JoinGame(roomID, "human-creator")
	if e == nil || e.Code != errcode.ErrRoomFull {
		t.Fatalf("expected ErrRoomFull, got %v", e)
	}

	// Step 2: WS handler detects ErrRoomFull and hands off to spectate.
	room, e := m.SpectateGame(roomID, "human-creator")
	if e != nil || room == nil {
		t.Fatalf("expected spectate to accept, got room=%v err=%v", room, e)
	}

	// Final assertions: live state preserved + user as spectator only.
	if room.State == nil || room.State.Phase == PhaseFilling {
		t.Fatalf("expected State.Phase != Filling, got %v", room.State)
	}
	if room.Seats[0] != "bot_0" {
		t.Fatalf("seat 0 was rewritten by spectator attach, got %q", room.Seats[0])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Spectators["human-creator"]; !ok {
		t.Fatalf("creator never landed in Spectators")
	}
}

// spectatorKeysLocked is a small helper for readable test failures. Caller
// MUST hold r.mu.
func spectatorKeysLocked(r *WerewolfRoom) []string {
	if r == nil || r.Spectators == nil {
		return nil
	}
	out := make([]string, 0, len(r.Spectators))
	for k := range r.Spectators {
		out = append(out, k)
	}
	return out
}

// TestHandleDisconnect_WolfStuckRegression 验证 BUG-R7-P0-disconnect-stuck 的修复:
// bot-only 房间中真人中途掉线时,HandleDisconnect 必须把该座位标记为死亡,使
// firstLivingWolfLocked 跳过该座位,acting seat 推进到下一个存活的 bot wolf,
// 避免 night_wolves 阶段 watchdog 永久卡在已断线的座位上。
func TestHandleDisconnect_WolfStuckRegression(t *testing.T) {
	m := stubWWMgr()
	const roomID = "test-disconnect-wolf"
	// 7 人兼容模式: 4 狼 + 预言家 + 女巫 + 猎人(或白痴/守卫)。
	m.SetSeatCount(roomID, 7)
	// 真人坐 seat 0(最先入座),6 个 bot 坐 1..6。
	_, _, _ = m.JoinGame(roomID, "human_0")
	m.SetSeatCount(roomID, 7)
	bots := []string{"bot_1", "bot_2", "bot_3", "bot_4", "bot_5", "bot_6"}
	for _, b := range bots {
		_, _, _ = m.JoinGame(roomID, b)
	}
	r := m.getRoom(roomID)
	if r == nil || r.State == nil || r.State.Phase == PhaseFilling {
		t.Fatalf("game should have started, got phase=%v", r.State)
	}

	r.mu.Lock()
	humanSeat := r.State.SeatOf("human_0")
	if humanSeat == NoSeat {
		r.mu.Unlock()
		t.Fatal("human_0 not seated")
	}
	// 构造最坏情形:真人是狼,且是所有存活狼中座位最小的(即 acting seat),
	// 同时至少还有一个其他 bot wolf 存活(断线后 acting seat 应推进到它)。
	// 把座位 < humanSeat 的狼全部杀死(此处 humanSeat 最小,无需杀)。
	r.State.Roles[humanSeat] = RoleWerewolf
	r.State.Players[humanSeat].Alive = true
	// 确保 seat 1 也是一个存活的 bot wolf(断线后 acting seat 的推进目标)。
	r.State.Roles[1] = RoleWerewolf
	r.State.Players[1].Alive = true
	// 确认 firstLivingWolfLocked 当前返回真人座位(断线前的卡死状态)。
	if firstLivingWolfLocked(r.State) != humanSeat {
		r.mu.Unlock()
		t.Fatalf("precondition: firstLivingWolf should be human seat %d", humanSeat)
	}
	// 把 acting seat 设为真人座位,模拟 night_wolves 卡死状态。
	r.State.TurnActingSeat = humanSeat
	r.State.Phase = PhaseNightWolves
	r.mu.Unlock()

	// 模拟真人断线被踢出。
	if e := m.HandleDisconnect(roomID, "human_0"); e != nil {
		t.Fatalf("HandleDisconnect failed: %v", e)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// 1) 真人座位必须标记为死亡。
	if r.State.AliveSeat(humanSeat) {
		t.Fatalf("human seat %d should be dead after disconnect", humanSeat)
	}
	if r.State.Players[humanSeat].DeathCause != "disconnected" {
		t.Fatalf("death cause = %q, want disconnected", r.State.Players[humanSeat].DeathCause)
	}
	// 2) firstLivingWolfLocked 必须跳过已死亡的真人座位,返回下一个存活狼。
	nextWolf := firstLivingWolfLocked(r.State)
	if nextWolf == humanSeat {
		t.Fatalf("firstLivingWolfLocked still returns disconnected human seat %d", humanSeat)
	}
	if !r.State.AliveSeat(nextWolf) {
		t.Fatalf("firstLivingWolfLocked returns dead/NoSeat %d, want alive wolf", nextWolf)
	}
	// 3) acting seat 不应再指向已死亡的真人座位。
	if r.State.TurnActingSeat == humanSeat {
		t.Fatalf("acting seat still points at disconnected human seat %d", humanSeat)
	}
}

// TestHandleDisconnect_NotInProgress 验证游戏未开局时 HandleDisconnect 是 no-op:
// 不应把座位标记为死亡(否则 filling 阶段掉线玩家会被误判为死亡)。
func TestHandleDisconnect_NotInProgress(t *testing.T) {
	m := stubWWMgr()
	const roomID = "test-disconnect-filling"
	m.SetSeatCount(roomID, 7)
	_, _, _ = m.JoinGame(roomID, "human_0")
	r := m.getRoom(roomID)
	if r == nil || r.State == nil {
		t.Fatal("room/state should exist")
	}
	// filling 阶段: Status != "playing"。
	if e := m.HandleDisconnect(roomID, "human_0"); e != nil {
		t.Fatalf("HandleDisconnect in filling should be no-op, got %v", e)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seat := r.State.SeatOf("human_0")
	if seat != NoSeat && !r.State.AliveSeat(seat) {
		t.Fatalf("filling-phase disconnect should NOT mark seat dead")
	}
}
