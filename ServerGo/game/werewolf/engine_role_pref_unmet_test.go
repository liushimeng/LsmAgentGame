package werewolf

// engine_role_pref_unmet_test.go — 2026-08-11 BUG-ROLE-MISMATCH-P0 回归测试。
//
// 背景(自动化测试报告 自动化测试报告_20260810_214758.md):
//   人类玩家在 RoomCreateModal 选「猎人」(creator_role=hunter),实测发牌得到
//   「猎魔人」(demon_hunter)。根因是 13 人随机牌组(4狼+1预言家+2~3神职+平民)
//   只有 2~3 张神职牌,骑士/守卫/猎魔人/猎人常缺席;玩家偏好 hunter 但牌组
//   无 hunter 时,ApplyPreferredRoles 返回 unmet,座位降级为随机角色。
//   此前 unmet 仅记 Warn 日志(fail-quiet),玩家全程无任何可感知信号 ——
//   既无 WS 帧也无 UI 提示,只能实测发现「所选 ≠ 实际身份」。
//
// 修复(engine.go / view.go):
//   1. GameState 新增 PreferredRolesUnmet []int —— StartGame 内 unmet 直接落地。
//   2. BuildClientState 对本人下发 my_preferred_role + my_role_pref_unmet。
//   3. 前端 useWerewolf 收到后弹全局 toast + GameInfoPanel 就地提示。
//
// 本测试覆盖引擎 + 视图层契约:
//   U-01  牌组有所选角色 → unmet 为空,view 不下发两字段
//   U-02  牌组无所选角色(猎人) → PreferredRolesUnmet 记录该座位,
//         本人视角 my_preferred_role=hunter + my_role_pref_unmet=true
//   U-03  其他玩家视角 **绝不** 下发这两字段(身份保密 §135)
//   U-04  观战者视角 **绝不** 下发这两字段(身份保密 §135)
//   U-05  已满足偏好的座位,my_role_pref_unmet 必须为 false(不误报)

import "testing"

// makeStartedGame13WithPrefs 启动一局 13 人局并注入座位角色偏好。
// 用 AddPlayerAt 按座位入座(等价生产 RegisterAgentSeats / ManagerAddPlayerAt),
// 避免 AddPlayer 的"首个空位"语义在满座时与 SeatCount 判定交错。
func makeStartedGame13WithPrefs(t *testing.T, seed int64, users [MaxPlayers]string, prefs map[int]Role) *GameState {
	t.Helper()
	gs := NewGame(seed)
	gs.SeatCount = MaxPlayers
	for i, u := range users {
		if u == "" {
			continue
		}
		if _, e := gs.AddPlayerAt(u, Seat(i)); e != nil {
			t.Fatalf("add player %d: %v", i, e)
		}
	}
	gs.PreferredRoles = prefs
	if e := gs.StartGame(); e != nil {
		t.Fatalf("start: %v", e)
	}
	return gs
}

// findSeatWithRole 返回 13 人局中指定角色的座位;不存在返回 -1。
func findSeatWithRole(gs *GameState, want Role) int {
	for i := 0; i < MaxPlayers; i++ {
		if gs.Seats[i] != "" && gs.Roles[i] == want {
			return i
		}
	}
	return -1
}

// U-01: 牌组有所选角色(预言家每局必有) → unmet 为空,view 不下发两字段。
func TestRolePrefUnmet_U01_MetPrefNoFields(t *testing.T) {
	gs := makeStartedGame13WithPrefs(t, 42, fullSeats13(), map[int]Role{3: RoleSeer})
	if len(gs.PreferredRolesUnmet) != 0 {
		t.Fatalf("U-01: seer is guaranteed in 13p deck, unmet=%v want empty", gs.PreferredRolesUnmet)
	}
	cs := BuildClientState("r1", gs.Seats, 3, gs)
	if cs.MyPreferredRole != "" || cs.MyRolePrefUnmet {
		t.Fatalf("U-01: met pref must not surface unmet fields, got pref=%q unmet=%v",
			cs.MyPreferredRole, cs.MyRolePrefUnmet)
	}
}

