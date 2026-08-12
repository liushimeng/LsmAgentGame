// Package api — model_wallet_api.go exposes the bot-wallet admin endpoints.
//
// Endpoints:
//
//	GET  /api/admin/llm/bots/:botUserID/wallet
//	POST /api/admin/llm/bots/:botUserID/wallet/adjust      (super admin only)
//
// GET returns the wallet balance + lifetime totals + the last N ledger rows
// (delegated to service.ModelLogService.GetBotWalletSummary). The handler
// itself doesn't compute any wallet math.
//
// POST adjust is the "manual credit / debit" escape hatch for ops. It MUST
// verify the target user is actually a bot (IsBot=true) so an admin can't
// silently siphon coins from a human player row by typo. Implementation
// reuses service.WalletService.Credit / Debit with TxTypeAdminAdjust so the
// same double-entry ledger rule applies.
package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/models"
	"LsmAgentGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ModelWalletAPI serves the bot-wallet admin endpoints.
type ModelWalletAPI struct {
	walletSvc *service.WalletService
	modelSvc  *service.ModelLogService
	userSvc   UserRoleChecker
	gormDB    *gorm.DB
}

// NewModelWalletAPI wires the handler. modelSvc + gormDB together let the GET
// path look up the bot's nickname for the response payload.
func NewModelWalletAPI(
	walletSvc *service.WalletService,
	userSvc UserRoleChecker,
	modelSvc *service.ModelLogService,
	gormDB *gorm.DB,
) *ModelWalletAPI {
	return &ModelWalletAPI{
		walletSvc: walletSvc,
		userSvc:   userSvc,
		modelSvc:  modelSvc,
		gormDB:    gormDB,
	}
}

// AdjustWalletRequest is the JSON body for POST /api/admin/llm/bots/:botUserID/wallet/adjust.
// Amount is signed: positive = credit, negative = debit. Remark is required
// so the ledger has a human-readable audit trail.
type AdjustWalletRequest struct {
	Amount int64  `json:"amount" binding:"required"`
	Remark string `json:"remark" binding:"required,min=1,max=255"`
}

// requireAdmin mirrors the same shape as ModelAdminAPI.requireAdmin.
func (h *ModelWalletAPI) requireAdmin(c *gin.Context) (string, models.UserType, bool) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return "", 0, false
	}
	if h.userSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "user service not wired",
		})
		return "", 0, false
	}
	userType, err := h.userSvc.GetUserType(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return "", 0, false
	}
	if userType < models.UserTypeAdmin {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    errcode.ErrPermissionDenied,
			"message": "需要管理员权限",
		})
		return "", 0, false
	}
	return uid, userType, true
}

// loadBotUser fetches the target bot row and verifies IsBot=true. Returns
// (nil, false) on any failure — caller must NOT write a response in that
// case (loadBotUser has already done so).
func (h *ModelWalletAPI) loadBotUser(c *gin.Context, botUserID string) (*models.TLsmGameUser, bool) {
	if h.gormDB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "db not wired",
		})
		return nil, false
	}
	var u models.TLsmGameUser
	if err := h.gormDB.WithContext(c.Request.Context()).
		Where("id = ?", botUserID).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code": errcode.ErrValidationFailed, "message": "user not found",
			})
			return nil, false
		}
		logger.L().Error("admin bot wallet: lookup failed",
			zap.String("bot_user_id", botUserID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrDB, "message": "lookup failed",
		})
		return nil, false
	}
	if !u.IsBot {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed,
			"message": "target user is not a bot; manual adjustment is bot-only",
		})
		return nil, false
	}
	return &u, true
}

// ─────────────────── GetBotWallet ───────────────────

// GetBotWallet GET /api/admin/llm/bots/:botUserID/wallet?tx_limit=50
//
// Returns the bot's wallet balance + total_earned + total_spent + the last
// N ledger entries. Defaults to the 50 most recent transactions; capped at
// 500.
func (h *ModelWalletAPI) GetBotWallet(c *gin.Context) {
	if _, _, ok := h.requireAdmin(c); !ok {
		return
	}
	if h.modelSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "model log service not wired",
		})
		return
	}

	botUserID := strings.TrimSpace(c.Param("botUserID"))
	if botUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "botUserID required",
		})
		return
	}
	if _, ok := h.loadBotUser(c, botUserID); !ok {
		return
	}

	txLimit, _ := strconv.Atoi(c.DefaultQuery("tx_limit", "50"))

	summary, err := h.modelSvc.GetBotWalletSummary(c.Request.Context(), botUserID, txLimit)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"bot_user_id":  botUserID,
			"balance":      summary.Balance,
			"total_earned": summary.TotalEarned,
			"total_spent":  summary.TotalSpent,
			"transactions": summary.Transactions,
			"tx_limit":     txLimit,
		},
	})
}

