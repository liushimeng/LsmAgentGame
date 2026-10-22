// Package vcplayer — tools_realworld_test.go: 批次65 realworld_info 工具单测
// (契约 §3.7;沿批次35「单工具+op」范式)。
package vcplayer

import (
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
