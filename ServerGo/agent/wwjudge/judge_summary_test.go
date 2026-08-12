// Package agent — judge_summary_test.go: 单元测试覆盖。
package wwjudge

import (
	"strings"
	"testing"
)

func TestBuildSummaryPrompt_IncludesAll5Sections(t *testing.T) {
	in := SummaryInput{
		RoomID:     "room1",
		DayNumber:  3,
		Winner:     "good",
		AliveSeats: []int{0, 4, 8},
		DeadSeats:  []int{1, 5},
		Roles:      map[int]string{0: "seer", 1: "werewolf", 4: "villager", 5: "hunter", 8: "witch"},
		Speeches:   []string{"1号说: 我是预言家"},
		ChatTail:   []string{"(聊天 1)"},
	}
	prompt := BuildSummaryPrompt(in)
	for _, want := range []string{"【阵营胜负】", "【关键翻盘点】", "【角色操作时间线】", "【MVP 玩家】", "【狼人悍跳记录】"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing title %q", want)
		}
	}
	if !strings.Contains(prompt, "room1") {
		t.Errorf("prompt missing room ID")
	}
	if !strings.Contains(prompt, "好人阵营") {
		t.Errorf("prompt missing translated winner")
	}
}

func TestParseSummary_SplitsSections(t *testing.T) {
	raw := strings.Join([]string{
		"【阵营胜负】好人阵营以 4:3 票差获胜。",
		"【关键翻盘点】第三天预言家查验 5 号为狼,扭转局势。",
		"【角色操作时间线】D1 狼刀 7 号平民,D2 女巫毒 4 号,D3 狼自爆。",
		"【MVP 玩家】9 号预言家,3 次正确查验。",
		"【狼人悍跳记录】2 号狼冒充预言家,被 11 号真预言家对跳戳穿。",
	}, "\n")
	got := ParseSummary(raw)
	if got.Outcome != "好人阵营以 4:3 票差获胜。" {
		t.Errorf("Outcome = %q", got.Outcome)
	}
	if !strings.HasPrefix(got.MVP, "9 号预言家") {
		t.Errorf("MVP prefix wrong: %q", got.MVP)
	}
	if !strings.Contains(got.WolfDeception, "2 号狼") {
		t.Errorf("WolfDeception missing wolf ref: %q", got.WolfDeception)
	}
}

func TestParseSummary_FillsMissing(t *testing.T) {
	raw := "【阵营胜负】好人阵营获胜。"
	got := ParseSummary(raw)
	if got.Outcome != "好人阵营获胜。" {
		t.Errorf("Outcome = %q", got.Outcome)
	}
	if got.TurningPoint != "" || got.RoleTimeline != "" || got.MVP != "" || got.WolfDeception != "" {
		t.Errorf("missing sections not filled with empty: %+v", got)
	}
}

func TestParseSummary_FreeformFallback(t *testing.T) {
	raw := "对局结束,好人获胜。"
	got := ParseSummary(raw)
	if got.Outcome != raw {
		t.Errorf("freeform fallback: Outcome = %q", got.Outcome)
	}
}

func TestFallbackSummary_EmptyWinner(t *testing.T) {
	in := SummaryInput{RoomID: "r1", DayNumber: 2}
	got := FallbackSummary(in, "ctx canceled")
	if !strings.Contains(got, "LLM 总结失败") {
		t.Errorf("FallbackSummary should mention failure: %q", got)
	}
}

func TestFallbackSummary_WithWinner(t *testing.T) {
	in := SummaryInput{
		RoomID:     "r1",
		DayNumber:  5,
		Winner:     "wolf",
		WinnerSeat: 2,
		Roles:      map[int]string{2: "werewolf"},
	}
	got := FallbackSummary(in, "")
	if !strings.Contains(got, "狼人阵营") {
		t.Errorf("FallbackSummary missing winner: %q", got)
	}
	if !strings.Contains(got, "3号") {
		t.Errorf("FallbackSummary missing MVP seat: %q", got)
	}
}

func TestFlattenSummary_AllSections(t *testing.T) {
	s := SummarySections{
		Outcome:       "好人获胜",
		TurningPoint:  "D3 预言家跳反",
		RoleTimeline:  "D1 狼刀 7 号",
		MVP:           "9 号预言家",
		WolfDeception: "2 号狼冒充预言家",
	}
	got := FlattenSummary(s)
	for _, want := range []string{"【阵营胜负】好人获胜", "【关键翻盘点】D3 预言家跳反"} {
		if !strings.Contains(got, want) {
			t.Errorf("FlattenSummary missing %q in %q", want, got)
		}
	}
}

