package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/llm"
	"LsmAgentGame/llm/anthropic"
)

// TestProviderChat_WireFormat verifies the provider sends the Anthropic wire
// format (system + messages + tools + max_tokens) and the correct bearer +
// anthropic-version headers.
func TestProviderChat_WireFormat(t *testing.T) {
	var gotAuth, gotVersion, gotCT, gotUA string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("anthropic-version")
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"type":"message","id":"msg_123","model":"MeiTuan-model","role":"assistant",
			"content":[{"type":"tool_use","id":"tool_1","name":"speak","input":{"text":"你好"}}],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":42,"output_tokens":17}
		}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	p.SetUserAgent("LsmAgentGame/v1.0.0 Jul  7 2026 10:00:00")
	req := llm.LLMRequest{
		Model:     "MeiTuan-model",
		System:    []llm.SystemBlock{{Type: "text", Text: "you are a werewolf player"}},
		Messages:  []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "go"}}}},
		Tools:     []llm.ToolDef{{Name: "speak", Description: "speak", InputSchema: map[string]any{"type": "object"}}},
		MaxTokens: 200,
		Metadata:  llm.Metadata{UserID: "agent-1"},
	}
	resp, err := p.Chat(context.Background(), "sk-real-key-abc", req)
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}

	if gotAuth != "Bearer sk-real-key-abc" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version header = %q", gotVersion)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("Content-Type header = %q", gotCT)
	}
	if gotUA != "LsmAgentGame/v1.0.0 Jul  7 2026 10:00:00" {
		t.Errorf("User-Agent header = %q, want %q", gotUA, "LsmAgentGame/v1.0.0 Jul  7 2026 10:00:00")
	}
	if gotBody["model"] != "MeiTuan-model" {
		t.Errorf("body.model = %v", gotBody["model"])
	}
	if gotBody["max_tokens"] == nil {
		t.Errorf("body.max_tokens missing")
	}
	if _, ok := gotBody["system"].([]interface{}); !ok {
		t.Errorf("body.system not array: %#v", gotBody["system"])
	}
	if _, ok := gotBody["messages"].([]interface{}); !ok {
		t.Errorf("body.messages not array")
	}
	if _, ok := gotBody["tools"].([]interface{}); !ok {
		t.Errorf("body.tools not array")
	}

	// Response parsing.
	if resp.ID != "msg_123" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "tool_use" || resp.Content[0].Name != "speak" {
		t.Errorf("parsed content = %+v", resp.Content)
	}
	if resp.Usage.InputTokens != 42 || resp.Usage.OutputTokens != 17 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

// TestProvider_UserAgent verifies that SetUserAgent injects the User-Agent
// header into every outbound LLM request, and that an empty User-Agent is
// omitted (Go's default takes over).
func TestProvider_UserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"type":"message","id":"m","model":"X","role":"assistant","content":[],"stop_reason":"end_turn","usage":{}}`))
	}))
	defer srv.Close()

	// With User-Agent set.
	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	p.SetUserAgent("LsmAgentGame/v1.2.3 Jan  1 2026 00:00:00")
	_, err := p.Chat(context.Background(), "sk-x", llm.LLMRequest{
		Model: "X", Messages: []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "hi"}}}}, MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if gotUA != "LsmAgentGame/v1.2.3 Jan  1 2026 00:00:00" {
		t.Errorf("User-Agent = %q, want LsmAgentGame/v1.2.3 ...", gotUA)
	}
}

// TestRegistry_SetUserAgent ensures SetUserAgent propagates to all providers.
func TestRegistry_SetUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"type":"message","id":"m","model":"MeiTuan-model","role":"assistant","content":[],"stop_reason":"end_turn","usage":{}}`))
	}))
	defer srv.Close()

	r := llm.NewRegistry(config.LLMConfig{
		Endpoint: srv.URL,
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "MeiTuan-model", APIKey: "sk-real"},
		},
	})
	r.SetUserAgent("LsmAgentGame/v9.9.9 TestBuild")

	provider, key, err := r.Get("MeiTuan-model")
	if err != nil {
		t.Fatalf("Get err: %v", err)
	}
	_, err = provider.Chat(context.Background(), key, llm.LLMRequest{
		Model: "MeiTuan-model", Messages: []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "hi"}}}}, MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if gotUA != "LsmAgentGame/v9.9.9 TestBuild" {
		t.Errorf("User-Agent = %q, want LsmAgentGame/v9.9.9 TestBuild", gotUA)
	}
}

// TestProviderChat_ParseText exercises the text + usage parsing branch.
func TestProviderChat_ParseText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"type":"message","id":"m2","model":"Kimi-model","role":"assistant",
			"content":[{"type":"text","text":"Hello there"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":3}
		}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	resp, err := p.Chat(context.Background(), "sk-x", llm.LLMRequest{
		Model: "Kimi-model", Messages: []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "hi"}}}}, MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Text() != "Hello there" {
		t.Errorf("Text = %q", resp.Text())
	}
	if len(resp.ToolUses()) != 0 {
		t.Errorf("ToolUses = %v", resp.ToolUses())
	}
}

