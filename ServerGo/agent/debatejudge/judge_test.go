// Package debatejudge — 裁判工具测试(2026-08-31 §20260831-02)。
//
// 覆盖:
//   - clampBestDebater:合法座位直通 / 1-based 换算 / 越界回退一辩 / 未知队伍
package debatejudge

import (
	"testing"

	"LsmAgentGame/game/debate"
	"LsmAgentGame/llm"
)

// newJudgeForTest 构造带 2 队(4 人 / 3 人)的裁判实例(无 provider,纯结构测试)。
func newJudgeForTest(t *testing.T) *AgentJudge {
	t.Helper()
	room := debate.NewDebateRoom("debate_test", debate.RoomConfig{
		Topic:       debate.DebateTopic{ID: "t", Text: "测试辩题", Type: "classic"},
		Mode:        debate.ModeTwoTeam,
		PhaseConfig: debate.QuickPhaseConfig(),
		Teams: []debate.TeamConfig{
			{TeamID: 0, Stance: debate.StancePro, Agents: []debate.AgentConfig{
				{SeatID: 0, Role: debate.RoleFirst, ModelKey: "m1"},
				{SeatID: 1, Role: debate.RoleSecond, ModelKey: "m2"},
				{SeatID: 2, Role: debate.RoleThird, ModelKey: "m3"},
				{SeatID: 3, Role: debate.RoleFourth, ModelKey: "m4"},
			}},
			{TeamID: 1, Stance: debate.StanceCon, Agents: []debate.AgentConfig{
				{SeatID: 0, Role: debate.RoleFirst, ModelKey: "m5"},
				{SeatID: 1, Role: debate.RoleSecond, ModelKey: "m6"},
				{SeatID: 2, Role: debate.RoleThird, ModelKey: "m7"},
			}},
		},
		Judges: []debate.JudgeConfig{{JudgeID: 0, ModelKey: "m8"}},
	}, nil)
	return NewJudge(room, nil, 0, "m8", &llm.Registry{})
}

func TestClampBestDebater(t *testing.T) {
	j := newJudgeForTest(t)

	cases := []struct {
		name              string
		teamID, seat, want int
	}{
		{"合法座位直通", 0, 2, 2},
		{"一 based 提交 4(四辩)", 0, 4, 3},
		{"一 based 提交 3(三辩, 3 人队)", 1, 3, 2},
		{"越界 9 回退一辩", 0, 9, 0},
		{"负数回退一辩", 0, -1, 0},
		{"未知队伍回退 0", 5, 2, 0},
	}
	for _, c := range cases {
		if got := j.clampBestDebater(c.teamID, c.seat); got != c.want {
			t.Errorf("%s: clampBestDebater(%d, %d) = %d, want %d",
				c.name, c.teamID, c.seat, got, c.want)
		}
	}
}
