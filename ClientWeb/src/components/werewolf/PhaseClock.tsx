// PhaseClock — 服务端下发的 phase_extra.phase_deadline_at 本地 setInterval 渲染倒计时
// (2026-07-09 §13 增强 — 时钟机制)。
//
// Props:
//   - deadlineAt: RFC3339 绝对时间戳,来自 phase_extra.phase_deadline_at。
//   - fallbackSec: 兜底秒数(可选,无 deadlineAt 时退化展示)。
//   - phaseLabel: 当前阶段显示名,如 "🌅 黎明"。
//
// 行为:
//   - useEffect 启动 500ms 精度 setInterval,本地计算 remaining。
//   - remaining === 0 时显示红色脉冲 + "⌛ 等待阶段推进…"
//   - 服务端 watchdog 会在 deadline 到期后立即派发 skip,客户端无需主动推送。
import React, { useEffect, useState } from 'react';

interface PhaseClockProps {
  deadlineAt: string | undefined;
  fallbackSec?: number;
  phaseLabel: string;
}

export const PhaseClock: React.FC<PhaseClockProps> = ({
  deadlineAt,
  fallbackSec,
  phaseLabel,
}) => {
  const [remaining, setRemaining] = useState<number>(fallbackSec ?? 0);

  useEffect(() => {
    if (!deadlineAt) {
      setRemaining(fallbackSec ?? 0);
      return;
    }
    const deadline = new Date(deadlineAt).getTime();
    const tick = () => {
      const diff = Math.max(0, Math.floor((deadline - Date.now()) / 1000));
      setRemaining(diff);
    };
    tick();
    const id = setInterval(tick, 500);
    return () => clearInterval(id);
  }, [deadlineAt, fallbackSec]);

  const isOverdue = remaining === 0 && !!deadlineAt;
  return (
    <div
      className={`phase-clock ${isOverdue ? 'is-overdue' : ''}`}
      data-testid="phase-clock"
    >
      <span className="phase-clock__label">{phaseLabel}</span>
      <span className="phase-clock__time">
        {isOverdue ? '⌛ 等待阶段推进…' : `⏱ ${remaining}s`}
      </span>
    </div>
  );
};

export default PhaseClock;