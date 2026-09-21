// Package llm — linepool_test.go 覆盖 LLM 线路池(虚拟城市改造,2026-09-21)
// 的纯内存语义,契约来源见 lag_docs/财商流游戏/已实现/02-LLM线路池/。
//
// 全部用例不触 DB、不发网络请求 —— provider 用桩实现,只验证令牌调度:
//   - 容量上限: M=3 时第 4 个并发 Acquire 阻塞,Release 放行;
//   - 加权轮询: A(lines=2)/B(lines=1) 6 次 Acquire → A 4 次 B 2 次;
//   - ctx 超时: 阻塞中返回 ErrAllLinesBusy 且令牌数不变;
//   - Release 幂等;
//   - Stats 的 lines / in_flight 记账;
//   - clamp [1,64] 与空池行为。
package llm

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	types "LsmAgentGame/llm/types"
)

// linepoolStubProvider 是最小 LLMProvider 桩 —— 线路池只透传 provider,
// 从不调用它,这里只需非 nil 且可比较身份。
type linepoolStubProvider struct{ tag string }

func (p *linepoolStubProvider) Chat(context.Context, string, types.LLMRequest) (types.LLMResponse, error) {
	return types.LLMResponse{}, nil
}

func (p *linepoolStubProvider) ChatStream(context.Context, string, types.LLMRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (p *linepoolStubProvider) ProviderType() string { return types.ProviderTypeAnthropicMessages }

// TestLinePool_CapacityLimit — 契约 §9 容量上限:M=3 时 4 个并发 Acquire
// 恰 1 个阻塞;Release 后放行。
func TestLinePool_CapacityLimit(t *testing.T) {
	p := NewLinePool([]LineSpec{
		{ModelKey: "M1", Provider: &linepoolStubProvider{tag: "m1"}, APIKey: "k1", Lines: 3},
	})
	if got := p.Total(); got != 3 {
		t.Fatalf("Total() = %d, want 3", got)
	}

	leases := make([]*LineLease, 0, 3)
	for i := 0; i < 3; i++ {
		l, err := p.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire #%d: %v", i+1, err)
		}
		leases = append(leases, l)
	}

	// 第 4 个 Acquire 必须阻塞(全忙)。
	acquired := make(chan *LineLease, 1)
	failed := make(chan error, 1)
	go func() {
		l, err := p.Acquire(context.Background())
		if err != nil {
			failed <- err
			return
		}
		acquired <- l
	}()
	select {
	case l := <-acquired:
		t.Fatalf("4th Acquire must block while M=3 in flight, got lease %v", l)
	case err := <-failed:
		t.Fatalf("4th Acquire unexpected error: %v", err)
	case <-time.After(150 * time.Millisecond):
		// 阻塞符合预期。
	}

	// Release 放行:阻塞者必须拿到租约。
	leases[0].Release()
	select {
	case l := <-acquired:
		if l == nil || l.ModelKey != "M1" {
			t.Fatalf("unexpected lease after Release: %+v", l)
		}
		l.Release() // 归还,避免后续 goroutine 泄漏计数。
	case err := <-failed:
		t.Fatalf("post-Release Acquire error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Release did not unblock the waiting Acquire")
	}
}

// TestLinePool_WeightedRoundRobin — 契约 §9 加权轮询:A(lines=2)/B(lines=1),
// 6 次 Acquire(两轮「拿光 + 全部归还」)→ A 4 次 B 2 次。令牌通道的组成
// (A×2+B×1)在归还后不变,因此分布与具体交错顺序无关。
func TestLinePool_WeightedRoundRobin(t *testing.T) {
	p := NewLinePool([]LineSpec{
		{ModelKey: "A", Provider: &linepoolStubProvider{tag: "a"}, APIKey: "ka", Lines: 2},
		{ModelKey: "B", Provider: &linepoolStubProvider{tag: "b"}, APIKey: "kb", Lines: 1},
	})
	if got := p.Total(); got != 3 {
		t.Fatalf("Total() = %d, want 3", got)
	}

	counts := map[string]int{}
	for round := 0; round < 2; round++ {
		var held []*LineLease
		for i := 0; i < 3; i++ {
			l, err := p.Acquire(context.Background())
			if err != nil {
				t.Fatalf("round %d Acquire #%d: %v", round+1, i+1, err)
			}
			counts[l.ModelKey]++
			held = append(held, l)
		}
		for _, l := range held {
			l.Release()
		}
	}
	if counts["A"] != 4 || counts["B"] != 2 {
		t.Fatalf("weighted distribution = A:%d B:%d, want A:4 B:2", counts["A"], counts["B"])
	}
}

