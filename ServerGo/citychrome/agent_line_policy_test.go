// Package citychrome — agent_line_policy_test.go: 批次79 线路策略单测。
//
// 策略语义:""=沿用 cfg.ModelKey(旧行为);LinePolicyRandom=每个 mission
// 从 registry.ListEnabled() 按 concurrency_lines 加权随机;其它=固定 key。
package citychrome

import (
	"context"
	"io"
	"testing"

	llmtypes "LsmAgentGame/llm/types"
)

// policyFakeProvider 最小 provider 桩(线路策略不发起真实调用)。
type policyFakeProvider struct{}

func (policyFakeProvider) Chat(context.Context, string, llmtypes.LLMRequest) (llmtypes.LLMResponse, error) {
	return llmtypes.LLMResponse{}, nil
}

func (policyFakeProvider) ChatStream(context.Context, string, llmtypes.LLMRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (policyFakeProvider) ProviderType() string { return "fake-policy" }

// policyFakeRegistry 返回固定模型目录,记录 Get 的 modelKey。
type policyFakeRegistry struct {
	models []llmtypes.ModelInfo
	got    []string
}

func (r *policyFakeRegistry) Get(modelKey string) (llmtypes.LLMProvider, string, error) {
	r.got = append(r.got, modelKey)
	return policyFakeProvider{}, "k", nil
}

func (r *policyFakeRegistry) GetThinkingEnabled(string) (bool, int) { return false, 0 }

func (r *policyFakeRegistry) ListEnabled() []llmtypes.ModelInfo { return r.models }

// 缺省策略 → 沿用 cfg.ModelKey(零回归)。
func TestResolveLineKey_LegacyDefault(t *testing.T) {
	a := NewAgent(AgentConfig{ModelKey: "DeepSeek-model"}, nil, &policyFakeRegistry{})
	if got := a.resolveLineKey(); got != "DeepSeek-model" {
		t.Fatalf("legacy default = %q, want DeepSeek-model", got)
	}
}

// 固定策略 → 原样返回,忽略池内容。
func TestResolveLineKey_Pinned(t *testing.T) {
	a := NewAgent(AgentConfig{ModelKey: "DeepSeek-model"}, nil, &policyFakeRegistry{
		models: []llmtypes.ModelInfo{{Model: "Kimi-model", ConcurrencyLines: 4}},
	})
	a.SetLinePolicy("Kimi-model")
	if got := a.resolveLineKey(); got != "Kimi-model" {
		t.Fatalf("pinned = %q, want Kimi-model", got)
	}
}

// random 策略 → 结果始终来自池;加权(4:1)下大池更常被抽中。
func TestResolveLineKey_RandomFromPool(t *testing.T) {
	reg := &policyFakeRegistry{models: []llmtypes.ModelInfo{
		{Model: "A-model", ConcurrencyLines: 4},
		{Model: "B-model", ConcurrencyLines: 1},
	}}
	a := NewAgent(AgentConfig{ModelKey: "DeepSeek-model"}, nil, reg)
	a.SetLinePolicy(LinePolicyRandom)
	counts := map[string]int{}
	for i := 0; i < 500; i++ {
		key := a.resolveLineKey()
		if key != "A-model" && key != "B-model" {
			t.Fatalf("random key %q not in pool", key)
		}
		counts[key]++
	}
	if counts["A-model"] == 0 || counts["B-model"] == 0 {
		t.Fatalf("weighted random must hit both models, counts=%v", counts)
	}
	if counts["A-model"] <= counts["B-model"] {
		t.Fatalf("4-line model must dominate, counts=%v", counts)
	}
}

// random 但池空 → 回退 cfg.ModelKey(不因线路池空缺整条通道失败)。
func TestResolveLineKey_RandomEmptyPoolFallback(t *testing.T) {
	a := NewAgent(AgentConfig{ModelKey: "DeepSeek-model"}, nil, &policyFakeRegistry{})
	a.SetLinePolicy(LinePolicyRandom)
	if got := a.resolveLineKey(); got != "DeepSeek-model" {
		t.Fatalf("empty pool fallback = %q, want DeepSeek-model", got)
	}
}

// randomLineKey 空列表 / 权重口径。
func TestRandomLineKey(t *testing.T) {
	if got := randomLineKey(nil); got != "" {
		t.Fatalf("nil list must yield empty, got %q", got)
	}
	if got := randomLineKey([]llmtypes.ModelInfo{{Model: "Z-model"}}); got != "Z-model" {
		t.Fatalf("zero concurrency_lines must default to weight 1, got %q", got)
	}
}

// 离线占位 Service(agent=nil)的 SetLinePolicy/LinePolicy 必须 nil-safe。
func TestServiceSetLinePolicy_NilSafe(t *testing.T) {
	var svc *Service
	svc.SetLinePolicy("random")
	if got := svc.LinePolicy(); got != "" {
		t.Fatalf("nil service LinePolicy = %q, want empty", got)
	}
}
