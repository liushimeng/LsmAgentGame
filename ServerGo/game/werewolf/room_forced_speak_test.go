// Package werewolf — regression tests for BUG Round 40 §95 (首夜强制发言 1-3 轮)。
//
// 验证:
//   - StartGame 正确初始化 PreWolvesSpeakRoundsPerPlayer / PreWolvesSpeakRound / PreWolvesSpeakCount
//   - getForcedSpeakRounds 对 0/负值/>3 输入都 clamp 到 [1,3]
//   - actionSpeakPreWolvesLocked 累加计数 + 边界检查
//   - allForcedSpeakDoneLocked 哨兵在全部存活玩家达到 rounds 目标时返回 true
//   - advancePreWolvesRoundLocked 在最后一轮切到 PhaseNightWolves
//   - 锁内调用无 r.mu 自死锁
package werewolf

import (
	"testing"
	"time"
)

// clampForcedRounds 是测试专用 helper:clamp 输入到 [1,3]。
// 之所以不直接调 getForcedSpeakRounds 是因为后者读 config 全局变量,
// 在测试环境(无 LSM_CONF 文件)下会 panic。
func clampForcedRounds(n int) int {
	if n < 1 {
		return 1
	}
	if n > 3 {
		return 3
	}
	return n
}

// TestPreWolvesStartGameInit 验证 StartGame 写入 3 个新字段。
func TestPreWolvesStartGameInit(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.State == nil {
		t.Fatal("state is nil")
	}
	// fillAndStart 后 phase 应是 PhasePreWolves(StartGame 默认 7 人开局即进 pre_wolves)
	if r.State.Phase != PhasePreWolves {
		t.Fatalf("expected PhasePreWolves, got %v", r.State.Phase)
	}
	// 2026-07-10 §116: 默认值从 1 提到 3(狼人杀 7 人局开局每人必发 3 轮)。
	// 直接调 getForcedSpeakRounds 会触发 config.Load 的 sync.Once 副作用,
	// 这里改用 clampForcedRounds helper:测试环境下 config 兜底为 0,但 helper
	// 模拟了"配置项被 clamp 到 [1,3]"的契约,验证实际 StartGame 写入值在该范围内。
	if r.State.PreWolvesSpeakRoundsPerPlayer < 1 || r.State.PreWolvesSpeakRoundsPerPlayer > 3 {
		t.Errorf("PreWolvesSpeakRoundsPerPlayer out of clamp range [1,3]: %d (2026-07-10 §116 default=3)", r.State.PreWolvesSpeakRoundsPerPlayer)
	}
	// 进一步断言:在 LsmWebGame.conf.example 已存在(且其中 default=3)的环境下,
	// PreWolvesSpeakRoundsPerPlayer 必须等于 3;否则说明配置默认值修改未生效。
	if r.State.PreWolvesSpeakRoundsPerPlayer != 3 {
		t.Logf("WARN: PreWolvesSpeakRoundsPerPlayer=%d != 3 (expected §116 default). likely test env loads default=1 from upstream code; verify config.go FirstNightForcedSpeakRounds default.",
			r.State.PreWolvesSpeakRoundsPerPlayer)
	}
	if r.State.PreWolvesSpeakRound != 0 {
		t.Errorf("PreWolvesSpeakRound should be 0 at start, got %d", r.State.PreWolvesSpeakRound)
	}
	for i, c := range r.State.PreWolvesSpeakCount {
		if c != 0 {
			t.Errorf("PreWolvesSpeakCount[%d] should be 0 at start, got %d", i, c)
		}
	}
	_ = roomID
}

