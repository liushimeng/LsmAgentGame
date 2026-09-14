/**
 * MonthTicker — 月度节拍条：距月结倒计时进度条（next_month_at − now，250ms 步进）
 * + 事件横滚（game.event 增量流，生命事件 emoji 前缀）+ 最新一帧月度汇总
 * （全座位 cash_delta / net_worth 迷你表，可折叠）。
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatDelta, type WealthGameState } from '@/types/wealth';
import type { WealthEventFrame, WealthMonthFrame } from '@/types/wealth';

/** 事件类型 → emoji 前缀（产品设计 §8.2 呈现规则）。 */
const EVENT_EMOJI: Record<string, string> = {
  action: '🎯', move: '🚚', settle: '🧾', market: '📈',
  life: '📅', chat: '💬', error: '⚠️',
};

function tick(): number {
  return Date.now();
}

interface Props {
  gameState: WealthGameState | null;
  eventFeed: WealthEventFrame[];
  lastMonth: WealthMonthFrame | null;
}

export function MonthTicker({ gameState, eventFeed, lastMonth }: Props) {
  const t = useT();
  const [now, setNow] = useState(tick);
  // 窗口长度估计：协议不带 month_ms → 用本会话观测到的最大剩余秒近似满刻度。
  const maxRemainSec = useRef(1);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(tick()), 250);
    return () => window.clearInterval(timer);
  }, []);

  const remainMs = gameState?.next_month_at ? Math.max(0, gameState.next_month_at - now) : 0;
  const remainSec = Math.ceil(remainMs / 1000);
  if (remainSec > maxRemainSec.current) maxRemainSec.current = remainSec;
  const pct = Math.max(0, Math.min(100, (remainSec / maxRemainSec.current) * 100));

  const recentEvents = useMemo(() => [...eventFeed].slice(-30).reverse(), [eventFeed]);

  return (
    <div className="wealth-ticker">
      <div className="wealth-ticker__month">
        📅 {gameState ? t('wealth.month' as TKey, { m: gameState.month }) : '—'} ·{' '}
        {gameState ? t('wealth.age' as TKey, { a: gameState.age }) : '—'}
        {gameState?.phase === 'settling' && (
          <span className="wealth-badge wealth-phase-badge--settling">
            {t('wealth.phase.settling' as TKey)}
          </span>
        )}
      </div>
      <div className="wealth-ticker__countdown" role="timer" aria-label="month countdown">
        <div className="wealth-ticker__bar">
          <div className="wealth-ticker__bar-fill" style={{ width: `${pct}%` }} />
        </div>
        <span className="wealth-ticker__remain">{remainSec}s</span>
      </div>
      <div className="wealth-ticker__events">
        {recentEvents.map((e, i) => (
          <span
            key={`${i}-${e.month}-${e.text}`}
            className={
              'wealth-ticker__event' +
              (e.type === 'error' ? ' wealth-ticker__event--error' : '') +
              (e.type === 'life' ? ' wealth-ticker__event--life' : '')
            }
          >
            {EVENT_EMOJI[e.type] ?? '•'} {e.text}
          </span>
        ))}
        {recentEvents.length === 0 && (
          <span className="wealth-ticker__event wealth-ticker__event--muted">
            {t('wealth.ticker.empty' as TKey)}
          </span>
        )}
      </div>
      {lastMonth && (
        <details className="wealth-ticker__summary">
          <summary>
            🧾 {t('wealth.ticker.summary' as TKey)} · M{lastMonth.month}
          </summary>
          <table className="wealth-table">
            <thead>
              <tr>
                <th>#</th>
                <th>{t('wealth.ticker.cashDelta' as TKey)}</th>
                <th>{t('wealth.netWorth' as TKey)}</th>
                <th>FI</th>
                <th>{t('wealth.ticker.note' as TKey)}</th>
              </tr>
            </thead>
            <tbody>
              {lastMonth.summaries.map((s) => (
                <tr key={s.seat}>
                  <td>{s.seat + 1}</td>
                  <td className={s.cash_delta >= 0 ? 'wealth-num--pos' : 'wealth-num--neg'}>
                    {formatDelta(s.cash_delta)}
                  </td>
                  <td>{formatCny(s.net_worth)}</td>
                  <td>{s.fi_index.toFixed(2)}</td>
                  <td className="wealth-ticker__note">{s.note}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      )}
    </div>
  );
}
