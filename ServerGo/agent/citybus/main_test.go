// Package citybus — main_test.go: 测试入口(预热全局 logger)。
package citybus

import (
	"os"
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/logger"
)

// TestMain 在主协程预热全局 logger。
//
// logger.L() 的懒初始化(**global == nil 时赋值)**不是并发安全的;
// 本包的 Post/postLoop 与并发用例会启动多个 goroutine 首次触发日志,
// 不预热会被 -race 判为 data race。这里显式 Init 一次即可。
func TestMain(m *testing.M) {
	_ = logger.Init(&config.Config{})
	os.Exit(m.Run())
}
