// Package models contains the GORM schema for every table.
//
// Per project rules (CLAUDE.md §3), model files in this directory use the
// t_lsm_game_*.go prefix. All other Go files in the project use snake_case.
package models

import "time"

// UserType defines the role-based access level for users.
// 1 = normal user (default), 2 = admin, 3 = super admin.
type UserType int

const (
	UserTypeNormal UserType = 1 // 普通用户（默认）
	UserTypeAdmin  UserType = 2 // 管理员用户
	UserTypeSuper  UserType = 3 // 超级管理员用户
)

// TLsmGameUser is the account table.
//
// IsBot / BotProviderID / LinkedProviderAccount (added by the "模型管理 +
// 模型玩家持久化 + 模型金币" plan, kind-skipping-moth §1.2) identify rows
// that back an LLM-driven bot seat rather than a human account. JSON tags
// use omitempty so the existing /api/user/* endpoints see no new fields
// for normal users.
type TLsmGameUser struct {
	ID           string     `gorm:"type:char(36);primaryKey"              json:"id"`
	Account      string     `gorm:"type:varchar(32);uniqueIndex;not null" json:"account"`
	// Nickname is the user-facing display name. Unique across the platform;
	// defaults to Account during registration.
	Nickname     string     `gorm:"type:varchar(64);uniqueIndex;not null" json:"nickname"`
	PasswordHash string     `gorm:"type:varchar(255);not null"            json:"-"`
	Phone        string     `gorm:"type:varchar(32);index"                json:"phone"`
	Email        string     `gorm:"type:varchar(128);index"               json:"email"`
	// UserType defines the user's role: 1=normal, 2=admin, 3=super admin.
	// Defaults to 1 (normal user) on registration.
	UserType     UserType   `gorm:"type:tinyint;not null;default:1"      json:"user_type"`
	// MyInviteCode is the user's OWN personal referral code, generated at
	// registration. Other users register with this code to be linked back to
	// this user as their referrer. Unique so it can be looked up directly.
	// This is the ONLY invite-code concept left in the system — there is no
	// admin-managed "gate" code anymore (CLAUDE.md invite refactor 2026-06).
	MyInviteCode string     `gorm:"type:varchar(32);uniqueIndex"          json:"my_invite_code"`
	// ReferrerUserID is the ID of the user whose personal invite code
	// (MyInviteCode) this user supplied at registration. Empty if none.
	// Indexed so "who did I invite" / "who invited me" lookups are cheap.
	ReferrerUserID string   `gorm:"type:char(36);index"                   json:"referrer_user_id"`
	// ReferralCount is how many users have registered using this user's
	// personal invite code. Incremented atomically at each referred signup.
	ReferralCount int       `gorm:"not null;default:0"                    json:"referral_count"`
	// Language is the user's preferred UI locale. One of zh-CN / en / ja.
	// Defaults to zh-CN; AutoMigrate backfills the column for existing rows.
	Language     string     `gorm:"type:varchar(10);not null;default:'zh-CN'" json:"language"`
	// IsBot is true when this account backs an LLM-driven bot seat rather than
	// a real human user. Bot rows have PasswordHash set to a non-loginable
	// random hash and IsBot=true by convention; the auth flow rejects them
	// (see util.VerifyPassword / AuthService.Login).
	IsBot                 bool   `gorm:"not null;default:false"               json:"is_bot,omitempty"`
	// BotProviderID points to t_lsm_game_llm_provider.id and identifies which
	// LLM model drives this bot. Indexed for "list all bots of provider X".
	// Empty for human accounts.
	BotProviderID         string `gorm:"type:char(36);index;default:''"       json:"bot_provider_id,omitempty"`
	// LinkedProviderAccount is the external account name (e.g. the provider's
	// vendor-side login) this bot is associated with, for audit display.
	LinkedProviderAccount string `gorm:"type:varchar(64);default:''"          json:"linked_provider_account,omitempty"`
	CreatedAt    time.Time  `gorm:"autoCreateTime"                        json:"created_at"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime"                        json:"updated_at"`
	LastLoginAt  *time.Time `                                              json:"last_login_at,omitempty"`
}

// TableName pins the SQL table name.
func (TLsmGameUser) TableName() string { return "t_lsm_game_user" }