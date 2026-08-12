// Package agent — agent_prop_inspect.go: 道具查询类工具 (v3 §G2) 的实现。
//
// 本文件实现 prop_inspect / prop_status / prop_history 三个查询工具:
//   - prop_inspect: 道具盘点,返回可用清单 + 个人/全局预算 + 最近效果
//   - prop_status:  快速状态查询,返回冷却/余额/can_use_prop 综合判断
//   - prop_history: 公开道具使用历史(最近 N 条)
//
// 这些工具**纯查询无副作用**,不需要 ToolRunner 扩展接口。
// 数据源:DispatchTool 调用时通过 closure 捕获当前轮的 wwtypes.GameContext。
package wwplayer

import (
	"LsmWebGame/agent/wwtypes"
	"encoding/json"
	"fmt"
	"strings"
)

// currentGC 是 prop_inspect / prop_status / prop_history 三个查询工具的数据源。
// 由 DispatchTool 调用前通过 SetCurrentGC 设置,工具内 closure 读这个变量。
// 注意:不是 goroutine 安全 —— 只在单线程 dispatch 路径下使用,与 BuildUserPrompt 同步。
var currentGC *wwtypes.GameContext

// SetCurrentGC 设置当前轮的 wwtypes.GameContext（由 DispatchTool 调用前同步设置）。
// 仅供 agent 包内部使用;外部请勿直接调用。
func SetCurrentGC(gc *wwtypes.GameContext) {
	currentGC = gc
}

// ClearCurrentGC 清理当前轮的 wwtypes.GameContext（防止内存泄漏 / 跨轮污染）。
func ClearCurrentGC() {
	currentGC = nil
}

// formatPropInspect 实现 prop_inspect 工具的格式化输出（JSON 字符串）。
// scope='mine' 仅自己;'all' 含公开使用历史。
func formatPropInspect(scope string) string {
	if currentGC == nil {
		return `{"error":"no game context"}`
	}
	gc := currentGC
	out := map[string]any{
		"cooldown_remaining_sec": gc.PropCooldownRemainingSec,
		"used_this_game":         gc.PropUsedThisGame,
		"max_per_game":           gc.PropMaxPerGame,
		"budget_remaining":       gc.RoomPropBudget - gc.RoomPropBudgetUsed,
		"budget_total":           gc.RoomPropBudget,
		"last_effect":            gc.PropLastEffect,
	}
	// 可购买道具
	props := make([]map[string]any, 0, len(gc.PropSnapshot))
	for _, s := range gc.PropSnapshot {
		props = append(props, map[string]any{
			"prop_key":      s.PropKey,
			"name_zh":       s.NameZh,
			"price":         s.Price,
			"base_hit_rate": s.BaseHitRate,
			"is_aoe":        s.IsAOE,
		})
	}
	out["props"] = props
	if scope == "all" && len(gc.PropHistorySnapshot) > 0 {
		hist := make([]map[string]any, 0, len(gc.PropHistorySnapshot))
		for _, h := range gc.PropHistorySnapshot {
			hist = append(hist, map[string]any{
				"from_seat":   h.FromSeat + 1,
				"to_seat":     h.ToSeat + 1,
				"prop_key":    h.PropKey,
				"prop_name":   h.PropNameZh,
				"hit":         h.Hit,
				"effect_hint": h.EffectHint,
				"phase":       h.Phase,
				"round":       h.Round,
			})
		}
		out["history"] = hist
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// formatPropStatus 实现 prop_status 工具的格式化输出（JSON 字符串）。
// 返回综合判断 can_use_prop（冷却/已用次数/全局预算/余额 全满足才 true）。
func formatPropStatus() string {
	if currentGC == nil {
		return `{"error":"no game context"}`
	}
	gc := currentGC
	budgetRemaining := gc.RoomPropBudget - gc.RoomPropBudgetUsed
	cheapestPrice := int64(0)
	for _, s := range gc.PropSnapshot {
		if cheapestPrice == 0 || s.Price < cheapestPrice {
			cheapestPrice = s.Price
		}
	}
	canUse := gc.PropCooldownRemainingSec == 0 &&
		gc.PropUsedThisGame < gc.PropMaxPerGame &&
		(gc.RoomPropBudget <= 0 || budgetRemaining >= cheapestPrice) &&
		gc.WalletBalance >= cheapestPrice

	out := map[string]any{
		"cooldown_remaining_sec":    gc.PropCooldownRemainingSec,
		"used_this_game":            gc.PropUsedThisGame,
		"max_per_game":              gc.PropMaxPerGame,
		"wallet_balance":            gc.WalletBalance,
		"room_prop_budget_remaining": budgetRemaining,
		"cheapest_prop_price":       cheapestPrice,
		"can_use_prop":              canUse,
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// formatPropHistory 实现 prop_history 工具的格式化输出（JSON 字符串）。
// limit ≤ 20,默认 10。
func formatPropHistory(limit int) string {
	if currentGC == nil {
		return `{"error":"no game context"}`
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	hist := currentGC.PropHistorySnapshot
	// 取最近 N 条（按时间正序，新→旧）
	if len(hist) > limit {
		hist = hist[len(hist)-limit:]
	}
	out := make([]map[string]any, 0, len(hist))
	for _, h := range hist {
		out = append(out, map[string]any{
			"from_seat":   h.FromSeat + 1,
			"to_seat":     h.ToSeat + 1,
			"prop_key":    h.PropKey,
			"prop_name":   h.PropNameZh,
			"hit":         h.Hit,
			"effect_hint": h.EffectHint,
			"phase":       h.Phase,
			"round":       h.Round,
		})
	}
	b, _ := json.Marshal(map[string]any{
		"count":   len(out),
		"history": out,
	})
	return string(b)
}

// ensure strings is referenced (avoid unused import errors if all imports trimmed).
var _ = strings.HasPrefix

// ensure fmt is referenced.
var _ = fmt.Sprintf