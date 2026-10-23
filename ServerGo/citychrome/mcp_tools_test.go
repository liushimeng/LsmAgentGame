// Package citychrome — mcp_tools_test.go: web_done 校验 + web_control 白名单
// 单测(契约 §7.1 第 1 条)。
package citychrome

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func payloadMap(t *testing.T, category, source string, quotes, news []map[string]any) map[string]any {
	t.Helper()
	return map[string]any{"category": category, "source": source, "quotes": quotes, "news": news}
}

func TestValidatePayload_OK(t *testing.T) {
	raw := payloadMap(t, "a_share", "东方财富",
		[]map[string]any{
			{"symbol": "000001", "name": "上证指数", "price": 3221.5, "change_pct": 0.82, "currency": "CNY"},
			{"symbol": "000002", "name": "深证成指", "price": 10100.0, "change_pct": -0.3},
		},
		[]map[string]any{
			{"title": "头条", "url": "https://x/1", "published_at": 1729300000},
		})
	p, err := ValidatePayload(12, raw)
	if err != nil {
		t.Fatalf("ValidatePayload: %v", err)
	}
	if p.Category != "a_share" || len(p.Quotes) != 2 || len(p.News) != 1 {
		t.Errorf("payload = %+v", p)
	}
}

func TestValidatePayload_UnknownCategoryRejected(t *testing.T) {
	raw := payloadMap(t, "mars", "s", nil, nil)
	if _, err := ValidatePayload(12, raw); err == nil || !strings.Contains(err.Error(), "未知 category") {
		t.Errorf("unknown category should reject: %v", err)
	}
	// general 不是合法 mission 类目。
	raw = payloadMap(t, "general", "s", nil, nil)
	if _, err := ValidatePayload(12, raw); err == nil {
		t.Error("general should not be a valid mission category")
	}
}

func TestValidatePayload_DirtyRowsDroppedNotWholePackage(t *testing.T) {
	raw := payloadMap(t, "crypto", "CoinGecko",
		[]map[string]any{
			{"symbol": "BTC", "name": "比特币", "price": 65000.0, "change_pct": 2.1},
			{"symbol": "", "name": "坏", "price": 1.0, "change_pct": 1},     // symbol 空
			{"symbol": "X", "name": "坏价", "price": "abc", "change_pct": 1}, // 非数值 → unmarshal 失败
		}, nil)
	// 非数值 price 会让整个 unmarshal 失败(JSON 类型严格)——这也是"整包拒绝"的合法形态。
	if _, err := ValidatePayload(12, raw); err == nil {
		t.Fatal("非数值 price 应整包拒绝(JSON 类型错)")
	}
	// 数值合法但语义脏:单条丢弃。
	raw = payloadMap(t, "crypto", "CoinGecko",
		[]map[string]any{
			{"symbol": "BTC", "name": "比特币", "price": 65000.0, "change_pct": 2.1},
			{"symbol": "X", "name": "零价", "price": 0, "change_pct": 0},
			{"symbol": "Y", "name": "超幅", "price": 1, "change_pct": 150},
		},
		[]map[string]any{
			{"title": "好新闻", "url": "https://x/1"},
			{"title": " ", "url": "https://x/2"}, // 空白标题
		})
	p, err := ValidatePayload(12, raw)
	if err != nil {
		t.Fatalf("ValidatePayload: %v", err)
	}
	if len(p.Quotes) != 1 || p.Quotes[0].Symbol != "BTC" {
		t.Errorf("dirty quotes should be dropped, got %+v", p.Quotes)
	}
	if len(p.News) != 1 {
		t.Errorf("blank-title news should be dropped, got %+v", p.News)
	}
}

