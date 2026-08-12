/**
 * SheriffElectedOverlay.tsx — 警长当选专属通知
 *
 * 监听 gameState.sheriff_seat 变化,当从 -1 变为有效座位号时,
 * 显示 2.5 秒覆盖层「⭐ 警长:X号」。类似 DayNightOverlay 的模式。
 * pointer-events:none 不阻塞交互。
 *
 * BUG-R245-P2-01 (2026-08-06): 原选举结果仅在活动流中广播,
 * 用户看不到明显通知,座位卡 ★ badge 因 phase 跳太快也不容易注意到。
 * 新增此组件在选举完成后立刻展示醒目 overlay。
 *
 * i18n: DayNightOverlay 采用硬编码中文(不走 t()),本组件保持一致。
 * 英文/日文用户看到中文 emoji+数字仍可理解。
 */

import { useEffect, useRef, useState } from 'react';

interface Props {
  /** 当前 sheriff_seat, -1 表示无警长 */
  sheriffSeat: number;
}

export function SheriffElectedOverlay({ sheriffSeat }: Props) {
  const [visible, setVisible] = useState(false);
  const [displaySeat, setDisplaySeat] = useState<number>(-1);
  const prevSeatRef = useRef<number>(-1);

  useEffect(() => {
    const prev = prevSeatRef.current;
    prevSeatRef.current = sheriffSeat;

    // 只在从 -1 → 有效座位号 时触发(首次选举 / 重开后选举)
    if (sheriffSeat < 0 || prev >= 0) return;

    setDisplaySeat(sheriffSeat);
    setVisible(true);
    const timer = window.setTimeout(() => setVisible(false), 2500);
    return () => window.clearTimeout(timer);
  }, [sheriffSeat]);

  if (!visible || displaySeat < 0) return null;

  return (
    <div
      className="ww-daynight-overlay ww-daynight-overlay--sheriff"
      role="presentation"
      data-testid="sheriff-elected-overlay"
    >
      <div className="ww-daynight-overlay__inner">
        <div className="ww-daynight-overlay__emoji" aria-hidden="true">
          ⭐
        </div>
        <div className="ww-daynight-overlay__title">
          警长: {displaySeat + 1}号
        </div>
        <div className="ww-daynight-overlay__subtitle">
          警徽生效 · 拥有归票权
        </div>
      </div>
    </div>
  );
}

export default SheriffElectedOverlay;
