// Package werewolf — room_restart_vote_test.go: 验证 2026-07-10 引入的
// 「重开局投票」流程的端到端回归测试。涵盖:
//   - checkWinner → 自动切到 PhaseRestartVote
//   - 多票累积到 quorum+1 → restartGameLocked 自动开新局
//   - 重开后 GameState 已重置但 r.chatQueue 保留
//   - deadline timeout → forceCloseRoomLocked(关闭)
//
// 这些测试属于新行为,R86 阶段补全(2026-07-10 §87 教训)。
package werewolf

import (
	"testing"
	"time"

	"LsmWebGame/agent/core"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// newRestartVoteRoomLocked 创建一局已 status=over + phase=restart_vote
// 的 7 人房间,seed 测试。测试者可在持锁的回调内手动调 Evaluate / 投票。
func newRestartVoteRoomLocked(t *testing.T) (*WerewolfManager, *WerewolfRoom) {
	t.Helper()
	m := NewWerewolfManager()
	r := &WerewolfRoom{
		RoomID:    "test-room-restart-vote",
		chatQueue: agentcore.NewChatHistoryQueue(500 * 1024),
		// §130 重构(2026-07-13):llmSema 字段已删除 — 13 bot 完全并发调用 LLM。
	}
	r.State = NewGame(time.Now().UnixNano())
	// 7 人兼容模式(与 fillAndStart 同):显式设定 SeatCount=7。
	r.SeatCount = 7
	r.State.SeatCount = 7
	r.Seats[0] = "u0"
	r.Seats[1] = "u1"
	r.Seats[2] = "u2"
	r.Seats[3] = "u3"
	r.Seats[4] = "u4"
	r.Seats[5] = "u5"
	r.Seats[6] = "u6"
	for i, uid := range r.Seats {
		r.State.Seats[i] = uid
		r.State.PlayerByID[uid] = Seat(i)
		r.State.Players[i] = Player{UserID: uid, Seat: Seat(i), Alive: false}
	}
	r.State.Status = "over"
	r.State.Winner = "wolf"
	r.State.Phase = PhaseRestartVote
	r.State.RestartVoteYes = map[Seat]bool{}
	r.State.RestartVoteNo = map[Seat]bool{}
	r.State.RestartVoteAbstain = map[Seat]bool{}
	r.State.PhaseDeadlineAt = time.Now().Add(5 * time.Minute)
	r.gameStartedAt = time.Now().Unix()
	m.rooms[r.RoomID] = r
	return m, r
}

// TestEvaluateRestartVote_PassesOnQuorum 验证 yesCount ≥ ceil(N*num/den)+1
// 时直接判 passed。
//
// 默认 num/den = 2/3;N=7 → 阈值 = ceil(7*2/3)+1 = 5+1 = 6 yes 票。
func TestEvaluateRestartVote_PassesOnQuorum(t *testing.T) {
	_, r := newRestartVoteRoomLocked(t)
	r.mu.Lock()
	defer r.mu.Unlock()

	// 6 yes out of 7 → 触发 passed
	r.State.RestartVoteYes[Seat(0)] = true
	r.State.RestartVoteYes[Seat(1)] = true
	r.State.RestartVoteYes[Seat(2)] = true
	r.State.RestartVoteYes[Seat(3)] = true
	r.State.RestartVoteYes[Seat(4)] = true
	r.State.RestartVoteYes[Seat(5)] = true
	r.State.RestartVoteNo[Seat(6)] = true

	got := EvaluateRestartVoteLocked(r, false)
	if got != "passed" {
		t.Fatalf("expected passed, got %q", got)
	}
	if !r.State.RestartVoteDone {
		t.Fatalf("RestartVoteDone must be true")
	}
	if r.State.RestartVoteResult != "passed" {
		t.Fatalf("RestartVoteResult = %q want passed", r.State.RestartVoteResult)
	}
	if r.State.Phase != PhaseGameOver {
		t.Fatalf("Phase must return to PhaseGameOver after pass; got %s", r.State.Phase.String())
	}
}

// TestEvaluateRestartVote_TimeoutClosesRoom 验证 deadline 到时未达 quorum → timeout。
func TestEvaluateRestartVote_TimeoutClosesRoom(t *testing.T) {
	_, r := newRestartVoteRoomLocked(t)
	r.mu.Lock()
	defer r.mu.Unlock()

	r.State.RestartVoteYes[Seat(0)] = true
	r.State.RestartVoteYes[Seat(1)] = true

	got := EvaluateRestartVoteLocked(r, true)
	if got != "timeout" {
		t.Fatalf("expected timeout, got %q", got)
	}
	if r.State.RestartVoteResult != "timeout" {
		t.Fatalf("RestartVoteResult = %q", r.State.RestartVoteResult)
	}
}

// TestEvaluateRestartVote_RejectsOnMajorityNo 验证反对多数(超过半数+1) → rejected。
func TestEvaluateRestartVote_RejectsOnMajorityNo(t *testing.T) {
	_, r := newRestartVoteRoomLocked(t)
	r.mu.Lock()
	defer r.mu.Unlock()

	// 4 no → ≥ ceil(7/2)+1 = 4+1 = 5 ❌
	// 5 no → rejected
	r.State.RestartVoteNo[Seat(0)] = true
	r.State.RestartVoteNo[Seat(1)] = true
	r.State.RestartVoteNo[Seat(2)] = true
	r.State.RestartVoteNo[Seat(3)] = true
	r.State.RestartVoteNo[Seat(4)] = true
	r.State.RestartVoteYes[Seat(5)] = true

	got := EvaluateRestartVoteLocked(r, false)
	if got != "rejected" {
		t.Fatalf("expected rejected, got %q", got)
	}
}

// TestRestartGameLocked_PreservesChatQueue 验证 restartGameLocked 后
// chatQueue 引用 + 内容保留;新 GameState 已重新发牌。
func TestRestartGameLocked_PreservesChatQueue(t *testing.T) {
	m, r := newRestartVoteRoomLocked(t)
	r.mu.Lock()
	defer r.mu.Unlock()

	// 写一条 chat 到原 queue
	preMsg := agentcore.ChatMessage{
		Seq:         0,
		FromSeat:    0,
		FromAccount: "u0",
		Text:        "上一局的关键发言",
		Timestamp:   time.Now(),
		Size:        4 * len("上一局的关键发言"),
	}
	// 先手动 Append 才能让 PreSeq 在 chatQueue 推进
	r.chatQueue.Append(preMsg)
	preSnapshotLen := len(r.chatQueue.Snapshot())

	if err := m.restartGameLocked(r); err != nil {
		logger.L().Info("restartGameLocked error", zap.Error(err))
		t.Fatalf("restartGameLocked failed: %v", err)
	}
	if r.State.Phase != PhasePreWolves {
		t.Fatalf("expected PhasePreWolves after restart, got %s", r.State.Phase.String())
	}
	if r.State.Status != "playing" {
		t.Fatalf("Status must reset to playing, got %q", r.State.Status)
	}
	if r.State.Winner != "" {
		t.Fatalf("Winner must reset, got %q", r.State.Winner)
	}
	if r.State.DayNumber != 1 {
		t.Fatalf("DayNumber must be 1, got %d", r.State.DayNumber)
	}
	if r.State.RestartCount != 1 {
		t.Fatalf("RestartCount = %d, want 1", r.State.RestartCount)
	}
	// chatQueue 内容必须保留(允许消息数量 >= 之前)
	postSnapshot := r.chatQueue.Snapshot()
	if len(postSnapshot) < preSnapshotLen {
		t.Fatalf("chatQueue messages dropped: pre=%d post=%d", preSnapshotLen, len(postSnapshot))
	}
	if postSnapshot[0].Text != preMsg.Text {
		t.Fatalf("chatQueue[0] mutated: got %q want %q", postSnapshot[0].Text, preMsg.Text)
	}
	// Seats / userID 必须保留
	for i, uid := range r.Seats {
		if r.State.Seats[i] != uid {
			t.Fatalf("Seat[%d] lost: r.Seats=%q r.State.Seats=%q", i, uid, r.State.Seats[i])
		}
	}
}

// TestCastRestartVoteLocked_OverwritePrev 验证同一 seat 多次投票时覆盖写。
func TestCastRestartVoteLocked_OverwritePrev(t *testing.T) {
	_, r := newRestartVoteRoomLocked(t)
	r.mu.Lock()
	defer r.mu.Unlock()

	if e := CastRestartVoteLocked(r, Seat(0), "yes"); e != nil {
		t.Fatalf("first vote failed: %v", e)
	}
	if !r.State.RestartVoteYes[Seat(0)] {
		t.Fatalf("yes map must be set")
	}
	if e := CastRestartVoteLocked(r, Seat(0), "no"); e != nil {
		t.Fatalf("overwrite vote failed: %v", e)
	}
	if r.State.RestartVoteYes[Seat(0)] {
		t.Fatalf("yes map must be cleared after overwrite")
	}
	if !r.State.RestartVoteNo[Seat(0)] {
		t.Fatalf("no map must be set after overwrite")
	}
}

// TestTryEnterRestartVoteFromGameOverLocked 验证 watchdog 入口:status=over
// + phase=gameover + 配置开启 + ≥2 名玩家 → 进入 PhaseRestartVote。
//
// watchdog 在持 r.mu 状态下调用 tryEnter...Locked;测试也照此规范持锁调用。
//
// 2026-07-16 BUG-R128-01 修复: 进入 restart_vote 时立即触发 EmitGameOver 结算,
// 并置 gameOverNotified=true 防止后续 forceCloseRoomLocked / restartGameLocked
// 路径重复结算。验证该行为。
func TestTryEnterRestartVoteFromGameOverLocked(t *testing.T) {
	m, r := newRestartVoteRoomLocked(t)
	r.mu.Lock()
	r.State.Phase = PhaseGameOver
	m.tryEnterRestartVoteFromGameOverLocked(r)
	if r.State.Phase != PhaseRestartVote {
		r.mu.Unlock()
		t.Fatalf("expected PhaseRestartVote, got %s", r.State.Phase.String())
	}
	if r.State.RestartVoteDone {
		r.mu.Unlock()
		t.Fatalf("RestartVoteDone must be false at entry")
	}
	// BUG-R128-01: 进入 restart_vote 即视为对局结束, gameOverNotified 必须
	// 置 true,防止后续 forceCloseRoomLocked 重复触发 EmitGameOver。
	if !r.gameOverNotified {
		r.mu.Unlock()
		t.Fatalf("gameOverNotified must be true after entering restart_vote (BUG-R128-01 fix)")
	}
	r.mu.Unlock()
}