// login_guard_test.go — 20260923-01 §4.2 / §9 单测。
//
// 覆盖：阈值触发锁定、锁定档位 ×4 递增与 86400s 封顶、Reset、Janitor 清扫、
// 零值配置默认值、空键不可键（单测/进程内调用短路的关键前提）、
// 账户键与 IP 键各自的阈值、键推导的大小写与 phone 优先规则。
//
// 全部断言都不依赖真实 sleep（档位与清扫通过直接读写包内字段验证），
// 因此本文件在 CI 上耗时 < 100ms。
package util

import (
	"testing"
	"time"

	"LsmAgentGame/config"
)

// guardEntry 是测试专用的小工具：取包内条目（同包可见）。
func guardEntry(g *LoginGuard, key string) *loginGuardEntry {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.entries[key]
}

func TestLoginGuard_KeysAreNotKeyableWhenEmpty(t *testing.T) {
	if k := LoginGuardKeyAccount("", ""); k != "" {
		t.Fatalf("empty account/phone must not be keyable, got %q", k)
	}
	if k := LoginGuardKeyAccount("   ", ""); k != "" {
		t.Fatalf("blank account must not be keyable, got %q", k)
	}
	if k := LoginGuardKeyIP(""); k != "" {
		t.Fatalf("empty ip must not be keyable, got %q", k)
	}

	g := NewLoginGuard(config.LoginGuardConfig{})
	// 空键的全部操作都是 no-op，且绝不建条目（否则匿名请求会共享一个全局桶）。
	if n := g.RecordFailure(""); n != 0 {
		t.Fatalf("RecordFailure(\"\") = %d, want 0", n)
	}
	if d := g.Remaining(""); d != 0 {
		t.Fatalf("Remaining(\"\") = %v, want 0", d)
	}
	g.Reset("") // must not panic
	g.mu.Lock()
	n := len(g.entries)
	g.mu.Unlock()
	if n != 0 {
		t.Fatalf("empty key created %d entries, want 0", n)
	}
}

func TestLoginGuard_KeyDerivation(t *testing.T) {
	// 账户键：大小写不敏感。
	if LoginGuardKeyAccount("Alice", "") != LoginGuardKeyAccount("alice", "") {
		t.Fatal("account key must be case-insensitive")
	}
	// phone 非空时优先（与 AuthService 的用户查找顺序一致）。
	if LoginGuardKeyAccount("alice", "13800000000") == LoginGuardKeyAccount("alice", "") {
		t.Fatal("phone must take precedence over account in the key")
	}
	if LoginGuardKeyAccount("alice", "13800000000") != LoginGuardKeyAccount("bob", "13800000000") {
		t.Fatal("same phone must produce the same key")
	}
	// 账户键与 IP 键命名空间必须互不碰撞（前缀 a: / i:）。
	if LoginGuardKeyAccount("1.2.3.4", "") == LoginGuardKeyIP("1.2.3.4") {
		t.Fatal("account key and ip key must not collide")
	}
}

func TestLoginGuard_ZeroConfigUsesDefaults(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{}) // 全零值
	if g.window != 900*time.Second {
		t.Errorf("window = %v, want 900s", g.window)
	}
	if g.acctMax != 5 {
		t.Errorf("acctMax = %d, want 5", g.acctMax)
	}
	if g.ipMax != 50 {
		t.Errorf("ipMax = %d, want 50", g.ipMax)
	}
	if g.lockBase != 60*time.Second {
		t.Errorf("lockBase = %v, want 60s", g.lockBase)
	}
	if g.escalMemory != maxLockSeconds*time.Second {
		t.Errorf("escalMemory = %v, want %ds", g.escalMemory, maxLockSeconds)
	}

	// 行为验证：默认阈值 5 —— 第 4 次不锁，第 5 次锁 ~60s。
	key := LoginGuardKeyAccount("zeroconf", "")
	for i := 1; i <= 4; i++ {
		if n := g.RecordFailure(key); n != i {
			t.Fatalf("failure #%d returned count %d", i, n)
		}
		if d := g.Remaining(key); d != 0 {
			t.Fatalf("locked after %d failures (< threshold 5), remaining %v", i, d)
		}
	}
	if n := g.RecordFailure(key); n != 5 {
		t.Fatalf("failure #5 returned count %d, want 5", n)
	}
	d := g.Remaining(key)
	if d <= 0 || d > 60*time.Second {
		t.Fatalf("expected a ~60s lockout after threshold, got %v", d)
	}
}

