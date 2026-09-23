// origin_guard_test.go — 20260923-01 §4.7 单测。
//
// 覆盖：Origin 白名单五条规则（无头放行 / 同源 / 配置白名单 / dev 通配 /
// 其余拒绝）、默认端口归一化、upgraderFor 的按 cfg 缓存、remoteIP 解析、
// ServeWS 的升级限流（429+10502+Retry-After，且发生在 JWT 解析之前）与
// 单用户并发连接数上限。
package ws

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/util"
)

// originRequest 造一个带 Origin 头的握手请求（host 为 r.Host）。
func originRequest(origin, host string, tlsConn bool) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ws?token=x", nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if tlsConn {
		r.TLS = &tls.ConnectionState{}
	}
	return r
}

func TestOriginAllowed_NoOriginHeaderPasses(t *testing.T) {
	cfg := &config.Config{} // dev_mode=false，最严格
	if !originAllowed(cfg, originRequest("", "game.example.com:39001", true)) {
		t.Fatal("a request without an Origin header must pass (non-browser clients)")
	}
}

func TestOriginAllowed_SameOriginPasses(t *testing.T) {
	cfg := &config.Config{}
	cases := []struct{ origin, host string }{
		{"https://game.example.com:39001", "game.example.com:39001"},
		{"https://GAME.Example.COM:39001", "game.example.com:39001"}, // 大小写不敏感
		{"https://game.example.com", "game.example.com"},
	}
	for _, c := range cases {
		if !originAllowed(cfg, originRequest(c.origin, c.host, true)) {
			t.Errorf("same-origin %q vs host %q must pass", c.origin, c.host)
		}
	}
	// 默认端口归一化：https://host:443 == Host: "host"（TLS 请求）。
	if !originAllowed(cfg, originRequest("https://game.example.com:443", "game.example.com", true)) {
		t.Error("https default port 443 must normalize to the bare host")
	}
	// 非 TLS + http 默认端口 80 同理。
	if !originAllowed(cfg, originRequest("http://game.example.com:80", "game.example.com", false)) {
		t.Error("http default port 80 must normalize to the bare host")
	}
	// 端口不同 ⇒ 非同源（且不在白名单）⇒ 拒绝。
	if originAllowed(cfg, originRequest("https://game.example.com:8443", "game.example.com:39001", true)) {
		t.Error("a different port is not same-origin and must be rejected")
	}
}

func TestOriginAllowed_ConfiguredAllowLists(t *testing.T) {
	cfg := &config.Config{}
	cfg.CORS.AllowedOrigins = []string{"https://lobby.example.com"}
	cfg.Security.WSGuard.AllowedOrigins = []string{"https://mirror.example.com:39001"}

	if !originAllowed(cfg, originRequest("https://lobby.example.com", "game.example.com:39001", true)) {
		t.Error("an origin listed in cors.allowed_origins must pass")
	}
	if !originAllowed(cfg, originRequest("https://mirror.example.com:39001", "game.example.com:39001", true)) {
		t.Error("an origin listed in security.ws_guard.allowed_origins must pass")
	}
	// 精确匹配：子域/后缀不得蒙混过关。
	for _, bad := range []string{
		"https://evil.lobby.example.com",
		"https://lobby.example.com.evil.test",
		"http://lobby.example.com", // scheme 不同
		"https://evil.test",
	} {
		if originAllowed(cfg, originRequest(bad, "game.example.com:39001", true)) {
			t.Errorf("origin %q must be rejected (exact match only)", bad)
		}
	}
}

func TestOriginAllowed_DevModeLocalhostWildcard(t *testing.T) {
	dev := &config.Config{}
	dev.Server.DevMode = true
	for _, origin := range []string{
		"http://localhost:5173",
		"http://localhost:5174",
		"http://127.0.0.1:5173",
		"https://localhost:39001",
	} {
		if !originAllowed(dev, originRequest(origin, "127.0.0.1:39001", false)) {
			t.Errorf("dev_mode must accept local origin %q (Vite dev server)", origin)
		}
	}
	// dev 通配不放开外网源。
	if originAllowed(dev, originRequest("https://evil.test", "127.0.0.1:39001", false)) {
		t.Error("dev_mode must not accept a foreign origin")
	}
	// 生产（dev_mode=false）下 localhost 跨源不再放行。
	prod := &config.Config{}
	if originAllowed(prod, originRequest("http://localhost:5173", "game.example.com:39001", true)) {
		t.Error("localhost wildcard must be dev-only")
	}
}

func TestOriginAllowed_RejectsGarbageAndNil(t *testing.T) {
	cfg := &config.Config{}
	if originAllowed(cfg, originRequest("https://exa mple.com", "game.example.com:39001", true)) {
		t.Error("an unparseable Origin must be rejected")
	}
	if originAllowed(cfg, nil) {
		t.Error("a nil request must be rejected")
	}
	// cfg==nil（未接线的测试路径）：仅保留无头/同源两条规则。
	if !originAllowed(nil, originRequest("", "game.example.com:39001", true)) {
		t.Error("nil cfg must still accept a request without an Origin header")
	}
	if !originAllowed(nil, originRequest("https://game.example.com:39001", "game.example.com:39001", true)) {
		t.Error("nil cfg must still accept same-origin")
	}
	if originAllowed(nil, originRequest("https://evil.test", "game.example.com:39001", true)) {
		t.Error("nil cfg must reject a foreign origin")
	}
}

