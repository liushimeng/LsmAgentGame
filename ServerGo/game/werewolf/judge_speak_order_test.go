// Package werewolf — judge_speak_order_test.go: §20260810-02 E2 验证。
//
// wwjudge.GameSnapshot.SpeakOrder 此前声明后零生产写入点
// （K3-Surpport-01 §1 F2），法官在 judge_speak_start 唤醒时看不到发言顺序。
// 本测试锁定两条不变式：
//  1. PhaseSpeak 时 SpeakOrder 被正确填充，且为 0-indexed 座位号
//  2. 非 PhaseSpeak（含平票 PK 复用 SpeakOrder 的场景）时留空，避免法官误读
//
// §92a：buildJudgeSnapshotLocked 是锁内变体，测试必须持锁调用。
package werewolf

import (
	"testing"
	"time"
)

// newSpeakOrderTestRoom 构造 4 座位房间，并设置一个已知的发言顺序。
func newSpeakOrderTestRoom(phase Phase) *WerewolfRoom {
	gs := NewGame(11)
	gs.SeatCount = 4
	for i := 0; i < 4; i++ {
		gs.Seats[i] = "user-" + string(rune('A'+i))
		gs.Players[i].Alive = true
		gs.Players[i].IsBot = true
		gs.Roles[i] = RoleVillager
	}
	gs.Phase = phase
	gs.DayNumber = 2
	gs.SpeakOrder = []Seat{2, 3, 0, 1}
	gs.SpeakTurnSeat = 2

	return &WerewolfRoom{
		RoomID: "speak-order-test-room",
		State:  gs,
	}
}

// lockedSnapshot 持锁调用 buildJudgeSnapshotLocked，并用超时守卫防止自死锁挂起（§92a）。
func lockedSnapshot(t *testing.T, r *WerewolfRoom, kind string) (snap struct{ Order []int }) {
	t.Helper()
	done := make(chan []int, 1)
	go func() {
		r.mu.Lock()
		s := r.buildJudgeSnapshotLocked(kind)
		r.mu.Unlock()
		done <- s.SpeakOrder
	}()
	select {
	case order := <-done:
		return struct{ Order []int }{Order: order}
	case <-time.After(3 * time.Second):
		t.Fatal("buildJudgeSnapshotLocked 超时 —— 可能存在 §92a 自死锁")
		return
	}
}

// TestJudgeSnapshot_E2_SpeakOrderFilledInPhaseSpeak PhaseSpeak 时应填充发言顺序。
func TestJudgeSnapshot_E2_SpeakOrderFilledInPhaseSpeak(t *testing.T) {
	r := newSpeakOrderTestRoom(PhaseSpeak)
	got := lockedSnapshot(t, r, "judge_speak_start").Order

	want := []int{2, 3, 0, 1}
	if len(got) != len(want) {
		t.Fatalf("SpeakOrder 长度 = %d, want %d (got=%v) —— 字段仍为零写入点？", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SpeakOrder[%d] = %d, want %d (完整 got=%v)", i, got[i], want[i], got)
		}
	}
}

// TestJudgeSnapshot_E2_SpeakOrderEmptyOutsidePhaseSpeak 非发言阶段必须留空。
// SpeakOrder 在平票 PK 与警长竞选时被复用为「参与者列表」，下发会让法官误读为发言顺序。
func TestJudgeSnapshot_E2_SpeakOrderEmptyOutsidePhaseSpeak(t *testing.T) {
	for _, p := range []Phase{PhaseVote, PhaseDawn, PhaseSheriff, PhaseNightWolves} {
		r := newSpeakOrderTestRoom(p)
		if got := lockedSnapshot(t, r, "judge_other").Order; len(got) != 0 {
			t.Errorf("phase=%v 时 SpeakOrder = %v, want empty", p, got)
		}
	}
}
