package wwplayer

import (
	"testing"
	"time"
)

// §20260812-04 U5 (P0-6) 回归测试 —— 房间级 LLM 信号量不得跨内层轮次累积。
//
// 缺陷:`defer a.ReleaseLLMSlot()` 写在 handleEvent 的 for 循环体内。Go 的 defer
// 是函数级不是块级,于是 N 轮内层循环 = acquire N 次、直到 handleEvent 返回才
// 一次性释放 N 次。cap=4 的房间信号量会被单个 bot 一次 wake 吃满,其余 bot
// 全部 AcquireLLMSlot 超时 → reWake。
//
// 这里直接对信号量原语建模「一轮 acquire、轮末 release」的契约,
// 断言 cap=4 的信号量在任意轮数下都不会被单个 Agent 占满。

// U5-01: 修复后的模式(轮末释放)——连续 10 轮后信号量应完全空闲。
func TestSlotLeak_U5_01_PerRoundReleaseDoesNotAccumulate(t *testing.T) {
	sema := make(chan struct{}, 4)
	a := &Agent{llmSema: sema}

	// 复刻 handleEvent 修复后的结构:slotHeld 标志 + 轮首/函数末释放。
	slotHeld := false
	releaseSlot := func() {
		if slotHeld {
			slotHeld = false
			a.ReleaseLLMSlot()
		}
	}
	defer releaseSlot()

	for round := 0; round < 10; round++ {
		releaseSlot() // 轮首归还上一轮的槽
		if !a.AcquireLLMSlot(50 * time.Millisecond) {
			t.Fatalf("第 %d 轮 acquire 失败:单个 Agent 把 cap=4 的信号量占满了(泄漏)", round)
		}
		slotHeld = true
	}
	releaseSlot()

	if got := len(sema); got != 0 {
		t.Fatalf("10 轮后信号量应完全释放,实际仍占用 %d 个槽", got)
	}
}

// U5-02: 缺陷模式对照 —— 若每轮 acquire 都不释放,第 5 轮必然拿不到槽。
// 这条用例证明上面的契约不是空断言:同样的 cap=4,不释放就是会满。
func TestSlotLeak_U5_02_AccumulatingWouldSaturate(t *testing.T) {
	sema := make(chan struct{}, 4)
	a := &Agent{llmSema: sema}

	for round := 0; round < 4; round++ {
		if !a.AcquireLLMSlot(20 * time.Millisecond) {
			t.Fatalf("前 4 轮不该失败,第 %d 轮就失败了", round)
		}
	}
	// 第 5 次:模拟旧代码「defer 堆到函数结束」的效果。
	if a.AcquireLLMSlot(20 * time.Millisecond) {
		t.Fatal("cap=4 时第 5 次 acquire 不应成功 —— 说明测试模型本身有问题")
	}

	// 归还,避免影响其它用例(同一 chan 是局部的,这里只为语义完整)。
	for i := 0; i < 4; i++ {
		a.ReleaseLLMSlot()
	}
}

// U5-03: 其它 Agent 必须还能拿到槽 —— 这才是本缺陷的真实业务影响
//（13 人局里 1 个 bot 卡死 12 个）。
func TestSlotLeak_U5_03_OtherAgentsStillGetSlots(t *testing.T) {
	sema := make(chan struct{}, 4) // 房间级共享
	slow := &Agent{llmSema: sema}
	fast := &Agent{llmSema: sema}

	slotHeld := false
	release := func() {
		if slotHeld {
			slotHeld = false
			slow.ReleaseLLMSlot()
		}
	}
	// 慢 bot 跑 8 轮,每轮规范地占一个槽再还。
	for round := 0; round < 8; round++ {
		release()
		if !slow.AcquireLLMSlot(50 * time.Millisecond) {
			t.Fatalf("慢 bot 第 %d 轮 acquire 失败", round)
		}
		slotHeld = true

		// 慢 bot 持槽期间,快 bot 仍应能拿到剩余 3 个槽中的一个。
		if !fast.AcquireLLMSlot(50 * time.Millisecond) {
			t.Fatalf("慢 bot 第 %d 轮持槽时,快 bot 被饿死 —— 信号量泄漏", round)
		}
		fast.ReleaseLLMSlot()
	}
	release()
}
