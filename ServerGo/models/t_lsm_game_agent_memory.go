// Package models — Agent 持久化记忆表。
//
// 2026-07-20 §131 新增(狼人杀 Agent 持久化记忆)。每个 LLM 模型(model_key)
// 拥有一份跨局、跨进程的 Markdown 记忆(类比 Claude Code 的 MEMORY.md),
// 每局结束后由该模型自己的 LLM 自我迭代总结,下一局每次 LLM 调用注入。
// 详见 docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md。
//
// Per CLAUDE.md §3, models in this directory use the t_lsm_game_*.go prefix.
package models

import "time"

// TLsmGameAgentMemory 是"一模型一行"的持久化记忆行。
//
// Version 是乐观锁版本号:每次写回 +1,防多房间(重开局相邻触发)并发覆盖。
// GameCount 已累计总结的局数;LastGameID 最近一次总结的 roomID。
// 不建外键:provider 可被软删/重建,记忆按 model_key 独立存活。
type TLsmGameAgentMemory struct {
	ID             string     `gorm:"type:char(36);primaryKey"               json:"id"`
	ModelKey       string     `gorm:"type:varchar(64);uniqueIndex;not null"  json:"model_key"`
	MemoryMD       string     `gorm:"type:mediumtext;not null"               json:"memory_md"`
	Version        uint       `gorm:"type:int unsigned;not null;default:0"   json:"version"`
	GameCount      uint       `gorm:"type:int unsigned;not null;default:0"   json:"game_count"`
	LastGameID     string     `gorm:"type:varchar(64);not null;default:''"   json:"last_game_id"`
	LastIteratedAt *time.Time `gorm:"type:datetime;default:null"             json:"last_iterated_at"`
	CreatedAt      time.Time  `gorm:"autoCreateTime"                         json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime"                         json:"updated_at"`
}

// TableName pins the SQL table name.
func (TLsmGameAgentMemory) TableName() string { return "t_lsm_game_agent_memory" }
