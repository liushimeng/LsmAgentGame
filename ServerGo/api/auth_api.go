// Package api holds Gin HTTP handlers. One file per resource.
//
// auth_api.go provides /api/health and the auth endpoints implemented by
// service.AuthService (register / login / refresh / logout). Login,
// register, and refresh all set an AES-GCM-encrypted HttpOnly cookie
// (configurable name / TTL / Secure flag — defaults: lsm_auth, 48h, Secure).
//
// 20260923-01 §4.5 加固（本文件）：
//   - Login/Register 入口消费 "login" 桶令牌（IP 突发防护）→ 超限 429/10502；
//   - 错误码 → HTTP 状态映射收敛：10501/10502 → 429（10501 带 Retry-After），
//     10102（密码错 / 账号不存在，已由 service 层均一化）由 401 → **400**，
//     验证码类 1030x 与其余业务码保持 400；
//   - 失败/成功日志补 request_id + client_ip（CLAUDE.md §7）。
//
// 成功路径与 setAuthCookie / token 下发**零改动**（§5 契约）。
package api

import (
	"net/http"
	"regexp"
	"strconv"
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
	// limiter 是 "login" 桶的 IP 令牌桶（20260923-01 §4.5，main.go 注入）。
	// nil = 未接线（security.enabled=false / 单测）→ 入口限流整体短路。
	limiter *util.RateLimiter
}

// NewAuthAPI wires the handler with its service. limiter may be nil — every
// use site nil-checks, so a disabled security section (or a unit test that
// passes nil) behaves exactly like the pre-hardening code.
func NewAuthAPI(svc *service.AuthService, cfg *config.Config, limiter *util.RateLimiter) *AuthAPI {
	return &AuthAPI{svc: svc, cfg: cfg, limiter: limiter}
}

// ── 20260923-01 §4.5 节流与状态码映射（api 包内共享，captcha_api.go 复用）──

// authRateLimitBucket 是 /api/auth/* 在 util.RateLimiter 注册的桶名。
const authRateLimitBucket = "login"

// captchaRateLimitBucket 是 /api/captcha 的桶名（§4.6）。
const captchaRateLimitBucket = "captcha"

// retryAfterMsgRe 从 service 层 10501 消息（"…retry after 42s"）里回捞秒数，
// 作为 guard 剩余时长已归零时的兜底（前端倒计时与 Retry-After 同口径）。
var retryAfterMsgRe = regexp.MustCompile(`retry after (\d+)s`)

// denyRateLimited 写出 429 + 10502 的统一限流响应。retryAfter>0 时附带
// Retry-After 头，前端 LoginForm 据此倒计时（§5 契约）。
func denyRateLimited(c *gin.Context, retryAfter time.Duration) {
	if secs := util.CeilSeconds(retryAfter); secs > 0 {
		c.Header("Retry-After", strconv.Itoa(secs))
	}
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"code":    errcode.ErrAuthRateLimited,
		"message": errcode.DefaultMessages[errcode.ErrAuthRateLimited],
	})
}

// allowBucket 消费一个 IP 令牌；超限则写好 429 响应并返回 false。
// limiter==nil（security 关闭 / 单测）或 ip=="" 时永远放行。
func allowBucket(c *gin.Context, limiter *util.RateLimiter, bucket string) bool {
	if limiter == nil {
		return true
	}
	ip := c.ClientIP()
	ok, retry := limiter.Allow(bucket, ip)
	if ok {
		return true
	}
	logger.L().Warn("rate limited",
		zap.String("bucket", bucket),
		zap.String("path", c.Request.URL.Path),
		zap.String("client_ip", ip),
		zap.String("request_id", c.GetString("request_id")),
		zap.Int("retry_after_seconds", util.CeilSeconds(retry)))
	denyRateLimited(c, retry)
	return false
}