// TestProviderChat_RetryOn5xx ensures we retry on 5xx max N times and surface a
// non-retryable error.
func TestProviderChat_RetryOn5xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"server_error","message":"boom"}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 2)
	_, err := p.Chat(context.Background(), "sk-x", llm.LLMRequest{
		Model: "X", Messages: []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "x"}}}}, MaxTokens: 10,
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if calls != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

// TestProviderChat_NoRetryOn4xx ensures 4xx (e.g. 401) fails fast.
func TestProviderChat_NoRetryOn4xx(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"auth","message":"bad key"}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 5)
	_, err := p.Chat(context.Background(), "sk-placeholder", llm.LLMRequest{
		Model: "X", Messages: []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "x"}}}}, MaxTokens: 10,
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on 4xx), got %d", calls)
	}
}

// TestRegistry_PlaceholderUnavailable verifies a placeholder key is rejected.
func TestRegistry_PlaceholderUnavailable(t *testing.T) {
	r := llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://localhost:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "MeiTuan-model", APIKey: llm.PlaceholderKey},
			{AgentName: "B", Model: "Real-model", APIKey: "sk-real"},
		},
	})
	if _, _, err := r.Get("MeiTuan-model"); err == nil {
		t.Errorf("expected placeholder to be unavailable")
	}
	if _, _, err := r.Get("Real-model"); err != nil {
		t.Errorf("expected real key to work: %v", err)
	}
	if r.Count() != 1 {
		t.Errorf("Count = %d", r.Count())
	}
	list := r.List()
	if len(list) != 2 {
		t.Errorf("List len = %d (placeholders must appear in List but Get rejects them)", len(list))
	}
}

// TestRegistry_UnusableProviders verifies that UnusableProviders enumerates
// every configured model that cannot drive an agent, with the correct reason
// per reachable key class (placeholder / empty_key) and in sorted order.
// This is the observability surface behind the BUG-R115-01 startup warning and
// GET /api/llm/health: the operator must see exactly which models need a real
// key rather than discovering N quarantined agents mid-game.
func TestRegistry_UnusableProviders(t *testing.T) {
	r := llm.NewRegistry(config.LLMConfig{
		Endpoint: "http://localhost:1/x",
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "MeiTuan-model", APIKey: llm.PlaceholderKey}, // placeholder
			{AgentName: "B", Model: "Real-model", APIKey: "sk-real"},            // usable
			{AgentName: "C", Model: "Empty-model", APIKey: ""},                  // empty_key
			{AgentName: "D", Model: "Space-model", APIKey: "   "},               // empty_key (trimmed)
		},
	})

	got := r.UnusableProviders()
	want := []llm.UnusableModel{
		{Model: "Empty-model", Reason: "empty_key"},
		{Model: "MeiTuan-model", Reason: "placeholder"},
		{Model: "Space-model", Reason: "empty_key"},
	}
	if len(got) != len(want) {
		t.Fatalf("UnusableProviders = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("UnusableProviders[%d] = %+v, want %+v (full: %+v)", i, got[i], want[i], got)
		}
	}

	// A fully-usable registry reports an empty unusable list.
	full := llm.NewRegistry(config.LLMConfig{
		Endpoint:  "http://localhost:1/x",
		Providers: []config.ProviderConfig{{AgentName: "B", Model: "Real-model", APIKey: "sk-real"}},
	})
	if u := full.UnusableProviders(); len(u) != 0 {
		t.Fatalf("fully-usable registry UnusableProviders = %+v, want empty", u)
	}

	// Health must carry the same list so /api/llm/health surfaces it, and the
	// usable count must exclude the unusable models.
	h := r.Health()
	if len(h.Unusable) != len(want) {
		t.Fatalf("Health().Unusable = %+v, want %+v", h.Unusable, want)
	}
	if h.UsableKeys != 1 {
		t.Errorf("Health().UsableKeys = %d, want 1", h.UsableKeys)
	}
}


