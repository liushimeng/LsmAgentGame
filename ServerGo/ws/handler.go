package ws

import (
	"fmt"
	"net/http"

	"LsmWebGame/config"
	"LsmWebGame/errcode"
	"LsmWebGame/logger"
	"LsmWebGame/util"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Authenticated by JWT; for dev we accept any origin. Tighten in prod.
		return true
	},
}

// Handler returns a Gin handler that performs the WSS upgrade.
//
// BUG-WEREWOLF-P0-NEW-37 注释：Handler 是 39001 (HTTPS) gin 路由的入口。
// 对 39002 (WSS) 端口请直接用 ServeWS —— gin.CreateTestContext 会在
// gorilla 接管连接后把 responseWriter 留在半 hijacked 状态,导致部分客户端
// 的 game.spectate ack 丢失。Handler 本身只是 ServeWS 的薄封装。
func Handler(cfg *config.Config, hub *Hub, chat *ChatService, game *GameService, room *RoomWsService, user *UserWsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Recover from any panic to avoid crashing the entire connection handler.
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("ws handler panic",
					zap.Any("recover", r),
					zap.String("remote", c.ClientIP()))
			}
		}()

		ServeWS(cfg, hub, chat, game, room, user, c.Writer, c.Request)
	}
}

// ServeWS is the net/http variant of Handler. It performs the WSS upgrade,
// JWT auth, client registration and pump launch without wrapping the request
// in a gin.Context — required for the dedicated WSS server (port 39002) where
// gin.CreateTestContext leaves the responseWriter in a half-hijacked state
// after gorilla upgrades the connection (BUG-WEREWOLF-P0-NEW-37).
func ServeWS(cfg *config.Config, hub *Hub, chat *ChatService, game *GameService, room *RoomWsService, user *UserWsService, w http.ResponseWriter, r *http.Request) {
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

	// Log the upgrade attempt BEFORE auth so we can see even bad-token requests.
	logger.L().Info("ws upgrade request",
		zap.String("remote", remote),
		zap.Int("token_len", len(token)),
	)

	uid, err := util.ParseToken(token, cfg.JWT.Secret)
	if err != nil {
		ce := errcode.AsError(err)
		logger.L().Warn("ws auth failed",
			zap.String("remote", remote),
			zap.Int("token_len", len(token)),
			zap.Int("code", ce.Code),
			zap.String("err", ce.Message),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"code":%d,"message":%q}`, ce.Code, ce.Message)
		return
	}

	// Mark the handler position so we can correlate "ws upgrade request"
	// with whichever exit point is reached next. Without this, a hung
	// upgrader.Upgrade() leaves operators with no log trail to debug.
	logger.L().Info("ws auth ok, calling upgrader.Upgrade",
		zap.String("user_id", uid),
		zap.String("remote", remote),
	)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.L().Warn("ws upgrade failed",
			zap.String("user_id", uid),
			zap.String("remote", remote),
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