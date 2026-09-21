// Package llm — registry_linepool_test.go 覆盖 Registry 与 LinePool 的集成
// 语义(虚拟城市改造,2026-09-21):
//
//   - TotalLines = Σ enabled 行 lines(cfg 路径缺省 1);
//   - providers 行表变化(reload 核心)后 LinePool() 指向新池、Total 反映新表;
//   - 在途租约持旧池令牌,Release 归还旧池不 panic;
//   - disabled 行不占线路;
//   - per-endpoint 覆盖行在 rebuild 时懒建 provider,且与 Registry.Get 复用
//     同一实例(两套构造不漂移);
//   - nil / 零值 Registry 的 LinePool() 兜底为空池。
//
// 全部用例不触 DB:行表变更通过同包直接改 r.providers + 调
// rebuildLinePoolLocked()(即 Registry.Reload 成功后调用的同一函数)模拟;
// 真 DB 的 Reload 全链路由 llmintegration 套件覆盖。
package llm

import (
	"context"
	"testing"

	"LsmAgentGame/config"
	types "LsmAgentGame/llm/types"
)

// newLinePoolTestRegistry 构造一个纯 cfg 模式的 Registry,带 n 个 provider
// (model key 依次 m1..),api_key 为合成短 key。
func newLinePoolTestRegistry(n int) *Registry {
	cfg := config.LLMConfig{}
	for i := 1; i <= n; i++ {
		p := config.ProviderConfig{
			AgentName: "cfg-agent",
			Model:     "m" + string(rune('0'+i)),
			APIKey:    "sk-test",
		}
		cfg.Providers = append(cfg.Providers, p)
	}
	return NewRegistry(cfg)
}

// TestRegistry_TotalLines_FromConfig — cfg 路径无 concurrency_lines 概念,
// 每行缺省 1:N 行启用即 M=N;List() 条目带 concurrency_lines。
func TestRegistry_TotalLines_FromConfig(t *testing.T) {
	r := newLinePoolTestRegistry(3)
	if got := r.TotalLines(); got != 3 {
		t.Fatalf("TotalLines() = %d, want 3 (cfg rows default to 1 line each)", got)
	}
	for _, info := range r.List() {
		if info.ConcurrencyLines != 1 {
			t.Fatalf("List()[%s].ConcurrencyLines = %d, want 1", info.Model, info.ConcurrencyLines)
		}
	}
}

// TestRegistry_LinePool_NeverNil — 任何构造路径后 LinePool() 非 nil;
// nil / 零值 Registry 兜底返回共享空池(Total=0),消费方按「池不可用」回退。
func TestRegistry_LinePool_NeverNil(t *testing.T) {
	if p := newLinePoolTestRegistry(0).LinePool(); p == nil || p.Total() != 0 {
		t.Fatalf("empty registry LinePool() = %v, want non-nil empty pool", p)
	}
	var nilReg *Registry
	if p := nilReg.LinePool(); p == nil || p.Total() != 0 {
		t.Fatalf("nil receiver LinePool() = %v, want non-nil empty pool", p)
	}
	if got := nilReg.TotalLines(); got != 0 {
		t.Fatalf("nil receiver TotalLines() = %d, want 0", got)
	}
}

// TestRegistry_LinePool_RebuildSwapsPool — 行表变化后(reload 核心):
// LinePool() 指向新池实例、TotalLines 反映新表;旧租约 Release 归还旧池
// 不 panic,新池独立可用。
func TestRegistry_LinePool_RebuildSwapsPool(t *testing.T) {
	r := newLinePoolTestRegistry(2)
	if got := r.TotalLines(); got != 2 {
		t.Fatalf("TotalLines() = %d, want 2", got)
	}
	oldPool := r.LinePool()
	lease, err := oldPool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("old pool Acquire: %v", err)
	}

	// 模拟 Reload 后的新行表:删掉 m2、把 m1 的线路数改为 5。
	r.mu.Lock()
	rp := r.providers["m1"]
	rp.lines = 5
	rp.info.ConcurrencyLines = 5
	r.providers["m1"] = rp
	delete(r.providers, "m2")
	r.rebuildLinePoolLocked() // Registry.Reload 成功后调用的同一函数
	r.mu.Unlock()

	newPool := r.LinePool()
	if newPool == oldPool {
		t.Fatal("LinePool() must point to a NEW pool after the row table changed")
	}
	if got := r.TotalLines(); got != 5 {
		t.Fatalf("TotalLines() = %d after rebuild, want 5 (single row × 5 lines)", got)
	}
	stats := newPool.Stats()
	if len(stats) != 1 || stats[0].ModelKey != "m1" || stats[0].Lines != 5 {
		t.Fatalf("new pool stats = %+v, want single m1×5", stats)
	}

	// 旧租约 Release 归还旧池:不 panic、不影响新池。
	lease.Release()
	lease.Release() // 幂等
	if got := r.TotalLines(); got != 5 {
		t.Fatalf("TotalLines() = %d after old-lease Release, still want 5", got)
	}
	// 新池可独立 Acquire 满 5 条。
	var held []*LineLease
	for i := 0; i < 5; i++ {
		l, err := newPool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("new pool Acquire #%d: %v", i+1, err)
		}
		held = append(held, l)
	}
	for _, l := range held {
		l.Release()
	}
}

