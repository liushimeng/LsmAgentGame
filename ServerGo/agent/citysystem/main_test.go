// Package citysystem — main_test.go: 测试入口(预热全局 logger)。
package citysystem

import (
	"os"
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/logger"
)

// TestMain 在主协程预热全局 logger(logger.L() 懒初始化非并发安全;
// 线路策略并发用例会多 goroutine 首次触发日志,-race 下会误报)。
func TestMain(m *testing.M) {
	_ = logger.Init(&config.Config{})
	os.Exit(m.Run())
}
