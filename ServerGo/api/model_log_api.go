// Package api — model_log_api.go exposes the read-only admin endpoints that
// back the React ModelAdminPage / Detail / GameLog views. Every handler
// requires admin role and delegates the actual queries to
// service.ModelLogService (this file deliberately holds no GORM calls).
//
// Endpoints:
//
//	GET /api/admin/llm/providers/:id/games
//	GET /api/admin/llm/games/:gameLogID
//	GET /api/admin/llm/games/:gameLogID/messages
//
// All three read from the persisted tables only — no in-memory state is
// touched. This makes them safe to call while a 7-AI game is in flight and
// immune to a server restart.
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/models"
	"LsmWebGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ModelLogAPI is the read-only handler for the 5 new model tables.
type ModelLogAPI struct {
	svc      *service.ModelLogService
	userSvc  UserRoleChecker
}

// NewModelLogAPI wires the handler with its dependencies.
func NewModelLogAPI(
	svc *service.ModelLogService,
	userSvc UserRoleChecker,
) *ModelLogAPI {
	return &ModelLogAPI{svc: svc, userSvc: userSvc}
}

// requireAdmin reuses the UserService-based check (same shape as
// ModelAdminAPI.requireAdmin — duplicated here so the two handler types stay
// decoupled and easier to test in isolation).
func (h *ModelLogAPI) requireAdmin(c *gin.Context) (string, bool) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return "", false
	}
	if h.userSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "user service not wired",
		})
		return "", false
	}
	userType, err := h.userSvc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", false
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return "", false
	}
	return uid, true
}

// ─────────────────── ListProviderGames ───────────────────

// ListProviderGames GET /api/admin/llm/providers/:id/games?limit=20&offset=0&since=<rfc3339>
//
// Returns paginated game_log rows for one provider. `since` is optional; when
// present it's parsed as RFC3339 and applied to started_at >= since.
func (h *ModelLogAPI) ListProviderGames(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.svc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "model log service not wired",
		})
		return
	}

	providerID := c.Param("id")
	if providerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "id required",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	var since time.Time
	if raw := strings.TrimSpace(c.Query("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": errcode.ErrValidationFailed,
				"message": "since must be RFC3339 (e.g. 2026-01-01T00:00:00Z)",
			})
			return
		}
		since = t
	}

	games, err := h.svc.ListProviderGames(c.Request.Context(), providerID, limit, offset, since)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"provider_id": providerID,
			"games":       games,
			"limit":       limit,
			"offset":      offset,
			"total":       len(games),
		},
	})
}

// ─────────────────── GetGameLog ───────────────────

// GetGameLog GET /api/admin/llm/games/:gameLogID
//
// Returns the full game_log row by id. The handler deliberately does NOT
// fold chat_message + action rows into the response — that's what the
// /messages endpoint is for, and a single combined payload would balloon
// for long games.
func (h *ModelLogAPI) GetGameLog(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.svc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "model log service not wired",
		})
		return
	}

	id := c.Param("gameLogID")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "gameLogID required",
		})
		return
	}
	row, err := h.svc.GetGameLog(c.Request.Context(), id)
	if err != nil {
		ce := errcode.AsError(err)
		// ValidationFailed here = not-found (see ModelLogService.GetGameLog).
		if ce.Code == errcode.ErrValidationFailed {
			c.JSON(http.StatusNotFound, gin.H{"code": ce.Code, "message": "game_log not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": row,
	})
}

// ─────────────────── ListGameMessages ───────────────────

// ListGameMessages GET /api/admin/llm/games/:gameLogID/messages?limit=200&offset=0
//
// Returns {messages, actions} for one game_log. The two slices are fetched
// in parallel using goroutines so a 7-bot game's 1k+ rows don't serialise
// the two queries.
//
// Limit clamps: messages <= 2000, actions <= 2000. The front-end game-log
// viewer defaults to the tail 200 rows.
func (h *ModelLogAPI) ListGameMessages(c *gin.Context) {
	if _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.svc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "model log service not wired",
		})
		return
	}

	id := c.Param("gameLogID")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "gameLogID required",
		})
		return
	}

	msgLimit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	actLimit := msgLimit
	msgOffset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	actOffset := msgOffset

	// Verify the game_log exists first so we don't silently return empty
	// arrays for a typo'd id.
	if _, err := h.svc.GetGameLog(c.Request.Context(), id); err != nil {
		ce := errcode.AsError(err)
		if ce.Code == errcode.ErrValidationFailed {
			c.JSON(http.StatusNotFound, gin.H{"code": ce.Code, "message": "game_log not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	type msgResult struct {
		msgs []models.TLsmGameModelChatMessage
		err  error
	}
	type actResult struct {
		acts []models.TLsmGameModelAction
		err  error
	}

	msgCh := make(chan msgResult, 1)
	actCh := make(chan actResult, 1)

	go func() {
		msgs, err := h.svc.ListGameMessages(c.Request.Context(), id, msgLimit, msgOffset)
		msgCh <- msgResult{msgs, err}
	}()
	go func() {
		acts, err := h.svc.ListGameActions(c.Request.Context(), id, actLimit, actOffset)
		actCh <- actResult{acts, err}
	}()

	mr := <-msgCh
	ar := <-actCh

	if mr.err != nil {
		ce := errcode.AsError(mr.err)
		logger.L().Error("model_log_api: list messages failed",
			zap.String("game_log_id", id), zap.Error(mr.err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	if ar.err != nil {
		ce := errcode.AsError(ar.err)
		logger.L().Error("model_log_api: list actions failed",
			zap.String("game_log_id", id), zap.Error(ar.err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"game_log_id": id,
			"messages":    mr.msgs,
			"actions":     ar.acts,
			"total_messages": len(mr.msgs),
			"total_actions":  len(ar.acts),
			"limit":       msgLimit,
			"offset":      msgOffset,
		},
	})
}
