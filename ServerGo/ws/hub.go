// Package ws implements the WebSocket layer.
//
// Lifecycle:
//   - Handler upgrades the request, validates the JWT (via ?token=), and registers
//     a Client with the Hub.
//   - Each Client runs a readPump and a writePump goroutine.
//   - The Hub keeps three indexes for fan-out:
//       clients      : userID → set of *Client (per-connection bookkeeping)
//       lobby        : set of *Client currently subscribed to lobby chat
//       rooms[roomID]: set of *Client currently subscribed to room chat
//
// Wire format is a length-prefixed JSON envelope; proto framing is added later
// once the codegen is wired in.
package ws

import (
	"encoding/json"
	"sync"
	"time"

	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"
	"go.uber.org/zap"
)

// Envelope is the JSON envelope used on the wire.
type Envelope struct {
	Type    string          `json:"type"`
	Seq     int64           `json:"seq,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// PendingDisconnect tracks a player who has disconnected but may reconnect
// within the grace period (15 seconds). After the timeout, the player is
// automatically removed from the room.
type PendingDisconnect struct {
	RoomID         string
	Timer          *time.Timer
	StopCh         chan struct{} // closed by CancelDisconnectTimer to abort the wait goroutine
	DisconnectedAt time.Time
}

// RoomVacancy tracks a room that currently has no online users. If no one
// re-joins within the grace period (5 minutes), the room is deleted from the
// DB and its in-memory game state is cleaned up.
type RoomVacancy struct {
	RoomID    string
	Timer     *time.Timer
	StopCh    chan struct{}
	VacatedAt time.Time
}

// RoomStateBroadcaster abstracts the dependency the Hub needs to build a
// real-time room-state snapshot after a join/leave/timeout. The concrete
// implementation is *RoomService wrapped in main.go. Keeping this as an
// interface lets us keep the Hub free of an import cycle against the
// concrete service package.
type RoomStateBroadcaster interface {
	// GetRoomInfo returns a minimal {id, game_kind, capacity, current_count,
	// status} tuple for a single room, or (nil, nil) if the room was deleted.
	GetRoomInfo(roomID string) (id, gameKind string, capacity, currentCount int, status string, ok bool)
}

// RoomStateSnapshot is the minimal subset of *service.RoomDetail that the
// hub fans out to lobby subscribers. Fields mirror service.RoomDetail
// (id/game_kind/capacity/current_count/status) so the frontend can replace
// the entry in its cached `rooms` list without re-fetching the whole list.
type RoomStateSnapshot struct {
	ID           string `json:"id"`
	GameKind     string `json:"game_kind"`
	Capacity     int    `json:"capacity"`
	CurrentCount int    `json:"current_count"`
	Status       string `json:"status"`
}

// ToInfo converts the snapshot into the wire shape used by room.list_resp.
// Kept here so callers don't have to depend on the service package.
func (s *RoomStateSnapshot) ToInfo() map[string]any {
	if s == nil {
		return map[string]any{
			"id":            "",
			"game_kind":     "",
			"capacity":      0,
			"current_count": 0,
			"status":        "removed",
		}
	}
	return map[string]any{
		"id":            s.ID,
		"game_kind":     s.GameKind,
		"capacity":      s.Capacity,
		"current_count": s.CurrentCount,
		"status":        s.Status,
	}
}

// buildRoomStateSnapshot turns the RoomStateBroadcaster's return values into
// a RoomStateSnapshot for the wire payload. Returns nil when the room is
// gone so the caller can emit a "removed" placeholder.
func buildRoomStateSnapshot(id, gameKind string, capacity, currentCount int, status string, ok bool) *RoomStateSnapshot {
	if !ok {
		return nil
	}
	return &RoomStateSnapshot{
		ID:           id,
		GameKind:     gameKind,
		Capacity:     capacity,
		CurrentCount: currentCount,
		Status:       status,
	}
}

// Hub tracks connected clients and per-scope subscriptions.
//
// The hub maintains **two disjoint** per-room broadcast sets:
//
//   - rooms[roomID]      : clients receiving the room's game broadcasts
//                          (players — receive `game.*` per-seat frames)
//   - spectators[roomID] : clients receiving only sanitized spectator frames
//                          (observers — receive `game.state` rebuilt without
//                          any per-seat secrets)
//
// Keeping the two sets separate is the foundation of spectator isolation:
// player code paths (`BroadcastRoom`) never have to think about whether a
// recipient is a spectator, and spectator code paths (`BroadcastRoomSpectators`)
// never have to filter for them either. Chat fan-out intentionally uses
// `BroadcastRoom` for players and a separate helper for spectators so that
// players' chat fan-out is unchanged.
type Hub struct {
	mu               sync.RWMutex
	clients          map[string]map[*Client]struct{} // userID → clients
	lobby            map[*Client]struct{}            // lobby subscribers
	rooms            map[string]map[*Client]struct{} // roomID → subscribers (players + chat-only subs)
	spectators       map[string]map[*Client]struct{} // roomID → spectators
	pendingDisconnects map[string]*PendingDisconnect  // userID → pending disconnect timer
	vacancyTimers      map[string]*RoomVacancy        // roomID → pending empty-room deletion timer

	// protoRouter proto 消息路由器（懒加载，为 nil 时仅 JSON 模式可用）
	protoRouter *ProtoRouter

	// RoomSvc is consulted on join/leave/timeout to fan out a fresh
	// `room.state` envelope to lobby subscribers. May be nil during very
	// early construction; SetRoomService must be called before serving WS.
	roomSvc RoomStateBroadcaster
	// LeaveRoomFn removes a user from a room at the service layer (DB +
	// game manager cleanup). Wired via SetLeaveRoomFunc.
	leaveRoomFn func(roomID, userID string) *errcode.Error
	// LeaveSpectateFn removes a spectator row at the service layer.
	leaveSpectateFn func(roomID, userID string) *errcode.Error
	// DeleteRoomIfEmptyFn deletes a room from the DB if it has no players
	// or spectators. Wired via SetDeleteRoomIfEmptyFunc.
	deleteRoomIfEmptyFn func(roomID string) (bool, *errcode.Error)
	// CleanupGameStateFn removes in-memory game state for all managers.
	// Wired via SetGameManagerCleanupFunc.
	cleanupGameStateFn func(roomID string)
}

// NewHub constructs an empty Hub.
func NewHub() *Hub {
	return &Hub{
		clients:            make(map[string]map[*Client]struct{}),
		lobby:              make(map[*Client]struct{}),
		rooms:              make(map[string]map[*Client]struct{}),
		spectators:         make(map[string]map[*Client]struct{}),
		pendingDisconnects: make(map[string]*PendingDisconnect),
		vacancyTimers:      make(map[string]*RoomVacancy),
	}
}

// InitProtoRouter 初始化 proto 消息路由器
// 必须在注册各服务之前调用
func (h *Hub) InitProtoRouter() {
	h.protoRouter = NewProtoRouter(h)
}

// ProtoRouter 暴露路由器（供各服务注册消息）
func (h *Hub) ProtoRouter() *ProtoRouter {
	return h.protoRouter
}

// SetRoomService wires the room-state broadcaster used by join/leave/timeout
// paths. Call once during startup, before the WS handler is mounted.
func (h *Hub) SetRoomService(svc RoomStateBroadcaster) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.roomSvc = svc
}

// SetLeaveRoomFunc wires the function that physically removes a player from
// the DB and game manager. Called by the 15s auto-kick timer; idempotent on
// the service side (LeaveRoom returns ErrRoomNotIn if already gone).
func (h *Hub) SetLeaveRoomFunc(fn func(roomID, userID string) *errcode.Error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.leaveRoomFn = fn
}

// SetLeaveSpectateFunc wires the spectator-cleanup counterpart of
// SetLeaveRoomFunc.
func (h *Hub) SetLeaveSpectateFunc(fn func(roomID, userID string) *errcode.Error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.leaveSpectateFn = fn
}

// SetDeleteRoomIfEmptyFunc wires the service-layer room deletion callback.
func (h *Hub) SetDeleteRoomIfEmptyFunc(fn func(roomID string) (bool, *errcode.Error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deleteRoomIfEmptyFn = fn
}

// SetGameManagerCleanupFunc wires the in-memory game-state cleanup callback.
func (h *Hub) SetGameManagerCleanupFunc(fn func(roomID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupGameStateFn = fn
}

// Register adds a client to the per-user connection set.
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.clients[c.UserID]
	if !ok {
		set = make(map[*Client]struct{})
		h.clients[c.UserID] = set
	}
	set[c] = struct{}{}
	logger.L().Info("ws connected",
		zap.String("user_id", c.UserID),
		zap.String("remote", c.RemoteAddr))
}

// Unregister removes a client from every index it joined.
func (h *Hub) Unregister(c *Client) {
	var roomID string
	var specRoomID string
	h.mu.Lock()
	if set, ok := h.clients[c.UserID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.UserID)
		}
	}
	delete(h.lobby, c)

	// 检查用户是否在房间中,如果在则启动掉线定时器
	for rid, set := range h.rooms {
		if _, ok := set[c]; ok {
			roomID = rid
			delete(set, c)
			break
		}
	}
	// 观察者集合:记录 roomID 但**不**启动 15s 宽限 —— observer 断线
	// 表示"不再围观",应立即清理 t_lsm_game_player(role='spectator') 行,
	// 否则同一 user 重新 spectate 同一 room 时会触发 ErrAlreadyInOtherRole
	// (30012)。leaveSpectateFn 由 main.go 注入的 RoomService.LeaveSpectate
	// 在 DB 上幂等,call 后我们直接清理。
	for rid, set := range h.spectators {
		if _, ok := set[c]; ok {
			specRoomID = rid
			delete(set, c)
			if len(set) == 0 {
				delete(h.spectators, rid)
			}
			break
		}
	}
	h.mu.Unlock()

	logger.L().Info("ws disconnected",
		zap.String("user_id", c.UserID),
		zap.String("remote", c.RemoteAddr))

	// ⚠️ 必须在 h.mu 释放后调用:startDisconnectTimer 内部要获取 h.mu.RLock
	// 广播房间状态,如果在 Lock 状态下调用会自死锁(同一 goroutine 不能既
	// 持写锁又申请读锁),从而 15 秒定时器永远起不来。
	if roomID != "" {
		h.startDisconnectTimer(c.UserID, roomID)
	}
	if specRoomID != "" {
		h.cleanupSpectator(c.UserID, specRoomID)
	}
	// 玩家/观战者断线清理后,如果房间已无任何在线用户,启动 5 分钟空房删除计时。
	if roomID != "" && h.isRoomEffectivelyEmpty(roomID) {
		h.maybeStartVacancyTimer(roomID)
	}
}

// cleanupSpectator removes a spectator row in DB and broadcasts room.state
// to lobby subscribers so the 👁 N indicator decrements. Called when a
// spectator's WS connection drops (page closed, navigated away, network loss)
// without sending game.unspectate.
func (h *Hub) cleanupSpectator(userID, roomID string) {
	h.mu.RLock()
	leave := h.leaveSpectateFn
	h.mu.RUnlock()
	if leave == nil {
		return
	}
	if e := leave(roomID, userID); e != nil {
		// 房间已被 force-disband / 清空时,leaveSpectateFn 返回
		// ErrRoomNotIn (30004) 或 ErrRoomNotFound (30001) 属于预期行为
		// (admin 已删除该行),降级为 Debug 避免刷屏 WARN。其他码(如 ErrDB)
		// 仍为真故障,保留 WARN。
		if e.Code == errcode.ErrRoomNotIn || e.Code == errcode.ErrRoomNotFound {
			logger.L().Debug("spectator auto-cleanup: room already gone (skipped)",
				zap.String("user_id", userID),
				zap.String("room_id", roomID),
				zap.Int("code", e.Code))
		} else {
			logger.L().Warn("spectator auto-cleanup failed",
				zap.String("user_id", userID),
				zap.String("room_id", roomID),
				zap.Int("code", e.Code))
		}
		return
	}
	logger.L().Info("spectator auto-cleaned on disconnect",
		zap.String("user_id", userID),
		zap.String("room_id", roomID))
	// 通知大厅刷新。
	h.broadcastRoomStateChange(roomID, "spectator_left", userID)
}

// BroadcastTo sends an envelope to every connection of the given user.
func (h *Hub) BroadcastTo(userID string, env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.clients[userID]; ok {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// startDisconnectTimer starts a 15-second timer for a disconnected player.
// If the player doesn't reconnect within the grace period, they are automatically
// removed from the room.
func (h *Hub) startDisconnectTimer(userID, roomID string) {
	// 如果已有定时器，先取消
	if pd, exists := h.pendingDisconnects[userID]; exists {
		pd.Timer.Stop()
	}

	logger.L().Warn("Player disconnected, starting 15s timer",
		zap.String("user_id", userID),
		zap.String("room_id", roomID))

	// 广播房间状态变化（玩家掉线中）
	h.broadcastRoomPlayerStatus(roomID, "disconnecting", userID)
	// 推送给大厅订阅者：座位暂时保留 15 秒，期间房间视为 “等待重连”。
	h.broadcastRoomStateChange(roomID, "player_disconnecting", userID)

	timer := time.NewTimer(15 * time.Second)
	stopCh := make(chan struct{})

	h.pendingDisconnects[userID] = &PendingDisconnect{
		RoomID:         roomID,
		Timer:          timer,
		StopCh:         stopCh,
		DisconnectedAt: time.Now(),
	}

	// 启动独立 goroutine 等待 15 秒; 期间如果被 CancelDisconnectTimer 关闭
	// StopCh,本 goroutine 提前退出,handleDisconnectTimeout 不会被调用。
	// 选 time.NewTimer + goroutine 而不是 time.AfterFunc 是为了在
	// 取消时更可控 (Stop 后 timer.C 不会再被写,select 会走 stopCh 分支)。
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("disconnect timer goroutine panic",
					zap.String("user_id", userID),
					zap.String("room_id", roomID),
					zap.Any("recover", r))
			}
		}()
		select {
		case <-timer.C:
			logger.L().Warn("15s disconnect timer fired",
				zap.String("user_id", userID),
				zap.String("room_id", roomID))
			h.handleDisconnectTimeout(userID, roomID)
		case <-stopCh:
			// 被取消（用户重连），什么也不做。
		}
	}()
}

// CancelDisconnectTimer cancels a pending disconnect timer when a player reconnects.
//
// IMPORTANT: broadcast functions (broadcastRoomPlayerStatus, broadcastRoomStateChange)
// acquire h.mu.RLock() internally. sync.RWMutex is NOT reentrant in Go, so we must
// release h.mu.Lock() BEFORE calling them — otherwise we deadlock.
func (h *Hub) CancelDisconnectTimer(userID string) {
	var (
		exists bool
		pd     *PendingDisconnect
	)
	h.mu.Lock()
	if p, ok := h.pendingDisconnects[userID]; ok {
		pd = p
		exists = true
		pd.Timer.Stop()
		if pd.StopCh != nil {
			close(pd.StopCh)
		}
		delete(h.pendingDisconnects, userID)
	}
	h.mu.Unlock()

	if !exists || pd == nil {
		return
	}

	logger.L().Info("Player reconnected, canceling timer",
		zap.String("user_id", userID),
		zap.String("room_id", pd.RoomID))

	// 广播房间状态变化（玩家已重连） — must be OUTSIDE the lock to avoid
	// deadlock: these functions acquire h.mu.RLock() internally.
	h.broadcastRoomPlayerStatus(pd.RoomID, "reconnected", userID)
	h.broadcastRoomStateChange(pd.RoomID, "player_reconnected", userID)
	// 玩家重连,取消可能存在的空房删除计时。
	h.cancelVacancyTimer(pd.RoomID)
}

// handleDisconnectTimeout is called when a player's disconnect timer expires.
// The player is automatically removed from the room: DB row deleted, game
// manager state cleared, and a fresh room.state snapshot is broadcast to
// lobby subscribers so the seat frees up immediately.
func (h *Hub) handleDisconnectTimeout(userID, roomID string) {
	h.mu.Lock()
	delete(h.pendingDisconnects, userID)
	h.mu.Unlock()

	logger.L().Warn("Player timed out, removing from room",
		zap.String("user_id", userID),
		zap.String("room_id", roomID))

	// 广播房间状态变化（玩家已离开）给仍然在房间里的连接。
	h.broadcastRoomPlayerStatus(roomID, "removed", userID)

	// 真正的清理：调用 service.LeaveRoom 让 DB 行 / game manager 同步更新。
	// LeaveRoom 内部是幂等的（玩家不存在时返回 ErrRoomNotIn），所以即使
	// 客户端在最后一秒通过 room.leave 主动退出，再次调用也是安全的。
	h.mu.RLock()
	leave := h.leaveRoomFn
	leaveSpec := h.leaveSpectateFn
	h.mu.RUnlock()

	// 优先判断用户是不是观察者：观察者走 leaveSpectate，否则走 leaveRoom。
	// 这里复用 hub.spectators 的查询结果——只有当目标 set 中没有该 userID 的
	// 在线连接时才会进 timeout 分支，所以 spectators 集合也不会再有它的条目。
	isSpectator := false
	h.mu.RLock()
	if set, ok := h.spectators[roomID]; ok {
		for c := range set {
			if c.UserID == userID {
				isSpectator = true
				break
			}
		}
	}
	h.mu.RUnlock()

	if isSpectator {
		if leaveSpec != nil {
			if e := leaveSpec(roomID, userID); e != nil {
				logger.L().Warn("auto-kick spectator cleanup failed",
					zap.String("user_id", userID),
					zap.String("room_id", roomID),
					zap.Int("code", e.Code))
			}
		}
	} else if leave != nil {
		if e := leave(roomID, userID); e != nil {
			logger.L().Warn("auto-kick player cleanup failed",
				zap.String("user_id", userID),
				zap.String("room_id", roomID),
				zap.Int("code", e.Code))
		}
	}

	// 通知大厅订阅者：座位已释放 / 房间可能已被删除。
	h.broadcastRoomStateChange(roomID, "player_removed", userID)

	// 15 秒掉线宽限结束后,如果房间已无任何在线用户,启动 5 分钟空房删除计时。
	if h.isRoomEffectivelyEmpty(roomID) {
		h.maybeStartVacancyTimer(roomID)
	}
}

// NotifyRoomChanged is invoked by the room service when a user joins/leaves
// a room (via HTTP or WS) so the lobby can refresh in real time. Safe to call
// before SetRoomService — the call is a no-op when no broadcaster is wired.
func (h *Hub) NotifyRoomChanged(roomID, action, userID string) {
	h.broadcastRoomStateChange(roomID, action, userID)
}

// broadcastRoomStateChange sends a `room.state` envelope to every lobby
// subscriber so the room list / capacity display refreshes immediately.
//
// action: "player_joined" | "player_left" | "player_disconnecting" |
//
//	"player_reconnected" | "player_removed" | "room_created" | "room_deleted"
func (h *Hub) broadcastRoomStateChange(roomID, action, userID string) {
	h.mu.RLock()
	svc := h.roomSvc
	h.mu.RUnlock()

	var room map[string]any
	if svc != nil {
		id, gk, cap, cnt, status, ok := svc.GetRoomInfo(roomID)
		if snap := buildRoomStateSnapshot(id, gk, cap, cnt, status, ok); snap != nil {
			room = snap.ToInfo()
		}
	}
	if room == nil {
		// 房间可能已被删除（最后一个玩家离开），给前端一个占位对象以移除卡片。
		room = (&RoomStateSnapshot{}).ToInfo()
		room["id"] = roomID
		room["status"] = "removed"
	}

	payload, _ := json.Marshal(map[string]any{
		"room":    room,
		"action":  action,
		"user_id": userID,
	})
	env := Envelope{Type: "room.state", Payload: payload}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.lobby {
		select {
		case c.send <- env:
		default:
		}
	}
}

// broadcastRoomPlayerStatus broadcasts a player status change to all room subscribers.
func (h *Hub) broadcastRoomPlayerStatus(roomID, action, userID string) {
	payload, _ := json.Marshal(map[string]any{
		"room_id": roomID,
		"action":  action,
		"user_id": userID,
	})
	env := Envelope{Type: "room.player_status", Payload: payload}

	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.rooms[roomID]; ok {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// SubscribeLobby adds a client to the lobby broadcast set.
func (h *Hub) SubscribeLobby(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lobby[c] = struct{}{}
}

// UnsubscribeLobby removes a client from the lobby broadcast set.
func (h *Hub) UnsubscribeLobby(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.lobby, c)
}

// SubscribeRoom adds a client to a room's broadcast set.
func (h *Hub) SubscribeRoom(roomID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.rooms[roomID]
	if !ok {
		set = make(map[*Client]struct{})
		h.rooms[roomID] = set
	}
	set[c] = struct{}{}
	// 有新用户进入房间,取消可能存在的空房删除计时。
	h.cancelVacancyTimerLocked(roomID)
}

// UnsubscribeRoom removes a client from a room's broadcast set.
func (h *Hub) UnsubscribeRoom(roomID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.rooms[roomID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.rooms, roomID)
		}
	}
	// 客户端主动离开房间后,如果该房间已无任何在线用户,启动 5 分钟空房删除计时。
	if h.isRoomEffectivelyEmptyLocked(roomID) {
		h.maybeStartVacancyTimerLocked(roomID)
	} else {
		h.cancelVacancyTimerLocked(roomID)
	}
}

// UnsubscribeRoomAll detaches every Client currently subscribed to roomID
// from BOTH the players set (rooms) and the spectators set (spectators),
// without firing any further broadcast or starting a vacancy timer.
//
// Used by BroadcastRoomRemoved — the `game.removed` envelope has already
// been enqueued for every subscriber by the caller; this method only
// updates the hub's bookkeeping so a subsequent hub.rooms[roomID] lookup
// returns empty (otherwise a reconnecting client could resurrect the
// dead room's broadcast set).
//
// No-op when roomID has no players or spectators. We deliberately do NOT
// start a vacancy timer — the room is being torn down by an explicit
// admin / boot-cleanup path, not because it just went empty organically.
func (h *Hub) UnsubscribeRoomAll(roomID string) {
	if roomID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, roomID)
	delete(h.spectators, roomID)
	// Also drop any in-flight vacancy timer so it can't fire and try to
	// DeleteRoomIfEmpty a row that is already gone (or that the disband
	// caller is about to DELETE). CancelVacancyTimerLocked is a no-op
	// when no timer exists.
	h.cancelVacancyTimerLocked(roomID)
}

// BroadcastLobby sends to every lobby subscriber.
func (h *Hub) BroadcastLobby(env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.lobby {
		select {
		case c.send <- env:
		default:
		}
	}
}

// BroadcastRoom sends to every subscriber of the given room.
func (h *Hub) BroadcastRoom(roomID string, env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.rooms[roomID]; ok {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// ─────────────────── Proto 广播方法 ───────────────────
//
// 与 JSON 版本并行，仅向已协商启用 proto 的客户端发送二进制帧。
// 双写策略：业务代码同时调用 BroadcastRoom + BroadcastRoomProto，
// 客户端根据能力接收对应格式。

// BroadcastLobbyProto 向所有大厅订阅者（已启用 proto）发送二进制帧
func (h *Hub) BroadcastLobbyProto(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.lobby {
		if !c.protoEnabled {
			continue
		}
		select {
		case c.protoSend <- data:
		default:
		}
	}
}

// BroadcastRoomProto 向房间内所有订阅者（已启用 proto）发送二进制帧
func (h *Hub) BroadcastRoomProto(roomID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.rooms[roomID]; ok {
		for c := range set {
			if !c.protoEnabled {
				continue
			}
			select {
			case c.protoSend <- data:
			default:
			}
		}
	}
}

// BroadcastToProto 向指定用户（已启用 proto）发送二进制帧
func (h *Hub) BroadcastToProto(userID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.clients[userID]; ok {
		for c := range set {
			if !c.protoEnabled {
				continue
			}
			select {
			case c.protoSend <- data:
			default:
			}
		}
	}
}

// BroadcastRoomSpectatorsProto 向房间观战者（已启用 proto）发送二进制帧
func (h *Hub) BroadcastRoomSpectatorsProto(roomID string, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.spectators[roomID]; ok {
		for c := range set {
			if !c.protoEnabled {
				continue
			}
			select {
			case c.protoSend <- data:
			default:
			}
		}
	}
}

// BroadcastAllProto 向所有在线客户端（已启用 proto）发送二进制帧
func (h *Hub) BroadcastAllProto(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range h.clients {
		for c := range set {
			if !c.protoEnabled {
				continue
			}
			select {
			case c.protoSend <- data:
			default:
			}
		}
	}
}

// ─────────────────── Spectator helpers ───────────────────
//
// The spectator set is intentionally separated from `rooms` so that:
//   1. Player broadcasts (game.moved, game.started, game.action_accepted …) are
//      NOT delivered to spectators.
//   2. Spectator broadcasts (sanitized game.state, game.spectators updates) are
//      NOT delivered to players.
//   3. Chat broadcasts that include both groups explicitly cross-fan-out via
//      `BroadcastRoomIncludingSpectators`, never by merging the two sets.

// SpectateRoom registers a client as an observer of a room. The same client
// may be registered for many rooms. Idempotent.
func (h *Hub) SpectateRoom(roomID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.spectators[roomID]
	if !ok {
		set = make(map[*Client]struct{})
		h.spectators[roomID] = set
	}
	set[c] = struct{}{}
	// 有新观战者进入,取消可能存在的空房删除计时。
	h.cancelVacancyTimerLocked(roomID)
}

// UnspectateRoom removes a client from the spectator set of a room.
func (h *Hub) UnspectateRoom(roomID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.spectators[roomID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.spectators, roomID)
		}
	}
	// 观战者离开后,如果房间已无任何在线用户,启动 5 分钟空房删除计时。
	if h.isRoomEffectivelyEmptyLocked(roomID) {
		h.maybeStartVacancyTimerLocked(roomID)
	} else {
		h.cancelVacancyTimerLocked(roomID)
	}
}

// BroadcastRoomSpectators sends an envelope to every spectator subscribed to
// the room. Players do NOT receive this envelope.
func (h *Hub) BroadcastRoomSpectators(roomID string, env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.spectators[roomID]; ok {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// BroadcastRoomIncludingSpectators sends an envelope to every player AND every
// spectator of the given room. Used exclusively by the chat fan-out so that
// spectator messages appear in the player's chat panel and vice versa.
func (h *Hub) BroadcastRoomIncludingSpectators(roomID string, env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.rooms[roomID]; ok {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
	if set, ok := h.spectators[roomID]; ok {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// BroadcastRoomIncludingSpectatorsExcluding is identical to
// BroadcastRoomIncludingSpectators except that any client whose UserID appears
// in `excludeUserIDs` is skipped. Used by the chat-service to keep the full
// whisper text off the wire for non-participants — see BUG-R75-WHISPER-REDACT.
//
// Multiple connections under the same userID (e.g. a player with two tabs, or
// a bot whose UserID collides with a spectator record) are all excluded if
// the id matches. excludeUserIDs is typically {sender, recipient}.
func (h *Hub) BroadcastRoomIncludingSpectatorsExcluding(roomID string, env Envelope, excludeUserIDs ...string) {
	if len(excludeUserIDs) == 0 {
		h.BroadcastRoomIncludingSpectators(roomID, env)
		return
	}
	excluded := make(map[string]struct{}, len(excludeUserIDs))
	for _, id := range excludeUserIDs {
		if id == "" {
			continue
		}
		excluded[id] = struct{}{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.rooms[roomID]; ok {
		for c := range set {
			if _, skip := excluded[c.UserID]; skip {
				continue
			}
			select {
			case c.send <- env:
			default:
			}
		}
	}
	if set, ok := h.spectators[roomID]; ok {
		for c := range set {
			if _, skip := excluded[c.UserID]; skip {
				continue
			}
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// RoomSpectatorCount returns the number of currently connected spectator
// clients for the given room. The lobby uses this for the "观战 N" indicator.
func (h *Hub) RoomSpectatorCount(roomID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.spectators[roomID]; ok {
		return len(set)
	}
	return 0
}

// IsSpectatorOf reports whether the given client is currently registered as a
// spectator of the given room. Used by the chat-service to stamp `from_role`.
func (h *Hub) IsSpectatorOf(roomID string, c *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.spectators[roomID]; ok {
		_, found := set[c]
		return found
	}
	return false
}

// connectedSpectatorUserIDs returns the userIDs of currently-connected
// spectator clients for a room. The game service uses this to fan out a
// sanitized state to every spectator without rebuilding per-user.
func (h *Hub) connectedSpectatorUserIDs(roomID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	set, ok := h.spectators[roomID]
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(set))
	out := make([]string, 0, len(set))
	for c := range set {
		if _, dup := seen[c.UserID]; dup {
			continue
		}
		seen[c.UserID] = struct{}{}
		out = append(out, c.UserID)
	}
	return out
}

// LobbySubscriberCount returns the size of the lobby set (debug/health).
func (h *Hub) LobbySubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.lobby)
}

// RoomSubscriberCount returns the size of a room's subscriber set.
func (h *Hub) RoomSubscriberCount(roomID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if set, ok := h.rooms[roomID]; ok {
		return len(set)
	}
	return 0
}

// RoomUserIDs returns deduplicated user IDs currently subscribed to the room.
// Used by the chat whisper feature to populate the @mention autocomplete list.
func (h *Hub) RoomUserIDs(roomID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	set, ok := h.rooms[roomID]
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(set))
	out := make([]string, 0, len(set))
	for c := range set {
		if _, dup := seen[c.UserID]; !dup {
			seen[c.UserID] = struct{}{}
			out = append(out, c.UserID)
		}
	}
	return out
}

// BroadcastAll sends an envelope to every connected client across all users.
func (h *Hub) BroadcastAll(env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range h.clients {
		for c := range set {
			select {
			case c.send <- env:
			default:
			}
		}
	}
}

// KickUser closes every WebSocket connection belonging to the given user.
// The corresponding ReadPump/WritePump goroutines exit automatically and
// trigger the deferred Unregister cleanup. Used by admin delete flows.
func (h *Hub) KickUser(userID string) {
	h.mu.RLock()
	set, ok := h.clients[userID]
	h.mu.RUnlock()
	if !ok || len(set) == 0 {
		return
	}
	// Take a snapshot under RLock, kick outside lock to avoid holding it
	// while doing IO.
	clients := make([]*Client, 0, len(set))
	h.mu.RLock()
	for c := range set {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	payload, _ := json.Marshal(map[string]any{
		"code":    40010,
		"message": "账户已被管理员删除",
	})
	env := Envelope{Type: "user.kicked", Payload: payload}
	for _, c := range clients {
		// Best-effort: send kick notification, then close the socket.
		select {
		case c.send <- env:
		default:
		}
		_ = c.conn.Close()
	}
}

// SendToUser delivers a single envelope to every connection currently
// registered for userID. Best-effort non-blocking write; drops the envelope
// if the per-client send channel is full. Returns the number of clients the
// envelope was pushed to (0 if userID has no live connection).
// BUG-WEREWOLF-R55-WHISPER: needed to deliver full whisper text only to the
// recipient without broadcasting to the rest of the room.
func (h *Hub) SendToUser(userID string, env Envelope) int {
	if userID == "" {
		return 0
	}
	h.mu.RLock()
	set, ok := h.clients[userID]
	if !ok {
		h.mu.RUnlock()
		return 0
	}
	clients := make([]*Client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	delivered := 0
	for _, c := range clients {
		select {
		case c.send <- env:
			delivered++
		default:
		}
	}
	return delivered
}

// ConnectedUserIDs returns the list of currently connected users.
func (h *Hub) ConnectedUserIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.clients))
	for uid := range h.clients {
		out = append(out, uid)
	}
	return out
}

// RunHeartbeat pushes a heartbeat envelope to every connected client on a tick.
// It blocks until the hub is asked to stop.
func (h *Hub) RunHeartbeat(stop <-chan struct{}) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			payload, _ := json.Marshal(map[string]any{"server_ts": now.UnixMilli()})
			env := Envelope{Type: "heartbeat", Payload: payload}
			h.mu.RLock()
			for _, set := range h.clients {
				for c := range set {
					select {
					case c.send <- env:
					default:
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

const roomVacancyTimeout = 5 * time.Minute

// isRoomEffectivelyEmpty reports whether a room has no connected players,
// no connected spectators, and no pending disconnect grace timers. Safe to
// call with or without the lock held.
func (h *Hub) isRoomEffectivelyEmpty(roomID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isRoomEffectivelyEmptyLocked(roomID)
}

// IsRoomEmpty is the public, lock-free wrapper around
// isRoomEffectivelyEmpty. Exposed for the boot cleanup pass in
// service.RoomService.BootCleanupOrphanedAgentRooms so it can decide
// whether an orphan agent room has any live clients before force-disbanding
// it.
//
// Returns true when the hub has no record of roomID at all (which is the
// common case after a restart — the in-memory broadcast sets are
// per-process).
func (h *Hub) IsRoomEmpty(roomID string) bool {
	return h.isRoomEffectivelyEmpty(roomID)
}

func (h *Hub) isRoomEffectivelyEmptyLocked(roomID string) bool {
	if set, ok := h.rooms[roomID]; ok && len(set) > 0 {
		return false
	}
	if set, ok := h.spectators[roomID]; ok && len(set) > 0 {
		return false
	}
	for _, pd := range h.pendingDisconnects {
		if pd.RoomID == roomID {
			return false
		}
	}
	return true
}

// maybeStartVacancyTimer starts (or restarts) the 5-minute vacancy timer for
// a room. The caller must ensure the room is effectively empty first.
func (h *Hub) maybeStartVacancyTimer(roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.maybeStartVacancyTimerLocked(roomID)
}

func (h *Hub) maybeStartVacancyTimerLocked(roomID string) {
	if !h.isRoomEffectivelyEmptyLocked(roomID) {
		return
	}
	// If a timer already exists, stop it first so we don't leak goroutines.
	if v, exists := h.vacancyTimers[roomID]; exists {
		v.Timer.Stop()
		if v.StopCh != nil {
			close(v.StopCh)
		}
		delete(h.vacancyTimers, roomID)
	}

	logger.L().Info("Room vacant, starting 5min deletion timer",
		zap.String("room_id", roomID))

	timer := time.NewTimer(roomVacancyTimeout)
	stopCh := make(chan struct{})
	h.vacancyTimers[roomID] = &RoomVacancy{
		RoomID:    roomID,
		Timer:     timer,
		StopCh:    stopCh,
		VacatedAt: time.Now(),
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.L().Error("vacancy timer goroutine panic",
					zap.String("room_id", roomID),
					zap.Any("recover", r))
			}
		}()
		select {
		case <-timer.C:
			logger.L().Info("5min vacancy timer fired",
				zap.String("room_id", roomID))
			h.onRoomVacancyTimeout(roomID)
		case <-stopCh:
			// cancelled — room is no longer empty
		}
	}()
}

// cancelVacancyTimer stops the pending deletion timer for a room, if any.
func (h *Hub) cancelVacancyTimer(roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancelVacancyTimerLocked(roomID)
}

func (h *Hub) cancelVacancyTimerLocked(roomID string) {
	if v, exists := h.vacancyTimers[roomID]; exists {
		v.Timer.Stop()
		if v.StopCh != nil {
			close(v.StopCh)
		}
		delete(h.vacancyTimers, roomID)
		logger.L().Info("Room no longer vacant, cancelled deletion timer",
			zap.String("room_id", roomID))
	}
}

// onRoomVacancyTimeout is called after a room has been empty for 5 minutes.
// It asks the service layer to delete the room if it is still empty, cleans
// up in-memory game state, and broadcasts the deletion to the lobby.
func (h *Hub) onRoomVacancyTimeout(roomID string) {
	h.mu.Lock()
	delete(h.vacancyTimers, roomID)
	h.mu.Unlock()

	h.mu.RLock()
	deleteRoom := h.deleteRoomIfEmptyFn
	cleanupGame := h.cleanupGameStateFn
	h.mu.RUnlock()

	if deleteRoom == nil {
		return
	}

	deleted, err := deleteRoom(roomID)
	if err != nil {
		logger.L().Warn("vacancy room deletion failed",
			zap.String("room_id", roomID),
			zap.Int("code", err.Code))
		return
	}
	if !deleted {
		logger.L().Info("vacancy room deletion skipped: room no longer empty",
			zap.String("room_id", roomID))
		return
	}

	logger.L().Info("Room auto-deleted after 5 minutes of vacancy",
		zap.String("room_id", roomID))

	if cleanupGame != nil {
		cleanupGame(roomID)
	}

	// 通知大厅订阅者移除房间卡片。
	h.broadcastRoomStateChange(roomID, "room_deleted", "")
}