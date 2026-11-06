/**
 * 批次 86 · 可靠性与可观测性契约类型（leaf 模块）。
 *
 * 契约：lag_docs/虚拟城市/已实现/86-可靠性与可观测性/01-方案设计.md §3.1 / §3.2 / §3.3 / §4。
 *
 * 迁出独立文件的原因：`types/virtualCity.ts` 已达 §4 的 1800 行硬上限
 *   （批次 61/62/70/71 起陆续拆出 virtualCityStages / virtualCitySense /
 *   virtualCityCalibration / virtualCityFamily）；本批新增 5 个接口另置，
 *   外部 import 路径保持 `@/types/virtualCity` 不变（virtualCity.ts re-export）。
 *
 * 字段名 / 可空性一律与后端 JSON tag 逐字对齐（snake_case），后端
 *   `game/virtual_city/event_lifecycle.go` + `view_key_metrics.go` +
 *   `room_save_snapshot.go` 是协议唯一实现。
 */

// ── §3.3 关键指标（game.state.key_metrics）──────────────────────────────

/** 关键指标（KeyMetricsJSON；后端恒下发，空态零值，不 omit）。 */
export interface VirtualCityKeyMetrics {
  /** 人口 = 非空座位数（居民数）。 */
  population: number;
  /** 经济产出 = 居民总财富（元；Society.TotalWealth）。 */
  economic_output: number;
  /** 社会满意度 0..100（100 − gini×100 + SentimentBias）。 */
  satisfaction: number;
  /** 事件队列积压 = 未注入世界事件数 + 活动场景数。 */
  event_queue_backlog: number;
  /** 活动场景数（EventQueueBacklog 的组成部分，单独列出便于诊断）。 */
  active_scenarios: number;
  /** 未注入世界事件数（EventQueueBacklog 的组成部分）。 */
  pending_world_events: number;
}

// ── §3.1 事件生命周期留痕（game.state.event_lifecycle）──────────────────

/** 单个决策闸门的结果（GateDecision）。 */
export interface VirtualCityGateDecision {
  /** idempotency | magnitude_limit | active_limit | monthly_count | kind_dedup | unknown_kind | params。 */
  gate: string;
  /** false ⇒ 该闸门拦截。 */
  passed: boolean;
  /** 拦截原因（未通过时；后端 omitempty ⇒ 可空）。 */
  detail?: string;
}

/** 单条执行效果（ScenarioEffect）。 */
export interface VirtualCityScenarioEffect {
  metric: string;
  delta: number;
  text: string;
}

/**
 * 单条事件生命周期留痕（4 阶段：触发 / 决策 / 执行 / 结果）。
 *
 * ⚠ 环形缓冲新→旧，≤50 条；旧后端 / 未产生留痕 ⇒ 整个键缺省或空数组。
 *   `snapshot_id` 为空 ⇒ 无关联快照，不可回放。
 */
export interface VirtualCityEventLifecycleTrace {
  trace_id: string;
  /** 关联世界事件指纹（可空）。 */
  event_key?: string;
  scenario_kind?: string;
  /** unix 毫秒。 */
  triggered_at: number;
  /** manual | agent_tool | realworld_rule | auto_inject | replay | system。 */
  trigger_source: string;
  /** 触发依据（规则 id / 工具名 / 用户备注）。 */
  trigger_detail: string;
  decision_gates: VirtualCityGateDecision[];
  /** pass | reject。 */
  decision_outcome: string;
  execution_magnitude: number;
  /** 持续月数（注入参数）。 */
  execution_duration: number;
  execution_target: string;
  execution_effects: VirtualCityScenarioEffect[];
  /** success | failed | rolled_back。 */
  result_outcome: string;
  result_err_code: number;
  result_err_msg?: string;
  state_hash_before: string;
  state_hash_after: string;
  /** 注入前快照 id（空 = 无快照，不可回放）。 */
  snapshot_id?: string;
}

// ── §3.2 快照元信息（SnapshotMeta；不含 WorldJSON）──────────────────────

/** 快照元信息（列表 / 存档 / 读档响应的 `snapshot` 字段）。 */
export interface VirtualCitySnapshotMeta {
  snapshot_id: string;
  /** unix 毫秒。 */
  created_at: number;
  month: number;
  state_hash: string;
  /** manual | auto_rollback | recovery | replay。 */
  reason: string;
  size_bytes: number;
}
