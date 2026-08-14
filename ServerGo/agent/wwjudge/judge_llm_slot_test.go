// Package wwjudge — judge_llm_slot_test.go: §20260814-01 U3 回归测试。
//
// 覆盖:
//   - J01 nil 信号量 → Acquire 恒 true(向后兼容:既有测试桩零改动)
//   - J02 cap=1 且已占满 → Acquire 在预算内返回 false(不 panic、不阻塞)
//   - J03 Release 确实归还槽位(防 §20260812-04 U5 defer 泄漏的镜像缺陷)
//   - J04 wait<=0 走非阻塞 select/default 分支
//   - J05 Release 无匹配 Acquire 时安全(不 panic、不阻塞)
//   - J06 Acquire 的实际等待时长受 wait 约束(不会无限等)
//   - J07 JudgeSlotAcquireWait 小于 player bot 的 5s(旁路让位纪律)
package wwjudge

import (
	"testing"
	"time"
)

func TestJudgeSlot_J01_NilSemaphoreAlwaysAcquires(t *testing.T) {
	j := NewAgentJudge("room-1", "model-1")
	// 未注入信号量 —— 所有既有测试桩与「cap<=0 禁用」配置都走这条路径。
	for i := 0; i < 10; i++ {
		if !j.AcquireLLMSlot(JudgeSlotAcquireWait) {
			t.Fatalf("nil 信号量下第 %d 次 Acquire 返回 false —— 破坏向后兼容", i+1)
		}
	}
	j.ReleaseLLMSlot() // 不应 panic
}

func TestJudgeSlot_J02_SaturatedSemaphoreFailsFast(t *testing.T) {
	sema := make(chan struct{}, 1)
	sema <- struct{}{} // 占满(模拟 player bot 持有唯一槽位)

	j := NewAgentJudge("room-1", "model-1")
	j.SetLLMSemaphore(sema)

	start := time.Now()
	if j.AcquireLLMSlot(100 * time.Millisecond) {
		t.Fatal("信号量已占满,Acquire 却返回 true")
	}
	// 关键:必须在预算内返回,不能无限阻塞法官 goroutine。
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Acquire 阻塞 %v,远超 100ms 预算", elapsed)
	}
}

// TestJudgeSlot_J03_ReleaseReturnsSlot 是最重要的一条:
// 若 Release 漏了(或 defer 位置写错),法官每次播报都会永久吃掉一个槽位,
// 几轮之后 cap=4 的房间对 player bot 而言等于 cap=0 —— 正是
// §20260812-04 U5「defer 写在 for 循环体内」缺陷的镜像形态。
func TestJudgeSlot_J03_ReleaseReturnsSlot(t *testing.T) {
	sema := make(chan struct{}, 2)
	j := NewAgentJudge("room-1", "model-1")
	j.SetLLMSemaphore(sema)

	if !j.AcquireLLMSlot(time.Second) {
		t.Fatal("空信号量首次 Acquire 失败")
	}
	if got := len(sema); got != 1 {
		t.Fatalf("Acquire 后占用 = %d, want 1", got)
	}
	j.ReleaseLLMSlot()
	if got := len(sema); got != 0 {
		t.Errorf("Release 后占用 = %d, want 0 —— 槽位泄漏", got)
	}

	// 反复 Acquire/Release 不应累积占用。
	for i := 0; i < 20; i++ {
		if !j.AcquireLLMSlot(time.Second) {
			t.Fatalf("第 %d 轮 Acquire 失败 —— 说明前序 Release 未生效(泄漏)", i+1)
		}
		j.ReleaseLLMSlot()
	}
	if got := len(sema); got != 0 {
		t.Errorf("20 轮后占用 = %d, want 0", got)
	}
}

func TestJudgeSlot_J04_ZeroWaitIsNonBlocking(t *testing.T) {
	sema := make(chan struct{}, 1)
	sema <- struct{}{}

	j := NewAgentJudge("room-1", "model-1")
	j.SetLLMSemaphore(sema)

	start := time.Now()
	if j.AcquireLLMSlot(0) {
		t.Fatal("wait=0 且已占满,Acquire 却返回 true")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("wait=0 应立即返回,实际耗时 %v", elapsed)
	}
}

// TestJudgeSlot_J05_ReleaseWithoutAcquireIsSafe 与 wwplayer.ReleaseLLMSlot
// 的「Safe to call without a matching acquire」契约对齐。
func TestJudgeSlot_J05_ReleaseWithoutAcquireIsSafe(t *testing.T) {
	sema := make(chan struct{}, 2)
	j := NewAgentJudge("room-1", "model-1")
	j.SetLLMSemaphore(sema)

	// 空信号量上 Release —— select/default 应直接跳过,不阻塞不 panic。
	done := make(chan struct{})
	go func() {
		j.ReleaseLLMSlot()
		j.ReleaseLLMSlot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("无匹配 Acquire 的 Release 阻塞了")
	}
	if got := len(sema); got != 0 {
		t.Errorf("占用 = %d, want 0", got)
	}
}

func TestJudgeSlot_J06_WaitBudgetIsRespected(t *testing.T) {
	sema := make(chan struct{}, 1)
	sema <- struct{}{}

	j := NewAgentJudge("room-1", "model-1")
	j.SetLLMSemaphore(sema)

	start := time.Now()
	j.AcquireLLMSlot(200 * time.Millisecond)
	elapsed := time.Since(start)
	// 至少等了预算(说明真的在等而非立刻放弃),但不显著超出。
	if elapsed < 150*time.Millisecond {
		t.Errorf("等待 %v < 预算 200ms —— timer 分支未生效", elapsed)
	}
	if elapsed > 600*time.Millisecond {
		t.Errorf("等待 %v 远超预算 200ms", elapsed)
	}
}

// TestJudgeSlot_J07_JudgeBudgetYieldsToPlayerBots 锚定 U3 的核心设计决策:
// 法官/解说的槽位预算必须**小于** player bot 的 llmSlotAcquireWait(5s),
// 否则「装饰性调用给推进游戏的调用让路」这条纪律就不成立。
//
// 5s 是 wwplayer/run.go:346 的 llmSlotAcquireWait,该包不导出该常量,
// 故此处以字面量锚定 —— 若那边调整了预算,本断言会提示重新评估两者关系。
func TestJudgeSlot_J07_JudgeBudgetYieldsToPlayerBots(t *testing.T) {
	const playerBotAcquireWait = 5 * time.Second // wwplayer/run.go:346
	if JudgeSlotAcquireWait >= playerBotAcquireWait {
		t.Errorf("JudgeSlotAcquireWait=%v 不小于 player bot 的 %v —— "+
			"旁路装饰性调用必须给推进游戏的调用让路(§20260814-01 U3 核心决策)",
			JudgeSlotAcquireWait, playerBotAcquireWait)
	}
}
