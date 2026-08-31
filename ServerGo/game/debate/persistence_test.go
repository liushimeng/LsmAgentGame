// Package debate — persistence_test.go(2026-08-31 §20260831-08)。
//
// 覆盖落库的纯逻辑部分(无 DB 依赖,包内白盒):
//   - computeStatsDeltas:单局 → 模型统计增量(胜负/最佳辩手/异常局跳过)
//   - statsStore.applyDeltas/exportDeltas:UPSERT 累加语义(两局累加 = 增量之和,
//     与 DB 侧 INSERT ... ON DUPLICATE KEY UPDATE col = col + ? 的累加结果一致)
//   - buildDebateRoomRecord:房间记录行(JSON 列可还原 / 无结果时 -1 + "null")
//   - buildSpeechRow / buildScoreRows:发言与评审行(ID 兜底 / 确定性 ID)
//   - AttachPersistence(gormDB=nil)不接线、不 panic
//   - chainSpeechHook 链式包装:先同步广播、后异步落库
//
// GORM 语句本身(OnConflict UPSERT SQL)依赖真实 MySQL,由集成环境验证;
// 本包测试基建无 sqlite/sqlmock(见 go.mod),按 debate 包现有测试风格仅测纯逻辑。
package debate

import (
	"encoding/json"
	"testing"
	"time"
)

// newPersistenceTestConfig 构造一份合法 RoomConfig(2 队 × 2 人 + 3 裁判)。
//
// 故意让 m1 在两队各占 1 座((0,0) 与 (1,1)),验证「同局同模型多座位分别计」。
func newPersistenceTestConfig() RoomConfig {
	return RoomConfig{
		Topic:           DebateTopic{ID: "classic_001", Text: "人性本善", Type: "classic"},
		Mode:            ModeTwoTeam,
		PhaseConfig:     DefaultPhaseConfig(),
		SpectatorConfig: DefaultSpectatorConfig(),
		Teams: []TeamConfig{
			{TeamID: 0, Stance: StancePro, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m1"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m2"},
			}},
			{TeamID: 1, Stance: StanceCon, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m3"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m1"},
			}},
		},
		Judges: []JudgeConfig{
			{JudgeID: 0, ModelKey: "mj0"},
			{JudgeID: 1, ModelKey: "mj1"},
			{JudgeID: 2, ModelKey: "mj2"},
		},
		CreatedBy: "user_persist",
		CreatedAt: 1700000001,
	}
}

// newPersistenceTestResult 构造一份评审结果:0 队胜,最佳辩手 = 0 队 1 号座。
func newPersistenceTestResult(abnormal bool) *DebateResult {
	return &DebateResult{
		WinnerTeamID: 0,
		BestDebater:  BestDebaterInfo{TeamID: 0, Seat: 1},
		TeamScores: []TeamFinalScore{
			{TeamID: 0, TotalScore: 40},
			{TeamID: 1, TotalScore: 30},
		},
		IsAbnormal: abnormal,
	}
}

func TestComputeStatsDeltas_WinBestAndScore(t *testing.T) {
	room := NewDebateRoom("debate_p1", newPersistenceTestConfig(), nil)
	deltas := computeStatsDeltas(room, newPersistenceTestResult(false))
	if len(deltas) != 3 {
		t.Fatalf("expected 3 model keys, got %d: %v", len(deltas), deltas)
	}

	// m1 两队各 1 座((0,0) 与 (1,1)):参赛 2 次,胜 1 次(仅 0 队那次),
	// 最佳辩手 0 次(最佳辩手是 (0,1) = m2),分数和 = 40 + 30。
	m1 := deltas["m1"]
	if m1.TotalGames != 2 || m1.WinCount != 1 || m1.BestDebaterCount != 0 || m1.ScoreSum != 70 {
		t.Errorf("m1 delta = %+v, want {2 1 0 70}", m1)
	}
	// m2 = 0 队 1 号座:胜 + 最佳辩手都命中。
	m2 := deltas["m2"]
	if m2.TotalGames != 1 || m2.WinCount != 1 || m2.BestDebaterCount != 1 || m2.ScoreSum != 40 {
		t.Errorf("m2 delta = %+v, want {1 1 1 40}", m2)
	}
	m3 := deltas["m3"]
	if m3.TotalGames != 1 || m3.WinCount != 0 || m3.BestDebaterCount != 0 || m3.ScoreSum != 30 {
		t.Errorf("m3 delta = %+v, want {1 0 0 30}", m3)
	}
}

