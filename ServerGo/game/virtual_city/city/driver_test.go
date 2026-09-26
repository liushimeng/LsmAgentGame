// Package city — driver_test.go: 居民驱动层测试(2026-09-22 §17-CityHuman
// 全民驱动,契约 02 §9 八用例)。
//
//	预算(PerMonth=5 → 恰 5 次 LLM;5 条 speak → 5 条 VoiceRecord)
//	| 线程池(Workers=4 + PerMonth=16 → 峰值并发 ≤4)
//	| 线路全忙(Acquire 恒失败 → 不 panic、不阻塞、lastDriven=0)
//	| 轮转公平(两月 PerMonth=2、N=4 → 并集 = 全员、无重复)
//	| 意图演化(job_seeking 再就业率上升;次月 intent 清零)
//	| move_out | 回退开关(Enabled=false 零调用) | 驱动开关下发(omitempty)
package city

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// fakeDriverProvider 驱动层 fake:每次 Chat 记录并发峰值,返回一条 speak
// tool_use(契约 §9「5 条 speak → 5 条 VoiceRecord」)。sleepMS>0 时模拟
// LLM 耗时(线程池并发观测)。
type fakeDriverProvider struct {
	mu       sync.Mutex
	calls    int
	inFlight int
	peak     int
	sleepMS  int
}

func (f *fakeDriverProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	f.calls++
	f.inFlight++
	if f.inFlight > f.peak {
		f.peak = f.inFlight
	}
	f.mu.Unlock()
	if f.sleepMS > 0 {
		time.Sleep(time.Duration(f.sleepMS) * time.Millisecond)
	}
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	return llmtypes.LLMResponse{
		StopReason: "tool_use",
		Content: []llmtypes.ContentBlock{
			{Type: "tool_use", ID: fmt.Sprintf("drv_%d", f.calls), Name: "speak",
				Input: map[string]any{"text": "这个月想多攒点钱,少点外卖。"}},
		},
	}, nil
}

func (f *fakeDriverProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, fmt.Errorf("fake: no stream")
}
func (f *fakeDriverProvider) ProviderType() string { return "fake-driver" }

// fakeCalls 并发安全读取调用计数/峰值。
func (f *fakeDriverProvider) stats() (calls, peak int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.peak
}

// newDriverPool 构建 lines 条线路的 fake 线路池。
func newDriverPool(p llmtypes.LLMProvider, lines int) func() *llm.LinePool {
	specs := make([]llm.LineSpec, 0, 1)
	if lines > 0 {
		specs = append(specs, llm.LineSpec{
			ModelKey: "DriverFake-model",
			Info:     llmtypes.ModelInfo{Model: "DriverFake-model"},
			Provider: p,
			APIKey:   "fake-key",
			Lines:    lines,
		})
	}
	pool := llm.NewLinePool(specs)
	return func() *llm.LinePool { return pool }
}

// syncVoiceSink 并发安全收集 VoiceRecord(onVoice 回调来自多 worker)。
type syncVoiceSink struct {
	mu      sync.Mutex
	records []VoiceRecord
}

func (s *syncVoiceSink) collect(vr VoiceRecord) {
	s.mu.Lock()
	s.records = append(s.records, vr)
	s.mu.Unlock()
}
func (s *syncVoiceSink) all() []VoiceRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]VoiceRecord(nil), s.records...)
}

