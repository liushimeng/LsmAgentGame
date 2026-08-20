// Package models — wallet transaction (double-entry ledger).
//
// Every balance change in the system produces exactly one row here. The
// balance_after column lets an auditor re-play the ledger and arrive at the
// same ending balance as the wallet table — the two sources of truth must
// always agree.
package models

import "time"

// TLsmGameWalletTx is an immutable ledger entry. Never update or delete a row;
// corrections are done via an opposite-sign entry.
//
// v2.0 (DEFECT 2) idempotency note: settlement idempotency is enforced at the
// APPLICATION layer (WalletService.AlreadySettled, scoped to settlement tx
// types), NOT via a DB UNIQUE(user_id, ref_type, ref_id, tx_type) index. A
// global unique index would break legitimate repeat rows for tx types that use
// empty/repeating ref fields (daily_login writes empty ref_type/ref_id every
// day; admin_adjust/admin_daily_grant reuse the admin's user id as ref_id). See
// WalletService.AlreadySettled for the full rationale.
type TLsmGameWalletTx struct {
	ID           string    `gorm:"type:char(36);primaryKey"              json:"id"`
	UserID       string    `gorm:"type:char(36);index:idx_user_created,priority:1;not null" json:"user_id"`
	TxType       string    `gorm:"type:varchar(32);index;not null"       json:"tx_type"`
	Amount       int64     `gorm:"type:bigint;not null"                  json:"amount"`
	BalanceAfter int64     `gorm:"type:bigint;not null"                  json:"balance_after"`
	RefType      string    `gorm:"type:varchar(32);index;default:''"     json:"ref_type"`
	RefID        string    `gorm:"type:varchar(128);index;default:''"       json:"ref_id"`
	GameKind     string    `gorm:"type:varchar(32);index;default:''"     json:"game_kind"`
	Remark       string    `gorm:"type:varchar(255);default:''"          json:"remark"`
	CreatedAt    time.Time `gorm:"autoCreateTime;index:idx_user_created,priority:2" json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameWalletTx) TableName() string { return "t_lsm_game_wallet_tx" }
