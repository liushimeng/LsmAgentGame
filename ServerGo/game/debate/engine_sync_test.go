// Package debate — 引擎同步与广播钩子测试(2026-08-31 §20260831-02)。
//
// 覆盖:
//   - driveSpeaker 阻塞等待 NotifyTurnDone(§20260831-02 核心修复)
//   - SubmitSpeech 成功后触发 onSpeech 钩子
//   - SubmitCrossExamQuestion 触发 onCrossExam 钩子
//   - AddJudgeScore 触发 onJudgeScore 钩子
//   - runJudgingPhase 收齐评分后构建 result(§20260831-02 补 BuildResult 接线)
package debate

import (
	"context"
	"testing"
	"time"
)

// newSyncTestEnv 构造带 2 队 × 2 人 + 2 裁判的房间与已启动 ctx 的引擎。
func newSyncTestEnv(t *testing.T) (*DebateManager, *DebateRoom, *DebateEngine, context.CancelFunc) {
	t.Helper()
	mgr := NewDebateManager()
	cfg := RoomConfig{
		Topic:           DebateTopic{ID: "classic_001", Text: "人性本善", Type: "classic"},
		Mode:            ModeTwoTeam,
		PhaseConfig:     QuickPhaseConfig(),
		SpectatorConfig: DefaultSpectatorConfig(),
		Teams: []TeamConfig{
			{TeamID: 0, Stance: StancePro, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m1"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m2"},
				{SeatID: 2, Role: RoleThird, ModelKey: "m3"},
			}},
			{TeamID: 1, Stance: StanceCon, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m4"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m5"},
				{SeatID: 2, Role: RoleThird, ModelKey: "m6"},
			}},
		},
		Judges: []JudgeConfig{
			{JudgeID: 0, ModelKey: "m5"},
			{JudgeID: 1, ModelKey: "m6"},
		},
		CreatedBy: "user1",
	}
	room, e := mgr.CreateRoom(cfg)
	if e != nil {
		t.Fatalf("CreateRoom failed: %v", e.Message)
	}
	eng := NewDebateEngine(room, mgr)
	ctx, cancel := context.WithCancel(context.Background())
	eng.ctx = ctx
	eng.cancel = cancel
	return mgr, room, eng, cancel
}

// TestDriveSpeakerWaitsForTurnDone 验证 driveSpeaker 在 NotifyTurnDone 前阻塞。
func TestDriveSpeakerWaitsForTurnDone(t *testing.T) {
	_, room, eng, cancel := newSyncTestEnv(t)
	defer cancel()

	room.SetPhase(PhaseOpeningArgument)

	done := make(chan struct{})
	go func() {
		eng.driveSpeaker(PhaseOpeningArgument, 0, 0)
		close(done)
	}()

	// 未通知前必须阻塞(500ms 内不允许返回)
	select {
	case <-done:
		t.Fatal("driveSpeaker 在 NotifyTurnDone 之前不应返回")
	case <-time.After(500 * time.Millisecond):
	}

	// 通知后应立即返回
	eng.NotifyTurnDone(0, 0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("NotifyTurnDone 之后 driveSpeaker 应返回")
	}
}

// TestDriveSpeakerTimesOut 引擎 ctx 取消后 driveSpeaker 必须兜底放行(不永久卡死)。
//
// §20260831-02 v2:等待预算与阶段 deadline 解耦(固定 110s),
// 兜底路径 = ctx 取消(房间解散 / 比赛终止)。
func TestDriveSpeakerTimesOut(t *testing.T) {
	_, room, eng, cancel := newSyncTestEnv(t)
	defer cancel()

	room.SetPhase(PhaseOpeningArgument)

	done := make(chan struct{})
	go func() {
		eng.driveSpeaker(PhaseOpeningArgument, 0, 0)
		close(done)
	}()

	// 未通知前阻塞
	select {
	case <-done:
		t.Fatal("driveSpeaker 在 NotifyTurnDone / ctx 取消之前不应返回")
	case <-time.After(300 * time.Millisecond):
	}

	// ctx 取消 → 兜底放行
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("引擎 ctx 取消后 driveSpeaker 应兜底放行")
	}
}

