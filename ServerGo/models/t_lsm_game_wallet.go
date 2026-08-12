// Package models — wallet table.
//
// t_lsm_game_wallet holds the per-user coin balance. Registration seeds it
// with 1000 coins (the "注册默认 1000" product decision).
package models

import "time"

// TLsmGameWallet is the per-user wallet. One row per user (user_id unique).
type TLsmGameWallet struct {
	ID          string    `gorm:"type:char(36);primaryKey"              json:"id"`
	UserID      string    `gorm:"type:char(36);uniqueIndex;not null"    json:"user_id"`
	Balance     int64     `gorm:"type:bigint;not null;default:1000"     json:"balance"`
	TotalEarned int64     `gorm:"type:bigint;not null;default:0"        json:"total_earned"`
	TotalSpent int64     `gorm:"type:bigint;not null;default:0"        json:"total_spent"`
	CreatedAt   time.Time `gorm:"autoCreateTime"                        json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"                        json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameWallet) TableName() string { return "t_lsm_game_wallet" }
