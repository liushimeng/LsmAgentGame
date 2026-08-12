// stream.go — Anthropic SSE accumulation helpers.
//
// ChatStream opens an HTTP/SSE body. This file builds the in-process
// machinery that turns the byte stream back into a types.LLMResponse, the
// same shape the non-streaming Chat() returns:
//
//   1. AccumulateStream drives ParseSSE through a small state machine that
//      knows about the 8 message_* / content_block_* / error event types.
//   2. ChatStreamAccumulate (in anthropic.go) wraps the two together for
//      callers that just want a synchronous LLMResponse.
//
// The state machine keeps a map[int]*blockBuilder keyed by the SSE content
// block index, and a slice []types.ContentBlock kept in arrival order so
// finalization on content_block_stop naturally produces an ordered Content
// array. The message_start event seeds resp.ID / resp.Model / resp.Usage
// input+output counters; message_delta updates StopReason / Usage.
//
// BUG-R65-3.2: mid-stream 503 detection. Some proxies translate upstream
// unavailability into a SSE error event (e.g. {"type":"error","error":
// {"type":"...","message":"All endpoints failed"}}) that arrives after the
// HTTP dial returned 2xx. We detect the "503 / Service Unavailable / All
// endpoints failed" markers and call the Provider's recordEndpointFailure()
// hook so the short-circuit breaker trips before the next wake cycle.

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	types "LsmAgentGame/llm/types"
)

// providerCtxKey is the unexported context key used to pass the active
// *Provider through to AccumulateStream without changing its public
// signature. The value is set by Provider.ChatStreamAccumulate so the SSE
// error-event branch can call recordEndpointFailure on hard 503s.
type providerCtxKey struct{}

// withProvider returns a derived context carrying the *Provider reference.
func withProvider(ctx context.Context, p *Provider) context.Context {
	return context.WithValue(ctx, providerCtxKey{}, p)
}

// providerFromContext extracts the *Provider reference (if any) from ctx.
// The second return value is false when no provider is attached (i.e. the
// caller used AccumulateStream directly without going through the
// Provider wrapper, in which case the breaker hook is silently skipped).
func providerFromContext(ctx context.Context) (*Provider, bool) {
	v := ctx.Value(providerCtxKey{})
	if v == nil {
		return nil, false
	}
	p, ok := v.(*Provider)
	return p, ok
}

// ErrStreamTruncated indicates the SSE body ended cleanly (EOF) without a
// message_stop event. Any partial Content / Usage already accumulated is
// returned alongside the error so the caller can still salvage what was
// received (e.g. log the partial tool_use for forensics).
var ErrStreamTruncated = errors.New("anthropic stream truncated: missing message_stop")

// ErrStreamToolUsePartial indicates the tool_use input JSON failed to parse
// after all input_json_delta chunks were concatenated. We retain the raw
// string under {"_partial": ...} so the caller can inspect / retry rather
// than silently dropping the tool call.
var ErrStreamToolUsePartial = errors.New("anthropic stream: tool_use input_json decode failed")

// blockBuilder holds per-content-block incremental state. Streams emit one
// content_block_start, N content_block_delta events, then content_block_stop.
// We bucket the deltas here and finalize into a single ContentBlock on
// content_block_stop.
//
// §128 对话即思考重构:thinking 块已删除,仅保留 text / tool_use 两种类型。
type blockBuilder struct {
	// Type is one of "text" | "tool_use". Drives finalization.
	Type string

	// Text accumulates text_delta chunks.
	Text strings.Builder
	// JSONBuf accumulates input_json_delta partial-JSON chunks.
	JSONBuf bytes.Buffer

	// ID / Name are set from the content_block_start.content_block envelope
	// so we don't lose them when only deltas arrive.
	ID   string
	Name string

	// partialErr is non-nil if input_json decode failed; we still emit the
	// best-effort ContentBlock with a {"_partial":raw} payload so downstream
	// tool dispatch sees a valid input.
	partialErr error
}

