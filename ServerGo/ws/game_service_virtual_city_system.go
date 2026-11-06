// Package ws — game_service_virtual_city_system.go: 批次81 City-System 主
// Agent 的 WS 帧处理(2026-11-03 §批次81 City-System 主 Agent 与系统对话)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md
// §3.5(帧契约)/ §6.1(数据契约);批次84 装配层接线见
// lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/01-方案设计.md
// §4.3(Agent 双向通讯)/ §9.1(装配层接线清单):
//   - City-WebGL-Show 房间级 handler 按 msg.Type 路由(run_display / message),
//     修 §2.5b「一律按显示请求解析」的类型错误;
//   - City-System 注册房间级上行 handler(RegisterBusHandler,接收来信)。
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
	"fmt"
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
	// 批次87 P1-6:emit 回调带 userID —— 会话类事件定向投递(仅发起用户),
	// userID=="" 的房间级事件(Agent 来信)仍全房广播。
	emit := func(userID string, ev citysystem.SystemEvent) {
		s.emitCitySystemEvent(roomID, userID, ev)
	}
	ag := citysystem.NewAgent(r, citysystem.Config{
		ModelKey:        s.vcSystemCfg.ModelKey,
		MaxSteps:        s.vcSystemCfg.MaxSteps,
		MaxMessages:     s.vcSystemCfg.MaxMessages,
		RequestTimeout:  s.vcSystemCfg.RequestTimeout,
		ToolResultLimit: s.vcSystemCfg.ToolResultLimit,
	}, s.vcSystemRegistry, citybus.Default(), store, emit)
	ag.SetLinePolicy(r.CitySystemLine())
	// 批次84 §4.3.2:City-System 房间级上行 handler(接收其它 Agent 的
	// "message" 来信 → EventAgentMessage 事件 + 来信队列,下一轮注入上下文)。
	// 先 Unregister 再注册:同房重建/测试复用 roomID 时强制重绑到当前 Agent
	// (RegisterRoom 对重复注册报错,不覆盖旧 handler;与下方 City-WebGL-Show
	// 同口径)。失败只 Warn 不阻塞(来信通道降级,主对话不受影响)。
	citybus.Default().UnregisterRoom(roomID, string(agentroot.AgentClassCitySystem))
	if err := ag.RegisterBusHandler(citybus.Default()); err != nil {
		logger.L().Warn("citysystem register bus handler failed",
			zap.String("room_id", roomID), zap.Error(err))
	}
	// City-WebGL-Show 房间级 bus handler(批次81 + 批次84 §9.1):按 msg.Type
	// 路由 —— "run_display"/"" 走显示请求;"message" 走对话式翻译;其余明确报错
	// (修 §2.5b「一律按显示请求解析」:收 "message" 会 JSON 反序列化类型错误)。
	// msg.Payload = display.go 的 displayRequest(text/district/mode/x/z/phase/…);
	// 返回 []DisplayCommand(JSON 中性,citysystem 侧 parseDisplayCommands 可解析)。
	// **单一发射点**:show_command 帧一律由 citysystem.Agent.emitCommands 下发
	// (focus_camera/apply_display 工具派发后统一 emit),本处 emit 置 nil —— 否则
	// citywebgl.Run 路径逐条 emit + citysystem 侧 emitCommands 会**双重发帧**
	// (同 trace_id 重复)。citywebgl.NewAgent 对 nil emit 天然安全(指令只收集
	// 不实时下发)。
	showAg := citywebgl.NewAgent(citywebgl.Config{ModelKey: s.vcSystemCfg.ModelKey}, s.vcSystemRegistry, nil)
	showAg.SetLinePolicy(r.CitySystemLine())
	// 先 Unregister 再 Register:同房重建/测试复用 roomID 时强制重绑到当前
	// 房间的 emit(RegisterRoom 对重复注册报错,不覆盖旧 handler)。
	citybus.Default().UnregisterRoom(roomID, string(agentroot.AgentClassCityWebGLShow))
	// 批次85 §3.3.3:RegisterRoom → RegisterRoomWith 补能力描述(list_agents
	// 展示,修批次84 §8-10「未登记描述」)。MessageTypes 实测自下方 handler 的
	// switch msg.Type 分支(""/"run_display"/"message";"" 是 run_display 的
	// 缺省别名)。
	if err := citybus.Default().RegisterRoomWith(roomID, string(agentroot.AgentClassCityWebGLShow),
		[]string{"", "run_display", "message"},
		"渲染显示 Agent:把结构化/自然语言显示请求翻译为 DisplayCommand 显示原语(run_display 缺省零延迟直通,message 走对话式翻译并逐条下发前端,回中文摘要)。",
		func(ctx context.Context, msg citybus.Message) (any, error) {
			switch msg.Type {
			case "", "run_display":
				// 显示路径。判据必须用 HasStructuredParams()(批次84):
				// 旧内联判据只认 district/x/z,会把仅带 phase/weather/
				// entity_id 等新参数的请求误落 LLM 路径(失去零延迟)。
				req, err := citywebgl.ParseDisplayRequest(msg.Payload)
				if err != nil {
					return nil, err
				}
				if req.HasStructuredParams() {
					return showAg.Structured(req), nil
				}
				return showAg.Run(ctx, req)
			case "message":
				// 对话分支(批次84 §4.3.3):把自然语言请求翻译成显示指令,
				// **逐条下发前端**(需求 2「自动渲染显示」+ 需求 5「实时显示
				// 正在干什么」),再回 System 一句中文摘要。
				// ⚠ 只在本分支经 emitCitySystemEvent 下发(其内部对
				// EventShowCommand 会额外发 game.virtual_city_show_command,
				// 复用之,不自拼帧);run_display/"" 分支保持**单发射点**不变
				// (showAg emit 恒 nil,靠 citysystem.Agent.emitCommands 发),
				// 否则双发帧。
				text, err := webglBusMessageText(msg.Payload)
				if err != nil {
					return nil, err
				}
				cmds, err := showAg.Run(ctx, citywebgl.DisplayRequest{Text: text})
				if err != nil {
					return nil, fmt.Errorf("City-WebGL-Show 显示翻译失败: %w", err)
				}
				for _, cmd := range cmds {
					// userID="" ⇒ 房间级投递(批次87 P1-6:该 show_command 由
					// City-WebGL-Show 房间级 bus handler 派发,沿用批次84
					//「自动渲染显示」的全房语义,不改)。
					s.emitCitySystemEvent(roomID, "", citysystem.SystemEvent{
						Kind: citysystem.EventShowCommand, Data: cmd,
					})
				}
				types := make([]string, 0, len(cmds))
				for _, c := range cmds {
					types = append(types, c.Type)
				}
				if len(types) == 0 {
					return "已翻译为 0 条显示指令(未能从描述中提取显示参数;如需渲染请用 focus_camera / apply_display)。", nil
				}
				return fmt.Sprintf("已翻译为 %d 条显示指令:%s", len(types), strings.Join(types, "、")), nil
			default:
				return nil, fmt.Errorf("City-WebGL-Show 不支持的消息类型 %q(本 Agent 支持 run_display / message)", msg.Type)
			}
		}); err != nil {
		logger.L().Warn("citywebgl register room handler failed",
			zap.String("room_id", roomID), zap.Error(err))
	}
	sa := &systemRoomAgent{agent: ag, store: store}
	actual, _ := s.vcSystemAgents.LoadOrStore(roomID, sa)
	return actual.(*systemRoomAgent)
}