func TestValidatePayload_QuotesTruncated(t *testing.T) {
	quotes := make([]map[string]any, 0, 15)
	for i := 0; i < 15; i++ {
		quotes = append(quotes, map[string]any{
			"symbol": string(rune('A' + i)), "name": "n", "price": float64(i + 1), "change_pct": 0.0,
		})
	}
	p, err := ValidatePayload(12, payloadMap(t, "hk", "s", quotes, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Quotes) != 12 {
		t.Errorf("quotes should truncate to 12, got %d", len(p.Quotes))
	}
}

func TestValidatePayload_EmptySourceRejected(t *testing.T) {
	if _, err := ValidatePayload(12, payloadMap(t, "us", "", nil, nil)); err == nil {
		t.Error("empty source should reject")
	}
}

// TestValidatePayload_SourceTruncated source 语义是站点名:Agent 塞入长解释
// (E2E 实机:「财联社·电报(cls.cn/telegraph,发布时间为…)」)时按 rune 上限
// 截断,防污染看板来源列。
func TestValidatePayload_SourceTruncated(t *testing.T) {
	long := "财联社·电报(cls.cn/telegraph,发布时间为页面标注的北京时间2026-10-22换算Unix秒)"
	p, err := ValidatePayload(12, payloadMap(t, "news", long, nil, nil))
	if err != nil {
		t.Fatalf("ValidatePayload: %v", err)
	}
	if p.Source == long {
		t.Error("overlong source should be truncated")
	}
	if n := utf8.RuneCountInString(p.Source); n > missionSourceMaxRunes+utf8.RuneCountInString("…(截断)") {
		t.Errorf("source runes = %d, want <= %d", n, missionSourceMaxRunes+5)
	}
	if !strings.HasPrefix(p.Source, "财联社·电报") {
		t.Errorf("truncated source should keep the site-name prefix: %q", p.Source)
	}
	// 短 source 原样保留(不误伤)。
	p, err = ValidatePayload(12, payloadMap(t, "news", "财联社·电报", nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != "财联社·电报" {
		t.Errorf("short source must be preserved, got %q", p.Source)
	}
}

func TestValidatePayload_NewsOnlyMissionAllowed(t *testing.T) {
	p, err := ValidatePayload(12, payloadMap(t, "metal", "LBMA", nil,
		[]map[string]any{{"title": "金价新高", "url": "https://x/g"}}))
	if err != nil || len(p.News) != 1 {
		t.Errorf("news-only mission should pass: %+v %v", p, err)
	}
}

func TestToolset_Dispatch_ActionWhitelist(t *testing.T) {
	ts := NewToolset(nil, 12, 20) // client nil:白名单拒绝在触碰 client 之前发生。
	text, isErr := ts.Dispatch(context.Background(), ToolWebControl, map[string]any{
		"page_id": "p_a", "action": "delete_hard_drive", "params": map[string]any{},
	})
	if !isErr || !strings.Contains(text, "不在白名单") {
		t.Errorf("offlist action should be rejected: %q err=%v", text, isErr)
	}
	// eval_js 在白名单内(应尝试走 client 并因 nil client panic?——不应 panic,要 recover 或先校验)。
	// nil client 会 nil 指针 panic,这里只验证白名单含 eval_js。
	if !webControlActions["eval_js"] || !webControlActions["new_tab"] || !webControlActions["click"] {
		t.Error("whitelist must include eval_js/new_tab/click")
	}
}

func TestToolset_Dispatch_WebLookWhitelist(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebLook, map[string]any{
		"page_id": "p_a", "info": "filesystem",
	})
	if !isErr || !strings.Contains(text, "不在白名单") {
		t.Errorf("offlist info should be rejected: %q", text)
	}
}

func TestToolset_Dispatch_WebDone(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	raw, _ := json.Marshal(payloadMap(t, "futures", "上期所",
		[]map[string]any{{"symbol": "RB", "name": "螺纹钢", "price": 3500.0, "change_pct": 1.2}}, nil))
	var payload any
	_ = json.Unmarshal(raw, &payload)
	text, isErr := ts.Dispatch(context.Background(), ToolWebDone, map[string]any{"payload": payload})
	if isErr {
		t.Fatalf("web_done should pass: %q", text)
	}
	if ts.Done == nil || ts.Done.Category != "futures" {
		t.Errorf("toolset.Done = %+v", ts.Done)
	}
	// 非法 payload → 错误且 Done 不置位。
	text, isErr = ts.Dispatch(context.Background(), ToolWebDone, map[string]any{
		"payload": map[string]any{"category": "nope", "source": "s"},
	})
	if !isErr || ts.Done.Category != "futures" {
		t.Errorf("bad payload should error and not overwrite Done: %q done=%+v", text, ts.Done)
	}
}

func TestToolset_Dispatch_UnknownTool(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	if _, isErr := ts.Dispatch(context.Background(), "web_hack", nil); !isErr {
		t.Error("unknown tool should error")
	}
}

func TestBuildTools_TenTools(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 11 {
		t.Fatalf("BuildTools = %d, want 11 (批次77 §3.1:7→10;批次78 §3.2:10→11)", len(tools))
	}
	names := map[string]bool{}
	for _, td := range tools {
		names[td.Name] = true
		if td.Description == "" || td.InputSchema == nil {
			t.Errorf("tool %s missing description/schema", td.Name)
		}
	}
	for _, want := range []string{ToolWebOpen, ToolWebControl, ToolWebEval, ToolWebLook,
		ToolWebList, ToolWebClose, ToolWebDone, ToolWebPlan, ToolWebRead, ToolWebSearch,
		ToolWebParse} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestParseEvalItems(t *testing.T) {
	items, err := parseEvalItems([]any{
		map[string]any{"label": "a", "expression": "1+1", "await_promise": true},
		map[string]any{"expression": "document.title"},
	})
	if err != nil || len(items) != 2 {
		t.Fatalf("items = %+v err=%v", items, err)
	}
	if items[0].Label != "a" || items[0].Params["await_promise"] != true {
		t.Errorf("item0 = %+v", items[0])
	}
	if _, err := parseEvalItems([]any{map[string]any{"label": "x"}}); err == nil {
		t.Error("missing expression should fail")
	}
	if _, err := parseEvalItems("nope"); err == nil {
		t.Error("non-array should fail")
	}
}

// ── 批次77 §3.1 新工具单测(web_plan / web_read / web_search)──

// TestToolset_WebPlan_OK 规划记录:goal + steps 以 " → " 连接写进 Toolset.Plan。
func TestToolset_WebPlan_OK(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebPlan, map[string]any{
		"goal":  "采 A 股指数",
		"steps": []any{"打开腾讯行情", "提取指数", "web_done 提交"},
	})
	if isErr {
		t.Fatalf("web_plan should pass: %q", text)
	}
	if !strings.Contains(text, "已记录规划 3 步") {
		t.Errorf("result should announce step count: %q", text)
	}
	if want := "采 A 股指数：打开腾讯行情 → 提取指数 → web_done 提交"; ts.Plan != want {
		t.Errorf("Plan = %q, want %q", ts.Plan, want)
	}
	// goal 缺省 ⇒ 只 join steps。
	text, isErr = ts.Dispatch(context.Background(), ToolWebPlan, map[string]any{
		"steps": []any{"第一步", "第二步"},
	})
	if isErr || !strings.Contains(text, "已记录规划 2 步") {
		t.Errorf("plan without goal should pass: %q err=%v", text, isErr)
	}
	if want := "第一步 → 第二步"; ts.Plan != want {
		t.Errorf("Plan = %q, want %q", ts.Plan, want)
	}
}

// TestToolset_WebPlan_Rejects 空 steps / 超 8 条 / 超长步 / 超长 goal 全部拒绝。
func TestToolset_WebPlan_Rejects(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"空 steps", map[string]any{"steps": []any{}}, "steps"},
		{"steps 缺失", map[string]any{"goal": "g"}, "steps"},
		{"空白步骤", map[string]any{"steps": []any{"  "}}, "为空"},
		{"steps 非数组", map[string]any{"steps": "abc"}, "steps"},
		{"goal 超长", map[string]any{"goal": strings.Repeat("长", 81), "steps": []any{"a"}}, "goal"},
		{"步骤超长", map[string]any{"steps": []any{strings.Repeat("长", 81)}}, "80 字"},
	}
	for _, c := range cases {
		text, isErr := ts.Dispatch(context.Background(), ToolWebPlan, c.input)
		if !isErr {
			t.Errorf("%s: should be rejected, got %q", c.name, text)
			continue
		}
		if !strings.Contains(text, c.want) {
			t.Errorf("%s: error should mention %q, got %q", c.name, c.want, text)
		}
	}
	// 9 条 steps 超上限。
	nine := make([]any, 9)
	for i := range nine {
		nine[i] = "步骤"
	}
	if text, isErr := ts.Dispatch(context.Background(), ToolWebPlan, map[string]any{"steps": nine}); !isErr {
		t.Errorf("9 steps should be rejected: %q", text)
	}
	// 全部拒绝后 Plan 不得被写入。
	if ts.Plan != "" {
		t.Errorf("rejected plans must not touch Plan, got %q", ts.Plan)
	}
}

