// Regression tests for GameService.ValidateAgentSeats — the R106 P0 pre-write
// registry validation hook. Verifies the three failure modes (unknown /
// placeholder-key / empty-key) plus the nil-seats and nil-registry no-op paths.
//
// Constructing an llm.Registry for tests is cheap: NewRegistry(cfg) is config-
// only with no DB, and loadFromConfigLocked marks a provider available iff the
// api_key is non-empty AND not the PlaceholderKey sentinel. That flag drives
// registry.IsAvailable, which is exactly what ValidateAgentSeats consults.

package ws

import (
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/llm"
	"LsmAgentGame/service"
)

// stubRegistry builds a registry with the listed models, each flagged
// available iff api_key is non-empty and non-placeholder.
func stubRegistry(t *testing.T, keys map[string]string) *llm.Registry {
	t.Helper()
	pcs := make([]config.ProviderConfig, 0, len(keys))
	for model, key := range keys {
		pcs = append(pcs, config.ProviderConfig{
			AgentName: model,
			Model:     model,
			APIKey:    key,
		})
	}
	return llm.NewRegistry(config.LLMConfig{Providers: pcs})
}

// newWerewolfSvc builds a GameService whose werewolfMgr holds the given registry
// (or nil) so ValidateAgentSeats can run without wiring main.go.
func newWerewolfSvc(reg *llm.Registry) *GameService {
	mgr := werewolf.NewWerewolfManagerWithRegistry(reg)
	return &GameService{werewolfMgr: mgr}
}

func TestValidateAgentSeats_NilWerewolfMgr(t *testing.T) {
	s := &GameService{werewolfMgr: nil}
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{{Seat: 0, ModelKey: "whatever"}})
	if err != nil {
		t.Fatalf("nil werewolfMgr must short-circuit, got %v", err)
	}
}

func TestValidateAgentSeats_NilRegistry(t *testing.T) {
	s := &GameService{werewolfMgr: werewolf.NewWerewolfManagerWithRegistry(nil)}
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{{Seat: 0, ModelKey: "X"}})
	if err != nil {
		t.Fatalf("nil registry must short-circuit, got %v", err)
	}
}

func TestValidateAgentSeats_EmptySeats(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{"MeiTuan-model": "sk-real"}))
	if err := s.ValidateAgentSeats(nil); err != nil {
		t.Fatalf("nil seats must short-circuit, got %v", err)
	}
	if err := s.ValidateAgentSeats([]service.AgentSeatConfig{}); err != nil {
		t.Fatalf("empty seats must short-circuit, got %v", err)
	}
}

func TestValidateAgentSeats_AllValid(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{
		"MeiTuan-model": "sk-mt",
		"DouBao-model":  "sk-db",
	}))
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{
		{Seat: 0, ModelKey: "MeiTuan-model"},
		{Seat: 1, ModelKey: "DouBao-model"},
	})
	if err != nil {
		t.Fatalf("valid keys must pass, got %v", err)
	}
}

func TestValidateAgentSeats_UnknownKeyRejected(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{"MeiTuan-model": "sk-real"}))
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{
		{Seat: 0, ModelKey: "MeiTuan-model"},
		{Seat: 1, ModelKey: "GPT-4o"},          // not registered
		{Seat: 2, ModelKey: "Claude-3.5"},      // not registered
	})
	if err == nil {
		t.Fatalf("expected ErrValidationFailed for unknown keys")
	}
	if err.Code != errcode.ErrValidationFailed {
		t.Fatalf("expected code %d, got %d (%s)", errcode.ErrValidationFailed, err.Code, err.Message)
	}
	for _, bad := range []string{"GPT-4o", "Claude-3.5"} {
		if !contains(err.Message, bad) {
			t.Fatalf("expected message to mention %q, got %q", bad, err.Message)
		}
	}
}

func TestValidateAgentSeats_PlaceholderKeyRejected(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{
		"MeiTuan-model": "API-KEY-PLACEHOLDER", // placeholder → not available
	}))
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{
		{Seat: 0, ModelKey: "MeiTuan-model"},
	})
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("placeholder-key model must be rejected, got %v", err)
	}
}

