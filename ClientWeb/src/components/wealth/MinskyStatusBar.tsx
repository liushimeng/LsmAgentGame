/**
 * MinskyStatusBar — 明斯基风险提示条（P1 明斯基引擎）。
 *
 * 在 ActionPanel 顶部展示当前融资等级与全局风险：
 *   - hedge(绿) / speculative(黄) / ponzi(红) 三色等级徽章
 *   - 庞氏玩家占比 > 30% 阈值警告
 *   - 冷却期剩余月
 *   - 明斯基时刻红屏闪烁动画（最近事件含 "minsky" 类型时触发）
 *
 * 数据源：game.state.players[mySeat].minsky_tier / debt_to_income
 *          + game.state.minsky_overview + eventFeed（minsky 事件检测）
 */

import { useEffect, useRef, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';
import {
  minskyTierColor,
  type WealthGameState,
  type WealthMinskyTier,
} from '@/types/wealth';
import { useWealthStore } from '@/store/wealth.store';

interface Props {
  gameState: WealthGameState | null;
  mySeat: number;
}

/** 查找最近 minsky 事件（最近 1 条，含 "minsky" type 关键字）。 */
function findRecentMinskyEvent(events: { type?: string; month?: number }[], sinceMonth: number): boolean {
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i];
    if (ev.month !== undefined && ev.month < sinceMonth) break;
    if (ev.type === 'minsky') return true;
  }
  return false;
}

export function MinskyStatusBar({ gameState, mySeat }: Props) {
  const t = useT();
  const eventFeed = useWealthStore((s) => s.eventFeed);

  const me = gameState?.players.find((p) => p.seat === mySeat);
  const overview = gameState?.minsky_overview;
  const tier: WealthMinskyTier | undefined = me?.minsky_tier;
  const debtToIncome = me?.debt_to_income;

  // 明斯基时刻红屏闪烁：最近一轮 month 内出现过 minsky 事件即闪烁 3 秒。
  const currentMonth = gameState?.month ?? 0;
  const lastMinskyMonth = useRef<number>(-1);
  const [flashing, setFlashing] = useState(false);

  useEffect(() => {
    if (!findRecentMinskyEvent(eventFeed, currentMonth)) return;
    if (lastMinskyMonth.current === currentMonth) return;
    lastMinskyMonth.current = currentMonth;
    setFlashing(true);
    const timer = window.setTimeout(() => setFlashing(false), 3000);
    return () => window.clearTimeout(timer);
  }, [eventFeed, currentMonth]);

  // 当后端未下发任何明斯基数据时，整个提示条隐藏。
  if (!tier && !overview) return null;

  const tierColor = minskyTierColor(tier);
  const dangerRatio = overview && overview.ponzi_ratio > 0.3;

  return (
    <div className={'wealth-minsky-statusbar' + (flashing ? ' wealth-minsky-moment-flash' : '')}>
      {/* 我的融资等级 */}
      {tier && (
        <span
          className="wealth-badge wealth-minsky-statusbar__badge"
          style={{ background: tierColor, color: '#ffffff' }}
        >
          {t(`minsky.${tier}` as TKey)}
          {debtToIncome !== undefined ? ` · ${(debtToIncome * 100).toFixed(0)}%` : ''}
        </span>
      )}

      {/* 全局风险提示 */}
      {overview && dangerRatio && (
        <span className="wealth-minsky-statusbar__warn">
          ⚠️ {t('minsky.globalWarning' as TKey, { pct: (overview.ponzi_ratio * 100).toFixed(0) })}
        </span>
      )}

      {/* 冷却期 */}
      {overview && overview.cooldown_left > 0 && (
        <span className="wealth-minsky-statusbar__cooldown">
          {t('minsky.cooldown' as TKey, { n: overview.cooldown_left })}
        </span>
      )}

      {/* 时刻闪烁动画说明文字 */}
      {flashing && (
        <span className="wealth-minsky-statusbar__flash-text">
          🚨 {t('minsky.moment' as TKey)}
        </span>
      )}
    </div>
  );
}