// removeCitySystemAgent 清理房间级 SystemAgent(房间销毁路径调用)。
// 同步注销 City-System 上行 handler 与 City-WebGL-Show 的房间级 bus handler
// (资源卫生;批次84 §4.3.2 注册的来信通道一并回收,防泄漏)。
func (s *GameService) removeCitySystemAgent(roomID string) {
	s.vcSystemAgents.Delete(roomID)
	citybus.Default().UnregisterRoom(roomID, string(agentroot.AgentClassCitySystem))
	citybus.Default().UnregisterRoom(roomID, string(agentroot.AgentClassCityWebGLShow))
}

// webglBusMessageText 解析 City-WebGL-Show "message" 分支的载荷:
// map[string]any{"text": "..."}(批次84 契约)/ 裸字符串(与其它 Agent 一致,
// 兼容批次81 send_agent_message 的裸字符串载荷)。
// 非法载荷(缺 text / text 非字符串 / text 空)返回明确中文失败说明,不 panic。
func webglBusMessageText(payload any) (string, error) {
	switch p := payload.(type) {
	case string:
		text := strings.TrimSpace(p)
		if text == "" {
			return "", fmt.Errorf("消息内容为空:请在 text 字段中给出显示需求描述")
		}
		return text, nil
	case []byte:
		return webglBusMessageText(string(p))
	case map[string]any:
		raw, ok := p["text"]
		if !ok {
			return "", fmt.Errorf("消息载荷缺少 text 字段:期望 {\"text\": \"...\"}")
		}
		text, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("消息载荷的 text 字段类型错误:期望字符串,实际类型 %T", raw)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return "", fmt.Errorf("消息内容为空:请在 text 字段中给出显示需求描述")
		}
		return text, nil
	default:
		return "", fmt.Errorf("消息载荷格式错误:期望 {\"text\": \"...\"},实际类型 %T", payload)
	}
}

// systemRoomIDFromPayload 尽力从**非法载荷**里解析出 room_id(批次87 P1-2)。
//
// 为什么需要:载荷校验失败(如 room_id 缺失 / text 为空)时,主解析已经失败,
// 但只要 room_id 本身存在,错误帧仍应带上它 —— 前端才能把错误归属到房间。
// 解析不出时返回空串(前端按「无房间归属」处理,与旧行为一致)。
func systemRoomIDFromPayload(payload json.RawMessage) string {
	var probe struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.RoomID)
}

