/**
 * QuoteTable — 批次 65 实时看板报价表（批次 89 增「单位 / 异常徽章 / 置信度 / 口径卡」）。
 *
 * 行 = 名称 / 价格（tabular-nums）/ 涨跌徽章（A 股惯例涨红跌绿，沿
 * .virtualCity-num--up/down 色板）/ 迷你 sparkline；行点击展开该行 symbol 的
 * CandleChart（服务端自采价点环 → 5min OHLC）。
 *
 * 批次 89（真实数据口径卡与异常告警层）：
 * - 名称后追加**单位小字**（`unit` → i18n，未知单位回退原始 code，不丢信息）。
 * - 涨跌幅后追加**异常徽章**（high/low/stale/jump 四态）—— 徽章**必带文案 + 图标**
 *   （§26 五大反模式：不得只靠颜色传达信息）。
 * - 价格下追加**置信度**：细条 + 百分比小字（同样不只靠颜色）。
 * - 展开区在 CandleChart 之外追加**口径卡**：指标定义 / 计算式 / 来源 / 刷新档位 /
 *   原文链接（source_url 可点击，target=_blank rel=noreferrer）。
 *   口径卡匹配不到时显示「该指标暂无口径卡」+ 报价自带 source/source_url，
 *   **绝不编造口径**（后端口径单一事实来源，设计文档 §8）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type {
  MetricSpec,
  RealtimeCategory,
  RealtimeQuote,
  RtAnomalyKind,
} from '@/types/virtualCityRealtime';
import { CandleChart } from './CandleChart';
import {
  anomalyLabelKey,
  findMetricSpec,
  pctText,
  unitLabelKey,
} from './boardUtils';

/** 迷你走势 SVG 折线（MarketPanel.Sparkline 同款范式：60×18 viewBox）。 */
function Sparkline({ points, color }: { points: number[]; color: string }) {
  if (points.length < 2) {
    return <svg className="virtualCity-spark" viewBox="0 0 60 18" width="60" height="18" aria-hidden="true" />;
  }
  const min = Math.min(...points);
  const max = Math.max(...points);
  const span = max - min || 1;
  const path = points
    .map((v, i) => {
      const x = (i / (points.length - 1)) * 58 + 1;
      const y = 17 - ((v - min) / span) * 15;
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(' ');
  return (
    <svg className="virtualCity-spark" viewBox="0 0 60 18" width="60" height="18" aria-hidden="true">
      <path d={path} fill="none" stroke={color} strokeWidth="1.5" />
    </svg>
  );
}

/** 涨跌徽章 class（涨红跌绿）。 */
function chgClass(pct: number): string {
  if (pct > 0) return 'virtualCity-rt__chg virtualCity-rt__chg--up';
  if (pct < 0) return 'virtualCity-rt__chg virtualCity-rt__chg--down';
  return 'virtualCity-rt__chg';
}

/** 异常徽章 class（high 涨异常 / low 跌异常 / stale 陈旧 / jump 跳变）。 */
function anomalyClass(kind: string): string {
  return `virtualCity-rt__anom virtualCity-rt__anom--${kind}`;
}

/** 异常徽章图标（图标 + 文案双通道，禁只靠颜色）。 */
const ANOMALY_ICON: Record<string, string> = {
  high: '▲',
  low: '▼',
  stale: '⏳',
  jump: '⚡',
};

/**
 * 异常徽章（无异常 ⇒ 整块不渲染）。
 *
 * 文案走 i18n；未知 kind 回退展示原始 kind 字符串（不丢信息、不白屏）。
 */
export function AnomalyBadge({ kind }: { kind: RtAnomalyKind | string }) {
  const t = useT();
  const key = anomalyLabelKey(kind);
  const label = key ? t(key as TKey) : kind;
  return (
    <span className={anomalyClass(kind)} title={label}>
      <span className="virtualCity-rt__anom-icon" aria-hidden="true">
        {ANOMALY_ICON[kind] ?? '⚠'}
      </span>
      <span className="virtualCity-rt__anom-text">{label}</span>
    </span>
  );
}

/**
 * 置信度：细条 + 百分比小字。
 *
 * ⚠ 后端 confidence 恒给值（缺省 0）；前端**不复制阈值口径**，只渲染后端给的数。
 * 数值非有限 ⇒ 渲染占位符，不显示 NaN。
 */
export function ConfidenceBar({ value }: { value: number }) {
  const t = useT();
  const ok = typeof value === 'number' && Number.isFinite(value);
  const pct = ok ? Math.max(0, Math.min(1, value)) : 0;
  const text = ok ? `${Math.round(value * 100)}%` : '—';
  return (
    <span
      className="virtualCity-rt__conf"
      title={`${t('virtualCity.rt.confidence' as TKey)} ${text}`}
      data-testid="virtualCity-rt-conf"
    >
      <span className="virtualCity-rt__conf-bar" aria-hidden="true">
        <span className="virtualCity-rt__conf-fill" style={{ width: `${pct * 100}%` }} />
      </span>
      <span className="virtualCity-rt__conf-text">{text}</span>
    </span>
  );
}

/**
 * 口径卡（展开区）。
 *
 * 字段来源：命中口径卡 ⇒ `MetricSpec`；未命中 ⇒ 回退报价自带的 unit / source /
 * source_url，并显式标注「暂无口径卡」（不编造定义与计算式）。
 */
export function CaliberCard({
  quote,
  spec,
  categoryKey,
}: {
  quote: RealtimeQuote;
  spec: MetricSpec | null;
  categoryKey: string;
}) {
  const t = useT();
  const unitKey = unitLabelKey(quote.unit);
  const unitText = unitKey ? t(unitKey as TKey) : quote.unit || '—';
  const source = spec?.source || quote.source || '—';
  const link = spec?.source_url || quote.source_url || '';
  return (
    <div className="virtualCity-rt__caliber" data-testid="virtualCity-rt-caliber">
      <div className="virtualCity-rt__caliber-title">
        {t('virtualCity.rt.caliber.title' as TKey)}
      </div>
      {!spec && (
        <div className="virtualCity-rt__caliber-none">
          {t('virtualCity.rt.caliber.none' as TKey)}
          {/* ⚠ D5（活体验收结论，**无需改动**）：下方「原文链接」显示 `—` 是
              **后端刻意留空**（采集层本就不带 per-quote URL，后端不编造链接），
              **非渲染缺陷** ⇒ 勿改文案、勿伪造链接。 */}
        </div>
      )}
      <dl className="virtualCity-rt__caliber-list">
        <dt>{t('virtualCity.rt.caliber.definition' as TKey)}</dt>
        <dd>{spec?.caliber || '—'}</dd>
        <dt>{t('virtualCity.rt.caliber.formula' as TKey)}</dt>
        <dd>
          {spec?.formula ? (
            <code className="virtualCity-rt__caliber-code">{spec.formula}</code>
          ) : (
            '—'
          )}
        </dd>
        <dt>{t('virtualCity.rt.caliber.source' as TKey)}</dt>
        <dd>{source}</dd>
        <dt>{t('virtualCity.rt.caliber.refresh' as TKey)}</dt>
        <dd>{spec?.refresh || '—'}</dd>
        <dt>{t('virtualCity.rt.caliber.unit' as TKey)}</dt>
        <dd>{unitText}</dd>
        <dt>{t('virtualCity.rt.caliber.link' as TKey)}</dt>
        <dd>
          {link ? (
            <a
              className="virtualCity-rt__caliber-link"
              href={link}
              target="_blank"
              rel="noreferrer"
              title={link}
            >
              {t('virtualCity.rt.caliber.linkOpen' as TKey)}
            </a>
          ) : (
            '—'
          )}
        </dd>
      </dl>
      {spec && (
        <div className="virtualCity-rt__caliber-id" title={spec.id}>
          {t('virtualCity.rt.caliber.id' as TKey)}: {spec.id} · {categoryKey}
        </div>
      )}
    </div>
  );
}

function QuoteRow({
  quote,
  history,
  categoryKey,
  metricSpecs,
}: {
  quote: RealtimeQuote;
  history: { t: number; p: number }[];
  categoryKey: string;
  metricSpecs: MetricSpec[] | null;
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  const sparkPoints = useMemo(() => history.map((h) => h.p), [history]);
  const sparkColor = quote.change_pct >= 0 ? '#f87171' : '#4ade80';
  const unitKey = unitLabelKey(quote.unit);
  const unitText = unitKey ? t(unitKey as TKey) : quote.unit || '';
  const spec = useMemo(
    () => findMetricSpec(metricSpecs, categoryKey, quote),
    [metricSpecs, categoryKey, quote],
  );
  const anomaly = quote.anomaly ?? '';
  return (
    <>
      <button
        type="button"
        className={'virtualCity-rt__qrow' + (expanded ? ' virtualCity-rt__qrow--open' : '')}
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        title={`${t('virtualCity.rt.source' as TKey)}: ${quote.source}`}
      >
        <span className="virtualCity-rt__qname">
          <span className="virtualCity-rt__qname-text">{quote.name}</span>
          {quote.currency ? <small className="virtualCity-rt__qcur">{quote.currency}</small> : null}
          {unitText ? (
            <small className="virtualCity-rt__qunit" data-testid="virtualCity-rt-unit">
              {unitText}
            </small>
          ) : null}
        </span>
        <span className="virtualCity-rt__qprice">{quote.price.toLocaleString()}</span>
        <span className="virtualCity-rt__qchgcell">
          <span className={chgClass(quote.change_pct)}>
            {quote.change_pct > 0 ? '+' : ''}{pctText(quote.change_pct, 2)}
          </span>
        </span>
        <span className="virtualCity-rt__qspark">
          <Sparkline points={sparkPoints} color={sparkColor} />
        </span>
        {/* 批次 89 · 质量带（整行铺开）：单位 / 异常徽章 / 置信度。
            侧栏仅 340~380px，第 5 列并排会把「价格」挤没 ⇒ 走整行带
            （与批次 78 MissionLogPanel「站点尝试带」同范式）。 */}
        <span className="virtualCity-rt__qmeta">
          {anomaly ? <AnomalyBadge kind={anomaly} /> : null}
          <ConfidenceBar value={quote.confidence} />
        </span>
      </button>
      {expanded && (
        <div className="virtualCity-rt__qdetail">
          <CandleChart points={history} />
          <CaliberCard quote={quote} spec={spec} categoryKey={categoryKey} />
        </div>
      )}
    </>
  );
}

export function QuoteTable({
  category,
  categoryKey,
  metricSpecs,
}: {
  category: RealtimeCategory;
  /** 行情类目键（a_share / hk / …）—— 口径卡匹配用。 */
  categoryKey: string;
  /** 批次 89：口径卡目录（HTTP 快照回读，null = 快照未回读）。 */
  metricSpecs: MetricSpec[] | null;
}) {
  const t = useT();
  if (!category.quotes.length) {
    return (
      <div className="virtualCity-panel__empty virtualCity-rt__empty">
        <span className="virtualCity-rt__empty-icon" aria-hidden="true">📈</span>
        <p>{t('virtualCity.rt.empty.category' as TKey)}</p>
      </div>
    );
  }
  return (
    <div className="virtualCity-rt__quotes">
      <div className="virtualCity-rt__qrow virtualCity-rt__qrow--head">
        <span>{t('virtualCity.rt.col.name' as TKey)}</span>
        <span>{t('virtualCity.rt.col.price' as TKey)}</span>
        <span>{t('virtualCity.rt.col.change' as TKey)}</span>
        <span>{t('virtualCity.rt.col.trend' as TKey)}</span>
      </div>
      {category.quotes.map((q) => (
        <QuoteRow
          key={q.symbol}
          quote={q}
          history={category.history[q.symbol] ?? []}
          categoryKey={categoryKey}
          metricSpecs={metricSpecs}
        />
      ))}
    </div>
  );
}

export default QuoteTable;
