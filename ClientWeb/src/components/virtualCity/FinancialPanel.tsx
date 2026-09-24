/**
 * FinancialPanel — 我的财务三表 + 五维资源条 + FI 半圆仪表。
 *
 * 数据源：game.state.my.*（观战者 / 未开局时 my=null → 空态）。
 * 三表 Tab：损益（my.monthly.detail）/ 资产负债（my.assets + my.loans）/
 * 现金流（ledger_recent 聚合）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  fiIndexColor,
  formatCny,
  formatPct,
  type VirtualCityGameState,
} from '@/types/virtualCity';

type FinTab = 'income' | 'balance' | 'cashflow';

/** FI 半圆仪表（0–2 封顶；档位色 <0.5 灰 / 0.5–1 蓝 / ≥1 绿 / ≥1.5 金）。 */
function FiGauge({ fi }: { fi: number }) {
  const t = useT();
  const clamped = Math.max(0, Math.min(2, fi));
  const angle = Math.PI * (1 - clamped / 2); // 180°→0°
  const r = 42;
  const cx = 52;
  const cy = 52;
  const nx = cx + r * Math.cos(angle);
  const ny = cy - r * Math.sin(angle);
  const largeArc = clamped / 2 > 0.5 ? 1 : 0;
  return (
    <div className="virtualCity-fi-gauge">
      <svg viewBox="0 0 104 62" width="104" height="62" role="img" aria-label={`FI ${clamped.toFixed(2)}`}>
        <path d={`M ${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}`} fill="none" stroke="#374151" strokeWidth="9" strokeLinecap="round" />
        <path
          d={`M ${cx - r} ${cy} A ${r} ${r} 0 ${largeArc} 1 ${nx} ${ny}`}
          fill="none"
          stroke={fiIndexColor(clamped)}
          strokeWidth="9"
          strokeLinecap="round"
        />
        <text x={cx} y={cy - 8} textAnchor="middle" className="virtualCity-fi-gauge__value" fill={fiIndexColor(clamped)}>
          {clamped.toFixed(2)}
        </text>
      </svg>
      <span className="virtualCity-fi-gauge__label">{t('virtualCity.fiIndex' as TKey)}</span>
    </div>
  );
}

/** 资源条（0–10，能量可透支到 -3 → 显示时 clamp 0 但数值照实）。 */
function ResourceBar({ label, value }: { label: string; value: number }) {
  const max = 10;
  const pct = Math.max(0, Math.min(100, (value / max) * 100));
  const negative = value < 0;
  return (
    <div className="virtualCity-res-bar">
      <span className="virtualCity-res-bar__label">{label}</span>
      <div className="virtualCity-res-bar__track">
        <div
          className={'virtualCity-res-bar__fill' + (negative ? ' virtualCity-res-bar__fill--neg' : '')}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="virtualCity-res-bar__value">{value.toFixed(0)}/{max}</span>
    </div>
  );
}

interface Props {
  gameState: VirtualCityGameState | null;
}