func TestLoginGuard_AccountAndIPThresholdsAreSeparate(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{
		WindowSeconds: 60,
		MaxFailures:   2,
		IPMaxFailures: 4,
		LockSeconds:   1,
		MemorySeconds: 60,
	})
	acct := LoginGuardKeyAccount("thresholds", "")
	ip := LoginGuardKeyIP("10.0.0.1")

	// IP 键阈值 4：前 3 次不锁（账户键阈值 2 与它无关）。
	for i := 0; i < 3; i++ {
		g.RecordFailure(ip)
	}
	if d := g.Remaining(ip); d != 0 {
		t.Fatalf("ip key locked before its own threshold, remaining %v", d)
	}
	// 账户键阈值 2。
	g.RecordFailure(acct)
	if d := g.Remaining(acct); d != 0 {
		t.Fatalf("account key locked after 1 failure, remaining %v", d)
	}
	g.RecordFailure(acct)
	if d := g.Remaining(acct); d <= 0 {
		t.Fatal("account key must be locked at its threshold (2)")
	}
	g.RecordFailure(ip)
	if d := g.Remaining(ip); d <= 0 {
		t.Fatal("ip key must be locked at its threshold (4)")
	}
}

func TestLoginGuard_EscalatingLockDuration(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{
		WindowSeconds: 60,
		MaxFailures:   1, // 每次失败立即触发锁定，便于观察档位
		IPMaxFailures: 50,
		LockSeconds:   60,
		MemorySeconds: 3600,
	})
	// ×4 递增（60s 基数）：档位 n → 60×4^n 秒。
	// n=0..5 → 60s / 4m / 16m / 64m / 4h16m / 17h4m，n=5 仍未触顶。
	want := []time.Duration{
		60 * time.Second,    // n=0
		240 * time.Second,   // n=1
		960 * time.Second,   // n=2
		3840 * time.Second,  // n=3
		15360 * time.Second, // n=4  (4h16m)
		61440 * time.Second, // n=5  (17h4m)
	}
	for i, w := range want {
		if got := g.lockDuration(i); got != w {
			t.Errorf("lockDuration(%d) = %v, want %v", i, got, w)
		}
	}
	// 封顶 86400s（24h）：60×4^6=245760s 越界 ⇒ 档位 6 起一律钳到上限。
	for _, n := range []int{6, 8, 20, 1000} {
		if got := g.lockDuration(n); got != maxLockSeconds*time.Second {
			t.Errorf("lockDuration(%d) = %v, want cap %ds", n, got, maxLockSeconds)
		}
	}

	// 实际锁定走同一档位：第 1 次触发 60s（escalations 0），第 2 次 240s。
	key := LoginGuardKeyAccount("escalate", "")
	g.RecordFailure(key)
	e := guardEntry(g, key)
	if e == nil {
		t.Fatal("entry missing after first failure")
	}
	if e.escalations != 1 {
		t.Errorf("escalations = %d, want 1", e.escalations)
	}
	if d := time.Until(e.lockedUntil); d <= 55*time.Second || d > 60*time.Second {
		t.Errorf("first lockout = %v, want ~60s", d)
	}
	g.RecordFailure(key)
	if e.escalations != 2 {
		t.Errorf("escalations = %d, want 2", e.escalations)
	}
	if d := time.Until(e.lockedUntil); d <= 230*time.Second || d > 240*time.Second {
		t.Errorf("second lockout = %v, want ~240s (×4)", d)
	}
}

