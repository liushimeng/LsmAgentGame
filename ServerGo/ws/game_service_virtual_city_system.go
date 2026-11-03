// Package ws — game_service_virtual_city_system.go: 批次81 City-System 主
// Agent 的 WS 帧处理(2026-11-03 §批次81 City-System 主 Agent 与系统对话)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md
// §3.5(帧契约)/ §6.1(数据契约)。
//
// 帧:
//
//	C→S game.virtual_city_system_message  {room_id, text}
//	C→S game.virtual_city_system_reset    {room_id}
//	C→S game.virtual_city_system_history  {room_id}
//	S→C game.virtual_city_system_accepted {room_id}
//	S→C game.virtual_city_system_cleared  {room_id}
//	S→C game.virtual_city_system_event    {room_id, kind, data}
//	S→C game.virtual_city_system_history  {room_id, messages[]}
//	S→C game.virtual_city_show_command    {room_id, command}
//
// 纪律:
//   - **不阻塞 WS 读循环**:agent.Run 在独立 goroutine 执行, handler 立即回
//     accepted 帧(契约 §3.5);
//   - **并发限流 1**:每用户同时只允许一轮(busy 标记,35060);
//   - **权限**:任意登录用户(与场景推演同口径,§19.5 观察者可用),不消耗座位;
//     control_simulation 的暂停权限沿用 room.Pause 的房主校验(35011);
//   - **生命周期**:每房间一个 systemRoomAgent(agent + 会话存储),房间销毁时
//     经 removeCitySystemAgent 清理。
package ws

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
	"LsmAgentGame/agent/citysystem"
	"LsmAgentGame/agent/citywebgl"
	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"
	llmtypes "LsmAgentGame/llm/types"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// systemRoomAgent 是每房间一个的 City-System 运行时(agent + 会话存储 +
// 每用户并发限流标记)。
type systemRoomAgent struct {
	agent *citysystem.Agent
	store *citysystem.ConversationStore
	// busy 记录「该用户本轮是否仍在执行」(userID → struct{})。
	// LoadOrStore 原子语义 = 并发限流 1;Run 结束后 Delete。
	busy sync.Map
}

// SetCitySystemRuntime 注入 City-System 运行时配置(main.go 装配)。
// reg 为 nil 时 Agent 的 registry 接口保持 nil(Run 时报错,不 panic)。
func (s *GameService) SetCitySystemRuntime(enabled bool, cfg config.ResolvedCitySystemConfig, reg citysystem.LLMRegistry) {
	s.vcSystemEnabled = enabled
	s.vcSystemCfg = cfg
	s.vcSystemRegistry = reg
}

// getOrCreateSystemRoomAgent 取/建房间级 SystemAgent(sync.Map 幂等)。
// 线路策略在构造时从房间字段读取(与批次79 CityChromeLine 同构)。
func (s *GameService) getOrCreateSystemRoomAgent(roomID string, r *virtual_city.VirtualCityRoom) *systemRoomAgent {
	if v, ok := s.vcSystemAgents.Load(roomID); ok {
		return v.(*systemRoomAgent)
	}
	store := citysystem.NewConversationStore(s.vcSystemCfg.MaxMessages)
	emit := func(ev citysystem.SystemEvent) { s.emitCitySystemEvent(roomID, ev) }
	ag := citysystem.NewAgent(r, citysystem.Config{
		ModelKey:        s.vcSystemCfg.ModelKey,
		MaxSteps:        s.vcSystemCfg.MaxSteps,
		MaxMessages:     s.vcSystemCfg.MaxMessages,
		RequestTimeout:  s.vcSystemCfg.RequestTimeout,
		ToolResultLimit: s.vcSystemCfg.ToolResultLimit,
	}, s.vcSystemRegistry, citybus.Default(), store, emit)
	ag.SetLinePolicy(r.CitySystemLine())
	// City-WebGL-Show 房间级 bus handler(批次81):msg.Payload = display.go 的
	// displayRequest(text/district/mode/x/z);返回 []DisplayCommand(JSON 中性,
	// citysystem 侧 parseDisplayCommands 可解析)。
	// **单一发射点**:show_command 帧一律由 citysystem.Agent.emitCommands 下发
	// (focus_camera 工具派发后统一 emit),本处 emit 置 nil —— 否则 citywebgl.Run
	// 路径逐条 emit + citysystem 侧 emitCommands 会**双重发帧**(同 trace_id 重复)。
	// citywebgl.NewAgent 对 nil emit 天然安全(指令只收集不实时下发)。
	showAg := citywebgl.NewAgent(citywebgl.Config{ModelKey: s.vcSystemCfg.ModelKey}, s.vcSystemRegistry, nil)
	showAg.SetLinePolicy(r.CitySystemLine())
	// 先 Unregister 再 Register:同房重建/测试复用 roomID 时强制重绑到当前
	// 房间的 emit(RegisterRoom 对重复注册报错,不覆盖旧 handler)。
	citybus.Default().UnregisterRoom(roomID, string(agentroot.AgentClassCityWebGLShow))
	if err := citybus.Default().RegisterRoom(roomID, string(agentroot.AgentClassCityWebGLShow),
		func(ctx context.Context, msg citybus.Message) (any, error) {
			// 路径选择:参数齐备(district 或 x/z)⇒ Structured 零延迟不过 LLM;
			// 否则 Run(自然语言 ReAct)。
			req, err := citywebgl.ParseDisplayRequest(msg.Payload)
			if err != nil {
				return nil, err
			}
			if req.District != "" || req.X != nil || req.Z != nil {
				return showAg.Structured(req), nil
			}
			return showAg.Run(ctx, req)
		}); err != nil {
		logger.L().Warn("citywebgl register room handler failed",
			zap.String("room_id", roomID), zap.Error(err))
	}
	sa := &systemRoomAgent{agent: ag, store: store}
	actual, _ := s.vcSystemAgents.LoadOrStore(roomID, sa)
	return actual.(*systemRoomAgent)
}

