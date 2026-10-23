// Package vcplayer — tools_realworld_test.go: 批次65 realworld_info 工具单测
// (契约 §3.7;沿批次35「单工具+op」范式)。
package vcplayer

import (
	"fmt"
	"strings"
	"testing"

	"LsmAgentGame/citychrome"
	"LsmAgentGame/config"
)

// ensureCityChromeOfflineSingleton 启动 enabled=false 的占位单例
// (离线但 FeedCompactJSON 可用);幂等。
func ensureCityChromeOfflineSingleton(t *testing.T) {
	t.Helper()
	if citychrome.Get() == nil {
		citychrome.Start(config.CityChromeConfig{Enabled: false}, nil)
	}
}

func TestDispatch_RealworldInfo_Overview(t *testing.T) {
	ensureCityChromeOfflineSingleton(t)
	a := &Agent{}
	res := a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "overview"})
	if res.IsErr {
		t.Fatalf("realworld_info overview: %s", res.Text)
	}
	if res.Budget {
		t.Error("realworld_info must not consume action budget")
	}
	if !strings.Contains(res.Text, `"channel"`) {
		t.Errorf("overview should carry channel: %s", res.Text)
	}
}

func TestDispatch_RealworldInfo_QuotesAndNews(t *testing.T) {
	ensureCityChromeOfflineSingleton(t)
	a := &Agent{}
	for _, op := range []string{"quotes", "news"} {
		res := a.DispatchTool(ToolRealworldInfo, map[string]any{"op": op})
		if res.IsErr {
			t.Errorf("op=%s: %s", op, res.Text)
		}
	}
	// 未知 op → 错误回灌。
	res := a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "hack"})
	if !res.IsErr {
		t.Error("unknown op should error")
	}
}

func TestDispatch_RealworldInfo_NoRunnerRequired(t *testing.T) {
	ensureCityChromeOfflineSingleton(t)
	// 只读外部数据工具不依赖引擎 ToolRunner:未绑 runner 也应可用(批次65)。
	a := &Agent{}
	res := a.DispatchTool(ToolRealworldInfo, map[string]any{})
	if res.IsErr {
		t.Errorf("realworld_info must not require engine runner: %s", res.Text)
	}
	// 对比:check_state 仍要求 runner 绑定(引擎工具语义不变)。
	res = a.DispatchTool(ToolCheckState, map[string]any{})
	if !res.IsErr || !strings.Contains(res.Text, "tool runner not bound") {
		t.Errorf("engine tools still require runner: %+v", res)
	}
}

// TestDispatch_RealworldInfo_Keyword 批次77 §3.5:realworld_info 新增可选
// keyword(op=news 时按标题过滤);不耗动作预算;schema 必须声明该参数。
func TestDispatch_RealworldInfo_Keyword(t *testing.T) {
	ensureCityChromeOfflineSingleton(t)
	a := &Agent{}
	res := a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "news", "keyword": "黄金"})
	if res.IsErr {
		t.Fatalf("realworld_info with keyword: %s", res.Text)
	}
	if res.Budget {
		t.Error("realworld_info must not consume action budget")
	}
	if !strings.Contains(res.Text, `"news"`) {
		t.Errorf("news result should carry the news array: %s", res.Text)
	}
	// keyword 缺省仍可用(不过滤)。
	res = a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "news"})
	if res.IsErr {
		t.Errorf("realworld_info news without keyword: %s", res.Text)
	}
	// 工具 schema 必须声明 keyword(否则 LLM 不会传)。
	props, ok := realworldInfoSchemaProps(t)
	if !ok {
		t.Fatal("realworld_info schema properties not found")
	}
	if _, found := props["keyword"]; !found {
		t.Error("realworld_info schema must declare the optional keyword property")
	}
}

// realworldInfoSchemaProps 取 realworld_info 的 schema properties。
func realworldInfoSchemaProps(t *testing.T) (map[string]any, bool) {
	t.Helper()
	for _, td := range BuildTools() {
		if td.Name != ToolRealworldInfo {
			continue
		}
		props, ok := td.InputSchema["properties"].(map[string]any)
		return props, ok
	}
	return nil, false
}

// TestDispatch_RealworldInfo_HistoryAndSources 批次78 §3.5:realworld_info
// 新增 op=history(symbol 必填)与 op=sources(采集源清单 + 健康度)。
func TestDispatch_RealworldInfo_HistoryAndSources(t *testing.T) {
	ensureCityChromeOfflineSingleton(t)
	a := &Agent{}

	// op=history:symbol 必填 —— 缺省报错(不 panic)。
	res := a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "history"})
	if !res.IsErr {
		t.Errorf("history without symbol should error: %+v", res)
	}
	// 带 symbol(离线通道无数据 ⇒ points 为空数组,仍不报错)。
	res = a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "history", "symbol": "000001"})
	if res.IsErr {
		t.Fatalf("history with symbol: %s", res.Text)
	}
	if res.Budget {
		t.Error("realworld_info must not consume action budget")
	}
	if !strings.Contains(res.Text, `"points"`) {
		t.Errorf("history result should carry points: %s", res.Text)
	}

	// op=sources:采集源清单。
	res = a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "sources"})
	if res.IsErr {
		t.Fatalf("sources: %s", res.Text)
	}
	for _, want := range []string{`"categories"`, `"sites"`, `"last_source"`, `"ok"`, `"fail"`} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("sources result missing %q: %s", want, res.Text)
		}
	}
}

// TestDispatch_RealworldInfo_SymbolFilter 批次78 §3.5:op=quotes 支持 symbol
// 精确过滤;schema 必须声明 symbol 参数。
func TestDispatch_RealworldInfo_SymbolFilter(t *testing.T) {
	ensureCityChromeOfflineSingleton(t)
	a := &Agent{}
	res := a.DispatchTool(ToolRealworldInfo, map[string]any{"op": "quotes", "symbol": "000001"})
	if res.IsErr {
		t.Fatalf("quotes with symbol: %s", res.Text)
	}
	if !strings.Contains(res.Text, `"quotes"`) {
		t.Errorf("quotes result should carry the quotes array: %s", res.Text)
	}
	props, ok := realworldInfoSchemaProps(t)
	if !ok {
		t.Fatal("realworld_info schema properties not found")
	}
	if _, found := props["symbol"]; !found {
		t.Error("realworld_info schema must declare the optional symbol property")
	}
	// op 枚举扩为 5 值。
	opSchema, _ := props["op"].(map[string]any)
	got := map[string]bool{}
	switch enum := opSchema["enum"].(type) {
	case []string:
		for _, e := range enum {
			got[e] = true
		}
	case []any:
		for _, e := range enum {
			got[fmt.Sprint(e)] = true
		}
	default:
		t.Fatalf("unexpected op enum shape: %v", opSchema["enum"])
	}
	for _, want := range []string{"overview", "quotes", "news", "history", "sources"} {
		if !got[want] {
			t.Errorf("realworld_info op enum missing %q (got %v)", want, opSchema["enum"])
		}
	}
}
