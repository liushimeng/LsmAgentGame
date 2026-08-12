// Package llm — BUG-R214 registry.HealthCheck 多端点探测回归测试。
//
// 修复前 HealthCheck 只对 legacy 标量 r.endpoint 发 HEAD,却把整个
// r.endpoints failover 列表原样塞进 status.Endpoints 供 UI 展示 —— 备用端点
// **从未被探测过**。一台备用主机彻底不可达时,健康页照样绿灯,直到 13 个
// bot 在房间里集体挂满 dial 超时(报告 20260801_061438)才暴露。
//
// 本文件断言 4 条不变式:
//
//	H01 每个配置的端点都被真实探测,结果逐条落在 EndpointStatuses;
//	H02 主端点死、备用端点活 → OK=true 且 per-endpoint 明细如实区分;
//	H03 全部端点死 → OK=false 且 LastError 含每个端点的失败原因;
//	H04 既有字段(OK/Endpoint/Endpoints/LastError/UsableKeys)语义不变,
//	    Health() 缓存读能回放 per-endpoint 明细。
package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LsmWebGame/config"
	"LsmWebGame/llm"
)

// deadURL 起一个 httptest server 再立刻关闭,拿到一个"曾经监听、现已关闭"
// 的环回端口 —— 本地立即 ECONNREFUSED,不做任何外部网络调用。
func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	u := srv.URL
	srv.Close()
	return u
}

// TestR214_HealthCheck_ProbesEveryEndpoint (H01/H02) —— 主端点死、备用端点
// 活时,两个端点都必须被探测,且 per-endpoint 明细如实区分。
func TestR214_HealthCheck_ProbesEveryEndpoint(t *testing.T) {
	dead := deadURL(t)
	var aliveHits int32
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		atomic.AddInt32(&aliveHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer alive.Close()

	cfg := config.LLMConfig{
		Endpoint:   dead,
		Endpoints:  []string{dead, alive.URL},
		TimeoutMs:  2000,
		MaxRetries: 0,
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "A-model", APIKey: "key-a", ProviderType: "anthropic"},
		},
	}
	r := llm.NewRegistry(cfg)
	got := r.HealthCheck(context.Background())

	if len(got.EndpointStatuses) != 2 {
		t.Fatalf("EndpointStatuses len = %d, want 2 —— 备用端点未被探测(BUG-R214 回归)", len(got.EndpointStatuses))
	}
	if atomic.LoadInt32(&aliveHits) < 1 {
		t.Errorf("secondary endpoint was never probed (hits=0) —— HealthCheck 仍只探测标量 endpoint")
	}

	byEndpoint := map[string]llm.EndpointHealth{}
	for _, es := range got.EndpointStatuses {
		byEndpoint[es.Endpoint] = es
	}
	if es, ok := byEndpoint[dead]; !ok {
		t.Errorf("dead endpoint %q missing from EndpointStatuses", dead)
	} else {
		if es.OK {
			t.Errorf("dead endpoint reported OK=true")
		}
		if es.LastError == "" {
			t.Errorf("dead endpoint has empty LastError")
		}
	}
	if es, ok := byEndpoint[alive.URL]; !ok {
		t.Errorf("alive endpoint %q missing from EndpointStatuses", alive.URL)
	} else if !es.OK {
		t.Errorf("alive endpoint reported OK=false: %s", es.LastError)
	} else if es.StatusCode != http.StatusOK {
		t.Errorf("alive endpoint StatusCode = %d, want 200", es.StatusCode)
	}

	// 聚合语义:有 failover 时"任一端点活"即整体可用。
	if !got.OK {
		t.Errorf("aggregate OK = false while a healthy secondary exists (last_error=%q)", got.LastError)
	}
}

// TestR214_HealthCheck_AllDeadReportsEach (H03) —— 全部端点不可达时 OK=false,
// LastError 必须点名每个端点,而不是只回一句主端点的错误。
func TestR214_HealthCheck_AllDeadReportsEach(t *testing.T) {
	d1 := deadURL(t)
	d2 := deadURL(t)

	cfg := config.LLMConfig{
		Endpoint:   d1,
		Endpoints:  []string{d1, d2},
		TimeoutMs:  1000,
		MaxRetries: 0,
	}
	r := llm.NewRegistry(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got := r.HealthCheck(ctx)

	if got.OK {
		t.Fatalf("expected OK=false when every endpoint is unreachable")
	}
	if len(got.EndpointStatuses) != 2 {
		t.Fatalf("EndpointStatuses len = %d, want 2", len(got.EndpointStatuses))
	}
	for _, ep := range []string{d1, d2} {
		if !strings.Contains(got.LastError, ep) {
			t.Errorf("LastError %q does not name endpoint %q —— operator 无法分辨哪一跳死了", got.LastError, ep)
		}
	}
}

// TestR214_HealthCheck_BackwardCompatibleShape (H04) —— 既有字段语义不变,
// 且 Health() 的缓存读能回放 per-endpoint 明细。/api/llm/health badge、
// main.go 启动日志、api/model_admin_api.go 都只按名取既有字段。
func TestR214_HealthCheck_BackwardCompatibleShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.LLMConfig{
		Endpoint:   srv.URL,
		TimeoutMs:  2000,
		MaxRetries: 0,
		Providers: []config.ProviderConfig{
			{AgentName: "A", Model: "A-model", APIKey: "key-a", ProviderType: "anthropic"},
		},
	}
	r := llm.NewRegistry(cfg)
	got := r.HealthCheck(context.Background())

	if !got.OK {
		t.Fatalf("single healthy endpoint: OK=false, err=%q", got.LastError)
	}
	if got.Endpoint != srv.URL {
		t.Errorf("Endpoint = %q, want %q (既有字段语义必须不变)", got.Endpoint, srv.URL)
	}
	if got.UsableKeys != 1 {
		t.Errorf("UsableKeys = %d, want 1", got.UsableKeys)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty on a healthy probe", got.LastError)
	}

	cached := r.Health()
	if !cached.OK {
		t.Errorf("Health() should report OK after a successful probe")
	}
	if len(cached.EndpointStatuses) != 1 {
		t.Errorf("Health() EndpointStatuses len = %d, want 1 —— 缓存读未回放 per-endpoint 明细", len(cached.EndpointStatuses))
	}
}

// TestR214_HealthCheck_NoEndpointStillReports —— 空配置分支不 panic,
// 且保持修复前的 "no endpoint configured" 文案(既有测试依赖它)。
func TestR214_HealthCheck_NoEndpointStillReports(t *testing.T) {
	r := llm.NewRegistry(config.LLMConfig{})
	got := r.HealthCheck(context.Background())
	if got.OK {
		t.Fatalf("expected OK=false on empty endpoint config")
	}
	if !strings.Contains(got.LastError, "no endpoint") {
		t.Errorf("LastError = %q, want it to mention 'no endpoint'", got.LastError)
	}
}
