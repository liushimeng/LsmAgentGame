// security_test.go — 20260923-01 §4.8 单测。
//
// 覆盖：SecurityHeaders 的五条固定头 + /api/ 前缀专属的 X-Robots-Tag +
// HSTS 仅在生产（dev_mode=false）下发 + nil cfg 不 panic；
// AuthBodyLimit 的请求体上限（在 ShouldBindJSON 之前生效）与 no-store。
package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/config"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// newEngine builds a minimal engine: mw as a global middleware plus one API
// route and one static-ish route so prefix behaviour can be compared.
func newEngine(mw gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	if mw != nil {
		r.Use(mw)
	}
	r.POST("/api/auth/login", func(c *gin.Context) {
		var body struct {
			Account string `json:"account"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 20001, "message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "account": body.Account})
	})
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "spa") })
	return r
}

func TestSecurityHeaders_AlwaysOn(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.DevMode = true
	r := newEngine(SecurityHeaders(cfg))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	// 非 /api/ 路径不下发 X-Robots-Tag（SPA 首页是公开门面）。
	if got := w.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("X-Robots-Tag on a non-API path = %q, want empty", got)
	}
	// dev_mode=true → 不下发 HSTS（自签证书 + HSTS 会锁死本地浏览器）。
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must be absent in dev_mode, got %q", got)
	}
}

func TestSecurityHeaders_RobotsTagOnlyForAPI(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.DevMode = true
	r := newEngine(SecurityHeaders(cfg))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"account":"a"}`)))
	if got := w.Header().Get("X-Robots-Tag"); got != "noindex" {
		t.Errorf("X-Robots-Tag on /api/* = %q, want noindex", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff missing on /api/*, got %q", got)
	}
}

func TestSecurityHeaders_HSTSOnlyInProduction(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.DevMode = false
	r := newEngine(SecurityHeaders(cfg))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	got := w.Header().Get("Strict-Transport-Security")
	if got != "max-age=15768000; includeSubDomains" {
		t.Errorf("HSTS = %q, want max-age=15768000; includeSubDomains", got)
	}
}

func TestSecurityHeaders_NilConfigDoesNotPanic(t *testing.T) {
	r := newEngine(SecurityHeaders(nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("nil cfg must not emit HSTS, got %q", got)
	}
}

func TestAuthBodyLimit_RejectsOversizedBody(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.BodyLimitBytes = 32
	r := newEngine(AuthBodyLimit(cfg))

	w := httptest.NewRecorder()
	// 32 字节上限，投喂 200 字节 → ShouldBindJSON 必须在读取阶段失败。
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"account":"`+strings.Repeat("A", 200)+`"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body must be rejected before binding)", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestAuthBodyLimit_AllowsBodyWithinLimit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.BodyLimitBytes = 32
	r := newEngine(AuthBodyLimit(cfg))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"account":"abc"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a body within the limit", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestAuthBodyLimit_FallsBackTo16KiB(t *testing.T) {
	cfg := &config.Config{} // BodyLimitBytes 零值 → defaultBodyLimitBytes
	r := newEngine(AuthBodyLimit(cfg))

	// 16 KiB 以内：放行。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"account":"`+strings.Repeat("A", 1024)+`"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("1 KiB body: status = %d, want 200", w.Code)
	}
	// 超过 16 KiB：拒绝。
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"account":"`+strings.Repeat("A", 20000)+`"}`)))
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("20 KiB body: status = %d, want 400", w2.Code)
	}
}

func TestAuthBodyLimit_NilConfigDoesNotPanic(t *testing.T) {
	r := newEngine(AuthBodyLimit(nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"account":"abc"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
