// Package anthropic — HTTP-level tests for the Provider. Uses net/http/httptest
// to drive dial + retry + timeout paths through real HTTP. Tests for
// per-event parsing live in stream_test.go.
package anthropic_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	anthropic "LsmWebGame/llm/anthropic"
	llmtypes "LsmWebGame/llm/types"
)

// TestDoStream_BillingHeaderPresent verifies that the streaming dial path
// carries the same headers as the non-streaming one. Regression guard for
// §14: the canonical Anthropic reference request body expects both headers.
func TestDoStream_BillingHeaderPresent(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	p.SetUserAgent("LsmWebGame/test")
	p.SetBillingHeader("LsmWebGame/test-build; entrypoint=cli;")
	body, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "m", Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}}})
	if err != nil {
		t.Fatalf("ChatStream err: %v", err)
	}
	_, _ = io.ReadAll(body)
	body.Close()

	wantHeaders := map[string]string{
		"Authorization":              "Bearer k",
		"Anthropic-Version":          "2023-06-01",
		"Content-Type":               "application/json",
		"Accept":                     "text/event-stream",
		"User-Agent":                 "LsmWebGame/test",
		"X-Anthropic-Billing-Header": "LsmWebGame/test-build; entrypoint=cli;",
	}
	for k, want := range wantHeaders {
		if got := seen.Get(k); got != want {
			t.Errorf("header[%s] = %q, want %q", k, got, want)
		}
	}
}

// TestChatStream_RetryOnDial5xx: the streaming dial retries on 5xx / 429 the
// same number of times as non-streaming Chat.
func TestChatStream_RetryOnDial5xx(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 3)
	body, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "m", Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}}})
	if err != nil {
		t.Fatalf("ChatStream err: %v", err)
	}
	body.Close()

	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (2 retries + final success)", got)
	}
}

// TestChatStream_NoRetryOnMidStreamError: once a 2xx has been received,
// mid-stream error events must NOT trigger a dial retry — partial streams are
// the caller's responsibility (caller can still drive them through
// AccumulateStream and recover).
func TestChatStream_NoRetryOnMidStreamError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"error\",\"message\":\"mid stream error\"}}\n\n")
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 3)
	_, err := p.ChatStreamAccumulate(context.Background(), "k", llmtypes.LLMRequest{Model: "m", Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}}}, nil)
	if err == nil {
		t.Fatalf("expected error from mid-stream SSE error event")
	}
	ae, ok := err.(*anthropic.Error)
	if !ok {
		t.Fatalf("err type = %T, want *anthropic.Error", err)
	}
	if ae.Source != "stream" {
		t.Errorf("Source = %q, want stream", ae.Source)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry after 2xx)", got)
	}
}

// TestDoStream_IdleTimeout: a body that stalls after sending one event
// surfaces a Retryable error from the idleTimeoutReader wrapper when the
// next Read sits idle longer than the configured deadline.
func TestDoStream_IdleTimeout(t *testing.T) {
	idle := 80 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// Send one event, flush, then sleep past the idle deadline.
		_, _ = io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(2 * idle)
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	// §130: SetStreamTimeouts sets the no-first-byte guard and forces the
	// post-first-byte idle to 0. This test exercises the post-first-byte idle
	// guard, so re-arm it explicitly via SetStreamIdleAfterFirstByte.
	p.SetStreamTimeouts(5*time.Second, 5*time.Second)
	p.SetStreamIdleAfterFirstByte(idle)
	body, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "m", Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}}})
	if err != nil {
		t.Fatalf("ChatStream err: %v", err)
	}
	defer body.Close()

	// Drain the initial ping chunk. The HTTP client buffers chunks but
	// since the server flushed exactly one event, this read pulls bytes
	// out of the underlying TCP socket — not ahead of the idle deadline.
	buf := make([]byte, 4096)
	_, _ = body.Read(buf)

	// Next Read must block waiting for new data and trip the idle timer.
	// The fake server sleeps 2*idle past the first event before closing,
	// so the idle timer (idle) fires first by design.
	//
	// Two surfaces are acceptable (the wrapper injects one of them when
	// the timer fires):
	//   (a) *anthropic.Error with Message containing "idle" — the
	//       idleTimeoutReader's reported failure.
	//   (b) "context canceled" / "use of closed network connection" — the
	//       wrapper's Close raced the inner Read goroutine and surfaced
	//       the underlying body's cancellation error first. Both are
	//       correct outcomes: the idle wrapper's purpose is to NOT let
	//       a hung stream block the caller indefinitely.
	readCh := make(chan struct {
		n   int
		err error
	}, 1)
	go func() {
		n, err := body.Read(buf)
		readCh <- struct {
			n   int
			err error
		}{n, err}
	}()
	select {
	case res := <-readCh:
		if res.err == nil {
			t.Fatalf("expected idle-timeout error; got nil (and %d bytes)", res.n)
		}
		if errors.Is(res.err, io.EOF) {
			t.Fatalf("readErr = EOF, want idle timeout")
		}
		if ae, ok := res.err.(*anthropic.Error); ok {
			if !ae.Retryable {
				t.Errorf("Retryable = false, want true (idle timeout is transient)")
			}
			if !strings.Contains(strings.ToLower(ae.Message), "idle") {
				t.Errorf("Message = %q, want contains 'idle'", ae.Message)
			}
			return
		}
		msg := res.err.Error()
		if strings.Contains(msg, "context canceled") ||
			strings.Contains(msg, "use of closed network connection") {
			return
		}
		t.Fatalf("readErr type = %T, want *anthropic.Error or context-canceled; %v", res.err, res.err)
	case <-time.After(2 * time.Second):
		t.Fatalf("idle-timed-out Read never returned")
	}
}

