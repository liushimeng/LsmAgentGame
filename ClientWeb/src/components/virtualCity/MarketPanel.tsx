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
  VIRTUAL_CITY_DISTRICTS,
  formatCny,
  formatPct,
  virtualCityStockBreakerActive,
  type VirtualCityCentralBank,
  type VirtualCityCycle,
  type VirtualCityCyclePhase,
  type VirtualCityDistrictId,
  type VirtualCityGameState,
} from '@/types/virtualCity';
import type { VirtualCityMarketPoint } from '@/store/virtualCity.store';
import './virtualCity-batch20.css';

/** 价差徽章色阶（批次 20 文档 3 B4：过热窄、恐慌宽；阈值按四阶段 4/8/15/20bps 分档）。 */
function spreadTierClass(bps: number): string {
  if (bps <= 5) return 'virtualCity-micro__badge--narrow'; // 扩张 4bp — 绿（流动性好）
  if (bps <= 10) return 'virtualCity-micro__badge--mid';   // 衰退 8bp — 灰
  return 'virtualCity-micro__badge--wide';                 // 恐慌 15 / 过热 20bp — 红
}

/** 迷你走势 SVG 折线（最近 N 点，归一化高度）。 */
function Sparkline({ points, color }: { points: number[]; color: string }) {
  if (points.length < 2) return <svg className="virtualCity-spark" viewBox="0 0 60 18" width="60" height="18" />;
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
    <div className="virtualCity-quote">
      <span className="virtualCity-quote__label">{label}</span>
      <span className="virtualCity-quote__value">{value}</span>
      {delta !== undefined && (
        <span className={delta >= 0 ? 'virtualCity-quote__delta virtualCity-num--pos' : 'virtualCity-quote__delta virtualCity-num--neg'}>
          {delta >= 0 ? '▲' : '▼'}
          {(Math.abs(delta) / (prev || 1) * 100).toFixed(1)}%
        </span>
      )}
      <Sparkline points={history} color={color} />
    </div>
  );
}

interface Props {
  gameState: VirtualCityGameState | null;
  marketHistory: VirtualCityMarketPoint[];
  onSelectDistrict: (id: VirtualCityDistrictId) => void;
}

const CYCLE_CLASS: Record<VirtualCityCyclePhase, string> = {
  recovery: 'virtualCity-cycle--recovery',
  boom: 'virtualCity-cycle--boom',
  recession: 'virtualCity-cycle--recession',
  depression: 'virtualCity-cycle--depression',
};

/** 信贷约束状态：宽松 / 中性 / 收紧 / 惜贷（阈值来自设计文档 §6.5）。 */
function creditTightnessKey(v: number): TKey {
  if (v >= 1) return 'virtualCity.cb.tightness.cautious' as TKey;
  if (v > 0.7) return 'virtualCity.cb.tightness.tight' as TKey;
  if (v >= 0.3) return 'virtualCity.cb.tightness.neutral' as TKey;
  return 'virtualCity.cb.tightness.loose' as TKey;
}

/** 信贷约束状态色（暗色主题 ≥4.5:1）。 */
function creditTightnessColor(v: number): string {
  if (v >= 1) return '#f87171';   // 惜贷 — 红
  if (v > 0.7) return '#fb923c';  // 收紧 — 橙
  if (v >= 0.3) return '#fbbf24'; // 中性 — 琥珀
  return '#4ade80';               // 宽松 — 绿
}

