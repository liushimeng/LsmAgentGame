// Package citychrome — webuse_client_test.go: go-web-debug-tool client 单测
// (契约 §7.1 第 1 条:信封解析 mock HTTP)。
package citychrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newMockServer 起 mock 底座:handler 收 (path, body map) 返回 (code, message, data)。
func newMockServer(t *testing.T, handler func(path string, body map[string]any) (int, string, any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		code, msg, data := handler(r.URL.Path, body)
		resp := map[string]any{"code": code, "message": msg, "data": data}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestWebUseClient_NewChromePage_OK(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		if path != "/NewChromePage" {
			return 1000, "bad path", nil
		}
		if body["url"] != "https://example.com" {
			return 1001, "missing url", nil
		}
		return 0, "ok", map[string]any{"page_id": "p_abc123", "title": "示例", "final_url": "https://example.com/"}
	})
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "", 5000)
	pi, err := c.NewChromePage(context.Background(), "https://example.com", "")
	if err != nil {
		t.Fatalf("NewChromePage: %v", err)
	}
	if pi.PageID != "p_abc123" || pi.Title != "示例" {
		t.Errorf("unexpected PageInfo: %+v", pi)
	}
}

func TestWebUseClient_EnvelopeNonZeroError(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		return 2002, "selector not found", nil
	})
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "", 5000)
	_, err := c.NewChromePage(context.Background(), "https://example.com", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "2002") || !strings.Contains(err.Error(), "selector not found") {
		t.Errorf("error should carry code+message: %v", err)
	}
}

func TestWebUseClient_AuthBearerHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": map[string]any{"pages": []any{}}})
	}))
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "tok123", 5000)
	if _, err := c.ListChromePages(context.Background()); err != nil {
		t.Fatalf("ListChromePages: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q, want Bearer tok123", gotAuth)
	}
}

func TestWebUseClient_BatchEval_Parse(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		items, _ := body["items"].([]any)
		if len(items) != 2 {
			return 1001, "items invalid", nil
		}
		return 0, "ok", map[string]any{
			"results": []any{
				map[string]any{"index": 0, "label": "a", "ok": true, "code": 0, "result": map[string]any{"type": "number", "value": 42}},
				map[string]any{"index": 1, "label": "b", "ok": false, "code": 2002, "message": "boom"},
			},
			"succeeded": 1, "failed": 1, "duration_ms": 7, "truncated": false,
		}
	})
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "", 5000)
	br, err := c.BatchEval(context.Background(), "p_x", []EvalItem{
		{Label: "a", Params: map[string]any{"expression": "1"}},
		{Label: "b", Params: map[string]any{"expression": "2"}},
	})
	if err != nil {
		t.Fatalf("BatchEval: %v", err)
	}
	if br.Succeeded != 1 || br.Failed != 1 || len(br.Results) != 2 {
		t.Errorf("unexpected BatchEvalResult: %+v", br)
	}
	if br.Results[0].Result == nil {
		t.Error("results[0].Result should be preserved")
	}
}

func TestWebUseClient_BatchEval_ItemsLimits(t *testing.T) {
	c := NewWebUseClient("http://127.0.0.1:1", "", 1000)
	if _, err := c.BatchEval(context.Background(), "p_x", nil); err == nil {
		t.Error("empty items should fail")
	}
	big := make([]EvalItem, 65)
	for i := range big {
		big[i] = EvalItem{Params: map[string]any{"expression": "1"}}
	}
	if _, err := c.BatchEval(context.Background(), "p_x", big); err == nil {
		t.Error("65 items should fail")
	}
}

func TestWebUseClient_ControlAndLookAndClose(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/ControlChromePage":
			return 0, "ok", map[string]any{"result": true, "spawned_page_id": "p_spawn"}
		case "/LookChromePageInfo":
			return 0, "ok", map[string]any{"info": body["info"], "data": map[string]any{"ok": true}}
		case "/CloseChromePage":
			return 0, "ok", map[string]any{"closed": true}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "", 5000)
	ctx := context.Background()

	cr, err := c.ControlChromePage(ctx, "p_a", "click", map[string]any{"selector": "#x"})
	if err != nil || cr.SpawnedPageID != "p_spawn" {
		t.Errorf("ControlChromePage: %+v err=%v", cr, err)
	}
	lr, err := c.LookChromePageInfo(ctx, "p_a", "console", nil)
	if err != nil || lr.Info != "console" {
		t.Errorf("LookChromePageInfo: %+v err=%v", lr, err)
	}
	if err := c.CloseChromePage(ctx, "p_a"); err != nil {
		t.Errorf("CloseChromePage: %v", err)
	}
}

func TestWebUseClient_ClosePage_NotFoundIdempotent(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		return 2000, "page_id not found", nil
	})
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "", 5000)
	if err := c.CloseChromePage(context.Background(), "p_gone"); err != nil {
		t.Errorf("2000 on close should be idempotent success: %v", err)
	}
}

func TestWebUseClient_Healthy(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		return 0, "ok", map[string]any{"pages": []any{}}
	})
	defer srv.Close()
	c := NewWebUseClient(srv.URL, "", 5000)
	if !c.Healthy(context.Background()) {
		t.Error("Healthy should be true against reachable mock")
	}
	dead := NewWebUseClient("http://127.0.0.1:1", "", 500)
	if dead.Healthy(context.Background()) {
		t.Error("Healthy should be false against unreachable server")
	}
}

func TestWebUseClient_TimeoutDefault(t *testing.T) {
	c := NewWebUseClient("http://x", "", 0)
	if c.httpClient.Timeout != 90*time.Second {
		t.Errorf("default timeout = %v, want 90s", c.httpClient.Timeout)
	}
}
