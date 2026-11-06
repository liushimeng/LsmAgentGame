// Package virtual_city — room_world_event.go: 批次80 真实世界事件自动注入的
// 房间层接线(派生 → 幂等闸门 → 安全阀 → 场景引擎注入 → 留痕)。
//
// 契约出处:lag_docs/虚拟城市/已实现/80-真实世界事件自动注入引擎/01-方案设计.md
// §3.3(注入与安全阀)。
//
// 纪律:
//   - **锁纪律(§92a)**:本文件全部方法由房间层持 r.mu 调用;
//     refreshWorldEventsLocked 在 trySettle 锁内、月结前触发(与
//     refreshExoLocked 同位置);
//   - **worldEventSource 契约**:纯内存快照、**禁止回调房间**(只读 FeedStore
//     类数据;装配层在 main.go 完成 Feed→WorldEventInput 映射);
//   - **零 rand**:事件派生(DeriveWorldEvents)与场景注入(applyScenarioToWorld)
//     均不消费 w.Rand ⇒ 存量对局回归零偏移;
//   - **零回归**:无候选事件 ⇒ 绝不调用 applyScenarioToWorld ⇒ 固定 seed 双跑
//     逐位相等。
package virtual_city

import (
	"fmt"
	"time"
)

// worldEventRoomState 房间级世界事件接线状态(批次 80)。
//
// 独立成结构体是为了把字段声明放在本文件(room.go 已 1749 行,禁再加逻辑);
// 房间结构体只内嵌一行。
type worldEventRoomState struct {
	// worldEventSource 真实世界事件源(Manager 注入,可能为 nil)。
	// 契约:纯内存快照、禁止回调房间。
	worldEventSource func() []WorldEventInput
	// autoEnabled 自动注入总开关(false ⇒ 不派生、不注入)。
	autoEnabled bool
	// maxPerMonth 每月自动注入条数上限。
	maxPerMonth int
	// maxMagnitude 自动注入强度上限(L1..L5)。
	maxMagnitude int
}

// worldEventLimitsDefault 安全阀默认值(与 config city_chrome{} 默认一致:
// 开启 / 每月 2 条 / 强度上限 3)。
const (
	worldEventLimitsDefaultMaxPerMonth  = 2
	worldEventLimitsDefaultMaxMagnitude = 3
)

// newWorldEventRoomState 构造默认接线状态(自动注入开启;每月 2 条;强度上限 3)。
func newWorldEventRoomState() worldEventRoomState {
	return worldEventRoomState{
		autoEnabled:  true,
		maxPerMonth:  worldEventLimitsDefaultMaxPerMonth,
		maxMagnitude: worldEventLimitsDefaultMaxMagnitude,
	}
}

// SetWorldEventSource 注入真实世界事件源(Manager 装配,建房时调用一次)。
func (r *VirtualCityRoom) SetWorldEventSource(fn func() []WorldEventInput) {
	r.mu.Lock()
	r.worldEvent.worldEventSource = fn
	r.mu.Unlock()
}

// SetAutoScenarioLimits 回写自动注入安全阀参数(Manager.CreateRoom 调用,
// 须在 Start 之前;Start 时落进 World)。零值 ⇒ 取默认(见 newWorldEventRoomState)。
func (r *VirtualCityRoom) SetAutoScenarioLimits(enabled bool, maxPerMonth, maxMagnitude int) {
	r.mu.Lock()
	r.worldEvent.autoEnabled = enabled
	r.worldEvent.maxPerMonth = maxPerMonth
	r.worldEvent.maxMagnitude = maxMagnitude
	if r.World != nil {
		applyAutoScenarioLimits(r.World, enabled, maxPerMonth, maxMagnitude)
	}
	r.mu.Unlock()
}

// applyAutoScenarioLimits 把安全阀参数写入 World(Start 与 SetAutoScenarioLimits
// 共用;零 rand)。
func applyAutoScenarioLimits(w *World, enabled bool, maxPerMonth, maxMagnitude int) {
	if w == nil {
		return
	}
	if maxPerMonth <= 0 {
		maxPerMonth = worldEventLimitsDefaultMaxPerMonth
	}
	if maxMagnitude <= 0 || maxMagnitude > scenarioMaxMagnitude {
		maxMagnitude = worldEventLimitsDefaultMaxMagnitude
	}
	w.WorldEventAutoEnabled = enabled
	w.WorldEventMaxPerMonth = maxPerMonth
	w.WorldEventMaxMagnitude = maxMagnitude
}