// TestProviderChat_ToolUseInputNormalize verifies that a tool_use block with nil
// Input gets rewritten to {} before serialization (P0-8a / DouBao compatibility).
//
// BUG-R118-01 (2026-07-14 BUG-R117-01 修复回归): ensureAssistantMessageHasText
// 预置一个带非空 "text" 值的 neutral text 块(Text=" ",空格)到缺 text 的
// assistant 前(DouBao 严格校验器要求每个消息至少有一个 text 字段存在)。
// R117 初版修复用 Text="",被 ContentBlock.MarshalJSON 的 omitempty 把 "text"
// key 丢弃 → wire 退化为 `{"type":"text"}`(无 text 字段)→ 仍 400。
// 测试 fixture 发纯 tool_use assistant,断言 wire 上:
//   - 首个 text 块携带非空 "text" 字段(BUG-R118-01 回归守卫);
//   - tool_use 块 input 字段存在(原 P0-8a 不变量)。
func TestProviderChat_ToolUseInputNormalize(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"type":"message","id":"m","model":"DouBao-model","role":"assistant","content":[],"stop_reason":"end_turn","usage":{}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	req := llm.LLMRequest{
		Model: "DouBao-model",
		Messages: []llm.Message{
			{Role: "assistant", Content: []llm.ContentBlock{
				{Type: "tool_use", ID: "tu_1", Name: "speak", Input: nil},
			}},
		},
		MaxTokens: 50,
	}
	if _, err := p.Chat(context.Background(), "sk-x", req); err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("messages empty")
	}
	m0, _ := msgs[0].(map[string]any)
	cs, _ := m0["content"].([]any)
	if len(cs) == 0 {
		t.Fatalf("content empty")
	}
	// BUG-R117-01 / BUG-R118-01: cs[0] is now the prepended neutral text
	// block (with a non-empty "text" value); cs[1] holds the original
	// tool_use. Find blocks by type rather than relying on positional
	// indexing — this also future-proofs the test against further
	// pre-flight normalizations.
	var textBlock, toolUseBlock map[string]any
	for _, c := range cs {
		block, _ := c.(map[string]any)
		if block == nil {
			continue
		}
		switch block["type"] {
		case "text":
			textBlock = block
		case "tool_use":
			toolUseBlock = block
		}
	}
	if toolUseBlock == nil {
		t.Fatalf("tool_use block missing from wire body; got %+v", cs)
	}
	if _, ok := toolUseBlock["input"]; !ok {
		t.Errorf("tool_use.input key missing — DouBao would reject")
	}
	// BUG-R118-01 回归守卫: prepended text 块必须携带 "text" key且非空。
	// ContentBlock.MarshalJSON 在 text 分支对 Text 字段使用 `omitempty`,
	// 若 Text="" 则 wire 退化为 `{"type":"text"}`(无 text 字段),DouBao 校验器
	// 会报 400 "missing messages.content.text parameter" — R117 修复正是
	// 因此被 omitempty 吃掉而回归为 R118。
	if textBlock == nil {
		t.Fatalf("prepended text block missing from assistant wire body; got %+v", cs)
	}
	textVal, _ := textBlock["text"].(string)
	if _, ok := textBlock["text"]; !ok {
		t.Errorf("prepended text block missing 'text' key (omitempty dropped it) — DouBao would reject with 400; content=%+v", textBlock)
	} else if textVal == "" {
		// omitempty 边界: Text=="" 时 "text" key 被丢弃 → wire 退化为
		// `{"type":"text"}`(无 text 字段)→ DouBao 400 回归 R118。
		t.Errorf("prepended text block has empty 'text' value — omitempty would drop the 'text' key; content=%+v", textBlock)
	}
}

// TestRegistryHealthCheck_OK verifies a HEAD probe against a reachable
// 200-responding server marks the registry as healthy.
func TestRegistryHealthCheck_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.LLMConfig{
		Endpoint:   srv.URL,
		TimeoutMs:  3000,
		MaxRetries: 0,
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "A-model", APIKey: "key-a", ProviderType: "anthropic"},
		},
	}
	r := llm.NewRegistry(cfg)
	got := r.HealthCheck(context.Background())
	if !got.OK {
		t.Fatalf("expected OK=true, got error %q", got.LastError)
	}
	if got.UsableKeys != 1 {
		t.Errorf("expected 1 usable key, got %d", got.UsableKeys)
	}

	// Cached read should reflect the same OK state.
	cached := r.Health()
	if !cached.OK {
		t.Errorf("Health() should report OK after successful probe")
	}
}

// TestRegistryHealthCheck_Unreachable verifies that an unreachable endpoint
// populates LastError and leaves OK=false. Mirrors the Round 25 P0 symptom
// where the proxy returned 401 — the front-end /api/llm/health now surfaces
// the failure before any 7-agent room is created.
func TestRegistryHealthCheck_Unreachable(t *testing.T) {
	cfg := config.LLMConfig{
		Endpoint:   "http://127.0.0.1:1/never-listening",
		TimeoutMs:  500,
		MaxRetries: 0,
		Providers:  nil,
	}
	r := llm.NewRegistry(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	got := r.HealthCheck(ctx)
	if got.OK {
		t.Fatalf("expected OK=false on unreachable endpoint")
	}
	if got.LastError == "" {
		t.Errorf("expected non-empty LastError")
	}
}

// TestRegistryHealthCheck_NoEndpoint verifies the "no endpoint configured"
// branch — neither panics nor mis-reports OK.
func TestRegistryHealthCheck_NoEndpoint(t *testing.T) {
	cfg := config.LLMConfig{}
	r := llm.NewRegistry(cfg)
	got := r.HealthCheck(context.Background())
	if got.OK {
		t.Fatalf("expected OK=false on empty endpoint")
	}
	if !strings.Contains(got.LastError, "no endpoint") {
		t.Errorf("expected 'no endpoint' in LastError, got %q", got.LastError)
	}
}
