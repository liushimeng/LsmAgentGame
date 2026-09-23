// Package openai — sysprompt_head_test.go: §14.3 三段式 system 提示词头在
// openai-completions 协议下的断言。
//
// OpenAI 协议没有 system[] 数组,三段头与 Agent 自有块一起被拼成单条
// role:"system" message —— 头必须在最前面(前缀稳定 = 上游 prefix cache 可命中),
// 且 cc_is_subagent / cc_agent_name 必须跟随调用方 AgentClassName。
package openai

import (
	"strings"
	"testing"

	"LsmAgentGame/llm/sysprompt"
	types "LsmAgentGame/llm/types"
)

const headTestAgentClass = "LsmAgentGame-Werewolf-Player"

func TestSystemPromptHead_PrependedInOpenAIProtocol(t *testing.T) {
	r := buildRequest(types.LLMRequest{
		Model:          "m",
		MaxTokens:      100,
		AgentClassName: headTestAgentClass,
		System:         []types.SystemBlock{{Type: "text", Text: "body"}},
		Messages:       []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, false)

	if r.Messages[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", r.Messages[0].Role)
	}
	wantPrefix := sysprompt.BillingHeaderText(
		sysprompt.EntrypointServer, true, headTestAgentClass) + "\n\n"
	if !strings.HasPrefix(r.Messages[0].Content, wantPrefix) {
		t.Errorf("system message 必须以计费元数据头开头(带 cc_agent_name): %q", r.Messages[0].Content)
	}
	for _, want := range []string{sysprompt.IdentityText, sysprompt.CoreRulesText, "body"} {
		if !strings.Contains(r.Messages[0].Content, want) {
			t.Errorf("system message 缺少 %q", truncateForLog(want))
		}
	}
	// 顺序:计费头 → 身份 → 核心规则 → 自有块。
	if strings.Index(r.Messages[0].Content, sysprompt.IdentityText) >
		strings.Index(r.Messages[0].Content, sysprompt.CoreRulesText) {
		t.Errorf("身份段必须在核心规则段之前")
	}
	if strings.Index(r.Messages[0].Content, sysprompt.CoreRulesText) >
		strings.Index(r.Messages[0].Content, "body") {
		t.Errorf("三段式头必须整体位于自有块之前")
	}
}

// TestSystemPromptHead_IdempotentOpenAI: 调用方已自带头时不得重复。
func TestSystemPromptHead_IdempotentOpenAI(t *testing.T) {
	pre := sysprompt.Head(headTestAgentClass)
	r := buildRequest(types.LLMRequest{
		Model:          "m",
		MaxTokens:      100,
		AgentClassName: headTestAgentClass,
		System: append(append([]types.SystemBlock{}, pre...),
			types.SystemBlock{Type: "text", Text: "body"}),
		Messages: []types.Message{{Role: "user", Content: []types.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, false)
	if n := strings.Count(r.Messages[0].Content, "x-anthropic-billing-header:"); n != 1 {
		t.Errorf("计费头出现 %d 次, want 1", n)
	}
}

func truncateForLog(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
