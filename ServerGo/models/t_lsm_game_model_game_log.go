// Package models — per-model game log table.
//
// Per the "模型管理 + 模型玩家持久化 + 模型金币" plan (kind-skipping-moth §1.3),
// each row records one model's participation in one game. The composite index
// idx_provider_created = (provider_id, started_at) covers the hottest query:
// "list this model's games in time order". BotUserID and RoomID are kept as
// separate indexes so that "what did this bot play?" and "what models were in
// this room?" stay cheap.
//
// v2.0 (DEFECT 3): idx_room_seat = (room_id, seat) covers the settlement hot
// path RecordLogService.GameLogIDByRoomSeat(room_id, seat) on cache-miss /
// process-restart. The standalone room_id index is kept for room-scoped
// queries that don't filter by seat.
//
// EndedAt and CoinDelta are populated when the game finishes (won/lost/drawn).
// Result defaults to "" during play and is set to one of {win, lose, draw,
// abandoned} on terminal events.
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameModelGameLog is one model's record of one game.
type TLsmGameModelGameLog struct {
	ID            string     `gorm:"type:char(36);primaryKey"                                          json:"id"`
	ProviderID    string     `gorm:"type:char(36);index:idx_provider_created,priority:1;not null"      json:"provider_id"`
	BotUserID     string     `gorm:"type:char(36);index;not null"                                       json:"bot_user_id"`
	RoomID        string     `gorm:"type:char(36);index;index:idx_room_seat,priority:1;not null"         json:"room_id"`
	GameKind      string     `gorm:"type:varchar(32);index;not null"                                    json:"game_kind"`
	Seat          int        `gorm:"not null;index:idx_room_seat,priority:2"                             json:"seat"`
	Role          string     `gorm:"type:varchar(32);not null;default:''"                               json:"role"`
	StartedAt     time.Time  `gorm:"index:idx_provider_created,priority:2"                              json:"started_at"`
	EndedAt       *time.Time `                                                                           json:"ended_at,omitempty"`
	Result        string     `gorm:"type:varchar(32);not null;default:''"                               json:"result"`
	CoinDelta     int64      `gorm:"not null;default:0"                                                 json:"coin_delta"`
	LLMCallCount  int        `gorm:"not null;default:0"                                                 json:"llm_call_count"`
	InputTokens   int        `gorm:"not null;default:0"                                                 json:"input_tokens"`
	OutputTokens  int        `gorm:"not null;default:0"                                                 json:"output_tokens"`
	FinalHand     string     `gorm:"type:varchar(255);default:''"                                       json:"final_hand"`
}

// TableName pins the SQL table name.
func (TLsmGameModelGameLog) TableName() string { return "t_lsm_game_model_game_log" }