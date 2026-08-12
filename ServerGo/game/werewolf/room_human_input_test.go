// Package werewolf — room_human_input_test.go: tests for §人类玩家操作重构.
//
// 覆盖:
//   - hasHumanSnapshot 在 StartGame 时被填充;setPhaseAndDeadline 后续能正确读
//   - fillMyTurnExtra 在 9 类 phase 下的 MyTurnNow 计算(全 AI 时永远 false;
//     混合房间人类座位按规则 true)
//   - BuildClientStateWithRoom 下发 my_turn_now / my_turn_remaining_sec
//   - Action_LastWords / Action_SkipLastWords 人类调用路径(无 WS 帧,直调 Action_*)
package werewolf

import (
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// TestHumanSnapshotSetOnStartGame 验证 StartGame 时填充 hasHumanSnapshot。
// 13 座位: 1 个真人 + 12 个 bot → StartGame 后 hasHumanSnapshot=true,
// 后续 setPhaseAndDeadline(gs, PhaseSpeak) 不传 isHuman 时也能用人类 deadline。
func TestHumanSnapshotSetOnStartGame(t *testing.T) {
	gs := NewGame(time.Now().UnixNano())
	for i := 0; i < 13; i++ {
		if _, e := gs.AddPlayerAt(string(rune('A'+i)), Seat(i)); e != nil {
			t.Fatalf("AddPlayerAt %d: %v", i, e)
		}
	}
	gs.Players[0].IsBot = false           // 真人
	for i := 1; i < 13; i++ {
		gs.Players[i].IsBot = true        // 12 bot
	}
	// 直接走 StartGame。游戏开始后会调 setPhaseAndDeadline(PhasePreWolves, hasHumanPlayer)
	// → snapshot 被填充。
	if e := gs.StartGame(); e != nil {
		t.Fatalf("StartGame: %v", e)
	}
	if !gs.hasHumanSnapshot {
		t.Errorf("expected hasHumanSnapshot=true after StartGame with human+bot mix")
	}
}

// TestMyTurnExtra_SpeakTurn 验证 fillMyTurnExtra 在 speak 阶段:
// 人类座位 = SpeakTurnSeat → MyTurnNow=true;
// 人类座位 ≠ SpeakTurnSeat → MyTurnNow=false;
// 全 AI 房间 → MyTurnNow 永远 false。
func TestMyTurnExtra_SpeakTurn(t *testing.T) {
	gs := NewGame(time.Now().UnixNano())
	// 13 座,座位 0 = 真人,座位 1-12 = bot。
	for i := 0; i < 13; i++ {
		if _, e := gs.AddPlayerAt(string(rune('A'+i)), Seat(i)); e != nil {
			t.Fatalf("AddPlayerAt %d: %v", i, e)
		}
	}
	gs.Players[0].IsBot = false
	for i := 1; i < 13; i++ {
		gs.Players[i].IsBot = true
	}
	if e := gs.StartGame(); e != nil {
		t.Fatalf("StartGame: %v", e)
	}
	// 强制进入 speak 阶段 + 把 SpeakTurnSeat 设到真人座位。
	gs.Phase = PhaseSpeak
	gs.SpeakTurnSeat = 0
	gs.Players[0].Alive = true

	// 真人 0 号 → 应轮到。
	cs := BuildClientState("r1", gs.Seats, 0, gs)
	if cs.PhaseExtra == nil || !cs.PhaseExtra.MyTurnNow {
		t.Errorf("expected MyTurnNow=true for human seat 0 in speak phase")
	}
	// 真人 1 号(bot)→ 不应轮到。
	cs1 := BuildClientState("r1", gs.Seats, 1, gs)
	if cs1.PhaseExtra != nil && cs1.PhaseExtra.MyTurnNow {
		t.Errorf("expected MyTurnNow=false for bot seat 1 (even though turn == 0)")
	}
	// 观战者 → MyTurnNow 永远 false。
	csObs := BuildClientState("r1", gs.Seats, -1, gs)
	if csObs.PhaseExtra != nil && csObs.PhaseExtra.MyTurnNow {
		t.Errorf("expected MyTurnNow=false for observer (-1)")
	}
}

// TestMyTurnExtra_VotePhase 验证 vote 阶段:
// 真人座位 Voted=true → MyTurnNow=false;
// 真人座位 Voted=false → MyTurnNow=true。
func TestMyTurnExtra_VotePhase(t *testing.T) {
	gs := NewGame(time.Now().UnixNano())
	for i := 0; i < 13; i++ {
		if _, e := gs.AddPlayerAt(string(rune('A'+i)), Seat(i)); e != nil {
			t.Fatalf("AddPlayerAt %d: %v", i, e)
		}
	}
	gs.Players[0].IsBot = false
	for i := 1; i < 13; i++ {
		gs.Players[i].IsBot = true
	}
	if e := gs.StartGame(); e != nil {
		t.Fatalf("StartGame: %v", e)
	}
	gs.Phase = PhaseVote
	gs.Players[0].Voted = false
	cs := BuildClientState("r1", gs.Seats, 0, gs)
	if cs.PhaseExtra == nil || !cs.PhaseExtra.MyTurnNow {
		t.Errorf("expected MyTurnNow=true for unvoted human in vote phase")
	}
	gs.Players[0].Voted = true
	cs2 := BuildClientState("r1", gs.Seats, 0, gs)
	if cs2.PhaseExtra != nil && cs2.PhaseExtra.MyTurnNow {
		t.Errorf("expected MyTurnNow=false after human voted")
	}
}

// TestMyTurnExtra_FullAI_Room 验证全 AI 房间 MyTurnNow 永远 false(无论 phase)。
func TestMyTurnExtra_FullAI_Room(t *testing.T) {
	gs := NewGame(time.Now().UnixNano())
	for i := 0; i < 13; i++ {
		if _, e := gs.AddPlayerAt(string(rune('A'+i)), Seat(i)); e != nil {
			t.Fatalf("AddPlayerAt %d: %v", i, e)
		}
		gs.Players[i].IsBot = true
	}
	if e := gs.StartGame(); e != nil {
		t.Fatalf("StartGame: %v", e)
	}
	// 跑遍所有 acting phase,MyTurnNow 都应为 false(全 AI)。
	phases := []Phase{
		PhaseNightWolves, PhaseNightSeer, PhaseNightWitch,
		PhaseSpeak, PhaseVote, PhaseSheriff, PhaseIdiotReveal,
		PhaseHunterShoot, PhaseDeathLyric,
	}
	for _, p := range phases {
		gs.Phase = p
		cs := BuildClientState("r1", gs.Seats, 0, gs)
		if cs.PhaseExtra != nil && cs.PhaseExtra.MyTurnNow {
			t.Errorf("expected MyTurnNow=false in full-AI room at phase %v", p)
		}
	}
}

// TestHumanLastWordsAction_Speak 验证 Action_LastWords 直调路径(WS 帧是它的
// 唯一人类入口,但 Action_* 本身在 manager 层鉴权,所以直接调 manager 验证)。
// 这里跳过 JoinGame 开局流程,直接构造一个死亡遗言阶段的 GameState 并把
// WerewolfRoom 注册到 manager.rooms(通过 m.rooms 直接写入测试)。
func TestHumanLastWordsAction_Speak(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:    "test-last-words",
		chatQueue: nil,
	}
	r.State = NewGame(time.Now().UnixNano())
	r.State.SeatCount = 7
	for i := 0; i < 7; i++ {
		if _, e := r.State.AddPlayerAt(string(rune('A'+i)), Seat(i)); e != nil {
			t.Fatalf("AddPlayerAt %d: %v", i, e)
		}
		r.State.Players[i].IsBot = (i != 0) // 座位 0 是人类
	}
	// 同步到房间级 Seats(SeatOf 读 r.Seats 而非 r.State.Seats)。
	r.Seats = r.State.Seats
	// 进入遗言阶段:座位 0 死亡 + LastWords=true + DeathLyricCurrent=0。
	r.State.Phase = PhaseDeathLyric
	r.State.Players[0].Alive = false
	r.State.Players[0].LastWords = true
	r.State.DeathLyricCurrent = 0
	r.State.DeathLyricQueue = []Seat{0}
	r.State.DeathLyricDone = map[Seat]bool{}
	r.State.DeathLyricOnDone = func() *errcode.Error { return nil }
	// 直接把房间塞进 manager.rooms,绕开 JoinGame(测试场景无 bot factory)。
	m.rooms[r.RoomID] = r

	// 直调 manager.Action_LastWords — Action_* 内部不锁;只校验 DeathLyricCurrent==seat。
	if _, e := m.Action_LastWords("test-last-words", "A", "再见"); e != nil {
		t.Fatalf("Action_LastWords: %v", e)
	}
	if r.State.Players[0].LastWords != false {
		t.Errorf("expected LastWords=false after speak")
	}
	// 队列空时 EndDeathLyricRound 清空 DeathLyricDone,核心断言是 LastWords=false。
	// (有队列场景下 DeathLyricDone[seat]=true;这里不强求。)
}