// ① 预算:PerMonth=5 → 恰 5 次 LLM 调用;5 条 speak → 5 条 VoiceRecord。
func TestResidentDriver_BudgetPerMonth(t *testing.T) {
	fp := &fakeDriverProvider{}
	d := NewResidentDriver(DriverConfig{Enabled: true, Workers: 2, PerMonth: 5}, newDriverPool(fp, 4))
	b := NewBackdrop(50, rand.New(rand.NewSource(21)), nil)
	sink := &syncVoiceSink{}
	done := make(chan struct{})
	go func() {
		d.RunMonth(b, 3, sink.collect)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunMonth blocked")
	}
	if calls, _ := fp.stats(); calls != 5 {
		t.Fatalf("llm calls = %d, want exactly 5 (PerMonth budget)", calls)
	}
	if got := len(sink.all()); got != 5 {
		t.Fatalf("voice records = %d, want 5", got)
	}
	if ds := d.Snapshot(); ds.DrivenLast != 5 || ds.PerMonth != 5 || ds.Workers != 2 || !ds.Enabled {
		t.Fatalf("driver snapshot = %+v, want driven=5 perMonth=5 workers=2 enabled", ds)
	}
	for _, vr := range sink.all() {
		if vr.Month != 3 || vr.Text == "" || vr.Name == "" || vr.ModelKey != "DriverFake-model" {
			t.Fatalf("bad voice record: %+v", vr)
		}
		if len([]rune(vr.Text)) > driverSpeakMaxRunes {
			t.Fatalf("speak text over %d runes: %q", driverSpeakMaxRunes, vr.Text)
		}
	}
	// 快照透出:Backdrop.SetDriver 后 Snapshot.Driver 下发。
	b.SetDriver(d)
	if s := b.Snapshot(); s.Driver == nil || s.Driver.DrivenLast != 5 {
		t.Fatalf("snapshot driver block missing/drifted: %+v", s.Driver)
	}
}

// ② 线程池:Workers=4 + PerMonth=16 → 峰值并发 ≤4(线路池给 8 条,
// 排除线路池先卡上限的干扰 —— 唯一约束来源是线程池)。
func TestResidentDriver_WorkerConcurrencyCap(t *testing.T) {
	fp := &fakeDriverProvider{sleepMS: 25}
	d := NewResidentDriver(DriverConfig{Enabled: true, Workers: 4, PerMonth: 16}, newDriverPool(fp, 8))
	b := NewBackdrop(100, rand.New(rand.NewSource(22)), nil)
	done := make(chan struct{})
	go func() {
		d.RunMonth(b, 1, func(VoiceRecord) {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunMonth blocked")
	}
	calls, peak := fp.stats()
	if calls != 16 {
		t.Fatalf("llm calls = %d, want 16", calls)
	}
	if peak > 4 {
		t.Fatalf("peak concurrency = %d, want ≤ 4 (thread pool cap)", peak)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency = %d, want > 1 (workers must run concurrently)", peak)
	}
}

// ③ 线路全忙:唯一线路被外部持有 → Acquire 恒 ErrAllLinesBusy → 不 panic、
// 不阻塞(短超时)、lastDriven=0。
func TestResidentDriver_AllLinesBusy(t *testing.T) {
	fp := &fakeDriverProvider{}
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "DriverFake-model", Info: llmtypes.ModelInfo{Model: "DriverFake-model"},
		Provider: fp, APIKey: "k", Lines: 1,
	}})
	// 外部持住唯一线路 → 驱动层 Acquire 只能等超时。
	held, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("pre-acquire: %v", err)
	}
	defer held.Release()
	d := NewResidentDriver(DriverConfig{Enabled: true, Workers: 2, PerMonth: 4, AcquireTimeoutMS: 80},
		func() *llm.LinePool { return pool })
	b := NewBackdrop(30, rand.New(rand.NewSource(23)), nil)
	t0 := time.Now()
	done := make(chan struct{})
	go func() {
		d.RunMonth(b, 2, func(VoiceRecord) { t.Error("no voice expected when all lines busy") })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunMonth blocked on busy lines")
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Fatalf("RunMonth took %v, want fast-fail", d)
	}
	if calls, _ := fp.stats(); calls != 0 {
		t.Fatalf("llm calls = %d, want 0", calls)
	}
	if ds := d.Snapshot(); ds.DrivenLast != 0 {
		t.Fatalf("driven_last = %d, want 0 (nothing completed)", ds.DrivenLast)
	}
}

// ④ 轮转公平:两月 PerMonth=2、N=4 → 两月 picks 并集 = 全员、无重复。
func TestResidentDriver_RotationFairness(t *testing.T) {
	b := NewBackdrop(4, rand.New(rand.NewSource(24)), nil)
	var cursor uint64
	seen := map[int]struct{}{}
	for round := 0; round < 2; round++ {
		picks := b.PickDriverCandidates(2, &cursor)
		if len(picks) != 2 {
			t.Fatalf("round %d picks = %v, want 2", round, picks)
		}
		for _, idx := range picks {
			if _, dup := seen[idx]; dup {
				t.Fatalf("resident %d picked twice across rotation window", idx)
			}
			seen[idx] = struct{}{}
		}
	}
	if len(seen) != 4 {
		t.Fatalf("union = %v, want all 4 residents covered", seen)
	}
	// cursor 推进量 = 2×PerMonth。
	if cursor != 4 {
		t.Fatalf("cursor = %d, want 4", cursor)
	}
}

