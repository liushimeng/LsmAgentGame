// Package models — t_lsm_game_prop_usage_log: 道具使用日志表。
//
// 记录每次道具使用的完整信息：谁用了什么道具、对谁用、花了多少钱、
// 金币去向（彩池回滚/系统吸收/目标补偿）、是否中招、效果摘要。
// 供审计、反作弊、数据分析用。
//
// 2026-07-21 道具系统设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package models

import "time"

// TLsmGamePropUsageLog 是道具使用日志的一行。append-only，禁止 UPDATE/DELETE。
type TLsmGamePropUsageLog struct {
	ID            string    `gorm:"type:char(36);primaryKey"                                    json:"id"`
	RoomID        string    `gorm:"type:char(36);index:idx_room;not null"                        json:"room_id"`
	GameLogID     string    `gorm:"type:char(36);index:idx_game_log;not null;default:''"         json:"game_log_id"`
	PropID        string    `gorm:"type:char(36);index:idx_prop;not null"                        json:"prop_id"`
	FromSeat      int       `gorm:"type:int;not null"                                            json:"from_seat"`
	FromUserID    string    `gorm:"type:char(36);index:idx_from_user;not null"                   json:"from_user_id"`
	ToSeat        int       `gorm:"type:int;not null"                                            json:"to_seat"` // AOE 时为 -1
	ToUserID      string    `gorm:"type:char(36);index:idx_to_user;not null;default:''"          json:"to_user_id"`
	PricePaid     int64     `gorm:"type:bigint;not null"                                        json:"price_paid"`
	PotReturn     int64     `gorm:"type:bigint;not null;default:0"                               json:"pot_return"`      // 回滚彩池金额
	SystemAbsorb  int64     `gorm:"type:bigint;not null;default:0"                               json:"system_absorb"`   // 系统吸收金额
	TargetCompens int64     `gorm:"type:bigint;not null;default:0"                               json:"target_compens"`  // 目标补偿金额
	Hit           bool      `gorm:"type:tinyint(1);not null;default:0"                           json:"hit"`             // 是否中招
	EffectText    string    `gorm:"type:varchar(255);not null;default:''"                        json:"effect_text"`     // 效果摘要(≤200字)
	PhaseAtUse    string    `gorm:"type:varchar(32);not null;default:''"                         json:"phase_at_use"`
	RoundAtUse    int       `gorm:"type:int;not null;default:0"                                  json:"round_at_use"`
	CreatedAt     time.Time `gorm:"autoCreateTime"                                               json:"created_at"`
}

// TableName pins the SQL table name.
func (TLsmGamePropUsageLog) TableName() string { return "t_lsm_game_prop_usage_log" }
