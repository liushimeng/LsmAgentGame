// Package agent — ratelimit.go: a small token-bucket limiter for agent speech.
//
// We need ≤ 2 speeches/minute ⇒ a minimum 30s interval between speak/whisper
// tool calls. A single-token bucket with 30s refill gives exactly that with a
// single burst allowed at startup (so the first speech is immediate).
package agentcore

import (
	"context"
	"sync"
	"time"
)

// SpeakLimiter enforces a minimum interval between agent speeches. goroutine-safe.
type SpeakLimiter struct {
	mu      sync.Mutex
	interval time.Duration
	last    time.Time
}

// NewSpeakLimiter creates a limiter with the given minimum interval.
func NewSpeakLimiter(interval time.Duration) *SpeakLimiter {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &SpeakLimiter{interval: interval, last: time.Time{}}
}

// SetInterval replaces the minimum interval. goroutine-safe: every read of
// l.interval (Allow / Wait / WaitWithTimeout) already happens under l.mu, so a
// mutating setter needs no extra synchronisation beyond taking the same lock.
//
// 2026-08-14 §20260814-01 U2 — 难度档位「发言节奏」维度的执行器。
// DifficultyProfile.SpeakLimiterScale 自 §20260811-09 落地起就是死字段
// （4 处赋值、0 处生产读取，§130 同一 struct 内第三个同病字段——
// MemoryInjectRunes 由 §20260812-04 U4 修、MaxToolUse 由 §20260813-04 U3 修）。
// 因为 limiter 的 interval 在 NewSpeakLimiter 构造期即固定，而难度档位要到
// StartAgentsLocked 才注入，故必须有这个 setter 才能把 scale 落到实处。
//
// d <= 0 视为「不修改」而非「回退默认 30s」：调用方（SetDifficultySpeakScale）
// 已把 scale<=0 归一化为 1.0，此处再兜一层，避免误把 limiter 打回默认值。
func (l *SpeakLimiter) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.interval = d
}

// Interval returns the current minimum interval (test/observability helper).
func (l *SpeakLimiter) Interval() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.interval
}

// Allow reports whether a speech is permitted right now without consuming a
// token. Useful for the UI to show the agent "is thinking/can speak".
func (l *SpeakLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Since(l.last) >= l.interval
}

// Wait blocks until the next speech is allowed, or ctx is cancelled. Returns
// ctx.Err() if cancelled.
func (l *SpeakLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	deadline := l.last.Add(l.interval)
	l.mu.Unlock()
	d := time.Until(deadline)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Mark records that a speech happened now, resetting the interval.
func (l *SpeakLimiter) Mark() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last = time.Now()
}

// WaitWithTimeout blocks until the next speech is allowed, ctx is cancelled,
// or maxWait elapses — whichever comes first. Returns ctx.Err() on
// cancellation, or a non-nil error on maxWait expiry. A capped wait prevents
// the agent goroutine from blocking indefinitely on a congested limiter if
// the room's ctx is slow to cancel (R1 fix: avoid goroutine leak).
func (l *SpeakLimiter) WaitWithTimeout(ctx context.Context, maxWait time.Duration) error {
	l.mu.Lock()
	deadline := l.last.Add(l.interval)
	l.mu.Unlock()
	d := time.Until(deadline)
	if d <= 0 {
		return nil
	}
	if maxWait > 0 && d > maxWait {
		d = maxWait
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
