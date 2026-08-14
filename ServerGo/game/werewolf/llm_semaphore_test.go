// Package werewolf — llm_semaphore_test.go: §20260814-01 U3 房间级信号量
// 单一事实来源的回归测试。
//
// 覆盖:
//   - S01 幂等 —— 重复调用不重建 chan(否则 bot 与法官会拿到不同的 chan)
//   - S02 nil room 不 panic
//   - S03 已有信号量时不被覆盖(容量与引用都保持)
//   - S04 法官与 bot 共享**同一个** chan 引用(U3 的核心不变量)
package werewolf

import "testing"

func TestLLMSema_S01_Idempotent(t *testing.T) {
	r := &WerewolfRoom{}
	r.ensureLLMSemaphoreLocked()
	first := r.llmSema
	r.ensureLLMSemaphoreLocked()
	r.ensureLLMSemaphoreLocked()

	if r.llmSema != first {
		t.Error("ensureLLMSemaphoreLocked 非幂等 —— 重复调用重建了 chan;" +
			"这会让先注入的 bot 与后注入的法官持有不同信号量,并发上限失效")
	}
}

func TestLLMSema_S02_NilRoomIsSafe(t *testing.T) {
	var r *WerewolfRoom
	r.ensureLLMSemaphoreLocked() // 不应 panic
}

func TestLLMSema_S03_ExistingSemaphoreNotReplaced(t *testing.T) {
	pre := make(chan struct{}, 7)
	r := &WerewolfRoom{llmSema: pre}
	r.ensureLLMSemaphoreLocked()

	if r.llmSema != pre {
		t.Error("已存在的信号量被覆盖 —— 房间复用期间 cap 必须保持不变")
	}
	if got := cap(r.llmSema); got != 7 {
		t.Errorf("cap = %d, want 7(未被 cfg 值覆盖)", got)
	}
}

// TestLLMSema_S04_JudgeAndBotShareSameChannel 锚定 U3 的核心不变量:
// 法官与 player bot 必须共享**同一个** chan 引用,否则各自有独立配额,
// 「房间级并发上限」这个概念就不存在了 —— 这正是 U3 修复前的实际状态
// (法官压根没有信号量,等价于独立无限配额)。
func TestLLMSema_S04_JudgeAndBotShareSameChannel(t *testing.T) {
	r := &WerewolfRoom{}

	// 模拟 StartAgentsLocked(room_agent.go)的注入。
	r.ensureLLMSemaphoreLocked()
	botSema := r.llmSema

	// 模拟 startJudgeGoroutine(judge_summary_bridge.go)的注入。
	r.ensureLLMSemaphoreLocked()
	judgeSema := r.llmSema

	// 模拟 startCommentatorGoroutineLocked(commentary_room.go)的注入。
	r.ensureLLMSemaphoreLocked()
	commentarySema := r.llmSema

	if botSema != judgeSema || judgeSema != commentarySema {
		t.Error("bot / 法官 / 解说 拿到了不同的信号量引用 —— " +
			"房间级并发上限失效(U3 修复前法官与解说完全绕过信号量,在飞可达 6)")
	}
}
