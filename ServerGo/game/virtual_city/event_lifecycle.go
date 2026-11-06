// Package virtual_city — event_lifecycle.go: 批次86 事件生命周期留痕
// (2026-11-06 §可靠性与可观测性)。
//
// 契约:lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §3.1。
//
// 定位:把「触发 → 决策 → 执行 → 结果」四阶段结构化留痕,补齐批次80 之前
// 只有「执行/结果碎片」的缺口 —— WorldEventLog 只记「事件 → 是否注入」,
// 不记决策依据;ScenarioApplied 只记「注入参数 + 效果」,不记触发来源与
// 前后状态哈希。
//
// 纪律:
//   - **零 rand**:绝不触碰 w.Rand;EventLifecycleLog **不参与 StateHash**
//     (仅审计留痕与看板展示)⇒ 存量对局回归零偏移;
//   - **环形缓冲 cap 50,新→旧**(与 WorldEventLog 同构;head insert);
//   - **值语义**:环内恒存值;对外查询一律返回副本(调用方改不到内部);
//   - **nil 安全**:World nil / 未开局均返回非 nil 空切片(JSON 恒 [])。
package virtual_city

import (
	"fmt"
	"sync/atomic"
	"time"

	"LsmAgentGame/errcode"
)

// EventPhase 事件生命周期阶段(需求书 §五「触发、决策、执行、结果」)。
// 留痕结构按四阶段分节;常量供前端时间线渲染与单测断言使用。
const (
	EventPhaseTrigger   = "trigger"
	EventPhaseDecision  = "decision"
	EventPhaseExecution = "execution"
	EventPhaseResult    = "result"
)

// TriggerSource 触发来源(留痕归属)。
const (
	TriggerSourceAgentTool     = "agent_tool"      // City-System inject_scenario
	TriggerSourceRealWorldRule = "realworld_rule" // 批次80 规则派生
	TriggerSourceAutoInject    = "auto_inject"     // 月结自动注入
	TriggerSourceManual        = "manual"          // HTTP 直发
	TriggerSourceReplay        = "replay"          // 回放重放
	TriggerSourceSystem        = "system"          // 系统内部
)

// 决策/结果 outcome 取值(契约 §3.1.1 注释)。
const (
	EventDecisionPass   = "pass"
	EventDecisionReject = "reject"

	EventResultSuccess    = "success"
	EventResultFailed     = "failed"
	EventResultRolledBack = "rolled_back"
)

// 决策闸门名(契约 §3.1.1 GateDecision.Gate 枚举)。
const (
	EventGateIdempotency    = "idempotency"     // 幂等指纹(房间层)
	EventGateMonthlyCount   = "monthly_count"   // 每月条数上限(房间层)
	EventGateMagnitudeLimit = "magnitude_limit" // 强度上限
	EventGateKindDedup      = "kind_dedup"      // 同 kind 本月去重(房间层)
	EventGateActiveLimit    = "active_limit"    // 活动场景数上限
	EventGateUnknownKind    = "unknown_kind"    // 场景 kind 命中目录
	EventGateParams         = "params"          // 持续月数 / target 合法性
)

// eventLifecycleLogCap 事件生命周期留痕环容量(契约 §3.1.2:≤50,超出丢最旧)。
const eventLifecycleLogCap = 50

// eventTraceSeq 留痕 id 自增序列(同进程内唯一性;与 unixnano 拼接)。
var eventTraceSeq uint64

// newEventTraceID 生成留痕 id("evt-<unixnano>-<seq>";零 rand,不消耗 w.Rand)。
func newEventTraceID() string {
	n := atomic.AddUint64(&eventTraceSeq, 1)
	return fmt.Sprintf("evt-%d-%x", time.Now().UnixNano(), n)
}

// GateDecision 单个决策闸门的结果。
type GateDecision struct {
	Gate   string `json:"gate"`             // idempotency | magnitude_limit | active_limit | monthly_count | kind_dedup | unknown_kind | params
	Passed bool   `json:"passed"`           // false ⇒ 该闸门拦截
	Detail string `json:"detail,omitempty"` // 拦截原因(未通过时)
}