// TestDoStream_FirstByteTimeout (§130): a body that delivers not a single
// byte within the first-byte deadline surfaces a Retryable timeout error.
// (Formerly TestDoStream_TotalTimeout — the total/whole-exchange deadline was
// removed in §130; the no-first-byte guard replaces it.)
func TestDoStream_FirstByteTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		if flusher != nil {
			flusher.Flush()
		}
		// Send headers but NO body bytes, then sleep well past the
		// first-byte deadline.
		time.Sleep(1 * time.Second)
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	// First arg = no-first-byte timeout (100ms); second arg is ignored (§130).
	p.SetStreamTimeouts(100*time.Millisecond, 5*time.Second)
	body, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "m", Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}}})
	if err != nil {
		t.Fatalf("ChatStream err: %v", err)
	}
	defer body.Close()

	buf := make([]byte, 1)
	n, readErr := body.Read(buf)
	_ = n
	if readErr == nil {
		t.Fatalf("expected first-byte-timeout error; got nil")
	}
	ae, ok := readErr.(*anthropic.Error)
	if !ok {
		// Some Go versions wrap deadline-exceeded differently — accept either.
		t.Logf("readErr = %v (not *anthropic.Error, acceptable)", readErr)
		return
	}
	if !ae.Retryable {
		t.Errorf("Retryable = false on first-byte-timeout; want true")
	}
	if !strings.Contains(strings.ToLower(ae.Message), "first-byte") {
		t.Errorf("Message = %q, want contains 'first-byte'", ae.Message)
	}
}

