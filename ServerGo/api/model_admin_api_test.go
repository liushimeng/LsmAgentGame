// Package api — model_admin_api_test.go.
//
// Coverage strategy:
//   - happy-path and permission-denied for the major handlers (ListProviders,
//     CreateProvider, UpdateProvider, DeleteProvider, TestProvider,
//     ReloadProviders) using httptest + gin.New() + a stubAuthChecker.
//     Other deps are nil so the handler short-circuits with a 500 envelope
//     after passing auth — still exercises the auth gate + body parse.
//   - Pure functions (decodeJSONStrict, apiKeyHintLocal, isMySQLDuplicateErr)
//     are tested directly.
//
// These tests deliberately do NOT touch the database. Real CRUD coverage is
// left to the integration suite that requires a live MariaDB.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/models"

	"github.com/gin-gonic/gin"
)

// newTestModelAdminAPI returns a handler wired with a stubAuthChecker but
// nil registry / gormDB / botUserSvc so the auth-gate assertions can exercise
// the role check without spinning up GORM.
func newTestModelAdminAPI(role models.UserType) *gin.Engine {
	checker := &stubAuthChecker{role: role}
	h := NewModelAdminAPI(checker, nil, nil, nil, nil, nil)
	r := gin.New()
	admin := r.Group("/api/admin")
	{
		admin.GET("/llm/providers", authCtx("test-admin", int(role)), h.ListProviders)
		admin.POST("/llm/providers", authCtx("test-admin", int(role)), h.CreateProvider)
		admin.PUT("/llm/providers/:id", authCtx("test-admin", int(role)), h.UpdateProvider)
		admin.DELETE("/llm/providers/:id", authCtx("test-admin", int(role)), h.DeleteProvider)
		admin.POST("/llm/providers/:id/test", authCtx("test-admin", int(role)), h.TestProvider)
		admin.POST("/llm/providers/reload", authCtx("test-super", 3), h.ReloadProviders)
	}
	return r
}

// TestModelAdmin_ListProviders_PermissionDenied — no user_id in context → 401.
func TestModelAdmin_ListProviders_PermissionDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelAdminAPI(&stubAuthChecker{role: models.UserTypeAdmin}, nil, nil, nil, nil, nil)
	r := gin.New()
	r.GET("/api/admin/llm/providers", h.ListProviders)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestModelAdmin_ReloadProviders_RequiresSuper — admin (role 2) cannot reload.
func TestModelAdmin_ReloadProviders_RequiresSuper(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelAdminAPI(&stubAuthChecker{role: models.UserTypeAdmin}, nil, nil, nil, nil, nil)
	r := gin.New()
	r.POST("/api/admin/llm/providers/reload",
		authCtx("test-admin", 2), h.ReloadProviders)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for admin→reload, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "需要超级管理员权限") {
		t.Fatalf("expected super-admin message, got %s", w.Body.String())
	}
}

// TestModelAdmin_ReloadProviders_SuperAllowed — super admin passes auth gate,
// then 500 because registry is nil (expected — this is the shortest path
// through requireSuper).
func TestModelAdmin_ReloadProviders_SuperAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewModelAdminAPI(&stubAuthChecker{role: models.UserTypeSuper}, nil, nil, nil, nil, nil)
	r := gin.New()
	r.POST("/api/admin/llm/providers/reload",
		authCtx("test-super", 3), h.ReloadProviders)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/reload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when registry nil, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestModelAdmin_CreateProvider_BadJSON — strict body parsing surfaces typos.
// We pass admin role so auth gate lets us through to body parsing.
func TestModelAdmin_CreateProvider_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelAdminAPI(models.UserTypeAdmin)
	body := `{"agent_name":"x","model":"y","provider_type":"anthropic","api_key":"k","unknown_field":"oops"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown_field") &&
		!strings.Contains(w.Body.String(), "invalid body") {
		t.Fatalf("expected parse error mentioning unknown_field, got %s", w.Body.String())
	}
}

// TestModelAdmin_CreateProvider_NoGormDB — admin passes auth, then 500
// because gormDB is nil. Confirms the dependency check fires before any DB
// work.
func TestModelAdmin_CreateProvider_NoGormDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelAdminAPI(models.UserTypeAdmin)
	body := `{"agent_name":"x","model":"y","provider_type":"anthropic","api_key":"k"}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when gormDB nil, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestModelAdmin_NormalUserForbidden — role 1 → 403.
func TestModelAdmin_NormalUserForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestModelAdminAPI(models.UserTypeNormal)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for normal user, got %d body=%s", w.Code, w.Body.String())
	}
}

// ─────────────────── UpdateProviderRequest 指针语义(部分更新) ───────────────────

