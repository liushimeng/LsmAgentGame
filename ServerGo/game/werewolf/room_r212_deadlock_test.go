package werewolf

// BUG-R212 回归测试 —— 狼人杀 13 人局「创建房间弹窗卡死」+「刷新后永久
// ⏳ 正在同步游戏状态…」的两个 P0 根因。
//
// 根因 A (P0-01) §92a `r.mu` 自死锁:
//   AggregateAgentStats() 内部 r.mu.Lock(),却被 BuildClientStateWithRoom 直接
//   调用,而后者的全部 4 个调用点(GetState / StateForSeat / SpectatorState /
//   SpectatorView)都已持有 r.mu。sync.Mutex 不可重入 → 第二次 Lock() 永久阻塞
//   且不释放锁。表现:
//     - CreateRoomWithAgents → SyncSeat → broadcastWerewolfState → StateForSeat
//       死锁 → POST /api/games/werewolf/rooms 永不返回 → 前端弹窗卡死;
//     - 刷新后 requestState → GetState 撞同一死锁 → game.state 永不下发;
//     - 该房间所有 REST 快照路径退化为 lockRoomBriefly 200ms 超时兜底。
//
// 根因 B (P0-02) 双重解锁:
//   completeHumanWaitAndStart 同时有 `defer r.mu.Unlock()` 与末尾显式
//   `r.mu.Unlock()` → `fatal error: sync: unlock of unlocked mutex`(不可
//   recover,直接杀进程)。
//
// §92a 硬约束:锁内函数的测试必须**持锁**调用 + 带超时守卫,否则测试会在
// 未持锁的宽松环境下通过,放过生产路径上的自死锁。

import (
	"testing"
	"time"
)

// runWithTimeout 在独立 goroutine 里跑 fn,超过 d 未完成即判定死锁。
// 死锁的 goroutine 无法被杀死,只能任其泄漏并让测试失败 —— 这正是 §92a
// 要求的「超时守卫」:没有它,自死锁会让 `go test` 挂到整包超时,报错信息
// 完全指不到具体函数。
func runWithTimeout(t *testing.T, d time.Duration, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s 在 %v 内未返回 —— 判定为 r.mu 自死锁(§92a)。"+
			"BuildClientStateWithRoom 的调用方已持有 r.mu,其内部任何直接或间接的 "+
			"r.mu.Lock() 都会永久阻塞", name, d)
	}
}

// newR212Room 构造一个最小可用房间:有 State、有座位,足以驱动
// BuildClientStateWithRoom 的完整字段填充路径(含 AgentStats 聚合)。
func newR212Room(roomID string) *WerewolfRoom {
	r := &WerewolfRoom{RoomID: roomID}
	r.State = NewGame(20260730)
	r.State.SeatCount = MaxPlayers
	return r
}

// TestR212_A01_BuildClientStateWithRoom_UnderLock_NoSelfDeadlock 是本轮的核心
// 回归:在**持有 r.mu**(与生产路径一致)的前提下调 BuildClientStateWithRoom。
//
// 修复前:AggregateAgentStats 二次 Lock → 永久阻塞 → 超时守卫触发。
// 修复后:走 aggregateAgentStatsLocked(不加锁)→ 正常返回。
func TestR212_A01_BuildClientStateWithRoom_UnderLock_NoSelfDeadlock(t *testing.T) {
	r := newR212Room("r212-a01")

	runWithTimeout(t, 3*time.Second, "BuildClientStateWithRoom(持锁)", func() {
		// 完全复刻 StateForSeat / GetState / SpectatorState / SpectatorView
		// 的锁语义:先持锁,再构建视图。
		r.mu.Lock()
		defer r.mu.Unlock()
		cs := BuildClientStateWithRoom(r.RoomID, r, 0)
		if cs == nil {
			t.Errorf("BuildClientStateWithRoom 不应返回 nil")
		}
	})
}

// TestR212_A02_StateForSeat_NoSelfDeadlock 直接打生产入口。StateForSeat 是
// broadcastWerewolfState 的下游 —— 也就是 POST 创建房间挂起时 goroutine dump
// 里的那一帧(ws/game_service_werewolf.go:688)。
func TestR212_A02_StateForSeat_NoSelfDeadlock(t *testing.T) {
	m := NewWerewolfManager()
	r := newR212Room("r212-a02")
	m.rooms[r.RoomID] = r

	runWithTimeout(t, 3*time.Second, "StateForSeat", func() {
		if cs := m.StateForSeat(r.RoomID, 0); cs == nil {
			t.Errorf("StateForSeat 不应返回 nil")
		}
	})

	// 死锁若发生,锁不会被释放。用 TryLock 断言锁已干净归还 —— 这条断言能
	// 抓住「函数返回了但锁泄漏」这种超时守卫看不见的变体。
	if !r.mu.TryLock() {
		t.Fatalf("StateForSeat 返回后 r.mu 仍被持有 —— 锁泄漏")
	}
	r.mu.Unlock()
}

