package werewolf

// BUG-20260812-04-B 回归测试 ——「JoinGame → startCommentatorGoroutine 自死锁」。
//
// 根因:startCommentatorGoroutine(原实现)首行在 m.registry 守卫之后
// 立即 r.mu.Lock(),把 `!r.commentaryDesired || r.commentator != nil` 早退守卫
// 放在 Lock() 之后。结果是即使房间**未开启 AI 解说**(commentaryDesired=false),
// 进入函数就会无条件二次 Lock,而它的调用方 JoinGame 持锁(line 1549),
// → sync.Mutex 不可重入 → 永久自死锁,狼人杀 13 人局开局 100% 冻结。
//
// 修复:startCommentatorGoroutine 公开变体在锁外做早退守卫,然后委托
// startCommentatorGoroutineLocked 锁内变体(registry/model/style 均搬到首行后,
// 永不二次 Lock)。

import (
	"sync"
	"testing"
	"time"

	"LsmAgentGame/agent/wwcommentator"
)

const (
	// commentarySelfDeadlockTimeout — §92a 超时守卫:没它,自死锁会让整个
	// `go test` 包挂到超时,报错信息指不到具体函数。3s 足够覆盖 Fast mutex
	// 的「Lock 互相阻塞」表现,符合 R212 的超时档位。
	commentarySelfDeadlockTimeout = 3 * time.Second
)

// commentaryDeadlockHelper —— 复刻 JoinGame 持锁临界区结构,验证被调函数在
// 持锁态被调用时不会触发自死锁。调用 fn 时已持有 r.mu。
func commentaryDeadlockHelper(t *testing.T, r *WerewolfRoom, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 与 JoinGame(line 1549)的锁语义完全一致:持锁 + defer 解锁,
		// 然后在临界区里调被测函数。任何被测函数内部 r.mu.Lock() 都会卡死。
		r.mu.Lock()
		defer r.mu.Unlock()
		fn()
	}()
	select {
	case <-done:
	case <-time.After(commentarySelfDeadlockTimeout):
		t.Fatalf("startCommentatorGoroutine 在持锁上下文 %v 内未返回 — §92a 自死锁复发",
			commentarySelfDeadlockTimeout)
	}
}

// newBugB04Room — 最小可用房间(commentaryDesired 默认 false)。
func newBugB04Room() *WerewolfRoom {
	return &WerewolfRoom{RoomID: "bug-b04-room"}
}

// TestR212B_B01_StartCommentator_CommentaryDisabled_NoSelfDeadlock —
// commentaryDesired=false 时,JoinGame 持有 r.mu,startCommentatorGoroutine
// 必须立即返回,不能进入 r.mu.Lock()。修复前:自死锁 → 测试超时失败。
// 修复后:公开变体在锁外早退,委托都不需要,直接 return。
func TestR212B_B01_StartCommentator_CommentaryDisabled_NoSelfDeadlock(t *testing.T) {
	r := newBugB04Room()
	m := &WerewolfManager{}
	commentaryDeadlockHelper(t, r, func() {
		m.startCommentatorGoroutine(r, func(string, []byte) {})
	})
}

// TestR212B_B02_StartCommentator_NilRegistry_NoSelfDeadlock —
// 修复前:进 r.mu.Lock() 必然卡死。修复后:m.registry == nil 早退在锁外完成。
func TestR212B_B02_StartCommentator_NilRegistry_NoSelfDeadlock(t *testing.T) {
	r := newBugB04Room()
	r.commentaryDesired = true // 开关已开,但 registry 缺失也不应死锁
	m := &WerewolfManager{}   // registry == nil
	commentaryDeadlockHelper(t, r, func() {
		m.startCommentatorGoroutine(r, func(string, []byte) {})
	})
}

// TestR212B_B03_StartCommentator_AlreadyRunning_NoSelfDeadlock —
// commentator != nil 早退路径:即便已经启动过,公开变体也必须在锁外早退。
func TestR212B_B03_StartCommentator_AlreadyRunning_NoSelfDeadlock(t *testing.T) {
	r := newBugB04Room()
	r.commentaryDesired = true
	r.commentator = &wwcommentator.CommentatorAgent{}
	m := &WerewolfManager{}
	commentaryDeadlockHelper(t, r, func() {
		m.startCommentatorGoroutine(r, func(string, []byte) {})
	})
}

// TestR212B_B04_LockedVariant_ReducesLockLoop — startCommentatorGoroutineLocked
// 在 commentator != nil 或 commentaryDesired=false 时必须立即 return,
// **不**二次 Lock。验证:持锁调用 Locked 变体,早退路径必须不阻塞。
func TestR212B_B04_LockedVariant_ReducesLockLoop(t *testing.T) {
	r := newBugB04Room()
	r.commentaryDesired = true
	r.commentator = &wwcommentator.CommentatorAgent{}
	m := &WerewolfManager{}
	commentaryDeadlockHelper(t, r, func() {
		m.startCommentatorGoroutineLocked(r, func(string, []byte) {})
	})
}

// TestR212B_B05_PublicAndLocked_AreNotReentrant — 防止未来再有人在公开变体里
// 引入「已持锁时再次持锁」的退化路径。持锁 + 调公开变体,公开变体不再二次持锁
// 的最简证据是:测试在档位超时内完成。
func TestR212B_B05_PublicAndLocked_AreNotReentrant(t *testing.T) {
	r := newBugB04Room()
	r.commentaryDesired = false
	m := &WerewolfManager{}

	// 模拟 JoinGame 路径:外层 Lock + 委托
	var wg sync.WaitGroup
	wg.Add(1)
	start := time.Now()
	go func() {
		defer wg.Done()
		r.mu.Lock()
		defer r.mu.Unlock()
		// 委托公开变体,期望:在锁外 early-return。
		m.startCommentatorGoroutine(r, func(string, []byte) {})
	}()
	wg.Wait()
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("公开变体即便有外部 Lock 也应在 500ms 内返回,实际 %v", elapsed)
	}
}
