// room_judge_test.go — 2026-07-16 主持人重构单测
//
// 验证 docs/狼人杀-重构方案/主持人Agent重构设计.md §6.3 映射表:judgeKindForPhase 把对局 phase 映射
// 为法官唤醒事件 kind,秘密阶段(NightWolves/Seer/Witch)返回空字符串 → 法官夜间静默。
package werewolf

import "testing"

// TestJudgeKindForPhase_PerPhase 验证 11 类公开 phase 各映射到对应 kind。
func TestJudgeKindForPhase_PerPhase(t *testing.T) {
	cases := []struct {
		phase Phase
		want  string
	}{
		{PhaseFilling, "judge_filling_welcome"},
		{PhasePreWolves, "judge_pre_wolves"},
		{PhaseDawn, "judge_dawn_announce"},
		{PhaseSheriff, "judge_sheriff_start"},
		{PhaseSpeak, "judge_speak_start"},
		{PhaseVote, "judge_vote_start"},
		{PhaseIdiotReveal, "judge_idiot_reveal"},
		{PhaseHunterShoot, "judge_hunter_shoot"},
		{PhaseDeathLyric, "judge_last_words"},
		{PhaseRestartVote, "judge_restart_vote_result"},
		{PhaseGameOver, "judge_game_over"},
	}
	for _, c := range cases {
		if got := judgeKindForPhase(c.phase); got != c.want {
			t.Errorf("judgeKindForPhase(%v) = %q, want %q", c.phase, got, c.want)
		}
	}
}

// TestJudgeKindForPhase_SilentAtNight 验证缺陷 🟡3 的夜间静默约束:
// 秘密阶段 NightWolves / NightSeer / NightWitch 返回空字符串 → 不调 wake,
// 法官在夜间静默观察不发言。
func TestJudgeKindForPhase_SilentAtNight(t *testing.T) {
	silent := []Phase{PhaseNightWolves, PhaseNightSeer, PhaseNightWitch}
	for _, p := range silent {
		if got := judgeKindForPhase(p); got != "" {
			t.Errorf("judgeKindForPhase(%v) = %q, want empty (night silence)", p, got)
		}
	}
}

// TestJudgeKindForPhase_UnknownPhase 验证未列出 phase(哨兵/越界)返回空。
func TestJudgeKindForPhase_UnknownPhase(t *testing.T) {
	if got := judgeKindForPhase(Phase(-1)); got != "" {
		t.Errorf("judgeKindForPhase(Phase(-1)) = %q, want empty", got)
	}
}