// removeCitySystemAgent 清理房间级 SystemAgent(房间销毁路径调用)。
// 同步注销 City-WebGL-Show 的房间级 bus handler(资源卫生)。
func (s *GameService) removeCitySystemAgent(roomID string) {
	s.vcSystemAgents.Delete(roomID)
	citybus.Default().UnregisterRoom(roomID, string(agentroot.AgentClassCityWebGLShow))
}

// handleVirtualCitySystemMessage 处理 game.virtual_city_system_message。
func (s *GameService) handleVirtualCitySystemMessage(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" || strings.TrimSpace(req.Text) == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.virtual_city_system_message payload")
		return
	}
	if !s.vcSystemEnabled {
		s.sendError(c, env.Seq, errcode.ErrVirtualCitySystemAgentDisabled, "")
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendError(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "")
		return
	}
	sa := s.getOrCreateSystemRoomAgent(req.RoomID, r)
	if _, loaded := sa.busy.LoadOrStore(c.UserID, struct{}{}); loaded {
		s.sendError(c, env.Seq, errcode.ErrVirtualCitySystemAgentBusy, "")
		return
	}
	s.sendOK(c, env.Seq, "game.virtual_city_system_accepted", map[string]any{"room_id": req.RoomID})
	go func() {
		defer sa.busy.Delete(c.UserID)
		timeout := s.vcSystemCfg.RequestTimeout
		if timeout <= 0 {
			timeout = 180 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := sa.agent.Run(ctx, c.UserID, req.Text); err != nil {
			logger.L().Warn("citysystem run failed",
				zap.String("room_id", req.RoomID), zap.String("user_id", c.UserID), zap.Error(err))
		}
	}()
}

// handleVirtualCitySystemReset 处理 game.virtual_city_system_reset(清空会话)。
func (s *GameService) handleVirtualCitySystemReset(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.virtual_city_system_reset payload")
		return
	}
	if !s.vcSystemEnabled {
		s.sendError(c, env.Seq, errcode.ErrVirtualCitySystemAgentDisabled, "")
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendError(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "")
		return
	}
	sa := s.getOrCreateSystemRoomAgent(req.RoomID, r)
	sa.agent.Reset(c.UserID)
	s.sendOK(c, env.Seq, "game.virtual_city_system_cleared", map[string]any{"room_id": req.RoomID})
	s.hub.BroadcastRoomIncludingSpectators(req.RoomID, wsEnvelope("game.virtual_city_system_event", 0, map[string]any{
		"room_id": req.RoomID, "kind": citysystem.EventCleared,
		"data": map[string]any{"user_id": c.UserID},
	}))
}

// handleVirtualCitySystemHistory 处理 game.virtual_city_system_history
// (该用户在此房的会话;无会话 → 空数组)。
func (s *GameService) handleVirtualCitySystemHistory(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" {
		s.sendError(c, env.Seq, errcode.ErrValidationFailed, "invalid game.virtual_city_system_history payload")
		return
	}
	if s.vcMgr.Get(req.RoomID) == nil {
		s.sendError(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "")
		return
	}
	msgs := s.SystemAgentHistory(req.RoomID, c.UserID)
	s.sendOK(c, env.Seq, "game.virtual_city_system_history", map[string]any{
		"room_id": req.RoomID, "messages": msgs,
	})
}

// emitCitySystemEvent 把 Agent 事件广播为 game.virtual_city_system_event;
// show_command 额外发一条 game.virtual_city_show_command(契约 §3.5/§6.1)。
//
// 调用方 = Agent 的 Run goroutine;hub 广播自身并发安全(内部 RLock)。
func (s *GameService) emitCitySystemEvent(roomID string, ev citysystem.SystemEvent) {
	s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.virtual_city_system_event", 0, map[string]any{
		"room_id": roomID, "kind": ev.Kind, "data": ev.Data,
	}))
	if ev.Kind == citysystem.EventShowCommand {
		s.hub.BroadcastRoomIncludingSpectators(roomID, wsEnvelope("game.virtual_city_show_command", 0, map[string]any{
			"room_id": roomID, "command": ev.Data,
		}))
	}
}

// SystemAgentHistory 返回该用户在此房的会话消息(副本;无会话 → 空数组)。
// 实现 api.SystemAgentSource 窄接口(供 HTTP 历史端点复用)。
func (s *GameService) SystemAgentHistory(roomID, userID string) []llmtypes.Message {
	if v, ok := s.vcSystemAgents.Load(roomID); ok {
		msgs := v.(*systemRoomAgent).store.Get(roomID + "/" + userID)
		if len(msgs) == 0 {
			return []llmtypes.Message{}
		}
		return msgs
	}
	return []llmtypes.Message{}
}

// ResetSystemAgent 清空该用户在此房的会话(实现 api.SystemAgentSource)。
func (s *GameService) ResetSystemAgent(roomID, userID string) {
	if v, ok := s.vcSystemAgents.Load(roomID); ok {
		v.(*systemRoomAgent).agent.Reset(userID)
	}
}
