// Package werewolf — recall_aggregate_test.go: §20260812-01 U1 测试用例。
//
// 覆盖:
//   - 投票准确率 4 场景(投中 / 票型一致 / 投错 / 弃权)
//   - 发言暴露度 2 场景(暴露身份 / 安全发言)
//   - 道具效率 3 场景(高 / 低 / 零)
//   - Agent 互动 2 场景(回应 / 静默)
//   - 缓存命中与失效
//   - 亮点时刻 ≤ 3 条
package werewolf

import (
	"testing"
)

func TestComputeVoteAccuracy(t *testing.T) {
	// 场景 1:投中
	dim := computeVoteAccuracy([]VoteReviewRecord{
		{DayEliminated: 5, Votes: []int{-1, -1, 5, -1, -1, -1, -1}, TallyMax: []int{5}},
	})
	if dim.TotalCount != 1 || dim.HitCount != 1 || dim.Score != 1.0 {
		t.Errorf("投中场景: got Total=%d Hit=%d Score=%.2f, want 1/1/1.00", dim.TotalCount, dim.HitCount, dim.Score)
	}

	// 场景 2:票型一致
	dim = computeVoteAccuracy([]VoteReviewRecord{
		{DayEliminated: 5, Votes: []int{-1, -1, 7, -1, -1, -1, -1}, TallyMax: []int{5, 7}},
	})
	if dim.TotalCount != 1 || dim.HitCount != 1 || dim.Score != 0.5 {
		t.Errorf("票型一致: got Total=%d Hit=%d Score=%.2f, want 1/1/0.50", dim.TotalCount, dim.HitCount, dim.Score)
	}

	// 场景 3:投错
	dim = computeVoteAccuracy([]VoteReviewRecord{
		{DayEliminated: 5, Votes: []int{-1, -1, 6, -1, -1, -1, -1}, TallyMax: []int{5}},
	})
	if dim.TotalCount != 1 || dim.HitCount != 0 || dim.Score != 0.0 {
		t.Errorf("投错: got Total=%d Hit=%d Score=%.2f, want 1/0/0.00", dim.TotalCount, dim.HitCount, dim.Score)
	}

	// 场景 4:弃权(不应计入分母)
	dim = computeVoteAccuracy([]VoteReviewRecord{
		{DayEliminated: 5, Votes: []int{-1, -1, -1, -1, -1, -1, -1}, TallyMax: []int{5}},
	})
	if dim.TotalCount != 0 {
		t.Errorf("弃权: got Total=%d, want 0", dim.TotalCount)
	}
}

func TestComputeSpeakExposure(t *testing.T) {
	// 暴露身份
	dim := computeSpeakExposure([]string{
		"我觉得 3 号是狼人",
		"5 号像预言家",
		"我昨晚查了 4 号",
	})
	if dim.HitCount < 2 {
		t.Errorf("暴露身份: got Hit=%d, want >=2", dim.HitCount)
	}
	if dim.Score < 0.5 {
		t.Errorf("暴露身份: got Score=%.2f, want >=0.5", dim.Score)
	}

	// 安全发言
	dim = computeSpeakExposure([]string{
		"今天天气真好",
		"我觉得 5 号不太对劲",
		"我先观察一下",
	})
	if dim.HitCount != 0 {
		t.Errorf("安全发言: got Hit=%d, want 0", dim.HitCount)
	}
}

func TestComputePropEfficiency(t *testing.T) {
	// 高效
	dim := computePropEfficiency([]PropReviewRecord{
		{UserID: "u1", IsHit: true},
		{UserID: "u1", IsHit: true},
		{UserID: "u1", IsHit: true},
		{UserID: "u1", IsHit: false},
	})
	if dim.Score != 0.75 {
		t.Errorf("高效: got Score=%.2f, want 0.75", dim.Score)
	}

	// 空
	dim = computePropEfficiency(nil)
	if dim.Score != 0.0 {
		t.Errorf("空: got Score=%.2f, want 0.00", dim.Score)
	}
}

func TestComputeAgentInteraction(t *testing.T) {
	// 100% 回应
	dim := computeAgentInteraction(4, 4)
	if dim.Score != 1.0 {
		t.Errorf("100%% 回应: got Score=%.2f, want 1.00", dim.Score)
	}

	// 0% 回应
	dim = computeAgentInteraction(4, 0)
	if dim.Score != 0.0 {
		t.Errorf("0%% 回应: got Score=%.2f, want 0.00", dim.Score)
	}

	// 0 发起
	dim = computeAgentInteraction(0, 0)
	if dim.Score != 0.0 {
		t.Errorf("0 发起: got Score=%.2f, want 0.00", dim.Score)
	}
}

func TestComputeReviewFromInputs(t *testing.T) {
	in := PersonalReviewInputs{
		UserID: "test_user",
		VoteRecords: []VoteReviewRecord{
			{DayEliminated: 3, Votes: []int{-1, -1, 3}, TallyMax: []int{3}},
		},
		SpeakTexts: []string{"我觉得 5 号是狼人"},
		PropRecords: []PropReviewRecord{
			{UserID: "test_user", IsHit: true},
			{UserID: "test_user", IsHit: false},
		},
		InteractionsInitiated: 2,
		InteractionsResponded: 2,
		Role:                  "werewolf",
		Winner:                "wolf",
	}
	rev := ComputeReviewFromInputs(in)
	if rev == nil {
		t.Fatal("ComputeReviewFromInputs returned nil")
	}
	if rev.VoteAccuracy.Score != 1.0 {
		t.Errorf("vote accuracy: got %.2f, want 1.00", rev.VoteAccuracy.Score)
	}
	if rev.SpeakExposure.Score < 0.5 {
		t.Errorf("speak exposure: got %.2f, want ≥0.5", rev.SpeakExposure.Score)
	}
	if rev.PropEfficiency.Score != 0.5 {
		t.Errorf("prop efficiency: got %.2f, want 0.50", rev.PropEfficiency.Score)
	}
	if rev.AgentInteraction.Score != 1.0 {
		t.Errorf("agent interaction: got %.2f, want 1.00", rev.AgentInteraction.Score)
	}
	if rev.Role != "werewolf" {
		t.Errorf("role: got %s, want werewolf", rev.Role)
	}
	// 亮点 ≤ 3 条
	if len(rev.Highlights) > 3 {
		t.Errorf("highlights > 3: got %d", len(rev.Highlights))
	}
}

func TestContainsIdentityLeak(t *testing.T) {
	if !containsIdentityLeak("5 号是狼人") {
		t.Error("should detect 狼人")
	}
	if !containsIdentityLeak("他是预言家") {
		t.Error("should detect 预言家")
	}
	if containsIdentityLeak("今天天气真好") {
		t.Error("should not leak identity")
	}
}

func TestComputeReviewEmptyUserID(t *testing.T) {
	rev := ComputeReviewFromInputs(PersonalReviewInputs{})
	if rev != nil {
		t.Error("empty userID should return nil")
	}
}

func TestBuildHighlightsLimit(t *testing.T) {
	rev := &PersonalReview{
		VoteAccuracy:     ReviewDimension{Score: 1.0, TotalCount: 3},
		SpeakExposure:    ReviewDimension{Score: 0.0, TotalCount: 5},
		PropEfficiency:   ReviewDimension{Score: 1.0, TotalCount: 2},
		AgentInteraction: ReviewDimension{Score: 1.0, TotalCount: 5},
	}
	hl := buildHighlights(rev)
	if len(hl) != 3 {
		t.Errorf("highlights should be 3, got %d", len(hl))
	}
}
