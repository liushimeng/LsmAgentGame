// Package werewolf — cards_rolepool_test.go: 卡池瘦身(2026-07-24)回归测试。
//
// 目标:
//  1. RandomDeck13 / AssignRoles13Random 永远不会再产出 RoleScarecrow / RolePrince。
//  2. 已有的 deck 不变量仍然成立:长度=13,4 狼 + 1 预言家,平民≥5,最后一位必为平民。
//  3. 历史 wire 兼容字符串保留:RoleScarecrow.String()=="scarecrow"、
//     RolePrince.String()=="prince"、RoleDisplayName 不为"未知"、FactionOf/IsGodRole 行为不变。
//
// 测试全部使用确定性种子,确保失败可复现。
package werewolf

import (
	"testing"
)

// 覆盖足够多 seed,确保 shuffle 的随机性不会偶然把 scarecrow/prince 拉回牌组。
var rolePoolSeeds = []int64{
	1, 2, 3, 4, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47,
	53, 59, 61, 67, 71, 73, 79, 83, 89, 97, 100, 1234, 2026,
	20260724, 9999999, -1, -42, 0,
}

// 13 个占座 userID(从 seat=0 起连续入座),便于 AssignRoles13Random 真的发牌。
var rolePoolUserIDs = func() [MaxPlayers]string {
	var u [MaxPlayers]string
	for i := 0; i < MaxPlayers; i++ {
		u[i] = "user_" + string(rune('A'+i))
	}
	return u
}()

func TestRandomDeck13_NeverDealsRetiredRoles(t *testing.T) {
	// §198 骑士重新加入 godRolePool,不再算退役。
	// §猎魔人 猎魔人重新加入 godRolePool,不再算退役。剩余 6 名延续原 retired 断言。
	retired := []Role{
		RoleMagician, RoleMerchant, RoleDreamer,
		RoleCrow, RoleScarecrow, RolePrince, RolePureWhite,
	}
	for _, seed := range rolePoolSeeds {
		deck := RandomDeck13(seed)
		if len(deck) != MaxPlayers {
			t.Fatalf("seed=%d: deck length = %d, want %d", seed, len(deck), MaxPlayers)
		}
		for i, r := range deck {
			for _, retiredRole := range retired {
				if r == retiredRole {
					t.Fatalf("seed=%d: deck[%d] = %s, must never be dealt (retired 2026-07-29)", seed, i, retiredRole)
				}
			}
		}
	}
}

func TestAssignRoles13Random_NeverDealsRetiredRoles(t *testing.T) {
	// §198 骑士重新加入 godRolePool,不再算退役。
	// §猎魔人 猎魔人重新加入 godRolePool,不再算退役。剩余 6 名延续原 retired 断言。
	retired := []Role{
		RoleMagician, RoleMerchant, RoleDreamer,
		RoleCrow, RoleScarecrow, RolePrince, RolePureWhite,
	}
	for _, seed := range rolePoolSeeds {
		roles := AssignRoles13Random(seed, rolePoolUserIDs)
		for seat, r := range roles {
			for _, retiredRole := range retired {
				if r == retiredRole {
					t.Fatalf("seed=%d: roles[%d] = %s, must never be dealt (retired 2026-07-29)", seed, seat, retiredRole)
				}
			}
		}
	}
}

func TestRandomDeck13_DeckInvariantsHold(t *testing.T) {
	for _, seed := range rolePoolSeeds {
		deck := RandomDeck13(seed)
		// 1. 总长度恒为 13。
		if len(deck) != MaxPlayers {
			t.Fatalf("seed=%d: deck length = %d, want %d", seed, len(deck), MaxPlayers)
		}
		// 2. 必须含 4 狼。
		wolfCount := 0
		for _, r := range deck {
			if r == RoleWerewolf {
				wolfCount++
			}
		}
		if wolfCount != 4 {
			t.Fatalf("seed=%d: wolf count = %d, want 4", seed, wolfCount)
		}
		// 3. 必须含 1 预言家。
		seerCount := 0
		for _, r := range deck {
			if r == RoleSeer {
				seerCount++
			}
		}
		if seerCount != 1 {
			t.Fatalf("seed=%d: seer count = %d, want 1", seed, seerCount)
		}
		// 4. 平民≥5。
		villagerCount := 0
		for _, r := range deck {
			if r == RoleVillager {
				villagerCount++
			}
		}
		if villagerCount < 5 {
			t.Fatalf("seed=%d: villager count = %d, want >= 5", seed, villagerCount)
		}
		// 5. 第 13 位必为平民(RandomDeck13 末位强制)。
		if deck[len(deck)-1] != RoleVillager {
			t.Fatalf("seed=%d: deck last role = %s, want villager", seed, deck[len(deck)-1])
		}
		// 6. 神职总数 = 2~3(从 godRolePool 抽 2~3)。
		godCount := 0
		for _, r := range deck {
			if r != RoleWerewolf && r != RoleVillager && r != RoleSeer {
				godCount++
			}
		}
		if godCount < 2 || godCount > 3 {
			t.Fatalf("seed=%d: god count = %d, want 2..3", seed, godCount)
		}
	}
}

