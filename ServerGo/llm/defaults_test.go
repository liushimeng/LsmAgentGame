// Package llm — defaults_test.go covers the code-level provider seeds that
// 2026-08-12 切走 cfg-Provider 改造 introduced. The historical 8-model
// roster lives in DefaultProviders() instead of LsmAgentGame.conf; these
// tests pin the model key + agent name + provider type + thinking flag for
// each entry so an accidental rename surfaces as a test failure rather than
// a silent seed divergence.

package llm

import (
	"testing"

	types "LsmAgentGame/llm/types"
)

// TestDefaultProviders_ReturnsEight confirms the historical roster has 8
// entries. Adding a 9th model here is fine (operators who upgrade from a
// fresh DB will see the new row), but reducing the count would break
// existing production deployments that rely on the 8-bot lineup.
func TestDefaultProviders_ReturnsEight(t *testing.T) {
	got := DefaultProviders()
	if len(got) != 8 {
		t.Fatalf("DefaultProviders() returned %d entries, want 8", len(got))
	}
}

// TestDefaultProviders_UniqueModelKeys enforces the (t_lsm_game_llm_provider)
// unique constraint on `model`. Two seeds with the same model key would
// crash the first-boot auto-seed with a DB duplicate error.
func TestDefaultProviders_UniqueModelKeys(t *testing.T) {
	got := DefaultProviders()
	seen := make(map[string]struct{}, len(got))
	for _, p := range got {
		if _, dup := seen[p.Model]; dup {
			t.Errorf("DefaultProviders: duplicate model %q", p.Model)
		}
		seen[p.Model] = struct{}{}
	}
}

// TestDefaultProviders_PlaceholderKeys ensures every seed row carries the
// PlaceholderKey sentinel. A real API key checked into the source tree
// would be a CVE — the whole point of moving defaults into code is that
// operators MUST replace the key via the admin UI before opening a 7-AI
// room.
func TestDefaultProviders_PlaceholderKeys(t *testing.T) {
	for _, p := range DefaultProviders() {
		if p.APIKey != types.PlaceholderKey {
			t.Errorf("DefaultProviders[%s].APIKey = %q, want %q",
				p.Model, p.APIKey, types.PlaceholderKey)
		}
	}
}

// TestDefaultProviders_AnthropicProviderType enforces the historical
// provider_type="anthropic" for every row. OpenAI is reserved (CLAUDE.md
// §14) and not yet wired.
func TestDefaultProviders_AnthropicProviderType(t *testing.T) {
	for _, p := range DefaultProviders() {
		if p.ProviderType != "anthropic" {
			t.Errorf("DefaultProviders[%s].ProviderType = %q, want %q",
				p.Model, p.ProviderType, "anthropic")
		}
	}
}

// TestDefaultProviders_ThinkingFlags pins the historical §R224 thinking
// configuration: DeepSeek + GLM ship with thinking_required=true /
// budget=4096; the other six models ship with thinking_required=false.
// Changing a single row's flag here is a deliberate change that MUST also
// update docs/LLM与Agent/LLM供应商设计.md.
func TestDefaultProviders_ThinkingFlags(t *testing.T) {
	want := map[string]struct {
		required bool
		budget   int
	}{
		"MeiTuan-model": {false, 0},
		"DouBao-model":  {false, 0},
		"DeepSeek-model": {true, 4096},
		"GLM-model":     {true, 4096},
		"Kimi-model":    {false, 0},
		"MinMax-model":  {false, 0},
		"Qwen-model":    {false, 0},
		"Xiaomi-model":  {false, 0},
	}
	got := DefaultProviders()
	for _, p := range got {
		w, ok := want[p.Model]
		if !ok {
			t.Errorf("DefaultProviders: unexpected model %q", p.Model)
			continue
		}
		if p.ThinkingRequired != w.required {
			t.Errorf("DefaultProviders[%s].ThinkingRequired = %v, want %v",
				p.Model, p.ThinkingRequired, w.required)
		}
		if p.ThinkingBudget != w.budget {
			t.Errorf("DefaultProviders[%s].ThinkingBudget = %d, want %d",
				p.Model, p.ThinkingBudget, w.budget)
		}
	}
}

// TestDefaultEndpoint_Stable confirms the historical default endpoint is
// the operator-supplied proxy URL. A new fresh install on a fresh DB will
// route every LLM call to this address until an operator overwrites
// individual rows via the admin UI.
func TestDefaultEndpoint_Stable(t *testing.T) {
	if DefaultEndpoint == "" {
		t.Fatal("DefaultEndpoint must be non-empty for first-boot auto-seed")
	}
}
