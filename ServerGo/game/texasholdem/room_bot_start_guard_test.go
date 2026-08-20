// room_bot_start_guard_test.go — 2026-08-20 §P0-NEW-1 / P0-3 回归测试。
//
// 验证 JoinGame 自动开局守卫 allBotSeatsOccupiedLocked:
//   - RegisterBotSeats 先于 JoinGame 时,任何已标记 bot 座位未入座前不开局;
//   - 全部 bot 座位入座后(或人类入座满足守卫时)正常开局;
//   - JoinGame 失败回滚(UnregisterBotSeat)后守卫不再阻塞;
//   - 纯人类房间(BotSeats 全 false)行为不变(满 2 人即开局)。
package texasholdem

import "testing"

// TestJoinGame_BotSeatGuardBlocksEarlyStart: 先标记 bot 座位,人类先入座的
// 场景下守卫必须阻止提前开局(否则开局后入座的 bot 本手底牌全零,
// 且 BotSeats 时序错位会让 ProcessBotTurn 永久不驱动)。
func TestJoinGame_BotSeatGuardBlocksEarlyStart(t *testing.T) {
	mgr := NewTexasHoldemManager()
	const roomID = "room-guard-1"

	// 1. 先于任何 JoinGame 标记 bot 座位(2/3 号位)
	mgr.RegisterBotSeats(roomID, map[int]string{2: "ModelA", 3: "ModelB"})

	// 2. 两名人类入座(座位 0/1)—— 即使满 2 人也不得开局
	if _, started, e := mgr.JoinGame(roomID, "human-1"); e != nil || started {
		t.Fatalf("join human-1: started=%v err=%v", started, e)
	}
	if _, started, e := mgr.JoinGame(roomID, "human-2"); e != nil || started {
		t.Fatalf("join human-2: started=%v err=%v (bot seats 2/3 not occupied, must not start)", started, e)
	}
	r := mgr.GetRoomForBot(roomID)
	if r == nil || r.State == nil {
		t.Fatal("room state should exist after joins")
	}
	if r.State.Street != PhaseWaiting {
		t.Fatalf("street = %s, want waiting (guard should block)", r.State.Street)
	}

	// 3. bot 按配置座位逐个入座:坐满最后一个标记座位的那次入座才触发开局
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-a", 2); e != nil || started {
		t.Fatalf("join bot-a@2: started=%v err=%v (seat 3 still empty)", started, e)
	}
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-b", 3); e != nil || !started {
		t.Fatalf("join bot-b@3: started=%v err=%v (all bot seats occupied, must start)", started, e)
	}
	if r.State.Street == PhaseWaiting {
		t.Fatal("hand should have started after all bot seats occupied")
	}

	// 4. P0-3 回归:本手全员底牌必须非零(开局前已全员入座)
	for i := 0; i < MaxPlayers; i++ {
		p := &r.State.Players[i]
		if p.UserID == "" {
			continue
		}
		if p.Hole[0].Rank == 0 || p.Hole[1].Rank == 0 {
			t.Errorf("seat %d (%s) has zero-value hole cards", i, p.UserID)
		}
	}
}

// TestJoinGame_BotSeatGuardRollback: JoinGame 失败回滚(UnregisterBotSeat)后,
// 空 bot 座位不得继续阻塞自动开局。
func TestJoinGame_BotSeatGuardRollback(t *testing.T) {
	mgr := NewTexasHoldemManager()
	const roomID = "room-guard-2"

	mgr.RegisterBotSeats(roomID, map[int]string{2: "ModelA"})
	// 模拟该 bot 座位 JoinGame 失败 → 回滚标记
	mgr.UnregisterBotSeat(roomID, 2)

	if _, _, e := mgr.JoinGame(roomID, "human-1"); e != nil {
		t.Fatalf("join human-1: %v", e)
	}
	if _, started, e := mgr.JoinGame(roomID, "human-2"); e != nil || !started {
		t.Fatalf("join human-2: started=%v err=%v (bot mark rolled back, must start)", started, e)
	}

	// 幂等性:重复回滚 / 越界座位 / 不存在房间均安全返回
	mgr.UnregisterBotSeat(roomID, 2)
	mgr.UnregisterBotSeat(roomID, -1)
	mgr.UnregisterBotSeat(roomID, MaxPlayers)
	mgr.UnregisterBotSeat("no-such-room", 0)
}

// TestJoinGame_PureHumanRoomUnchanged: 纯人类房间(BotSeats 全 false)真空真,
// 满 2 人即自动开局,行为与守卫引入前一致。
func TestJoinGame_PureHumanRoomUnchanged(t *testing.T) {
	mgr := NewTexasHoldemManager()
	const roomID = "room-guard-3"

	if _, started, e := mgr.JoinGame(roomID, "human-1"); e != nil || started {
		t.Fatalf("join human-1: started=%v err=%v", started, e)
	}
	if _, started, e := mgr.JoinGame(roomID, "human-2"); e != nil || !started {
		t.Fatalf("join human-2: started=%v err=%v (pure human room must start at 2 players)", started, e)
	}
}

