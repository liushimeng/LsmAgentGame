// Package debate — 单元测试(2026-08-31 §20260831-01)。
//
// 覆盖:
//   - PhaseCN / StanceLabel 等枚举 → 字符串映射
//   - AllowedToolsForPhaseRole 工具过滤
//   - FairModelAssignment 公平分配(每队不重复 + 裁判独立)
//   - ValidateAssignment 校验合法性
//   - DefaultStancesForMode / DefaultRolesForTeamSize
//   - ComputeFinalScores 中位数聚合
//   - DetermineWinner 多数决
//   - TruncateRune / CountRune 字符级截断
package debate

import (
	"testing"
)

func TestPhaseCN(t *testing.T) {
	cases := map[Phase]string{
		PhaseFilling:          "等待开始",
		PhasePreparation:      "赛前准备",
		PhaseOpeningArgument:  "开篇立论",
		PhaseRebuttal:         "驳论",
		PhaseCrossExamination: "质询",
		PhaseCrossExamSummary: "质询小结",
		PhaseFreeDebate:       "自由辩论",
		PhaseClosingArgument:  "总结陈词",
		PhaseJudging:          "裁判评审",
		PhaseResult:           "公布结果",
		PhaseGameOver:         "对局结束",
	}
	for p, want := range cases {
		if got := PhaseCN(p); got != want {
			t.Errorf("PhaseCN(%s) = %q, want %q", p, got, want)
		}
	}
}

func TestIsValidPhase(t *testing.T) {
	if !IsValidPhase(PhaseOpeningArgument) {
		t.Error("PhaseOpeningArgument should be valid")
	}
	if IsValidPhase("unknown_phase") {
		t.Error("unknown_phase should be invalid")
	}
}

