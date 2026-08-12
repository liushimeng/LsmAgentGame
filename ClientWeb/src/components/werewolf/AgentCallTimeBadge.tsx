// 2026-07-12 Agent 最后调用时间实时脉冲 + 相对时间显示。
// 在座位卡 SeatCell 内 LLM 性能徽章旁渲染:脉冲圆点(5px) + "12s 前"。
//
// 脉冲/颜色对齐后端 llm_call_phase:
//   calling / streaming → 绿脉冲(#22c55e)
//   retrying           → 橙脉冲(#f59e0b)
//   quarantined        → 红静态(#ef4444)
//   idle / 其它        → 灰静态(#6b7280)
//
// a11y:prefers-reduced-motion 时停止脉冲(参 §126 ThinkingDots 降级写法);
// 圆点 aria-hidden,相对时间文本可读。

import React from 'react';
import type { BotContextJSON } from '@/types/werewolf';
import { useT } from '@/hooks/useT';
import { formatRelativeTime } from '@/shared/utils/time';

interface AgentCallTimeBadgeProps {
  ctx: BotContextJSON;
  nowMs: number;
}

function dotClass(phase: BotContextJSON['llm_call_phase'], quarantined?: boolean): string {
  if (quarantined || phase === 'quarantined') return 'bot-call-dot--quarantined';
  switch (phase) {
    case 'calling':
    case 'streaming':
      return 'bot-call-dot--calling';
    case 'retrying':
      return 'bot-call-dot--retrying';
    default:
      return 'bot-call-dot--idle';
  }
}

export const AgentCallTimeBadge: React.FC<AgentCallTimeBadgeProps> = ({ ctx, nowMs }) => {
  const t = useT();
  const phase = ctx.llm_call_phase ?? 'idle';
  const lastCall = ctx.last_llm_call_at_ms ?? 0;
  const relative = formatRelativeTime(lastCall, nowMs);
  if (!relative) {
    return (
      <span className="werewolf-seat__call-time werewolf-seat__call-time--never">
        <span className={`bot-call-dot ${dotClass(phase, ctx.quarantined)}`} aria-hidden />
        {t('werewolf.bot.neverCalled')}
      </span>
    );
  }
  const suffix = t('werewolf.bot.suffixAgo');
  return (
    <span
      className="werewolf-seat__call-time"
      data-testid={`seat-call-time-${ctx.seat}`}
      title={`${relative} ${suffix}`}
    >
      <span className={`bot-call-dot ${dotClass(phase, ctx.quarantined)}`} aria-hidden />
      {' '}{relative}{suffix}
    </span>
  );
};

export default AgentCallTimeBadge;
