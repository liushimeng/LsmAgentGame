// Package virtual_city — room_dimension_status.go: 批次85 全维度状态视图
// (City-System 观察闭环的只读读出通道;2026-11-05 §批次85)。
//
// 契约: lag_docs/虚拟城市/已实现/85-全维度状态视图与物体级显示控制/
//       01-方案设计.md §3.1(维度状态视图)。
//
// 消费方: ServerGo/agent/citysystem 的 query_city_state(13 scope 观察闭环
// 两段)与 query_world_events(dimension 过滤)。
//
// 纪律(与 room_system_agent.go 一致):
//   - **只读**:全部方法持 r.mu 读取既有状态,**禁止回调房间**(§92a),
//     不得调用 GetStatus()/Engine() 等会再取锁的方法;
//   - **nil 安全**:World / 各子系统 / 房间未开局均返回可读空视图,不 panic;
//   - **返回副本**:视图字段全部按值重建,调用方修改不污染内部状态;
//   - **零 RNG / 零回归**:不新增 World 持久化状态,不触碰 w.Rand(§197),
//     既有 101+ 条场景语义不动 —— 一切从既有状态 + 活动场景 + 世界事件派生。
package virtual_city

import (
	"sort"
)

// DimensionStatusView 维度状态视图(批次85 §3.1.1)。
// 「正在发生什么」= 本维度进行中场景 + 本维度相关世界事件,全部派生自既有状态。
type DimensionStatusView struct {
	// Group 维度名(13 个 ScenarioGroup* 分组名之一;未知维度为空串)。
	Group string
	// ActiveScenarios 本维度进行中的场景(新→旧)。
	ActiveScenarios []ScenarioBrief
	// RecentEvents 本维度相关世界事件(新→旧,≤5)。
	RecentEvents []WorldEventBrief
}

// ScenarioBrief 场景摘要(只含读出需要的字段)。
type ScenarioBrief struct {
	ID, Kind, Name  string
	Magnitude       int
	RemainingMonths int
	Target          string
	AppliedMonth    int
}

// WorldEventBrief 世界事件摘要。
type WorldEventBrief struct {
	Title, Kind, Source string
	ObservedAt          int64
	Injected            bool
}

// dimensionRecentEventsCap 是 DimensionStatusView.RecentEvents 的条数上限
// (契约 §3.1.1:新→旧,≤5)。
const dimensionRecentEventsCap = 5

// SystemDimensionStatus 返回一个维度的「正在发生什么」视图(只读、持锁、
// 不回调房间;批次85 §3.1.1)。
//
// dim 取 13 个分组名(ScenarioGroup* 常量);未知维度(含空串)返回空视图。
// World 为 nil(未开局)返回该维度名下的空视图(可读、不 panic)。
// 返回值为副本,调用方修改不污染内部状态。
func (r *VirtualCityRoom) SystemDimensionStatus(dim string) DimensionStatusView {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 未知维度(含空串)⇒ 空视图(宁缺勿滥,绝不编造归属)。
	if dim == "" || !scenarioGroupKnown(dim) {
		return DimensionStatusView{}
	}
	out := DimensionStatusView{
		Group:           dim,
		ActiveScenarios: []ScenarioBrief{},
		RecentEvents:    []WorldEventBrief{},
	}
	w := r.World
	if w == nil {
		return out
	}
	// 本维度进行中场景(判定:scenarioDefFor(kind).spec.Group == dim,
	// 既有 kind→group 映射,L1/L2/L3 全覆盖)。
	for _, sc := range w.Scenarios {
		if sc == nil || ScenarioDimensionOf(sc.Kind) != dim {
			continue
		}
		out.ActiveScenarios = append(out.ActiveScenarios, scenarioBriefOf(sc))
	}
	// 新→旧:w.Scenarios 按注入序(旧→新)追加,反转后按注入月稳定降序。
	reverseScenarioBriefs(out.ActiveScenarios)
	sort.SliceStable(out.ActiveScenarios, func(i, j int) bool {
		return out.ActiveScenarios[i].AppliedMonth > out.ActiveScenarios[j].AppliedMonth
	})
	// 本维度相关世界事件(WorldEventLog 本身新→旧;归不到具体维度的事件
	// 不计入 —— 宁缺勿滥)。
	for _, rec := range w.WorldEventLog {
		if WorldEventDimensionOf(rec) != dim {
			continue
		}
		out.RecentEvents = append(out.RecentEvents, WorldEventBrief{
			Title:      rec.Title,
			Kind:       rec.Kind,
			Source:     rec.Source,
			ObservedAt: rec.ObservedAt,
			Injected:   rec.Injected,
		})
		if len(out.RecentEvents) >= dimensionRecentEventsCap {
			break
		}
	}
	return out
}

// ScenarioDimensionOf 返回场景 kind 所属的维度分组名(目录分组;未知 kind
// 返回空串 —— 归不到具体维度,宁缺勿滥)。
func ScenarioDimensionOf(kind string) string {
	if def, ok := scenarioDefFor(kind); ok {
		return def.spec.Group
	}
	return ""
}

// WorldEventDimensionOf 返回世界事件记录归集到的维度分组名(批次85 §3.1.1)。
//
// 归集判据:先按 ScenarioKind(事件注入时映射的场景 kind)归集,再按
// Kind(事件类型)兜底;两者都归不到具体维度时返回空串 —— 该事件不计入
// 任何维度视图(绝不编造归属)。
func WorldEventDimensionOf(rec WorldEventRecord) string {
	if g := ScenarioDimensionOf(rec.ScenarioKind); g != "" {
		return g
	}
	return ScenarioDimensionOf(rec.Kind)
}

// CashDamageScenarioBriefs 返回「居民现金损失」类活动场景摘要
// (scenarioProfile.CashDamageFrac > 0,跨全部维度;新→旧)。
//
// 只读、不加锁、不回调房间 —— 调用方须已持 r.mu 或独占 w(§92a);
// 数据源是既有场景幅度表(profile),零新增状态、零编造。
func CashDamageScenarioBriefs(w *World) []ScenarioBrief {
	out := []ScenarioBrief{}
	if w == nil {
		return out
	}
	for _, sc := range w.Scenarios {
		if sc == nil {
			continue
		}
		def, ok := scenarioDefFor(sc.Kind)
		if !ok || def.profile.CashDamageFrac <= 0 {
			continue
		}
		out = append(out, scenarioBriefOf(sc))
	}
	reverseScenarioBriefs(out)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].AppliedMonth > out[j].AppliedMonth
	})
	return out
}

// scenarioBriefOf 由活动场景构建摘要(名称取目录,未知 kind 回退 kind 本身
// —— 与 room_system_agent.go::SystemSummaryText 同口径,不编造)。
func scenarioBriefOf(sc *ScenarioActive) ScenarioBrief {
	name := sc.Kind
	if def, ok := scenarioDefFor(sc.Kind); ok {
		name = def.spec.Name
	}
	return ScenarioBrief{
		ID:              sc.ID,
		Kind:            sc.Kind,
		Name:            name,
		Magnitude:       sc.Magnitude,
		RemainingMonths: sc.RemainingMonths,
		Target:          sc.Target,
		AppliedMonth:    sc.AppliedMonth,
	}
}

// reverseScenarioBriefs 原地反转(注入序旧→新 ⇒ 新→旧)。
func reverseScenarioBriefs(in []ScenarioBrief) {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
}
