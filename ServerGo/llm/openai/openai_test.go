package openai

import (
	"encoding/json"
	"strings"
	"testing"

	types "LsmAgentGame/llm/types"
)

// TestChatCompletionsURL verifies the §3 URL auto-append rules.
func TestChatCompletionsURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"http://proxy:8080/openai", "http://proxy:8080/openai/chat/completions"},
		{"http://proxy:8080/openai/chat/completions", "http://proxy:8080/openai/chat/completions"},
		{"", ""},
	}
	for _, c := range cases {
		if got := ChatCompletionsURL(c.in); got != c.want {
			t.Errorf("ChatCompletionsURL(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func baseReq() types.LLMRequest {
	return types.LLMRequest{
		Model:     "gpt-test",
		MaxTokens: 100,
		System:    []types.SystemBlock{{Type: "text", Text: "be kind"}, {Type: "text", Text: "be terse"}},
		Messages: []types.Message{
			{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}},
			{Role: "assistant", Content: []types.ContentBlock{{Type: "text", Text: "hello"}}},
			{Role: "assistant", Content: []types.ContentBlock{{
				Type: "tool_use", ID: "call_1", Name: "speak",
				Input: map[string]any{"text": "hi"},
			}}},
			{Role: "user", Content: []types.ContentBlock{{
				Type: "tool_result", ToolUseID: "call_1", Content: []types.ContentBlock{{Type: "text", Text: "ok"}},
			}}},
			{Role: "assistant", Content: []types.ContentBlock{{Type: "thinking", ThinkingBudget: 100}}},
		},
		Tools: []types.ToolDef{{
			Name: "speak", Description: "speak", InputSchema: map[string]any{
				"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}},
			},
		}},
	}
}

// TestBuildRequest_Conversion verifies the full message/tool mapping (design doc
// §2.3–§2.5): system join, text only, assistant tool_calls, user tool_result,
// thinking dropped, tool input JSON-stringified.
func TestBuildRequest_Conversion(t *testing.T) {
	r := buildRequest(baseReq(), false)
	// system → leading system message with \n\n join.
	if r.Messages[0].Role != "system" || r.Messages[0].Content != "be kind\n\nbe terse" {
		t.Fatalf("system message wrong: %+v", r.Messages[0])
	}
	// assistant tool_use → tool_calls with arguments as JSON STRING.
	found := false
	for _, m := range r.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			found = true
			if m.ToolCalls[0].ID != "call_1" || m.ToolCalls[0].Function.Name != "speak" {
				t.Fatalf("tool call id/name wrong: %+v", m.ToolCalls[0])
			}
			if !strings.Contains(m.ToolCalls[0].Function.Arguments, `"text"`) {
				t.Fatalf("arguments should be JSON string, got %q", m.ToolCalls[0].Function.Arguments)
			}
		}
	}
	if !found {
		t.Fatal("no assistant tool_calls produced")
	}
	// user tool_result → role:"tool" message.
	foundTool := false
	for _, m := range r.Messages {
		if m.Role == "tool" {
			foundTool = true
			if m.ToolCallID != "call_1" {
				t.Fatalf("tool message tool_call_id wrong: %q", m.ToolCallID)
			}
		}
	}
	if !foundTool {
		t.Fatal("no role:tool message produced for tool_result")
	}
	// thinking dropped: the final assistant turn carried only a thinking block,
	// which is dropped, leaving an assistant message with empty content. Total:
	// sys(1) + user(1) + assistant(1) + assistant+tool_calls(1) + tool(1) +
	// assistant-empty(1) = 6.
	if len(r.Messages) != 6 {
		t.Fatalf("expected 6 messages, got %d: %+v", len(r.Messages), r.Messages)
	}
	// The last (thinking-source) assistant message must carry no leaked
	// reasoning into the OpenAI content string.
	if last := r.Messages[len(r.Messages)-1]; last.Role != "assistant" {
		t.Fatalf("last message role wrong: %+v", last)
	}
	// tools[] envelope.
	if len(r.Tools) != 1 || r.Tools[0].Type != "function" || r.Tools[0].Function.Name != "speak" {
		t.Fatalf("tools wrong: %+v", r.Tools)
	}
	// stream+stream_options.
	sr := buildRequest(baseReq(), true)
	if !sr.Stream || sr.StreamOpts == nil || !sr.StreamOpts.IncludeUsage {
		t.Fatal("stream request missing stream_options.include_usage")
	}
}

