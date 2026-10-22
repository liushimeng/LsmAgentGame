/**
 * 批次 65 · City-Chrome 实时数据通道 + 实时看板 — 类型定义。
 *
 * 契约唯一事实来源：lag_docs/虚拟城市/已实现/65-CityChrome实时数据通道与实时看板/01-方案设计.md
 * §3.2（FeedStore 数据模型，后端逐字实现）+ §3.3（WS 帧 game.realtime_feed）+
 * §3.5（HTTP 快照 / 刷新端点）。
 *
 * 独立成文件：types/virtualCity.ts 已 1789 行逼近 §4 的 1800 行硬上限。
 */

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