export function FinancialPanel({ gameState }: Props) {
  const t = useT();
  const [tab, setTab] = useState<FinTab>('income');
  const my = gameState?.my ?? null;
  const me = useMemo(
    () => gameState?.players.find((p) => p.seat === gameState.my_seat) ?? null,
    [gameState],
  );

  if (!my || !me) {
    return (
      <div className="virtualCity-panel__empty">
        <p>{t(gameState ? ('virtualCity.panel.spectatorEmpty' as TKey) : ('virtualCity.panel.waiting' as TKey))}</p>
      </div>
    );
  }

  // 现金流 Tab：ledger_recent 本月聚合（前端推导）。
  const month = gameState!.month;
  const monthLedger = (gameState!.ledger_recent ?? []).filter((l) => l.month === month);
  const inflow = monthLedger.filter((l) => l.to.startsWith('seat:')).reduce((s, l) => s + l.amount_cny, 0);
  const outflow = monthLedger.filter((l) => l.from.startsWith('seat:')).reduce((s, l) => s + l.amount_cny, 0);

  const tabs: { key: FinTab; label: string }[] = [
    { key: 'income', label: t('virtualCity.panel.income' as TKey) },
    { key: 'balance', label: t('virtualCity.panel.balance' as TKey) },
    { key: 'cashflow', label: t('virtualCity.panel.cashflow' as TKey) },
  ];

  return (
    <div className="virtualCity-finpanel">
      {/* 概要行：现金 / 净资产 / FI 仪表 */}
      <div className="virtualCity-finpanel__summary">
        <div className="virtualCity-finpanel__kv">
          <span>{t('virtualCity.cash' as TKey)}</span>
          <b className={my.cash < 0 ? 'virtualCity-num--neg' : ''}>¥{formatCny(my.cash)}</b>
        </div>
        <div className="virtualCity-finpanel__kv">
          <span>{t('virtualCity.netWorth' as TKey)}</span>
          <b>¥{formatCny(my.net_worth)}</b>
        </div>
        <div className="virtualCity-finpanel__kv">
          <span>{t('virtualCity.passiveIncome' as TKey)}</span>
          <b>¥{formatCny(my.passive_income)}</b>
        </div>
        <FiGauge fi={my.fi_index} />
      </div>

      {/* 五维资源条（E/N/K + 信用分 + 年龄） */}
      <div className="virtualCity-finpanel__res">
        <ResourceBar label={t('virtualCity.energy' as TKey)} value={my.resources.energy} />
        <ResourceBar label={t('virtualCity.network' as TKey)} value={my.resources.network} />
        <ResourceBar label={t('virtualCity.cognition' as TKey)} value={my.resources.cognition} />
        <div className="virtualCity-finpanel__meta">
          <span className="virtualCity-badge virtualCity-badge--credit">
            {t('virtualCity.creditScore' as TKey)} {my.credit_score}
          </span>
          <span className="virtualCity-badge virtualCity-badge--band">
            {t(`virtualCity.incomeBand.${me.income_band}` as TKey)}
          </span>
          <span className="virtualCity-badge">
            {t('virtualCity.family' as TKey)}：{my.family.marital === 'married' ? t('virtualCity.family.married' as TKey) : t('virtualCity.family.single' as TKey)}
            {my.family.children > 0 ? ` ×${my.family.children}` : ''}
          </span>
        </div>
      </div>

      {/* 三表 Tab */}
      <div className="virtualCity-tabs virtualCity-tabs--fin">
        {tabs.map((x) => (
          <button
            key={x.key}
            type="button"
            className={'virtualCity-tabs__btn' + (tab === x.key ? ' virtualCity-tabs__btn--active' : '')}
            onClick={() => setTab(x.key)}
          >
            {x.label}
          </button>
        ))}
      </div>

      {tab === 'income' && (
        <div className="virtualCity-finpanel__table">
          <div className="virtualCity-finpanel__kv virtualCity-finpanel__kv--head">
            <span>{t('virtualCity.monthlyIncome' as TKey)} ¥{formatCny(my.monthly.income)}</span>
            <span>{t('virtualCity.monthlyExpense' as TKey)} ¥{formatCny(my.monthly.expense)}</span>
            <span className={my.monthly.net >= 0 ? 'virtualCity-num--pos' : 'virtualCity-num--neg'}>
              {t('virtualCity.monthlyNet' as TKey)} ¥{formatCny(my.monthly.net)}
            </span>
          </div>
          <ul className="virtualCity-detail-list">
            {my.monthly.detail.map((d, i) => (
              <li key={i} className="virtualCity-detail-list__row">
                <span className="virtualCity-detail-list__text">{d.text || d.key}</span>
                <span className={d.amount_cny >= 0 ? 'virtualCity-num--pos' : 'virtualCity-num--neg'}>
                  {d.amount_cny >= 0 ? '+' : ''}¥{formatCny(d.amount_cny)}
                </span>
              </li>
            ))}
            {my.monthly.detail.length === 0 && (
              <li className="virtualCity-detail-list__row virtualCity-detail-list__row--empty">
                {t('virtualCity.panel.empty' as TKey)}
              </li>
            )}
          </ul>
          <div className="virtualCity-finpanel__kv virtualCity-finpanel__kv--foot">
            <span>{t('virtualCity.monthlyTax' as TKey)} ¥{formatCny(my.monthly.tax)}</span>
            <span>{t('virtualCity.monthlySocial' as TKey)} ¥{formatCny(my.monthly.social)}</span>
            <span>{t('virtualCity.pension' as TKey)} ¥{formatCny(my.pension_cny)}</span>
          </div>
        </div>
      )}

      {tab === 'balance' && (
        <div className="virtualCity-finpanel__table">
          <table className="virtualCity-table">
            <thead>
              <tr>
                <th>{t('virtualCity.assets' as TKey)}</th>
                <th>{t('virtualCity.loan.units' as TKey)}</th>
                <th>{t('virtualCity.loan.value' as TKey)}</th>
                <th>{t('virtualCity.monthlyNet' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {my.assets.map((a, i) => (
                <tr key={i}>
                  <td>{a.name}</td>
                  <td>{a.units}</td>
                  <td>¥{formatCny(a.value_cny)}</td>
                  <td className={a.monthly_flow_cny >= 0 ? 'virtualCity-num--pos' : 'virtualCity-num--neg'}>
                    {a.monthly_flow_cny >= 0 ? '+' : ''}¥{formatCny(a.monthly_flow_cny)}
                  </td>
                </tr>
              ))}
              {my.assets.length === 0 && (
                <tr><td colSpan={4} className="virtualCity-table__empty">{t('virtualCity.noAssets' as TKey)}</td></tr>
              )}
            </tbody>
          </table>
          <table className="virtualCity-table">
            <thead>
              <tr>
                <th>{t('virtualCity.loans' as TKey)}</th>
                <th>{t('virtualCity.loan.balance' as TKey)}</th>
                <th>{t('virtualCity.loan.rate' as TKey)}</th>
                <th>{t('virtualCity.loan.monthly' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {my.loans.map((l) => (
                <tr key={l.id}>
                  <td>{l.id} · {l.kind}</td>
                  <td>¥{formatCny(l.balance)}</td>
                  <td>{formatPct(l.annual_rate)}</td>
                  <td>¥{formatCny(l.monthly_payment)} ×{l.months_left}</td>
                </tr>
              ))}
              {my.loans.length === 0 && (
                <tr><td colSpan={4} className="virtualCity-table__empty">{t('virtualCity.noLoans' as TKey)}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {tab === 'cashflow' && (
        <div className="virtualCity-finpanel__table">
          <div className="virtualCity-finpanel__kv virtualCity-finpanel__kv--head">
            <span className="virtualCity-num--pos">↑ ¥{formatCny(inflow)}</span>
            <span className="virtualCity-num--neg">↓ ¥{formatCny(outflow)}</span>
            <span className={inflow - outflow >= 0 ? 'virtualCity-num--pos' : 'virtualCity-num--neg'}>
              Σ ¥{formatCny(inflow - outflow)}
            </span>
          </div>
          <ul className="virtualCity-detail-list">
            {monthLedger.slice(-20).reverse().map((l, i) => (
              <li key={i} className="virtualCity-detail-list__row">
                <span className="virtualCity-detail-list__text">
                  {l.from} → {l.to} · {l.category}{l.note ? ` · ${l.note}` : ''}
                </span>
                <span>¥{formatCny(l.amount_cny)}</span>
              </li>
            ))}
            {monthLedger.length === 0 && (
              <li className="virtualCity-detail-list__row virtualCity-detail-list__row--empty">
                {t('virtualCity.panel.empty' as TKey)}
              </li>
            )}
          </ul>
        </div>
      )}

      {/* 职业目标（终局对照展示） */}
      {my.goals.length > 0 && (
        <details className="virtualCity-goals">
          <summary>🎯 {t('virtualCity.goals' as TKey)}</summary>
          <ul>
            {my.goals.map((g, i) => (
              <li key={i}>{g}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
