package ws

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"LsmAgentGame/util"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ── 20260923-01 §4.7 WebSocket 加固 ──────────────────────────────────────
//
// 旧实现是一个包级 upgrader，CheckOrigin 无条件 `return true`（方案 A5：
// 任意站点页面都能携带 cookie/token 发起跨源 WS 握手）。现在：
//   - upgrader 按 *config.Config 缓存（upgraderFor），Origin 策略读配置；
//   - ServeWS 在 JWT 解析**之前**过 "ws" 桶 IP 限流（SetWSRateLimiter 注入）；
//   - JWT 通过后检查单用户并发连接数（Hub.UserConnCount）。
//
// Handler(39001) 与 wsHandler(39002) 共用 ServeWS ⇒ 两条路径同策略。

// upgraderCache 按 cfg 指针缓存 *websocket.Upgrader（sync.Map：并发安全、
// 读多写少；键为 nil 也合法，兜住不传 cfg 的测试路径）。
var upgraderCache sync.Map // *config.Config → *websocket.Upgrader

// wsRateLimiter 是 "ws" 桶的 IP 令牌桶，由 main.go 在启动时注入一次
// （§130：SetWSRateLimiter）。atomic.Pointer 而非裸变量 —— ServeWS 在两条
// 监听器上并发读取，`go test -race` 下裸写裸读会被判数据竞争。
// nil（security.enabled=false / 单测）→ 升级限流整体短路。
var wsRateLimiter atomic.Pointer[util.RateLimiter]

// SetWSRateLimiter wires the "ws" bucket limiter (main.go, 20260923-01 §4.9).
// Passing nil disables WS upgrade rate limiting.
func SetWSRateLimiter(l *util.RateLimiter) {
	if l == nil {
		wsRateLimiter.Store(nil)
		return
	}
	wsRateLimiter.Store(l)
}

// upgraderFor returns the cached upgrader bound to cfg's Origin policy.
func upgraderFor(cfg *config.Config) *websocket.Upgrader {
	if v, ok := upgraderCache.Load(cfg); ok {
		return v.(*websocket.Upgrader)
	}
	u := &websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(cfg, r)
		},
	}
	actual, _ := upgraderCache.LoadOrStore(cfg, u)
	return actual.(*websocket.Upgrader)
}

// originAllowed 实现 §4.7 的 Origin 白名单（拒绝时 gorilla 回 403，不升级）：
//  1. 无 Origin 头 → 放行（非浏览器客户端：wscat / 服务端 SDK / 健康探针）；
//  2. Origin 的 host:port == 请求 Host（大小写不敏感、去默认端口）→ 放行
//     （生产同源：页面与 WSS 同 host:port）；
//  3. Origin 精确命中 cors.allowed_origins ∪ security.ws_guard.allowed_origins
//     → 放行（配置化，绝不硬编码）；
//  4. server.dev_mode=true 且 Origin host ∈ {localhost, 127.0.0.1}（任意端口）
//     → 放行（保住 Vite 5173/5174… 开发流与 AutoTest）；
//  5. 其余 → 拒绝。
func originAllowed(cfg *config.Config, r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true // 非浏览器客户端
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		logger.L().Warn("ws origin rejected: unparseable",
			zap.String("origin", origin),
			zap.String("remote", r.RemoteAddr))
		return false
	}
	originHost := normalizeHostPort(u.Scheme, u.Host)
	// ② 同源（页面 host:port == 请求 Host）。
	if requestHost := normalizeHostPort(schemeOf(r), r.Host); requestHost != "" && originHost == requestHost {
		return true
	}
	if cfg == nil {
		return false
	}
	// ③ 配置白名单（CORS ∪ ws_guard）。
	for _, allowed := range cfg.CORS.AllowedOrigins {
		if allowed = strings.TrimSpace(allowed); allowed != "" && strings.EqualFold(allowed, origin) {
			return true
		}
	}
	for _, allowed := range cfg.Security.WSGuard.AllowedOrigins {
		if allowed = strings.TrimSpace(allowed); allowed != "" && strings.EqualFold(allowed, origin) {
			return true
		}
	}
	// ④ dev 通配：localhost / 127.0.0.1 任意端口（Hostname 已剥掉 IPv6 方括号）。
	if cfg.Server.DevMode {
		if h := strings.ToLower(u.Hostname()); h == "localhost" || h == "127.0.0.1" {
			return true
		}
	}
	logger.L().Warn("ws origin rejected",
		zap.String("origin", origin),
		zap.String("host", r.Host),
		zap.String("remote", r.RemoteAddr))
	return false
}

