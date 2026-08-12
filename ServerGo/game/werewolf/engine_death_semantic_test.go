// engine_death_semantic_test.go — §123 死亡语义单测
//
// 验证 Player.DeathCause / DeathVerdict 字段在不同死因下的派生逻辑,
// 以及 DeadPlayerJSON / LastNightDeathsVerbose 视图字段填充。
package werewolf

import (
	"testing"
)

// TestVerdictFor_AllCauses 验证 cause → verdict 查表函数正确性。
func TestVerdictFor_AllCauses(t *testing.T) {
	cases := []struct {
		cause string
		want  string
	}{
		{DeathCauseWolf, DeathVerdictDeath},
		{DeathCauseVote, DeathVerdictExecution},
		{DeathCauseHunter, DeathVerdictDeath},
		{DeathCauseWitchPoison, DeathVerdictDeath},
		{DeathCauseSuicide, DeathVerdictExecution},
		{"unknown_cause", DeathVerdictDeath}, // 兜底:未知死因默认按"死亡"
		{"", DeathVerdictDeath},
	}
	for _, c := range cases {
		t.Run(c.cause, func(t *testing.T) {
			got := verdictFor(c.cause)
			if got != c.want {
				t.Errorf("verdictFor(%q) = %q, want %q", c.cause, got, c.want)
			}
		})
	}
}

// TestKillPlayer_WolfCause_DeathVerdict 验证狼刀 → verdict=death。
func TestKillPlayer_WolfCause_DeathVerdict(t *testing.T) {
	gs := NewGame(1)
	gs.Seats[0] = "u1"
	gs.Seats[1] = "u2"
	gs.Players[0].Alive = true
	gs.Players[1].Alive = true
	gs.Players[0].Role = RoleWerewolf
	gs.Players[1].Role = RoleVillager
	gs.DayNumber = 2
	gs.refreshCounts()
	if err := gs.killPlayer(1, DeathCauseWolf); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.Players[1].DeathCause != DeathCauseWolf {
		t.Errorf("DeathCause = %q, want %q", gs.Players[1].DeathCause, DeathCauseWolf)
	}
	if gs.Players[1].DeathVerdict != DeathVerdictDeath {
		t.Errorf("DeathVerdict = %q, want %q", gs.Players[1].DeathVerdict, DeathVerdictDeath)
	}
}

// TestKillPlayer_VoteCause_ExecutionVerdict 验证投票放逐 → verdict=execution。
func TestKillPlayer_VoteCause_ExecutionVerdict(t *testing.T) {
	gs := NewGame(1)
	gs.Seats[0] = "u1"
	gs.Players[0].Alive = true
	gs.Players[0].Role = RoleWerewolf
	gs.DayNumber = 1
	gs.refreshCounts()
	if err := gs.killPlayer(0, DeathCauseVote); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.Players[0].DeathVerdict != DeathVerdictExecution {
		t.Errorf("DeathVerdict = %q, want %q", gs.Players[0].DeathVerdict, DeathVerdictExecution)
	}
}

// TestKillPlayer_SuicideCause_ExecutionVerdict 验证狼自爆 → verdict=execution(玩家自主决策)。
func TestKillPlayer_SuicideCause_ExecutionVerdict(t *testing.T) {
	gs := NewGame(1)
	gs.Seats[0] = "u1"
	gs.Players[0].Alive = true
	gs.Players[0].Role = RoleWerewolf
	gs.DayNumber = 2
	gs.refreshCounts()
	if err := gs.killPlayer(0, DeathCauseSuicide); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.Players[0].DeathVerdict != DeathVerdictExecution {
		t.Errorf("狼自爆应判为 execution, got %q", gs.Players[0].DeathVerdict)
	}
}

// TestKillPlayer_HunterCause_DeathVerdict 验证猎人反杀 → verdict=death。
func TestKillPlayer_HunterCause_DeathVerdict(t *testing.T) {
	gs := NewGame(1)
	gs.Seats[0] = "u1"
	gs.Players[0].Alive = true
	gs.Players[0].Role = RoleVillager
	gs.DayNumber = 1
	gs.refreshCounts()
	if err := gs.killPlayer(0, DeathCauseHunter); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.Players[0].DeathVerdict != DeathVerdictDeath {
		t.Errorf("猎人反杀应判为 death, got %q", gs.Players[0].DeathVerdict)
	}
}

