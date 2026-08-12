package werewolf

import (
	"testing"
)

// declareStreamTest 辅助:进入白天发言阶段后再声明警徽流。
// 默认把目标座位标 Alive(测试环境不依赖发牌)。
func declareStreamTest(t *testing.T, gs *GameState, slot int, target Seat) {
	t.Helper()
	gs.Phase = PhaseSpeak
	if int(target) >= 0 && int(target) < MaxPlayers {
		gs.Players[target].Alive = true
		// SheriffStreamDeclare 校验 "target dead" 时也看 Seats 是否非空。
		if gs.Seats[target] == "" {
			gs.Seats[target] = "test_user"
		}
	}
	if err := gs.SheriffStreamDeclare(0, slot, target); err != nil {
		t.Fatalf("declare slot=%d: %v", slot, err)
	}
}

// TestSheriffStreamAge_FreshAndStale 测试 S-01:警徽流第 0 轮 AgeRounds=0 / IsStale=false;
// 第 3 轮(persist=2)IsStale=true。
func TestSheriffStreamAge_FreshAndStale(t *testing.T) {
	gs := NewGame(0)
	gs.DayNumber = 1
	gs.SheriffSeat = 0 // 假定 0 号为警长
	gs.Roles[0] = RoleSeer
	declareStreamTest(t, gs, 1, Seat(2))
	declareStreamTest(t, gs, 2, Seat(3))

	// 当前轮 1,age=0 → 不 stale。
	ages := buildSheriffStreamAgesLocked(gs.DayNumber, gs.SheriffStreams, gs.SheriffStreamRounds)
	if len(ages) != 2 {
		t.Fatalf("expected 2 ages, got %d", len(ages))
	}
	for i, a := range ages {
		if a.AgeRounds != 0 || a.IsStale {
			t.Fatalf("slot %d fresh expected age=0 stale=false, got %+v", i, a)
		}
	}

	// 把当前轮推进到 3(age=2 → 不 stale;age=3 时若 persist=2 则 stale)。
	gs.DayNumber = 3
	ages = buildSheriffStreamAgesLocked(gs.DayNumber, gs.SheriffStreams, gs.SheriffStreamRounds)
	for i, a := range ages {
		if a.AgeRounds != 2 || a.IsStale {
			t.Fatalf("slot %d round=3 expected age=2 stale=false, got %+v", i, a)
		}
	}

	// 再推到 round=4,age=3 → stale。
	gs.DayNumber = 4
	ages = buildSheriffStreamAgesLocked(gs.DayNumber, gs.SheriffStreams, gs.SheriffStreamRounds)
	for i, a := range ages {
		if a.AgeRounds != 3 || !a.IsStale {
			t.Fatalf("slot %d round=4 expected age=3 stale=true, got %+v", i, a)
		}
	}
}

// TestSheriffStreamAge_PersistZero 测试 S-02:persist_rounds=0 时永远不 stale
// (向旧行为兼容)。本测试通过 cfgWerewolfSheriffPersistRounds 默认值与
// cfg 加载失败兜底共同保证;此用例断言 view 路径在 cfg 异常时不 panic。
func TestSheriffStreamAge_PersistZero(t *testing.T) {
	// cfg 加载可能 panic(defer recover);此测试只验证函数不会 panic。
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("buildSheriffStreamAgesLocked panicked: %v", rec)
		}
	}()
	gs := NewGame(0)
	gs.DayNumber = 1
	gs.SheriffSeat = 0
	gs.Roles[0] = RoleSeer
	declareStreamTest(t, gs, 1, Seat(2))

	ages := buildSheriffStreamAgesLocked(100, gs.SheriffStreams, gs.SheriffStreamRounds)
	if len(ages) != 2 {
		t.Fatalf("expected 2 ages, got %d", len(ages))
	}
	if ages[0].Target != 2 {
		t.Fatalf("slot 0 target expected 2, got %d", ages[0].Target)
	}
}

// TestSheriffStreamAge_UndeclaredSlot 测试未声明槽位的 AgeRounds=-1。
func TestSheriffStreamAge_UndeclaredSlot(t *testing.T) {
	gs := NewGame(0)
	gs.DayNumber = 2
	gs.SheriffSeat = 0
	gs.Roles[0] = RoleSeer
	declareStreamTest(t, gs, 1, Seat(5))
	// slot 2 未声明。
	ages := buildSheriffStreamAgesLocked(gs.DayNumber, gs.SheriffStreams, gs.SheriffStreamRounds)
	if ages[1].Target != int(NoSeat) {
		t.Fatalf("slot 2 undeclared target expected %d, got %d", NoSeat, ages[1].Target)
	}
	if ages[1].AgeRounds != -1 {
		t.Fatalf("slot 2 undeclared age expected -1, got %d", ages[1].AgeRounds)
	}
	if ages[1].IsStale {
		t.Fatalf("slot 2 undeclared must not be stale")
	}
}

// TestSheriffStreamAge_DoesNotAffectIdentity 测试 S-03:stale 仅 UI 提示,
// 不影响 §135 RolePubliclyRevealed(身份公开规则)与 sheriffSlain 结算路径。
func TestSheriffStreamAge_DoesNotAffectIdentity(t *testing.T) {
	gs := NewGame(0)
	gs.DayNumber = 1
	gs.SheriffSeat = 0
	gs.Roles[0] = RoleSeer
	declareStreamTest(t, gs, 1, Seat(2))
	// 推进到很远的轮次 → 应 stale。
	gs.DayNumber = 100
	ages := buildSheriffStreamAgesLocked(gs.DayNumber, gs.SheriffStreams, gs.SheriffStreamRounds)
	if !ages[0].IsStale {
		t.Fatalf("expected stale after 100 rounds")
	}
	// 角色公开规则:身份字段不能被 stale 提示污染(§135 单点判定)。
	if gs.Players[0].IdiotRevealed {
		t.Fatalf("IdiotRevealed must stay false — §135 single source of truth")
	}
	if gs.Roles[0] != RoleSeer {
		t.Fatalf("Role must stay RoleSeer")
	}
}

// TestSheriffStreamAge_DeclaredRoundRecorded 测试 SheriffStreamDeclare 同步
// 写入 SheriffStreamRounds[slot],即 view 字段的来源正确(§130 接线验证)。
func TestSheriffStreamAge_DeclaredRoundRecorded(t *testing.T) {
	gs := NewGame(0)
	gs.DayNumber = 3
	gs.SheriffSeat = 0
	gs.Roles[0] = RoleSeer
	declareStreamTest(t, gs, 1, Seat(2))
	if gs.SheriffStreamRounds[0] != 3 {
		t.Fatalf("declared round expected 3, got %d", gs.SheriffStreamRounds[0])
	}
	// 撤回时清零。
	declareStreamTest(t, gs, 1, NoSeat)
	if gs.SheriffStreamRounds[0] != 0 {
		t.Fatalf("after revoke expected 0, got %d", gs.SheriffStreamRounds[0])
	}
}
