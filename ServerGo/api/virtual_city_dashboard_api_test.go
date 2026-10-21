// Package api — virtual_city_dashboard_api_test.go: 虚拟城市「实时看板」
// 统一数据体系契约测试(批次69,2026-10-21 §批次69)。
//
// 覆盖:
//	① 维度目录:非空 / id 唯一 / 恰好 11 条 / group 合法 / title_key 前缀;
//	② 数据源登记册:恰好 22 条 / URL 均 https / name 非空;
//	③ 端点 svc=nil ⇒ code 35053 且 dashboard 骨架完整(11/22/offline);
//	④ 端点 svc≠nil ⇒ code 0 且 schema_version=1、generated_at>0。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/config"
	"LsmAgentGame/errcode"
	"LsmAgentGame/game/virtual_city"

	"github.com/gin-gonic/gin"
)

// dashboardRouter 挂载看板端点(绕过鉴权中间件 —— 与其它 virtual_city
// 端点同口径:鉴权由 router.go 的 AuthRequired 统一保证)。
func dashboardRouter(svc *citychrome.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	a := NewVirtualCityDashboardAPI(svc)
	r := gin.New()
	r.GET("/api/virtual_city/dashboard", a.Dashboard)
	return r
}

// getDashboardJSON 打一次看板端点并解析 JSON。
func getDashboardJSON(t *testing.T, r *gin.Engine) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/virtual_city/dashboard", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("http status = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json %q: %v", w.Body.String(), err)
	}
	return body
}

// TestVirtualCityDashboard_Catalog(①+②):维度目录与数据源登记册契约。
func TestVirtualCityDashboard_Catalog(t *testing.T) {
	dims := virtual_city.DashboardDimensions()
	if len(dims) == 0 {
		t.Fatal("dimensions empty")
	}
	// 批次71:追加「校准」维度 ⇒ 11 → 12。
	if len(dims) != 12 {
		t.Fatalf("dimensions = %d, want 12", len(dims))
	}
	seen := map[string]bool{}
	for _, d := range dims {
		if d.ID == "" {
			t.Fatalf("dimension with empty id: %+v", d)
		}
		if seen[d.ID] {
			t.Fatalf("duplicate dimension id %q", d.ID)
		}
		seen[d.ID] = true
		switch d.Group {
		case "city", "market", "external":
		default:
			t.Fatalf("dimension %q group = %q, want city|market|external", d.ID, d.Group)
		}
		if !strings.HasPrefix(d.TitleKey, "virtualCity.dash.dim.") {
			t.Fatalf("dimension %q title_key = %q, want prefix virtualCity.dash.dim.", d.ID, d.TitleKey)
		}
		if d.Source == "" || d.Scope == "" || d.Unit == "" || d.Refresh == "" {
			t.Fatalf("dimension %q has empty enum field: %+v", d.ID, d)
		}
	}

	sources := virtual_city.DashboardDataSources()
	if len(sources) != 22 {
		t.Fatalf("data_sources = %d, want 22", len(sources))
	}
	for _, s := range sources {
		if s.Name == "" {
			t.Fatalf("data source with empty name: %+v", s)
		}
		if !strings.HasPrefix(s.URL, "https://") {
			t.Fatalf("data source %q url = %q, want https:// prefix", s.Name, s.URL)
		}
		if s.Category == "" || s.Purpose == "" {
			t.Fatalf("data source %q missing category/purpose: %+v", s.Name, s)
		}
	}

	if virtual_city.DashboardSchemaVersion() != 1 {
		t.Fatalf("schema version = %d, want 1", virtual_city.DashboardSchemaVersion())
	}

	// 返回副本:调用方改动不得污染包级目录。
	dims[0].ID = "mutated"
	if virtual_city.DashboardDimensions()[0].ID == "mutated" {
		t.Fatal("DashboardDimensions must return a copy, not the package-level slice")
	}
}

// TestVirtualCityDashboard_EndpointOffline(③):svc=nil ⇒ 35053 + 骨架完整。
func TestVirtualCityDashboard_EndpointOffline(t *testing.T) {
	body := getDashboardJSON(t, dashboardRouter(nil))
	if code := int(body["code"].(float64)); code != errcode.ErrCityChromeChannelOffline {
		t.Fatalf("code = %d, want %d (body=%v)", code, errcode.ErrCityChromeChannelOffline, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing: %v", body)
	}
	dash, ok := data["dashboard"].(map[string]any)
	if !ok {
		t.Fatalf("data.dashboard missing: %v", data)
	}
	if got := len(dash["dimensions"].([]any)); got != 12 {
		t.Fatalf("offline dashboard dimensions = %d, want 12", got)
	}
	if got := len(dash["data_sources"].([]any)); got != 22 {
		t.Fatalf("offline dashboard data_sources = %d, want 22", got)
	}
	rt, ok := dash["realtime"].(map[string]any)
	if !ok {
		t.Fatalf("dashboard.realtime missing: %v", dash)
	}
	channel, ok := rt["channel"].(map[string]any)
	if !ok {
		t.Fatalf("realtime.channel missing: %v", rt)
	}
	if channel["status"] != "offline" {
		t.Fatalf("realtime.channel.status = %v, want offline", channel["status"])
	}
	if detail, _ := channel["detail"].(string); detail == "" {
		t.Fatal("realtime.channel.detail must explain the offline reason")
	}
	if got := len(rt["categories"].(map[string]any)); got != 0 {
		t.Fatalf("offline realtime categories = %d, want 0", got)
	}
	if got := len(rt["news"].([]any)); got != 0 {
		t.Fatalf("offline realtime news = %d, want 0", got)
	}
}

// TestVirtualCityDashboard_EndpointOnline(④):svc≠nil ⇒ code 0 + 版本/时间戳。
func TestVirtualCityDashboard_EndpointOnline(t *testing.T) {
	// citychrome.Start 是全局单例;enabled=false ⇒ 离线占位实例(无底座依赖、
	// 不起调度器),但 svc 非 nil ⇒ 端点按「通道已接入」语义返回 code 0。
	svc := citychrome.Start(config.CityChromeConfig{Enabled: false}, nil)
	if svc == nil {
		t.Fatal("citychrome.Start returned nil")
	}
	body := getDashboardJSON(t, dashboardRouter(svc))
	if code := int(body["code"].(float64)); code != errcode.OK {
		t.Fatalf("code = %d, want 0 (body=%v)", code, body)
	}
	dash := body["data"].(map[string]any)["dashboard"].(map[string]any)
	if got := int(dash["schema_version"].(float64)); got != 1 {
		t.Fatalf("schema_version = %d, want 1", got)
	}
	if got := int64(dash["generated_at"].(float64)); got <= 0 {
		t.Fatalf("generated_at = %d, want > 0", got)
	}
	if got := len(dash["dimensions"].([]any)); got != 12 {
		t.Fatalf("dimensions = %d, want 12", got)
	}
	if got := len(dash["data_sources"].([]any)); got != 22 {
		t.Fatalf("data_sources = %d, want 22", got)
	}
	// realtime 与 realtime_board 的 feed 同构:必须带 channel/categories/news。
	rt := dash["realtime"].(map[string]any)
	for _, k := range []string{"channel", "categories", "news"} {
		if _, ok := rt[k]; !ok {
			t.Fatalf("realtime missing key %q: %v", k, rt)
		}
	}
}
