// Package types defines the LLM wire types + LLMProvider interface. It is a
// leaf package: it imports nothing else under ServerGo/llm, so both the parent
// llm package (registry) and llm/anthropic (provider impl) can import it
// without a cycle.
//
// Request/response shapes follow the canonical Anthropic Messages API — the
// same shape used in `CluadeCode请求RequestBody的Anthropic协议定义数据用例.json`
// at the repo root (top-level keys: model / system / messages / tools /
// metadata / output_config / max_tokens, etc.). See
// `docs/LLM与Agent/LLM供应商设计.md` for the per-field rationale.
package types

import (
	"context"
	"encoding/json"
	"io"
)

// ContentBlock is a single block inside an Anthropic `content` array. The wire
// format supports several block types; the fields here cover the subset this
// client emits and parses:
//
//   - "text"        → Text
//   - "tool_use"    → ID / Name / Input
//   - "tool_result" → ToolUseID + Content
//   - "thinking"    → Thinking (extended thinking, 2026-08-01 §R224 修复)
//
// §R224 (BUG-NEW-1 messages.content.thinking missing): 2026-08-01 实测
// 全 8 家代理(美团/豆包/DeepSeek/GLM/Kimi/MiniMax/Qwen/小米)严格校验
// `messages.content.thinking` 字段存在,缺则报 400。今日累计 4208 次 400。
// §128 重构于 2026-07-12 误删 extended thinking 块,但 §14.1 协议对齐要求
// 保留 thinking 块的可注入能力(按 `cfg.Providers[i].thinking_required` 决定)。
// 故此处重新引入 thinking 块:MarshalJSON "thinking" 分支产出
// `{"type":"thinking","budget":N}` 严格符合 Anthropic 协议(CLAUDE.md §14.1)。
type ContentBlock struct {
	Type string `json:"type" yaml:"type"`

	// Type = "text"
	Text string `json:"text,omitempty" yaml:"text,omitempty"`

	// Type = "tool_use" — fields are intentionally NOT omitempty: certain
	// proxies (e.g. DouBao) reject tool_use blocks missing "input" / "id" /
	// "name" even when the value is an empty object / string. We always emit the
	// keys; BuildInput / BuildID / BuildName normalize nil maps to {} so the
	// wire payload is always a valid tool_use block.
	ID    string         `json:"id" yaml:"id"`
	Name  string         `json:"name" yaml:"name"`
	Input map[string]any `json:"input" yaml:"input"`

	// Type = "tool_result"
	ToolUseID string         `json:"tool_use_id,omitempty" yaml:"tool_use_id,omitempty"`
	Content   []ContentBlock `json:"content,omitempty" yaml:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty" yaml:"is_error,omitempty"`

	// Type = "thinking" — §R224 (2026-08-01): 重新引入 extended thinking
	// 协议的 content-block 字段。Anthropic wire 格式为
	// `{"type":"thinking","budget":N}` (N 为 token 上限,典型 4096/8192)。
	// 不带 id/name/input 字段,§14.1 "ContentBlock wire 形状必须按 Type 收敛"
	// 规则适用 — MarshalJSON "thinking" 分支只输出 type/budget。
	ThinkingBudget int `json:"budget,omitempty" yaml:"budget,omitempty"`
}

