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
	// CreatorUserID 2026-11-13 §测试房自助解散 — 建房者 user_id,创建者自助
	// 解散端点(DELETE /api/rooms/:id)的权限判据;历史行为空 = 不可自助解散
	// (仍走管理员 DELETE /api/admin/rooms/:room_id)。
	CreatorUserID string    `gorm:"type:varchar(36);index;not null;default:''" json:"creator_user_id"`
	CreatedAt     time.Time `gorm:"autoCreateTime"                   json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"                   json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameRoom) TableName() string { return "t_lsm_game_room" }
