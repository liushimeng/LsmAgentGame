// Package agent — agent_prop_inspect_test.go: 道具查询类工具 (v3 §G2) 的单元测试。
//
// 覆盖:
//   1. formatPropInspect / formatPropStatus / formatPropHistory 在 currentGC=nil 时返回合理错误
//   2. formatPropInspect 在 currentGC 设置后输出 JSON 含 props/budget/cooldown
//   3. formatPropStatus.can_use_prop 综合判断(冷却/预算/余额)
//   4. formatPropHistory limit 限制
//   5. WalletSustainabilityBlock 4 档紧急度 + 决策原则数字正确
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"encoding/json"
	"strings"
	"testing"
)

// TestFormatPropInspect_NilGC 验证 currentGC=nil 时返回错误 JSON。
func TestFormatPropInspect_NilGC(t *testing.T) {
	ClearCurrentGC()
	out := formatPropInspect("mine")
	if !strings.Contains(out, "no game context") {
		t.Errorf("nil gc 应返回 no game context, got %q", out)
	}
}

// TestFormatPropStatus_NilGC 验证 currentGC=nil 时 status 返回错误 JSON。
func TestFormatPropStatus_NilGC(t *testing.T) {
	ClearCurrentGC()
	out := formatPropStatus()
	if !strings.Contains(out, "no game context") {
		t.Errorf("nil gc 应返回 no game context, got %q", out)
	}
}

// TestFormatPropHistory_NilGC 验证 currentGC=nil 时 history 返回错误 JSON。
func TestFormatPropHistory_NilGC(t *testing.T) {
	ClearCurrentGC()
	out := formatPropHistory(10)
	if !strings.Contains(out, "no game context") {
		t.Errorf("nil gc 应返回 no game context, got %q", out)
	}
}

// TestFormatPropStatus_CanUse 综合判断:冷却/已用次数/全局预算/余额。
func TestFormatPropStatus_CanUse(t *testing.T) {
	gc := &wwtypes.GameContext{
		PropCooldownRemainingSec: 0,
		PropUsedThisGame:         0,
		PropMaxPerGame:           3,
		WalletBalance:            5000,
		RoomPropBudget:           900,
		RoomPropBudgetUsed:       0,
		PropSnapshot: []wwtypes.PropSnapshot{
			{PropKey: "char_confuse", NameZh: "胡言乱语", Price: 100, BaseHitRate: 20},
			{PropKey: "long_swear", NameZh: "长篇废话", Price: 250, BaseHitRate: 35},
		},
	}
	SetCurrentGC(gc)
	defer ClearCurrentGC()

	out := formatPropStatus()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("json parse failed: %v, out=%s", err, out)
	}
	if !parsed["can_use_prop"].(bool) {
		t.Errorf("健康状态应 can_use_prop=true, got %v", parsed["can_use_prop"])
	}
	// 冷却中 → 不能用
	gc.PropCooldownRemainingSec = 30
	out = formatPropStatus()
	json.Unmarshal([]byte(out), &parsed)
	if parsed["can_use_prop"].(bool) {
		t.Errorf("冷却中应 can_use_prop=false")
	}
	gc.PropCooldownRemainingSec = 0
	// 余额不足 → 不能用
	gc.WalletBalance = 50 // < cheapest 100
	out = formatPropStatus()
	json.Unmarshal([]byte(out), &parsed)
	if parsed["can_use_prop"].(bool) {
		t.Errorf("余额不足应 can_use_prop=false")
	}
	gc.WalletBalance = 5000
	// 全局预算耗尽 → 不能用
	gc.RoomPropBudgetUsed = 900
	out = formatPropStatus()
	json.Unmarshal([]byte(out), &parsed)
	if parsed["can_use_prop"].(bool) {
		t.Errorf("全局预算耗尽应 can_use_prop=false")
	}
}

