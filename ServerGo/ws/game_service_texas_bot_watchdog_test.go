// game_service_texas_bot_watchdog_test.go — §B8 bot 回合 watchdog 回归测试(2026-08-20)。
//
// 设计 §7:「30s 同 actingSeat 强制 fold」。watchdog tick 是纯同步函数,
// 直接驱动 manager + GameService 最小构造(hub/driver 真实,LLM registry 不需要)。
package ws

import (
	"testing"
	"time"

	"LsmAgentGame/agent/thpagent"
	"LsmAgentGame/game/texasholdem"
)

// newWatchdogTestService 构造最小可用 GameService(仅 watchdog tick 依赖的字段)。
func newWatchdogTestService() (*GameService, *texasholdem.TexasHoldemManager) {
	mgr := texasholdem.NewTexasHoldemManager()
	s := &GameService{
		hub:                 NewHub(),
		texasHoldemMgr:      mgr,
		thpDriver:           thpagent.NewDriver(),
		thpActionTimeoutSec: 1,
	}
	return s, mgr
}

// setupBotHand 开一桌 2 bot 对局,返回 (room, 当前回合座位)。
func setupBotHand(t *testing.T, mgr *texasholdem.TexasHoldemManager, roomID string) (*texasholdem.TexasHoldemRoom, int) {
	t.Helper()
	if _, _, e := mgr.JoinGame(roomID, "bot-a"); e != nil {
		t.Fatalf("join a: %v", e)
	}
	r, _, e := mgr.JoinGame(roomID, "bot-b")
	if e != nil {
		t.Fatalf("join b: %v", e)
	}
	r.RegisterBotSeatsLocked(map[int]string{0: "ModelA", 1: "ModelB"})
	if r.State == nil || r.State.Street == texasholdem.PhaseWaiting {
		t.Fatal("hand should have auto-started")
	}
	return r, r.State.Turn
}

// TestB8_WatchdogForceFoldStuckBot: 回合停滞超 timeout+10s → 强制 fold。
// 双人局一方 fold 即手牌结束(handOver),验证 Folded + Street=over。
func TestB8_WatchdogForceFoldStuckBot(t *testing.T) {
	s, mgr := newWatchdogTestService()
	r, turn := setupBotHand(t, mgr, "room-wd-stuck")

	// 把该回合时钟拨到 1 分钟前(模拟 ProcessBotTurn 触发链断裂)
	r.TurnStartedAt = time.Now().Add(-time.Minute)
	r.TurnStartSeat = turn

	if keep := s.texasBotWatchdogTick("room-wd-stuck"); !keep {
		t.Fatal("tick should keep running for existing room")
	}

	if !r.State.Players[turn].Folded {
		t.Errorf("stuck bot seat %d should be force-folded", turn)
	}
	if r.State.Street != texasholdem.PhaseOver {
		t.Errorf("2-player fold should end hand, street = %s", r.State.Street)
	}
}

// TestB8_WatchdogSkipsFreshTurn: 回合刚开始(< timeout)不干预。
func TestB8_WatchdogSkipsFreshTurn(t *testing.T) {
	s, mgr := newWatchdogTestService()
	r, turn := setupBotHand(t, mgr, "room-wd-fresh")
	_ = turn
	// TurnStartedAt 由 JoinGame → markTurnLocked 置为当前时间
	if r.TurnStartedAt.IsZero() {
		t.Fatal("markTurnLocked should stamp turn start at hand start")
	}
	if keep := s.texasBotWatchdogTick("room-wd-fresh"); !keep {
		t.Fatal("tick should keep running")
	}
	if r.State.Players[r.State.Turn].Folded {
		t.Error("fresh turn must not be folded")
	}
}

// TestB8_WatchdogExitsWhenRoomGone: 房间删除后 tick 返回 false(watchdog 自退,防泄漏)。
func TestB8_WatchdogExitsWhenRoomGone(t *testing.T) {
	s, _ := newWatchdogTestService()
	if keep := s.texasBotWatchdogTick("no-such-room"); keep {
		t.Error("tick should return false for missing room (watchdog self-exit)")
	}
}

// TestB8_WatchdogSkipsHumanTurn: 当前回合是人类座位时不干预。
func TestB8_WatchdogSkipsHumanTurn(t *testing.T) {
	s, mgr := newWatchdogTestService()
	r, turn := setupBotHand(t, mgr, "room-wd-human")
	// 把当前回合座位改为「非 bot」:清掉该座位的 bot 标记,回合一分钟前开始
	r.BotSeats[turn] = false
	r.TurnStartedAt = time.Now().Add(-time.Minute)
	if keep := s.texasBotWatchdogTick("room-wd-human"); !keep {
		t.Fatal("tick should keep running")
	}
	if r.State.Players[turn].Folded {
		t.Error("human turn must never be force-folded by bot watchdog")
	}
}

// TestB8_CleanupRuntimeStopsWatchdog: cleanupTexasHoldemBotRuntime 取消 watchdog +
// 清串行守卫 + 注销 driver 座位(UnregisterAgents 的生产接线验证,§130 防御)。
func TestB8_CleanupRuntimeStopsWatchdog(t *testing.T) {
	s, mgr := newWatchdogTestService()
	setupBotHand(t, mgr, "room-wd-clean")
	if e := s.thpDriver.RegisterAgents("room-wd-clean", []thpagent.SeatConfig{
		{Seat: 0, UserID: "bot-a", ModelKey: "ModelA", ModelName: "ModelA"},
	}); e != nil {
		t.Fatal(e)
	}
	s.startTexasBotWatchdog("room-wd-clean")
	if _, ok := s.thpWatchdogs.Load("room-wd-clean"); !ok {
		t.Fatal("watchdog should be registered")
	}
	s.cleanupTexasHoldemBotRuntime("room-wd-clean")
	if _, ok := s.thpWatchdogs.Load("room-wd-clean"); ok {
		t.Error("watchdog should be removed after cleanup")
	}
	if got := s.thpDriver.GetAgentCountForRoom("room-wd-clean"); got != 0 {
		t.Errorf("agents should be unregistered after cleanup, got %d", got)
	}
	if _, ok := s.thpTurnGuards.Load("room-wd-clean"); ok {
		t.Error("turn guard should be removed after cleanup")
	}
}
