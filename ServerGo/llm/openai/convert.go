// Package openai — convert.go holds the OpenAI Chat Completions wire types and
// the bidirectional mapping between them and the protocol-neutral
// llm/types.LLMRequest / LLMResponse.
//
// This is NOT an "Anthropic→OpenAI translation shim": the OpenAI protocol is
// its own system with its own message roles (system/user/assistant/tool),
// tool_calls envelope, and streaming chunk shape. This file owns the complete
// mapping rules; docs/LLM与Agent/AgentOpenAI工具集与道具协议.md §2 is the
// human-readable contract these functions implement.
//
// Authoritative wire samples (opencode Agent captures):
//
//	tmpPlan/OpenAI协议-opencode-Agent-RequestBody.json
//	tmpPlan/OpenAI协议-opencode-Agent-ResponseBody.json
package openai

import (
	"encoding/json"
	"strings"

	types "LsmAgentGame/llm/types"
)

// ─── request wire types ───

// chatRequest is the OpenAI POST /chat/completions body. Field set deliberately
// limited to what the authoritative capture shows + what our agents need:
// model / messages / max_tokens / temperature / tools / tool_choice / stream /
// stream_options. Anthropic-only concepts (top-level system / metadata /
// output_config / thinking / cache_control) have NO counterpart here and are
// never emitted.
type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []chatMessage   `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Tools       []chatTool      `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	StreamOpts  *streamOptions  `json:"stream_options,omitempty"`
}

