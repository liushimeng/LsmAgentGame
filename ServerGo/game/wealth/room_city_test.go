// Package wealth — room_city_test.go: 城市背景层房间接线测试
// (2026-09-21 §虚拟城市 契约 03 §6 / 04 §1.3-1.4)。
//
//	昵称升级(注册占位 → Start 抽卡后 AI·<Card.Name> / AI·居民N号)
//	| resident_count 建城 + view city 块
//	| 月结顺序(SettleMonth → TickMonth)
//	| 池驱动座位 EnsureAgents + 决策完成(全链路)
package wealth

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LsmAgentGame/game/wealth/profession"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// ── 昵称策略(契约 04 §1.4)──

// 注册时:池驱动座位(model_key 空)占位 AI·居民<seat>号;显式 model_key 保持
// AI·<model_key>。
func TestRegisterBotSeats_PoolSeatPlaceholderNickname(t *testing.T) {
	r := NewWealthRoom("room-nick", 3000, "curated", 7, 4)
	r.RegisterBotSeats(
		map[int]string{0: "bot-0", 1: "bot-1"},
		map[int]string{0: "", 1: "ModelA"},
		nil,
	)
	if got := r.Nicknames[0]; got != "AI·居民1号" {
		t.Fatalf("pool seat nickname = %q, want %q", got, "AI·居民1号")
	}
	if got := r.Nicknames[1]; got != "AI·ModelA" {
		t.Fatalf("explicit model seat nickname = %q, want %q", got, "AI·ModelA")
	}
}

// Start 抽卡后:池驱动座位升级;curated 卡无化名 → 回退 AI·居民N号;
// 显式 model_key 座位昵称不变。
func TestStart_UpgradePoolSeatNicknames_CuratedFallback(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	// 改造:座位 0..1 为池驱动(空 model_key),其余显式模型。
	r.mu.Lock()
	r.SeatModelKeys[0] = ""
	r.SeatModelKeys[1] = ""
	r.mu.Unlock()
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	for seat := 0; seat < 2; seat++ {
		want := "AI·居民" + string(rune('1'+seat)) + "号"
		if got := r.Nicknames[seat]; got != want {
			t.Fatalf("curated pool seat %d nickname = %q, want %q (curated 无卡名回退)", seat, got, want)
		}
	}
	if got := r.Nicknames[2]; got != "AI·MC" {
		t.Fatalf("explicit seat 2 nickname = %q, want AI·MC", got)
	}
}

// docs 池卡带化名 → 池驱动座位升级为 AI·<Card.Name>(真实职业卡人名)。
func TestStart_UpgradePoolSeatNicknames_DocsName(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过真实文档池昵称升级")
	}
	root := docsPoolRootWealth(t)
	if _, err := os.Stat(root); err != nil {
		t.Skipf("docs pool not available: %v", err)
	}
	m := NewManager(Config{MonthMs: 3000, PoolDefault: "docs", Seed: 4242}, nil)
	m.SetLoader(profession.NewLoader(root))
	r := m.CreateRoom("room-nick-docs")
	botUsers := make(map[int]string, 12)
	for seat := 0; seat < 12; seat++ {
		botUsers[seat] = "b" + string(rune('a'+seat))
	}
	r.RegisterBotSeats(botUsers, map[int]string{}, nil) // 全池驱动
	if e := r.Start(m.loader); e != nil {
		t.Fatalf("start: %v", e)
	}
	upgraded := 0
	for seat := 0; seat < 12; seat++ {
		nick := r.Nicknames[seat]
		if !strings.HasPrefix(nick, "AI·") {
			t.Fatalf("seat %d nickname %q must start with AI·", seat, nick)
		}
		if !strings.Contains(nick, "居民") {
			upgraded++ // AI·<Card.Name>
		}
	}
	if upgraded == 0 {
		t.Fatal("docs pool cards carry names — at least one seat must upgrade to AI·<Card.Name>")
	}
}

// ── 建城 + view city 块(契约 03 §6 / 04 §1.3)──

