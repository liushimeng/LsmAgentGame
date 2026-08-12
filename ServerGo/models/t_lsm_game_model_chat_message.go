// Package models — per-model LLM chat message persistence.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §1.4),
// this table is separate from t_lsm_game_chat_message (which holds public
// chat). This table holds EVERY LLM traffic — system prompt, user prompt,
// assistant text, tool_use blocks, tool_result, thinking blocks, etc. — so
// the admin model-detail page can replay a model's reasoning end-to-end.
//
// The composite index idx_gamelog_seq = (game_log_id, seq) covers the hottest
// query: "give me the time-ordered traffic for one game". A secondary index
// on (bot_user_id, provider_id) supports "give me this bot's last N calls
// across all games" use cases.
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameModelChatMessage is one message in one model's LLM transcript.
type TLsmGameModelChatMessage struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"                          json:"id"`
	GameLogID  string    `gorm:"type:char(36);index:idx_gamelog_seq,priority:1;not null" json:"game_log_id"`
	BotUserID  string    `gorm:"type:char(36);index;not null"                       json:"bot_user_id"`
	ProviderID string    `gorm:"type:char(36);index;not null"                       json:"provider_id"`
	RoomID     string    `gorm:"type:char(36);index;not null"                       json:"room_id"`
	Seq        int64     `gorm:"not null;index:idx_gamelog_seq,priority:2"          json:"seq"`
	Role       string    `gorm:"type:varchar(16);not null"                          json:"role"`
	Content    string    `gorm:"type:mediumtext;not null"                           json:"content"`
	Phase      string    `gorm:"type:varchar(32);not null;default:''"               json:"phase"`
	ToolName   string    `gorm:"type:varchar(64);not null;default:''"               json:"tool_name"`
	ToolInput  string    `gorm:"type:text;not null"                                 json:"tool_input"`
	Thinking   string    `gorm:"type:mediumtext;not null"                           json:"thinking"`
	StopReason string    `gorm:"type:varchar(32);not null;default:''"               json:"stop_reason"`
	LatencyMs  int       `gorm:"not null;default:0"                                 json:"latency_ms"`
	CreatedAt  time.Time `gorm:"autoCreateTime"                                     json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameModelChatMessage) TableName() string { return "t_lsm_game_model_chat_message" }