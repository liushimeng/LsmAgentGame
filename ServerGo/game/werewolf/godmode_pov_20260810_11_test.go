// Package werewolf - godmode_pov_20260810_11_test.go: §20260810-11 V1 GodMode PerSeatPOV 测试
//
// 覆盖:
//  1. populateGodModeLocked 填充 PerSeatPOV(全 13 座位)
//  2. RoleRevealed 在 Status="over" 后为 true,之前为 false
//  3. IdiotRevealed 触发立即公开
//  4. 无人座位不出现在 POV 中

package werewolf

import "testing"

// TestPopulateGodMode_PerSeatPOV_Basic 验证 PerSeatPOV 字段对齐。
func TestPopulateGodMode_PerSeatPOV_Basic(t *testing.T) {
	gs := &GameState{Status: "playing"}
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i] = Player{Seat: Seat(i), Alive: i < 10} // 10 活 3 死
	}
	gs.Roles[0] = RoleWerewolf
	gs.Roles[1] = RoleSeer
	gs.Roles[2] = RoleWitch
	// 9 座位不分配(模拟空座)
	r := &WerewolfRoom{
		State: gs,
		Seats: [MaxPlayers]string{},
	}
	for i := 0; i < 10; i++ {
		r.Seats[i] = "user-" + string(rune('A'+i))
	}

	snap := r.populateGodModeLocked()
	if snap == nil {
		t.Fatal("snap should not be nil for non-nil state")
	}
	if len(snap.PerSeatPOV) != 10 {
		t.Errorf("PerSeatPOV should have 10 entries (only occupied seats), got %d", len(snap.PerSeatPOV))
	}
	pov0, ok := snap.PerSeatPOV[0]
	if !ok {
		t.Fatal("seat 0 should be in PerSeatPOV")
	}
	if pov0.Role != RoleWerewolf.String() {
		t.Errorf("seat 0 role = %q, want %q", pov0.Role, RoleWerewolf.String())
	}
	if pov0.Faction != FactionWolf.String() {
		t.Errorf("seat 0 faction = %q, want %q", pov0.Faction, FactionWolf.String())
	}
	if pov0.RoleRevealed {
		t.Errorf("seat 0 RoleRevealed should be false during playing")
	}
	if len(pov0.NightActions) != 0 {
		t.Errorf("NightActions should default to empty slice, got %d items", len(pov0.NightActions))
	}
	if len(pov0.PublicCommitments) != 0 {
		t.Errorf("PublicCommitments should default to empty slice, got %d items", len(pov0.PublicCommitments))
	}
}

// TestPopulateGodMode_RoleRevealed_OnGameOver 终局后全部公开。
func TestPopulateGodMode_RoleRevealed_OnGameOver(t *testing.T) {
	gs := &GameState{Status: "over"}
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i] = Player{Seat: Seat(i), Alive: true}
	}
	gs.Roles[0] = RoleWerewolf
	gs.Roles[1] = RoleVillager
	r := &WerewolfRoom{State: gs, Seats: [MaxPlayers]string{}}
	for i := 0; i < 7; i++ {
		r.Seats[i] = "user-" + string(rune('A'+i))
	}
	snap := r.populateGodModeLocked()
	if snap == nil {
		t.Fatal("snap nil")
	}
	for i := 0; i < 7; i++ {
		pov := snap.PerSeatPOV[i]
		if !pov.RoleRevealed {
			t.Errorf("seat %d should be RoleRevealed after game over", i)
		}
	}
}

// TestPopulateGodMode_RoleRevealed_IdiotFlips 验证白痴翻牌立即公开。
func TestPopulateGodMode_RoleRevealed_IdiotFlips(t *testing.T) {
	gs := &GameState{Status: "playing"}
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i] = Player{Seat: Seat(i), Alive: true}
	}
	gs.Roles[0] = RoleIdiot
	gs.Players[0].IdiotRevealed = true
	r := &WerewolfRoom{State: gs, Seats: [MaxPlayers]string{}}
	r.Seats[0] = "user-A"
	snap := r.populateGodModeLocked()
	pov := snap.PerSeatPOV[0]
	if !pov.RoleRevealed {
		t.Errorf("seat 0 (Idiot revealed) should have RoleRevealed=true")
	}
}

// TestPopulateGodMode_RoleRevealed_HunterFires 猎人开枪后公开。
func TestPopulateGodMode_RoleRevealed_HunterFires(t *testing.T) {
	gs := &GameState{Status: "playing"}
	for i := 0; i < MaxPlayers; i++ {
		gs.Players[i] = Player{Seat: Seat(i), Alive: i != 0} // 猎人已死
	}
	gs.Roles[0] = RoleHunter
	gs.Players[0].HunterFired = true
	r := &WerewolfRoom{State: gs, Seats: [MaxPlayers]string{}}
	r.Seats[0] = "user-A"
	snap := r.populateGodModeLocked()
	pov := snap.PerSeatPOV[0]
	if !pov.RoleRevealed {
		t.Errorf("seat 0 (Hunter fired) should have RoleRevealed=true")
	}
}

// TestPopulateGodMode_NilState 验证 nil State 时返回 nil。
func TestPopulateGodMode_NilState(t *testing.T) {
	r := &WerewolfRoom{}
	if snap := r.populateGodModeLocked(); snap != nil {
		t.Errorf("expected nil for nil state, got %+v", snap)
	}
}
