package werewolf

import (
	"testing"
)

// §20260811-09 U2 — Agent 难度分级 ProfileFor / NormalizeAgentDifficulty 单元测试。
// 覆盖:合法值映射 / 未知值归一化 / Coin 倍率整数十倍 / SpeakLimiterScale / MaxToolUse
// 与「normal 档 PromptDirective 空串」纪律。

func TestDifficulty_ProfileFor_KnownValues(t *testing.T) {
	cases := []struct {
		name     string
		in       AgentDifficulty
		mult     int
		toolUse  int
		hyp      bool
		mem      bool
		scale    float64
		hasDir   bool
	}{
		{"easy", DifficultyEasy, 5, 3, false, false, 1.5, true},
		{"normal", DifficultyNormal, 10, 0, true, true, 1.0, false},
		{"hard", DifficultyHard, 15, 6, true, true, 1.0, true},
		{"hell", DifficultyHell, 20, 8, true, true, 0.8, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ProfileFor(tc.in)
			if p.CoinMultiplierX10 != tc.mult {
				t.Errorf("mult=%d, want %d", p.CoinMultiplierX10, tc.mult)
			}
			if p.MaxToolUse != tc.toolUse {
				t.Errorf("MaxToolUse=%d, want %d", p.MaxToolUse, tc.toolUse)
			}
			if p.InjectHypotheses != tc.hyp {
				t.Errorf("InjectHypotheses=%v, want %v", p.InjectHypotheses, tc.hyp)
			}
			if p.InjectLongMemory != tc.mem {
				t.Errorf("InjectLongMemory=%v, want %v", p.InjectLongMemory, tc.mem)
			}
			if p.SpeakLimiterScale != tc.scale {
				t.Errorf("SpeakLimiterScale=%v, want %v", p.SpeakLimiterScale, tc.scale)
			}
			hasDir := p.PromptDirective != ""
			if hasDir != tc.hasDir {
				t.Errorf("PromptDirective非空=%v, want %v", hasDir, tc.hasDir)
			}
		})
	}
}

func TestDifficulty_ProfileFor_UnknownFallsBackToNormal(t *testing.T) {
	p := ProfileFor(AgentDifficulty("unknown"))
	np := ProfileFor(DifficultyNormal)
	if p != np {
		t.Errorf("unknown 档应归一化为 normal, 两者 Profile 应相等\n got: %+v\nwant: %+v", p, np)
	}
}

func TestDifficulty_NormalizeAgentDifficulty(t *testing.T) {
	cases := map[string]AgentDifficulty{
		"":        DifficultyNormal, // 空串 = 默认 normal
		"easy":    DifficultyEasy,
		"normal":  DifficultyNormal,
		"hard":    DifficultyHard,
		"hell":    DifficultyHell,
		"unknown": DifficultyNormal, // 未知值兜底
		"AI":      DifficultyNormal, // 大小写敏感(对齐 §198 JudgeMode 同款)
		"agent":   DifficultyNormal, // 与法官枚举同名但语义不同,这里视作未知
	}
	for in, want := range cases {
		if got := NormalizeAgentDifficulty(in); got != want {
			t.Errorf("NormalizeAgentDifficulty(%q)=%v, want %v", in, got, want)
		}
	}
}

func TestDifficulty_AllDifficulties(t *testing.T) {
	all := AllDifficulties()
	if len(all) != 4 {
		t.Fatalf("AllDifficulties 应返回 4 档, got %d", len(all))
	}
	want := []AgentDifficulty{DifficultyEasy, DifficultyNormal, DifficultyHard, DifficultyHell}
	for i, d := range want {
		if all[i] != d {
			t.Errorf("index %d: got %v, want %v", i, all[i], d)
		}
	}
}

func TestDifficulty_SetAgentDifficultyLocked_NormalizesUnknown(t *testing.T) {
	r := &WerewolfRoom{}
	r.setAgentDifficultyLocked("garbage")
	if r.agentDifficulty != string(DifficultyNormal) {
		t.Errorf("setAgentDifficultyLocked(garbage) -> %q, want %q",
			r.agentDifficulty, DifficultyNormal)
	}
	r.setAgentDifficultyLocked("easy")
	if r.agentDifficulty != string(DifficultyEasy) {
		t.Errorf("setAgentDifficultyLocked(easy) -> %q, want %q",
			r.agentDifficulty, DifficultyEasy)
	}
	// 空串应走 normal(默认)。
	r.setAgentDifficultyLocked("")
	if r.agentDifficulty != string(DifficultyNormal) {
		t.Errorf("setAgentDifficultyLocked(\"\") -> %q, want %q",
			r.agentDifficulty, DifficultyNormal)
	}
}

// 结算倍率通过 DifficultyCoinMultiplierX10Locked 暴露。
// 必须 = ProfileFor.CoinMultiplierX10 —— 防止后续重构同步漂移(§130)。
func TestDifficulty_DifficultyCoinMultiplierX10Locked_MatchesProfile(t *testing.T) {
	for _, d := range AllDifficulties() {
		r := &WerewolfRoom{agentDifficulty: string(d)}
		got := r.DifficultyCoinMultiplierX10Locked()
		want := int64(ProfileFor(d).CoinMultiplierX10)
		if got != want {
			t.Errorf("difficulty=%v: DifficultyCoinMultiplierX10Locked=%d, ProfileFor=%d",
				d, got, want)
		}
	}
}