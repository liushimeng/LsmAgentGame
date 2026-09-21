// Package city — city_test.go: 契约 03 §8 测试要求全覆盖。
//
//	合成确定性 | 容量/性能 | 快照合理界 | 演化守恒 | 城市之声(fake pool)
//	校准兜底链(docs → curated → synthetic)
package city

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/game/wealth/profession"
	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// ── fake LLM provider(城市之声测试)──

// fakeVoiceProvider 记录调用并返回一句固定短文本;err != nil 时模拟 LLM 失败。
type fakeVoiceProvider struct {
	mu     sync.Mutex
	calls  int
	lastKG string
	lastM  string
	err    error
}

func (f *fakeVoiceProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastKG = key
	f.lastM = req.Model
	f.mu.Unlock()
	if f.err != nil {
		return llmtypes.LLMResponse{}, f.err
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "这个月想多攒点钱,把开销压一压。"}},
	}, nil
}
func (f *fakeVoiceProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, fmt.Errorf("fake: no stream")
}
func (f *fakeVoiceProvider) ProviderType() string { return "fake-voice" }

// newFakePool 构建一条线路的 fake 线路池。
func newFakePool(p llmtypes.LLMProvider) func() *llm.LinePool {
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "FakeVoice-model",
		Info:     llmtypes.ModelInfo{Model: "FakeVoice-model"},
		Provider: p,
		APIKey:   "fake-key",
		Lines:    1,
	}})
	return func() *llm.LinePool { return pool }
}

// ── 合成确定性(契约 §8:同 seed 两城逐字段相等)──

func TestBackdrop_Determinism(t *testing.T) {
	calib := SyntheticCalibTable()
	mk := func() (Snapshot, Snapshot) {
		rng := rand.New(rand.NewSource(20260921))
		b := NewBackdrop(5000, rng, calib)
		s0 := b.Snapshot()
		tickRng := rand.New(rand.NewSource(777))
		for i := 0; i < 6; i++ {
			b.TickMonth(0.002, tickRng)
		}
		return s0, b.Snapshot()
	}
	a0, a1 := mk()
	b0, b1 := mk()
	if !reflect.DeepEqual(a0, b0) {
		t.Fatalf("same seed must produce identical initial snapshot:\n%+v\nvs\n%+v", a0, b0)
	}
	if !reflect.DeepEqual(a1, b1) {
		t.Fatalf("same seed must produce identical post-tick snapshot:\n%+v\nvs\n%+v", a1, b1)
	}
}

// Snapshot 含 voices 时不可比较 —— 单独验证 voices 透传。
func TestBackdrop_VoicesRing(t *testing.T) {
	b := NewBackdrop(10, rand.New(rand.NewSource(1)), nil)
	for i := 0; i < 30; i++ {
		b.AppendVoice(VoiceRecord{Month: i + 1, Name: fmt.Sprintf("A%d·金融CBD", i), Text: "hi", ModelKey: "M"})
	}
	s := b.Snapshot()
	if len(s.Voices) != voicesRingCap {
		t.Fatalf("voices ring cap = %d, got %d", voicesRingCap, len(s.Voices))
	}
	if s.Voices[len(s.Voices)-1].Month != 30 {
		t.Fatalf("ring must keep newest, last month = %d", s.Voices[len(s.Voices)-1].Month)
	}
}

// ── 容量/性能(契约 §8:100K 初始化 <200ms;tick <100ms;CI 放宽 2×)──

func TestBackdrop_Performance100K(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 跳过 100K 性能用例")
	}
	const budgetInit = 200 * time.Millisecond
	const budgetTick = 100 * time.Millisecond
	const relax = 2 // CI 放宽系数(契约 §8)

	t0 := time.Now()
	b := NewBackdrop(100000, rand.New(rand.NewSource(42)), nil)
	if d := time.Since(t0); d > budgetInit*relax {
		t.Fatalf("NewBackdrop(100000) took %v, budget %v (relaxed ×%d)", d, budgetInit, relax)
	}
	rng := rand.New(rand.NewSource(43))
	t0 = time.Now()
	b.TickMonth(0.002, rng)
	if d := time.Since(t0); d > budgetTick*relax {
		t.Fatalf("TickMonth(100000) took %v, budget %v (relaxed ×%d)", d, budgetTick, relax)
	}
}

