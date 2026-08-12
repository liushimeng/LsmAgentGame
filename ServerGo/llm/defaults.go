// Package llm — code-level defaults that replace the per-provider list that
// used to live in LsmAgentGame.conf.llm.providers[]. After the
// 2026-08-12 切走 cfg-Provider 改造 the runtime source of truth is the
// t_lsm_game_llm_provider table; when that table is empty on first boot the
// registry auto-seeds the rows defined here so a fresh deployment still has the
// historical 8 default models (美团/豆包/DeepSeek/智谱/Kimi/MiniMax/Qwen/Xiaomi).
//
// Why code constants instead of "another config file":
//   - §118 / kind-skipping-moth §3 already established DB-first loading;
//     putting defaults in code removes the temptation to edit the conf again.
//   - The defaults are public: anyone can `git grep DefaultProviders` and see
//     what a brand-new install ships with. No more "why is my DB empty after
//     I removed llm.providers from the conf" surprise.
//   - Tests can rely on a stable, in-process function instead of a fixture
//     JSON file.
//
// Editing rules:
//   - The 8 models here are mirrored 1:1 with the historical LsmAgentGame.conf
//     rows. Changing one without a coordinated DB migration will produce a
//     different seed on first boot — fine, but document the change in
//     docs/LLM与Agent/LLM供应商设计.md.
//   - Add new providers via t_lsm_game_llm_provider admin API after the room
//     is running; only edit this list when the deployment is brand-new and the
//     DB has never been seeded.
package llm

import (
	types "LsmAgentGame/llm/types"
)

// DefaultProviderSeed is the wire-shape used by NewRegistryWithDB's
// DB-empty seed path. It mirrors config.ProviderConfig field-for-field so
// seedFromConfigLocked (the function that walks this list) can stay
// unchanged.
//
// The api_key on every row is the PlaceholderKey sentinel so a freshly seeded
// registry reports every model as `available=false` until an operator
// replaces the key via /api/admin/llm/providers. UnusableProviders() surfaces
// these at startup (see main.go BUG-R115-01 warning) so operators see the
// problem before they open a 7-AI room.
type DefaultProviderSeed struct {
	AgentName    string
	Model        string
	ProviderType string
	APIKey       string
	// ThinkingRequired / ThinkingBudget mirror the historical
	// cfg.LLM.Providers[].thinking_required / thinking_budget columns.
	// §R224: 8/8 default proxies observed in production reject requests that
	// omit the leading `{type:"thinking", budget:N}` block, so we ship
	// thinking_enabled=true for the models that historically had the flag.
	ThinkingRequired bool
	ThinkingBudget   int
}

// DefaultProviders is the historical 8-model roster, byte-for-byte identical
// to the rows that lived in LsmAgentGame.conf.llm.providers[] before the
// 2026-08-12 removal. Each row's APIKey is the PlaceholderKey sentinel; the
// seed path encrypts it the same way cfg-loaded rows are encrypted
// (util.EncryptAPIKey).
//
// DO NOT edit this list casually — a 9th model added here will appear on every
// fresh install, and editing an existing row will diverge from rows already in
// operators' DBs (the auto-seed only fires when the table is empty, so
// existing rows are NOT overwritten by an updated DefaultProviders).
func DefaultProviders() []DefaultProviderSeed {
	return []DefaultProviderSeed{
		{AgentName: "美团 LongCat-2.0", Model: "MeiTuan-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey},
		{AgentName: "豆包 2.0", Model: "DouBao-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey},
		{AgentName: "DeepSeek V4-Pro", Model: "DeepSeek-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey, ThinkingRequired: true, ThinkingBudget: 4096},
		{AgentName: "智谱 GLM-5.2", Model: "GLM-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey, ThinkingRequired: true, ThinkingBudget: 4096},
		{AgentName: "Kimi 2.7", Model: "Kimi-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey},
		{AgentName: "MiniMax M3", Model: "MinMax-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey},
		{AgentName: "Qwen 3.7-Plus-and-Max", Model: "Qwen-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey},
		{AgentName: "Xiaomi mimo-v2.5-pro", Model: "Xiaomi-model", ProviderType: "anthropic", APIKey: types.PlaceholderKey},
	}
}

// DefaultEndpoint is the historical LsmAgentGame.conf.llm.endpoint value.
// The shared anthropic.Provider appends "/v1/messages" when calling the
// upstream proxy. Operators can override per-row via the admin UI
// (t_lsm_game_llm_provider.endpoint), in which case the per-row value wins.
const DefaultEndpoint = "http://8.130.85.252:29000/Anthropic"

// DefaultEndpoints is the failover list (BUG-R220). When the primary
// endpoint keeps failing dial/5xx/503 the provider advances through this list.
// Currently the same single endpoint as the legacy scalar; kept here so an
// operator who wants to add a backup proxy doesn't need to hand-edit a code
// constant — they can do it through t_lsm_game_llm_provider rows plus the
// shared llm.endpoints[0] override (cfg.LLM.Endpoints).
var DefaultEndpoints = []string{
	DefaultEndpoint,
}