// EmitMap is the per-type JSON field sets for ContentBlock. It is used by
// MarshalJSON to emit ONLY the fields that belong to a given Type — the
// decoder side keeps using the full struct (all fields tagged omitempty or
// not), but the wire payload must be tight: a "text" block must not carry
// "id"/"name"/"input" and a "tool_result" must not carry "id"/"name"/
// "input" either. Strict proxies (e.g. DouBao) reject the polluted blocks
// with an empty/zero-graticule response (0 tokens, empty content) — the
// dominant failure mode observed in the Doubao-01-Respose-Body.json snapshot.
//
// Wire shape reference (canonical Claude Code Anthropic protocol):
//
//	CluadeCode的Anthropic协议-RequestBody-数据用例01/02/03.json
//	CluadeCode的Anthropic协议-ResposeBody-数据用例01/02/03.json
//
//	- "text"        → {type, text}
//	- "tool_use"    → {type, id, name, input}   (id/name/input ALWAYS present)
//	- "tool_result" → {type, tool_use_id, content, is_error}
func (cb ContentBlock) MarshalJSON() ([]byte, error) {
	switch cb.Type {
	case "text":
		// R143 (2026-07-17) — 防御 Anthropic 400 "missing messages.content.text parameter":
		// text 块必须始终带 text 字段(空字符串也是内容,如 LLM 调用 skip/idle_silent
		// 返回空时也得有占位文本,否则上游严格代理(doubao-seed-2.0-pro 等)直接拒绝)。
		// 原实现用 omitempty,空 Text 被吞掉 → 线上 15280-400 错误。
		// 同时禁止泄漏 id/name/input 等 tool_use 专属字段。
		text := cb.Text
		if text == "" {
			text = "(empty)"
		}
		v := struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: cb.Type, Text: text}
		return json.Marshal(v)
	case "tool_use":
		// id/name/input intentionally Always present (no omitempty): strict
		// proxies reject tool_use blocks missing these keys even when empty.
		v := struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}{Type: cb.Type, ID: cb.ID, Name: cb.Name, Input: cb.Input}
		return json.Marshal(v)
	case "tool_result":
		v := struct {
			Type      string         `json:"type"`
			ToolUseID string         `json:"tool_use_id,omitempty"`
			Content   []ContentBlock `json:"content,omitempty"`
			IsError   bool           `json:"is_error,omitempty"`
		}{Type: cb.Type, ToolUseID: cb.ToolUseID, Content: cb.Content, IsError: cb.IsError}
		return json.Marshal(v)
	case "thinking":
		// §R224 (2026-08-01) — 重新引入 extended thinking 块的 wire 格式。
		// 严格遵循 Anthropic 协议:{type, budget} — 禁止 id/name/input 污染。
		// budget > 0 才输出,避免 0 budget 被某些代理(DouBao)误判为"关闭"
		// 而重复报缺字段。
		v := struct {
			Type   string `json:"type"`
			Budget int    `json:"budget,omitempty"`
		}{Type: cb.Type, Budget: cb.ThinkingBudget}
		return json.Marshal(v)
	default:
		// Unknown block type (e.g. future thinking/image): fall back to the
		// minimal safe shape that preserves type discrimination.
		v := struct {
			Type string `json:"type"`
		}{Type: cb.Type}
		return json.Marshal(v)
	}
}

// Message is an Anthropic `messages[]` entry.
//
// `Role` is one of "user" | "assistant". `Content` mirrors the wire format:
// each entry is a ContentBlock (text / tool_use / tool_result).
type Message struct {
	Role    string         `json:"role" yaml:"role"`
	Content []ContentBlock `json:"content" yaml:"content"`
}

// SystemBlock is an Anthropic `system[]` entry. The reference JSON at the repo
// root shows `system` as [{type:"text", text:"..."}].
type SystemBlock struct {
	Type         string            `json:"type" yaml:"type"`
	Text         string            `json:"text" yaml:"text"`
	CacheControl map[string]string `json:"cache_control,omitempty" yaml:"cache_control,omitempty"`
}

// ToolDef is an Anthropic `tools[]` entry. `InputSchema` must be a JSON Schema
// object (i.e. {"type":"object","properties":{...},"required":[...]}).
type ToolDef struct {
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description" yaml:"description"`
	InputSchema map[string]any `json:"input_schema" yaml:"input_schema"`
}

// Metadata carries optional per-request metadata (Anthropic `metadata`).
type Metadata struct {
	UserID string `json:"user_id,omitempty" yaml:"user_id,omitempty"`
}

