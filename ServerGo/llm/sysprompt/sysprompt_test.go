package sysprompt

import (
	"encoding/json"
	"strings"
	"testing"

	types "LsmAgentGame/llm/types"
)

const testAgentClass = "LsmAgentGame-Werewolf-Player"

// TestHead_WireShape 锁定三段式头的 wire 形状(§14.1「ContentBlock wire 形状
// 必须按 Type 收敛」在 system 数组上的同款纪律):
//
//	[0] 计费头   — 无 cache_control
//	[1] 身份声明 — cache_control=ephemeral
//	[2] 核心规则 — cache_control=ephemeral
func TestHead_WireShape(t *testing.T) {
	head := Head(testAgentClass)
	if len(head) != 3 {
		t.Fatalf("want 3 head blocks, got %d", len(head))
	}

	want0 := "x-anthropic-billing-header: cc_version=" + CCVersion +
		"; cc_entrypoint=server; cc_is_subagent=true; cc_agent_name=" + testAgentClass + ";"
	if head[0].Text != want0 {
		t.Errorf("billing block text = %q, want %q", head[0].Text, want0)
	}
	if head[0].CacheControl != nil {
		t.Errorf("billing block must NOT carry cache_control, got %v", head[0].CacheControl)
	}

	if head[1].Text != IdentityText {
		t.Errorf("identity block text = %q, want %q", head[1].Text, IdentityText)
	}
	if head[2].Text != CoreRulesText {
		t.Errorf("core rules block text mismatch (len %d vs %d)", len(head[2].Text), len(CoreRulesText))
	}
	for i := 1; i <= 2; i++ {
		if head[i].CacheControl["type"] != "ephemeral" {
			t.Errorf("head[%d] cache_control = %v, want ephemeral", i, head[i].CacheControl)
		}
		if head[i].Type != "text" {
			t.Errorf("head[%d] type = %q, want text", i, head[i].Type)
		}
	}

	// wire 断言:text 块只允许 {type,text}(+可选 cache_control),不得携带
	// tool_use / tool_result 专属键。
	for i, b := range head {
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal head[%d]: %v", i, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal head[%d]: %v", i, err)
		}
		for _, forbidden := range []string{"id", "name", "input", "tool_use_id", "is_error"} {
			if _, ok := m[forbidden]; ok {
				t.Errorf("head[%d] leaks %q key: %s", i, forbidden, raw)
			}
		}
	}
}

// TestHead_NonAgentCaller: 无 AgentClassName 的调用(健康探针 / 后台自检)
// 必须 cc_is_subagent=false 且不带 cc_agent_name。
func TestHead_NonAgentCaller(t *testing.T) {
	head := Head("")
	want := "x-anthropic-billing-header: cc_version=" + CCVersion +
		"; cc_entrypoint=server; cc_is_subagent=false;"
	if head[0].Text != want {
		t.Errorf("billing block text = %q, want %q", head[0].Text, want)
	}
	if strings.Contains(head[0].Text, "cc_agent_name") {
		t.Errorf("empty agent class must not emit cc_agent_name: %q", head[0].Text)
	}
}

// TestEnsureHead_Prepends: 头必须在 Agent 自有块之前(缓存前缀顺序),
// 且 Agent 自有块逐字节不变。
func TestEnsureHead_Prepends(t *testing.T) {
	body := []types.SystemBlock{
		{Type: "text", Text: "【13 人标准竞技局规则】…", CacheControl: map[string]string{"type": "ephemeral"}},
		{Type: "text", Text: "persona"},
	}
	got := EnsureHead(body, testAgentClass)
	if len(got) != 5 {
		t.Fatalf("want 5 blocks (3 head + 2 body), got %d", len(got))
	}
	for i := 0; i < 3; i++ {
		if got[i].Text != Head(testAgentClass)[i].Text {
			t.Errorf("block %d is not the standard head", i)
		}
	}
	if got[3].Text != body[0].Text || got[4].Text != body[1].Text {
		t.Errorf("body blocks mutated: %+v", got[3:])
	}
	if got[3].CacheControl["type"] != "ephemeral" {
		t.Errorf("body cache_control lost: %v", got[3].CacheControl)
	}
}

// TestEnsureHead_Idempotent: 已带头的 body 不得被重复注入。
func TestEnsureHead_Idempotent(t *testing.T) {
	once := EnsureHead([]types.SystemBlock{{Type: "text", Text: "body"}}, testAgentClass)
	twice := EnsureHead(once, testAgentClass)
	if len(twice) != len(once) {
		t.Fatalf("double injection: %d → %d blocks", len(once), len(twice))
	}
	if strings.Count(joinTexts(twice), BillingHeaderPrefix) != 1 {
		t.Errorf("billing header text appears %d times, want 1: %q",
			strings.Count(joinTexts(twice), BillingHeaderPrefix), joinTexts(twice))
	}
}

// TestEnsureHead_EmptyBody: 无自有块时只返回三段头(而非 nil)。
func TestEnsureHead_EmptyBody(t *testing.T) {
	got := EnsureHead(nil, "")
	if len(got) != 3 {
		t.Fatalf("want 3 blocks, got %d", len(got))
	}
}

// TestHeadBytes_Positive: 字节估算必须覆盖三段文本(供 Agent 字节预算使用)。
func TestHeadBytes_Positive(t *testing.T) {
	n := HeadBytes(testAgentClass)
	want := len(IdentityText) + len(CoreRulesText) + len(headBillingText(testAgentClass))
	if n <= want {
		t.Errorf("HeadBytes = %d, want > %d (文本本体 + cache_control JSON 近似)", n, want)
	}
}

// TestTextsAreByteStable: 三段文本是全体 Agent 的共享缓存前缀,必须逐字节
// 稳定 —— 任何一次无意修改都会让全量 Agent 的 prompt cache 命中失效。
// 这里锁定关键锚点(而非整个文本哈希,便于人工校对 diff)。
func TestTextsAreByteStable(t *testing.T) {
	anchors := []string{
		"x-anthropic-billing-header:",
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.",
		"You are an agent for Claude Code, Anthropic's official CLI for Claude.",
		"Complete the task fully",
		"Your strengths:",
		"Guidelines:",
		"NEVER create files unless they're absolutely necessary",
		"do not re-delegate your entire assignment to another single subagent",
		"Do NOT Write report/summary/findings/analysis .md files",
		"\n\n",
	}
	all := BillingHeaderText(EntrypointServer, true, testAgentClass) + IdentityText + CoreRulesText
	for _, a := range anchors {
		if !strings.Contains(all, a) {
			t.Errorf("三段式文本缺少锚点 %q", a)
		}
	}
	if strings.Contains(CoreRulesText, "total_tokens") {
		t.Errorf("核心规则不得收编 Claude Code 的会话态 <total_tokens> 动态行")
	}
}

func headBillingText(agentClass string) string {
	return BillingHeaderText(EntrypointServer, agentClass != "", agentClass)
}

func joinTexts(blocks []types.SystemBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		sb.WriteString(b.Text)
	}
	return sb.String()
}
