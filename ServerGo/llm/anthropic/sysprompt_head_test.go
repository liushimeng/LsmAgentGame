// Package anthropic — sysprompt_head_test.go: 三段式 system 提示词头的
// wire 级断言(§14.3)。
//
// 这里断言的是最终出站请求体,而不是 Agent 侧的中间产物 —— 三段式头由
// Provider 在序列化前统一注入,所以「全部 Agent 都带上头」这一性质只能在
// 这一层被真正证明。
package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/agent/wwjudge"
	"LsmAgentGame/agent/wwplayer"
	anthropic "LsmAgentGame/llm/anthropic"
	"LsmAgentGame/llm/sysprompt"
	llmtypes "LsmAgentGame/llm/types"
)

const headTestAgentClass = "LsmAgentGame-Werewolf-Player"

// capturedSystem 启动一个 httptest 上游,返回其捕获到的 system[] 块。
// stream=true 时走 ChatStream(SSE)路径,否则走 Chat(JSON)路径。
func capturedSystem(t *testing.T, req llmtypes.LLMRequest, stream bool) []map[string]any {
	t.Helper()
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	p := anthropic.New([]string{srv.URL}, 5*time.Second, 0)
	if stream {
		body, err := p.ChatStream(context.Background(), "k", req)
		if err != nil {
			t.Fatalf("ChatStream: %v", err)
		}
		_, _ = io.ReadAll(body)
		_ = body.Close()
	} else {
		if _, err := p.Chat(context.Background(), "k", req); err != nil {
			t.Fatalf("Chat: %v", err)
		}
	}

	var parsed struct {
		System []map[string]any `json:"system"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode outbound body: %v (body=%s)", err, raw)
	}
	return parsed.System
}

func assertHead(t *testing.T, system []map[string]any, wantBody int) {
	t.Helper()
	if len(system) != 3+wantBody {
		bs, _ := json.Marshal(system)
		t.Fatalf("system blocks = %d, want %d (3 head + %d body): %s", len(system), 3+wantBody, wantBody, bs)
	}

	wantBilling := "x-anthropic-billing-header: cc_version=" + sysprompt.CCVersion +
		"; cc_entrypoint=server; cc_is_subagent=true; cc_agent_name=" + headTestAgentClass + ";"
	if got := system[0]["text"]; got != wantBilling {
		t.Errorf("system[0].text = %v, want %q", got, wantBilling)
	}
	if _, ok := system[0]["cache_control"]; ok {
		t.Errorf("system[0] 计费头不得带 cache_control: %v", system[0])
	}
	if got := system[1]["text"]; got != sysprompt.IdentityText {
		t.Errorf("system[1].text = %v, want %q", got, sysprompt.IdentityText)
	}
	if got := system[2]["text"]; got != sysprompt.CoreRulesText {
		t.Errorf("system[2] 核心规则文本与 sysprompt.CoreRulesText 不一致")
	}
	for i := 1; i <= 2; i++ {
		cc, _ := system[i]["cache_control"].(map[string]any)
		if cc["type"] != "ephemeral" {
			t.Errorf("system[%d].cache_control = %v, want ephemeral", i, system[i]["cache_control"])
		}
	}
	for i, b := range system {
		for k := range b {
			switch k {
			case "type", "text", "cache_control":
			default:
				t.Errorf("system[%d] 泄漏非法键 %q: %v", i, k, b)
			}
		}
	}
}

// TestSystemPromptHead_PrependedOnNonStreamWire: 非流式路径 —— 头在调用方
// 自有块之前,自有块逐字节不变。
func TestSystemPromptHead_PrependedOnNonStreamWire(t *testing.T) {
	body := llmtypes.SystemBlock{Type: "text", Text: "狼人杀硬约束:只能调用工具列表中的工具。"}
	system := capturedSystem(t, llmtypes.LLMRequest{
		Model:          "m",
		AgentClassName: headTestAgentClass,
		System:         []llmtypes.SystemBlock{body},
		Messages:       []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, false)
	assertHead(t, system, 1)
	if got := system[3]["text"]; got != body.Text {
		t.Errorf("system[3].text = %v, want caller block %q", got, body.Text)
	}
}

// TestSystemPromptHead_PrependedOnStreamWire: 流式路径与非流式对称。
func TestSystemPromptHead_PrependedOnStreamWire(t *testing.T) {
	system := capturedSystem(t, llmtypes.LLMRequest{
		Model:          "m",
		AgentClassName: headTestAgentClass,
		System:         []llmtypes.SystemBlock{{Type: "text", Text: "body"}},
		Messages:       []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
		Stream:         true,
	}, true)
	assertHead(t, system, 1)
}

// TestSystemPromptHead_NoCallerBlocks: 无自有块时只发三段头 —— 三段式头是
// 协议层常量,不随调用方有没有 prompt 而退化。
func TestSystemPromptHead_NoCallerBlocks(t *testing.T) {
	system := capturedSystem(t, llmtypes.LLMRequest{
		Model:          "m",
		AgentClassName: headTestAgentClass,
		Messages:       []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, false)
	assertHead(t, system, 0)
}

// TestSystemPromptHead_Idempotent: 调用方已自带头时不得重复注入。
func TestSystemPromptHead_Idempotent(t *testing.T) {
	pre := sysprompt.Head(headTestAgentClass)
	system := capturedSystem(t, llmtypes.LLMRequest{
		Model:          "m",
		AgentClassName: headTestAgentClass,
		System: append(append([]llmtypes.SystemBlock{}, pre...),
			llmtypes.SystemBlock{Type: "text", Text: "body"}),
		Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, false)
	if len(system) != 4 {
		t.Fatalf("system blocks = %d, want 4 (no double head)", len(system))
	}
	blob, _ := json.Marshal(system)
	if n := strings.Count(string(blob), "x-anthropic-billing-header:"); n != 1 {
		t.Errorf("计费头出现 %d 次, want 1: %s", n, blob)
	}
}

// TestSystemPromptHead_NonAgentCallerIsNotSubagent: 无 AgentClassName 的调用
// (健康探针 / 后台自检)⇒ cc_is_subagent=false,且不带 cc_agent_name。
func TestSystemPromptHead_NonAgentCallerIsNotSubagent(t *testing.T) {
	system := capturedSystem(t, llmtypes.LLMRequest{
		Model:    "m",
		Messages: []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
	}, false)
	want := "x-anthropic-billing-header: cc_version=" + sysprompt.CCVersion +
		"; cc_entrypoint=server; cc_is_subagent=false;"
	if got := system[0]["text"]; got != want {
		t.Errorf("system[0].text = %v, want %q", got, want)
	}
}

// TestSystemPromptHead_RealAgentPromptBuilders: 端到端回归 —— 用**真实 Agent
// 的 prompt builder** 产出的 system 组装请求,最终 wire 必须是
// 「三段式头 + 该 Agent 自有块」,且自有块字节不变。
//
// 这条用例证明的不是"provider 会插头"(上面已证),而是"Agent 侧零改动即升级":
// 狼人杀玩家(14KB 规则段 + 自有 cache 断点)与法官(单块动态公告)两条形态迥异
// 的 Agent 都只多出三段头,原有 prompt 一个字节不动。
func TestSystemPromptHead_RealAgentPromptBuilders(t *testing.T) {
	cases := []struct {
		name       string
		agentClass string
		blocks     []llmtypes.SystemBlock
	}{
		{
			name:       "werewolf-player",
			agentClass: "LsmAgentGame-Werewolf-Player",
			blocks:     wwplayer.BuildSystemPrompt("", wwplayer.PersonalityVector{}, "", "", false),
		},
		{
			name:       "werewolf-judge",
			agentClass: "LsmAgentGame-Werewolf-Judge",
			blocks:     wwjudge.BuildJudgeSystemPrompt(wwjudge.JudgePendingSpeakStart, wwjudge.GameSnapshot{}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.blocks) == 0 {
				t.Fatalf("%s 的 prompt builder 返回空 system", tc.name)
			}
			system := capturedSystem(t, llmtypes.LLMRequest{
				Model:          "m",
				AgentClassName: tc.agentClass,
				System:         tc.blocks,
				Messages:       []llmtypes.Message{{Role: "user", Content: []llmtypes.ContentBlock{{Type: "text", Text: "hi"}}}},
			}, false)

			wantBilling := "x-anthropic-billing-header: cc_version=" + sysprompt.CCVersion +
				"; cc_entrypoint=server; cc_is_subagent=true; cc_agent_name=" + tc.agentClass + ";"
			if got := system[0]["text"]; got != wantBilling {
				t.Errorf("system[0].text = %v, want %q", got, wantBilling)
			}
			if got := system[2]["text"]; got != sysprompt.CoreRulesText {
				t.Errorf("%s: system[2] 不是核心行为规则段", tc.name)
			}
			for i, b := range tc.blocks {
				if got := system[3+i]["text"]; got != b.Text {
					t.Errorf("%s: 自有块 %d 字节被改动", tc.name, i)
				}
			}
		})
	}
}
