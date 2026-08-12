// Package api holds Gin HTTP handlers. One file per resource.
//
// admin_api.go provides admin-only user management endpoints:
//
//	GET    /api/admin/users        — list all users (admin + super admin)
//	DELETE /api/admin/users/:id    — delete a user (super admin only)
//	DELETE /api/admin/rooms/:id    — force-disband a room (super admin only)
//	POST   /api/admin/rooms/cleanup — run a one-shot stale-room sweep
//	POST   /api/admin/chat/cleanup  — delete lobby chat messages by time range (admin + super admin)
package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdminAPI is the admin resource handler.
type AdminAPI struct {
	svc     *service.UserService
	roomSvc *service.RoomService
	wm      *werewolf.WerewolfManager
	db      *gorm.DB
}

// NewAdminAPI wires the handler with its service.
func NewAdminAPI(svc *service.UserService, roomSvc *service.RoomService, wm *werewolf.WerewolfManager, db *gorm.DB) *AdminAPI {
	return &AdminAPI{svc: svc, roomSvc: roomSvc, wm: wm, db: db}
}

// ListUsers GET /api/admin/users (admin + super admin).
func (a *AdminAPI) ListUsers(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}

	// Check user type
	userType, err := a.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "管理员权限不足",
		})
		return
	}

	users, err := a.svc.ListAllUsers(c.Request.Context())
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    users,
	})
}

// DeleteUser DELETE /api/admin/users/:id (super admin only).
func (a *AdminAPI) DeleteUser(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}

	// Check user type - only super admin can delete
	userType, err := a.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	if userType < models.UserTypeSuper {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要超级管理员权限",
		})
		return
	}

	targetID := c.Param("id")
	if targetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "用户 ID 不能为空",
		})
		return
	}

	// Prevent self-deletion
	if targetID == uid {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "不能删除自己",
		})
		return
	}

	// Generous timeout — InnoDB rollback on cancelled context is more
	// expensive than waiting a few extra seconds for the commit.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Clean up rooms the user was in FIRST — once the user row is gone,
	// there's no way to discover which rooms the user was in (player rows
	// are also removed by DeleteUserWithRelatedData). Non-fatal.
	if a.roomSvc != nil {
		if err := a.roomSvc.DeleteRoomsByUser(ctx, targetID); err != nil {
			logger.L().Warn("room cleanup after admin delete",
				zap.String("target_id", targetID), zap.Error(err))
		}
	}

	if err := a.svc.DeleteUserWithRelatedData(ctx, targetID); err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	logger.L().Info("user deleted by admin",
		zap.String("admin_id", uid),
		zap.String("target_id", targetID))

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
	})
}

// CleanupStaleRooms POST /api/admin/rooms/cleanup?age_minutes=30
//
// Forces a one-shot JanitorSweepStale pass. Use it to clear accumulated test
// rooms or zombie rooms that escaped the regular janitor (process crash,
// player row orphaned without a hub-driven cleanup, etc.).
//
// age_minutes defaults to 30; valid range [1, 1440].
//
// ROUND 25 BUG-WEREWOLF-P2-NEW-9 follow-up: previously super-admin only.
// Lowered to admin (UserTypeAdmin) since the operation is read-mostly
// (scans rooms by age, deletes expired) and produces no destructive effect
// beyond what the regular Janitor already does on its hourly sweep. This
// unblocks test_01/test_02 (admin role) from clearing Round 24-25 leftover
// rooms without escalating to super-admin.
func (a *AdminAPI) CleanupStaleRooms(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	userType, err := a.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return
	}

	ageMin, err := strconv.Atoi(c.DefaultQuery("age_minutes", "30"))
	if err != nil || ageMin < 1 || ageMin > 1440 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "age_minutes must be an integer in [1, 1440]",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if a.roomSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "room service not wired",
		})
		return
	}
	stats := a.roomSvc.JanitorSweepStale(ctx, time.Duration(ageMin)*time.Minute)
	logger.L().Info("admin triggered stale room cleanup",
		zap.String("admin_id", uid),
		zap.Int("age_minutes", ageMin),
		zap.Int("scanned", stats.Scanned),
		zap.Int("deleted", stats.Deleted),
		zap.Int("skipped", stats.Skipped),
		zap.Int64("duration_ms", stats.Duration.Milliseconds()))
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"scanned":     stats.Scanned,
			"deleted":     stats.Deleted,
			"skipped":     stats.Skipped,
			"duration_ms": stats.Duration.Milliseconds(),
		},
	})
}