// TestJoinGame_BotSeatsMarkedBeforeJoin: 生产主路径 —— 全部 bot 按配置座位
// (JoinGameAtSeat)入座,人类在前的场景;最后一个 bot 入座触发开局,
// 且配置座位 = 物理座位 = BotSeats 标记,BotSeats 与 Seats 严格对齐。
func TestJoinGame_BotSeatsMarkedBeforeJoin(t *testing.T) {
	mgr := NewTexasHoldemManager()
	const roomID = "room-guard-4"

	mgr.RegisterBotSeats(roomID, map[int]string{0: "ModelA", 1: "ModelB"})
	// 人类先入座(指定座位 2 —— 生产路径由 registerTexasHoldemAgentSeats
	// 按 DB 座位预入座,避免 first-empty 抢走已标记的 bot 座位)
	if _, started, e := mgr.JoinGameAtSeat(roomID, "human-1", 2); e != nil || started {
		t.Fatalf("join human-1@2: started=%v err=%v (bot seats 0/1 empty, must not start)", started, e)
	}
	// bot 按配置座位入座:第一个 bot 不满 2 人不开局?——人类已在座,满 2 人,
	// 但守卫要求全部 bot 座位入座,故仍不开局
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-a", 0); e != nil || started {
		t.Fatalf("join bot-a@0: started=%v err=%v (bot seat 1 still empty)", started, e)
	}
	// 最后一个 bot 入座 → 守卫满足 → 开局
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-b", 1); e != nil || !started {
		t.Fatalf("join bot-b@1: started=%v err=%v (all bot seats occupied, must start)", started, e)
	}
	r := mgr.GetRoomForBot(roomID)
	if r.Seats[0] != "bot-a" || r.Seats[1] != "bot-b" || r.Seats[2] != "human-1" {
		t.Fatalf("seat mapping mismatch: %v", r.Seats)
	}
	for i := 0; i < MaxPlayers; i++ {
		p := &r.State.Players[i]
		if p.UserID == "" {
			continue
		}
		if p.Hole[0].Rank == 0 || p.Hole[1].Rank == 0 {
			t.Errorf("seat %d (%s) has zero-value hole cards", i, p.UserID)
		}
	}
}

// TestJoinGameAtSeat_ScatteredSeatsNoDeadlock: 前端 agent_seats 座位号随机,
// bot 配置座位可能分散(如 {4,5})。JoinGameAtSeat 按指定座位入座,
// 守卫只依赖「标记座位是否被其 bot 占据」,不受物理入座顺序影响 ——
// 若 bot 走 first-empty 入座(物理 0/1),标记 {4,5} 将永不满足,永久不开局。
func TestJoinGameAtSeat_ScatteredSeatsNoDeadlock(t *testing.T) {
	mgr := NewTexasHoldemManager()
	const roomID = "room-guard-5"

	mgr.RegisterBotSeats(roomID, map[int]string{4: "ModelA", 5: "ModelB"})
	// 两名人类先入座(first-empty → 座位 0/1)
	if _, _, e := mgr.JoinGame(roomID, "human-1"); e != nil {
		t.Fatalf("join human-1: %v", e)
	}
	if _, started, e := mgr.JoinGame(roomID, "human-2"); e != nil || started {
		t.Fatalf("join human-2: started=%v err=%v (bot seats 4/5 empty, must not start)", started, e)
	}
	// bot 按分散的配置座位入座
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-a", 4); e != nil || started {
		t.Fatalf("join bot-a@4: started=%v err=%v (bot seat 5 still empty)", started, e)
	}
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-b", 5); e != nil || !started {
		t.Fatalf("join bot-b@5: started=%v err=%v (all bot seats occupied, must start)", started, e)
	}
	r := mgr.GetRoomForBot(roomID)
	if r.Seats[4] != "bot-a" || r.Seats[5] != "bot-b" {
		t.Fatalf("seat mapping mismatch: %v", r.Seats)
	}
	// 引擎侧 Players 也必须按同一座位对齐(否则 StartHand 发牌错位)
	if r.State.Players[4].UserID != "bot-a" || r.State.Players[5].UserID != "bot-b" {
		t.Fatalf("engine seat mapping mismatch: %+v", r.State.Players)
	}

	// 幂等性:重复 JoinGameAtSeat 安全返回
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-a", 4); e != nil || started {
		t.Fatalf("re-join bot-a@4: started=%v err=%v (must be idempotent)", started, e)
	}
}
