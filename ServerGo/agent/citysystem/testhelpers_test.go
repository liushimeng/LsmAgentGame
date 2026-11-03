// Package citysystem — testhelpers_test.go: 单测共用夹具(2026-11-03 §批次81)。
package citysystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
)

// newTestRoom 构造一个开局中的虚拟城市房间(与 scenario_test 同款装配)。
func newTestRoom(t *testing.T, roomID string) *virtual_city.VirtualCityRoom {
	t.Helper()
	r := virtual_city.NewVirtualCityRoom(roomID, 3000, 42, 4)
	botUsers := map[int]string{}
	botModels := map[int]string{}
	for seat := 0; seat < 10; seat++ {
		botUsers[seat] = fmt.Sprintf("bot-%d", seat)
		botModels[seat] = "TestModel"
	}
	r.RegisterBotSeats(botUsers, botModels)
	if e := r.Start(nil); e != nil {
		t.Fatalf("start room: %v", e)
	}
	return r
}

// ─────────────────────────── fake LLM ───────────────────────────

// fakeProvider 按队列返回响应;记录全部请求;支持流式事件回调。
type fakeProvider struct {
	mu        sync.Mutex
	calls     int
	reqs      []llmtypes.LLMRequest
	responses []llmtypes.LLMResponse
	errs      []error
	stream    []llmtypes.StreamEvent
}

func (f *fakeProvider) next() (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return llmtypes.LLMResponse{}, f.errs[i]
	}
	if i < len(f.responses) {
		return f.responses[i], nil
	}
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: "done"}},
	}, nil
}

func (f *fakeProvider) Chat(_ context.Context, _ string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	return f.next()
}

func (f *fakeProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fake: no stream")
}

func (f *fakeProvider) ProviderType() string { return "fake" }

func (f *fakeProvider) ChatStreamAccumulate(_ context.Context, _ string, req llmtypes.LLMRequest,
	onProgress func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	evs := append([]llmtypes.StreamEvent(nil), f.stream...)
	f.mu.Unlock()
	for _, ev := range evs {
		if err := onProgress(ev); err != nil {
			return llmtypes.LLMResponse{}, err
		}
	}
	return f.next()
}

// fakeRegistry 实现 LLMRegistry。
type fakeRegistry struct {
	p       llmtypes.LLMProvider
	enabled bool
	budget  int
	list    []llmtypes.ModelInfo
}

func (r *fakeRegistry) Get(string) (llmtypes.LLMProvider, string, error) { return r.p, "k", nil }
func (r *fakeRegistry) GetThinkingEnabled(string) (bool, int)            { return r.enabled, r.budget }
func (r *fakeRegistry) ListEnabled() []llmtypes.ModelInfo                { return r.list }

// newTestAgent 构造带 fake registry / 独立 hub 的 Agent,返回事件收集器。
func newTestAgent(t *testing.T, room *virtual_city.VirtualCityRoom, reg LLMRegistry) (*Agent, *[]SystemEvent) {
	t.Helper()
	hub := citybus.New()
	t.Cleanup(hub.Close)
	store := NewConversationStore(40)
	var mu sync.Mutex
	events := make([]SystemEvent, 0, 16)
	emit := func(e SystemEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	a := NewAgent(room, Config{ModelKey: "TestModel"}, reg, hub, store, emit)
	a.SetLinePolicy("")
	return a, &events
}

// newTestHub 构造独立总线(测试清理自动 Close)。
func newTestHub(t *testing.T) *citybus.Hub {
	t.Helper()
	h := citybus.New()
	t.Cleanup(h.Close)
	return h
}

// collectEvents 快照事件列表(测试内单线程使用,无需加锁)。
func collectEvents(events *[]SystemEvent) []SystemEvent {
	out := make([]SystemEvent, len(*events))
	copy(out, *events)
	return out
}

// hasEventKind 报告事件流是否包含指定 kind。
func hasEventKind(events []SystemEvent, kind string) bool {
	for _, e := range events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

// textResponse 构造一条纯文本响应。
func textResponse(text string) llmtypes.LLMResponse {
	return llmtypes.LLMResponse{
		StopReason: "end_turn",
		Content:    []llmtypes.ContentBlock{{Type: "text", Text: text}},
	}
}

// toolResponse 构造一条单工具调用响应。
func toolResponse(id, name string, input map[string]any) llmtypes.LLMResponse {
	return llmtypes.LLMResponse{
		StopReason: "tool_use",
		Content:    []llmtypes.ContentBlock{{Type: "tool_use", ID: id, Name: name, Input: input}},
	}
}

// assertAgentClassWired 校验 AgentClassName 已登记(§24,防「声明了却从不接线」)。
func assertAgentClassWired(t *testing.T) {
	t.Helper()
	if agentroot.AgentClassCitySystem == "" {
		t.Fatal("AgentClassCitySystem must be non-empty (§24)")
	}
	found := false
	for _, c := range agentroot.AllAgentClassNames() {
		if c == agentroot.AgentClassCitySystem {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("AgentClassCitySystem must be registered in AllAgentClassNames()")
	}
}
