// Package agent — agent_retry_config.go: 2026-07-12 §127 增强。
//
// 把 cfg.Werewolf.LLMMaxRetries 注入 Agent.maxLLMRetries。
// 测试环境无 conf 文件时 config.Load() 会 panic;用 defer recover 兜底,保持默认 7。
package wwplayer

import (
	"LsmAgentGame/config"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// loadAgentRetryConfigInto 把 cfg.Werewolf.LLMMaxRetries 注入 a.maxLLMRetries 字段。
// 用 defer recover 兜底测试环境 conf 缺失/字段缺失——保持 5 默认。
// 2026-07-15 R131 修复: 7→5,与 config.go applyDefaults 保持一致。
// 2026-07-24 优化:5→7,允许更多次重试 + 线性退避(2s/4s/6s/8s...),给上游
// 抖动更多恢复机会,避免 13 人局连锁 quarantine。
func loadAgentRetryConfigInto(a *Agent) {
	if a == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			if a.maxLLMRetries <= 0 {
				a.maxLLMRetries = 7
			}
			logger.L().Debug("agent: retry config load skipped (recovered)",
				zap.Any("panic", r), zap.Int("seat", a.Seat))
		}
	}()
	cfg := config.Load().Werewolf
	if cfg.LLMMaxRetries > 0 {
		a.maxLLMRetries = cfg.LLMMaxRetries
	} else {
		a.maxLLMRetries = 7
	}
}
