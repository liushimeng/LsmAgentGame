// Package config — city_system_config.go: 批次81 City-System 主 Agent 配置段
// (2026-11-03 §批次81 City-System 主 Agent 与系统对话)。
//
// 契约: lag_docs/虚拟城市/已实现/81-CitySystem-Agent与系统对话/01-方案设计.md §3.7。
//
// 独立成文件而非并入 config.go —— config.go 已接近 §4 单文件 ≤1800 行硬上限
// (1736 行),本段约 60 行;拆出后 config.go 维持在限内,结构也与既有
// 「配置段就近声明」惯例一致(字段仍在 Config 结构体、默认值仍在 applyDefaults)。
package config

import "time"

// CitySystemConfig 是 city_system{} 顶级段(批次81;契约 §3.7):
// LsmAgentGame-City-System 主 Agent 的对话式控制面(虚拟城市定位 v3.0 的
// 唯一人类控制入口)。enabled=false ⇒ WS 帧返回 35058,前端隐藏入口。
type CitySystemConfig struct {
	// Enabled 总开关(默认 false,显式开启;与 city_chrome.enabled 同语义)。
	Enabled bool `json:"enabled"`
	// ModelKey 系统 Agent 的 LLM 模型 key(registry.Get;缺省 DeepSeek-model)。
	// 房间级 city_system_line="random" 时每轮从线路池随机取线覆盖本值。
	ModelKey string `json:"model_key"`
	// MaxSteps 单轮 ReAct 步数上限(缺省 8;防 LLM 死循环)。
	MaxSteps int `json:"max_steps"`
	// MaxMessages 单会话消息上限(缺省 40;超出裁剪)。
	MaxMessages int `json:"max_messages"`
	// RequestTimeoutMs 单轮总超时(缺省 180000;含流式续命 §197)。
	RequestTimeoutMs int `json:"request_timeout_ms"`
	// ToolResultLimit 单个工具结果回灌上限 rune(缺省 4000)。
	ToolResultLimit int `json:"tool_result_limit"`
}

// ResolvedCitySystemConfig 是归一化后的 city_system 配置(缺省值已填)。
type ResolvedCitySystemConfig struct {
	ModelKey        string
	MaxSteps        int
	MaxMessages     int
	RequestTimeout  time.Duration
	ToolResultLimit int
}

// Resolved 返回归一化配置(缺省值见 applyDefaults;零值安全)。
func (c CitySystemConfig) Resolved() ResolvedCitySystemConfig {
	r := ResolvedCitySystemConfig{
		ModelKey:        c.ModelKey,
		MaxSteps:        c.MaxSteps,
		MaxMessages:     c.MaxMessages,
		RequestTimeout:  time.Duration(c.RequestTimeoutMs) * time.Millisecond,
		ToolResultLimit: c.ToolResultLimit,
	}
	if r.ModelKey == "" {
		r.ModelKey = "DeepSeek-model"
	}
	if r.MaxSteps <= 0 {
		r.MaxSteps = 8
	}
	if r.MaxMessages <= 0 {
		r.MaxMessages = 40
	}
	if r.RequestTimeout <= 0 {
		r.RequestTimeout = 180 * time.Second
	}
	if r.ToolResultLimit <= 0 {
		r.ToolResultLimit = 4000
	}
	return r
}
