// Package models — secret_letter table (§20260812-03 U2).
//
// §3.1 暗线信件 + §4.3 私密结盟提议合并后的统一抽象。白天 speak→vote 窗口
// 内可发送短消息到任意非自己非死亡玩家,§119 协议层三重隔离:
//   - 不入 chat_message 表
//   - 不入 chat_history 队列
//   - 不入 BotTranscript.HeartThought
//
// 仅 SecretLetterRoom 内存态持有 + DB 持久化(用于审计/历史复盘),
// 前端 SecretLetterPanel 仅渲染「自己收发的」信件。
package models

import "time"

// TLsmGameSecretLetter is one private letter sent from one seat to another
// during a werewolf game's daytime speak→vote window.
type TLsmGameSecretLetter struct {
	ID        string    `gorm:"type:char(36);primaryKey"                          json:"id"`
	RoomID    string    `gorm:"type:char(36);not null;index:idx_slr_room_round"   json:"room_id"`
	FromSeat  int       `gorm:"not null"                                          json:"from_seat"`
	ToSeat    int       `gorm:"not null"                                          json:"to_seat"`
	Body      string    `gorm:"type:varchar(200);not null"                        json:"body"`
	Round     int       `gorm:"not null"                                          json:"round"`
	IsRead    bool      `gorm:"not null;default:false"                            json:"is_read"`
	CreatedAt time.Time `gorm:"autoCreateTime"                                    json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameSecretLetter) TableName() string { return "t_lsm_game_secret_letter" }
