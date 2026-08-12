// Package werewolf - challenge_20260810_11_test.go: §20260810-11 H2 challenge 工具测试
//
// 覆盖:
//  1. 成功路径:challengeLocked 写入状态
//  2. 校验失败:wrong phase / dead / self / empty / too long / used today
//  3. 锁内变体语义(校验顺序)
//  4. 公开 Action_Challenge 入口
//
// 此测试不依赖 mgr.createRoom,用最小的 GameState 直接调底层函数。

package werewolf

import (
	"strings"
	"testing"

	"LsmAgentGame/errcode"
)

// makeChallengeTestState 创建 7 座位全存活 + 全 PhaseSpeak 的测试用 GameState。
func makeChallengeTestState() *GameState {
	gs := &GameState{
		Phase:     PhaseSpeak,
		DayNumber: 1,
	}
	for i := Seat(0); i < Seat(7); i++ {
		gs.Seats[i] = "user-" + string(rune('A'+int(i)))
		gs.Players[i] = Player{
			Seat:               i,
			Alive:              true,
			ChallengeUsedToday: false,
			LastChallengedBy:   -1,
		}
	}
	return gs
}

// TestChallenge_SuccessPath 验证成功路径。
func TestChallenge_SuccessPath(t *testing.T) {
	gs := makeChallengeTestState()
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	e := m.challengeLocked(r, 0, 1, "你昨晚为什么不救 5 号?")
	if e != nil {
		t.Fatalf("expected success, got %v", e)
	}
	if !gs.Players[0].ChallengeUsedToday {
		t.Errorf("seat 0 should have ChallengeUsedToday=true")
	}
	if gs.Players[1].LastChallengedBy != 0 {
		t.Errorf("seat 1 LastChallengedBy want 0, got %d", gs.Players[1].LastChallengedBy)
	}
	if gs.Players[1].LastChallengeQuestion != "你昨晚为什么不救 5 号?" {
		t.Errorf("seat 1 LastChallengeQuestion not set correctly: %q",
			gs.Players[1].LastChallengeQuestion)
	}
	// 其他座位不受影响
	if gs.Players[2].LastChallengedBy != -1 {
		t.Errorf("seat 2 should have LastChallengedBy=-1 (untouched), got %d",
			gs.Players[2].LastChallengedBy)
	}
}

// TestChallenge_WrongPhase 验证非 PhaseSpeak 阶段拒绝。
func TestChallenge_WrongPhase(t *testing.T) {
	gs := makeChallengeTestState()
	gs.Phase = PhaseNightWolves
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	e := m.challengeLocked(r, 0, 1, "测试问题")
	if e == nil {
		t.Fatal("expected error for PhaseNightWolves, got nil")
	}
	if e.Code != errcode.ErrValidationFailed {
		t.Errorf("want ErrValidationFailed, got %d", e.Code)
	}
	if !strings.Contains(e.Message, "PhaseSpeak") {
		t.Errorf("error message should mention PhaseSpeak: %q", e.Message)
	}
}

// TestChallenge_UsedToday 验证每人每天 1 次限制。
func TestChallenge_UsedToday(t *testing.T) {
	gs := makeChallengeTestState()
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	if e := m.challengeLocked(r, 0, 1, "首次质疑"); e != nil {
		t.Fatalf("first challenge should succeed, got %v", e)
	}
	// 第二次同座位同 phase 应失败
	e := m.challengeLocked(r, 0, 2, "二次质疑")
	if e == nil {
		t.Fatal("expected error for used today, got nil")
	}
	if !strings.Contains(e.Message, "already used") {
		t.Errorf("error should mention 'already used': %q", e.Message)
	}
}

// TestChallenge_SelfForbidden 验证不能自疑。
func TestChallenge_SelfForbidden(t *testing.T) {
	gs := makeChallengeTestState()
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	e := m.challengeLocked(r, 0, 0, "测试")
	if e == nil {
		t.Fatal("expected error for self challenge, got nil")
	}
	if !strings.Contains(e.Message, "self") {
		t.Errorf("error should mention 'self': %q", e.Message)
	}
}

// TestChallenge_EmptyQuestion 验证空问题拒绝。
func TestChallenge_EmptyQuestion(t *testing.T) {
	gs := makeChallengeTestState()
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	e := m.challengeLocked(r, 0, 1, "")
	if e == nil {
		t.Fatal("expected error for empty question, got nil")
	}
	if !strings.Contains(e.Message, "empty") {
		t.Errorf("error should mention 'empty': %q", e.Message)
	}
}

// TestChallenge_TooLong 验证 60 字上限(按 rune 计数)。
func TestChallenge_TooLong(t *testing.T) {
	gs := makeChallengeTestState()
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	// 61 字符
	longQ := strings.Repeat("问", 61)
	e := m.challengeLocked(r, 0, 1, longQ)
	if e == nil {
		t.Fatal("expected error for too long question, got nil")
	}
	if !strings.Contains(e.Message, "too long") {
		t.Errorf("error should mention 'too long': %q", e.Message)
	}
}

// TestChallenge_DeadTarget 验证质疑死亡玩家失败。
func TestChallenge_DeadTarget(t *testing.T) {
	gs := makeChallengeTestState()
	gs.Players[1].Alive = false
	r := &WerewolfRoom{State: gs}
	m := &WerewolfManager{}

	e := m.challengeLocked(r, 0, 1, "测试")
	if e == nil {
		t.Fatal("expected error for dead target, got nil")
	}
	if !strings.Contains(e.Message, "dead") {
		t.Errorf("error should mention 'dead': %q", e.Message)
	}
}

// TestChallenge_ActionChallenge_PublicEntry 验证公开 Action_Challenge 入口。
// 跳过(需要 mgr.createRoom + Hub setup),改为编译期断言:Action_Challenge 签名存在。
func TestChallenge_ActionChallenge_PublicEntry(t *testing.T) {
	// 编译期保证:Action_Challenge 签名
	var _ func(*WerewolfManager, string, string, Seat, string) (*WerewolfRoom, *errcode.Error) =
		(*WerewolfManager).Action_Challenge

	// 运行时也跑一遍基础 happy path
	gs := makeChallengeTestState()
	r := &WerewolfRoom{State: gs, RoomID: "test-room"}
	// 同步 r.Seats 与 gs.Seats(SeatOf 读 r.Seats)
	for i := 0; i < 7; i++ {
		r.Seats[i] = gs.Seats[i]
	}
	m := &WerewolfManager{rooms: map[string]*WerewolfRoom{r.RoomID: r}}
	uid := gs.Seats[0]

	_, e := m.Action_Challenge(r.RoomID, uid, 1, "公开质疑测试")
	if e != nil {
		t.Fatalf("Action_Challenge should succeed, got %v", e)
	}
	if !gs.Players[0].ChallengeUsedToday {
		t.Errorf("seat 0 should have ChallengeUsedToday=true after Action_Challenge")
	}
}

// TestChallenge_BlockRender 跳过 (ChallengeBlock 在 agent/wwplayer 包,
// 本测试文件仅覆盖 game/werewolf 包的锁内变体与公开 Action 入口)
// 详见 ServerGo/agent/wwplayer/prop_blocks_test.go(若新增)。