// ─────────────────── AdjustBotWallet ───────────────────

// AdjustBotWallet POST /api/admin/llm/bots/:botUserID/wallet/adjust (super admin only).
//
// Adjusts the bot's balance by `amount` coins. Positive = credit (calls
// WalletService.Credit), negative = debit (calls WalletService.Debit). The
// bot-only restriction is enforced by loadBotUser(IsBot=true check).
//
// Returns the new balance after the adjustment plus the ledger row's
// balance_after so the caller can confirm the math.
func (h *ModelWalletAPI) AdjustBotWallet(c *gin.Context) {
	callerID, userType, ok := h.requireAdmin(c)
	if !ok {
		return
	}
	if userType < models.UserTypeSuper {
		c.JSON(http.StatusForbidden, gin.H{
			"code": errcode.ErrPermissionDenied,
			"message": "需要超级管理员权限",
		})
		return
	}
	if h.walletSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": errcode.ErrInternal, "message": "wallet service not wired",
		})
		return
	}

	botUserID := strings.TrimSpace(c.Param("botUserID"))
	if botUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "botUserID required",
		})
		return
	}
	if _, ok := h.loadBotUser(c, botUserID); !ok {
		return
	}

	var req AdjustWalletRequest
	if !decodeJSONStrict(c, &req) {
		return
	}
	if req.Amount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": errcode.ErrValidationFailed, "message": "amount must be non-zero",
		})
		return
	}

	// txType "admin_adjust" (WalletService.TxTypeAdminAdjust). refType="admin"
	// (the caller is an admin), refID=callerID (the admin's user id).
	txType := string(service.TxTypeAdminAdjust)
	refType := "admin"
	refID := callerID
	gameKind := ""  // not a game transaction
	remark := req.Remark

	if req.Amount > 0 {
		if err := h.walletSvc.Credit(c.Request.Context(),
			botUserID, txType, refType, refID, gameKind, remark, req.Amount); err != nil {
			h.respondWalletErr(c, err, "credit failed")
			return
		}
	} else {
		if err := h.walletSvc.Debit(c.Request.Context(),
			botUserID, txType, refType, refID, gameKind, remark, -req.Amount); err != nil {
			h.respondWalletErr(c, err, "debit failed")
			return
		}
	}

	// Re-read the new balance so the caller can verify.
	newBalance, _, _, err := h.walletSvc.GetBalanceWithStats(c.Request.Context(), botUserID)
	if err != nil {
		// Non-fatal — the adjust already committed. Log + return what we have.
		logger.L().Warn("admin adjust wallet: post-balance read failed",
			zap.String("bot_user_id", botUserID), zap.Error(err))
		newBalance = 0
	}

	logger.L().Info("admin adjusted bot wallet",
		zap.String("admin_id", callerID),
		zap.String("bot_user_id", botUserID),
		zap.Int64("amount", req.Amount),
		zap.Int64("new_balance", newBalance),
		zap.String("remark", remark))

	c.JSON(http.StatusOK, gin.H{
		"code": errcode.OK, "message": "ok",
		"data": gin.H{
			"bot_user_id": botUserID,
			"amount":      req.Amount,
			"new_balance": newBalance,
			"remark":      remark,
		},
	})
}

// respondWalletErr maps a wallet-service error to a 4xx/5xx envelope.
// Insufficient balance gets a 400; everything else a 500.
func (h *ModelWalletAPI) respondWalletErr(c *gin.Context, err error, prefix string) {
	ce := errcode.AsError(err)
	httpStatus := http.StatusInternalServerError
	if ce.Code == errcode.ErrWalletInsufficientBalance {
		httpStatus = http.StatusBadRequest
	}
	logger.L().Warn("admin adjust bot wallet failed",
		zap.String("prefix", prefix),
		zap.Error(err))
	c.JSON(httpStatus, gin.H{"code": ce.Code, "message": ce.Message})
}
