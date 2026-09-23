// Package api — captcha HTTP handler.
//
// CaptchaAPI exposes the public /api/captcha endpoint that issues a new
// challenge. The answer is held in the process-local util.CaptchaStore and
// the SVG is rendered inline so the frontend can render it without a
// separate image fetch.
//
// 20260923-01 §4.6 加固（本文件）：
//   - 入口消费 "captcha" 桶令牌 → 超限 429 + 10502 + Retry-After；
//   - 签发改用 store.IssueBound(…, clientIP)：条目带 IP 哈希（校验端
//     VerifyWithIP 在 service 层执行绑定）并受 max_pending 容量驱逐保护；
//   - SVG 改用 util.RenderSVGPuzzle(answer, cfg.Captcha.Decoys) 反爬渲染。
//
// **响应 JSON 形状不变**：{captcha_id, svg, expires_at, length}（§5 契约）。
package api

import (
	"net/http"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// CaptchaAPI issues and (internally) verifies captchas.
type CaptchaAPI struct {
	cfg   *config.Config
	store *util.CaptchaStore
	// limiter 是 "captcha" 桶的 IP 令牌桶（§4.6，main.go 注入）。
	// nil = 未接线（security.enabled=false / 单测）→ 签发限流短路。
	limiter *util.RateLimiter
}

// NewCaptchaAPI wires the handler. limiter may be nil (see AuthAPI.limiter).
func NewCaptchaAPI(cfg *config.Config, store *util.CaptchaStore, limiter *util.RateLimiter) *CaptchaAPI {
	return &CaptchaAPI{cfg: cfg, store: store, limiter: limiter}
}

// Issue POST /api/captcha — returns {captcha_id, svg, expires_at}.
//
// Implementation note: the answer is never sent to the client; only the
// id and the rendered SVG. The answer is stored, single-use, for the
// configured TTL.
func (a *CaptchaAPI) Issue(c *gin.Context) {
	ip := c.ClientIP()
	// §4.6 第 1 步：HTTP 层挡机器洪泛（超限不再签发，不占 store 容量）。
	if !allowBucket(c, a.limiter, captchaRateLimitBucket) {
		return
	}
	ttl := time.Duration(a.cfg.Captcha.TTLSeconds) * time.Second
	length := a.cfg.Captcha.Length
	// §4.6 第 2 步：签发绑定来源 IP（哈希存储）+ max_pending 容量驱逐。
	id, answer, err := a.store.IssueBound(length, ttl, ip)
	if err != nil {
		logger.L().Error("captcha generation failed",
			zap.String("client_ip", ip),
			zap.String("request_id", c.GetString("request_id")),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "captcha generation failed",
		})
		return
	}
	// §4.6 第 3 步：反爬渲染（DOM 乱序 + XML 实体 + decoys + 前景干扰）。
	svg := util.RenderSVGPuzzle(answer, a.cfg.Captcha.Decoys)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"captcha_id": id,
			"svg":        svg,
			"expires_at": time.Now().Add(ttl).Unix(),
			"length":     length,
		},
	})
}
