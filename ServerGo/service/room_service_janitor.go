package service

import (
	"context"
	"time"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type JanitorStats struct {
	Scanned  int
	Deleted  int
	Skipped  int
	Duration time.Duration
}

func (s *RoomService) UpdateRoomStatus(roomID, status string) *errcode.Error {
	if err := s.db.Model(&models.TLsmGameRoom{}).
		Where("id = ?", roomID).
		Update("status", status).Error; err != nil {
		logger.L().Error("update room status",
			zap.String("room_id", roomID),
			zap.String("status", status),
			zap.Error(err))
		return errcode.Code(errcode.ErrDB)
	}
	logger.L().Debug("room status updated",
		zap.String("room_id", roomID),
		zap.String("status", status))
	return nil
}

func (s *RoomService) JanitorSweep(ctx context.Context, olderThan time.Duration) JanitorStats {
	start := time.Now()
	cutoff := start.Add(-olderThan)
	stats := JanitorStats{}

	var candidates []models.TLsmGameRoom
	if err := s.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", "open", cutoff).
		Order("created_at ASC").
		Limit(500). // safety cap; janitor is allowed to take time but not all at once
		Find(&candidates).Error; err != nil {
		logger.L().Warn("janitor scan failed", zap.Error(err))
		stats.Duration = time.Since(start)
		return stats
	}
	stats.Scanned = len(candidates)

	for _, r := range candidates {
		select {
		case <-ctx.Done():
			stats.Duration = time.Since(start)
			return stats
		default:
		}
		var remaining int64
		if err := s.db.WithContext(ctx).
			Model(&models.TLsmGamePlayer{}).
			Where("room_id = ?", r.ID).
			Count(&remaining).Error; err != nil {
			logger.L().Warn("janitor player count failed",
				zap.String("room_id", r.ID), zap.Error(err))
			stats.Skipped++
			continue
		}
		if remaining > 0 {
			stats.Skipped++
			continue
		}
		deleted, derr := s.DeleteRoomIfEmpty(r.ID)
		if derr != nil {
			logger.L().Warn("janitor delete failed",
				zap.String("room_id", r.ID), zap.Error(derr))
			stats.Skipped++
			continue
		}
		if deleted {
			stats.Deleted++
		} else {
			stats.Skipped++
		}
	}
	stats.Duration = time.Since(start)
	logger.L().Info("janitor sweep finished",
		zap.Int("scanned", stats.Scanned),
		zap.Int("deleted", stats.Deleted),
		zap.Int("skipped", stats.Skipped),
		zap.Duration("duration", stats.Duration))
	return stats
}

func (s *RoomService) RunJanitor(interval, olderThan, staleMaxAge, zombieMaxAge time.Duration, stopCh <-chan struct{}) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if olderThan <= 0 {
		olderThan = 30 * time.Minute
	}
	if staleMaxAge <= 0 {
		staleMaxAge = 30 * time.Minute
	}
	logger.L().Info("room janitor started",
		zap.Duration("interval", interval),
		zap.Duration("older_than", olderThan),
		zap.Duration("stale_max_age", staleMaxAge),
		zap.Duration("zombie_max_age", zombieMaxAge))

	// Run an immediate sweep on boot — fixes the "X status=open empty rooms
	// from prior test runs" scenario. Stale sweep first so any backlog from
	// the previous run (process crash, kill -9) is cleared before the
	// regular empty-room sweep runs.
	runAllSweeps := func() {
		s.JanitorSweepStale(context.Background(), staleMaxAge)
		s.JanitorSweep(context.Background(), olderThan)
		if zombieMaxAge > 0 {
			s.JanitorSweepZombiePlaying(context.Background(), zombieMaxAge)
		}
		// R187-1: 回收卡在 filling 阶段的狼人杀房间(默认 5 分钟,
		// 早于 30 分钟的 JanitorSweepStale)。阈值读 cfg.Werewolf.FillingReaperSec;
		// cfg 缺失 / 0 时 JanitorSweepStaleFilling 内部兜底 5 分钟。
		fillingMaxAge := 5 * time.Minute
		if s.cfg != nil && s.cfg.Werewolf.FillingReaperSec > 0 {
			fillingMaxAge = time.Duration(s.cfg.Werewolf.FillingReaperSec) * time.Second
		}
		s.JanitorSweepStaleFilling(context.Background(), fillingMaxAge)
	}
	runAllSweeps()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			logger.L().Info("room janitor stopped")
			return
		case <-ticker.C:
			runAllSweeps()
		}
	}
}

