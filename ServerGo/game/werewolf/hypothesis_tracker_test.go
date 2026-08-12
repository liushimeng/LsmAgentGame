package werewolf

// hypothesis_tracker_test.go — §20260810-07 自检测试
//
// 验证 §130「声明了却从不接线」:5 处写入(W1~W5)+ 2 处读取(R1~R2)+ §135 spectator 隔离。

import (
	"strings"
	"testing"

	wwplayer "LsmAgentGame/agent/wwplayer"
)

// TestHypothesis_ParseSummary_VoteCase: T1 — LLM 输出 vote + 📊 JSON 段时,
// 正则捕获 + 写入 HypothesisStore 成功。
func TestHypothesis_ParseSummary_VoteCase(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	store := r.hypothesisStoreLocked()
	summary := `vote → 2号  📊 [{"target_seat":5,"role_guess":"werewolf","confidence":75,"supporting":"第2轮悍跳","refuting":"投票跟好人大流","updated_at":1234567890000},{"target_seat":7,"role_guess":"seer","confidence":60,"supporting":"金水真实","refuting":"逻辑跳跃","updated_at":1234567890000}]`
	store.UpdateFromDecisionSummary(3, 2, summary)
	got := store.GetLocked(3)
	if got == nil {
		t.Fatal("HypothesisTable 未写入")
	}
	if len(got.Entries) != 2 {
		t.Fatalf("期望 2 条假说,实际 %d 条", len(got.Entries))
	}
	if got.Entries[0].RoleGuess != "werewolf" || got.Entries[0].Confidence != 75 {
		t.Errorf("第 1 条假说字段错误: %+v", got.Entries[0])
	}
	if got.Entries[1].RoleGuess != "seer" || got.Entries[1].Confidence != 60 {
		t.Errorf("第 2 条假说字段错误: %+v", got.Entries[1])
	}
	if got.Round != 2 {
		t.Errorf("Round=%d, 期望 2", got.Round)
	}
}

// TestHypothesis_ParseSummary_Malformed: T2 — LLM 输出不合规 JSON 时,
// 静默丢弃,**不**panic,**不**写入。
func TestHypothesis_ParseSummary_Malformed(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	store := r.hypothesisStoreLocked()
	cases_input := []string{
		`vote → 2号  📊 not_a_json`,
		`vote → 2号  📊 [{broken json`,
		`vote → 2号 (no marker)`,
		``,
	}
	for i, s := range cases_input {
		store.UpdateFromDecisionSummary(i, 1, s)
	}
	got := store.SnapshotAllLocked()
	if len(got) != 0 {
		t.Errorf("非法输入应静默丢弃,实际写入 %d 张表", len(got))
	}
}

// TestHypothesis_Confidence_Clamp: Bonus — confidence 必须限幅 0~100。
func TestHypothesis_Confidence_Clamp(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	store := r.hypothesisStoreLocked()
	summary := `📊 [{"target_seat":5,"role_guess":"werewolf","confidence":150,"supporting":"","refuting":"","updated_at":1},{"target_seat":6,"role_guess":"villager","confidence":-20,"supporting":"","refuting":"","updated_at":1}]`
	store.UpdateFromDecisionSummary(2, 1, summary)
	got := store.GetLocked(2)
	if got == nil {
		t.Fatal("未写入")
	}
	if got.Entries[0].Confidence != 100 {
		t.Errorf("Confidence 越界 150 应被限幅为 100,实际 %d", got.Entries[0].Confidence)
	}
	if got.Entries[1].Confidence != 0 {
		t.Errorf("Confidence 越界 -20 应被限幅为 0,实际 %d", got.Entries[1].Confidence)
	}
}