// TestToolset_WebRead_RejectsBadURL 非 http(s) URL 拒绝(client 为 nil 时也
// 不得触碰底座 ⇒ 校验必须先于 HTTP 调用)。
func TestToolset_WebRead_RejectsBadURL(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	for _, bad := range []string{"", "ftp://x/y", "example.com", "javascript:alert(1)"} {
		text, isErr := ts.Dispatch(context.Background(), ToolWebRead, map[string]any{"url": bad})
		if !isErr || !strings.Contains(text, "http://") {
			t.Errorf("url %q should be rejected, got %q err=%v", bad, text, isErr)
		}
	}
	if len(ts.OpenedPages) != 0 {
		t.Errorf("rejected web_read must not open pages: %v", ts.OpenedPages)
	}
}

// TestToolset_WebRead_ViaFakeBase web_read 走 httptest 假底座:开页 + 两条
// BatchEval(title/正文) → 返回含 page_id/title/正文,并登记 OpenedPages。
func TestToolset_WebRead_ViaFakeBase(t *testing.T) {
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			if body["url"] != "https://quote.example/idx" {
				return 1001, "unexpected url", nil
			}
			return 0, "ok", map[string]any{"page_id": "p_read1", "title": "行情页", "final_url": "https://quote.example/idx"}
		case "/BatchEval":
			items, _ := body["items"].([]any)
			if len(items) != 2 {
				return 1002, "want 2 eval items", nil
			}
			return 0, "ok", map[string]any{
				"results": []any{
					map[string]any{"index": 0, "ok": true, "result": map[string]any{"type": "string", "value": "上证指数行情"}},
					map[string]any{"index": 1, "ok": true, "result": map[string]any{"type": "string", "value": "沪指 3221.5 涨 0.82%"}},
				},
				"succeeded": 2, "failed": 0,
			}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebRead,
		map[string]any{"url": "https://quote.example/idx"})
	if isErr {
		t.Fatalf("web_read should pass: %q", text)
	}
	for _, want := range []string{"page_id=p_read1", "title=上证指数行情", "沪指 3221.5 涨 0.82%"} {
		if !strings.Contains(text, want) {
			t.Errorf("web_read result missing %q: %s", want, text)
		}
	}
	if len(ts.OpenedPages) != 1 || ts.OpenedPages[0] != "p_read1" {
		t.Errorf("OpenedPages = %v, want [p_read1]", ts.OpenedPages)
	}
}