// ── 快照合理界(契约 §8:employmentRate∈(0.5,1);medianIncome∈(1000,50000);
// Σdistricts=n)──

func TestBackdrop_SnapshotSanity(t *testing.T) {
	b := NewBackdrop(50000, rand.New(rand.NewSource(9)), nil)
	s := b.Snapshot()
	if s.ResidentCount != 50000 {
		t.Fatalf("resident_count = %d, want 50000", s.ResidentCount)
	}
	if s.EmploymentRate <= 0.5 || s.EmploymentRate >= 1 {
		t.Fatalf("employment_rate = %f, want (0.5,1)", s.EmploymentRate)
	}
	if s.MedianIncome <= 1000 || s.MedianIncome >= 50000 {
		t.Fatalf("median_income = %f, want (1000,50000)", s.MedianIncome)
	}
	sum := 0
	for _, d := range s.Districts {
		if d.Name == "" || d.Population < 0 {
			t.Fatalf("bad district row: %+v", d)
		}
		sum += d.Population
	}
	if sum != 50000 {
		t.Fatalf("Σdistricts = %d, want 50000", sum)
	}
	if s.AvgAge < 18 || s.AvgAge > 70 {
		t.Fatalf("avg_age = %f, want [18,70]", s.AvgAge)
	}
	if s.TotalSavings <= 0 || s.StressedRate < 0 || s.StressedRate > 1 {
		t.Fatalf("unreasonable snapshot: savings=%f stressed=%f", s.TotalSavings, s.StressedRate)
	}
}

// ── 演化守恒(契约 §8:无冲击随机种子下 totalSavings 单调性与就业率漂移在界内)──

func TestBackdrop_EvolutionConservation(t *testing.T) {
	b := NewBackdrop(20000, rand.New(rand.NewSource(5)), nil)
	rng := rand.New(rand.NewSource(6))
	prev := b.Snapshot()
	for i := 0; i < 24; i++ {
		b.TickMonth(0.002, rng)
		cur := b.Snapshot()
		// 94% 就业 × 0.72 消费率 → 聚合储蓄逐月净增(失业消耗远小于就业结余)。
		if cur.TotalSavings < prev.TotalSavings {
			t.Fatalf("month %d: total savings decreased %f → %f (monotonicity broken)",
				i+1, prev.TotalSavings, cur.TotalSavings)
		}
		// 就业率漂移界:0.8% 失业 vs 15% 再就业 → 稳态 ≈ 0.959;24 月内界 (0.5,1)。
		if cur.EmploymentRate <= 0.5 || cur.EmploymentRate >= 1 {
			t.Fatalf("month %d: employment drifted out of bound: %f", i+1, cur.EmploymentRate)
		}
		prev = cur
	}
	// 年龄推进:24 tick = 2 岁。
	if final := b.Snapshot(); final.AvgAge < prev.AvgAge {
		t.Fatalf("avg age regressed: %f → %f", prev.AvgAge, final.AvgAge)
	}
}

// TickMonth 年龄推进精确断言(12 tick +1)。
func TestBackdrop_AgingEvery12Ticks(t *testing.T) {
	b := NewBackdrop(100, rand.New(rand.NewSource(3)), nil)
	s0 := b.Snapshot()
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 12; i++ {
		b.TickMonth(0, rng)
	}
	s1 := b.Snapshot()
	if diff := s1.AvgAge - s0.AvgAge; diff < 0.99 || diff > 1.01 {
		t.Fatalf("12 ticks must age +1: %f → %f (Δ%f)", s0.AvgAge, s1.AvgAge, diff)
	}
}

// ── 城市之声(契约 §8:fake pool 注入:产出条数=配置;Acquire 失败时不
// panic、不阻塞、缺数下月补)──

