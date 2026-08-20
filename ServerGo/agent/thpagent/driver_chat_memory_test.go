// driver_chat_memory_test.go — 2026-08-20 §B2(Memory 接线) + §B5(poker_chat 接线) 回归测试。
//
// §130 防御:本文件同时充当「接线存在性」验证 —— OnNewHandLocked(带座位表) /
// RecordPlayerAction / AppendHandResult / BluffHintFor / DecideAction 的 ChatText
// 若被改回死代码,这些测试全部失败。
package thpagent

import (
	"context"
	"errors"
	"io"
	"testing"

	"LsmAgentGame/llm/types"
)

// fakeProvider 是测试用 LLMProvider:固定返回预置响应。
type fakeProvider struct {
	resp types.LLMResponse
	err  error
}

func (f *fakeProvider) Chat(ctx context.Context, key string, req types.LLMRequest) (types.LLMResponse, error) {
	return f.resp, f.err
}

func (f *fakeProvider) ChatStream(ctx context.Context, key string, req types.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fakeProvider: stream not implemented")
}

func (f *fakeProvider) ProviderType() string { return "fake" }

// pokerResponse 构造含 poker_action(+可选 poker_chat)的 LLM 响应。
func pokerResponse(action, thought, chat string) types.LLMResponse {
	blocks := []types.ContentBlock{{
		Type: "tool_use",
		ID:   "tu_1",
		Name: "poker_action",
		Input: map[string]any{
			"action":           action,
			"amount":           float64(0),
			"internal_thought": thought,
		},
	}}
	if chat != "" {
		blocks = append(blocks, types.ContentBlock{
			Type: "tool_use",
			ID:   "tu_2",
			Name: "poker_chat",
			Input: map[string]any{
				"text": chat,
			},
		})
	}
	return types.LLMResponse{Content: blocks}
}

// registerAgentWithProvider 注册一个座位并注入 fake provider。
func registerAgentWithProvider(t *testing.T, d *Driver, roomID string, seat int, p types.LLMProvider) {
	t.Helper()
	if err := d.RegisterAgents(roomID, []SeatConfig{
		{Seat: seat, UserID: "bot-seat", ModelKey: "ModelA", ModelName: "ModelA"},
	}); err != nil {
		t.Fatalf("RegisterAgents: %v", err)
	}
	d.mu.RLock()
	a := d.rooms[roomID].agents[seat]
	d.mu.RUnlock()
	if a == nil {
		t.Fatalf("agent at seat %d not registered", seat)
	}
	a.SetProvider(p, "ModelA")
	a.SetAPIKey("test-key")
}

// ─────────────────── §B5: poker_chat 接线 ───────────────────

// TestB5_ChatDeliveredWithAction: poker_chat 通过限流时,ChatText 挂到 Action 返回。
func TestB5_ChatDeliveredWithAction(t *testing.T) {
	d := NewDriver()
	registerAgentWithProvider(t, d, "room1", 0, &fakeProvider{resp: pokerResponse("call", "跟注看看", "大家好")})
	d.mu.RLock()
	d.rooms["room1"].dispatch[0].minChatIntervalSec = 0
	d.mu.RUnlock()

	action, err := d.DecideAction(context.Background(), "room1", 0, &GameContextForAgent{RoomID: "room1", Street: "preflop"})
	if err != nil {
		t.Fatalf("DecideAction: %v", err)
	}
	if action.Type != ActCall {
		t.Errorf("action.Type = %q, want call", action.Type)
	}
	if action.ChatText != "大家好" {
		t.Errorf("action.ChatText = %q, want 大家好", action.ChatText)
	}
	if action.Thought != "跟注看看" {
		t.Errorf("action.Thought = %q, want 跟注看看", action.Thought)
	}
}

// TestB5_ChatRateLimitedButActionApplied: chat 超每手 2 次限流时被丢弃,
// 但 action 照常应用(工具协议 §5)。这是「chat 丢弃 ≠ 动作失败」的回归锚点。
func TestB5_ChatRateLimitedButActionApplied(t *testing.T) {
	d := NewDriver()
	registerAgentWithProvider(t, d, "room1", 0, &fakeProvider{resp: pokerResponse("call", "t", "发言")})
	d.mu.RLock()
	d.rooms["room1"].dispatch[0].minChatIntervalSec = 0
	d.mu.RUnlock()

	ctx := &GameContextForAgent{RoomID: "room1", Street: "preflop"}
	// 第 1、2 次 chat 应通过
	for i := 0; i < 2; i++ {
		action, err := d.DecideAction(context.Background(), "room1", 0, ctx)
		if err != nil {
			t.Fatalf("DecideAction #%d: %v", i, err)
		}
		if action.ChatText != "发言" {
			t.Fatalf("chat #%d should pass rate limit, got %q", i+1, action.ChatText)
		}
		d.OnNewRoundLocked("room1", 0) // 每轮 poker_action 限流复位
	}
	// 第 3 次 chat 超限 → 丢弃,但 action 仍是 call
	action, err := d.DecideAction(context.Background(), "room1", 0, ctx)
	if err != nil {
		t.Fatalf("DecideAction #3: %v", err)
	}
	if action.ChatText != "" {
		t.Errorf("chat #3 should be dropped by rate limit, got %q", action.ChatText)
	}
	if action.Type != ActCall {
		t.Errorf("action must still apply when chat dropped, got %q", action.Type)
	}
}