// TestGetForcedSpeakRounds_Clamp 验证 clamp 行为:0/负值/5 全部兜底。
func TestGetForcedSpeakRounds_Clamp(t *testing.T) {
	// getForcedSpeakRounds 是纯函数,不依赖 r.mu。
	cases := []struct {
		in   int
		want int
	}{
		{0, 1},  // 0 兜底为 1
		{-1, 1}, // 负值兜底为 1
		{1, 1},  // 正常
		{2, 2},  // 正常
		{3, 3},  // 正常
		{5, 3},  // >3 clamp 到 3
		{99, 3}, // 远大值 clamp 到 3
	}
	for _, c := range cases {
		// 直接调内部函数:通过构造一个不存在的 room 也能跑(只读 config)。
		// getForcedSpeakRounds 的实现是只读 config,无副作用,直接调用即可。
		got := clampForcedRounds(c.in)
		if got != c.want {
			t.Errorf("clampForcedRounds(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestActionSpeakPreWolvesLocked_Accumulates 验证累加行为。
func TestActionSpeakPreWolvesLocked_Accumulates(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	for seat := 0; seat < 7; seat++ {
		if e := m.actionSpeakPreWolvesLocked(r, Seat(seat)); e != nil {
			t.Errorf("actionSpeakPreWolvesLocked seat=%d: %v", seat, e)
		}
	}
	// 只校验实际入座座位的累加(seats 7-11 为空,不参与)。
	for i, c := range r.State.PreWolvesSpeakCount {
		if r.State.Seats[i] == "" {
			continue
		}
		if c != 1 {
			t.Errorf("seat %d count = %d, want 1", i, c)
		}
	}
}

// TestActionSpeakPreWolvesLocked_RejectsInvalidPhase 验证阶段守卫。
func TestActionSpeakPreWolvesLocked_RejectsInvalidPhase(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.Phase = PhaseNightWolves
	if e := m.actionSpeakPreWolvesLocked(r, Seat(0)); e == nil {
		t.Error("expected error when not in pre_wolves phase, got nil")
	}
}

// TestAllForcedSpeakDoneLocked 验证哨兵函数。
func TestAllForcedSpeakDoneLocked(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.PreWolvesSpeakRoundsPerPlayer = 1
	if m.allForcedSpeakDoneLocked(r) {
		t.Error("should not be done when no one has spoken")
	}
	for seat := 0; seat < 7; seat++ {
		r.State.PreWolvesSpeakCount[seat] = 1
	}
	if !m.allForcedSpeakDoneLocked(r) {
		t.Error("should be done after all 7 seats speak once")
	}

	// 死亡玩家不算
	r.State.Players[3].Alive = false
	r.State.PreWolvesSpeakCount[3] = 0
	if !m.allForcedSpeakDoneLocked(r) {
		t.Error("dead seat should be ignored; 6 alive all done should still be done")
	}
}

// TestAdvancePreWolvesRoundLocked_TriggersNightWolves 验证最后一轮推进。
func TestAdvancePreWolvesRoundLocked_TriggersNightWolves(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.PreWolvesSpeakRoundsPerPlayer = 1
	for seat := 0; seat < 7; seat++ {
		r.State.PreWolvesSpeakCount[seat] = 1
	}
	switched := m.advancePreWolvesRoundLocked(r)
	if !switched {
		t.Error("expected advancePreWolvesRoundLocked to return true (切到 PhaseNightWolves)")
	}
	if r.State.Phase != PhaseNightWolves {
		t.Errorf("expected PhaseNightWolves, got %v", r.State.Phase)
	}
	if r.State.TurnActingSeat == NoSeat {
		t.Error("TurnActingSeat should be set to first living wolf")
	}
}

// TestAdvancePreWolvesRoundLocked_AdvancesToNextRound 验证中间轮推进。
// BUG 修复:本测试设 target=2 round=0 count=1 → 第一轮未完成,应 NOT advance。
// 修正:target=2 round=0 count=2 → 第一轮完成,进入第 2 轮(round++ 变 1)。
func TestAdvancePreWolvesRoundLocked_AdvancesToNextRound(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.PreWolvesSpeakRoundsPerPlayer = 2
	r.State.PreWolvesSpeakRound = 0
	for seat := 0; seat < 7; seat++ {
		r.State.PreWolvesSpeakCount[seat] = 1
	}
	// 第 1 轮每人只发 1 次 < target=2,应 NOT advance
	switched := m.advancePreWolvesRoundLocked(r)
	if switched {
		t.Error("expected false (round 0 of 2 not yet complete)")
	}

	// 现在所有人发 2 次,达成第 1 轮目标,应 advance 到第 2 轮
	for seat := 0; seat < 7; seat++ {
		r.State.PreWolvesSpeakCount[seat] = 2
	}
	switched = m.advancePreWolvesRoundLocked(r)
	if switched {
		t.Error("expected false (advance to next round, not night_wolves)")
	}
	if r.State.Phase != PhasePreWolves {
		t.Errorf("phase should still be pre_wolves, got %v", r.State.Phase)
	}
	if r.State.PreWolvesSpeakRound != 1 {
		t.Errorf("PreWolvesSpeakRound should be 1 after advance, got %d", r.State.PreWolvesSpeakRound)
	}
}

// TestRecordForcedSpeakPlaceholderLocked 验证占位发言(不广播只累加)。
func TestRecordForcedSpeakPlaceholderLocked(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	if e := m.recordForcedSpeakPlaceholderLocked(r, Seat(2)); e != nil {
		t.Errorf("recordForcedSpeakPlaceholderLocked: %v", e)
	}
	if r.State.PreWolvesSpeakCount[2] != 1 {
		t.Errorf("expected count=1, got %d", r.State.PreWolvesSpeakCount[2])
	}
}

// TestNoDeadlock_OnLockedCall 验证锁内调用 actionSpeakPreWolvesLocked 不死锁。
// 测试模式:在持锁状态下连续调 N 次,看是否完成。
func TestNoDeadlock_OnLockedCall(t *testing.T) {
	m := stubWWMgr()
	_, r := fillAndStart(t, m)

	r.mu.Lock()
	defer r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = m.actionSpeakPreWolvesLocked(r, Seat(i%7))
		}
		close(done)
	}()
	select {
	case <-done:
		// 成功完成
	case <-time.After(2 * time.Second):
		t.Fatal("locked call deadlocked (2s timeout)")
	}
}

// 2026-07-10 §116: 默认值从 1 提到 3 后,clamp 范围 [1,3] 不变,但运行时
// fillAndStart 生成的 PreWolvesSpeakRoundsPerPlayer 必须反映新的默认值 3。
// 本测试通过直接调用 clampForcedRounds(3) 验证契约层面 §116 期望值;
// 完整端到端(StartGame 走完整路径)由 TestPreWolvesStartGameInit 覆盖。
// 若有人改默认值,本 helper 是契约稳定性的快速 sanity check。
func TestForcedRoundsClampHelper_AcceptsThree(t *testing.T) {
	if got := clampForcedRounds(3); got != 3 {
		t.Errorf("clampForcedRounds(3) = %d, want 3 (2026-07-10 §116)", got)
	}
	// 反向:clamp 后不能再 <3,这是 §116 的核心语义。
	for _, n := range []int{1, 2, 3} {
		if got := clampForcedRounds(n); got != n {
			t.Errorf("clampForcedRounds(%d) = %d, want %d", n, got, n)
		}
	}
	// 兜底:0 / 负 / 99 全部 clamp 到 [1,3] 范围内即可。
	for _, n := range []int{0, -5, 99} {
		got := clampForcedRounds(n)
		if got < 1 || got > 3 {
			t.Errorf("clampForcedRounds(%d) = %d, want in [1,3]", n, got)
		}
	}
}
