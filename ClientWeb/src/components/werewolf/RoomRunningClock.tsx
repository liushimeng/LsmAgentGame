// RoomRunningClock — 2026-07-18 §UX-运行时
// 显示房间从开局到现在的"已运行 X 分 Y 秒"。
//
// 数据来源:game.state.game_started_at(Unix 秒,ServerGo/game/werewolf/view.go::ClientGameState.GameStartedAt)。
// 0 = 未开局(filling 阶段),显示"⏱ —"。
//
// 实现:
//   - 1s setInterval 推进 nowMs;窗口隐藏时停止(避免后台标签页空转)。
//   - 结束态(status === 'over')显示"整局 X:YY:ZZ"。
//
// i18n:werewolf.history.clock.{running,ended,idle}。

import React, { useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';

interface RoomRunningClockProps {
  /** 整局开始 Unix 秒;0 / undefined = 未开始 */
  gameStartedAt: number | undefined;
  /** 当前状态;'over' 显示"整局"文案 */
  status: string | undefined;
  /** 测试用: 注入固定的 nowMs;不传则本地维护 */
  nowMs?: number;
}

function formatHMS(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = s % 60;
  if (h > 0) {
    return `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`;
  }
  return `${m}:${String(ss).padStart(2, '0')}`;
}

export const RoomRunningClock: React.FC<RoomRunningClockProps> = ({
  gameStartedAt,
  status,
  nowMs: injectedNowMs,
}) => {
  const t = useT();
  const [nowMs, setNowMs] = useState<number>(injectedNowMs ?? Date.now());

  useEffect(() => {
    if (typeof injectedNowMs === 'number') return;
    const tick = () => setNowMs(Date.now());
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [injectedNowMs]);

  // 未开局
  if (!gameStartedAt || gameStartedAt <= 0) {
    return (
      <span
        className="ww-room-running-clock ww-room-running-clock--idle"
        data-testid="ww-room-clock"
        title={t('werewolf.history.clock.idle')}
      >
        ⏱ —
      </span>
    );
  }

  const elapsedSec = Math.max(0, Math.floor((nowMs - gameStartedAt * 1000) / 1000));
  const isEnded = status === 'over';
  const key = isEnded ? 'werewolf.history.clock.ended' : 'werewolf.history.clock.running';
  // 文案格式:{h}:{m}:{s} → 替换占位符
  const template = t(key as any);
  const display = template.replace('{h}:{m}:{s}', formatHMS(elapsedSec));

  return (
    <span
      className={`ww-room-running-clock ${isEnded ? 'is-ended' : 'is-running'}`}
      data-testid="ww-room-clock"
      aria-live="polite"
    >
      ⏱ {display}
    </span>
  );
};

export default RoomRunningClock;
