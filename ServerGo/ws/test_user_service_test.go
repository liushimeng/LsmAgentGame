// File naming: per docs/国际化与命名规范.md, new server test files use the
// test_*_test.go convention.
package ws

import (
	"testing"

	"LsmWebGame/models"
	"LsmWebGame/service"
)

// sampleView 构造一条用户视图用于裁剪测试。
func sampleView(id string, ut models.UserType) service.AdminUserView {
	return service.AdminUserView{
		ID:           id,
		Account:      "acct-" + id,
		Nickname:     "nick-" + id,
		Phone:        "13800000000",
		Email:        id + "@example.com",
		UserType:     ut,
		MyInviteCode: "INV" + id,
		CreatedAt:    1000,
	}
}

// TestBuildUserItem_NormalUserRedaction 普通用户只能看到 id/nickname/online，
// 详细字段必须被裁剪掉。
func TestBuildUserItem_NormalUserRedaction(t *testing.T) {
	v := sampleView("u1", models.UserTypeNormal)
	item := buildUserItem(v, models.UserTypeNormal, "caller", true)

	if item.ID != "u1" || item.Nickname != "nick-u1" || !item.Online {
		t.Fatalf("basic fields wrong: %+v", item)
	}
	if item.Account != "" || item.Phone != "" || item.Email != "" || item.MyInviteCode != "" || item.UserType != 0 || item.CreatedAt != 0 {
		t.Fatalf("normal user should NOT see detailed fields: %+v", item)
	}
	if item.CanDelete {
		t.Fatalf("normal user must never see can_delete")
	}
}

// TestBuildUserItem_AdminSeesDetailNoDelete 管理员能看到详细字段，但无删除权限。
func TestBuildUserItem_AdminSeesDetailNoDelete(t *testing.T) {
	v := sampleView("u1", models.UserTypeNormal)
	item := buildUserItem(v, models.UserTypeAdmin, "caller", false)

	if item.Account == "" || item.Phone == "" || item.Email == "" || item.MyInviteCode == "" {
		t.Fatalf("admin should see detailed fields: %+v", item)
	}
	if item.CanDelete {
		t.Fatalf("admin must NOT have can_delete")
	}
}

// TestBuildUserItem_SuperCanDeleteRules 超管对普通/管理员可删除，但不能删超管或自己。
func TestBuildUserItem_SuperCanDeleteRules(t *testing.T) {
	cases := []struct {
		name      string
		target    service.AdminUserView
		callerID  string
		wantDelete bool
	}{
		{"normal target", sampleView("n1", models.UserTypeNormal), "boss", true},
		{"admin target", sampleView("a1", models.UserTypeAdmin), "boss", true},
		{"super target", sampleView("s1", models.UserTypeSuper), "boss", false},
		{"self", sampleView("boss", models.UserTypeAdmin), "boss", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := buildUserItem(tc.target, models.UserTypeSuper, tc.callerID, false)
			if item.CanDelete != tc.wantDelete {
				t.Fatalf("%s: can_delete = %v, want %v", tc.name, item.CanDelete, tc.wantDelete)
			}
		})
	}
}
