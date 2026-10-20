// Package citychrome — mcp_tools_test.go: web_done 校验 + web_control 白名单
// 单测(契约 §7.1 第 1 条)。
package citychrome

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
			{"symbol": "", "name": "坏", "price": 1.0, "change_pct": 1},          // symbol 空
			{"symbol": "X", "name": "坏价", "price": "abc", "change_pct": 1},     // 非数值 → unmarshal 失败
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

func TestBuildTools_SevenTools(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 7 {
		t.Fatalf("BuildTools = %d, want 7", len(tools))
	}
	names := map[string]bool{}
	for _, td := range tools {
		names[td.Name] = true
		if td.Description == "" || td.InputSchema == nil {
			t.Errorf("tool %s missing description/schema", td.Name)
		}
	}
	for _, want := range []string{ToolWebOpen, ToolWebControl, ToolWebEval, ToolWebLook, ToolWebList, ToolWebClose, ToolWebDone} {
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
