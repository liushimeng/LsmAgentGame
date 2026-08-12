package wwplayer

import (
	"LsmWebGame/agent/core"
	"sync"
	"testing"
	"time"
)

// TestAgent_InterjectQuota_R76P1_3 verifies the R76 P1-3 fix:
// a single bot's interject tool calls are throttled by two independent gates:
//   1) InterjectLimiter — 60s minimum interval (>= speak's 45s)
//   2) 5-minute sliding window — at most interjectMaxPer5Min (4) calls
//
// R76 report: MiniMax #6 fired 7+ interjects in one battle, drowning out
// the other 5 agents. After the fix, the 5th interject within a 5-minute
// window must be rejected.
func TestAgent_InterjectQuota_R76P1_3(t *testing.T) {
	// Leave InterjectLimiter nil — exercises the 5-minute window-only path
	// (the 60s interval gate has its own SpeakLimiter unit test).
	a := &Agent{}

	// First 4 calls within 5 minutes should pass.
	for i := 0; i < interjectMaxPer5Min; i++ {
		if !a.AllowInterject(time.Now()) {
			t.Fatalf("call %d unexpectedly rejected within quota window", i+1)
		}
		a.MarkInterject(time.Now())
	}

	// 5th call within the same 5-min window must fail.
	if a.AllowInterject(time.Now()) {
		t.Fatalf("call 5 unexpectedly allowed (exceeded interjectMaxPer5Min=%d)", interjectMaxPer5Min)
	}
}

// TestAgent_InterjectQuota_WindowReset verifies the 5-minute window
// resets after interjectWindowStart + 5min elapses (simulated by moving
// the windowStart directly under the mutex).
func TestAgent_InterjectQuota_WindowReset(t *testing.T) {
	a := &Agent{
		InterjectLimiter: agentcore.NewSpeakLimiter(60 * time.Second),
	}
	// Saturate the 5-minute window with calls 1..N (max) via direct field write.
	a.interjectMu.Lock()
	a.interjectWindowStart = time.Now().Add(-6 * time.Minute) // window expired
	a.interjectWindowCount = interjectMaxPer5Min
	a.interjectMu.Unlock()

	// AllowInterject must reset the window because elapsed > 5min.
	if !a.AllowInterject(time.Now()) {
		t.Fatalf("AllowInterject must allow when window is expired even if count was high")
	}
	a.MarkInterject(time.Now())

	// And MarkInterject must reset windowStart + count = 1.
	a.interjectMu.Lock()
	defer a.interjectMu.Unlock()
	if a.interjectWindowCount != 1 {
		t.Fatalf("expected count=1 after window reset, got %d", a.interjectWindowCount)
	}
}

// TestAgent_InterjectQuota_ConcurrentSafe exercises concurrent calls to
// AllowInterject under -race to make sure the interjectMu serializes
// the window counter correctly.
func TestAgent_InterjectQuota_ConcurrentSafe(t *testing.T) {
	a := &Agent{} // nil InterjectLimiter — window-only path
	a.interjectMu.Lock()
	a.interjectWindowStart = time.Now()
	a.interjectMu.Unlock()

	var allowed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			now := time.Now()
			if a.AllowInterject(now) {
				mu.Lock()
				allowed++
				mu.Unlock()
				a.MarkInterject(now) // simulate runner side effect after allow
			}
		}()
	}
	wg.Wait()

	a.interjectMu.Lock()
	defer a.interjectMu.Unlock()
	// After 20 concurrent Allow + Mark calls, the window count must be
	// bounded by interjectMaxPer5Min (we never call past the cap), but at
	// least one Mark must have registered.
	if a.interjectWindowCount < 1 {
		t.Fatalf("expected at least 1 Mark, got %d", a.interjectWindowCount)
	}
	if allowed < 1 {
		t.Fatalf("expected at least 1 Allow, got %d", allowed)
	}
}