// TestFormatPropHistory_Limit 验证 limit 生效。
func TestFormatPropHistory_Limit(t *testing.T) {
	hist := make([]wwtypes.PropHistoryRecord, 0, 25)
	for i := 0; i < 25; i++ {
		hist = append(hist, wwtypes.PropHistoryRecord{
			FromSeat: i, ToSeat: i + 1, PropKey: "test", Round: i, CreatedAt: int64(i),
		})
	}
	SetCurrentGC(&wwtypes.GameContext{PropHistorySnapshot: hist})
	defer ClearCurrentGC()

	// limit=0 → 默认 10
	out := formatPropHistory(0)
	var parsed map[string]any
	json.Unmarshal([]byte(out), &parsed)
	if int(parsed["count"].(float64)) != 10 {
		t.Errorf("默认 limit 应为 10, got %v", parsed["count"])
	}
	// limit=5
	out = formatPropHistory(5)
	json.Unmarshal([]byte(out), &parsed)
	if int(parsed["count"].(float64)) != 5 {
		t.Errorf("limit=5 应返回 5 条, got %v", parsed["count"])
	}
	// limit=100 → 上限 20
	out = formatPropHistory(100)
	json.Unmarshal([]byte(out), &parsed)
	if int(parsed["count"].(float64)) != 20 {
		t.Errorf("limit=100 应截断到 20, got %v", parsed["count"])
	}
}

// TestFormatPropInspect_Scope 验证 scope=mine/all 差异。
func TestFormatPropInspect_Scope(t *testing.T) {
	gc := &wwtypes.GameContext{
		PropCooldownRemainingSec: 0,
		PropUsedThisGame:         0,
		PropMaxPerGame:           3,
		RoomPropBudget:           900,
		RoomPropBudgetUsed:       0,
		PropSnapshot: []wwtypes.PropSnapshot{
			{PropKey: "long_swear", NameZh: "长篇废话", Price: 250, BaseHitRate: 35},
		},
		PropHistorySnapshot: []wwtypes.PropHistoryRecord{
			{FromSeat: 0, ToSeat: 1, PropKey: "long_swear", PropNameZh: "长篇废话", Hit: true},
		},
	}
	SetCurrentGC(gc)
	defer ClearCurrentGC()

	// mine: 应不含 history
	out := formatPropInspect("mine")
	var parsed map[string]any
	json.Unmarshal([]byte(out), &parsed)
	if _, hasHist := parsed["history"]; hasHist {
		t.Error("scope=mine 不应含 history 字段")
	}
	// all: 应含 history
	out = formatPropInspect("all")
	json.Unmarshal([]byte(out), &parsed)
	if _, hasHist := parsed["history"]; !hasHist {
		t.Error("scope=all 应含 history 字段")
	}
}

// TestWalletSustainabilityBlock_Urgency 验证 4 档紧急度档位正确。
func TestWalletSustainabilityBlock_Urgency(t *testing.T) {
	cases := []struct {
		balance int64
		ante    int64
		expect  string
	}{
		{5000, 100, "🟢 健康"},  // 50局
		{800, 100, "🟡 警戒"},   // 8局
		{300, 100, "🟠 危险"},   // 3局
		{100, 100, "🔴 濒死"},   // 1局
		{0, 100, ""},           // 余额=0 → 不渲染
	}
	for _, c := range cases {
		gc := &wwtypes.GameContext{WalletBalance: c.balance, AnteAmount: c.ante}
		out := WalletSustainabilityBlock(gc)
		if c.expect == "" {
			if out != "" {
				t.Errorf("余额 %d 应不渲染, got %q", c.balance, out)
			}
			continue
		}
		if !strings.Contains(out, c.expect) {
			t.Errorf("余额 %d 应含 %q, got %q", c.balance, c.expect, out)
		}
	}
	// 默认 AnteAmount=100
	gc := &wwtypes.GameContext{WalletBalance: 500, AnteAmount: 0}
	out := WalletSustainabilityBlock(gc)
	if !strings.Contains(out, "100") {
		t.Errorf("默认 AnteAmount=100 应出现 100, got %q", out)
	}
}

// TestWalletSustainabilityBlock_NilGC 验证 nil GC 不渲染。
func TestWalletSustainabilityBlock_NilGC(t *testing.T) {
	if out := WalletSustainabilityBlock(nil); out != "" {
		t.Errorf("nil gc 应返回空, got %q", out)
	}
}