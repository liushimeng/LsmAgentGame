// ip_ratelimit_test.go — 20260923-01 §4.3 / §9 单测。
//
// 覆盖：burst 内放行 → 超限拒绝且 retryAfter>0、令牌回滴后恢复、
// 空 IP 一律放行（进程内调用/单测短路）、未注册桶 fail-open、
// 非法 spec 不注册、Janitor 清扫空闲 IP 桶、CeilSeconds 向上取整。
package util

import (
	"testing"
	"time"
)

func TestRateLimiter_BurstThenDenyWithRetryAfter(t *testing.T) {
	l := NewRateLimiter(2*time.Second, RateBucketSpec{Name: "login", PerMinute: 60, Burst: 3})
	for i := 1; i <= 3; i++ {
		ok, retry := l.Allow("login", "1.2.3.4")
		if !ok {
			t.Fatalf("attempt %d within burst=3 was denied (retryAfter %v)", i, retry)
		}
		if retry != 0 {
			t.Fatalf("attempt %d returned retryAfter %v, want 0", i, retry)
		}
	}
	ok, retry := l.Allow("login", "1.2.3.4")
	if ok {
		t.Fatal("attempt 4 must be denied once the burst is exhausted")
	}
	if retry <= 0 {
		t.Fatalf("denied attempt must report retryAfter > 0, got %v", retry)
	}
	// 另一个 IP 有自己独立的桶（不误伤）。
	if ok, _ := l.Allow("login", "5.6.7.8"); !ok {
		t.Fatal("a different IP must not inherit the exhausted bucket")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	// 6000/min = 100 tokens/s ⇒ 50ms 至少回滴 5 个令牌。
	l := NewRateLimiter(0, RateBucketSpec{Name: "captcha", PerMinute: 6000, Burst: 1})
	if ok, _ := l.Allow("captcha", "9.9.9.9"); !ok {
		t.Fatal("first attempt must pass")
	}
	if ok, _ := l.Allow("captcha", "9.9.9.9"); ok {
		t.Fatal("second immediate attempt must be denied (burst=1)")
	}
	time.Sleep(60 * time.Millisecond)
	if ok, retry := l.Allow("captcha", "9.9.9.9"); !ok {
		t.Fatalf("attempt after refill was denied (retryAfter %v)", retry)
	}
}

func TestRateLimiter_EmptyIPAlwaysPasses(t *testing.T) {
	l := NewRateLimiter(time.Second, RateBucketSpec{Name: "ws", PerMinute: 1, Burst: 1})
	for i := 0; i < 50; i++ {
		ok, retry := l.Allow("ws", "")
		if !ok || retry != 0 {
			t.Fatalf("empty IP must always pass (in-proc/unit-test short-circuit), got ok=%v retry=%v", ok, retry)
		}
	}
	// 空 IP 不建条目（否则所有匿名调用共享一个 map key）。
	l.mu.Lock()
	n := len(l.buckets["ws"])
	l.mu.Unlock()
	if n != 0 {
		t.Fatalf("empty IP created %d buckets, want 0", n)
	}
}

func TestRateLimiter_UnknownBucketPasses(t *testing.T) {
	l := NewRateLimiter(time.Second, RateBucketSpec{Name: "login", PerMinute: 1, Burst: 1})
	// 未注册桶 fail-open：注册是显式的（main.go），拼错桶名不能变成全拒。
	for i := 0; i < 10; i++ {
		if ok, retry := l.Allow("nope", "1.2.3.4"); !ok || retry != 0 {
			t.Fatalf("unknown bucket must pass, got ok=%v retry=%v", ok, retry)
		}
	}
	// 已注册桶仍按 burst 生效（对照）。
	if ok, _ := l.Allow("login", "1.2.3.4"); !ok {
		t.Fatal("registered bucket first attempt must pass")
	}
	if ok, _ := l.Allow("login", "1.2.3.4"); ok {
		t.Fatal("registered bucket must deny after burst")
	}
}

func TestRateLimiter_InvalidSpecsAreNotRegistered(t *testing.T) {
	l := NewRateLimiter(time.Second,
		RateBucketSpec{Name: "", PerMinute: 10, Burst: 5},          // 无名
		RateBucketSpec{Name: "zeroRate", PerMinute: 0, Burst: 5},   // 零速率
		RateBucketSpec{Name: "zeroBurst", PerMinute: 10, Burst: 0}, // 零容量
	)
	for _, bucket := range []string{"", "zeroRate", "zeroBurst"} {
		if ok, _ := l.Allow(bucket, "1.2.3.4"); !ok {
			t.Fatalf("spec %q must not be registered (a zeroed config field must never create a deny-all bucket)", bucket)
		}
	}
	// Register 后再用：合法 spec 生效。
	l.Register(RateBucketSpec{Name: "late", PerMinute: 1, Burst: 1})
	if ok, _ := l.Allow("late", "1.2.3.4"); !ok {
		t.Fatal("late-registered bucket first attempt must pass")
	}
	if ok, _ := l.Allow("late", "1.2.3.4"); ok {
		t.Fatal("late-registered bucket must deny after burst")
	}
}

func TestRateLimiter_JanitorSweepsIdleIPs(t *testing.T) {
	l := NewRateLimiter(time.Second, RateBucketSpec{Name: "login", PerMinute: 60, Burst: 5})
	l.Allow("login", "1.1.1.1")
	l.Allow("login", "2.2.2.2")
	// 把 1.1.1.1 的 seen 推到 idle TTL（10min）之外。
	l.mu.Lock()
	l.buckets["login"]["1.1.1.1"].seen = time.Now().Add(-11 * time.Minute)
	l.mu.Unlock()

	stop := make(chan struct{})
	go l.Janitor(5*time.Millisecond, stop)
	time.Sleep(40 * time.Millisecond)
	close(stop)

	l.mu.Lock()
	_, staleStillThere := l.buckets["login"]["1.1.1.1"]
	_, fresh := l.buckets["login"]["2.2.2.2"]
	l.mu.Unlock()
	if staleStillThere {
		t.Fatal("janitor must sweep IP buckets idle beyond rateLimitIdleTTL")
	}
	if !fresh {
		t.Fatal("janitor must keep recently-seen IP buckets")
	}
}

func TestRateLimiter_ConcurrentAllowIsSafe(t *testing.T) {
	l := NewRateLimiter(time.Second, RateBucketSpec{Name: "ws", PerMinute: 6000, Burst: 100})
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				l.Allow("ws", "3.3.3.3")
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

func TestCeilSeconds(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want int
	}{
		{0, 0},
		{-5 * time.Second, 0},
		{1 * time.Millisecond, 1},
		{1 * time.Second, 1},
		{1500 * time.Millisecond, 2},
		{59*time.Second + 999*time.Millisecond, 60},
	}
	for _, c := range cases {
		if got := CeilSeconds(c.in); got != c.want {
			t.Errorf("CeilSeconds(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
