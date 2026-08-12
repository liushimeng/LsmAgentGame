// Package models — generic key/value store for server-side metadata.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §2.1),
// this table backs persistent server-side secrets and small configuration
// blobs that must survive across restarts but are not appropriate for the
// human-editable LsmAgentGame.conf file. The primary use today is storing the
// 32-byte master AES key (base64-encoded) used to encrypt per-provider API
// keys via util.EncryptAPIKey.
//
// Reserved keys (current):
//   - "llm_api_key_master" : base64(AES-256-GCM master key). Auto-created on
//                            first call to util.EnsureMasterKey.
//
// Future keys may include feature flags, rate-limiter buckets, etc.
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameKV is a single-row key/value record. Key is the natural primary key.
type TLsmGameKV struct {
	Key       string    `gorm:"type:varchar(64);primaryKey" json:"key"`
	Value     string    `gorm:"type:text;not null"           json:"value"`
	Remark    string    `gorm:"type:varchar(255);default:''" json:"remark"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"               json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameKV) TableName() string { return "t_lsm_game_kv" }