// streamOptions asks the upstream to attach a usage object to the final chunk
// (authoritative capture: "stream_options": {"include_usage": true}).
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatMessage is one OpenAI messages[] entry. Content is a plain string on the
// wire (the captures show assistant turns with content:"" alongside
// tool_calls); ToolCalls / ToolCallID carry the function-calling envelope.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatToolCall is the OpenAI assistant→tool call envelope. Arguments is a JSON
// STRING (not an object) on the wire — see the RequestBody capture:
//
//	"function": {"name":"bash","arguments":"{\"command\": \"go run . -help\"}"}
type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // always "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatTool is the OpenAI tools[] entry — the Anthropic input_schema is a
// standard JSON Schema and passes through verbatim as function.parameters.
type chatTool struct {
	Type     string `json:"type"` // always "function"
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// namedToolChoice is the {"type":"function","function":{"name":…}} shape used
// when Anthropic tool_choice forces a specific tool.
type namedToolChoice struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// ─── response wire types ───

// chatResponse mirrors the non-streaming chat.completion body (authoritative
// ResponseBody capture).
type chatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role             string         `json:"role"`
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content,omitempty"`
			ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// chatError is the OpenAI error envelope: {"error":{"message","type","code"}}.
type chatError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// ─── URL ───

// ChatCompletionsURL resolves the full request URL for a configured base
// endpoint. Hard rules (design doc §3):
//
//  1. trailing slashes are trimmed;
//  2. a base already ending in "/chat/completions" is used as-is;
//  3. otherwise "/chat/completions" is appended.
//
// The admin UI therefore accepts "https://api.openai.com/v1" or a proxy root
// like "http://proxy:8080/openai" — the suffix is never duplicated.
func ChatCompletionsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

// ─── request conversion ───

// buildRequest converts a protocol-neutral LLMRequest into the OpenAI wire
// body. stream controls both the "stream" flag and the stream_options block
// (include_usage so the final SSE chunk carries token counts).
func buildRequest(req types.LLMRequest, stream bool) chatRequest {
	out := chatRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}
	if stream {
		out.StreamOpts = &streamOptions{IncludeUsage: true}
	}

	// system[] → a single leading system message (texts joined with \n\n).
	var sysParts []string
	for _, sb := range req.System {
		if t := strings.TrimSpace(sb.Text); t != "" {
			sysParts = append(sysParts, sb.Text)
		}
	}
	if len(sysParts) > 0 {
		out.Messages = append(out.Messages, chatMessage{
			Role:    "system",
			Content: strings.Join(sysParts, "\n\n"),
		})
	}

	for _, m := range req.Messages {
		out.Messages = append(out.Messages, convertMessage(m)...)
	}

	for _, td := range req.Tools {
		var ct chatTool
		ct.Type = "function"
		ct.Function.Name = td.Name
		ct.Function.Description = td.Description
		ct.Function.Parameters = td.InputSchema
		if ct.Function.Parameters == nil {
			ct.Function.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out.Tools = append(out.Tools, ct)
	}

	out.ToolChoice = convertToolChoice(req.ToolChoice)
	return out
}

// convertMessage expands ONE Anthropic message into the OpenAI message
// sequence it represents:
//
//   - user text blocks           → one {"role":"user","content":"…"}
//   - user tool_result blocks    → one {"role":"tool",…} PER block (OpenAI
//     requires a separate tool message per tool_call_id), followed by the
//     remaining text as a user message if any;
//   - assistant text blocks      → content string (may be "");
//   - assistant tool_use blocks  → tool_calls[] on the same assistant message;
//   - thinking blocks            → dropped (§128: transient reasoning is not
//     replayable conversation history; replaying it breaks strict gateways).
func convertMessage(m types.Message) []chatMessage {
	var texts []string
	var toolResults []types.ContentBlock
	var toolUses []types.ContentBlock
	for _, c := range m.Content {
		switch c.Type {
		case "text":
			texts = append(texts, c.Text)
		case "tool_result":
			toolResults = append(toolResults, c)
		case "tool_use":
			toolUses = append(toolUses, c)
		default:
			// thinking / unknown — dropped by design.
		}
	}
	joined := strings.Join(texts, "\n")

	if m.Role == "assistant" {
		msg := chatMessage{Role: "assistant", Content: joined}
		for _, tu := range toolUses {
			msg.ToolCalls = append(msg.ToolCalls, buildToolCall(tu))
		}
		return []chatMessage{msg}
	}

	// user (or any non-assistant role carrying tool_results).
	var out []chatMessage
	for _, tr := range toolResults {
		out = append(out, chatMessage{
			Role:       "tool",
			ToolCallID: tr.ToolUseID,
			Content:    flattenToolResultContent(tr),
		})
	}
	if joined != "" || len(out) == 0 {
		role := m.Role
		if role != "user" {
			role = "user"
		}
		out = append(out, chatMessage{Role: role, Content: joined})
	}
	return out
}

// buildToolCall converts one Anthropic tool_use block into the OpenAI
// function-call envelope. Input is serialized to a JSON STRING (the OpenAI
// wire shape); nil input normalizes to "{}".
func buildToolCall(tu types.ContentBlock) chatToolCall {
	var tc chatToolCall
	tc.ID = tu.ID
	tc.Type = "function"
	tc.Function.Name = tu.Name
	if tu.Input == nil {
		tc.Function.Arguments = "{}"
	} else if raw, err := json.Marshal(tu.Input); err == nil {
		tc.Function.Arguments = string(raw)
	} else {
		tc.Function.Arguments = "{}"
	}
	return tc
}

// flattenToolResultContent renders a tool_result block's nested content as a
// single plain-text string for the OpenAI role:"tool" message.
func flattenToolResultContent(tr types.ContentBlock) string {
	var parts []string
	for _, c := range tr.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// convertToolChoice maps Anthropic tool_choice onto the OpenAI enum:
//
//	nil / {"type":"auto"}           → "auto"
//	{"type":"any"}                  → "required"
//	{"type":"tool","name":"foo"}    → {"type":"function","function":{"name":"foo"}}
func convertToolChoice(tc *types.ToolChoice) any {
	if tc == nil {
		return nil
	}
	switch tc.Type {
	case "any":
		return "required"
	case "tool":
		if tc.Name == "" {
			return "auto"
		}
		var n namedToolChoice
		n.Type = "function"
		n.Function.Name = tc.Name
		return n
	default:
		return "auto"
	}
}

// ─── response conversion ───

// normalizeResponse folds a parsed chat.completion body into the neutral
// LLMResponse. reasoning_content is counted but NEVER materialized into
// Content (same rule as the Anthropic thinking block: transient reasoning
// must not re-enter conversation history).
func normalizeResponse(cr chatResponse) types.LLMResponse {
	resp := types.LLMResponse{
		ID:    cr.ID,
		Model: cr.Model,
		Usage: types.LLMUsage{
			InputTokens:  cr.Usage.PromptTokens,
			OutputTokens: cr.Usage.CompletionTokens,
		},
	}
	if len(cr.Choices) == 0 {
		return resp
	}
	ch := cr.Choices[0]
	resp.StopReason = mapFinishReason(ch.FinishReason)
	if strings.TrimSpace(ch.Message.Content) != "" {
		resp.Content = append(resp.Content, types.ContentBlock{Type: "text", Text: ch.Message.Content})
	}
	for _, tc := range ch.Message.ToolCalls {
		resp.Content = append(resp.Content, toolCallToBlock(tc))
	}
	return resp
}

// toolCallToBlock converts one OpenAI tool_calls[] entry into an Anthropic-
// shaped tool_use ContentBlock. The arguments JSON string is decoded into
// Input; undecodable arguments fall back to {"_partial": raw} (mirrors
// anthropic stream.go's ErrStreamToolUsePartial salvage semantics).
func toolCallToBlock(tc chatToolCall) types.ContentBlock {
	input := map[string]any{}
	raw := strings.TrimSpace(tc.Function.Arguments)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			input = map[string]any{
				"_partial":       tc.Function.Arguments,
				"_partial_error": err.Error(),
			}
		}
	}
	return types.ContentBlock{
		Type:  "tool_use",
		ID:    tc.ID,
		Name:  tc.Function.Name,
		Input: input,
	}
}

// mapFinishReason normalizes OpenAI finish_reason onto the Anthropic
// stop_reason vocabulary the agent layer already understands.
func mapFinishReason(fr string) string {
	switch fr {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return fr
	}
}
