// Package citychrome — scheduler_test.go: 熔断退避 / 同站点间隔 / 轮转与
// 空闲降载单测(契约 §7.1 第 1 条;注入 fake clock + fake runner,
// 同步调 execute/maybeRotate,不走 Run 循环)。
package citychrome

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeClock 是可控时钟(单线程测试用,无锁)。
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) nowFunc() func() time.Time { return func() time.Time { return c.now } }

func (c *fakeClock) sleepFunc() func(time.Duration) {
	return func(d time.Duration) {
		c.sleeps = append(c.sleeps, d)
		c.now = c.now.Add(d)
	}
}

// newTestScheduler 构造注入 fake clock/runner 的调度器。
func newTestScheduler(t *testing.T, runner MissionRunner) (*Scheduler, *fakeClock) {
	t.Helper()
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	st := NewFeedStore(12, 20)
	s := NewScheduler(SchedulerConfig{RefreshSeconds: 1800, QueueCapacity: 8}, st, runner)
	s.SetClock(clk.nowFunc(), clk.sleepFunc())
	return s, clk
}

func TestScheduler_SuccessMergeAndReset(t *testing.T) {
	notified := 0
	s, _ := newTestScheduler(t, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		return &MissionPayload{Category: m.Category, Source: "s",
			Quotes: []RealtimeQuote{quote("A", "a", 1, 1)}}, nil
	})
	s.SetNotify(func(Feed) { notified++ })
	s.execute(BuildMission(CategoryAShare))
	if got := s.store.Snapshot().Channel; got.Status != ChannelOnline || got.LastOKAt == 0 {
		t.Errorf("channel = %+v", got)
	}
	if s.ConsecutiveFails() != 0 {
		t.Errorf("fails = %d, want 0", s.ConsecutiveFails())
	}
	if notified != 1 {
		t.Errorf("notify = %d, want 1", notified)
	}
	if got := len(s.store.Snapshot().Categories); got != 1 {
		t.Errorf("categories = %d, want 1", got)
	}
}

func TestScheduler_CircuitBreakerBackoff(t *testing.T) {
	s, clk := newTestScheduler(t, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		return nil, errors.New("llm boom")
	})
	base := clk.now
	// 2 次失败:熔断未开(但通道已 offline 带原因)。
	for i := 0; i < 2; i++ {
		s.execute(BuildMission(CategoryAShare))
	}
	if s.IsOpen() {
		t.Fatal("breaker should be closed after 2 fails")
	}
	if got := s.store.Snapshot().Channel; got.Status != ChannelOffline || got.Detail == "" {
		t.Errorf("channel should be offline with detail before threshold: %+v", got)
	}
	// 第 3 次失败:熔断打开,退避 5m。
	s.execute(BuildMission(CategoryAShare))
	if !s.IsOpen() {
		t.Fatal("breaker should open after 3 consecutive fails")
	}
	// 退避期间 mission 直接丢弃(不增失败计数)。
	failsBefore := s.ConsecutiveFails()
	s.execute(BuildMission(CategoryHK))
	if s.ConsecutiveFails() != failsBefore {
		t.Errorf("dropped mission should not count fails: %d -> %d", failsBefore, s.ConsecutiveFails())
	}
	// 5m 退避窗口内仍开。
	clk.now = base.Add(4 * time.Minute)
	if !s.IsOpen() {
		t.Error("breaker should still open at 4m (backoff 5m)")
	}
	// 过窗口 → 闭合;再失败一次(fail#4)→ 重新打开 5m。
	clk.now = base.Add(6 * time.Minute)
	if s.IsOpen() {
		t.Error("breaker should close after 5m backoff")
	}
	s.execute(BuildMission(CategoryAShare)) // fail#4 → 打开至 6m+5m=11m
	if !s.IsOpen() {
		t.Fatal("breaker should reopen at fail#4")
	}
	// 跳过窗口 → fail#5(5m)、fail#6(退避升至 10m)。
	clk.now = base.Add(12 * time.Minute)
	s.execute(BuildMission(CategoryAShare)) // fail#5 → 打开至 17m
	clk.now = base.Add(18 * time.Minute)
	s.execute(BuildMission(CategoryAShare)) // fail#6 → 退避 10m,打开至 28m
	if !s.IsOpen() {
		t.Fatal("breaker should be open at fail#6")
	}
	clk.now = base.Add(27 * time.Minute)
	if !s.IsOpen() {
		t.Error("breaker should still open before 10m backoff expires")
	}
	clk.now = base.Add(29 * time.Minute)
	if s.IsOpen() {
		t.Error("breaker should close after 10m backoff")
	}
}

