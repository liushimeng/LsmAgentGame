// Package api holds Gin HTTP handlers. One file per resource.
//
// auth_api.go provides /api/health and the auth endpoints implemented by
// service.AuthService (register / login / refresh / logout). Login,
// register, and refresh all set an AES-GCM-encrypted HttpOnly cookie
// (configurable name / TTL / Secure flag — defaults: lsm_auth, 48h, Secure).
package api

import (
	"net/http"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/service"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AuthAPI is the auth resource handler.
type AuthAPI struct {
	svc *service.AuthService
	cfg *config.Config
}

// NewAuthAPI wires the handler with its service.
func NewAuthAPI(svc *service.AuthService, cfg *config.Config) *AuthAPI {
	return &AuthAPI{svc: svc, cfg: cfg}
}

// cookieTTL is a small helper used by all three endpoints that set the cookie.
func (a *AuthAPI) cookieTTL() time.Duration {
	return time.Duration(a.cfg.Cookie.TTLSeconds) * time.Second
}

// setAuthCookie writes the encrypted auth cookie onto the response.
func (a *AuthAPI) setAuthCookie(c *gin.Context, value string) {
	if value == "" {
		return
	}
	cookie := util.BuildAuthCookie(a.cfg.Cookie.Name, value, a.cookieTTL(), a.cfg.Cookie.Secure)
	http.SetCookie(c.Writer, cookie)
}

// clearAuthCookie instructs the browser to drop the cookie immediately.
func (a *AuthAPI) clearAuthCookie(c *gin.Context) {
	http.SetCookie(c.Writer, util.BuildClearCookie(a.cfg.Cookie.Name, a.cfg.Cookie.Secure))
}

// Health responds with a tiny JSON document. Use it for readiness probes.
func (a *AuthAPI) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": errcode.DefaultMessages[errcode.OK],
		"data": gin.H{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// Register POST /api/auth/register
//
// Invitation model: the registration form takes ONE invite field — the
// personal invite code (MyInviteCode) of an existing user who referred this
// registrant. There is no admin-managed "gate" code.
func (a *AuthAPI) Register(c *gin.Context) {
	var req struct {
		Account      string `json:"account"        binding:"required,min=3,max=32"`
		Password     string `json:"password"       binding:"required,min=6,max=64"`
		Nickname     string `json:"nickname"`
		Phone        string `json:"phone"`
		Email        string `json:"email"          binding:"omitempty,email"`
		ReferrerCode string `json:"referrer_code"  binding:"required,min=8,max=32"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": err.Error(),
		})
		return
	}
	resp, err := a.svc.Register(c.Request.Context(), service.RegisterInput{
		Account:      req.Account,
		Password:     req.Password,
		Nickname:     req.Nickname,
		Phone:        req.Phone,
		Email:        req.Email,
		ReferrerCode: req.ReferrerCode,
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Warn("register failed",
			zap.String("account", req.Account),
			zap.Int("code", ce.Code))
		c.JSON(http.StatusBadRequest, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	a.setAuthCookie(c, resp.CookieValue)
	logger.L().Info("register ok",
		zap.String("account", req.Account),
		zap.String("user_id", resp.UserID))
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": resp})
}

// Login POST /api/auth/login
//
// Request body:
//   - For real users: {account|captcha_id, password, captcha_answer}
//   - For agent-bypass (account = test19082jauishf8): {account, password}
//   - Or phone login: {phone, password, captcha_id, captcha_answer}
func (a *AuthAPI) Login(c *gin.Context) {
	var req struct {
		Account       string `json:"account"`
		Phone         string `json:"phone"`
		Password      string `json:"password"        binding:"required"`
		CaptchaID     string `json:"captcha_id"`
		CaptchaAnswer string `json:"captcha_answer"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    errcode.ErrValidationFailed,
			"message": err.Error(),
		})
		return
	}
	resp, err := a.svc.Login(c.Request.Context(), service.LoginInput{
		Account:       req.Account,
		Phone:         req.Phone,
		Password:      req.Password,
		CaptchaID:     req.CaptchaID,
		CaptchaAnswer: req.CaptchaAnswer,
		IP:            c.ClientIP(),
		UA:            c.Request.UserAgent(),
	})
	if err != nil {
		ce := errcode.AsError(err)
		// Captcha errors are validation-grade; respond 400 so clients can re-render.
		status := http.StatusUnauthorized
		if ce.Code == errcode.ErrAuthCaptchaMissing || ce.Code == errcode.ErrAuthCaptchaWrong || ce.Code == errcode.ErrAuthCaptchaExpired {
			status = http.StatusBadRequest
		}
		logger.L().Warn("login failed",
			zap.String("account", req.Account),
			zap.String("phone", req.Phone),
			zap.Int("code", ce.Code))
		c.JSON(status, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	a.setAuthCookie(c, resp.CookieValue)
	logger.L().Info("login ok",
		zap.String("account", req.Account),
		zap.String("phone", req.Phone),
		zap.String("user_id", resp.UserID))
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": resp})
}

// Refresh POST /api/auth/refresh (protected) — also re-issues the cookie.
func (a *AuthAPI) Refresh(c *gin.Context) {
	uid, _ := c.Get("user_id")
	uidStr, _ := uid.(string)
	if uidStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    errcode.ErrAuthMissingToken,
			"message": errcode.DefaultMessages[errcode.ErrAuthMissingToken],
		})
		return
	}
	resp, err := a.svc.Refresh(c.Request.Context(), uidStr)
	if err != nil {
		ce := errcode.AsError(err)
		c.JSON(http.StatusUnauthorized, gin.H{"code": ce.Code, "message": ce.Message})
		return
	}
	a.setAuthCookie(c, resp.CookieValue)
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": resp})
}

// Logout POST /api/auth/logout — clears the auth cookie. No-body.
//
// No token check here — clearing the cookie is idempotent. Client code
// must call setAuthToken(null) and clear local state after this returns.
func (a *AuthAPI) Logout(c *gin.Context) {
	a.clearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": gin.H{}})
}
