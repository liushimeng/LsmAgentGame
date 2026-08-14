// Package openai — stream.go: SSE parser + aggregator for the OpenAI
// chat.completion.chunk protocol.
//
// OpenAI streaming uses NO `event:` lines — just `data:` frames of
// chat.completion.chunk JSON terminated by `data: [DONE]`. The aggregator
// rebuilds a protocol-neutral LLMResponse. For Agent-layer compatibility it
// also synthesizes the same progress event shape the anthropic stream.go
// accumulator emits (_first_token / message_start / message_stop), so callers
// stay protocol-agnostic (design doc §2.7).
package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	types "LsmAgentGame/llm/types"
)

// chatChunk is the OpenAI data-frame shape. We only declare the subset the
// agents produce; unknown fields are silently ignored (forward compatibility).
type chatChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string             `json:"role"`
			Content          string             `json:"content"`
			ReasoningContent string             `json:"reasoning_content,omitempty"`
			Reasoning        string             `json:"reasoning,omitempty"`
			ToolCalls        []chatChunkToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// chatChunkToolCall carries per-tool deltas. ID + function.name appear only on
// the FIRST chunk of a tool (index keyed); arguments is a partial-JSON string
// accumulated across chunks until finish.
type chatChunkToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// errStreamDone is a sentinel returned by the per-chunk callback to request
// early termination (the [DONE] sentinel). AccumulateStream translates it to a
// clean success return.
var errStreamDone = errors.New("openai: stream completed")

// toolAccum holds in-progress per-index tool reconstruction state.
type toolAccum struct {
	id   string
	name string
	args bytes.Buffer
}

// ParseSSE reads an OpenAI SSE byte stream and yields each decoded chunk. Empty
// lines frame chunk boundaries. Comment lines (leading ':') are skipped.
// `data: [DONE]` short-circuits by returning a sentinel via the callback.
//
// ParseSSE itself only does wire I/O + framing; semantic aggregation lives in
// AccumulateStream. Keeping them separate mirrors the anthropic split
// (ParseSSE ↔ AccumulateStream) so the two providers share one mental model.
func ParseSSE(r io.Reader, onChunk func(chatChunk) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<16), 1<<20) // up to 1 MiB per frame
	var buf strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if buf.Len() > 0 {
				data := buf.String()
				if strings.TrimSpace(data) == "[DONE]" {
					return nil // clean end-of-stream
				}
				var chunk chatChunk
				if err := json.Unmarshal([]byte(data), &chunk); err == nil {
					if cerr := onChunk(chunk); cerr != nil {
						if errors.Is(cerr, errStreamDone) {
							return nil
						}
						return cerr
					}
				}
				// Per-frame parse failure is non-fatal (forward compat / noisy
				// upstream): log is the caller's job, we keep scanning.
			}
			buf.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // heartbeat / comment
		}
		if strings.HasPrefix(line, "data:") {
			chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if chunk == "" {
				continue
			}
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.WriteString(chunk)
		}
	}
	return scanner.Err()
}

// ErrStreamTruncated signals the body ended (EOF / reset) without a [DONE] and
// without a finish_reason. Parallel to the anthropic equivalent so callers can
// treat the two providers identically.
var ErrStreamTruncated = errors.New("openai stream truncated: missing finish / [DONE]")

// AccumulateStream drives ParseSSE through a small state machine that
// reassembles Content / Usage / StopReason and fires protocol-compatible
// progress events. onProgress may be nil; a non-nil callback returning error
// aborts the stream with that error.
//
// Progress event shape (synthesized for Agent-layer compatibility, §2.7):
//   - `_first_token`      — first time any text arrives;
//   - `message_start`     — first chunk carrying id/model;
//   - `content_block_delta` with DeltaType "text_delta" — per text fragment;
//   - `message_stop`      — when a finish_reason / [DONE] is observed.
//
// reasoning_content deltas are counted into reasoningLen but NEVER materialized
// into Content (transient reasoning must not re-enter conversation history).
func AccumulateStream(r io.Reader, onProgress func(types.StreamEvent) error) (types.LLMResponse, error) {
	var resp types.LLMResponse
	tools := map[int]*toolAccum{}
	toolOrder := []int{}
	finished := false
	var reasoningLen int
	startedText := false
	textBuf := strings.Builder{}

	parseErr := ParseSSE(r, func(chunk chatChunk) error {
		if chunk.ID != "" && resp.ID == "" {
			resp.ID = chunk.ID
		}
		if chunk.Model != "" && resp.Model == "" {
			resp.Model = chunk.Model
			if onProgress != nil {
				_ = onProgress(types.StreamEvent{Type: "message_start", MessageID: resp.ID, MessageModel: resp.Model})
			}
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.ReasoningContent != "" {
				reasoningLen += len(ch.Delta.ReasoningContent)
			}
			if ch.Delta.Reasoning != "" {
				reasoningLen += len(ch.Delta.Reasoning)
			}
			if ch.Delta.Content != "" {
				if !startedText {
					startedText = true
					if onProgress != nil {
						_ = onProgress(types.StreamEvent{Type: "_first_token"})
					}
				}
				textBuf.WriteString(ch.Delta.Content)
				if onProgress != nil {
					_ = onProgress(types.StreamEvent{
						Type:      "content_block_delta",
						DeltaType: "text_delta",
						Delta:     ch.Delta.Content,
					})
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				acc, ok := tools[tc.Index]
				if !ok {
					acc = &toolAccum{}
					tools[tc.Index] = acc
					toolOrder = append(toolOrder, tc.Index)
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason != "" {
				resp.StopReason = mapFinishReason(ch.FinishReason)
				finished = true
			}
		}
		if chunk.Usage != nil {
			resp.Usage.InputTokens = chunk.Usage.PromptTokens
			resp.Usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		if finished && onProgress != nil {
			_ = onProgress(types.StreamEvent{Type: "message_stop", StopReason: resp.StopReason})
			return errStreamDone
		}
		return nil
	})

	// Flush the accumulated text block (joined deltas → one text ContentBlock,
	// mirroring the anthropic accumulator's single text block per content block).
	if text := strings.TrimSpace(textBuf.String()); text != "" {
		resp.Content = append(resp.Content, types.ContentBlock{Type: "text", Text: text})
	}
	// Flush accumulated tool calls in index order.
	for _, idx := range toolOrder {
		acc := tools[idx]
		raw := strings.TrimSpace(acc.args.String())
		input := map[string]any{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &input); err != nil {
				input = map[string]any{"_partial": raw, "_partial_error": err.Error()}
			}
		}
		resp.Content = append(resp.Content, types.ContentBlock{
			Type: "tool_use", ID: acc.id, Name: acc.name, Input: input,
		})
	}

	if parseErr != nil {
		var oe *Error
		if errors.As(parseErr, &oe) {
			return resp, oe
		}
		// transport / framing errors surface as retryable so the agent can recover.
		return resp, &Error{HTTPStatus: 0, Retryable: true, Source: "stream", Message: parseErr.Error()}
	}

	if !finished && (len(resp.Content) > 0 || resp.ID != "" || reasoningLen > 0) {
		return resp, ErrStreamTruncated
	}
	return resp, nil
}
