// Package werewolf — cards_preferred_roles_test.go: 2026-08-06 §20260806-03
// 自选角色注入(ApplyPreferredRoles)的单元测试。
// 详见 docs/狼人杀13人局-优化和解决-20260806-03.md §6。
package werewolf

import (
	"testing"
)

// allOccupied 返回 13 座全满的 occupied 数组(本组测试均满座发牌)。
func allOccupied() [MaxPlayers]bool {
	var o [MaxPlayers]bool
	for i := range o {
		o[i] = true
	}
	return o
}

// roleMultiset 统计 roles 的多重集(每种角色的张数)。
func roleMultiset(roles [MaxPlayers]Role) map[Role]int {
	m := make(map[Role]int)
	for _, r := range roles {
		m[r]++
	}
	return m
}

func assertSameMultiset(t *testing.T, before, after [MaxPlayers]Role) {
	t.Helper()
	b, a := roleMultiset(before), roleMultiset(after)
	if len(b) != len(a) {
		t.Fatalf("multiset size changed: before=%v after=%v", b, a)
	}
	for r, n := range b {
		if a[r] != n {
			t.Fatalf("multiset changed for role %s: before=%d after=%d (full before=%v after=%v)",
				r, n, a[r], b, a)
		}
	}
}

// P-01: 单座位指定命中(预言家)后牌组多重集不变。
func TestApplyPreferredRoles_P01_SingleSeatSeer(t *testing.T) {
	roles := AssignRoles13Random(42, fullSeats13())
	if roles[3] == RoleSeer {
		// 已命中也算通过(函数应 no-op)
	}
	before := roles
	unmet := ApplyPreferredRoles(&roles, allOccupied(), map[int]Role{3: RoleSeer})
	if len(unmet) != 0 {
		t.Fatalf("P-01: seat 3 wants seer but unmet=%v (13 人牌组必有预言家)", unmet)
	}
	if roles[3] != RoleSeer {
		t.Fatalf("P-01: roles[3] = %s, want seer", roles[3])
	}
	assertSameMultiset(t, before, roles)
}

// P-02: 指定角色不在牌组 → 降级随机,多重集不变。
// 构造一副不含骑士的牌(标准 13 人固定牌组),指定骑士必然无法满足。
func TestApplyPreferredRoles_P02_RoleNotInDeck(t *testing.T) {
	roles := AssignRoles13(42, fullSeats13()) // 固定牌组:无 guard/knight/demon_hunter
	before := roles
	unmet := ApplyPreferredRoles(&roles, allOccupied(), map[int]Role{5: RoleKnight})
	if len(unmet) != 1 || unmet[0] != 5 {
		t.Fatalf("P-02: want unmet=[5], got %v", unmet)
	}
	assertSameMultiset(t, before, roles)
}

// P-03: 两座抢同一限量角色(预言家仅 1 张)→ 升序先得,另一座降级。
func TestApplyPreferredRoles_P03_TwoSeatsCompeteForSeer(t *testing.T) {
	roles := AssignRoles13Random(7, fullSeats13())
	before := roles
	unmet := ApplyPreferredRoles(&roles, allOccupied(), map[int]Role{
		2: RoleSeer,
		9: RoleSeer,
	})
	// 升序:seat 2 先得;seat 9 必然无法满足(牌组只有 1 张预言家)。
	if roles[2] != RoleSeer {
		t.Fatalf("P-03: roles[2] = %s, want seer (升序先得)", roles[2])
	}
	if len(unmet) != 1 || unmet[0] != 9 {
		t.Fatalf("P-03: want unmet=[9], got %v", unmet)
	}
	assertSameMultiset(t, before, roles)
}

// P-04: 偏好座位已命中 → no-op,整副牌原样。
func TestApplyPreferredRoles_P04_AlreadySatisfied(t *testing.T) {
	roles := AssignRoles13Random(99, fullSeats13())
	seerSeat := -1
	for i, r := range roles {
		if r == RoleSeer {
			seerSeat = i
			break
		}
	}
	if seerSeat < 0 {
		t.Fatal("P-04: deck has no seer (impossible for RandomDeck13)")
	}
	before := roles
	unmet := ApplyPreferredRoles(&roles, allOccupied(), map[int]Role{seerSeat: RoleSeer})
	if len(unmet) != 0 {
		t.Fatalf("P-04: already-satisfied seat reported unmet=%v", unmet)
	}
	if roles != before {
		t.Fatalf("P-04: no-op expected but deck changed: before=%v after=%v", before, roles)
	}
}

