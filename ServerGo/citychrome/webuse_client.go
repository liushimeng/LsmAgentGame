// Package citychrome — webuse_client.go: go-web-debug-tool HTTP client。
//
// 底座为子模块 go-web-debug-tool(独立进程,默认 :29008),6 端点:
// NewChromePage / ControlChromePage / LookChromePageInfo / BatchEval /
// ListChromePages / CloseChromePage。统一信封 {code,message,data},
// code 非 0 即报错;超时 request_timeout_ms;auth_token 非空带 Bearer 头。
//
// 契约: go-web-debug-tool/MCP_*.md(子模块只读参考)。
package citychrome

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebUseClient 是 go-web-debug-tool 的 HTTP 客户端。
type WebUseClient struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

// NewWebUseClient 构造客户端。debugToolURL 形如 "http://127.0.0.1:29008";
// timeoutMs ≤0 时取 90s 缺省(契约 §3.1 request_timeout_ms 默认 90000)。
func NewWebUseClient(debugToolURL, authToken string, timeoutMs int) *WebUseClient {
	if timeoutMs <= 0 {
		timeoutMs = 90000
	}
	return &WebUseClient{
		baseURL:   strings.TrimRight(debugToolURL, "/"),
		authToken: authToken,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
	}
}

// envelope 是底座统一响应信封。
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// apiError 是底座非 0 码的本地错误形态。
type apiError struct {
	code    int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("webuse code=%d: %s", e.code, e.message)
}

// post 发 POST 请求并解信封;code 非 0 返回 *apiError。
func (c *WebUseClient) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("webuse encode %s: %w", path, err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return nil, fmt.Errorf("webuse new request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webuse %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("webuse read %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("webuse decode %s: %w (body %.200s)", path, err, raw)
	}
	if env.Code != 0 {
		return nil, &apiError{code: env.Code, message: env.Message}
	}
	return env.Data, nil
}

// PageInfo 是 NewChromePage 的返回(page_id + 元信息)。
type PageInfo struct {
	PageID   string `json:"page_id"`
	Title    string `json:"title"`
	FinalURL string `json:"final_url"`
}

// NewChromePage POST /NewChromePage。waitUntil 可空(底座默认 load)。
func (c *WebUseClient) NewChromePage(ctx context.Context, url, waitUntil string) (*PageInfo, error) {
	body := map[string]any{"url": url}
	if waitUntil != "" {
		body["wait_until"] = waitUntil
	}
	data, err := c.post(ctx, "/NewChromePage", body)
	if err != nil {
		return nil, err
	}
	var pi PageInfo
	if err := json.Unmarshal(data, &pi); err != nil {
		return nil, fmt.Errorf("webuse NewChromePage decode data: %w", err)
	}
	if pi.PageID == "" {
		return nil, fmt.Errorf("webuse NewChromePage: empty page_id")
	}
	return &pi, nil
}

// ControlResult 是 ControlChromePage 的返回。
type ControlResult struct {
	Result        json.RawMessage `json:"result"`
	SpawnedPageID string          `json:"spawned_page_id"`
}

// ControlChromePage POST /ControlChromePage 透传(action + params)。
// action 白名单校验在 mcp_tools 层做(契约 §4)。
func (c *WebUseClient) ControlChromePage(ctx context.Context, pageID, action string, params map[string]any) (*ControlResult, error) {
	if params == nil {
		params = map[string]any{}
	}
	data, err := c.post(ctx, "/ControlChromePage", map[string]any{
		"page_id": pageID,
		"action":  action,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	var cr ControlResult
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, fmt.Errorf("webuse ControlChromePage decode data: %w", err)
	}
	return &cr, nil
}

// LookResult 是 LookChromePageInfo 的返回(data.info + data.data)。
type LookResult struct {
	Info string          `json:"info"`
	Data json.RawMessage `json:"data"`
}

// LookChromePageInfo POST /LookChromePageInfo 透传(info + params)。
func (c *WebUseClient) LookChromePageInfo(ctx context.Context, pageID, info string, params map[string]any) (*LookResult, error) {
	body := map[string]any{"page_id": pageID, "info": info}
	if params != nil {
		body["params"] = params
	}
	data, err := c.post(ctx, "/LookChromePageInfo", body)
	if err != nil {
		return nil, err
	}
	var lr LookResult
	if err := json.Unmarshal(data, &lr); err != nil {
		return nil, fmt.Errorf("webuse LookChromePageInfo decode data: %w", err)
	}
	return &lr, nil
}

// EvalItem 是 BatchEval 的单项(契约 MCP_BatchEval_Def.md §3.1)。
type EvalItem struct {
	Label  string         `json:"label,omitempty"`
	Params map[string]any `json:"params"`
}

// EvalItemResult 是 BatchEval 单项结果。
type EvalItemResult struct {
	Index   int             `json:"index"`
	Label   string          `json:"label"`
	OK      bool            `json:"ok"`
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

// BatchEvalResult 是 BatchEval 的整体返回。
type BatchEvalResult struct {
	Results    []EvalItemResult `json:"results"`
	Succeeded  int              `json:"succeeded"`
	Failed     int              `json:"failed"`
	DurationMs int64            `json:"duration_ms"`
	Truncated  bool             `json:"truncated"`
}

// BatchEval POST /BatchEval(1-64 条)。items 为空直接报错(底座也拒)。
func (c *WebUseClient) BatchEval(ctx context.Context, pageID string, items []EvalItem) (*BatchEvalResult, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("webuse BatchEval: items empty")
	}
	if len(items) > 64 {
		return nil, fmt.Errorf("webuse BatchEval: items %d exceeds max 64", len(items))
	}
	data, err := c.post(ctx, "/BatchEval", map[string]any{
		"page_id": pageID,
		"items":   items,
	})
	if err != nil {
		return nil, err
	}
	var br BatchEvalResult
	if err := json.Unmarshal(data, &br); err != nil {
		return nil, fmt.Errorf("webuse BatchEval decode data: %w", err)
	}
	return &br, nil
}

// ListedPage 是 ListChromePages 的单页元信息。
type ListedPage struct {
	PageID    string `json:"page_id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// ListChromePages POST /ListChromePages(轻量,也用作健康探测)。
func (c *WebUseClient) ListChromePages(ctx context.Context) ([]ListedPage, error) {
	data, err := c.post(ctx, "/ListChromePages", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Pages []ListedPage `json:"pages"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("webuse ListChromePages decode data: %w", err)
	}
	return out.Pages, nil
}

// CloseChromePage POST /CloseChromePage;page 已不存在(2000)视为已清理成功。
func (c *WebUseClient) CloseChromePage(ctx context.Context, pageID string) error {
	_, err := c.post(ctx, "/CloseChromePage", map[string]any{"page_id": pageID})
	if err != nil {
		if ae, ok := err.(*apiError); ok && ae.code == 2000 {
			return nil // 幂等清理。
		}
		return err
	}
	return nil
}

// Healthy 轻量健康探测(ListChromePages 可达即可)。
func (c *WebUseClient) Healthy(ctx context.Context) bool {
	if c == nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.ListChromePages(probeCtx)
	return err == nil
}
