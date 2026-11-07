/**
 * 批次 89 · 真实数据口径卡与异常告警层 —— 前端零依赖单测。
 *
 * 由 `scripts/verify_virtual_city_realtime_batch89.mjs` 经入口 import 打包执行；
 * 模块加载即自跑，失败抛错 ⇒ 进程非零退出。
 *
 * 覆盖三块：
 *  ① 报价行渲染（单位小字 / 异常徽章 / 置信度）；
 *  ② 口径卡渲染（定义 / 计算式 / 来源 / 刷新档位 / 原文链接 / 未命中降级）；
 *  ③ 「口径与异常」面板渲染（口径卡按类目分组 + 异常告警清单 + 空态）。
 *
 * 另附 i18n 三语键集合一致性断言（缺 key 即白屏，§29.8 铁律）。
 */

// react-dom/server.browser：esbuild（format:esm）打包 node 版 server 会因
// `require('stream')` 抛「Dynamic require not supported」，browser 版零 node
// 内置依赖且导出同名 renderToStaticMarkup。
import { renderToStaticMarkup } from 'react-dom/server.browser';
import { useI18nStore } from '@/store/i18n.store';
import { AnomalyBadge, CaliberCard, ConfidenceBar, QuoteTable } from '../RealtimeBoard/QuoteTable';
import { MetricSpecPanel } from '../RealtimeBoard/MetricSpecPanel';
import {
  anomalyLabelKey,
  findMetricSpec,
  groupSpecsByCategory,
  unitLabelKey,
} from '../RealtimeBoard/boardUtils';
import type { Anomaly, MetricSpec, RealtimeCategory, RealtimeQuote } from '@/types/virtualCityRealtime';
import zh from '@/i18n/locales/virtualCityRealtime-zh';
import en from '@/i18n/locales/virtualCityRealtime-en';
import ja from '@/i18n/locales/virtualCityRealtime-ja';

function ok(cond: boolean, msg: string): void {
  if (!cond) throw new Error(`[virtualCityRealtimeMetric] ${msg}`);
}

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(
      `[virtualCityRealtimeMetric] ${msg}: expected ${String(expected)}, got ${String(actual)}`,
    );
  }
}

// zustand SSR 快照陷阱（见 cityLabelsDom.test.tsx 夹具注释）：夹具原地改字段。
useI18nStore.getState().lang = 'zh-CN';

const SPECS: MetricSpec[] = [
  {
    id: 'a_share.index.sh',
    category: 'a_share',
    indicator: '上证指数',
    unit: 'point',
    caliber: '沪市全部 A 股的加权综合指数，反映沪深两市整体走势',
    formula: 'change_pct = (price - prev_close) / prev_close * 100',
    source: '腾讯财经',
    source_url: 'https://qt.gtimg.cn/q=sh000001',
    refresh: '交易时段 600s / 休市 7200s',
  },
  {
    id: 'crypto.btc',
    category: 'crypto',
    indicator: 'BTC',
    unit: 'usd',
    caliber: '比特币兑美元最新成交价',
    formula: 'price = 最新成交价',
    source: '腾讯财经',
    source_url: 'https://qt.gtimg.cn/q=btcusdt',
    refresh: '交易时段 600s / 休市 7200s',
  },
];

/** 确定性「当前时刻」—— 供异常行相对时间渲染（D3）。 */
const NOW_SEC = 1767225600;

const ANOMALIES: Anomaly[] = [
  {
    category: 'a_share',
    symbol: 'sh000001',
    name: '上证指数',
    kind: 'high',
    severity: 2,
    value: 4.25,
    detail: '涨幅 4.25% > 阈值 3.0%(类目 a_share)',
    observed_at: 1767225600,
  },
];

function makeQuote(over: Partial<RealtimeQuote> = {}): RealtimeQuote {
  return {
    symbol: 'sh000001',
    name: '上证指数',
    price: 3245.67,
    change_pct: 0.82,
    updated_at: 1767225600,
    source: '腾讯财经',
    unit: 'point',
    source_url: 'https://qt.gtimg.cn/q=sh000001',
    confidence: 0.94,
    ...over,
  };
}

function makeCategory(quotes: RealtimeQuote[]): RealtimeCategory {
  return {
    updated_at: 1767225600,
    source: '腾讯财经',
    quotes,
    history: {},
  };
}

