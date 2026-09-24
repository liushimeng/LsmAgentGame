// Package virtual_city — survey_test.go: 社会调研生命周期单测
// (2026-09-16 §财商流P1-2,调研契约 §9.1)。
package virtual_city

import (
	"fmt"
	"strings"
	"testing"

	"LsmAgentGame/errcode"
)

// newPlayingSurveyRoom 构造 10 bot 开局中的房间(调研对象 = 全体 Agent)。
func newPlayingSurveyRoom(t *testing.T) (*VirtualCityRoom, *World) {
	t.Helper()
	r := NewVirtualCityRoom("sv-room", 3000, 11, 4)
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = fmt.Sprintf("bot-%d", seat)
		botModels[seat] = "TestModel"
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start room: %v", e)
	}
	return r, r.World
}

// mustLaunch 发起调研,失败即 Fatal。
func mustLaunch(t *testing.T, r *VirtualCityRoom, q string, opts ...string) *Survey {
	t.Helper()
	sv, e := r.LaunchSurvey(q, opts)
	if e != nil {
		t.Fatalf("launch survey: %v", e)
	}
	return sv
}

// mustBeginAgentRunner 为直接调用工具桥的测试获取当月决策令牌。
func mustBeginAgentRunner(t *testing.T, r *VirtualCityRoom, seat int) *AgentRunner {
	t.Helper()
	runner := NewAgentRunner(r, seat)
	if err := runner.BeginDecision(seat, r.Month()); err != nil {
		t.Fatalf("begin agent decision seat %d: %v", seat, err)
	}
	t.Cleanup(func() { runner.EndDecision(seat, r.Month()) })
	return runner
}

// TestSurvey_LaunchValidation 发起校验:options 1 个/7 个 → 35016;
// question 空 → 35016(§9.1)。
func TestSurvey_LaunchValidation(t *testing.T) {
	r, _ := newPlayingSurveyRoom(t)
	cases := []struct {
		name     string
		question string
		options  []string
		wantCode int
	}{
		{"empty question", "", []string{"A", "B"}, errcode.ErrVirtualCitySurveyOptionsInvalid},
		{"one option", "问", []string{"A"}, errcode.ErrVirtualCitySurveyOptionsInvalid},
		{"seven options", "问", []string{"1", "2", "3", "4", "5", "6", "7"}, errcode.ErrVirtualCitySurveyOptionsInvalid},
		{"blank option", "问", []string{"A", "  "}, errcode.ErrVirtualCitySurveyOptionsInvalid},
	}
	for _, c := range cases {
		if _, e := r.LaunchSurvey(c.question, c.options); e == nil || e.Code != c.wantCode {
			t.Errorf("%s: got %v, want code %d", c.name, e, c.wantCode)
		}
	}
	// 合法发起。
	sv := mustLaunch(t, r, "奶茶涨 20% 你还买吗?", "照买", "少买", "不买")
	if sv.ID != "SV1" {
		t.Errorf("first survey id: got %s, want SV1", sv.ID)
	}
	if sv.DeadlineMonth != sv.LaunchMonth+2 {
		t.Errorf("deadline: got %d, want %d (+2)", sv.DeadlineMonth, sv.LaunchMonth+2)
	}
	if sv.Status != SurveyOpen {
		t.Errorf("status: got %s, want open", sv.Status)
	}
	// 事件已入流。
	found := false
	for _, ev := range r.World.RecentEvents(10) {
		if ev.Type == "survey" && strings.Contains(ev.Text, "新调研") {
			found = true
		}
	}
	if !found {
		t.Error("launch event missing")
	}
}