// TestToolset_WebSearch_ViaFakeBase web_search:开必应 SERP → 提取 → 立即关页
// (SERP 页不进 OpenedPages)。
func TestToolset_WebSearch_ViaFakeBase(t *testing.T) {
	var closed []string
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			u, _ := body["url"].(string)
			if !strings.HasPrefix(u, "https://cn.bing.com/search?q=") || !strings.HasSuffix(u, "&ensearch=0") {
				return 1001, "unexpected serp url: " + u, nil
			}
			return 0, "ok", map[string]any{"page_id": "p_serp", "title": "必应", "final_url": u}
		case "/BatchEval":
			return 0, "ok", map[string]any{
				"results": []any{
					map[string]any{"index": 0, "ok": true, "result": map[string]any{
						"type":  "string",
						"value": `[{"title":"上证指数实时行情","url":"https://x/1","snippet":"3200 点震荡"}]`,
					}},
				},
				"succeeded": 1,
			}
		case "/CloseChromePage":
			id, _ := body["page_id"].(string)
			closed = append(closed, id)
			return 0, "ok", map[string]any{"closed": true}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebSearch,
		map[string]any{"query": "上证指数 实时行情"})
	if isErr {
		t.Fatalf("web_search should pass: %q", text)
	}
	if !strings.Contains(text, "上证指数实时行情") || !strings.Contains(text, "3200 点震荡") {
		t.Errorf("web_search should return extracted items: %s", text)
	}
	if len(closed) != 1 || closed[0] != "p_serp" {
		t.Errorf("SERP page must be closed immediately, closed = %v", closed)
	}
	if len(ts.OpenedPages) != 0 {
		t.Errorf("SERP page must not enter OpenedPages: %v", ts.OpenedPages)
	}
}

