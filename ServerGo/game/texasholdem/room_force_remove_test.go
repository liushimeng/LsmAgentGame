// room_force_remove_test.go — 2026-08-24 BUG-TEXAS-DISCARD-STALL (R17 P1) 回归测试。
//
// 缺陷背景:人类玩家对局中断线,15s 超时后 RoomService.LeaveRoom 只清 DB 行,
// in-memory 对局的回合仍停在已离线的人类座位上 —— bot 回合 watchdog 只管
// bot 座位(IsBotSeatTurn),手牌永久停滞(实测 ≥10min),且 vacancy timer
// 被 bot 的 DB 行卡住无法删房,形成「纯 bot 僵尸房」。
//
// 修复:TexasHoldemManager.ForceRemovePlayer —— 轮到该座位时走
// ApplyAction(fold) 正道推进回合;未轮到则直接标记 Folded;两种情况都把
// 座位从引擎与 r.Seats 摘除,防止后续手牌给幽灵座位发牌。
package texasholdem

import "testing"

// 构造 1 人类(seat 0) + 2 bot(seat 1/2) 的进行中房间,返回 manager 与房间。
func newForceRemoveRoom(t *testing.T) (*TexasHoldemManager, *TexasHoldemRoom) {
	t.Helper()
	mgr := NewTexasHoldemManager()
	const roomID = "room-force-remove"
	mgr.RegisterBotSeats(roomID, map[int]string{1: "ModelA", 2: "ModelB"})
	if _, _, e := mgr.JoinGameAtSeat(roomID, "human-1", 0); e != nil {
		t.Fatalf("join human: %v", e)
	}
	if _, _, e := mgr.JoinGameAtSeat(roomID, "bot-a", 1); e != nil {
		t.Fatalf("join bot-a: %v", e)
	}
	if _, started, e := mgr.JoinGameAtSeat(roomID, "bot-b", 2); e != nil || !started {
		t.Fatalf("join bot-b: started=%v err=%v", started, e)
	}
	r := mgr.GetRoomForBot(roomID)
	if r == nil || r.State == nil || r.State.Street == PhaseWaiting {
		t.Fatal("hand should be in progress after all seats occupied")
	}
	return mgr, r
}

