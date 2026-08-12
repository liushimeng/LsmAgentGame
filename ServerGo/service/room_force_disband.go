package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// GameServiceAPI is the subset of *ws.GameService that RoomService needs to
// force-disband a room. The interface lives here (not in package ws) so
// service.RoomService does not have to import package ws — importing ws
// would be a cycle (ws already imports service.RoomService for the
// gameJoiner / agentSeater callbacks wired in main.go).
//
// The methods mirror exactly the two ws.GameService methods exercised by
// ForceDisbandRoom:
//   - RemoveRoomState  : tears down in-memory GameState across every manager
//                        (xiangqi / chess / junqi / doudizhu / texasholdem /
//                        werewolf). For werewolf this also triggers
//                        stopAgentsLocked so all agent goroutines for the
//                        room receive their cancel signal.
//   - BroadcastRoomRemoved: pushes a single `game.removed` envelope to every
//                        player + spectator of the room, then unsubscribes
//                        them so the client-side WS layer drops the room
//                        from its local state.
type GameServiceAPI interface {
	RemoveRoomState(roomID string)
	BroadcastRoomRemoved(roomID, reason string)
	// WerewolfFillingRoomSnapshot returns a read-only snapshot of every
	// in-memory werewolf room still stuck in the filling phase (R187-1).
	// Returns nil when no werewolf manager is wired (unit tests).
	WerewolfFillingRoomSnapshot() []WerewolfFillingRoomInfo
	// ForceCloseWerewolfFillingRoom removes a filling-phase werewolf room
	// from the in-memory manager and stops its agent goroutines. Returns
	// false when the room is absent or started mid-sweep.
	ForceCloseWerewolfFillingRoom(roomID string) bool
}

// WerewolfFillingRoomInfo is a service-package-local mirror of
// werewolf.FillingRoomInfo (projected by ws.GameService to avoid a
// service → game/werewolf import). Keep both shapes in sync.
type WerewolfFillingRoomInfo struct {
	RoomID        string
	Phase         string
	CreatedAt     time.Time
	OccupiedSeats int
	Spectators    int
}

// HubAPI is the subset of *ws.Hub that RoomService.BootCleanupOrphanedAgentRooms
// needs to decide whether an in-memory room is still actively in use. The
// boot cleanup must avoid force-disbanding a room whose agents are still
// running live — IsRoomEmpty returns true when neither hub.rooms[roomID] nor
// hub.spectators[roomID] holds any connected Client.
type HubAPI interface {
	IsRoomEmpty(roomID string) bool
}

// DisbandResult is the value returned by ForceDisbandRoom. All fields are
// JSON-serialisable so the admin handler can echo them straight back.
type DisbandResult struct {
	RoomID         string    `json:"room_id"`
	GameKind       string    `json:"game_kind"`
	PlayersDeleted int       `json:"players_deleted"`
	Reason         string    `json:"reason"`
	RemovedAt      time.Time `json:"removed_at"`
}

// SetGameServiceHook registers the GameServiceAPI implementation. main.go
// wires the real *ws.GameService into RoomService after both objects are
// constructed — this keeps the dependency direction service ← ws (legal)
// and avoids the inverse import cycle.
func (s *RoomService) SetGameServiceHook(gs GameServiceAPI) {
	s.gameSvc = gs
}

// SetHubHook registers the HubAPI implementation. Used by boot cleanup to
// probe for actively-connected players before force-disbanding an orphan.
func (s *RoomService) SetHubHook(h HubAPI) {
	s.hubHook = h
}

