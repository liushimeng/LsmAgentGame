// Package citysystem — bus_handler.go: City-System 的「Agent 来信」上行通道
// (2026-11-05 §批次84)。
//
// 契约: lag_docs/虚拟城市/已实现/84-全维度事件体系与Agent双向对话/
//       01-方案设计.md §4.3.2(Agent 来信上行通道)。
//
// 本文件把 City-System **注册为房间级总线 handler**,接收其它 Agent 的
// 上行消息(`Type == "message"` 且 `From != City-System`),三步:
//
//  1. 立即 emit SystemEvent{Kind: EventAgentMessage, Data:{from,text,ts}}
//     —— 经 WS 推给前端渲染「📨 来自 <Agent>」卡片;
//  2. 写入 ConversationStore 的**独立来信队列**(环形 ≤20,不进 messages);
//  3. **下一轮** Run 构造 system 时由 BuildSystemBlocks 注入
//     「【来自其它 Agent 的消息】」段(≤5 条,时间倒序;取走出队)。
//
// 装配层接线:房间级 Agent 建好后调
//
//	ag.RegisterBusHandler(citybus.Default())
//
// (见 CitySystem-Agent设计.md「装配层接入说明」)。房间销毁时调
// hub.UnregisterRoom(roomID, AgentClassCitySystem) 注销(与批次81 的
// City-WebGL-Show 注销同一处)。
//
// 纪律:
//   - handler 不做 LLM 调用、不阻塞:收信 → emit → 入队 → 回一句确认;
//   - **绝不编造**:回执只说明「已收到并入队」,不假装已回答对方的问题;
//   - Type != "message" 的消息不入队(只登记排查),避免污染来信通道。
package citysystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentroot "LsmAgentGame/agent"
	"LsmAgentGame/agent/citybus"
)

// BusTypeMessage 是批次84 的双向对话消息类型(与 citychrome / vcplayer /
// citywebgl 的同名常量**逐字一致**;载荷 map[string]any{"text": "..."},
// 亦兼容批次81 的裸字符串载荷)。
const BusTypeMessage = "message"

// RegisterBusHandler 把 City-System 注册为**房间级**总线 handler
// (批次84 §4.3.2),接收其它 Agent 的上行消息("message" 类型)。
//
// 装配层调用时机:房间级 City-System Agent 建好之后(WS 层
// getOrCreateSystemRoomAgent);同一房间重复注册会失败(总线同房同类
// 不允许重复注册),装配层应先 UnregisterRoom 再 Register(与批次81 的
// City-WebGL-Show 同口径)。
//
// 上行载荷形态(与 citychrome/vcplayer 的 NotifySystem 一致):
//
//	map[string]any{"text": "<消息正文>"}   // 批次84 契约形态
//	"<消息正文>"                          // 兼容批次81 裸字符串
//
// 返回:handler 的回执文本(对方 ask_agent 的回包)。**不代为回答**对方
// 的问题,只确认「已收到并入队」—— 真正的回答走下一轮系统对话。
func (a *Agent) RegisterBusHandler(hub *citybus.Hub) error {
	if hub == nil {
		return fmt.Errorf("citysystem: hub is nil")
	}
	if a == nil || a.RoomID == "" {
		return fmt.Errorf("citysystem: room id is empty")
	}
	return hub.RegisterRoom(a.RoomID, string(agentroot.AgentClassCitySystem),
		func(_ context.Context, msg citybus.Message) (any, error) {
			return a.handleBusMessage(msg), nil
		})
}

// handleBusMessage 处理一条上行总线消息(不返回 error;失败只影响回执文案)。
//
//   - Type != "message":不入队,回执说明不支持的消息类型;
//   - From == City-System:自我发信(不应出现),忽略以防死循环;
//   - 正常:emit 事件 + 入队 + 回执。
func (a *Agent) handleBusMessage(msg citybus.Message) any {
	if msg.Type != BusTypeMessage {
		return fmt.Sprintf("City-System 仅支持 %s 类型的上行消息,已忽略 %q",
			BusTypeMessage, msg.Type)
	}
	from := strings.TrimSpace(msg.From)
	if from == "" || from == string(agentroot.AgentClassCitySystem) {
		return "已忽略(来源为空或为 City-System 自身,防回环)。"
	}
	text := strings.TrimSpace(busMessageText(msg.Payload))
	if text == "" {
		return "来信正文为空,已忽略。"
	}
	now := time.Now().UnixMilli()
	in := IncomingMessage{From: from, Text: text, Ts: now}

	// 1) 立即下发前端(「Agent 来信」卡片)。
	a.emit(SystemEvent{Kind: EventAgentMessage, Data: map[string]any{
		"from": in.From, "text": in.Text, "ts": in.Ts,
	}})
	// 2) 入来信队列(环形 ≤20;下一轮 system 注入一次)。
	a.store.AppendIncoming(incomingKey(a.RoomID), in)
	// 3) 回执(**不代答**):只确认已收到。
	return fmt.Sprintf("City-System 已收到你的消息并记入来信队列,将在下一轮系统对话上下文中参考;" +
		"如需即时答复,请等人类发起系统对话后追问。")
}

// busMessageText 解析上行消息载荷:map{"text":...} / 裸字符串 均可;
// 其它形态返回空串(调用方按「正文为空」忽略)。
func busMessageText(payload any) string {
	switch p := payload.(type) {
	case string:
		return strings.TrimSpace(p)
	case []byte:
		return strings.TrimSpace(string(p))
	case map[string]any:
		if raw, ok := p["text"]; ok {
			if s, ok := raw.(string); ok {
				return strings.TrimSpace(s)
			}
		}
		return ""
	default:
		// 兜底:JSON 往返取 text 字段(跨包 map 形态差异不 panic)。
		raw, err := json.Marshal(payload)
		if err != nil {
			return ""
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			if s, ok := m["text"].(string); ok {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
}