// refreshWorldEventsLocked 月结前派生并注入真实世界事件(批次 80)。
// 调用方持房间锁(trySettle 锁内);与 refreshExoLocked 同位置。
//
// 流程(契约 §3.3):
//  1. 读事件源(纯内存快照;nil ⇒ 直接返回);
//  2. DeriveWorldEvents 派生候选事件(纯函数;多份输入合并为一份统计集合);
//  3. 逐条过幂等闸门(World.WorldEventSeen)与安全阀(每月条数上限 /
//     强度上限 / 同 kind 本月去重 / 活动场景数上限);
//  4. 通过者调 applyScenarioToWorldTraced(持房间锁,复用批次 68 全链路;
//     批次86 起带 4 阶段留痕 + 注入前快照);
//  5. 写 World.WorldEventLog 记录(事件 + 即时 effects;被拦下的记 Injected=false)。
//
// 批次86 §3.1.4:每个闸门(含幂等)的判定结果写入 World.EventLifecycleLog
// (新→旧环 ≤50)—— 此前只记「是否注入」,不记决策依据。
//
// 零 rand;无候选 ⇒ 绝不触碰 World 状态(零回归契约)。
func (r *VirtualCityRoom) refreshWorldEventsLocked() {
	w := r.World
	if w == nil || !w.WorldEventAutoEnabled {
		return
	}
	src := r.worldEvent.worldEventSource
	if src == nil {
		return
	}
	inputs := src()
	if len(inputs) == 0 {
		return
	}
	// 装配层可能给出多份快照(不同通道/时点)⇒ 合并为一份统计集合后统一派生
	// (R1 的「a_share∪hk∪us 全体报价」必须跨来源整体计算)。
	merged := WorldEventInput{}
	for _, in := range inputs {
		merged.Quotes = append(merged.Quotes, in.Quotes...)
		merged.News = append(merged.News, in.News...)
	}
	now := time.Now().Unix()
	candidates := DeriveWorldEvents(merged, now)
	if len(candidates) == 0 {
		return // 无候选 ⇒ 不注入、不留痕(零回归)
	}
	// 跨月重置安全阀计数器(每月 ≤N 条 / 同 kind 每月至多 1 次 ⇒ 按月独立)。
	w.worldEventAutoCounterReset()

	injectedThisMonth := w.worldEventAutoCount()
	for _, ev := range candidates {
		if ev.Key == "" {
			continue
		}
		// 批次86 §3.1.4:触发来源区分 —— 有规则 id ⇒ realworld_rule,否则 auto_inject。
		source := TriggerSourceAutoInject
		if ev.Rule != "" {
			source = TriggerSourceRealWorldRule
		}
		if w.worldEventSeen(ev.Key) {
			// 幂等闸门:同一事件不重复注入(批次86:拦截依据进留痕)。
			w.appendEventLifecycleTraceLocked(*newWorldEventRejectedTrace(r.RoomID, ev, source,
				EventGateIdempotency, "事件指纹已处理:"+ev.Key))
			continue
		}
		rec := WorldEventRecord{
			Key:          ev.Key,
			Rule:         ev.Rule,
			Kind:         ev.Kind,
			Severity:     ev.Severity,
			Direction:    ev.Direction,
			Scope:        ev.Scope,
			Title:        ev.Title,
			Detail:       ev.Detail,
			Magnitude:    ev.Magnitude,
			ScenarioKind: ev.ScenarioKind,
			ScenarioMag:  ev.ScenarioMag,
			Source:       ev.Source,
			URL:          ev.URL,
			ObservedAt:   ev.ObservedAt,
			Effects:      make([]ScenarioEffect, 0),
		}
		// 安全阀(任一不满足 ⇒ 跳过注入,留痕 Injected=false;批次86:逐闸门留痕)。
		gates := make([]GateDecision, 0, 5)
		rejected := ""
		switch {
		case injectedThisMonth >= w.WorldEventMaxPerMonth:
			rejected = EventGateMonthlyCount
			gates = append(gates, GateDecision{Gate: EventGateMonthlyCount, Passed: false,
				Detail: fmt.Sprintf("本月已注入 %d 条,达上限 %d", injectedThisMonth, w.WorldEventMaxPerMonth)})
		case ev.ScenarioMag > w.WorldEventMaxMagnitude:
			rejected = EventGateMagnitudeLimit
			gates = append(gates, GateDecision{Gate: EventGateMagnitudeLimit, Passed: false,
				Detail: fmt.Sprintf("强度 L%d 超上限 L%d", ev.ScenarioMag, w.WorldEventMaxMagnitude)})
		case w.worldEventAutoKindInjected(ev.ScenarioKind):
			rejected = EventGateKindDedup
			gates = append(gates, GateDecision{Gate: EventGateKindDedup, Passed: false,
				Detail: "同 kind 本月已注入:" + ev.ScenarioKind})
		case len(w.Scenarios) >= scenarioMaxActive:
			rejected = EventGateActiveLimit
			gates = append(gates, GateDecision{Gate: EventGateActiveLimit, Passed: false,
				Detail: fmt.Sprintf("活动场景 %d 个已达上限 %d", len(w.Scenarios), scenarioMaxActive)})
		}
		if rejected != "" {
			tr := newWorldEventTrace(r.RoomID, ev, source)
			tr.DecisionGates = gates
			tr.DecisionOutcome = EventDecisionReject
			tr.ResultOutcome = EventResultFailed
			tr.ResultErrMsg = "安全阀拦截:" + rejected
			w.appendEventLifecycleTraceLocked(*tr)
			w.worldEventLogUpsert(rec)
			continue
		}
		note := "真实世界事件·" + ev.Rule + " " + ev.Title
		// 闸门全通过 ⇒ 房间层留痕预置通过项,引擎 applyScenarioToWorldTraced
		// 追加自己的决策段(unknown_kind / magnitude_limit / params /
		// active_limit)并补执行/结果,最终由引擎追加进留痕环。
		tr := newWorldEventTrace(r.RoomID, ev, source)
		tr.DecisionGates = append(gates,
			GateDecision{Gate: EventGateMonthlyCount, Passed: true},
			GateDecision{Gate: EventGateMagnitudeLimit, Passed: true},
			GateDecision{Gate: EventGateKindDedup, Passed: true},
			GateDecision{Gate: EventGateActiveLimit, Passed: true},
		)
		_, effects, _, err := applyScenarioToWorldTraced(w, ev.ScenarioKind, ev.ScenarioMag,
			ev.ScenarioDur, ev.ScenarioTarget, note, time.Now().UnixMilli(), r.seed, tr)
		if err == nil {
			rec.Injected = true
			rec.InjectedMonth = w.worldEventEffectiveMonth()
			rec.Effects = effects
			injectedThisMonth++
			w.worldEventAutoMarkInjected(ev.ScenarioKind)
			w.worldEventMarkSeen(ev.Key)
		}
		w.worldEventLogUpsert(rec)
	}
}

