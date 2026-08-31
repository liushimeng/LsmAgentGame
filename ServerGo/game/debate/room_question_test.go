// Package debate — §20260831-06 观众提问队列 + 模型胜率统计单测。
package debate

import (
	"testing"
)

// newTestRoomWithTeams 构造带 2 队 × 2 辩手 + 2 裁判的最小房间。
func newTestRoomWithTeams(t *testing.T) *DebateRoom {
	t.Helper()
	cfg := RoomConfig{
		Topic: DebateTopic{ID: "t1", Text: "测试辩题", Type: "classic"},
		Mode:  ModeTwoTeam,
		Teams: []TeamConfig{
			{TeamID: 0, Stance: StancePro, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "A-model"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "B-model"},
			}},
			{TeamID: 1, Stance: StanceCon, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "C-model"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "D-model"},
			}},
		},
		Judges:    []JudgeConfig{{JudgeID: 0, ModelKey: "E-model"}, {JudgeID: 1, ModelKey: "F-model"}},
		CreatedBy: "owner",
	}
	return NewDebateRoom("debate_test_room", cfg, nil)
}

func TestAddSpectatorQuestion(t *testing.T) {
	r := newTestRoomWithTeams(t)

	q, err := r.AddSpectatorQuestion("user_1", "裁判如何看待正方定义?")
	if err != nil {
		t.Fatalf("AddSpectatorQuestion failed: %v", err)
	}
	if q.ID == "" || q.AnswerJudgeID != -1 {
		t.Fatalf("unexpected question: %+v", q)
	}

	qs := r.SpectatorQuestions()
	if len(qs) != 1 || qs[0].Text != "裁判如何看待正方定义?" {
		t.Fatalf("unexpected queue: %+v", qs)
	}

	// 空文本拒绝
	if _, err := r.AddSpectatorQuestion("user_1", ""); err == nil {
		t.Fatal("empty text should be rejected")
	}

	// 超长截断到 200 字
	long := ""
	for i := 0; i < 250; i++ {
		long += "问"
	}
	q2, err := r.AddSpectatorQuestion("user_2", long)
	if err != nil {
		t.Fatalf("long question failed: %v", err)
	}
	if CountRune(q2.Text) != 200 {
		t.Fatalf("expected truncation to 200 runes, got %d", CountRune(q2.Text))
	}
}

func TestSpectatorQuestionRingBuffer(t *testing.T) {
	r := newTestRoomWithTeams(t)

	for i := 0; i < maxSpectatorQuestions+5; i++ {
		if _, err := r.AddSpectatorQuestion("user", "问题"); err != nil {
			t.Fatalf("add #%d failed: %v", i, err)
		}
	}
	qs := r.SpectatorQuestions()
	if len(qs) != maxSpectatorQuestions {
		t.Fatalf("expected ring buffer cap %d, got %d", maxSpectatorQuestions, len(qs))
	}
}

func TestAnswerSpectatorQuestion(t *testing.T) {
	r := newTestRoomWithTeams(t)

	q, _ := r.AddSpectatorQuestion("user_1", "正方定义是否过宽?")

	// 未回答时出现在未回答列表
	if len(r.UnansweredSpectatorQuestions()) != 1 {
		t.Fatal("expected 1 unanswered question")
	}

	answered, err := r.AnswerSpectatorQuestion(0, q.ID, "定义范围合理,双方均有展开。")
	if err != nil {
		t.Fatalf("AnswerSpectatorQuestion failed: %v", err)
	}
	if answered.Answer == "" || answered.AnswerJudgeID != 0 || answered.AnsweredAtMS == 0 {
		t.Fatalf("unexpected answered record: %+v", answered)
	}

	// 已回答后从未回答列表消失
	if len(r.UnansweredSpectatorQuestions()) != 0 {
		t.Fatal("expected 0 unanswered questions after answer")
	}

	// 幂等:重复回答返回已有记录,不报错
	again, err := r.AnswerSpectatorQuestion(1, q.ID, "第二个裁判的回答")
	if err != nil {
		t.Fatalf("idempotent answer failed: %v", err)
	}
	if again.AnswerJudgeID != 0 || again.Answer != "定义范围合理,双方均有展开。" {
		t.Fatalf("expected first answer preserved: %+v", again)
	}

	// 不存在的 ID 报错
	if _, err := r.AnswerSpectatorQuestion(0, "q_nonexistent", "x"); err != ErrQuestionNotFound {
		t.Fatalf("expected ErrQuestionNotFound, got %v", err)
	}

	// 空回答报错
	if _, err := r.AnswerSpectatorQuestion(0, q.ID, ""); err == nil {
		t.Fatal("empty answer should be rejected")
	}
}

