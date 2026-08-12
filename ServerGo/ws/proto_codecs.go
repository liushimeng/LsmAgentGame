// WebSocket Protobuf 编解码工具
//
// 提供 proto 模式下的消息编解码、类型注册表等基础能力。
// 与 legacy JSON 模式（hub.go 中的 Envelope）并行运行，
// 通过 client.ReadPump 的消息类型（Text vs Binary）分发。
//
// 协议协商流程：
//   1. 客户端建立 WS 连接
//   2. 客户端发送 system.proto_capability 帧（JSON 或二进制均可）
//   3. 服务端回 system.proto_ack 确认
//   4. 之后双方可使用 BinaryMessage 发送 proto 帧
//   5. 若服务端不支持，客户端降级到 JSON（向后兼容）

package ws

import (
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	commonpb "LsmAgentGame/proto/pb/common"
)

// ProtoHandler 处理单个 proto 消息的函数
// env 是完整的信封，msg 是已反序列化的 payload
type ProtoHandler func(c *Client, env *commonpb.Envelope, msg proto.Message)

// ProtoMessageFactory 创建指定类型的消息实例（用于反序列化）
type ProtoMessageFactory func() proto.Message

// ProtoRegistry proto 消息注册表
//
// 维护 "消息类型字符串" → "消息工厂函数 + 处理函数" 的映射。
// 所有 proto 帧都通过注册表分发。
type ProtoRegistry struct {
	mu       sync.RWMutex
	factory  map[string]ProtoMessageFactory // type → 工厂函数
	handlers map[string][]ProtoHandler      // type → handler 列表（支持多订阅）
}

// NewProtoRegistry 创建注册表
func NewProtoRegistry() *ProtoRegistry {
	return &ProtoRegistry{
		factory:  make(map[string]ProtoMessageFactory),
		handlers: make(map[string][]ProtoHandler),
	}
}

// Register 注册消息类型及其处理函数
// msgType: 消息类型字符串（如 "chat.send"、"game.werewolf_vote"）
// factory: 创建消息实例的工厂函数
// handler: 处理函数（可多次注册，按顺序调用）
func (r *ProtoRegistry) Register(msgType string, factory ProtoMessageFactory, handler ProtoHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factory[msgType]; !exists {
		r.factory[msgType] = factory
	}
	r.handlers[msgType] = append(r.handlers[msgType], handler)
}

// UnmarshalPayload 根据消息类型反序列化 payload
func (r *ProtoRegistry) UnmarshalPayload(msgType string, data []byte) (proto.Message, error) {
	r.mu.RLock()
	factory, ok := r.factory[msgType]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown proto message type: %s", msgType)
	}

	msg := factory()
	if err := proto.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", msgType, err)
	}
	return msg, nil
}

// Dispatch 分发 proto 消息到对应 handler
// 返回 false 表示消息类型未注册
func (r *ProtoRegistry) Dispatch(c *Client, env *commonpb.Envelope) (bool, error) {
	r.mu.RLock()
	factory, okFactory := r.factory[env.Type]
	handlers := r.handlers[env.Type]
	r.mu.RUnlock()

	if !okFactory {
		return false, nil
	}

	// 反序列化 payload
	msg := factory()
	if len(env.Payload) > 0 {
		if err := proto.Unmarshal(env.Payload, msg); err != nil {
			return true, fmt.Errorf("unmarshal %s payload: %w", env.Type, err)
		}
	}

	// 调用所有 handler
	for _, h := range handlers {
		h(c, env, msg)
	}
	return true, nil
}

// HasType 检查是否注册了指定类型
func (r *ProtoRegistry) HasType(msgType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factory[msgType]
	return ok
}

// ListTypes 列出所有已注册的消息类型
func (r *ProtoRegistry) ListTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.factory))
	for t := range r.factory {
		types = append(types, t)
	}
	return types
}

// ─────────────────── 便捷函数 ───────────────────

// MarshalProtoEnvelope 把 payload 消息打包进 Envelope 并序列化为二进制
func MarshalProtoEnvelope(msgType string, seq int64, payload proto.Message) ([]byte, error) {
	env := &commonpb.Envelope{
		Type: msgType,
		Seq:  seq,
	}
	if payload != nil {
		data, err := proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		env.Payload = data
	}
	return proto.Marshal(env)
}

// UnmarshalProtoEnvelope 从二进制数据反序列化 Envelope
func UnmarshalProtoEnvelope(data []byte) (*commonpb.Envelope, error) {
	env := &commonpb.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return env, nil
}

// SendProtoError 发送 proto 格式的错误帧
func (c *Client) SendProtoError(msgType string, seq int64, code int32, message string) {
	env, _ := MarshalProtoEnvelope(msgType, seq, &commonpb.ErrorPayload{
		Code:    code,
		Message: message,
	})
	select {
	case c.protoSend <- env:
	default:
		// 发送队列满，丢弃
	}
}
