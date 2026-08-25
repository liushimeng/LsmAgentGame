// §20260819-02 P0-1 — SpectateGame 懒创建 + 座位 hydrator 回归测试。
// 覆盖 5 个不变式(对应 plan §2.5 T1..T5):
//   T1 内存缺失 -> 懒创建成功,返回 (room, created=true, nil)
//   T2 hydrator 恢复混合座位(1 人类 + 2 Agent)后 View ready=true
//   T3 hydrator 恢复后纯 Agent 房间自动 StartHand(Street != PhaseWaiting)
//   T4 已有内存房间时 SpectateGame 不触发 hydrator(幂等,不重复 StartHand)
//   T5 观战者(viewer=-1)MyHole 恒空(不泄漏底牌)
package texasholdem

import (
	"testing"
)

func newRoomForSpectateTest(seats [MaxPlayers]string, botSeats [MaxPlayers]bool, botModels [MaxPlayers]string) *TexasHoldemRoom {
	r := &TexasHoldemRoom{
		RoomID:    "test-room",
		Seats:     seats,
		BotSeats:  botSeats,
		BotModels: botModels,
		State:     NewGame(1, 200),
	}
	// 把 BotSeats/BotModels 与 Seats 对齐(仅测试用)。
	for i, s := range seats {
		if s != "" {
			r.State.Players[i] = Player{UserID: s, Seat: i, Stack: 10000}
		}
	}
	r.State.NumSeat = r.Occupied()
	return r
}

// T1 内存缺失 -> 懒创建,created=true。
func TestSpectateGame_LazyCreatesOnMemoryMiss(t *testing.T) {
	mgr := NewTexasHoldemManager()
	room, created, err := mgr.SpectateGame("missing-room", "viewer-1")
	if err != nil {
		t.Fatalf("expected no error on memory miss, got %v", err)
	}
	if !created {
		t.Fatalf("expected created=true, got false")
	}
	if room == nil {
		t.Fatalf("expected non-nil room")
	}
	if room.RoomID != "missing-room" {
		t.Fatalf("wrong room id: %q", room.RoomID)
	}
	if _, ok := room.Spectators["viewer-1"]; !ok {
		t.Fatalf("viewer not registered as spectator")
	}
}

// T2 hydrator 恢复 1 人类 + 2 Agent 混合座位,View ready=true。
func TestSpectateGame_HydrateMixedSeatsReady(t *testing.T) {
	mgr := NewTexasHoldemManager()
	called := false
	mgr.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		called = true
		return []SeatRestoreInfo{
			{Seat: 1, UserID: "human-1", ModelKey: ""},
			{Seat: 3, UserID: "bot-3", ModelKey: "DeepSeek-model"},
			{Seat: 5, UserID: "bot-5", ModelKey: "GLM-model"},
		}, nil
	})
	room, created, err := mgr.SpectateGame("mixed-room", "viewer-1")
	if err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}
	if !created {
		t.Fatalf("expected created=true on memory miss")
	}
	if !called {
		t.Fatalf("hydrator not called")
	}
	if room.Seats[1] != "human-1" || room.Seats[3] != "bot-3" || room.Seats[5] != "bot-5" {
		t.Fatalf("seats not restored: %+v", room.Seats)
	}
	if !room.BotSeats[3] || !room.BotSeats[5] || room.BotSeats[1] {
		t.Fatalf("bot flags wrong: %+v", room.BotSeats)
	}
	if room.BotModels[3] != "DeepSeek-model" || room.BotModels[5] != "GLM-model" {
		t.Fatalf("bot models wrong: %+v", room.BotModels)
	}
	if !room.IsReady() {
		t.Fatalf("3 seated should be ready")
	}
}

// T3 hydrator 恢复后纯 Agent 房间自动 StartHand(Street != PhaseWaiting)。
func TestSpectateGame_HydrateAutoStartsPureAgentRoom(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		return []SeatRestoreInfo{
			{Seat: 0, UserID: "bot-0", ModelKey: "DeepSeek-model"},
			{Seat: 2, UserID: "bot-2", ModelKey: "GLM-model"},
		}, nil
	})
	room, _, err := mgr.SpectateGame("pure-bot", "viewer-1")
	if err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}
	if room.State == nil {
		t.Fatalf("state nil after hydrate")
	}
	if room.State.Street == PhaseWaiting {
		t.Fatalf("pure agent room should auto-start hand, got PhaseWaiting")
	}
	if room.State.HandNumber < 1 {
		t.Fatalf("expected hand number >=1, got %d", room.State.HandNumber)
	}
}

// T4 已有内存房间时 SpectateGame 不触发 hydrator(幂等,不重置手牌)。
func TestSpectateGame_NoHydrateWhenRoomExists(t *testing.T) {
	mgr := NewTexasHoldemManager()
	// 预先放一个有手牌进行中的房间。
	r := newRoomForSpectateTest(
		[MaxPlayers]string{"a", "b", "", "", "", ""},
		[MaxPlayers]bool{},
		[MaxPlayers]string{},
	)
	_ = r.State.StartHand()
	originalHandNum := r.State.HandNumber
	originalStreet := r.State.Street
	mgr.rooms["existing"] = r

	called := false
	mgr.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		called = true
		return []SeatRestoreInfo{{Seat: 0, UserID: "z", ModelKey: ""}}, nil
	})
	room, created, err := mgr.SpectateGame("existing", "viewer-1")
	if err != nil {
		t.Fatalf("spectate failed: %v", err)
	}
	if created {
		t.Fatalf("created must be false when room already exists")
	}
	if called {
		t.Fatalf("hydrator must not be called when room already in memory")
	}
	if room.State.HandNumber != originalHandNum || room.State.Street != originalStreet {
		t.Fatalf("hydrate must not reset in-flight hand: hand %d->%d, street %d->%d",
			originalHandNum, room.State.HandNumber, originalStreet, room.State.Street)
	}
}

