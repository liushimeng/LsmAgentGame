// Package models — LLM provider metadata table.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §1.1),
// each row represents one configured LLM model whose metadata is editable from
// the admin UI (no longer requires editing LsmWebGame.conf + restart).
//
// API keys are NEVER stored in plaintext: APIKeyEnc holds the AES-256-GCM
// ciphertext produced by util.EncryptAPIKey, and APIKeyHint stores a short
// fingerprint (e.g. "sk-abcd...wxyz") for display purposes only.
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameLlmProvider is the per-model metadata row.
//
// Endpoint: when non-empty, overrides the global LLM endpoint from
// config.LLMConfig for this provider. Empty means "use the global default".
//
// §R224 (2026-08-01) — 重新引入 §128 误删的 extended thinking 配置字段。
// `thinking_enabled=true` 表示此模型要求 anthropic.Provider 在每条 message
// 头部注入 `{type:"thinking", budget:N}` 块;`thinking_budget_tokens` 为
// budget 数值(典型 4096/8192)。详见 BUG-NEW-1 (20260801_124553) §3.1。
// 旧行(§128 后入库的)的两个新列默认为 0/false,等同"未启用"。
type TLsmGameLlmProvider struct {
	ID                    string    `gorm:"type:char(36);primaryKey"                     json:"id"`
	AgentName             string    `gorm:"type:varchar(64);uniqueIndex;not null"        json:"agent_name"`
	Model                 string    `gorm:"type:varchar(64);uniqueIndex;not null"        json:"model"`
	ProviderType          string    `gorm:"type:varchar(32);not null;default:'anthropic'" json:"provider_type"`
	APIKeyEnc             string    `gorm:"type:text;not null"                           json:"-"`
	APIKeyHint            string    `gorm:"type:varchar(16);not null;default:''"         json:"api_key_hint"`
	Endpoint              string    `gorm:"type:varchar(256);not null;default:''"        json:"endpoint"`
	Enabled               bool      `gorm:"not null;default:true"                        json:"enabled"`
	Remark                string    `gorm:"type:varchar(255);default:''"                 json:"remark"`
	// §R224 — extended thinking 配置开关与 budget。两列均允许 0 值;
	// gorm default 都为 0/'' 表示"未启用" + "无 budget(代码内 fallback 4096)"。
	ThinkingEnabled       bool      `gorm:"not null;default:false"                       json:"thinking_enabled"`
	ThinkingBudgetTokens  int       `gorm:"not null;default:0"                           json:"thinking_budget_tokens"`
	CreatedAt             time.Time `gorm:"autoCreateTime"                               json:"created_at"`
	UpdatedAt             time.Time `gorm:"autoUpdateTime"                               json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameLlmProvider) TableName() string { return "t_lsm_game_llm_provider" }