// Package citychrome — scheduler.go: 类目轮转调度 + 熔断退避 + 单飞任务队列
// (契约 §3.6;批次76 修订:单指针轮转 → 类目级分级调度)。
//
//   - 轮转(批次76 §五.6 分级调度;批次88 交易时段感知修订):8 类目各自
//     独立计时,每 tick(5s)按 MissionCategories 顺序取「距上次运行 ≥ 类目
//     专属间隔」的首个类目入队;行情 6 类目按交易时段分档 —— 开市
//     quote_open_refresh_seconds(默认 600s,分钟级)/休市
//     quote_closed_refresh_seconds(默认 7200s,降频 12×),news 分钟级
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
	backoffBase      = 5 * time.Minute  // 首次退避
	backoffMax       = 30 * time.Minute // 封顶
	siteMinInterval  = 10 * time.Second // 同站点访问最小间隔(契约 §6)
)

// 默认类目刷新间隔(秒)——与 config.CityChromeConfig 的 applyDefaults 缺省
// 一致(批次76 §五.6 分级调度;批次88 §三.2/§五.6 交易时段分档)。
// 批次88 返工起:调度轮转与 realworld_info stale 判定统一经 IntervalTiers
// .Seconds 解析(配置档位优先、本常量兜底)——「stale 阈值恒等于 2×调度器
// 真实轮转间隔」由同一解析路径保证。
const (
	// defaultQuoteRefreshSeconds 是批次88 前的行情统一间隔(1800s)。
	// 批次88 起行情档位由 defaultQuoteOpen/ClosedRefreshSeconds 取代;
	// RefreshSeconds 字段仅保留兼容旧配置,不再决定行情轮转档位。
	defaultQuoteRefreshSeconds = 1800
	// defaultQuoteOpenRefreshSeconds 行情开市档(分钟级,与 news 600s 同口径)。
	defaultQuoteOpenRefreshSeconds = 600
	// defaultQuoteClosedRefreshSeconds 行情休市档(非交易时段降频 12×)。
	defaultQuoteClosedRefreshSeconds = 7200
	defaultNewsRefreshSeconds        = 600   // news 类目(分钟级)
	defaultMacroRefreshSeconds       = 86400 // macro 类目(日级)
)

// IntervalTiers 是类目轮转间隔档位(秒)。**0 值字段 ⇒ 回退包默认档位**
// (行情开/休市 600/7200、news 600、macro 86400)。
// 调度器(Scheduler.categoryInterval)与 realworld_info stale 判定
// (FeedCompactJSON)共用本结构的 Seconds 解析 —— 可配置值优先于包默认,
// 同一时刻、同一档位,故任意合法配置下 stale 阈值恒等于 2×调度真实轮转
// 间隔(批次88 同口径)。
type IntervalTiers struct {
	QuoteOpenSeconds   int // 行情开市档(0 ⇒ 600)
	QuoteClosedSeconds int // 行情休市档(0 ⇒ 7200)
	NewsSeconds        int // news 档(0 ⇒ 600)
	MacroSeconds       int // macro 档(0 ⇒ 86400)
}

// Seconds 返回类目 cat 在 now 时刻的轮转间隔(秒;可配置值优先于包默认)。
// 行情 6 类目按交易时段开市/休市分档(CategoryMarketOpen,纯函数);
// news/macro 固定档(不受时段约束)。
func (t IntervalTiers) Seconds(cat string, now time.Time) int64 {
	switch cat {
	case CategoryNews:
		if t.NewsSeconds > 0 {
			return int64(t.NewsSeconds)
		}
		return defaultNewsRefreshSeconds
	case CategoryMacro:
		if t.MacroSeconds > 0 {
			return int64(t.MacroSeconds)
		}
		return defaultMacroRefreshSeconds
	}
	open, closed := int64(defaultQuoteOpenRefreshSeconds), int64(defaultQuoteClosedRefreshSeconds)
	if t.QuoteOpenSeconds > 0 {
		open = int64(t.QuoteOpenSeconds)
	}
	if t.QuoteClosedSeconds > 0 {
		closed = int64(t.QuoteClosedSeconds)
	}
	if CategoryMarketOpen(cat, now) {
		return open
	}
	return closed
}

// categoryRefreshInterval 返回类目在**包缺省档位**下的刷新间隔(秒)
// (= 零值 IntervalTiers 的解析结果;批次88 修订:行情开市 600s /
// 休市 7200s,news 600s、macro 86400s)。
//
// ⚠ 批次88 返工后用途收窄:生产 stale 判定(realworld.go)与调度轮转
// (Scheduler.categoryInterval)均走 IntervalTiers.Seconds(配置档位优先),
// 本函数只是「零配置档位」便捷入口/测试锚点,不再声称唯一事实来源。
func categoryRefreshInterval(cat string, now time.Time) int64 {
	return IntervalTiers{}.Seconds(cat, now)
}

// MissionRunner 是一次 mission 执行器(Agent.RunMission 签名;测试注入 fake)。
type MissionRunner func(ctx context.Context, m Mission) (*MissionPayload, error)