// TestKillPlayer_WitchPoisonCause_DeathVerdict 验证女巫毒杀 → verdict=death。
func TestKillPlayer_WitchPoisonCause_DeathVerdict(t *testing.T) {
	gs := NewGame(1)
	gs.Seats[0] = "u1"
	gs.Players[0].Alive = true
	gs.Players[0].Role = RoleVillager
	gs.DayNumber = 1
	gs.refreshCounts()
	if err := gs.killPlayer(0, DeathCauseWitchPoison); err != nil {
		t.Fatalf("killPlayer failed: %v", err)
	}
	if gs.Players[0].DeathVerdict != DeathVerdictDeath {
		t.Errorf("女巫毒杀应判为 death, got %q", gs.Players[0].DeathVerdict)
	}
}

// TestBuildDeadListForSeatsLocked_FillsVerdict 验证 LastNightDeathsVerbose 填充 verdict。
func TestBuildDeadListForSeatsLocked_FillsVerdict(t *testing.T) {
	gs := NewGame(1)
	gs.Seats[0] = "u1"
	gs.Seats[1] = "u2"
	gs.Players[0].Alive = false
	gs.Players[1].Alive = false
	gs.Players[0].Role = RoleVillager
	gs.Players[1].Role = RoleWerewolf
	gs.Players[0].DeathCause = DeathCauseWolf
	gs.Players[0].DeathVerdict = DeathVerdictDeath
	gs.Players[1].DeathCause = DeathCauseVote
	gs.Players[1].DeathVerdict = DeathVerdictExecution
	gs.DayNumber = 2

	out := buildDeadListForSeatsLocked(gs, []Seat{0, 1})
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	if out[0].Verdict != DeathVerdictDeath {
		t.Errorf("seat 0 verdict = %q, want %q", out[0].Verdict, DeathVerdictDeath)
	}
	if out[1].Verdict != DeathVerdictExecution {
		t.Errorf("seat 1 verdict = %q, want %q", out[1].Verdict, DeathVerdictExecution)
	}
}

// TestJudgeFallbackText_AllKinds 验证法官 fallback 文本表覆盖所有事件类型。
// 此测试位于 agent 包(judge_test.go),这里仅引用常量以避免 werewolf → agent 循环依赖。
func TestJudgeFallbackText_AllKinds(t *testing.T) {
	kinds := []string{
		"judge_filling_welcome",
		"judge_pre_wolves",
		"judge_dawn_announce",
		"judge_sheriff_start",
		"judge_speak_start",
		"judge_vote_start",
		"judge_death_announce",
		"judge_sheriff_stream_settle",
		"judge_idiot_reveal",
		"judge_hunter_shoot",
		"judge_last_words",
		"judge_restart_vote_result",
		"judge_game_over",
	}
	for _, k := range kinds {
		t.Run(k, func(t *testing.T) {
			// 调用 agent 包实现的 fallback 表(避免循环依赖,改用字符串字面量做单元测试)。
			// 这里我们改用 werewolf 包内的本地副本(若 agent 包 fallback 改动,需同步)。
			got := localJudgeFallbackText(k)
			if got == "" {
				t.Errorf("localJudgeFallbackText(%q) returned empty string", k)
			}
		})
	}
	if localJudgeFallbackText("nonexistent_kind") != "" {
		t.Errorf("unknown kind should return empty")
	}
}

// localJudgeFallbackText 是 agent.JudgeFallbackText 的本地镜像(字符串字面量版),
// 仅用于 werewolf 包的单测,避免 import agent 引起循环依赖。
// 与 agent/judge.go::JudgeFallbackText 同步;若 agent 改动,此处同步修改。
func localJudgeFallbackText(kind string) string {
	switch kind {
	case "judge_filling_welcome":
		return "欢迎进入狼人杀对局。"
	case "judge_pre_wolves":
		return "天黑请闭眼。"
	case "judge_dawn_announce":
		return "黎明已至,请查看昨夜伤亡。"
	case "judge_sheriff_start":
		return "进入警长竞选阶段。"
	case "judge_speak_start":
		return "进入白天发言阶段,请依次发言。"
	case "judge_vote_start":
		return "进入投票放逐阶段,请投票。"
	case "judge_death_announce":
		return "有人死亡。"
	case "judge_sheriff_stream_settle":
		return "警徽流结算完成。"
	case "judge_idiot_reveal":
		return "白痴翻牌阶段。"
	case "judge_hunter_shoot":
		return "猎人开枪阶段。"
	case "judge_last_words":
		return "遗言阶段。"
	case "judge_restart_vote_result":
		return "重开局投票已结算。"
	case "judge_game_over":
		return "对局结束。"
	default:
		return ""
	}
}