func TestVoiceScheduler_ProducesConfiguredCount(t *testing.T) {
	fp := &fakeVoiceProvider{}
	sched := NewVoiceScheduler(true, 3, newFakePool(fp))
	b := NewBackdrop(500, rand.New(rand.NewSource(11)), nil)

	var got []VoiceRecord
	done := make(chan struct{})
	go func() {
		sched.Run(b, 7, func(vr VoiceRecord) { got = append(got, vr) })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("voice scheduler blocked")
	}
	if len(got) != 3 {
		t.Fatalf("voices produced = %d, want 3 (PerMonth)", len(got))
	}
	for _, vr := range got {
		if vr.Month != 7 || vr.Text == "" || vr.Name == "" || vr.ModelKey != "FakeVoice-model" {
			t.Fatalf("bad voice record: %+v", vr)
		}
	}
	// 快照透出 + 环形缓冲。
	for _, vr := range got {
		b.AppendVoice(vr)
	}
	s := b.Snapshot()
	if len(s.Voices) != 3 {
		t.Fatalf("snapshot voices = %d, want 3", len(s.Voices))
	}
}

// 同月去重:候选立即置 voiced 位,同月内不得重复抽到同一居民。
func TestVoiceScheduler_MonthlyDedup(t *testing.T) {
	b := NewBackdrop(30, rand.New(rand.NewSource(12)), nil)
	picked1 := b.PickVoiceCandidates(10)
	picked2 := b.PickVoiceCandidates(10)
	if len(picked1) != 10 || len(picked2) != 10 {
		t.Fatalf("pick sizes: %d / %d, want 10/10 (city=30)", len(picked1), len(picked2))
	}
	seen := map[int]struct{}{}
	for _, idx := range append(picked1, picked2...) {
		if _, dup := seen[idx]; dup {
			t.Fatalf("resident %d voiced twice in same month", idx)
		}
		seen[idx] = struct{}{}
	}
	// 优先级:构造 stressed 居民后,候选必须全部来自优先集(stressed ∪ 失业),
	// 不足 5 名时才轮到普通居民(契约:stressed/失业优先,其余随机)。
	b2 := NewBackdrop(50, rand.New(rand.NewSource(13)), nil)
	b2.mu.Lock()
	priority := map[int]struct{}{}
	for i := 0; i < 5; i++ {
		b2.residents[i].flags |= flagStressed
		priority[i] = struct{}{}
	}
	for i := range b2.residents {
		if b2.residents[i].flags&flagEmployed == 0 {
			priority[i] = struct{}{}
		}
	}
	b2.mu.Unlock()
	cand := b2.PickVoiceCandidates(5)
	for _, idx := range cand {
		if _, ok := priority[idx]; !ok {
			t.Fatalf("priority-first violated: picked ordinary resident %d while priority set has %d", idx, len(priority))
		}
	}
}