// T5 观战者(viewer=-1)MyHole 恒空,不泄漏任何底牌。
func TestSpectateGame_SpectatorViewHidesHoleCards(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		return []SeatRestoreInfo{
			{Seat: 0, UserID: "bot-0", ModelKey: "DeepSeek-model"},
			{Seat: 1, UserID: "bot-1", ModelKey: "GLM-model"},
		}, nil
	})
	room, _, err := mgr.SpectateGame("spectator-view-test", "viewer-x")
	if err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}
	// engine 端给每个 bot 塞私有底牌(模拟手牌内态),观战 view 不应透出。
	room.State.Players[0].Hole = [2]Card{{Rank: 14, Suit: SuitSpade}, {Rank: 13, Suit: SuitSpade}}
	room.State.Players[1].Hole = [2]Card{{Rank: 12, Suit: SuitHeart}, {Rank: 11, Suit: SuitHeart}}
	cs := BuildClientStateWithRoom(
		"spectator-view-test",
		room.Seats, room.BotSeats, room.BotModels,
		-1, // 观战者
		room.State,
		room.BotHeartThought, room.BotThinking, room.BotLastChat,
	)
	if len(cs.MyHole) != 0 {
		t.Fatalf("spectator view leaked MyHole: %+v", cs.MyHole)
	}
	for i, p := range cs.Players {
		if len(p.Hole) != 0 {
			t.Fatalf("spectator view leaked Hole on seat %d: %+v", i, p.Hole)
		}
	}
}

// hydrator 返回错误时房间仍存在但座位为空(失败不致命,静默 WARN)。
func TestSpectateGame_HydrateErrorDoesNotPanic(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		return nil, errDBFakeForTest
	})
	room, created, err := mgr.SpectateGame("hydrate-err", "viewer-1")
	if err != nil {
		t.Fatalf("expected no error propagated, got %v", err)
	}
	if !created {
		t.Fatalf("expected created=true")
	}
	if room.Occupied() != 0 {
		t.Fatalf("hydrate failure should leave room empty, got occupied=%d", room.Occupied())
	}
}

// 哨兵错误仅测试用 -- errcode.Error 的 Import 是公开包,但避免引入循环
// 测试文件单独声明。
var errDBFakeForTest = errcodeNewCode(99999, "fake db error")

// 轻量复制 errcode 包的 Code 函数签名,避免测试文件循环导入。
func errcodeNewCode(code int, msg string) error {
	type coder interface{ Code() int }
	_ = coder(nil)
	return &fakeErr{code: code, msg: msg}
}

type fakeErr struct {
	code int
	msg  string
}

func (e *fakeErr) Error() string  { return e.msg }
func (e *fakeErr) Code() int      { return e.code }

// T6 观战视图的座位契约 —— 前端 TexasHoldemTable 的座位旋转依赖此契约:
//   1. MySeat 恒为 -1(观战者没有「自己」的座位);
//   2. Players 恒为长度 6 的满数组(每个物理座位一项,空座 HasPlayer=false)。
//
// 2026-08-20 §德州扑克观战崩溃回归防线: 前端曾用 (mySeat+i)%6 旋转座位,
// mySeat=-1 时 JS 负取模产出 seatOrder[0]=-1 → players[-1]=undefined →
// PlayerSeat 解引用 has_player 抛错 → 整页「页面渲染异常」。前端已改为
// 观战者不旋转; 此测试锁死后端这一侧的契约, 防止 MySeat 哨兵值或 Players
// 长度被改动后前端再次踩空。
func TestSpectatorView_SeatContractForClientRotation(t *testing.T) {
	mgr := NewTexasHoldemManager()
	mgr.SetSeatHydrator(func(roomID string) ([]SeatRestoreInfo, error) {
		// 1 真人 + 5 Agent —— 复现用户报告的房间构成。
		return []SeatRestoreInfo{
			{Seat: 0, UserID: "human-1"},
			{Seat: 1, UserID: "bot-1", ModelKey: "DeepSeek-model"},
			{Seat: 2, UserID: "bot-2", ModelKey: "GLM-model"},
			{Seat: 3, UserID: "bot-3", ModelKey: "Kimi-model"},
			{Seat: 4, UserID: "bot-4", ModelKey: "Qwen-model"},
			{Seat: 5, UserID: "bot-5", ModelKey: "DouBao-model"},
		}, nil
	})
	room, _, err := mgr.SpectateGame("spectator-seat-contract", "viewer-y")
	if err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}

	cs := BuildClientStateWithRoom(
		"spectator-seat-contract",
		room.Seats, room.BotSeats, room.BotModels,
		-1, // 观战者
		room.State,
		room.BotHeartThought, room.BotThinking, room.BotLastChat,
	)

	if cs.MySeat != -1 {
		t.Fatalf("spectator MySeat must stay the -1 sentinel, got %d", cs.MySeat)
	}
	if len(cs.Players) != MaxPlayers {
		t.Fatalf("Players must always have %d entries so the client can index every seat, got %d",
			MaxPlayers, len(cs.Players))
	}
	// 客户端按 0..5 自然座序渲染: 每个下标都必须可安全解引用。
	for seat := 0; seat < MaxPlayers; seat++ {
		if got := cs.Players[seat].Seat; got != seat {
			t.Fatalf("Players[%d].Seat = %d, want %d (index must equal physical seat)", seat, got, seat)
		}
		if !cs.Players[seat].HasPlayer {
			t.Fatalf("seat %d hydrated with a user but HasPlayer=false", seat)
		}
	}
}