func TestStart_CityBackdropCreatedAndViewExposed(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	r.mu.Lock()
	r.ResidentCount = 5000
	r.cityVoiceEnabled = false // 测试不触发 LLM
	r.mu.Unlock()
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	if r.City == nil || r.City.ResidentCount() != 5000 {
		t.Fatalf("city backdrop not created: %+v", r.City)
	}
	snap := r.CitySnapshotView()
	if snap == nil || snap.ResidentCount != 5000 {
		t.Fatalf("city snapshot missing: %+v", snap)
	}
	// view 透出 + 池驱动座位 model_display。
	cs := BuildClientState("room-city", 0, r.Engine(), r.SnapshotSeats(), r.SnapshotNicknames(),
		r.SnapshotBotSeats(), r.SnapshotModelKeys(), r.SnapshotTranscripts(),
		r.GameStartedAtUnix(), r.NextMonthAtUnix(), snap)
	if cs.City == nil || cs.City.ResidentCount != 5000 {
		t.Fatalf("game.state.city missing: %+v", cs.City)
	}
	// 未建城房(旧形态)city 块 omit。
	r2 := newRoomWithSeats(t, 12)
	if e := r2.Start(nil); e != nil {
		t.Fatalf("start2: %v", e)
	}
	cs2 := BuildClientState("room-nocity", 0, r2.Engine(), r2.SnapshotSeats(), r2.SnapshotNicknames(),
		r2.SnapshotBotSeats(), r2.SnapshotModelKeys(), r2.SnapshotTranscripts(),
		r2.GameStartedAtUnix(), r2.NextMonthAtUnix(), r2.CitySnapshotView())
	if cs2.City != nil {
		t.Fatalf("resident_count=0 room must omit city block, got %+v", cs2.City)
	}
}

// 同 seed 同城(建城确定性;契约 03 §6「同城同人」)。
func TestStart_CityDeterministicPerSeed(t *testing.T) {
	mk := func() *WealthRoom {
		r := NewWealthRoom("room-det", 3000, "curated", 555, 4)
		botUsers := make(map[int]string, 12)
		for seat := 0; seat < 12; seat++ {
			botUsers[seat] = "b" + string(rune('a'+seat))
		}
		r.RegisterBotSeats(botUsers, nil, nil)
		r.SetResidentCount(2000)
		if e := r.Start(nil); e != nil {
			t.Fatalf("start: %v", e)
		}
		return r
	}
	a, b := mk(), mk()
	sa, sb := a.CitySnapshotView(), b.CitySnapshotView()
	if sa == nil || sb == nil {
		t.Fatal("cities not created")
	}
	if sa.ResidentCount != sb.ResidentCount || sa.TotalSavings != sb.TotalSavings ||
		sa.MedianIncome != sb.MedianIncome || sa.AvgAge != sb.AvgAge {
		t.Fatalf("same seed must rebuild identical city:\n%+v\nvs\n%+v", sa, sb)
	}
}

// 月结顺序:SettleMonth → City.TickMonth(tick 后快照演化)。
func TestTrySettle_TicksCityAfterSettleMonth(t *testing.T) {
	r := newRoomWithSeats(t, 12)
	r.mu.Lock()
	r.ResidentCount = 1000
	r.cityVoiceEnabled = false
	r.mu.Unlock()
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	before := r.CitySnapshotView()
	// 全员提交 → 提前月结路径。
	r.mu.Lock()
	for _, p := range r.World.Players {
		if p != nil {
			p.Submitted = true
		}
	}
	r.mu.Unlock()
	if !r.trySettle(func(string) {}) {
		t.Fatal("trySettle did not advance a month")
	}
	after := r.CitySnapshotView()
	if after == nil {
		t.Fatal("city lost after settle")
	}
	// 94% 就业 × 0.72 消费率:一个月后总储蓄应增长。
	if after.TotalSavings <= before.TotalSavings {
		t.Fatalf("city did not tick on settle: savings %f → %f", before.TotalSavings, after.TotalSavings)
	}
	if r.Month() < 2 {
		t.Fatalf("engine month = %d, want ≥2 (settle must precede city tick)", r.Month())
	}
}

// ── 池驱动座位 EnsureAgents + 决策完成(全链路;契约 01 §3.2 B3)──

// fakeCityProvider 池模式全链路 fake:第一轮 submit_month。
type fakeCityProvider struct{ calls *int32 }

func (f fakeCityProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	n := atomic.AddInt32(f.calls, 1)
	if n == 1 {
		return llmtypes.LLMResponse{
			StopReason: "tool_use",
			Content: []llmtypes.ContentBlock{
				{Type: "tool_use", ID: "pool_submit_1", Name: "submit_month", Input: map[string]any{}},
			},
		}, nil
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "结束"}},
	}, nil
}
func (f fakeCityProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f fakeCityProvider) ProviderType() string { return "fake-city" }

