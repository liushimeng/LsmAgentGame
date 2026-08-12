// Package anthropic — StreamEvent / AccumulateStream tests. These guard the
// state-machine behavior described in stream.go: per-Index block builders,
// tool_use partial-JSON reassembly, error-event surfacing, and truncation.
package anthropic_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	anthropic "LsmAgentGame/llm/anthropic"
	llmtypes "LsmAgentGame/llm/types"
)

// runAccumulate drives one SSE body through AccumulateStream and returns
// the (resp, err) tuple. Tests use this helper rather than the lower-level
// ParseSSE so they exercise the full accumulator path.
func runAccumulate(body string) (llmtypes.LLMResponse, error) {
	return anthropic.AccumulateStream(context.Background(), strings.NewReader(body), nil)
}

// TestParseSSE_FixtureStream covers the happy-path ParseSSE walk: the
// onEvent callback fires once per SSE event in source order, with
// content_block_delta carrying the text fragment.
func TestParseSSE_FixtureStream(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"msg-1","model":"fake-model","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":7}}

event: message_stop
data: {"type":"message_stop"}

`
	var order []string
	var textParts []string
	err := anthropic.ParseSSE(strings.NewReader(body), func(ev llmtypes.StreamEvent) error {
		order = append(order, ev.Type)
		if ev.Type == "content_block_delta" && ev.DeltaType == "text_delta" {
			textParts = append(textParts, ev.Delta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ParseSSE err: %v", err)
	}
	wantOrder := []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(order) != len(wantOrder) {
		t.Fatalf("event order = %v, want %v", order, wantOrder)
	}
	for i, et := range wantOrder {
		if order[i] != et {
			t.Errorf("event[%d] = %q, want %q", i, order[i], et)
		}
	}
	if strings.Join(textParts, "") != "Hello, world" {
		t.Errorf("text delta accumulation = %q", strings.Join(textParts, ""))
	}
}

// TestAccumulateStream_HappyPath_Text asserts a clean text-only stream
// produces a single ContentBlock with the joined text and a populated
// StopReason / Usage.
func TestAccumulateStream_HappyPath_Text(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"claude-opus-4","usage":{"input_tokens":42,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Final answer."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`
	resp, err := runAccumulate(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.ID != "m" {
		t.Errorf("ID = %q", resp.ID)
	}
	if resp.Model != "claude-opus-4" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Content len = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Final answer." {
		t.Errorf("text block = %+v", resp.Content[0])
	}
	if resp.Usage.InputTokens != 42 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want input=42 output=5", resp.Usage)
	}
}

// TestAccumulateStream_ToolUsePartialJSON verifies that split
// input_json_delta chunks are reassembled into a valid Input map.
func TestAccumulateStream_ToolUsePartialJSON(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-1","name":"speak"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"te"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"xt\":\"he"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"llo\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

event: message_stop
data: {"type":"message_stop"}

`
	resp, err := runAccumulate(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Content len = %d", len(resp.Content))
	}
	tu := resp.Content[0]
	if tu.Type != "tool_use" || tu.ID != "call-1" || tu.Name != "speak" {
		t.Errorf("tool_use block = %+v", tu)
	}
	if got := tu.Input["text"]; got != "hello" {
		t.Errorf("input.text = %v, want hello", got)
	}
}


// TestAccumulateStream_Truncation: a clean EOF with a populated Content but
// no message_stop returns ErrStreamTruncated alongside the partial response.
func TestAccumulateStream_Truncation(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"partial","model":"x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"got cut off"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

`
	resp, err := runAccumulate(body)
	if !errors.Is(err, anthropic.ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	if resp.ID != "partial" {
		t.Errorf("ID = %q, want partial even on truncation", resp.ID)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "got cut off" {
		t.Errorf("partial content not retained: %+v", resp.Content)
	}
}

// TestAccumulateStream_ErrorEvent_503IsRetryable — BUG-R89 P0-cascade fix:
// when a SSE error event carries a 503-class message ("All endpoints
// failed" / "service unavailable" / "503"), the returned *Error must be
// Retryable:true so the agent's retry-cooldown window (agent/run.go
// failCooldownWindow=60s) absorbs the failure instead of cascading into
// 2-strike permanent quarantine in 12/13-bot shared-proxy rooms.
//
// Pre-fix behavior: mid-stream 503 was Retryable:false → cascading
// permanent quarantine on every bot within two wake cycles (R89 report:
// 12/12 bot LLM calls failed, agent coverage 0%).
func TestAccumulateStream_ErrorEvent_503IsRetryable(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"All endpoints failed"}}

`
	resp, err := runAccumulate(body)
	if err == nil {
		t.Fatalf("expected error from SSE error event, got nil; resp=%+v", resp)
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *anthropic.Error, got %T: %v", err, err)
	}
	if ae.Source != "stream" {
		t.Errorf("Source = %q, want %q", ae.Source, "stream")
	}
	if !ae.Retryable {
		t.Errorf("Retryable = false, want true (BUG-R89 cascade fix: 503 must trigger cooldown window, not permanent quarantine)")
	}
}

// TestAccumulateStream_ErrorEvent_Non503StaysNonRetryable — companion to the
// 503 test above. Non-503 stream errors (quota exceeded, auth failures,
// malformed requests) MUST stay Retryable:false so they escalate to fast
// quarantine via the agent's permanent-failure threshold (>=2 strikes).
// Otherwise permanent client-side errors would be absorbed by the cooldown
// window and never trigger the SkipPhaseAction auto-skip path.
func TestAccumulateStream_ErrorEvent_Non503StaysNonRetryable(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: error
data: {"type":"error","error":{"type":"authentication_error","message":"quota exhausted"}}

`
	_, err := runAccumulate(body)
	if err == nil {
		t.Fatalf("expected error from SSE error event")
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *anthropic.Error, got %T", err)
	}
	if ae.Retryable {
		t.Errorf("Retryable = true, want false (quota/auth errors are permanent client-side failures and must escalate to quarantine)")
	}
}