// ForceDisbandRoom deletes a room and every row that references it, in a
// single transaction, then fans a `game.removed` envelope out to anyone
// still connected. It is the admin-only "kill switch" for rooms that:
//
//   - Are stuck in `status='playing'` after a process restart that
//     orphaned the in-memory GameState (the room row stays in DB but no
//     goroutines are driving it).
//   - Are filled exclusively with role='agent' seats whose backing LLM
//     providers have all been disabled — the agents loop on 401/403
//     responses and never advance.
//   - Are otherwise unkillable through normal flow (the human creator
//     disconnected, leaving the auto-vacancy timer racing with a stuck
//     hub.rooms[roomID] entry that never goes empty).
//
// The function is intentionally idempotent and safe to call when the room
// is already gone (returns ErrRoomNotFound so the caller can decide how to
// surface it). The DB write is atomic: either both t_lsm_game_player rows
// AND the t_lsm_game_room row are deleted, or nothing changes. The WS
// broadcast happens AFTER the commit so we never tell a client "room gone"
// while the row is still live in DB (which would race a reconnect).
func (s *RoomService) ForceDisbandRoom(ctx context.Context, roomID, adminID, reason string) (DisbandResult, error) {
	res := DisbandResult{
		RoomID:    roomID,
		Reason:    reason,
		RemovedAt: time.Now().UTC(),
	}
	if strings.TrimSpace(roomID) == "" {
		return res, errcode.CodeMsg(errcode.ErrValidationFailed, "room_id required")
	}
	if s.db == nil {
		return res, errcode.Code(errcode.ErrInternal)
	}

	// 0) Tear down in-memory state UP FRONT so any in-flight LLM call
	//    cannot race with the DELETE and try to write into a GameState
	//    we're about to drop. RemoveRoomState is a no-op on game kinds
	//    that don't have an in-memory room. We also broadcast a
	//    `game.removed` envelope first so a stuck-but-still-connected
	//    client gets the signal before any DB row disappears (the
	//    broadcast itself is a no-op on empty hub sets).
	if s.gameSvc != nil {
		s.gameSvc.RemoveRoomState(roomID)
	}

	// 1) Load the room. Not-found is treated as a soft success (the room
	//    is already gone — nothing for us to do) but the result still
	//    records the requested ID so the audit log captures the attempt.
	var room models.TLsmGameRoom
	if err := s.db.WithContext(ctx).Where("id = ?", roomID).First(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L().Info("force disband: room already absent",
				zap.String("room_id", roomID),
				zap.String("admin_id", adminID),
				zap.String("reason", reason))
			return res, errcode.Code(errcode.ErrRoomNotFound)
		}
		logger.L().Error("force disband: load room",
			zap.String("room_id", roomID), zap.Error(err))
		return res, errcode.Code(errcode.ErrDB)
	}
	res.GameKind = room.GameKind

	// 2) Count affected player rows so the audit log shows "this many
	//    player/spectator rows were dropped". Done OUTSIDE the transaction
	//    so the count is a stable snapshot even if the DELETE touches
	//    concurrent inserts (admittedly rare for admin path).
	var playerCount int64
	if err := s.db.WithContext(ctx).Model(&models.TLsmGamePlayer{}).
		Where("room_id = ?", roomID).Count(&playerCount).Error; err != nil {
		logger.L().Error("force disband: count players",
			zap.String("room_id", roomID), zap.Error(err))
		return res, errcode.Code(errcode.ErrDB)
	}
	res.PlayersDeleted = int(playerCount)

	// 3) Atomic DB delete. The transaction is the source of truth — if
	//    it rolls back, the WS broadcast below is skipped and the next
	//    caller will see the room still exists.
	tx := s.db.WithContext(ctx).Begin()
	if err := tx.Where("room_id = ?", roomID).
		Delete(&models.TLsmGamePlayer{}).Error; err != nil {
		tx.Rollback()
		logger.L().Error("force disband: delete players",
			zap.String("room_id", roomID), zap.Error(err))
		return res, errcode.Code(errcode.ErrDB)
	}
	if err := tx.Where("id = ?", roomID).
		Delete(&models.TLsmGameRoom{}).Error; err != nil {
		tx.Rollback()
		logger.L().Error("force disband: delete room",
			zap.String("room_id", roomID), zap.Error(err))
		return res, errcode.Code(errcode.ErrDB)
	}
	// Cascade: drop the room's chat messages so the orphaned t_lsm_game_chat
	// table doesn't hold onto history for a room that no longer exists.
	if err := tx.Where("scope = ? AND room_id = ?", "room", roomID).
		Delete(&models.TLsmGameChatMessage{}).Error; err != nil {
		tx.Rollback()
		logger.L().Error("force disband: delete chat messages",
			zap.String("room_id", roomID), zap.Error(err))
		return res, errcode.Code(errcode.ErrDB)
	}
	if err := tx.Commit().Error; err != nil {
		logger.L().Error("force disband: commit",
			zap.String("room_id", roomID), zap.Error(err))
		return res, errcode.Code(errcode.ErrDB)
	}

	// 4) Broadcast AFTER the commit. If this fails (e.g. gameSvc not
	//    wired), the room is still gone — the next janitor sweep / client
	//    reconnect will reconcile the orphaned hub entry, so we log and
	//    continue rather than roll back.
	if s.gameSvc != nil {
		s.gameSvc.BroadcastRoomRemoved(roomID, reason)
	}

	logger.L().Warn("admin force-disbanded room",
		zap.String("room_id", roomID),
		zap.String("game_kind", res.GameKind),
		zap.String("admin_id", adminID),
		zap.String("reason", reason),
		zap.Int("players_deleted", res.PlayersDeleted),
		zap.Time("removed_at", res.RemovedAt))

	return res, nil
}