func (s *RoomService) JanitorSweepStale(ctx context.Context, maxAge time.Duration) JanitorStats {
	start := time.Now()
	cutoff := start.Add(-maxAge)
	stats := JanitorStats{}

	var candidates []models.TLsmGameRoom
	if err := s.db.WithContext(ctx).
		Where("status = ? AND created_at < ?", "open", cutoff).
		Order("created_at ASC").
		Limit(500). // safety cap
		Find(&candidates).Error; err != nil {
		logger.L().Warn("stale janitor scan failed", zap.Error(err))
		stats.Duration = time.Since(start)
		return stats
	}
	stats.Scanned = len(candidates)

	for _, r := range candidates {
		select {
		case <-ctx.Done():
			stats.Duration = time.Since(start)
			return stats
		default:
		}
		var pc int64
		if err := s.db.WithContext(ctx).
			Model(&models.TLsmGamePlayer{}).
			Where("room_id = ?", r.ID).Count(&pc).Error; err != nil {
			logger.L().Warn("stale janitor count failed",
				zap.String("room_id", r.ID), zap.Error(err))
			stats.Skipped++
			continue
		}
		// Consistency guard: only force-delete when the cached current_count
		// matches the actual player-row count. Drift is excluded (left for
		// JanitorSweep's empty-room pass once the room really empties).
		if pc != int64(r.CurrentCount) {
			stats.Skipped++
			continue
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("room_id = ?", r.ID).
				Delete(&models.TLsmGamePlayer{}).Error; err != nil {
				return err
			}
			// Cascade: drop the stale room's chat messages.
			if err := tx.Where("scope = ? AND room_id = ?", "room", r.ID).
				Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
				return err
			}
			return tx.Where("id = ?", r.ID).Delete(&models.TLsmGameRoom{}).Error
		}); err != nil {
			logger.L().Warn("stale janitor delete failed",
				zap.String("room_id", r.ID), zap.Error(err))
			stats.Skipped++
			continue
		}
		stats.Deleted++
		logger.L().Info("stale room force-deleted",
			zap.String("room_id", r.ID),
			zap.String("game_kind", r.GameKind),
			zap.Int("current_count", r.CurrentCount),
			zap.Int("age_min", int(time.Since(r.CreatedAt).Minutes())))
	}
	stats.Duration = time.Since(start)
	logger.L().Info("stale janitor sweep finished",
		zap.Int("scanned", stats.Scanned),
		zap.Int("deleted", stats.Deleted),
		zap.Int("skipped", stats.Skipped),
		zap.Duration("duration", stats.Duration))
	return stats
}

// zombieRule* 是 JanitorSweepZombiePlaying 的回收规则名。每条被翻成
// `over` 的房间都携带一条明确规则,既进日志也是回归测试的断言对象。
const (
	zombieRuleNone = "" // 不回收
	// zombieRuleMaxDuration:房间自创建起持续 status='playing' 超过
	// zombieMaxAge —— 「一局对局的绝对时长上限」,与 updated_at 无关。
	zombieRuleMaxDuration = "max_game_duration"
	// zombieRuleAbandoned:werewolf 纯 bot 房 + hub 上零人类 + 已超过
	// 快速回收窗口 —— 「没人在看的机器人局」提前回收。
	zombieRuleAbandoned = "abandoned_bot_room"
)