// TestSurvey_LaunchLimits 限流:第 2 个并发 open → 35017;同月第 2 次(第 1 个
// 已关)→ 35018;第 21 个 → 35018(§9.1)。
func TestSurvey_LaunchLimits(t *testing.T) {
	r, w := newPlayingSurveyRoom(t)
	// ① 已有 open → 35017。
	mustLaunch(t, r, "Q1?", "A", "B")
	if _, e := r.LaunchSurvey("Q2?", []string{"A", "B"}); e == nil || e.Code != errcode.ErrVirtualCitySurveyOpenExists {
		t.Errorf("second open: got %v, want %d", e, errcode.ErrVirtualCitySurveyOpenExists)
	}
	// 关闭第 1 个;同月再发 → 35018(每月 1 个)。
	w.CloseAndAggregate(w.Surveys[0])
	if _, e := r.LaunchSurvey("Q2?", []string{"A", "B"}); e == nil || e.Code != errcode.ErrVirtualCitySurveyMonthlyLimit {
		t.Errorf("same-month relaunch: got %v, want %d", e, errcode.ErrVirtualCitySurveyMonthlyLimit)
	}
	// 推月 + 发起 + 关闭,循环到累计 20 个;第 21 个 → 35018。
	for i := 0; i < 19; i++ {
		w.Month++
		sv, e := r.LaunchSurvey(fmt.Sprintf("Q%d?", i+2), []string{"A", "B"})
		if e != nil {
			t.Fatalf("launch #%d: %v", i+2, e)
		}
		w.CloseAndAggregate(sv)
	}
	if len(w.Surveys) != 20 {
		t.Fatalf("surveys count: got %d, want 20", len(w.Surveys))
	}
	w.Month++
	if _, e := r.LaunchSurvey("Q21?", []string{"A", "B"}); e == nil || e.Code != errcode.ErrVirtualCitySurveyMonthlyLimit {
		t.Errorf("21st survey: got %v, want %d (cap 20)", e, errcode.ErrVirtualCitySurveyMonthlyLimit)
	}
}

// TestSurvey_AnswerAggregate 回答聚合:3 答(A/B/A)→ Counts=[2,1]、
// Percents≈[0.667,0.333]、Total=3;重复回答 → 35019;option_index 越界 → 35016;
// 关闭后回答 → 35019(§9.1)。
func TestSurvey_AnswerAggregate(t *testing.T) {
	r, w := newPlayingSurveyRoom(t)
	sv := mustLaunch(t, r, "支持哪种?", "A 方案", "B 方案")
	a0 := mustBeginAgentRunner(t, r, 0)
	a1 := mustBeginAgentRunner(t, r, 1)
	a2 := mustBeginAgentRunner(t, r, 2)

	if e := a0.AnswerSurvey(0, sv.ID, 0, "便宜实惠"); e != nil {
		t.Fatalf("answer 0: %v", e)
	}
	if e := a1.AnswerSurvey(1, sv.ID, 1, "质量优先"); e != nil {
		t.Fatalf("answer 1: %v", e)
	}
	// 越界 option_index → 35016。
	if e := a2.AnswerSurvey(2, sv.ID, 5, "x"); e == nil || errCodeOf(e) != errcode.ErrVirtualCitySurveyOptionsInvalid {
		t.Errorf("option idx 5: got %v, want %d", e, errcode.ErrVirtualCitySurveyOptionsInvalid)
	}
	if e := a2.AnswerSurvey(2, sv.ID, 0, "跟随大众"); e != nil {
		t.Fatalf("answer 2: %v", e)
	}
	// 重复回答 → 35019。
	if e := a0.AnswerSurvey(0, sv.ID, 1, "改主意"); e == nil || errCodeOf(e) != errcode.ErrVirtualCitySurveyNotFound {
		t.Errorf("duplicate answer: got %v, want %d", e, errcode.ErrVirtualCitySurveyNotFound)
	}
	// 人类座位(非 bot)不可答 → 35005。
	r.MuLock()
	r.BotSeats[3] = false
	r.MuUnlock()
	if e := NewAgentRunner(r, 3).AnswerSurvey(3, sv.ID, 0, "人类"); e == nil || errCodeOf(e) != errcode.ErrVirtualCityPlayerInactive {
		t.Errorf("human seat answer: got %v, want %d", e, errcode.ErrVirtualCityPlayerInactive)
	}
	// 10 bot 中只答了 3 个 → 仍 open(不提前关闭)。
	if sv.Status != SurveyOpen {
		t.Fatalf("survey should still be open (3/10 answered)")
	}
	// 关闭聚合。
	w.CloseAndAggregate(sv)
	res := sv.Result
	if res == nil {
		t.Fatal("result missing after close")
	}
	if res.Total != 3 || res.Counts[0] != 2 || res.Counts[1] != 1 {
		t.Errorf("counts: total=%d counts=%v, want 3 [2 1]", res.Total, res.Counts)
	}
	if diff := math_Abs(res.Percents[0] - 2.0/3.0); diff > 1e-9 {
		t.Errorf("percent[0]: got %f, want %.6f", res.Percents[0], 2.0/3.0)
	}
	if len(res.TopReasons) != 3 {
		t.Errorf("top reasons: got %d, want 3", len(res.TopReasons))
	}
	// 关闭后回答 → 35019。
	if e := a1.AnswerSurvey(1, sv.ID, 0, "补答"); e == nil || errCodeOf(e) != errcode.ErrVirtualCitySurveyNotFound {
		t.Errorf("answer after close: got %v, want %d", e, errcode.ErrVirtualCitySurveyNotFound)
	}
	// 幂等:重复关闭不重复聚合。
	total := res.Total
	w.CloseAndAggregate(sv)
	if sv.Result.Total != total {
		t.Errorf("CloseAndAggregate not idempotent: %d -> %d", total, sv.Result.Total)
	}
}

