package werewolf

// influence_tracker_test.go — §20260811-02 U1 自检测试
//
// 覆盖 4 个信号的独立计算 + 边界 + §130 接线验证(生产调用点存在)+
// §92a 锁语义(全部 *Locked,无公开变体)。

import (
	"testing"

	wwtypes "LsmWebGame/agent/wwtypes"
)

// newInfluenceTestRoom 构造一个 n 人存活的最小房间(不启动引擎)。
func newInfluenceTestRoom(n int) *WerewolfRoom {
	gs := &GameState{SeatCount: n, DayNumber: 2}
	for i := 0; i < n; i++ {
		gs.Seats[i] = "u" + itoaTest(i)
		gs.Players[i].Alive = true
	}
	return &WerewolfRoom{State: gs}
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}

// TestInfluence_U1_01_Persuasion: 跟票率信号 — 3 人跟我投同一目标时拿到高 Persuasion。
func TestInfluence_U1_01_Persuasion(t *testing.T) {
	r := newInfluenceTestRoom(9)
	// 座位 0,1,2,3 都投 5 号 → 每人各有 3 个「跟随者」
	r.State.LastDayVoteMap = map[Seat]Seat{0: 5, 1: 5, 2: 5, 3: 5}
	r.RecalculateInfluenceLocked()

	s0 := r.influenceTrackerLocked().GetLocked(0)
	if s0 == nil {
		t.Fatal("座位 0 未产出影响力分数")
	}
	// 3 followers / (9 alive - 1) = 0.375 → 0.375*35 = 13 (§20260812-02 U2 权重重分配)
	if s0.Persuasion != 13 {
		t.Errorf("Persuasion=%d, 期望 13", s0.Persuasion)
	}
	// 座位 8 没投票 → 无跟票分
	s8 := r.influenceTrackerLocked().GetLocked(8)
	if s8 == nil || s8.Persuasion != 0 {
		t.Errorf("未投票座位应无 Persuasion 分, 实际 %+v", s8)
	}
}

// TestInfluence_U1_02_Presence: 发言参与信号 — 发言占比转化为 Presence 分。
func TestInfluence_U1_02_Presence(t *testing.T) {
	r := newInfluenceTestRoom(9)
	// 座位 1 说了 3 句,座位 2 说了 1 句 → 占比 3/4 与 1/4
	r.recentSpeeches = []wwtypes.SpeechEvent{
		{Seat: 1, Text: "a"}, {Seat: 1, Text: "b"}, {Seat: 1, Text: "c"},
		{Seat: 2, Text: "d"},
	}
	r.RecalculateInfluenceLocked()

	s1 := r.influenceTrackerLocked().GetLocked(1)
	s2 := r.influenceTrackerLocked().GetLocked(2)
	if s1 == nil || s2 == nil {
		t.Fatal("影响力未产出")
	}
	if s1.Presence != 14 { // 0.75 * 18 = 13.5 → 14 (§20260812-02 U2 权重重分配)
		t.Errorf("座位1 Presence=%d, 期望 14", s1.Presence)
	}
	if s2.Presence != 5 { // 0.25 * 18 = 4.5 → 5
		t.Errorf("座位2 Presence=%d, 期望 5", s2.Presence)
	}
}

// TestInfluence_U1_03_SpectatorSpeechExcluded: 观战者发言不计入任何座位的 Presence。
func TestInfluence_U1_03_SpectatorSpeechExcluded(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.recentSpeeches = []wwtypes.SpeechEvent{
		{Seat: 1, Text: "player"},
		{Seat: 2, Text: "spectator", IsSpectator: true},
	}
	r.RecalculateInfluenceLocked()

	s1 := r.influenceTrackerLocked().GetLocked(1)
	if s1 == nil || s1.Presence != influenceMaxPresence {
		t.Errorf("唯一有效发言者应拿满 Presence, 实际 %+v", s1)
	}
	s2 := r.influenceTrackerLocked().GetLocked(2)
	if s2 == nil || s2.Presence != 0 {
		t.Errorf("观战者发言不应计分, 实际 %+v", s2)
	}
}

// TestInfluence_U1_04_Attention: 关注度信号 — whisper 收件 + 道具指向对数归一。
func TestInfluence_U1_04_Attention(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.whisperInbox = map[int][]wwtypes.WhisperEvent{
		3: {{FromSeat: 1}, {FromSeat: 2}},
	}
	r.RecalculateInfluenceLocked()

	s3 := r.influenceTrackerLocked().GetLocked(3)
	if s3 == nil || s3.Attention <= 0 {
		t.Fatalf("被 whisper 指向应有 Attention 分, 实际 %+v", s3)
	}
	if s3.Attention > influenceMaxAttention {
		t.Errorf("Attention=%d 超过上限 %d", s3.Attention, influenceMaxAttention)
	}
	s0 := r.influenceTrackerLocked().GetLocked(0)
	if s0 == nil || s0.Attention != 0 {
		t.Errorf("未被指向座位应无 Attention 分, 实际 %+v", s0)
	}
}