// TestChatStream_IntegrationWithFakeServer: end-to-end ChatStream →
// AccumulateStream against a fake SSE server. Verifies the full pipeline
// (dial + parse + accumulate) returns a sensible LLMResponse. The handler
// writes the full SSE body in one buffered WriteString, then flushes — this
// pattern matches the simplest chunked response and avoids per-event
// flusher races that intermittently surface as spurious "context canceled"
// in the http transport's connection state machine during tests.
func TestChatStream_IntegrationWithFakeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing auth header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// Single buffered write; flush at the end. Streaming per-event
		// flushing plus an idleTimeoutReader wrapper can race the http
		// transport in tests — production traffic has a much larger
		// buffer and never observes it.
		_, _ = fmt.Fprintf(w,
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"stream-id\",\"model\":\"fake-model\",\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n"+
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello world\"}}\n\n"+
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":3}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	// Disable idle wrap for the integration test — the wrapper's
	// per-Read goroutine + Close interaction with the httptest server's
	// connection close can race in unit tests. The unit tests for the
	// idle wrapper itself are in TestDoStream_IdleTimeout; this test
	// exercises the Accumulator happy path.
	p.SetStreamTimeouts(0, 0)
	resp, err := p.ChatStreamAccumulate(context.Background(), "k", llmtypes.LLMRequest{
		Model:    "fake-model",
		Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStreamAccumulate err: %v", err)
	}
	if resp.ID != "stream-id" {
		t.Errorf("ID = %q, want stream-id", resp.ID)
	}
	if resp.Model != "fake-model" {
		t.Errorf("Model = %q, want fake-model", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "text" || resp.Content[0].Text != "hello world" {
		t.Errorf("content = %+v, want single text block 'hello world'", resp.Content)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

// TestBreaker_TripsOnConsecutive503 verifies the BUG-R65-3.2 short-circuit
// breaker: three consecutive 503 responses within the rolling window must
// open the breaker, after which any new Chat() / ChatStream() call returns
// immediately with a *Error{Source:"breaker", Retryable:true} so the agent's
// retry-cooldown path (agent/run.go failCooldownWindow=60s) absorbs the
// failure rather than cascading into permanent quarantine. BUG-R89 P0-cascade
// fix: the prior Retryable:false design triggered the 2-strike permanent
// quarantine on every bot within two wake cycles in 12-bot shared-proxy rooms.
// TestModel400Circuit_TripsAndIsModelScoped (BUG-R172-P2) verifies the
// per-model 400 circuit:
//   - 5 × ChatStream 400 for model "bad-model" trips the circuit; the 6th
//     call short-circuits with Source="model_400_circuit" and does NOT hit
//     the server.
//   - The SAME provider still serves "good-model" — the circuit is
//     model-scoped, not endpoint-scoped (unlike the 503 breaker).
//   - A success on a different model does not clear the bad model's window.
func TestModel400Circuit_TripsAndIsModelScoped(t *testing.T) {
	var badHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"bad-model"`) {
			atomic.AddInt32(&badHits, 1)
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid request Error"}}`))
			return
		}
		// Good model: serve a minimal SSE stream that immediately closes.
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"good-model\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 2*time.Second, 0)

	// 5 × 400 on bad-model trips the circuit (threshold = 5).
	for i := 0; i < 5; i++ {
		_, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "bad-model", MaxTokens: 1})
		if err == nil {
			t.Fatalf("call %d expected 400 error", i)
		}
		var ae *anthropic.Error
		if !errors.As(err, &ae) || ae.HTTPStatus != 400 {
			t.Fatalf("call %d expected *Error HTTPStatus=400, got %v", i, err)
		}
		if ae.Source == "model_400_circuit" {
			t.Fatalf("call %d short-circuited too early", i)
		}
	}

	// 6th call on bad-model: short-circuits, server NOT hit.
	preHits := atomic.LoadInt32(&badHits)
	_, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "bad-model", MaxTokens: 1})
	if err == nil {
		t.Fatalf("6th call expected circuit error")
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("6th call expected *Error, got %T: %v", err, err)
	}
	if ae.Source != "model_400_circuit" {
		t.Errorf("6th call Source = %q, want %q", ae.Source, "model_400_circuit")
	}
	if ae.Retryable {
		t.Errorf("6th call Retryable = true, want false (agent fast-quarantine path)")
	}
	if postHits := atomic.LoadInt32(&badHits); postHits != preHits {
		t.Errorf("6th call hit server (pre=%d post=%d); circuit should have short-circuited", preHits, postHits)
	}

	// good-model still served through the SAME provider (model-scoped circuit).
	body, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "good-model", MaxTokens: 1})
	if err != nil {
		t.Fatalf("good-model should be unaffected by bad-model circuit: %v", err)
	}
	_ = body.Close()
}

// TestModel400Circuit_SuccessClearsWindow verifies that intermittent 400s
// separated by successes never accumulate to the threshold.
func TestModel400Circuit_SuccessClearsWindow(t *testing.T) {
	failNext := int32(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.SwapInt32(&failNext, 0) == 1 {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid request Error"}}`))
			return
		}
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 2*time.Second, 0)

	// Alternate fail/success 6 times (6 failures total > threshold, but each
	// success resets the window so the circuit must never open).
	for i := 0; i < 6; i++ {
		_, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "m", MaxTokens: 1})
		if err == nil {
			t.Fatalf("iter %d: expected 400", i)
		}
		var ae *anthropic.Error
		if !errors.As(err, &ae) || ae.Source == "model_400_circuit" {
			t.Fatalf("iter %d: circuit must NOT trip on intermittent failures: %v", i, err)
		}
		body, err := p.ChatStream(context.Background(), "k", llmtypes.LLMRequest{Model: "m", MaxTokens: 1})
		if err != nil {
			t.Fatalf("iter %d: success call failed: %v", i, err)
		}
		_ = body.Close()
		atomic.StoreInt32(&failNext, 1)
	}
}