// P-05: 与"尾位强制平民"交互 — 第 13 人(最大座位号)指定狼人可生效且多重集守恒。
// 注:引擎语义 = StartGame 中 AssignRoles13Random 之后再调 ApplyPreferredRoles,
// 本测试直接模拟该顺序(AssignRoles13Random 内部已做尾位平民交换)。
func TestApplyPreferredRoles_P05_LastSeatWolfOverride(t *testing.T) {
	seats := fullSeats13()
	roles := AssignRoles13Random(1234, seats)
	lastSeat := -1
	for i := MaxPlayers - 1; i >= 0; i-- {
		if seats[i] != "" {
			lastSeat = i
			break
		}
	}
	if lastSeat < 0 {
		t.Fatal("P-05: no occupied seat")
	}
	before := roles
	unmet := ApplyPreferredRoles(&roles, allOccupied(), map[int]Role{lastSeat: RoleWerewolf})
	if len(unmet) != 0 {
		t.Fatalf("P-05: last seat wants werewolf but unmet=%v (牌组必有 4 狼)", unmet)
	}
	if roles[lastSeat] != RoleWerewolf {
		t.Fatalf("P-05: roles[%d] = %s, want werewolf", lastSeat, roles[lastSeat])
	}
	assertSameMultiset(t, before, roles)
}

// P-06: 全偏好(13 座全指定合法角色)结果仍是合法多重集。
func TestApplyPreferredRoles_P06_AllSeatsPreferred(t *testing.T) {
	roles := AssignRoles13Random(2026, fullSeats13())
	before := roles
	prefs := map[int]Role{
		0: RoleWerewolf, 1: RoleWerewolf, 2: RoleWerewolf, 3: RoleWerewolf,
		4: RoleSeer, 5: RoleWitch, 6: RoleHunter, 7: RoleIdiot,
		8: RoleVillager, 9: RoleVillager, 10: RoleVillager, 11: RoleVillager, 12: RoleVillager,
	}
	_ = ApplyPreferredRoles(&roles, allOccupied(), prefs)
	// 标准 13 人固定牌组恰好满足 4狼+预+女+猎+白痴+5平民 → 全部命中( RandomDeck13
	// 的神职构成不保证含 witch/hunter/idiot,但多重集必须守恒,未命中降级随机即可)。
	assertSameMultiset(t, before, roles)
	// 至少有座位被满足(4 狼 + 预言家必然存在于任何 13 人牌组)。
	if roles[0] != RoleWerewolf {
		t.Fatalf("P-06: roles[0] = %s, want werewolf (牌组必有 4 狼)", roles[0])
	}
	if roles[4] != RoleSeer {
		t.Fatalf("P-06: roles[4] = %s, want seer (牌组必有预言家)", roles[4])
	}
}

// P-07: ParseRoleName 白名单与退役角色拒绝。
func TestParseRoleName_Whitelist(t *testing.T) {
	// 合法:9 角色 + random + 空
	for _, name := range append(SelectableRoleNames, "random", "") {
		if _, ok := ParseRoleName(name); !ok {
			t.Fatalf("ParseRoleName(%q) rejected, want accepted", name)
		}
	}
	// ""/"random" → RoleUnknown(随机哨兵)
	for _, name := range []string{"", "random"} {
		if r, _ := ParseRoleName(name); r != RoleUnknown {
			t.Fatalf("ParseRoleName(%q) = %v, want RoleUnknown(random sentinel)", name, r)
		}
	}
	// 已退役角色必须拒绝(§134/§198 卡池契约)
	for _, retired := range []string{"magician", "merchant", "dreamer", "crow", "scarecrow", "prince", "pure_white"} {
		if _, ok := ParseRoleName(retired); ok {
			t.Fatalf("ParseRoleName(%q) accepted retired role, want rejected", retired)
		}
	}
	// 垃圾输入拒绝
	if _, ok := ParseRoleName("superman"); ok {
		t.Fatal("ParseRoleName(superman) accepted, want rejected")
	}
}

// P-08: 空座位的偏好自动无效(不 panic,不交换)。
func TestApplyPreferredRoles_P08_EmptySeatIgnored(t *testing.T) {
	roles := AssignRoles13Random(555, fullSeats13())
	before := roles
	occupied := allOccupied()
	occupied[6] = false // 6 号位空
	unmet := ApplyPreferredRoles(&roles, occupied, map[int]Role{6: RoleSeer})
	if len(unmet) != 0 {
		t.Fatalf("P-08: empty-seat pref should be silently skipped, got unmet=%v", unmet)
	}
	assertSameMultiset(t, before, roles)
	if roles != before {
		t.Fatal("P-08: empty-seat pref must not mutate deck")
	}
}

// fullSeats13 构造 13 个非空 userID 的座位数组。
func fullSeats13() [MaxPlayers]string {
	var s [MaxPlayers]string
	for i := range s {
		s[i] = "user_" + string(rune('a'+i))
	}
	return s
}