// TestAccumulateStream_OnProgressErrorAborts: an onProgress callback that
// returns a sentinel error short-circuits the stream; subsequent events
// are not delivered.
func TestAccumulateStream_OnProgressErrorAborts(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: message_stop
data: {"type":"message_stop"}

`
	sentinel := io.ErrClosedPipe
	count := 0
	_, err := anthropic.AccumulateStream(context.Background(), strings.NewReader(body), func(ev llmtypes.StreamEvent) error {
		count++
		if ev.Type == "content_block_start" {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if count != 3 {
		// 2026-07-12 §127: 首个 content_block_start 触发一次额外的 _first_token
		// 轻量回调(给调用方切 PhaseStreaming)。完整序列:message_start,
		// _first_token, content_block_start → 3 次。
		t.Errorf("onProgress fired %d times; expected to abort after content_block_start + _first_token", count)
	}
}

// TestAccumulateStream_MultipleToolUses: two tool_use blocks at Index 0 and
// Index 1 are emitted in arrival order, each carrying their own ID / Name / Input.
func TestAccumulateStream_MultipleToolUses(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"x"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c-1","name":"wolf_kill"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"target\":3}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c-2","name":"whisper"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"to\":4,\"text\":\"secret\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

event: message_stop
data: {"type":"message_stop"}

`
	resp, err := runAccumulate(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("Content len = %d, want 2", len(resp.Content))
	}
	if resp.Content[0].Name != "wolf_kill" {
		t.Errorf("block[0].Name = %q", resp.Content[0].Name)
	}
	if got := resp.Content[0].Input["target"]; got != float64(3) {
		t.Errorf("block[0].target = %v, want 3", got)
	}
	if resp.Content[1].Name != "whisper" {
		t.Errorf("block[1].Name = %q", resp.Content[1].Name)
	}
	if got := resp.Content[1].Input["to"]; got != float64(4) {
		t.Errorf("block[1].to = %v, want 4", got)
	}
}

// TestAccumulateStream_CacheTokenDelta: message_delta's incremental
// input_tokens (cache read/write) are added to the seed value from
// message_start; output_tokens from message_delta OVERWRITES the seed.
func TestAccumulateStream_CacheTokenDelta(t *testing.T) {
	const body = `event: message_start
data: {"type":"message_start","message":{"id":"m","model":"x","usage":{"input_tokens":100,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":25,"output_tokens":13}}

event: message_stop
data: {"type":"message_stop"}

`
	resp, err := runAccumulate(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Usage.InputTokens != 125 { // 100 seed + 25 cache delta
		t.Errorf("Usage.InputTokens = %d, want 125", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 13 { // overwritten (cumulative final)
		t.Errorf("Usage.OutputTokens = %d, want 13", resp.Usage.OutputTokens)
	}
}

// TestParseSSE_MalformedJSONNonFatal: a single event with broken JSON must
// not abort the stream — subsequent valid events still arrive.
func TestParseSSE_MalformedJSONNonFatal(t *testing.T) {
	const body = `event: message_start
data: this-is-not-json

event: ping
data: {"type":"ping"}

event: message_stop
data: {"type":"message_stop"}

`
	var types []string
	err := anthropic.ParseSSE(strings.NewReader(body), func(ev llmtypes.StreamEvent) error {
		types = append(types, ev.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("ParseSSE err = %v, want nil (malformed event is non-fatal)", err)
	}
	want := []string{"ping", "message_stop"} // message_start dropped due to bad JSON
	if len(types) != len(want) {
		t.Fatalf("types = %v, want %v", types, want)
	}
	for i, et := range want {
		if types[i] != et {
			t.Errorf("types[%d] = %q, want %q", i, types[i], et)
		}
	}
}
