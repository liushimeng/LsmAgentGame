// Package vcplayer — rate_test.go: LLM 令牌桶单测(2026-09-26 §批次25 §3.3)。
package vcplayer

import (
	"sync"
	"testing"
	"time"
)

// TestTokenBucket_CapacityAndInterval 容量 2、每 interval 补 1 枚:连续 3 次
// 取令牌第 3 次被拒;时钟前进一个 interval 后恢复 1 枚;补满不超过容量。
func TestTokenBucket_CapacityAndInterval(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	b := NewTokenBucketWithClock(30*time.Second, clock)
	if b.capacity != 2 {
		t.Fatalf("capacity = %d, want 2", b.capacity)
	}
	if !b.Allow() {
		t.Fatal("first Allow must succeed (满桶)")
	}
	if !b.Allow() {
		t.Fatal("second Allow must succeed (容量 2)")
	}
	if b.Allow() {
		t.Fatal("third Allow must be rejected (桶空)")
	}
	if b.Allow() {
		t.Fatal("fourth Allow must still be rejected (未补充)")
	}
	advance(30 * time.Second) // 恰好补 1 枚
	if !b.Allow() {
		t.Fatal("after 1 interval Allow must succeed (补充 1 枚)")
	}
	if b.Allow() {
		t.Fatal("only 1 token refilled after 1 interval")
	}
	advance(5 * time.Minute) // 远超容量,补满为止
	if !b.Allow() || !b.Allow() {
		t.Fatal("long idle must refill to full capacity 2")
	}
	if b.Allow() {
		t.Fatal("refill must cap at capacity 2")
	}
}

// TestTokenBucket_DefaultInterval 缺省/非正间隔回落 30s。
func TestTokenBucket_DefaultInterval(t *testing.T) {
	b := NewTokenBucket(0)
	if b.interval != defaultLLMMinInterval {
		t.Fatalf("interval = %v, want %v", b.interval, defaultLLMMinInterval)
	}
	if b.interval != 30*time.Second {
		t.Fatalf("defaultLLMMinInterval = %v, want 30s", b.interval)
	}
}

// TestTokenBucket_NilSafe nil 桶放行(未装配限速的测试夹具)。
func TestTokenBucket_NilSafe(t *testing.T) {
	var b *TokenBucket
	if !b.Allow() {
		t.Fatal("nil bucket must allow")
	}
}