// AccumulateStream consumes an SSE byte stream (typically the io.ReadCloser
// returned by ChatStream) and reconstructs an LLMResponse. onProgress is
// fired after each successfully parsed event (it may be nil). Callers must
// close the underlying body — AccumulateStream does not own it.
//
// Truncation: a clean EOF without message_stop returns the partial
// response alongside ErrStreamTruncated. Use types.LLMResponse{} only when
// message_start was also missing.
//
// Mid-stream error event: returns the partial response alongside a *Error
// whose Source=="stream". The caller can choose to log+continue.
//
// BUG-R65-3.2: ctx is consulted for the *Provider reference (set via
// withProvider by Provider.ChatStreamAccumulate) so mid-stream SSE error
// events that indicate a hard 503 can be fed to the short-circuit breaker.
// Callers that don't need breaker support (mostly tests) can pass
// context.Background().
func AccumulateStream(ctx context.Context, r io.Reader, onProgress func(types.StreamEvent) error) (types.LLMResponse, error) {
	var resp types.LLMResponse
	blocks := make(map[int]*blockBuilder)
	var blockOrder []int
	// §127: 用于在首 token / 首个 content_block_start 到达时通知调用方。
	var firstBlockSeen bool

	parseErr := ParseSSE(r, func(ev types.StreamEvent) error {
		switch ev.Type {
		case "message_start":
			if ev.MessageID != "" {
				resp.ID = ev.MessageID
			}
			if ev.MessageModel != "" {
				resp.Model = ev.MessageModel
			}
			resp.Usage.InputTokens = ev.UsageInput
			resp.Usage.OutputTokens = ev.UsageOutput
		case "content_block_start":
			// §127: 首个内容块到达 = 流式首 token 已生成,触发外部 progress hook。
			// _first_token 是额外的一次轻量回调,供调用方切 PhaseStreaming;
			// 然后正常走 blockBuilder 默认逻辑。
			if !firstBlockSeen {
				firstBlockSeen = true
				if onProgress != nil {
					_ = onProgress(types.StreamEvent{Type: "_first_token"})
				}
			}
			bb := &blockBuilder{
				Type: ev.ContentBlockType,
				ID:   ev.ContentBlockID,
				Name: ev.ContentBlockName,
			}
			blocks[ev.Index] = bb
			blockOrder = append(blockOrder, ev.Index)
		case "content_block_delta":
			bb, ok := blocks[ev.Index]
			if !ok {
				// Should never happen — content_block_start precedes every
				// content_block_delta. Skip defensively.
				break
			}
			switch ev.DeltaType {
			case "text_delta":
				bb.Text.WriteString(ev.Delta)
			case "input_json_delta":
				bb.JSONBuf.WriteString(ev.Delta)
			default:
				// §128 对话即思考重构:thinking_delta / signature_delta 已删除。
				// 未知 delta 类型 — 静默忽略以保持前向兼容。
			}
		case "content_block_stop":
			bb, ok := blocks[ev.Index]
			if !ok {
				break
			}
			// §14.1 (2026-08-02) — 丢弃 thinking 内容块,不物化进 resp.Content。
			// extended thinking 是模型的瞬时推理,不属于可重放的对话历史;
			// 若写入 Content 并被 recordTranscript 回显到下一轮请求,wire 上
			// 会变成 {"type":"thinking"}(budget 丢失),严格代理报 400
			// "missing messages.content.thinking parameter"。此处源头剔除,
			// 与 SanitizeMessagesForAnthropic 的请求期剔除双保险。
			if bb.Type == "thinking" {
				break
			}
			finalized := finalizeBlock(bb)
			resp.Content = append(resp.Content, finalized)
		case "message_delta":
			// delta.stop_reason + message_delta.usage.*
			// parseStreamEventJSON lifts stop_reason from delta.stop_reason
			// because the typed StreamEvent can't reach into a nested
			// object via json tags alone. The accumulator still double-
			// checks via the raw envelope below as a belt-and-braces fix
			// (some proxies emit stop_reason at the top level instead).
			if ev.StopReason != "" {
				resp.StopReason = ev.StopReason
			}
			// In Anthropic's wire format:
			//   - message_start.usage.output_tokens is the input ONLY
			//   - message_delta.usage.output_tokens is the cumulative OUTPUT
			//   - message_delta.usage.input_tokens is a per-event cache
			//     write/read increment (NOT cumulative)
			// We accumulate input_tokens on the model-side because they
			// ARE additive (cache writes + reads), and we OVERWRITE
			// output_tokens since message_delta is cumulative.
			resp.Usage.InputTokens += ev.UsageInput
			if ev.UsageOutput > 0 {
				resp.Usage.OutputTokens = ev.UsageOutput
			}
		case "message_stop":
			// Clean termination. Drop any zero-value placeholders before
			// handing the response back so callers don't have to nil-check.
			resp.Content = dropEmptyPlaceholders(resp.Content)
			// Emit the final event, then we're done. Returning nil keeps the
			// scanner loop going until the underlying body EOFs.
		case "ping":
			// Heartbeat — nothing to accumulate.
		case "error":
			resp.Content = dropEmptyPlaceholders(resp.Content)
			// BUG-R65-3.2: when the upstream surfaces a 503 inside the SSE
			// body (e.g. "All endpoints failed" with the proxy translating
			// upstream unavailability to a mid-stream error event), feed it
			// to the provider's breaker so subsequent calls in the same
			// window short-circuit instead of repeating the dial. We
			// conservatively match on both the literal "503" and "Service
			// Unavailable" text in the event payload.
			//
			// BUG-R89 P0-cascade fix: 503-class stream errors are transient
			// upstream outages, not permanent client-side failures. Mark
			// them Retryable:true so the agent's retry-cooldown window
			// (agent/run.go failCooldownWindow=60s) absorbs the failure
			// instead of incrementing consecutiveFailures toward the
			// 2-strike quarantine line. In a 12-bot room that shares one
			// proxy (R89 report: 12/12 bot LLM calls failed, agent
			// coverage 0%), Retryable:false was triggering cascading
			// permanent quarantine on every bot within two wake cycles.
			// Non-503 stream errors (quota exceeded, auth failures,
			// malformed requests) stay Retryable:false — those ARE
			// permanent from the client's perspective and need fast
			// quarantine to keep the room moving via SkipPhaseAction.
			//
			// The retryable classification is computed from the message
			// alone (not from provider context) so it's deterministic
			// regardless of which caller invokes AccumulateStream.
			msg := strings.ToLower(ev.ErrorMessage)
			is503 := strings.Contains(msg, "503") || strings.Contains(msg, "service unavailable") ||
				strings.Contains(msg, "all endpoints failed")
			if is503 {
				if p, ok := providerFromContext(ctx); ok {
					// BUG-R214: record503 已泛化为 recordEndpointFailure
					// (端点级不可用记账,不再只统计 HTTP 503)。
					p.recordEndpointFailure(failReasonStream503)
				}
			}
			return &Error{
				HTTPStatus: 0,
				Retryable:  is503,
				Source:     "stream",
				Message:    ev.ErrorMessage,
			}
		case "done":
			// [DONE] sentinel — defensive return.
			return io.EOF
		}
		if onProgress != nil {
			if perr := onProgress(ev); perr != nil {
				return perr
			}
		}
		return nil
	})

	// parseErr covers scanner errors (network drops, body close, idle
	// timeout wrapper firing) and the cases where we returned a *Error.
	if parseErr != nil {
		// If the parse error is io.EOF — clean stream end after a
		// message_stop — we treat it as success and return resp.
		if errors.Is(parseErr, io.EOF) {
			if resp.StopReason == "" && len(resp.Content) > 0 {
				// Saw deltas but no message_stop before EOF — truncated.
				return resp, ErrStreamTruncated
			}
			return resp, nil
		}
		// Surface the underlying *Error verbatim (so the source-tagged
		// anthropic.Error reaches the caller). Other errors are wrapped.
		var ae *Error
		if errors.As(parseErr, &ae) {
			return resp, ae
		}
		// If we already saw a message_stop the stream was complete — any
		// post-EOS noise (context canceled when the connection closes)
		// is benign and the caller should see the parsed response.
		if resp.StopReason != "" {
			return resp, nil
		}
		// ROUND 40 P0-1 — upstream proxy mid-flight drop classification.
		//
		// When the upstream Anthropic proxy (configured via cfg.llm.endpoint / cfg.llm.endpoints)
		// resets the TCP connection 1.5-4s into a stream (after auth but
		// before any tokens arrive), the *http.Response body Read surfaces
		// the failure as either context.Canceled (when our own ctx is the
		// trigger), or — more commonly — as a wrapped
		// "use of closed network connection" / "context canceled" returned
		// from the underlying transport. Without this classifier that error
		// becomes a plain `errors.New` value that the agent's type-assertion
		//
		//     if ae, ok := err.(*anthropic.Error); ok && !ae.Retryable { ... }
		//
		// falls through, so the failure is *not* counted as a retryable
		// error and the agent's consecutive_failures counter ticks toward
		// the 5-strike quarantine line. After 5 mid-flight drops the bot
		// is permanently disabled for the room, leaving the watchdog to
		// auto-skip every turn — the 4/7 quarantine observed in the R40
		// full-AI run.
		//
		// Mark these as Retryable=true so the agent's outer retry loop
		// (agent/run.go: llmMaxRetries exponential backoff) gets a chance
		// to re-dial before counting it. We deliberately use
		// errors.Is(parseErr, context.Canceled) rather than string-matching
		// because both net/http's transport and our idleTimeoutReader wrap
		// errors and we want all of them caught here.
		if errors.Is(parseErr, context.Canceled) || errors.Is(parseErr, context.DeadlineExceeded) ||
			strings.Contains(parseErr.Error(), "context canceled") ||
			strings.Contains(parseErr.Error(), "use of closed network connection") ||
			strings.Contains(parseErr.Error(), "connection reset") {
			return resp, &Error{
				HTTPStatus: 0,
				Retryable:  true,
				Source:     "stream",
				Message:    fmt.Sprintf("stream transport dropped: %v", parseErr),
			}
		}
		return resp, fmt.Errorf("anthropic: stream read: %w", parseErr)
	}

	// Scanner returned nil without an error event — body ended cleanly but
	// no message_stop. Treat as truncation only if we actually saw anything.
	if resp.StopReason == "" && (len(resp.Content) > 0 || resp.ID != "") {
		return resp, ErrStreamTruncated
	}
	return resp, nil
}

