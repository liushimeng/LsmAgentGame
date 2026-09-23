package wealth

import (
	"sync"
	"testing"
	"time"

	"LsmAgentGame/agent/wealthplayer"
	"LsmAgentGame/errcode"
	"LsmAgentGame/service"
)

type openingHookChatSender struct {
	mu       sync.Mutex
	messages []openingHookMessage
}

func (f *openingHookChatSender) SendFromBot(roomID, botUserID, botAccount, modelKey, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, openingHookMessage{
		userID: botUserID, account: botAccount, modelKey: modelKey, text: text,
	})
	return nil
}

func (f *openingHookChatSender) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func TestWealthRoom_StartEmitsTwelveOpeningHooks(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	chat := &openingHookChatSender{}
	r.SetChatSender(chat)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	if got := chat.len(); got != MaxSeats {
		t.Fatalf("opening hooks = %d, want %d", got, MaxSeats)
	}
	chat.mu.Lock()
	seen := make(map[string]struct{}, MaxSeats)
	for i, msg := range chat.messages {
		if msg.userID == "" || msg.account == "" || msg.modelKey == "" || msg.text == "" {
			t.Fatalf("hook %d missing identity/text: %+v", i, msg)
		}
		if _, dup := seen[msg.text]; dup {
			t.Fatalf("duplicate opening hook: seat=%d text=%q", i, msg.text)
		}
		seen[msg.text] = struct{}{}
	}
	chat.mu.Unlock()

	// 开局钩子只允许一批;重复调用不得造成 24 条聊天刷屏。
	r.mu.Lock()
	messages := r.openingHooksLocked()
	r.mu.Unlock()
	r.sendOpeningHooks(messages)
	if got := chat.len(); got != MaxSeats {
		t.Fatalf("opening hooks after retry = %d, want %d", got, MaxSeats)
	}
}

func TestWealthRoom_TranscriptMetadataUpdatesAndMarksInactive(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	before := r.SnapshotTranscripts()
	for seat := 0; seat < MaxSeats; seat++ {
		got := before[seat]
		if got.Month != 1 || got.UpdatedAt <= 0 || !got.Active {
			t.Fatalf("seat %d initial transcript = %+v, want month=1 updated>0 active", seat, got)
		}
		if got.LastDecisionSummary == "" {
			t.Fatalf("seat %d initial decision summary is empty", seat)
		}
	}

	r.mu.Lock()
	r.World.Players[10].Alive = false
	r.resetMonthFlagsLocked()
	r.mu.Unlock()
	after := r.SnapshotTranscripts()[10]
	if after.Active || after.LastDecisionSummary != "已出局,停止月度决策" {
		t.Fatalf("inactive transcript = %+v", after)
	}

	// 旧月 Agent 回写不得覆盖新月 / 出局状态。
	NewAgentRunner(r, 10).RecordTranscript(10, wealthplayer.BotTranscript{
		Month: 1, Active: true, LastDecisionSummary: "stale",
	})
	if stale := r.SnapshotTranscripts()[10]; stale.LastDecisionSummary == "stale" {
		t.Fatalf("stale month transcript was accepted: %+v", stale)
	}
}

func TestWealthRoom_MonthTimeoutWritesObservableTranscript(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	r.mu.Lock()
	r.NextMonthAt = time.Now().Add(-time.Millisecond)
	r.mu.Unlock()
	if !r.trySettle(nil) {
		t.Fatal("trySettle did not advance month")
	}
	got := r.SnapshotTranscripts()[0]
	if got.Month != 2 || got.UpdatedAt <= 0 || !got.Active {
		t.Fatalf("timeout transcript metadata = %+v", got)
	}
	if got.LastDecisionMonth != 1 {
		t.Fatalf("timeout last decision month = %d, want 1", got.LastDecisionMonth)
	}
	if got.LastDecisionSummary != "月窗结束,Agent 未提交动作,系统自动结算" ||
		got.LastToolInput != "system_timeout" {
		t.Fatalf("timeout transcript missing system fallback: %+v", got)
	}
}

func TestAgentRunner_ActionBroadcastsEventAndTranscript(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	var mu sync.Mutex
	events := make([]EventRecord, 0, 1)
	r.SetHooks(BroadcastHooks{OnEvent: func(_ string, ev EventRecord) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
	}})

	runner := NewAgentRunner(r, 0)
	if err := runner.BeginDecision(0, 1); err != nil {
		t.Fatalf("begin decision: %v", err)
	}
	defer runner.EndDecision(0, 1)
	if err := runner.SetConsumption(0, 1); err != nil {
		t.Fatalf("set consumption: %v", err)
	}
	mu.Lock()
	if len(events) != 1 {
		t.Fatalf("agent action events = %d, want 1", len(events))
	}
	ev := events[0]
	mu.Unlock()
	if ev.Month != 1 || ev.Seat != 0 || ev.Type != "action" || ev.Text == "" {
		t.Fatalf("agent action event = %+v", ev)
	}
	got := r.SnapshotTranscripts()[0]
	if got.Month != 1 || got.LastDecisionMonth != 1 || got.UpdatedAt <= 0 || !got.Active {
		t.Fatalf("action transcript metadata = %+v", got)
	}
	if got.LastToolInput != wealthplayer.ToolSetConsumption {
		t.Fatalf("action transcript tool = %q, want %q", got.LastToolInput, wealthplayer.ToolSetConsumption)
	}
}

