package wealth

import (
	"fmt"
	"testing"
	"time"

	"LsmAgentGame/errcode"
)

// TestManager_CreateRoom_NoDeadlock 回归: 2026-09-14 P0 死锁 —— CreateRoom
// 持 m.mu 调用 pendingOptsApply(内部再次 Lock)导致每次建房自死锁,建房 HTTP
// 永久挂起。修复后(先登记 m.rooms、释放锁后再 applyOpts)本测试在 2s 内必须
// 完成;死锁复发时超时失败。
func TestManager_CreateRoom_NoDeadlock(t *testing.T) {
	m := NewManager(Config{MonthMs: 3000}, nil)
	m.ApplyRoomOptions("room-x", &WealthRoomOptions{MonthMs: 5000, Seed: 42})

	done := make(chan *WealthRoom, 1)
	go func() { done <- m.CreateRoom("room-x") }()
	select {
	case r := <-done:
		if r == nil {
			t.Fatal("CreateRoom returned nil")
		}
		if r.MonthMs != 5000 || r.seed != 42 {
			t.Fatalf("pending opts not applied: monthMs=%d seed=%d", r.MonthMs, r.seed)
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
	r := NewWealthRoom("room-b", 3000, 7, 4)
	r.RegisterBotSeats(
		map[int]string{1: "bot-uid-1", 2: "bot-uid-2"},
		map[int]string{1: "ModelA", 2: "ModelB"},
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
	r.RegisterBotSeats(map[int]string{seat: "bot-uid-x"}, map[int]string{seat: "ModelC"})
	if r.Seats[seat] != "human-uid" {
		t.Fatalf("RegisterBotSeats overwrote human seat %d", seat)
	}
	// bot 座位参与 occupied 计数(影响满 8 自动开局判定)。
	if r.Occupied() != 3 {
		t.Fatalf("occupied = %d, want 3", r.Occupied())
	}
}

// TestManager_CreateRoom_HydratedTenOrElevenBotsRestoreFullAgentMode 覆盖
// 服务重启恢复语义:10/11 bot 房虽仍有 1-2 个物理空位,但已属于全 Agent 房,
// 恢复后不得重新接受人类入座。
func TestManager_CreateRoom_HydratedTenOrElevenBotsRestoreFullAgentMode(t *testing.T) {
	for _, count := range []int{MinSeats, MinSeats + 1} {
		t.Run(fmt.Sprintf("%dbots", count), func(t *testing.T) {
			seats := make([]SeatRestoreInfo, count)
			for seat := 0; seat < count; seat++ {
				seats[seat] = SeatRestoreInfo{
					Seat:     seat,
					UserID:   fmt.Sprintf("restored-bot-%d", seat),
					IsBot:    true,
					ModelKey: fmt.Sprintf("RestoredModel%d", seat),
				}
			}
			m := NewManager(Config{
				MonthMs: 3000,
				AgentEnabled: true, AgentConcurrency: DefaultAgentConcurrency,
			}, nil)
			m.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
				return seats, nil
			})

			r := m.CreateRoom("room-hydrated-full-agent")
			if !r.IsFullAgentMode() {
				t.Fatal("hydrated 10/11 bot room did not restore FullAgentMode")
			}
			if got := r.Occupied(); got != count {
				t.Fatalf("occupied = %d, want %d", got, count)
			}
			seat, _, err := r.JoinGame("human-after-restore", "human")
			if err == nil || err.Code != errcode.ErrWealthFullAgentReject {
				t.Fatalf("JoinGame error = %v, want ErrWealthFullAgentReject", err)
			}
			if seat != -1 {
				t.Fatalf("human seat = %d, want -1", seat)
			}
		})
	}

	// 9 bot 恢复房不触发全 Agent 语义,保留一个可加入物理空位。
	m := NewManager(Config{MonthMs: 3000}, nil)
	m.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		seats := make([]SeatRestoreInfo, MinSeats-1)
		for seat := range seats {
			seats[seat] = SeatRestoreInfo{
				Seat: seat, UserID: fmt.Sprintf("bot-%d", seat),
				IsBot: true, ModelKey: "RestoredModel",
			}
		}
		return seats, nil
	})
	r := m.CreateRoom("room-hydrated-nine-bots")
	if r.IsFullAgentMode() {
		t.Fatal("9 bot restored room should not be full-agent")
	}
}