// TestSurvey_EarlyCloseWhenAllBotsAnswered 全员已答提前关闭:存活 bot 全答 →
// 立即 closed(不等 deadline,§9.1)。
func TestSurvey_EarlyCloseWhenAllBotsAnswered(t *testing.T) {
	r, w := newPlayingSurveyRoom(t)
	sv := mustLaunch(t, r, "全员表态?", "赞成", "反对")
	for seat := 0; seat < 10; seat++ {
		if e := mustBeginAgentRunner(t, r, seat).AnswerSurvey(seat, sv.ID, seat%2, "理由"); e != nil {
			t.Fatalf("answer seat %d: %v", seat, e)
		}
	}
	if sv.Status != SurveyClosed {
		t.Fatalf("survey should auto-close after all bots answered, status=%s", sv.Status)
	}
	if sv.Result == nil || sv.Result.Total != 10 {
		t.Fatalf("result: %+v, want total 10", sv.Result)
	}
	// 事件包含结果摘要。
	found := false
	for _, ev := range w.RecentEvents(5) {
		if ev.Type == "survey" && strings.Contains(ev.Text, "结果") {
			found = true
		}
	}
	if !found {
		t.Error("aggregate event missing")
	}
}

// TestSurvey_DeadlineClose deadline 关闭:LaunchMonth=5 → DeadlineMonth=7;
// 月结推进跨过 7 时 CloseSurveyIfDue 关闭并填 Result;SettleResult.ClosedSurveys
// 携带该调研(§9.1)。
func TestSurvey_DeadlineClose(t *testing.T) {
	r, w := newPlayingSurveyRoom(t)
	w.Month = 5
	sv := mustLaunch(t, r, "到期测试?", "A", "B")
	if sv.DeadlineMonth != 7 {
		t.Fatalf("deadline: got %d, want 7", sv.DeadlineMonth)
	}
	mustBeginAgentRunner(t, r, 0).AnswerSurvey(0, sv.ID, 1, "选 B")
	// 月结 1:5→6 未到期;月结 2:6→7 未到期;月结 3:7→8 > 7 → 关闭。
	for i := 0; i < 2; i++ {
		if _, res := w.SettleMonth(); len(res.ClosedSurveys) != 0 {
			t.Fatalf("settle %d: should not close yet", i)
		}
	}
	_, res := w.SettleMonth()
	if len(res.ClosedSurveys) != 1 || res.ClosedSurveys[0].ID != sv.ID {
		t.Fatalf("closed surveys: %+v, want [%s]", res.ClosedSurveys, sv.ID)
	}
	if sv.Status != SurveyClosed || sv.Result == nil || sv.Result.Total != 1 {
		t.Errorf("closed survey state: status=%s result=%+v", sv.Status, sv.Result)
	}
}

