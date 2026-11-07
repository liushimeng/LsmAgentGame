/**
 * 批次 65 · City-Chrome 实时数据通道 + 实时看板 — 类型定义。
 *
 * 契约唯一事实来源：lag_docs/虚拟城市/已实现/65-CityChrome实时数据通道与实时看板/01-方案设计.md
 * §3.2（FeedStore 数据模型，后端逐字实现）+ §3.3（WS 帧 game.realtime_feed）+
 * §3.5（HTTP 快照 / 刷新端点）。
 *
 * 独立成文件：types/virtualCity.ts 已 1789 行逼近 §4 的 1800 行硬上限。
 */

/**
 * 批次 89 · 异常种类（后端 `DeriveAnomalies` 的 kind 值域，逐字对齐）。
 *
 * ⚠ 阈值边界口径（**严格超过 `>`**）：恰好等于阈值**不触发**。
 *   设计文档 §4.2 的阈值边界写法与 §9.1 验收的「恰好等于阈值不触发」自相矛盾，
 *   已由主 Agent 收口裁定取后者 ⇒ 与后端 `anomaly.go` 的 `>` 表述一致。
 *
 * - high：涨幅**超过**类目阈值（§4.2 阈值表）
 * - low：跌幅**超过**类目阈值
 * - stale：陈旧（age **>** 2 × 该类目调度档位，批次 88 口径）
 * - jump：跳变（|price-prev|/prev **>** 2 × 类目阈值）
 */
export type RtAnomalyKind = 'high' | 'low' | 'stale' | 'jump';

/** 单条实时报价（站点口径）。 */
export interface RealtimeQuote {
  /** 站点口径代码或名称，如 "000001"/"上证指数"/"BTC"。 */
  symbol: string;
  name: string;
  price: number;
  /** 百分比，-100~+100。 */
  change_pct: number;
  currency?: string;
  /** Unix 秒。 */
  updated_at: number;
  /** 采集来源站点名。 */
  source: string;
  /**
   * 批次 89 · 计量单位（如 "point" / "cny" / "usd_per_oz"）。
   *
   * 后端恒给值（缺省 ""），**不得**标可选 —— 批次 84 教训：
   * 空值有语义的字段加 `omitempty` 会在 JSON 往返时静默丢失。
   */
  unit: string;
  /**
   * 批次 89 · 采集源页面链接（免登录可浏览）。
   *
   * 后端恒给值（缺省 ""），命中口径卡时取 `MetricSpec.source_url`。
   */
  source_url: string;
  /**
   * 批次 89 · 置信度 0..1（新鲜度 × 源权威度 × 异常惩罚，保留 3 位小数）。
   *
   * 后端恒给值（缺省 0）。前端**不复制阈值口径**，只渲染后端给的数。
   */
  confidence: number;
  /**
   * 批次 89 · 异常标记（空串 / 缺省 = 无异常）。
   *
   * 取值见 {@link RtAnomalyKind}；后端 `omitempty` ⇒ 无异常时字段缺省。
   */
  anomaly?: RtAnomalyKind | '';
  /**
   * 批次 89 · 命中的口径卡 id（如 "a_share.index.sh"）。
   *
   * 后端在 `DeriveQuoteQuality` 里用 `LookupMetricSpec` 命中口径卡时填入
   * 该卡的 `MetricSpec.id`，**未命中时为空串 / 缺省**。
   *
   * ⚠ 为什么需要它：后端口径卡的 `Keywords`（含 "sh000001" 这类站点代码别名）
   *   是 `json:"-"` **不下发** ⇒ 前端无法复刻后端的模糊匹配；站点口径名与
   *   指标名不一致时（如站点名「沪指」vs 指标名「上证指数」）后端已命中、
   *   前端却匹配不到 ⇒ 显示「该指标暂无口径卡」，批次 89 的核心价值打折。
   *   ⇒ 后端下发 `spec_id` 做**显式关联**，前端据此精确命中。
   *
   * 可选字段（后端未命中时整个键缺省）。
   */
  spec_id?: string;
}

