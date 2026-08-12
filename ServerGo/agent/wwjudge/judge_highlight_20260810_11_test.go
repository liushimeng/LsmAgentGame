// Package wwjudge - judge_highlight_20260810_11_test.go: §20260810-11 H1 高光时刻第 6 段测试
//
// 覆盖:
//  1. ParseSummary 兼容 5 段(旧 LLM 输出)与 6 段(新 LLM 输出)
//  2. HasHighlight 在非空非"[]"时返回 true
//  3. AllSectionsFilled 不强制第 6 段(向后兼容)
//  4. JSON 解析失败时 Highlight 字段保留原始字符串(用于前端 retry)

package wwjudge

import (
	"strings"
	"testing"
)

// TestParseSummary_FiveSectionsBackCompat 旧 LLM 输出 5 段仍能解析,Highlight 为空。
func TestParseSummary_FiveSectionsBackCompat(t *testing.T) {
	raw := "【阵营胜负】好人胜利。\n【关键翻盘点】D2 预言家查验。\n【角色操作时间线】D1 狼刀 3。\n【MVP 玩家】7 号预言家。\n【狼人悍跳记录】无。\n"
	got := ParseSummary(raw)
	if got.Outcome != "好人胜利。" {
		t.Errorf("Outcome mismatch: %q", got.Outcome)
	}
	if got.Highlight != "" {
		t.Errorf("Highlight should be empty for 5-section LLM, got %q", got.Highlight)
	}
	if !got.AllSectionsFilled() {
		t.Errorf("AllSectionsFilled should still be true (5 core sections filled)")
	}
	if got.HasHighlight() {
		t.Errorf("HasHighlight should be false for 5-section LLM")
	}
}

// TestParseSummary_SixSections 新 LLM 输出 6 段,Highlight 正确解析。
func TestParseSummary_SixSections(t *testing.T) {
	highlightJSON := `[{"seat":1,"moment":"亮明女巫","quote":"我还有一瓶毒"},{"seat":5,"moment":"跳预言家","quote":"5号是狼"},{"seat":9,"moment":"守卫守对","quote":"完美"}]`
	raw := "【阵营胜负】好人胜利。\n" +
		"【关键翻盘点】D2 预言家查验。\n" +
		"【角色操作时间线】D1 狼刀 3。\n" +
		"【MVP 玩家】7 号预言家。\n" +
		"【狼人悍跳记录】无。\n" +
		"【高光时刻】" + highlightJSON + "\n"
	got := ParseSummary(raw)
	if !got.HasHighlight() {
		t.Errorf("HasHighlight should be true for 6-section LLM")
	}
	if got.Highlight != highlightJSON {
		t.Errorf("Highlight should be raw JSON, got %q", got.Highlight)
	}
	if !got.AllSectionsFilled() {
		t.Errorf("AllSectionsFilled should be true")
	}
}

// TestParseSummary_EmptyHighlight 验证空高光 JSON "[]" 不算有高光。
func TestParseSummary_EmptyHighlight(t *testing.T) {
	raw := "【阵营胜负】好人胜利。\n" +
		"【关键翻盘点】D2 预言家查验。\n" +
		"【角色操作时间线】D1 狼刀 3。\n" +
		"【MVP 玩家】7 号预言家。\n" +
		"【狼人悍跳记录】无。\n" +
		"【高光时刻】[]\n"
	got := ParseSummary(raw)
	if got.HasHighlight() {
		t.Errorf("HasHighlight should be false for '[]'")
	}
	if !got.AllSectionsFilled() {
		t.Errorf("AllSectionsFilled should still be true (5 core sections filled)")
	}
}

// TestSummarySectionsJSON_HighlightFieldToJSON 验证 ToJSON 透传 Highlight。
func TestSummarySectionsJSON_HighlightFieldToJSON(t *testing.T) {
	raw := "【阵营胜负】好人胜利。\n" +
		"【关键翻盘点】D2 预言家查验。\n" +
		"【角色操作时间线】D1 狼刀 3。\n" +
		"【MVP 玩家】7 号预言家。\n" +
		"【狼人悍跳记录】无。\n" +
		"【高光时刻】[{\"seat\":1,\"moment\":\"X\",\"quote\":\"Y\"}]\n"
	got := ParseSummary(raw)
	j := got.ToJSON("test-model")
	if !strings.Contains(j.Highlight, "seat") {
		t.Errorf("SummarySectionsJSON.Highlight should contain JSON, got %q", j.Highlight)
	}
}

// TestBuildSummaryPrompt_Mentions6Sections 验证 prompt 提示 6 段。
func TestBuildSummaryPrompt_Mentions6Sections(t *testing.T) {
	in := SummaryInput{
		RoomID:    "test-room",
		DayNumber: 1,
		Winner:    "good",
	}
	prompt := BuildSummaryPrompt(in)
	// 必含第 6 段标题与 JSON 数组说明
	if !strings.Contains(prompt, "【高光时刻】") {
		t.Error("prompt should contain 【高光时刻】")
	}
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should mention JSON format for highlights")
	}
	if !strings.Contains(prompt, "6 个段标题") {
		t.Error("prompt should mention 6 sections (5 + highlight)")
	}
}