// CleanupChatMessages POST /api/admin/chat/cleanup
//
// Deletes lobby chat messages within a specified time range.
// Admin and super admin can use this endpoint.
//
// Request body JSON:
//
//	{
//	  "start_time": "2026-01-01T00:00:00Z",  // RFC3339 format, required
//	  "end_time":   "2026-01-02T00:00:00Z"   // RFC3339 format, required
//	}
//
// Response JSON:
//
//	{
//	  "code": 0,
//	  "message": "ok",
//	  "data": { "deleted_count": 42 }
//	}
func (a *AdminAPI) CleanupChatMessages(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}

	// Check user type — admin or super admin
	userType, err := a.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return
	}

	// Parse request body
	var req struct {
		StartTime string `json:"start_time" binding:"required"`
		EndTime   string `json:"end_time"   binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "请提供 start_time 和 end_time（RFC3339 格式）",
		})
		return
	}

	startTime, err := time.Parse(time.RFC3339, req.StartTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "start_time 格式无效，请使用 RFC3339 格式（如 2026-01-01T00:00:00Z）",
		})
		return
	}

	endTime, err := time.Parse(time.RFC3339, req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "end_time 格式无效，请使用 RFC3339 格式（如 2026-01-02T00:00:00Z）",
		})
		return
	}

	if !endTime.After(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "end_time 必须晚于 start_time",
		})
		return
	}

	if a.db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "database not wired",
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// Delete lobby chat messages (scope="lobby") within the time range
	result := a.db.WithContext(ctx).
		Where("scope = ? AND created_at >= ? AND created_at < ?", "lobby", startTime, endTime).
		Delete(&models.TLsmGameChatMessage{})
	if result.Error != nil {
		logger.L().Error("admin chat cleanup failed",
			zap.String("admin_id", uid),
			zap.Error(result.Error))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "清理聊天消息失败",
		})
		return
	}

	deletedCount := result.RowsAffected
	logger.L().Info("admin chat cleanup",
		zap.String("admin_id", uid),
		zap.Time("start_time", startTime),
		zap.Time("end_time", endTime),
		zap.Int64("deleted_count", deletedCount))

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"deleted_count": deletedCount,
		},
	})
}

// ForceDisbandRoom DELETE /api/admin/rooms/:room_id?reason=...
//
// Super-admin "kill switch" for a single room. Tears down in-memory
// GameState across every game manager (cancelling any running werewolf
// agent goroutines), deletes every t_lsm_game_player row for the room,
// deletes the t_lsm_game_room row itself, and broadcasts a single
// `game.removed` envelope to every currently connected player +
// spectator so their UIs drop the room.
//
// Idempotent: when the room is already gone the handler returns 200
// with `players_deleted: 0` rather than a 404 — admin tooling shouldn't
// have to special-case "already disbanded". The optional `reason` query
// parameter is forwarded to the audit log AND to every client frame so
// the front-end can show a sensible "Room removed by admin" toast.
func (a *AdminAPI) ForceDisbandRoom(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	userType, err := a.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if userType < models.UserTypeSuper {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要超级管理员权限",
		})
		return
	}

	roomID := c.Param("room_id")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": "room_id required",
		})
		return
	}

	reason := c.Query("reason")
	if reason == "" {
		reason = "admin force disband"
	}

	if a.roomSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "room service not wired",
		})
		return
	}

	// Generous timeout — disband covers an in-memory GameState cancel
	// (synchronous, fast) plus a DB transaction (fast) plus a WS broadcast
	// (synchronous, fast). 30s is the same ceiling DeleteUser uses; the
	// boot cleanup also uses 60s.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	res, derr := a.roomSvc.ForceDisbandRoom(ctx, roomID, uid, reason)
	if derr != nil {
		ce := errcode.AsError(derr)
		// ErrRoomNotFound is still a 200 with players_deleted=0 — the
		// admin's intent ("make sure this room is gone") is satisfied.
		// Everything else is a real error.
		if ce.Code == errcode.ErrRoomNotFound {
			c.JSON(http.StatusOK, gin.H{
				"code":    errcode.OK,
				"message": "room already absent",
				"data": gin.H{
					"room_id":         res.RoomID,
					"game_kind":       res.GameKind,
					"players_deleted": res.PlayersDeleted,
					"reason":          res.Reason,
					"removed_at":      res.RemovedAt,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"room_id":         res.RoomID,
			"game_kind":       res.GameKind,
			"players_deleted": res.PlayersDeleted,
			"reason":          res.Reason,
			"removed_at":      res.RemovedAt,
		},
	})
}

// GetWerewolfChatHistory GET /api/admin/werewolf/rooms/:room_id/chat_history
//
// 2026-07-09 §13-bugfix — 返回房间共享 500K 队列的完整内容,以及每个 bot 的
// read pointer。供前端调试面板 + 7 人局公平性审查使用。
//
// Query params:
//   - limit (optional, default 200, max 2000): 最多返回多少条消息(从尾部取)
//
// 鉴权:admin / super admin(人类型 ≥ UserTypeAdmin)。
//
// Response JSON:
//
//	{
//	  "code": 0,
//	  "data": {
//	    "room_id":        "abc123",
//	    "exists":         true,
//	    "queue_bytes":    47210,
//	    "queue_cap":      512000,
//	    "queue_count":    87,
//	    "messages":       [ {seq, from_seat, agent_name, text, ...}, ... ],
//	    "read_pointers":  { "0": 87, "1": 87, ... }
//	  }
//	}
func (a *AdminAPI) GetWerewolfChatHistory(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": errcode.ErrAuthMissingToken, "message": errcode.DefaultMessages[errcode.ErrAuthMissingToken]})
		return
	}
	userType, err := a.svc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return
	}

	roomID := c.Param("room_id")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": errcode.ErrValidationFailed, "message": "room_id required"})
		return
	}
	if a.wm == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": errcode.ErrInternal, "message": "werewolf manager not wired"})
		return
	}

	limit := 200
	if s := c.Query("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
			if limit > 2000 {
				limit = 2000
			}
		}
	}

	allMsgs, ptrs, bytes, ok := a.wm.ChatQueueSnapshot(roomID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": errcode.ErrInternal, "message": "room not found"})
		return
	}

	// 取末尾 limit 条
	start := 0
	if len(allMsgs) > limit {
		start = len(allMsgs) - limit
	}
	tail := allMsgs[start:]

	ptrOut := make(map[string]uint64, len(ptrs))
	for seat, seq := range ptrs {
		ptrOut[strconv.Itoa(seat)] = seq
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"room_id":       roomID,
			"exists":        true,
			"queue_bytes":   bytes,
			"queue_count":   len(allMsgs),
			"returned":      len(tail),
			"limit":         limit,
			"messages":      tail,
			"read_pointers": ptrOut,
		},
	})
}
