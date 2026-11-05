// Package citybus — bus.go: 虚拟城市 Agent 消息总线(2026-11-03 §批次81;
// 2026-11-05 §批次84 扩展 Agent 目录能力)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md §3.1;
//       lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md §4.3.1。
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
//
// 批次84 扩展(§4.3.1)**只增不改**:既有 Register / RegisterRoom / UnregisterRoom
// / Send / Post / Agents 的语义与返回值**逐字不变**;新增:
//   - AgentDescriptor + Describe(roomID):结构化 Agent 目录(类级 + 房间级);
//   - RegisterWith / RegisterRoomWith:注册 handler 的同时登记能力描述
//     (消息类型 + 一句话描述),供 list_agents 展示。
// 旧 Register/RegisterRoom 语义保留:未登记描述 ⇒ MessageTypes 为空、Description 为空。
package citybus

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// AgentScope 描述一个已注册 Agent 的作用域。
const (
	// ScopeClass 类级 Agent(全局唯一注册)。
	ScopeClass = "class"
	// ScopeRoom 房间级 Agent(每房一个实例)。
	ScopeRoom = "room"
)

// AgentDescriptor 描述一个已注册 Agent(批次84 §4.3.1)。
//
// 由 Describe(roomID) 返回:类级 Agent(Scope=ScopeClass)+ 该房间的房间级
// Agent(Scope=ScopeRoom, RoomID 非空)。MessageTypes / Description 来自
// RegisterWith / RegisterRoomWith 的登记;未登记时为空。
type AgentDescriptor struct {
	// ClassName 是 AgentClassName(class_names.go 常量)。
	ClassName string `json:"class_name"`
	// Scope 是 "class" | "room"。
	Scope string `json:"scope"`
	// RoomID 房间级 Agent 所属房间(类级为空)。
	RoomID string `json:"room_id,omitempty"`
	// MessageTypes 该 Agent 支持的消息类型(未登记为空)。
	MessageTypes []string `json:"message_types,omitempty"`
	// Description 一句话能力描述(未登记为空)。
	Description string `json:"description,omitempty"`
}

// agentMeta 是注册时登记的能力元信息(消息类型 + 描述)。
type agentMeta struct {
	types []string
	desc  string
}

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
	// classMeta 类级 Agent 的能力元信息(批次84;未登记则无条目)。
	classMeta map[string]agentMeta
	// roomMeta 房间级 Agent 的能力元信息(批次84;roomID → class → meta)。
	roomMeta map[string]map[string]agentMeta

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
		classes:   make(map[string]Handler),
		rooms:     make(map[string]map[string]Handler),
		classMeta: make(map[string]agentMeta),
		roomMeta:  make(map[string]map[string]agentMeta),
		posts:     make(chan postItem, postQueueCap),
		done:      make(chan struct{}),
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
	return h.RegisterWith(class, nil, "", handler)
}

// RegisterWith 注册类级 handler **并登记能力描述**(批次84 §4.3.1)。
//
// 与 Register 的差别**仅在元信息**:types 声明该 Agent 支持的消息类型,
// desc 是一句话能力描述(供 list_agents 展示)。两者都可为空 —— 此时
// Describe 返回的该条目 MessageTypes/Description 为空(与 Register 等价)。
//
// 校验与重复注册语义与 Register **逐字一致**(空 class / nil handler /
// 重复注册 ⇒ error)。
func (h *Hub) RegisterWith(class string, types []string, desc string, handler Handler) error {
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
	h.classMeta[class] = agentMeta{types: copyTypes(types), desc: desc}
	return nil
}

// RegisterRoom 注册房间级 handler(同房同类重复注册 ⇒ error)。
func (h *Hub) RegisterRoom(roomID, class string, handler Handler) error {
	return h.RegisterRoomWith(roomID, class, nil, "", handler)
}

// RegisterRoomWith 注册房间级 handler **并登记能力描述**(批次84 §4.3.1)。
//
// 语义与 RegisterRoom **逐字一致**,仅额外记录 types / desc 元信息。
func (h *Hub) RegisterRoomWith(roomID, class string, types []string, desc string, handler Handler) error {
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
	mm := h.roomMeta[roomID]
	if mm == nil {
		mm = make(map[string]agentMeta)
		h.roomMeta[roomID] = mm
	}
	mm[class] = agentMeta{types: copyTypes(types), desc: desc}
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
	if mm := h.roomMeta[roomID]; mm != nil {
		delete(mm, class)
		if len(mm) == 0 {
			delete(h.roomMeta, roomID)
		}
	}
}

// Describe 返回已注册 Agent 的结构化目录(批次84 §4.3.1):**类级 Agent +
// 该房间的房间级 Agent**。
//
// 排序稳定:先类级(按 ClassName 升序),后房间级(按 ClassName 升序)。
// 房间级只返回 roomID 指定的房间;其他房间的房间级 Agent 不可见(房间隔离)。
//
// MessageTypes / Description 来自 RegisterWith / RegisterRoomWith 的登记;
// 旧 Register / RegisterRoom 注册的 Agent 这两项为空(不影响发现)。
func (h *Hub) Describe(roomID string) []AgentDescriptor {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]AgentDescriptor, 0, len(h.classes)+len(h.rooms[roomID]))
	for class := range h.classes {
		meta := h.classMeta[class]
		out = append(out, AgentDescriptor{
			ClassName:    class,
			Scope:        ScopeClass,
			MessageTypes: copyTypes(meta.types),
			Description:  meta.desc,
		})
	}
	for class := range h.rooms[roomID] {
		var meta agentMeta
		if mm := h.roomMeta[roomID]; mm != nil {
			meta = mm[class]
		}
		out = append(out, AgentDescriptor{
			ClassName:    class,
			Scope:        ScopeRoom,
			RoomID:       roomID,
			MessageTypes: copyTypes(meta.types),
			Description:  meta.desc,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope == ScopeClass
		}
		return out[i].ClassName < out[j].ClassName
	})
	return out
}

// copyTypes 复制消息类型切片(避免调用方后续修改污染注册表)。
func copyTypes(types []string) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, len(types))
	copy(out, types)
	return out
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
