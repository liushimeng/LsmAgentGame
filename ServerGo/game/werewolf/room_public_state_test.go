// Package werewolf — regression tests for WerewolfManager.GetPublicState
// (Round 23 P1 BUG FIX). RoomService.GetRoomDetail previously could only
// echo t_lsm_game_room.status, which never advanced mid-game in the lobby
// view; Phase / RoundNumber in the REST response were always "?" / 0.
// GetPublicState fills the gap by reading the in-memory engine state
// (Phase / DayNumber / Status / Winner) so REST clients can render the
// stage indicator without subscribing to WS frames.
//
// The contract pinned by these tests:
//
//  1. Unknown room       → ("", 0, false)         (caller must NOT echo to REST)
//  2. Room created, no State yet → ("filling", 0, true)
//  3. Room in playing state → phase == PhaseString, day == DayNumber
//  4. Room in over state → phase == "over", winner populated
package werewolf

import (
	"testing"

	"LsmAgentGame/errcode"
)

// TestGetPublicState_UnknownRoomReturnsFalse ensures the no-room branch
// does NOT echo placeholder values back to the caller. Without this guard,
// RoomService would happily surface phase="filling" for every typo'd roomID
// in the lobby.
func TestGetPublicState_UnknownRoomReturnsFalse(t *testing.T) {
	m := stubWWMgr()
	ps, ok := m.GetPublicState("does-not-exist")
	if ok {
		t.Fatalf("expected ok=false for unknown room, got %+v", ps)
	}
	if ps.Phase != "" || ps.Day != 0 || ps.Status != "" || ps.Winner != "" {
		t.Fatalf("expected zero-value PublicState, got %+v", ps)
	}
}

// TestGetPublicState_FillingPhaseReturnsFilling covers the "lobby visible,
// no one joined yet" path. The REST caller can distinguish "room exists,
// game not started" from "room does not exist" by inspecting ok=true +
// Phase="filling".
func TestGetPublicState_FillingPhaseReturnsFilling(t *testing.T) {
	m := stubWWMgr()
	const roomID = "filling-room"
	// First JoinGame creates the in-memory room. Phase remains PhaseFilling
	// until the 7th seat is occupied.
	if _, _, e := m.JoinGame(roomID, "u1"); e != nil && e.Code != errcode.ErrRoomNotFound {
		// tolerate any non-fatal error; we only need the room to exist.
		t.Logf("JoinGame(%s,u1) returned %v (continuing)", roomID, e)
	}
	ps, ok := m.GetPublicState(roomID)
	if !ok {
		t.Fatalf("expected ok=true after first JoinGame, got ok=false")
	}
	if ps.Phase != PhaseFilling.String() {
		t.Fatalf("expected phase=%q, got %q", PhaseFilling.String(), ps.Phase)
	}
	if ps.Day != 0 {
		t.Fatalf("expected day=0 in PhaseFilling, got %d", ps.Day)
	}
}

// TestGetPublicState_StartedRoomExposesPhaseAndDay pins the core REST
// visibility contract: once the room has been auto-started by filling 7
// seats, GetPublicState must surface the live Phase.String() and DayNumber
// so the lobby detail panel can render "第 N 天 · 白天发言" without WS.
func TestGetPublicState_StartedRoomExposesPhaseAndDay(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)
	if r == nil || r.State == nil {
		t.Fatalf("fillAndStart returned nil State")
	}
	r.mu.Lock()
	r.State.DayNumber = 3
	r.State.Phase = PhaseSpeak
	r.mu.Unlock()

	ps, ok := m.GetPublicState(roomID)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if ps.Phase != "speak" {
		t.Fatalf("expected phase=%q, got %q", "speak", ps.Phase)
	}
	if ps.Day != 3 {
		t.Fatalf("expected day=3, got %d", ps.Day)
	}
	if ps.Status != "playing" {
		t.Fatalf("expected status=playing, got %q", ps.Status)
	}
}