func TestScheduler_BackoffCap30m(t *testing.T) {
	s, clk := newTestScheduler(t, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		return nil, errors.New("boom")
	})
	// 连续 12 次失败,每次失败前跳过退避窗口;单档退避序列为
	// 5m(fail 3) 5m(4) 5m(5) 10m(6) 10m(7) 10m(8) 20m(9) 20m(10) 20m(11) 30m(12,封顶)。
	var lastBackoff time.Duration
	for i := 1; i <= 12; i++ {
		before := clk.now
		s.execute(BuildMission(CategoryAShare))
		s.mu.Lock()
		until := s.breakerUntil
		s.mu.Unlock()
		lastBackoff = until.Sub(before)
		clk.now = until.Add(time.Second) // 跳过退避窗口,进入下一档
	}
	if lastBackoff != 30*time.Minute {
		t.Errorf("12th fail backoff = %v, want capped 30m", lastBackoff)
	}
}

func TestScheduler_HealthProbeFailCountsAsFailure(t *testing.T) {
	s, _ := newTestScheduler(t, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		t.Error("runner should not be called when unhealthy")
		return nil, nil
	})
	s.SetHealthProbe(func(ctx context.Context) bool { return false })
	for i := 0; i < 3; i++ {
		s.execute(BuildMission(CategoryAShare))
	}
	if !s.IsOpen() {
		t.Error("unhealthy probes should trip the breaker")
	}
}

func TestScheduler_SiteIntervalDiscipline(t *testing.T) {
	var runs int
	s, clk := newTestScheduler(t, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		runs++
		return &MissionPayload{Category: m.Category, Source: "s"}, nil
	})
	// 同一 host 连续两次 execute:第二次应 sleep 补足 10s。
	s.execute(BuildMission(CategoryAShare)) // quote.eastmoney.com
	clk.now = clk.now.Add(3 * time.Second)
	s.execute(BuildMission(CategoryAShare)) // 同 host,应等 7s
	if len(clk.sleeps) != 1 || clk.sleeps[0] != 7*time.Second {
		t.Errorf("sleeps = %v, want [7s]", clk.sleeps)
	}
	if runs != 2 {
		t.Errorf("runs = %d, want 2", runs)
	}
	// 换类目(host 不同)→ 不睡。
	clk.now = clk.now.Add(20 * time.Second)
	s.execute(BuildMission(CategoryHK))
	if len(clk.sleeps) != 1 {
		t.Errorf("different host should not sleep: %v", clk.sleeps)
	}
}

func TestScheduler_RotationAndIdleUnload(t *testing.T) {
	s, clk := newTestScheduler(t, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		return &MissionPayload{Category: m.Category, Source: "s"}, nil
	})
	// 空闲降载:无房间 → 不入队。
	s.SetRoomPresence(func() bool { return false })
	s.maybeRotate()
	if len(s.in) != 0 {
		t.Error("rotation should pause when no virtual_city rooms")
	}
	// 有房间 → 冷启动逐类补齐:每 tick 1 个,序 0 = a_share。
	s.SetRoomPresence(func() bool { return true })
	s.maybeRotate()
	if len(s.in) != 1 {
		t.Fatalf("queue len = %d, want 1", len(s.in))
	}
	m := <-s.in
	if m.Category != CategoryAShare {
		t.Errorf("first rotation = %q, want a_share", m.Category)
	}
	// 冷启动下一 tick → 序 1 = hk(未运行过的类目视为到期)。
	s.maybeRotate()
	if len(s.in) != 1 {
		t.Fatalf("queue len = %d, want 1 (cold start fill)", len(s.in))
	}
	m = <-s.in
	if m.Category != CategoryHK {
		t.Errorf("second rotation = %q, want hk (cold start)", m.Category)
	}
	// 全部 8 类目跑过一轮后(t 各记为 t0):未到任何间隔 → 不再入队。
	for i := 0; i < 6; i++ {
		s.maybeRotate()
		if len(s.in) != 1 {
			t.Fatalf("cold start fill %d: queue len = %d, want 1", i+3, len(s.in))
		}
		<-s.in
	}
	s.maybeRotate()
	if len(s.in) != 0 {
		t.Error("rotation should respect per-category intervals")
	}
	// 到行情间隔(1800s)→ a_share 首个到期。
	clk.now = clk.now.Add(1801 * time.Second)
	s.maybeRotate()
	if len(s.in) != 1 {
		t.Fatalf("queue len = %d, want 1 after interval", len(s.in))
	}
	if m = <-s.in; m.Category != CategoryAShare {
		t.Errorf("rotation after interval = %q, want a_share", m.Category)
	}
	// 队列有积压 → 不叠加。
	s.in <- BuildMission(CategoryUS)
	s.maybeRotate()
	if len(s.in) != 1 {
		t.Error("rotation should not stack when queue busy")
	}
}

