// Package citychrome — scheduler.go: 类目轮转调度 + 熔断退避 + 单飞任务队列
// (契约 §3.6;批次76 修订:单指针轮转 → 类目级分级调度)。
//
//   - 轮转(批次76 §五.6 分级调度):8 类目各自独立计时,每 tick(5s)按
//     MissionCategories 顺序取「距上次运行 ≥ 类目专属间隔」的首个类目入队;
//     行情 6 类目共用 refresh_seconds(默认 1800s),news 分钟级
//     (news_refresh_seconds,默认 600s),macro 日级(macro_refresh_seconds,
//     默认 86400s)。冷启动时未运行过的类目视为到期,逐 tick 顺序补齐;
//   - 单飞:全局同时只跑 1 个 mission(单 worker 串行队列),LLM 调用走 llmSema
//     节流(Agent.acquireLLM);
//   - 熔断:连续 3 次 mission 失败 ⇒ channel=offline + 指数退避
//     (5m→10m→20m→封顶 30m),任一成功即复位 online;
//   - 空闲降载:Hub 内无 virtual_city 房间 ⇒ 暂停自动轮转(HTTP 手动刷新仍可用);
//   - 同站点 ≥10s 间隔纪律(契约 §6)。手动刷新不重置 lastRunAt(只受 60s 限流)。
package citychrome

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"LsmAgentGame/logger"

	"go.uber.org/zap"
)

// 熔断/退避常量(契约 §3.6)。
const (
	breakerThreshold = 3                // 连续失败次数阈值
	backoffBase      = 5 * time.Minute // 首次退避
	backoffMax       = 30 * time.Minute // 封顶
	siteMinInterval  = 10 * time.Second // 同站点访问最小间隔(契约 §6)
)

// MissionRunner 是一次 mission 执行器(Agent.RunMission 签名;测试注入 fake)。
type MissionRunner func(ctx context.Context, m Mission) (*MissionPayload, error)

// SchedulerConfig 是调度器参数。
type SchedulerConfig struct {
	RefreshSeconds      int // 行情类目轮转间隔秒(默认 1800;批次76 起 6 行情类目各自独立计时)
	NewsRefreshSeconds  int // news 类目轮转间隔秒(批次76;默认 600,分钟级)
	MacroRefreshSeconds int // macro 类目轮转间隔秒(批次76;默认 86400,日级)
	QueueCapacity       int // 任务队列容量(默认 8)
}

// Scheduler 是全局单飞任务队列 + 轮转 + 熔断退避。
type Scheduler struct {
	cfg      SchedulerConfig
	store    *FeedStore
	runner   MissionRunner
	healthy  func(ctx context.Context) bool
	hasRooms func() bool // 空闲降载探针(注入:Hub 有无 virtual_city 房间)
	notify   func(Feed)  // mission 成功合并后回调(Service 扇出 listener)

	now   func() time.Time // 可注入(测试)
	sleep func(time.Duration)

	mu           sync.Mutex
	in           chan Mission
	stopCh       chan struct{}
	stopped      bool
	lastRunAt    map[string]time.Time // 类目 → 上次自动轮转入队时间(批次76:替代单指针 rotIdx/lastRotateAt)
	fails        int
	breakerUntil time.Time
	lastSite     map[string]time.Time
	running      bool
}

// NewScheduler 构造。runner 为 nil 时 mission 一律失败(熔断最终会置 offline)。
func NewScheduler(cfg SchedulerConfig, store *FeedStore, runner MissionRunner) *Scheduler {
	if cfg.RefreshSeconds <= 0 {
		cfg.RefreshSeconds = 1800
	}
	if cfg.NewsRefreshSeconds <= 0 {
		cfg.NewsRefreshSeconds = 600
	}
	if cfg.MacroRefreshSeconds <= 0 {
		cfg.MacroRefreshSeconds = 86400
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 8
	}
	return &Scheduler{
		cfg:       cfg,
		store:     store,
		runner:    runner,
		healthy:   func(context.Context) bool { return true },
		hasRooms:  func() bool { return true },
		now:       time.Now,
		sleep:     time.Sleep,
		in:        make(chan Mission, cfg.QueueCapacity),
		stopCh:    make(chan struct{}),
		lastRunAt: map[string]time.Time{},
		lastSite:  map[string]time.Time{},
	}
}