// 池可用 → 空 model_key 座位也建 Agent 并完成决策(经线路池调用)。
func TestEnsureAgents_PoolModeDecisionCompletes(t *testing.T) {
	var calls int32
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "PoolFake-model",
		Info:     llmtypes.ModelInfo{Model: "PoolFake-model"},
		Provider: fakeCityProvider{calls: &calls},
		APIKey:   "fake-key",
		Lines:    2,
	}})
	m := NewManager(Config{
		MonthMs: 3000, PoolDefault: "curated", AgentEnabled: true,
		AgentDecisionTimeoutSec: 5, AgentConcurrency: 4,
	}, fakeSubmitRegistry{p: fakeSubmitProvider{calls: &calls}})
	m.SetLinePoolSource(func() *llm.LinePool { return pool })

	r := m.CreateRoom("room-poolmode")
	botUsers := make(map[int]string, 12)
	botModels := make(map[int]string, 12)
	for seat := 0; seat < 12; seat++ {
		botUsers[seat] = "b" + string(rune('a'+seat))
		botModels[seat] = "" // 全池驱动
	}
	r.RegisterBotSeats(botUsers, botModels, nil)
	m.EnsureAgents(r)
	r.mu.Lock()
	installed := 0
	for seat := 0; seat < 12; seat++ {
		if r.agents[seat] != nil {
			installed++
		}
	}
	r.mu.Unlock()
	if installed != 12 {
		t.Fatalf("pool-driven seats must install agents, got %d/12", installed)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	r.mu.Lock()
	semCap := cap(r.agentSem)
	r.mu.Unlock()
	if semCap != 2 {
		t.Fatalf("agentSem capacity must follow pool total (2), got %d", semCap)
	}
	// 决策完成:LLM 被调用且座位提交(经 fake 池线路)。
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		submitted := 0
		for seat := 0; seat < 12; seat++ {
			if p := r.World.Players[seat]; p != nil && p.Submitted {
				submitted++
			}
		}
		r.mu.Unlock()
		if submitted >= 12 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("pool-mode agents failed to complete decisions (submit_month)")
}

// 池不可用(Total==0)→ 空 model_key 座位跳过(与旧行为一致,不空转)。
func TestEnsureAgents_NoPoolSkipsEmptyModelSeats(t *testing.T) {
	m := NewManager(Config{
		MonthMs: 3000, PoolDefault: "curated", AgentEnabled: true,
		AgentDecisionTimeoutSec: 5, AgentConcurrency: 4,
	}, fakeSubmitRegistry{p: fakeSubmitProvider{calls: new(int32)}})
	// 不注入 linePoolSource。
	r := m.CreateRoom("room-nopool")
	botUsers := map[int]string{0: "b0"}
	botModels := map[int]string{0: ""}
	r.RegisterBotSeats(botUsers, botModels, nil)
	m.EnsureAgents(r)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agents[0] != nil {
		t.Fatal("empty model_key seat must be skipped when pool unavailable")
	}
}

// 混合座位:显式 model_key 座位不受池影响。
func TestEnsureAgents_MixedSeats(t *testing.T) {
	var calls int32
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "PoolFake-model", Info: llmtypes.ModelInfo{Model: "PoolFake-model"},
		Provider: fakeCityProvider{calls: &calls}, APIKey: "k", Lines: 1,
	}})
	m := NewManager(Config{
		MonthMs: 3000, PoolDefault: "curated", AgentEnabled: true,
		AgentDecisionTimeoutSec: 5, AgentConcurrency: 4,
	}, fakeSubmitRegistry{p: fakeSubmitProvider{calls: &calls}})
	m.SetLinePoolSource(func() *llm.LinePool { return pool })
	r := m.CreateRoom("room-mixed")
	r.RegisterBotSeats(
		map[int]string{0: "b0", 1: "b1"},
		map[int]string{0: "", 1: "FixedModel"},
		nil,
	)
	m.EnsureAgents(r)
	r.mu.Lock()
	a0, a1 := r.agents[0], r.agents[1]
	r.mu.Unlock()
	if a0 == nil || a0.ModelKey != "" {
		t.Fatalf("pool seat agent missing or not pool-mode: %+v", a0)
	}
	if a1 == nil || a1.ModelKey != "FixedModel" {
		t.Fatalf("explicit seat agent must keep fixed model: %+v", a1)
	}
	if a0.ModelName != PoolModelDisplay {
		t.Fatalf("pool seat display = %q, want %q", a0.ModelName, PoolModelDisplay)
	}
}

// fakeTextProvider 恒返回一句纯文本(城市之声 fake;tool_use 形状不适用)。
type fakeTextProvider struct{}

