// Package vcplayer — run_loop_test.go: 月度决策 tool loop 回归测试
// (2026-09-26 §批次25):LLM 令牌桶节流(round 级)+ appendMessages 双重
// 追加 bug 回归。
package vcplayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"LsmAgentGame/agent/vctypes"
	llmtypes "LsmAgentGame/llm/types"
)

// loopFakeProvider 每轮返回一个 budget 动作 tool_use(rest);记录每次请求的
// messages 快照(用于断言无重复 assistant/tool_result 入流)。
type loopFakeProvider struct {
	mu       sync.Mutex
	calls    int
	messages [][]llmtypes.Message
}

func (f *loopFakeProvider) Chat(ctx context.Context, key string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.messages = append(f.messages, append([]llmtypes.Message(nil), req.Messages...))
	f.mu.Unlock()
	return llmtypes.LLMResponse{
		StopReason: "tool_use",
		Content: []llmtypes.ContentBlock{{
			Type: "tool_use", ID: fmt.Sprintf("tu-%d", n),
			Name: ToolRest, Input: map[string]any{},
		}},
	}, nil
}

func (f *loopFakeProvider) ChatStream(ctx context.Context, key string, req llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fake: no stream")
}
func (f *loopFakeProvider) ProviderType() string { return "fake-loop" }

type loopFakeRegistry struct{ p llmtypes.LLMProvider }

func (r loopFakeRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	return r.p, "k", nil
}
func (r loopFakeRegistry) GetThinkingEnabled(modelKey string) (bool, int) { return false, 0 }

// loopFakeRunner 复用 fakeTradeRunner 全量工具实现,额外统计 submit 次数。
type loopFakeRunner struct {
	fakeTradeRunner
	submitCount int
}

func (f *loopFakeRunner) SubmitMonth(seat int) error {
	f.submitCount++
	return nil
}

func newLoopAgent(t *testing.T, maxActions int, rateInterval time.Duration) (*Agent, *loopFakeProvider, *loopFakeRunner) {
	t.Helper()
	fp := &loopFakeProvider{}
	fr := &loopFakeRunner{}
	a := NewAgent("room-loop", "u-loop", "Loop-model", "LoopModel", 0, maxActions, 5*time.Second)
	a.BindRegistry(loopFakeRegistry{p: fp})
	a.BindRunner(fr)
	a.SetLLMRateLimit(rateInterval)
	return a, fp, fr
}

// TestOnMonthStart_RateLimitStopsRounds 令牌桶(容量 2,间隔 1h ≈ 不补充)下
// ≤3 轮 tool loop 只发起 2 次 LLM 调用,第 3 轮取不到令牌即中止并兜底提交
// (绝不无限重试)。
func TestOnMonthStart_RateLimitStopsRounds(t *testing.T) {
	a, fp, fr := newLoopAgent(t, 10, time.Hour)
	a.OnMonthStart(context.Background(), &vctypes.GameContext{
		RoomID: "room-loop", GameKind: "virtual_city", Month: 1,
	})
	if fp.calls != 2 {
		t.Fatalf("llm calls = %d, want 2 (令牌桶容量)", fp.calls)
	}
	if fr.submitCount != 1 {
		t.Fatalf("submit count = %d, want 1 (桶空后兜底 submit_month)", fr.submitCount)
	}
	if tr := a.Transcript(); tr.LastToolInput != "rate_limited" {
		t.Fatalf("transcript LastToolInput = %q, want rate_limited", tr.LastToolInput)
	}
}

// TestOnMonthStart_OverBudgetAppendsOnce 回归:超预算分支触发时,assistant
// tool_use / tool_result 回合必须恰好追加一次 —— 旧实现在分支内 append 后
// break,循环尾再 append 一次,第 2 轮请求里 messages 会重复携带同一
// assistant 回合(费 token,且可能触发上游 400)。
func TestOnMonthStart_OverBudgetAppendsOnce(t *testing.T) {
	a, fp, fr := newLoopAgent(t, 2, time.Hour) // maxActions=2:第 2 轮触发超预算
	a.OnMonthStart(context.Background(), &vctypes.GameContext{
		RoomID: "room-loop", GameKind: "virtual_city", Month: 1,
	})
	if fp.calls != 2 {
		t.Fatalf("llm calls = %d, want 2 (round0 用 1 预算,round1 超预算结束)", fp.calls)
	}
	if fr.submitCount != 1 {
		t.Fatalf("submit count = %d, want 1", fr.submitCount)
	}
	// 第 2 次请求的 messages:[user 初始, assistant round0, user tool_result]
	// —— 恰好各一段,assistant 不得重复。
	msgs := fp.messages[1]
	if len(msgs) != 3 {
		t.Fatalf("round1 request messages len = %d, want 3 (重复追加?)", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Fatalf("round1 roles = %s/%s/%s, want user/assistant/user",
			msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
	assistantToolUses := 0
	for _, b := range msgs[1].Content {
		if b.Type == "tool_use" {
			assistantToolUses++
			if b.ID != "tu-1" {
				t.Fatalf("assistant tool_use id = %q, want tu-1 (round0 响应)", b.ID)
			}
		}
	}
	if assistantToolUses != 1 {
		t.Fatalf("assistant tool_use blocks = %d, want 1", assistantToolUses)
	}
	toolResults := 0
	for _, b := range msgs[2].Content {
		if b.Type == "tool_result" {
			toolResults++
			if b.ToolUseID != "tu-1" {
				t.Fatalf("tool_result tool_use_id = %q, want tu-1", b.ToolUseID)
			}
		}
	}
	if toolResults != 1 {
		t.Fatalf("tool_result blocks = %d, want 1", toolResults)
	}
}
