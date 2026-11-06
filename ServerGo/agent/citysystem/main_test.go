// Package citysystem — main_test.go: 测试入口(预热全局 logger)。
package citysystem

import (
	"os"
	"testing"

	"LsmAgentGame/config"
	"LsmAgentGame/game/virtual_city"
	"LsmAgentGame/logger"
)

// TestMain 在主协程预热全局 logger(logger.L() 懒初始化非并发安全;
// 线路策略并发用例会多 goroutine 首次触发日志,-race 下会误报)。
func TestMain(m *testing.M) {
	_ = logger.Init(&config.Config{})
	// 批次86:场景注入会落盘快照到 ./virtual_city_saves/<roomID>/;
	// 测试重定向到临时目录,避免污染仓库工作目录。
	dir, _ := os.MkdirTemp("", "vc-citysystem-test-")
	if dir != "" {
		virtual_city.SetSnapshotDir(dir)
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
