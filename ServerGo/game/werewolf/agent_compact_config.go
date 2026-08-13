// Package werewolf — agent_compact_config.go: 局内 LLM 语义压缩的配置读取与注入。
//
// 2026-08-13 §20260813-02 U1 新增。
//
// 配置项(config.WerewolfConfig,applyDefaults 已置默认值):
//   - werewolf.agent_compact_enabled    (默认 true)  — 消息数超阈值时用 bot
//     自己的 provider 把最早 1/3 消息压缩为结构化摘要;false 退回纯规则式压缩。
//   - werewolf.agent_compact_max_tokens (默认 1200)  — 压缩调用 max_tokens 预算。
//
// 测试环境 config.Load() 可能 panic(找不到 LsmAgentGame.conf.example,
// §197 教训 3),所有读取函数 defer recover 兜底:
//   - enabled  panic → false(与 cfgAgentMemoryEnabled 同模式,
//     避免无配置环境下误触发 LLM 调用);
//   - maxTokens panic → 1200(代码内常量兜底)。
package werewolf

import (
	"LsmAgentGame/agent/wwplayer"
	"LsmAgentGame/config"
)

// defaultAgentCompactMaxTokens 是压缩 LLM 调用的 max_tokens 兜底值。
// 压缩输出是 4 段结构化摘要,1200 token 足够;过大徒增计费。
const defaultAgentCompactMaxTokens = 1200

// cfgAgentCompactEnabled 安全读取 config.WerewolfConfig.AgentCompactEnabled。
// 默认 true(applyDefaults 已置);测试环境 config.Load() panic 时按"关闭"
// 兜底,避免无配置环境下误触发 LLM 调用(与 cfgAgentMemoryEnabled 同模式)。
func cfgAgentCompactEnabled() (enabled bool) {
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return false
	}
	return c.Werewolf.AgentCompactEnabled
}

// cfgAgentCompactMaxTokens 安全读取 config.WerewolfConfig.AgentCompactMaxTokens。
// 默认 1200;测试环境 config.Load() panic 时按默认值兜底。
func cfgAgentCompactMaxTokens() (n int) {
	n = defaultAgentCompactMaxTokens
	defer func() { _ = recover() }()
	c := config.Load()
	if c == nil {
		return defaultAgentCompactMaxTokens
	}
	if c.Werewolf.AgentCompactMaxTokens > 0 {
		n = c.Werewolf.AgentCompactMaxTokens
	}
	return n
}

// cfgAgentCompactConfig 汇总为 wwplayer.CompactConfig,供 StartAgentsLocked
// 为每个 bot 注入(ag.SetCompactConfig)。Provider/apiKey 不走本配置 —
// run_compact.go 在触发点直接使用 Agent 自身构造期绑定的 a.Provider/a.apiKey
// (与 NewWithRoom 的 registry.Get 路径一致)。
func cfgAgentCompactConfig() wwplayer.CompactConfig {
	cfg := wwplayer.DefaultCompactConfig()
	cfg.Enabled = cfgAgentCompactEnabled()
	cfg.MaxTokens = cfgAgentCompactMaxTokens()
	// 超时固定 60s(§3 设计:慢模型降级规则压缩,不阻塞主决策循环)。
	cfg.TimeoutSec = 60
	return cfg
}
