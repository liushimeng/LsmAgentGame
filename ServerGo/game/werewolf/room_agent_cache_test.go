package werewolf

import (
	"testing"

	"LsmAgentGame/agent/wwtypes"
)

// TestStaticContextCache_HitMiss 验证静态缓存的命中与未命中。
func TestStaticContextCache_HitMiss(t *testing.T) {
	r := &WerewolfRoom{}
	calls := 0
	builder := func() *wwtypes.StaticContext {
		calls++
		return &wwtypes.StaticContext{MySeat: 0, SeatCount: 13}
	}

	// 首次调用:未命中,调用 builder
	sc1 := getStaticContext(r, 0, builder)
	if sc1 == nil || sc1.SeatCount != 13 {
		t.Fatalf("expected SeatCount=13, got %v", sc1)
	}
	if calls != 1 {
		t.Fatalf("expected 1 builder call, got %d", calls)
	}

	// 二次调用:命中缓存,不调用 builder
	sc2 := getStaticContext(r, 0, builder)
	if sc2 != sc1 {
		t.Fatal("expected same pointer from cache")
	}
	if calls != 1 {
		t.Fatalf("expected still 1 builder call (cached), got %d", calls)
	}

	// 不同 seat:未命中,调用 builder
	sc3 := getStaticContext(r, 1, builder)
	if sc3 == sc1 {
		t.Fatal("expected different pointer for different seat")
	}
	if calls != 2 {
		t.Fatalf("expected 2 builder calls, got %d", calls)
	}
}

// TestPhaseStateCache_InvalidateOnChange 验证阶段变化时缓存失效。
func TestPhaseStateCache_InvalidateOnChange(t *testing.T) {
	r := &WerewolfRoom{}
	calls := 0
	builder := func() *wwtypes.PhaseStateContext {
		calls++
		return &wwtypes.PhaseStateContext{Phase: "speak", DivineCnt: 3}
	}

	// 首次调用:未命中
	psc1 := getPhaseStateContext(r, 0, "speak", builder)
	if psc1 == nil || psc1.DivineCnt != 3 {
		t.Fatalf("expected DivineCnt=3, got %v", psc1)
	}
	if calls != 1 {
		t.Fatalf("expected 1 builder call, got %d", calls)
	}

	// 同阶段:命中缓存
	psc2 := getPhaseStateContext(r, 0, "speak", builder)
	if psc2 != psc1 {
		t.Fatal("expected same pointer from cache")
	}
	if calls != 1 {
		t.Fatalf("expected still 1 builder call (cached), got %d", calls)
	}

	// 阶段变化:失效缓存,重新构建
	psc3 := getPhaseStateContext(r, 0, "vote", builder)
	if psc3 == psc1 {
		t.Fatal("expected different pointer after phase change")
	}
	if calls != 2 {
		t.Fatalf("expected 2 builder calls after phase change, got %d", calls)
	}

	// 回到旧阶段:再次失效(因为 phaseStatePhase 已变为 "vote")
	psc4 := getPhaseStateContext(r, 0, "speak", builder)
	if psc4 == psc3 {
		// OK - new build
	}
	if calls != 3 {
		t.Fatalf("expected 3 builder calls, got %d", calls)
	}
}

// TestInvalidateContextCaches 验证缓存失效函数。
func TestInvalidateContextCaches(t *testing.T) {
	r := &WerewolfRoom{}
	// 先填充缓存
	getStaticContext(r, 0, func() *wwtypes.StaticContext {
		return &wwtypes.StaticContext{MySeat: 0}
	})
	getPhaseStateContext(r, 0, "speak", func() *wwtypes.PhaseStateContext {
		return &wwtypes.PhaseStateContext{Phase: "speak"}
	})

	if len(r.staticContextCache) != 1 {
		t.Fatalf("expected 1 static cache entry, got %d", len(r.staticContextCache))
	}
	if len(r.phaseStateCache) != 1 {
		t.Fatalf("expected 1 phase cache entry, got %d", len(r.phaseStateCache))
	}

	// 失效
	invalidateContextCaches(r)

	if len(r.staticContextCache) != 0 {
		t.Fatalf("expected 0 static cache entries after invalidate, got %d", len(r.staticContextCache))
	}
	if len(r.phaseStateCache) != 0 {
		t.Fatalf("expected 0 phase cache entries after invalidate, got %d", len(r.phaseStateCache))
	}
	if r.phaseStatePhase != "" {
		t.Fatalf("expected empty phaseStatePhase, got %q", r.phaseStatePhase)
	}
}

// TestWinConditionFor 验证胜利条件描述。
func TestWinConditionFor(t *testing.T) {
	tests := []struct {
		role     Role
		faction  Faction
		expected string
	}{
		{RoleWerewolf, FactionWolf, "狼人屠边"},
		{RoleVillager, FactionGood, "放逐全部 4 狼人"},
		{RoleSeer, FactionGood, "放逐全部 4 狼人"},
		{RoleWerewolf, FactionGood, "异常状态"}, // 异常情况
		{RoleVillager, FactionWolf, "狼人屠边"},  // 狼人阵营
	}
	for _, tc := range tests {
		got := winConditionFor(tc.role, tc.faction)
		if !stringContains(got, tc.expected) {
			t.Errorf("winConditionFor(%v, %v) = %q, want containing %q", tc.role, tc.faction, got, tc.expected)
		}
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
