// Package werewolf — wolfpack_roles_test.go: §20260810-10 U1 狼队战术分工单测。
//
// 覆盖不变式:
//  1. AutoAssignRoles 按座位升序套模板(4/3/2 狼);1 狼不指派。
//  2. AutoAssignRoles 确定性(无随机,同输入同输出)。
//  3. AssignRole 仅狼王可调;占用互换;幂等;非法枚举拒绝。
//  4. PurgeByDeath 移除死亡狼分工;RotateKing 顺延到最小存活狼座位。
//  5. ResetAssignments 清空。
package werewolf

import (
	"testing"
)

func TestWolfRoles_AutoAssign4Wolves(t *testing.T) {
	w := NewWolfPackRoom("r1", 50)
	w.SetMembers([]int{2, 5, 8, 11})
	w.AutoAssignRoles([]int{2, 5, 8, 11})
	table, king := w.RoleSnapshot()
	if len(table) != 4 {
		t.Fatalf("want 4 roles, got %d", len(table))
	}
	if table[2] != WolfRoleHype || table[5] != WolfRoleCharger ||
		table[8] != WolfRoleHook || table[11] != WolfRoleDeep {
		t.Fatalf("template mismatch: %+v", table)
	}
	if king != 2 {
		t.Fatalf("want king=2, got %d", king)
	}
}

func TestWolfRoles_AutoAssignDeterministic(t *testing.T) {
	// 乱序输入也必须得到同一结果(按座位升序)。
	a := NewWolfPackRoom("r", 50)
	a.SetMembers([]int{11, 2, 8, 5})
	a.AutoAssignRoles([]int{11, 2, 8, 5})
	b := NewWolfPackRoom("r", 50)
	b.SetMembers([]int{2, 5, 8, 11})
	b.AutoAssignRoles([]int{2, 5, 8, 11})
	ta, ka := a.RoleSnapshot()
	tb, kb := b.RoleSnapshot()
	if ka != kb {
		t.Fatalf("king mismatch %d vs %d", ka, kb)
	}
	for seat, role := range ta {
		if tb[seat] != role {
			t.Fatalf("seat %d role mismatch %s vs %s", seat, role, tb[seat])
		}
	}
}

func TestWolfRoles_Templates3And2(t *testing.T) {
	w3 := NewWolfPackRoom("r", 50)
	w3.SetMembers([]int{1, 4, 9})
	w3.AutoAssignRoles([]int{1, 4, 9})
	t3, _ := w3.RoleSnapshot()
	if t3[1] != WolfRoleHype || t3[4] != WolfRoleCharger || t3[9] != WolfRoleHook {
		t.Fatalf("3-wolf template mismatch: %+v", t3)
	}
	w2 := NewWolfPackRoom("r", 50)
	w2.SetMembers([]int{3, 7})
	w2.AutoAssignRoles([]int{3, 7})
	t2, _ := w2.RoleSnapshot()
	if t2[3] != WolfRoleHype || t2[7] != WolfRoleHook {
		t.Fatalf("2-wolf template mismatch: %+v", t2)
	}
	// 1 狼不指派。
	w1 := NewWolfPackRoom("r", 50)
	w1.SetMembers([]int{6})
	w1.AutoAssignRoles([]int{6})
	t1, k1 := w1.RoleSnapshot()
	if len(t1) != 0 || k1 != -1 {
		t.Fatalf("1-wolf should have no roles, got %+v king=%d", t1, k1)
	}
}

func TestWolfRoles_AssignOnlyKing(t *testing.T) {
	w := NewWolfPackRoom("r", 50)
	w.SetMembers([]int{2, 5, 8, 11})
	w.AutoAssignRoles([]int{2, 5, 8, 11})
	// 非狼王(5号)尝试改分工 → 拒绝。
	if _, err := w.AssignRole(5, WolfRoleDeep); err == nil {
		t.Fatal("non-king assign should fail")
	}
	// 非成员(0号)尝试 → 拒绝。
	if _, err := w.AssignRole(0, WolfRoleDeep); err == nil {
		t.Fatal("non-member assign should fail")
	}
	// 非法枚举 → 拒绝。
	if _, err := w.AssignRole(2, "superman"); err == nil {
		t.Fatal("invalid role should fail")
	}
}

func TestWolfRoles_AssignSwap(t *testing.T) {
	w := NewWolfPackRoom("r", 50)
	w.SetMembers([]int{2, 5, 8, 11})
	w.AutoAssignRoles([]int{2, 5, 8, 11})
	// 狼王 2 号(hype)改为 deep → 与 11 号(deep)互换。
	old, err := w.AssignRole(2, WolfRoleDeep)
	if err != nil {
		t.Fatalf("king assign failed: %v", err)
	}
	if old != WolfRoleHype {
		t.Fatalf("want old=hype, got %s", old)
	}
	table, _ := w.RoleSnapshot()
	if table[2] != WolfRoleDeep || table[11] != WolfRoleHype {
		t.Fatalf("swap mismatch: %+v", table)
	}
	// 幂等:再次改同样的分工 → 无变更。
	old2, err := w.AssignRole(2, WolfRoleDeep)
	if err != nil || old2 != WolfRoleDeep {
		t.Fatalf("idempotent assign failed: old=%s err=%v", old2, err)
	}
}

func TestWolfRoles_PurgeRemovesRoleAndRotatesKing(t *testing.T) {
	w := NewWolfPackRoom("r", 50)
	w.SetMembers([]int{2, 5, 8, 11})
	w.AutoAssignRoles([]int{2, 5, 8, 11})
	// 狼王 2 号死亡 → 分工移除 + 狼王顺延到 5。
	w.PurgeByDeath([]int{2})
	newKing := w.RotateKing()
	if newKing != 5 {
		t.Fatalf("want new king=5, got %d", newKing)
	}
	table, king := w.RoleSnapshot()
	if _, ok := table[2]; ok {
		t.Fatalf("dead wolf role should be removed: %+v", table)
	}
	if king != 5 {
		t.Fatalf("snapshot king want 5, got %d", king)
	}
	// 全部死亡 → king=-1。
	w.PurgeByDeath([]int{5, 8, 11})
	if k := w.RotateKing(); k != -1 {
		t.Fatalf("all dead want king=-1, got %d", k)
	}
}

func TestWolfRoles_RotateKingIdempotentWhenAlive(t *testing.T) {
	w := NewWolfPackRoom("r", 50)
	w.SetMembers([]int{2, 5, 8, 11})
	w.AutoAssignRoles([]int{2, 5, 8, 11})
	if k := w.RotateKing(); k != 2 {
		t.Fatalf("king alive, rotate should be no-op, got %d", k)
	}
}

func TestWolfRoles_ResetAssignments(t *testing.T) {
	w := NewWolfPackRoom("r", 50)
	w.SetMembers([]int{2, 5})
	w.AutoAssignRoles([]int{2, 5})
	w.ResetAssignments()
	table, king := w.RoleSnapshot()
	if len(table) != 0 || king != -1 {
		t.Fatalf("reset failed: table=%+v king=%d", table, king)
	}
}

func TestWolfRoles_Labels(t *testing.T) {
	if WolfRoleLabel(WolfRoleHype) != "悍跳位" ||
		WolfRoleLabel(WolfRoleCharger) != "冲锋位" ||
		WolfRoleLabel(WolfRoleHook) != "倒钩位" ||
		WolfRoleLabel(WolfRoleDeep) != "深水位" {
		t.Fatal("label mismatch")
	}
	if validWolfRole("nope") {
		t.Fatal("invalid role should not be valid")
	}
}
