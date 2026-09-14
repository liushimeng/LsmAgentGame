package wealth

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	llmtypes "LsmAgentGame/llm/types"
)

// fakeBuyProvider 第一轮返回 buy_asset tool_use,之后返回纯文本(结束)。
// 用于复现 2026-09-14 P0 死锁: apply 持 r.mu 后闭包内 Engine() 二次加锁。
type fakeBuyProvider struct{ calls *int32 }

func (f fakeBuyProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	n := atomic.AddInt32(f.calls, 1)
	if n == 1 {
		return llmtypes.LLMResponse{
			StopReason: "tool_use",
			Content: []llmtypes.ContentBlock{
				{Type: "text", Text: "本月买入指数基金"},
				{Type: "tool_use", ID: "call_test_1", Name: "buy_asset", Input: map[string]any{"asset": "stock_index", "amount_cny": 1000}},
			},
		}, nil
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "结束本月"}},
	}, nil
}
func (f fakeBuyProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f fakeBuyProvider) ProviderType() string { return "fake" }

type fakeBuyRegistry struct{ p llmtypes.LLMProvider }

func (f fakeBuyRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return f.p, "fake-key", nil
}
func (f fakeBuyRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }

// TestBotBuyAsset_NoDeadlock 回归: bot 执行 buy_asset 不得死锁房间锁。
// 死锁复发时 3s 超时失败(修复前 apply → Engine() 二次加锁,整房卡死)。
func TestBotBuyAsset_NoDeadlock(t *testing.T) {
	var calls int32
	m := NewManager(Config{MonthMs: 3000, PoolDefault: "curated", AgentEnabled: true, AgentDecisionTimeoutSec: 5},
		fakeBuyRegistry{p: fakeBuyProvider{calls: &calls}})
	r := m.CreateRoom("room-buy")
	r.RegisterBotSeats(map[int]string{1: "b1", 2: "b2"}, map[int]string{1: "MA", 2: "MB"}, nil)
	if _, _, e := r.JoinGame("h0", "human"); e != nil {
		t.Fatal(e)
	}
	if e := r.Start(nil); e != nil {
		t.Fatalf("start: %v", e)
	}
	m.EnsureAgents(r)
	r.wakeBots()

	// 关键断言: 房间锁在 3s 内可获得(死锁时这里永远拿不到)。
	done := make(chan struct{})
	go func() {
		r.mu.Lock()
		r.mu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("room lock wedged: bot buy_asset self-deadlocked (apply → Engine 二次加锁)")
	}
	// bot 确实跑完了 LLM 循环。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&calls) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("bot never called LLM")
	}
}
