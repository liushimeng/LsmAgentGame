// Package wwplayer — prompt_portrait_test.go: §20260810-10 U2 模型自画像单测。
//
// 覆盖不变式:
//  1. nil / !SampleOK → 通用自画像基线。
//  2. SampleOK → 渲染总胜率 + 狼/好人分阵营胜率 + 与全模型平均的对比。
//  3. 狼胜率显著高于好人 → "发挥欺骗优势"建议;反之 → "深水/倒钩"建议;均衡 → 灵活建议。
//  4. BuildSystemPrompt("") 与 BuildSystemPrompt(portrait) 的差异仅在末尾追加段
//     (prefix 完全一致 → Anthropic prompt cache 前缀命中)。
//  5. 自画像文案不含任何座位号/单局细节(§135 隐私)。
package wwplayer

import (
	"strings"
	"testing"
)

func TestPortrait_NilStats_GenericBaseline(t *testing.T) {
	got := BuildSelfPortraitText(nil)
	if !strings.Contains(got, "通用基线") {
		t.Fatalf("nil stats should render generic baseline, got: %s", got)
	}
	if !strings.Contains(got, "<8 局") {
		t.Fatalf("generic baseline should mention min games, got: %s", got)
	}
}

func TestPortrait_SmallSample_GenericBaseline(t *testing.T) {
	got := BuildSelfPortraitText(&SelfPortraitStats{
		Games: 3, WinRate: 1.0, SampleOK: false, // 3 局 100% 胜率也不应采信
	})
	if !strings.Contains(got, "通用基线") {
		t.Fatalf("small sample should fall back to generic baseline, got: %s", got)
	}
	if strings.Contains(got, "100.0%") {
		t.Fatalf("small sample stats must NOT leak into portrait, got: %s", got)
	}
}

func TestPortrait_SampleOK_RendersRates(t *testing.T) {
	got := BuildSelfPortraitText(&SelfPortraitStats{
		Games:         23,
		WinRate:       0.435,
		WolfGames:     9,
		WolfWinRate:   0.556,
		GoodGames:     14,
		GoodWinRate:   0.357,
		AvgWinRateAll: 0.312,
		SampleOK:      true,
	})
	for _, want := range []string{"23 局", "43.5%", "31.2%", "55.6%", "35.7%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("portrait missing %q: %s", want, got)
		}
	}
	// 狼胜率(55.6%) > 好人胜率(35.7%) + 10pp → 发挥欺骗优势建议。
	if !strings.Contains(got, "狼人侧显著强于好人侧") {
		t.Fatalf("want wolf-strong advice, got: %s", got)
	}
}

func TestPortrait_GoodStrongerAdvice(t *testing.T) {
	got := BuildSelfPortraitText(&SelfPortraitStats{
		Games: 20, WinRate: 0.5, SampleOK: true,
		WolfGames: 8, WolfWinRate: 0.25,
		GoodGames: 12, GoodWinRate: 0.667,
		AvgWinRateAll: 0.4,
	})
	if !strings.Contains(got, "好人侧显著强于狼人侧") {
		t.Fatalf("want good-strong advice, got: %s", got)
	}
	// 狼胜率低于平均 → 谨慎悍跳提示。
	if !strings.Contains(got, "谨慎") {
		t.Fatalf("want cautious wolf hint, got: %s", got)
	}
}

func TestPortrait_BalancedAdvice(t *testing.T) {
	got := BuildSelfPortraitText(&SelfPortraitStats{
		Games: 16, WinRate: 0.5, SampleOK: true,
		WolfGames: 8, WolfWinRate: 0.5,
		GoodGames: 8, GoodWinRate: 0.5,
		AvgWinRateAll: 0.45,
	})
	if !strings.Contains(got, "均衡") {
		t.Fatalf("want balanced advice, got: %s", got)
	}
}

func TestPortrait_SystemPromptPrefixStable(t *testing.T) {
	// 空串 → 与旧版逐字节一致(回归断言)。
	base := BuildSystemPrompt("", PersonalityVector{}, "", "")
	if len(base) != 1 {
		t.Fatalf("want 1 system block, got %d", len(base))
	}
	portrait := BuildSelfPortraitText(&SelfPortraitStats{
		Games: 10, WinRate: 0.6, SampleOK: true,
		WolfGames: 5, WolfWinRate: 0.8,
		GoodGames: 5, GoodWinRate: 0.4,
		AvgWinRateAll: 0.4,
	})
	withPortrait := BuildSystemPrompt(portrait, PersonalityVector{}, "", "")
	if !strings.HasPrefix(withPortrait[0].Text, base[0].Text) {
		t.Fatal("portrait must be appended at END (prefix must stay byte-identical for cache)")
	}
	if !strings.HasSuffix(withPortrait[0].Text, portrait+"\n") {
		t.Fatal("portrait text must appear at system end")
	}
}

func TestPortrait_NoSeatLeak(t *testing.T) {
	// §135 隐私:自画像文案绝不含座位号/单局玩家信息。
	got := BuildSelfPortraitText(&SelfPortraitStats{
		Games: 100, WinRate: 0.9, SampleOK: true,
		WolfGames: 50, WolfWinRate: 0.9,
		GoodGames: 50, GoodWinRate: 0.9,
		AvgWinRateAll: 0.3,
	})
	for _, banned := range []string{"号玩家", "座位", "seat"} {
		if strings.Contains(got, banned) {
			t.Fatalf("portrait leaks %q: %s", banned, got)
		}
	}
}
