// Package virtual_city — watchdog.go: 月窗口 watchdog + Agent 超时兜底(2026-09-14 §财商流P0)。
//
// 契约: 后端架构文档 §14。月窗口 watchdog 监测 tick 卡死,Agent 超时
// 由 vcplayer.Agent 的 ctx(decisionTimeout)+ room.runLoop 的 settleCh
// 兜底(强制 submit_month,summary="timeout")。本文件提供的 watchdog 是
// 第二层防御:若因 bug 导致房间 loop 整卡死(settleCh 未触达且 timer 失效),
// 1.5× month_ms 后强制推进并 logger.Error。
package virtual_city

import (
	"time"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// StartWatchdog 启动房间监控 goroutine(每 2s 检查一次)。
// onStuck 在检测到 stuck 时被调用(锁外):通常发出 settleCh 信号触发强制结算。
func (r *VirtualCityRoom) StartWatchdog(onStuck func()) {
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-t.C:
				r.mu.Lock()
				if r.closed || r.Status != StatusPlaying || r.Paused {
					r.mu.Unlock()
					continue
				}
				threshold := time.Duration(r.MonthMs)*time.Millisecond + time.Duration(r.MonthMs)*time.Millisecond/2
				stuck := time.Since(r.NextMonthAt) > threshold
				r.mu.Unlock()
				if stuck {
					logger.L().Error("virtual_city room stuck beyond watchdog threshold, forcing settle",
						zap.String("room_id", r.RoomID),
						zap.Duration("threshold", threshold))
					if onStuck != nil {
						onStuck()
					} else {
						select {
						case r.settleCh <- struct{}{}:
						default:
						}
					}
				}
			}
		}
	}()
}