// TestLinePool_CtxTimeout — 契约 §9 ctx 取消:阻塞中 ctx 超时 →
// ErrAllLinesBusy,且令牌数不变(超时路径绝不从通道偷走令牌)。
func TestLinePool_CtxTimeout(t *testing.T) {
	p := NewLinePool([]LineSpec{
		{ModelKey: "A", Provider: &linepoolStubProvider{}, APIKey: "ka", Lines: 2},
	})
	for i := 0; i < 2; i++ {
		if _, err := p.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire #%d: %v", i+1, err)
		}
	}
	if got := len(p.tokens); got != 0 {
		t.Fatalf("tokens = %d after draining M=2, want 0", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := p.Acquire(ctx)
	if !errors.Is(err, ErrAllLinesBusy) {
		t.Fatalf("Acquire with expiring ctx = %v, want ErrAllLinesBusy", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("Acquire returned after %v, want it to have waited for ctx timeout", elapsed)
	}
	// 令牌数不变:超时路径没有消耗任何令牌,且在途计数仍为 M。
	if got := len(p.tokens); got != 0 {
		t.Fatalf("tokens = %d after ctx timeout, want 0 (token must not be consumed)", got)
	}
	for _, s := range p.Stats() {
		if s.InFlight != s.Lines {
			t.Fatalf("stats[%s].InFlight = %d, want %d", s.ModelKey, s.InFlight, s.Lines)
		}
	}
}

// TestLinePool_ReleaseIdempotent — 契约 §9:Release 幂等,重复调用只归还
// 一次令牌,在途计数不为负。
func TestLinePool_ReleaseIdempotent(t *testing.T) {
	p := NewLinePool([]LineSpec{
		{ModelKey: "A", Provider: &linepoolStubProvider{}, APIKey: "ka", Lines: 2},
	})
	l, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	l.Release()
	l.Release()
	l.Release()
	if got := len(p.tokens); got != 2 {
		t.Fatalf("tokens = %d after triple Release, want 2 (idempotent)", got)
	}
	for _, s := range p.Stats() {
		if s.InFlight != 0 {
			t.Fatalf("stats[%s].InFlight = %d after releases, want 0", s.ModelKey, s.InFlight)
		}
	}
	// 池仍可正常使用:两次 Acquire 立即成功。
	for i := 0; i < 2; i++ {
		l2, err := p.Acquire(context.Background())
		if err != nil {
			t.Fatalf("re-Acquire #%d: %v", i+1, err)
		}
		l2.Release()
	}
}

// TestLinePool_Stats — Stats 按构建顺序返回各模型 lines / in_flight。
func TestLinePool_Stats(t *testing.T) {
	p := NewLinePool([]LineSpec{
		{ModelKey: "A", Provider: &linepoolStubProvider{}, APIKey: "ka", Lines: 2},
		{ModelKey: "B", Provider: &linepoolStubProvider{}, APIKey: "kb", Lines: 1},
	})
	stats := p.Stats()
	if len(stats) != 2 {
		t.Fatalf("len(Stats) = %d, want 2", len(stats))
	}
	if stats[0].ModelKey != "A" || stats[0].Lines != 2 || stats[0].InFlight != 0 {
		t.Fatalf("stats[0] = %+v, want {A 2 0}", stats[0])
	}
	if stats[1].ModelKey != "B" || stats[1].Lines != 1 || stats[1].InFlight != 0 {
		t.Fatalf("stats[1] = %+v, want {B 1 0}", stats[1])
	}

	var held []*LineLease
	for i := 0; i < 2; i++ {
		l, err := p.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		held = append(held, l)
	}
	inFlight := 0
	for _, s := range p.Stats() {
		inFlight += s.InFlight
	}
	if inFlight != 2 {
		t.Fatalf("Σ InFlight = %d after 2 Acquires, want 2", inFlight)
	}
	for _, l := range held {
		l.Release()
	}
	inFlight = 0
	for _, s := range p.Stats() {
		inFlight += s.InFlight
	}
	if inFlight != 0 {
		t.Fatalf("Σ InFlight = %d after Release, want 0", inFlight)
	}
}

// TestLinePool_LeaseCarriesProviderAndKey — 租约透传 ModelKey / Info /
// Provider / APIKey,消费方(财商流)用它直接发起调用。
func TestLinePool_LeaseCarriesProviderAndKey(t *testing.T) {
	prov := &linepoolStubProvider{tag: "x"}
	p := NewLinePool([]LineSpec{
		{
			ModelKey: "X",
			Info:     types.ModelInfo{Model: "X", ProviderType: types.ProviderTypeAnthropicMessages, ConcurrencyLines: 1},
			Provider: prov,
			APIKey:   "sk-lease-key",
			Lines:    1,
		},
	})
	l, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()
	if l.ModelKey != "X" {
		t.Fatalf("lease.ModelKey = %q, want X", l.ModelKey)
	}
	if l.APIKey != "sk-lease-key" {
		t.Fatalf("lease.APIKey = %q, want sk-lease-key", l.APIKey)
	}
	if l.Provider != types.LLMProvider(prov) {
		t.Fatal("lease.Provider is not the exact spec provider instance")
	}
	if l.Info.ConcurrencyLines != 1 || l.Info.Model != "X" {
		t.Fatalf("lease.Info = %+v, want Model=X ConcurrencyLines=1", l.Info)
	}
}

// TestLinePool_ClampAndEmpty — Lines 越界在构建时 clamp 到 [1,64];
// 空 specs 返回 Total=0 的空池,Acquire 立即 ErrAllLinesBusy(绝不挂死)。
func TestLinePool_ClampAndEmpty(t *testing.T) {
	p := NewLinePool([]LineSpec{
		{ModelKey: "zero", Provider: &linepoolStubProvider{}, Lines: 0},
		{ModelKey: "huge", Provider: &linepoolStubProvider{}, Lines: 100},
	})
	if got := p.Total(); got != 65 {
		t.Fatalf("Total() = %d, want 65 (clamp 1 + 64)", got)
	}
	stats := p.Stats()
	if stats[0].Lines != 1 || stats[1].Lines != 64 {
		t.Fatalf("clamped lines = %+v, want [1 64]", stats)
	}

	empty := NewLinePool(nil)
	if got := empty.Total(); got != 0 {
		t.Fatalf("empty pool Total() = %d, want 0", got)
	}
	if _, err := empty.Acquire(context.Background()); !errors.Is(err, ErrAllLinesBusy) {
		t.Fatalf("empty pool Acquire = %v, want ErrAllLinesBusy", err)
	}
	if stats := empty.Stats(); stats != nil {
		t.Fatalf("empty pool Stats() = %+v, want nil", stats)
	}
}