func TestAllowedToolsForPhaseRole(t *testing.T) {
	// 一辩在立论阶段应可用 speech
	tools := AllowedToolsForPhaseRole(PhaseOpeningArgument, RoleFirst)
	found := false
	for _, tn := range tools {
		if tn == ToolSpeech {
			found = true
		}
	}
	if !found {
		t.Error("一辩在立论阶段应可用 speech")
	}

	// 二辩在立论阶段不能 speech
	tools = AllowedToolsForPhaseRole(PhaseOpeningArgument, RoleSecond)
	for _, tn := range tools {
		if tn == ToolSpeech {
			t.Error("二辩在立论阶段不应有 speech")
		}
	}

	// 三辩在质询阶段应可用 cross_exam_question
	tools = AllowedToolsForPhaseRole(PhaseCrossExamination, RoleThird)
	found = false
	for _, tn := range tools {
		if tn == ToolCrossExamQuestion {
			found = true
		}
	}
	if !found {
		t.Error("三辩在质询阶段应有 cross_exam_question")
	}

	// 自由辩论阶段所有人都可用 free_debate_speak
	for _, role := range []Role{RoleFirst, RoleSecond, RoleThird, RoleFourth} {
		tools := AllowedToolsForPhaseRole(PhaseFreeDebate, role)
		found := false
		for _, tn := range tools {
			if tn == ToolFreeDebateSpeak {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 在自由辩论阶段应有 free_debate_speak", role)
		}
	}
}

func TestFairModelAssignment(t *testing.T) {
	// 11 个模型(8 默认 + 3 额外)→ 足够 2 队 × 4 人 + 3 个独立裁判
	models := []string{
		"MeiTuan-model", "DouBao-model", "DeepSeek-model", "GLM-model",
		"Kimi-model", "MinMax-model", "Qwen-model", "Xiaomi-model",
		"Anthropic-Sonnet-model", "GPT-4o-model", "Llama-3.1-model",
	}
	team, judges, err := FairModelAssignment(2, 4, 3, models, nil)
	if err != nil {
		t.Fatalf("FairModelAssignment failed: %v", err)
	}
	if len(team) != 2 {
		t.Errorf("expected 2 teams, got %d", len(team))
	}
	for tid, agents := range team {
		if len(agents) != 4 {
			t.Errorf("team %d expected 4 agents, got %d", tid, len(agents))
		}
		// 每队内不重复
		seen := map[string]bool{}
		for _, m := range agents {
			if seen[m] {
				t.Errorf("team %d has duplicate model %s", tid, m)
			}
			seen[m] = true
		}
	}
	if len(judges) != 3 {
		t.Errorf("expected 3 judges, got %d", len(judges))
	}
	// 裁判与辩方不重复
	if err := ValidateAssignment(team, judges); err != nil {
		t.Errorf("ValidateAssignment: %v", err)
	}
}

func TestFairModelAssignmentSmallPool(t *testing.T) {
	// 3 个模型不足以 2 队 × 4 人,应返回错误
	models := []string{"A", "B", "C"}
	_, _, err := FairModelAssignment(2, 4, 3, models, nil)
	if err == nil {
		t.Error("expected error for small model pool")
	}
}

func TestDefaultStancesForMode(t *testing.T) {
	cases := map[Mode][]Stance{
		ModeTwoTeam:   {StancePro, StanceCon},
		ModeThreeTeam: {StancePro, StanceCon, StanceNeutral},
		ModeFourTeam:  {StanceGovUpper, StanceGovLower, StanceOppUpper, StanceOppLower},
		ModeFiveTeam:  {StanceAngle1, StanceAngle2, StanceAngle3, StanceAngle4, StanceAngle5},
	}
	for mode, want := range cases {
		got := DefaultStancesForMode(mode)
		if len(got) != len(want) {
			t.Errorf("DefaultStancesForMode(%s): len=%d want %d", mode, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("DefaultStancesForMode(%s)[%d] = %s want %s", mode, i, got[i], want[i])
			}
		}
	}
}

func TestDefaultRolesForTeamSize(t *testing.T) {
	cases := map[int]int{
		2: 2, // 一辩 + 四辩
		3: 3, // 一辩 + 二辩 + 四辩
		4: 4, // 一/二/三/四辩
	}
	for n, want := range cases {
		roles := DefaultRolesForTeamSize(n)
		if len(roles) != want {
			t.Errorf("DefaultRolesForTeamSize(%d): len=%d want %d", n, len(roles), want)
		}
	}
}

func TestComputeFinalScores(t *testing.T) {
	judges := []JudgeScore{
		{JudgeID: 0, Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{ArgumentQuality: 8, LogicRigor: 8, LanguageExpression: 7, TeamCoordination: 8, RebuttalEffectiveness: 7}, TotalScore: 38, Comment: "good"},
			{TeamID: 1, Scores: ScoreDimensions{ArgumentQuality: 6, LogicRigor: 7, LanguageExpression: 7, TeamCoordination: 6, RebuttalEffectiveness: 7}, TotalScore: 33, Comment: "ok"},
		}, WinnerTeamID: 0},
		{JudgeID: 1, Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{ArgumentQuality: 7, LogicRigor: 8, LanguageExpression: 8, TeamCoordination: 7, RebuttalEffectiveness: 8}, TotalScore: 38},
			{TeamID: 1, Scores: ScoreDimensions{ArgumentQuality: 7, LogicRigor: 6, LanguageExpression: 7, TeamCoordination: 7, RebuttalEffectiveness: 6}, TotalScore: 33},
		}, WinnerTeamID: 0},
		{JudgeID: 2, Rankings: []TeamRanking{
			{TeamID: 0, Scores: ScoreDimensions{ArgumentQuality: 8, LogicRigor: 7, LanguageExpression: 8, TeamCoordination: 8, RebuttalEffectiveness: 7}, TotalScore: 38},
			{TeamID: 1, Scores: ScoreDimensions{ArgumentQuality: 6, LogicRigor: 6, LanguageExpression: 7, TeamCoordination: 7, RebuttalEffectiveness: 6}, TotalScore: 32},
		}, WinnerTeamID: 0},
	}

	scores := ComputeFinalScores(judges, 2)
	if len(scores) != 2 {
		t.Fatalf("expected 2 team scores, got %d", len(scores))
	}
	if scores[0].Rank != 1 {
		t.Errorf("team 0 rank = %d, want 1", scores[0].Rank)
	}
	if scores[1].Rank != 2 {
		t.Errorf("team 1 rank = %d, want 2", scores[1].Rank)
	}
}

func TestDetermineWinner(t *testing.T) {
	judges := []JudgeScore{
		{WinnerTeamID: 0},
		{WinnerTeamID: 0},
		{WinnerTeamID: 1},
	}
	winner, _ := DetermineWinner(judges)
	if winner != 0 {
		t.Errorf("DetermineWinner = %d, want 0", winner)
	}
}