// EventLifecycleTrace 单条事件生命周期留痕(4 阶段)。
//
// 纪律:恒为值语义(环形缓冲存值);Effects 为副本;SnapshotID 关联 §3.2 快照。
//
// ⚠ roomID 为**非导出**字段(不进 JSON 契约):仅用于引擎层 snapshot 落盘时
// 定位 ./virtual_city_saves/<roomID>/ 子目录 —— applyScenarioToWorldTraced
// 是引擎级函数,拿不到房间号,由房间层建 trace 时写入。空 ⇒ 跳过快照。
type EventLifecycleTrace struct {
	TraceID      string `json:"trace_id"`
	EventKey     string `json:"event_key,omitempty"` // 关联世界事件指纹(可空)
	ScenarioKind string `json:"scenario_kind,omitempty"`
	TriggeredAt  int64  `json:"triggered_at"` // unix 毫秒

	// ① 触发
	TriggerSource string `json:"trigger_source"` // TriggerSource*
	TriggerDetail string `json:"trigger_detail"` // 触发依据(规则 id / 工具名 / 用户备注)

	// ② 决策
	DecisionGates   []GateDecision `json:"decision_gates"`
	DecisionOutcome string         `json:"decision_outcome"` // pass | reject

	// ③ 执行
	ExecutionMagnitude int              `json:"execution_magnitude"`
	ExecutionDuration  int              `json:"execution_duration"` // 持续月数(注入参数)
	ExecutionTarget    string           `json:"execution_target"`
	ExecutionEffects   []ScenarioEffect `json:"execution_effects"`

	// ④ 结果
	ResultOutcome   string `json:"result_outcome"` // success | failed | rolled_back
	ResultErrCode   int    `json:"result_err_code"`
	ResultErrMsg    string `json:"result_err_msg,omitempty"`
	StateHashBefore string `json:"state_hash_before"`
	StateHashAfter  string `json:"state_hash_after"`

	// 回放关联:注入前快照 id(§3.2;空 = 无快照,不可回放)。
	SnapshotID string `json:"snapshot_id,omitempty"`

	roomID string // 非导出:快照落盘子目录定位(见上方注释)
}

// cloneEventLifecycleTrace 深拷贝一条留痕(切片字段独立;调用方改不到环内)。
func cloneEventLifecycleTrace(tr *EventLifecycleTrace) EventLifecycleTrace {
	if tr == nil {
		return EventLifecycleTrace{}
	}
	cp := *tr
	if tr.DecisionGates != nil {
		cp.DecisionGates = append([]GateDecision(nil), tr.DecisionGates...)
	}
	if tr.ExecutionEffects != nil {
		cp.ExecutionEffects = append([]ScenarioEffect(nil), tr.ExecutionEffects...)
	}
	return cp
}

// appendEventLifecycleTraceLocked 追加一条留痕(新→旧;环上限 eventLifecycleLogCap)。
// 零 rand;nil World 安全(直接丢弃 —— 没有世界就没有留痕环)。
func (w *World) appendEventLifecycleTraceLocked(tr EventLifecycleTrace) {
	if w == nil {
		return
	}
	if tr.DecisionGates == nil {
		tr.DecisionGates = make([]GateDecision, 0)
	}
	if tr.ExecutionEffects == nil {
		tr.ExecutionEffects = make([]ScenarioEffect, 0)
	}
	out := make([]EventLifecycleTrace, 0, len(w.EventLifecycleLog)+1)
	out = append(out, tr)
	out = append(out, w.EventLifecycleLog...)
	if len(out) > eventLifecycleLogCap {
		out = out[:eventLifecycleLogCap]
	}
	w.EventLifecycleLog = out
}

// initEventLifecycleTrace 初始化留痕的触发段 + Before 哈希(零 rand)。
func initEventLifecycleTrace(trace *EventLifecycleTrace, w *World, kind string, at int64) {
	if trace == nil {
		return
	}
	trace.TraceID = newEventTraceID()
	trace.ScenarioKind = kind
	trace.TriggeredAt = at
	// 房间层可能已预置安全阀闸门(批次86 §3.1.4)⇒ 只在 nil 时初始化,不覆盖。
	if trace.DecisionGates == nil {
		trace.DecisionGates = make([]GateDecision, 0, 5)
	}
	if trace.ExecutionEffects == nil {
		trace.ExecutionEffects = make([]ScenarioEffect, 0)
	}
	trace.DecisionOutcome = ""
	trace.ResultOutcome = ""
	if w != nil {
		trace.StateHashBefore = w.StateHash()
	}
}

// addGateDecision 追加一道闸门记录(nil trace 安全;零 rand)。
func addGateDecision(trace *EventLifecycleTrace, gate string, passed bool, detail string) {
	if trace == nil {
		return
	}
	trace.DecisionGates = append(trace.DecisionGates, GateDecision{
		Gate: gate, Passed: passed, Detail: detail,
	})
}

// finalizeTraceFailure 记录失败结果并追加留痕(nil trace / nil World 安全)。
//
// DecisionOutcome 恒置 reject;ResultOutcome 恒置 failed;StateHashAfter 取
// 当前状态(校验失败时状态尚未被改写 ⇒ 与 Before 一致,正是「零改动」的
// 可验证证据)。
func finalizeTraceFailure(trace *EventLifecycleTrace, w *World, e *errcode.Error) {
	if trace == nil {
		return
	}
	trace.DecisionOutcome = EventDecisionReject
	trace.ResultOutcome = EventResultFailed
	if e != nil {
		trace.ResultErrCode = e.Code
		trace.ResultErrMsg = e.Message
	}
	if w != nil {
		h := w.StateHash()
		if trace.StateHashBefore == "" {
			trace.StateHashBefore = h
		}
		trace.StateHashAfter = h
		w.appendEventLifecycleTraceLocked(*trace)
	}
}