/** 央行货币政策快照区块（M0/M1/M2 + 政策利率 + 信贷约束）。 */
function CentralBankSection({ cb, cycle }: { cb: VirtualCityCentralBank; cycle?: VirtualCityCycle }) {
  const t = useT();
  const tightnessColor = creditTightnessColor(cb.credit_tightness);
  return (
    <div className="virtualCity-cb">
      <div className="virtualCity-cb__head">
        <span className="virtualCity-cb__title">{t('virtualCity.cb.title' as TKey)}</span>
        <span
          className="virtualCity-badge"
          style={{ background: tightnessColor, color: '#0f172a' }}
        >
          {t(creditTightnessKey(cb.credit_tightness))}
        </span>
      </div>

      {/* 货币三层次：M0 / M1 / M2（万元） */}
      <div className="virtualCity-cb__row">
        <span className="virtualCity-cb__label">{t('virtualCity.cb.m0' as TKey)}</span>
        <span className="virtualCity-cb__value">¥{(cb.m0_cny / 1e4).toFixed(1)}万</span>
        <span className="virtualCity-cb__label">{t('virtualCity.cb.m1' as TKey)}</span>
        <span className="virtualCity-cb__value">¥{(cb.m1_cny / 1e4).toFixed(1)}万</span>
        <span className="virtualCity-cb__label">{t('virtualCity.cb.m2' as TKey)}</span>
        <span className="virtualCity-cb__value">¥{(cb.m2_cny / 1e4).toFixed(1)}万</span>
      </div>

      {/* 基础货币 + 货币乘数 */}
      <div className="virtualCity-cb__row">
        <span className="virtualCity-cb__label">{t('virtualCity.cb.mb' as TKey)}</span>
        <span className="virtualCity-cb__value">¥{(cb.mb_cny / 1e4).toFixed(1)}万</span>
        <span className="virtualCity-cb__label">{t('virtualCity.cb.multiplier' as TKey)}</span>
        <span className="virtualCity-cb__value">{cb.money_multiplier.toFixed(3)}</span>
      </div>

      {/* 政策利率 / LPR / 5Y LPR / CPI（百分数 2 位） */}
      <div className="virtualCity-cb__row">
        <span className="virtualCity-cb__label">{t('virtualCity.cb.policyRate' as TKey)}</span>
        <span className="virtualCity-cb__value">{formatPct(cb.policy_rate, 2)}</span>
        <span className="virtualCity-cb__label">{t('virtualCity.lpr' as TKey)}</span>
        <span className="virtualCity-cb__value">{formatPct(cb.lpr, 2)}</span>
        <span className="virtualCity-cb__label">{t('virtualCity.cpi' as TKey)}</span>
        <span className="virtualCity-cb__value">{formatPct(cb.cpi, 2)}</span>
      </div>

      {/* P1 5Y LPR 展示（如有） */}
      {cycle?.lpr5y !== undefined && (
        <div className="virtualCity-cb__row">
          <span className="virtualCity-cb__label">{t('lpr.5y' as TKey)}</span>
          <span className="virtualCity-cb__value virtualCity-cb__value--lpr5y">{formatPct(cycle.lpr5y, 2)}</span>
          <span className="virtualCity-cb__label">{t('lpr.5yNote' as TKey)}</span>
          <span className="virtualCity-cb__value">{t('lpr.5yHint' as TKey)}</span>
        </div>
      )}

      {/* 信贷约束 + 贷款额度乘数 */}
      <div className="virtualCity-cb__row">
        <span className="virtualCity-cb__label">{t('virtualCity.cb.creditTightness' as TKey)}</span>
        <span className="virtualCity-cb__value" style={{ color: tightnessColor }}>
          {formatPct(cb.credit_tightness, 0)}
        </span>
        <span className="virtualCity-cb__label">{t('virtualCity.cb.loanQuota' as TKey)}</span>
        <span className="virtualCity-cb__value">{formatPct(cb.loan_quota_factor, 0)}</span>
      </div>
    </div>
  );
}