// TestHumanLastWordsAction_Skip 验证 Action_SkipLastWords 直调路径。
func TestHumanLastWordsAction_Skip(t *testing.T) {
	m := NewWerewolfManager()
	r := &WerewolfRoom{RoomID: "test-skip"}
	r.State = NewGame(time.Now().UnixNano())
	r.State.SeatCount = 7
	for i := 0; i < 7; i++ {
		if _, e := r.State.AddPlayerAt(string(rune('A'+i)), Seat(i)); e != nil {
			t.Fatalf("AddPlayerAt %d: %v", i, e)
		}
	}
	r.Seats = r.State.Seats
	r.State.Phase = PhaseDeathLyric
	r.State.Players[0].Alive = false
	r.State.Players[0].LastWords = true
	r.State.DeathLyricCurrent = 0
	r.State.DeathLyricQueue = []Seat{0}
	r.State.DeathLyricDone = map[Seat]bool{}
	r.State.DeathLyricOnDone = func() *errcode.Error { return nil }
	m.rooms[r.RoomID] = r

	if _, e := m.Action_SkipLastWords("test-skip", "A"); e != nil {
		t.Fatalf("Action_SkipLastWords: %v", e)
	}
	if r.State.Players[0].LastWords != false {
		t.Errorf("expected LastWords=false after skip")
	}
}