func TestLoginGuard_SlidingWindowPrunesOldFailures(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{
		WindowSeconds: 60,
		MaxFailures:   3,
		IPMaxFailures: 50,
		LockSeconds:   60,
		MemorySeconds: 3600,
	})
	key := LoginGuardKeyAccount("window", "")
	g.RecordFailure(key)
	g.RecordFailure(key)
	// 把两次失败推到窗口之外（61s 前）→ 下一次失败应重新从 1 计数。
	e := guardEntry(g, key)
	old := time.Now().Add(-61 * time.Second)
	g.mu.Lock()
	for i := range e.failures {
		e.failures[i] = old
	}
	g.mu.Unlock()

	if n := g.RecordFailure(key); n != 1 {
		t.Fatalf("count after window prune = %d, want 1", n)
	}
	if d := g.Remaining(key); d != 0 {
		t.Fatalf("must not be locked, remaining %v", d)
	}
}

func TestLoginGuard_ResetClearsLockAndFailures(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{
		WindowSeconds: 60,
		MaxFailures:   2,
		IPMaxFailures: 50,
		LockSeconds:   60,
		MemorySeconds: 3600,
	})
	acct := LoginGuardKeyAccount("reset", "")
	ip := LoginGuardKeyIP("10.9.9.9")
	g.RecordFailure(acct)
	g.RecordFailure(acct) // → locked
	g.RecordFailure(ip)
	if g.Remaining(acct) <= 0 {
		t.Fatal("precondition: account key should be locked")
	}

	g.Reset(acct)
	g.Reset(ip)
	if d := g.Remaining(acct); d != 0 {
		t.Fatalf("Remaining after Reset = %v, want 0", d)
	}
	if guardEntry(g, acct) != nil || guardEntry(g, ip) != nil {
		t.Fatal("Reset must drop the entries entirely (escalation tier included)")
	}
	// Reset 之后重新计数从 1 开始，且档位回到基础值。
	if n := g.RecordFailure(acct); n != 1 {
		t.Fatalf("count after Reset = %d, want 1", n)
	}
	if got := g.lockDuration(guardEntry(g, acct).escalations); got != 60*time.Second {
		t.Fatalf("lock tier after Reset = %v, want base 60s", got)
	}
}

func TestLoginGuard_JanitorPurgesIdleEntries(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{
		WindowSeconds: 60,
		MaxFailures:   5,
		IPMaxFailures: 50,
		LockSeconds:   60,
		MemorySeconds: 1, // 1s 记忆窗 → 陈旧条目立刻可清扫
	})
	stale := LoginGuardKeyAccount("stale", "")
	fresh := LoginGuardKeyAccount("fresh", "")
	g.RecordFailure(stale)
	g.RecordFailure(fresh)
	// stale 的 lastEvent 推到 2h 前（远超 memory_seconds=1s）且未锁定。
	g.mu.Lock()
	g.entries[stale].lastEvent = time.Now().Add(-2 * time.Hour)
	g.mu.Unlock()

	stop := make(chan struct{})
	go g.Janitor(5*time.Millisecond, stop)
	time.Sleep(60 * time.Millisecond)
	close(stop)

	g.mu.Lock()
	_, staleStillThere := g.entries[stale]
	_, freshKept := g.entries[fresh]
	g.mu.Unlock()
	if staleStillThere {
		t.Fatal("janitor must purge entries idle beyond memory_seconds")
	}
	if !freshKept {
		t.Fatal("janitor must keep recently-touched entries")
	}
}

func TestLoginGuard_JanitorKeepsLockedEntries(t *testing.T) {
	g := NewLoginGuard(config.LoginGuardConfig{
		WindowSeconds: 60,
		MaxFailures:   1,
		IPMaxFailures: 50,
		LockSeconds:   60,
		MemorySeconds: 1,
	})
	key := LoginGuardKeyAccount("locked-keep", "")
	g.RecordFailure(key) // 立即锁定 60s
	// lastEvent 陈旧，但 lockedUntil 仍在未来 → 不得清扫（否则锁定失效）。
	g.mu.Lock()
	g.entries[key].lastEvent = time.Now().Add(-2 * time.Hour)
	g.mu.Unlock()

	stop := make(chan struct{})
	go g.Janitor(5*time.Millisecond, stop)
	time.Sleep(40 * time.Millisecond)
	close(stop)

	if d := g.Remaining(key); d <= 0 {
		t.Fatalf("locked entry was purged by the janitor (remaining %v)", d)
	}
}
