package wealth

import (
	"testing"
	"time"
)

// TestManager_CreateRoom_NoDeadlock 回归: 2026-09-14 P0 死锁 —— CreateRoom
// 持 m.mu 调用 pendingOptsApply(内部再次 Lock)导致每次建房自死锁,建房 HTTP
// 永久挂起。修复后(先登记 m.rooms、释放锁后再 applyOpts)本测试在 2s 内必须
// 完成;死锁复发时超时失败。
func TestManager_CreateRoom_NoDeadlock(t *testing.T) {
	m := NewManager(Config{MonthMs: 3000, PoolDefault: "curated"}, nil)
	m.ApplyRoomOptions("room-x", &WealthRoomOptions{MonthMs: 5000, Pool: "docs", Seed: 42})

	done := make(chan *WealthRoom, 1)
	go func() { done <- m.CreateRoom("room-x") }()
	select {
	case r := <-done:
		if r == nil {
			t.Fatal("CreateRoom returned nil")
		}
		if r.MonthMs != 5000 || r.pool != "docs" || r.seed != 42 {
			t.Fatalf("pending opts not applied: monthMs=%d pool=%s seed=%d", r.MonthMs, r.pool, r.seed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CreateRoom deadlocked (2s timeout)")
	}

	// 已存在路径同样不得死锁。
	done2 := make(chan *WealthRoom, 1)
	go func() { done2 <- m.CreateRoom("room-x") }()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("CreateRoom existing-path deadlocked")
	}
}

// TestWealthRoom_RegisterBotSeats_SeatsUsers 回归: 2026-09-14 P0 ——
// RegisterBotSeats 旧版不写 Seats[seat] 的 bot userID,Start 发卡跳过 bot、
// EnsureAgents 跳过,bot 永不上场。
func TestWealthRoom_RegisterBotSeats_SeatsUsers(t *testing.T) {
	r := NewWealthRoom("room-b", 3000, "curated", 7, 4)
	r.RegisterBotSeats(
		map[int]string{1: "bot-uid-1", 2: "bot-uid-2"},
		map[int]string{1: "ModelA", 2: "ModelB"},
		nil,
	)
	for _, seat := range []int{1, 2} {
		if r.Seats[seat] == "" {
			t.Fatalf("seat %d: bot userID not seated", seat)
		}
		if !r.BotSeats[seat] {
			t.Fatalf("seat %d: BotSeats not marked", seat)
		}
		if r.SeatModelKeys[seat] == "" {
			t.Fatalf("seat %d: SeatModelKeys empty", seat)
		}
		if r.Nicknames[seat] == "" {
			t.Fatalf("seat %d: nickname empty", seat)
		}
	}
	// 人类 JoinGame 不得占用 bot 座位。
	seat, _, e := r.JoinGame("human-uid", "human")
	if e != nil {
		t.Fatalf("JoinGame: %v", e)
	}
	if seat == 1 || seat == 2 {
		t.Fatalf("human seated on bot seat %d", seat)
	}
	// 幂等: 重复注册不覆盖已有人类座位。
	r.RegisterBotSeats(map[int]string{seat: "bot-uid-x"}, map[int]string{seat: "ModelC"}, nil)
	if r.Seats[seat] != "human-uid" {
		t.Fatalf("RegisterBotSeats overwrote human seat %d", seat)
	}
	// bot 座位参与 occupied 计数(影响满 8 自动开局判定)。
	if r.Occupied() != 3 {
		t.Fatalf("occupied = %d, want 3", r.Occupied())
	}
}