// ⑤ 意图演化:set_intent(job_seeking) 后 TickMonth 失业者再就业率显著上升
// (≈30% vs 对照 ≈15%);次月 intent 清零(增量回落基线)。
func TestResidentDriver_IntentEvolution(t *testing.T) {
	const n = 4000
	mk := func(withIntent bool) *Backdrop {
		b := NewBackdrop(n, rand.New(rand.NewSource(25)), nil)
		b.mu.Lock()
		for i := range b.residents {
			b.residents[i].flags &^= flagEmployed | flagStressed
		}
		b.mu.Unlock()
		if withIntent {
			for i := 0; i < n; i++ {
				b.ApplyIntent(i, "job_seeking", "")
			}
		}
		return b
	}
	tr := rand.New(rand.NewSource(99))
	with := mk(true)
	with.TickMonth(0, tr)
	rateWith := with.Snapshot().EmploymentRate
	if rateWith < 0.26 || rateWith > 0.34 {
		t.Fatalf("job_seeking reemployment = %f, want ≈0.30", rateWith)
	}
	without := mk(false)
	without.TickMonth(0, tr)
	rateBase := without.Snapshot().EmploymentRate
	if rateBase < 0.10 || rateBase > 0.20 {
		t.Fatalf("baseline reemployment = %f, want ≈0.15", rateBase)
	}
	if rateWith <= rateBase+0.08 {
		t.Fatalf("job_seeking uplift too small: %f vs %f", rateWith, rateBase)
	}
	// 次月:intent 已清零 → 增量回落基线(≈15%);intent 位确认全 0。
	before := rateWith
	with.TickMonth(0, tr)
	rate2 := with.Snapshot().EmploymentRate
	if d := rate2 - before; d < 0.08 || d > 0.20 {
		t.Fatalf("second month delta = %f, want baseline ≈0.15 (intent must be consumed)", d)
	}
	with.mu.Lock()
	for i := range with.residents {
		if with.residents[i].intent != intentNone || with.residents[i].moveTarget != 0 {
			with.mu.Unlock()
			t.Fatalf("resident %d intent not cleared", i)
		}
	}
	with.mu.Unlock()
}

// ⑥ move_out:ApplyIntent(move_out, target) → TickMonth 后 district==target;
// 非法目标/非法意图整条忽略。
func TestApplyIntent_MoveOut(t *testing.T) {
	b := NewBackdrop(20, rand.New(rand.NewSource(26)), nil)
	b.ApplyIntent(3, "move_out", "tech")
	b.mu.Lock()
	if b.residents[3].intent != intentMoveOut || int(b.residents[3].moveTarget) != 1 {
		b.mu.Unlock()
		t.Fatal("move_out intent/target not recorded")
	}
	b.mu.Unlock()
	b.TickMonth(0, rand.New(rand.NewSource(1)))
	b.mu.Lock()
	got := b.residents[3].district
	b.mu.Unlock()
	if int(got) != 1 {
		t.Fatalf("after tick district = %d, want 1 (tech)", got)
	}
	// 非法目标:move_out 无有效城区 → 整条忽略;未知意图名 → 忽略。
	b.ApplyIntent(4, "move_out", "no-such-district")
	b.ApplyIntent(5, "moon_walk", "")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.residents[4].intent != intentNone || b.residents[5].intent != intentNone {
		t.Fatal("invalid intents must be ignored")
	}
}

