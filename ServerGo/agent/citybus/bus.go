// Package citybus — bus.go: 虚拟城市 Agent 消息总线(2026-11-03 §批次81)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md §3.1。
//
// 职责:Agent 之间的**注册 / 寻址 / 请求-响应 / 单向投递**。不感知 LLM,
// 不感知引擎 —— handler 内部想调什么都行,本包只做路由。
//
// 两级寻址:
//
//	类级   Register(class, handler)               —— 全局唯一(如 City-Chrome)
//	房间级 RegisterRoom(roomID, class, handler)   —— 每房一个(如 City-WebGL-Show)
//
// Send 房间级优先,回退类级;两者都未注册 ⇒ error("agent not registered: <to>")。
// Post 走有缓冲 channel(cap 64)异步投递,满则丢弃并告警,绝不阻塞调用方
// (WS handler goroutine 不能被慢 Agent 拖住)。
//
// 并发:Register/Unregister/Send 全程持锁;handler 在**调用方 goroutine** 执行,
// 超时由调用方 context 控制(不在此处包一层)。
package citybus

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// postQueueCap 是 Post 异步队列容量(满则丢弃 + Warn,不阻塞)。
const postQueueCap = 64

// Message 是一次 Agent 间通信的载荷。
type Message struct {
	// From / To 是 AgentClassName(class_names.go 常量)。
	From string
	To   string
	// Type 由调用方自定义:"message" | "run_display" | "collect" | ...
	Type string
	// RoomID 目标房间("" = 类级全局目标)。
	RoomID string
	// Payload 任意 JSON 可序列化载荷(由 handler 自行断言)。
	Payload any
	// TraceID 调用链标识("" = 未指定);用于跨 Agent 串一次请求。
	TraceID string
}

// Handler 处理一条消息。返回值作为 Send 的响应;Post 丢弃返回值。
type Handler func(ctx context.Context, msg Message) (any, error)

// Hub 是消息总线。零值不可用,用 New() 或 Default()。
type Hub struct {
	mu sync.RWMutex
	// classes 类级注册表(key = AgentClassName)。
	classes map[string]Handler
	// rooms 房间级注册表(roomID → class → handler)。
	rooms map[string]map[string]Handler

	posts chan postItem
	done  chan struct{}
	once  sync.Once
}

// postItem 是 Post 的队列元素。
type postItem struct {
	ctx     context.Context
	roomID  string
	from    string
	to      string
	typ     string
	payload any
}

// New 构造独立 Hub(测试 / 多实例用;自带 postLoop goroutine,Close 释放)。
func New() *Hub {
	h := &Hub{
		classes: make(map[string]Handler),
		rooms:   make(map[string]map[string]Handler),
		posts:   make(chan postItem, postQueueCap),
		done:    make(chan struct{}),
	}
	go h.postLoop()
	return h
}

// Default 返回包级单例(main.go 装配层使用;全局唯一总线)。
func Default() *Hub {
	return defaultHub
}

var defaultHub = New()

// Close 停止 Post 投递协程(Runtime 退出或测试清理用;幂等)。
func (h *Hub) Close() {
	h.once.Do(func() { close(h.done) })
}

// Register 注册类级 handler(重复注册同一 class ⇒ error)。
func (h *Hub) Register(class string, handler Handler) error {
	if class == "" {
		return fmt.Errorf("citybus: class must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("citybus: handler for %q must not be nil", class)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.classes[class]; ok {
		return fmt.Errorf("citybus: agent class already registered: %s", class)
	}
	h.classes[class] = handler
	return nil
}

// RegisterRoom 注册房间级 handler(同房同类重复注册 ⇒ error)。
func (h *Hub) RegisterRoom(roomID, class string, handler Handler) error {
	if roomID == "" {
		return fmt.Errorf("citybus: room id must not be empty")
	}
	if class == "" {
		return fmt.Errorf("citybus: class must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("citybus: handler for %s/%s must not be nil", roomID, class)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.rooms[roomID]
	if m == nil {
		m = make(map[string]Handler)
		h.rooms[roomID] = m
	}
	if _, ok := m[class]; ok {
		return fmt.Errorf("citybus: agent already registered for room %s: %s", roomID, class)
	}
	m[class] = handler
	return nil
}

// UnregisterRoom 注销房间级 handler(不存在 = no-op)。
func (h *Hub) UnregisterRoom(roomID, class string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.rooms[roomID]
	if m == nil {
		return
	}
	delete(m, class)
	if len(m) == 0 {
		delete(h.rooms, roomID)
	}
}

// lookup 取 handler:房间级优先,回退类级(读锁)。
func (h *Hub) lookup(roomID, class string) Handler {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if m := h.rooms[roomID]; m != nil {
		if fn := m[class]; fn != nil {
			return fn
		}
	}
	return h.classes[class]
}

// Send 同步请求-响应:房间级优先,回退类级;都未注册 ⇒ error。
func (h *Hub) Send(ctx context.Context, roomID, from, to, typ string, payload any) (any, error) {
	handler := h.lookup(roomID, to)
	if handler == nil {
		return nil, fmt.Errorf("agent not registered: %s", to)
	}
	return handler(ctx, Message{
		From: from, To: to, Type: typ, RoomID: roomID, Payload: payload,
	})
}

// Post 单向异步投递:队列满则丢弃并 Warn,不阻塞调用方。
func (h *Hub) Post(ctx context.Context, roomID, from, to, typ string, payload any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	item := postItem{ctx: ctx, roomID: roomID, from: from, to: to, typ: typ, payload: payload}
	select {
	case h.posts <- item:
		return nil
	default:
		logger.L().Warn("citybus: post queue full, message dropped",
			zap.String("room_id", roomID),
			zap.String("from", from),
			zap.String("to", to),
			zap.String("type", typ))
		return nil
	}
}

// postLoop 消费 Post 队列(单协程串行执行,避免慢 handler 阻塞 WS)。
func (h *Hub) postLoop() {
	for {
		select {
		case <-h.done:
			return
		case item := <-h.posts:
			handler := h.lookup(item.roomID, item.to)
			if handler == nil {
				logger.L().Warn("citybus: post target not registered, dropped",
					zap.String("room_id", item.roomID),
					zap.String("to", item.to),
					zap.String("type", item.typ))
				continue
			}
			if _, err := handler(item.ctx, Message{
				From: item.from, To: item.to, Type: item.typ,
				RoomID: item.roomID, Payload: item.payload,
			}); err != nil {
				logger.L().Warn("citybus: post handler failed",
					zap.String("room_id", item.roomID),
					zap.String("to", item.to),
					zap.String("type", item.typ),
					zap.Error(err))
			}
		}
	}
}

// Agents 返回已注册的**类级** Agent 类名(不含房间级),排序后返回。
func (h *Hub) Agents() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.classes))
	for class := range h.classes {
		out = append(out, class)
	}
	sort.Strings(out)
	return out
}