// SetHealthProbe 注入底座健康探针(默认恒 true;Service 装配 client.Healthy)。
// 持锁 —— Run 可能已启动,防止与 execute 的读竞态。
func (s *Scheduler) SetHealthProbe(fn func(ctx context.Context) bool) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthy = fn
}

// SetRoomPresence 注入空闲降载探针(默认恒 true;main.go 装配 vcMgr.RoomIDs)。
func (s *Scheduler) SetRoomPresence(fn func() bool) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hasRooms = fn
}

// SetNotify 注入 mission 成功回调。
func (s *Scheduler) SetNotify(fn func(Feed)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = fn
}

// SetClock 注入时钟(测试用:now/sleep)。
func (s *Scheduler) SetClock(now func() time.Time, sleep func(time.Duration)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now != nil {
		s.now = now
	}
	if sleep != nil {
		s.sleep = sleep
	}
}

// Run 启动调度循环(阻塞;另起 goroutine 调用)。stop 关闭后退出。
func (s *Scheduler) Run() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case m := <-s.in:
			s.execute(m)
		case <-tick.C:
			s.maybeRotate()
		}
	}
}

// Stop 停止调度循环。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
}

// Enqueue 入队一次 mission(队列满返回错误;手动/自动刷新同一出口)。
func (s *Scheduler) Enqueue(m Mission) error {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return fmt.Errorf("scheduler stopped")
	}
	select {
	case s.in <- m:
		return nil
	default:
		return fmt.Errorf("mission queue full(cap %d)", s.cfg.QueueCapacity)
	}
}

// QueueLen 返回当前积压(测试/观测)。
func (s *Scheduler) QueueLen() int { return len(s.in) }

// IsOpen 报告熔断器是否闭合(false = 退避中,自动轮转暂停;手动刷新仍会入队,
// execute 内再判定一次,拒绝期内的 mission 直接计失败)。
func (s *Scheduler) IsOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Before(s.breakerUntil)
}

// ConsecutiveFails 返回当前连续失败计数(测试/观测)。
func (s *Scheduler) ConsecutiveFails() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fails
}

// categoryInterval 返回类目专属轮转间隔(批次76 §五.6 分级调度):
// 行情 6 类目共用 RefreshSeconds;news/Macro 各自独立(默认 600s/86400s)。
func (s *Scheduler) categoryInterval(cat string) time.Duration {
	sec := s.cfg.RefreshSeconds
	switch cat {
	case CategoryNews:
		sec = s.cfg.NewsRefreshSeconds
	case CategoryMacro:
		sec = s.cfg.MacroRefreshSeconds
	}
	return time.Duration(sec) * time.Second
}

// nextRotationCategory 返回下一个到期类目(不推进计时;手动刷新 category 缺省
// 用)。未运行过的类目视为到期;全部未到期时回退 MissionCategories[0]。
// Service.Refresh 调用。
func (s *Scheduler) nextRotationCategory() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cat, ok := s.dueCategoryLocked(); ok {
		return cat
	}
	return MissionCategories[0]
}

// dueCategoryLocked 扫描首个到期类目(须持 s.mu;不修改状态)。
// 冷启动语义:lastRunAt 无记录 = 从未运行 = 立即到期,按序补齐。
// ok=false 表示当前无类目到期 —— maybeRotate 必须据此跳过,否则会每 tick
// 强制入队同一类目(批次76 踩坑:回退值被误当成到期结果 ⇒ 死循环)。
func (s *Scheduler) dueCategoryLocked() (string, bool) {
	for _, cat := range MissionCategories {
		last, ok := s.lastRunAt[cat]
		if !ok || s.now().Sub(last) >= s.categoryInterval(cat) {
			return cat, true
		}
	}
	return "", false
}

