// ClientWeb/src/components/werewolf/BattleReportHighlights.tsx
// §20260811-07 U2 — 自动高光集锦战报卡片组件。
//
// 数据来源:`game.state.battle_report_highlights[]`(终局时由后端 view.go 下发)。
// 视觉:3 张大卡片(顶部)+ 折叠展开剩余;每张卡片含 icon / title / quote / 「📋 原始素材」展开。
//
// 风险:
//   §119 协议层隔离:本组件仅读服务端 BattleReportHighlights(已在 view.go 末段填充);
//   §135 身份公开:战报数据来自终局时刻,身份已经公开(§135 白名单第 1 条);
//   §122 限流:无额外 LLM 调用,纯前端展示。

import { useState } from 'react';
import { useT } from '@/hooks/useT';

export interface BattleReportHighlight {
  kind: string;
  seat: number;
  round: number;
  quote: string;
  source_data: string;
}

interface BattleReportHighlightsProps {
  /** 后端下发的 BattleReportHighlights 数组(≤5 条,FIFO 16 → 截断后 5)。 */
  highlights: BattleReportHighlight[];
}

const KIND_ICON: Record<string, string> = {
  guardian_shield: '🛡️',
  witch_save: '🧪',
  witch_poison_wolf: '☠️',
  close_vote: '⚖️',
  hunter_kill_wolf: '🔫',
  wolf_suicide: '💥',
  // §20260830-02 — 自爆带走
  suicide_take: '🧨',
};

const KIND_COLOR: Record<string, string> = {
  guardian_shield: 'battle-report--guard',
  witch_save: 'battle-report--witch',
  witch_poison_wolf: 'battle-report--witch',
  close_vote: 'battle-report--vote',
  hunter_kill_wolf: 'battle-report--hunter',
  wolf_suicide: 'battle-report--wolf',
  // §20260830-02 — 自爆带走(红色系与 wolf_suicide 同档)
  suicide_take: 'battle-report--wolf',
};

export function BattleReportHighlights({ highlights }: BattleReportHighlightsProps) {
  const t = useT();
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);
  const [showRest, setShowRest] = useState(false);

  if (!highlights || highlights.length === 0) {
    return null;
  }

  const top = highlights.slice(0, 3);
  const rest = highlights.slice(3);

  const renderCard = (h: BattleReportHighlight, idx: number, isTop: boolean) => {
    const icon = KIND_ICON[h.kind] ?? '✨';
    const colorClass = KIND_COLOR[h.kind] ?? '';
    const titleKey = `werewolf.battleReport.${h.kind}` as const;
    const isExpanded = expandedIdx === idx;
    return (
      <div
        key={`${h.kind}-${h.seat}-${h.round}-${idx}`}
        className={`battle-report-card ${colorClass} ${isTop ? 'battle-report-card--top' : ''}`}
        data-testid={`battle-report-card-${idx}`}
      >
        <div className="battle-report-card__icon" aria-hidden>{icon}</div>
        <div className="battle-report-card__body">
          <div className="battle-report-card__title">
            {t(titleKey as any)}
          </div>
          <div className="battle-report-card__meta">
            {t('werewolf.battleReport.seat' as any).replace('{n}', String(h.seat + 1))}
            {' · '}
            {t('werewolf.battleReport.round' as any).replace('{n}', String(h.round))}
          </div>
          {h.quote && (
            <div className="battle-report-card__quote">"{h.quote}"</div>
          )}
          {h.source_data && (
            <button
              type="button"
              className="battle-report-card__toggle"
              onClick={() => setExpandedIdx(isExpanded ? null : idx)}
              aria-expanded={isExpanded}
            >
              {isExpanded
                ? t('werewolf.battleReport.hideSource' as any)
                : t('werewolf.battleReport.showSource' as any)}
            </button>
          )}
          {isExpanded && h.source_data && (
            <div className="battle-report-card__source">
              📋 {h.source_data}
            </div>
          )}
        </div>
      </div>
    );
  };

  return (
    <div className="battle-report" data-testid="battle-report">
      <div className="battle-report__header">
        <h3 className="battle-report__title">
          🏆 {t('werewolf.battleReport.title' as any)}
        </h3>
      </div>
      <div className="battle-report__grid battle-report__grid--top">
        {top.map((h, i) => renderCard(h, i, true))}
      </div>
      {rest.length > 0 && (
        <>
          <button
            type="button"
            className="battle-report__show-rest"
            onClick={() => setShowRest((v) => !v)}
            aria-expanded={showRest}
          >
            {showRest
              ? t('werewolf.battleReport.collapseRest' as any)
              : t('werewolf.battleReport.expandRest' as any).replace('{n}', String(rest.length))}
          </button>
          {showRest && (
            <div className="battle-report__grid battle-report__grid--rest">
              {rest.map((h, i) => renderCard(h, i + top.length, false))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