const (
	// zombieAbandonedAgeDivisor:无人观战快速回收窗口 = zombieMaxAge / 4
	// (默认 4h / 4 = 1h)。不引入第二个 magic number,随调用方参数缩放。
	zombieAbandonedAgeDivisor = 4
	// zombieAbandonedMinAge:快速回收窗口的下限。防止调用方传入很小的
	// zombieMaxAge 时把窗口压到几分钟,误杀刚开局的慢模型 bot 房。
	zombieAbandonedMinAge = 15 * time.Minute
	// zombieSweepScanLimit:单轮扫描上限。playing 房间数量本就很少,
	// 这个 cap 只是防御性的(与 JanitorSweep / JanitorSweepStale 一致)。
	zombieSweepScanLimit = 500
)

// zombieExceedsMaxDuration 判定「房间自创建起持续 playing 是否已超过上限」。
//
// BUG-R219 修复要点(2026-08-01):判据是 **created_at** 而不是 updated_at。
// `updated_at` 是 `gorm:"autoUpdateTime"` 的**行写入时间戳**,不是**对局进度
// 信号** —— watchdog 每跳过一轮就会 touch 它,卡死房间因此永远刷不出阈值
// (实测房间 ce288893 在大厅存活 15h+)。`created_at` 是 `autoCreateTime`,
// 建行后再不改写,是当前 schema 下唯一免费、单调、不被后台活动污染的
// 「这局跑了多久」信号。
//
// 覆盖面证明:GORM 建行时同时写 created_at 与 updated_at,故恒有
// updated_at >= created_at,于是 `updated_at < cutoff` ⟹ `created_at < cutoff`。
// 也就是说本规则是旧 updated_at 规则的**严格超集** —— 旧逻辑能扫到的行,
// 新逻辑一定能扫到,不存在回收面积缩小的风险,旧规则因此被删除而非并存
// (§130:不留死代码)。
func zombieExceedsMaxDuration(createdAt, now time.Time, zombieMaxAge time.Duration) bool {
	if zombieMaxAge <= 0 || createdAt.IsZero() {
		return false
	}
	return createdAt.Before(now.Add(-zombieMaxAge))
}

// zombieAbandonedAge 计算「无人观战快速回收」窗口(zombieMaxAge / 4,
// 不低于 zombieAbandonedMinAge,且不超过 zombieMaxAge 本身)。
func zombieAbandonedAge(zombieMaxAge time.Duration) time.Duration {
	if zombieMaxAge <= 0 {
		return 0
	}
	d := zombieMaxAge / zombieAbandonedAgeDivisor
	if d < zombieAbandonedMinAge {
		d = zombieAbandonedMinAge
	}
	if d > zombieMaxAge {
		d = zombieMaxAge
	}
	return d
}

// zombieAbandonedCandidate 是 zombieRuleAbandoned 的**纯年龄/游戏类型前置
// 条件**(不含 hub 探测与 DB 计数,便于单测)。
//
// 扫 werewolf + texasholem(2026-08-20 §20260819-02 扩展):其它 3 款游戏
//(xiangqi/chess/junqi/doudizhu)以分钟计自然收敛,不会留下 playing 僵尸。
// 狼人杀和德州扑克的纯 bot 房间在 hub vacancy timer 之外仍可能成为永久
// playing 僵尸(玩家 / 观察者长期不在线 + 没有 vacancy 触发条件),需要
// janitor 兜底。
//
// 可达性:主路径已把 created_at < now-zombieMaxAge 的行翻成 over,本规则
// 的窗口是 now-zombieMaxAge .. now-zombieAbandonedAge(默认 4h..1h),是一段
// 主路径覆盖不到的**真实区间** —— cea6126 的旧二次扫描用的是与主路径
// **完全相同**的谓词,因此永远匹配 0 行(死代码),这里是它的修正版。
func zombieAbandonedCandidate(gameKind string, createdAt, now time.Time, zombieMaxAge time.Duration) bool {
	if (gameKind != "werewolf" && gameKind != "texasholdem") || createdAt.IsZero() {
		return false
	}
	age := zombieAbandonedAge(zombieMaxAge)
	if age <= 0 {
		return false
	}
	return createdAt.Before(now.Add(-age))
}

