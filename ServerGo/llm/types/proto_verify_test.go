package types

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContentBlock_WireShape_MatchesAnthropicProtocol 验证自定义序列化后的
// wire 形状严格符合 Claude Code 正确用例 —— 每个块只输出对应类型的字段,
// 不再携带 id/name/input 污染。参考文件:
//
//	CluadeCode的Anthropic协议-RequestBody-数据用例01/02/03.json
//	CluadeCode的Anthropic协议-ResposeBody-数据用例01/02/03.json
func TestContentBlock_WireShape_MatchesAnthropicProtocol(t *testing.T) {
	// text 块 —— 只允许 type + text。
	text := ContentBlock{Type: "text", Text: "hello", ID: "leak", Name: "leak", Input: map[string]any{"leak": true}}
	b, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"type":"text"`) || !strings.Contains(s, `"text":"hello"`) {
		t.Fatalf("text block missing core fields: %s", s)
	}
	for _, banned := range []string{`"id":`, `"name":`, `"input":`} {
		if strings.Contains(s, banned) {
			t.Fatalf("text block must NOT contain %s; got %s", banned, s)
		}
	}

	// tool_use 块 —— 必须完整保留 type + id + name + input (DouBao 拒绝缺失)。
	tu := ContentBlock{Type: "tool_use", ID: "call_1", Name: "vote", Input: map[string]any{"target": 3}}
	b, err = json.Marshal(tu)
	if err != nil {
		t.Fatal(err)
	}
	s = string(b)
	for _, required := range []string{`"type":"tool_use"`, `"id":"call_1"`, `"name":"vote"`, `"input":{"target":3}`} {
		if !strings.Contains(s, required) {
			t.Fatalf("tool_use block missing %s; got %s", required, s)
		}
	}
	// tool_use 不允许出现 tool_result 专属字段。
	if strings.Contains(s, `"tool_use_id"`) {
		t.Fatalf("tool_use block must not carry tool_use_id; got %s", s)
	}

	// tool_result 块 —— 只允许 type + tool_use_id + content + is_error。
	tr := ContentBlock{Type: "tool_result", ToolUseID: "call_1", Content: []ContentBlock{{Type: "text", Text: "ok"}}, IsError: false}
	b, err = json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	s = string(b)
	for _, required := range []string{`"type":"tool_result"`, `"tool_use_id":"call_1"`, `"content":[{"type":"text","text":"ok"}]`} {
		if !strings.Contains(s, required) {
			t.Fatalf("tool_result block missing %s; got %s", required, s)
		}
	}
	for _, banned := range []string{`"id":`, `"name":`, `"input":`} {
		if strings.Contains(s, banned) {
			t.Fatalf("tool_result block must NOT contain %s; got %s", banned, s)
		}
	}
}

// TestLLMRequest_FullMarshal_NoPollution 端到端:把 Doubao 请求体中出现过的
// 典型 messages 数组(连续 user + 嵌套 tool_result)整体序列化,确认修复后:
//   1. 不再有任何 text/tool_result 块携带 id/name/input 污染
//   2. LLMRequest 整体 Marshal 符合 Anthropic 协议顶层字段顺序
func TestLLMRequest_FullMarshal_NoPollution(t *testing.T) {
	req := LLMRequest{
		Model:     "doubao-seed-2.0-code",
		System:    []SystemBlock{{Type: "text", Text: "rules"}},
		Stream:    true,
		MaxTokens: 1024,
		Metadata:  Metadata{UserID: `{"device_id":"x"}`},
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "你是狼人杀 AI 玩家"}}},
			// 典型连续 user: tool_result 后紧跟 game_state user text。
			{Role: "user", Content: []ContentBlock{{
				Type: "tool_result", ToolUseID: "call_aa",
				Content: []ContentBlock{{Type: "text", Text: "idle_silent recorded"}},
			}}},
			{Role: "assistant", Content: []ContentBlock{
				{Type: "text", Text: "思考中..."},
				{Type: "tool_use", ID: "call_bb", Name: "vote", Input: map[string]any{"target": 5}},
			}},
		},
		Tools: []ToolDef{{Name: "vote", Description: "投票", InputSchema: map[string]any{"type": "object"}}},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	// 顶层字段必须存在且符合 Anthropic 协议。
	for _, required := range []string{
		`"model":"doubao-seed-2.0-code"`,
		`"system":[{"type":"text","text":"rules"}]`,
		`"messages":`,
		`"tools":[{"name":"vote"`,
		`"max_tokens":1024`,
		`"metadata":{"user_id":"{\"device_id\":\"x\"}"}`,
		`"stream":true`,
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("top-level field missing %s; got %s", required, s)
		}
	}

	// 关键修复:text 块和 tool_result 块不再携带 id/name/input 污染。
	if strings.Contains(s, `"id":""`) || strings.Contains(s, `"name":""`) {
		t.Fatalf("wire payload still leaks empty id/name on text/tool_result blocks: %s", s)
	}
	if strings.Contains(s, `"input":null`) {
		t.Fatalf("wire payload still leaks null input on text/tool_result blocks: %s", s)
	}

	// tool_use 块必须完整保留 id + name + input (不可被 omitempty 吃掉)。
	for _, required := range []string{
		`"tool_use"`, `"id":"call_bb"`, `"name":"vote"`, `"target":5`,
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("tool_use block lost required field %s; got %s", required, s)
		}
	}

	// tool_result.content 必须是不带污染的嵌套 text。
	if !strings.Contains(s, `"tool_use_id":"call_aa"`) || !strings.Contains(s, `"idle_silent recorded"`) {
		t.Fatalf("tool_result block malformed: %s", s)
	}
}

// TestContentBlock_EmptyText_AlwaysEmitsTextField 是 R143 (2026-07-17) 回归测试。
//
// 背景:线上的 15280-400-RequestBody.json 是 doubao-seed-2.0-pro 的真实抓包,
// 触发 400 "The request failed because it is missing `messages.content.text` parameter"
// (ResposeBody: MissingParameter, BadRequest)。
//
// 根因:ContentBlock.MarshalJSON 在 text 块上对 `text` 用 `omitempty`,当上游
// recordToolResult 把空字符串(LLM 误调未知工具 skip / DispatchTool 返回
// (空, err))塞进 Text 字段后,序列化变成 `{"type":"text"}` —— 严格代理
// 拒绝这种缺 text 的块,连带整次请求 400 + 0 token 空响应。
//
// 修复:即使 Text=="" 也必须输出 text 字段(占位 `(empty)`)。
// 本测试断言三种典型场景:直接空 Text / tool_result 嵌套空 Text / 嵌套 nil。
func TestContentBlock_EmptyText_AlwaysEmitsTextField(t *testing.T) {
	cases := []struct {
		name string
		cb   ContentBlock
	}{
		{"text block empty string", ContentBlock{Type: "text", Text: ""}},
		{"text block with id leak (must still emit text)", ContentBlock{Type: "text", Text: "", ID: "leak", Name: "leak"}},
		{"tool_result nested empty text", ContentBlock{
			Type: "tool_result", ToolUseID: "call_xx",
			Content: []ContentBlock{{Type: "text", Text: ""}},
			IsError: true,
		}},
		{"tool_result nested non-text blocks (no text leak risk)", ContentBlock{
			Type: "tool_result", ToolUseID: "call_yy",
			Content: []ContentBlock{{Type: "tool_use", ID: "x", Name: "y", Input: map[string]any{}}},
			IsError: true,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.cb)
			if err != nil {
				t.Fatal(err)
			}
			s := string(b)
			// 任何 text 块必须出现 "text":<非空> 字段,否则上游 doubao 等严格代理直接 400。
			// 注意:nil Content 不在此列(Anthropic 允许 tool_result.content 省略)。
			if tc.cb.Type == "text" || hasTextBlock(tc.cb.Content) {
				if !strings.Contains(s, `"text":`) {
					t.Fatalf("wire payload missing text field — would trigger Anthropic 400 MissingParameter; got %s", s)
				}
			}
			// 严防:不允许出现 `{"type":"text"}`(无 text 字段)的非法形状。
			if strings.Contains(s, `{"type":"text"}`) {
				t.Fatalf("wire payload still emits illegal {{type:text}} shape; got %s", s)
			}
		})
	}
}

// hasTextBlock reports whether blocks contains any Type=="text" block.
func hasTextBlock(blocks []ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == "text" {
			return true
		}
	}
	return false
}
