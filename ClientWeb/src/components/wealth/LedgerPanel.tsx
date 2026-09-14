/**
 * LedgerPanel — 财富流水：双式流水列表（最近 50 条，from→to 实体图标 + 类目 +
 * 金额红绿）+ 「本月汇总」Tab（category 聚合，前端推导）。
 *
 * 数据源：game.state.ledger_recent（服务端脱敏后仅含本人相关 + 公共条目）。
 */

import { useMemo, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, type WealthGameState } from '@/types/wealth';

/** Ledger 实体图标（from/to 前缀 → emoji；双式记账实体枚举）。 */
function entityIcon(entity: string): string {
  if (!entity) return '❔';
  if (entity.startsWith('seat:')) return '💺';
  if (entity === 'bank') return '🏦';
  if (entity === 'gov') return '🏛';
  if (entity === 'market') return '📈';
  if (entity === 'world') return '🌍';
  if (entity === 'insurer') return '🛡';
  return '❔';
}

type LedgerTab = 'flow' | 'monthly';

interface Props {
  gameState: WealthGameState | null;
}

export function LedgerPanel({ gameState }: Props) {
  const t = useT();
  const [tab, setTab] = useState<LedgerTab>('flow');
  const ledger = gameState?.ledger_recent ?? [];
  const month = gameState?.month ?? 0;

  // 本月汇总：按 category 聚合（支出视角 = from 为本人座位）。
  const monthly = useMemo(() => {
    const rows = ledger.filter((l) => l.month === month);
    const byCat = new Map<string, { total: number; count: number }>();
    for (const l of rows) {
      const cur = byCat.get(l.category) ?? { total: 0, count: 0 };
      cur.total += l.amount_cny;
      cur.count += 1;
      byCat.set(l.category, cur);
    }
    return [...byCat.entries()]
      .map(([category, v]) => ({ category, ...v }))
      .sort((a, b) => b.total - a.total);
  }, [ledger, month]);

  return (
    <div className="wealth-ledgerpanel">
      <div className="wealth-tabs">
        <button
          type="button"
          className={'wealth-tabs__btn' + (tab === 'flow' ? ' wealth-tabs__btn--active' : '')}
          onClick={() => setTab('flow')}
        >
          {t('wealth.ledger.title' as TKey)}
        </button>
        <button
          type="button"
          className={'wealth-tabs__btn' + (tab === 'monthly' ? ' wealth-tabs__btn--active' : '')}
          onClick={() => setTab('monthly')}
        >
          {t('wealth.ledger.monthly' as TKey)}
        </button>
      </div>

      {tab === 'flow' && (
        <ul className="wealth-ledger-list">
          {ledger.map((l, i) => (
            <li key={i} className="wealth-ledger-list__row">
              <span className="wealth-ledger-list__entities">
                {entityIcon(l.from)} {l.from} <span className="wealth-ledger-list__arrow">→</span> {entityIcon(l.to)} {l.to}
              </span>
              <span className="wealth-ledger-list__cat">{l.category}</span>
              <span className="wealth-ledger-list__amount">¥{formatCny(l.amount_cny)}</span>
            </li>
          ))}
          {ledger.length === 0 && (
            <li className="wealth-ledger-list__row wealth-ledger-list__row--empty">
              {t('wealth.panel.empty' as TKey)}
            </li>
          )}
        </ul>
      )}

      {tab === 'monthly' && (
        <table className="wealth-table">
          <thead>
            <tr>
              <th>{t('wealth.ledger.category' as TKey)}</th>
              <th>{t('wealth.ledger.count' as TKey)}</th>
              <th>{t('wealth.ledger.amount' as TKey)}</th>
            </tr>
          </thead>
          <tbody>
            {monthly.map((r) => (
              <tr key={r.category}>
                <td>{r.category}</td>
                <td>{r.count}</td>
                <td>¥{formatCny(r.total)}</td>
              </tr>
            ))}
            {monthly.length === 0 && (
              <tr>
                <td colSpan={3} className="wealth-table__empty">{t('wealth.panel.empty' as TKey)}</td>
              </tr>
            )}
          </tbody>
        </table>
      )}
    </div>
  );
}
