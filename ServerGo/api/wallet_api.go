// Package api — wallet HTTP endpoints.
//
// All routes are protected by middleware.AuthRequired. The authenticated
// user_id is taken from the Gin context — clients cannot query anyone else's
// wallet.
//
//   GET    /api/wallet/balance        — current coin balance
//   GET    /api/wallet/transactions   — paginated ledger (default 20 / max 200)
//   POST   /api/wallet/claim-daily    — manually trigger the UTC+8 daily bonus
//                                      (idempotent; the login flow already
//                                      claims it automatically)
package api

import (
	"net/http"
	"strconv"
	"time"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// WalletAPI is the wallet HTTP resource handler.
type WalletAPI struct {
	svc *service.WalletService
}

// NewWalletAPI wires the handler with its service.
func NewWalletAPI(svc *service.WalletService) *WalletAPI {
	return &WalletAPI{svc: svc}
}

// GetBalance GET /api/wallet/balance.
//
// Returns balance + total_earned + total_spent so the frontend can render
// "累计获得/消耗" without a second call. A missing wallet row resolves to
// all-zero values (defensive — a missing wallet is treated as zero).
func (a *WalletAPI) GetBalance(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	balance, totalEarned, totalSpent, err := a.svc.GetBalanceWithStats(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"user_id":      uid,
			"balance":      balance,
			"total_earned": totalEarned,
			"total_spent":  totalSpent,
		},
	})
}

// ListTransactions GET /api/wallet/transactions?limit=20&offset=0.
func (a *WalletAPI) ListTransactions(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	rows, total, err := a.svc.ListTransactions(c.Request.Context(), uid, limit, offset)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"total":   total,
			"limit":   limit,
			"offset":  offset,
			"entries": rows,
		},
	})
}

// ClaimDaily POST /api/wallet/claim-daily.
//
// Idempotent: the unique key on (user_id, reward_date) guarantees only one
// credit per UTC+8 day. Returns 30014 when already claimed.
func (a *WalletAPI) ClaimDaily(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	balanceAfter, err := a.svc.ClaimDailyReward(c.Request.Context(), uid, time.Now())
	if err != nil {
		ce := errcode.AsError(err)
		if ce.Code == errcode.ErrWalletDailyRewardClaimed {
			c.JSON(http.StatusOK, gin.H{"code": ce.Code, "message": ce.Message})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	logger.L().Info("daily reward claimed via API",
		zap.String("user_id", uid),
		zap.Int64("balance_after", balanceAfter))
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"claimed":       true,
			"amount":        service.DefaultDailyLoginReward, // expose to frontend
			"balance_after": balanceAfter,
		},
	})
}
