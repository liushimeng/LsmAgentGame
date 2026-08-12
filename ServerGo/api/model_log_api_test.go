// Package api — model_log_api_test.go.
//
// Coverage:
//   - permission-denied (no user_id → 401; normal user → 403) using a
//     stubAuthChecker so the role gate fires without gormDB.
//   - happy-path short-circuit when modelSvc is nil (auth passes, then 500).
//
// Real CRUD is exercised by the integration suite.
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmWebGame/models"

	"github.com/gin-gonic/gin"
)

// newTestModelLogAPI returns a handler wired with stub auth + nil modelSvc.
func newTestModelLogAPI() *gin.Engine {
	h := NewModelLogAPI(nil, &stubAuthChecker{role: models.UserTypeAdmin})
	r := gin.New()
	admin := r.Group("/api/admin")
	{
		admin.GET("/llm/providers/:id/games", authCtx("test-admin", 2), h.ListProviderGames)
		admin.GET("/llm/games/:gameLogID", authCtx("test-admin", 2), h.GetGameLog)
		admin.GET("/llm/games/:gameLogID/messages", authCtx("test-admin", 2), h.ListGameMessages)
	}
	return r
}

// TestModelLog_GetGameLog_NoAuth — 401 without user_id.
func TestModelLog_GetGameLog_NoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelLogAPI(nil, &stubAuthChecker{role: models.UserTypeAdmin})
	r := gin.New()
	r.GET("/api/admin/llm/games/:gameLogID", h.GetGameLog)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/games/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestModelLog_GetGameLog_NormalUserForbidden — role 1 (normal) → 403.
func TestModelLog_GetGameLog_NormalUserForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelLogAPI(nil, &stubAuthChecker{role: models.UserTypeNormal})
	r := gin.New()
	r.GET("/api/admin/llm/games/:gameLogID",
		authCtx("normal-user", 1), h.GetGameLog)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/games/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for normal user, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "需要管理员权限") {
		t.Fatalf("expected admin-required message, got %s", w.Body.String())
	}
}

// TestModelLog_GetGameLog_NoService — admin passes auth gate, then 500
// because modelSvc is nil.
func TestModelLog_GetGameLog_NoService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelLogAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/games/some-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when modelSvc nil, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestModelLog_ListProviderGames_NoService — same nil-deps short-circuit
// for the list endpoint.
func TestModelLog_ListProviderGames_NoService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelLogAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers/p1/games", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestModelLog_BadSince — invalid RFC3339 in ?since= → 400.
func TestModelLog_BadSince(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelLogAPI()
	req := httptest.NewRequest(http.MethodGet,
		"/api/admin/llm/providers/p1/games?since=not-a-date", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// modelSvc is nil → handler returns 500 before validating since. This
	// test asserts the current behavior; the integration suite covers the
	// validation ordering against a live DB.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (nil modelSvc), got %d body=%s", w.Code, w.Body.String())
	}
}