// TestScheduler_PerCategoryCadence 批次76 §五.6 分级调度:news 分钟级(600s)、
// macro 日级(此处用 7200s 缩短用例)、行情类目统一间隔(1800s)各自独立计时。
func TestScheduler_PerCategoryCadence(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	st := NewFeedStore(12, 20)
	s := NewScheduler(SchedulerConfig{
		RefreshSeconds:      1800,
		NewsRefreshSeconds:  600,
		MacroRefreshSeconds: 7200,
		QueueCapacity:       8,
	}, st, func(ctx context.Context, m Mission) (*MissionPayload, error) {
		return &MissionPayload{Category: m.Category, Source: "s"}, nil
	})
	s.SetClock(clk.nowFunc(), clk.sleepFunc())
	t0 := clk.now
	// 冷启动:8 次 maybeRotate 逐类入队并排空,全部 lastRunAt = t0。
	for i := 0; i < len(MissionCategories); i++ {
		s.maybeRotate()
		if len(s.in) != 1 {
			t.Fatalf("cold start %d: queue len = %d, want 1", i, len(s.in))
		}
		<-s.in
	}
	// t0+599s:news 未到 600s,其余更久 → 无一到期。
	clk.now = t0.Add(599 * time.Second)
	s.maybeRotate()
	if len(s.in) != 0 {
		t.Error("nothing should be due at +599s")
	}
	// t0+601s:仅 news 到期。
	clk.now = t0.Add(601 * time.Second)
	s.maybeRotate()
	if len(s.in) != 1 {
		t.Fatalf("queue len = %d at +601s, want 1 (news)", len(s.in))
	}
	if m := <-s.in; m.Category != CategoryNews {
		t.Errorf("due at +601s = %q, want news", m.Category)
	}
	// t0+1801s:行情类目到期,序首 a_share(news 刚跑过未到 600s)。
	clk.now = t0.Add(1801 * time.Second)
	s.maybeRotate()
	if len(s.in) != 1 {
		t.Fatalf("queue len = %d at +1801s, want 1", len(s.in))
	}
	if m := <-s.in; m.Category != CategoryAShare {
		t.Errorf("due at +1801s = %q, want a_share", m.Category)
	}
	// 排空其余到期行情类目(同 tick 序内逐个)。
	for {
		s.maybeRotate()
		if len(s.in) == 0 {
			break
		}
		<-s.in
	}
	// t0+7201s:macro 到期(此前 news 又到期过也无妨,序扫描先遇 crypto 前
	// 的到期行情类目——此处直接断言 macro 在到期集合内)。
	clk.now = t0.Add(7201 * time.Second)
	due := map[string]bool{}
	for {
		s.maybeRotate()
		if len(s.in) == 0 {
			break
		}
		m := <-s.in
		due[m.Category] = true
	}
	if !due[CategoryMacro] {
		t.Errorf("macro should be due at +7201s, due set = %v", due)
	}
}

func TestScheduler_EnqueueAfterStop(t *testing.T) {
	s, _ := newTestScheduler(t, nil)
	s.Stop()
	if err := s.Enqueue(BuildMission(CategoryAShare)); err == nil {
		t.Error("enqueue after stop should fail")
	}
}

func TestScheduler_NilRunnerFailsMission(t *testing.T) {
	s, _ := newTestScheduler(t, nil)
	s.execute(BuildMission(CategoryAShare))
	if s.ConsecutiveFails() != 1 {
		t.Errorf("fails = %d, want 1", s.ConsecutiveFails())
	}
}

func TestScheduler_SingleFlightByChannel(t *testing.T) {
	// in 是带缓冲 chan,单 worker 从 chan 取任务 —— 构造层面保证单飞;
	// 此处验证队列容量边界。
	s, _ := newTestScheduler(t, nil)
	for i := 0; i < 8; i++ {
		if err := s.Enqueue(BuildMission(CategoryAShare)); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if err := s.Enqueue(BuildMission(CategoryAShare)); err == nil {
		t.Error("9th enqueue should hit capacity")
	}
	if s.QueueLen() != 8 {
		t.Errorf("QueueLen = %d, want 8", s.QueueLen())
	}
}
