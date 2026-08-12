// Package models — daily login reward dedup table.
//
// One row per (user_id, reward_date). The unique key guarantees that the UTC+8
// daily login bonus can only be claimed once per natural day — replaying the
// login flow, WS reconnect, or page refresh cannot double-credit.
package models

import "time"

// TLsmGameDailyReward records that a user has already received the daily
// login bonus for a given UTC+8 date.
type TLsmGameDailyReward struct {
	ID         string    `gorm:"type:char(36);primaryKey"              json:"id"`
	UserID     string    `gorm:"type:char(36);uniqueIndex:uk_user_date,priority:1;not null" json:"user_id"`
	RewardDate string    `gorm:"type:date;uniqueIndex:uk_user_date,priority:2;not null"    json:"reward_date"`
	Amount     int64     `gorm:"type:bigint;not null;default:2000"     json:"amount"`
	CreatedAt  time.Time `gorm:"autoCreateTime"                        json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameDailyReward) TableName() string { return "t_lsm_game_daily_reward" }
