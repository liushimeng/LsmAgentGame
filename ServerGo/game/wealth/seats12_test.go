// Package wealth — seats12_test.go: 12 座扩容集成测试(2026-09-16 §12 座扩容)。
//
// 覆盖任务要求的三个核心场景:
//  1. 10 bot 全 Agent 房可开局并跑完 ≥1 个月(MinSeats 边界);
//  2. 12 座满员开局(MaxSeats 边界);
//  3. 9 座拒绝开局返回 ErrWealthNotEnoughPlayers(35003)。
//
// 2026-09-22 §17-CityHuman(契约 03 §4):原精选池数量门禁随精选卡层退役
// 删除;新增「wealth 建房恒 12 深度座位」断言(发卡池恒满 MaxSeats、来源
// 恒 docs-or-synthetic;12 深度座位的服务端合成由
// service/room_service_wealth_fill_test.go 的 wealthDeepSeats 断言)。
package wealth

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/wealth/profession"
	llmtypes "LsmAgentGame/llm/types"
)

// TestSeats12_DeepSeatCardPoolAlwaysFull wealth 建房恒 12 深度座位:
// 无 loader(合成兜底)时发卡池也必须恒满 MaxSeats(12) 张、互不重复、
// 全部 Validate 通过且 Source=synthetic(精选层退役后的兜底,契约 03 §2.2)。
func TestSeats12_DeepSeatCardPoolAlwaysFull(t *testing.T) {
	if MaxSeats != 12 {
		t.Fatalf("MaxSeats = %d, want 12 (深度层固定 12)", MaxSeats)
	}
	r := NewWealthRoom("room-deep12", 3000, 7, 4)
	r.mu.Lock()
	cards := r.buildCardPoolLocked()
	r.mu.Unlock()
	if len(cards) != MaxSeats {
		t.Fatalf("card pool = %d, want %d (12 深度座位发卡恒满)", len(cards), MaxSeats)
	}
	seen := map[string]struct{}{}
	for i, c := range cards {
		if _, dup := seen[c.ID]; dup {
			t.Fatalf("card %d duplicate id %s", i, c.ID)
		}
		seen[c.ID] = struct{}{}
		if c.Source != "synthetic" {
			t.Errorf("card %d source = %q, want synthetic (无 loader 时合成兜底)", i, c.Source)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("card %d (%s) invalid: %v", i, c.ID, err)
		}
	}
}

// TestSeats12_StartRejects9Seats 9 座(< MinSeats 10)拒绝开局,返回 35003。
func TestSeats12_StartRejects9Seats(t *testing.T) {
	r := newRoomWithSeats(t, 9)
	e := r.Start(nil)
	if e == nil {
		t.Fatal("9 seats should be rejected")
	}
	if e.Code != errcode.ErrWealthNotEnoughPlayers {
		t.Fatalf("9 seats: code = %d, want %d (ErrWealthNotEnoughPlayers)", e.Code, errcode.ErrWealthNotEnoughPlayers)
	}
}

// TestSeats12_StartsAt10Seats 10 座(= MinSeats)可开局,10 玩家全部注入且职业互不重复。
func TestSeats12_StartsAt10Seats(t *testing.T) {
	r := newRoomWithSeats(t, 10)
	if e := r.Start(nil); e != nil {
		t.Fatalf("10 seats start: %v", e)
	}
	if r.World == nil {
		t.Fatal("World not initialized")
	}
	occupied := 0
	seen := map[string]struct{}{}
	for _, p := range r.World.Players {
		if p == nil {
			continue
		}
		occupied++
		if _, dup := seen[p.Card.ID]; dup {
			t.Fatalf("duplicate profession %s", p.Card.ID)
		}
		seen[p.Card.ID] = struct{}{}
	}
	if occupied != 10 {
		t.Fatalf("occupied = %d, want 10", occupied)
	}
	if r.World.Ledger.WorldInjectCount() != 10 {
		t.Fatalf("initial inject count = %d, want 10", r.World.Ledger.WorldInjectCount())
	}
}