// maybeRotate 自动轮转(批次76:类目级分级调度):每 tick 至多入队 1 个到期
// 类目(按 MissionCategories 顺序取首个)。空闲降载 / 熔断 / 已有积压时跳过。
// 入队即记 lastRunAt(入队失败也记 —— 下个间隔再试,避免热循环重试打爆队列)。
func (s *Scheduler) maybeRotate() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if s.now().Before(s.breakerUntil) {
		s.mu.Unlock()
		return // 熔断退避中。
	}
	if len(s.in) > 0 {
		s.mu.Unlock()
		return // 已有排队任务。
	}
	if !s.hasRooms() {
		s.mu.Unlock()
		return // 空闲降载:无 virtual_city 房间。
	}
	cat, due := s.dueCategoryLocked()
	if !due {
		s.mu.Unlock()
		return // 无类目到期(批次76:必须跳过,不得回退首个类目)。
	}
	s.lastRunAt[cat] = s.now()
	s.mu.Unlock()
	logger.L().Info("citychrome rotate mission", zap.String("category", cat))
	if err := s.Enqueue(BuildMission(cat)); err != nil {
		logger.L().Warn("citychrome enqueue rotated mission failed", zap.Error(err))
	}
}

// execute 执行单飞 mission:同站点间隔纪律 → 健康探测 → runner →
// 成功合并复位 / 失败计熔断。
func (s *Scheduler) execute(m Mission) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	now := s.now()
	// 熔断判定(手动刷新入队后可能刚打开熔断)。
	if now.Before(s.breakerUntil) {
		s.mu.Unlock()
		logger.L().Warn("citychrome mission dropped: circuit breaker open",
			zap.String("category", m.Category),
			zap.Time("until", s.breakerUntil))
		return
	}
	// 同站点 ≥10s 间隔纪律(契约 §6)。
	host := siteHost(m.TargetURL)
	if host != "" {
		if last, ok := s.lastSite[host]; ok && now.Sub(last) < siteMinInterval {
			wait := siteMinInterval - now.Sub(last)
			s.mu.Unlock()
			logger.L().Info("citychrome site interval discipline",
				zap.String("host", host), zap.Duration("wait", wait))
			s.sleep(wait)
			s.mu.Lock()
		}
		s.lastSite[host] = s.now()
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if s.healthy != nil && !s.healthy(ctx) {
		s.fail(m, fmt.Errorf("debug tool unreachable"))
		return
	}
	if s.runner == nil {
		s.fail(m, fmt.Errorf("mission runner not bound"))
		return
	}
	payload, err := s.runner(ctx, m)
	if err != nil {
		s.fail(m, err)
		return
	}
	// 成功:合并 + 复位熔断 + 通知。
	nowUnix := s.now().Unix()
	stats := s.store.MergeMissionResult(nowUnix, payload)
	s.store.SetChannel(ChannelOnline, "", nowUnix)
	s.mu.Lock()
	s.fails = 0
	s.breakerUntil = time.Time{}
	s.mu.Unlock()
	logger.L().Info("citychrome mission ok",
		zap.String("category", m.Category),
		zap.String("source", payload.Source),
		zap.Int("quotes_kept", stats.QuotesKept),
		zap.Int("quotes_dropped", stats.QuotesDropped),
		zap.Int("news_added", stats.NewsAdded),
		zap.Int("news_dup", stats.NewsDupSkipped))
	if s.notify != nil {
		s.notify(s.store.Snapshot())
	}
}

// fail 计一次失败;连续 3 次打开熔断并指数退避(5m→10m→20m→封顶 30m)。
// 每次失败都把通道置 offline 并记最近错误(契约 §3.2 Detail 语义);
// 成功(execute 内)复位 online 并清 Detail。
func (s *Scheduler) fail(m Mission, err error) {
	s.mu.Lock()
	s.fails++
	fails := s.fails
	needOpen := fails >= breakerThreshold
	if needOpen {
		backoff := backoffBase << uint((fails-breakerThreshold)/breakerThreshold)
		if backoff > backoffMax {
			backoff = backoffMax
		}
		s.breakerUntil = s.now().Add(backoff)
	}
	breakerUntil := s.breakerUntil
	s.mu.Unlock()
	s.store.SetChannel(ChannelOffline, fmt.Sprintf("mission %s 失败: %v", m.Category, err), s.now().Unix())
	logger.L().Warn("citychrome mission failed",
		zap.String("category", m.Category),
		zap.Int("consecutive_fails", fails),
		zap.Error(err))
	if needOpen {
		logger.L().Warn("citychrome circuit breaker open",
			zap.Time("until", breakerUntil))
	}
}

// siteHost 取站点主域名(同站点间隔纪律的口径;解析失败返回空 = 不约束)。
func siteHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}
