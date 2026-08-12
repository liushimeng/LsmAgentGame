// Package agent — speak_whisper_factcheck_test.go: 验证 BUG-R151-FAIRNESS-001
// 的 FactCheckWhisperAttribution 行为。
//
// 核心场景:
//   1. 无私聊归因动词 → 文本原样返回, wasFactChecked=false。
//   2. 有归因动词但 seat 在 inbox 内 → 文本保留, wasFactChecked=false(允许合法引用)。
//   3. 有归因动词且 seat 不在 inbox 内 → 整段替换为 "[已过滤:无可证实的私聊]", wasFactChecked=true。
//   4. R151 真实案例 "9号 私聊告诉我 12号 是悍跳" → 命中过滤。
package wwplayer

import (
	"LsmAgentGame/agent/wwtypes"
	"strings"
	"testing"
)

func TestFactCheckWhisperAttribution_NoVerb(t *testing.T) {
	text := "我是8号,今晚我要投12号,因为他昨晚说话前后矛盾。"
	inbox := []wwtypes.WhisperEvent{}
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if hit {
		t.Errorf("no attribution verb should not trigger fact-check, got hit=true, cleaned=%q", got)
	}
	if got != text {
		t.Errorf("no attribution verb should return text unchanged, got %q", got)
	}
}

func TestFactCheckWhisperAttribution_LegitimateQuote(t *testing.T) {
	// 9号 真的 whisper 给 8号 了 → 合法引用,保留。
	text := "9号 私聊告诉我 12号 是悍跳狼队友,大家小心。"
	inbox := []wwtypes.WhisperEvent{
		{FromSeat: 8, From: "Bot 9号", IsBot: true, Text: "12号 是我队友"},
	}
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if hit {
		t.Errorf("legitimate quote (seat 9 in inbox) should not be flagged, got hit=true, cleaned=%q", got)
	}
	if got != text {
		t.Errorf("legitimate quote should be preserved verbatim, got %q want %q", got, text)
	}
}

func TestFactCheckWhisperAttribution_FabricatedR151Case(t *testing.T) {
	// R151 Bot 8号 真实捏造的私聊归因句式:「9号 私聊告诉我 12号 是悍跳」。
	// 这是核心诱导话术,9号 从未 whisper 给我 → 应被过滤。
	text := "9号 私聊告诉我 12号 是悍跳狼队友,大家小心。"
	inbox := []wwtypes.WhisperEvent{} // 9号 从未 whisper 给我
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if !hit {
		t.Errorf("fabricated whisper claim should trigger fact-check, got hit=false, kept=%q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的私聊]") {
		t.Errorf("cleaned text should contain filter marker, got %q", got)
	}
}

func TestFactCheckWhisperAttribution_OtherBotLie(t *testing.T) {
	// R151 Bot 2号 真实原话:「12号 私聊我让我帮他拉票」 → 12号 没 whisper 给我。
	text := "其实12号昨晚私聊我让我帮他拉票,我拒绝了,但这事大家应该知道。"
	inbox := []wwtypes.WhisperEvent{} // 12号 从未 whisper 给我
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if !hit {
		t.Errorf("fabricated Bot 2号 claim should be filtered, got hit=false, kept=%q", got)
	}
	if !strings.Contains(got, "[已过滤:无可证实的私聊]") {
		t.Errorf("cleaned text should contain filter marker, got %q", got)
	}
}

func TestFactCheckWhisperAttribution_MultipleClaims(t *testing.T) {
	// 同一段发言里多次归因:3号 合法 + 7号 捏造 → 只过滤 7号 那段。
	text := "3号 私聊告诉我他是好人,但7号 私下跟我说4号 是悍跳。"
	inbox := []wwtypes.WhisperEvent{
		{FromSeat: 2, From: "Bot 3号", IsBot: true, Text: "我是平民"},
		// 注意:7号 seat=6 → 1-indexed 7号,FromSeat=6
	}
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if !hit {
		t.Errorf("one fabricated claim should trigger fact-check, got hit=false, kept=%q", got)
	}
	// 3号 (legitimate) 部分应保留。
	if !strings.Contains(got, "3号") {
		t.Errorf("legitimate 3号 quote should be preserved, got %q", got)
	}
	// 7号 (fabricated) 部分应被替换。
	if !strings.Contains(got, "[已过滤:无可证实的私聊]") {
		t.Errorf("fabricated 7号 quote should be filtered, got %q", got)
	}
}

func TestFactCheckWhisperAttribution_SpectatorInboxIgnored(t *testing.T) {
	// 观战者(FromSeat=-1)whisper 不应被视为 bot 私聊 → 即使 9号 的 whisper
	// 是观战者发的,bot 在公屏仍应被过滤(只有 bot 互发才算合法归因)。
	text := "9号 私聊告诉我他是预言家。"
	inbox := []wwtypes.WhisperEvent{
		{FromSeat: -1, From: "观战者test_01", IsSpectator: true, Text: "我是预言家"},
	}
	_, hit := FactCheckWhisperAttribution(text, inbox)
	if !hit {
		t.Errorf("spectator whisper should not legitimize bot-attribution claim, got hit=false")
	}
}

func TestFactCheckWhisperAttribution_NoInboxAllFabricated(t *testing.T) {
	// 没有任何 inbox 时,任何归因都该被过滤(早期 pre_wolves 阶段典型)。
	text := "10号 跟我说他是女巫,大家听他的。"
	inbox := []wwtypes.WhisperEvent{}
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if !hit {
		t.Errorf("no inbox should treat all claims as fabricated, got hit=false")
	}
	if !strings.Contains(got, "[已过滤:无可证实的私聊]") {
		t.Errorf("cleaned text should contain filter marker, got %q", got)
	}
}

func TestFactCheckWhisperAttribution_NonAttributionUsesSeatNumber(t *testing.T) {
	// 「10号 是狼」这种描述,不归因到私聊 → 不应被过滤。
	text := "我认为10号 是狼,因为今天他的发言太可疑了。"
	inbox := []wwtypes.WhisperEvent{}
	got, hit := FactCheckWhisperAttribution(text, inbox)
	if hit {
		t.Errorf("non-attribution seat mention should not trigger fact-check, got hit=true, cleaned=%q", got)
	}
	if got != text {
		t.Errorf("non-attribution should be preserved, got %q", got)
	}
}