func (f fakeTextProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "这个月想多攒点钱。"}},
	}, nil
}
func (f fakeTextProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f fakeTextProvider) ProviderType() string { return "fake-text" }

// 城市之声:线路池可用 + enabled → 月结后异步产出 city_voice 事件
// (EventRecord.type=city_voice + Backdrop 环形缓冲)。
func TestCityVoice_EventEmittedAfterSettle(t *testing.T) {
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "VoiceFake-model", Info: llmtypes.ModelInfo{Model: "VoiceFake-model"},
		Provider: fakeTextProvider{}, APIKey: "k", Lines: 1,
	}})
	r := newRoomWithSeats(t, 12)
	r.mu.Lock()
	r.ResidentCount = 300
	r.cityVoiceEnabled = true
	r.cityVoicePerMonth = 2
	r.linePoolSource = func() *llm.LinePool { return pool }
	r.mu.Unlock()
	events := make(chan EventRecord, 16)
	r.SetHooks(BroadcastHooks{OnEvent: func(_ string, ev EventRecord) {
		select {
		case events <- ev:
		default:
		}
	}})
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	r.mu.Lock()
	for _, p := range r.World.Players {
		if p != nil {
			p.Submitted = true
		}
	}
	r.mu.Unlock()
	if !r.trySettle(func(string) {}) {
		t.Fatal("settle failed")
	}
	// 城市之声经 goroutine 异步 —— 轮询 5s(期间引擎月结事件同流,跳过)。
	deadline := time.Now().Add(5 * time.Second)
	got := 0
	for time.Now().Before(deadline) && got == 0 {
		select {
		case ev := <-events:
			if ev.Type != "city_voice" {
				continue // 引擎 settle 事件(policy/settle 等)
			}
			if ev.Month < 1 || ev.Seat != -1 || !strings.Contains(ev.Text, ":") {
				t.Fatalf("bad city_voice event: %+v", ev)
			}
			got++
		case <-time.After(100 * time.Millisecond):
		}
	}
	if got == 0 {
		t.Fatal("city_voice event not emitted after settle")
	}
	// 环形缓冲随快照透出。
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.CitySnapshotView(); s != nil && len(s.Voices) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("city voice not stored in backdrop ring")
}

// cpi 缺省路径:economy 关闭时 tick 用 defaultCityCPI。
func TestCurrentCPI_Default(t *testing.T) {
	r := NewWealthRoom("room-cpi", 3000, "curated", 1, 4)
	r.mu.Lock()
	defer r.mu.Unlock()
	if got := r.currentCPILocked(); got != defaultCityCPI {
		t.Fatalf("default cpi = %f, want %f", got, defaultCityCPI)
	}
}

// tickCity rng 独立性:城市 rng 不吃引擎 rng 流(同 seed 重放引擎演化不受影响)。
func TestCityRng_IndependentFromEngineRng(t *testing.T) {
	mk := func() int64 {
		r := NewWealthRoom("room-rng", 3000, "curated", 999, 4)
		botUsers := make(map[int]string, 12)
		for seat := 0; seat < 12; seat++ {
			botUsers[seat] = "b" + string(rune('a'+seat))
		}
		r.RegisterBotSeats(botUsers, nil, nil)
		r.SetResidentCount(100)
		if e := r.Start(nil); e != nil {
			t.Fatalf("start: %v", e)
		}
		return r.rng.Int63() // 引擎 rng(发卡后)当前位置
	}
	if mk() != mk() {
		t.Fatal("engine rng stream must be unaffected by city construction")
	}
}

// 建房链路透传:WealthRoomOptions.ResidentCount 经 ApplyRoomOptions 落到房间
// (service wealthRoomConfigurer → Manager.ApplyRoomOptions 同路径)。
func TestManager_ApplyRoomOptions_ResidentCount(t *testing.T) {
	m := NewManager(Config{MonthMs: 3000, PoolDefault: "curated"}, nil)
	m.ApplyRoomOptions("room-rc", &WealthRoomOptions{MonthMs: 5000, Seed: 42, ResidentCount: 12345})
	r := m.CreateRoom("room-rc")
	if r.ResidentCount != 12345 {
		t.Fatalf("resident_count = %d, want 12345 (pending opts not applied)", r.ResidentCount)
	}
	// 负数防御为 0。
	m.ApplyRoomOptions("room-rc-neg", &WealthRoomOptions{ResidentCount: -7})
	r2 := m.CreateRoom("room-rc-neg")
	if r2.ResidentCount != 0 {
		t.Fatalf("negative resident_count must clamp to 0, got %d", r2.ResidentCount)
	}
}
