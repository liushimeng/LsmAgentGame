package service

import (
	"context"
	"time"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"

	"go.uber.org/zap"
)

// CleanupStats is the snapshot returned by BootCleanupOrphanedAgentRooms.
// Mirrors the JanitorStats shape so the boot logger can use the same
// field names, with two extra counters that only the boot path produces:
//
//   - HardDeleted : rooms that had no in-memory GameState and were
//                   physically DELETEd from DB (no WS broadcast needed).
//   - Disbanded   : rooms that had live in-memory state (status='playing'
//                   but the hub had no connected clients) and were
//                   force-disbanded via ForceDisbandRoom.
//
// Scanned counts every candidate row inspected; Skipped covers rows we
// declined to act on (status not handled, hub had live connections, etc).
type CleanupStats struct {
	Scanned     int
	Disbanded   int
	HardDeleted int
	Skipped     int
}

// OrphanRoomRow is the projection BootCleanupOrphanedAgentRooms uses to
// list candidate rooms. Kept package-local because no caller outside the
// service needs the raw tuple.
type OrphanRoomRow struct {
	ID         string
	GameKind   string
	Status     string
	UpdatedAt  time.Time
	HumanCount int
	AgentCount int
}

// orphanStalePlayingAge is how old a status='playing' werewolf room must
// be (last updated_at) before the boot pass considers it for force
// disband. Matches the hub's 5-minute vacancy timer so the two passes
// agree on "definitely abandoned": a room whose updated_at is newer than
// the cutoff probably had a reconnect / phase advance within the window.
const orphanStalePlayingAge = 5 * time.Minute

// BootCleanupOrphanedAgentRooms scans the DB for werewolf rooms that
// contain at least one role='agent' row and reconciles them against the
// hub's in-memory state. Designed to run ONCE on process start (see
// main.go's goroutine launcher); it is NOT idempotent against itself
// during normal operation — the regular JanitorSweep / JanitorSweepStale
// / JanitorSweepZombiePlaying paths handle the steady-state.
//
// Two distinct outcomes:
//
//   1. `status='open'` AND `human_count == 0` AND `agent_count > 0`
//      → there are no humans around and the room never started. These
//      rooms have NO in-memory GameState to clean up (CreateRoom + no
//      JoinGame ever fired ForceStartIfReady), so we can drop the DB
//      rows directly via HardDeleteRoom. This is the common case after
//      a bot-vs-bot test that crashed mid-creation.
//
//   2. `status='playing'` AND `human_count == 0` AND `agent_count > 0`
//      AND the hub reports IsRoomEmpty AND `now - updated_at > 5min`
//      → the agents were driving a game that the hub has since
//      forgotten (server restart, or every WS client disconnected long
//      ago and the agent goroutines have all panicked). ForceDisbandRoom
//      tears down any residual GameState (no-op when already gone) and
//      drops the rows. Crucially this also fires BroadcastRoomRemoved
//      which is a no-op when nobody is connected.
//
// Everything else is Skipped — the regular janitor / live users will
// sort it out. We do NOT disband rooms with live humans present; even
// if the room looks stuck, a human may be trying to reconnect.
func (s *RoomService) BootCleanupOrphanedAgentRooms(ctx context.Context) CleanupStats {
	stats := CleanupStats{}
	if s.db == nil {
		return stats
	}

	candidates, err := s.scanOrphanedAgentRooms(ctx)
	if err != nil {
		logger.L().Warn("boot orphan scan failed", zap.Error(err))
		return stats
	}
	stats.Scanned = len(candidates)

	now := time.Now()
	for _, row := range candidates {
		select {
		case <-ctx.Done():
			logger.L().Info("boot orphan cleanup cancelled",
				zap.Int("remaining", len(candidates)-stats.Scanned+stats.Scanned))
			return stats
		default:
		}

		switch {
		case row.Status == "open" && row.HumanCount == 0 && row.AgentCount > 0:
			// Path 1 — never started, hard delete.
			if _, derr := s.HardDeleteRoom(ctx, row.ID); derr != nil {
				logger.L().Warn("boot orphan hard-delete failed",
					zap.String("room_id", row.ID), zap.Error(derr))
				stats.Skipped++
				continue
			}
			stats.HardDeleted++

		case row.Status == "playing" && row.HumanCount == 0 && row.AgentCount > 0 &&
			s.IsRoomEmpty(row.ID) && now.Sub(row.UpdatedAt) > orphanStalePlayingAge:
			// Path 2 — was playing, hub says nobody's there, stale enough
			// to be considered abandoned. ForceDisbandRoom drops the DB
			// rows AND calls RemoveRoomState + BroadcastRoomRemoved.
			_, derr := s.ForceDisbandRoom(ctx, row.ID, "system-boot-cleanup",
				"orphan agent room (stale playing, no live clients)")
			if derr != nil {
				// ForceDisbandRoom returns ErrRoomNotFound when the
				// row vanished between our scan and the disband — that
				// is a soft success (we wanted it gone), so it does
				// NOT count as a Skipped.
				if ce, ok := derr.(*errcode.Error); ok && ce.Code == errcode.ErrRoomNotFound {
					stats.HardDeleted++
					continue
				}
				logger.L().Warn("boot orphan force-disband failed",
					zap.String("room_id", row.ID), zap.Error(derr))
				stats.Skipped++
				continue
			}
			stats.Disbanded++

		default:
			stats.Skipped++
		}
	}

	logger.L().Info("boot orphan agent room cleanup finished",
		zap.Int("scanned", stats.Scanned),
		zap.Int("disbanded", stats.Disbanded),
		zap.Int("hard_deleted", stats.HardDeleted),
		zap.Int("skipped", stats.Skipped))
	return stats
}

