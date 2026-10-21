/**
 * QuoteTable — 批次 65 实时看板报价表。
 *
 * 行 = 名称 / 价格（tabular-nums）/ 涨跌徽章（A 股惯例涨红跌绿，沿
 * .virtualCity-num--up/down 色板）/ 迷你 sparkline；行点击展开该行 symbol 的
 * CandleChart（服务端自采价点环 → 5min OHLC）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import type { RealtimeCategory, RealtimeQuote } from '@/types/virtualCityRealtime';
import { CandleChart } from './CandleChart';
import { pctText } from './boardUtils';

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

function QuoteRow({
  quote,
  history,
}: {
  quote: RealtimeQuote;
  history: { t: number; p: number }[];
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  const sparkPoints = useMemo(() => history.map((h) => h.p), [history]);
  const sparkColor = quote.change_pct >= 0 ? '#f87171' : '#4ade80';
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
          {quote.name}
          {quote.currency ? <small className="virtualCity-rt__qcur">{quote.currency}</small> : null}
        </span>
        <span className="virtualCity-rt__qprice">{quote.price.toLocaleString()}</span>
        <span className={chgClass(quote.change_pct)}>
          {quote.change_pct > 0 ? '+' : ''}{pctText(quote.change_pct, 2)}
        </span>
        <span className="virtualCity-rt__qspark">
          <Sparkline points={sparkPoints} color={sparkColor} />
        </span>
      </button>
      {expanded && (
        <div className="virtualCity-rt__qdetail">
          <CandleChart points={history} />
        </div>
      )}
    </>
  );
}

export function QuoteTable({ category }: { category: RealtimeCategory }) {
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
        <QuoteRow key={q.symbol} quote={q} history={category.history[q.symbol] ?? []} />
      ))}
    </div>
  );
}

export default QuoteTable;