// HardDeleteRoom drops a room from the DB by ID regardless of occupancy.
// It is the boot-cleanup companion to ForceDisbandRoom used for rooms that
// are already known to have no in-memory GameState (no need to call
// RemoveRoomState / BroadcastRoomRemoved on those). Wrapped in a single
// transaction so the player rows + room row vanish atomically.
//
// Returns the number of player rows deleted, or an errcode.Error if any
// DB step failed. The error is logged here so callers don't need to
// double-log.
func (s *RoomService) HardDeleteRoom(ctx context.Context, roomID string) (int, error) {
	if strings.TrimSpace(roomID) == "" {
		return 0, errcode.CodeMsg(errcode.ErrValidationFailed, "room_id required")
	}
	if s.db == nil {
		return 0, errcode.Code(errcode.ErrInternal)
	}

	var playersDeleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("room_id = ?", roomID).Delete(&models.TLsmGamePlayer{})
		if res.Error != nil {
			return res.Error
		}
		playersDeleted = res.RowsAffected

		rRes := tx.Where("id = ?", roomID).Delete(&models.TLsmGameRoom{})
		if rRes.Error != nil {
			return rRes.Error
		}
		// If the room row was already gone, that's fine — we still
		// dropped whatever orphan player rows pointed at it.
		// Cascade: drop the room's chat messages so deleted rooms don't
		// leak history.
		if cErr := tx.Where("scope = ? AND room_id = ?", "room", roomID).
			Delete(&models.TLsmGameChatMessage{}).Error; cErr != nil {
			return cErr
		}
		return nil
	})
	if err != nil {
		logger.L().Error("hard delete room failed",
			zap.String("room_id", roomID), zap.Error(err))
		return 0, errcode.Code(errcode.ErrDB)
	}
	logger.L().Info("hard-deleted orphan agent room",
		zap.String("room_id", roomID),
		zap.Int64("players_deleted", playersDeleted))
	return int(playersDeleted), nil
}

// IsRoomEmpty forwards to the injected HubAPI if wired, otherwise returns
// true (treat the absence of a hub hook as "no live clients"). Used by
// boot cleanup to gate force-disband on "nobody is currently connected".
func (s *RoomService) IsRoomEmpty(roomID string) bool {
	if s.hubHook == nil {
		return true
	}
	return s.hubHook.IsRoomEmpty(roomID)
}