// TestB5_ChatDeduped: 整段复读文本被 agentcore.DedupSpeakText 合并后才广播。
func TestB5_ChatDeduped(t *testing.T) {
	// 构造「同句复读」:同一 chunk 连续重复 — DedupSpeakText 的相邻去重会合并
	repeat := "加注试探。加注试探。加注试探。"
	d := NewDriver()
	registerAgentWithProvider(t, d, "room1", 0, &fakeProvider{resp: pokerResponse("call", "t", repeat)})
	d.mu.RLock()
	d.rooms["room1"].dispatch[0].minChatIntervalSec = 0
	d.mu.RUnlock()

	action, err := d.DecideAction(context.Background(), "room1", 0, &GameContextForAgent{RoomID: "room1", Street: "preflop"})
	if err != nil {
		t.Fatalf("DecideAction: %v", err)
	}
	// 去重后应只剩一句
	if action.ChatText != "加注试探。" {
		t.Errorf("deduped chat = %q, want 加注试探。", action.ChatText)
	}
}

// TestB5_LLMErrorFoldsWithoutChat: LLM 调用失败 → fold 兜底,无 ChatText。
func TestB5_LLMErrorFoldsWithoutChat(t *testing.T) {
	d := NewDriver()
	registerAgentWithProvider(t, d, "room1", 0, &fakeProvider{err: errors.New("upstream 500")})
	action, err := d.DecideAction(context.Background(), "room1", 0, &GameContextForAgent{RoomID: "room1"})
	if err != nil {
		t.Fatalf("DecideAction should not error on LLM failure: %v", err)
	}
	if action.Type != ActFold {
		t.Errorf("LLM failure should force fold, got %q", action.Type)
	}
	if action.ChatText != "" {
		t.Errorf("no chat on LLM failure, got %q", action.ChatText)
	}
}

// ─────────────────── §B2: Memory 接线 ───────────────────

// TestB2_OnNewHandLocked_ResetsAndCounts: 每手开始 → ResetCurrentHand + 对其他入座玩家
// IncrementHandsPlayed(含人类座位)。
func TestB2_OnNewHandLocked_ResetsAndCounts(t *testing.T) {
	d := NewDriver()
	if err := d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "A", ModelName: "A"},
		{Seat: 2, UserID: "bot2", ModelKey: "B", ModelName: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	seats := [6]string{"bot0", "human1", "bot2", "", "", ""}
	d.OnNewHandLocked("room1", seats)
	d.OnNewHandLocked("room1", seats)

	d.mu.RLock()
	mem0 := d.rooms["room1"].memories[0]
	mem2 := d.rooms["room1"].memories[2]
	d.mu.RUnlock()

	st := mem0.OpponentStatSnapshot("human1")
	if st == nil || st.HandsPlayed != 2 {
		t.Errorf("bot0 memory: human1 HandsPlayed = %+v, want 2", st)
	}
	st = mem0.OpponentStatSnapshot("bot2")
	if st == nil || st.HandsPlayed != 2 {
		t.Errorf("bot0 memory: bot2 HandsPlayed = %+v, want 2", st)
	}
	st = mem2.OpponentStatSnapshot("bot0")
	if st == nil || st.HandsPlayed != 2 {
		t.Errorf("bot2 memory: bot0 HandsPlayed = %+v, want 2", st)
	}
	// ResetCurrentHand: 记录动作后 OnNewHandLocked 应清空
	mem0.RecordAction(ActionRecordForMemory{Seat: 0, ActionType: "call"})
	if n := len(mem0.CurrentHandActionsSnapshot()); n != 1 {
		t.Fatalf("precondition: 1 action recorded, got %d", n)
	}
	d.OnNewHandLocked("room1", seats)
	if n := len(mem0.CurrentHandActionsSnapshot()); n != 0 {
		t.Errorf("ResetCurrentHand not wired: still %d actions", n)
	}
}