func TestGameOverRejectsQuestion(t *testing.T) {
	r := newTestRoomWithTeams(t)
	r.SetPhase(PhaseGameOver)
	if _, err := r.AddSpectatorQuestion("user_1", "还能提问吗"); err != ErrQuestionRejected {
		t.Fatalf("expected ErrQuestionRejected after game over, got %v", err)
	}
}

func TestStatsRecordGameResult(t *testing.T) {
	m := NewDebateManager()
	r := newTestRoomWithTeams(t)

	res := &DebateResult{
		WinnerTeamID:   0,
		WinnerTeamName: "正方",
		BestDebater:    BestDebaterInfo{Seat: 1, TeamID: 0, Name: "正方二辩"},
		TeamScores: []TeamFinalScore{
			{TeamID: 0, TotalScore: 42.0},
			{TeamID: 1, TotalScore: 35.0},
		},
	}
	m.RecordGameResult(r, res)

	stats := m.Stats()
	if len(stats) != 4 {
		t.Fatalf("expected 4 model stats, got %d: %+v", len(stats), stats)
	}
	byKey := map[string]ModelStats{}
	for _, s := range stats {
		byKey[s.ModelKey] = s
	}

	// A-model(正方一辩):1 场 1 胜,非最佳辩手,均分 42
	if s := byKey["A-model"]; s.TotalGames != 1 || s.WinCount != 1 || s.BestDebaterCount != 0 || s.AvgTotalScore != 42.0 || s.WinRate != 1.0 {
		t.Fatalf("A-model unexpected: %+v", s)
	}
	// B-model(正方二辩):最佳辩手
	if s := byKey["B-model"]; s.BestDebaterCount != 1 || s.WinCount != 1 {
		t.Fatalf("B-model unexpected: %+v", s)
	}
	// C-model(反方一辩):1 场 0 胜,均分 35
	if s := byKey["C-model"]; s.TotalGames != 1 || s.WinCount != 0 || s.AvgTotalScore != 35.0 || s.WinRate != 0 {
		t.Fatalf("C-model unexpected: %+v", s)
	}

	// 排序:胜率降序 → A/B(1.0) 在前
	if stats[0].WinRate != 1.0 || stats[len(stats)-1].WinRate != 0 {
		t.Fatalf("expected sorted by win rate desc: %+v", stats)
	}

	// 第二局:反方胜 → C/D 各 +1 胜,A/B 胜率降至 0.5
	res2 := &DebateResult{
		WinnerTeamID: 1,
		BestDebater:  BestDebaterInfo{Seat: 0, TeamID: 1},
		TeamScores: []TeamFinalScore{
			{TeamID: 0, TotalScore: 30.0},
			{TeamID: 1, TotalScore: 44.0},
		},
	}
	m.RecordGameResult(r, res2)
	byKey2 := map[string]ModelStats{}
	for _, s := range m.Stats() {
		byKey2[s.ModelKey] = s
	}
	if s := byKey2["A-model"]; s.TotalGames != 2 || s.WinCount != 1 || s.WinRate != 0.5 {
		t.Fatalf("A-model after game2 unexpected: %+v", s)
	}
	// 场均 = (42+30)/2 = 36
	if s := byKey2["A-model"]; s.AvgTotalScore != 36.0 {
		t.Fatalf("A-model avg score expected 36, got %v", s.AvgTotalScore)
	}
	// game2 最佳辩手 = 1 队 seat 0 = C-model(一辩)
	if s := byKey2["C-model"]; s.BestDebaterCount != 1 {
		t.Fatalf("C-model best debater expected 1: %+v", s)
	}
}

func TestStatsAbnormalResultSkipsWinner(t *testing.T) {
	m := NewDebateManager()
	r := newTestRoomWithTeams(t)

	res := &DebateResult{
		WinnerTeamID: 0,
		IsAbnormal:   true, // 全 fallback → 不计胜负
		TeamScores: []TeamFinalScore{
			{TeamID: 0, TotalScore: 25.0},
			{TeamID: 1, TotalScore: 25.0},
		},
	}
	m.RecordGameResult(r, res)
	for _, s := range m.Stats() {
		if s.WinCount != 0 || s.TotalGames != 1 {
			t.Fatalf("abnormal result should not count wins: %+v", s)
		}
	}
}
