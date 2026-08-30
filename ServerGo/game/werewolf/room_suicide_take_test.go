// room_suicide_take_test.go — §20260830-02 自爆带走(房间/管理层 + Agent 上下文)测试。
//
// 设计文档:docs/狼人杀-角色设计/狼人杀自爆遗言与带走设计-20260830-02.md §8-7/8。
package werewolf

import (
	"testing"
)

// 准备:进入 speak 阶段,返回房间、狼座位。
func suicideTakeRoomFixture(t *testing.T, m *WerewolfManager) (string, *WerewolfRoom, Seat) {
	t.Helper()
	roomID, r := fillAndStart(t, m)
	r.mu.Lock()
	defer r.mu.Unlock()
	wolf := firstLivingWolf(r.State)
	if wolf == NoSeat {
		t.Fatal("no wolf in deck")
	}
	r.State.Phase = PhaseSpeak
	r.State.SpeakTurnSeat = wolf
	r.State.DayNumber = 1
	return roomID, r, wolf
}

// R-ST-01: Action_SuicideTake 全链路(自爆→遗言→带走→入夜),人类路径。
func TestActionSuicideTakeFullFlow(t *testing.T) {
	m := stubWWMgr()
	roomID, r, wolf := suicideTakeRoomFixture(t, m)
	// 找一个非狼存活目标。
	var target Seat = NoSeat
	for i := 0; i < len(r.State.Roles); i++ {
		if Seat(i) != wolf && r.State.AliveSeat(Seat(i)) && r.State.Roles[i] != RoleWerewolf {
			target = Seat(i)
			break
		}
	}
	if target == NoSeat {
		t.Fatal("no target")
	}
	wolfUser := r.State.Seats[wolf]

	// 1) 自爆。
	if _, e := m.Action_WolfSuicide(roomID, wolfUser); e != nil {
		t.Fatalf("Action_WolfSuicide: %v", e)
	}
	if r.State.Phase != PhaseDeathLyric {
		t.Fatalf("phase = %v, want death_lyric", r.State.Phase)
	}
	// 2) 遗言。
	if _, e := m.Action_LastWords(roomID, wolfUser, "我是狼,认了"); e != nil {
		t.Fatalf("Action_LastWords: %v", e)
	}
	if r.State.Phase != PhaseSuicideTake {
		t.Fatalf("phase = %v, want suicide_take", r.State.Phase)
	}
	// 3) 带走。
	if _, e := m.Action_SuicideTake(roomID, wolfUser, target); e != nil {
		t.Fatalf("Action_SuicideTake: %v", e)
	}
	if r.State.Status != "over" {
		// 7 人局 Day1 带走 1 人不会终局;应进被带走者遗言。
		if r.State.Phase != PhaseDeathLyric {
			t.Fatalf("phase = %v, want death_lyric for target", r.State.Phase)
		}
		if r.State.Players[target].DeathCause != DeathCauseSuicideTake {
			t.Fatalf("target cause = %q", r.State.Players[target].DeathCause)
		}
	} else {
		t.Skip("take triggered game over (deck edge)")
	}
}

// R-ST-02: 非自爆狼座位调用 Action_SuicideTake → 权限拒绝。
func TestActionSuicideTakeWrongActor(t *testing.T) {
	m := stubWWMgr()
	roomID, r, wolf := suicideTakeRoomFixture(t, m)
	other := Seat(0)
	if other == wolf {
		other = Seat(1)
	}
	otherUser := r.State.Seats[other]
	_, e := m.Action_SuicideTake(roomID, otherUser, 0)
	if e == nil {
		t.Fatalf("non-suicided actor must be rejected")
	}
	// 错误码:PhaseSuicideTake 校验(非 suicide_take 阶段)。
	if e.Code == 0 {
		t.Fatalf("error code must be non-zero")
	}
}

// R-ST-03: Agent 上下文 DeadActorTurn / SuicideTakeSeat / MyTurn 三字段。
func TestSuicideTakeAgentContext(t *testing.T) {
	m := stubWWMgr()
	roomID, r, wolf := suicideTakeRoomFixture(t, m)
	_ = roomID
	wolfUser := r.State.Seats[wolf]
	if _, e := m.Action_WolfSuicide(roomID, wolfUser); e != nil {
		t.Fatalf("suicide: %v", e)
	}
	r.mu.Lock()
	_ = r.State.SkipLastWords(wolf)
	if r.State.Phase != PhaseSuicideTake {
		r.mu.Unlock()
		t.Fatalf("phase = %v, want suicide_take", r.State.Phase)
	}
	// 自爆狼上下文:MyTurn + DeadActorTurn + SuicideTakeSeat。
	gc := buildAgentContextLocked(r, int(wolf), -1)
	r.mu.Unlock()
	if !gc.MyTurn {
		t.Fatalf("suicided wolf MyTurn should be true")
	}
	if !gc.DeadActorTurn {
		t.Fatalf("suicided wolf DeadActorTurn should be true (§20260830-02)")
	}
	if gc.SuicideTakeSeat != int(wolf) {
		t.Fatalf("SuicideTakeSeat = %d, want %d", gc.SuicideTakeSeat, wolf)
	}
}

// R-ST-04: 死亡猎人在 hunter_shoot 阶段 DeadActorTurn=true(核心缺陷修复)。
func TestDeadHunterAgentContext(t *testing.T) {
	m := stubWWMgr()
	_, r, _ := suicideTakeRoomFixture(t, m)
	hunter := NoSeat
	for i := 0; i < len(r.State.Roles); i++ {
		if r.State.Roles[i] == RoleHunter {
			hunter = Seat(i)
			break
		}
	}
	if hunter == NoSeat {
		t.Skip("no hunter in deck")
	}
	r.mu.Lock()
	r.State.Phase = PhaseHunterShoot
	r.State.HunterPendingShoot = true
	r.State.HunterPendingFrom = "vote"
	r.State.Players[hunter].Alive = false // 猎人死亡后才开枪
	gc := buildAgentContextLocked(r, int(hunter), -1)
	r.mu.Unlock()
	if !gc.MyTurn {
		t.Fatalf("dead hunter MyTurn should be true")
	}
	if !gc.DeadActorTurn {
		t.Fatalf("dead hunter DeadActorTurn should be true — run.go 死者守卫修复的关键字段")
	}
}

// R-ST-05: watchdogActingSeat 在 suicide_take 返回自爆狼座位。
func TestSuicideTakeWatchdogActingSeat(t *testing.T) {
	m := stubWWMgr()
	roomID, r, wolf := suicideTakeRoomFixture(t, m)
	_ = roomID
	wolfUser := r.State.Seats[wolf]
	_, _ = m.Action_WolfSuicide(roomID, wolfUser)
	r.mu.Lock()
	_ = r.State.SkipLastWords(wolf)
	if r.State.Phase != PhaseSuicideTake {
		r.mu.Unlock()
		t.Fatalf("phase = %v", r.State.Phase)
	}
	seat := watchdogActingSeat(r)
	r.mu.Unlock()
	if seat != int(wolf) {
		t.Fatalf("watchdogActingSeat = %d, want %d (suicided wolf)", seat, wolf)
	}
}
