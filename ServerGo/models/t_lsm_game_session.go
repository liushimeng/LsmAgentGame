package models

import "time"

// TLsmGameSession tracks issued JWTs for audit + revocation.
type TLsmGameSession struct {
	ID        string    `gorm:"type:char(36);primaryKey"        json:"id"`
	UserID    string    `gorm:"type:char(36);index;not null"    json:"user_id"`
	Token     string    `gorm:"type:varchar(1024);not null"     json:"-"`
	IP        string    `gorm:"type:varchar(64)"                json:"ip"`
	UA        string    `gorm:"type:varchar(255)"               json:"ua"`
	ExpiresAt time.Time `gorm:"index"                           json:"expires_at"`
	CreatedAt time.Time `gorm:"autoCreateTime"                  json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameSession) TableName() string { return "t_lsm_game_session" }
