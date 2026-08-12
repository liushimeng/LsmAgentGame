// Package api — recall_chat_api.go: 狼人杀「赛后复盘问答」REST 入口。
//
// 2026-08-11 §20260811-05 U2 新增。
//
//	POST /api/games/werewolf/rooms/:roomId/recall_chat — 向本局 bot 座位提问
//
// 权限:JWT 必須 + 调用者是该房间的入座玩家(含已死亡)或观战者;
// 对局必须已结束(PhaseGameOver);每用户每房间限流(默认 10 次)。
// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260811-05.md §U2。
package api

import (
	"errors"
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RecallChatAPI exposes the werewolf recall-chat REST endpoint.
// manager 为 nil 时 handler 返回 500(装配错误,不应静默)。
type RecallChatAPI struct {
	manager *werewolf.WerewolfManager
}

// NewRecallChatAPI wires the handler with its dependencies.
func NewRecallChatAPI(manager *werewolf.WerewolfManager) *RecallChatAPI {
	return &RecallChatAPI{manager: manager}
}

// recallChatRequest 是 POST recall_chat 的请求体(严格 JSON)。
type recallChatRequest struct {
	Seat     int    `json:"seat"`
	Question string `json:"question"`
}

// RecallChat POST /api/games/werewolf/rooms/:roomId/recall_chat
//
// payload 字段(严格,DisallowUnknownFields):
//   - seat     int    (必填;0-indexed bot 座位)
//   - question string (必填;≤200 字)
//
// 响应 data:
//   - seat / model_key / role / answer / fallback / took_ms
//     (与 werewolf.RecallChatResult 一一对应;§121 数据形状前后端同步)
func (h *RecallChatAPI) RecallChat(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "recall chat: manager not wired",
		})
		return
	}
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	roomID := c.Param("roomId")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "missing room id",
		})
		return
	}

	var req recallChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "invalid json: " + err.Error(),
		})
		return
	}
	if req.Seat < 0 || req.Seat >= 13 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "seat out of range (0-12)",
		})
		return
	}
	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "question is empty",
		})
		return
	}

	// 成员校验:调用者必须是该房间的入座玩家或观战者。
	if !h.isRoomMember(roomID, uid) {
		c.JSON(http.StatusForbidden, gin.H{
			"code": errcode.ErrPermissionDenied, "message": "仅本房间的玩家或观战者可以提问",
		})
		return
	}

	// 限流:每用户每房间 N 次(默认 10)。
	if !h.manager.AllowRecallChat(roomID, uid) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code": errcode.ErrValidationFailed, "message": "本房间提问次数已用完",
		})
		return
	}

	result, err := h.manager.RecallChat(c.Request.Context(), roomID, req.Seat, req.Question)
	if err != nil {
		switch {
		case errors.Is(err, werewolf.ErrRecallDisabled):
			c.JSON(http.StatusNotFound, gin.H{
				"code": errcode.ErrGameNotFound, "message": "复盘问答未开启",
			})
		case errors.Is(err, werewolf.ErrRecallNotOver):
			c.JSON(http.StatusBadRequest, gin.H{
				"code": errcode.ErrValidationFailed, "message": "对局尚未结束,终局后才能复盘提问",
			})
		case errors.Is(err, werewolf.ErrRecallNoBot):
			c.JSON(http.StatusBadRequest, gin.H{
				"code": errcode.ErrValidationFailed, "message": "该座位不是 AI 玩家",
			})
		default:
			logger.L().Warn("RecallChat failed",
				zap.String("room_id", roomID),
				zap.String("uid", uid),
				zap.Error(err))
			ce := errcode.AsError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    result,
	})
}

// isRoomMember 报告 userID 是否是该房间的入座玩家或观战者。
// 入座判定走 manager.FactionByUserID(§20260810-03 F1 同款);
// 观战判定走房间的 Spectators 集合快照。
func (h *RecallChatAPI) isRoomMember(roomID, userID string) bool {
	// 入座玩家(含已死亡):isSpectator=false。
	if _, _, isSpectator := h.manager.FactionByUserID(roomID, userID); !isSpectator {
		return true
	}
	// 观战者集合。
	return h.manager.IsSpectatorOf(roomID, userID)
}
