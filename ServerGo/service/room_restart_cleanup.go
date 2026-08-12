package service

import (
	"context"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// RestartCleanupStats is the snapshot returned by BootCleanupStaleWerewolfRooms.
//
// On a process restart the in-memory werewolf state is wiped automatically
// because no rooms exist in the fresh WerewolfManager.rooms map. But the DB
// rows for those rooms (t_lsm_game_room + t_lsm_game_player) are still there.
// Without a boot-time pass they hang around until the regular janitor picks
// them up after 30 minutes (JanitorSweepStale) or 4 hours
// (JanitorSweepZombiePlaying).
//
// BootCleanupStaleWerewolfRooms shortens that window by force-disbanding
// every werewolf room found in the DB. The rationale is:
//
//   - All in-memory state is gone (process restarted).
//   - Every connected player will get a WS disconnect when the previous
//     process exits; whoever reconnects finds an empty lobby because every
//     room is gone.
//   - Any half-running agent goroutine from the previous process died with
//     the kernel — we don't try to revive them.
//   - Holding the rows for 30 minutes would just leak player names through
//     the lobby list and the room detail endpoint, polluting telemetry.
//
// We deliberately skip the human-presence guard that
// BootCleanupOrphanedAgentRooms uses. That other helper only fires when
// the in-memory state is gone AND no humans were connected at restart
// time — but on a fresh process the hub has zero connections period, so
// "no humans connected" is always true. Forcing the disband over every
// werewolf room is the simplest policy that keeps the service clean.
type RestartCleanupStats struct {
	Scanned     int
	Disbanded   int
	HardDeleted int
	Skipped     int
}

// BootCleanupStaleWerewolfRooms scans every werewolf room in the DB and
// force-disbands it via RoomService.ForceDisbandRoom, which:
//
//   - tears down any residual in-memory GameState (no-op post-restart),
//   - fans a `game.removed` envelope out to still-connected clients,
//   - deletes player rows + chat history + the room row atomically.
//
// Safe to call when DB is nil (returns zero stats, no-op). Cancel-safe via
// ctx. Designed for the boot-time path in main.go; NOT idempotent against
// itself while running (the scan will pick up its own freshly deleted
// rows, but ForceDisbandRoom returns ErrRoomNotFound for those, which we
// count as a soft success).
func (s *RoomService) BootCleanupStaleWerewolfRooms(ctx context.Context) RestartCleanupStats {
	stats := RestartCleanupStats{}
	if s.db == nil {
		return stats
	}

	var roomIDs []string
	if err := s.db.WithContext(ctx).
		Table("t_lsm_game_room").
		Where("game_kind = ?", "werewolf").
		Pluck("id", &roomIDs).Error; err != nil {
		logger.L().Warn("boot restart cleanup: scan failed", zap.Error(err))
		return stats
	}
	stats.Scanned = len(roomIDs)
	if stats.Scanned == 0 {
		logger.L().Info("boot restart cleanup: no werewolf rooms to clean")
		return stats
	}

	logger.L().Info("boot restart cleanup: force-disbanding stale werewolf rooms",
		zap.Int("count", stats.Scanned))

	for _, rid := range roomIDs {
		select {
		case <-ctx.Done():
			logger.L().Info("boot restart cleanup cancelled",
				zap.Int("remaining", len(roomIDs)-stats.Disbanded-stats.HardDeleted-stats.Skipped))
			return stats
		default:
		}
		if rid == "" {
			continue
		}
		_, derr := s.ForceDisbandRoom(ctx, rid, "system-boot-restart",
			"server restart — werewolf rooms do not survive process boundary; rejoin lobby")
		if derr == nil {
			stats.Disbanded++
			continue
		}
		// ErrRoomNotFound = row vanished between our scan and the disband —
		// treat as soft success (we wanted it gone anyway).
		if ce, ok := derr.(*errcode.Error); ok && ce.Code == errcode.ErrRoomNotFound {
			stats.HardDeleted++
			continue
		}
		logger.L().Warn("boot restart cleanup: force-disband failed",
			zap.String("room_id", rid), zap.Error(derr))
		stats.Skipped++
	}

	logger.L().Warn("boot restart cleanup finished",
		zap.Int("scanned", stats.Scanned),
		zap.Int("disbanded", stats.Disbanded),
		zap.Int("hard_deleted", stats.HardDeleted),
		zap.Int("skipped", stats.Skipped))
	return stats
}