// scanOrphanedAgentRooms runs the candidate query: every room that has at
// least one role='agent' row in t_lsm_game_player, with the human + agent
// role counts broken out so the caller can decide which action (if any)
// to take. Returns an empty slice (not an error) when no candidates match.
func (s *RoomService) scanOrphanedAgentRooms(ctx context.Context) ([]OrphanRoomRow, error) {
	type row struct {
		ID         string
		GameKind   string
		Status     string
		UpdatedAt  time.Time
		HumanCount int
		AgentCount int
	}

	// GROUP BY r.id collapses to one row per room. We restrict the
	// candidate set to werewolf rooms because role='agent' is only
	// emitted for werewolf today; including other game kinds would
	// require an extra role='agent' guard inside the SUM(CASE ...) anyway.
	const q = `
SELECT r.id              AS id,
       r.game_kind       AS game_kind,
       r.status          AS status,
       r.updated_at      AS updated_at,
       SUM(CASE WHEN p.role <> 'agent' THEN 1 ELSE 0 END) AS human_count,
       SUM(CASE WHEN p.role =  'agent' THEN 1 ELSE 0 END) AS agent_count
FROM t_lsm_game_room r
JOIN t_lsm_game_player p ON p.room_id = r.id
WHERE r.game_kind = 'werewolf'
GROUP BY r.id, r.game_kind, r.status, r.updated_at
HAVING SUM(CASE WHEN p.role = 'agent' THEN 1 ELSE 0 END) > 0
`
	var rawRows []row
	if err := s.db.WithContext(ctx).Raw(q).Scan(&rawRows).Error; err != nil {
		return nil, err
	}
	out := make([]OrphanRoomRow, 0, len(rawRows))
	for _, r := range rawRows {
		out = append(out, OrphanRoomRow{
			ID:         r.ID,
			GameKind:   r.GameKind,
			Status:     r.Status,
			UpdatedAt:  r.UpdatedAt,
			HumanCount: r.HumanCount,
			AgentCount: r.AgentCount,
		})
	}
	return out, nil
}

// Note on interface satisfaction: GameServiceAPI is satisfied by
// *ws.GameService (production) and by the fakeGameSvc in
// room_force_disband_test.go (tests). The compile-time check happens at
// the call sites — main.go's `roomSvc.SetGameServiceHook(gameSvcWs)` and
// the test's `&RoomService{gameSvc: fake}` — both fail to build if the
// interface contract drifts.