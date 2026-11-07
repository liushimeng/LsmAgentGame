// Package api — virtual_city_realtime_spec_test.go: 批次89 口径卡 / 异常告警
// 字段的 HTTP 出口契约测试。
//
// 覆盖:
//
//	① GET /api/virtual_city/realtime_board 的 data.metric_specs /
//	   data.anomalies 在 svc=nil(离线)与 svc≠nil(在线)两种分支下均为
//	   **数组**(非 null)—— 前端会直接遍历;
//	② GET /api/virtual_city/dashboard 的 data.dashboard.metric_specs /
//	   data.dashboard.anomalies 同上;
//	③ 口径卡条目形状(设计文档 §3.2)与异常条目形状(§3.4)。
package api

import (
	"testing"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/config"
)

// TestVirtualCityRealtimeBoard_MetricSpecsAndAnomalies 批次89:快照端点
// 追加 metric_specs / anomalies(离线分支为空数组而非 null)。
func TestVirtualCityRealtimeBoard_MetricSpecsAndAnomalies(t *testing.T) {
	// 离线分支(svc=nil)。
	body := getRealtimeBoardJSON(t, realtimeBoardRouter(nil))
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing: %v", body)
	}
	for _, k := range []string{"metric_specs", "anomalies"} {
		v, ok := data[k].([]any)
		if !ok {
			t.Fatalf("offline data.%s must be an array (not null): %v", k, data)
		}
		if len(v) != 0 {
			t.Errorf("offline data.%s = %d entries, want 0", k, len(v))
		}
	}

	// 在线分支(svc≠nil):metric_specs 非空,anomalies 为数组(可为空)。
	svc := citychrome.Start(config.CityChromeConfig{Enabled: false}, nil)
	if svc == nil {
		t.Fatal("citychrome.Start returned nil")
	}
	body = getRealtimeBoardJSON(t, realtimeBoardRouter(svc))
	data = body["data"].(map[string]any)
	specs, ok := data["metric_specs"].([]any)
	if !ok {
		t.Fatalf("data.metric_specs must be an array: %v", data)
	}
	if len(specs) < 20 {
		t.Errorf("metric_specs = %d, want >= 20", len(specs))
	}
	anoms, ok := data["anomalies"].([]any)
	if !ok {
		t.Fatalf("data.anomalies must be an array: %v", data)
	}
	_ = anoms
}

// TestVirtualCityDashboard_MetricSpecsAndAnomalies 批次89:看板端点追加
// metric_specs / anomalies(离线分支为空数组而非 null)。
func TestVirtualCityDashboard_MetricSpecsAndAnomalies(t *testing.T) {
	body := getDashboardJSON(t, dashboardRouter(nil))
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing: %v", body)
	}
	dash, ok := data["dashboard"].(map[string]any)
	if !ok {
		t.Fatalf("dashboard missing: %v", data)
	}
	for _, k := range []string{"metric_specs", "anomalies"} {
		v, ok := dash[k].([]any)
		if !ok {
			t.Fatalf("offline dashboard.%s must be an array (not null): %v", k, dash)
		}
		if len(v) != 0 {
			t.Errorf("offline dashboard.%s = %d entries, want 0", k, len(v))
		}
	}

	svc := citychrome.Start(config.CityChromeConfig{Enabled: false}, nil)
	if svc == nil {
		t.Fatal("citychrome.Start returned nil")
	}
	body = getDashboardJSON(t, dashboardRouter(svc))
	dash = body["data"].(map[string]any)["dashboard"].(map[string]any)
	specs, ok := dash["metric_specs"].([]any)
	if !ok {
		t.Fatalf("dashboard.metric_specs must be an array: %v", dash)
	}
	if len(specs) < 20 {
		t.Errorf("dashboard.metric_specs = %d, want >= 20", len(specs))
	}
	if _, ok := dash["anomalies"].([]any); !ok {
		t.Fatalf("dashboard.anomalies must be an array: %v", dash)
	}
}

// TestVirtualCityMetricSpecShape 口径卡条目 JSON 字段名(设计文档 §3.2)。
func TestVirtualCityMetricSpecShape(t *testing.T) {
	specs := citychrome.MetricSpecs()
	if len(specs) == 0 {
		t.Fatal("metric specs empty")
	}
	s := specs[0]
	if s.ID == "" || s.Category == "" || s.Indicator == "" || s.Unit == "" ||
		s.Caliber == "" || s.Formula == "" || s.Source == "" || s.SourceURL == "" || s.Refresh == "" {
		t.Errorf("metric spec has empty field: %+v", s)
	}
}
