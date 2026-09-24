/**
 * MonthTicker — 月度节拍条：距月结倒计时进度条（next_month_at − now，250ms 步进）
 * + 事件横滚（game.event 增量流，生命事件 emoji 前缀）+ 最新一帧月度汇总
 * （全座位 cash_delta / net_worth 迷你表，可折叠）。
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import { formatCny, formatDelta, type VirtualCityCityProfileProgress, type VirtualCityGameState } from '@/types/virtualCity';
import type { VirtualCityEventFrame, VirtualCityMonthFrame } from '@/types/virtualCity';

/** 事件类型 → emoji 前缀（产品设计 §8.2 呈现规则）。 */
const EVENT_EMOJI: Record<string, string> = {
  action: '🎯', move: '🚚', settle: '🧾', market: '📈',
  life: '📅', chat: '💬', error: '⚠️',
  // 档案锚定设计 §8.3 — 档案锚定终态事件（📦 前缀，文案前端 i18n 合成）。
  city_profiles: '📦',
};

/**
 * city_profiles 事件 → i18n 文案（§8.3：「城市人物档案锚定完成 done/total」或失败提示）。
 * 载荷优先取 frame.profiles，兼容后端把它放进 data.profiles 的形状；
 * 无法解析时返回 null（回退渲染服务端 text）。
 */
function cityProfilesPayload(e: VirtualCityEventFrame): VirtualCityCityProfileProgress | null {
  if (e.type !== 'city_profiles') return null;
  if (e.profiles) return e.profiles;
  if (e.data && typeof e.data === 'object') {
    const d = e.data as { profiles?: VirtualCityCityProfileProgress };
    if (d.profiles) return d.profiles;
  }
  return null;
}

function tick(): number {
  return Date.now();
}

interface Props {
  gameState: VirtualCityGameState | null;
  eventFeed: VirtualCityEventFrame[];
  lastMonth: VirtualCityMonthFrame | null;
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
    <div className="virtualCity-ticker">
      <div className="virtualCity-ticker__month">
        📅 {gameState ? t('virtualCity.month' as TKey, { m: gameState.month }) : '—'} ·{' '}
        {gameState ? t('virtualCity.age' as TKey, { a: gameState.age }) : '—'}
        {gameState?.phase === 'settling' && (
          <span className="virtualCity-badge virtualCity-phase-badge--settling">
            {t('virtualCity.phase.settling' as TKey)}
          </span>
        )}
      </div>
      <div className="virtualCity-ticker__countdown" role="timer" aria-label="month countdown">
        <div className="virtualCity-ticker__bar">
          <div className="virtualCity-ticker__bar-fill" style={{ width: `${pct}%` }} />
        </div>
        <span className="virtualCity-ticker__remain">{remainSec}s</span>
      </div>
      <div className="virtualCity-ticker__events">
        {recentEvents.map((e, i) => {
          // city_profiles：前端 i18n 合成三语文案（服务端 text 仅作解析失败兜底）。
          const cp = cityProfilesPayload(e);
          const text = cp
            ? cp.status === 'failed'
              ? t('virtualCity.cityProfiles.anchorFailedEvent' as TKey)
              : t('virtualCity.cityProfiles.anchorDoneEvent' as TKey, {
                  done: cp.done,
                  total: cp.total,
                })
            : e.text;
          return (
            <span
              key={`${i}-${e.month}-${e.text}`}
              className={
                'virtualCity-ticker__event' +
                (e.type === 'error' ? ' virtualCity-ticker__event--error' : '') +
                (e.type === 'life' ? ' virtualCity-ticker__event--life' : '')
              }
            >
              {EVENT_EMOJI[e.type] ?? '•'} {text}
            </span>
          );
        })}
        {recentEvents.length === 0 && (
          <span className="virtualCity-ticker__event virtualCity-ticker__event--muted">
            {t('virtualCity.ticker.empty' as TKey)}
          </span>
        )}
      </div>
      {lastMonth && (
        <details className="virtualCity-ticker__summary">
          <summary>
            🧾 {t('virtualCity.ticker.summary' as TKey)} · M{lastMonth.month}
          </summary>
          {/* 10–12 座位：每座位一行，限高滚动（.virtualCity-ticker__summary-scroll），
              避免展开后把底部节拍条撑出视口。 */}
          <div className="virtualCity-ticker__summary-scroll">
            <table className="virtualCity-table">
              <thead>
                <tr>
                  <th>#</th>
                  <th>{t('virtualCity.ticker.cashDelta' as TKey)}</th>
                  <th>{t('virtualCity.netWorth' as TKey)}</th>
                  <th>FI</th>
                  <th>{t('virtualCity.ticker.note' as TKey)}</th>
                </tr>
              </thead>
              <tbody>
                {lastMonth.summaries.map((s) => (
                  <tr key={s.seat}>
                    <td>{s.seat + 1}</td>
                    <td className={s.cash_delta >= 0 ? 'virtualCity-num--pos' : 'virtualCity-num--neg'}>
                      {formatDelta(s.cash_delta)}
                    </td>
                    <td>{formatCny(s.net_worth)}</td>
                    <td>{s.fi_index.toFixed(2)}</td>
                    <td className="virtualCity-ticker__note">{s.note}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </div>
  );
}