// TestToolset_WebSearch_Rejects 空 query / 超长 query 拒绝(client 为 nil)。
func TestToolset_WebSearch_Rejects(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	if text, isErr := ts.Dispatch(context.Background(), ToolWebSearch, map[string]any{"query": "  "}); !isErr {
		t.Errorf("empty query should be rejected: %q", text)
	}
	if text, isErr := ts.Dispatch(context.Background(), ToolWebSearch,
		map[string]any{"query": strings.Repeat("词", 101)}); !isErr {
		t.Errorf("overlong query should be rejected: %q", text)
	}
}

// ── 批次78 §3.2 新增/增强分支单测 ───────────────────────────────────

// TestEvalStringValue_UnescapesRawJSONString 底座 BatchEval 的 result 是
// **裸 JSON 字符串**(带外层引号 + \" 转义)。evalStringValue 必须解一次转义,
// 否则正则 `v_xxx="([^"]*)"` 永远匹配不到(确定性解析彻底失效)。
func TestEvalStringValue_UnescapesRawJSONString(t *testing.T) {
	raw := json.RawMessage(`"v_sh000001=\"1~上证指数~000001~3658.03~3713.03~\""`)
	got := evalStringValue(raw)
	if strings.Contains(got, `\"`) {
		t.Errorf("result must be unescaped, got %q", got)
	}
	if got != `v_sh000001="1~上证指数~000001~3658.03~3713.03~"` {
		t.Errorf("unescaped = %q", got)
	}
	// 兼容形状:{"type":"string","value":"..."}。
	got2 := evalStringValue(json.RawMessage(`{"type":"string","value":"hello"}`))
	if got2 != "hello" {
		t.Errorf("legacy shape = %q, want hello", got2)
	}
	// 空 result。
	if evalStringValue(json.RawMessage(nil)) != "" {
		t.Error("nil result should be empty string")
	}
}

// TestToolset_WebRead_ParseBranch web_read 的 parse 分支:结构化块**在前**,
// 正文在后;正文来自底座的裸 JSON 字符串形态(必须已解转义)。
func TestToolset_WebRead_ParseBranch(t *testing.T) {
	quote := `v_sh000001="1~上证指数~000001~3658.03~3713.03~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~20261022161500~-55.00~-1.48~3714.48~3649.86";`
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			return 0, "ok", map[string]any{"page_id": "p_parse", "title": "行情", "final_url": "https://qt.gtimg.cn/"}
		case "/BatchEval":
			return 0, "ok", map[string]any{
				"results": []any{
					map[string]any{"index": 0, "ok": true, "result": "腾讯行情"},
					// 裸 JSON 字符串形态(真实底座):必须解转义后再解析。
					map[string]any{"index": 1, "ok": true, "result": quote},
				},
				"succeeded": 2,
			}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebRead,
		map[string]any{"url": "https://qt.gtimg.cn/q=sh000001"})
	if isErr {
		t.Fatalf("web_read should pass: %q", text)
	}
	blockIdx := strings.Index(text, "【结构化解析")
	bodyIdx := strings.Index(text, "正文如下")
	if blockIdx < 0 {
		t.Fatalf("structured block missing: %s", text)
	}
	if bodyIdx < 0 || blockIdx > bodyIdx {
		t.Errorf("structured block must come BEFORE the body snippet: %s", text)
	}
	if !strings.Contains(text, "auto→tencent_quote") {
		t.Errorf("block header mismatch: %s", text)
	}
	if !strings.Contains(text, `"symbol":"000001"`) {
		t.Errorf("block rows missing: %s", text)
	}
	// 站点访问轨迹已登记(批次78 §3.3-5)。
	if len(ts.VisitedURLs) != 1 || ts.VisitedURLs[0] != "https://qt.gtimg.cn/q=sh000001" {
		t.Errorf("VisitedURLs = %v", ts.VisitedURLs)
	}
}

