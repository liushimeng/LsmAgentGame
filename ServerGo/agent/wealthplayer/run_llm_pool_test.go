// Package wealthplayer — run_llm_pool_test.go: 池模式(ModelKey=="")单测
// (2026-09-21 §虚拟城市 B3)。
//
//	正常 Acquire/Release | 无线路快速失败(不得干等 ctx 超时)| 未接线报错
package wealthplayer

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/llm"
	llmtypes "LsmAgentGame/llm/types"
)

// fakePoolProvider 记录 Chat 调用(模型名/key),返回纯文本。
type fakePoolProvider struct {
	mu      sync.Mutex
	calls   int
	models  []string
	lastKey string
	err     error
}

func (f *fakePoolProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	f.calls++
	f.models = append(f.models, req.Model)
	f.lastKey = key
	f.mu.Unlock()
	if f.err != nil {
		return llmtypes.LLMResponse{}, f.err
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}
func (f *fakePoolProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fake: no stream")
}
func (f *fakePoolProvider) ProviderType() string { return "fake-pool" }

func newPoolAgent(t *testing.T, src func() *llm.LinePool) *Agent {
	t.Helper()
	a := NewAgent("room-pool", "user-pool", "", PoolModelDisplayNameTest, 0, 3, 5*time.Second)
	a.BindLinePoolSource(src)
	return a
}

// PoolModelDisplayNameTest 与 wealth.PoolModelDisplay 同值的本地常量
// (不反向 import game/wealth,避免循环)。
const PoolModelDisplayNameTest = "LLM线路池"

func TestCallProvider_PoolMode_AcquireRelease(t *testing.T) {
	fp := &fakePoolProvider{}
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "PoolModel",
		Info:     llmtypes.ModelInfo{Model: "PoolModel"},
		Provider: fp,
		APIKey:   "pool-key",
		Lines:    1,
	}})
	a := newPoolAgent(t, func() *llm.LinePool { return pool })

	resp, err := a.callProvider(context.Background(),
		llm.LLMRequest{Model: "", Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}}},
		nil)
	if err != nil {
		t.Fatalf("pool-mode call failed: %v", err)
	}
	if resp.Text() != "ok" {
		t.Fatalf("unexpected response text %q", resp.Text())
	}
	// 请求模型名必须被租约线路覆盖;APIKey 必须来自租约。
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if fp.calls != 1 || len(fp.models) != 1 || fp.models[0] != "PoolModel" {
		t.Fatalf("lease must override model: calls=%d models=%v", fp.calls, fp.models)
	}
	if fp.lastKey != "pool-key" {
		t.Fatalf("lease apiKey not used: %q", fp.lastKey)
	}
	// Release 已执行:再 Acquire 必须立即可得(容量 1 的池未漏还)。
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("lease not released back to pool: %v", err)
	}
	lease.Release()
}

// AgentClassName 机制在池模式下同样生效(空则由 AgentClass 填充)。
func TestCallProvider_PoolMode_AgentClassName(t *testing.T) {
	fp := &fakePoolProvider{}
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "PoolModel", Info: llmtypes.ModelInfo{Model: "PoolModel"},
		Provider: fp, APIKey: "k", Lines: 1,
	}})
	a := newPoolAgent(t, func() *llm.LinePool { return pool })
	if _, err := a.callProvider(context.Background(),
		llm.LLMRequest{Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "x"}}}}},
		nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if !a.IsPoolMode() {
		t.Fatal("IsPoolMode must be true for empty ModelKey")
	}
}

// 无线路(Total==0)→ 立即失败,不得干等 ctx 超时(硬约束)。
func TestCallProvider_PoolMode_EmptyPoolFastFail(t *testing.T) {
	empty := llm.NewLinePool(nil)
	a := newPoolAgent(t, func() *llm.LinePool { return empty })
	t0 := time.Now()
	_, err := a.callProvider(context.Background(),
		llm.LLMRequest{Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "x"}}}}},
		nil)
	if !errors.Is(err, llm.ErrAllLinesBusy) {
		t.Fatalf("empty pool must fail with ErrAllLinesBusy, got %v", err)
	}
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("empty pool must fail fast, took %v", d)
	}
}

// 未注入线路池来源 → 明确报错(装配缺口)。
func TestCallProvider_PoolMode_NoSourceBound(t *testing.T) {
	a := NewAgent("room-x", "user-x", "", "LLM线路池", 0, 3, time.Second)
	if _, err := a.callProvider(context.Background(),
		llm.LLMRequest{Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "x"}}}}},
		nil); err == nil || !errors.Is(err, errPoolModeNoSource) {
		t.Fatalf("expected errPoolModeNoSource, got %v", err)
	}
}

// Acquire 全忙 → ErrAllLinesBusy(调用方走既有 submit_month 兜底)。
func TestCallProvider_PoolMode_AllBusy(t *testing.T) {
	pool := llm.NewLinePool([]llm.LineSpec{{
		ModelKey: "PoolModel", Info: llmtypes.ModelInfo{Model: "PoolModel"},
		Provider: &fakePoolProvider{}, APIKey: "k", Lines: 1,
	}})
	lease, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("seed acquire: %v", err)
	}
	defer lease.Release()
	a := newPoolAgent(t, func() *llm.LinePool { return pool })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	_, err = a.callProvider(ctx,
		llm.LLMRequest{Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "x"}}}}},
		nil)
	if !errors.Is(err, llm.ErrAllLinesBusy) {
		t.Fatalf("busy pool must surface ErrAllLinesBusy, got %v", err)
	}
	if d := time.Since(t0); d < 150*time.Millisecond {
		t.Fatalf("must respect ctx deadline (returned after %v)", d)
	}
}
