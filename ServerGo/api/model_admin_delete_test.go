// Package api — model_admin_delete_test.go (§20260816-03).
//
// 覆盖「删除语义」修复的可单测部分：硬删除的权限门禁。
//
// 背景（§20260816-03）：`DELETE /providers/:id` 一直是软删除（enabled=false），
// 但 ListProviders 不过滤 enabled，导致管理员删掉的行刷新后原样重现。修复包含
// 三部分：列表默认过滤 + 新增 ?hard=1 物理删除 + 前端删除后重新对账。
//
// 本文件只测 hard=1 的**权限分流**：软删除要 admin，硬删除必须 super。
// 这是纯 auth-gate 逻辑，在 gormDB=nil 的桩环境下即可断言，不需要真库。
//
// 列表过滤（enabled=true）与引用检查（409）依赖真实 SQL，留给
// `-tags llmintegration` 的集成层与手工验收覆盖 —— 本包 header 已声明
// "These tests deliberately do NOT touch the database"，不在此破例。
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"LsmAgentGame/models"

	"github.com/gin-gonic/gin"
)

// newDeleteOnlyRouter 挂一个只有 DELETE 路由的引擎，用给定角色签发上下文。
// gormDB 为 nil：通过鉴权后 handler 会因 "db not wired" 返回 500，
// 因此「500」在本文件里等价于「已通过权限门禁」。
func newDeleteOnlyRouter(role models.UserType) *gin.Engine {
	h := NewModelAdminAPI(&stubAuthChecker{role: role}, nil, nil, nil, nil, nil)
	r := gin.New()
	r.DELETE("/api/admin/llm/providers/:id",
		authCtx("test-user", int(role)), h.DeleteProvider)
	return r
}

// TestDelete_20260816_03_SoftDeleteAllowsAdmin —— 软删除（不带 hard）
// 普通管理员即可执行；应当越过权限门禁（gormDB=nil ⇒ 500）。
func TestDelete_20260816_03_SoftDeleteAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newDeleteOnlyRouter(models.UserTypeAdmin)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/llm/providers/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Fatalf("软删除不应要求超级管理员, got 403 body=%s", w.Body.String())
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (db not wired, 说明已过权限门禁), got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestDelete_20260816_03_HardDeleteRejectsPlainAdmin —— 硬删除对普通管理员
// 必须 403。硬删除不可逆（物理删行 + 删 bot user），权限必须比软删除更严。
func TestDelete_20260816_03_HardDeleteRejectsPlainAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newDeleteOnlyRouter(models.UserTypeAdmin)
	req := httptest.NewRequest(http.MethodDelete,
		"/api/admin/llm/providers/abc?hard=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("hard=1 对普通管理员必须 403, got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestDelete_20260816_03_HardDeleteAllowsSuper —— 超级管理员可通过门禁。
func TestDelete_20260816_03_HardDeleteAllowsSuper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newDeleteOnlyRouter(models.UserTypeSuper)
	req := httptest.NewRequest(http.MethodDelete,
		"/api/admin/llm/providers/abc?hard=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusForbidden {
		t.Fatalf("超级管理员不应被 403 拦住, body=%s", w.Body.String())
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (db not wired, 说明已过权限门禁), got %d body=%s",
			w.Code, w.Body.String())
	}
}

// TestDelete_20260816_03_HardOnlyOnExplicitFlag —— 只有 hard=1 才走硬删除。
// hard=0 / hard=true / hard 缺省一律按软删除处理（普通管理员即可）。
// 防的是「参数拼写略有不同就意外触发不可逆操作」。
func TestDelete_20260816_03_HardOnlyOnExplicitFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?hard=0", "?hard=true", "?hard=yes"} {
		r := newDeleteOnlyRouter(models.UserTypeAdmin)
		req := httptest.NewRequest(http.MethodDelete,
			"/api/admin/llm/providers/abc"+q, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusForbidden {
			t.Errorf("query %q 不应被当作硬删除(403): body=%s", q, w.Body.String())
		}
	}
}
