package texasholdem

import "testing"

// TestAllIn_TurnSkipsAllInOnNextStreet 回归 §BUG-TEXAS-ALLIN-TURN (R13 P1-2):
// 全押玩家在下一条街(flop/turn/river)不应被轮转到。庄家与其他玩家均全押、
// 仅剩庄家自己可行动时,advanceToNextStreet 必须把 turn 交回庄家,而不是停在
// 庄家之后第一个「非空但已全押」的座位上。
// 布局对齐 R13 报告(座位 0=庄家,2=SB,4=BB,1/3/5 空位):庄家之后第一个非空
// 座位是全押的 SB,历史 nextActiveSeat 不跳过全押 → turn 落在全押座位,卡死。
func TestAllIn_TurnSkipsAllInOnNextStreet(t *testing.T) {
	gs := NewGame(42, 200)
	gs.AddPlayerAt("p0", 0, 10000) // 庄家(唯一保持可行动)
	gs.AddPlayerAt("p2", 2, 1000)  // SB,短码(全押)
	gs.AddPlayerAt("p4", 4, 1000)  // BB,短码(全押)
	gs.Button = -1                 // StartHand 旋转后 button=0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// 翻牌前:庄家(3 人桌 UTG)先动
	if gs.Turn != 0 {
		t.Fatalf("preflop first turn = %d, want 0", gs.Turn)
	}
	if _, e := gs.ApplyAction(0, Action{Type: ActCall}); e != nil {
		t.Fatalf("button call failed: %v", e)
	}
	// SB 全押 → BB 全押 → 庄家跟注补齐 → 进入 flop
	if gs.Turn != 2 {
		t.Fatalf("after button call: turn = %d, want 2 (SB)", gs.Turn)
	}
	if _, e := gs.ApplyAction(2, Action{Type: ActAllIn}); e != nil {
		t.Fatalf("SB all-in failed: %v", e)
	}
	if gs.Turn != 4 {
		t.Fatalf("after SB all-in: turn = %d, want 4 (BB)", gs.Turn)
	}
	if _, e := gs.ApplyAction(4, Action{Type: ActAllIn}); e != nil {
		t.Fatalf("BB all-in failed: %v", e)
	}
	if gs.Turn != 0 {
		t.Fatalf("after BB all-in: turn = %d, want 0 (button)", gs.Turn)
	}
	if _, e := gs.ApplyAction(0, Action{Type: ActCall}); e != nil {
		t.Fatalf("button call-to-match failed: %v", e)
	}

	// 此时 flop,turn 必须回到庄家(0),而不是停在已全押的 SB(2)
	if gs.Street != PhaseFlop {
		t.Fatalf("street = %v, want flop", gs.Street)
	}
	if gs.Turn != 0 {
		t.Fatalf("BUG-TEXAS-ALLIN-TURN: turn on next street = %d, want 0 (skip all-in SB 2 / BB 4, back to button)", gs.Turn)
	}
}

// TestAllIn_TurnSkipsAllInMidRound 回归 §BUG-TEXAS-ALLIN-TURN (ApplyAction 路径):
// 加注重新开启行动时,夹在中间的全押玩家必须被回合轮转跳过,直接落到
// 下一个仍需要行动的玩家,而非停在无法行动的全押座位上。
func TestAllIn_TurnSkipsAllInMidRound(t *testing.T) {
	gs := NewGame(42, 200)
	gs.AddPlayerAt("p0", 0, 20000) // 庄家
	gs.AddPlayerAt("p1", 1, 1000)  // SB,短码(将全押)
	gs.AddPlayerAt("p2", 2, 20000) // BB
	gs.AddPlayerAt("p3", 3, 20000) // UTG
	gs.Button = -1                 // StartHand 旋转后 button=0
	if e := gs.StartHand(); e != nil {
		t.Fatal(e)
	}

	// UTG 弃牌 → 庄家跟注 → SB 全押 → BB 跟注 → 庄家再加注
	if _, e := gs.ApplyAction(3, Action{Type: ActFold}); e != nil {
		t.Fatalf("UTG fold failed: %v", e)
	}
	if _, e := gs.ApplyAction(0, Action{Type: ActCall}); e != nil {
		t.Fatalf("button call failed: %v", e)
	}
	if gs.Turn != 1 {
		t.Fatalf("after button call: turn = %d, want 1 (SB)", gs.Turn)
	}
	if _, e := gs.ApplyAction(1, Action{Type: ActAllIn}); e != nil {
		t.Fatalf("SB all-in failed: %v", e)
	}
	if _, e := gs.ApplyAction(2, Action{Type: ActCall}); e != nil {
		t.Fatalf("BB call failed: %v", e)
	}
	if gs.Turn != 0 {
		t.Fatalf("after BB call: turn = %d, want 0 (button)", gs.Turn)
	}
	// 庄家再加注到 2000(合法:>= currentBet 1000 + minRaise 800)
	if _, e := gs.ApplyAction(0, Action{Type: ActRaise, Amount: 2000}); e != nil {
		t.Fatalf("button raise failed: %v", e)
	}

	// 庄家加注后,下一个应行动者是 BB(2),而不是全押的 SB(1)
	if gs.Turn != 2 {
		t.Fatalf("BUG-TEXAS-ALLIN-TURN: after button raise, turn = %d, want 2 (skip all-in SB 1)", gs.Turn)
	}
}