// ─────────────────────────── 房间层查询(契约 §3.1.6) ───────────────────────────

// EventLifecycleTraces 返回留痕环(新→旧;副本;nil 安全)。
//
// limit <= 0 ⇒ 返回全部(≤eventLifecycleLogCap);limit > 0 ⇒ 返回前 limit 条。
// 锁纪律(§92a):持 r.mu 只读,返回值全部分片独立,不回调房间。
func (r *VirtualCityRoom) EventLifecycleTraces(limit int) []EventLifecycleTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []EventLifecycleTrace{}
	if r.World == nil {
		return out
	}
	n := len(r.World.EventLifecycleLog)
	if limit > 0 && limit < n {
		n = limit
	}
	for i := 0; i < n; i++ {
		out = append(out, cloneEventLifecycleTrace(&r.World.EventLifecycleLog[i]))
	}
	return out
}

// EventLifecycleTraceByID 按 trace_id 精确查询(未命中 nil;返回副本)。
func (r *VirtualCityRoom) EventLifecycleTraceByID(traceID string) *EventLifecycleTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.eventLifecycleTraceByIDLocked(traceID)
}

// eventLifecycleTraceByIDLocked 按 trace_id 查询(调用方持 r.mu)。
func (r *VirtualCityRoom) eventLifecycleTraceByIDLocked(traceID string) *EventLifecycleTrace {
	if r.World == nil || traceID == "" {
		return nil
	}
	for i := range r.World.EventLifecycleLog {
		if r.World.EventLifecycleLog[i].TraceID != traceID {
			continue
		}
		cp := cloneEventLifecycleTrace(&r.World.EventLifecycleLog[i])
		return &cp
	}
	return nil
}

// ─────────────────────────── 回放(契约 §3.1.5) ───────────────────────────

// ReplayScenarioTrace 按 trace_id 回放一次注入(批次86 §3.1.5)。
//
// 语义:① 找到 trace;② 加载 trace.SnapshotID 关联的快照;③ 恢复到 World;
// ④ 用 trace 记录的参数重放注入(TriggerSourceReplay);⑤ 校验 StateHashAfter
// 与原 trace 一致(确定性验证)。
//
// 返回:新 trace(回放记录)/ 校验结果(true = 与原 trace 的 After 哈希一致)/
// 错误(35002 未运行、35067 留痕不存在、35066 快照缺失或损坏)。
//
// ⚠ 快照缺失(被轮转)⇒ 返回 ErrVirtualCitySnapshotNotFound,**不静默**
// (契约 §3.1.5「误差兜底」)。
func (r *VirtualCityRoom) ReplayScenarioTrace(traceID string) (*EventLifecycleTrace, bool, *errcode.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.World == nil || r.Status != StatusPlaying {
		return nil, false, errcode.Code(errcode.ErrVirtualCityNotPlaying)
	}
	src := r.eventLifecycleTraceByIDLocked(traceID)
	if src == nil {
		return nil, false, errEventTraceNotFound(traceID)
	}
	if src.SnapshotID == "" {
		return nil, false, errSnapshotNotFound("")
	}
	snap, err := LoadWorldSnapshot(r.RoomID, src.SnapshotID)
	if err != nil {
		return nil, false, errSnapshotNotFound(src.SnapshotID)
	}
	nw, err := unmarshalWorld(snap.WorldJSON)
	if err != nil {
		return nil, false, errSnapshotNotFound(src.SnapshotID)
	}
	r.applyWorldConfigLocked(nw)
	r.World = nw
	// 事件游标跟随恢复后的世界,避免恢复后漏播/重播事件。
	r.eventsSent = len(nw.Events)
	// 指标历史环(批次84)同上:清空恢复点之后的月份,由后续月结重新采样。
	r.metricHistory = nil

	trace := newScenarioInjectionTrace(r.RoomID, TriggerSourceReplay, "ReplayScenarioTrace:"+src.TraceID)
	trace.EventKey = src.EventKey
	_, _, _, e := applyScenarioToWorldTraced(nw, src.ScenarioKind, src.ExecutionMagnitude,
		src.ExecutionDuration, src.ExecutionTarget, src.TriggerDetail, time.Now().UnixMilli(), nw.Seed, trace)
	if e != nil {
		return nil, false, e
	}
	match := trace.StateHashAfter != "" && trace.StateHashAfter == src.StateHashAfter
	return trace, match, nil
}