// SchedulerConfig 是调度器参数。
type SchedulerConfig struct {
	// RefreshSeconds 是批次88 前的行情统一间隔(legacy:保留兼容,不再决定
	// 行情轮转档位 —— 批次88 起由 QuoteOpen/QuoteClosedSeconds 取代)。
	RefreshSeconds int
	// QuoteOpenSeconds 行情开市档轮转间隔秒(批次88 §三.2/§五.6;0 ⇒ 包默认 600)。
	QuoteOpenSeconds int
	// QuoteClosedSeconds 行情休市档轮转间隔秒(批次88;0 ⇒ 包默认 7200)。
	QuoteClosedSeconds int
	// NewsRefreshSeconds news 类目轮转间隔秒(批次76;默认 600,分钟级)。
	NewsRefreshSeconds int
	// MacroRefreshSeconds macro 类目轮转间隔秒(批次76;默认 86400,日级)。
	MacroRefreshSeconds int
	// QueueCapacity 任务队列容量(默认 8)。
	QueueCapacity int
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

	// onComplete 是 mission 完成回调(批次84:成功/失败各触发一次)。nil = 不回调。
	// 回调在调度 goroutine 内执行,实现方不得阻塞(只做非阻塞投递)。
	onComplete func(Mission, *MissionPayload, error)

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
		cfg.RefreshSeconds = defaultQuoteRefreshSeconds
	}
	if cfg.QuoteOpenSeconds <= 0 {
		cfg.QuoteOpenSeconds = defaultQuoteOpenRefreshSeconds
	}
	if cfg.QuoteClosedSeconds <= 0 {
		cfg.QuoteClosedSeconds = defaultQuoteClosedRefreshSeconds
	}
	if cfg.NewsRefreshSeconds <= 0 {
		cfg.NewsRefreshSeconds = defaultNewsRefreshSeconds
	}
	if cfg.MacroRefreshSeconds <= 0 {
		cfg.MacroRefreshSeconds = defaultMacroRefreshSeconds
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

// SetOnComplete 注入 mission **完成**回调(批次84:成功与失败各触发一次,
// 让「Agent 对话」的等待者能拿到结果或失败原因)。
//
// 回调在调度 goroutine 内同步执行:实现方必须是非阻塞的(只做投递)。
func (s *Scheduler) SetOnComplete(fn func(Mission, *MissionPayload, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onComplete = fn
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

// categoryInterval 返回类目专属轮转间隔(批次76 §五.6 分级调度;批次88 修订:
// 行情 6 类目按交易时段分档 —— 开市 QuoteOpenSeconds / 休市 QuoteClosedSeconds,
// 可配置值优先于包默认 600/7200;news/Macro 仍按各自配置)。
// 解析统一走 IntervalTiers.Seconds —— 与 realworld stale 判定同一路径,
// 「stale 阈值恒等于 2×本间隔」由构造保证(批次88 同口径)。
func (s *Scheduler) categoryInterval(cat string, now time.Time) time.Duration {
	return time.Duration(s.tiers().Seconds(cat, now)) * time.Second
}

// tiers 把 SchedulerConfig 映射为共享档位解析结构(批次88:与 Service 侧
// stale 判定构造同一形状、同一字段,保证两边逐档一致)。
func (s *Scheduler) tiers() IntervalTiers {
	return IntervalTiers{
		QuoteOpenSeconds:   s.cfg.QuoteOpenSeconds,
		QuoteClosedSeconds: s.cfg.QuoteClosedSeconds,
		NewsSeconds:        s.cfg.NewsRefreshSeconds,
		MacroSeconds:       s.cfg.MacroRefreshSeconds,
	}
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
	now := s.now() // 批次88:档位判定与计时取同一时刻(同口径)。
	for _, cat := range MissionCategories {
		last, ok := s.lastRunAt[cat]
		if !ok || now.Sub(last) >= s.categoryInterval(cat, now) {
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
		// 批次84:任务被丢弃也要通知等待者(否则「Agent 对话」的等待者只能超时)。
		s.complete(m, nil, fmt.Errorf("熔断器开启,本轮采集任务被丢弃"))
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
		s.complete(m, nil, fmt.Errorf("采集底座不可达(debug tool unreachable)"))
		return
	}
	if s.runner == nil {
		s.fail(m, fmt.Errorf("mission runner not bound"))
		s.complete(m, nil, fmt.Errorf("采集执行器未绑定"))
		return
	}
	payload, err := s.runner(ctx, m)
	if err != nil {
		s.fail(m, err)
		s.complete(m, nil, err)
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
	// 批次84:成功也走完成回调(唤醒「Agent 对话」等待者 → 结果上行 City-System)。
	s.complete(m, payload, nil)
}

// complete 触发 mission 完成回调(成功/失败各一次)。
//
// 锁纪律(§92a):调用方**不得**持 s.mu;本方法内部只短暂取锁取回调引用。
func (s *Scheduler) complete(m Mission, payload *MissionPayload, err error) {
	s.mu.Lock()
	fn := s.onComplete
	s.mu.Unlock()
	if fn == nil {
		return
	}
	fn(m, payload, err)
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