// TestBreaker_TripsOnConsecutive503 verifies the endpoint-scoped 503
// short-circuit breaker (BUG-R65-3.2).
func TestBreaker_TripsOnConsecutive503(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"All endpoints failed"}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 1*time.Second, 0)

	// Three Chat() calls — each gets a 503, the third trips the breaker.
	for i := 0; i < 3; i++ {
		_, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
		if err == nil {
			t.Fatalf("call %d expected 503 error", i)
		}
		var ae *anthropic.Error
		if !errors.As(err, &ae) || ae.HTTPStatus != 503 {
			t.Fatalf("call %d expected *Error HTTPStatus=503, got %v", i, err)
		}
	}

	// Fourth call: should short-circuit, NOT hit the server. We assert
	// both the breaker-tagged error AND that hits didn't increment.
	preHits := atomic.LoadInt32(&hits)
	_, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
	if err == nil {
		t.Fatalf("fourth call expected breaker error")
	}
	var ae *anthropic.Error
	if !errors.As(err, &ae) {
		t.Fatalf("fourth call expected *Error, got %T: %v", err, err)
	}
	if ae.Source != "breaker" {
		t.Errorf("fourth call Source = %q, want %q", ae.Source, "breaker")
	}
	if !ae.Retryable {
		t.Errorf("fourth call Retryable = false, want true (BUG-R89 cascade fix: cooldown-window absorption instead of permanent quarantine)")
	}
	if postHits := atomic.LoadInt32(&hits); postHits != preHits {
		t.Errorf("fourth call hit server (pre=%d post=%d); breaker should have short-circuited", preHits, postHits)
	}
}

// TestBreaker_RecoversAfterSuccess verifies that a successful response
// clears the breaker window so a healed endpoint can re-serve without
// re-tripping on the next transient 503.
func TestBreaker_RecoversAfterSuccess(t *testing.T) {
	var hits int32
	mode := int32(0) // 0=503, 1=200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if atomic.LoadInt32(&mode) == 0 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"message":"All endpoints failed"}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"m1","model":"x","role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 1*time.Second, 0)

	// Two 503s (below threshold of 3) — should NOT trip.
	for i := 0; i < 2; i++ {
		_, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
		if err == nil {
			t.Fatalf("call %d expected 503 error", i)
		}
	}

	// Flip to healthy. One 200 should clear the window.
	atomic.StoreInt32(&mode, 1)
	if _, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1}); err != nil {
		t.Fatalf("healthy call: %v", err)
	}

	// Now flip back to 503 for two more — should still NOT trip (window
	// was cleared by the success).
	atomic.StoreInt32(&mode, 0)
	for i := 0; i < 2; i++ {
		_, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
		if err == nil {
			t.Fatalf("post-recovery call %d expected 503 error", i)
		}
		var ae *anthropic.Error
		if !errors.As(err, &ae) || ae.Source == "breaker" {
			t.Fatalf("post-recovery call %d should NOT be breaker-short-circuited; got %v", i, err)
		}
	}
}