func TestCountRune(t *testing.T) {
	cases := map[string]int{
		"hello": 5,
		"辩论":   2,
		"":     0,
	}
	for s, want := range cases {
		if got := CountRune(s); got != want {
			t.Errorf("CountRune(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestTruncateRune(t *testing.T) {
	s := "辩论比赛真精彩"
	// §20260831-06 修复:按 rune 计数截断(首期用例锁定了字节索引 bug,
	// max=4 时误期望 2 字;正确语义是保留前 4 个字符)。
	if got := TruncateRune(s, 4); got != "辩论比赛" {
		t.Errorf("TruncateRune(%q, 4) = %q, want %q", s, got, "辩论比赛")
	}
	if got := TruncateRune(s, 100); got != s {
		t.Errorf("TruncateRune(%q, 100) should be unchanged", s)
	}
	if got := TruncateRune(s, 0); got != "" {
		t.Errorf("TruncateRune(%q, 0) = %q, want empty", s, got)
	}
}

func TestStanceLabel(t *testing.T) {
	cases := map[Stance]string{
		StancePro:      "正方",
		StanceCon:      "反方",
		StanceNeutral:  "中立",
		StanceGovUpper: "政府上院",
	}
	for s, want := range cases {
		if got := StanceLabel(s); got != want {
			t.Errorf("StanceLabel(%s) = %q, want %q", s, got, want)
		}
	}
}

func TestFindTopicByID(t *testing.T) {
	topics := BuiltInTopics()
	if len(topics) < 30 {
		t.Errorf("expected at least 30 topics, got %d", len(topics))
	}
	// classic_001 必须存在
	if _, ok := FindTopicByID("classic_001"); !ok {
		t.Error("classic_001 should exist")
	}
	// 不存在的 ID 应返回 false
	if _, ok := FindTopicByID("nonexistent_999"); ok {
		t.Error("nonexistent_999 should not exist")
	}
}

func TestSeatKey(t *testing.T) {
	if k := SeatKey(0, 1); k != "0:1" {
		t.Errorf("SeatKey(0, 1) = %q, want %q", k, "0:1")
	}
	t1, s1, ok := ParseSeatKey("0:1")
	if !ok || t1 != 0 || s1 != 1 {
		t.Errorf("ParseSeatKey(\"0:1\") = (%d, %d, %v)", t1, s1, ok)
	}
}

func TestDefaultPhaseConfig(t *testing.T) {
	cfg := DefaultPhaseConfig()
	if cfg.OpeningArgumentSec == 0 {
		t.Error("DefaultPhaseConfig should set OpeningArgumentSec")
	}
	if cfg.MaxSpeechChars == 0 {
		t.Error("DefaultPhaseConfig should set MaxSpeechChars")
	}
}

func TestModelShort(t *testing.T) {
	cases := map[string]string{
		"MeiTuan-model":    "MeiTuan",
		"DeepSeek-V3-Pro": "DeepSeek-V3", // 取最后一个 "-" 前的部分
		"Qwen":             "Qwen",
	}
	for in, want := range cases {
		if got := ModelShort(in); got != want {
			t.Errorf("ModelShort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoleName(t *testing.T) {
	if RoleName(RoleFirst) != "一辩" {
		t.Errorf("RoleName(RoleFirst) = %q, want %q", RoleName(RoleFirst), "一辩")
	}
}

func TestSpectatorRegistry(t *testing.T) {
	sr := newSpectatorRegistry()
	if sr.Has("u1") {
		t.Error("empty registry should not have u1")
	}
	s := sr.Add(Spectator{UserID: "u1", Kind: SpectatorKindViewer})
	if s == nil || s.UserID != "u1" {
		t.Error("Add failed")
	}
	if !sr.Has("u1") {
		t.Error("after Add, Has should return true")
	}
	if sr.Count() != 1 {
		t.Errorf("Count = %d, want 1", sr.Count())
	}
	sr.Remove("u1")
	if sr.Has("u1") {
		t.Error("after Remove, Has should return false")
	}
}

func TestDebateManagerCreateRoom(t *testing.T) {
	mgr := NewDebateManager()
	cfg := RoomConfig{
		Topic: DebateTopic{ID: "classic_001", Text: "人性本善", Type: "classic"},
		Mode:  ModeTwoTeam,
		PhaseConfig: DefaultPhaseConfig(),
		SpectatorConfig: DefaultSpectatorConfig(),
		Teams: []TeamConfig{
			{TeamID: 0, Stance: StancePro, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m1"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m2"},
			}},
			{TeamID: 1, Stance: StanceCon, Agents: []AgentConfig{
				{SeatID: 0, Role: RoleFirst, ModelKey: "m3"},
				{SeatID: 1, Role: RoleSecond, ModelKey: "m4"},
			}},
		},
		Judges: []JudgeConfig{
			{JudgeID: 0, ModelKey: "m5"},
			{JudgeID: 1, ModelKey: "m6"},
			{JudgeID: 2, ModelKey: "m7"},
		},
		CreatedBy: "user1",
	}
	room, e := mgr.CreateRoom(cfg)
	if e != nil {
		t.Fatalf("CreateRoom failed: %v", e.Message)
	}
	if room.RoomID == "" {
		t.Error("room id should not be empty")
	}
	if len(mgr.List()) != 1 {
		t.Errorf("expected 1 room, got %d", len(mgr.List()))
	}
	// 校验能查询
	if _, ok := mgr.Get(room.RoomID); !ok {
		t.Error("Get should find the created room")
	}
}