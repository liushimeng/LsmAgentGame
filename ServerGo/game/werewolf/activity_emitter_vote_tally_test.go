// Package werewolf — activity_emitter_vote_tally_test.go
// §20260809-02 U2 Bot 票型回灌 的单元测试。
//
// 覆盖 fillDayVoteMapLocked 的关键边界:
//   - 7 个存活 bot 全投票,LastDayVoteMap 应有 7 条
//   - NoSeat(弃权)不进入 Map
//   - 死人(Alive=false)的票不进入 Map
//   - 越界 VoteTarget 兜底丢弃
//   - 空状态(NilRoom / NilState)安全
//
// 此测试不依赖 mgr.createRoom,用最小的 GameState 直接调底层函数。

package werewolf

import "testing"

// TestFillDayVoteMapLocked_Basic 7 存活全投票 → 7 条 Map。
func TestFillDayVoteMapLocked_Basic(t *testing.T) {
	gs := &GameState{}
	for i := Seat(0); i < Seat(7); i++ {
		// AliveSeat 要求 Seats[seat] != "" 才视为"存活",必须填 UserID。
		gs.Seats[i] = "user-" + string(rune('A'+int(i)))
		gs.Players[i] = Player{Seat: i, Alive: true, Voted: true, VoteTarget: (i + 1) % 7}
	}
	// 5..12 不投票 / 不存活,不应出现在 Map 中。
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}
	m.fillDayVoteMapLocked(r)
	if got := len(gs.LastDayVoteMap); got != 7 {
		t.Fatalf("LastDayVoteMap len = %d, want 7", got)
	}
	// 校验对位关系
	if v, ok := gs.LastDayVoteMap[0]; !ok || v != 1 {
		t.Errorf("Map[0] = (%d, %v), want (1, true)", v, ok)
	}
	if v, ok := gs.LastDayVoteMap[6]; !ok || v != 0 {
		t.Errorf("Map[6] = (%d, %v), want (0, true)", v, ok)
	}
}

// TestFillDayVoteMapLocked_SkipAbstain NoSeat(弃权) 不进入 Map。
func TestFillDayVoteMapLocked_SkipAbstain(t *testing.T) {
	gs := &GameState{}
	for i := Seat(0); i < Seat(5); i++ {
		gs.Seats[i] = "user-" + string(rune('A'+int(i)))
	}
	for i := Seat(0); i < Seat(3); i++ {
		gs.Players[i] = Player{Seat: i, Alive: true, Voted: true, VoteTarget: i} // i → i(自投,可能存在但允许)
	}
	// Seat 3 弃权
	gs.Players[3] = Player{Seat: 3, Alive: true, Voted: true, VoteTarget: NoSeat}
	// Seat 4 存活但本轮没投票
	gs.Players[4] = Player{Seat: 4, Alive: true, Voted: false, VoteTarget: NoSeat}
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}
	m.fillDayVoteMapLocked(r)
	if got := len(gs.LastDayVoteMap); got != 3 {
		t.Fatalf("abstain should be skipped, got len = %d, want 3", got)
	}
	if _, has3 := gs.LastDayVoteMap[3]; has3 {
		t.Errorf("seat 3 abstained but appears in Map")
	}
	if _, has4 := gs.LastDayVoteMap[4]; has4 {
		t.Errorf("seat 4 did not vote but appears in Map")
	}
}

// TestFillDayVoteMapLocked_SkipDead 死人(Alive=false)的票不进入 Map。
func TestFillDayVoteMapLocked_SkipDead(t *testing.T) {
	gs := &GameState{}
	gs.Seats[0] = "user-A"
	gs.Seats[1] = "user-B" // 也填 Seats(死人仍然占座位),通过 Alive=false 过滤。
	gs.Players[0] = Player{Seat: 0, Alive: true, Voted: true, VoteTarget: 1}
	// Seat 1 已死,但仍然填了投票数据(典型 bug 场景:服务器只清 Alive 不清 Voted)。
	gs.Players[1] = Player{Seat: 1, Alive: false, Voted: true, VoteTarget: 2}
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}
	m.fillDayVoteMapLocked(r)
	if _, ok := gs.LastDayVoteMap[1]; ok {
		t.Errorf("dead seat 1 must not appear in LastDayVoteMap")
	}
	if _, ok := gs.LastDayVoteMap[0]; !ok {
		t.Errorf("alive seat 0 must appear in LastDayVoteMap")
	}
}

// TestFillDayVoteMapLocked_OutOfRange 越界 VoteTarget 兜底丢弃。
func TestFillDayVoteMapLocked_OutOfRange(t *testing.T) {
	gs := &GameState{}
	for i := Seat(0); i < Seat(3); i++ {
		gs.Seats[i] = "user-" + string(rune('A'+int(i)))
	}
	gs.Players[0] = Player{Seat: 0, Alive: true, Voted: true, VoteTarget: 99} // 越界
	gs.Players[1] = Player{Seat: 1, Alive: true, Voted: true, VoteTarget: -7} // 负数
	gs.Players[2] = Player{Seat: 2, Alive: true, Voted: true, VoteTarget: 3}  // 合法
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}
	m.fillDayVoteMapLocked(r)
	if got := len(gs.LastDayVoteMap); got != 1 {
		t.Fatalf("only seat 2 should survive; got len = %d", got)
	}
	if v, ok := gs.LastDayVoteMap[2]; !ok || v != 3 {
		t.Errorf("Map[2] = (%d, %v), want (3, true)", v, ok)
	}
}

// TestFillDayVoteMapLocked_NilSafe 验证 nil-room / nil-state 不 panic。
func TestFillDayVoteMapLocked_NilSafe(t *testing.T) {
	m := &WerewolfManager{}
	// nil room
	m.fillDayVoteMapLocked(nil)
	// nil state
	r := &WerewolfRoom{}
	m.fillDayVoteMapLocked(r)
}
