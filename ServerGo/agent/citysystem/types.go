// Package citysystem — types.go: 虚拟城市系统主 Agent 公共类型
// (2026-11-03 §批次81)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md §3.2。
package citysystem

import (
	"time"

	llmtypes "LsmAgentGame/llm/types"
)

// Config 是 City-System Agent 的运行参数(从 config.CitySystemConfig 收窄;
// 零值由 NewAgent 归一)。
type Config struct {
	// ModelKey 是 LLM 模型 key(registry.Get);空 = 未配置。
	ModelKey string
	// MaxSteps 单轮 ReAct 步数上限(缺省 8)。
	MaxSteps int
	// MaxMessages 单会话消息上限(缺省 40,超出裁剪)。
	MaxMessages int
	// RequestTimeout 单轮总超时(缺省 180s)。
	RequestTimeout time.Duration
	// ToolResultLimit 单个工具结果回灌上限 rune(缺省 4000)。
	ToolResultLimit int
}

// SystemEventKind 是下发前端的 SystemEvent.Kind 枚举(契约 §3.5)。
const (
	// EventAgentStart 一轮对话开始。
	EventAgentStart = "agent_start"
	// EventText 模型文本增量(前端打字机)。
	EventText = "text"
	// EventThinking 模型思考增量(若上游仍产出 thinking_delta)。
	EventThinking = "thinking"
	// EventToolStart 工具调用开始。
	EventToolStart = "tool_start"
	// EventToolResult 工具调用结果。
	EventToolResult = "tool_result"
	// EventShowCommand 结构化 3D 显示指令(来自 City-WebGL-Show)。
	EventShowCommand = "show_command"
	// EventReport 预测报告卡片(query_prediction_report 命中时下发)。
	EventReport = "report"
	// EventAgentMessage 其它 Agent 的上行来信(批次84 §4.3.2「Agent 来信」):
	// 前端渲染「📨 来自 <Agent>」卡片;同一消息在下一轮 system 提示词
	// 【来自其它 Agent 的消息】段注入一次。
	EventAgentMessage = "agent_message"
	// EventAgentEnd 一轮对话结束。
	EventAgentEnd = "agent_end"
	// EventError 一轮对话异常。
	EventError = "error"
	// EventCleared 会话已清空(前端提示)。
	EventCleared = "cleared"
)

// IncomingMessage 是一条来自其它 Agent 的上行消息(批次84「Agent 来信」)。
//
// 载荷形态与 WS 帧 game.virtual_city_system_event(kind=agent_message) 的
// data 段逐字一致:
//
//	{"from":"LsmAgentGame-City-Chrome","text":"…采集到的行情摘要…","ts":1730000000000}
//
// 存储:ConversationStore 的**独立来信队列**(环形 ≤20,不进 messages 历史);
// 下一轮构造 system 提示词时注入「【来自其它 Agent 的消息】」段(≤5,时间倒序)。
type IncomingMessage struct {
	From string `json:"from"`
	Text string `json:"text"`
	Ts   int64  `json:"ts"`
}

// SystemEvent 是 Agent → 前端的事件流载荷(WS 帧
// game.virtual_city_system_event 的 data 段)。
type SystemEvent struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// ToolCallRecord 是一次工具调用的留痕(前端工具卡 / 历史回放)。
type ToolCallRecord struct {
	Tool  string `json:"tool"`
	Input string `json:"input"`
	Text  string `json:"text"`
	IsErr bool   `json:"is_err"`
	Ts    int64  `json:"ts"`
}

// LLMRegistry 是 llm.Registry 的窄接口(与 citychrome/vcplayer 同款;
// 测试可注入 fake)。
type LLMRegistry interface {
	Get(modelKey string) (llmtypes.LLMProvider, string, error)
	GetThinkingEnabled(modelKey string) (bool, int)
	// ListEnabled 返回可用模型目录(随机线路策略的候选集,
	// 权重 = concurrency_lines)。
	ListEnabled() []llmtypes.ModelInfo
}