// TestRegistry_LinePool_DisabledRowExcluded — M = Σ **enabled** 行;
// 停用行不占线路(与 Get/List 的 enabled 语义一致)。
func TestRegistry_LinePool_DisabledRowExcluded(t *testing.T) {
	r := newLinePoolTestRegistry(2)
	if got := r.TotalLines(); got != 2 {
		t.Fatalf("TotalLines() = %d, want 2", got)
	}
	r.mu.Lock()
	rp := r.providers["m2"]
	rp.enabled = false
	r.providers["m2"] = rp
	r.rebuildLinePoolLocked()
	r.mu.Unlock()
	if got := r.TotalLines(); got != 1 {
		t.Fatalf("TotalLines() = %d after disabling m2, want 1", got)
	}
}

// TestRegistry_LinePool_PerEndpointProviderSharedWithGet — 带 per-row
// endpoint 覆盖且 provider 尚未懒建的行,rebuild 时用与 Get 慢路径相同的
// helper 构造并写回;之后 Get 命中快路径返回**同一实例**(两套构造不漂移),
// 租约透传同一 provider 与解密 key。
func TestRegistry_LinePool_PerEndpointProviderSharedWithGet(t *testing.T) {
	r := newLinePoolTestRegistry(0)
	r.mu.Lock()
	r.providers["ep-model"] = registeredProvider{
		info: types.ModelInfo{
			Model:            "ep-model",
			ProviderType:     types.ProviderTypeAnthropicMessages,
			ConcurrencyLines: 2,
		},
		key:       "sk-ep-key",
		available: true,
		enabled:   true,
		endpoint:  "http://proxy.example/Anthropic",
		protocol:  types.ProviderTypeAnthropicMessages,
		lines:     2,
		// provider 留空 —— 模拟 populateLocked 的 per-endpoint 覆盖行,
		// 等待懒建。
	}
	r.rebuildLinePoolLocked()
	// rebuild 必须已把懒建的 provider 写回行(后续 Get 命中快路径)。
	if r.providers["ep-model"].provider == nil {
		r.mu.Unlock()
		t.Fatal("rebuildLinePoolLocked did not build + store the per-endpoint provider")
	}
	built := r.providers["ep-model"].provider
	r.mu.Unlock()

	if got := r.TotalLines(); got != 2 {
		t.Fatalf("TotalLines() = %d, want 2", got)
	}
	lease, err := r.LinePool().Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer lease.Release()
	if lease.Provider == nil || lease.Provider != built {
		t.Fatal("lease.Provider must be the exact instance rebuilt by registry (shared with Get)")
	}
	if lease.APIKey != "sk-ep-key" {
		t.Fatalf("lease.APIKey = %q, want sk-ep-key", lease.APIKey)
	}
	if lease.ModelKey != "ep-model" {
		t.Fatalf("lease.ModelKey = %q, want ep-model", lease.ModelKey)
	}

	// Get 走快路径并返回同一实例 + 同一 key。
	gp, gk, gerr := r.Get("ep-model")
	if gerr != nil {
		t.Fatalf("Get: %v", gerr)
	}
	if gp != built {
		t.Fatal("Get returned a different provider instance than the pool lease")
	}
	if gk != "sk-ep-key" {
		t.Fatalf("Get key = %q, want sk-ep-key", gk)
	}
}
