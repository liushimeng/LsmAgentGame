/**
 * MarketPanel — 行情面板：周期徽章（四色，白字对比度 ≥5:1）+ LPR/CPI +
 * 股指/金价/债券年化（数字 + 相对上月箭头 + 迷你走势）+ 8 区房价指数横条
 * （beta 标注，点击城区联动地图聚焦）。
 *
 * 数据源：game.state.cycle / market + store.marketHistory（每月一条快照）。
 */

import { useMemo } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  WEALTH_DISTRICTS,
  formatCny,
  formatPct,
  type WealthCentralBank,
  type WealthCyclePhase,
  type WealthDistrictId,
  type WealthGameState,
} from '@/types/wealth';
import type { WealthMarketPoint } from '@/store/wealth.store';

/** 迷你走势 SVG 折线（最近 N 点，归一化高度）。 */
function Sparkline({ points, color }: { points: number[]; color: string }) {
  if (points.length < 2) return <svg className="wealth-spark" viewBox="0 0 60 18" width="60" height="18" />;
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
    <svg className="wealth-spark" viewBox="0 0 60 18" width="60" height="18" aria-hidden="true">
      <path d={path} fill="none" stroke={color} strokeWidth="1.5" />
    </svg>
  );
}

interface QuoteProps {
  label: string;
  value: string;
  prev?: number;
  curr: number;
  history: number[];
  /** 数值越大越好（箭头方向语义）。 */
  color?: string;
}

function QuoteRow({ label, value, prev, curr, history, color = '#e5e7eb' }: QuoteProps) {
  const delta = prev !== undefined ? curr - prev : undefined;
  return (
    <div className="wealth-quote">
      <span className="wealth-quote__label">{label}</span>
      <span className="wealth-quote__value">{value}</span>
      {delta !== undefined && (
        <span className={delta >= 0 ? 'wealth-quote__delta wealth-num--pos' : 'wealth-quote__delta wealth-num--neg'}>
          {delta >= 0 ? '▲' : '▼'}
          {(Math.abs(delta) / (prev || 1) * 100).toFixed(1)}%
        </span>
      )}
      <Sparkline points={history} color={color} />
    </div>
  );
}

interface Props {
  gameState: WealthGameState | null;
  marketHistory: WealthMarketPoint[];
  onSelectDistrict: (id: WealthDistrictId) => void;
}

const CYCLE_CLASS: Record<WealthCyclePhase, string> = {
  recovery: 'wealth-cycle--recovery',
  boom: 'wealth-cycle--boom',
  recession: 'wealth-cycle--recession',
  depression: 'wealth-cycle--depression',
};

export function MarketPanel({ gameState, marketHistory, onSelectDistrict }: Props) {
  const t = useT();
  if (!gameState) {
    return (
      <div className="wealth-panel__empty">
        <p>{t('wealth.panel.waiting' as TKey)}</p>
      </div>
    );
  }
  const { cycle, market } = gameState;
  const prev = marketHistory.length >= 2 ? marketHistory[marketHistory.length - 2] : undefined;
  const stocks = marketHistory.map((h) => h.stock);
  const golds = marketHistory.map((h) => h.gold);
  const bonds = marketHistory.map((h) => h.bond);
  const maxIdx = Math.max(...market.districts.map((d) => d.price_index), 1.001);

  const houseBars = useMemo(
    () =>
      market.districts.map((d) => {
        const def = WEALTH_DISTRICTS.find((x) => x.id === d.id);
        return { d, def, pct: Math.round((d.price_index / maxIdx) * 100) };
      }),
    [market, maxIdx],
  );

  return (
    <div className="wealth-marketpanel">
      <div className="wealth-marketpanel__head">
        <span className={`wealth-badge wealth-cycle ${CYCLE_CLASS[cycle.phase]}`}>
          {t(`wealth.cycle.${cycle.phase}` as TKey)}
        </span>
        <span className="wealth-marketpanel__months">
          {t('wealth.market.cycleLeft' as TKey, { n: cycle.months_left })}
        </span>
      </div>
      <div className="wealth-marketpanel__rates">
        <span className="wealth-badge">{t('wealth.lpr' as TKey)} {formatPct(cycle.lpr)}</span>
        <span className="wealth-badge">{t('wealth.cpi' as TKey)} {formatPct(cycle.cpi)}</span>
      </div>

      <QuoteRow
        label={t('wealth.stockIndex' as TKey)}
        value={market.stock_index.toFixed(2)}
        curr={market.stock_index}
        prev={prev?.stock}
        history={stocks}
        color="#60a5fa"
      />
      <QuoteRow
        label={t('wealth.goldPrice' as TKey)}
        value={market.gold_price.toFixed(0)}
        curr={market.gold_price}
        prev={prev?.gold}
        history={golds}
        color="#fbbf24"
      />
      <QuoteRow
        label={t('wealth.bondYield' as TKey)}
        value={formatPct(market.bond_yield)}
        curr={market.bond_yield}
        prev={prev?.bond}
        history={bonds}
        color="#34d399"
      />

      <div className="wealth-marketpanel__districts">
        <div className="wealth-marketpanel__districts-title">
          {t('wealth.housePrice' as TKey)}
        </div>
        {houseBars.map(({ d, def, pct }) => (
          <button
            key={d.id}
            type="button"
            className="wealth-hp-bar"
            onClick={() => def && onSelectDistrict(def.id)}
            title={def ? `${def.nameZh} · β${def.houseBeta.toFixed(2)}` : d.id}
          >
            <span className="wealth-hp-bar__label">{t(`wealth.district.${d.id}` as TKey)}</span>
            <span className="wealth-hp-bar__track">
              <span
                className="wealth-hp-bar__fill"
                style={{ width: `${pct}%`, background: def?.color ?? '#9ca3af' }}
              />
            </span>
            <span className="wealth-hp-bar__value">
              {d.price_index.toFixed(2)}
              {def ? ` ·β${def.houseBeta.toFixed(2)}` : ''}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