/**
 * 批次 89 · 指标口径卡（后端 `metric_spec.go` 静态注册表）。
 *
 * 字段名与后端 JSON **逐字一致**（snake_case）。仅随 HTTP 快照下发
 * （`GET /api/virtual_city/realtime_board` 的 `data.metric_specs`、
 * `GET /api/virtual_city/dashboard` 的 `data.dashboard.metric_specs`），
 * **不进 WS `game.realtime_feed` 帧**（静态目录不占高频帧）。
 */
export interface MetricSpec {
  /** 唯一 id（如 "a_share.index.sh"）。 */
  id: string;
  /** 类目：a_share|hk|us|metal|futures|crypto|macro|news。 */
  category: string;
  /** 指标显示名（如 "上证指数"）。 */
  indicator: string;
  /** 计量单位（与 {@link RealtimeQuote.unit} 同取值表）。 */
  unit: string;
  /** 口径说明：这个数到底是什么。 */
  caliber: string;
  /** 计算式（如 "change_pct = (price - prev_close) / prev_close * 100"）。 */
  formula: string;
  /** 来源站点名。 */
  source: string;
  /** 来源站点 / 接口地址。 */
  source_url: string;
  /** 刷新档位说明（如 "交易时段 600s / 休市 7200s"）。 */
  refresh: string;
}

/**
 * 批次 89 · 异常告警条目（后端 `DeriveAnomalies` 输出）。
 *
 * 字段名与后端 JSON **逐字一致**。仅随 HTTP 快照下发
 * （`data.anomalies` / `data.dashboard.anomalies`），**不进 WS 帧**。
 */
export interface Anomaly {
  /** 行情类目。 */
  category: string;
  /** 站点口径代码。 */
  symbol: string;
  /** 显示名。 */
  name: string;
  /** 异常种类（见 {@link RtAnomalyKind}）。 */
  kind: RtAnomalyKind;
  /** 严重度 1..3（越大越严重）。 */
  severity: number;
  /**
   * 触发值 —— ⚠ **单位随 kind 变**（后端 `Anomaly.Value` 口径，逐字对齐）：
   * - high / low / jump = **幅度百分比**（change_pct 或跳变幅度，单位 %）
   * - stale = **陈旧秒数**（age_seconds，单位秒）
   *
   * ⇒ 前端渲染必须按 kind 分支：stale 不追加 `%` 后缀。
   */
  value: number;
  /** 触发依据（含阈值与实测值；后端文案用「超过阈值」口径）。 */
  detail: string;
  /** Unix 秒。 */
  observed_at: number;
}

/** 自采价点（服务端入环，看板客户端聚合 OHLC 用）。 */
export interface RealtimePoint {
  /** Unix 秒。 */
  t: number;
  p: number;
}

/** 单类目行情快照。 */
export interface RealtimeCategory {
  updated_at: number;
  source: string;
  /** ≤ symbols_per_category（服务端配置，默认 12）。 */
  quotes: RealtimeQuote[];
  /** symbol → 最近 ≤48 点自采价序列。 */
  history: Record<string, RealtimePoint[]>;
}

/** 热点新闻。 */
export interface RealtimeNews {
  /** md5(title+url) 前 12 位。 */
  id: string;
  title: string;
  source: string;
  /** a_share/hk/us/metal/futures/crypto；综合新闻用 "general"。 */
  category: string;
  /** Unix 秒。 */
  published_at: number;
  url: string;
}

/** 数据通道状态。 */
export interface ChannelStatus {
  /** "online" | "offline"。 */
  status: string;
  /** 离线原因/最近错误。 */
  detail: string;
  last_ok_at: number;
}

/** WS 帧（game.realtime_feed）与 HTTP 快照共用的载荷。 */
export interface RealtimeFeed {
  channel: ChannelStatus;
  updated_at: number;
  /** key: a_share/hk/us/metal/futures/crypto。 */
  categories: Record<string, RealtimeCategory>;
  /** ≤ news_limit（默认 20），新→旧。 */
  news: RealtimeNews[];
}