func TestComputeStatsDeltas_AbnormalSkipsWinAndBest(t *testing.T) {
	room := NewDebateRoom("debate_p1", newPersistenceTestConfig(), nil)
	deltas := computeStatsDeltas(room, newPersistenceTestResult(true))
	for key, d := range deltas {
		if d.WinCount != 0 || d.BestDebaterCount != 0 {
			t.Errorf("abnormal game: %s should have no win/best, got %+v", key, d)
		}
		if d.TotalGames == 0 {
			t.Errorf("abnormal game: %s participation must still count, got %+v", key, d)
		}
	}
	// 裁判模型绝不参与统计。
	if _, ok := deltas["mj0"]; ok {
		t.Error("judge model mj0 must not appear in stats deltas")
	}
}

func TestComputeStatsDeltas_NilGuards(t *testing.T) {
	if computeStatsDeltas(nil, &DebateResult{}) != nil {
		t.Error("nil room should yield nil deltas")
	}
	if computeStatsDeltas(&DebateRoom{}, nil) != nil {
		t.Error("nil result should yield nil deltas")
	}
}

// TestStatsStoreApplyExportUpsertAccumulation 验证 UPSERT 累加语义的纯逻辑:
// DB 侧两局 INSERT ... ON DUPLICATE KEY UPDATE col = col + delta 的最终行,
// 等价于进程内对两份 delta 依次 applyDeltas 后的 exportDeltas。
func TestStatsStoreApplyExportUpsertAccumulation(t *testing.T) {
	room := NewDebateRoom("debate_p1", newPersistenceTestConfig(), nil)
	res := newPersistenceTestResult(false)

	store := newStatsStore()
	store.applyDeltas(computeStatsDeltas(room, res)) // 第 1 局 upsert
	store.applyDeltas(computeStatsDeltas(room, res)) // 第 2 局 upsert

	exported := store.exportDeltas()
	m1 := exported["m1"]
	if m1.TotalGames != 4 || m1.WinCount != 2 || m1.BestDebaterCount != 0 || m1.ScoreSum != 140 {
		t.Errorf("m1 after two games = %+v, want {4 2 0 140}", m1)
	}
	m2 := exported["m2"]
	if m2.TotalGames != 2 || m2.WinCount != 2 || m2.BestDebaterCount != 2 || m2.ScoreSum != 80 {
		t.Errorf("m2 after two games = %+v, want {2 2 2 80}", m2)
	}

	// 快照派生值(场均 / 胜率)也应同步正确。
	var snapM1 *ModelStats
	for _, ms := range store.snapshot() {
		if ms.ModelKey == "m1" {
			s := ms
			snapM1 = &s
		}
	}
	if snapM1 == nil {
		t.Fatal("m1 missing from snapshot")
	}
	if snapM1.WinRate != 0.5 {
		t.Errorf("m1 win rate = %v, want 0.5", snapM1.WinRate)
	}
	if snapM1.AvgTotalScore != 35 { // 140 / 4
		t.Errorf("m1 avg total score = %v, want 35", snapM1.AvgTotalScore)
	}
}

