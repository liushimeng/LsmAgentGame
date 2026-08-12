// Package api holds Gin HTTP handlers. One file per resource.
//
// user_api.go provides the protected per-user preference endpoints backed by
// service.UserService:
//   GET   /api/user/profile   — current user's id / account / language
//   PATCH /api/user/language  — persist the UI language preference
package api

import (
	"net/http"

	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// UserAPI is the user-preference resource handler.
type UserAPI struct {
	svc *service.UserService
}

// NewUserAPI wires the handler with its service.
func NewUserAPI(svc *service.UserService) *UserAPI {
	return &UserAPI{svc: svc}
}

// uidFromContext extracts the authenticated user id set by middleware.AuthRequired.
func uidFromContext(c *gin.Context) string {
	uid, _ := c.Get("user_id")
	s, _ := uid.(string)
	return s
}

// GetProfile GET /api/user/profile (protected).
func (a *UserAPI) GetProfile(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	user, err := a.svc.GetProfile(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	// Who registered using this user's personal invite code.
	referrals, err := a.svc.ListReferrals(c.Request.Context(), uid)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"user_id":          user.ID,
			"account":          user.Account,
			"nickname":         user.Nickname,
			"language":         user.Language,
			"my_invite_code":   user.MyInviteCode,
			"referral_count":   user.ReferralCount,
			"referrer_user_id": user.ReferrerUserID,
			"referrals":        referrals,
		},
	})
}

// UpdateLanguage PATCH /api/user/language (protected).
func (a *UserAPI) UpdateLanguage(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	var req struct {
		Language string `json:"language" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": err.Error(),
		})
		return
	}
	if err := a.svc.UpdateLanguage(c.Request.Context(), uid, req.Language); err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	logger.L().Info("user language updated",
		zap.String("user_id", uid),
		zap.String("language", req.Language))
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"language": req.Language},
	})
}

// UpdateNickname PATCH /api/user/nickname (protected).
func (a *UserAPI) UpdateNickname(c *gin.Context) {
	uid := uidFromContext(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	var req struct {
		Nickname string `json:"nickname" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": err.Error(),
		})
		return
	}
	if err := a.svc.UpdateNickname(c.Request.Context(), uid, req.Nickname); err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	logger.L().Info("user nickname updated",
		zap.String("user_id", uid),
		zap.String("nickname", req.Nickname))
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    gin.H{"nickname": req.Nickname},
	})
}