// authErrorStatus 是 §4.5 的错误码 → HTTP 状态映射表。
//
//	10501（锁定）/ 10502（限流） → 429
//	其余一律 400：10102（credential，含由 10101 收敛而来的账号不存在）、
//	1030x（验证码）、10103/10104/10105、10202、20001、40002 …
//
// 10102 从 401 收敛到 400 修掉方案 A9：前端 http.ts 把**任何 401** 当作
// 「会话过期」清 store + 弹 toast，密码输错的用户会看到 [10102]
// SESSION_EXPIRED 乱码；同时把「鉴权端点」指纹从 401 挪走。
func authErrorStatus(code int) int {
	switch code {
	case errcode.ErrAuthTooManyAttempts, errcode.ErrAuthRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusBadRequest
	}
}

// retryAfterSeconds 计算 10501 的 Retry-After：优先取 guard 的剩余锁定时长
// （向上取整），其次解析消息里的 "retry after Ns"，最后兜底 30s（与前端
// LoginForm 缺省一致，保证倒计时永不显示 0）。
func retryAfterSeconds(remaining time.Duration, msg string) int {
	if secs := util.CeilSeconds(remaining); secs > 0 {
		return secs
	}
	if m := retryAfterMsgRe.FindStringSubmatch(msg); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n
		}
	}
	return 30
}

// respondAuthError 写出认证类错误响应；10501 额外带 Retry-After 头。
func respondAuthError(c *gin.Context, ce *errcode.Error, lockRemaining time.Duration) {
	if ce.Code == errcode.ErrAuthTooManyAttempts {
		c.Header("Retry-After", strconv.Itoa(retryAfterSeconds(lockRemaining, ce.Message)))
	}
	c.JSON(authErrorStatus(ce.Code), gin.H{"code": ce.Code, "message": ce.Message})
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
	ip := c.ClientIP()
	rid := c.GetString("request_id")
	// §4.5 入口突发限流（limiter==nil 时短路）。
	if !allowBucket(c, a.limiter, authRateLimitBucket) {
		return
	}
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
		// §4.4：IP 维度锁定键（空串 → 不可键，进程内调用零感知）。
		IP: ip,
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Warn("register failed",
			zap.String("account", req.Account),
			zap.Int("code", ce.Code),
			zap.String("request_id", rid),
			zap.String("client_ip", ip))
		// Retry-After 与 Login 同一键推导（account/phone 优先 phone，见
		// util.LoginGuardKeyAccount）——RegisterInput 与 LoginInput 的键完全一致。
		respondAuthError(c, ce, a.svc.LockoutRetryAfter(service.LoginInput{
			Account: req.Account,
			Phone:   req.Phone,
			IP:      ip,
		}))
		return
	}
	a.setAuthCookie(c, resp.CookieValue)
	logger.L().Info("register ok",
		zap.String("account", req.Account),
		zap.String("user_id", resp.UserID),
		zap.String("request_id", rid),
		zap.String("client_ip", ip))
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": resp})
}

// Login POST /api/auth/login
//
// Request body:
//   - {account, password, captcha_id, captcha_answer}
//   - Or phone login: {phone, password, captcha_id, captcha_answer}
//     (2026-08-25 起所有账号均需 CAPTCHA，无旁路)
func (a *AuthAPI) Login(c *gin.Context) {
	ip := c.ClientIP()
	rid := c.GetString("request_id")
	// §4.5 入口突发限流：先挡批量机器流量，再进 body 解析与 bcrypt。
	if !allowBucket(c, a.limiter, authRateLimitBucket) {
		return
	}
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
		IP:            ip,
		UA:            c.Request.UserAgent(),
	})
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Warn("login failed",
			zap.String("account", req.Account),
			zap.String("phone", req.Phone),
			zap.Int("code", ce.Code),
			zap.String("request_id", rid),
			zap.String("client_ip", ip))
		// 10501 的 Retry-After 复用 service 的键推导（§4.4 导出方法）；
		// 其余错误码 LockoutRetryAfter 返回 0，不写头。
		respondAuthError(c, ce, a.svc.LockoutRetryAfter(service.LoginInput{
			Account: req.Account,
			Phone:   req.Phone,
			IP:      ip,
		}))
		return
	}
	a.setAuthCookie(c, resp.CookieValue)
	logger.L().Info("login ok",
		zap.String("account", req.Account),
		zap.String("phone", req.Phone),
		zap.String("user_id", resp.UserID),
		zap.String("request_id", rid),
		zap.String("client_ip", ip))
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