// LLMRequest is the top-level request body sent to the model endpoint. It maps
// 1:1 onto the Anthropic `POST /v1/messages` body — see the reference JSON in
// the repo root (`CluadeCode请求RequestBody的Anthropic协议定义数据用例.json`).
type LLMRequest struct {
	Model     string        `json:"model" yaml:"model"`
	System    []SystemBlock `json:"system,omitempty" yaml:"system,omitempty"`
	Messages  []Message     `json:"messages" yaml:"messages"`
	Tools     []ToolDef     `json:"tools,omitempty" yaml:"tools,omitempty"`
	MaxTokens int           `json:"max_tokens" yaml:"max_tokens"`
	Metadata  Metadata      `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	// OutputConfig mirrors the Anthropic `output_config` object — see
	// CluadeCode请求RequestBody的Anthropic协议定义数据用例.json (e.g.
	// {"effort":"high"} for adaptive reasoning). nil ⇒ field omitted.
	OutputConfig *OutputConfig `json:"output_config,omitempty" yaml:"output_config,omitempty"`
	// ToolChoice forces the model to call a specific tool ("any"|"auto"|{name}).
	// nil ⇒ field omitted (model picks freely).
	ToolChoice *ToolChoice `json:"tool_choice,omitempty" yaml:"tool_choice,omitempty"`
	// Stream when true asks the provider to deliver SSE chunks. Non-streaming
	// callers (e.g. the werewolf Agent) leave this nil / false.
	Stream bool `json:"stream,omitempty" yaml:"stream,omitempty"`
	// Temperature — optional; nil ⇒ field omitted (model default).
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	// BUG-R226-P1-02 (2026-08-01) — 顶层 extended thinking 字段,wire 形状
	// 对齐 §14.1 权威用例:{"type":"enabled","budget_tokens":N} /
	// {"type":"disabled"}(type 判别符由 ThinkingConfig.MarshalJSON 保证)。
	// 非 nil 时 anthropic provider 将其原样落到出站请求顶层;
	// **不注入消息级 thinking 内容块**(权威用例 content[] 只含
	// text/tool_use/tool_result 三种块)。
	//
	// 设计:由 registry.GetThinkingEnabled(modelKey) 决定每模型是否需要
	// thinking,agent.callProvider 统一注入;典型 budget=4096
	// (LsmWebGame.conf.example 默认值)。
	Thinking *ThinkingConfig `json:"thinking,omitempty" yaml:"thinking,omitempty"`
	// AgentClassName 2026-08-06 §AgentClassName 增强:调用方 Agent 类别标识
	// (例 "LsmWebGame-Werewolf-Player" / "LsmWebGame-Werewolf-Judge")。
	// 非空时 anthropic provider 用它拼装 User-Agent:
	//   User-Agent: <AgentClassName>/<AppVersion> <buildDateTime>
	// 空时回退到 provider 的全局 p.userAgent(由 llm.Registry.SetUserAgent
	// 注入,等价旧行为)。**不参与** wire 序列化,仅出站 HTTP 头使用。
	//
	// 详见 ServerGo/agent/class_names.go。
	AgentClassName string `json:"-" yaml:"-"`
}

// OutputConfig mirrors the Anthropic `output_config` object. Currently only the
// `effort` field is widely supported (`"low"` / `"medium"` / `"high"` / `"max"`).
// Empty string ⇒ field omitted.
type OutputConfig struct {
	Effort string `json:"effort,omitempty" yaml:"effort,omitempty"`
}

// ToolChoice mirrors Anthropic `tool_choice`. Either:
//   - {"type":"auto"}      → model decides
//   - {"type":"any"}       → must call exactly one of the supplied tools
//   - {"type":"tool","name":"foo"} → must call the named tool
//
// Zero value (Type=="") renders as the default (model picks) and is treated as
// nil by the anthropic provider.
type ToolChoice struct {
	Type string `json:"type" yaml:"type"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// LLMResponse is the parsed response from a provider, normalized into the
// Anthropic shape regardless of the underlying backend.
type LLMResponse struct {
	ID         string         `json:"id" yaml:"id"`
	Model      string         `json:"model" yaml:"model"`
	StopReason string         `json:"stop_reason" yaml:"stop_reason"` // "end_turn" | "tool_use" | "max_tokens" | "stop_sequence"
	Content    []ContentBlock `json:"content" yaml:"content"`
	Usage      LLMUsage       `json:"usage" yaml:"usage"`
}

// LLMUsage reports consumed token counts.
type LLMUsage struct {
	InputTokens  int `json:"input_tokens" yaml:"input_tokens"`
	OutputTokens int `json:"output_tokens" yaml:"output_tokens"`
}

// ToolUses is a convenience filter over Content blocks of type "tool_use".
func (r LLMResponse) ToolUses() []ContentBlock {
	var out []ContentBlock
	for _, c := range r.Content {
		if c.Type == "tool_use" {
			out = append(out, c)
		}
	}
	return out
}

// NormalizeToolUseInput walks Content and replaces nil Input maps with an empty
// object. Used right before serialization so the request body always contains
// a non-null "input" key on tool_use blocks. DouBao and similar strict proxies
// reject missing keys.
func (r *LLMResponse) NormalizeToolUseInput() {
	for i := range r.Content {
		if r.Content[i].Type == "tool_use" && r.Content[i].Input == nil {
			r.Content[i].Input = map[string]any{}
		}
	}
}

// §128 对话即思考重构:ThinkingConfig 结构体已删除。

// ThinkingConfig is the per-request extended thinking knob.
//
// BUG-R226-P1-02 (2026-08-01) — wire 格式对齐 §14.1 权威用例
// (CluadeCode的Anthropic协议-RequestBody-数据用例01/02/03.json):
// 顶层 `thinking` 字段形状为 `{"type":"enabled","budget_tokens":N}` /
// `{"type":"disabled"}`,**必须带 type 判别符**;权威用例的
// messages[].content[] 只含 text/tool_use/tool_result 三种块,
// **从不携带 thinking 内容块**。此前实现(R224 e799a7f)发出的是
// `{"enabled":true,"budget_tokens":4096}`(无 type)+ 往每条 message 头部
// 注入 {"type":"thinking","budget":N} 内容块,两个方向都错,
// 严格校验的上游(DouBao 最严、GLM 次之)因此 400
// "missing messages.content.thinking parameter"。
//
// - Type: "enabled" / "disabled"(空值序列化时归一化为 "enabled")。
// - BudgetTokens: token 上限;Type=="disabled" 时省略。
//   ≤0 在序列化时兜底为 4096(与 §14.1 "budget ≥ 1024" 一致)。
type ThinkingConfig struct {
	Type         string `json:"type,omitempty" yaml:"type,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty" yaml:"budget_tokens,omitempty"`
}

// MarshalJSON 保证 wire 形状始终带 type 判别符:
// disabled → {"type":"disabled"};enabled(默认) → {"type":"enabled","budget_tokens":N}。
func (tc ThinkingConfig) MarshalJSON() ([]byte, error) {
	if tc.Type == "disabled" {
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: "disabled"})
	}
	budget := tc.BudgetTokens
	if budget <= 0 {
		budget = 4096
	}
	return json.Marshal(struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	}{Type: "enabled", BudgetTokens: budget})
}