// U-02: 牌组无所选角色(猎人) → PreferredRolesUnmet 记录该座位 + 本人视角两字段。
// 13 人随机牌组神职 2~3 张、从 6 神职池抽,多数牌组无 hunter;遍历 seed
// 找一副「无 hunter」的牌组做确定性断言。
func TestRolePrefUnmet_U02_UnmetPrefSurfacesToSelf(t *testing.T) {
	seed := int64(-1)
	for s := int64(1); s < 500; s++ {
		probe := makeStartedGame13WithPrefs(t, s, fullSeats13(), nil)
		if findSeatWithRole(probe, RoleHunter) < 0 {
			seed = s
			break
		}
	}
	if seed < 0 {
		t.Fatal("U-02: no seed in [1,500) produces a 13p deck without hunter")
	}
	t.Logf("U-02: using seed=%d (deck has no hunter)", seed)

	gs := makeStartedGame13WithPrefs(t, seed, fullSeats13(), map[int]Role{5: RoleHunter})

	// 引擎层:PreferredRolesUnmet 必须记录 5 号位。
	found := false
	for _, s := range gs.PreferredRolesUnmet {
		if s == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("U-02: PreferredRolesUnmet=%v, want contains seat 5", gs.PreferredRolesUnmet)
	}
	// 视图层:本人(5 号位)必须看到 my_preferred_role=hunter + my_role_pref_unmet=true。
	cs := BuildClientState("r1", gs.Seats, 5, gs)
	if !cs.MyRolePrefUnmet {
		t.Fatalf("U-02: cs.my_role_pref_unmet=false, want true")
	}
	if cs.MyPreferredRole != "hunter" {
		t.Fatalf("U-02: cs.my_preferred_role=%q, want %q", cs.MyPreferredRole, "hunter")
	}
}

// U-03: 其他玩家视角绝不下发这两字段(身份保密 §135)。
func TestRolePrefUnmet_U03_OtherPlayerSeesNothing(t *testing.T) {
	gs := makeStartedGame13WithPrefs(t, 42, fullSeats13(), map[int]Role{5: RoleHunter})
	// 无论 5 号位是否满足,其他座位视角永远收不到这两字段。
	for viewer := 0; viewer < MaxPlayers; viewer++ {
		if viewer == 5 {
			continue
		}
		cs := BuildClientState("r1", gs.Seats, viewer, gs)
		if cs.MyPreferredRole != "" || cs.MyRolePrefUnmet {
			t.Fatalf("U-03: viewer=%d must not see seat 5's pref state, got pref=%q unmet=%v",
				viewer, cs.MyPreferredRole, cs.MyRolePrefUnmet)
		}
	}
}

// U-04: 观战者视角绝不下发这两字段(身份保密 §135)。
func TestRolePrefUnmet_U04_SpectatorSeesNothing(t *testing.T) {
	gs := makeStartedGame13WithPrefs(t, 42, fullSeats13(), map[int]Role{5: RoleHunter})
	cs := BuildClientState("r1", gs.Seats, -1, gs) // -1 = spectator
	if cs.MyPreferredRole != "" || cs.MyRolePrefUnmet {
		t.Fatalf("U-04: spectator must not see pref state, got pref=%q unmet=%v",
			cs.MyPreferredRole, cs.MyRolePrefUnmet)
	}
}

// U-05: 已满足偏好的座位不误报(预言家每局必有,偏好必满足)。
func TestRolePrefUnmet_U05_SatisfiedPrefNotReported(t *testing.T) {
	gs := makeStartedGame13WithPrefs(t, 7, fullSeats13(), map[int]Role{2: RoleSeer})
	cs := BuildClientState("r1", gs.Seats, 2, gs)
	if cs.MyRolePrefUnmet {
		t.Fatalf("U-05: satisfied pref must not set my_role_pref_unmet")
	}
	if cs.MyPreferredRole != "" {
		t.Fatalf("U-05: satisfied pref must not set my_preferred_role, got %q", cs.MyPreferredRole)
	}
}
