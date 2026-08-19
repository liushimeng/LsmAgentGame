/**
 * BotThoughtPanel — 简化版: 显示最近一句内心独白 + 折叠历史
 * 沿用狼人杀 AgentThoughtPanel 模式,仅保留核心展示
 * 2026-08-19 §德州扑克Agent
 */
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  thought: string;
  visible?: boolean;
}

export function BotThoughtPanel({ thought, visible = true }: Props) {
  const t = useT();
  if (!visible || !thought) return null;

  return (
    <details className="thp-bot-thought">
      <summary>💭 {t('texasholdem.bot.recentDecision' as TKey)}</summary>
      <p className="thp-bot-thought__text">{thought}</p>
    </details>
  );
}

export default BotThoughtPanel;