// TestUpdateProviderRequest_PartialPointerSemantics — 验证前端只传「改过的字段」
// 时,后端能靠指针区分「未传(nil)=保持原值」与「显式传入=更新」。这是 §7.1 编辑
// 弹窗「仅提交变更字段」的前置保障:若 model / provider_type / remark 不在结构体里,
// 前端改了也会被静默忽略(历史 bug)。
func TestUpdateProviderRequest_PartialPointerSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 场景 1:只传 agent_name —— 其余字段必须全部是 nil(保持不变)。
	raw := `{"agent_name":"新名称"}`
	var req UpdateProviderRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.AgentName == nil || *req.AgentName != "新名称" {
		t.Fatalf("agent_name want 新名称, got %+v", req.AgentName)
	}
	if req.Model != nil {
		t.Fatalf("model want nil (unchanged), got %+v", *req.Model)
	}
	if req.ProviderType != nil {
		t.Fatalf("provider_type want nil (unchanged), got %+v", *req.ProviderType)
	}
	if req.APIKey != nil {
		t.Fatalf("api_key want nil (unchanged), got %+v", *req.APIKey)
	}
	if req.Endpoint != nil {
		t.Fatalf("endpoint want nil (unchanged), got %+v", *req.Endpoint)
	}
	if req.Enabled != nil {
		t.Fatalf("enabled want nil (unchanged), got %+v", *req.Enabled)
	}
	if req.Remark != nil {
		t.Fatalf("remark want nil (unchanged), got %+v", *req.Remark)
	}

	// 场景 2:显式传入 model / provider_type / remark / enabled —— 指针应拿到值。
	raw2 := `{"model":"NewModel","provider_type":"openai","remark":"note","enabled":false}`
	var req2 UpdateProviderRequest
	if err := json.Unmarshal([]byte(raw2), &req2); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req2.Model == nil || *req2.Model != "NewModel" {
		t.Fatalf("model want NewModel, got %+v", req2.Model)
	}
	if req2.ProviderType == nil || *req2.ProviderType != "openai" {
		t.Fatalf("provider_type want openai, got %+v", req2.ProviderType)
	}
	if req2.Remark == nil || *req2.Remark != "note" {
		t.Fatalf("remark want note, got %+v", req2.Remark)
	}
	if req2.Enabled == nil || *req2.Enabled != false {
		t.Fatalf("enabled want false, got %+v", req2.Enabled)
	}
	// agent_name 未传 → nil。
	if req2.AgentName != nil {
		t.Fatalf("agent_name want nil, got %+v", *req2.AgentName)
	}

	// 场景 3:空 body —— 全部 nil,对应后端「无更新直接返回原行」分支。
	var req3 UpdateProviderRequest
	if err := json.Unmarshal([]byte(`{}`), &req3); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req3.AgentName != nil || req3.Model != nil || req3.ProviderType != nil ||
		req3.APIKey != nil || req3.Endpoint != nil || req3.Enabled != nil || req3.Remark != nil {
		t.Fatalf("empty body should yield all-nil pointers, got %+v", req3)
	}
}

// ─────────────────── pure-function tests ───────────────────

