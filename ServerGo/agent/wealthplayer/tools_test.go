package wealthplayer

import (
	"strings"
	"testing"
)

// TestBuildTools_All17ToolsPresent 17 工具齐备(§7)。
func TestBuildTools_All17ToolsPresent(t *testing.T) {
	tools := BuildTools()
	if len(tools) != 17 {
		t.Errorf("tools count: got %d, want 17", len(tools))
	}
	want := map[string]bool{
		ToolCheckState: false, ToolBuyAsset: false, ToolSellAsset: false,
		ToolBuyHouse: false, ToolTakeLoan: false, ToolRepayLoan: false,
		ToolStartSide: false, ToolStopSide: false, ToolStudy: false,
		ToolSocialize: false, ToolRest: false, ToolWorkOvertime: false,
		ToolMoveDistrict: false, ToolConsume: false, ToolDonate: false,
		ToolSpeak: false, ToolSubmitMonth: false,
	}
	for _, t1 := range tools {
		if _, ok := want[t1.Name]; !ok {
			t.Errorf("unknown tool: %s", t1.Name)
		}
		want[t1.Name] = true
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("missing tool: %s", name)
		}
	}
}

// TestBuildTools_RequiredFields 检查关键工具的必填字段(避免 §14.1 wire 协议偏差)。
func TestBuildTools_RequiredFields(t *testing.T) {
	tools := BuildTools()
	for _, tool := range tools {
		if tool.Name == "" {
			t.Errorf("tool missing name")
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s missing input_schema", tool.Name)
		}
	}
}

// TestToolNames_Returns17Names 工具名列表 = 17。
func TestToolNames_Returns17Names(t *testing.T) {
	names := ToolNames()
	if len(names) != 17 {
		t.Errorf("names count: got %d, want 17", len(names))
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "check_state") &&
			!strings.HasPrefix(n, "buy_") &&
			!strings.HasPrefix(n, "sell_") &&
			!strings.HasPrefix(n, "start_") &&
			!strings.HasPrefix(n, "stop_") &&
			!strings.HasPrefix(n, "study") &&
			!strings.HasPrefix(n, "socialize") &&
			!strings.HasPrefix(n, "rest") &&
			!strings.HasPrefix(n, "work_") &&
			!strings.HasPrefix(n, "move_") &&
			!strings.HasPrefix(n, "consume") &&
			!strings.HasPrefix(n, "donate") &&
			!strings.HasPrefix(n, "take_") &&
			!strings.HasPrefix(n, "repay_") &&
			!strings.HasPrefix(n, "speak") &&
			!strings.HasPrefix(n, "submit_") {
			t.Errorf("unexpectedname: %s", n)
		}
	}
}