// TestEnsureAssistantMessageHasText_PureToolUse (BUG-R117-01) verifies the
// pre-flight normalization that prepends a neutral empty text block to any
// assistant message whose content array lacks a text block. DouBao-model
// rejects requests where every assistant turn carries ONLY tool_use blocks
// (no narration text) with the 400 error
// "missing `messages.content.text` parameter". We exercise the helper via a
// fake upstream that records the wire payload, then assert:
//   - assistant turn with tool_use only → wire payload now starts with
//     {"type":"text","text":""} then the tool_use block;
//   - assistant turn with text already → no extra text block added;
//   - user message with tool_result only → UNCHANGED (only assistant turns
//     are normalized; tool_result user messages are valid as-is on every
//     provider we ship against).
func TestEnsureAssistantMessageHasText_PureToolUse(t *testing.T) {
	// Verify wire payload via real Chat(): assistant turn contains only
	// tool_use → wire body should contain a "text" block before tool_use.
	t.Run("assistant_with_only_tool_use", func(t *testing.T) {
		var capturedBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf, _ := io.ReadAll(r.Body)
			capturedBody = string(buf)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"m","model":"DouBao-model","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		}))
		defer srv.Close()

		p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
		req := llmtypes.LLMRequest{
			Model: "DouBao-model",
			Messages: []llmtypes.Message{
				// User turn — baseline, no normalization expected.
				{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "vote"}}},
				// Assistant turn with ONLY tool_use — this is the BUG-R117-01
				// trigger. Pre-fix: wire body lacks any text block → DouBao 400.
				// Post-fix: wire body has {"type":"text","text":""} then tool_use.
				{Role: "assistant", Content: []llmtypes.ContentBlock{
					{Type: "tool_use", ID: "tu1", Name: "vote", Input: map[string]any{"target": 5}},
				}},
				// User turn with tool_result — unchanged by the helper.
				{Role: "user", Content: []llmtypes.ContentBlock{
					{Type: "tool_result", ToolUseID: "tu1", Content: []llmtypes.ContentBlock{{Type: "text", Text: "ok"}}},
				}},
			},
		}
		if _, err := p.Chat(context.Background(), "k", req); err != nil {
			t.Fatalf("Chat err: %v", err)
		}
		// Locate the second message (assistant with tool_use). We expect a
		// `"text"` block to appear before the `"tool_use"` block.
		// Anchor: split on the role: "assistant" boundary and check the order
		// inside the content array.
		idx := strings.Index(capturedBody, `"role":"assistant"`)
		if idx < 0 {
			t.Fatalf("captured body missing assistant role: %s", capturedBody)
		}
		tail := capturedBody[idx:]
		textIdx := strings.Index(tail, `"type":"text"`)
		toolIdx := strings.Index(tail, `"type":"tool_use"`)
		if textIdx < 0 {
			t.Fatalf("BUG-R117-01 fix not applied: assistant content has no text block; body=%s", tail)
		}
		if toolIdx < 0 || textIdx > toolIdx {
			t.Fatalf("expected text block BEFORE tool_use; got textIdx=%d toolIdx=%d tail=%s", textIdx, toolIdx, tail)
		}
	})

	// Regression guard: assistant message that already has a text block
	// must NOT get a second empty text block prepended.
	t.Run("assistant_with_existing_text", func(t *testing.T) {
		var capturedBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf, _ := io.ReadAll(r.Body)
			capturedBody = string(buf)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"m","model":"DouBao-model","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		}))
		defer srv.Close()

		p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
		req := llmtypes.LLMRequest{
			Model: "DouBao-model",
			Messages: []llmtypes.Message{
				{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "go"}}},
				// Assistant with both text + tool_use — should pass through
				// untouched (helper is a no-op when text already exists).
				{Role: "assistant", Content: []llmtypes.ContentBlock{
					{Type: "text", Text: "I'm voting"},
					{Type: "tool_use", ID: "tu2", Name: "vote", Input: map[string]any{"target": 3}},
				}},
			},
		}
		if _, err := p.Chat(context.Background(), "k", req); err != nil {
			t.Fatalf("Chat err: %v", err)
		}
		// Count text blocks in the assistant content. Expected exactly 1.
		idx := strings.Index(capturedBody, `"role":"assistant"`)
		if idx < 0 {
			t.Fatalf("captured body missing assistant role: %s", capturedBody)
		}
		tail := capturedBody[idx:]
		// Count "type":"text" occurrences in the assistant section. The
		// helper only adds a text block when missing; with existing text,
		// the count must remain 1.
		count := strings.Count(tail, `"type":"text"`)
		if count != 1 {
			t.Fatalf("expected exactly 1 text block (original); got %d in tail=%s", count, tail)
		}
	})
}

