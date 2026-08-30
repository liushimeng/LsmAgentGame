/**
 * SuicideTakeInline — §20260830-02 自爆带走面板(自爆狼本人操作)。
 *
 * 设计文档:docs/狼人杀-角色设计/狼人杀自爆遗言与带走设计-20260830-02.md §7。
 *
 * 渲染条件(由 WerewolfGamePage 控制):phase === 'suicide_take' &&
 * my_seat === suicided_wolf_seat。此时自爆狼已死亡但拥有「带走一名存活
 * 玩家」的行动权(死者行动白名单,与猎枪同语义)。
 *
 * 行为:
 *   - 顶部死亡态 hint(你已自爆出局,这是最后一击)
 *   - 存活座位网格(点选即提交带走,不可撤回)+ 红色警示
 *   - 「放弃带走」按钮 → onTake(-1)
 *   - 提交后渲染紧凑「已提交」态(§20260823-02 P9 同款;点击恢复可重试)
 *   - 倒计时(phase_extra.remaining_sec + 1s 本地 tick,超时服务端 watchdog
 *     自动派发放弃带走)
 */

import { useEffect, useState } from 'react';
import { useT } from '@/hooks/useT';
import type { WerewolfGameState } from '@/types/werewolf';

interface Props {
  gameState: WerewolfGameState;
  onTake: (target: number) => void;
  busy: boolean;
}

export function SuicideTakeInline({ gameState, onTake, busy }: Props) {
  const t = useT();
  const phase = gameState.phase;
  const [submitted, setSubmitted] = useState(false);
  const [, setTick] = useState(0);

  // 1s tick 刷新倒计时(服务端是快照,本地 tick 补足最后一秒观感)。
  useEffect(() => {
    if (phase !== 'suicide_take') return;
    const id = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [phase]);
  // 阶段离开时复位已提交态。
  useEffect(() => {
    if (phase !== 'suicide_take') setSubmitted(false);
  }, [phase]);

  if (phase !== 'suicide_take') return null;

  const remaining = Math.max(0, gameState.phase_extra?.remaining_sec ?? 30);

  if (submitted) {
    return (
      <div
        className="werewolf-action-panel ww-cap-submitted"
        role="button"
        tabIndex={0}
        onClick={() => setSubmitted(false)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setSubmitted(false);
          }
        }}
        data-testid="suicide-take-submitted"
      >
        {t('werewolf.panel.submitted')}
      </div>
    );
  }

  const take = (seat: number) => {
    if (busy) return;
    onTake(seat);
    setSubmitted(true);
  };

  const decline = () => {
    if (busy) return;
    onTake(-1);
    setSubmitted(true);
  };

  return (
    <div className="werewolf-action-panel suicide-take-panel" data-testid="suicide-take-panel">
      <header className="suicide-take-panel__header">
        <h4>🧨 {t('werewolf.suicideTake.title')}</h4>
        <span className={`suicide-take-panel__countdown${remaining <= 10 ? ' is-critical' : ''}`}>
          ⏱ {remaining}s
        </span>
      </header>
      <p className="suicide-take-panel__hint" data-testid="suicide-take-dead-hint">
        {t('werewolf.suicideTake.deadHint')}
      </p>
      <div className="seat-grid">
        {Array.from({ length: gameState.max_seat ?? 13 }).map((_, seat) => {
          const p = gameState.players?.[seat];
          if (!p || !p.alive) return null;
          return (
            <button
              key={seat}
              type="button"
              className="seat-chip suicide-take-panel__chip"
              onClick={() => take(seat)}
              disabled={busy}
              data-testid={`suicide-take-target-${seat}`}
            >
              #{seat + 1}
            </button>
          );
        })}
      </div>
      <footer className="suicide-take-panel__footer">
        <button
          type="button"
          className="btn btn-secondary"
          onClick={decline}
          disabled={busy}
          data-testid="suicide-take-decline"
        >
          🕊 {t('werewolf.suicideTake.decline')}
        </button>
      </footer>
    </div>
  );
}