func TestBuildDebateRoomRecord_WithResult(t *testing.T) {
	room := NewDebateRoom("debate_p9", newPersistenceTestConfig(), nil)
	room.MarkStarted()

	rec, err := buildDebateRoomRecord("debate_p9", room, newPersistenceTestResult(false), 1700000000)
	if err != nil {
		t.Fatalf("buildDebateRoomRecord: %v", err)
	}
	if rec.ID != "debate_p9" || rec.TopicID != "classic_001" || rec.Mode != "two_team" {
		t.Errorf("bad header fields: %+v", rec)
	}
	if rec.Status == "" || rec.CurrentPhase == "" {
		t.Errorf("status/current_phase must be set, got %q/%q", rec.Status, rec.CurrentPhase)
	}
	if rec.StartedAt == 0 || rec.CreatedAt == 0 || rec.FinishedAt == 0 {
		t.Errorf("timestamps must be non-zero: %+v", rec)
	}
	if rec.WinnerTeamID != 0 || rec.BestDebaterSeat != 1 || rec.BestDebaterTeamID != 0 {
		t.Errorf("winner fields = %d/%d/%d, want 0/1/0",
			rec.WinnerTeamID, rec.BestDebaterSeat, rec.BestDebaterTeamID)
	}

	// JSON 列必须能还原。
	var teams []TeamConfig
	if err := json.Unmarshal([]byte(rec.TeamConfig), &teams); err != nil || len(teams) != 2 {
		t.Errorf("team_config not restorable: err=%v len=%d", err, len(teams))
	}
	var judges []JudgeConfig
	if err := json.Unmarshal([]byte(rec.JudgeConfig), &judges); err != nil || len(judges) != 3 {
		t.Errorf("judge_config not restorable: err=%v len=%d", err, len(judges))
	}
	var res DebateResult
	if err := json.Unmarshal([]byte(rec.Result), &res); err != nil || res.WinnerTeamID != 0 {
		t.Errorf("result not restorable: err=%v winner=%d", err, res.WinnerTeamID)
	}
}

func TestBuildDebateRoomRecord_WithoutResult(t *testing.T) {
	room := NewDebateRoom("debate_px", newPersistenceTestConfig(), nil)
	rec, err := buildDebateRoomRecord("debate_px", room, nil, 1700000000)
	if err != nil {
		t.Fatalf("buildDebateRoomRecord: %v", err)
	}
	if rec.WinnerTeamID != -1 || rec.BestDebaterSeat != -1 || rec.BestDebaterTeamID != -1 {
		t.Errorf("no-result record should use -1 sentinels, got %+v", rec)
	}
	if rec.Result != "null" {
		t.Errorf("result should be literal null, got %q", rec.Result)
	}
	if rec.FinishedAt != 1700000000 {
		t.Errorf("finished_at should fall back to now, got %d", rec.FinishedAt)
	}
}

func TestBuildSpeechRow(t *testing.T) {
	sp := Speech{
		ID: "sp_1", Phase: PhaseOpeningArgument, TeamID: 0, Seat: 1,
		SpeakerName: "正方二辩", Stance: StancePro, Role: RoleSecond,
		Content: "我方认为", WordCount: 5, DurationSec: 12, Timestamp: 1700000000123,
		References: []string{"对方一辩发言"}, InternalThought: "思考", ModelKey: "m2",
	}
	row := buildSpeechRow("debate_p1", sp)
	if row.ID != "sp_1" || row.RoomID != "debate_p1" || row.ModelKey != "m2" {
		t.Errorf("bad row: %+v", row)
	}
	if row.DurationMs != 12000 {
		t.Errorf("duration_ms = %d, want 12000", row.DurationMs)
	}
	if row.CreatedAt != 1700000000123 {
		t.Errorf("created_at should mirror speech timestamp(ms), got %d", row.CreatedAt)
	}
	var refs []string
	if err := json.Unmarshal([]byte(row.References), &refs); err != nil || len(refs) != 1 {
		t.Errorf("references not JSON array: err=%v raw=%q", err, row.References)
	}

	// ID 兜底路径。
	sp2 := Speech{Timestamp: 1700000099999}
	row2 := buildSpeechRow("debate_p2", sp2)
	if row2.ID != "debate_p2:s1700000099999" {
		t.Errorf("fallback id = %q", row2.ID)
	}
}