export function MarketPanel({ gameState, marketHistory, onSelectDistrict }: Props) {
  const t = useT();
  if (!gameState) {
    return (
      <div className="virtualCity-panel__empty">
        <p>{t('virtualCity.panel.waiting' as TKey)}</p>
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
        const def = VIRTUAL_CITY_DISTRICTS.find((x) => x.id === d.id);
        return { d, def, pct: Math.round((d.price_index / maxIdx) * 100) };
      }),
    [market, maxIdx],
  );

  // ── 批次 20 文档 3 B4：股票微观结构（旧局字段缺省 → 对应 UI 整段不渲染）──
  const breakerActive = virtualCityStockBreakerActive(gameState);
  const spreadBps = market.spread_bps;
  const stockHeld = (gameState.my?.assets ?? []).find((a) => a.kind === 'stock_index');
  const t1Locked = gameState.my?.stock_t1_locked ?? 0;

  return (
    <div className="virtualCity-marketpanel">
      {/* 熔断期顶部红条（month ≤ breaker_until；买卖按钮禁用由 ActionPanel 弹窗承担） */}
      {breakerActive && (
        <div className="virtualCity-micro__breaker" role="alert" data-testid="virtualCity-breaker-banner">
          🛑 {t('virtualCity.micro.breaker' as TKey, { n: market.breaker_until ?? 0 })}
        </div>
      )}
      <div className="virtualCity-marketpanel__head">
        <span className={`virtualCity-badge virtualCity-cycle ${CYCLE_CLASS[cycle.phase]}`}>
          {t(`virtualCity.cycle.${cycle.phase}` as TKey)}
        </span>
        <span className="virtualCity-marketpanel__months">
          {t('virtualCity.market.cycleLeft' as TKey, { n: cycle.months_left })}
        </span>
      </div>
      <div className="virtualCity-marketpanel__rates">
        <span className="virtualCity-badge">{t('virtualCity.lpr' as TKey)} {formatPct(cycle.lpr)}</span>
        {cycle.lpr5y !== undefined && (
          <span className="virtualCity-badge virtualCity-badge--lpr5y">
            {t('lpr.5y' as TKey)} {formatPct(cycle.lpr5y)}
          </span>
        )}
        <span className="virtualCity-badge">{t('virtualCity.cpi' as TKey)} {formatPct(cycle.cpi)}</span>
      </div>

      <CentralBankSection cb={gameState.central_bank} cycle={cycle} />

      <QuoteRow
        label={t('virtualCity.stockIndex' as TKey)}
        value={market.stock_index.toFixed(2)}
        curr={market.stock_index}
        prev={prev?.stock}
        history={stocks}
        color="#60a5fa"
      />
      {/* 双边价 + 价差徽章（文档 3 B2-2：买价上浮 / 卖价下沉，价差按阶段） */}
      {(market.stock_buy_unit !== undefined || market.stock_sell_unit !== undefined) && (
        <div className="virtualCity-micro__prices" data-testid="virtualCity-micro-prices">
          <span>{t('virtualCity.micro.buyUnit' as TKey, { price: (market.stock_buy_unit ?? market.stock_index).toFixed(2) })}</span>
          <span>{t('virtualCity.micro.sellUnit' as TKey, { price: (market.stock_sell_unit ?? market.stock_index).toFixed(2) })}</span>
          {spreadBps !== undefined && spreadBps > 0 && (
            <span className={`virtualCity-micro__badge ${spreadTierClass(spreadBps)}`}>
              {t('virtualCity.micro.spread' as TKey, { bps: spreadBps })}
            </span>
          )}
        </div>
      )}
      {/* 持仓行：份数 + T+1 冻结角标（t1_locked 未下发时不渲染角标） */}
      {stockHeld && (
        <div className="virtualCity-micro__hold" data-testid="virtualCity-micro-hold">
          <span>
            {t('virtualCity.asset.stock' as TKey)} ×{stockHeld.units} · ¥{formatCny(stockHeld.value_cny)}
          </span>
          {t1Locked > 0 && (
            <span
              className="virtualCity-micro__badge virtualCity-micro__badge--t1"
              title={t('virtualCity.micro.t1Locked' as TKey, { n: t1Locked })}
            >
              {t('virtualCity.micro.t1Locked' as TKey, { n: t1Locked })}
            </span>
          )}
        </div>
      )}
      <QuoteRow
        label={t('virtualCity.goldPrice' as TKey)}
        value={market.gold_price.toFixed(0)}
        curr={market.gold_price}
        prev={prev?.gold}
        history={golds}
        color="#fbbf24"
      />
      <QuoteRow
        label={t('virtualCity.bondYield' as TKey)}
        value={formatPct(market.bond_yield)}
        curr={market.bond_yield}
        prev={prev?.bond}
        history={bonds}
        color="#34d399"
      />

      <div className="virtualCity-marketpanel__districts">
        <div className="virtualCity-marketpanel__districts-title">
          {t('virtualCity.housePrice' as TKey)}
        </div>
        {houseBars.map(({ d, def, pct }) => (
          <button
            key={d.id}
            type="button"
            className="virtualCity-hp-bar"
            onClick={() => def && onSelectDistrict(def.id)}
            title={def ? `${def.nameZh} · β${def.houseBeta.toFixed(2)}` : d.id}
          >
            <span className="virtualCity-hp-bar__label">{t(`virtualCity.district.${d.id}` as TKey)}</span>
            <span className="virtualCity-hp-bar__track">
              <span
                className="virtualCity-hp-bar__fill"
                style={{ width: `${pct}%`, background: def?.color ?? '#9ca3af' }}
              />
            </span>
            <span className="virtualCity-hp-bar__value">
              {d.price_index.toFixed(2)}
              {def ? ` ·β${def.houseBeta.toFixed(2)}` : ''}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
