// Package agent — consistency_check_test.go: 行为一致性校验单元测试 (§20260811-06 U4)。
//
// 覆盖 3 类规则:
//   - R1: 同 round 内身份反复跳变(high)
//   - R2: 跨 round 平民跳神(medium)
//   - R3: 投票自相矛盾(low,本批次未触发检测,留占位)
package wwplayer

import "testing"

func TestExtractRoleClaim_ChineseVillager(t *testing.T) {
	if got := extractRoleClaim("我是平民,大家小心"); got != "我是平民" {
		t.Errorf("expected 平民, got %q", got)
	}
}

func TestExtractRoleClaim_ChineseSeerJump(t *testing.T) {
	if got := extractRoleClaim("我跳预言家,昨晚查了3号是狼人"); got != "跳预言家" {
		t.Errorf("expected 跳预言家, got %q", got)
	}
}

func TestExtractRoleClaim_EnglishSeer(t *testing.T) {
	if got := extractRoleClaim("I am the seer, I checked 3 last night"); got != "i am the seer" {
		t.Errorf("expected i am the seer, got %q", got)
	}
}

func TestExtractRoleClaim_NoClaim(t *testing.T) {
	if got := extractRoleClaim("我觉得3号发言有点奇怪"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRunConsistencyCheck_R1_SameRoundMultiClaim(t *testing.T) {
	a := &Agent{
		lastTranscript: &BotTranscript{
			RoleClaims: []RoleClaim{
				{Round: 2, Claim: "我是平民"},
				{Round: 2, Claim: "跳预言家"},
			},
		},
	}
	got := runConsistencyCheckLocked(a)
	if got.Rule != "R1" {
		t.Errorf("expected R1, got %s", got.Rule)
	}
	if got.Severity != "high" {
		t.Errorf("expected high severity, got %s", got.Severity)
	}
}

func TestRunConsistencyCheck_R2_VillagerToSeerAcrossRounds(t *testing.T) {
	a := &Agent{
		lastTranscript: &BotTranscript{
			RoleClaims: []RoleClaim{
				{Round: 1, Claim: "我是平民"},
				{Round: 3, Claim: "跳预言家"},
			},
		},
	}
	got := runConsistencyCheckLocked(a)
	if got.Rule != "R2" {
		t.Errorf("expected R2, got %s", got.Rule)
	}
	if got.Severity != "medium" {
		t.Errorf("expected medium severity, got %s", got.Severity)
	}
}

func TestRunConsistencyCheck_OK_NoClaims(t *testing.T) {
	a := &Agent{lastTranscript: &BotTranscript{}}
	got := runConsistencyCheckLocked(a)
	if got.Rule != "OK" {
		t.Errorf("expected OK, got %s", got.Rule)
	}
}

func TestRunConsistencyCheck_OK_ConsistentClaims(t *testing.T) {
	a := &Agent{
		lastTranscript: &BotTranscript{
			RoleClaims: []RoleClaim{
				{Round: 1, Claim: "跳预言家"},
				{Round: 2, Claim: "跳预言家"},
			},
		},
	}
	got := runConsistencyCheckLocked(a)
	if got.Rule != "OK" {
		t.Errorf("expected OK, got %s", got.Rule)
	}
}

func TestAppendRoleClaim_FIFO_30(t *testing.T) {
	a := &Agent{lastTranscript: &BotTranscript{}}
	for i := 0; i < 35; i++ {
		a.AppendRoleClaim(i, "跳预言家")
	}
	claims := a.lastTranscript.RoleClaims
	if len(claims) != consistencyCheckMaxEntries {
		t.Errorf("expected FIFO cap %d, got %d", consistencyCheckMaxEntries, len(claims))
	}
	// 最新一条应保留
	if claims[len(claims)-1].Round != 34 {
		t.Errorf("expected last round 34, got %d", claims[len(claims)-1].Round)
	}
}

func TestAppendRoleClaim_EmptyClaimSkipped(t *testing.T) {
	a := &Agent{lastTranscript: &BotTranscript{}}
	a.AppendRoleClaim(1, "")
	if len(a.lastTranscript.RoleClaims) != 0 {
		t.Errorf("expected empty claim to be skipped")
	}
}

func TestSetLastConsistencyCheck_OK_Clears(t *testing.T) {
	a := &Agent{lastTranscript: &BotTranscript{
		LastConsistencyCheck: &ConsistencyCheckResult{Rule: "R1", Severity: "high"},
	}}
	a.SetLastConsistencyCheck(ConsistencyCheckResult{Rule: "OK"})
	if a.lastTranscript.LastConsistencyCheck != nil {
		t.Errorf("expected OK to clear LastConsistencyCheck")
	}
}