// schemeOf 推断请求 scheme（TLS 握手过 → https，否则 http）。仅用于
// normalizeHostPort 去默认端口。
func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if s := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); s != "" {
		return s
	}
	return "http"
}

// normalizeHostPort 小写化并去掉 scheme 的默认端口（https:443 / http:80），
// 让 "https://Host:443" 与 "Host" 判为同源。
func normalizeHostPort(scheme, hostport string) string {
	hp := strings.ToLower(strings.TrimSpace(hostport))
	if hp == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		hp = strings.TrimSuffix(hp, ":443")
	case "http":
		hp = strings.TrimSuffix(hp, ":80")
	}
	return hp
}

// remoteIP 从 RemoteAddr 取出裸 IP（"1.2.3.4:5678" → "1.2.3.4"）。
// 不经 gin.ClientIP()：39002 的 net/http 路径没有 gin.Context，且
// router 已把 TrustedProxies 收敛为空 → 真实 socket IP 才是可信维度。
func remoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		return host
	}
	return addr
}

// denyWSUpgrade 在升级发生**之前**写 JSON 拒绝响应（不 hijack 连接）。
// retryAfter>0 时附带 Retry-After 头，客户端走既有重连退避。
func denyWSUpgrade(w http.ResponseWriter, status, code int, msg string, retryAfter time.Duration) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	if secs := util.CeilSeconds(retryAfter); secs > 0 {
		h.Set("Retry-After", strconv.Itoa(secs))
	}
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"code":%d,"message":%q}`, code, msg)
}

// Handler returns a Gin handler that performs the WSS upgrade.
//
// BUG-WEREWOLF-P0-NEW-37 注释：Handler 是 39001 (HTTPS) gin 路由的入口。
// 对 39002 (WSS) 端口请直接用 ServeWS —— gin.CreateTestContext 会在
// gorilla 接管连接后把 responseWriter 留在半 hijacked 状态,导致部分客户端
// 的 game.spectate ack 丢失。Handler 本身只是 ServeWS 的薄封装。
func Handler(cfg *config.Config, hub *Hub, chat *ChatService, game *GameService, room *RoomWsService, user *UserWsService, debate *DebateService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Recover from any panic to avoid crashing the entire connection handler.
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("ws handler panic",
					zap.Any("recover", r),
					zap.String("remote", c.ClientIP()))
			}
		}()

		ServeWS(cfg, hub, chat, game, room, user, debate, c.Writer, c.Request)
	}
}

// ServeWS is the net/http variant of Handler. It performs the WSS upgrade,
// JWT auth, client registration and pump launch without wrapping the request
// in a gin.Context — required for the dedicated WSS server (port 39002) where
// gin.CreateTestContext leaves the responseWriter in a half-hijacked state
// after gorilla upgrades the connection (BUG-WEREWOLF-P0-NEW-37).
func ServeWS(cfg *config.Config, hub *Hub, chat *ChatService, game *GameService, room *RoomWsService, user *UserWsService, debate *DebateService, w http.ResponseWriter, r *http.Request) {
	// Recover from any panic to avoid crashing the entire connection handler.
	defer func() {
		if rec := recover(); rec != nil {
			logger.L().Error("ws handler panic",
				zap.Any("recover", rec),
				zap.String("remote", r.RemoteAddr))
		}
	}()

	token := r.URL.Query().Get("token")
	remote := r.RemoteAddr
	ip := remoteIP(r)

	// ── §4.7 第 2 步：升级速率限制，放在 JWT 解析之前 ──────────────────
	// 坏 token 的洪泛同样消耗 bcrypt/JWT 解析与日志，必须先在最便宜的
	// 位置挡掉。limiter 未注入（security.enabled=false / 单测）→ 短路。
	if lim := wsRateLimiter.Load(); lim != nil {
		if ok, retry := lim.Allow("ws", ip); !ok {
			logger.L().Warn("ws upgrade rate limited",
				zap.String("remote", remote),
				zap.String("client_ip", ip),
				zap.Int("retry_after_seconds", util.CeilSeconds(retry)))
			denyWSUpgrade(w, http.StatusTooManyRequests, errcode.ErrAuthRateLimited,
				errcode.DefaultMessages[errcode.ErrAuthRateLimited], retry)
			return
		}
	}

	// Log the upgrade attempt BEFORE auth so we can see even bad-token requests.
	logger.L().Info("ws upgrade request",
		zap.String("remote", remote),
		zap.String("client_ip", ip),
		zap.String("origin", r.Header.Get("Origin")),
		zap.Int("token_len", len(token)),
	)

	uid, err := util.ParseToken(token, cfg.JWT.Secret)
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Warn("ws auth failed",
			zap.String("remote", remote),
			zap.String("client_ip", ip),
			zap.Int("token_len", len(token)),
			zap.Int("code", ce.Code),
			zap.String("err", ce.Message),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"code":%d,"message":%q}`, ce.Code, ce.Message)
		return
	}

	// ── §4.7 第 3 步：单用户并发连接数上限 ─────────────────────────────
	// 刷新/重连有 15s 宽限期兜底，默认 10 条足够多端 + 反复刷新，不误伤。
	// max_conns_per_user<=0 视为不限。
	if cfg != nil {
		if maxConns := cfg.Security.WSGuard.MaxConnsPerUser; maxConns > 0 {
			if n := hub.UserConnCount(uid); n >= maxConns {
				logger.L().Warn("ws upgrade rejected: too many connections for user",
					zap.String("user_id", uid),
					zap.String("remote", remote),
					zap.String("client_ip", ip),
					zap.Int("conns", n),
					zap.Int("max_conns_per_user", maxConns))
				denyWSUpgrade(w, http.StatusTooManyRequests, errcode.ErrAuthRateLimited,
					"too many concurrent connections for this user", 0)
				return
			}
		}
	}

	// Mark the handler position so we can correlate "ws upgrade request"
	// with whichever exit point is reached next. Without this, a hung
	// upgrader.Upgrade() leaves operators with no log trail to debug.
	logger.L().Info("ws auth ok, calling upgrader.Upgrade",
		zap.String("user_id", uid),
		zap.String("remote", remote),
	)

	// Origin 校验在 Upgrade 内部经 CheckOrigin 执行（拒绝 → 403，不升级）。
	conn, err := upgraderFor(cfg).Upgrade(w, r, nil)
	if err != nil {
		logger.L().Warn("ws upgrade failed",
			zap.String("user_id", uid),
			zap.String("remote", remote),
			zap.String("origin", r.Header.Get("Origin")),
			zap.Error(err),
		)
		return
	}
	logger.L().Info("ws upgrade succeeded",
		zap.String("user_id", uid),
		zap.String("remote", remote),
	)
	client := NewClient(hub, conn, uid, remote)
	client.AttachChat(chat)
	client.AttachGame(game)
	client.AttachRoom(room)
	client.AttachUser(user)
	client.AttachDebate(debate)
	hub.Register(client)

	// Cancel any pending disconnect timer for this user in a separate
	// goroutine so it never blocks the ReadPump/WritePump launch.
	// If the old client's cleanup goroutine holds h.mu, this would
	// otherwise stall the handler and prevent the pumps from starting.
	go hub.CancelDisconnectTimer(uid)

	go client.WritePump()
	go client.ReadPump()

	logger.L().Info("ws pumps launched",
		zap.String("user_id", uid),
		zap.String("remote", remote))
}