// TestSeats12_StartsAt12Seats 12 座满员(= MaxSeats)开局,12 玩家互不重复职业。
func TestSeats12_StartsAt12Seats(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	if e := r.Start(nil); e != nil {
		t.Fatalf("12 seats start: %v", e)
	}
	seen := map[string]struct{}{}
	occupied := 0
	for _, p := range r.World.Players {
		if p == nil {
			continue
		}
		occupied++
		if _, dup := seen[p.Card.ID]; dup {
			t.Fatalf("duplicate profession %s at 12-seat room", p.Card.ID)
		}
		seen[p.Card.ID] = struct{}{}
	}
	if occupied != 12 {
		t.Fatalf("occupied = %d, want 12", occupied)
	}
}

// TestSeats12_TenBotFullAgentRoom_RunsOneMonth 10 bot 全 Agent 房(创建者观战)
// 可开局并跑完 ≥1 个月 —— 任务「最少 10 个 Agent 跑全场」的核心验收。
//
// 用 fakeSubmitProvider 让 bot 第一轮就返回 submit_month(无需真实 LLM);
// 月窗口 3s,跑 2 个月后 month 应 ≥2。
func TestSeats12_TenBotFullAgentRoom_RunsOneMonth(t *testing.T) {
	var calls int32
	m := NewManager(Config{
		MonthMs:                 3000,
		AgentEnabled:            true,
		AgentDecisionTimeoutSec: 5,
		AgentConcurrency:        DefaultAgentConcurrency,
	}, fakeSubmitRegistry{p: fakeSubmitProvider{calls: &calls}})

	r := m.CreateRoom("room-10bot")
	// 10 bot 全 Agent 房:注册 10 bot 座位(创建者降级为观战者,不 JoinGame)。
	botUsers := make(map[int]string, 10)
	botModels := make(map[int]string, 10)
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = "b" + string(rune('0'+seat))
		botModels[seat] = "M" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	// 与真实 ws startWealthRoom 时序一致:先装配 agents,再 Start。
	// Start 末尾会自动 wakeBots;若后装配,首月 wake 时 agents map 为空。
	m.EnsureAgents(r)
	r.mu.Lock()
	for seat := 0; seat < 10; seat++ {
		if r.agents[seat] == nil {
			r.mu.Unlock()
			t.Fatalf("seat %d agent must be installed before Start", seat)
		}
	}
	r.mu.Unlock()
	if e := r.Start(nil); e != nil {
		t.Fatalf("10 bot start: %v", e)
	}
	// 启动月度主循环(驱动 settleCh → SettleMonth → month++);后台 goroutine,
	// 测试退出时 Close 兜底(房间不持久化,无需 onFinish 清理)。
	go r.RunLoop(func(roomID string) {})
	t.Cleanup(func() { r.Close() })

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if r.Month() >= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if r.Month() < 2 {
		t.Fatalf("month = %d, want ≥2 (10 bot room failed to advance)", r.Month())
	}
	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("bot never called LLM")
	}
}

// TestSeats12_DistrictsContractGuard 城区静态表契约守卫
// (v2.11 12 座扩容: 城区数与座位数无关;v2.12 阶段 2: 8 城区 → 16 城区,
// 前 8 个 P0 城区的 id/顺序不可修改 —— 与前端 types/wealth.ts
// WEALTH_DISTRICTS 顺序完全一致,§130 契约对齐)。
func TestSeats12_DistrictsEightUnchanged(t *testing.T) {
	if DistrictCount != 16 {
		t.Fatalf("DistrictCount = %d, want 16 (v2.12 phase-2 expansion)", DistrictCount)
	}
	if len(DistrictDefs) != 16 {
		t.Fatalf("len(DistrictDefs) = %d, want 16", len(DistrictDefs))
	}
	// 前 8 区 P0 契约:id 与顺序冻结(扩展只允许追加,不允许改写)。
	wantFirst8 := [8]string{
		"finance", "tech", "industry", "oldtown",
		"commerce", "residential", "suburb", "riverside",
	}
	for i, want := range wantFirst8 {
		if got := DistrictDefs[i].ID; got != want {
			t.Fatalf("DistrictDefs[%d].ID = %q, want %q (P0 前 8 区顺序冻结)", i, got, want)
		}
	}
}

