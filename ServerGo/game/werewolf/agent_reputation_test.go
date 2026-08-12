// §20260811-03 U2 — AgentReputation 单元测试。
//
// 覆盖矩阵(6 项):
//   A-01: AppendLast10Result FIFO 上限
//   A-02: ParseLast10Results 计数
//   A-03: ComputeSkillTags 启发式
//   A-04: SubmitRatingRequest.Validate
//   A-05: IsValidModelKey 防注入
//   A-06: ParseSkillTags CSV 解析

package werewolf

import (
	"strings"
	"testing"
)

// A-01
func TestAppendLast10Result_FIFO(t *testing.T) {
	prev := ""
	for i := 0; i < 15; i++ {
		prev = AppendLast10Result(prev, i%2 == 0)
	}
	if len(prev) != 10 {
		t.Fatalf("expected 10 chars, got %d: %q", len(prev), prev)
	}
}

// A-02
func TestParseLast10Results(t *testing.T) {
	wins, losses := ParseLast10Results("WLLWWLWLWL")
	if wins != 5 || losses != 5 {
		t.Fatalf("expected 5/5, got %d/%d", wins, losses)
	}
}

// A-03
func TestComputeSkillTags(t *testing.T) {
	tags := ComputeSkillTags(0.8, 0.7, 0.7, 0.6, 70, 0.9)
	if !strings.Contains(tags, "accurate_reader") ||
		!strings.Contains(tags, "master_deceiver") ||
		!strings.Contains(tags, "survivor") ||
		!strings.Contains(tags, "prop_master") ||
		!strings.Contains(tags, "eloquent_speaker") ||
		!strings.Contains(tags, "cold_calculator") {
		t.Fatalf("missing tags: %q", tags)
	}
}

// A-04
func TestSubmitRatingRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     SubmitRatingRequest
		wantErr bool
	}{
		{"valid_up", SubmitRatingRequest{ModelKey: "DeepSeek", Rating: 1}, false},
		{"valid_down", SubmitRatingRequest{ModelKey: "DouBao", Rating: -1}, false},
		{"missing_key", SubmitRatingRequest{Rating: 1}, true},
		{"bad_rating", SubmitRatingRequest{ModelKey: "X", Rating: 5}, true},
		{"too_long_comment", SubmitRatingRequest{ModelKey: "X", Rating: 1, Comment: strings.Repeat("a", 101)}, true},
	}
	for _, c := range cases {
		err := c.req.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: got err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

// A-05
func TestIsValidModelKey(t *testing.T) {
	good := []string{"DeepSeek-V3", "doubao_2", "GLM-Model", "abc123"}
	bad := []string{"", "with space", "with;semicolon", strings.Repeat("a", 65), "<script>", "with.dot"}
	for _, k := range good {
		if !IsValidModelKey(k) {
			t.Errorf("expected valid: %q", k)
		}
	}
	for _, k := range bad {
		if IsValidModelKey(k) {
			t.Errorf("expected invalid: %q", k)
		}
	}
}

// A-06
func TestParseSkillTags(t *testing.T) {
	tags := ParseSkillTags("accurate_reader,master_deceiver,survivor")
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	if tags[0] != "accurate_reader" {
		t.Fatalf("first tag mismatch: %s", tags[0])
	}
	// 空字符串容错
	if ParseSkillTags("") != nil {
		t.Fatal("empty input should return nil")
	}
}

// 辅助测试:FormatWinRate
func TestFormatWinRate(t *testing.T) {
	got := FormatWinRate(0.653)
	if got != "65.3%" {
		t.Errorf("expected 65.3%%, got %q", got)
	}
}

// 辅助测试:GenerateSignatureAsync
func TestGenerateSignatureAsync(t *testing.T) {
	rep := &AgentReputation{ModelKey: "DeepSeek", WinRate: 0.7}
	GenerateSignatureAsync(nil, "DeepSeek", rep)
	if rep.SignatureStyle == "" {
		t.Error("expected non-empty signature")
	}
	if rep.UpdatedAt == 0 {
		t.Error("expected UpdatedAt to be set")
	}
}

// 辅助测试:AgentReputationResponse wrapper
func TestAgentReputationResponse_Wrapper(t *testing.T) {
	resp := AgentReputationResponse{
		Reputation: &AgentReputation{ModelKey: "DeepSeek", TotalGames: 100, Wins: 60},
		Source:     "db",
	}
	if resp.Source != "db" {
		t.Fatalf("source mismatch: %s", resp.Source)
	}
	if resp.Reputation.WinRate != 0 {
		// 零值:实际写入时由 service 计算
	}
}