// TestToolset_WebRead_UnknownRecipe web_read 的未知 parse 配方拒绝(不碰底座)。
func TestToolset_WebRead_UnknownRecipe(t *testing.T) {
	ts := NewToolset(nil, 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebRead,
		map[string]any{"url": "https://x/y", "parse": "nope"})
	if !isErr || !strings.Contains(text, "未知解析配方") {
		t.Errorf("unknown recipe should be rejected: %q err=%v", text, isErr)
	}
	// wait_selector 超长拒绝。
	text, isErr = ts.Dispatch(context.Background(), ToolWebRead,
		map[string]any{"url": "https://x/y", "wait_selector": strings.Repeat("a", 201)})
	if !isErr || !strings.Contains(text, "wait_selector") {
		t.Errorf("overlong wait_selector should be rejected: %q err=%v", text, isErr)
	}
}

// TestToolset_WebRead_WaitSelector web_read 的 wait_selector 分支:打开后调用
// 底座 wait 动作(selector + timeout_ms),再读正文。
func TestToolset_WebRead_WaitSelector(t *testing.T) {
	var waited map[string]any
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			return 0, "ok", map[string]any{"page_id": "p_wait", "title": "SPA", "final_url": "https://spa.example/"}
		case "/ControlChromePage":
			action, _ := body["action"].(string)
			if action != "wait" {
				return 1001, "unexpected action " + action, nil
			}
			waited, _ = body["params"].(map[string]any)
			return 0, "ok", map[string]any{"result": map[string]any{"waited_for": ".quote"}}
		case "/BatchEval":
			return 0, "ok", map[string]any{
				"results": []any{
					map[string]any{"index": 0, "ok": true, "result": "SPA 行情"},
					map[string]any{"index": 1, "ok": true, "result": "上证指数 3658.03"},
				},
				"succeeded": 2,
			}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebRead, map[string]any{
		"url": "https://spa.example/", "wait_selector": ".quote", "wait_ms": 3000,
	})
	if isErr {
		t.Fatalf("web_read with wait_selector should pass: %q", text)
	}
	if waited == nil {
		t.Fatal("wait action was not called")
	}
	if waited["selector"] != ".quote" {
		t.Errorf("wait selector = %v, want .quote", waited["selector"])
	}
	if fmt.Sprint(waited["timeout_ms"]) != "3000" {
		t.Errorf("wait timeout_ms = %v, want 3000", waited["timeout_ms"])
	}
	if !strings.Contains(text, "上证指数 3658.03") {
		t.Errorf("web_read should return the body: %s", text)
	}
}

// TestToolset_WebParse 批次78 第 11 个工具:校验分支 + 正常解析。
func TestToolset_WebParse(t *testing.T) {
	ts := NewToolset(nil, 12, 20) // 本地工具:不需要底座。
	quote := `v_sh000001="1~上证指数~000001~3658.03~3713.03~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~20261022161500~-55.00~-1.48~3714.48~3649.86";`
	text, isErr := ts.Dispatch(context.Background(), ToolWebParse, map[string]any{"text": quote})
	if isErr {
		t.Fatalf("web_parse should pass: %q", text)
	}
	var res ParseResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("web_parse result must be JSON: %v (%s)", err, text)
	}
	if !res.Matched || res.Count != 1 || res.Rows[0].Symbol != "000001" {
		t.Errorf("parse result = %+v", res)
	}
	// 空 text 拒绝。
	if _, isErr := ts.Dispatch(context.Background(), ToolWebParse, map[string]any{"text": "  "}); !isErr {
		t.Error("empty text should be rejected")
	}
	// 超长 text 拒绝。
	long := strings.Repeat("字", webParseTextMaxRunes+1)
	if _, isErr := ts.Dispatch(context.Background(), ToolWebParse, map[string]any{"text": long}); !isErr {
		t.Error("overlong text should be rejected")
	}
	// 未知配方拒绝。
	_, isErr = ts.Dispatch(context.Background(), ToolWebParse, map[string]any{"text": "x", "recipe": "nope"})
	if !isErr {
		t.Error("unknown recipe should be rejected")
	}
}

