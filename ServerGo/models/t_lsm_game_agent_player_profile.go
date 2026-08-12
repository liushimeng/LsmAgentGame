// Package models — Agent 玩家行为画像表。
//
// 2026-08-11 §20260811-05 U1 新增(狼人杀 Agent 玩家行为模式学习)。
// 每个 (LLM 模型 model_key × 人类玩家 user_id) 组合拥有一份跨局的
// Markdown 打法画像,每局结束后由该模型自己的 LLM 异步迭代更新,
// 下一局经房间级预取缓存注入 GameContext.PlayerProfiles 渲染进 prompt。
//
// 隐私合规(知识库 Agent-Surpport-01 §4.1 硬约束):
//   - 只持久化 LLM 摘要后的「打法画像」(风格标签/历史倾向),不存聊天原文;
//   - 画像只对 bot 自己(prompt 注入)与 admin 可见,无前端公开接口。
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameAgentPlayerProfile 是 "一模型 × 一人类玩家" 的行为画像行。
//
// Version 是乐观锁版本号:每次写回 +1,防多房间(重开局相邻触发)并发覆盖。
// GamesTogether 已累计同局次数;WinsTogether 同阵营且胜利的次数。
// 不建外键:provider/user 可被软删/重建,画像按 (model_key,user_id) 独立存活。
type TLsmGameAgentPlayerProfile struct {
	ID            string     `gorm:"type:char(36);primaryKey"                             json:"id"`
	ModelKey      string     `gorm:"type:varchar(64);uniqueIndex:uk_model_user;not null"  json:"model_key"`
	UserID        string     `gorm:"type:varchar(64);uniqueIndex:uk_model_user;not null"  json:"user_id"`
	ProfileMD     string     `gorm:"type:text;not null"                                   json:"profile_md"`
	GamesTogether uint       `gorm:"type:int unsigned;not null;default:0"                 json:"games_together"`
	WinsTogether  uint       `gorm:"type:int unsigned;not null;default:0"                 json:"wins_together"`
	Version       uint       `gorm:"type:int unsigned;not null;default:0"                 json:"version"`
	LastSeenAt    *time.Time `gorm:"type:datetime;default:null"                           json:"last_seen_at"`
	CreatedAt     time.Time  `gorm:"autoCreateTime"                                       json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime"                                       json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameAgentPlayerProfile) TableName() string { return "t_lsm_game_agent_player_profile" }
