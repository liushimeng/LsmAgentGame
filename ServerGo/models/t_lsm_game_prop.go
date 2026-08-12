// Package models — t_lsm_game_prop: 狼人杀 13 人局道具目录表。
//
// 一行定义一种道具（心理战武器化）。6 种道具对应 6 类 LLM 注入攻击手法：
//   - markdown_bomb: Markdown 格式注入攻击（紧急公告）
//   - nested_maze: 提示词套娃（剧本迷宫）
//   - char_confuse: 字符级欺骗注入（胡言乱语）
//   - long_swear: 长上下文注意力失焦（长篇废话）
//   - task_disguise: 任务马甲式注入（编剧委托）
//   - emotion_plea: 情绪操控式注入（苦苦哀求）
//
// 2026-07-21 道具系统设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package models

import "time"

// TLsmGameProp 是道具目录的一行。管理员可通过 enabled 字段禁用某种道具，
// 或通过 price / base_hit_rate 字段动态调整经济平衡（无需重启）。
//
// v2 重设计（2026-07-21）新增列：
//   - inject_gen_key:注入生成器注册表 key（默认 == prop_key,可不显式设置）。
//   - effect_type: 效果落地类型,逗号分隔（expose_identity/attention_scatter/
//     target_twist/emotion_disturb/confuse_seer）。决定命中后 GameContext 的
//     "干扰信号" 如何应用（见 prop_effect.go）。
//   - twist_seat_src: target_twist 使用,引导座位来源（from_seat/random_enemy/
//     most_trusted）。
type TLsmGameProp struct {
	ID           string    `gorm:"type:char(36);primaryKey"                       json:"id"`
	PropKey      string    `gorm:"type:varchar(64);uniqueIndex;not null"          json:"prop_key"`
	NameZh       string    `gorm:"type:varchar(64);not null"                      json:"name_zh"`
	NameEn       string    `gorm:"type:varchar(64);not null"                      json:"name_en"`
	NameJa       string    `gorm:"type:varchar(64);not null"                      json:"name_ja"`
	Description  string    `gorm:"type:varchar(512);not null;default:''"          json:"description"`
	Price        int64     `gorm:"type:bigint;not null;default:100"               json:"price"`
	BaseHitRate  int       `gorm:"type:int;not null;default:30"                   json:"base_hit_rate"` // 基础中招率(百分比 0-100)
	IsAOE        bool      `gorm:"type:tinyint(1);not null;default:0"             json:"is_aoe"`        // 是否范围效果
	TargetCamp   string    `gorm:"type:varchar(16);not null;default:'any'"        json:"target_camp"`   // 'any'|'wolf'|'good'
	Enabled      bool      `gorm:"type:tinyint(1);not null;default:1"             json:"enabled"`
	MaxPerGame   int       `gorm:"type:int;not null;default:3"                    json:"max_per_game"`  // 每局每位玩家最多购买数
	CooldownSec  int       `gorm:"type:int;not null;default:30"                   json:"cooldown_sec"`  // 同玩家两次使用间隔(秒)
	InjectGenKey string    `gorm:"type:varchar(64);not null;default:''"           json:"inject_gen_key"`
	EffectType   string    `gorm:"type:varchar(64);not null;default:''"           json:"effect_type"`
	TwistSeatSrc string    `gorm:"type:varchar(32);not null;default:'from_seat'"  json:"twist_seat_src"`
	CreatedAt    time.Time `gorm:"autoCreateTime"                                  json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"                                  json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameProp) TableName() string { return "t_lsm_game_prop" }
