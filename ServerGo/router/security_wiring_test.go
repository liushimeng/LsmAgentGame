// security_wiring_test.go — 20260923-01 §4.8 接线验证（CLAUDE.md §130）。
//
// router.New 有 25 个 API 参数，本用例只给 health / captcha / auth 三个真正
// 会被调用的入口传实体，其余传 nil（gin 只在**注册期**取方法值，nil 指针
// 接收者不会被解引用）。断言三件事：
//
//  1. SecurityHeaders 挂在全局链上（/api/* 带 X-Robots-Tag，生产带 HSTS）；
//  2. AuthBodyLimit 挂在 /api/auth/*（超限 body 在绑定阶段就 400）；
//  3. SetTrustedProxies(cfg.Security.TrustedProxies) 生效 —— 默认空表 ⇒
//     伪造 X-Forwarded-For 不能改写 ClientIP()（方案 A6 的核心修复）。
package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/api"
	"LsmAgentGame/config"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// newTestRouter 用最小实体构造 engine，并额外挂一个回显 ClientIP 的探针路由
// （gin 的 trustedCIDRs 不导出，只能从行为侧验证）。
func newTestRouter(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	// Health 只读 cfg，不需要 service；Login/Register 在本用例中永远走不到
	// service 调用（body 超限在绑定阶段就被拒）。
	authAPI := api.NewAuthAPI(nil, cfg, nil)
	captchaAPI := api.NewCaptchaAPI(cfg, util.NewCaptchaStore(), nil)
	r := New(cfg, authAPI, nil, captchaAPI, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil)
	r.GET("/api/__probe_clientip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	return r
}

func baseConfig(devMode bool, trustedProxies []string) *config.Config {
	cfg := &config.Config{}
	cfg.Server.DevMode = devMode
	cfg.Security.TrustedProxies = trustedProxies
	cfg.Security.BodyLimitBytes = 32
	cfg.Captcha.TTLSeconds = 180
	cfg.Captcha.Length = 5
	cfg.Captcha.Decoys = 2
	// gin-contrib/cors 在 allow-list 为空时会 panic（"all origins disabled"），
	// 因此测试配置必须给出至少一个来源——与运行态 conf 一致。
	cfg.CORS.AllowedOrigins = []string{"https://localhost:39001"}
	return cfg
}

func TestNew_SecurityHeadersAreMountedGlobally(t *testing.T) {
	r := newTestRouter(t, baseConfig(false, []string{}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/health status = %d, want 200", w.Code)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"X-Robots-Tag":              "noindex",
		"Strict-Transport-Security": "max-age=15768000; includeSubDomains",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestNew_AuthBodyLimitIsMounted(t *testing.T) {
	r := newTestRouter(t, baseConfig(true, []string{}))

	// 32 字节上限，投喂 200 字节：必须在 ShouldBindJSON 阶段失败（400 +
	// 20001），而不是进到 handler 逻辑（那会因 svc==nil panic → 500）。
	w := httptest.NewRecorder()
	body := `{"account":"` + strings.Repeat("A", 200) + `","password":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized /api/auth/login body: status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "20001") {
		t.Errorf("body = %s, want validation code 20001", w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	// /api/captcha 单独包了一层 AuthBodyLimit（同样 no-store）。
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/captcha", strings.NewReader("{}")))
	if w2.Code != http.StatusOK {
		t.Fatalf("/api/captcha status = %d, want 200", w2.Code)
	}
	if got := w2.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("/api/captcha Cache-Control = %q, want no-store", got)
	}
	// 签发响应形状不变（§5 契约）：captcha_id / svg / expires_at / length。
	for _, field := range []string{"captcha_id", "svg", "expires_at", "length"} {
		if !strings.Contains(w2.Body.String(), field) {
			t.Errorf("/api/captcha response lost field %q: %s", field, w2.Body.String())
		}
	}
}

func TestNew_TrustedProxiesIgnoreSpoofedXFF(t *testing.T) {
	// 默认（security.trusted_proxies=[]）：XFF 不可信 ⇒ ClientIP = 真实 socket IP。
	r := newTestRouter(t, baseConfig(false, []string{}))
	req := httptest.NewRequest(http.MethodGet, "/api/__probe_clientip", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if got := strings.TrimSpace(w.Body.String()); got != "203.0.113.7" {
		t.Fatalf("ClientIP with an empty trusted_proxies list = %q, want the socket IP 203.0.113.7", got)
	}

	// 显式把反代网段写进配置后，XFF 才被采信（运维部署在反代后面时用）。
	r2 := newTestRouter(t, baseConfig(false, []string{"203.0.113.0/24"}))
	req2 := httptest.NewRequest(http.MethodGet, "/api/__probe_clientip", nil)
	req2.RemoteAddr = "203.0.113.7:54321"
	req2.Header.Set("X-Forwarded-For", "1.2.3.4")
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	if got := strings.TrimSpace(w2.Body.String()); got != "1.2.3.4" {
		t.Fatalf("ClientIP with the proxy CIDR trusted = %q, want the XFF value 1.2.3.4", got)
	}
}