/** 看板类目联合类型（"general" 仅用于新闻归类，不是行情类目）。 */
export type RealtimeBoardCategory =
  | 'a_share' | 'hk' | 'us' | 'metal' | 'futures' | 'crypto' | 'macro' | 'news' | 'general';

/** HTTP 刷新端点响应（POST /api/virtual_city/realtime_board/refresh）。 */
export interface RealtimeBoardRefreshResult {
  accepted: boolean;
  queued_category: string;
}

/** 批次 77 · 采集任务状态（MissionRecord.status）。 */
export type MissionStatus = 'running' | 'ok' | 'fail';

/**
 * 批次 77 · 采集任务遥测记录（服务端 MissionLog 环形缓冲 ≤20 条，新→旧）。
 *
 * 契约：仅随 HTTP 快照 GET /api/virtual_city/realtime_board 的 data.missions 下发，
 * **不进 WS game.realtime_feed 帧**（无记录为 []，非 null）。
 */
export interface MissionRecord {
  /** "<category>-<startedAtMs>"。 */
  id: string;
  /** a_share|hk|us|metal|futures|crypto|macro|news。 */
  category: string;
  /** Unix 秒。 */
  started_at: number;
  duration_ms: number;
  status: MissionStatus;
  /** 成功时的采集站点名，失败为空串。 */
  source: string;
  steps_used: number;
  max_steps: number;
  /** 规划摘要（goal + steps 以 " → " 连接），可能为空串。 */
  plan: string;
  /** 提交行情条数。 */
  quotes: number;
  /** 提交新闻条数。 */
  news: number;
  /** 失败原因，成功为空串。 */
  error: string;
  /**
   * 批次 78 · 本次 mission 访问过的 URL 轨迹（新→旧去重，≤6 条，每条 ≤120 rune）。
   *
   * 契约：服务端 `MissionRecord.Visited` 的 JSON 名逐字对齐 `visited`；
   * 缺省为空数组（早期记录无该字段）。前端只展示 host + pathname。
   * ⚠ 服务端 Go 侧 nil slice 会序列化成 `null` ⇒ 渲染端一律 `m.visited ?? []`。
   */
  visited: string[];
}

/**
 * 批次 78 · 采集健康度聚合（服务端 `MissionLog.Stats()`）。
 *
 * 契约字段名与后端 JSON **逐字一致**（snake_case，不做驼峰改写）：
 * total / ok / fail / running / success_rate / avg_steps / avg_duration_ms / last_error。
 *
 * 仅随 HTTP 快照 GET /api/virtual_city/realtime_board 的 data.stats 下发，
 * **不进 WS game.realtime_feed 帧**（无记录时全零：success_rate=0、last_error=""）。
 */
export interface MissionStats {
  total: number;
  ok: number;
  fail: number;
  running: number;
  /** 成功率 0..1（分母 = ok + fail）。 */
  success_rate: number;
  /** 成功记录平均步数。 */
  avg_steps: number;
  /** 成功记录平均耗时（毫秒）。 */
  avg_duration_ms: number;
  /** 最近一次失败原因，成功为空串。 */
  last_error: string;
}

/** 批次 66 外盘情绪锚（game.state.market.exo_anchor；契约 01 §3.3）。 */
export interface VirtualCityExoAnchor {
  /** false = 数据过期/通道离线 ⇒ bias 恒 0（对局零回归）。 */
  active: boolean;
  /** [-1, +1] 风险偏好分（tanh 饱和）。 */
  sentiment: number;
  /** 股票月漂移加法项（小数，封顶 ±0.003）。 */
  stock_bias: number;
  /** 黄金月漂移加法项（小数，封顶 ±0.002）。 */
  gold_bias: number;
  /** 参与计算的最新报价 Unix 秒。 */
  updated_at: number;
}