// Text concatenates every Text block in Content, separated by newlines.
func (r LLMResponse) Text() string {
	var s string
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			if s != "" {
				s += "\n"
			}
			s += c.Text
		}
	}
	return s
}

// LLMProvider is the unified interface each backend implements. Adding a new
// protocol (e.g. OpenAI) means implementing this interface and registering it
// — callers in agent/ are unaffected.
type LLMProvider interface {
	// Chat sends one complete request. `key` is the per-provider API key
	// (injected by the registry; callers never hard-code it).
	Chat(ctx context.Context, key string, req LLMRequest) (LLMResponse, error)
	// ChatStream sends one request with `stream=true` and returns a ReadCloser
	// of Anthropic SSE chunks (`event: message_start / content_block_delta /
	// message_stop / ping …`). The caller is responsible for closing the
	// stream. Implementations must surface HTTP / wire errors via the
	// returned error — chunk parse failures should not abort the whole stream
	// silently (caller decides whether to log + continue or fail).
	ChatStream(ctx context.Context, key string, req LLMRequest) (io.ReadCloser, error)
	// ProviderType returns a short protocol id, e.g. "anthropic" / "openai".
	ProviderType() string
}

// StreamEvent is one parsed Anthropic SSE event. The Delta carries
// incremental content for content_block_delta events; MessageStart carries
// the initial response skeleton; MessageStop signals end-of-stream. The
// caller accumulates deltas to rebuild Content / Usage just like the
// reference CluadeCode streaming client does.
//
// The Message* / ContentBlock* fields below are populated by the
// anthropic.ParseSSE parser from `message_start.message.{id,model}` and
// `content_block_start.content_block.{type,id,name}` envelopes so an
// Accumulator can rebuild LLMResponse without re-parsing the JSON payload.
// All five are omitempty so legacy callers that ignore them keep working.
type StreamEvent struct {
	// Type matches the SSE event name: "message_start" / "content_block_start"
	// / "content_block_delta" / "content_block_stop" / "message_delta" /
	// "message_stop" / "ping" / "error" / "done" (the last from `[DONE]`).
	Type string `json:"type"`
	// Index is the content-block index for block-level events; 0 otherwise.
	Index int `json:"index,omitempty"`
	// DeltaType is the type of the content-block delta ("text_delta" /
	// "input_json_delta"). §128 对话即思考重构:thinking_delta 已移除。
	DeltaType string `json:"delta_type,omitempty"`
	// Delta carries the incremental payload. For text_delta the model output
	// text; for input_json_delta it is a partial JSON string (caller must
	// reassemble); for message_delta it is a StopReason change.
	Delta string `json:"delta,omitempty"`
	// StopReason is only populated on message_delta / message_stop.
	StopReason string `json:"stop_reason,omitempty"`
	// UsageInput / UsageOutput are populated on message_start (initial) and
	// message_delta (incremental).
	UsageInput  int `json:"usage_input_tokens,omitempty"`
	UsageOutput int `json:"usage_output_tokens,omitempty"`
	// ErrorMessage is populated only when Type=="error".
	ErrorMessage string `json:"error_message,omitempty"`
	// MessageID / MessageModel come from message_start.message.{id,model}.
	MessageID    string `json:"message_id,omitempty"`
	MessageModel string `json:"message_model,omitempty"`
	// ContentBlockType / ContentBlockID / ContentBlockName come from
	// content_block_start.content_block.{type,id,name}.
	ContentBlockType string `json:"content_block_type,omitempty"`
	ContentBlockID   string `json:"content_block_id,omitempty"`
	ContentBlockName string `json:"content_block_name,omitempty"`
}

// PlaceholderKey is the sentinel an operator leaves in conf.example. Any
// provider whose api_key equals this value is marked unavailable so we never
// accidentally ship bearer <placeholder> to the proxy.
const PlaceholderKey = "API-KEY-PLACEHOLDER"

// ModelInfo is the safe, key-free projection exposed via GET /api/llm/models.
type ModelInfo struct {
	AgentName    string `json:"agent_name"`
	Model        string `json:"model"`
	ProviderType string `json:"provider_type"`
}

// Registry stays in package llm (ServerGo/llm/registry.go) — it imports this
// package, so defining it here would cycle.
// type Registry struct { ... }  // SEE llm/registry.go
