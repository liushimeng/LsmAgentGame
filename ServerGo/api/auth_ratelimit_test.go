// auth_ratelimit_test.go — 20260923-01 §4.5 单测（api 层节流与状态码映射）。
//
// AuthService 需要 gorm/DB，因此本文件只覆盖 api 层的纯逻辑与 gin 上下文
// 行为：错误码 → HTTP 状态映射（10102 由 401 收敛到 400 是方案 A9 的修复）、
// Retry-After 秒数推导优先级、allowBucket 的 429/10502 响应形状、
// limiter==nil 时的短路。
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// testCtx 造一个带请求的 gin.Context（allowBucket 会读 URL.Path / ClientIP）。
func testCtx(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	c.Request.RemoteAddr = "203.0.113.7:54321"
	return c, w
}

func TestAuthErrorStatus_Mapping(t *testing.T) {
	cases := []struct {
		code int
		want int
	}{
		// 节流 / 锁定 → 429（§4.5）。
		{errcode.ErrAuthTooManyAttempts, http.StatusTooManyRequests},
		{errcode.ErrAuthRateLimited, http.StatusTooManyRequests},
		// credential 类：10102 由 401 收敛到 400（消除 A9 前端误吞 SESSION_EXPIRED）。
		{errcode.ErrAuthPasswordWrong, http.StatusBadRequest},
		{errcode.ErrAuthAccountNotFound, http.StatusBadRequest},
		// 验证码类保持 400。
		{errcode.ErrAuthCaptchaMissing, http.StatusBadRequest},
		{errcode.ErrAuthCaptchaWrong, http.StatusBadRequest},
		{errcode.ErrAuthCaptchaExpired, http.StatusBadRequest},
		// 其余业务码统一 400。
		{errcode.ErrAuthTokenExpired, http.StatusBadRequest},
		{errcode.ErrAuthInvalidToken, http.StatusBadRequest},
		{errcode.ErrAuthMissingToken, http.StatusBadRequest},
		{errcode.ErrValidationFailed, http.StatusBadRequest},
		{errcode.ErrDB, http.StatusBadRequest},
	}
	for _, c := range cases {
		if got := authErrorStatus(c.code); got != c.want {
			t.Errorf("authErrorStatus(%d) = %d, want %d", c.code, got, c.want)
		}
	}
	// 关键回归锚点：credential 失败绝不返回 401。
	if authErrorStatus(errcode.ErrAuthPasswordWrong) == http.StatusUnauthorized {
		t.Fatal("10102 must not map to 401 — the frontend treats any 401 as session expiry")
	}
}

func TestRetryAfterSeconds_Precedence(t *testing.T) {
	// ① guard 剩余时长优先（向上取整）。
	if got := retryAfterSeconds(1500*time.Millisecond, "too many failed attempts, retry after 99s"); got != 2 {
		t.Errorf("remaining wins: got %d, want 2", got)
	}
	// ② 剩余时长归零 → 回捞消息里的秒数。
	if got := retryAfterSeconds(0, "too many failed attempts, retry after 99s"); got != 99 {
		t.Errorf("message fallback: got %d, want 99", got)
	}
	// ③ 两者都没有 → 兜底 30s（与前端 LoginForm 缺省一致）。
	if got := retryAfterSeconds(0, "too many failed attempts, retry later"); got != 30 {
		t.Errorf("default fallback: got %d, want 30", got)
	}
	if got := retryAfterSeconds(0, ""); got != 30 {
		t.Errorf("empty message fallback: got %d, want 30", got)
	}
	// ④ 消息里的 0 不可用 → 兜底。
	if got := retryAfterSeconds(0, "retry after 0s"); got != 30 {
		t.Errorf("zero seconds in message: got %d, want 30", got)
	}
}

func TestRespondAuthError_LockoutCarriesRetryAfter(t *testing.T) {
	c, w := testCtx(http.MethodPost, "/api/auth/login")
	respondAuthError(c, errcode.CodeMsg(errcode.ErrAuthTooManyAttempts,
		"too many failed attempts, retry after 42s"), 41*time.Second+400*time.Millisecond)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q, want 42 (ceil of the remaining lockout)", got)
	}
	if !strings.Contains(w.Body.String(), `"code":10501`) {
		t.Errorf("body = %s, want code 10501", w.Body.String())
	}
}

func TestRespondAuthError_CredentialFailureIs400WithoutRetryAfter(t *testing.T) {
	c, w := testCtx(http.MethodPost, "/api/auth/login")
	respondAuthError(c, errcode.Code(errcode.ErrAuthPasswordWrong), 0)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After = %q, want empty for a credential failure", got)
	}
	if !strings.Contains(w.Body.String(), `"code":10102`) {
		t.Errorf("body = %s, want code 10102", w.Body.String())
	}
}

func TestAllowBucket_DeniesWith429AndRetryAfter(t *testing.T) {
	lim := util.NewRateLimiter(time.Second, util.RateBucketSpec{Name: authRateLimitBucket, PerMinute: 60, Burst: 2})

	for i := 0; i < 2; i++ {
		c, w := testCtx(http.MethodPost, "/api/auth/login")
		if !allowBucket(c, lim, authRateLimitBucket) {
			t.Fatalf("attempt %d within burst=2 was denied", i+1)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d wrote status %d, want the default 200 (nothing written)", i+1, w.Code)
		}
	}

	c, w := testCtx(http.MethodPost, "/api/auth/login")
	if allowBucket(c, lim, authRateLimitBucket) {
		t.Fatal("attempt 3 must be denied once the burst is exhausted")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":10502`) {
		t.Errorf("body = %s, want code 10502", w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q, want a positive number of seconds", got)
	}
	if !c.IsAborted() {
		t.Error("the context must be aborted so the handler never runs")
	}
}

func TestAllowBucket_NilLimiterShortCircuits(t *testing.T) {
	// security.enabled=false / 单测：limiter==nil ⇒ 永远放行且不写响应。
	for i := 0; i < 100; i++ {
		c, w := testCtx(http.MethodPost, "/api/captcha")
		if !allowBucket(c, nil, captchaRateLimitBucket) {
			t.Fatalf("attempt %d denied with a nil limiter", i+1)
		}
		if w.Code != http.StatusOK || c.IsAborted() {
			t.Fatalf("attempt %d: code=%d aborted=%v, want 200/false", i+1, w.Code, c.IsAborted())
		}
	}
}

func TestAllowBucket_EmptyClientIPPasses(t *testing.T) {
	lim := util.NewRateLimiter(time.Second, util.RateBucketSpec{Name: captchaRateLimitBucket, PerMinute: 1, Burst: 1})
	for i := 0; i < 5; i++ {
		c, _ := testCtx(http.MethodPost, "/api/captcha")
		c.Request.RemoteAddr = "" // ClientIP() → ""（不可键）
		if !allowBucket(c, lim, captchaRateLimitBucket) {
			t.Fatalf("attempt %d with an empty client IP must pass", i+1)
		}
	}
}
