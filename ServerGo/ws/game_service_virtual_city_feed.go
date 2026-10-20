// Package ws — game_service_virtual_city_feed.go: City-Chrome 实时数据
// WS 广播(批次65,2026-10-20 §批次65;契约 §3.3)。
//
// 订阅 citychrome.OnFeed(listener 注入,citychrome 不依赖本包)→ 节流合并
// (距上次广播 <10s 挂起合并,全局单飞:上一个广播未落地不叠加)→ 枚举
// vcMgr 全部 virtual_city 房间 → 广播帧 {type:"game.realtime_feed",
// feed:<Feed>}(BroadcastRoomIncludingSpectators 含观战者)。
package ws

import (
	"sync"
	"time"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// realtimeFeedMinInterval 是广播节流间隔(契约 §3.3:≥10s)。
const realtimeFeedMinInterval = 10 * time.Second

// realtimeFeedBcaster 是 game.realtime_feed 广播器(节流合并 + 单飞)。
type realtimeFeedBcaster struct {
	s *GameService

	mu       sync.Mutex
	lastAt   time.Time
	pending  *citychrome.Feed
	timer    *time.Timer
	inFlight bool
}

// WireCityChromeFeed 订阅 citychrome Feed 更新并接线广播。svc/vcMgr/hub
// 任一为 nil 时 no-op 并告警(§130:nil-safe 接线)。
func (s *GameService) WireCityChromeFeed(svc *citychrome.Service) {
	if svc == nil || s.vcMgr == nil || s.hub == nil {
		logger.L().Warn("citychrome realtime_feed broadcast not wired",
			zap.Bool("svc_nil", svc == nil),
			zap.Bool("vc_mgr_nil", s.vcMgr == nil),
			zap.Bool("hub_nil", s.hub == nil))
		return
	}
	b := &realtimeFeedBcaster{s: s}
	svc.OnFeed(b.onFeed)
	logger.L().Info("citychrome realtime_feed broadcast wired")
}

// onFeed 是 OnFeed 回调(调度 goroutine 内同步执行;仅做状态记账,立即返回)。
// 语义:快照全量,pending 覆盖 = 挂起合并;inFlight 期间只挂起不叠加(单飞)。
func (b *realtimeFeedBcaster) onFeed(f citychrome.Feed) {
	b.mu.Lock()
	snap := f
	b.pending = &snap
	if b.inFlight {
		b.mu.Unlock()
		return
	}
	if elapsed := time.Since(b.lastAt); elapsed >= realtimeFeedMinInterval {
		b.mu.Unlock()
		go b.flush()
		return
	}
	if b.timer == nil {
		wait := realtimeFeedMinInterval - time.Since(b.lastAt)
		b.timer = time.AfterFunc(wait, func() {
			b.mu.Lock()
			b.timer = nil
			b.mu.Unlock()
			go b.flush()
		})
	}
	b.mu.Unlock()
}

// flush 取走挂起快照并广播(锁内只改状态,发送在锁外)。
func (b *realtimeFeedBcaster) flush() {
	b.mu.Lock()
	f := b.pending
	b.pending = nil
	if f == nil {
		b.mu.Unlock()
		return
	}
	b.lastAt = time.Now()
	b.inFlight = true
	b.mu.Unlock()

	b.s.broadcastRealtimeFeed(*f)

	b.mu.Lock()
	b.inFlight = false
	again := b.pending != nil
	b.mu.Unlock()
	if again {
		// 广播期间又有 mission 完成:补发最新快照(pending 始终只有一份)。
		go b.flush()
	}
}

// broadcastRealtimeFeed 枚举全部 virtual_city 房间广播 game.realtime_feed。
func (s *GameService) broadcastRealtimeFeed(f citychrome.Feed) {
	env := wsEnvelope("game.realtime_feed", 0, map[string]any{"feed": f})
	rooms := s.vcMgr.RoomIDs()
	for _, rid := range rooms {
		s.hub.BroadcastRoomIncludingSpectators(rid, env)
	}
	if len(rooms) > 0 {
		logger.L().Debug("realtime_feed broadcast",
			zap.Int("rooms", len(rooms)),
			zap.String("channel", f.Channel.Status))
	}
}