// TestGetPublicState_GameOverPropagatesWinner ensures the room-detail API
// exposes the winner string ("wolf"|"good") once the engine transitions
// to PhaseGameOver, so the lobby list can show "已结束 · 好人胜利" without
// joining the room via WS.
func TestGetPublicState_GameOverPropagatesWinner(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)
	if r == nil || r.State == nil {
		t.Fatalf("fillAndStart returned nil State")
	}
	r.mu.Lock()
	r.State.Phase = PhaseGameOver
	r.State.Status = "over"
	r.State.Winner = "good"
	r.State.DayNumber = 2
	r.mu.Unlock()

	ps, ok := m.GetPublicState(roomID)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if ps.Phase != "over" {
		t.Fatalf("expected phase=%q, got %q", "over", ps.Phase)
	}
	if ps.Status != "over" {
		t.Fatalf("expected status=over, got %q", ps.Status)
	}
	if ps.Winner != "good" {
		t.Fatalf("expected winner=good, got %q", ps.Winner)
	}
	if ps.Day != 2 {
		t.Fatalf("expected day=2, got %d", ps.Day)
	}
}

// TestSpectateGame_RestartRecoveryForceStarts pins the contract that
// BUG-WEREWOLF-SPECTATE-FILLING (Round 24 P0) requires: when the in-memory
// state was lost (e.g. server restart) but the persisted bot seats survived,
// a fresh SpectateGame must force-start the game so the spectator sees a
// live phase (night_wolves / speak / vote / …) rather than a permanent
// filling. Previously this branch only fired when r.State != nil — so a
// post-restart room with r.State == nil would create a brand-new NewGame
// in PhaseFilling and stay stuck there forever, surfacing as
// "👁 观战中（等待 7 位玩家入座…）" on the React side.
//
// Setup mimics the post-restart state: in-memory manager has no room for
// `roomID`; only the hydrator (DB) knows about the 7 bot seats.
func TestSpectateGame_RestartRecoveryForceStarts(t *testing.T) {
	m := stubWWMgr()
	const roomID = "restart-recovery-room"

	// Wire a hydrator that pretends the DB has 7 bot seats for this room.
	// This is the post-restart view: nothing in m.rooms yet, only DB rows.
	bots := make([]AgentSeatInfo, 0, MaxPlayers)
	for i := 0; i < MaxPlayers; i++ {
		bots = append(bots, AgentSeatInfo{
			Seat:     i,
			UserID:   "bot_restart_" + string(rune('a'+i)),
			ModelKey: "TestModel",
		})
	}
	m.hydrator = func(rid string) ([]AgentSeatInfo, error) {
		if rid != roomID {
			return nil, errcode.Code(errcode.ErrRoomNotFound)
		}
		return bots, nil
	}

	// Spectate triggers the recovery path: empty m.rooms entry + hydrator
	// restores seats + State is nil → must force-start, not NewGame-and-stop.
	room, e := m.SpectateGame(roomID, "spectator-1")
	if e != nil {
		t.Fatalf("SpectateGame returned err: %d (%s)", e.Code, e.Message)
	}
	if room == nil {
		t.Fatalf("SpectateGame returned nil room")
	}
	if room.State == nil {
		t.Fatalf("SpectateGame left State==nil (recovery broken)")
	}
	if room.State.Phase == PhaseFilling {
		t.Fatalf("SpectateGame left State in PhaseFilling — spectator stuck "+
			"on waiting-board; expected post-restart force-start, got phase=%q",
			room.State.Phase.String())
	}

	// And the public REST view (consumed by /api/rooms/:id) must reflect the
	// live phase, not "filling".
	ps, ok := m.GetPublicState(roomID)
	if !ok {
		t.Fatalf("GetPublicState returned ok=false after Spectate recovery")
	}
	if ps.Phase == PhaseFilling.String() {
		t.Fatalf("REST /api/rooms/:id still reports phase=filling after recovery; "+
			"lobby list will show 'waiting' forever. got phase=%q status=%q",
			ps.Phase, ps.Status)
	}
	if ps.Status != "playing" {
		t.Fatalf("expected status=playing after force-start, got %q", ps.Status)
	}
}