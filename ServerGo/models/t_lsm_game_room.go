package models

import "time"

// TLsmGameRoom is a game room. One row per (game, room-id).
type TLsmGameRoom struct {
	ID           string    `gorm:"type:char(36);primaryKey"        json:"id"`
	Name         string    `gorm:"type:varchar(64);not null;default:''" json:"name"`
	GameKind     string    `gorm:"type:varchar(32);index;not null"  json:"game_kind"`
	Capacity     int       `gorm:"not null;default:4"               json:"capacity"`
	CurrentCount int       `gorm:"not null;default:0"               json:"current_count"`
	Status       string    `gorm:"type:varchar(16);not null;default:'open'" json:"status"`
	CreatedAt    time.Time `gorm:"autoCreateTime"                   json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"                   json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameRoom) TableName() string { return "t_lsm_game_room" }
