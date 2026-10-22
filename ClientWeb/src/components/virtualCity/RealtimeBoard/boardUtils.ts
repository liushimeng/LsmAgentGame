/**
 * 批次 65 · 实时看板纯函数工具 — 5min OHLC 分桶 / 时间差分档 / 核心指数匹配。
 *
 * 数据诚实口径（方案 §5.2）：服务端 History 是「自采价点环（≤48 点）」，
 * 看板只做客户端聚合，不做伪历史。桶数 <2 时 CandleChart 降级折线并标注「采集中」。
 */

import type { RealtimeBoardCategory, RealtimeCategory, RealtimeQuote } from '@/types/virtualCityRealtime';

/** 5 分钟分桶（秒）。 */
export const RT_BUCKET_SEC = 300;

/** 一根聚合蜡烛。 */
export interface RtOhlc {
  /** 桶起点 Unix 秒。 */
  t: number;
  open: number;
  high: number;
  low: number;
  close: number;
}

/**
 * 自采价点序列 → 5min OHLC 蜡烛序列。
 *
 * 实现：按 floor(t/BUCKET_SEC) 分桶；每桶 open=首点、close=末点、high/low=极值。
 * 输入乱序也能正确（先按 t 排序再扫）。空输入返回 []。
 */
export function aggregateOHLC(points: { t: number; p: number }[]): RtOhlc[] {
  if (!points.length) return [];
  const sorted = [...points].sort((a, b) => a.t - b.t);
  const buckets = new Map<number, RtOhlc>();
  for (const pt of sorted) {
    const key = Math.floor(pt.t / RT_BUCKET_SEC) * RT_BUCKET_SEC;
    const cur = buckets.get(key);
    if (!cur) {
      buckets.set(key, { t: key, open: pt.p, high: pt.p, low: pt.p, close: pt.p });
    } else {
      if (pt.p > cur.high) cur.high = pt.p;
      if (pt.p < cur.low) cur.low = pt.p;
      cur.close = pt.p;
    }
  }
  return [...buckets.values()].sort((a, b) => a.t - b.t);
}

/** 时间差分档（渲染端映射 i18n 键）。 */
export type RtTimeAgoUnit = 'just' | 'min' | 'hour' | 'day';

export interface RtTimeAgo {
  n: number;
  unit: RtTimeAgoUnit;
}

/**
 * Unix 秒 → 「{n} {unit} 前」分档。
 * nowSec 注入便于测试；未来时间归入 just。
 */
export function timeAgoParts(tsSec: number, nowSec = Math.floor(Date.now() / 1000)): RtTimeAgo {
  const diff = Math.max(0, nowSec - tsSec);
  if (diff < 60) return { n: 0, unit: 'just' };
  const min = Math.floor(diff / 60);
  if (min < 60) return { n: min, unit: 'min' };
  const hour = Math.floor(min / 60);
  if (hour < 24) return { n: hour, unit: 'hour' };
  return { n: Math.floor(hour / 24), unit: 'day' };
}

/** i18n 键：virtualCity.rt.updated.{unit}（just 无 {n} 占位）。 */
export function timeAgoKey(ago: RtTimeAgo): string {
  return `virtualCity.rt.updated.${ago.unit}`;
}

/** 总览核心指数卡配置：跨类目各挑一个代表指数（有数据才显示）。 */
export interface RtCoreIndexDef {
  /** 行情类目。 */
  category: RealtimeBoardCategory;
  /** 名称模糊匹配关键词（按序取首个命中）。 */
  keywords: string[];
}

export const RT_CORE_INDICES: RtCoreIndexDef[] = [
  { category: 'a_share', keywords: ['上证指数', '上证综指', '上证'] },
  { category: 'hk', keywords: ['恒生指数', '恒生', '恒指'] },
  { category: 'us', keywords: ['纳斯达克', '纳指', '道指', '标普'] },
  { category: 'metal', keywords: ['黄金', '伦敦金', 'Au'] },
  { category: 'futures', keywords: ['原油', 'WTI', '布伦特'] },
  { category: 'crypto', keywords: ['BTC', '比特币'] },
  // 批次76:宏观指标（无数据时不显示）。
  { category: 'macro', keywords: ['LPR', 'CPI', 'GDP'] },
];

/** 在报价列表里按关键词序匹配第一条（大小写不敏感）。 */
export function pickQuote(quotes: RealtimeQuote[], keywords: string[]): RealtimeQuote | null {
  for (const kw of keywords) {
    const low = kw.toLowerCase();
    const hit = quotes.find((q) => q.name.toLowerCase().includes(low));
    if (hit) return hit;
  }
  return null;
}

/** 取某类目的行情数据（feed.categories 缺该类目 → null）。 */
export function getCategory(
  categories: Record<string, RealtimeCategory>,
  key: string,
): RealtimeCategory | null {
  return categories[key] ?? null;
}

/**
 * 行情涨跌幅文本 —— 契约口径是**百分数**(0.12 = 0.12%),不是小数。
 * ⚠ 不可用 types/virtualCityFormat.ts 的 formatPct:那个是「小数 → 百分比」
 * (0.035 → "3.5%"),用在这里会二次 ×100(批次65 缺陷:上证 +0.12% 显示成 +12.00%)。
 */
export function pctText(v: number | undefined | null, digits = 2): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${v.toFixed(digits)}%`;
}