// TestValidateAgentSeats_ErrorListsAvailableModels covers R187-2: when one
// or more requested model_keys are rejected, the error message must also
// carry the currently available model keys (`available_models: [...]`) so
// API clients can auto-retry with a corrected key. The list must match
// IsAvailable semantics (registered && enabled && non-placeholder key) and
// must exclude the rejected/unavailable keys themselves.
func TestValidateAgentSeats_ErrorListsAvailableModels(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{
		"MeiTuan-model": "sk-mt",               // available
		"DouBao-model":  "sk-db",               // available
		"GLM-model":     "API-KEY-PLACEHOLDER", // placeholder → NOT available
	}))
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{
		{Seat: 0, ModelKey: "Tencent-model"}, // unknown → rejected
	})
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("unknown key must be rejected, got %v", err)
	}
	if !contains(err.Message, "Tencent-model") {
		t.Fatalf("message must name the rejected key, got %q", err.Message)
	}
	if !contains(err.Message, "available_models: [") {
		t.Fatalf("message must carry available_models tail, got %q", err.Message)
	}
	for _, good := range []string{"MeiTuan-model", "DouBao-model"} {
		if !contains(err.Message, good) {
			t.Fatalf("available_models must include %q, got %q", good, err.Message)
		}
	}
	if contains(err.Message, "GLM-model") {
		t.Fatalf("available_models must exclude placeholder-key model, got %q", err.Message)
	}
}

// TestIsModelAvailable covers the R187-2 ModelAvailabilityHook: the probe
// must mirror registry.IsAvailable and degrade to false when no registry
// is wired.
func TestIsModelAvailable(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{
		"MeiTuan-model": "sk-mt",
		"GLM-model":     "API-KEY-PLACEHOLDER",
	}))
	if !s.IsModelAvailable("MeiTuan-model") {
		t.Fatal("available model must report true")
	}
	if s.IsModelAvailable("GLM-model") {
		t.Fatal("placeholder-key model must report false")
	}
	if s.IsModelAvailable("nope") {
		t.Fatal("unknown model must report false")
	}
	noReg := &GameService{werewolfMgr: nil}
	if noReg.IsModelAvailable("MeiTuan-model") {
		t.Fatal("nil manager must report false")
	}
}

func TestValidateAgentSeats_EmptyKeyRejected(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{
		"MeiTuan-model": "", // empty key → not available
	}))
	err := s.ValidateAgentSeats([]service.AgentSeatConfig{
		{Seat: 0, ModelKey: "MeiTuan-model"},
	})
	if err == nil || err.Code != errcode.ErrValidationFailed {
		t.Fatalf("empty-key model must be rejected, got %v", err)
	}
}

// TestValidateAgentSeats_InvisibleRuneKeyAccepted covers R187-2's mirror
// scenario: the registry holds the clean key "Tencent-model" while the
// client copy-pasted a displayed key carrying a stray zero-width non-joiner
// (U+200C). The seat must validate AND the seat's ModelKey must be rewritten
// in place to the clean form so downstream consumers never see the dirty key.
func TestValidateAgentSeats_InvisibleRuneKeyAccepted(t *testing.T) {
	s := newWerewolfSvc(stubRegistry(t, map[string]string{
		"Tencent-model": "sk-real",
	}))
	seats := []service.AgentSeatConfig{
		{Seat: 0, ModelKey: "Tencent\u200c-model"}, // ZWNJ between "Tencent" and "-model"
	}
	if err := s.ValidateAgentSeats(seats); err != nil {
		t.Fatalf("key with stray ZWNJ must match the clean registry key, got %v", err)
	}
	if seats[0].ModelKey != "Tencent-model" {
		t.Fatalf("ModelKey must be sanitized in place, got %q", seats[0].ModelKey)
	}
}

// contains is a tiny helper so we don't pull in strings for just this test.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
