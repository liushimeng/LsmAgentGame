// Package debate — 观战者支持 + 人类"非参赛"管理。
//
// 2026-08-31 §20260831-01 — 辩论比赛是纯 Agent 参赛游戏,人类身份仅:
//   - 房主(创建者)
//   - 观众(观战者)
//
// 两种身份均可连接房间、订阅 WS 帧、聊天互动,但**不可**发送 bot action 帧。
// 与狼人杀观战者架构对齐:Hub.rooms(玩家)+ Hub.spectators(观众)互不相交。
//
// 详细设计见 docs/架构与协议/观战者架构.md + docs/辩论比赛/04-辩论比赛界面与交互设计.md §1。
package debate

import (
	"sync"
)

// SpectatorKind 观战者类型(暂只支持"观众",预留扩展)。
type SpectatorKind string

const (
	SpectatorKindViewer    SpectatorKind = "viewer"    // 普通观众
	SpectatorKindHost      SpectatorKind = "host"      // 房主(在 PhaseFilling 时也可视为观众)
	SpectatorKindResearcher SpectatorKind = "researcher" // 研究用,预留
)

// Spectator 观战者记录。
type Spectator struct {
	UserID  string        `json:"user_id"`
	Kind    SpectatorKind `json:"kind"`
	JoinedAt int64        `json:"joined_at"`
}

// spectatorRegistry 房间级观战者集合(由 DebateRoom 内嵌)。
//
// 线程安全:由内部 mu 保护。
type spectatorRegistry struct {
	mu          sync.RWMutex
	spectators  map[string]*Spectator // userID → Spectator
}

// newSpectatorRegistry 构造空集合。
func newSpectatorRegistry() *spectatorRegistry {
	return &spectatorRegistry{spectators: make(map[string]*Spectator)}
}

// Add 添加观战者(已存在则 no-op + 返回旧记录)。
func (sr *spectatorRegistry) Add(s Spectator) *Spectator {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if existing, ok := sr.spectators[s.UserID]; ok {
		return existing
	}
	if s.JoinedAt == 0 {
		s.JoinedAt = WallNow()
	}
	sr.spectators[s.UserID] = &s
	return &s
}

// Remove 移除观战者。
func (sr *spectatorRegistry) Remove(userID string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	delete(sr.spectators, userID)
}

// Has 判断 userID 是否在观战者集合内。
func (sr *spectatorRegistry) Has(userID string) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	_, ok := sr.spectators[userID]
	return ok
}

// Count 观战者人数。
func (sr *spectatorRegistry) Count() int {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return len(sr.spectators)
}

// List 观战者列表副本。
func (sr *spectatorRegistry) List() []Spectator {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	out := make([]Spectator, 0, len(sr.spectators))
	for _, s := range sr.spectators {
		out = append(out, *s)
	}
	return out
}

// ============================================================================
// 房间辅助方法(放在 room_spectator.go 以避免 room.go 超 1800 行)
// ============================================================================

// AddSpectator 把用户加入本房间的观战者集合。
//
// 房主自动加入观战者集合(即使 PhaseFilling 也能看到 lobby)。
func (r *DebateRoom) AddSpectator(userID string, kind SpectatorKind) *Spectator {
	if r.viewers == nil {
		r.viewers = newSpectatorRegistry()
	}
	s := r.viewers.Add(Spectator{
		UserID: userID, Kind: kind,
	})
	return s
}

// RemoveSpectator 移除观战者。
func (r *DebateRoom) RemoveSpectator(userID string) {
	if r.viewers == nil {
		return
	}
	r.viewers.Remove(userID)
}

// HasSpectator 判断是否在观战。
func (r *DebateRoom) HasSpectator(userID string) bool {
	if r.viewers == nil {
		return r.IsOwner(userID)
	}
	return r.viewers.Has(userID) || r.IsOwner(userID)
}

// SpectatorCount 观战者总数(含房主)。
func (r *DebateRoom) SpectatorCount() int {
	if r.viewers == nil {
		return 1 // 默认 1 房主
	}
	c := r.viewers.Count()
	if !r.viewers.Has(r.Config.CreatedBy) {
		c++ // 房主不在 viewers 内时单独 +1
	}
	return c
}

// IsSpectatorInputAllowed 校验 userID 是否可发送 bot action 帧。
//
// 辩论比赛纯 Agent 参赛,人类不能调 bot action 工具;允许发送的帧仅:
//   - chat.* (聊天室)
//   - debate.spectator_question (向裁判提问)
//   - debate.like (点赞)
//
// 不允许:
//   - debate.submit_speech (发言由 bot 完成)
//   - debate.cross_exam_* (质询由 bot 完成)
func (r *DebateRoom) IsSpectatorInputAllowed(userID string, frameType string) bool {
	// 房主 / 观众可发送 chat 帧与提问帧
	switch frameType {
	case "chat.send", "chat.history", "chat.subscribe", "chat.unsubscribe",
		"debate.spectator_question", "debate.like":
		return true
	}
	return false
}