// TestNormalizeResponse maps an OpenAI chat.completion body (mirrors the
// authoritative ResponseBody capture) into LLMResponse.
func TestNormalizeResponse(t *testing.T) {
	body := `{
		"id":"chatcmpl-x","object":"chat.completion","model":"qwen3.8-max",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi",
			"reasoning_content":"thought…",
			"tool_calls":[{"id":"call_1","type":"function",
			  "function":{"name":"speak","arguments":"{\"text\":\"hello\"}"}}]},
		  "finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`
	var cr chatResponse
	if err := json.Unmarshal([]byte(body), &cr); err != nil {
		t.Fatal(err)
	}
	resp := normalizeResponse(cr)
	if resp.ID != "chatcmpl-x" || resp.Model != "qwen3.8-max" {
		t.Fatalf("id/model wrong: %+v", resp)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop_reason want tool_use got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("usage wrong: %+v", resp.Usage)
	}
	// reasoning_content must NOT appear in Content (only text + tool_use).
	if len(resp.Content) != 2 {
		t.Fatalf("want 2 content blocks (text+tool_use), got %d: %+v", len(resp.Content), resp.Content)
	}
	if resp.Content[0].Type != "text" || resp.Content[1].Type != "tool_use" {
		t.Fatalf("content order wrong: %+v", resp.Content)
	}
	if resp.Content[1].Name != "speak" {
		t.Fatalf("tool name wrong: %+v", resp.Content[1])
	}
}

// TestAccumulateStream reconstructs text + a multi-chunk tool call with a
// fragmented arguments string (the real-world SSE shape) and the [DONE] sentinel.
func TestAccumulateStream(t *testing.T) {
	sse := "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"m\"," +
		"\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n" +
		"\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n" +
		"\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n" +
		"\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"r\"}}]}\n" +
		"\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0," +
		"\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"speak\",\"arguments\":\"\"}}]}}]}\n" +
		"\n" +
		// arguments for the tool: the full JSON is {"text":"hi"}, deliberately
		// split across two chunks (mid-string) to exercise fragment reassembly.
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0," +
		"\"function\":{\"arguments\":\"{\\\"text\\\":\\\"hi\"}}]}}]}\n" +
		"\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0," +
		"\"function\":{\"arguments\":\"\\\"}\"}}]}}]}\n" +
		"\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]," +
		"\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n" +
		"\n" +
		"data: [DONE]\n\n"

	var firstToken, stopSeen bool
	resp, err := AccumulateStream(strings.NewReader(sse), func(ev types.StreamEvent) error {
		if ev.Type == "_first_token" {
			firstToken = true
		}
		if ev.Type == "message_stop" {
			stopSeen = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("AccumulateStream err: %v", err)
	}
	if !firstToken || !stopSeen {
		t.Fatalf("progress events wrong: firstToken=%v stopSeen=%v", firstToken, stopSeen)
	}
	if resp.ID != "c1" || resp.Model != "m" {
		t.Fatalf("id/model wrong: %+v", resp)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop reason want tool_use got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("usage wrong: %+v", resp.Usage)
	}
	// text "Hello" + reasoning dropped + tool_use speak {text:hi}.
	if len(resp.Content) != 2 {
		t.Fatalf("want 2 content blocks, got %d: %+v", len(resp.Content), resp.Content)
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Hello" {
		t.Fatalf("text block wrong: %+v", resp.Content[0])
	}
	if resp.Content[1].Type != "tool_use" || resp.Content[1].Name != "speak" {
		t.Fatalf("tool block wrong: %+v", resp.Content[1])
	}
	in := resp.Content[1].Input
	if in["text"] != "hi" {
		t.Fatalf("tool input wrong: %+v", in)
	}
	// _partial must NOT appear when arguments parsed cleanly.
	if _, ok := in["_partial"]; ok {
		t.Fatalf("unexpected _partial in clean input: %+v", in)
	}
}

// TestAccumulateStream_Truncated: EOF with content but no [DONE]/finish returns
// ErrStreamTruncated + the partial response.
func TestAccumulateStream_Truncated(t *testing.T) {
	sse := "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n"
	resp, err := AccumulateStream(strings.NewReader(sse), nil)
	if err != ErrStreamTruncated {
		t.Fatalf("want ErrStreamTruncated, got %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hel" {
		t.Fatalf("partial response wrong: %+v", resp)
	}
}

// TestMapFinishReason covers the known enum values.
func TestMapFinishReason(t *testing.T) {
	cases := map[string]string{
		"stop": "end_turn", "tool_calls": "tool_use", "length": "max_tokens",
		"content_filter": "content_filter", "unknown": "unknown",
	}
	for in, want := range cases {
		if got := mapFinishReason(in); got != want {
			t.Errorf("mapFinishReason(%q)=%q want %q", in, got, want)
		}
	}
}
