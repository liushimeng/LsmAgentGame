// Package service — agent_player_profile_service.go: 狼人杀 Agent 玩家行为画像的 DB 存取层。
//
// 2026-08-11 §20260811-05 U1 新增。一 (model_key × user_id) 一行
// (t_lsm_game_agent_player_profile)。写路径用 version 乐观锁防多房间
// (重开局相邻触发)并发覆盖,与 §131 AgentMemoryService 同一模式:
// UPDATE ... WHERE id=? AND version=?;影响 0 行则重读重试 1 次,再失败返回错误
// (调用方仅 log,绝不阻塞游戏流)。
// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-05.md §U1。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"LsmWebGame/models"
	"LsmWebGame/util"

	"gorm.io/gorm"
)

// AgentPlayerProfileService 管理 t_lsm_game_agent_player_profile 的读写。
type AgentPlayerProfileService struct {
	db *gorm.DB
}

// NewAgentPlayerProfileService builds an AgentPlayerProfileService.
func NewAgentPlayerProfileService(db *gorm.DB) *AgentPlayerProfileService {
	return &AgentPlayerProfileService{db: db}
}

// LoadProfile 读取指定 (model_key, user_id) 的画像;行不存在返回 (nil, nil)。
func (s *AgentPlayerProfileService) LoadProfile(ctx context.Context, modelKey, userID string) (*models.TLsmGameAgentPlayerProfile, error) {
	if s.db == nil {
		return nil, errors.New("agent player profile: db not wired")
	}
	var row models.TLsmGameAgentPlayerProfile
	err := s.db.WithContext(ctx).
		Where("model_key = ? AND user_id = ?", modelKey, userID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// LoadProfilesForUsers 批量读取某模型对一组人类玩家的画像(房间级预取用)。
// 返回 user_id → profile 行的 map;无画像的 user_id 不出现在 map 中。
func (s *AgentPlayerProfileService) LoadProfilesForUsers(ctx context.Context, modelKey string, userIDs []string) (map[string]*models.TLsmGameAgentPlayerProfile, error) {
	out := make(map[string]*models.TLsmGameAgentPlayerProfile, len(userIDs))
	if s.db == nil {
		return nil, errors.New("agent player profile: db not wired")
	}
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []models.TLsmGameAgentPlayerProfile
	err := s.db.WithContext(ctx).
		Where("model_key = ? AND user_id IN ?", modelKey, userIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		r := rows[i]
		out[r.UserID] = &r
	}
	return out, nil
}

// SaveIterated 把一次赛后画像迭代的结果写回。语义:
//   - 行不存在 → INSERT(version=1, games_together=1, wins_together=win?1:0);
//   - 行存在 → UPDATE ... WHERE id=? AND version=oldVersion
//     (version+1, games_together+1, wins_together+win);
//   - 乐观锁冲突 → 重读以最新 version 为基重试 1 次;再失败返回错误。
//
// profileMD 为空串时保留旧画像(仅更新计数器)——LLM 调用失败/输出不合格时
// 调用方传 "" 走计数器-only 更新,不丢旧画像。
func (s *AgentPlayerProfileService) SaveIterated(ctx context.Context, modelKey, userID, profileMD string, sameCampWin bool) error {
	if s.db == nil {
		return errors.New("agent player profile: db not wired")
	}
	now := time.Now()
	row, err := s.LoadProfile(ctx, modelKey, userID)
	if err != nil {
		return fmt.Errorf("agent player profile: load: %w", err)
	}
	if row == nil {
		wins := uint(0)
		if sameCampWin {
			wins = 1
		}
		row = &models.TLsmGameAgentPlayerProfile{
			ID:            util.NewUUID(),
			ModelKey:      modelKey,
			UserID:        userID,
			ProfileMD:     profileMD,
			GamesTogether: 1,
			WinsTogether:  wins,
			Version:       1,
			LastSeenAt:    &now,
		}
		if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
			// 并发 INSERT 撞 unique(model_key,user_id) → 退化到 UPDATE 重试 1 次。
			if !isPlayerProfileDuplicateErr(err) {
				return fmt.Errorf("agent player profile: insert: %w", err)
			}
			return s.saveIteratedUpdate(ctx, modelKey, userID, profileMD, sameCampWin, now, true)
		}
		return nil
	}
	return s.saveIteratedUpdate(ctx, modelKey, userID, profileMD, sameCampWin, now, false)
}

// saveIteratedUpdate 执行乐观锁 UPDATE;retry 为 true 表示已是重试,再次冲突直接报错。
func (s *AgentPlayerProfileService) saveIteratedUpdate(ctx context.Context, modelKey, userID, profileMD string, sameCampWin bool, now time.Time, retry bool) error {
	row, err := s.LoadProfile(ctx, modelKey, userID)
	if err != nil {
		return fmt.Errorf("agent player profile: reload: %w", err)
	}
	if row == nil {
		return fmt.Errorf("agent player profile: row vanished for %q/%q", modelKey, userID)
	}
	newMD := row.ProfileMD
	if profileMD != "" {
		newMD = profileMD
	}
	wins := row.WinsTogether
	if sameCampWin {
		wins++
	}
	res := s.db.WithContext(ctx).
		Model(&models.TLsmGameAgentPlayerProfile{}).
		Where("id = ? AND version = ?", row.ID, row.Version).
		Updates(map[string]any{
			"profile_md":     newMD,
			"version":        row.Version + 1,
			"games_together": row.GamesTogether + 1,
			"wins_together":  wins,
			"last_seen_at":   &now,
		})
	if res.Error != nil {
		return fmt.Errorf("agent player profile: update: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		if retry {
			return fmt.Errorf("agent player profile: version conflict after retry for %q/%q", modelKey, userID)
		}
		return s.saveIteratedUpdate(ctx, modelKey, userID, profileMD, sameCampWin, now, true)
	}
	return nil
}

// ClearForUser 清空指定玩家在所有模型下的画像(隐私合规:用户一键清除)。
// 返回受影响行数。软策略:profile_md 置空 + version+1,保留计数器行
// (与 §131 Clear 同款"保留历史审计链路"语义)。
func (s *AgentPlayerProfileService) ClearForUser(ctx context.Context, userID string) (int64, error) {
	if s.db == nil {
		return 0, errors.New("agent player profile: db not wired")
	}
	res := s.db.WithContext(ctx).
		Model(&models.TLsmGameAgentPlayerProfile{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"profile_md": "",
			"version":    gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return 0, fmt.Errorf("agent player profile: clear user: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ClearForModelUser 清空指定 (model_key, user_id) 画像(管理端点用)。
func (s *AgentPlayerProfileService) ClearForModelUser(ctx context.Context, modelKey, userID string) error {
	if s.db == nil {
		return errors.New("agent player profile: db not wired")
	}
	row, err := s.LoadProfile(ctx, modelKey, userID)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	res := s.db.WithContext(ctx).
		Model(&models.TLsmGameAgentPlayerProfile{}).
		Where("id = ? AND version = ?", row.ID, row.Version).
		Updates(map[string]any{
			"profile_md": "",
			"version":    row.Version + 1,
		})
	if res.Error != nil {
		return fmt.Errorf("agent player profile: clear: %w", res.Error)
	}
	return nil
}

// isPlayerProfileDuplicateErr 匹配 MySQL/MariaDB Error 1062(duplicate key)。
func isPlayerProfileDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Error 1062") || strings.Contains(msg, "Duplicate entry")
}
