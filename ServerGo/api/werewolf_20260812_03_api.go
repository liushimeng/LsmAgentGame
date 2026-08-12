// Package api — werewolf_20260812_03_api.go: 狼人杀 13 人局 §20260812-03 三项升级 REST 入口。
//
// 包含 3 个独立端点(共享同一 manager / 同一 isRoomMember 判定):
//   - GET  /api/games/werewolf/rooms/:roomId/win-probability  (U1 阵营胜率热力图)
//   - POST /api/games/werewolf/rooms/:roomId/secret-letter    (U2 暗线信件发送)
//   - GET  /api/games/werewolf/rooms/:roomId/secret-letter/inbox (U2 收件箱)
//
// 共用 §20260811-05 RecallChatAPI 的:
//   - 鉴权:JWT + isRoomMember(入座玩家或观战者)
//   - 数据形状契约:严格 JSON + wrapper data(§121)
//
// 限制:
//   - §132 隐私:U1 胜率**仅**观战者可见(写文件时另加 lockRoomBriefly 校验)
//   - §119 协议层隔离:U2 暗线信件不入 chat_message 表/队列/BotTranscript
//   - §130 接线:在 router.go 注册 3 个路由
package api

import (
	"net/http"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/game/werewolf"
	"LsmAgentGame/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Werewolf20260812API 是 §20260812-03 三项升级的 REST 入口。
type Werewolf20260812API struct {
	manager *werewolf.WerewolfManager
}

// NewWerewolf20260812API 装配 handler。
func NewWerewolf20260812API(manager *werewolf.WerewolfManager) *Werewolf20260812API {
	return &Werewolf20260812API{manager: manager}
}

// GetWinProbability GET /api/games/werewolf/rooms/:roomId/win-probability
//
// §132 隐私隔离:仅观战者(viewer<0)可见;玩家入座时返回 403。
// §121 数据形状:data = { probabilities: number[13], round: int, computed_at: int64 }
func (h *Werewolf20260812API) GetWinProbability(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "win-probability: manager not wired",
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

	// §132 隐私隔离:仅观战者可见
	_, _, isSpectator := h.manager.FactionByUserID(roomID, uid)
	if !isSpectator {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "胜率热力图仅观战者可见",
		})
		return
	}

	// §92a:通过 manager.SpectatorView 间接取数据(它内部已 lockRoomBriefly 持锁)
	snap := h.manager.SpectatorView(roomID)
	if snap == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": errcode.ErrGameNotFound, "message": "房间不存在或未在游戏中",
		})
		return
	}
	if snap.WinRateProbability == nil {
		snap.WinRateProbability = []float64{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"probabilities": snap.WinRateProbability,
			"round":         snap.Day,
			"computed_at":   time.Now().Unix(),
		},
	})
}

// isRoomMember 报告 userID 是否是该房间的入座玩家或观战者。
// 复用 RecallChatAPI.isRoomMember 的判定逻辑。
func (h *Werewolf20260812API) isRoomMember(roomID, userID string) bool {
	if _, _, isSpectator := h.manager.FactionByUserID(roomID, userID); !isSpectator {
		return true
	}
	return h.manager.IsSpectatorOf(roomID, userID)
}

