// 2026-07-10 §重构 — 单 bot LLM 调用相位多态指示器。
// 5 态状态机(idle / calling / streaming / retrying / quarantined),配合
// elapsed 时间 + retry 进度 + error class 渲染不同文案 + 视觉。
//
// 设计原则:
//   1. 文案由 i18n (zh-CN / en / ja) 提供,通过 t() hook 解析 {seconds} {current} {max} {reason} 占位符
//   2. 调用 <3s 显示"即将发言";3-15s 显示"思考中 N 秒";≥15s 显示"深度思考中 N 秒"
//   3. retry loop 内显示"重试 N/M" + 上次失败分类(5xx/429/timeout)
//   4. 流式首 token 到达显示"生成中"
//   5. quarantine(模型暂不可用)显示"模型响应异常 · 系统代打中 (<reason>)",
//      即便 isLLMCalling=false 也要保留;后端 manager 自动代打,游戏仍正常推进
//   6. idle 状态返回 null(交给父组件不渲染)
//
// 视觉风格:模仿 ChatGPT o1 / Claude.ai / Gemini — 蓝紫渐变 + 1.5s 错峰跳点;
// retrying / cooling 用黄/红脉冲区分。
// a11y:prefers-reduced-motion 时跳点静止,文案不变;role="status" + aria-live polite。

import React from 'react';
import { BotContextJSON } from '../../types/werewolf';
import { useT } from '@/hooks/useT';
import { ThinkingDots } from './ThinkingDots';

interface BotPhaseIndicatorProps {
  /** 后端下发的 BotContextJSON;取 llm_call_phase / llm_call_started_at /
   *  retry_attempt / retry_max_attempts / last_error_class / quarantined / quarantine_reason 字段。 */
  bot: BotContextJSON;
  /** 父组件传入的"当前时刻" ms,通常 1s setInterval 提供,
   *  让倒计时实时刷新而不必在每个指示器里再开 setInterval(性能优化)。 */
  nowMs: number;
  /** 测试 ID 后缀,默认 `${seat}`。 */
  testIdSuffix?: string;
}

const PHASE_DEEP_THRESHOLD_MS = 15_000; // ≥15s 进入"深度思考"档
const PHASE_SOON_THRESHOLD_MS = 3_000; // <3s 显示"即将发言"

