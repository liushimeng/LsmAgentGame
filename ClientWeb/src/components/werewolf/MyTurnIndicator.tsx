/**
 * MyTurnIndicator — 2026-07-21 §人类玩家操作重构
 *
 * 「轮到我了」专属提示组件。仅在 phase_extra.my_turn_now === true 时渲染;
 * 否则返回 null(不占布局)。
 *
 * 视觉规则:
 *   - 默认浅蓝背景 + ⏱ 倒计时数字(white,1.5em)
 *   - 剩余 ≤ 10s:数字变红 + pulse 动画 + 「⚠ 还剩 10 秒」徽章
 *   - 剩余 ≤ 5s:加「⏳ 5 秒后自动跳过」徽章
 *
 * 与 <PhaseClock> 的区别:
 *   - PhaseClock 显示当前阶段的全局 deadline(所有人可见);
 *   - MyTurnIndicator 仅对"我"显示,且根据是否轮到行动切换"被动 vs 主动"。
 */

import { useEffect, useState } from 'react';

interface Props {
  myTurnNow: boolean;
  myTurnRemainingSec: number;
  mySeat: number;
}

export function MyTurnIndicator({ myTurnNow, myTurnRemainingSec, mySeat }: Props) {
  // 本地 1s tick 触发 re-render;窗口不可见时暂停以省电。
  // 服务端给的 remaining_sec 是 1 帧快照;客户端每 1s 重新渲染读取最新 prop。
  // (新帧 game.state 会刷新 prop;这是兜底 1s 视觉刷新。)
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!myTurnNow) return;
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') {
        setTick((n) => n + 1);
      }
    }, 1000);
    return () => clearInterval(t);
  }, [myTurnNow]);

  if (!myTurnNow || mySeat < 0) return null;

  // 渲染时按当前 tick 重新计算剩余秒数(服务端给的只是 1 帧快照)
  // 真实组件不重读 phase_extra,直接用 prop(组件 re-mount 时 prop 也会更新)。
  const remain = myTurnRemainingSec;
  const isWarning = remain <= 10;
  const isCritical = remain <= 5;
  const isExpired = remain <= 0;

  return (
    <div
      className={`my-turn-indicator${isWarning ? ' is-warning' : ''}${isCritical ? ' is-critical' : ''}${isExpired ? ' is-expired' : ''}`}
      role="status"
      aria-live="polite"
      data-testid="my-turn-indicator"
    >
      <span className="my-turn-indicator__seat">🎯 轮到 #{mySeat + 1} 行动</span>
      <span className="my-turn-indicator__countdown">
        {isExpired ? '⏭ 已自动跳过' : `⏱ ${remain}s`}
      </span>
      {isWarning && !isExpired && (
        <span className="my-turn-indicator__badge">
          {isCritical ? '⏳ 5 秒后自动跳过' : '⚠ 还剩 10 秒'}
        </span>
      )}
    </div>
  );
}
