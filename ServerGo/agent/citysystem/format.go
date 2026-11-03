// Package citysystem — format.go: 引擎只读数据的 Agent 可读文本渲染
// (2026-11-03 §批次81)。
//
// 数据来源全部是 game/virtual_city 的**只读**房间 API:
//
//	room.SystemSummaryText(scope)   城市状态摘要(持锁只读)
//	room.WorldEventLogView()        世界事件日志快照(持锁只读)
//	room.PredictionReport(runID)    预测报告(持锁只读)
//
// 本文件只做**渲染**,不碰引擎状态、不加锁(锁由房间方法负责)。
package citysystem

import (
	"fmt"
	"strings"

	"LsmAgentGame/game/virtual_city"
)

// worldEventText 把事件日志渲染为 Agent 可读文本(空日志返回提示语)。
func worldEventText(records []virtual_city.WorldEventRecord) string {
	if len(records) == 0 {
		return "暂无真实世界事件(自动注入引擎未观测到事件,或数据通道离线)。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "真实世界事件 %d 条(新→旧):\n", len(records))
	for i, r := range records {
		if i >= 20 {
			fmt.Fprintf(&b, "…另有 %d 条\n", len(records)-20)
			break
		}
		state := "未注入"
		if r.Injected {
			state = fmt.Sprintf("已注入 %s L%d(M%d)", r.ScenarioKind, r.ScenarioMag, r.InjectedMonth)
		}
		fmt.Fprintf(&b, "%d. [%s] %s(%s) — %s\n", i+1, r.Rule, r.Title, r.Source, state)
		if r.Detail != "" {
			fmt.Fprintf(&b, "   %s\n", r.Detail)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// predictionReportText 把预测报告格式化为 Agent 可读文本(报告卡片数据另行下发)。
func predictionReportText(rep *virtual_city.PredictionReport) string {
	if rep == nil {
		return "暂无预测运行报告。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "预测报告 %s(Tier %s):%d 月观测,起点 M%d\n",
		rep.Meta.RunID, rep.Meta.Tier, rep.Meta.MonthsObserved, rep.Meta.StartMonth)
	if len(rep.Scenarios) > 0 {
		parts := make([]string, 0, len(rep.Scenarios))
		for _, s := range rep.Scenarios {
			parts = append(parts, fmt.Sprintf("%s %s L%d/%d月", s.Kind, s.Name, s.Magnitude, s.DurationMonths))
		}
		fmt.Fprintf(&b, "注入场景: %s\n", strings.Join(parts, "; "))
	}
	if len(rep.FirstMonthImpact) > 0 {
		parts := make([]string, 0, len(rep.FirstMonthImpact))
		for _, im := range rep.FirstMonthImpact {
			parts = append(parts, fmt.Sprintf("%s %+.2f%%", im.Metric, im.DeltaPct))
		}
		fmt.Fprintf(&b, "首月冲击: %s\n", strings.Join(parts, "; "))
	}
	if len(rep.Transmission.Chains) > 0 {
		parts := make([]string, 0, len(rep.Transmission.Chains))
		for _, c := range rep.Transmission.Chains {
			parts = append(parts, fmt.Sprintf("%s(%.0f%%)", c.Chain, c.Contribution*100))
		}
		fmt.Fprintf(&b, "传导链路: %s\n", strings.Join(parts, "; "))
	}
	if rep.Resilience.Applicable {
		fmt.Fprintf(&b, "韧性画像: 恢复 %d / 适应 %d / 转型 %d\n",
			rep.Resilience.RecoveryScore, rep.Resilience.AdaptationScore, rep.Resilience.TransformScore)
	}
	if len(rep.Boundaries) > 0 {
		fmt.Fprintf(&b, "边界与限制: %s\n", strings.Join(rep.Boundaries, "; "))
	}
	return strings.TrimRight(b.String(), "\n")
}
