package service

import (
	"strings"
	"testing"
)

func TestBuildBotAccountFromModel(t *testing.T) {
	cases := []struct {
		name   string
		model  string
		want   string
	}{
		{"camel-case collapsed", "DouBao-model", "bot_doubao_model"},
		{"pascal with hyphens", "DeepSeek-V4-Pro", "bot_deepseek_v4_pro"},
		{"already lower", "minimax-model", "bot_minimax_model"},
		{"mixed punctuation", "Qwen.3.7/Plus-and-Max", "bot_qwen_3_7_plus_and_max"},
		{"trailing punctuation stripped", "Kimi-2.7-", "bot_kimi_2_7"},
		{"empty input", "", "bot_"},
		{"only punctuation", "---", "bot_"},
		{"long input truncated", strings.Repeat("A", 100) + "-model", "bot_" + strings.Repeat("a", 28)},
		{"digits preserved", "GLM5.2", "bot_glm5_2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildBotAccountFromModel(tc.model)
			if got != tc.want {
				t.Fatalf("BuildBotAccountFromModel(%q) = %q, want %q", tc.model, got, tc.want)
			}
			if len(got) > 32 {
				t.Fatalf("BuildBotAccountFromModel(%q) returned %q (%d chars), exceeds VARCHAR(32)", tc.model, got, len(got))
			}
		})
	}
}