func TestWealthAgentPublishesDecisionTranscriptBeforeSubmit(t *testing.T) {
	var calls int32
	m := NewManager(Config{
		MonthMs: 3000, AgentEnabled: true,
		AgentDecisionTimeoutSec: 5, AgentConcurrency: DefaultAgentConcurrency,
	}, fakeSubmitRegistry{p: fakeSubmitProvider{calls: &calls}})
	r := m.CreateRoom("room-agent-transcript")
	botUsers := make(map[int]string, MaxSeats)
	botModels := make(map[int]string, MaxSeats)
	for seat := 0; seat < MaxSeats; seat++ {
		botUsers[seat] = "b" + string(rune('0'+seat))
		botModels[seat] = "M" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	m.EnsureAgents(r)
	agent := r.agents[0]
	if agent == nil {
		t.Fatal("agent 0 not installed")
	}
	m.runAgentMonth(r, 0, agent)

	got := r.SnapshotTranscripts()[0]
	if got.Month != 1 || got.LastDecisionMonth != 1 || got.UpdatedAt <= 0 || !got.Active {
		t.Fatalf("agent transcript metadata = %+v", got)
	}
	if got.LastToolInput != wealthplayer.ToolSubmitMonth || got.LastDecisionSummary == "" {
		t.Fatalf("agent transcript missing submit decision: %+v", got)
	}
	if !r.World.Players[0].Submitted {
		t.Fatal("agent did not submit month")
	}
}

func TestAgentRunner_RejectsStaleMonthActionAndSubmit(t *testing.T) {
	r := newRoomWithSeats(t, MaxSeats)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	runner := NewAgentRunner(r, 0)
	if err := runner.BeginDecision(0, 1); err != nil {
		t.Fatalf("begin M1 decision: %v", err)
	}

	r.mu.Lock()
	r.NextMonthAt = time.Now().Add(-time.Millisecond)
	r.mu.Unlock()
	if !r.trySettle(nil) {
		t.Fatal("room did not settle from M1 to M2")
	}
	if r.Month() != 2 {
		t.Fatalf("month = %d, want 2", r.Month())
	}
	if err := runner.SetConsumption(0, 1); err == nil {
		t.Fatal("stale M1 action was accepted in M2")
	}
	if err := runner.SubmitMonth(0); err == nil {
		t.Fatal("stale M1 submit was accepted in M2")
	}
	if err := runner.BeginDecision(0, 2); err == nil {
		t.Fatal("new decision overwrote an active stale decision slot")
	}

	runner.EndDecision(0, 1)
	if err := runner.BeginDecision(0, 2); err != nil {
		t.Fatalf("begin M2 decision: %v", err)
	}
	defer runner.EndDecision(0, 2)
	if err := runner.SetConsumption(0, 1); err != nil {
		t.Fatalf("current M2 action: %v", err)
	}
}

func TestWealthRoom_FullAgentModeRejectsHumanWhileOpen(t *testing.T) {
	r := NewWealthRoom("room-full-agent-open", 3000, 1, 4)
	r.SetFullAgentMode(true)
	if r.GetStatus() != StatusOpen {
		t.Fatalf("status = %q, want open", r.GetStatus())
	}
	seat, _, err := r.JoinGame("human-user", "human")
	if err == nil {
		t.Fatal("open full-agent room accepted human player")
	}
	if err.Code != errcode.ErrWealthFullAgentReject {
		t.Fatalf("join error code = %d, want %d", err.Code, errcode.ErrWealthFullAgentReject)
	}
	if seat != -1 {
		t.Fatalf("human seat = %d, want -1", seat)
	}
	if _, seated := r.SeatOf("human-user"); seated {
		t.Fatal("full-agent room seated human user")
	}
}

func TestManager_CreateRoomUsesPendingOptionsForRoomConfigLog(t *testing.T) {
	m := NewManager(Config{MonthMs: 8000, Seed: 99}, nil)
	m.ApplyRoomOptions("room-pending-config", &service.WealthRoomOptions{
		MonthMs: 3000,
		Seed:    123,
	})
	r := m.CreateRoom("room-pending-config")
	// 2026-09-22 §17:roomConfigForLog 第二返回值由 pool 改为 resident_count。
	monthMs, residents := r.roomConfigForLog()
	if monthMs != 3000 || residents != 0 {
		t.Fatalf("room config for create log = (%d, %d), want (3000, 0)", monthMs, residents)
	}
}
