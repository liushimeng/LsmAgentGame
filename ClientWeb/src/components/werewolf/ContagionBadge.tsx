// §20260812-01 U3 — 情绪传染徽标(独立微组件,不破坏现有 EmotionAvatar)。
//
// 父级传入 optional `contagionKind` 即可渲染🦠 受感染微标。
// 颜色 / emoji 严格遵循 §26.4 状态徽章色板:背景 ≥ 0.55 透明度 + 白字 700。
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

export type ContagionKind = 'confident' | 'nervous' | 'angry' | 'calm';

const EMOJI: Record<ContagionKind, string> = {
  confident: '📢',
  nervous: '⚠️',
  angry: '🔥',
  calm: '🌊',
};

const BG: Record<ContagionKind, string> = {
  confident: 'rgba(241, 196, 15, 0.6)',  // 金
  nervous: 'rgba(231, 76, 60, 0.55)',    // 红 · §26.4 警告
  angry: 'rgba(231, 76, 60, 0.6)',
  calm: 'rgba(52, 152, 219, 0.6)',       // 蓝
};

export interface ContagionBadgeProps {
  kind: ContagionKind | null;
  strength?: number; // 0~1
}

/** ContagionBadge 在 state.bot_contexts[].contagion_kind 存在时渲染。 */
export function ContagionBadge({ kind, strength }: ContagionBadgeProps) {
  const t = useT();
  if (!kind) return null;
  const bg = BG[kind];
  return (
    <span
      className="contagion-badge"
      data-testid={`contagion-badge-${kind}`}
      style={{
        background: bg,
        color: '#fff',
        fontWeight: 700,
        padding: '2px 6px',
        borderRadius: 4,
        marginLeft: 4,
        fontSize: 11,
      }}
      title={`${t('werewolf.contagion.infected' as TKey)} · ${kind}${strength ? ` (${(strength * 100).toFixed(0)}%)` : ''}`}
    >
      {EMOJI[kind]} {t('werewolf.contagion.infected' as TKey)}
    </span>
  );
}
