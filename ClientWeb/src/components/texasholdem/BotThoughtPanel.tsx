/**
 * BotThoughtPanel — 简化版: 显示最近一句内心独白 + 折叠历史
 * 沿用狼人杀 AgentThoughtPanel 模式,仅保留核心展示
 * 2026-08-19 §德州扑克Agent
 * 2026-08-20 §F1: 接入 TexasHoldemTable(此前零引用,§126/§130「组件存在但未被 import
 * 等于不存在」复现);新增 seat/thinking props 以标识独白来源座位。
 */
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  thought: string;
  visible?: boolean;
  /** 0-indexed 座位号;展示时 +1(与 RoomCreateModal 「{n} 号位」一致)。 */
  seat?: number;
  /** 该座位 bot 是否正在思考(用于追加 ⏳ 指示)。 */
  thinking?: boolean;
}

export function BotThoughtPanel({ thought, visible = true, seat, thinking = false }: Props) {
  const t = useT();
  if (!visible || !thought) return null;

  const seatLabel =
    seat != null && seat >= 0
      ? `${t('texasholdem.createModal.seatNumber' as TKey, { n: seat + 1 })} ${t('texasholdem.bot.badge' as TKey)}`
      : null;

  return (
    <details className="thp-bot-thought">
      <summary>
        💭 {seatLabel ? `${seatLabel} · ` : ''}
        {t('texasholdem.bot.recentDecision' as TKey)}
        {thinking && (
          <span className="thp-bot-thought__thinking">
            ⏳ {t('texasholdem.bot.thinking' as TKey)}
          </span>
        )}
      </summary>
      <p className="thp-bot-thought__text">{thought}</p>
    </details>
  );
}

export default BotThoughtPanel;
