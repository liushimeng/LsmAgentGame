// Package util — per-IP token buckets (20260923-01 §4.3).
//
// RateLimiter is a process-local, concurrency-safe collection of named
// token buckets keyed by client IP. Known buckets (registered in main.go):
//
//   - "captcha" — /api/captcha issuance flood protection (per_ip_per_minute)
//   - "login"   — /api/auth/* burst protection (10/min, burst 5); complements
//     the sliding-window lockout in LoginGuard (which is the slow lane)
//   - "ws"      — WSS upgrade attempts incl. bad tokens (ws_guard)
//
// Design notes:
//   - ip == "" always passes (in-proc callers / unit-test short-circuit).
//   - Unknown bucket names pass (fail-open) — registration is explicit.
//   - Per-bucket IP map is capped (rateLimitMaxIPsPerBucket); on overflow we
//     lazily sweep long-idle buckets, then evict the least-recently-seen.
//   - Janitor() mirrors CaptchaStore.Janitor for periodic idle sweeping.
package util

import (
	"sync"
	"time"
)

// rateLimitMaxIPsPerBucket bounds memory (≈ 50k tracked IPs per bucket).
const rateLimitMaxIPsPerBucket = 50000

// rateLimitIdleTTL is how long an untouched IP bucket survives before the
// janitor / lazy sweep may reclaim it. Comfortably above any burst window.
const rateLimitIdleTTL = 10 * time.Minute

// RateBucketSpec registers one named bucket with the limiter.
type RateBucketSpec struct {
	Name      string // bucket name used in Allow()
	PerMinute int    // steady-state refill rate (tokens per minute)
	Burst     int    // bucket capacity (instantaneous allowance)
}

// ipTokenBucket is one IP's token state inside a named bucket.
type ipTokenBucket struct {
	tokens float64
	last   time.Time // last refill moment
	seen   time.Time // last Allow() touch — drives idle sweeping
}

// RateLimiter hosts the named per-IP token buckets.
type RateLimiter struct {
	// refillQuantum is security.captcha_guard.burst_refill_seconds expressed
	// as a duration: the 1/rate drip granularity. Reported retry-after values
	// are rounded up to at least one quantum so clients get a stable hint.
	refillQuantum time.Duration

	mu      sync.Mutex
	specs   map[string]RateBucketSpec
	buckets map[string]map[string]*ipTokenBucket // bucket name → ip → state
}

// NewRateLimiter builds a limiter. refillQuantum <= 0 disables retry-after
// rounding. Specs with PerMinute/Burst <= 0 are skipped (registration is
// config-driven; a zeroed config field must not create a deny-all bucket).
func NewRateLimiter(refillQuantum time.Duration, specs ...RateBucketSpec) *RateLimiter {
	l := &RateLimiter{
		refillQuantum: refillQuantum,
		specs:         make(map[string]RateBucketSpec),
		buckets:       make(map[string]map[string]*ipTokenBucket),
	}
	for _, s := range specs {
		l.Register(s)
	}
	return l
}

// Register (re)defines a named bucket. Re-registering keeps existing IP
// state — token counts are clamped against the new capacity on next use.
func (l *RateLimiter) Register(spec RateBucketSpec) {
	if spec.Name == "" || spec.PerMinute <= 0 || spec.Burst <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.specs[spec.Name] = spec
	if l.buckets[spec.Name] == nil {
		l.buckets[spec.Name] = make(map[string]*ipTokenBucket)
	}
}

// Allow consumes one token for ip in the named bucket. When the bucket is
// empty it returns (false, retryAfter) where retryAfter is the time until a
// token is available (rounded up to a second, at least one refill quantum).
//
// Always passes when: ip == "", the bucket name is unknown, or the limiter
// has no capacity pressure — the limiter is a burst guard, not an ACL.
func (l *RateLimiter) Allow(bucket, ip string) (bool, time.Duration) {
	if ip == "" {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	spec, ok := l.specs[bucket]
	if !ok {
		return true, 0
	}
	m := l.buckets[bucket]
	b := m[ip]
	if b == nil {
		if len(m) >= rateLimitMaxIPsPerBucket {
			l.sweepLocked(m, now)
			if len(m) >= rateLimitMaxIPsPerBucket {
				// Still full: evict the least-recently-seen entry so fresh
				// users are never denied because of stale map growth.
				if oldest := l.oldestLocked(m); oldest != "" {
					delete(m, oldest)
				}
			}
		}
		b = &ipTokenBucket{tokens: float64(spec.Burst), last: now, seen: now}
		m[ip] = b
	}
	// Refill at PerMinute/60 tokens per second, clamped to capacity.
	elapsed := now.Sub(b.last)
	if elapsed > 0 {
		b.tokens += elapsed.Seconds() * float64(spec.PerMinute) / 60.0
		if b.tokens > float64(spec.Burst) {
			b.tokens = float64(spec.Burst)
		}
		b.last = now
	}
	b.seen = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	deficit := 1 - b.tokens
	wait := time.Duration(deficit / (float64(spec.PerMinute) / 60.0) * float64(time.Second))
	if l.refillQuantum > 0 && wait < l.refillQuantum {
		wait = l.refillQuantum
	}
	return false, ceilSecond(wait)
}

// Janitor periodically sweeps idle IP buckets out of every named bucket.
// stop receives a struct{} to terminate, same convention as the captcha
// janitor.
func (l *RateLimiter) Janitor(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			l.mu.Lock()
			for _, m := range l.buckets {
				l.sweepLocked(m, now)
			}
			l.mu.Unlock()
		}
	}
}

// sweepLocked drops buckets idle for longer than rateLimitIdleTTL.
// l.mu must be held.
func (l *RateLimiter) sweepLocked(m map[string]*ipTokenBucket, now time.Time) {
	for ip, b := range m {
		if now.Sub(b.seen) > rateLimitIdleTTL {
			delete(m, ip)
		}
	}
}

// oldestLocked returns the least-recently-seen IP in m ("" when empty).
func (l *RateLimiter) oldestLocked(m map[string]*ipTokenBucket) string {
	var oldestIP string
	var oldest time.Time
	for ip, b := range m {
		if oldestIP == "" || b.seen.Before(oldest) {
			oldestIP, oldest = ip, b.seen
		}
	}
	return oldestIP
}

// CeilSeconds rounds d up to a whole second (0 stays 0). Exported for the
// HTTP layer's Retry-After headers (20260923-01 §4.5).
func CeilSeconds(d time.Duration) int {
	d = ceilSecond(d)
	return int(d / time.Second)
}

// ceilSecond rounds d up to a whole second (minimum 1s when d > 0).
func ceilSecond(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	s := d / time.Second
	if d%time.Second != 0 {
		s++
	}
	return s * time.Second
}
