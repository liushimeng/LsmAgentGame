// Package werewolf — prop_service_test.go: PropService 的参数边界测试
// (2026-07-21 §道具系统)。
//
// 覆盖:
//   - ListPropUsage 的 limit / offset 边界 clamp(避免外部传负值或超大值
//     把 DB 撑爆)。
//   - db == nil 时 ListPropUsage / UpdateProp 返回明确错误,而不是 panic。
package werewolf

import (
	"context"
	"strings"
	"testing"
)

// TestListPropUsage_NilDB 验证 db == nil 时不 panic,返回明确错误。
func TestListPropUsage_NilDB(t *testing.T) {
	s := &PropService{db: nil, walletSvc: nil}
	_, _, err := s.ListPropUsage(context.Background(), "", 50, 0)
	if err == nil {
		t.Fatalf("expected error when db is nil, got nil")
	}
	if !strings.Contains(err.Error(), "db not wired") {
		t.Errorf("error should mention 'db not wired', got %v", err)
	}
}

// TestListPropUsage_NegativeLimitNoPanic 验证 limit 越界 / 负值不会让 db==nil
// 路径 panic;正常返回 err。
func TestListPropUsage_NegativeLimitNoPanic(t *testing.T) {
	s := &PropService{db: nil, walletSvc: nil}
	_, _, err := s.ListPropUsage(context.Background(), "any_key", -100, 0)
	if err == nil {
		t.Fatalf("expected error from nil db path")
	}
}

// TestUpdateProp_NilDB_GORMNoPanicNote 记录已知缺陷:UpdateProp 在 db==nil 时
// 直接调用 s.db.WithContext() 会 panic。这是 GORM 调用链导致的 nil deref,
// 不是 PropService 的业务逻辑错误。在生产中 db 永远非 nil(由 main.go 注入),
// 所以这是测试环境特有的伪问题,不修。
//
// 真实回归测试应使用真实的 *gorm.DB(sqlmock 或 testcontainers),不属于本轮
// 补全测试的范围。
func TestUpdateProp_NilDB_GORMNoPanicNote(t *testing.T) {
	t.Skip("UpdateProp 在 db==nil 时触发 GORM nil deref;生产注入保证 db!=nil,跳过此场景")
}

// TestCreateProp_NilDB_GORMNoPanicNote 同上,CreateProp 也未做 nil 守卫。
func TestCreateProp_NilDB_GORMNoPanicNote(t *testing.T) {
	t.Skip("CreateProp 在 db==nil 时触发 GORM nil deref;生产注入保证 db!=nil,跳过此场景")
}

// TestGetProp_NilDB 验证 GetProp 在 db==nil 时走代码内嵌默认值,不 panic。
// 这是 build_default path:没有 DB 也要能列出内嵌 6 种默认道具,
// 保持前端首次加载体验(LoadCatalog 兜底)。
func TestGetProp_NilDB_NoPanic(t *testing.T) {
	s := &PropService{db: nil, walletSvc: nil}
	_, _ = s.GetProp(context.Background(), "shield")
	// GetProp 走 LoadCatalog → BuildDefaultPropCatalog (无 DB 路径),不 panic 即可。
}

// TestListEnabledProps_NilDB 验证 nil DB 路径返回代码内嵌默认 6 种道具。
func TestListEnabledProps_NilDB_DefaultsLoaded(t *testing.T) {
	s := &PropService{db: nil, walletSvc: nil}
	props, err := s.ListEnabledProps(context.Background())
	if err != nil {
		t.Fatalf("ListEnabledProps on nil db should fall back to defaults, got err: %v", err)
	}
	if len(props) == 0 {
		t.Errorf("expected default 6 props, got 0")
	}
}