// TestToolset_WebControl_WaitWhitelist 批次78:wait 进白名单(底座 actWait)。
func TestToolset_WebControl_WaitWhitelist(t *testing.T) {
	if !webControlActions["wait"] {
		t.Fatal("wait must be in the web_control whitelist")
	}
	var gotAction string
	var gotParams map[string]any
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		if path != "/ControlChromePage" {
			return 1000, "bad path", nil
		}
		gotAction, _ = body["action"].(string)
		gotParams, _ = body["params"].(map[string]any)
		return 0, "ok", map[string]any{"result": map[string]any{"waited_for": ".q"}}
	})
	defer srv.Close()
	ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebControl, map[string]any{
		"page_id": "p1", "action": "wait",
		"params": map[string]any{"selector": ".q", "timeout_ms": 5000},
	})
	if isErr {
		t.Fatalf("wait action should pass: %q", text)
	}
	if gotAction != "wait" {
		t.Errorf("action = %q, want wait", gotAction)
	}
	if gotParams["selector"] != ".q" {
		t.Errorf("params = %v", gotParams)
	}
}

// TestToolset_WebSearch_AntiBot 批次78:反爬识别(安全验证/403/异常流量)⇒
// 明确报错(is_err),不静默当 0 结果。
func TestToolset_WebSearch_AntiBot(t *testing.T) {
	for _, marker := range []string{"安全验证", "请完成下方验证", "403 Forbidden", "unusual traffic"} {
		srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
			switch path {
			case "/NewChromePage":
				return 0, "ok", map[string]any{"page_id": "p_block", "title": "必应", "final_url": "https://cn.bing.com/"}
			case "/BatchEval":
				return 0, "ok", map[string]any{
					"results": []any{
						map[string]any{"index": 0, "ok": true, "result": `[{"title":"__no_results__","url":"https://cn.bing.com/","snippet":""}]`},
						map[string]any{"index": 1, "ok": true, "result": marker},
					},
					"succeeded": 2,
				}
			case "/CloseChromePage":
				return 0, "ok", map[string]any{"closed": true}
			}
			return 1000, "bad path", nil
		})
		ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
		text, isErr := ts.Dispatch(context.Background(), ToolWebSearch, map[string]any{"query": "上证指数"})
		if !isErr {
			t.Errorf("marker %q: expected blocked error, got %q", marker, text)
		}
		if !strings.Contains(text, "被拦截") {
			t.Errorf("marker %q: error should say 被拦截, got %q", marker, text)
		}
		srv.Close()
	}
}

// TestToolset_WebSearch_BaiduEngine 批次78:engine=baidu 用百度 SERP URL。
func TestToolset_WebSearch_BaiduEngine(t *testing.T) {
	var serpURL string
	srv := newMockServer(t, func(path string, body map[string]any) (int, string, any) {
		switch path {
		case "/NewChromePage":
			serpURL, _ = body["url"].(string)
			return 0, "ok", map[string]any{"page_id": "p_bd", "title": "百度", "final_url": serpURL}
		case "/BatchEval":
			return 0, "ok", map[string]any{
				"results": []any{
					map[string]any{"index": 0, "ok": true, "result": `[{"title":"上证指数实时行情","url":"https://x/1","snippet":"3200 点震荡"}]`},
					map[string]any{"index": 1, "ok": true, "result": "正常页面"},
				},
				"succeeded": 2,
			}
		case "/CloseChromePage":
			return 0, "ok", map[string]any{"closed": true}
		}
		return 1000, "bad path", nil
	})
	defer srv.Close()
	ts := NewToolset(NewWebUseClient(srv.URL, "", 5000), 12, 20)
	text, isErr := ts.Dispatch(context.Background(), ToolWebSearch,
		map[string]any{"query": "上证指数 实时行情", "engine": "baidu"})
	if isErr {
		t.Fatalf("baidu search should pass: %q", text)
	}
	if !strings.HasPrefix(serpURL, "https://www.baidu.com/s?wd=") || !strings.HasSuffix(serpURL, "&ie=utf-8") {
		t.Errorf("baidu serp url = %q", serpURL)
	}
	if !strings.Contains(text, "上证指数实时行情") {
		t.Errorf("baidu results missing: %s", text)
	}
	// 非法 engine 拒绝。
	ts2 := NewToolset(nil, 12, 20)
	if _, isErr := ts2.Dispatch(context.Background(), ToolWebSearch,
		map[string]any{"query": "x", "engine": "google"}); !isErr {
		t.Error("unsupported engine should be rejected")
	}
}