// TestOnSpeechHookFired SubmitSpeech 成功后应触发 onSpeech 钩子。
func TestOnSpeechHookFired(t *testing.T) {
	mgr, room, _, cancel := newSyncTestEnv(t)
	defer cancel()

	fired := make(chan Speech, 1)
	mgr.SetOnSpeech(func(roomID string, sp Speech) {
		if roomID == room.RoomID {
			fired <- sp
		}
	})

	room.SetPhase(PhaseOpeningArgument)
	res := room.SubmitSpeech(0, 0, SpeechParams{Content: "我方认为人性本善,论点一:恻隐之心人皆有之。"})
	if !res.OK {
		t.Fatalf("SubmitSpeech failed: %s", res.Message)
	}

	select {
	case sp := <-fired:
		if sp.TeamID != 0 || sp.Content == "" {
			t.Errorf("hook 收到的发言异常: %+v", sp)
		}
	case <-time.After(time.Second):
		t.Fatal("SubmitSpeech 成功后 onSpeech 钩子未触发")
	}
}

// TestOnCrossExamHookFired 质询提问应触发 onCrossExam 钩子。
func TestOnCrossExamHookFired(t *testing.T) {
	mgr, room, _, cancel := newSyncTestEnv(t)
	defer cancel()

	fired := make(chan CrossExamEntry, 1)
	mgr.SetOnCrossExam(func(roomID string, e CrossExamEntry) {
		if roomID == room.RoomID {
			fired <- e
		}
	})

	room.SetPhase(PhaseCrossExamination)
	res := room.SubmitCrossExamQuestion(0, 2, CrossExamQuestionParams{
		TargetTeam: 1, TargetSeat: 1, Question: "请问对方辩友如何解释恶行的存在?",
	})
	if !res.OK {
		t.Fatalf("SubmitCrossExamQuestion failed: %s", res.Message)
	}

	select {
	case e := <-fired:
		if e.Question == "" {
			t.Error("钩子收到的质询问题为空")
		}
	case <-time.After(time.Second):
		t.Fatal("SubmitCrossExamQuestion 后 onCrossExam 钩子未触发")
	}
}

// TestOnJudgeScoreHookFired AddJudgeScore 应触发 onJudgeScore 钩子。
func TestOnJudgeScoreHookFired(t *testing.T) {
	mgr, room, _, cancel := newSyncTestEnv(t)
	defer cancel()

	fired := make(chan JudgeScore, 1)
	mgr.SetOnJudgeScore(func(roomID string, sc JudgeScore) {
		if roomID == room.RoomID {
			fired <- sc
		}
	})

	room.AddJudgeScore(FallbackJudgeScore(0, "m5", 2))

	select {
	case sc := <-fired:
		if sc.JudgeID != 0 {
			t.Errorf("钩子收到的评分 judge_id = %d, want 0", sc.JudgeID)
		}
	case <-time.After(time.Second):
		t.Fatal("AddJudgeScore 后 onJudgeScore 钩子未触发")
	}
}

// TestRunJudgingPhaseBuildsResult 评审阶段收齐评分后必须构建 result。
//
// §20260831-02:首期版本漏接 BuildResult,result 恒 nil。
func TestRunJudgingPhaseBuildsResult(t *testing.T) {
	_, room, eng, cancel := newSyncTestEnv(t)
	defer cancel()

	// 预置 2 份评分(= judgeCount)→ runJudgingPhase 的等待立即满足
	room.AddJudgeScore(JudgeScore{
		JudgeID: 0, ModelKey: "m5", WinnerTeamID: 0,
		Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{8, 8, 7, 8, 7}, TotalScore: 38, Comment: "good"},
			{TeamID: 1, Scores: ScoreDimensions{6, 7, 7, 6, 7}, TotalScore: 33, Comment: "ok"},
		},
	})
	room.AddJudgeScore(JudgeScore{
		JudgeID: 1, ModelKey: "m6", WinnerTeamID: 0,
		Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{7, 8, 7, 7, 8}, TotalScore: 37, Comment: "good"},
			{TeamID: 1, Scores: ScoreDimensions{7, 6, 7, 7, 6}, TotalScore: 33, Comment: "ok"},
		},
	})

	room.SetPhase(PhaseJudging)
	eng.runJudgingPhase()

	if room.Phase() != PhaseResult {
		t.Errorf("评审后阶段 = %s, want %s", room.Phase(), PhaseResult)
	}
	res := room.Result()
	if res == nil {
		t.Fatal("runJudgingPhase 后 result 不应为 nil(BuildResult 未接线)")
	}
	if res.WinnerTeamID != 0 {
		t.Errorf("winner = %d, want 0(两裁判都投队 0)", res.WinnerTeamID)
	}
	if len(res.TeamScores) != 2 {
		t.Errorf("TeamScores len = %d, want 2", len(res.TeamScores))
	}
}