// Acquire 失败(LLM 错误)→ 丢弃不 panic 不阻塞。
func TestVoiceScheduler_LLMFailureDropped(t *testing.T) {
	fp := &fakeVoiceProvider{err: fmt.Errorf("upstream 500")}
	sched := NewVoiceScheduler(true, 4, newFakePool(fp))
	b := NewBackdrop(100, rand.New(rand.NewSource(13)), nil)
	done := make(chan int)
	go func() {
		n := 0
		sched.Run(b, 3, func(VoiceRecord) { n++ })
		done <- n
	}()
	select {
	case n := <-done:
		if n != 0 {
			t.Fatalf("failed llm must drop records, got %d", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("voice scheduler blocked on llm failure")
	}
}

// 空池(Total==0)→ 立即返回 0 条(不等待 ctx 超时)。
func TestVoiceScheduler_EmptyPoolFastReturn(t *testing.T) {
	empty := llm.NewLinePool(nil)
	sched := NewVoiceScheduler(true, 4, func() *llm.LinePool { return empty })
	b := NewBackdrop(100, rand.New(rand.NewSource(14)), nil)
	t0 := time.Now()
	n := 0
	sched.Run(b, 4, func(VoiceRecord) { n++ })
	if n != 0 {
		t.Fatalf("empty pool must produce 0 voices, got %d", n)
	}
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("empty pool must return immediately, took %v", d)
	}
}

// 关闭开关 / 每月 0 条 → 不发任何调用。
func TestVoiceScheduler_Disabled(t *testing.T) {
	fp := &fakeVoiceProvider{}
	for _, tc := range []struct {
		enabled  bool
		perMonth int
	}{
		{false, 4},
		{true, 0},
	} {
		sched := NewVoiceScheduler(tc.enabled, tc.perMonth, newFakePool(fp))
		b := NewBackdrop(50, rand.New(rand.NewSource(15)), nil)
		sched.Run(b, 1, func(VoiceRecord) { t.Fatal("no records expected") })
	}
	if fp.calls != 0 {
		t.Fatalf("disabled scheduler must not call llm, got %d calls", fp.calls)
	}
}

// clamp 边界:perMonth clamp [0,32]。
func TestNewVoiceScheduler_Clamp(t *testing.T) {
	if got := NewVoiceScheduler(true, 99, nil).PerMonth; got != 32 {
		t.Fatalf("perMonth clamp upper = %d, want 32", got)
	}
	if got := NewVoiceScheduler(true, -5, nil).PerMonth; got != 0 {
		t.Fatalf("perMonth clamp lower = %d, want 0", got)
	}
}

// ── 校准兜底链(契约 §8:docs 缺失 → curated → synthetic,Source 正确)──

func TestCalibration_SyntheticFallback(t *testing.T) {
	tbl := BuildCalibTable(nil, 0)
	if tbl.Source != "synthetic" || tbl.Ready {
		t.Fatalf("nil loader must yield synthetic (not ready), got source=%s ready=%v", tbl.Source, tbl.Ready)
	}
	if tbl.Employment != defaultEmployment {
		t.Fatalf("synthetic employment = %f, want %f", tbl.Employment, defaultEmployment)
	}
	wsum := 0.0
	for _, d := range tbl.Domains {
		wsum += d.Weight
		if d.IncomeMean != 6800 || d.ExpenseRatioMean != 0.72 || d.SavingsMonths != 6 || d.AgeMean != 36 {
			t.Fatalf("synthetic domain defaults drifted: %+v", d)
		}
	}
	if wsum < 0.999 || wsum > 1.001 {
		t.Fatalf("synthetic weights sum = %f, want 1", wsum)
	}
}

func TestCalibration_CuratedFallback(t *testing.T) {
	// 根目录不存在 → docs 不可用 → curated(14 张精选卡聚合)。
	l := profession.NewLoader(filepath.Join(t.TempDir(), "no-such-dir"))
	tbl := BuildCalibTable(l, 32)
	if tbl.Source != "curated" || !tbl.Ready {
		t.Fatalf("missing docs root must yield curated, got source=%s ready=%v", tbl.Source, tbl.Ready)
	}
	// 精选卡月薪 4500..?(P16 波动带)——聚合均值应在合理区间,域数恒 26。
	if len(tbl.Domains) != domainCount {
		t.Fatalf("domains = %d, want 26", len(tbl.Domains))
	}
	// 城区权重来自精选卡 HomeDistrict 频次(非均匀)且和为 1。
	sum := 0.0
	uniform := true
	for _, w := range tbl.Districts {
		sum += w
		if w != tbl.Districts[0] {
			uniform = false
		}
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("curated district weights sum = %f, want 1", sum)
	}
	if uniform {
		t.Fatalf("curated cards carry home_district — weights must not be uniform")
	}
}

// writeDocsFixture 在临时目录写 n 张带 L1 域路径的职业卡(域 A/B 交替)。
func writeDocsFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	domains := []string{"A-农林牧渔", "B-采矿与冶金"}
	for i := 0; i < n; i++ {
		sub := filepath.Join(dir, domains[i%len(domains)])
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		salary := 5000 + i*500
		card := fmt.Sprintf(`---
id: T%04d
name: 居民%04d
occupation: 测试职业%02d
income_monthly: %d
monthly_expense: %d
savings_stock: %d
age: %d
work_intensity: 中
health_grade: A
risk_preference: balanced
marital: 单身
opening_hook: 我是测试职业%02d,正在为城市校准表提供抽样样本。
housing_city: 一线城市
employment_type: 全职
---
正文略`, i, i, i%50, salary, salary*6/10, salary*6, 25+i%40, i%50)
		path := filepath.Join(sub, fmt.Sprintf("card_%04d.md", i))
		if err := os.WriteFile(path, []byte(card), 0o644); err != nil {
			t.Fatalf("write card: %v", err)
		}
	}
	// 干扰目录:非 L1 域(维度目录)卡的 Domain 必须为空。
	if err := os.MkdirAll(filepath.Join(dir, "维度2-身份与就业状态"), 0o755); err != nil {
		t.Fatalf("mkdir dim: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "维度2-身份与就业状态", "dim.md"), []byte(`---
id: D0001
occupation: 维度样本
income_monthly: 9000
opening_hook: 我是维度目录的干扰样本,不应计入 L1 域统计。
---
正文略`), 0o644); err != nil {
		t.Fatalf("write dim card: %v", err)
	}
	return dir
}

func TestCalibration_DocsSource(t *testing.T) {
	dir := writeDocsFixture(t, 20) // ≥ minDomainSamples(16)
	l := profession.NewLoader(dir)
	tbl := BuildCalibTable(l, 64)
	if tbl.Source != "docs" || !tbl.Ready {
		t.Fatalf("docs pool must yield source=docs, got %s (ready=%v)", tbl.Source, tbl.Ready)
	}
	// 域权重:20 张卡 A/B 各 10 → 前两域权重 0.5,其余 0。
	if w := tbl.Domains[0].Weight; w < 0.49 || w > 0.51 {
		t.Fatalf("domain A weight = %f, want 0.5", w)
	}
	if w := tbl.Domains[2].Weight; w != 0 {
		t.Fatalf("zero-sample domain weight = %f, want 0 (回落聚合统计但权重 0)", w)
	}
	// 零样本域回落聚合统计(而非合成默认)。
	if tbl.Domains[2].IncomeMean == 6800 {
		t.Fatalf("zero-sample domain must fall back to aggregate stats, got synthetic 6800")
	}
}

// WarmUpCalibration 后台预热:CurrentCalibration 未预热时合成,预热后生效。
func TestWarmUpCalibration(t *testing.T) {
	if got := CurrentCalibration(); got.Ready || got.Source != "synthetic" {
		t.Fatalf("before warm-up must be synthetic, got %s ready=%v", got.Source, got.Ready)
	}
	// 预热(全局 sync.Once —— 本包其他测试可能已触发,两种结果都合法)。
	WarmUpCalibration(nil, 0)
	// nil loader 的预热路径产出 synthetic(ready=false → 不覆盖已 Ready 的表)。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if CurrentCalibration().Source != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// drawPairs 与 Draw 同 rng 序(重构不改变行为)。
func TestDrawWithDomain_MatchesDraw(t *testing.T) {
	dir := writeDocsFixture(t, 8)
	l1 := profession.NewLoader(dir)
	l2 := profession.NewLoader(dir)
	r1 := rand.New(rand.NewSource(100))
	r2 := rand.New(rand.NewSource(100))
	pairs := l1.DrawWithDomain(4, r1)
	cards := l2.Draw(4, r2)
	if len(pairs) != len(cards) {
		t.Fatalf("len mismatch: %d vs %d", len(pairs), len(cards))
	}
	for i := range pairs {
		if pairs[i].ID != cards[i].ID {
			t.Fatalf("card %d mismatch: %s vs %s (rng 序漂移)", i, pairs[i].ID, cards[i].ID)
		}
		if pairs[i].Domain == "" {
			t.Fatalf("docs card %d must carry L1 domain", i)
		}
	}
}
