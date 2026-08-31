package ws

import (
	"encoding/json"
	"errors"
	"time"

	"LsmAgentGame/logger"
	commonpb "LsmAgentGame/proto/pb/common"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1 << 20 // 1 MiB
	sendBufSize    = 64
	protoSendBuf   = 64
)

// 常见错误
var (
	ErrRecvQueueFull = errors.New("recv queue full")
)

// Client is a single WebSocket connection.
type Client struct {
	UserID     string
	RemoteAddr string
	conn       *websocket.Conn
	send       chan Envelope  // JSON 发送队列（legacy）
	protoSend  chan []byte    // Proto 二进制发送队列（新增）
	hub        *Hub
	chat       *ChatService   // may be nil until the main loop wires it
	game       *GameService   // may be nil until the main loop wires it
	room       *RoomWsService // may be nil until the main loop wires it
	user       *UserWsService // may be nil until the main loop wires it

	// protoEnabled 客户端是否已协商启用 proto 二进制模式
	// 启用后，服务端会主动通过 proto 通道推送消息
	protoEnabled bool
	// protoVersion 协商后的 proto 协议版本
	protoVersion int32
}

// NewClient wraps an upgraded connection.
func NewClient(hub *Hub, conn *websocket.Conn, userID, remote string) *Client {
	return &Client{
		UserID:     userID,
		RemoteAddr: remote,
		conn:       conn,
		send:       make(chan Envelope, sendBufSize),
		protoSend:  make(chan []byte, protoSendBuf),
		hub:        hub,
	}
}

// AttachChat wires the chat service after construction (avoids a circular
// constructor dependency between Hub and ChatService).
func (c *Client) AttachChat(svc *ChatService) {
	c.chat = svc
}

// AttachGame wires the game service after construction.
func (c *Client) AttachGame(svc *GameService) {
	c.game = svc
}

// AttachRoom wires the room WS service after construction.
func (c *Client) AttachRoom(svc *RoomWsService) {
	c.room = svc
}

// AttachUser wires the user list WS service after construction.
func (c *Client) AttachUser(svc *UserWsService) {
	c.user = svc
}

// Send enqueues an envelope to the client. Safe to call from any goroutine;
// the WritePump drains the channel. If the send queue is full the envelope
// is dropped (WritePump will unregister the client if it stays stuck).
//
// §20260831-01 — 由 ws.DebateService 等跨包服务调用,把广播帧塞回
// Client 的内部 send 通道。
func (c *Client) Send(env Envelope) {
	select {
	case c.send <- env:
	default:
		// 队列已满:丢弃 + 触发 WritePump 退出
		// (WritePump 在 send 满时会自然 unregister,这里不强行 close)
	}
}

// ReadPump reads messages from the socket and dispatches them. Run in its own goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close()
		// Note: Hub.Unregister 已经打印 "ws disconnected" 日志,这里不再重复。
	}()
	logger.L().Info("ws read pump start",
		zap.String("user_id", c.UserID),
		zap.String("remote", c.RemoteAddr),
	)
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		msgType, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.L().Warn("ws read error",
					zap.String("user_id", c.UserID),
					zap.String("remote", c.RemoteAddr),
					zap.Error(err),
				)
			} else {
				logger.L().Info("ws read end",
					zap.String("user_id", c.UserID),
					zap.String("remote", c.RemoteAddr),
					zap.Error(err),
				)
			}
			return
		}
		logger.L().Debug("ws raw recv",
			zap.String("user_id", c.UserID),
			zap.Int("bytes", len(raw)),
			zap.Int("msg_type", msgType),
		)

		// ── Proto 二进制消息 ──
		if msgType == websocket.BinaryMessage {
			c.handleBinaryMessage(raw)
			continue
		}

		// ── JSON 文本消息（legacy） ──
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			// Bad frame — reply with an error envelope and keep going.
			c.send <- Envelope{Type: "error", Payload: json.RawMessage(`{"code":20001,"message":"bad envelope"}`)}
			continue
		}
		logger.L().Debug("ws recv", zap.String("type", env.Type), zap.String("user_id", c.UserID))

		// Chat frames are routed to the chat service when available; everything
		// else falls back to the ack echo so existing clients keep working.
		if c.chat != nil && len(env.Type) >= 5 && env.Type[:5] == "chat." {
			c.chat.HandleClientFrame(c, env)
			continue
		}
		// Game frames are routed to the game service.
		if c.game != nil && len(env.Type) >= 5 && env.Type[:5] == "game." {
			c.game.HandleClientFrame(c, env)
			continue
		}
		// Room frames are routed to the room WS service.
		if c.room != nil && len(env.Type) >= 5 && env.Type[:5] == "room." {
			c.room.HandleClientFrame(c, env)
			continue
		}
		// User-list frames are routed to the user WS service.
		if c.user != nil && len(env.Type) >= 5 && env.Type[:5] == "user." {
			c.user.HandleClientFrame(c, env)
			continue
		}
		// Echo back as a minimal ack so the client can verify round-trip in dev.
		c.send <- Envelope{Type: "ack", Seq: env.Seq, Payload: env.Payload}
	}
}

// handleBinaryMessage 处理 proto 二进制帧
func (c *Client) handleBinaryMessage(raw []byte) {
	env, err := UnmarshalProtoEnvelope(raw)
	if err != nil {
		logger.L().Warn("ws proto decode error",
			zap.String("user_id", c.UserID),
			zap.Error(err),
		)
		c.SendProtoError("error", 0, 20001, "bad proto envelope")
		return
	}

	logger.L().Debug("ws proto recv",
		zap.String("type", env.Type),
		zap.String("user_id", c.UserID),
		zap.Int("payload_bytes", len(env.Payload)),
	)

	// 交给 proto 路由器分发
	if c.hub.protoRouter != nil {
		if err := c.hub.protoRouter.Dispatch(c, env); err != nil {
			logger.L().Warn("ws proto dispatch error",
				zap.String("user_id", c.UserID),
				zap.String("type", env.Type),
				zap.Error(err),
			)
			c.SendProtoError(env.Type, env.Seq, 20002, err.Error())
		}
	}
}

// handleProtoCapability 处理协议协商请求
func (c *Client) handleProtoCapability(req *commonpb.ProtoCapability) {
	c.protoEnabled = true
	c.protoVersion = req.Version
	if c.protoVersion <= 0 {
		c.protoVersion = 1
	}

	logger.L().Info("ws proto capability negotiated",
		zap.String("user_id", c.UserID),
		zap.Int32("version", c.protoVersion),
	)

	// 发送确认
	ack := &commonpb.ProtoCapabilityAck{
		Version:  c.protoVersion,
		Encoding: "binary",
	}
	data, err := MarshalProtoEnvelope("system.proto_ack", 0, ack)
	if err == nil {
		select {
		case c.protoSend <- data:
		default:
		}
	}
}

// WritePump serializes outbound messages and pings. Run in its own goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	logger.L().Info("ws write pump start",
		zap.String("user_id", c.UserID),
		zap.String("remote", c.RemoteAddr),
	)
	for {
		select {
		case env, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			data, err := json.Marshal(env)
			if err != nil {
				continue
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case data, ok := <-c.protoSend:
			// Proto 二进制帧
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}