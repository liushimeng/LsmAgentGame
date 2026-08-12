// BUG-R226-P1-02 (2026-08-01) 回归测试:顶层 thinking 字段的 wire 形状必须
// 带 type 判别符,严格对齐 §14.1 权威用例
// (CluadeCode的Anthropic协议-RequestBody-数据用例01/02/03.json 中
// "thinking": {"type": "disabled"} 的形状)。
//
// 背景:R224 (e799a7f) 重新引入 thinking 时序列化为
// `{"enabled":true,"budget_tokens":4096}`(**没有 type 判别符**),严格校验的
// 上游(DouBao 最严、GLM 次之)因此报 400
// "missing messages.content.thinking parameter" —— 本轮实测 110 次
// (DouBao 独占 97),DouBao 座位整局 0 发言。本测试在 ThinkingConfig 的
// 序列化形状漂移时立即失败,防止 R224 类"凭上游报错文案猜形状"的缺陷复现。
package types

import (
	"encoding/json"
	"testing"
)

// TestThinkingConfig_WireShape_EnabledHasTypeDiscriminator 断言 enabled 时
// wire 形状严格为 {"type":"enabled","budget_tokens":N},且无 enabled 布尔字段。
func TestThinkingConfig_WireShape_EnabledHasTypeDiscriminator(t *testing.T) {
	payload, err := json.Marshal(ThinkingConfig{Type: "enabled", BudgetTokens: 4096})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "enabled" {
		t.Fatalf("wire type = %v, want \"enabled\" (payload=%s)", got["type"], payload)
	}
	if got["budget_tokens"].(float64) != 4096 {
		t.Fatalf("wire budget_tokens = %v, want 4096 (payload=%s)", got["budget_tokens"], payload)
	}
	if _, ok := got["enabled"]; ok {
		t.Fatalf("wire leaked legacy \"enabled\" bool field (payload=%s)", payload)
	}
	if _, ok := got["budget"]; ok {
		t.Fatalf("wire leaked message-block-style \"budget\" field (payload=%s)", payload)
	}
}

// TestThinkingConfig_WireShape_DisabledOmitsBudget 断言 disabled 时 wire 形状
// 严格为 {"type":"disabled"},不带 budget_tokens(与权威用例一致)。
func TestThinkingConfig_WireShape_DisabledOmitsBudget(t *testing.T) {
	payload, err := json.Marshal(ThinkingConfig{Type: "disabled"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "disabled" {
		t.Fatalf("wire type = %v, want \"disabled\" (payload=%s)", got["type"], payload)
	}
	if _, ok := got["budget_tokens"]; ok {
		t.Fatalf("disabled wire must omit budget_tokens (payload=%s)", payload)
	}
	if len(got) != 1 {
		t.Fatalf("disabled wire must carry exactly one key (payload=%s)", payload)
	}
}

// TestThinkingConfig_WireShape_DefaultsToEnabledWithBudgetFallback 断言
// 零值(Type 空 / BudgetTokens ≤ 0)归一化为 {"type":"enabled","budget_tokens":4096}。
func TestThinkingConfig_WireShape_DefaultsToEnabledWithBudgetFallback(t *testing.T) {
	payload, err := json.Marshal(ThinkingConfig{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "enabled" {
		t.Fatalf("zero-value wire type = %v, want \"enabled\" (payload=%s)", got["type"], payload)
	}
	if got["budget_tokens"].(float64) != 4096 {
		t.Fatalf("zero-value wire budget_tokens = %v, want 4096 fallback (payload=%s)", got["budget_tokens"], payload)
	}
}

// TestLLMRequest_ThinkingTopLevelOnly 断言 LLMRequest 序列化时 thinking 只
// 出现在顶层,messages 内容块不含 type=="thinking" 块(§14.1 权威用例的
// messages[].content[] 只含 text/tool_use/tool_result 三种块)。
func TestLLMRequest_ThinkingTopLevelOnly(t *testing.T) {
	req := LLMRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
		},
		MaxTokens: 1024,
		Thinking:  &ThinkingConfig{Type: "enabled", BudgetTokens: 4096},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Thinking map[string]any `json:"thinking"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Thinking["type"] != "enabled" {
		t.Fatalf("top-level thinking type = %v, want \"enabled\" (payload=%s)", got.Thinking["type"], payload)
	}
	for mi, m := range got.Messages {
		for bi, b := range m.Content {
			if b["type"] == "thinking" {
				t.Fatalf("messages[%d].content[%d] leaked a thinking content block (payload=%s)", mi, bi, payload)
			}
		}
	}
}