// zombieRuleFor 是**单行判定的唯一事实来源** —— JanitorSweepZombiePlaying
// 每扫到一行就调它一次,回归测试也直接驱动它(而不是复刻一份 switch,
// 避免 room_orphan_cleanup_test.go::orphanRouter 那种"测试副本会漂移"的
// 隐患)。返回 zombieRule* 之一,zombieRuleNone 表示放过。
//
// hubEmpty / humanRows 由调用方现场探测后传入,便于测试注入:
//   - hubEmpty  : s.IsRoomEmpty(room.ID) 的结果(hub 上零连接为 true)
//   - humanRows : t_lsm_game_player 中 role <> 'agent' 的行数;探测失败时
//     调用方传 -1,本函数按"可能有人类"保守放过。
func zombieRuleFor(room models.TLsmGameRoom, now time.Time, zombieMaxAge time.Duration,
	hubEmpty bool, humanRows int64) string {
	if room.Status != "playing" {
		return zombieRuleNone
	}
	// 规则 1:绝对时长上限。刻意**不看** hub、**不看** updated_at ——
	// 一个观众挂在死了 15h 的房间里不该让它永远不可回收(BUG-R219)。
	if zombieExceedsMaxDuration(room.CreatedAt, now, zombieMaxAge) {
		return zombieRuleMaxDuration
	}
	// 规则 2:无人观战的纯 bot 狼人杀局,提前到 zombieMaxAge/4 回收。
	if zombieAbandonedCandidate(room.GameKind, room.CreatedAt, now, zombieMaxAge) {
		if !hubEmpty {
			return zombieRuleNone // hub 上有人在看
		}
		if humanRows != 0 {
			// >0 = 有真人 / 观察者行(可能掉线重连中);
			// <0 = 探测失败,保守放过,等下一轮。
			return zombieRuleNone
		}
		return zombieRuleAbandoned
	}
	return zombieRuleNone
}

