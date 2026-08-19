package texasholdem

import "testing"

// 2026-08-19 §德州扑克盲注透传 — 房间级盲注/买入配置回归测试。

// TestRoomConfig_001_DefaultWhenUnset: 未 SetRoomConfig 时 JoinGame 走 manager
// 默认值(BigBlind=200 / StartStack=10000)。
func TestRoomConfig_001_DefaultWhenUnset(t *testing.T) {
	m := NewTexasHoldemManager()
	r, _, e := m.JoinGame("room-default", "user-a")
	if e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	if r.State.BigBlind != 200 {
		t.Fatalf("default BigBlind = %d, want 200", r.State.BigBlind)
	}
	if got := r.State.Players[0].Stack; got != 10000 {
		t.Fatalf("default StartStack = %d, want 10000", got)
	}
}

// TestRoomConfig_002_OverrideApplies: SetRoomConfig 后 JoinGame 使用覆盖值。
func TestRoomConfig_002_OverrideApplies(t *testing.T) {
	m := NewTexasHoldemManager()
	m.SetRoomConfig("room-cfg", 50, 2500)
	r, _, e := m.JoinGame("room-cfg", "user-a")
	if e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	if r.State.BigBlind != 50 {
		t.Fatalf("override BigBlind = %d, want 50", r.State.BigBlind)
	}
	if got := r.State.Players[0].Stack; got != 2500 {
		t.Fatalf("override StartStack = %d, want 2500", got)
	}
	// 其他房间不受影响,仍走默认值。
	r2, _, e := m.JoinGame("room-other", "user-b")
	if e != nil {
		t.Fatalf("JoinGame(other) failed: %v", e)
	}
	if r2.State.BigBlind != 200 {
		t.Fatalf("unrelated room BigBlind = %d, want 200", r2.State.BigBlind)
	}
}

// TestRoomConfig_003_RemoveGameCleansConfig: RemoveGame 同步清理房间级配置,
// 同名房间重建后回退默认值(防 map 泄漏 / 配置串房)。
func TestRoomConfig_003_RemoveGameCleansConfig(t *testing.T) {
	m := NewTexasHoldemManager()
	m.SetRoomConfig("room-x", 1000, 50000)
	if _, _, e := m.JoinGame("room-x", "user-a"); e != nil {
		t.Fatalf("JoinGame failed: %v", e)
	}
	m.RemoveGame("room-x")
	r, _, e := m.JoinGame("room-x", "user-a")
	if e != nil {
		t.Fatalf("re-JoinGame failed: %v", e)
	}
	if r.State.BigBlind != 200 {
		t.Fatalf("after RemoveGame BigBlind = %d, want default 200", r.State.BigBlind)
	}
	if got := r.State.Players[0].Stack; got != 10000 {
		t.Fatalf("after RemoveGame StartStack = %d, want default 10000", got)
	}
}