// newRoomWithSeats 构造一个已入座 n 个 bot 的房(无 loader/无 LLM 注册表;
// 发卡走合成兜底)。
func newRoomWithSeats(t *testing.T, n int) *WealthRoom {
	t.Helper()
	if n < 0 || n > MaxSeats {
		t.Fatalf("n=%d out of [0,%d]", n, MaxSeats)
	}
	r := NewWealthRoom("room-seats", 3000, 7, 4)
	botUsers := make(map[int]string, n)
	botModels := make(map[int]string, n)
	for seat := 0; seat < n; seat++ {
		botUsers[seat] = "b" + string(rune('0'+seat))
		botModels[seat] = "M" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	return r
}

// docsPoolRoot 返回真实文档池根目录(仓库根 + lag_docs/虚拟城市/玩家职业设计)。
// 用 runtime.Caller 定位本文件,再上溯 3 层到仓库根:
// ServerGo/game/wealth → ServerGo/game → 仓库根。
func docsPoolRootWealth(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable; cannot locate docs pool")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "虚拟城市", "玩家职业设计")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs(%s): %v", root, err)
	}
	return abs
}

// TestSeats12_TwelveSeatRoomDraws12DocsCards 12 座全 Agent 房必须从**文档池**
// 抽到 12 张真实卡开局(Source == "docs",不许回退合成兜底)—— 这是任务
// 「12 座位全 Agent 房必须从文档池抽到 12 张真实卡开局」的 room 级验收。
func TestSeats12_TwelveSeatRoomDraws12DocsCards(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实文档池 12 座开局")
	}
	root := docsPoolRootWealth(t)
	if _, err := os.Stat(root); err != nil {
		t.Skipf("docs pool not available at %s: %v", root, err)
	}
	m := NewManager(Config{MonthMs: 3000, Seed: 42}, nil)
	m.SetLoader(profession.NewLoader(root))
	r := m.CreateRoom("room-12docs")
	// 12 bot 全 Agent 房(创建者观战)。
	botUsers := make(map[int]string, 12)
	botModels := make(map[int]string, 12)
	for seat := 0; seat < 12; seat++ {
		botUsers[seat] = "b" + string(rune('0'+seat))
		botModels[seat] = "M" + string(rune('A'+seat))
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(m.loader); e != nil {
		t.Fatalf("12-seat docs start: %v", e)
	}
	if r.World == nil {
		t.Fatal("World not initialized")
	}
	docsCount, fallbackCount := 0, 0
	seen := map[string]struct{}{}
	for _, p := range r.World.Players {
		if p == nil {
			t.Fatalf("seat has nil player(零值卡 / 抽卡不足): 12 座房出现座位空洞")
		}
		if p.Card.Source == "docs" {
			docsCount++
		} else {
			fallbackCount++
		}
		if _, dup := seen[p.Card.ID]; dup {
			t.Fatalf("duplicate card %s", p.Card.ID)
		}
		seen[p.Card.ID] = struct{}{}
	}
	t.Logf("12-seat room cards: docs=%d fallback=%d", docsCount, fallbackCount)
	if docsCount != 12 {
		t.Fatalf("docs cards = %d, want 12(不许回退合成兜底)", docsCount)
	}
}

// ── 测试用 fake LLM ──

// fakeSubmitProvider 第一轮返回 submit_month tool_use,之后纯文本结束。
type fakeSubmitProvider struct{ calls *int32 }

func (f fakeSubmitProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	n := atomic.AddInt32(f.calls, 1)
	if n == 1 {
		return llmtypes.LLMResponse{
			StopReason: "tool_use",
			Content: []llmtypes.ContentBlock{
				{Type: "tool_use", ID: "call_submit_1", Name: "submit_month", Input: map[string]any{}},
			},
		}, nil
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "结束本月"}},
	}, nil
}
func (f fakeSubmitProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f fakeSubmitProvider) ProviderType() string { return "fake-submit" }

type fakeSubmitRegistry struct{ p llmtypes.LLMProvider }

func (f fakeSubmitRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return f.p, "fake-key", nil
}
func (f fakeSubmitRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }
