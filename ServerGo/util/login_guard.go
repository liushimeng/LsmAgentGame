// Package util — login guard (20260923-01 §4.2).
//
// Process-local, concurrency-safe sliding-window failure counter with
// escalating lockouts for the auth endpoints. Deliberately has no external
// dependency (no Redis, no DB) — same style as CaptchaStore.
//
// Two dimensions are tracked under opaque keys produced by
// LoginGuardKeyAccount / LoginGuardKeyIP:
//
//   - account key  → threshold cfg.MaxFailures   (credential failures only)
//   - ip key       → threshold cfg.IPMaxFailures (credential + captcha
//     failures; whether captcha misses count is decided by the caller)
//
// Crossing a threshold locks the key for LockSeconds × 4^(escalations),
// capped at 86400s. The escalation tier survives MemorySeconds so repeated
// abuse keeps escalating even across window resets. Reset (on successful
// login / register) drops the entry entirely.
//
// Keys embed a type prefix ("a:" / "i:") so a single method set
// (Remaining / RecordFailure / Reset) can pick the right threshold without
// leaking the concept of "kind" into the call sites. The digests themselves
// are sha256 over the lower-cased identity / IP as specified in the plan doc.
package util

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"LsmAgentGame/config"
)

// LoginGuard key type prefixes. See package doc for why they exist.
const (
	loginGuardAcctPrefix = "a:"
	loginGuardIPPrefix   = "i:"
)

// maxLockSeconds caps a single lockout at 24h (plan doc §4.2).
const maxLockSeconds = 86400

// LoginGuard is the concurrency-safe in-memory lockout registry.
type LoginGuard struct {
	window      time.Duration
	acctMax     int
	ipMax       int
	lockBase    time.Duration
	escalMemory time.Duration

	mu      sync.Mutex
	entries map[string]*loginGuardEntry
}

// loginGuardEntry is one tracked key: recent failure timestamps (sliding
// window), the lockout deadline, and the escalation tier bookkeeping.
type loginGuardEntry struct {
	failures    []time.Time // sorted ascending, within the window
	lockedUntil time.Time
	escalations int       // number of lockouts already triggered (tier)
	lastEvent   time.Time // any touch — drives escalation decay + janitor
}

// NewLoginGuard builds a guard from config. Zero/blank fields fall back to
// the same defaults applyDefaults uses for security.login_guard, so callers
// may safely pass a config.LoginGuardConfig{} straight from a bare struct.
func NewLoginGuard(cfg config.LoginGuardConfig) *LoginGuard {
	if cfg.WindowSeconds <= 0 {
		cfg.WindowSeconds = 900
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.IPMaxFailures <= 0 {
		cfg.IPMaxFailures = 50
	}
	if cfg.LockSeconds <= 0 {
		cfg.LockSeconds = 60
	}
	if cfg.MemorySeconds <= 0 {
		cfg.MemorySeconds = maxLockSeconds
	}
	return &LoginGuard{
		window:      time.Duration(cfg.WindowSeconds) * time.Second,
		acctMax:     cfg.MaxFailures,
		ipMax:       cfg.IPMaxFailures,
		lockBase:    time.Duration(cfg.LockSeconds) * time.Second,
		escalMemory: time.Duration(cfg.MemorySeconds) * time.Second,
		entries:     make(map[string]*loginGuardEntry),
	}
}

// LoginGuardKeyAccount derives the account-dimension key.
//
// The identity is Phone when non-empty (it wins the user lookup), otherwise
// Account, lower-cased; digested with sha256 under the account type prefix.
// Empty identity → "" (not keyable) so anonymous-validation paths never
// share one global bucket.
func LoginGuardKeyAccount(account, phone string) string {
	ident := strings.TrimSpace(account)
	if p := strings.TrimSpace(phone); p != "" {
		ident = p
	}
	if ident == "" {
		return ""
	}
	return loginGuardAcctPrefix + digestKey(strings.ToLower(ident))
}

// LoginGuardKeyIP derives the IP-dimension key. ip == "" returns "" (not
// keyable) — this is the short-circuit that keeps unit tests and in-proc
// callers (bots / CLI logins without an HTTP peer) unaffected.
func LoginGuardKeyIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return loginGuardIPPrefix + digestKey(ip)
}

func digestKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// threshold maps a prefixed key to its configured limit. Unknown prefixes
// are treated as account keys (the stricter bucket).
func (g *LoginGuard) threshold(key string) int {
	if strings.HasPrefix(key, loginGuardIPPrefix) {
		return g.ipMax
	}
	return g.acctMax
}

// Remaining returns the lockout time left for key (0 when not locked or when
// key is empty).
func (g *LoginGuard) Remaining(key string) time.Duration {
	if key == "" {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.entries[key]
	if !ok {
		return 0
	}
	if d := time.Until(e.lockedUntil); d > 0 {
		return d
	}
	return 0
}

// RecordFailure appends a failure timestamp to the sliding window and, when
// the threshold is crossed, arms the next escalating lockout. It returns the
// post-increment failure count within the window (callers use it for the
// humanisation delay; a lock-triggering attempt returns the threshold).
func (g *LoginGuard) RecordFailure(key string) int {
	if key == "" {
		return 0
	}
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.entries[key]
	if e == nil {
		e = &loginGuardEntry{}
		g.entries[key] = e
	}
	// Escalation tier decays back to base after memory_seconds of silence.
	if now.Sub(e.lastEvent) > g.escalMemory {
		e.escalations = 0
	}
	// Prune failures that fell out of the sliding window.
	cutoff := now.Add(-g.window)
	idx := 0
	for idx < len(e.failures) && e.failures[idx].Before(cutoff) {
		idx++
	}
	if idx > 0 {
		e.failures = append(e.failures[:0], e.failures[idx:]...)
	}
	e.failures = append(e.failures, now)
	e.lastEvent = now
	count := len(e.failures)

	if count >= g.threshold(key) {
		e.lockedUntil = now.Add(g.lockDuration(e.escalations))
		e.escalations++
		e.failures = nil // window restarts after the lock is armed
	}
	return count
}

// lockDuration = LockSeconds × 4^n, capped at 86400s.
func (g *LoginGuard) lockDuration(escalations int) time.Duration {
	d := g.lockBase
	for i := 0; i < escalations && d < maxLockSeconds*time.Second; i++ {
		d *= 4
	}
	if d > maxLockSeconds*time.Second {
		d = maxLockSeconds * time.Second
	}
	return d
}

// Reset drops all state for key. Called on successful login (account + IP
// keys) and on successful registration.
func (g *LoginGuard) Reset(key string) {
	if key == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entries, key)
}

// Janitor periodically purges entries that have been fully idle for longer
// than the escalation memory window and are no longer locked (nothing left
// worth remembering). Same stop-channel convention as CaptchaStore.Janitor.
func (g *LoginGuard) Janitor(interval time.Duration, stop <-chan struct{}) {
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
			g.mu.Lock()
			for k, e := range g.entries {
				if now.Sub(e.lastEvent) > g.escalMemory && !now.Before(e.lockedUntil) {
					delete(g.entries, k)
				}
			}
			g.mu.Unlock()
		}
	}
}
