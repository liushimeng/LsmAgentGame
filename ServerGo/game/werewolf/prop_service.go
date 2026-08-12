// Package werewolf — prop_service.go: 道具目录加载 + 使用编排服务。
//
// 本文件在 werewolf 包中实现（避免 service ↔ werewolf 循环导入）。
// 职责：
//  1. 从 DB 加载道具目录（空表时 seed from code 内嵌默认值）。
//  2. 提供高层编排接口。
//  3. 提供管理接口（列出 / 更新道具配置）。
//
// 2026-07-21 道具系统设计（docs/狼人杀-道具与经济/狼人杀13人局道具系统设计.md）。
package werewolf

import (
	"context"
	"errors"
	"time"

	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/service"
	"LsmWebGame/util"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PropService 是道具系统服务（在 werewolf 包中实现）。
type PropService struct {
	db        *gorm.DB
	walletSvc *service.WalletService
}

// NewPropService 构造道具服务。
func NewPropService(db *gorm.DB, walletSvc *service.WalletService) *PropService {
	return &PropService{
		db:        db,
		walletSvc: walletSvc,
	}
}

// LoadCatalog 从 DB 加载道具目录。空表时 seed from code 内嵌默认值。
func (s *PropService) LoadCatalog(ctx context.Context) (*PropCatalog, error) {
	if s.db == nil {
		return BuildDefaultPropCatalog(), nil
	}
	var rows []models.TLsmGameProp
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		logger.L().Error("prop catalog load failed", zap.Error(err))
		return BuildDefaultPropCatalog(), nil
	}
	if len(rows) == 0 {
		// 空表 → seed from code
		_ = s.seedDefaultProps(ctx)
		_ = s.db.WithContext(ctx).Find(&rows).Error
	}
	return BuildPropCatalogFromModels(rows), nil
}

// seedDefaultProps 把代码内嵌的默认道具 upsert 到 DB（按 prop_key 唯一）。
// 使用 OnConflict Assign 更新全部字段（含 v2 新增的 inject_gen_key / effect_type /
// twist_seat_src），保证已有的 v1.1 行在迁移后也能拿到最新的效果配置。
func (s *PropService) seedDefaultProps(ctx context.Context) error {
	for _, entry := range GetDefaultProps() {
		m := entry.ToModel()
		m.UpdatedAt = time.Now()
		err := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "prop_key"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"name_zh", "name_en", "name_ja", "description",
					"price", "base_hit_rate", "is_aoe", "target_camp",
					"enabled", "max_per_game", "cooldown_sec",
					"inject_gen_key", "effect_type", "twist_seat_src",
					"updated_at",
				}),
			}).
			Create(&m).Error
		if err != nil {
			logger.L().Warn("prop seed upsert failed",
				zap.String("prop_key", m.PropKey), zap.Error(err))
		}
	}
	return nil
}

// ListEnabledProps 返回已启用的道具列表（用于前端展示）。
func (s *PropService) ListEnabledProps(ctx context.Context) ([]models.TLsmGameProp, error) {
	catalog, err := s.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	entries := catalog.ListEnabled()
	out := make([]models.TLsmGameProp, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ToModel())
	}
	return out, nil
}

// GetProp 按 key 查找单个道具（含 disabled）。
func (s *PropService) GetProp(ctx context.Context, propKey string) (*PropCatalogEntry, error) {
	catalog, err := s.LoadCatalog(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := catalog.Get(propKey)
	if !ok {
		return nil, errors.New("prop not found")
	}
	return entry, nil
}

// CreateProp 创建新道具（admin only）。
func (s *PropService) CreateProp(ctx context.Context, m models.TLsmGameProp) error {
	m.ID = util.NewUUID()
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Create(&m).Error
}

// UpdateProp 更新道具配置（admin only）。
func (s *PropService) UpdateProp(ctx context.Context, propKey string, updates map[string]interface{}) error {
	updates["updated_at"] = time.Now()
	return s.db.WithContext(ctx).
		Model(&models.TLsmGameProp{}).
		Where("prop_key = ?", propKey).
		Updates(updates).Error
}

// ListPropUsage 列出道具使用日志(分页 + 可选 prop_key 过滤)。
// admin only。Append-only 表,只读不写。
// 返回 (logs, total, error):logs 按 used_at DESC 排序,total 不分页的真实总数。
// 2026-07-21 §道具系统:支撑 GET /api/admin/werewolf/props/usage。
func (s *PropService) ListPropUsage(ctx context.Context, propKey string, limit, offset int) ([]models.TLsmGamePropUsageLog, int64, error) {
	if s.db == nil {
		return nil, 0, errors.New("prop service db not wired")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := s.db.WithContext(ctx).Model(&models.TLsmGamePropUsageLog{})
	if propKey != "" {
		q = q.Where("prop_key = ?", propKey)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.TLsmGamePropUsageLog
	if err := q.Order("used_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
