// Package virtual_city — view_key_metrics.go: 批次86 关键指标看板后端
// (2026-11-06 §可靠性与可观测性)。
//
// 契约:lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §3.3.1。
//
// 独立成文件的原因:view.go 已 1656 行(§4 硬上限 1800),新增结构体 + 取数
// 函数另置。
//
// 纪律:纯函数快照(持锁由调用方管理);零 rand;nil 安全(World/子系统 nil
// ⇒ 零值,不 panic)。
package virtual_city

// KeyMetricsJSON 是 game.state.key_metrics 的载荷(批次86 §3.3)。
//
// ⚠ 全部字段**不带 omitempty**(空态零值也下发)—— 前端首屏不依赖网络
// 也能渲染骨架(与「空数组 vs null」纪律一致)。
type KeyMetricsJSON struct {
	// Population 人口 = 非空座位数(居民数)。
	Population int `json:"population"`
	// EconomicOutput 经济产出 = 居民总财富(元;Society.TotalWealth)。
	EconomicOutput int64 `json:"economic_output"`
	// Satisfaction 社会满意度 0..100(100-gini*100+SentimentBias;Election nil ⇒ 无偏置)。
	Satisfaction float64 `json:"satisfaction"`
	// EventQueueBacklog 事件队列积压 = 未注入世界事件数 + 活动场景数。
	EventQueueBacklog int `json:"event_queue_backlog"`
	// ActiveScenarios 活动场景数(EventQueueBacklog 的组成部分,单独列出便于诊断)。
	ActiveScenarios int `json:"active_scenarios"`
	// PendingWorldEvents 未注入世界事件数(EventQueueBacklog 的组成部分)。
	PendingWorldEvents int `json:"pending_world_events"`
}

// BuildKeyMetricsJSON 构造 game.state.key_metrics 快照(纯函数;零 rand)。
//
// 取数口径(契约 §3.3.1,与 prediction.go 的 Satisfaction 同公式):
//   - Population = World.Players 非 nil 计数;
//   - EconomicOutput = World.Society.TotalWealth(nil ⇒ 0);
//   - Satisfaction = clampF(100 - gini*100 + SentimentBias, 0, 100)
//     (Election nil ⇒ 偏置 0;gini 取 clamp01 后的 Society.Gini);
//   - EventQueueBacklog = WorldEventLog 中 Injected=false 的条数 + len(Scenarios)。
func BuildKeyMetricsJSON(w *World) KeyMetricsJSON {
	out := KeyMetricsJSON{}
	gini, bias := 0.0, 0.0
	if w != nil {
		for _, p := range w.Players {
			if p != nil {
				out.Population++
			}
		}
		if w.Society != nil {
			out.EconomicOutput = w.Society.TotalWealth
			gini = clamp01(w.Society.Gini)
		}
		if w.Election != nil {
			bias = w.Election.SentimentBias
		}
		// 未注入世界事件数(环为新→旧,顺序不影响计数)。
		for i := range w.WorldEventLog {
			if !w.WorldEventLog[i].Injected {
				out.PendingWorldEvents++
			}
		}
		out.ActiveScenarios = len(w.Scenarios)
	}
	// 满意度公式对 nil 世界同样成立(gini=0、bias=0 ⇒ 100),
	// 与「空态零值也下发」的口径一致,不因 nil 早退而失真。
	out.Satisfaction = clampF(100-gini*100+bias, 0, 100)
	out.EventQueueBacklog = out.PendingWorldEvents + out.ActiveScenarios
	return out
}

// buildEventLifecycleJSON 构造 game.state.event_lifecycle 的载荷
// (批次86 §4.2:留痕经 game.state 下发,零新增 WS 帧)。
//
// 环为新→旧,取前 limit 条(limit <= 0 ⇒ 全部);副本,调用方改不到环内。
func buildEventLifecycleJSON(w *World, limit int) []EventLifecycleTrace {
	out := []EventLifecycleTrace{}
	if w == nil {
		return out
	}
	n := len(w.EventLifecycleLog)
	if limit > 0 && limit < n {
		n = limit
	}
	for i := 0; i < n; i++ {
		out = append(out, cloneEventLifecycleTrace(&w.EventLifecycleLog[i]))
	}
	return out
}