func TestUpgraderFor_CachesPerConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.DevMode = true
	u1 := upgraderFor(cfg)
	u2 := upgraderFor(cfg)
	if u1 != u2 {
		t.Fatal("upgraderFor must return the cached instance for the same cfg pointer")
	}
	if u1.CheckOrigin == nil {
		t.Fatal("CheckOrigin must be wired")
	}
	// CheckOrigin 必须委托给 originAllowed(cfg, …)。
	if !u1.CheckOrigin(originRequest("http://localhost:5173", "127.0.0.1:39001", false)) {
		t.Error("cached upgrader must honour the dev localhost wildcard")
	}
	if u1.CheckOrigin(originRequest("https://evil.test", "127.0.0.1:39001", false)) {
		t.Error("cached upgrader must reject a foreign origin")
	}
	// 不同 cfg → 不同实例（策略互不串味）。
	other := &config.Config{}
	if upgraderFor(other) == u1 {
		t.Error("a different cfg must not reuse another config's upgrader")
	}
	if u1.ReadBufferSize != 4096 || u1.WriteBufferSize != 4096 {
		t.Errorf("buffer sizes changed: read=%d write=%d, want 4096/4096", u1.ReadBufferSize, u1.WriteBufferSize)
	}
}

func TestRemoteIP(t *testing.T) {
	cases := []struct{ remoteAddr, want string }{
		{"203.0.113.7:54321", "203.0.113.7"},
		{"[2001:db8::1]:39001", "2001:db8::1"},
		{"203.0.113.7", "203.0.113.7"}, // 无端口 → 原串
		{"", ""},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.RemoteAddr = c.remoteAddr
		if got := remoteIP(r); got != c.want {
			t.Errorf("remoteIP(%q) = %q, want %q", c.remoteAddr, got, c.want)
		}
	}
	if got := remoteIP(nil); got != "" {
		t.Errorf("remoteIP(nil) = %q, want empty", got)
	}
}

func TestServeWS_RateLimitedBeforeJWTParse(t *testing.T) {
	lim := util.NewRateLimiter(time.Second, util.RateBucketSpec{Name: "ws", PerMinute: 60, Burst: 1})
	SetWSRateLimiter(lim)
	defer SetWSRateLimiter(nil) // 包级状态：用例结束必须复位

	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.Issuer = "LsmAgentGame"
	hub := NewHub()

	// ① 唯一令牌被消费 → 走到 JWT 解析（坏 token → 401）。
	w1 := httptest.NewRecorder()
	ServeWS(cfg, hub, nil, nil, nil, nil, nil, w1, httptest.NewRequest(http.MethodGet, "/ws?token=***", nil))
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first upgrade attempt: status = %d, want 401 (limiter passed, JWT failed)", w1.Code)
	}

	// ② 同 IP 第二次 → 在 JWT 之前被限流：429 + 10502 + Retry-After。
	w2 := httptest.NewRecorder()
	ServeWS(cfg, hub, nil, nil, nil, nil, nil, w2, httptest.NewRequest(http.MethodGet, "/ws?token=***", nil))
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second upgrade attempt: status = %d, want 429", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), `"code":10502`) {
		t.Errorf("body = %s, want code 10502", w2.Body.String())
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("a rate-limited upgrade must carry a Retry-After header")
	}
	if got := w2.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestServeWS_NoLimiterShortCircuits(t *testing.T) {
	SetWSRateLimiter(nil) // security.enabled=false / 单测路径
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	hub := NewHub()

	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		ServeWS(cfg, hub, nil, nil, nil, nil, nil, w, httptest.NewRequest(http.MethodGet, "/ws?token=***", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (JWT failure, never 429)", i, w.Code)
		}
	}
}

func TestServeWS_MaxConnsPerUser(t *testing.T) {
	SetWSRateLimiter(nil)
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.Issuer = "LsmAgentGame"
	cfg.Security.WSGuard.MaxConnsPerUser = 2
	hub := NewHub()

	const uid = "user-max-conns"
	token, _, err := util.IssueToken(uid, cfg.JWT.Secret, cfg.JWT.Issuer, time.Minute)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	// 预注册 2 条连接 → 恰好达到上限。
	for i := 0; i < 2; i++ {
		hub.Register(&Client{UserID: uid, RemoteAddr: fmt.Sprintf("10.0.0.%d:1", i)})
	}
	if n := hub.UserConnCount(uid); n != 2 {
		t.Fatalf("UserConnCount = %d, want 2", n)
	}

	w := httptest.NewRecorder()
	ServeWS(cfg, hub, nil, nil, nil, nil, nil, w, httptest.NewRequest(http.MethodGet, "/ws?token="+token, nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (max_conns_per_user reached)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "10502") {
		t.Errorf("body = %s, want code 10502", w.Body.String())
	}
	// 计数语义：空 userID 与未知用户都是 0（不可键 ⇒ 不受上限影响）。
	if n := hub.UserConnCount(""); n != 0 {
		t.Errorf("UserConnCount(\"\") = %d, want 0", n)
	}
	if n := hub.UserConnCount("nobody"); n != 0 {
		t.Errorf("UserConnCount(unknown) = %d, want 0", n)
	}
}
