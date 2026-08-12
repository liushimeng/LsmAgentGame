package werewolf

// commitment_ledger_test.go — 承诺账本单元测试（§20260810-06）。

import (
	"testing"
	"time"
)

func TestNewCommitmentLedger(t *testing.T) {
	cl := NewCommitmentLedger()
	if cl == nil {
		t.Fatal("NewCommitmentLedger returned nil")
	}
	if len(cl.items) != 0 {
		t.Fatalf("expected empty items, got %d", len(cl.items))
	}
}

func TestAddCommitment_Basic(t *testing.T) {
	cl := NewCommitmentLedger()
	c, err := cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "我怀疑3号", 3)
	if err != nil {
		t.Fatalf("AddCommitmentLocked failed: %v", err)
	}
	if c.Seat != 0 || c.Round != 1 || c.Template != CommitSeerCheck {
		t.Errorf("unexpected commitment fields: %+v", c)
	}
	if c.Status != CommitStatusPending {
		t.Errorf("expected pending, got %s", c.Status)
	}
}

func TestAddCommitment_MaxPerDay(t *testing.T) {
	cl := NewCommitmentLedger()
	// 添加 3 条（上限）
	for i := 0; i < 3; i++ {
		_, err := cl.AddCommitmentLocked(0, 1, CommitSeerCheck, i, "", "", 3)
		if err != nil {
			t.Fatalf("AddCommitmentLocked %d failed: %v", i, err)
		}
	}
	// 第 4 条应失败
	_, err := cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 4, "", "", 3)
	if err == nil {
		t.Fatal("expected max per day error")
	}
}

func TestAddCommitment_InvalidTemplate(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, "invalid_template", 3, "", "", 3)
	if err == nil {
		t.Fatal("expected invalid template error")
	}
}

func TestAddCommitment_TextTruncation(t *testing.T) {
	cl := NewCommitmentLedger()
	longText := string(make([]byte, 100))
	c, err := cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, longText, longText, 3)
	if err != nil {
		t.Fatalf("AddCommitmentLocked failed: %v", err)
	}
	if len(c.ParamText) > 30 {
		t.Errorf("ParamText not truncated: %d chars", len(c.ParamText))
	}
	if len(c.Reason) > 30 {
		t.Errorf("Reason not truncated: %d chars", len(c.Reason))
	}
}

