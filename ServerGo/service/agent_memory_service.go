// Package service — agent_memory_service.go: 狼人杀 Agent 持久化记忆的 DB 存取层。
//
// 2026-07-20 §131 新增。一模型一行(t_lsm_game_agent_memory),按 model_key 索引。
// 写路径用 version 乐观锁防多房间(重开局相邻触发)并发覆盖:
// UPDATE ... WHERE id=? AND version=?;影响 0 行则重读合并重试 1 次,再失败返回错误
// (调用方仅 log,绝不阻塞游戏流)。
// 详见 docs/狼人杀-Agent与系统/狼人杀Agent持久化记忆设计.md。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"LsmAgentGame/models"
	"LsmAgentGame/util"

	"gorm.io/gorm"
)

// AgentMemoryService 管理 t_lsm_game_agent_memory 的读写。
type AgentMemoryService struct {
	db *gorm.DB
}

// NewAgentMemoryService builds an AgentMemoryService.
func NewAgentMemoryService(db *gorm.DB) *AgentMemoryService {
	return &AgentMemoryService{db: db}
}

// Load 读取指定 model_key 的 memory_md;行不存在返回 ("", nil)。
func (s *AgentMemoryService) Load(ctx context.Context, modelKey string) (string, error) {
	row, err := s.LoadFull(ctx, modelKey)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", nil
	}
	return row.MemoryMD, nil
}

// LoadFull 读取完整记忆行;行不存在返回 (nil, nil)。
func (s *AgentMemoryService) LoadFull(ctx context.Context, modelKey string) (*models.TLsmGameAgentMemory, error) {
	if s.db == nil {
		return nil, errors.New("agent memory: db not wired")
	}
	var row models.TLsmGameAgentMemory
	err := s.db.WithContext(ctx).
		Where("model_key = ?", modelKey).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// SaveIterated 把一次自我迭代的结果写回。语义:
//   - 行不存在 → INSERT(version=1, game_count=1);
//   - 行存在 → UPDATE ... WHERE id=? AND version=oldVersion(version+1, game_count+1);
//   - 乐观锁冲突(影响 0 行)→ 重读最新行,以新 version 为基重试 1 次;
//   - 再失败返回错误(调用方仅 logger.Warn,不阻塞游戏流)。
func (s *AgentMemoryService) SaveIterated(ctx context.Context, modelKey, newMD, gameID string) error {
	if s.db == nil {
		return errors.New("agent memory: db not wired")
	}
	now := time.Now()
	row, err := s.LoadFull(ctx, modelKey)
	if err != nil {
		return fmt.Errorf("agent memory: load: %w", err)
	}
	if row == nil {
		row = &models.TLsmGameAgentMemory{
			ID:             util.NewUUID(),
			ModelKey:       modelKey,
			MemoryMD:       newMD,
			Version:        1,
			GameCount:      1,
			LastGameID:     gameID,
			LastIteratedAt: &now,
		}
		if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
			// 并发 INSERT 撞 unique(model_key) → 退化到 UPDATE 路径重试 1 次。
			if !isAgentMemoryDuplicateErr(err) {
				return fmt.Errorf("agent memory: insert: %w", err)
			}
			return s.saveIteratedUpdate(ctx, modelKey, newMD, gameID, now, true)
		}
		return nil
	}
	return s.saveIteratedUpdate(ctx, modelKey, newMD, gameID, now, false)
}

// saveIteratedUpdate 执行乐观锁 UPDATE;retry 为 true 表示已是重试(INSERT 撞
// unique 或上一轮 UPDATE 冲突),再次冲突直接返回错误不再递归。
func (s *AgentMemoryService) saveIteratedUpdate(ctx context.Context, modelKey, newMD, gameID string, now time.Time, retry bool) error {
	row, err := s.LoadFull(ctx, modelKey)
	if err != nil {
		return fmt.Errorf("agent memory: reload: %w", err)
	}
	if row == nil {
		// 竞态:行被删除。直接按 INSERT 处理(极小概率,失败仅 log)。
		return fmt.Errorf("agent memory: row vanished for %q", modelKey)
	}
	res := s.db.WithContext(ctx).
		Model(&models.TLsmGameAgentMemory{}).
		Where("id = ? AND version = ?", row.ID, row.Version).
		Updates(map[string]any{
			"memory_md":        newMD,
			"version":          row.Version + 1,
			"game_count":       row.GameCount + 1,
			"last_game_id":     gameID,
			"last_iterated_at": &now,
		})
	if res.Error != nil {
		return fmt.Errorf("agent memory: update: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		if retry {
			return fmt.Errorf("agent memory: version conflict after retry for %q", modelKey)
		}
		// 乐观锁冲突 → 重读合并重试 1 次(以最新 version 为基)。
		return s.saveIteratedUpdate(ctx, modelKey, newMD, gameID, now, true)
	}
	return nil
}

// Clear 清空指定模型的记忆(管理接口用):memory_md="",version+1。
// game_count 保留不动(历史审计链路)。行不存在时为 no-op(返回 nil)。
func (s *AgentMemoryService) Clear(ctx context.Context, modelKey string) error {
	if s.db == nil {
		return errors.New("agent memory: db not wired")
	}
	row, err := s.LoadFull(ctx, modelKey)
	if err != nil {
		return fmt.Errorf("agent memory: load: %w", err)
	}
	if row == nil {
		return nil
	}
	res := s.db.WithContext(ctx).
		Model(&models.TLsmGameAgentMemory{}).
		Where("id = ? AND version = ?", row.ID, row.Version).
		Updates(map[string]any{
			"memory_md": "",
			"version":   row.Version + 1,
		})
	if res.Error != nil {
		return fmt.Errorf("agent memory: clear: %w", res.Error)
	}
	return nil
}

// isAgentMemoryDuplicateErr 匹配 MySQL/MariaDB Error 1062(duplicate key)。
// 并发 INSERT 撞 unique(model_key) 时调用方退化到 UPDATE 路径。
func isAgentMemoryDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Error 1062") || strings.Contains(msg, "Duplicate entry")
}
