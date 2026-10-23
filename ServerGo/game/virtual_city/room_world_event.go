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
//  4. 通过者调 applyScenarioToWorld(持房间锁,复用批次 68 全链路);
//  5. 写 World.WorldEventLog 记录(事件 + 即时 effects;被拦下的记 Injected=false)。
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
		if w.worldEventSeen(ev.Key) {
			continue // 幂等闸门:同一事件不重复注入
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
		// 安全阀(任一不满足 ⇒ 跳过注入,留痕 Injected=false)。
		switch {
		case injectedThisMonth >= w.WorldEventMaxPerMonth:
			// 本月条数已满。
		case ev.ScenarioMag > w.WorldEventMaxMagnitude:
			// 强度超上限。
		case w.worldEventAutoKindInjected(ev.ScenarioKind):
			// 同 kind 本月已注入过(如 R1/R2/R3 同映射 market_crash)。
		case len(w.Scenarios) >= scenarioMaxActive:
			// 活动场景数满额。
		default:
			note := "真实世界事件·" + ev.Rule + " " + ev.Title
			_, effects, _, err := applyScenarioToWorld(w, ev.ScenarioKind, ev.ScenarioMag,
				ev.ScenarioDur, ev.ScenarioTarget, note, time.Now().UnixMilli(), r.seed)
			if err == nil {
				rec.Injected = true
				rec.InjectedMonth = w.worldEventEffectiveMonth()
				rec.Effects = effects
				injectedThisMonth++
				w.worldEventAutoMarkInjected(ev.ScenarioKind)
				w.worldEventMarkSeen(ev.Key)
			}
		}
		w.worldEventLogUpsert(rec)
	}
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