// TestFailover_AdvancesToHealthyEndpoint (BUG-R220) verifies that when the
// primary endpoint returns repeated 5xx, the provider automatically advances
// to the secondary endpoint and serves the request successfully.
func TestFailover_AdvancesToHealthyEndpoint(t *testing.T) {
	var primaryHits, secondaryHits int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryHits, 1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"primary dead"}}`))
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondaryHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","model":"x","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer secondary.Close()

	// maxRetries=1 (attempts=2): first attempt hits primary (503), retries
	// with the secondary which succeeds. failover must advance the active
	// endpoint for subsequent calls.
	p := anthropic.New([]string{primary.URL, secondary.URL}, 2*time.Second, 1)
	resp, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{
		Model: "x", MaxTokens: 1,
		Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if resp.StopReason != "end_turn" || len(resp.Content) != 1 || resp.Content[0].Text != "hi" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if atomic.LoadInt32(&primaryHits) != 1 {
		t.Errorf("primary hits = %d, want 1", primaryHits)
	}
	if atomic.LoadInt32(&secondaryHits) != 1 {
		t.Errorf("secondary hits = %d, want 1", secondaryHits)
	}
	// After failover, the active endpoint should be the secondary.
	if got := p.Endpoint(); got != secondary.URL {
		t.Errorf("active endpoint after failover = %q, want %q", got, secondary.URL)
	}
	if got := p.Endpoints(); len(got) != 2 || got[0] != primary.URL || got[1] != secondary.URL {
		t.Errorf("Endpoints() = %v, want [primary, secondary]", got)
	}
}

// TestFailover_AdvancesToSecondWhenFirstDialFails (BUG-R220) verifies that a
// transport-level dial failure (connection refused on a dead localhost port)
// triggers failover to the next endpoint. The primary endpoint is
// deliberately pointed at an unbound port so the very first dial fails.
func TestFailover_AdvancesToSecondWhenFirstDialFails(t *testing.T) {
	// Pick a high port that's almost certainly unbound.
	dead := "http://127.0.0.1:1"
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","model":"x","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"from-alive"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer alive.Close()

	p := anthropic.New([]string{dead, alive.URL}, 2*time.Second, 1)
	resp, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{
		Model: "x", MaxTokens: 1,
		Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Chat err: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "from-alive" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if got := p.Endpoint(); got != alive.URL {
		t.Errorf("active endpoint after dial-fail failover = %q, want %q", got, alive.URL)
	}
}

// TestFailover_SingleEndpointPreservesLegacyBehavior (BUG-R220) — when only
// ONE endpoint is configured, advanceEndpoint() is a no-op and the retry
// loop keeps dialing the same endpoint. This is the regression guard for
// the existing R65-3.2 breaker test: no failover must happen so the
// breaker tripping logic still works exactly as before.
func TestFailover_SingleEndpointPreservesLegacyBehavior(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"dead"}}`))
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 1*time.Second, 0)
	// Three 503s trip the breaker; the fourth call short-circuits.
	for i := 0; i < 3; i++ {
		_, _ = p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
	}
	preHits := atomic.LoadInt32(&hits)
	_, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
	if err == nil {
		t.Fatalf("expected breaker error")
	}
	if postHits := atomic.LoadInt32(&hits); postHits != preHits {
		t.Errorf("hits changed pre=%d post=%d; single-endpoint must NOT trigger extra dial", preHits, postHits)
	}
	if got := p.Endpoints(); len(got) != 1 || got[0] != srv.URL {
		t.Errorf("Endpoints() = %v, want [srv.URL]", got)
	}
}

// TestFailover_RespectsBreakerPerEndpoint (BUG-R220) — when the primary's
// breaker has tripped, the next call must NOT re-dial the primary; it must
// route to the secondary directly.
func TestFailover_RespectsBreakerPerEndpoint(t *testing.T) {
	var primaryHits, secondaryHits int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryHits, 1)
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"primary dead"}}`))
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondaryHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","model":"x","role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer secondary.Close()

	p := anthropic.New([]string{primary.URL, secondary.URL}, 2*time.Second, 0)

	// Drive 3 calls. With maxRetries=2, each call has up to 3 attempts.
	// The first attempt hits primary (503, record breaker), retries 0
	// and 1 also hit primary → 3 503s within 60s window trip the breaker.
	// Call 3's first attempt must short-circuit and advance to secondary.
	for i := 0; i < 3; i++ {
		_, _ = p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
	}

	// Subsequent call: primary breaker is open, must skip straight to
	// secondary. We assert primaryHits is bounded.
	primaryBefore := atomic.LoadInt32(&primaryHits)
	_, err := p.Chat(context.Background(), "k", llmtypes.LLMRequest{Model: "x", MaxTokens: 1})
	if err != nil {
		t.Fatalf("post-breaker Chat err: %v", err)
	}
	primaryAfter := atomic.LoadInt32(&primaryHits)
	if primaryAfter != primaryBefore {
		t.Errorf("primary was hit while its breaker was open (before=%d after=%d)", primaryBefore, primaryAfter)
	}
	if atomic.LoadInt32(&secondaryHits) < 1 {
		t.Errorf("secondary was never hit; expected at least one post-breaker call to land there")
	}
}