// TestHypothesis_SpectatorIsolation: T3 — §135 spectator 隔离
// populateHypotheses 在 viewer>=0 时 omitempty;viewer==-1 时填充。
func TestHypothesis_SpectatorIsolation(t *testing.T) {
	r := &WerewolfRoom{State: &GameState{}}
	r.hypothesisStoreLocked().UpdateFromDecisionSummary(
		3, 2,
		`vote → 5号  📊 [{"target_seat":7,"role_guess":"werewolf","confidence":80,"supporting":"x","refuting":"y","updated_at":1}]`)

	// 玩家视角
	csPlayer := &ClientGameState{}
	r.populateHypotheses(csPlayer, 3)
	if csPlayer.BotHypotheses != nil {
		t.Errorf("§135 违规:玩家(seat=3)看到了自己之外的假说? got %+v", csPlayer.BotHypotheses)
	}

	// 观战者视角
	csSpec := &ClientGameState{}
	r.populateHypotheses(csSpec, -1)
	if len(csSpec.BotHypotheses) != 1 {
		t.Fatalf("spectator 应收到 1 个 bot 假说表,实际 %d", len(csSpec.BotHypotheses))
	}
	if csSpec.BotHypotheses[0].Seat != 3 {
		t.Errorf("BotHypotheses[0].Seat=%d, 期望 3", csSpec.BotHypotheses[0].Seat)
	}
	if len(csSpec.BotHypotheses[0].Entries) != 1 {
		t.Errorf("Entries=%d, 期望 1", len(csSpec.BotHypotheses[0].Entries))
	}
}

// TestHypothesis_SanitizeStrip: Bonus — StripFromDecisionSummary 能剔除「📊 [...]」段。
func TestHypothesis_SanitizeStrip(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`vote → 2号  📊 [{"target_seat":5,"role_guess":"werewolf","confidence":75,"supporting":"s","refuting":"r","updated_at":1}]`, `vote → 2号`},
		{`vote → 2号 (no marker)`, `vote → 2号 (no marker)`},
		{``, ``},
		{`vote → 3号 📊[{"a":1}]`, `vote → 3号`},
	}
	for _, c := range cases {
		got := StripFromDecisionSummary(c.in)
		if got != c.want {
			t.Errorf("Strip(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestHypothesis_BotTranscriptSummaryField: W2 — populateHypothesisSummary
// 在 sanitize 之前把解析结果挂到 bt.HypothesisSummary;sanitize 在玩家侧清空。
func TestHypothesis_BotTranscriptSummaryField(t *testing.T) {
	bt := &wwplayer.BotTranscript{
		Seat:                3,
		LastDecisionSummary: `vote → 2号  📊 [{"target_seat":5,"role_guess":"werewolf","confidence":75,"supporting":"s","refuting":"r","updated_at":1}]`,
	}
	r := &WerewolfRoom{State: &GameState{DayNumber: 2}}
	populateHypothesisSummary(r, bt)
	if len(bt.HypothesisSummary) != 1 {
		t.Fatalf("bt.HypothesisSummary 长度=%d, 期望 1", len(bt.HypothesisSummary))
	}
	if bt.HypothesisSummary[0].TargetSeat != 5 || bt.HypothesisSummary[0].RoleGuess != "werewolf" {
		t.Errorf("HypothesisSummary[0]=%+v, 期望 seat=5 werewolf", bt.HypothesisSummary[0])
	}

	// sanitize 后,玩家侧应清空
	sanitized := sanitizeBotTranscript(*bt, false /*isSpectator=false → 玩家*/)
	if sanitized.HypothesisSummary != nil {
		t.Errorf("§135 违规:玩家侧 HypothesisSummary 应被清空,但 got=%+v", sanitized.HypothesisSummary)
	}

	// sanitize 后,spectator 侧保留
	bt2 := &wwplayer.BotTranscript{
		Seat:                3,
		LastDecisionSummary: `vote → 2号  📊 [{"target_seat":5,"role_guess":"werewolf","confidence":75,"supporting":"s","refuting":"r","updated_at":1}]`,
	}
	populateHypothesisSummary(r, bt2)
	sanitizedSpec := sanitizeBotTranscript(*bt2, true /*isSpectator=true → 观战*/)
	if len(sanitizedSpec.HypothesisSummary) != 1 {
		t.Errorf("spectator 应保留 HypothesisSummary,实际 %+v", sanitizedSpec.HypothesisSummary)
	}
	// LastDecisionSummary 中「📊 [...]」段必须被剥离(任何时候都不能下发原始 JSON)
	if strings.Contains(sanitizedSpec.LastDecisionSummary, "📊") {
		t.Errorf("§119/§135 违规:spectator 侧 LastDecisionSummary 含 📊 段: %q", sanitizedSpec.LastDecisionSummary)
	}
}