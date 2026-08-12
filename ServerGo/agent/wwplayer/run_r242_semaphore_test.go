package wwplayer_test

import (
	"sync"
	"testing"
	"time"

	"LsmAgentGame/agent/wwplayer"
)

// BUG-R242-P1-01: 房间级 LLM 并发信号量回归测试。
// §130 曾删除信号量导致 13 bot × 内层重试 fully concurrent 打满上游代理,
// 级联失败(实测 27min 66% 失败率)。修复恢复了 SetLLMSemaphore / AcquireLLMSlot /
// ReleaseLLMSlot 的接线。以下不变式防止再次静默失效。

// TestR242_NilSemaphore_AlwaysAcquire 验证未接线时(llmSema == nil,§130 行为)
// AcquireLLMSlot 恒返回 true,不阻塞。
func TestR242_NilSemaphore_AlwaysAcquire(t *testing.T) {
	a := &wwplayer.Agent{}
	for i := 0; i < 20; i++ {
		if !a.AcquireLLMSlot(time.Second) {
			t.Fatalf("expected acquire==true when semaphore is nil (iteration %d)", i)
		}
		// Release 在无信号量时应为 no-op,不应 panic。
		a.ReleaseLLMSlot()
	}
}

// TestR242_Saturated_NonBlocking 验证槽位满时非阻塞 try(wait=0)立即返回 false,
// 不阻塞调用方 goroutine。
func TestR242_Saturated_NonBlocking(t *testing.T) {
	sema := make(chan struct{}, 2)
	a := &wwplayer.Agent{}
	a.SetLLMSemaphore(sema)

	// 占满 2 个槽位。
	if !a.AcquireLLMSlot(time.Second) {
		t.Fatal("expected first acquire to succeed")
	}
	if !a.AcquireLLMSlot(time.Second) {
		t.Fatal("expected second acquire to succeed")
	}

	done := make(chan bool, 1)
	start := time.Now()
	go func() {
		got := a.AcquireLLMSlot(0) // 非阻塞 try
		done <- got
	}()
	select {
	case got := <-done:
		if got {
			t.Fatal("expected acquire==false when all slots are held (non-blocking try)")
		}
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("non-blocking try blocked for %v (should return immediately)", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("non-blocking try did not return promptly when saturated")
	}

	// 释放一个槽位后应能获取。
	a.ReleaseLLMSlot()
	if !a.AcquireLLMSlot(time.Second) {
		t.Fatal("expected acquire==true after a slot was released")
	}
	a.ReleaseLLMSlot()
	a.ReleaseLLMSlot()
}

// TestR242_BoundedWait_UnblocksOnRelease 验证有界等待在槽位释放后成功获取(而非
// 永久阻塞或立即放弃),这是"快模型不被慢模型无限阻塞"的关键不变式。
func TestR242_BoundedWait_UnblocksOnRelease(t *testing.T) {
	sema := make(chan struct{}, 1)
	a := &wwplayer.Agent{}
	a.SetLLMSemaphore(sema)

	if !a.AcquireLLMSlot(time.Second) {
		t.Fatal("expected first acquire to succeed")
	}

	acquired := make(chan bool, 1)
	go func() {
		acquired <- a.AcquireLLMSlot(2 * time.Second)
	}()

	// 等待一小段时间确认调用方确实在等待(而非立即返回 false)。
	time.Sleep(100 * time.Millisecond)
	a.ReleaseLLMSlot()

	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("expected acquire==true after slot was released within bounded wait")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire did not unblock after slot was released")
	}
	a.ReleaseLLMSlot()
}

// TestR242_ConcurrentAcquire_RespectsCapacity 验证并发获取不超过信号量容量,
// 即房间在途 LLM 调用数被严格限制在 cap 以内。
func TestR242_ConcurrentAcquire_RespectsCapacity(t *testing.T) {
	const cap = 4
	sema := make(chan struct{}, cap)
	a := &wwplayer.Agent{}
	a.SetLLMSemaphore(sema)

	var (
		mu        sync.Mutex
		inFlight  int
		peak      int
		wg        sync.WaitGroup
		iterations = 40
	)
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			if !a.AcquireLLMSlot(2 * time.Second) {
				return // 槽位满时放弃,符合预期
			}
			defer a.ReleaseLLMSlot()
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()
			// 模拟一次 LLM 调用(慢模型 50-200ms)。
			time.Sleep(50 + time.Duration(i%4)*50*time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if peak > cap {
		t.Fatalf("in-flight concurrency %d exceeded semaphore capacity %d", peak, cap)
	}
	if peak < 1 {
		t.Fatal("expected at least one concurrent acquisition")
	}
	t.Logf("observed peak in-flight = %d (capacity = %d)", peak, cap)
}
