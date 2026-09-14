package wealth

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	llmtypes "LsmAgentGame/llm/types"
)

type fakeProvider struct{ calls *int32 }

func (f fakeProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	atomic.AddInt32(f.calls, 1)
	return llmtypes.LLMResponse{}, nil
}
func (f fakeProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	atomic.AddInt32(f.calls, 1)
	return nil, nil
}
func (f fakeProvider) ProviderType() string { return "fake" }

type fakeRegistry struct {
	p     llmtypes.LLMProvider
	calls *int32
}

func (f fakeRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return f.p, "fake-key", nil
}
func (f fakeRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }

// TestEnsureAgents_BotsWake 回归: bot 注册后 EnsureAgents 必须装配 agent,
// settle 后 wakeBots 必须触发 OnMonthStart → LLM 调用。
func TestEnsureAgents_BotsWake(t *testing.T) {
	var calls int32
	reg := fakeRegistry{p: fakeProvider{calls: &calls}, calls: &calls}
	m := NewManager(Config{MonthMs: 3000, PoolDefault: "curated", AgentEnabled: true, AgentDecisionTimeoutSec: 2}, reg)
	r := m.CreateRoom("room-wake")
	r.RegisterBotSeats(map[int]string{1: "b1", 2: "b2"}, map[int]string{1: "MA", 2: "MB"}, nil)
	if _, _, e := r.JoinGame("h0", "human"); e != nil {
		t.Fatal(e)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	m.EnsureAgents(r)
	r.mu.Lock()
	nAgents := len(r.agents)
	r.mu.Unlock()
	if nAgents != 2 {
		t.Fatalf("agents = %d, want 2", nAgents)
	}
	// 触发一轮 wake(settle 后路径)。
	r.wakeBots()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("bots never called LLM (calls=%d)", calls)
}