// finalizeBlock turns a fully-buffered blockBuilder into a ContentBlock.
// tool_use blocks with empty input_json get an empty map (DouBao / DeepSeek
// strict-proxy requirement); unparseable JSON falls back to a
// {"_partial":raw} payload alongside a partialErr so the dispatcher can
// still inspect / log the partial.
func finalizeBlock(bb *blockBuilder) types.ContentBlock {
	switch bb.Type {
	case "text":
		return types.ContentBlock{Type: "text", Text: bb.Text.String()}
	case "tool_use":
		raw := bb.JSONBuf.Bytes()
		var input map[string]any
		if len(raw) == 0 {
			input = map[string]any{}
		} else if jerr := json.Unmarshal(raw, &input); jerr != nil {
			bb.partialErr = ErrStreamToolUsePartial
			input = map[string]any{
				"_partial":   string(raw),
				"_partial_error": jerr.Error(),
			}
		}
		// Defensive: never emit nil Input — strict proxies reject it.
		if input == nil {
			input = map[string]any{}
		}
		return types.ContentBlock{
			Type:  "tool_use",
			ID:    bb.ID,
			Name:  bb.Name,
			Input: input,
		}
	}
	// Unknown block type — emit as text with the JSON we have, or empty.
	return types.ContentBlock{Type: bb.Type}
}

// dropEmptyPlaceholders removes zero-value placeholder blocks that streams
// sometimes emit before a content_block_stop arrives. Today we only strip
// text blocks whose Text is empty; tool_use blocks with non-nil Input are
// kept even if empty.
func dropEmptyPlaceholders(cs []types.ContentBlock) []types.ContentBlock {
	if len(cs) == 0 {
		return cs
	}
	out := make([]types.ContentBlock, 0, len(cs))
	for _, b := range cs {
		if b.Type == "text" && b.Text == "" {
			continue
		}
		// §128 对话即思考重构:thinking 块过滤已删除
		out = append(out, b)
	}
	return out
}