func TestFlattenSummary_TruncatesLong(t *testing.T) {
	long := strings.Repeat("X", 2000)
	s := SummarySections{Outcome: long}
	got := FlattenSummary(s)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("FlattenSummary should truncate: tail=%q", got[len(got)-30:])
	}
}

func TestTruncateForPrompt(t *testing.T) {
	if got := truncateForPrompt("hello", 10); got != "hello" {
		t.Errorf("short passthrough: %q", got)
	}
	if got := truncateForPrompt("hello world", 5); !strings.HasSuffix(got, "…") {
		t.Errorf("long truncated: %q", got)
	}
	if got := truncateForPrompt("一二三四五六七八九十", 3); got != "一二三…" {
		t.Errorf("Chinese truncation: %q", got)
	}
}

func TestSummarySections_EmptyAndFilled(t *testing.T) {
	empty := SummarySections{}
	if !empty.IsEmpty() {
		t.Error("zero-value should be empty")
	}
	full := SummarySections{Outcome: "x", TurningPoint: "x", RoleTimeline: "x", MVP: "x", WolfDeception: "x"}
	if full.IsEmpty() {
		t.Error("fully-filled should not be empty")
	}
	if !full.AllSectionsFilled() {
		t.Error("fully-filled should report AllSectionsFilled")
	}
	half := SummarySections{Outcome: "x"}
	if half.AllSectionsFilled() {
		t.Error("partial should not report AllSectionsFilled")
	}
}

func TestSummarySections_ToJSON(t *testing.T) {
	s := SummarySections{Outcome: "x"}
	j := s.ToJSON("judge-model")
	if j.Model != "judge-model" {
		t.Errorf("Model missing")
	}
	if j.GeneratedAt <= 0 {
		t.Errorf("GeneratedAt should be unix millis, got %d", j.GeneratedAt)
	}
}

func TestEmitGameOverSummary_NilJudge(t *testing.T) {
	if EmitGameOverSummary(nil, "m1", SummaryInput{RoomID: "r1"}) {
		t.Error("nil judge should return false")
	}
}

func TestEmitGameOverSummary_FillsDefaults(t *testing.T) {
	j := NewAgentJudge("r1", "model-x")
	ok := EmitGameOverSummary(j, "", SummaryInput{DayNumber: 0})
	if !ok {
		t.Error("should enqueue event")
	}
	evt := <-j.events
	in, _ := evt.Extra["summary_input"].(SummaryInput)
	if in.RoomID != "r1" || in.DayNumber != 1 {
		t.Errorf("defaults not filled: %+v", in)
	}
	// Note: EmitGameOverSummary echoes caller-supplied model_key as-is.
	if m, _ := evt.Extra["model_key"].(string); m != "" {
		t.Errorf("caller model_key should be echoed: %q", m)
	}
}

func TestHandleGameOverSummaryInternal_NoBridge(t *testing.T) {
	j := NewAgentJudge("r1", "m1")
	evt := JudgeEvent{
		Kind: JudgePendingGameOverSummary,
		Extra: map[string]any{
			"summary_input": SummaryInput{RoomID: "r1", DayNumber: 2, Winner: "good", WinnerSeat: 0, Roles: map[int]string{0: "seer"}},
			"model_key":     "m1",
		},
	}
	j.handleGameOverSummaryInternal(evt)
	tr := j.JudgeTranscript()
	if tr.LastSummary == "" {
		t.Error("LastSummary should be set even with no bridge (fallback)")
	}
}

func TestLastGameMemoryBlock_Empty(t *testing.T) {
	if got := LastGameMemoryBlock("", nil); got != "" {
		t.Errorf("empty input should return empty: %q", got)
	}
	if got := LastGameMemoryBlock("model", nil); got != "" {
		t.Errorf("nil memories should return empty: %q", got)
	}
	if got := LastGameMemoryBlock("model", []string{}); got != "" {
		t.Errorf("empty memories should return empty: %q", got)
	}
}

func TestLastGameMemoryBlock_Filled(t *testing.T) {
	got := LastGameMemoryBlock("model-x", []string{"好人阵营获胜"})
	if !strings.Contains(got, "model-x") {
		t.Errorf("missing model key in output: %q", got)
	}
	if !strings.Contains(got, "好人阵营获胜") {
		t.Errorf("missing memory text: %q", got)
	}
}