export function runVirtualCityRealtimeMetricTests(): void {
  // ── ① 纯函数：单位 / 异常 kind → i18n 键 ───────────────────────────
  eq(unitLabelKey('point'), 'virtualCity.rt.unit.point', 'point 单位键');
  eq(unitLabelKey('usd_per_oz'), 'virtualCity.rt.unit.usd_per_oz', 'usd_per_oz 单位键');
  eq(unitLabelKey('unknown_unit'), null, '未知单位返回 null（回退原始 code）');
  eq(unitLabelKey(''), null, '空单位返回 null');
  eq(anomalyLabelKey('stale'), 'virtualCity.rt.anomaly.stale', 'stale 异常键');
  eq(anomalyLabelKey('nope'), null, '未知 kind 返回 null');

  // ── ② 纯函数：口径卡匹配（同类目 + 指标名精确优先）──────────────────
  const q = makeQuote();
  const hit = findMetricSpec(SPECS, 'a_share', q);
  ok(hit !== null, '上证指数应命中口径卡');
  eq(hit?.id, 'a_share.index.sh', '命中口径卡 id');
  eq(findMetricSpec(SPECS, 'crypto', q), null, '跨类目不命中（不猜口径）');
  eq(findMetricSpec(null, 'a_share', q), null, 'null 目录返回 null');
  eq(findMetricSpec([], 'a_share', q), null, '空目录返回 null');
  eq(
    findMetricSpec(SPECS, 'crypto', makeQuote({ symbol: 'btcusdt', name: 'BTC' }))?.id,
    'crypto.btc',
    'BTC 应命中 crypto 口径卡',
  );
  // 包含匹配：多命中时取 **indicator 最短**（更具体）的那张卡。
  const overlapping: MetricSpec[] = [
    { ...SPECS[0], id: 'a_share.index.sh', indicator: '上证综合指数' },
    { ...SPECS[0], id: 'a_share.index.sh2', indicator: '上证指数' },
  ];
  eq(
    findMetricSpec(overlapping, 'a_share', makeQuote({ name: '上证指数' }))?.id,
    'a_share.index.sh2',
    '包含匹配多命中时取最短 indicator',
  );
  // 站点代码与 indicator 差异大 ⇒ 走包含匹配；无对应卡 ⇒ 不命中（降级，不编造）。
  eq(
    findMetricSpec(SPECS, 'a_share', makeQuote({ symbol: 'sh000300', name: '腾讯沪深300' })),
    null,
    '目录无对应卡时不命中（不编造口径）',
  );
  const withHs300: MetricSpec[] = [
    ...SPECS,
    {
      id: 'a_share.index.csi300',
      category: 'a_share',
      indicator: '沪深300',
      unit: 'point',
      caliber: '沪深两市规模大流动性好的 300 只股票组成的指数',
      formula: 'change_pct = (price - prev_close) / prev_close * 100',
      source: '腾讯财经',
      source_url: 'https://qt.gtimg.cn/q=sh000300',
      refresh: '交易时段 600s / 休市 7200s',
    },
  ];
  eq(
    findMetricSpec(withHs300, 'a_share', makeQuote({ symbol: 'sh000300', name: '腾讯沪深300' }))?.id,
    'a_share.index.csi300',
    '「腾讯沪深300」应命中沪深300 口径卡（包含匹配）',
  );

  // ── ② spec_id 精确命中优先于 indicator 模糊匹配 ──────────────────
  // 后端 `DeriveQuoteQuality` 命中口径卡时下发 `spec_id`（json:"spec_id,omitempty"）。
  // 场景：站点名「沪指」与指标名「上证指数」不一致 —— 靠 indicator 匹配会漏配，
  //       靠 spec_id 则精确命中（两端口径一致）。
  const huIndex = findMetricSpec(
    SPECS,
    'a_share',
    makeQuote({ symbol: 'sh000001', name: '沪指', spec_id: 'a_share.index.sh' }),
  );
  eq(huIndex?.id, 'a_share.index.sh', 'spec_id 精确命中（站点名「沪指」）');
  eq(
    huIndex?.indicator,
    '上证指数',
    'spec_id 命中后展示的是该卡的指标名',
  );
  // spec_id 优先级最高：即使 indicator 也能模糊命中另一张卡，仍以 spec_id 为准。
  const specIdWins = findMetricSpec(
    withHs300,
    'a_share',
    makeQuote({ name: '沪深300', spec_id: 'a_share.index.sh' }),
  );
  eq(
    specIdWins?.id,
    'a_share.index.sh',
    'spec_id 优先于 indicator 模糊匹配',
  );

  // spec_id 为空 ⇒ 走既有兜底链（indicator 精确 → id/symbol → 包含匹配）。
  eq(
    findMetricSpec(SPECS, 'a_share', makeQuote({ name: '上证指数', spec_id: '' }))?.id,
    'a_share.index.sh',
    'spec_id 为空串时走 indicator 精确匹配兜底',
  );
  eq(
    findMetricSpec(SPECS, 'a_share', makeQuote({ name: '上证指数' }))?.id,
    'a_share.index.sh',
    'spec_id 缺省时走 indicator 精确匹配兜底',
  );
  eq(
    findMetricSpec(SPECS, 'a_share', makeQuote({ symbol: 'sh000001', name: '沪指' })),
    null,
    'spec_id 缺省且 indicator/包含都匹配不上 ⇒ 不命中（降级，不编造）',
  );
  // spec_id 指向的卡不在同类目目录内 ⇒ 不跨类目硬取，继续兜底。
  eq(
    findMetricSpec(SPECS, 'crypto', makeQuote({ name: 'BTC', spec_id: 'a_share.index.sh' }))?.id,
    'crypto.btc',
    'spec_id 跨类目时不硬取，走同类目兜底链',
  );

  // ── ③ 纯函数：按类目分组（保持首次出现顺序）────────────────────────
  const groups = groupSpecsByCategory(SPECS);
  eq(groups.length, 2, '两个类目分组');
  eq(groups[0].category, 'a_share', '第一组 a_share');
  eq(groups[1].category, 'crypto', '第二组 crypto');
  eq(groups[0].items.length, 1, 'a_share 组 1 条');
  eq(groupSpecsByCategory(null).length, 0, 'null 目录 → 0 组');

  // ── ④ 组件渲染：异常徽章（图标 + 文案双通道，§26）──────────────────
  const badgeHtml = renderToStaticMarkup(<AnomalyBadge kind="stale" />);
  ok(badgeHtml.includes('virtualCity-rt__anom--stale'), '异常徽章 class 必须含 kind');
  ok(badgeHtml.includes('virtualCity-rt__anom-icon'), '异常徽章必须带图标（不只靠颜色）');
  ok(badgeHtml.includes('数据陈旧'), '异常徽章必须带中文文案');

  // ── ⑤ 组件渲染：置信度（细条 + 百分比数字）────────────────────────
  const confHtml = renderToStaticMarkup(<ConfidenceBar value={0.94} />);
  ok(confHtml.includes('virtualCity-rt__conf-bar'), '置信度必须有细条');
  ok(confHtml.includes('virtualCity-rt__conf-text'), '置信度必须有百分比数字');
  ok(confHtml.includes('94%'), '置信度百分比文案');
  const confBad = renderToStaticMarkup(<ConfidenceBar value={Number.NaN} />);
  ok(confBad.includes('—'), '非有限置信度渲染占位符，不显示 NaN');

  // ── ⑥ 组件渲染：报价行（单位 / 异常 / 置信度）────────────────────
  const rowHtml = renderToStaticMarkup(
    <QuoteTable
      category={makeCategory([makeQuote({ anomaly: 'jump' })])}
      categoryKey="a_share"
      metricSpecs={SPECS}
    />,
  );
  ok(rowHtml.includes('virtualCity-rt__qunit'), '报价行必须显示单位小字');
  ok(rowHtml.includes('点位'), '单位小字必须是 i18n 文案（point → 点位）');
  ok(rowHtml.includes('virtualCity-rt__anom--jump'), '报价行必须显示异常徽章');
  ok(rowHtml.includes('价格跳变'), '报价行异常徽章文案');
  ok(rowHtml.includes('virtualCity-rt__conf-text'), '报价行必须显示置信度');

  // 无异常 ⇒ 不渲染徽章（不产出噪声节点）。
  const noAnomHtml = renderToStaticMarkup(
    <QuoteTable
      category={makeCategory([makeQuote()])}
      categoryKey="a_share"
      metricSpecs={SPECS}
    />,
  );
  ok(!noAnomHtml.includes('virtualCity-rt__anom'), '无异常时不渲染异常徽章');
  ok(noAnomHtml.includes('virtualCity-rt__qunit'), '无异常时单位仍显示');

  // ── ⑦ 组件渲染：口径卡（命中 ⇒ 定义 / 计算式 / 来源 / 刷新 / 链接）──
  const cardHtml = renderToStaticMarkup(
    <CaliberCard quote={q} spec={hit} categoryKey="a_share" />,
  );
  ok(cardHtml.includes('virtualCity-rt__caliber'), '口径卡节点必须渲染');
  ok(
    cardHtml.includes('沪市全部 A 股的加权综合指数'),
    '口径卡必须显示指标定义（caliber）',
  );
  ok(cardHtml.includes('change_pct = (price'), '口径卡必须显示计算式（formula）');
  ok(cardHtml.includes('交易时段 600s / 休市 7200s'), '口径卡必须显示刷新档位（refresh）');
  ok(cardHtml.includes('href="https://qt.gtimg.cn/q=sh000001"'), '口径卡原文链接必须可点击');
  ok(cardHtml.includes('rel="noreferrer"'), '外链必须带 rel=noreferrer');
  ok(cardHtml.includes('target="_blank"'), '外链必须 target=_blank');
  ok(!cardHtml.includes('暂无口径卡'), '命中口径卡时不得显示「暂无口径卡」');

  // 未命中 ⇒ 显式降级文案 + 报价自带来源/链接（**不编造**口径）。
  const missHtml = renderToStaticMarkup(
    <CaliberCard quote={q} spec={null} categoryKey="a_share" />,
  );
  ok(missHtml.includes('暂无口径卡'), '未命中口径卡必须显示降级文案');
  ok(missHtml.includes('腾讯财经'), '未命中时仍显示报价自带来源');
  ok(!missHtml.includes('沪市全部 A 股'), '未命中时**不得**显示其它指标的口径');

  // ── ⑧ 组件渲染：「口径与异常」面板 ───────────────────────────────
  const panelHtml = renderToStaticMarkup(
    <MetricSpecPanel metricSpecs={SPECS} anomalies={ANOMALIES} nowSec={NOW_SEC} />,
  );
  ok(panelHtml.includes('virtualCity-rt__specpanel'), '面板节点必须渲染');
  ok(panelHtml.includes('上证指数'), '面板必须列出口径卡指标');
  ok(panelHtml.includes('virtualCity-rt__specgroup'), '面板必须按类目分组');
  ok(panelHtml.includes('virtualCity-rt__anomrow'), '面板必须列出异常告警行');
  ok(panelHtml.includes('涨幅 4.25%'), '异常行必须显示判定依据（detail）');
  ok(panelHtml.includes('virtualCity-rt__sev--2'), '异常行必须显示严重度徽章');
  ok(panelHtml.includes('L2 中等'), '严重度徽章必须有文案');

  // 空态（各自空态，不是空白）。
  const emptyHtml = renderToStaticMarkup(
    <MetricSpecPanel metricSpecs={[]} anomalies={[]} nowSec={NOW_SEC} />,
  );
  ok(emptyHtml.includes('暂无口径卡'), '无口径卡必须显示空态');
  ok(emptyHtml.includes('当前无异常告警'), '无异常必须显示空态');

  // ⚠ `value` 单位按 kind 分：high/low/jump = 百分比；stale = **陈旧秒数**
  //   （后端 Anomaly.Value 口径 + 主 Agent 收口裁定）。
  const staleHtml = renderToStaticMarkup(
    <MetricSpecPanel
      metricSpecs={SPECS}
      nowSec={NOW_SEC}
      anomalies={[
        {
          category: 'a_share',
          symbol: 'sh000001',
          name: '上证指数',
          kind: 'stale',
          severity: 2,
          value: 14400,
          detail: '数据陈旧 14400s > 陈旧阈值 12000s',
          observed_at: 1767225600,
        },
      ]}
    />,
  );
  ok(staleHtml.includes('14400 秒'), 'stale 触发值必须按「秒」渲染');
  ok(!staleHtml.includes('14400.00%'), 'stale 触发值不得按百分比渲染');

  // high/low/jump ⇒ 幅度百分比（必须带 % 后缀）。
  const pctHtml = renderToStaticMarkup(
    <MetricSpecPanel
      metricSpecs={SPECS}
      nowSec={NOW_SEC}
      anomalies={[
        {
          category: 'a_share',
          symbol: 'sh000001',
          name: '上证指数',
          kind: 'high',
          severity: 2,
          value: 4.25,
          detail: '涨幅 4.25% > 阈值 3.0%(类目 a_share)',
          observed_at: 1767225600,
        },
        {
          category: 'crypto',
          symbol: 'btcusdt',
          name: 'BTC',
          kind: 'jump',
          severity: 3,
          value: 12.5,
          detail: '价格跳变 12.50% > 跳变阈值 10.00%(2×5.00%,类目 crypto)',
          observed_at: 1767225600,
        },
      ]}
    />,
  );
  ok(pctHtml.includes('4.25%'), 'high 触发值必须按百分比渲染');
  ok(pctHtml.includes('12.50%'), 'jump 触发值必须按百分比渲染');
  ok(!pctHtml.includes('4.25 秒'), '非 stale 不得按秒渲染');

  // ── D4：high / low 的「触发值」渲染**绝对值** ─────────────────────
  // 后端 low 的 `value` 是**带符号**的 change_pct（负数，如 -3.96），
  // 而 `detail` 是绝对幅度文案（「跌幅 3.96% > 阈值 3.00%」）
  // ⇒ 同屏出现「-3.96%」与「3.96%」像两个数 ⇒ 渲染绝对值对齐 detail。
  const lowHtml = renderToStaticMarkup(
    <MetricSpecPanel
      metricSpecs={SPECS}
      nowSec={NOW_SEC}
      anomalies={[
        {
          category: 'a_share',
          symbol: 'sh000001',
          name: '上证指数',
          kind: 'low',
          severity: 2,
          value: -3.96,
          detail: '跌幅 3.96% > 阈值 3.00%(类目 a_share)',
          observed_at: NOW_SEC - 120,
        },
      ]}
    />,
  );
  ok(lowHtml.includes('3.96%'), 'low 触发值必须渲染绝对值');
  ok(!lowHtml.includes('-3.96%'), 'low 触发值不得渲染带符号的负值');
  ok(lowHtml.includes('跌幅 3.96%'), 'detail 文案照原样渲染');

  // ── D3：观测时间渲染**相对时间**，不显示裸 Unix 时间戳 ────────────
  ok(
    !lowHtml.includes(String(NOW_SEC - 120)),
    '观测时间不得渲染裸 Unix 时间戳',
  );
  ok(lowHtml.includes('2 分钟前更新'), '观测时间必须渲染相对时间（与看板头部同口径）');
  // 复用既有 `virtualCity.rt.updated.*` 键，不新增键 ⇒ 断言用的是既有文案。
  ok(
    lowHtml.includes('观测时间'),
    '观测时间标签文案须保留',
  );
  // 刚观测到 ⇒ 「刚刚更新」（reuse 既有 updated.just 键）。
  const justHtml = renderToStaticMarkup(
    <MetricSpecPanel
      metricSpecs={SPECS}
      nowSec={NOW_SEC}
      anomalies={[
        {
          category: 'a_share',
          symbol: 'sh000001',
          name: '上证指数',
          kind: 'high',
          severity: 1,
          value: 3.5,
          detail: '涨幅 3.50% > 阈值 3.00%(类目 a_share)',
          observed_at: NOW_SEC,
        },
      ]}
    />,
  );
  ok(justHtml.includes('刚刚更新'), '刚观测到的异常须显示「刚刚更新」');

  // 未回读（null）也走空态，不崩。
  const nullHtml = renderToStaticMarkup(
    <MetricSpecPanel metricSpecs={null} anomalies={null} nowSec={NOW_SEC} />,
  );
  ok(nullHtml.includes('virtualCity-rt__specpanel'), 'null 快照也必须渲染面板');
  ok(!nullHtml.includes('undefined'), 'null 快照不得渲染 undefined 字样');

  // ── ⑨ i18n 三语键集合一致性（缺 key 即白屏，§29.8）────────────────
  const zhKeys = Object.keys(zh as Record<string, unknown>).sort();
  const enKeys = Object.keys(en as Record<string, unknown>).sort();
  const jaKeys = Object.keys(ja as Record<string, unknown>).sort();
  const missingEn = zhKeys.filter((k) => !enKeys.includes(k));
  const extraEn = enKeys.filter((k) => !zhKeys.includes(k));
  const missingJa = zhKeys.filter((k) => !jaKeys.includes(k));
  const extraJa = jaKeys.filter((k) => !zhKeys.includes(k));
  eq(missingEn.length, 0, `en 缺少键：${missingEn.join(',')}`);
  eq(extraEn.length, 0, `en 多余键：${extraEn.join(',')}`);
  eq(missingJa.length, 0, `ja 缺少键：${missingJa.join(',')}`);
  eq(extraJa.length, 0, `ja 多余键：${extraJa.join(',')}`);
  for (const [lang, dict] of [['zh', zh], ['en', en], ['ja', ja]] as const) {
    for (const [k, v] of Object.entries(dict as Record<string, unknown>)) {
      ok(typeof v === 'string', `${lang}.${k} 必须是字符串`);
      ok((v as string).length > 0, `${lang}.${k} 不得为空串`);
    }
  }

  // eslint-disable-next-line no-console
  console.log(
    `[virtualCityRealtimeMetric] 全部断言通过（三语键 ${zhKeys.length} 个）`,
  );
}

// 直接执行（被打包器当作入口 import）时自动运行。
runVirtualCityRealtimeMetricTests();
