// Package citywebgl — agent_test.go: Structured 组合 + Run(fake LLM)单测
// (2026-11-03 §批次81)。
package citywebgl

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	agentroot "LsmAgentGame/agent"
	llmtypes "LsmAgentGame/llm/types"
)

// fakeProvider 按脚本逐次返回响应;记录每次请求(断言 AgentClassName /
// messages 交替 / tools 接线)。
type fakeProvider struct {
	mu        sync.Mutex
	responses []llmtypes.LLMResponse
	calls     []llmtypes.LLMRequest
}

func (f *fakeProvider) Chat(_ context.Context, _ string, req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	return f.next(req)
}

func (f *fakeProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, errors.New("fake: no stream")
}

func (f *fakeProvider) ProviderType() string { return "fake-webgl" }

func (f *fakeProvider) ChatStreamAccumulate(_ context.Context, _ string, req llmtypes.LLMRequest,
	_ func(llmtypes.StreamEvent) error) (llmtypes.LLMResponse, error) {
	return f.next(req)
}

func (f *fakeProvider) next(req llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if len(f.responses) == 0 {
		return llmtypes.LLMResponse{}, errors.New("fake: no more responses")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

type fakeRegistry struct {
	p       llmtypes.LLMProvider
	enabled bool
	budget  int
	list    []llmtypes.ModelInfo
}

func (r *fakeRegistry) Get(string) (llmtypes.LLMProvider, string, error) { return r.p, "k", nil }
func (r *fakeRegistry) GetThinkingEnabled(string) (bool, int)            { return r.enabled, r.budget }
func (r *fakeRegistry) ListEnabled() []llmtypes.ModelInfo                { return r.list }

func toolUseResp(id, name string, input map[string]any) llmtypes.LLMResponse {
	return llmtypes.LLMResponse{
		StopReason: "tool_use",
		Content:    []llmtypes.ContentBlock{{Type: "tool_use", ID: id, Name: name, Input: input}},
	}
}

var endResp = llmtypes.LLMResponse{
	StopReason: "end_turn",
	Content:    []llmtypes.ContentBlock{{Type: "text", Text: "done"}},
}

// TestStructured 组合规则:district 优先于 (x,z);mode 追加 set_mode;无效参数不产指令。
func TestStructured(t *testing.T) {
	x, z := 10.0, -20.0
	cases := []struct {
		name string
		req  DisplayRequest
		want []DisplayCommand
	}{
		{"district", DisplayRequest{District: "d1"},
			[]DisplayCommand{{Type: DisplayFocusDistrict, District: "d1"}}},
		{"district+mode", DisplayRequest{District: "d1", Mode: "fly"},
			[]DisplayCommand{{Type: DisplayFocusDistrict, District: "d1"}, {Type: DisplaySetMode, Mode: "fly"}}},
		{"point", DisplayRequest{X: &x, Z: &z},
			[]DisplayCommand{{Type: DisplayFocusPoint, X: 10, Z: -20}}},
		{"point+mode", DisplayRequest{X: &x, Z: &z, Mode: "orbit"},
			[]DisplayCommand{{Type: DisplayFocusPoint, X: 10, Z: -20}, {Type: DisplaySetMode, Mode: "orbit"}}},
		{"district_wins_over_point", DisplayRequest{District: "d1", X: &x, Z: &z},
			[]DisplayCommand{{Type: DisplayFocusDistrict, District: "d1"}}},
		{"mode_only", DisplayRequest{Mode: "fly"},
			[]DisplayCommand{{Type: DisplaySetMode, Mode: "fly"}}},
		{"invalid_mode_only", DisplayRequest{Mode: "spin"}, []DisplayCommand{}},
		{"half_point_no_cmd", DisplayRequest{X: &x}, []DisplayCommand{}},
		{"empty", DisplayRequest{}, []DisplayCommand{}},
	}
	a := NewAgent(Config{}, nil, nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a.Structured(tc.req)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d cmds %+v, want %d %+v", len(got), got, len(tc.want), tc.want)
			}
			for i := range tc.want {
				if !reflect.DeepEqual(got[i], tc.want[i]) {
					t.Fatalf("cmd[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRun_EmitsAndReturns LLM 返回 tool_use → emit 实时下发 + 返回值收集 +
// tool_result 成对回灌。
func TestRun_EmitsAndReturns(t *testing.T) {
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolUseResp("tu-1", ToolFocusDistrict, map[string]any{"district": "d1"}),
		toolUseResp("tu-2", ToolSetMode, map[string]any{"mode": "fly"}),
		endResp,
	}}
	var emitted []DisplayCommand
	a := NewAgent(Config{ModelKey: "M", MaxSteps: 3}, &fakeRegistry{p: fp},
		func(cmd DisplayCommand) { emitted = append(emitted, cmd) })

	cmds, err := a.Run(context.Background(), DisplayRequest{Text: "看商业区并飞过去"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cmds) != 2 || len(emitted) != 2 {
		t.Fatalf("cmds=%+v emitted=%+v", cmds, emitted)
	}
	if cmds[0].Type != DisplayFocusDistrict || cmds[0].District != "d1" {
		t.Fatalf("cmd[0] = %+v", cmds[0])
	}
	if cmds[1].Type != DisplaySetMode || cmds[1].Mode != "fly" {
		t.Fatalf("cmd[1] = %+v", cmds[1])
	}
	for _, c := range cmds {
		if c.TraceID == "" {
			t.Fatalf("command missing trace_id: %+v", c)
		}
	}
	// 三次 LLM 调用:step0 user(text)→tu-1,step1 assistant+tool_result→tu-2,
	// step2 assistant+tool_result→end_turn。
	fp.mu.Lock()
	calls := append([]llmtypes.LLMRequest(nil), fp.calls...)
	fp.mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("llm calls = %d, want 3", len(calls))
	}
	for i, call := range calls {
		if call.AgentClassName != string(agentroot.AgentClassCityWebGLShow) {
			t.Fatalf("call[%d] AgentClassName = %q", i, call.AgentClassName)
		}
		if len(call.Tools) != len(ToolNames()) {
			t.Fatalf("call[%d] tools = %d, want %d", i, len(call.Tools), len(ToolNames()))
		}
	}
	// 第 2 次请求的 messages 必须 user/assistant 严格交替(§14.1)。
	msgs := calls[1].Messages
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3 (user/assistant/user)", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Fatalf("roles = %s/%s/%s", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
	if msgs[2].Content[0].Type != "tool_result" || msgs[2].Content[0].ToolUseID != "tu-1" {
		t.Fatalf("tool_result not paired: %+v", msgs[2].Content[0])
	}
	// system 带三段式头(§14.3,EnsureHead 幂等注入)。
	if len(calls[0].System) < 2 {
		t.Fatalf("system blocks = %d, want ≥2(含三段式头)", len(calls[0].System))
	}
}

// TestRun_DispatchErrorNotEmitted 工具缺参 → isErr 回灌,不 emit 不收集,
// 循环继续(模型可换工具重试)。
func TestRun_DispatchErrorNotEmitted(t *testing.T) {
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolUseResp("tu-1", ToolFocusDistrict, map[string]any{}), // 缺 district
		toolUseResp("tu-2", ToolClearDisplay, map[string]any{}),
		endResp,
	}}
	var emitted []DisplayCommand
	a := NewAgent(Config{ModelKey: "M"}, &fakeRegistry{p: fp},
		func(cmd DisplayCommand) { emitted = append(emitted, cmd) })
	cmds, err := a.Run(context.Background(), DisplayRequest{Text: "清一下"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Type != DisplayClear {
		t.Fatalf("cmds = %+v, want 1 clear", cmds)
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted = %+v", emitted)
	}
	// step0 的 tool_result is_error=true(缺 district)。
	fp.mu.Lock()
	msgs := fp.calls[1].Messages
	fp.mu.Unlock()
	tr := msgs[2].Content
	if len(tr) != 1 || !tr[0].IsError || tr[0].ToolUseID != "tu-1" {
		t.Fatalf("tool_results = %+v", tr)
	}
}

// TestRun_EmptyText 空文本请求直接报错(不打 LLM)。
func TestRun_EmptyText(t *testing.T) {
	a := NewAgent(Config{ModelKey: "M"}, &fakeRegistry{p: &fakeProvider{}}, nil)
	if _, err := a.Run(context.Background(), DisplayRequest{Text: "  "}); err == nil {
		t.Fatal("empty text must error")
	}
}

// TestRun_NilRegistry 未绑定 registry 报错不 panic。
func TestRun_NilRegistry(t *testing.T) {
	a := NewAgent(Config{ModelKey: "M"}, nil, nil)
	if _, err := a.Run(context.Background(), DisplayRequest{Text: "hi"}); err == nil {
		t.Fatal("nil registry must error")
	}
}

// TestRun_LLMError 已产出指令随 error 返回(不丢弃)。
func TestRun_LLMError(t *testing.T) {
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolUseResp("tu-1", ToolAnnotate, map[string]any{"text": "注意"}),
		// 第 2 步无脚本 → fake 报错。
	}}
	a := NewAgent(Config{ModelKey: "M"}, &fakeRegistry{p: fp}, nil)
	cmds, err := a.Run(context.Background(), DisplayRequest{Text: "标注"})
	if err == nil {
		t.Fatal("expected llm error")
	}
	if len(cmds) != 1 {
		t.Fatalf("partial cmds = %+v, want 1", cmds)
	}
}

// TestRun_StepsBound MaxSteps 达到即停(不无限循环)。
func TestRun_StepsBound(t *testing.T) {
	fp := &fakeProvider{responses: []llmtypes.LLMResponse{
		toolUseResp("tu-1", ToolClearDisplay, map[string]any{}),
		toolUseResp("tu-2", ToolClearDisplay, map[string]any{}),
		toolUseResp("tu-3", ToolClearDisplay, map[string]any{}),
		toolUseResp("tu-4", ToolClearDisplay, map[string]any{}),
	}}
	a := NewAgent(Config{ModelKey: "M", MaxSteps: 2}, &fakeRegistry{p: fp}, nil)
	cmds, err := a.Run(context.Background(), DisplayRequest{Text: "清"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("cmds = %d, want 2(MaxSteps bound)", len(cmds))
	}
	fp.mu.Lock()
	n := len(fp.calls)
	fp.mu.Unlock()
	if n != 2 {
		t.Fatalf("llm calls = %d, want 2", n)
	}
}

// TestNewAgent_DefaultMaxSteps MaxSteps ≤0 归一为 3(缺省)。
func TestNewAgent_DefaultMaxSteps(t *testing.T) {
	a := NewAgent(Config{MaxSteps: -1}, nil, nil)
	if a.cfg.MaxSteps != defaultMaxSteps {
		t.Fatalf("MaxSteps = %d, want %d", a.cfg.MaxSteps, defaultMaxSteps)
	}
}

// TestResolveLineKey 线路策略:固定 / 缺省 / random(池空回退)。
func TestResolveLineKey(t *testing.T) {
	a := NewAgent(Config{ModelKey: "Base"}, &fakeRegistry{
		p: &fakeProvider{},
		list: []llmtypes.ModelInfo{
			{Model: "L1", ConcurrencyLines: 2},
			{Model: "L2", ConcurrencyLines: 0},
		},
	}, nil)
	if got := a.resolveLineKey(); got != "Base" {
		t.Fatalf("default = %q, want Base", got)
	}
	a.SetLinePolicy("Fixed")
	if got := a.resolveLineKey(); got != "Fixed" {
		t.Fatalf("fixed = %q, want Fixed", got)
	}
	a.SetLinePolicy(LinePolicyRandom)
	got := a.resolveLineKey()
	if got != "L1" && got != "L2" {
		t.Fatalf("random = %q, want L1|L2", got)
	}
	// 池空回退 ModelKey。
	b := NewAgent(Config{ModelKey: "Base"}, &fakeRegistry{p: &fakeProvider{}}, nil)
	b.SetLinePolicy(LinePolicyRandom)
	if got := b.resolveLineKey(); got != "Base" {
		t.Fatalf("empty pool = %q, want Base", got)
	}
}

// TestUserPrompt_IncludeText user 消息包含请求文本(角色段 + 请求段)。
func TestUserPrompt_IncludeText(t *testing.T) {
	s := userPrompt(DisplayRequest{Text: "看看 d1", District: "d1", Mode: "fly"})
	if !strings.Contains(s, "看看 d1") || !strings.Contains(s, "district=d1") || !strings.Contains(s, "mode=fly") {
		t.Fatalf("userPrompt = %q", s)
	}
	if !strings.Contains(roleText, "结构化显示指令") || !strings.Contains(roleText, "orbit|fly") {
		t.Fatal("roleText missing discipline anchors")
	}
}
