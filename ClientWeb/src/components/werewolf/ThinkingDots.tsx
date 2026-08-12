// 2026-07-10 §重构 — LLM 调用指示器错峰跳点组件。
// 模仿 ChatGPT o1 'Reasoning' 与 Claude.ai 'Generating' 的 3 粒跳点动画:
// 1.5s 周期,3 粒错峰(0ms / 150ms / 300ms),给出"AI 正在推理"的暗示。
// 配色蓝紫渐变(从 #6e7bf2 → #8b5cf6),降低"长 API 调用"焦虑。
//
// a11y:
//   - prefers-reduced-motion 媒体查询下,跳点停止,改为静态 "…"
//   - role="status" aria-live="polite" 让屏幕阅读器按需朗读
//   - data-testid="thinking-dots" 方便 e2e 测试

import React from 'react';

interface ThinkingDotsProps {
  /** 当前调用相位;非 idle 时点亮,idle 时静态隐藏。 */
  active?: boolean;
  /** 自定义颜色类名(如 retrying 用黄,quarantined 用红)。 */
  colorClass?: string;
  /** a11y 文本,默认 "AI 正在思考"。 */
  ariaLabel?: string;
  /** 测试 ID 前缀,默认 "thinking-dots"。 */
  testId?: string;
}

export const ThinkingDots: React.FC<ThinkingDotsProps> = ({
  active = true,
  colorClass,
  ariaLabel = 'AI 正在思考',
  testId = 'thinking-dots',
}) => {
  if (!active) return null;

  const cls = `ww-thinking-dots${colorClass ? ' ' + colorClass : ''}`;
  return (
    <span
      className={cls}
      role="status"
      aria-live="polite"
      aria-label={ariaLabel}
      data-testid={testId}
    >
      <span className="ww-thinking-dots__dot ww-thinking-dots__dot--1" />
      <span className="ww-thinking-dots__dot ww-thinking-dots__dot--2" />
      <span className="ww-thinking-dots__dot ww-thinking-dots__dot--3" />
    </span>
  );
};

export default ThinkingDots;
