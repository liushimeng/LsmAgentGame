package api

import (
	"encoding/json"
	"net/http"

	"LsmAgentGame/errcode"
	"LsmAgentGame/service"
	"LsmAgentGame/ws"

	"github.com/gin-gonic/gin"
)

// createRoomRequest is the optional JSON body for POST /api/games/:kind/rooms.
// `agent_seats` is only honored for the "werewolf" kind (7-player bot games);
// other kinds ignore the field silently. `name` is an optional human-readable
// room name; when omitted the service auto-generates one.
type createRoomRequest struct {
	Name       string                    `json:"name,omitempty"`
	AgentSeats []service.AgentSeatConfig `json:"agent_seats,omitempty"`
	// 2026-07-16 主持人重构 — 房间级法官设置(可选)。mode: "ai"|"human"|"off";空=ai。
	Judge *service.JudgeConfig `json:"judge,omitempty"`
	// AgentDifficulty 2026-08-11 §20260811-09 U2 — Agent 难度分级配置(可选)。
	// 取值: easy/normal/hard/hell;缺省 / 未知值 = normal(归一化兜底)。
	// 后端 service.CreateRoomWithAgents 透传到 in-memory WerewolfRoom.agentDifficulty。
	AgentDifficulty string `json:"agent_difficulty,omitempty"`
	// Commentary 2026-08-11 §20260811-09 U1 — 观战模式 AI 解说(可选)。
	// nil = 关闭(默认);非 nil 时按 Style/ModelKey 启用。
	Commentary *service.CommentaryConfig `json:"commentary,omitempty"`
	// CreatorRole 2026-08-06 §20260806-03 — 创建者(人类座位)角色偏好(可选)。
	// 取值: werewolf/seer/witch/hunter/idiot/guard/knight/demon_hunter/villager
	// 或 "random"/缺省 = 随机。牌组中无此角色时降级为随机(多重集守恒置换)。
	CreatorRole string `json:"creator_role,omitempty"`
}

// RoomAPI serves the room management endpoints.
type RoomAPI struct {
	svc *service.RoomService
	hub RoomChangeNotifier
}

// RoomChangeNotifier is the subset of ws.Hub that the HTTP room API needs
// for real-time lobby refresh. Declared as an interface here to avoid an
// import cycle through the ws package.
type RoomChangeNotifier interface {
	NotifyRoomChanged(roomID, action, userID string)
}

// NewRoomAPI wires the handler.
func NewRoomAPI(svc *service.RoomService, hub *ws.Hub) *RoomAPI {
	return &RoomAPI{svc: svc, hub: hub}
}

// List GET /api/games/:kind/rooms — list open rooms for a game kind.
//
// BUG-R210-01 (2026-07-30): stamp `my_role` on each row so the frontend can
// render "进入房间" / "👁 观战" / "已满" after a page refresh without
// round-tripping each room. userID is taken from the JWT-bound context, so
// unauthenticated calls (defensive: should be rejected upstream by
// AuthMiddleware, but if a guest slips through) get an empty list response
// and fall back to the legacy joinable() check on the client.
func (a *RoomAPI) List(c *gin.Context) {
	kind := c.Param("kind")
	if kind == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": errcode.ErrValidationFailed, "message": "game kind is required"})
		return
	}
	userID := c.GetString("user_id")
	rooms := a.svc.ListRoomsForUser(c.Request.Context(), kind, userID)
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": rooms})
}

// Create POST /api/games/:kind/rooms — create a new room.
//
// Body (optional JSON):
//   { "agent_seats": [{ "seat": 0..6, "model_key": "X-model" }, ...] }
//
// agent_seats is only honored for the "werewolf" kind; other kinds forward to
// CreateRoomWithAgents which silently treats an empty list as "no agents".
// Switched from CreateRoom → CreateRoomWithAgents in Phase 4 to end the
// P0 bug where the body was dropped on the floor.
func (a *RoomAPI) Create(c *gin.Context) {
	kind := c.Param("kind")
	if kind == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": errcode.ErrValidationFailed, "message": "game kind is required"})
		return
	}
	userID := c.GetString("user_id")

	// Bind agent_seats (optional). Failure to bind (e.g. malformed JSON) is a
	// 400 — but an absent body is allowed and means "no agents".
	//
	// BUG-WEREWOLF-P0-NEW-14 (Round 28): Gin's ShouldBindJSON silently ignores
	// unknown fields. R1-R27 test scripts used wrong field names (bot_slots,
	// bot_models, game_type, etc.) and the server silently created 0-bot rooms.
	// Now we use json.Decoder with DisallowUnknownFields so callers get a
	// clear 400 error instead of silent data loss.
	var req createRoomRequest
	if c.Request.Body != nil {
		dec := json.NewDecoder(c.Request.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    errcode.ErrValidationFailed,
				"message": "invalid body: " + err.Error(),
			})
			return
		}
	}

	detail, e := a.svc.CreateRoomWithAgents(c.Request.Context(), kind, userID, req.Name, req.AgentSeats, req.Judge, req.AgentDifficulty, req.Commentary, req.CreatorRole)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if a.hub != nil {
		a.hub.NotifyRoomChanged(detail.ID, "room_created", userID)
	}
	// BUG-WEREWOLF-P0-NEW-14: echo agent_seats_count in response so the caller
	// can verify the server actually registered the expected number of bots.
	c.JSON(http.StatusOK, gin.H{
		"code":    errcode.OK,
		"message": "ok",
		"data":    detail,
		"agent_seats_count": len(req.AgentSeats),
	})
}

// Join POST /api/rooms/:id/join — join an existing room.
func (a *RoomAPI) Join(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	detail, e := a.svc.JoinRoom(roomID, userID)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if a.hub != nil {
		a.hub.NotifyRoomChanged(roomID, "player_joined", userID)
	}
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": detail})
}

// Leave POST /api/rooms/:id/leave — leave a room.
func (a *RoomAPI) Leave(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	if e := a.svc.LeaveRoom(roomID, userID); e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if a.hub != nil {
		a.hub.NotifyRoomChanged(roomID, "player_left", userID)
	}
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok"})
}

// Detail GET /api/rooms/:id — room detail with player list.
//
// BUG-R210-01 (2026-07-30): stamp `my_role` so the frontend can route
// correctly. Detail is consumed by both the lobby "我在房间" indicator and
// the in-page refresh flow (WerewolfGamePage's onMount requestState fallback).
func (a *RoomAPI) Detail(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	detail, e := a.svc.GetRoomDetailForUser(c.Request.Context(), roomID, userID)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": detail})
}

// Spectate POST /api/rooms/:id/spectate — register the caller as an observer.
// Idempotent: a user who is already spectating the room gets back the same
// detail without side effects. A user who is a seat-taking player of the room
// gets ErrAlreadyInOtherRole.
func (a *RoomAPI) Spectate(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	detail, e := a.svc.SpectateRoom(roomID, userID)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok", "data": detail})
}

// LeaveSpectate POST /api/rooms/:id/leave_spectate — drop the spectator row.
// Returns ErrRoomNotIn if the caller wasn't a spectator of the room.
func (a *RoomAPI) LeaveSpectate(c *gin.Context) {
	roomID := c.Param("id")
	userID := c.GetString("user_id")
	if e := a.svc.LeaveSpectate(roomID, userID); e != nil {
		c.JSON(http.StatusOK, gin.H{"code": e.Code, "message": e.Message})
		return
	}
	if a.hub != nil {
		a.hub.NotifyRoomChanged(roomID, "spectator_left", userID)
	}
	c.JSON(http.StatusOK, gin.H{"code": errcode.OK, "message": "ok"})
}
