// Package models — faction_bet table (§20260812-03 U3).
//
// §6.2 阵营赌注系统:白天 speak 结束 → vote 启动前 30s 窗口内,玩家可对其他玩家
// 阵营下注(用金币),猜对翻倍(赔率 1:1),未押中 50% 销毁 + 50% 滚存到下局。
//
// 约束:
//   - 每人每轮最多下注 1 次(UNIQUE 索引 room_id+user_id+round+target_seat)
//   - 金额范围 10~500 金币
//   - 仅人类玩家可下注(LLM Provider 不参与,§118)
//   - 押注信息对其他玩家**不可见**(§135 公平性)
package models

import "time"

// TLsmGameFactionBet is one faction bet placed by a human player during
// the daytime speak→vote window.
type TLsmGameFactionBet struct {
	ID              string    `gorm:"type:char(36);primaryKey"                                          json:"id"`
	RoomID          string    `gorm:"type:char(36);not null;index:idx_fb_room_user_round"              json:"room_id"`
	UserID          string    `gorm:"type:char(36);not null"                                            json:"user_id"`
	Round           int       `gorm:"not null;index:idx_fb_room_user_round,priority:2"                 json:"round"`
	TargetSeat      int       `gorm:"not null"                                                          json:"target_seat"`
	PredictedFaction string   `gorm:"type:varchar(8);not null"                                          json:"predicted_faction"` // "wolf"/"good"
	Amount          int       `gorm:"not null"                                                          json:"amount"`
	Settled         bool      `gorm:"not null;default:false"                                            json:"settled"`
	Payout          int       `gorm:"not null;default:0"                                                json:"payout"`
	Result          string    `gorm:"type:varchar(8);not null;default:''"                               json:"result"` // "win"/"lose"/""
	CreatedAt       time.Time `gorm:"autoCreateTime"                                                    json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGameFactionBet) TableName() string { return "t_lsm_game_faction_bet" }
