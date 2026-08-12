package service

import (
	"context"
	"time"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// JanitorSweepStaleFilling 回收「卡在 filling 阶段」的狼人杀房间(R187-1,
// 2026-07-23)。
//
// 背景:狼人杀房间创建后停在 PhaseFilling 等人入座。若创建者离开 /
// 关闭浏览器且再也不回来(R187 场景:agent_seats 为空 + 仅 1 人类),
// 此前唯一兜底是 30 分钟周期的 JanitorSweepStale,大厅会长时间挂着
// 「1/13 filling」的死房间。本 sweep 把回收窗口提前到 maxAge(默认
// 5 分钟,配置项 werewolf.filling_reaper_sec)。
//
// 判定条件(全部满足才回收):
//   - in-memory WerewolfManager 快照显示房间处于 filling(State==nil 或
//     Phase==filling)—— phase 只在内存里,不走 DB 查询;
//   - in-memory 对象创建至今 ≥ maxAge(CreatedAt 零值的老对象按"不过期"
//     跳过,交给 30 分钟的 JanitorSweepStale);
//   - hub 上没有任何人类玩家 / 观察者连接(IsRoomEmpty)—— 安全硬约束:
//     一个还在等朋友上线的人类绝不能被踢。
//
// 回收路径:ForceCloseWerewolfFillingRoom(in-memory 摘除 +
// stopAgentsLocked)+ ForceDisbandRoom 的 DB 删除路径复用 —— 但这里
// 不直接调 ForceDisbandRoom,因为它会再次 RemoveRoomState(幂等但多余)
// 且 room 可能已不在 DB;改为:先 ForceCloseWerewolfFillingRoom,
// 再 HardDeleteRoom 删除 DB 行 + 级联 player / chat 行,
// 最后 BroadcastRoomRemoved 通知(空集合时为 no-op)。
//
// 挂在既有 RunJanitor ticker 上(10 分钟一轮,与 JanitorSweepStale 等
// 共享),不新增 per-room goroutine。
func (s *RoomService) JanitorSweepStaleFilling(ctx context.Context, maxAge time.Duration) JanitorStats {
	start := time.Now()
	stats := JanitorStats{}
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}
	if s.gameSvc == nil {
		stats.Duration = time.Since(start)
		return stats
	}

	snapshot := s.gameSvc.WerewolfFillingRoomSnapshot()
	stats.Scanned = len(snapshot)
	cutoff := start.Add(-maxAge)

	for _, info := range snapshot {
		select {
		case <-ctx.Done():
			stats.Duration = time.Since(start)
			return stats
		default:
		}
		// 老对象(createdAt 零值)无法判定年龄 —— 保守跳过,交给
		// JanitorSweepStale 的 30 分钟兜底。
		if info.CreatedAt.IsZero() {
			stats.Skipped++
			continue
		}
		if info.CreatedAt.After(cutoff) {
			stats.Skipped++
			continue
		}
		// 安全硬约束:hub 上有人类玩家 / 观察者在线 → 跳过。
		// hubHook 未接线时 IsRoomEmpty 返回 true(视作无人),与
		// BootCleanupOrphanedAgentRooms 的语义一致。
		if !s.IsRoomEmpty(info.RoomID) {
			stats.Skipped++
			continue
		}

		// 1) in-memory 摘除(复核 phase,开始中则放弃)。
		if !s.gameSvc.ForceCloseWerewolfFillingRoom(info.RoomID) {
			stats.Skipped++
			continue
		}
		// 2) DB 行 + 级联删除(房间行可能已不存在 —— HardDeleteRoom
		//    对"行已消失"容错,只记录错误)。
		if s.db != nil {
			if _, err := s.HardDeleteRoom(ctx, info.RoomID); err != nil {
				logger.L().Warn("filling reaper: DB delete failed",
					zap.String("room_id", info.RoomID), zap.Error(err))
				// DB 删除失败不回滚 in-memory 摘除 —— 下一轮 janitor /
				// JanitorSweepStale 会兜底 DB 侧。
			}
		}
		// 3) 广播 game.removed(无连接时 no-op)。
		s.gameSvc.BroadcastRoomRemoved(info.RoomID, "filling_reaper")
		stats.Deleted++
		logger.L().Info("filling-phase werewolf room reaped",
			zap.String("room_id", info.RoomID),
			zap.Int("occupied_seats", info.OccupiedSeats),
			zap.Int("spectators", info.Spectators),
			zap.Int("age_min", int(time.Since(info.CreatedAt).Minutes())))
	}
	stats.Duration = time.Since(start)
	if stats.Scanned > 0 {
		logger.L().Info("filling reaper sweep finished",
			zap.Int("scanned", stats.Scanned),
			zap.Int("deleted", stats.Deleted),
			zap.Int("skipped", stats.Skipped),
			zap.Duration("duration", stats.Duration))
	}
	return stats
}
