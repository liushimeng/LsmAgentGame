// Package api — werewolf_review_api.go: 狼人杀「个人复盘 4 维」REST 入口
// (2026-08-14 §20260814-01 U1)。
//
//	GET /api/games/werewolf/rooms/:roomId/review/:userId — 拉取个人复盘
//
// # 为什么现在才有这个 handler
//
// §20260812-01 U1 落地了完整的 4 维聚合（`game/werewolf/recall_aggregate.go`,
// 335 行 + 单测）与前端面板（`PersonalReviewPanel.tsx`, 198 行），前端甚至
// 已经写死了本文件实现的这条 URL —— 但**路由从未存在**，聚合模块零生产调用，
// 组件零 import（§130 / §126）。本文件补齐中间缺失的那一层。
//
// # 权限
//
//   - JWT 必需；
//   - 调用者必须是该房间的入座玩家或观战者（与 RecallChat 同款 isRoomMember）；
//   - **且 :userId 必须等于调用者自己** —— 复盘含 `Role` 字段（自己的身份牌），
//     §135「身份公开公平性」要求它不得泄漏给他人。观战者也不例外：
//     观战者能看的是 GodMode 快照（已有独立 spectator 通道），
//     不是「以别人的身份视角看复盘」。
//
// 详见 docs/狼人杀-Agent与系统/狼人杀13人局Agent升级-20260814-01.md §U1。
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

// WerewolfReviewAPI exposes the werewolf personal-review REST endpoint.
// manager 为 nil 时 handler 返回 500（装配错误，不应静默）。
type WerewolfReviewAPI struct {
	manager *werewolf.WerewolfManager
}

// NewWerewolfReviewAPI wires the handler with its dependencies.
func NewWerewolfReviewAPI(manager *werewolf.WerewolfManager) *WerewolfReviewAPI {
	return &WerewolfReviewAPI{manager: manager}
}

// GetPersonalReview GET /api/games/werewolf/rooms/:roomId/review/:userId
//
// 响应 data（§121 数据形状 —— wrapper，与前端
// PersonalReviewPanel.tsx 的 `PersonalReviewResponse` 类型逐字段对应）:
//
//	{ review: {...4 维 + overall_score + highlights}, computed_at, from_cache }
func (h *WerewolfReviewAPI) GetPersonalReview(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "review: manager not wired",
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
	targetUID := c.Param("userId")
	if targetUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "missing user id",
		})
		return
	}

	// 成员校验:调用者必须是该房间的入座玩家或观战者。
	if !h.isRoomMember(roomID, uid) {
		c.JSON(http.StatusForbidden, gin.H{
			"code": errcode.ErrPermissionDenied, "message": "仅本房间的玩家或观战者可以查看复盘",
		})
		return
	}

	// §135 身份公开公平性:复盘含自己的角色牌,只能看自己的。
	// 这条守卫必须在 manager 调用**之前** —— 否则 ComputeReviewForUser 会把
	// 他人的 Role 算进 PersonalReview 并写进 30min 缓存,即便 handler 随后
	// 拒绝返回,缓存里也已经留下了一份可被后续请求命中的越权数据。
	if targetUID != uid {
		c.JSON(http.StatusForbidden, gin.H{
			"code": errcode.ErrPermissionDenied, "message": "只能查看自己的复盘",
		})
		return
	}

	resp, err := h.manager.ComputeReviewForUser(c.Request.Context(), roomID, targetUID)
	if err != nil {
		switch {
		case errors.Is(err, werewolf.ErrReviewNotOver):
			c.JSON(http.StatusBadRequest, gin.H{
				"code": errcode.ErrValidationFailed, "message": "对局尚未结束,终局后才能查看复盘",
			})
		case errors.Is(err, werewolf.ErrReviewForbidden):
			c.JSON(http.StatusForbidden, gin.H{
				"code": errcode.ErrPermissionDenied, "message": "只能查看自己的复盘",
			})
		default:
			logger.L().Warn("GetPersonalReview failed",
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
		"data":    resp,
	})
}

// isRoomMember 报告 userID 是否是该房间的入座玩家或观战者。
// 与 RecallChatAPI.isRoomMember 同款判定(入座走 FactionByUserID,
// 观战走 IsSpectatorOf)。
func (h *WerewolfReviewAPI) isRoomMember(roomID, userID string) bool {
	if _, _, isSpectator := h.manager.FactionByUserID(roomID, userID); !isSpectator {
		return true
	}
	return h.manager.IsSpectatorOf(roomID, userID)
}