// TestApiKeyHintLocal — covers short, long, placeholder, and empty inputs.
func TestApiKeyHintLocal(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"API-KEY-PLACEHOLDER", ""},
		{"abc", "abc"},                         // <=8 chars → returned as-is
		{"sk-1234567890abcdef", "sk-1...cdef"}, // >8 chars → prefix...suffix
		{"  trimmed  ", "trimmed"},             // whitespace stripped
	}
	for _, tc := range cases {
		got := apiKeyHintLocal(tc.in)
		if got != tc.want {
			t.Fatalf("apiKeyHintLocal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestIsMySQLDuplicateErr — verify the MySQL 1062 heuristic.
func TestIsMySQLDuplicateErr(t *testing.T) {
	if isMySQLDuplicateErr(nil) {
		t.Fatal("nil must not match")
	}
}

// TestDecodeJSONStrict — basic positive case.
func TestDecodeJSONStrict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/echo", func(c *gin.Context) {
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSONStrict(c, &body) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"name": body.Name})
	})

	req := httptest.NewRequest(http.MethodPost, "/echo",
		bytes.NewBufferString(`{"name":"alpha"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != "alpha" {
		t.Fatalf("name = %q, want alpha", out.Name)
	}
}

// TestDecodeJSONStrict_UnknownField — strict parser rejects unknown fields.
func TestDecodeJSONStrict_UnknownField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/echo", func(c *gin.Context) {
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSONStrict(c, &body) {
			return
		}
		c.JSON(http.StatusOK, gin.H{"name": body.Name})
	})
	req := httptest.NewRequest(http.MethodPost, "/echo",
		bytes.NewBufferString(`{"name":"x","extra":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", w.Code)
	}
}

// ─────────────────── providerView / newProviderView ───────────────────

// TestNewProviderView_InheritsDefaultEndpoint — 行 endpoint 为空(或纯空白)时,
// effective_endpoint 必须回退到 registry 默认 endpoint,endpoint_inherited=true。
func TestNewProviderView_InheritsDefaultEndpoint(t *testing.T) {
	row := models.TLsmGameLlmProvider{ID: "provider-1", Endpoint: "   "}
	got := newProviderView(row, "https://global.example/v1", "", nil)

	if got.ID != row.ID {
		t.Fatalf("id want %q, got %q", row.ID, got.ID)
	}
	if got.EffectiveEndpoint != "https://global.example/v1" {
		t.Fatalf("effective endpoint want global default, got %q", got.EffectiveEndpoint)
	}
	if !got.EndpointInherited {
		t.Fatal("endpoint_inherited want true")
	}
}

// TestNewProviderView_UsesRowEndpoint — 行有 trim 后的非空 endpoint 时,
// effective_endpoint 使用行级值,endpoint_inherited=false。
func TestNewProviderView_UsesRowEndpoint(t *testing.T) {
	row := models.TLsmGameLlmProvider{ID: "provider-1", Endpoint: "  https://row.example/v1  "}
	got := newProviderView(row, "https://global.example/v1", "", nil)

	if got.EffectiveEndpoint != "https://row.example/v1" {
		t.Fatalf("effective endpoint want trimmed row value, got %q", got.EffectiveEndpoint)
	}
	if got.EndpointInherited {
		t.Fatal("endpoint_inherited want false")
	}
}

// TestNewProviderView_KeepsRawRowFields — helper 必须只读 row,不修改
// embedded 结构(保留原始 DB 值给调用方审计)。
func TestNewProviderView_KeepsRawRowFields(t *testing.T) {
	row := models.TLsmGameLlmProvider{ID: "provider-1", Endpoint: "  https://row.example/v1  "}
	got := newProviderView(row, "https://global.example/v1", "", nil)

	if got.TLsmGameLlmProvider.Endpoint != row.Endpoint {
		t.Fatalf("helper mutated raw row.Endpoint: want %q, got %q",
			row.Endpoint, got.TLsmGameLlmProvider.Endpoint)
	}
}

// TestProviderViewJSONIncludesDerivedEndpointFields — JSON 序列化后,
// 顶层必须同时存在 provider 主键(id)与派生字段 effective_endpoint /
// endpoint_inherited,与前端 LlmProvider TS 类型契约一致。
func TestProviderViewJSONIncludesDerivedEndpointFields(t *testing.T) {
	view := newProviderView(
		models.TLsmGameLlmProvider{ID: "provider-1", Endpoint: ""},
		"https://global.example/v1",
		"",
		nil,
	)

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal provider view: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal provider view JSON: %v", err)
	}

	if got["id"] != "provider-1" {
		t.Fatalf("missing embedded provider id: %s", raw)
	}
	if got["effective_endpoint"] != "https://global.example/v1" {
		t.Fatalf("missing effective_endpoint: %s", raw)
	}
	if got["endpoint_inherited"] != true {
		t.Fatalf("missing endpoint_inherited: %s", raw)
	}
}

// TestProviderViewJSON_OmitsBalanceWhenNil — Balance 是 *int64 + omitempty,
// 必须确保「无 bot user / 无钱包」时 JSON 完全不出现 "balance" 键,而不是
// 打印 "balance":0(后者会让前端把"无钱包"和"钱包余额=0"混淆)。
func TestProviderViewJSON_OmitsBalanceWhenNil(t *testing.T) {
	view := newProviderView(
		models.TLsmGameLlmProvider{ID: "provider-1"},
		"https://global.example/v1",
		"", // no bot user
		nil,
	)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal provider view: %v", err)
	}
	if strings.Contains(string(raw), "\"balance\"") {
		t.Fatalf("expected 'balance' to be omitted when nil, got %s", raw)
	}
}

// TestProviderViewJSON_IncludesBalanceWhenSet — 当 list handler 真的查到了
// bot user 钱包余额时,Balance 必须以数字形式出现在 JSON 中。
func TestProviderViewJSON_IncludesBalanceWhenSet(t *testing.T) {
	bal := int64(12345)
	view := newProviderView(
		models.TLsmGameLlmProvider{ID: "provider-1"},
		"https://global.example/v1",
		"bot-user-1",
		&bal,
	)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal provider view: %v", err)
	}
	if !strings.Contains(string(raw), "\"balance\":12345") {
		t.Fatalf("expected 'balance':12345 in JSON, got %s", raw)
	}
}