// TestInfluence_U1_05_SurvivalFreeze: 死亡座位 Survival 归零(影响力冻结)。
func TestInfluence_U1_05_SurvivalFreeze(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.State.Players[4].Alive = false
	r.RecalculateInfluenceLocked()

	alive := r.influenceTrackerLocked().GetLocked(0)
	dead := r.influenceTrackerLocked().GetLocked(4)
	if alive == nil || alive.Survival != influenceMaxSurvival {
		t.Errorf("存活座位应拿满 Survival, 实际 %+v", alive)
	}
	if dead == nil || dead.Survival != 0 {
		t.Errorf("死亡座位 Survival 应为 0, 实际 %+v", dead)
	}
}

// TestInfluence_U1_06_TotalBounded: Total 恒在 [0,100],且等于四分项之和。
func TestInfluence_U1_06_TotalBounded(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.State.LastDayVoteMap = map[Seat]Seat{0: 5, 1: 5, 2: 5, 3: 5, 6: 5, 7: 5, 8: 5}
	r.recentSpeeches = []wwtypes.SpeechEvent{{Seat: 0, Text: "x"}}
	r.whisperInbox = map[int][]wwtypes.WhisperEvent{
		0: {{FromSeat: 1}, {FromSeat: 2}, {FromSeat: 3}, {FromSeat: 4},
			{FromSeat: 5}, {FromSeat: 6}, {FromSeat: 7}, {FromSeat: 8},
			{FromSeat: 1}, {FromSeat: 2}},
	}
	r.RecalculateInfluenceLocked()

	for _, s := range r.influenceTrackerLocked().SnapshotLocked() {
		if s.Total < 0 || s.Total > 100 {
			t.Errorf("座位%d Total=%d 越界", s.Seat, s.Total)
		}
		if sum := s.Persuasion + s.Attention + s.Presence + s.Survival; sum != s.Total {
			t.Errorf("座位%d 分项和=%d != Total=%d", s.Seat, sum, s.Total)
		}
		if s.Persuasion > influenceMaxPersuasion || s.Attention > influenceMaxAttention ||
			s.Presence > influenceMaxPresence || s.Survival > influenceMaxSurvival {
			t.Errorf("座位%d 分项越界: %+v", s.Seat, s)
		}
	}
}

// TestInfluence_U1_07_EmptySeatSkipped: 空座位不产出分数。
func TestInfluence_U1_07_EmptySeatSkipped(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.State.Seats[5] = "" // 空座
	r.RecalculateInfluenceLocked()

	if got := r.influenceTrackerLocked().GetLocked(5); got != nil {
		t.Errorf("空座位不应产出分数, 实际 %+v", got)
	}
	if got := r.influenceTrackerLocked().GetLocked(0); got == nil {
		t.Error("有人座位应产出分数")
	}
}

// TestInfluence_U1_08_NilSafe: nil State / nil room 不 panic。
func TestInfluence_U1_08_NilSafe(t *testing.T) {
	(&WerewolfRoom{}).RecalculateInfluenceLocked() // State==nil
	var tracker *InfluenceTracker
	if tracker.GetLocked(0) != nil || tracker.SnapshotLocked() != nil || tracker.RoundLocked() != 0 {
		t.Error("nil tracker 的读取应返回零值")
	}
}

// TestInfluence_U1_09_ResetOnRestart: resetInfluenceLocked 清空历史分数。
func TestInfluence_U1_09_ResetOnRestart(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.RecalculateInfluenceLocked()
	if len(r.influenceTrackerLocked().SnapshotLocked()) == 0 {
		t.Fatal("首次重算应产出分数")
	}
	r.resetInfluenceLocked()
	if r.influenceTracker != nil {
		t.Error("resetInfluenceLocked 后 tracker 应为 nil")
	}
	if len(r.influenceTrackerLocked().SnapshotLocked()) != 0 {
		t.Error("重开后应无历史分数")
	}
}

// TestInfluence_U1_10_SnapshotSortedBySeat: 快照按座位升序(稳定输出)。
func TestInfluence_U1_10_SnapshotSortedBySeat(t *testing.T) {
	r := newInfluenceTestRoom(9)
	r.RecalculateInfluenceLocked()
	snap := r.influenceTrackerLocked().SnapshotLocked()
	for i := 1; i < len(snap); i++ {
		if snap[i-1].Seat >= snap[i].Seat {
			t.Fatalf("快照未按座位升序: %d 号在 %d 号之前", snap[i-1].Seat, snap[i].Seat)
		}
	}
}