func TestEvaluateSeerCheck_Fulfilled(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(5, 1, CommitSeerCheck, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		SeerSeat:        5,
		SeerCheckTarget: 3,
		CurrentDay:      2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitSeerCheck, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusFulfilled {
		t.Errorf("expected fulfilled, got %s", changed[0].Status)
	}
}

func TestEvaluateSeerCheck_Broken(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(5, 1, CommitSeerCheck, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		SeerSeat:        5,
		SeerCheckTarget: 7, // 验了别人
		CurrentDay:      2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitSeerCheck, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusBroken {
		t.Errorf("expected broken, got %s", changed[0].Status)
	}
}

func TestEvaluateSeerCheck_Expired(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(5, 1, CommitSeerCheck, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		SeerSeat:        8, // 5号不是预言家
		SeerCheckTarget: 3,
		CurrentDay:      2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitSeerCheck, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusExpired {
		t.Errorf("expected expired, got %s", changed[0].Status)
	}
}

func TestEvaluateVoteTarget_Fulfilled(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, CommitVoteTarget, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		DayVoteMap: map[int]int{0: 3}, // 0号投了3号
		PlayerFactions: map[int]Faction{
			3: FactionWolf, // 3号是狼
		},
		CurrentDay: 2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitVoteTarget, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusFulfilled {
		t.Errorf("expected fulfilled, got %s", changed[0].Status)
	}
}

func TestEvaluateVoteTarget_Broken(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, CommitVoteTarget, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		DayVoteMap: map[int]int{0: 3},
		PlayerFactions: map[int]Faction{
			3: FactionGood, // 3号是好人
		},
		CurrentDay: 2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitVoteTarget, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusBroken {
		t.Errorf("expected broken, got %s", changed[0].Status)
	}
}

func TestEvaluateNoVoteFor_Fulfilled(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, CommitNoVoteFor, 7, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		DayVoteMap: map[int]int{0: 3}, // 0号投了3号（不是7号）
		CurrentDay: 1,
	}
	changed := cl.EvaluateForTriggerLocked(CommitNoVoteFor, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusFulfilled {
		t.Errorf("expected fulfilled, got %s", changed[0].Status)
	}
}

func TestEvaluateNoVoteFor_Broken(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, CommitNoVoteFor, 7, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		DayVoteMap: map[int]int{0: 7}, // 0号投了7号
		CurrentDay: 1,
	}
	changed := cl.EvaluateForTriggerLocked(CommitNoVoteFor, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusBroken {
		t.Errorf("expected broken, got %s", changed[0].Status)
	}
}

func TestEvaluateNoUseSkill_Fulfilled(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(2, 1, CommitNoUseSkill, -1, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		SkillUsedTonight: map[int]bool{2: false}, // 2号没用技能
		CurrentDay:       2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitNoUseSkill, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusFulfilled {
		t.Errorf("expected fulfilled, got %s", changed[0].Status)
	}
}

func TestEvaluateNoUseSkill_Broken(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(2, 1, CommitNoUseSkill, -1, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		SkillUsedTonight: map[int]bool{2: true}, // 2号用了技能
		CurrentDay:       2,
	}
	changed := cl.EvaluateForTriggerLocked(CommitNoUseSkill, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusBroken {
		t.Errorf("expected broken, got %s", changed[0].Status)
	}
}

func TestEvaluateApologyIfGood_Fulfilled(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, CommitApologyIfGood, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		IsGameOver: true,
		PlayerFactions: map[int]Faction{
			3: FactionGood, // 3号是好人
		},
	}
	changed := cl.EvaluateForTriggerLocked(CommitApologyIfGood, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusFulfilled {
		t.Errorf("expected fulfilled, got %s", changed[0].Status)
	}
}

func TestEvaluateApologyIfGood_Expired(t *testing.T) {
	cl := NewCommitmentLedger()
	_, err := cl.AddCommitmentLocked(0, 1, CommitApologyIfGood, 3, "", "", 3)
	if err != nil {
		t.Fatal(err)
	}

	facts := CommitFacts{
		IsGameOver: true,
		PlayerFactions: map[int]Faction{
			3: FactionWolf, // 3号是狼
		},
	}
	changed := cl.EvaluateForTriggerLocked(CommitApologyIfGood, facts)
	if len(changed) != 1 {
		t.Fatalf("expected 1 changed, got %d", len(changed))
	}
	if changed[0].Status != CommitStatusExpired {
		t.Errorf("expected expired, got %s", changed[0].Status)
	}
}

func TestGetCommitmentsForViewer_Self(t *testing.T) {
	cl := NewCommitmentLedger()
	_, _ = cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "", 3)
	// 模拟已兑现
	cl.items[0].Status = CommitStatusFulfilled

	view := cl.GetCommitmentsForViewerLocked(0) // 0号看自己
	if len(view) != 1 {
		t.Fatalf("expected 1, got %d", len(view))
	}
	if view[0].Status != CommitStatusFulfilled {
		t.Errorf("self should see real status, got %s", view[0].Status)
	}
}

func TestGetCommitmentsForViewer_Other(t *testing.T) {
	cl := NewCommitmentLedger()
	_, _ = cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "", 3)
	cl.items[0].Status = CommitStatusFulfilled

	view := cl.GetCommitmentsForViewerLocked(1) // 1号看0号
	if len(view) != 0 {
		t.Fatalf("other should not see non-pending commitments, got %d", len(view))
	}
}

func TestGetCommitmentsForViewer_OtherPending(t *testing.T) {
	cl := NewCommitmentLedger()
	_, _ = cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "", 3)

	view := cl.GetCommitmentsForViewerLocked(1) // 1号看0号的pending承诺
	if len(view) != 1 {
		t.Fatalf("expected 1, got %d", len(view))
	}
	if view[0].Status != CommitStatusPending {
		t.Errorf("other should see masked pending, got %s", view[0].Status)
	}
}

func TestGetCommitmentsForViewer_Spectator(t *testing.T) {
	cl := NewCommitmentLedger()
	_, _ = cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "", 3)
	cl.items[0].Status = CommitStatusFulfilled

	view := cl.GetCommitmentsForViewerLocked(-1) // 观战者
	if len(view) != 1 {
		t.Fatalf("expected 1, got %d", len(view))
	}
	if view[0].Status != CommitStatusFulfilled {
		t.Errorf("spectator should see real status, got %s", view[0].Status)
	}
}

func TestGetFulfillmentRate(t *testing.T) {
	cl := NewCommitmentLedger()
	// 3 条承诺：2 兑现 1 违背
	_, _ = cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "", 3)
	_, _ = cl.AddCommitmentLocked(0, 1, CommitVoteTarget, 5, "", "", 3)
	_, _ = cl.AddCommitmentLocked(0, 1, CommitNoVoteFor, 7, "", "", 3)

	cl.items[0].Status = CommitStatusFulfilled
	cl.items[1].Status = CommitStatusFulfilled
	cl.items[2].Status = CommitStatusBroken

	f, b, total, rate := cl.GetFulfillmentRateLocked(0)
	if f != 2 || b != 1 || total != 3 {
		t.Errorf("unexpected counts: f=%d b=%d total=%d", f, b, total)
	}
	expectedRate := 2.0 / 3.0
	if rate < expectedRate-0.01 || rate > expectedRate+0.01 {
		t.Errorf("expected rate ~%.2f, got %.2f", expectedRate, rate)
	}
}