// TestR212_A03_SpectatorState_NoSelfDeadlock 覆盖观战者视图路径
// (room_manage.go:301),同属 BuildClientStateWithRoom 的 4 个持锁调用点之一。
func TestR212_A03_SpectatorState_NoSelfDeadlock(t *testing.T) {
	m := NewWerewolfManager()
	r := newR212Room("r212-a03")
	m.rooms[r.RoomID] = r

	runWithTimeout(t, 3*time.Second, "SpectatorView", func() {
		if cs := m.SpectatorView(r.RoomID); cs == nil {
			t.Errorf("SpectatorView 不应返回 nil")
		}
	})

	if !r.mu.TryLock() {
		t.Fatalf("SpectatorView 返回后 r.mu 仍被持有 —— 锁泄漏")
	}
	r.mu.Unlock()
}

// TestR212_A04_AggregateAgentStats_PublicVariant_LocksItself 固定「公开变体」
// 的契约:未持锁的外部调用方可以直接调 AggregateAgentStats,它自己加锁。
//
// 这条测试防止未来有人「顺手」把公开变体也改成不加锁 —— 那会让未持锁的调用方
// 变成数据竞争(go test -race 可捕获)。
func TestR212_A04_AggregateAgentStats_PublicVariant_LocksItself(t *testing.T) {
	r := newR212Room("r212-a04")

	runWithTimeout(t, 3*time.Second, "AggregateAgentStats(未持锁)", func() {
		if stats := r.AggregateAgentStats(); stats == nil {
			t.Errorf("AggregateAgentStats 不应返回 nil")
		}
	})

	if !r.mu.TryLock() {
		t.Fatalf("AggregateAgentStats 返回后 r.mu 仍被持有 —— 公开变体未正确释放锁")
	}
	r.mu.Unlock()
}

// TestR212_A05_AggregateAgentStatsLocked_DoesNotTakeLock 反向固定「锁内变体」
// 的契约:它**绝不**自己加锁。若有人给 aggregateAgentStatsLocked 加回
// r.mu.Lock(),本测试会立刻超时失败,而不是等到生产环境创建房间时才炸。
func TestR212_A05_AggregateAgentStatsLocked_DoesNotTakeLock(t *testing.T) {
	r := newR212Room("r212-a05")

	runWithTimeout(t, 3*time.Second, "aggregateAgentStatsLocked(持锁)", func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if stats := r.aggregateAgentStatsLocked(); stats == nil {
			t.Errorf("aggregateAgentStatsLocked 不应返回 nil")
		}
	})
}

// TestR212_B01_CompleteHumanWaitAndStart_NoDoubleUnlock 覆盖根因 B。
//
// 双重解锁在 Go 里是 `fatal error`(runtime throw),**不可被 recover 捕获**,
// 会直接终止整个测试进程。因此本测试的判据不是「捕获 panic」,而是
// 「函数正常返回 + 锁状态干净」—— 修复前跑到末尾会 fatal,测试进程直接死掉,
// 这本身就是失败信号。
func TestR212_B01_CompleteHumanWaitAndStart_NoDoubleUnlock(t *testing.T) {
	m := NewWerewolfManager()
	r := newR212Room("r212-b01")
	m.rooms[r.RoomID] = r

	// 坐满 13 席,让 StartGame 能成功推进(否则会走提前 return 分支,
	// 测不到末尾的显式解锁路径)。
	for i := 0; i < MaxPlayers; i++ {
		uid := "u" + string(rune('a'+i))
		if _, e := r.State.AddPlayer(uid); e != nil {
			t.Fatalf("AddPlayer(seat=%d) 失败: %v", i, e)
		}
		r.Seats[i] = uid
	}

	runWithTimeout(t, 5*time.Second, "completeHumanWaitAndStart", func() {
		m.completeHumanWaitAndStart(r.RoomID)
	})

	// 末尾显式 Unlock 之后,锁必须处于「可获取」状态且只被解过一次。
	if !r.mu.TryLock() {
		t.Fatalf("completeHumanWaitAndStart 返回后 r.mu 仍被持有 —— 末尾漏解锁")
	}
	r.mu.Unlock()
}

// TestR212_B02_CompleteHumanWaitAndStart_EarlyReturn_ReleasesLock 固定两条
// 提前 return 分支(State==nil / Phase!=Filling)也必须显式解锁 —— 删掉
// `defer r.mu.Unlock()` 时最容易漏的正是这两条路径。
func TestR212_B02_CompleteHumanWaitAndStart_EarlyReturn_ReleasesLock(t *testing.T) {
	m := NewWerewolfManager()
	r := newR212Room("r212-b02")
	m.rooms[r.RoomID] = r

	// 把 Phase 推离 Filling,强制走「已被其他路径覆盖」的提前 return。
	r.State.Phase = PhaseNightWolves

	runWithTimeout(t, 3*time.Second, "completeHumanWaitAndStart(提前 return)", func() {
		m.completeHumanWaitAndStart(r.RoomID)
	})

	if !r.mu.TryLock() {
		t.Fatalf("提前 return 分支未释放 r.mu —— 该分支漏了显式 Unlock")
	}
	r.mu.Unlock()
}