// TestSurvey_TopReasonsDedupTruncate TopReasons:重复理由去重;超 3 条取前 3;
// 单条截 30 rune(§9.1)。
func TestSurvey_TopReasonsDedupTruncate(t *testing.T) {
	w := NewWorld(1, emptyCardsFor(0))
	w.Month = 1
	long := strings.Repeat("长", 50) // 50 rune → 截 30 + …
	sv := &Survey{
		ID: "SV1", Question: "Q?", Options: []string{"A", "B"},
		LaunchMonth: 1, DeadlineMonth: 3, Status: SurveyOpen,
		Answers: map[int]*SurveyAnswer{
			0: {Seat: 0, OptionIdx: 0, Reason: "一样的理由"},
			1: {Seat: 1, OptionIdx: 0, Reason: "一样的理由"}, // 重复 → 去重
			2: {Seat: 2, OptionIdx: 0, Reason: "第二条"},
			3: {Seat: 3, OptionIdx: 1, Reason: "第三条"},
			4: {Seat: 4, OptionIdx: 1, Reason: long},
		},
	}
	w.Surveys = []*Survey{sv}
	w.CloseAndAggregate(sv)
	res := sv.Result
	if len(res.TopReasons) != 3 {
		t.Fatalf("top reasons: got %d (%v), want 3", len(res.TopReasons), res.TopReasons)
	}
	if res.TopReasons[0] != "一样的理由" || res.TopReasons[1] != "第二条" || res.TopReasons[2] != "第三条" {
		t.Errorf("top reasons order/dedup: %v", res.TopReasons)
	}
	_ = long // 第 5 条被 3 条上限截断
	// 单条截 30 rune:单独构造。
	sv2 := &Survey{
		ID: "SV2", Question: "Q?", Options: []string{"A"},
		LaunchMonth: 1, DeadlineMonth: 3, Status: SurveyOpen,
		Answers: map[int]*SurveyAnswer{0: {Seat: 0, OptionIdx: 0, Reason: long}},
	}
	w.Surveys = append(w.Surveys, sv2)
	w.CloseAndAggregate(sv2)
	if got := []rune(sv2.Result.TopReasons[0]); len(got) != 31 { // 30 rune + …
		t.Errorf("truncated reason runes: got %d, want 31", len(got))
	}
}

// TestSurvey_DisabledSwitch survey_enabled=false:LaunchSurvey → 35010;
// 既有 open 照常关闭(§9.1)。
func TestSurvey_DisabledSwitch(t *testing.T) {
	r, w := newPlayingSurveyRoom(t)
	sv := mustLaunch(t, r, "先发起?", "A", "B")
	r.SetEconomyFlags(true, false)
	if _, e := r.LaunchSurvey("再发起?", []string{"A", "B"}); e == nil || e.Code != errcode.ErrVirtualCityGateFailed {
		t.Errorf("launch when disabled: got %v, want %d (35010)", e, errcode.ErrVirtualCityGateFailed)
	}
	if e := mustBeginAgentRunner(t, r, 0).AnswerSurvey(0, sv.ID, 0, "照常作答"); e != nil {
		t.Fatalf("answer on existing open survey: %v", e)
	}
	w.CloseAndAggregate(sv)
	if sv.Status != SurveyClosed || sv.Result == nil {
		t.Error("existing open survey should still close normally")
	}
}

// math_Abs 局部包装(避免与 goods_test 的 import 重复)。
func math_Abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// errCodeOf 从 error 接口解出 errcode 码(AgentRunner 方法返回 error)。
func errCodeOf(e error) int {
	if ec, ok := e.(*errcode.Error); ok && ec != nil {
		return ec.Code
	}
	return -1
}
