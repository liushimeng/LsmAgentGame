// Package citychrome — service.go: Service 全局单例(Start/Get/Stop)。
//
// 装配:WebUseClient + FeedStore + Agent + Scheduler;广播出口为 listener
// 注入(OnFeed),本包不 import ws(契约 §2 关键决策 1/2)。
// enabled=false 或底座不可达 ⇒ 空转,接口返回 channel.status="offline",
// 绝不阻塞对局(契约 §3.1)。
package citychrome

import (
	"context"
	"fmt"
	"sync"
	"time"

	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// manualRefreshWindow 是 HTTP 手动刷新同类目限流窗口(契约 §3.5:60s)。
const manualRefreshWindow = 60 * time.Second

var (
	instanceMu sync.RWMutex
	instance   *Service
)

// Service 是 City-Chrome 数据通道全局服务。
type Service struct {
	cfg    config.CityChromeConfig
	store  *FeedStore
	client *WebUseClient
	agent  *Agent
	sched  *Scheduler

	// llmSema 复用项目 llmSema 节流模式(全局 LLM 并发槽,cap 1 ——
	// 调度器本身单飞,这里再兜一层防手动刷新与轮转竞态)。
	llmSema chan struct{}

	listenersMu sync.RWMutex
	listeners   []func(Feed)

	refreshMu         sync.Mutex
	lastManualRefresh map[string]time.Time
	refreshNow        func() time.Time // 可注入(测试)
}

// Start 构造并启动全局服务(幂等:重复调用返回既有实例)。
// enabled=false 时仍创建占位实例(离线),HTTP 快照可用、刷新返回 offline。
func Start(cfg config.CityChromeConfig, reg LLMRegistry) *Service {
	instanceMu.Lock()
	if instance != nil {
		svc := instance
		instanceMu.Unlock()
		return svc
	}
	svc := newService(cfg, reg)
	instance = svc
	instanceMu.Unlock()
	logger.L().Info("citychrome service started",
		zap.Bool("enabled", cfg.Enabled),
		zap.String("debug_tool_url", cfg.DebugToolURL),
		zap.String("model_key", cfg.ModelKey),
		zap.Int("refresh_seconds", cfg.RefreshSeconds),
		zap.Int("news_refresh_seconds", cfg.NewsRefreshSeconds),
		zap.Int("macro_refresh_seconds", cfg.MacroRefreshSeconds))
	if cfg.Enabled {
		go svc.sched.Run()
	}
	return svc
}

// Get 返回全局实例(未 Start 时返回 nil;调用方必须 nil-safe)。
func Get() *Service {
	instanceMu.RLock()
	defer instanceMu.RUnlock()
	return instance
}

// Stop 停止调度循环并清空全局实例(main.go 优雅退出接入)。
func Stop() {
	instanceMu.Lock()
	svc := instance
	instance = nil
	instanceMu.Unlock()
	if svc != nil && svc.sched != nil {
		svc.sched.Stop()
	}
}

func newService(cfg config.CityChromeConfig, reg LLMRegistry) *Service {
	svc := &Service{
		cfg:               cfg,
		store:             NewFeedStore(cfg.SymbolsPerCategory, cfg.NewsLimit),
		llmSema:           make(chan struct{}, 1),
		lastManualRefresh: map[string]time.Time{},
		refreshNow:        time.Now,
	}
	now := time.Now().Unix()
	if !cfg.Enabled {
		svc.store.SetChannel(ChannelOffline, "city_chrome.enabled=false", now)
		return svc
	}
	svc.client = NewWebUseClient(cfg.DebugToolURL, cfg.AuthToken, cfg.RequestTimeoutMs)
	svc.agent = NewAgent(AgentConfig{
		ModelKey:           cfg.ModelKey,
		MaxStepsPerMission: cfg.MaxStepsPerMission,
		SymbolsPerCategory: cfg.SymbolsPerCategory,
		NewsLimit:          cfg.NewsLimit,
	}, svc.client, reg)
	// LLM 节流槽(5s 获取超时,复用项目 AcquireLLM/ReleaseLLM 语义)。
	svc.agent.SetLLMAcquire(func(ctx context.Context) (func(), bool) {
		select {
		case svc.llmSema <- struct{}{}:
			return func() {
				select {
				case <-svc.llmSema:
				default:
				}
			}, true
		case <-ctx.Done():
			return nil, false
		case <-time.After(5 * time.Second):
			return nil, false
		}
	})
	svc.sched = NewScheduler(SchedulerConfig{
		RefreshSeconds:      cfg.RefreshSeconds,
		NewsRefreshSeconds:  cfg.NewsRefreshSeconds,
		MacroRefreshSeconds: cfg.MacroRefreshSeconds,
	}, svc.store, svc.agent.RunMission)
	svc.sched.SetHealthProbe(svc.client.Healthy)
	svc.sched.SetNotify(svc.emitFanout)
	// 熔断初始态:未知,先按 offline 起步,首次成功 mission 翻 online。
	svc.store.SetChannel(ChannelOffline, "等待首次 mission", now)
	return svc
}

// SetRoomPresence 注入空闲降载探针(main.go:Hub 有无 virtual_city 房间)。
// 必须在 Start 后、调度循环消费前调用。
func (s *Service) SetRoomPresence(fn func() bool) {
	if s != nil && s.sched != nil {
		s.sched.SetRoomPresence(fn)
	}
}

// Enabled 报告配置开关。
func (s *Service) Enabled() bool { return s != nil && s.cfg.Enabled }

// Feed 返回当前快照(深拷贝;未 Start 由调用方 nil 守卫,此处再兜一层)。
func (s *Service) Feed() Feed {
	if s == nil {
		return Feed{Categories: map[string]RealtimeCategory{}, News: []RealtimeNews{}}
	}
	return s.store.Snapshot()
}

// Channel 返回通道状态。
func (s *Service) Channel() ChannelStatus {
	if s == nil {
		return ChannelStatus{Status: ChannelOffline, Detail: "service not started"}
	}
	return s.store.Channel()
}

// OnFeed 订阅 Feed 更新(广播出口:WS 层 listener 注入;每次成功 mission 后回调)。
// 返回取消函数。回调在调度 goroutine 内同步执行,不得阻塞。
func (s *Service) OnFeed(fn func(Feed)) func() {
	if s == nil || fn == nil {
		return func() {}
	}
	s.listenersMu.Lock()
	s.listeners = append(s.listeners, fn)
	idx := len(s.listeners) - 1
	s.listenersMu.Unlock()
	return func() {
		s.listenersMu.Lock()
		defer s.listenersMu.Unlock()
		if idx < len(s.listeners) && s.listeners[idx] != nil {
			s.listeners[idx] = nil
		}
	}
}

// emitFanout 调度器 notify 回调:扇出全部 listener。
func (s *Service) emitFanout(f Feed) {
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()
	for _, fn := range s.listeners {
		if fn != nil {
			fn(f)
		}
	}
}

// SetRefreshClock 注入手动刷新限流的时钟(测试)。
func (s *Service) SetRefreshClock(now func() time.Time) {
	if s != nil && now != nil {
		s.refreshNow = now
	}
}

// Refresh 入队一次 mission(HTTP 手动刷新;契约 §3.5)。
//   - category 空 = 取轮转下一类目;
//   - 同类目 60s 内重复 → 35054(429 语义);
//   - 通道 disabled → 35053;熔断退避中仍可入队(execute 内判定)。
//
// 返回 accepted + queuedCategory;失败返回 *errcode.Error。
func (s *Service) Refresh(category string) (bool, string, *errcode.Error) {
	if s == nil {
		return false, "", errcode.Code(errcode.ErrCityChromeChannelOffline)
	}
	if !s.cfg.Enabled {
		return false, "", errcode.CodeMsg(errcode.ErrCityChromeChannelOffline,
			"数据通道离线(city_chrome.enabled=false)")
	}
	if category == "" {
		category = s.sched.nextRotationCategory()
	}
	if !IsMissionCategory(category) {
		return false, "", errcode.CodeMsg(errcode.ErrValidationFailed,
			fmt.Sprintf("未知 category %q(须 a_share|hk|us|metal|futures|crypto|macro|news)", category))
	}
	s.refreshMu.Lock()
	now := s.refreshNow()
	if last, ok := s.lastManualRefresh[category]; ok && now.Sub(last) < manualRefreshWindow {
		remain := int(manualRefreshWindow-now.Sub(last)) / int(time.Second)
		s.refreshMu.Unlock()
		return false, category, errcode.CodeMsg(errcode.ErrCityChromeRefreshTooFrequent,
			fmt.Sprintf("同类目刷新过于频繁,请 %ds 后重试(429 语义)", remain))
	}
	s.lastManualRefresh[category] = now
	s.refreshMu.Unlock()
	if err := s.sched.Enqueue(BuildMission(category)); err != nil {
		s.refreshMu.Lock()
		delete(s.lastManualRefresh, category)
		s.refreshMu.Unlock()
		return false, category, errcode.CodeMsg(errcode.ErrInternal, err.Error())
	}
	return true, category, nil
}