// TestForceRemovePlayer_TurnSeatAdvancesHand: 回合正好停在断线人类座位上时,
// 强制移除必须推进回合到下一个可行动座位(核心 stall 场景)。
func TestForceRemovePlayer_TurnSeatAdvancesHand(t *testing.T) {
	mgr, r := newForceRemoveRoom(t)
	const roomID = "room-force-remove"

	// 把回合强制摆到人类座位(seat 0),模拟「轮到人类时断线」。
	r.mu.Lock()
	r.State.Turn = 0
	handBefore := r.State.HandNumber
	r.mu.Unlock()

	removed, handOver, handNum, delta := mgr.ForceRemovePlayer(roomID, "human-1")
	if !removed {
		t.Fatal("ForceRemovePlayer should report removed=true for seated human")
	}
	if handNum != handBefore {
		t.Errorf("handNum = %d, want %d", handNum, handBefore)
	}
	if delta > 0 {
		t.Errorf("delta = %d, want <= 0 (mid-hand removal can never have won the pot)", delta)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Players[0].UserID != "" {
		t.Error("seat 0 should be cleared from engine Players")
	}
	if r.Seats[0] != "" {
		t.Error("seat 0 should be cleared from room Seats")
	}
	if !handOver {
		// 手牌未结束 → 回合必须已离开 seat 0(否则仍在幽灵座位上停滞)。
		if r.State.Turn == 0 {
			t.Error("turn still on removed seat 0 — stall not fixed")
		}
		if r.State.Turn < 0 || r.State.Turn >= MaxPlayers || !r.BotSeats[r.State.Turn] {
			t.Errorf("turn = %d, want a bot seat (1 or 2)", r.State.Turn)
		}
	} else {
		if r.State.Status == StatusPlaying {
			t.Error("handOver=true but status still playing")
		}
	}
}

// TestForceRemovePlayer_NonTurnSeatFolds: 未轮到断线人类时,移除应标记弃牌
// 且不影响当前回合座位。
func TestForceRemovePlayer_NonTurnSeatFolds(t *testing.T) {
	mgr, r := newForceRemoveRoom(t)
	const roomID = "room-force-remove"

	// 把回合摆到 bot 座位(seat 1),人类(seat 0)未行动。
	r.mu.Lock()
	r.State.Turn = 1
	r.mu.Unlock()

	removed, _, _, _ := mgr.ForceRemovePlayer(roomID, "human-1")
	if !removed {
		t.Fatal("ForceRemovePlayer should report removed=true")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Turn != 1 {
		t.Errorf("turn = %d, want 1 (non-turn removal must not disturb current turn)", r.State.Turn)
	}
	if r.State.Players[0].UserID != "" || r.Seats[0] != "" {
		t.Error("seat 0 should be fully detached")
	}
}

// TestForceRemovePlayer_LastTwoPlayersEndsHand: 只剩 2 名未弃牌玩家时移除其中
// 一人,手牌必须立即以 endHandFold 收尾并结算给存活者。
func TestForceRemovePlayer_LastTwoPlayersEndsHand(t *testing.T) {
	mgr, r := newForceRemoveRoom(t)
	const roomID = "room-force-remove"

	// 先让 seat 2 的 bot 弃牌,只剩 human(0) + bot(1);再把回合摆到 bot(1),
	// 模拟「未轮到的人类断线,移除后只剩 1 人」。
	r.mu.Lock()
	r.State.Players[2].Folded = true
	r.State.Turn = 1
	potBefore := r.State.Pot
	winnerStackBefore := r.State.Players[1].Stack
	r.mu.Unlock()

	removed, handOver, _, _ := mgr.ForceRemovePlayer(roomID, "human-1")
	if !removed || !handOver {
		t.Fatalf("removed=%v handOver=%v, want both true (last-fold ends hand)", removed, handOver)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.State.Status == StatusPlaying {
		t.Error("hand should be over after last opponent removed")
	}
	if got := r.State.Players[1].Stack; got != winnerStackBefore+potBefore {
		t.Errorf("winner stack = %d, want %d (pot awarded)", got, winnerStackBefore+potBefore)
	}
}

// TestForceRemovePlayer_RejectsBotAndStranger: bot 座位与不在座用户不得被
// 此路径移除(bot 无 hub 连接,永不产生断线超时;不在座用户是调用方误调)。
func TestForceRemovePlayer_RejectsBotAndStranger(t *testing.T) {
	mgr, _ := newForceRemoveRoom(t)
	const roomID = "room-force-remove"

	if removed, _, _, _ := mgr.ForceRemovePlayer(roomID, "bot-a"); removed {
		t.Error("bot seat must not be force-removed via disconnect path")
	}
	if removed, _, _, _ := mgr.ForceRemovePlayer(roomID, "no-such-user"); removed {
		t.Error("non-seated user must report removed=false")
	}
	if removed, _, _, _ := mgr.ForceRemovePlayer("no-such-room", "human-1"); removed {
		t.Error("unknown room must report removed=false")
	}
}

// TestForceRemovePlayer_NextHandSkipsRemovedSeat: 移除后开新一手,幽灵座位
// 不得再被发牌/纳入盲注轮转(防止「下手牌再次卡在空座」二次 stall)。
func TestForceRemovePlayer_NextHandSkipsRemovedSeat(t *testing.T) {
	mgr, r := newForceRemoveRoom(t)
	const roomID = "room-force-remove"

	if removed, _, _, _ := mgr.ForceRemovePlayer(roomID, "human-1"); !removed {
		t.Fatal("remove failed")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.State.CanStartHand() {
		t.Skip("remaining bots cannot start next hand (busted?) — nothing to assert")
	}
	r.snapshotHandStartStacks()
	if e := r.State.StartHand(); e != nil {
		t.Fatalf("start next hand after removal: code=%d msg=%s", e.Code, e.Message)
	}
	if r.State.Players[0].UserID != "" {
		t.Error("removed seat 0 must stay empty in the next hand")
	}
	// 引擎 StartHand 的重置循环跳过空位,空座上的旧底牌是残留的展示数据
	//(不会被 dealHoleCards 覆盖,也不进 turn queue)—— 关键是回合与盲注
	// 轮转不得再落在该座位上。
	if r.State.Turn == 0 {
		t.Error("next hand turn landed on removed seat 0")
	}
}
