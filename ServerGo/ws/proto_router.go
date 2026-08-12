// WebSocket Protobuf 消息路由器
//
// 负责注册所有 proto 消息类型及其处理函数，并提供分发入口。
// 各模块（chat/game/room/user/wallet）在初始化时注册自己的消息。
//
// 设计原则：
//   - 与 JSON 路由（各 *Service.HandleClientFrame）逻辑共享
//   - proto handler 只做"反序列化 → 调用共享业务逻辑 → 序列化响应"
//   - 业务逻辑不在此处重复实现

package ws

import (
	"encoding/json"

	"google.golang.org/protobuf/proto"

	commonpb "LsmWebGame/proto/pb/common"
)

// ProtoRouter proto 消息路由器
// 持有注册表和各服务引用
type ProtoRouter struct {
	registry *ProtoRegistry
	hub      *Hub

	// 各服务引用（由 Hub 在初始化时注入）
	chat *ChatService
	game *GameService
	room *RoomWsService
	user *UserWsService
}

// NewProtoRouter 创建路由器
func NewProtoRouter(hub *Hub) *ProtoRouter {
	r := &ProtoRouter{
		registry: NewProtoRegistry(),
		hub:      hub,
	}
	r.registerSystemMessages()
	return r
}

// Registry 暴露注册表（供各模块 Register 调用）
func (r *ProtoRouter) Registry() *ProtoRegistry {
	return r.registry
}

// Dispatch 分发消息
func (r *ProtoRouter) Dispatch(c *Client, env *commonpb.Envelope) error {
	found, err := r.registry.Dispatch(c, env)
	if err != nil {
		return err
	}
	if !found {
		// 未知消息类型 → 回退到 JSON 模式处理（通过 Envelope 桥接）
		return r.bridgeToJSON(c, env)
	}
	return nil
}

// registerSystemMessages 注册系统级消息（协议协商、心跳等）
func (r *ProtoRouter) registerSystemMessages() {
	// 协议协商：客户端声明 proto 能力
	r.registry.Register("system.proto_capability",
		func() proto.Message { return &commonpb.ProtoCapability{} },
		func(c *Client, env *commonpb.Envelope, msg proto.Message) {
			c.handleProtoCapability(msg.(*commonpb.ProtoCapability))
		},
	)
}

// bridgeToJSON 将 proto 帧桥接到 JSON 处理路径
// 对于尚未迁移的消息类型，通过 JSON Envelope 中转，保证功能可用
// 注意：proto payload 是二进制，直接塞给 JSON 路径会解析失败，
// 所以此方法仅用于已迁移的消息回退。未迁移消息应在 handler 层直接处理。
func (r *ProtoRouter) bridgeToJSON(c *Client, env *commonpb.Envelope) error {
	// 构造 JSON 信封（payload 保持原样字节，JSON 路径会尝试解析）
	jsonEnv := Envelope{
		Type:    env.Type,
		Seq:     env.Seq,
		Payload: json.RawMessage(env.Payload),
	}

	// 直接按前缀分发（与 ReadPump 相同逻辑）
	if c.chat != nil && len(env.Type) >= 5 && env.Type[:5] == "chat." {
		c.chat.HandleClientFrame(c, jsonEnv)
		return nil
	}
	if c.game != nil && len(env.Type) >= 5 && env.Type[:5] == "game." {
		c.game.HandleClientFrame(c, jsonEnv)
		return nil
	}
	if c.room != nil && len(env.Type) >= 5 && env.Type[:5] == "room." {
		c.room.HandleClientFrame(c, jsonEnv)
		return nil
	}
	if c.user != nil && len(env.Type) >= 5 && env.Type[:5] == "user." {
		c.user.HandleClientFrame(c, jsonEnv)
		return nil
	}
	// 未知类型 → 回 ack
	jsonEnv.Type = "ack"
	select {
	case c.send <- jsonEnv:
	default:
	}
	return nil
}

// RegisterChatService 注入聊天服务并注册聊天 proto 消息
func (r *ProtoRouter) RegisterChatService(svc *ChatService) {
	r.chat = svc
	svc.registerProtoMessages(r.registry)
}

// RegisterGameService 注入游戏服务并注册游戏 proto 消息
func (r *ProtoRouter) RegisterGameService(svc *GameService) {
	r.game = svc
	svc.registerProtoMessages(r.registry)
}

// RegisterRoomService 注入房间服务并注册房间 proto 消息
func (r *ProtoRouter) RegisterRoomService(svc *RoomWsService) {
	r.room = svc
	svc.registerProtoMessages(r.registry)
}

// RegisterUserService 注入用户服务并注册用户 proto 消息
func (r *ProtoRouter) RegisterUserService(svc *UserWsService) {
	r.user = svc
	svc.registerProtoMessages(r.registry)
}
