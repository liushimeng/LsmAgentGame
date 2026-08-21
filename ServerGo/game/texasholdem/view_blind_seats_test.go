// R9 P1-1 回归测试: 视图层 SB/BB 座位推导与 engine.go startHand 对齐
// (bb 从 sb 继续顺时针推导, 只跳空座不跳弃牌), 以及 min_raise 字段透传。
package texasholdem

import (
	"testing"
)

// newBlindTestState 构造一个占用 occupiedSeats 指定座位、Button=button 的最小 GameState。
func newBlindTestState(button int, occupiedSeats ...int) *GameState {
	gs := &GameState{Button: button, BigBlind: 200}
	for _, s := range occupiedSeats {
		gs.Players[s].UserID = "user_at_seat"
		gs.Players[s].Seat = s
	}
	return gs
}

// TestBlindSeatsSparse 稀疏座位(报告 R9 P1-1 实测场景):
// 座位 0/1/2 占用, Button=2 → SB=0, BB=1。
// 修复前两个独立循环收敛到同一座位(SB=0, BB=0), 前端同一座位渲染 SB+BB 双徽章。
func TestBlindSeatsSparse(t *testing.T) {
	gs := newBlindTestState(2, 0, 1, 2)
	seats := [MaxPlayers]string{"u0", "u1", "u2", "", "", ""}
	cs := BuildClientState("r1", seats, 0, gs)
	if cs.SbSeat != 0 {
		t.Fatalf("SbSeat = %d, want 0", cs.SbSeat)
	}
	if cs.BbSeat != 1 {
		t.Fatalf("BbSeat = %d, want 1 (修复前会错误收敛为 0 与 SB 同座)", cs.BbSeat)
	}
}

// TestBlindSeatsHeadsUp 单挑规则与 engine.go startHand 单挑分支一致:
// 庄家 = SB, 对手 = BB。
func TestBlindSeatsHeadsUp(t *testing.T) {
	seats := [MaxPlayers]string{"u0", "u1", "", "", "", ""}

	// Button=0 → SB=0(庄家), BB=1(对手)
	gs := newBlindTestState(0, 0, 1)
	cs := BuildClientState("r1", seats, 0, gs)
	if cs.SbSeat != 0 || cs.BbSeat != 1 {
		t.Fatalf("Button=0: SbSeat=%d BbSeat=%d, want 0/1", cs.SbSeat, cs.BbSeat)
	}

	// Button=1 → SB=1(庄家), BB=0(对手)
	gs = newBlindTestState(1, 0, 1)
	cs = BuildClientState("r1", seats, 0, gs)
	if cs.SbSeat != 1 || cs.BbSeat != 0 {
		t.Fatalf("Button=1: SbSeat=%d BbSeat=%d, want 1/0", cs.SbSeat, cs.BbSeat)
	}
}

// TestBlindSeatsFoldedNotSkipped 翻后弃牌不错位:
// 推导只跳空座(UserID == "")不跳弃牌, 与发盲注时刻的座位占用一致。
// (不能用 nextActiveSeat — 它会跳过 Folded 玩家导致翻后徽章错位。)
func TestBlindSeatsFoldedNotSkipped(t *testing.T) {
	gs := newBlindTestState(2, 0, 1, 2)
	gs.Players[0].Folded = true // 翻后 SB 位玩家弃牌
	seats := [MaxPlayers]string{"u0", "u1", "u2", "", "", ""}
	cs := BuildClientState("r1", seats, 0, gs)
	if cs.SbSeat != 0 {
		t.Fatalf("SbSeat = %d, want 0 (弃牌不应影响徽章)", cs.SbSeat)
	}
	if cs.BbSeat != 1 {
		t.Fatalf("BbSeat = %d, want 1 (弃牌不应影响徽章)", cs.BbSeat)
	}
}

// TestMinRaisePassthrough 验证 GameState.MinRaise 透传到 ClientGameState
// (前端 raise slider 下界校验, 2026-08-21 R9 建议4)。
func TestMinRaisePassthrough(t *testing.T) {
	gs := newBlindTestState(2, 0, 1, 2)
	gs.MinRaise = 400
	seats := [MaxPlayers]string{"u0", "u1", "u2", "", "", ""}
	cs := BuildClientState("r1", seats, 0, gs)
	if cs.MinRaise != 400 {
		t.Fatalf("MinRaise = %d, want 400", cs.MinRaise)
	}
}

// TestBlindSeatsWaitingDefault waiting 阶段(gs=nil 或 Button=-1)
// SbSeat/BbSeat 必须为 -1(未发盲注), 不能是 Go 零值 0 导致 seat 0 误显示 SB 徽章。
func TestBlindSeatsWaitingDefault(t *testing.T) {
	seats := [MaxPlayers]string{"u0", "u1", "", "", "", ""}

	// gs == nil
	cs := BuildClientState("r1", seats, 0, nil)
	if cs.SbSeat != -1 || cs.BbSeat != -1 {
		t.Fatalf("gs=nil: SbSeat=%d BbSeat=%d, want -1/-1", cs.SbSeat, cs.BbSeat)
	}

	// Button = -1(未发牌)
	gs := newBlindTestState(-1, 0, 1)
	cs = BuildClientState("r1", seats, 0, gs)
	if cs.SbSeat != -1 || cs.BbSeat != -1 {
		t.Fatalf("Button=-1: SbSeat=%d BbSeat=%d, want -1/-1", cs.SbSeat, cs.BbSeat)
	}
}
