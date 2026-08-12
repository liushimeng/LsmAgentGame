// Package models — per-model action/decision record.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §1.5),
// each row records one tool call or in-game action a model made. This is the
// "decision" half of the audit trail — t_lsm_game_model_chat_message holds the
// raw LLM traffic, this table holds the parsed, structured decision.
//
// The composite index idx_gamelog_phase = (game_log_id, phase) covers the
// hottest query: "what actions did this model take in phase X of game Y".
// A separate index on ActionType supports "show me all seer_checks across
// the system" — useful for prompt tuning.
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameModelAction is one tool call / game action a model committed to.
type TLsmGameModelAction struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement"                              json:"id"`
	GameLogID    string    `gorm:"type:char(36);index:idx_gamelog_phase,priority:1;not null" json:"game_log_id"`
	BotUserID    string    `gorm:"type:char(36);index;not null"                         json:"bot_user_id"`
	Phase        string    `gorm:"type:varchar(32);index:idx_gamelog_phase,priority:2;not null" json:"phase"`
	ActionType   string    `gorm:"type:varchar(32);index;not null"                       json:"action_type"`
	ActionTarget string    `gorm:"type:varchar(64);not null;default:''"                 json:"action_target"`
	Payload      string    `gorm:"type:text;not null"                                    json:"payload"`
	Reasoning    string    `gorm:"type:mediumtext;not null"                              json:"reasoning"`
	Accepted     bool      `gorm:"not null;default:true"                                 json:"accepted"`
	CreatedAt    time.Time `gorm:"autoCreateTime"                                        json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameModelAction) TableName() string { return "t_lsm_game_model_action" }