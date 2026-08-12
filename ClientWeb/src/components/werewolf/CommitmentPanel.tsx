// CommitmentPanel.tsx — 行为承诺面板（§20260810-06）。
//
// 显示当前房间的承诺列表（按视角脱敏），白天发言阶段展开。
// 玩家可见：自己的承诺（含真实状态）+ 他人的 pending 承诺。
// 观战者可见：全部承诺的真实状态。

import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { useI18nStore } from '@/store/i18n.store';
import type { CommitmentJSON } from '../../types/werewolf';

interface CommitmentPanelProps {
  commitments: CommitmentJSON[];
  mySeat: number;
  isSpectator: boolean;
}

const TEMPLATE_LABELS: Record<string, Record<string, string>> = {
  seer_check: { 'zh-CN': '🔮 查验承诺', en: '🔮 Seer Check', ja: '🔮 占い承诺' },
  vote_target: { 'zh-CN': '🗳️ 投票承诺', en: '🗳️ Vote Target', ja: '🗳️ 投票承诺' },
  no_vote_for: { 'zh-CN': '🚫 不投承诺', en: '🚫 No Vote', ja: '🚫 不投票承诺' },
  no_use_skill: { 'zh-CN': '🛡️ 不用技能', en: '🛡️ No Skill', ja: '🛡️ 技能不使用' },
  apology_if_good: { 'zh-CN': '🙏 道歉承诺', en: '🙏 Apology', ja: '🙏 謝罪承诺' },
};

const STATUS_LABELS: Record<string, Record<string, string>> = {
  pending: { 'zh-CN': '⏳ 待验证', en: '⏳ Pending', ja: '⏳ 検証待ち' },
  fulfilled: { 'zh-CN': '✅ 已兑现', en: '✅ Fulfilled', ja: '✅ 達成' },
  broken: { 'zh-CN': '❌ 已违背', en: '❌ Broken', ja: '❌ 違反' },
  expired: { 'zh-CN': '⏰ 已过期', en: '⏰ Expired', ja: '⏰ 期限切れ' },
};

const STATUS_COLORS: Record<string, string> = {
  pending: '#f59e0b',
  fulfilled: '#22c55e',
  broken: '#ef4444',
  expired: '#6b7280',
};

export function CommitmentPanel({ commitments, mySeat, isSpectator }: CommitmentPanelProps) {
  const t = useT();
  const lang = useI18nStore((s) => s.lang);
  const [expanded, setExpanded] = useState(false);

  if (commitments.length === 0) {
    return null;
  }

  const myCommitments = commitments.filter(c => c.seat === mySeat);
  const otherCommitments = commitments.filter(c => c.seat !== mySeat);

  const getTemplateLabel = (template: string) => {
    const labels = TEMPLATE_LABELS[template];
    return labels ? labels[lang] || labels['zh-CN'] : template;
  };

  const getStatusLabel = (status: string) => {
    const labels = STATUS_LABELS[status];
    return labels ? labels[lang] || labels['zh-CN'] : status;
  };

  const getStatusColor = (status: string) => {
    return STATUS_COLORS[status] || '#6b7280';
  };

  return (
    <div className="commitment-panel">
      <button
        className="commitment-panel__toggle"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
      >
        📝 {t('werewolf.commitment.title')} ({commitments.length})
        <span className="commitment-panel__arrow">{expanded ? '▼' : '▶'}</span>
      </button>

      {expanded && (
        <div className="commitment-panel__content">
          {myCommitments.length > 0 && (
            <div className="commitment-panel__section">
              <h4 className="commitment-panel__section-title">{t('werewolf.commitment.my')}</h4>
              {myCommitments.map(c => (
                <div key={c.id} className="commitment-item commitment-item--mine">
                  <div className="commitment-item__header">
                    <span className="commitment-item__template">{getTemplateLabel(c.template)}</span>
                    <span
                      className="commitment-item__status"
                      style={{ color: getStatusColor(c.status) }}
                    >
                      {getStatusLabel(c.status)}
                    </span>
                  </div>
                  {c.param_seat >= 0 && (
                    <div className="commitment-item__target">
                      {t('werewolf.commitment.target')}: {c.param_seat + 1}{t('werewolf.commitment.seatSuffix')}
                    </div>
                  )}
                  {c.reason && (
                    <div className="commitment-item__reason">「{c.reason}」</div>
                  )}
                  <div className="commitment-item__meta">
                    {t('werewolf.commitment.day')} {c.round} {t('werewolf.commitment.daySuffix')}
                  </div>
                </div>
              ))}
            </div>
          )}

          {otherCommitments.length > 0 && (
            <div className="commitment-panel__section">
              <h4 className="commitment-panel__section-title">
                {isSpectator ? t('werewolf.commitment.all') : t('werewolf.commitment.other')}
              </h4>
              {otherCommitments.map(c => (
                <div key={c.id} className="commitment-item">
                  <div className="commitment-item__header">
                    <span className="commitment-item__seat">{c.seat + 1}{t('werewolf.commitment.seatSuffix')}</span>
                    <span className="commitment-item__template">{getTemplateLabel(c.template)}</span>
                    <span
                      className="commitment-item__status"
                      style={{ color: getStatusColor(c.status) }}
                    >
                      {getStatusLabel(c.status)}
                    </span>
                  </div>
                  {c.param_seat >= 0 && (
                    <div className="commitment-item__target">
                      {t('werewolf.commitment.target')}: {c.param_seat + 1}{t('werewolf.commitment.seatSuffix')}
                    </div>
                  )}
                  {c.reason && (
                    <div className="commitment-item__reason">「{c.reason}」</div>
                  )}
                  <div className="commitment-item__meta">
                    {t('werewolf.commitment.day')} {c.round} {t('werewolf.commitment.daySuffix')}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
