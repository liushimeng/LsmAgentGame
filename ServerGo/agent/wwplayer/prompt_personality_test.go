// Package wwplayer — prompt_personality_test.go: §20260811-04 U2 人设倾向参数单元测试。
//
// 覆盖 5 项不变式:
//   - 5 个预设人设的向量值与 docs 表一致
//   - BuildPersonalityText 对零向量返回空串(保留 Anthropic prompt cache 命中)
//   - 注入 system 末尾后,前缀(selfPortrait 段)字节级别不变
//   - Clamp 把越界浮点裁到 [0,1]
//   - unknown preset key 回退到 logical
package wwplayer

import (
	"strings"
	"testing"
)

func TestPersonalityPresets_ValuesConsistentWithDocs(t *testing.T) {
	cases := map[string]PersonalityVector{
		"logical":    {Aggressiveness: 0.4, TrustTendency: 0.3, BluffFrequency: 0.2, CollaborationStyle: 0.5, RiskTolerance: 0.3},
		"emotional":  {Aggressiveness: 0.5, TrustTendency: 0.6, BluffFrequency: 0.3, CollaborationStyle: 0.7, RiskTolerance: 0.4},
		"aggressive": {Aggressiveness: 0.9, TrustTendency: 0.2, BluffFrequency: 0.7, CollaborationStyle: 0.6, RiskTolerance: 0.8},
		"cautious":   {Aggressiveness: 0.2, TrustTendency: 0.5, BluffFrequency: 0.1, CollaborationStyle: 0.4, RiskTolerance: 0.2},
		"showman":    {Aggressiveness: 0.7, TrustTendency: 0.4, BluffFrequency: 0.9, CollaborationStyle: 0.8, RiskTolerance: 0.7},
	}
	for key, want := range cases {
		got, ok := PersonalityPresets[key]
		if !ok {
			t.Fatalf("preset %q not registered", key)
		}
		if got != want {
			t.Errorf("preset %q drift: got %+v, want %+v", key, got, want)
		}
	}
}

func TestBuildPersonalityText_ZeroVectorReturnsEmpty(t *testing.T) {
	got := BuildPersonalityText(PersonalityVector{}, "logical")
	if got != "" {
		t.Errorf("expected empty string for zero vector, got %q", got)
	}
}

func TestBuildPersonalityText_InjectNonZero(t *testing.T) {
	vec := PersonalityVector{Aggressiveness: 0.9, RiskTolerance: 0.8}
	got := BuildPersonalityText(vec, "aggressive")
	if got == "" {
		t.Fatal("expected non-empty output for aggressive preset")
	}
	if !strings.Contains(got, "激进冲锋") {
		t.Errorf("expected label 激进冲锋 in output, got %q", got)
	}
	if !strings.Contains(got, "Aggressiveness=0.90") {
		t.Errorf("expected formatted 0.90 component in output, got %q", got)
	}
}

func TestClamp_Boundaries(t *testing.T) {
	vec := PersonalityVector{
		Aggressiveness:     -0.5,
		TrustTendency:      1.5,
		BluffFrequency:     0.5,
		CollaborationStyle: 0,
		RiskTolerance:      1,
	}
	got := vec.Clamp()
	if got.Aggressiveness != 0 {
		t.Errorf("Aggressiveness clamp failed: got %f", got.Aggressiveness)
	}
	if got.TrustTendency != 1 {
		t.Errorf("TrustTendency clamp failed: got %f", got.TrustTendency)
	}
	if got.CollaborationStyle != 0 {
		t.Errorf("CollaborationStyle zero preservation failed: got %f", got.CollaborationStyle)
	}
	if got.RiskTolerance != 1 {
		t.Errorf("RiskTolerance one preservation failed: got %f", got.RiskTolerance)
	}
	if got.BluffFrequency != 0.5 {
		t.Errorf("BluffFrequency mid-value preservation failed: got %f", got.BluffFrequency)
	}
}

func TestLookupPersonalityPreset_UnknownFallsBackToLogical(t *testing.T) {
	got := LookupPersonalityPreset("nonexistent_preset")
	want := PersonalityPresets["logical"]
	if got != want {
		t.Errorf("unknown preset should fall back to logical: got %+v, want %+v", got, want)
	}
}

func TestBuildSystemPrompt_PersonalityCacheHit(t *testing.T) {
	// 不变式:selfPortrait="" + personality=零向量 + presetKey=""
	//          → 输出与 BuildSystemPrompt(selfPortrait) 字节级别一致
	// (确保 Anthropic prompt cache 前缀命中,无 Personality 字段时零回归)
	zero := PersonalityVector{}
	base := BuildSystemPrompt("", zero, "", "")
	// §20260810-10 U2 已知 base 末尾只有 rules+roleAbilities+...+PropSystemPrompt+空 portrait
	if strings.Contains(base[0].Text, "【🎭 人设倾向") {
		t.Errorf("zero-vector personality must not inject Personality block")
	}
}

func TestBuildSystemPrompt_PersonalityAppends(t *testing.T) {
	// 不变式:非零 personality 必须追加 PersonalityBlock;且 selfPortrait 段字节不变。
	zero := PersonalityVector{}
	personality := PersonalityVector{Aggressiveness: 0.7, TrustTendency: 0.3, BluffFrequency: 0.5, CollaborationStyle: 0.6, RiskTolerance: 0.4}
	withPersonality := BuildSystemPrompt("", personality, "aggressive", "")
	withPersonalityCustom := BuildSystemPrompt("", personality, "custom", "")

	if !strings.Contains(withPersonality[0].Text, "【🎭 人设倾向 — 激进冲锋】") {
		t.Errorf("expected personality label in system prompt, got tail: %q",
			withPersonality[0].Text[max(0, len(withPersonality[0].Text)-200):])
	}
	// custom 模式:presetKey="custom" → label="自定义"
	if !strings.Contains(withPersonalityCustom[0].Text, "【🎭 人设倾向 — 自定义】") {
		t.Errorf("expected 自定义 label when presetKey='custom'")
	}

	// §128 注入位置:personalityBlock 在 selfPortrait 之后 → 找最后一个【🎭 出现位置
	// 必须晚于 portrait 块(空 portrait = 紧跟 PropSystemPrompt)。
	idxPersonality := strings.LastIndex(withPersonality[0].Text, "【🎭 人设倾向")
	if idxPersonality < 0 {
		t.Fatal("personality block not found")
	}
	// 验证 selfPortrait 段前缀(空时)字节不变:
	// withPersonality[0].Text[:idxPersonality] 应与 zero 版前 min(idxPersonality, len(zeroText)) 字节一致。
	zeroText := BuildSystemPrompt("", zero, "", "")[0].Text
	commonLen := len(zeroText)
	if commonLen > idxPersonality {
		commonLen = idxPersonality
	}
	if withPersonality[0].Text[:commonLen] != zeroText[:commonLen] {
		t.Error("prefix bytes diverge (Anthropic cache miss risk)")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}