/** BotPhaseIndicator 单 bot 多态指示器 */
export const BotPhaseIndicator: React.FC<BotPhaseIndicatorProps> = ({
  bot,
  nowMs,
  testIdSuffix,
}) => {
  const t = useT();
  const phase = bot.llm_call_phase ?? 'idle';
  const suffix = testIdSuffix ?? `${bot.seat}`;

  // === 1. quarantined 永远优先(即使不在 calling) ===
  // 2026-07-24 — 文案软化:quarantine 只是"模型暂不可用,系统代打中",
  // 后端 manager 会 auto-skip 代打,游戏不中断;不再用"已禁用/已停止"措辞。
  if (bot.quarantined || phase === 'quarantined') {
    // §R180-P3-OBS4 修复:fallback 文案统一用 t() i18n,避免硬编码「5 连失败」
    // 与后端 maxConsecutiveFailures=10 (§R81) 阈值不一致造成的状态幻觉。
    const reason = bot.quarantine_reason || t('werewolf.thinking.quarantinedFallback');
    return (
      <span
        className="ww-bot-phase ww-bot-phase--quarantined"
        role="status"
        aria-live="polite"
        data-testid={`bot-phase-${suffix}`}
      >
        <span className="ww-bot-phase__icon" aria-hidden>⚠️</span>
        <span className="ww-bot-phase__label">
          {t('werewolf.thinking.quarantined', { reason })}
        </span>
      </span>
    );
  }

  // === 2. idle 状态不渲染(交给父组件默认状态) ===
  if (phase === 'idle' && !bot.llm_call_in_progress) {
    return null;
  }

  // === 3. streaming phase(流式首 token 到达) ===
  if (phase === 'streaming') {
    return (
      <span
        className="ww-bot-phase ww-bot-phase--streaming"
        role="status"
        aria-live="polite"
        data-testid={`bot-phase-${suffix}`}
      >
        <span className="ww-bot-phase__icon" aria-hidden>✨</span>
        <span className="ww-bot-phase__label">
          {t('werewolf.thinking.streaming')}
        </span>
        <ThinkingDots colorClass="ww-thinking-dots--streaming" />
      </span>
    );
  }

  // === 4. retrying phase(在 retry loop 内,等待 backoff 后重试)
  // §127: 增加 queued / throttled 子态,让前端能区分"排队等槽"与"HTTP重试"。
  if (phase === 'retrying') {
    const current = bot.retry_attempt ?? 1;
    const max = (bot.retry_max_attempts ?? 1) + 1;
    const remainingSec = Math.max(
      0,
      Math.ceil(((bot.next_retry_at_ms ?? 0) - nowMs) / 1000)
    );
    const errClass = bot.last_error_class ?? '5xx';
    type RetryKey =
      | 'werewolf.thinking.cooling'
      | 'werewolf.thinking.reconnecting'
      | 'werewolf.thinking.retry'
      | 'werewolf.thinking.queued'
      | 'werewolf.thinking.throttled';
    let labelKey: RetryKey;
    switch (errClass) {
      case 'queued':
        labelKey = 'werewolf.thinking.queued';
        break;
      case 'throttled':
        labelKey = 'werewolf.thinking.throttled';
        break;
      case '429':
        labelKey = 'werewolf.thinking.cooling';
        break;
      case 'timeout':
      case '5xx':
      case 'permanent':
        labelKey = 'werewolf.thinking.reconnecting';
        break;
      default:
        labelKey = 'werewolf.thinking.retry';
    }
    return (
      <span
        className={`ww-bot-phase ww-bot-phase--retrying ww-bot-phase--${errClass}`}
        role="status"
        aria-live="polite"
        data-testid={`bot-phase-${suffix}`}
      >
        <span className="ww-bot-phase__icon" aria-hidden>↻</span>
        <span className="ww-bot-phase__label">
          {labelKey === 'werewolf.thinking.retry'
            ? t(labelKey, { current: current as number, max: max as number })
            : t(labelKey)}
        </span>
        {remainingSec > 0 && (
          <span className="ww-bot-phase__countdown">{remainingSec}s</span>
        )}
      </span>
    );
  }

  // === 5. calling phase(主路径,3 档文案 + 跳点) ===
  if (phase === 'calling' || bot.llm_call_in_progress) {
    const startedAt = bot.llm_call_started_at ?? nowMs;
    const elapsedMs = Math.max(0, nowMs - startedAt);
    const elapsedSec = Math.floor(elapsedMs / 1000);
    let labelKey: 'werewolf.thinking.soon' | 'werewolf.thinking.active' | 'werewolf.thinking.deep';
    if (elapsedMs < PHASE_SOON_THRESHOLD_MS) {
      labelKey = 'werewolf.thinking.soon';
    } else if (elapsedMs < PHASE_DEEP_THRESHOLD_MS) {
      labelKey = 'werewolf.thinking.active';
    } else {
      labelKey = 'werewolf.thinking.deep';
    }
    const label =
      labelKey === 'werewolf.thinking.soon'
        ? t(labelKey)
        : t(labelKey, { seconds: elapsedSec as number });
    return (
      <span
        className="ww-bot-phase ww-bot-phase--calling"
        role="status"
        aria-live="polite"
        data-testid={`bot-phase-${suffix}`}
      >
        <span className="ww-bot-phase__icon" aria-hidden>🤖</span>
        <span className="ww-bot-phase__label">{label}</span>
        <ThinkingDots colorClass="ww-thinking-dots--calling" />
      </span>
    );
  }

  return null;
};

export default BotPhaseIndicator;
