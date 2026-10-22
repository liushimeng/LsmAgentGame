// Package api — virtual_city_realtime_api_test.go: 实时看板 Board 端点
// missions 字段形状断言(批次77 §3.4:missions 新→旧 ≤20 条,无记录返回
// [] 非 null;WS 帧结构不动)。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/config"
	"LsmAgentGame/errcode"

	"github.com/gin-gonic/gin"
)

// realtimeBoardRouter 挂载实时看板快照端点(绕过鉴权中间件 —— 与其它
// virtual_city 端点同口径:鉴权由 router.go 的 AuthRequired 统一保证)。
func realtimeBoardRouter(svc *citychrome.Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	a := NewVirtualCityRealtimeAPI(svc)
	r := gin.New()
	r.GET("/api/virtual_city/realtime_board", a.Board)
	return r
}

// getRealtimeBoardJSON 打一次快照端点并解析 JSON。
func getRealtimeBoardJSON(t *testing.T, r *gin.Engine) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/virtual_city/realtime_board", nil)
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

// TestVirtualCityRealtimeBoard_MissionsOffline(svc=nil):code=35053 且
// data.missions 为 [] (非 null)。
func TestVirtualCityRealtimeBoard_MissionsOffline(t *testing.T) {
	body := getRealtimeBoardJSON(t, realtimeBoardRouter(nil))
	if code := int(body["code"].(float64)); code != errcode.ErrCityChromeChannelOffline {
		t.Fatalf("code = %d, want %d (body=%v)", code, errcode.ErrCityChromeChannelOffline, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing: %v", body)
	}
	missions, ok := data["missions"].([]any)
	if !ok {
		t.Fatalf("data.missions must be an array (not null): %v", data)
	}
	if len(missions) != 0 {
		t.Errorf("offline missions = %d, want 0", len(missions))
	}
	if _, ok := data["feed"]; !ok {
		t.Errorf("offline data must still carry feed: %v", data)
	}
}

// TestVirtualCityRealtimeBoard_MissionsOnline(svc≠nil):code=0,missions 为
// 数组(无记录时 []),feed 同构带齐 channel/categories/news。
func TestVirtualCityRealtimeBoard_MissionsOnline(t *testing.T) {
	svc := citychrome.Start(config.CityChromeConfig{Enabled: false}, nil)
	if svc == nil {
		t.Fatal("citychrome.Start returned nil")
	}
	body := getRealtimeBoardJSON(t, realtimeBoardRouter(svc))
	if code := int(body["code"].(float64)); code != errcode.OK {
		t.Fatalf("code = %d, want 0 (body=%v)", code, body)
	}
	data := body["data"].(map[string]any)
	missions, ok := data["missions"].([]any)
	if !ok {
		t.Fatalf("data.missions must be an array (not null): %v", data)
	}
	if len(missions) > 20 {
		t.Errorf("missions = %d, want <= 20 (环形上限)", len(missions))
	}
	feed, ok := data["feed"].(map[string]any)
	if !ok {
		t.Fatalf("data.feed missing: %v", data)
	}
	for _, k := range []string{"channel", "categories", "news"} {
		if _, ok := feed[k]; !ok {
			t.Errorf("feed missing key %q: %v", k, feed)
		}
	}
}

// TestVirtualCityRealtimeBoard_MissionRecordShape 遥测记录经 HTTP 序列化后
// 的 JSON 字段名(批次77 §3.2 契约)。
func TestVirtualCityRealtimeBoard_MissionRecordShape(t *testing.T) {
	svc := citychrome.Start(config.CityChromeConfig{Enabled: false}, nil)
	rec := citychrome.MissionRecord{
		ID: "a_share-1", Category: citychrome.CategoryAShare, StartedAt: 1000,
		DurationMs: 1234, Status: citychrome.MissionStatusOK, Source: "腾讯行情",
		StepsUsed: 3, MaxSteps: 14, Plan: "打开腾讯行情 → 提取 → 提交",
		Quotes: 5, News: 2, Error: "",
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "category", "started_at", "duration_ms", "status",
		"source", "steps_used", "max_steps", "plan", "quotes", "news", "error"} {
		if _, ok := got[k]; !ok {
			t.Errorf("mission record json missing key %q: %s", k, raw)
		}
	}
	if svc == nil {
		t.Fatal("citychrome.Start returned nil")
	}
}
