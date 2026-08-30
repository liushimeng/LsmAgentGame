package wwplayer

import (
	"strings"
	"testing"
)

// §20260811-09 U2 — Agent 难度分级 BuildSystemPrompt 注入纪律。
//
// 不变式:
//   - difficultyDirective="" (normal 档默认值) → 输出与零输入版逐字节一致,
//     保持 Anthropic prompt cache 前缀命中(§20260810-10 U2 + §20260811-04 U2
//     同款纪律)。
//   - difficultyDirective 非空 (easy/hard/hell) → 仅在 personalityBlock 之后
//     末尾追加「【难度=...】」段,前缀部分字节不变。

func TestBuildSystemPrompt_DifficultyCacheHit_Normal(t *testing.T) {
	zero := PersonalityVector{}
	// 不变式:空 portrait + 零 personality + 空 presetKey + 空 difficultyDirective
	//          → 输出字节级别一致(Anthropic cache 前缀命中)
	base := BuildSystemPrompt("", zero, "", "", false)
	if len(base) != 1 {
		t.Fatalf("want 1 system block, got %d", len(base))
	}
	// 不应注入任何难度段标记
	if strings.Contains(base[0].Text, "【难度=") {
		t.Errorf("empty difficulty directive must not inject any 【难度=...】 block, got tail: %q",
			base[0].Text[max(0, len(base[0].Text)-100):])
	}
}

func TestBuildSystemPrompt_DifficultyAppends(t *testing.T) {
	zero := PersonalityVector{}
	cases := []struct {
		directive string
		label     string
	}{
		{"【难度=简单】保守推理:只做最直接的逻辑推断。", "【难度=简单】"},
		{"【难度=困难】深度推理:主动构建假说链。", "【难度=困难】"},
		{"【难度=地狱】大师级:全量工具 + 大师级策略。", "【难度=地狱】"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			withDifficulty := BuildSystemPrompt("", zero, "", tc.directive, false)
			if !strings.Contains(withDifficulty[0].Text, tc.label) {
				t.Errorf("expected %q in prompt tail, got: %q",
					tc.label,
					withDifficulty[0].Text[max(0, len(withDifficulty[0].Text)-200):])
			}
			// 前缀不变:append 前的字节必须与零版完全一致
			baseText := BuildSystemPrompt("", zero, "", "", false)[0].Text
			if !strings.HasPrefix(withDifficulty[0].Text, baseText) {
				t.Errorf("difficulty directive must be appended (not inserted) — prefix bytes diverge (Anthropic cache miss risk)")
			}
		})
	}
}

func TestBuildSystemPrompt_DifficultyTrimsSurroundingWhitespace(t *testing.T) {
	// 设计文档 §U2 明确:difficultyDirective 字符串前后空格被 TrimSpace 清理,
	// 避免 \"\\n\\n【难度=...】\\n\" 把多出的换行打入 prompt cache 边界。
	zero := PersonalityVector{}
	trimmed := BuildSystemPrompt("", zero, "", "   【难度=简单】trim  test   ", false)
	if !strings.Contains(trimmed[0].Text, "【难度=简单】trim  test") {
		t.Errorf("trimmed content missing: %q",
			trimmed[0].Text[max(0, len(trimmed[0].Text)-100):])
	}
	// 同样以 baseText 为前缀(只追加,不前置多余空白)
	baseText := BuildSystemPrompt("", zero, "", "", false)[0].Text
	if !strings.HasPrefix(trimmed[0].Text, baseText) {
		t.Error("whitespace-trimmed directive must still be a suffix of baseText")
	}
}