func TestGetFulfillmentRate_Empty(t *testing.T) {
	cl := NewCommitmentLedger()
	f, b, total, rate := cl.GetFulfillmentRateLocked(0)
	if f != 0 || b != 0 || total != 0 || rate != 0 {
		t.Errorf("expected all zeros, got f=%d b=%d total=%d rate=%.2f", f, b, total, rate)
	}
}

func TestGetFulfillmentRates(t *testing.T) {
	cl := NewCommitmentLedger()
	_, _ = cl.AddCommitmentLocked(0, 1, CommitSeerCheck, 3, "", "", 3)
	_, _ = cl.AddCommitmentLocked(1, 1, CommitVoteTarget, 5, "", "", 3)
	cl.items[0].Status = CommitStatusFulfilled
	cl.items[1].Status = CommitStatusBroken

	rates := cl.GetFulfillmentRatesLocked()
	if len(rates) != 2 {
		t.Fatalf("expected 2, got %d", len(rates))
	}
	for _, r := range rates {
		if r.Seat == 0 && r.Rate != 1.0 {
			t.Errorf("seat 0 expected rate 1.0, got %.2f", r.Rate)
		}
		if r.Seat == 1 && r.Rate != 0.0 {
			t.Errorf("seat 1 expected rate 0.0, got %.2f", r.Rate)
		}
	}
}

func TestCommitmentJSON(t *testing.T) {
	c := &Commitment{
		ID:        1,
		Seat:      3,
		Round:     2,
		Template:  CommitSeerCheck,
		ParamSeat: 7,
		Reason:    "test",
		Status:    CommitStatusPending,
		CreatedAt: time.Now().UnixMilli(),
	}
	j := c.ToJSON()
	if j.ID != 1 || j.Seat != 3 || j.Template != CommitSeerCheck {
		t.Errorf("unexpected JSON: %+v", j)
	}
}