// newWorldEventTrace 构造一条世界事件注入留痕(触发段 + 通过闸门;
// 决策剩余段 / 执行 / 结果由引擎 applyScenarioToWorldTraced 填充)。
func newWorldEventTrace(roomID string, ev WorldEvent, source string) *EventLifecycleTrace {
	return &EventLifecycleTrace{
		EventKey:      ev.Key,
		ScenarioKind:  ev.ScenarioKind,
		TriggeredAt:   time.Now().UnixMilli(),
		TriggerSource: source,
		TriggerDetail: fmt.Sprintf("%s %s", ev.Rule, ev.Title),
		roomID:        roomID,
	}
}

// newWorldEventRejectedTrace 构造一条被房间层闸门拦截的世界事件留痕
// (未执行 ⇒ 无 StateHash / 无 effects;批次86 §3.1.4)。
func newWorldEventRejectedTrace(roomID string, ev WorldEvent, source, gate, detail string) *EventLifecycleTrace {
	tr := newWorldEventTrace(roomID, ev, source)
	tr.DecisionGates = []GateDecision{{Gate: gate, Passed: false, Detail: detail}}
	tr.DecisionOutcome = EventDecisionReject
	tr.ResultOutcome = EventResultFailed
	tr.ResultErrMsg = "闸门拦截:" + gate
	return tr
}

// worldEventLogUpsert 写日志:同 key 已存在 ⇒ 原地更新(避免重复行);
// 否则按「新→旧」插入并裁剪到 worldEventLogCap。零 rand。
func (w *World) worldEventLogUpsert(rec WorldEventRecord) {
	if w == nil {
		return
	}
	if rec.Effects == nil {
		rec.Effects = make([]ScenarioEffect, 0)
	}
	for i := range w.WorldEventLog {
		if w.WorldEventLog[i].Key != rec.Key {
			continue
		}
		// 同一事件再次评估(如先前被安全阀拦下、本月额度释放后重试):
		// 原地更新注入状态与效果,保留原始观测时间与排序位。
		old := w.WorldEventLog[i]
		old.Injected = rec.Injected
		old.InjectedMonth = rec.InjectedMonth
		old.Effects = rec.Effects
		w.WorldEventLog[i] = old
		return
	}
	w.worldEventLogAdd(rec)
}