// TestB2_RecordPlayerAction: 行动者的动作写入所有其他 bot 的 OpponentStat +
// 所有 bot(含行动者自己)的 CurrentHandActions。
func TestB2_RecordPlayerAction(t *testing.T) {
	d := NewDriver()
	if err := d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "A", ModelName: "A"},
		{Seat: 1, UserID: "bot1", ModelKey: "B", ModelName: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	d.RecordPlayerAction("room1", 0, "bot0", "fold", 0, "preflop")

	d.mu.RLock()
	mem0 := d.rooms["room1"].memories[0]
	mem1 := d.rooms["room1"].memories[1]
	d.mu.RUnlock()

	// bot1 的画像: bot0 fold +1
	st := mem1.OpponentStatSnapshot("bot0")
	if st == nil || st.TotalFold != 1 {
		t.Errorf("mem1 opponent stat for bot0 = %+v, want TotalFold=1", st)
	}
	// 两个 bot 的本手时间线都有该动作
	if n := len(mem0.CurrentHandActionsSnapshot()); n != 1 {
		t.Errorf("mem0 current hand actions = %d, want 1", n)
	}
	if n := len(mem1.CurrentHandActionsSnapshot()); n != 1 {
		t.Errorf("mem1 current hand actions = %d, want 1", n)
	}
	// 未注册房间静默 no-op(不 panic)
	d.RecordPlayerAction("no-such-room", 0, "bot0", "fold", 0, "preflop")
}

// TestB2_AppendHandResult: 手牌结束 → 每个 bot AppendHand + 对手净盈亏/胜场。
func TestB2_AppendHandResult(t *testing.T) {
	d := NewDriver()
	if err := d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "A", ModelName: "A"},
		{Seat: 1, UserID: "bot1", ModelKey: "B", ModelName: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	community := [5]int{1, 2, 3, 4, 5}
	seats := []HandSeatSummary{
		{Seat: 0, UserID: "bot0", Hole: [2]int{49, 50}, NetDelta: 400, Won: true},
		{Seat: 1, UserID: "bot1", Hole: [2]int{21, 2}, NetDelta: -400, Won: false},
	}
	d.AppendHandResult("room1", 7, community, 5, []int{0}, seats)

	d.mu.RLock()
	mem0 := d.rooms["room1"].memories[0]
	mem1 := d.rooms["room1"].memories[1]
	d.mu.RUnlock()

	hands := mem0.RecentHandsSnapshot()
	if len(hands) != 1 || hands[0].HandNumber != 7 || hands[0].NetChipDelta != 400 {
		t.Errorf("mem0 recent hands = %+v, want hand #7 delta +400", hands)
	}
	hands1 := mem1.RecentHandsSnapshot()
	if len(hands1) != 1 || hands1[0].NetChipDelta != -400 {
		t.Errorf("mem1 recent hands = %+v, want delta -400", hands1)
	}
	// 对手画像: bot0 视角的 bot1 净盈亏 -400、0 胜
	st := mem0.OpponentStatSnapshot("bot1")
	if st == nil || st.NetChips != -400 || st.HandsWon != 0 {
		t.Errorf("mem0 opponent stat bot1 = %+v, want NetChips=-400 HandsWon=0", st)
	}
	st = mem1.OpponentStatSnapshot("bot0")
	if st == nil || st.NetChips != 400 || st.HandsWon != 1 {
		t.Errorf("mem1 opponent stat bot0 = %+v, want NetChips=+400 HandsWon=1", st)
	}
}

// TestB2_BluffHintFor: 无数据 → 0.15 中性默认;对手全弃牌 → 0.35(多偷)。
func TestB2_BluffHintFor(t *testing.T) {
	d := NewDriver()
	if err := d.RegisterAgents("room1", []SeatConfig{
		{Seat: 0, UserID: "bot0", ModelKey: "A", ModelName: "A"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := d.BluffHintFor("room1", 0); got != 0.15 {
		t.Errorf("no data: BluffHintFor = %f, want 0.15", got)
	}
	if got := d.BluffHintFor("no-such-room", 0); got != 0.15 {
		t.Errorf("no room: BluffHintFor = %f, want 0.15", got)
	}
	// 制造「对手必弃牌」画像: 先计手数再全部 fold
	d.OnNewHandLocked("room1", [6]string{"bot0", "human1", "", "", "", ""})
	d.RecordPlayerAction("room1", 1, "human1", "fold", 0, "preflop")
	d.RecordPlayerAction("room1", 1, "human1", "fold", 0, "flop")
	// human1 foldRate = 1.0 → BluffFrequency(1.0) = 0.35
	if got := d.BluffHintFor("room1", 0); got != 0.35 {
		t.Errorf("sticky-opponent: BluffHintFor = %f, want 0.35", got)
	}
}
