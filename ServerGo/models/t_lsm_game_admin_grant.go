// Package models — daily admin grant deduplication table.
//
// One row per (provider_id, grant_date). The composite unique key guarantees
// "每天每模型最多 grant 一次" (one grant per model per UTC+8 day) — replaying
// the call, double-clicking, or running two admin instances cannot grant the
// same model twice. Mirrors the dedup pattern from TLsmGameDailyReward so the
// two "one-per-day" mechanisms share the same idiom.
//
// 2026-07-14 §135: designed alongside the Agent wallet redesign (default
// initial balance 5000). Rows are written by API.ModelGrantAPI.GrantDaily
// BEFORE the matching WalletService.Credit call; on Credit failure the row is
// rolled back via DELETE so dedup state stays in sync with wallet state.
package models

import "time"

// TLsmGameAdminGrant records that a super admin has credited `Amount` coins
// to a given LLM provider's bot wallet on a given UTC+8 date.
type TLsmGameAdminGrant struct {
	ID           string    `gorm:"type:char(36);primaryKey"                                                                  json:"id"`
	ProviderID   string    `gorm:"type:char(36);uniqueIndex:uk_provider_date,priority:1;not null"                         json:"provider_id"`
	GrantDate    string    `gorm:"type:date;uniqueIndex:uk_provider_date,priority:2;not null"                              json:"grant_date"`
	GrantedByUID string    `gorm:"type:char(36);not null;default:''"                                                        json:"granted_by_uid"`
	Amount       int64     `gorm:"type:bigint;not null"                                                                     json:"amount"`
	BotUserID    string    `gorm:"type:char(36);index:idx_bot_user,priority:1;not null"                                     json:"bot_user_id"`
	BalanceAfter int64     `gorm:"type:bigint;not null;default:0"                                                           json:"balance_after"`
	Remark       string    `gorm:"type:varchar(255);default:''"                                                             json:"remark"`
	CreatedAt    time.Time `gorm:"autoCreateTime"                                                                           json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameAdminGrant) TableName() string { return "t_lsm_game_admin_grant" }
