// Package api — captcha HTTP handler.
//
// CaptchaAPI exposes the public /api/captcha endpoint that issues a new
// challenge. The answer is held in the process-local util.CaptchaStore and
// the SVG is rendered inline so the frontend can render it without a
// separate image fetch.
package api

import (
	"net/http"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
)

// CaptchaAPI issues and (internally) verifies captchas.
type CaptchaAPI struct {
	cfg     *config.Config
	store   *util.CaptchaStore
}

// NewCaptchaAPI wires the handler.
func NewCaptchaAPI(cfg *config.Config, store *util.CaptchaStore) *CaptchaAPI {
	return &CaptchaAPI{cfg: cfg, store: store}
}

// Issue POST /api/captcha — returns {captcha_id, svg, expires_at}.
//
// Implementation note: the answer is never sent to the client; only the
// id and the rendered SVG. The answer is stored, single-use, for the
// configured TTL.
func (a *CaptchaAPI) Issue(c *gin.Context) {
	ttl := time.Duration(a.cfg.Captcha.TTLSeconds) * time.Second
	length := a.cfg.Captcha.Length
	id, answer, err := a.store.Issue(length, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    errcode.ErrInternal,
			"message": "captcha generation failed",
		})
		return
	}
	svg := util.RenderSVGCode(answer)
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data": gin.H{
			"captcha_id":  id,
			"svg":         svg,
			"expires_at":  time.Now().Add(ttl).Unix(),
			"length":      length,
		},
	})
}
