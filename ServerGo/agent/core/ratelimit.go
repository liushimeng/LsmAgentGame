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
