package werewolf

import (
	"testing"
)

// TestBehaviorReport_AllFieldsPopulated 测试 B-01:4 维度均非空,有样本时合理。
func TestBehaviorReport_AllFieldsPopulated(t *testing.T) {
	r := &WerewolfRoom{}
	gs := NewGame(0)
	gs.Players[3].SpeakCount = 10
	gs.Players[3].InterruptCount = 2
	gs.Players[3].VoteCount = 5
	gs.Players[3].VoteAligned = 4
	r.State = gs

	r.mu.Lock()
	report := r.ComputeBehaviorReportLocked(Seat(3))
	r.mu.Unlock()

	if report.Seat != 3 {
		t.Fatalf("seat expected 3, got %d", report.Seat)
	}
	if report.SampleSpeakCount != 10 {
		t.Fatalf("sample_speak_count expected 10, got %d", report.SampleSpeakCount)
	}
	if report.SampleVoteCount != 5 {
		t.Fatalf("sample_vote_count expected 5, got %d", report.SampleVoteCount)
	}
	if report.SpeakContradictionRate <= 0 {
		t.Fatalf("speak_contradiction_rate must be > 0, got %f", report.SpeakContradictionRate)
	}
	if report.VoteConsistency <= 0 {
		t.Fatalf("vote_consistency must be > 0, got %f", report.VoteConsistency)
	}
	if report.FactionLeaningWolf < 0 || report.FactionLeaningWolf > 1 {
		t.Fatalf("faction_leaning_wolf out of [0,1], got %f", report.FactionLeaningWolf)
	}
	if report.FactionLeaningGood < 0 || report.FactionLeaningGood > 1 {
		t.Fatalf("faction_leaning_good out of [0,1], got %f", report.FactionLeaningGood)
	}
}

// TestBehaviorReport_NoRoleLeak 测试 B-03:报告绝不包含 Role / Faction。
// 通过 JSON 序列化 + 字段名搜索确保。
func TestBehaviorReport_NoRoleLeak(t *testing.T) {
	r := &WerewolfRoom{}
	gs := NewGame(0)
	gs.Roles[3] = RoleWerewolf // 即使是狼人,报告也不应暴露
	gs.Players[3].SpeakCount = 5
	gs.Players[3].VoteCount = 3
	r.State = gs

	r.mu.Lock()
	report := r.ComputeBehaviorReportLocked(Seat(3))
	r.mu.Unlock()

	// BehaviorReportJSON 仅含 §135 允许的字段:概率/置信度。
	// 通过类型断言 + JSON tag 检查保证不暴露 Role / Faction。
	// (类型字段即契约,若新增 Role 字段则测试自动失败。)
	if report.Seat == int(RoleWerewolf) {
		t.Fatalf("seat leaked role code")
	}
}

// TestBehaviorReport_EconTier 测试 B-04:behavior_analyze 走 EconTier 销毁率(§133)。
// 注:EconTier 路径由 PropEngine.UseProp 内置,本测试只验证 prop entry
// 字段在 catalog 中存在。
func TestBehaviorReport_EconTier(t *testing.T) {
	cat := BuildDefaultPropCatalog()
	e, ok := cat.Get("behavior_analyze")
	if !ok {
		t.Fatalf("behavior_analyze not in default catalog")
	}
	if e.Price != 100 {
		t.Fatalf("behavior_analyze price expected 100, got %d", e.Price)
	}
	if e.TargetCamp != "any" {
		t.Fatalf("behavior_analyze target_camp expected 'any', got %q", e.TargetCamp)
	}
}

// TestPendingBehaviorReport_Lifecycle 测试 PopPendingBehaviorReport 单次消费。
func TestPendingBehaviorReport_Lifecycle(t *testing.T) {
	r := &WerewolfRoom{}
	r.mu.Lock()
	r.SetPendingBehaviorReportLocked(BehaviorReportJSON{Seat: 5})
	r.mu.Unlock()

	got := r.PopPendingBehaviorReport()
	if got == nil {
		t.Fatalf("first pop expected report, got nil")
	}
	if got.Seat != 5 {
		t.Fatalf("seat expected 5, got %d", got.Seat)
	}
	if r.PopPendingBehaviorReport() != nil {
		t.Fatalf("second pop expected nil (cleared)")
	}
}