// JanitorSweepZombiePlaying 把长期卡在 status='playing' 的房间翻成 'over',
// 让它们从大厅列表里消失。两条规则(见 zombieRule* 常量):
//
//  1. max_game_duration —— created_at 超过 zombieMaxAge。**无条件**回收,
//     不看 hub、不看 updated_at。这条是 BUG-R219 的正解:一个观众挂在
//     死了 15 小时的房间里,不该让它永远不可回收。
//  2. abandoned_bot_room —— werewolf + created_at 超过 zombieMaxAge/4 +
//     hub 上零连接 + DB 里零非 agent 玩家行。即「没有任何人类在看的纯
//     机器人局」,提前回收。
//
// 关于 IsRoomEmpty 的定位(报告 20260801_083235 指出的坑):bot 座位不持有
// hub 连接,所以 IsRoomEmpty 对纯 bot 房恒为 true(假"空");反过来一个观众
// 就能让死房间恒为 false(假"占用")。因此本函数**只把它用作规则 2 的保守
// 收紧条件**(误判方向只会导致跳过,安全),假"占用"方向则由**完全不看
// hub** 的规则 1 兜底。规则 2 另外要求"零非 agent 玩家行",避免真人掉线
// 重连窗口内被翻表。
//
// 注意:本 sweep 只改 DB 的 status 字段(大厅可见性),**不**摘除 in-memory
// GameState、不广播 game.removed —— 那是 ForceDisbandRoom / 各 Boot* 清理
// 路径的职责。因此规则 1 即使误伤一个连续对战 4h+ 的活跃房间,后果也仅是
// 它从大厅列表消失,房内玩家不受影响。
func (s *RoomService) JanitorSweepZombiePlaying(ctx context.Context, zombieMaxAge time.Duration) JanitorStats {
	start := time.Now()
	stats := JanitorStats{}
	if s.db == nil || zombieMaxAge <= 0 {
		stats.Duration = time.Since(start)
		return stats
	}

	// 一次性拉出所有 playing 房间,再在 Go 侧按规则逐行判定 —— 判定逻辑
	// 因此可以是纯函数(可单测),同时日志能落到"哪条规则命中"的粒度。
	// playing 房间数量本就是个位数到几十,不存在扫描压力。
	var candidates []models.TLsmGameRoom
	if err := s.db.WithContext(ctx).
		Where("status = ?", "playing").
		Order("created_at ASC").
		Limit(zombieSweepScanLimit).
		Find(&candidates).Error; err != nil {
		logger.L().Warn("zombie playing janitor scan failed", zap.Error(err))
		stats.Duration = time.Since(start)
		return stats
	}
	stats.Scanned = len(candidates)

	now := start
	for i := range candidates {
		select {
		case <-ctx.Done():
			stats.Duration = time.Since(start)
			return stats
		default:
		}
		r := candidates[i]

		// 规则 2 才需要的两项现场探测。规则 1(绝对时长)完全不看它们,
		// 所以只在候选命中时才付出 hub 探测 + COUNT 的代价。
		hubEmpty, humanRows := true, int64(0)
		if !zombieExceedsMaxDuration(r.CreatedAt, now, zombieMaxAge) &&
			zombieAbandonedCandidate(r.GameKind, r.CreatedAt, now, zombieMaxAge) {
			// hubHook 未接线时 IsRoomEmpty 返回 true(视作无人),与
			// JanitorSweepStaleFilling / BootCleanupOrphanedAgentRooms 一致。
			hubEmpty = s.IsRoomEmpty(r.ID)
			if err := s.db.WithContext(ctx).
				Model(&models.TLsmGamePlayer{}).
				Where("room_id = ? AND role <> ?", r.ID, models.PlayerRoleAgent).
				Count(&humanRows).Error; err != nil {
				logger.L().Warn("zombie janitor human-row count failed",
					zap.String("room_id", r.ID), zap.Error(err))
				humanRows = -1 // 探测失败 → zombieRuleFor 保守放过
			}
		}

		rule := zombieRuleFor(r, now, zombieMaxAge, hubEmpty, humanRows)
		if rule == zombieRuleNone {
			stats.Skipped++
			continue
		}

		// `AND status = 'playing'` 复核:扫描与更新之间房间可能已自然结束。
		res := s.db.WithContext(ctx).
			Model(&models.TLsmGameRoom{}).
			Where("id = ? AND status = ?", r.ID, "playing").
			Update("status", "over")
		if res.Error != nil {
			logger.L().Warn("zombie janitor mark-over failed",
				zap.String("room_id", r.ID), zap.Error(res.Error))
			stats.Skipped++
			continue
		}
		if res.RowsAffected == 0 {
			stats.Skipped++
			continue
		}
		stats.Deleted++ // "removed" from the lobby view
		logger.L().Info("zombie playing room marked over",
			zap.String("room_id", r.ID),
			zap.String("game_kind", r.GameKind),
			zap.String("rule", rule),
			zap.Duration("age", now.Sub(r.CreatedAt)),
			zap.Duration("idle", now.Sub(r.UpdatedAt)))
	}

	stats.Duration = time.Since(start)
	if stats.Scanned > 0 {
		logger.L().Info("zombie playing janitor sweep finished",
			zap.Int("scanned", stats.Scanned),
			zap.Int("marked_over", stats.Deleted),
			zap.Int("skipped", stats.Skipped),
			zap.Duration("max_age", zombieMaxAge),
			zap.Duration("abandoned_age", zombieAbandonedAge(zombieMaxAge)),
			zap.Duration("duration", stats.Duration))
	}
	return stats
}


