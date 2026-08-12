// Package ws —— 房间管理 WebSocket 服务。
//
// 通过单一 WS 通道处理房间 CRUD 操作,与 REST API 并存,提供低延迟路径。
// 帧类型(所有消息封装在 Envelope 中,type 字段以 "room." 开头):
//
//   client → server
//     room.list    { game_kind }
//     room.create  { game_kind }
//     room.join    { room_id }
//     room.leave   { room_id }
//
//   server → client
//     room.list_resp    { rooms: [...] }
//     room.create_resp  RoomDetail
//     room.join_resp    RoomDetail
//     room.leave_resp   { room_id, ok }
//     room.error        ErrorPayload
package ws

import (
	"context"
	"encoding/json"

	"LsmAgentGame/logger"
	"LsmAgentGame/service"

	"go.uber.org/zap"
)

// RoomWsService 暴露房间操作的 WS 处理函数。
type RoomWsService struct {
	roomSvc *service.RoomService
	hub     *Hub
}

// NewRoomWsService 构造房间 WS 服务。
func NewRoomWsService(roomSvc *service.RoomService, hub *Hub) *RoomWsService {
	return &RoomWsService{roomSvc: roomSvc, hub: hub}
}

// HandleClientFrame 路由 room.* 帧到对应的处理器。
func (s *RoomWsService) HandleClientFrame(c *Client, env Envelope) {
	switch env.Type {
	case "room.list":
		s.handleList(c, env)
	case "room.create":
		s.handleCreate(c, env)
	case "room.join":
		s.handleJoin(c, env)
	case "room.leave":
		s.handleLeave(c, env)
	default:
		s.sendError(c, env.Seq, 20001, "unknown room message type: "+env.Type)
	}
}

// ─────────────────── Handlers ───────────────────

func (s *RoomWsService) handleList(c *Client, env Envelope) {
	var req struct {
		GameKind string `json:"game_kind"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.GameKind == "" {
		s.sendError(c, env.Seq, 20001, "invalid room.list payload")
		return
	}

	// 2026-07-30 §R210+对齐: 用 ListRoomsForUser(ctx, kind, userID) 注入
	// my_role — 之前用无 userID 旧 ListRooms → 永远为空 → 玩家刷新后只剩
	// "已满"按钮、无法重新进入房间。HTTP /api/games/:kind/rooms 已迁到
	// ListRoomsForUser(R210 修复),WS 路径遗漏,这里统一。
	// WS 路径没有 HTTP request,用 context.Background() 维持与 HTTP 路径
	// 相同的语义;RoomService 内部用 ctx 做 DB 查询超时/取消传播。
	rooms := s.roomSvc.ListRoomsForUser(context.Background(), req.GameKind, c.UserID)
	s.sendOK(c, env.Seq, "room.list_resp", map[string]any{
		"rooms": rooms,
	})
}

func (s *RoomWsService) handleCreate(c *Client, env Envelope) {
	var req struct {
		GameKind string `json:"game_kind"`
		Name     string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.GameKind == "" {
		s.sendError(c, env.Seq, 20001, "invalid room.create payload")
		return
	}

	detail, e := s.roomSvc.CreateRoom(req.GameKind, c.UserID, req.Name)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	s.sendOK(c, env.Seq, "room.create_resp", detail)

	// 通知大厅：新房间已创建，前端可以把它插入房间列表。
	if s.hub != nil {
		s.hub.NotifyRoomChanged(detail.ID, "room_created", c.UserID)
	}

	logger.L().Info("room created via ws",
		zap.String("room_id", detail.ID),
		zap.String("game_kind", req.GameKind),
		zap.String("user_id", c.UserID))
}

func (s *RoomWsService) handleJoin(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid room.join payload")
		return
	}

	// JoinRoom 已是幂等(已在房间时返回详情)。
	detail, e := s.roomSvc.JoinRoom(req.RoomID, c.UserID)
	if e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	// 关键:把 WS 客户端登记到 hub.rooms[roomID] 集合,这样后续 WS 断线时
	// Hub.Unregister 能看到 "用户在房间中" 并启动 15 秒掉线等待定时器。
	// 此前 room.join 只更新 DB、没碰 hub.rooms,导致断线时 Hub 找不到用户
	// 所在的房间,15 秒自动踢出逻辑形同虚设。
	if s.hub != nil {
		s.hub.SubscribeRoom(req.RoomID, c)
	}
	s.sendOK(c, env.Seq, "room.join_resp", detail)

	// 通知大厅：座位数变化 (current_count++),大厅卡片立即刷新可加入状态。
	// 幂等的 JoinRoom 在已加入时不会真的增计数,所以这里只在确实"新加入"
	// 时广播——通过比对加入前后的 CurrentCount 来判断。
	if s.hub != nil && detail != nil && detail.CurrentCount > 0 {
		// detail.CurrentCount 反映的是"加入后"的快照,所以 > 0 视为成功
		s.hub.NotifyRoomChanged(req.RoomID, "player_joined", c.UserID)
	}

	logger.L().Info("room joined via ws",
		zap.String("room_id", req.RoomID),
		zap.String("user_id", c.UserID))
}

func (s *RoomWsService) handleLeave(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, 20001, "invalid room.leave payload")
		return
	}

	if e := s.roomSvc.LeaveRoom(req.RoomID, c.UserID); e != nil {
		s.sendError(c, env.Seq, e.Code, e.Message)
		return
	}
	// 同步取消 WS 客户端在 hub.rooms 集合中的订阅,否则 Hub.Unregister
	// 还会看到用户"还在这个房间",误启动 15 秒掉线定时器。
	if s.hub != nil {
		s.hub.UnsubscribeRoom(req.RoomID, c)
	}
	s.sendOK(c, env.Seq, "room.leave_resp", map[string]any{
		"room_id": req.RoomID,
		"ok":      true,
	})

	// 通知大厅：座位释放 / 房间可能已被删除。
	if s.hub != nil {
		s.hub.NotifyRoomChanged(req.RoomID, "player_left", c.UserID)
	}

	logger.L().Info("room left via ws",
		zap.String("room_id", req.RoomID),
		zap.String("user_id", c.UserID))
}

// ─────────────────── Helpers ───────────────────

func (s *RoomWsService) sendOK(c *Client, seq int64, msgType string, payload any) {
	c.send <- Envelope{Type: msgType, Seq: seq, Payload: mustMarshal(payload)}
}

func (s *RoomWsService) sendError(c *Client, seq int64, code int, msg string) {
	c.send <- Envelope{Type: "room.error", Seq: seq, Payload: mustMarshal(map[string]any{
		"code": code, "message": msg,
	})}
}

// ─────────────────── Proto 消息注册 ───────────────────

// registerProtoMessages 在 proto 路由器中注册房间服务的 proto 消息
func (s *RoomWsService) registerProtoMessages(reg *ProtoRegistry) {
	// TODO: 迁移 room.list / room.create / room.join / room.leave / room.state 等
}
