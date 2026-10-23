/**
 * 批次 80 · 真实世界事件自动注入引擎 — 前端类型定义。
 *
 * 契约唯一事实来源：
 *   lag_docs/虚拟城市/已实现/80-真实世界事件自动注入引擎/01-方案设计.md
 *   §3.6（game.state.world_events JSON 契约）+ §3.4（WorldEventRecord 逐字段）。
 *
 * ⚠ 独立成文件：types/virtualCity.ts 已 1780/1800 触顶（§4 硬上限）——
 *   本批新类型只落本文件，virtualCity.ts 仅追加 1 行 import + 1 行字段声明。
 * ⚠ 字段名与后端 JSON **逐字一致**（snake_case，不做驼峰改写）。
 */

/** 事件方向（bullish 看多 / bearish 看空 / neutral 中性）。 */
export type WorldEventDirection = 'bullish' | 'bearish' | 'neutral';

/** 事件强度档 1..3（映射到场景 L1..L5 时受上限裁剪）。 */
export type WorldEventSeverity = 1 | 2 | 3;

/** 即时参数变化（批次 68 场景引擎 ScenarioEffect 结构复用）。 */
export interface WorldEventEffect {
  /** 指标 id（如 stock_index）；服务端权威。 */
  metric: string;
  /** 变化量（口径随指标而定：% / 点 / 绝对值）。 */
  delta: number;
  /** 服务端中文摘要（如「股指 -6.0%」）；可能为空串 ⇒ 前端回退 metric+delta。 */
  text: string;
}

/** 次月城市结果快照（因果链终点；事件注入后的下一次月结回填，未回填为 null）。 */
export interface WorldEventOutcome {
  /** 结果所属游戏月。 */
  month: number;
  /** CPI 同比（小数口径：0.021 = 2.1%）。 */
  cpi_yoy: number;
  /** 失业率（小数口径：0.045 = 4.5%）。 */
  unemployment_rate: number;
  /** 股票指数（点）。 */
  stock_index: number;
  /** 黄金价格（元/克）。 */
  gold_price: number;
  /** 商品价格指数（点）。 */
  goods_price_index: number;
  /** 居民现金合计（元）。 */
  resident_cash_total: number;
}

/** 单条世界事件记录（game.state.world_events.items[] 逐字段）。 */
export interface WorldEventRecord {
  /** 幂等指纹（行情类 rw:rule:bucket:day / 新闻类 rw:rule:newsId）。 */
  key: string;
  /** 派生规则 id：R1..R10。 */
  rule: string;
  /** 事件类型：equity_crash|gold_spike|crypto_crash|commodity_inflation|
   *  policy_easing|policy_tightening|earthquake|tsunami|extreme_disaster|survey_pulse。 */
  kind: string;
  /** 强度档 1..3。 */
  severity: WorldEventSeverity;
  /** 方向。 */
  direction: WorldEventDirection;
  /** 作用域：market|policy|disaster|macro|society。 */
  scope: string;
  /** 中文标题（看板 / 居民可见）。 */
  title: string;
  /** 一句话说明（触发依据，含关键数值）。 */
  detail: string;
  /** 触发量（% 或 1）。 */
  magnitude: number;
  /** 映射到的既有场景 kind（批次 68 场景目录）。 */
  scenario_kind: string;
  /** 注入强度 L1..L5（已受上限裁剪）。 */
  scenario_magnitude: number;
  /** 数据来源站点名。 */
  source: string;
  /** 原文链接（可能为空串 ⇒ 不渲染链接）。 */
  url: string;
  /** 观测时间（Unix 秒）。 */
  observed_at: number;
  /** 是否已注入城市（被安全阀拦下 ⇒ false）。 */
  injected: boolean;
  /** 注入发生的游戏月（未注入为 0）。 */
  injected_month: number;
  /** 即时参数变化（可能为空数组 / null）。 */
  effects: WorldEventEffect[] | null;
  /** 次月结果快照（待回填 ⇒ null）。 */
  outcome: WorldEventOutcome | null;
}

/**
 * game.state.world_events 顶层快照（批次 80 §3.6）。
 *
 * ⚠ 后端尚未上线时整个键**缺省**（undefined）⇒ 前端必须容错（不得白屏）。
 * ⚠ items / effects 后端 nil slice 会序列化成 `null` ⇒ 渲染端一律 `?? []`。
 */
export interface WorldEventsSnapshot {
  /** 自动注入总开关（false ⇒ 只观测不注入）。 */
  auto_enabled: boolean;
  /** 快照生成时间（Unix 秒）。 */
  updated_at: number;
  /** 本月已自动注入条数。 */
  injected_this_month: number;
  /** 事件列表（观测时间新→旧，≤20 条）。 */
  items: WorldEventRecord[] | null;
}
