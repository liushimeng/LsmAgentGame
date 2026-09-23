// Package middleware — HTTP security baseline (20260923-01 §4.8).
//
// Two middlewares live here:
//
//   - SecurityHeaders: global, always on (even when security.enabled=false —
//     the plan keeps the response-header layer unconditional because headers
//     are pure hardening with zero behavioural risk).
//   - AuthBodyLimit: mounted on /api/auth/* and /api/captcha only; caps the
//     request body before ShouldBindJSON reads it and marks the response
//     no-store so credential material never lands in a shared cache.
//
// Deliberately NOT here: an aggressive CSP (it would break the SPA's inline
// SVG / workers / fonts — listed as a follow-up in the plan doc).
package middleware

import (
	"net/http"
	"strings"

	"LsmAgentGame/config"

	"github.com/gin-gonic/gin"
)

// defaultBodyLimitBytes is the fallback cap for /api/auth/* + /api/captcha
// request bodies when security.body_limit_bytes is unset or non-positive
// (e.g. a config built in a test without applyDefaults).
const defaultBodyLimitBytes = 16384 // 16 KiB

// hstsHeaderValue — 6 months. Only emitted when server.dev_mode=false:
// HSTS + a self-signed local certificate bricks the browser for the whole
// max-age window, which would destroy the dev/AutoTest flow.
const hstsHeaderValue = "max-age=15768000; includeSubDomains"

// SecurityHeaders writes the hardening response headers on every request.
//
//	X-Content-Type-Options: nosniff                     — MIME sniffing off
//	X-Frame-Options: DENY                               — clickjacking
//	Referrer-Policy: strict-origin-when-cross-origin     — token leakage
//	Permissions-Policy: camera=(), microphone=(), geolocation=()
//	X-Robots-Tag: noindex                               — /api/* only
//	Strict-Transport-Security                           — prod only (see above)
//
// cfg may be nil (defensive: unit tests / embedders) — then HSTS is skipped
// and the limit falls back to defaultBodyLimitBytes.
func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	emitHSTS := cfg != nil && !cfg.Server.DevMode
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if emitHSTS {
			c.Header("Strict-Transport-Security", hstsHeaderValue)
		}
		// API responses must never be indexed by crawlers; static SPA
		// routes are left alone (they are the public face of the app).
		if c.Request != nil && strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("X-Robots-Tag", "noindex")
		}
		c.Next()
	}
}

// AuthBodyLimit caps the request body of the auth/captcha endpoints and marks
// them no-store. It must run BEFORE the handler binds JSON — gin's
// ShouldBindJSON reads c.Request.Body, so wrapping it here with
// http.MaxBytesReader makes an oversized payload fail at 16 KiB instead of
// being buffered whole (plan doc A8).
//
// Mount points (router.go): the /api/auth group and the /api/captcha route.
func AuthBodyLimit(cfg *config.Config) gin.HandlerFunc {
	limit := defaultBodyLimitBytes
	if cfg != nil && cfg.Security.BodyLimitBytes > 0 {
		limit = cfg.Security.BodyLimitBytes
	}
	return func(c *gin.Context) {
		// Credential-bearing endpoints: never cacheable, anywhere.
		c.Header("Cache-Control", "no-store")
		if c.Request != nil && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(limit))
		}
		c.Next()
	}
}