// SendSecretLetter POST /api/games/werewolf/rooms/:roomId/secret-letter
//
// §119 协议层隔离:暗线信件不入 chat_message / chat_history 队列 / BotTranscript。
// §97 不发新 phase(白天 speak→vote 之间即可);§122 限流共享 speakLimiter。
//
// payload(严格 JSON):
//   - target_seat int  (必填;0-indexed 目标座位,不可为自己/死亡/不存在)
//   - body        string (必填;≤200 字)
//
// 响应 data: { letter_id, sent_at }
func (h *Werewolf20260812API) SendSecretLetter(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "secret-letter: manager not wired",
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
	if !h.isRoomMember(roomID, uid) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "仅本房间的玩家可以发送暗线信件",
		})
		return
	}

	var req struct {
		TargetSeat int    `json:"target_seat"`
		Body       string `json:"body"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "invalid json: " + err.Error(),
		})
		return
	}
	if req.TargetSeat < 0 || req.TargetSeat >= 13 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "target_seat out of range (0-12)",
		})
		return
	}
	if req.Body == "" || len([]rune(req.Body)) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "body 必须非空且 ≤200 字",
		})
		return
	}

	// U2 §20260812-03 落地:调用 manager.SendSecretLetter
	letter, err := h.manager.SendSecretLetter(c.Request.Context(), roomID, uid, req.TargetSeat, req.Body)
	if err != nil {
		logger.L().Warn("SendSecretLetter failed",
			zap.String("room_id", roomID),
			zap.String("uid", uid),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"letter_id": letter.ID,
			"sent_at":   letter.CreatedAt.Unix(),
		},
	})
}

// GetSecretLetterInbox GET /api/games/werewolf/rooms/:roomId/secret-letter/inbox
//
// §119 协议层隔离:仅自己可读自己收到的(to_seat==callerSeat)。
// 响应 data: { letters: SecretLetterView[] }
func (h *Werewolf20260812API) GetSecretLetterInbox(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "secret-letter inbox: manager not wired",
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
	if !h.isRoomMember(roomID, uid) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "仅本房间的玩家可以查看收件箱",
		})
		return
	}
	letters, err := h.manager.GetSecretLetterInbox(c.Request.Context(), roomID, uid)
	if err != nil {
		logger.L().Warn("GetSecretLetterInbox failed",
			zap.String("room_id", roomID),
			zap.String("uid", uid),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "获取收件箱失败",
		})
		return
	}
	if letters == nil {
		letters = []werewolf.SecretLetterView{}
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{"letters": letters},
	})
}

// PlaceFactionBet POST /api/games/werewolf/rooms/:roomId/faction-bet
//
// §6.2 阵营赌注:白天 speak 阶段玩家下注(下注其他玩家阵营)。
// §133 EconTier:独立常量 FactionBetDestroyRate = 50。
// §135 公平性:押注信息对其他玩家**不可见**。
//
// payload(严格 JSON):
//   - target_seat       int    (必填;0-indexed 目标座位,不可为自己/死亡)
//   - predicted_faction string (必填;"wolf"/"good")
//   - amount            int    (必填;10~500)
//
// 响应 data: { bet_id, accepted_at }
// 注:钱包扣款由调用方在持锁外完成(本骨架仅生成 bet_id;生产实现需补 wallet 集成)。
func (h *Werewolf20260812API) PlaceFactionBet(c *gin.Context) {
	if h.manager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "faction-bet: manager not wired",
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
	if !h.isRoomMember(roomID, uid) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "仅本房间的玩家可以下注",
		})
		return
	}

	var req struct {
		TargetSeat       int    `json:"target_seat"`
		PredictedFaction string `json:"predicted_faction"`
		Amount           int    `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "invalid json: " + err.Error(),
		})
		return
	}

	betID, err := h.manager.PlaceFactionBet(roomID, uid, req.TargetSeat, req.PredictedFaction, req.Amount)
	if err != nil {
		logger.L().Debug("PlaceFactionBet rejected",
			zap.String("room_id", roomID),
			zap.String("uid", uid),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"bet_id":      betID,
			"accepted_at": time.Now().Unix(),
		},
	})
}

// GetFactionBetStatus GET /api/games/werewolf/rooms/:roomId/faction-bet-status
//
// 返回本房间最近一轮的下注状态(仅自己可见)。
// 响应 data: { window_open, round, bets: TLsmGameFactionBet[] }
func (h *Werewolf20260812API) GetFactionBetStatus(c *gin.Context) {
	roomID := c.Param("roomId")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "missing room id",
		})
		return
	}
	// §135 公平性:本接口仅返回窗口状态 + 自己的下注,不返回他人下注。
	// 完整实现需要 DB 查询 + per-user filter,本骨架返回空列表。
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"window_open": true,
			"round":       0,
			"bets":        []any{},
		},
	})
}
