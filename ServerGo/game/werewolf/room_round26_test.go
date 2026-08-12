// Package werewolf — room_round26_test.go: regression tests for the
// Round 26 API lock-contention fix and the Round 152 lock-poison fix.
//
// BUG-WEREWOLF-P1-LOCK: /api/games/werewolf/rooms and /api/rooms/{id}
// hung for 20s+ after a bot-exit event because GetPublicState blocked on
// r.mu.Lock() while an engine op (LLM retry / auto-skip dispatch /
// quarantine) held the same lock. The fix introduces a deadline-bounded
// lockRoomBriefly helper plus a per-room PublicState cache so the REST
// path always returns a bounded-latency response.
//
// BUG-R152-LLM-TIMEOUT-001: the original lockRoomBriefly spawned a
// goroutine that called r.mu.Lock(); if the caller timed out, the
// goroutine still grabbed r.mu, sent on the buffered channel (succeeds
// even with no reader), and returned WITHOUT calling r.mu.Unlock() —
// permanently poisoning the room's mutex. After rewriting to TryLock
// polling, a timed-out lockRoomBriefly leaves r.mu fully usable.
package werewolf

import (
	"testing"
	"time"
)

// TestLockRoomBriefly_AcquiresWhenFree: when nobody holds r.mu, the helper
// must return true within the deadline.
func TestLockRoomBriefly_AcquiresWhenFree(t *testing.T) {
	r := &WerewolfRoom{}
	if !lockRoomBriefly(r, 100*time.Millisecond) {
		t.Fatalf("lockRoomBriefly failed on uncontended lock")
	}
	r.mu.Unlock()
}

// TestLockRoomBriefly_TimesOutWhenHeld: when another goroutine holds r.mu
// past the deadline, the helper must return false rather than blocking.
func TestLockRoomBriefly_TimesOutWhenHeld(t *testing.T) {
	r := &WerewolfRoom{}
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Now()
	if lockRoomBriefly(r, 50*time.Millisecond) {
		t.Fatalf("lockRoomBriefly returned true while r.mu was held by us")
	}
	elapsed := time.Since(start)
	if elapsed > 250*time.Millisecond {
		t.Fatalf("lockRoomBriefly blocked %v past deadline (should return within 250ms)", elapsed)
	}
}

// TestLockRoomBriefly_TimeoutDoesNotPoisonMutex: regression test for
// BUG-R152-LLM-TIMEOUT-001. The original lockRoomBriefly implementation
// spawned a goroutine that called r.mu.Lock(); if the caller timed out, that
// goroutine still eventually grabbed r.mu, sent on the buffered channel
// (which succeeds even with no reader), and returned WITHOUT EVER calling
// r.mu.Unlock(). After even a single timed-out call the room's mutex
// became permanently held by a dead goroutine — every subsequent
// r.mu.Lock() blocked forever, including the very phase-watchdog
// goroutine that is supposed to rescue a stalled phase. Once poisoned,
// a 13-bot speak phase could never advance past a single hung LLM call
// (observed Bot 8 Qwen 3.7-Max LLM timeout → room froze at 1295s+).
//
// After the fix (TryLock-polling), a timeout returns false while leaving
// r.mu fully usable: a subsequent lockRoomBriefly(…,true call) must succeed
// and the caller must be able to Unlock cleanly.
func TestLockRoomBriefly_TimeoutDoesNotPoisonMutex(t *testing.T) {
	r := &WerewolfRoom{}

	// Hold r.mu via a synchronised goroutine so the next lockRoomBriefly
	// invocation is guaranteed to contend and time out. Use a rendezvous
	// channel to ensure the goroutine has acquired r.mu before we proceed.
	acquired := make(chan struct{})
	release := make(chan struct{})
	go func() {
		r.mu.Lock()
		close(acquired)
		<-release
		r.mu.Unlock()
	}()
	<-acquired

	// First call: time out. Must not poison r.mu.
	if lockRoomBriefly(r, 30*time.Millisecond) {
		t.Fatalf("lockRoomBriefly returned true while r.mu is held")
	}

	// Pause to ensure the deadline has fully elapsed and any hypothetical
	// goroutine from the old buggy implementation would have already grabbed
	// the lock by now.
	time.Sleep(50 * time.Millisecond)

	// Release the explicit holder; r.mu should be fully free now.
	close(release)
	time.Sleep(10 * time.Millisecond)

	// Second call: must succeed cleanly and leave the mutex in an
	// unlockable state. If the old bug were present, this would deadlock.
	ok := make(chan bool, 1)
	go func() {
		ok <- lockRoomBriefly(r, 200*time.Millisecond)
	}()
	select {
	case got := <-ok:
		if !got {
			t.Fatalf("lockRoomBriefly failed to acquire lock after a prior timeout (mutex likely poisoned)")
		}
		// Must be able to Unlock cleanly.
		r.mu.Unlock()
	case <-time.After(1 * time.Second):
		t.Fatalf("lockRoomBriefly deadlocked after a prior timeout (mutex poisoned)")
	}
}

// TestGetPublicState_LockContended_ReturnsCachedSnapshot: under contention,
// GetPublicState must return the last cached snapshot rather than blocking.
// Seeds the cache via a quick GetPublicState, then holds r.mu and re-queries.
func TestGetPublicState_LockContended_ReturnsCachedSnapshot(t *testing.T) {
	m := stubWWMgr()
	roomID, r := fillAndStart(t, m)
	if r == nil || r.State == nil {
		t.Fatalf("fillAndStart returned nil State")
	}
	// Seed cache by calling once with no contention.
	got, ok := m.GetPublicState(roomID)
	if !ok {
		t.Fatalf("GetPublicState first call failed")
	}
	if got.Phase == "" {
		t.Fatalf("expected phase set in seeded snapshot, got empty")
	}
	// Now hold r.mu past the deadline and re-query.
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Now()
	got2, ok := m.GetPublicState(roomID)
	elapsed := time.Since(start)
	if !ok {
		t.Fatalf("GetPublicState under contention returned ok=false")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("GetPublicState under contention blocked %v (must be bounded)", elapsed)
	}
	if got2.Phase != got.Phase {
		t.Fatalf("expected cached phase %q, got %q", got.Phase, got2.Phase)
	}
}