func TestAssignRoles13Random_LastSeatIsVillager(t *testing.T) {
	// 第 13 个入座座位强制为平民 — 现有不变量,不应被瘦身破坏。
	for _, seed := range rolePoolSeeds {
		roles := AssignRoles13Random(seed, rolePoolUserIDs)
		// 找最后一个非空座位(全满时即 seat=MaxPlayers-1)。
		lastOccupied := -1
		for i := MaxPlayers - 1; i >= 0; i-- {
			if rolePoolUserIDs[i] != "" {
				lastOccupied = i
				break
			}
		}
		if lastOccupied < 0 || roles[lastOccupied] != RoleVillager {
			t.Fatalf("seed=%d: last occupied seat %d role = %s, want villager",
				seed, lastOccupied, roles[lastOccupied])
		}
	}
}

func TestRoleScarecrow_StableLegacyString(t *testing.T) {
	// 2026-07-29 已退役角色的 wire 兼容行为:
	// String() 仍返回正确 wire 串(历史 DB / wire 帧 / 回放兼容);
	// FactionOf / IsGodRole / RoleDisplayName 走 default 分支(Unknown/false/"未知")。
	if got := RoleScarecrow.String(); got != "scarecrow" {
		t.Fatalf("RoleScarecrow.String() = %q, want %q (legacy wire contract)", got, "scarecrow")
	}
	if got := FactionOf(RoleScarecrow); got != FactionUnknown {
		t.Fatalf("FactionOf(RoleScarecrow) = %s, want FactionUnknown (retired 2026-07-29)", got)
	}
	if IsGodRole(RoleScarecrow) {
		t.Fatalf("IsGodRole(RoleScarecrow) = true, want false (retired 2026-07-29)")
	}
	if got := RoleDisplayName(RoleScarecrow); got != "未知" {
		t.Fatalf("RoleDisplayName(RoleScarecrow) = %q, want \"未知\" (retired 2026-07-29)", got)
	}
}

func TestRolePrince_StableLegacyString(t *testing.T) {
	if got := RolePrince.String(); got != "prince" {
		t.Fatalf("RolePrince.String() = %q, want %q (legacy wire contract)", got, "prince")
	}
	if got := FactionOf(RolePrince); got != FactionUnknown {
		t.Fatalf("FactionOf(RolePrince) = %s, want FactionUnknown (retired 2026-07-29)", got)
	}
	if IsGodRole(RolePrince) {
		t.Fatalf("IsGodRole(RolePrince) = true, want false (retired 2026-07-29)")
	}
	if got := RoleDisplayName(RolePrince); got != "未知" {
		t.Fatalf("RoleDisplayName(RolePrince) = %q, want \"未知\" (retired 2026-07-29)", got)
	}
}

