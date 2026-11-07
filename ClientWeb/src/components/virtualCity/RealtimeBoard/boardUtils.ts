/**
 * 批次 65 · 实时看板纯函数工具 — 5min OHLC 分桶 / 时间差分档 / 核心指数匹配。
 *
 * 数据诚实口径（方案 §5.2）：服务端 History 是「自采价点环（≤48 点）」，
 * 看板只做客户端聚合，不做伪历史。桶数 <2 时 CandleChart 降级折线并标注「采集中」。
 */

import type {
  MetricSpec,
  RealtimeBoardCategory,
  RealtimeCategory,
  RealtimeQuote,
} from '@/types/virtualCityRealtime';

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

// ── 批次 89 · 真实数据口径卡与异常告警层 ─────────────────────────────────
//
// 纪律（设计文档 §8）：阈值 / 单位 / 权威度**只在后端一处**，前端**不复制阈值**；
// 前端只渲染后端给的 `unit` / `confidence` / `anomaly` / `metric_specs` / `anomalies`。
//
// ⚠ 阈值边界口径（主 Agent 收口裁定）：**严格超过 `>`** —— 恰好等于阈值**不触发**
//   （设计文档 §4.2 的阈值边界写法与 §9.1 的「恰好等于阈值不触发」自相矛盾，取后者）。
//   ⇒ 前端文案/注释一律写「超过阈值」，与后端 `citychrome/anomaly.go` 的 `>`
//     判定式一致（自检：scripts/verify_virtual_city_realtime_batch89.mjs 的
//     阈值边界口径扫描会拦截残留的非严格比较写法）。
//
// ⚠ `Anomaly.value` 单位随 kind 变：high/low/jump = 幅度 %；stale = **陈旧秒数**
//   ⇒ 前端渲染必须按 kind 分支（见 MetricSpecPanel.tsx 的 `isStale`）。

/** 单位代码 → i18n 键（后端 `unit` 取值表，设计文档 §3.3）。 */
const UNIT_LABEL_KEYS: Record<string, string> = {
  point: 'virtualCity.rt.unit.point',
  cny: 'virtualCity.rt.unit.cny',
  hkd: 'virtualCity.rt.unit.hkd',
  usd: 'virtualCity.rt.unit.usd',
  cny_per_g: 'virtualCity.rt.unit.cny_per_g',
  usd_per_oz: 'virtualCity.rt.unit.usd_per_oz',
  usd_per_bbl: 'virtualCity.rt.unit.usd_per_bbl',
  cny_per_t: 'virtualCity.rt.unit.cny_per_t',
  pct: 'virtualCity.rt.unit.pct',
  cny_trillion: 'virtualCity.rt.unit.cny_trillion',
  text: 'virtualCity.rt.unit.text',
};

/**
 * 单位代码 → i18n 键（未知单位返回 null ⇒ 渲染端回退显示原始代码）。
 *
 * ⚠ 未知单位**不静默丢弃**：回退展示原始 code，保证「口径可读」不丢信息。
 */
export function unitLabelKey(unit: string | undefined | null): string | null {
  if (!unit) return null;
  return UNIT_LABEL_KEYS[unit] ?? null;
}

/** 异常种类 → i18n 键（后端 `anomaly` / `kind` 值域，设计文档 §3.1 / §3.4）。 */
const ANOMALY_LABEL_KEYS: Record<string, string> = {
  high: 'virtualCity.rt.anomaly.high',
  low: 'virtualCity.rt.anomaly.low',
  stale: 'virtualCity.rt.anomaly.stale',
  jump: 'virtualCity.rt.anomaly.jump',
};

/** 异常种类 → i18n 键（未知种类返回 null ⇒ 渲染端回退显示原始 kind）。 */
export function anomalyLabelKey(kind: string | undefined | null): string | null {
  if (!kind) return null;
  return ANOMALY_LABEL_KEYS[kind] ?? null;
}

/**
 * 报价 → 口径卡匹配。
 *
 * 匹配链（**优先级从高到低**）：
 *   ① `quote.spec_id` 精确命中 —— 后端 `DeriveQuoteQuality` 用
 *      `LookupMetricSpec`（含未下发的 `Keywords` 模糊匹配）命中后显式下发
 *      口径卡 id ⇒ **前端与后端口径完全一致**（最高优先级）。
 *   ② `spec.indicator === quote.name`（指标名精确）
 *   ③ `spec.id === quote.symbol` 或 `spec.id` 以 `.${quote.symbol}` 结尾
 *   ④ 包含匹配（`quote.name` ⊃ `spec.indicator` 或反之）—— 多命中时取
 *      **indicator 最短**者（更具体的那张卡），同长度取注册表首个。
 *
 * ⚠ ①②③ 都在**同类目内**匹配（`category` 非空时）。
 *
 * ⚠ 匹配失败**不猜**：返回 null ⇒ 渲染端显示「该指标暂无口径卡」+ 报价自带的
 *   `source` / `source_url`，绝不编造口径（设计文档 §8「口径单一」）。
 *
 * ⚠ ① 的意义：后端口径卡的 `Keywords`（含 "sh000001" 这类站点代码别名）是
 *   `json:"-"` **不下发** ⇒ 没有 `spec_id` 时，前端在站点口径名与指标名不一致时
 *   （如站点名「沪指」vs 指标名「上证指数」）会漏配。后端下发 `spec_id` 后
 *   该场景由 ① 精确命中，两端口径不再打架。
 */
export function findMetricSpec(
  specs: MetricSpec[] | null | undefined,
  category: string,
  quote: RealtimeQuote,
): MetricSpec | null {
  if (!specs || !specs.length) return null;
  const pool = specs.filter((s) => !category || s.category === category);
  if (!pool.length) return null;
  const name = (quote.name ?? '').trim();
  const symbol = (quote.symbol ?? '').trim();

  // ① 后端显式下发的口径卡 id（权威，最高优先级）。
  const specId = (quote.spec_id ?? '').trim();
  if (specId) {
    const bySpecId = pool.find((s) => s.id === specId);
    if (bySpecId) return bySpecId;
    // ⚠ spec_id 指向的卡不在同类目目录内 ⇒ 不跨类目硬取，继续走兜底链
    //   （避免把别的类目的口径贴到本行上）。
  }

  // ② 指标名精确。
  const byIndicator = pool.find((s) => s.indicator === name);
  if (byIndicator) return byIndicator;

  // ③ id / symbol 精确。
  if (symbol) {
    const byId = pool.find((s) => s.id === symbol || s.id.endsWith(`.${symbol}`));
    if (byId) return byId;
  }

  // ④ 包含匹配（兜底）：取**最短** indicator（更具体的一张卡）；同长度取首个。
  if (name.length >= 2) {
    const candidates = pool.filter(
      (s) =>
        s.indicator.length >= 2 &&
        (name.includes(s.indicator) || s.indicator.includes(name)),
    );
    if (candidates.length) {
      let best = candidates[0];
      for (const c of candidates) {
        if (c.indicator.length < best.indicator.length) best = c;
      }
      return best;
    }
  }
  return null;
}

/** 口径卡目录按类目分组（保持类目首次出现顺序）。 */
export function groupSpecsByCategory(
  specs: MetricSpec[] | null | undefined,
): Array<{ category: string; items: MetricSpec[] }> {
  const groups = new Map<string, MetricSpec[]>();
  for (const s of specs ?? []) {
    const list = groups.get(s.category);
    if (list) list.push(s);
    else groups.set(s.category, [s]);
  }
  return [...groups.entries()].map(([category, items]) => ({ category, items }));
}
