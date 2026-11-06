// Package virtual_city — room_event_lifecycle.go: 批次86 房间层条件注入入口
// (触发来源区分 + 留痕接线;2026-11-06 §可靠性与可观测性)。
//
// 契约:lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §3.1.4。
//
// 独立成文件的原因:room.go 已 1778 行(§4 硬上限 1800),本批把 ApplyScenario
// 的函数体搬到此处,room.go 只留薄壳转发(净减行数)。
//
// 纪律:
//   - **锁纪律(§92a)**:引擎校验与状态改写持 r.mu;注入事件经 hooks.OnEvent
//     **锁外**广播,随后 OnState 刷新(与改造前逐字一致);
//   - **零 rand**:留痕与快照不消耗 w.Rand;EventLifecycleLog 不参与 StateHash;
//   - **nil 安全**:World nil / 未开局由 applyScenarioToWorldTraced 统一返回
//     35002(与 LaunchSurvey 同口径)。
package virtual_city

import (
	"time"

	"LsmAgentGame/errcode"
)

// ApplyScenarioFromAgent 是 Agent 工具(inject_scenario)专用的条件注入入口
// (批次86 §3.1.4)。
//
// 与 ApplyScenario 签名完全一致,仅**触发来源**不同(留痕归因 agent_tool
// 而非 manual)—— 供 agent/citysystem 的 inject_scenario 工具调用
// (该调用点的接线不在本工作面内,由主 Agent 收口)。
func (r *VirtualCityRoom) ApplyScenarioFromAgent(kind string, magnitude, durationMonths int, target, note string) (*ScenarioAppliedRecord, []ScenarioEffect, *errcode.Error) {
	return r.applyScenarioFromSource(kind, magnitude, durationMonths, target, note,
		TriggerSourceAgentTool, "inject_scenario")
}

// applyScenarioFromSource 条件注入共用体(持 r.mu;批次86 §3.1.4)。
//
// source/detail 只进留痕(TriggerSource / TriggerDetail),不参与引擎语义
// ⇒ 对既有调用方零行为变化。
func (r *VirtualCityRoom) applyScenarioFromSource(kind string, magnitude, durationMonths int, target, note, source, detail string) (*ScenarioAppliedRecord, []ScenarioEffect, *errcode.Error) {
	r.mu.Lock()
	if r.World == nil || r.Status != StatusPlaying {
		r.mu.Unlock()
		return nil, nil, errcode.Code(errcode.ErrVirtualCityNotPlaying) // 35002
	}
	evIdx := len(r.World.Events)
	trace := newScenarioInjectionTrace(r.RoomID, source, detail)
	rec, effects, _, e := applyScenarioToWorldTraced(r.World, kind, magnitude, durationMonths,
		target, note, time.Now().UnixMilli(), r.seed, trace)
	var ev EventRecord
	hasEvent := len(r.World.Events) > evIdx
	if hasEvent {
		ev = r.World.Events[evIdx]
	}
	hooks := r.hooks
	roomID := r.RoomID
	r.mu.Unlock()
	if e != nil {
		return nil, nil, e
	}
	// ── 锁外回调(§92a)──
	if hasEvent && hooks.OnEvent != nil {
		hooks.OnEvent(roomID, ev)
	}
	if hooks.OnState != nil {
		hooks.OnState(roomID)
	}
	return rec, effects, nil
}

// newScenarioInjectionTrace 构造一条注入留痕(触发段;决策/执行/结果由引擎
// applyScenarioToWorldTraced 填充,最终由引擎追加进 World.EventLifecycleLog)。
func newScenarioInjectionTrace(roomID, source, detail string) *EventLifecycleTrace {
	return &EventLifecycleTrace{
		TriggerSource: source,
		TriggerDetail: detail,
		roomID:        roomID,
	}
}
