// Package api — model_wallet_api_test.go.
//
// Coverage:
//   - auth gate: 401 (no uid), 403 (normal user), 403 (admin attempting
//     /adjust which is super-admin only).
//   - happy-path short-circuit when modelSvc / walletSvc are nil — both
//     endpoints must return 500 with a consistent envelope.
//
// Real wallet mutation is exercised by service/wallet_service_test.go.
package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/models"

	"github.com/gin-gonic/gin"
)

// newTestModelWalletAPI returns a handler wired with stub auth + nil deps.
func newTestModelWalletAPI() *gin.Engine {
	h := NewModelWalletAPI(nil, &stubAuthChecker{role: models.UserTypeAdmin}, nil, nil)
	r := gin.New()
	admin := r.Group("/api/admin")
	{
		admin.GET("/llm/bots/:botUserID/wallet",
			authCtx("test-admin", 2), h.GetBotWallet)
		admin.POST("/llm/bots/:botUserID/wallet/adjust",
			authCtx("test-super", 3), h.AdjustBotWallet)
	}
	return r
}

// TestModelWallet_GetBotWallet_NoAuth — 401 without uid.
func TestModelWallet_GetBotWallet_NoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelWalletAPI(nil, &stubAuthChecker{role: models.UserTypeAdmin}, nil, nil)
	r := gin.New()
	r.GET("/api/admin/llm/bots/:botUserID/wallet", h.GetBotWallet)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/bots/u1/wallet", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestModelWallet_GetBotWallet_NormalUserForbidden — role 1 → 403.
func TestModelWallet_GetBotWallet_NormalUserForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelWalletAPI(nil, &stubAuthChecker{role: models.UserTypeNormal}, nil, nil)
	r := gin.New()
	r.GET("/api/admin/llm/bots/:botUserID/wallet",
		authCtx("normal", 1), h.GetBotWallet)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/bots/u1/wallet", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// TestModelWallet_AdjustBotWallet_AdminRoleForbidden — admin (role 2) → 403
// for the super-admin-only adjust endpoint.
func TestModelWallet_AdjustBotWallet_AdminRoleForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelWalletAPI(nil, &stubAuthChecker{role: models.UserTypeAdmin}, nil, nil)
	r := gin.New()
	r.POST("/api/admin/llm/bots/:botUserID/wallet/adjust",
		authCtx("admin-only", 2), h.AdjustBotWallet)
	body := `{"amount":100,"remark":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/bots/u1/wallet/adjust",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for admin→adjust, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "需要超级管理员权限") {
		t.Fatalf("expected super-admin message, got %s", w.Body.String())
	}
}

// TestModelWallet_GetBotWallet_NoService — admin passes auth, then 500
// because modelSvc is nil.
func TestModelWallet_GetBotWallet_NoService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelWalletAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/bots/u1/wallet", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when modelSvc nil, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestModelWallet_AdjustBotWallet_SuperAllowed — super admin passes auth,
// then 500 because walletSvc is nil.
func TestModelWallet_AdjustBotWallet_SuperAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelWalletAPI(nil, &stubAuthChecker{role: models.UserTypeSuper}, nil, nil)
	r := gin.New()
	r.POST("/api/admin/llm/bots/:botUserID/wallet/adjust",
		authCtx("super", 3), h.AdjustBotWallet)
	body := `{"amount":100,"remark":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/bots/u1/wallet/adjust",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when walletSvc nil, got %d body=%s", w.Code, w.Body.String())
	}
}