func TestRetiredRoles_StableLegacyString(t *testing.T) {
	// 6 个"半实现"已退役角色(magician/merchant/dreamer/crow/pure_white)
	// 的 wire 兼容行为:String() 仍返回正确 wire 串,但 FactionOf 返回 Unknown、
	// IsGodRole 返回 false、RoleDisplayName 返回"未知"。
	// §198 骑士不再算退役 — 已被加回 godRolePool(FactionOf/IsGodRole/RoleDisplayName 正常)。
	// §猎魔人 猎魔人不再算退役 — 已被加回 godRolePool(FactionOf/IsGodRole/RoleDisplayName 正常)。
	type tc struct {
		role        Role
		wireString  string
	}
	cases := []tc{
		{RoleMagician, "magician"},
		{RoleMerchant, "merchant"},
		{RoleDreamer, "dreamer"},
		{RoleCrow, "crow"},
		{RolePureWhite, "pure_white"},
	}
	for _, c := range cases {
		// String() 仍返回正确 wire 串(历史 DB / wire 帧 / 回放兼容)。
		if got := c.role.String(); got != c.wireString {
			t.Fatalf("Role(%d).String() = %q, want %q (legacy wire contract)", int(c.role), got, c.wireString)
		}
		// FactionOf 返回 Unknown(不再被识别为好人阵营)。
		if got := FactionOf(c.role); got != FactionUnknown {
			t.Fatalf("FactionOf(%s) = %s, want FactionUnknown (retired 2026-07-29)", c.wireString, got)
		}
		// IsGodRole 返回 false(不再被识别为神职)。
		if IsGodRole(c.role) {
			t.Fatalf("IsGodRole(%s) = true, want false (retired 2026-07-29)", c.wireString)
		}
		// RoleDisplayName 返回"未知"(不再有中文名)。
		if got := RoleDisplayName(c.role); got != "未知" {
			t.Fatalf("RoleDisplayName(%s) = %q, want \"未知\" (retired 2026-07-29)", c.wireString, got)
		}
	}
	// §198 骑士额外断言: 重新实现后 String()="knight" + FactionOf=Good + IsGodRole=true + RoleDisplayName="骑士"。
	if got := RoleKnight.String(); got != "knight" {
		t.Fatalf("RoleKnight.String() = %q, want %q", got, "knight")
	}
	if got := FactionOf(RoleKnight); got != FactionGood {
		t.Fatalf("FactionOf(RoleKnight) = %s, want FactionGood (§198)", got)
	}
	if !IsGodRole(RoleKnight) {
		t.Fatal("IsGodRole(RoleKnight) = false, want true (§198)")
	}
	if got := RoleDisplayName(RoleKnight); got != "骑士" {
		t.Fatalf("RoleDisplayName(RoleKnight) = %q, want %q (§198)", got, "骑士")
	}
	// §猎魔人 猎魔人额外断言: 重新实现后 String()="demon_hunter" + FactionOf=Good + IsGodRole=true + RoleDisplayName="猎魔人"。
	if got := RoleDemonHunter.String(); got != "demon_hunter" {
		t.Fatalf("RoleDemonHunter.String() = %q, want %q", got, "demon_hunter")
	}
	if got := FactionOf(RoleDemonHunter); got != FactionGood {
		t.Fatalf("FactionOf(RoleDemonHunter) = %s, want FactionGood (§猎魔人)", got)
	}
	if !IsGodRole(RoleDemonHunter) {
		t.Fatal("IsGodRole(RoleDemonHunter) = false, want true (§猎魔人)")
	}
	if got := RoleDisplayName(RoleDemonHunter); got != "猎魔人" {
		t.Fatalf("RoleDisplayName(RoleDemonHunter) = %q, want %q (§猎魔人)", got, "猎魔人")
	}
}

func TestGodRolePool_ExcludesRetiredRoles(t *testing.T) {
	// §198 骑士重新加入 godRolePool。
	// §猎魔人 猎魔人重新加入 godRolePool。剩余 6 名延续原 retired 断言。
	retired := []Role{
		RoleMagician, RoleMerchant, RoleDreamer,
		RoleCrow, RoleScarecrow, RolePrince, RolePureWhite,
	}
	for i, r := range godRolePool {
		for _, retiredRole := range retired {
			if r == retiredRole {
				t.Fatalf("godRolePool[%d] = %s, must be removed from active pool (retired 2026-07-29)", i, retiredRole)
			}
		}
	}
	// §198/§猎魔人 额外断言: pool 必须含 6 个完整实现的神职(女巫/猎人/白痴/守卫/骑士/猎魔人)。
	expected := []Role{RoleWitch, RoleHunter, RoleIdiot, RoleGuard, RoleKnight, RoleDemonHunter}
	if len(godRolePool) != len(expected) {
		t.Fatalf("godRolePool length = %d, want %d (witch/hunter/idiot/guard/knight/demon_hunter)", len(godRolePool), len(expected))
	}
	for i, r := range expected {
		if godRolePool[i] != r {
			t.Fatalf("godRolePool[%d] = %s, want %s", i, godRolePool[i], r)
		}
	}
}