// ⑦ 回退:Enabled=false → 驱动层零调用(房间级 VoiceScheduler 行为零变化
// 由 game/virtual_city 包 room_city_test 回归覆盖;此处钉死驱动侧 guard)。
func TestResidentDriver_Disabled(t *testing.T) {
	fp := &fakeDriverProvider{}
	d := NewResidentDriver(DriverConfig{Enabled: false, Workers: 4, PerMonth: 8}, newDriverPool(fp, 2))
	b := NewBackdrop(20, rand.New(rand.NewSource(27)), nil)
	d.RunMonth(b, 1, func(VoiceRecord) { t.Fatal("no records expected when disabled") })
	if calls, _ := fp.stats(); calls != 0 {
		t.Fatalf("disabled driver must not call llm, got %d", calls)
	}
	// PerMonth=0 → 同样零调用(0 归一为缺省 8,旧"仅抽样层"语义删除)。
	d2 := NewResidentDriver(DriverConfig{Enabled: true, Workers: 4, PerMonth: 0}, newDriverPool(fp, 2))
	d2.RunMonth(b, 1, func(VoiceRecord) { t.Fatal("no records expected at per_month=0") })
	if calls, _ := fp.stats(); calls != 0 {
		t.Fatalf("per_month=0 must not call llm, got %d", calls)
	}
	// clamp:Workers 0→4 / 99→64(2026-09-25 §LLM线路池配额 16→64);PerMonth
	// 99→64;超时 0→15000;Lines 负数→0。
	clamped := NewResidentDriver(DriverConfig{Enabled: true, Workers: 99, PerMonth: 99, AcquireTimeoutMS: 0, Lines: -3}, nil)
	if s := clamped.Snapshot(); s.Workers != 64 || s.PerMonth != 64 || s.Lines != 0 {
		t.Fatalf("clamp drifted: %+v", s)
	}
	def := NewResidentDriver(DriverConfig{Enabled: true}, nil)
	if s := def.Snapshot(); s.Workers != driverDefaultWorkers {
		t.Fatalf("workers default = %d, want %d", s.Workers, driverDefaultWorkers)
	}
	if def.cfg.AcquireTimeoutMS != driverDefaultAcquireTimeoutMS {
		t.Fatalf("acquire timeout default = %d, want %d", def.cfg.AcquireTimeoutMS, driverDefaultAcquireTimeoutMS)
	}
}

// ⑧ 驱动开关下发:Snapshot.Driver 字段 omitempty —— 关闭(未 SetDriver)时
// JSON 不含 driver 键;启用时四字段齐全。
func TestSnapshotDriverOmitEmpty(t *testing.T) {
	b := NewBackdrop(5, rand.New(rand.NewSource(28)), nil)
	data, err := json.Marshal(b.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "driver") {
		t.Fatalf("driver-off snapshot must omit driver block: %s", data)
	}
	b.SetDriver(NewResidentDriver(DriverConfig{Enabled: true, Workers: 3, PerMonth: 6}, nil))
	b.driver.setLastDriven(7)
	data, err = json.Marshal(b.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"driven_last":7`) {
		t.Fatalf("driver-on snapshot missing driven_last: %s", data)
	}
	if !strings.Contains(string(data), `"per_month":6`) || !strings.Contains(string(data), `"workers":3`) {
		t.Fatalf("driver-on snapshot missing fields: %s", data)
	}
}

// TestResidentDriver_RateLimitBucket 2026-09-26 §批次25(§3.3):LLMMinIntervalMs>0
// 时一次 RunMonth 内逐条共享令牌桶(容量 2)—— PerMonth=5 也只放行 2 条,
// 其余直接丢弃(不重试);LLMMinIntervalMs<=0 保持不限速(旧测试语义)。
func TestResidentDriver_RateLimitBucket(t *testing.T) {
	fp := &fakeDriverProvider{}
	d := NewResidentDriver(DriverConfig{Enabled: true, Workers: 2, PerMonth: 5, LLMMinIntervalMs: 60000}, newDriverPool(fp, 4))
	b := NewBackdrop(50, rand.New(rand.NewSource(21)), nil)
	sink := &syncVoiceSink{}
	done := make(chan struct{})
	go func() {
		d.RunMonth(b, 3, sink.collect)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunMonth blocked")
	}
	if calls, _ := fp.stats(); calls != 2 {
		t.Fatalf("llm calls = %d, want 2 (令牌桶容量限制;PerMonth=5 被收窄)", calls)
	}
	if ds := d.Snapshot(); ds.DrivenLast != 2 {
		t.Fatalf("driver snapshot DrivenLast = %d, want 2", ds.DrivenLast)
	}
}