// TestManager_EnsureAgentsLockedVariant_ReentrantUnderWriteLock 回归(§92a 复发,
// 2026-09-21 线上 P0):§虚拟城市 B3 给 EnsureAgents 新增 m.mu.RLock(读
// linePoolSource),而 CreateRoom hydrate 段持 m.mu 写锁调用它 —— Go RWMutex
// 不可重入 → 自死锁,所有带 agent 座位的 wealth 建房请求挂死(CPU 0%)。
// 既有 hydrate 测试因 registry==nil 在 RLock 之前短路,未覆盖该路径。
// 修复:拆出 ensureAgentsWithPool 锁内变体。本测试直接钉死重入性质 ——
// Manager 写锁持有时调用锁内变体必须 3s 内返回。
func TestManager_EnsureAgentsLockedVariant_ReentrantUnderWriteLock(t *testing.T) {
	m := NewManager(Config{
		MonthMs: 3000,
		AgentEnabled: true, AgentConcurrency: DefaultAgentConcurrency,
	}, fakeBuyRegistry{})
	r := NewWealthRoom("room-92a-reentrant", 3000, 7, 4)
	r.RegisterBotSeats(
		map[int]string{1: "bot-uid-92a"},
		map[int]string{1: "Model92a"},
	)

	m.mu.Lock() // 模拟 CreateRoom 持写锁现场
	done := make(chan struct{})
	go func() {
		m.ensureAgentsWithPool(m.linePoolSource, r)
		close(done)
	}()
	select {
	case <-done: // ok — 锁内变体在写锁内可重入
	case <-time.After(3 * time.Second):
		t.Fatal("EnsureAgents 在 Manager 写锁内死锁(§92a)")
	}
	m.mu.Unlock()

	// 行为不变性:锁内变体同样为显式 model_key 座位装配 Agent。
	r.mu.Lock()
	_, ok := r.agents[1]
	r.mu.Unlock()
	if !ok {
		t.Fatal("ensureAgentsWithPool 未给显式 model_key bot 座位建 Agent(行为回归)")
	}
}

// TestManager_CreateRoom_HydratePath_EnsureAgents_NoDeadlock 全链路回归:
// 生产死锁现场 = 服务重启后首次访问,room_service 已把 agent 座位写入 DB,
// CreateRoom 经 seatHydrator 读回(restoredBots>0)并在 m.mu 写锁内装配 bot
// agent。与上一测试的差异:本测试走公开 CreateRoom 入口(带非 nil registry,
// 与生产装配一致),钉死「每笔带 agent 座位的建房请求」不再挂死,且 agent
// 确实装配(行为不变)。旧代码在本测试上稳定复现挂死。
func TestManager_CreateRoom_HydratePath_EnsureAgents_NoDeadlock(t *testing.T) {
	const botCount = 3
	seats := make([]SeatRestoreInfo, botCount)
	for i := 0; i < botCount; i++ {
		seats[i] = SeatRestoreInfo{
			Seat:     i,
			UserID:   fmt.Sprintf("bot-92a-%d", i),
			IsBot:    true,
			ModelKey: fmt.Sprintf("Model92a-%d", i),
		}
	}
	m := NewManager(Config{
		MonthMs: 3000,
		AgentEnabled: true, AgentConcurrency: DefaultAgentConcurrency,
	}, fakeBuyRegistry{})
	m.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		return seats, nil
	})

	done := make(chan *WealthRoom, 1)
	go func() { done <- m.CreateRoom("room-92a-hydrate") }()
	select {
	case r := <-done:
		r.mu.Lock()
		got := len(r.agents)
		r.mu.Unlock()
		if got != botCount {
			t.Fatalf("hydrated bots 装配 agents = %d, want %d(行为回归)", got, botCount)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CreateRoom→hydrate→EnsureAgents 全链路死锁(§92a,3s 超时)")
	}
}