// handleVirtualCitySystemMessage 处理 game.virtual_city_system_message。
func (s *GameService) handleVirtualCitySystemMessage(c *Client, env Envelope) {
	var req struct {
		RoomID string `json:"room_id"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(env.Payload, &req); err != nil || req.RoomID == "" || strings.TrimSpace(req.Text) == "" {
		s.sendErrorRoom(c, env.Seq, errcode.ErrValidationFailed,
			"invalid game.virtual_city_system_message payload", systemRoomIDFromPayload(env.Payload))
		return
	}
	if !s.vcSystemEnabled {
		s.sendErrorRoom(c, env.Seq, errcode.ErrVirtualCitySystemAgentDisabled, "", req.RoomID)
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendErrorRoom(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "", req.RoomID)
		return
	}
	sa := s.getOrCreateSystemRoomAgent(req.RoomID, r)
	if _, loaded := sa.busy.LoadOrStore(c.UserID, struct{}{}); loaded {
		s.sendErrorRoom(c, env.Seq, errcode.ErrVirtualCitySystemAgentBusy, "", req.RoomID)
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
		s.sendErrorRoom(c, env.Seq, errcode.ErrValidationFailed,
			"invalid game.virtual_city_system_reset payload", systemRoomIDFromPayload(env.Payload))
		return
	}
	if !s.vcSystemEnabled {
		s.sendErrorRoom(c, env.Seq, errcode.ErrVirtualCitySystemAgentDisabled, "", req.RoomID)
		return
	}
	r := s.vcMgr.Get(req.RoomID)
	if r == nil {
		s.sendErrorRoom(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "", req.RoomID)
		return
	}
	sa := s.getOrCreateSystemRoomAgent(req.RoomID, r)
	sa.agent.Reset(c.UserID)
	s.sendOK(c, env.Seq, "game.virtual_city_system_cleared", map[string]any{"room_id": req.RoomID})
	// 批次87 P1-6:`cleared` 是**发起用户自己**的会话状态(会话存储按
	// roomID/userID 隔离)⇒ 定向投递。若全房广播,别人的「清空对话」会
	// 把同房其它用户的会话面板一起清空(前端 store 的 cleared 分支直接
	// 清空全部消息)。
	s.hub.BroadcastRoomToUser(req.RoomID, c.UserID, wsEnvelope("game.virtual_city_system_event", 0, map[string]any{
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
		s.sendErrorRoom(c, env.Seq, errcode.ErrValidationFailed,
			"invalid game.virtual_city_system_history payload", systemRoomIDFromPayload(env.Payload))
		return
	}
	if s.vcMgr.Get(req.RoomID) == nil {
		s.sendErrorRoom(c, env.Seq, errcode.ErrVirtualCityRoomNotFound, "", req.RoomID)
		return
	}
	msgs := s.SystemAgentHistory(req.RoomID, c.UserID)
	s.sendOK(c, env.Seq, "game.virtual_city_system_history", map[string]any{
		"room_id": req.RoomID, "messages": msgs,
	})
}

// emitCitySystemEvent 把 Agent 事件下发为 game.virtual_city_system_event;
// show_command 额外发一条 game.virtual_city_show_command(契约 §3.5/§6.1)。
//
// 投递范围(批次87 P1-6):
//   - userID != "" ⇒ **定向**给该用户(会话私有事件:agent_start / text /
//     thinking / tool_start / tool_result / report / error / agent_end,
//     以及由 Agent 工具派发的 show_command)。理由:会话存储已按
//     roomID/userID 隔离,全房广播会让同房 B 的面板并入 A 的流式回复,
//     且对话内容经线缆外泄给全房。
//   - userID == "" ⇒ **全房**(房间级事件:其它 Agent 来信 agent_message;
//     以及 City-WebGL-Show 房间级 bus handler 派发的 show_command ——
//     批次84「自动渲染显示」既有语义)。
//
// ⚠ 两条孪生帧(game.virtual_city_system_event 与 game.virtual_city_show_command)
// 必须**同范围**投递,否则前端只收到其中一条 ⇒ 显示指令丢失。
//
// 调用方 = Agent 的 Run goroutine;hub 广播自身并发安全(内部 RLock)。
func (s *GameService) emitCitySystemEvent(roomID, userID string, ev citysystem.SystemEvent) {
	deliver := func(env Envelope) {
		if userID != "" {
			s.hub.BroadcastRoomToUser(roomID, userID, env)
			return
		}
		s.hub.BroadcastRoomIncludingSpectators(roomID, env)
	}
	deliver(wsEnvelope("game.virtual_city_system_event", 0, map[string]any{
		"room_id": roomID, "kind": ev.Kind, "data": ev.Data,
	}))
	if ev.Kind == citysystem.EventShowCommand {
		deliver(wsEnvelope("game.virtual_city_show_command", 0, map[string]any{
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