func TestBuildScoreRows(t *testing.T) {
	sc := JudgeScore{
		JudgeID: 2, ModelKey: "mj2", WinnerTeamID: 1, OverallComment: "整体", IsFallback: true,
		Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{ArgumentQuality: 8, LogicRigor: 7,
				LanguageExpression: 6, TeamCoordination: 5, RebuttalEffectiveness: 4},
				TotalScore: 30, Comment: "c0", BestDebater: 1},
			{TeamID: 1, Scores: ScoreDimensions{ArgumentQuality: 9, LogicRigor: 8,
				LanguageExpression: 7, TeamCoordination: 6, RebuttalEffectiveness: 5},
				TotalScore: 35, Comment: "c1", BestDebater: 0},
		},
	}
	rows := buildScoreRows("debate_p3", sc, 1700000000111)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (judge × team), got %d", len(rows))
	}
	if rows[0].ID != "debate_p3:j2:t0" || rows[1].ID != "debate_p3:j2:t1" {
		t.Errorf("deterministic ids = %q / %q", rows[0].ID, rows[1].ID)
	}
	if rows[0].JudgeModelKey != "mj2" || rows[0].WinnerTeamID != 1 || !rows[0].IsFallback {
		t.Errorf("judge-level fields wrong: %+v", rows[0])
	}
	if rows[0].ArgumentQuality != 8 || rows[0].RebuttalEffectiveness != 4 ||
		rows[0].TotalScore != 30 || rows[0].BestDebaterSeat != 1 {
		t.Errorf("ranking fields wrong: %+v", rows[0])
	}
	if rows[0].CreatedAt != 1700000000111 {
		t.Errorf("created_at(ms) = %d", rows[0].CreatedAt)
	}
}

func TestAttachPersistenceNilDBIsNoop(t *testing.T) {
	mgr := NewDebateManager()
	called := false
	mgr.SetOnSpeech(func(roomID string, sp Speech) { called = true })

	if err := AttachPersistence(mgr, nil); err != nil {
		t.Fatalf("nil gormDB must be a silent no-op, got %v", err)
	}

	room, e := mgr.CreateRoom(newPersistenceTestConfig())
	if e != nil {
		t.Fatalf("CreateRoom: %v", e.Message)
	}
	room.emitSpeech(Speech{ID: "sp_x"})
	if !called {
		t.Error("original broadcast hook must stay wired after nil-db attach")
	}
}

func TestChainSpeechHookBroadcastsThenPersistsAsync(t *testing.T) {
	mgr := NewDebateManager()
	order := make(chan string, 4)
	mgr.SetOnSpeech(func(roomID string, sp Speech) { order <- "broadcast:" + roomID })

	persisted := make(chan string, 1)
	chainSpeechHook(mgr, func(roomID string, sp Speech) {
		persisted <- roomID + ":" + sp.ID
	})

	room, e := mgr.CreateRoom(newPersistenceTestConfig())
	if e != nil {
		t.Fatalf("CreateRoom: %v", e.Message)
	}
	room.emitSpeech(Speech{ID: "sp_y"})

	// 同步部分:广播必须立即完成。
	select {
	case ev := <-order:
		if ev != "broadcast:"+room.RoomID {
			t.Errorf("broadcast event = %q", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("broadcast hook not invoked synchronously")
	}
	// 异步部分:落库在 goroutine 中执行。
	select {
	case got := <-persisted:
		if got != room.RoomID+":sp_y" {
			t.Errorf("persist event = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("persist hook not invoked asynchronously")
	}
}

func TestRunSafeRecoversPanic(t *testing.T) {
	done := make(chan struct{})
	go runSafe("test", func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
		// panic 被 recover,goroutine 正常收尾。
	case <-time.After(time.Second):
		t.Fatal("runSafe did not finish after panic")
	}
}
