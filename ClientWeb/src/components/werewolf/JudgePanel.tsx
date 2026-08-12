/**
 * JudgePanel.tsx — 2026-07-10 §123 + §125 增强;2026-07-22 §UX-法官布局重构
 *
 * Agent 法官(主持人)详情面板。
 *
 * 历史:曾内嵌在 WerewolfTable 桌面区顶部。13 人局 4 行座位格被它整体下压,
 * 页面滚动后与游戏界面重合。现改为仅供 HistoryDrawer「⚖️ 法官」tab 使用的
 * 内容面板(不带自己的折叠壳/边框),页面顶部信息由 JudgeActionBar 承载。
 *
 * 数据源:game.state.judge_context(对齐 ServerGo/agent/judge.go::JudgeTranscript)。
 *
 * 渲染内容:
 *  - 状态行(模型 / 就绪·quarantine / 最近 LLM 耗时 / 待宣告)
 *  - 最近宣告全文
 *  - 宣告历史(全部展开列表,倒序)
 *  - 「一举一动」活动流(倒序全列表)
 */

import React from 'react';
import type { JudgeContextJSON } from '../../types/werewolf';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  enabled: boolean;
  context?: JudgeContextJSON;
  pending?: string;
}

export const JudgePanel: React.FC<Props> = ({ enabled, context, pending }) => {
  const t = useT();

  if (!enabled) {
    return (
      <div className="ww-history-panel__empty">{t('werewolf.judge.panel.disabled')}</div>
    );
  }
  if (!context) {
    return (
      <div className="ww-history-panel__empty">{t('werewolf.judge.panel.init')}</div>
    );
  }

  const quarantined = !!context.quarantined;
  const lastText = context.last_announcement || t('werewolf.judge.panel.waitingFirst');
  const modelName = context.model || 'judge';
  const statusEmoji = quarantined ? '⚠️' : '🟢';
  const statusLabel = quarantined
    ? t('werewolf.judge.status.quarantined')
    : t('werewolf.judge.status.ready');

  return (
    <div className="ww-judge-panel">
      {/* 状态行 */}
      <div className="ww-judge-panel__status">
        <span aria-hidden>⚖️</span>
        <span className="ww-judge-panel__model">{modelName}</span>
        <span
          className="ww-judge-panel__statusline"
          title={t('werewolf.judge.statusLine' as TKey, { emoji: statusEmoji, label: statusLabel })}
        >
          {statusEmoji} {statusLabel}
          {!quarantined && context.last_llm_ms ? ` · ${context.last_llm_ms} ms` : ''}
        </span>
        {quarantined && (
          <span className="ww-judge-panel__badge ww-judge-panel__badge--quarantined">
            {t('werewolf.judge.panel.quarantinedBadge')}
          </span>
        )}
        {pending && pending !== '' && (
          <span className="ww-judge-panel__badge ww-judge-panel__badge--pending">
            {t('werewolf.judge.pending')}: {pending}
          </span>
        )}
      </div>

      {/* 最近宣告 */}
      <div className="ww-judge-panel__last">
        <span className="ww-judge-panel__quote-mark">「</span>
        {lastText}
        <span className="ww-judge-panel__quote-mark">」</span>
      </div>

      {/* 宣告历史(倒序) */}
      {context.recent_announcements && context.recent_announcements.length > 1 && (
        <section className="ww-judge-panel__section">
          <h4 className="ww-judge-panel__section-title">{t('werewolf.judge.panel.historyTitle')}</h4>
          <ul className="ww-judge-panel__history">
            {[...context.recent_announcements].reverse().map((text, i) => (
              <li key={i}>
                <span className="ww-judge-panel__bullet">›</span> {text}
              </li>
            ))}
          </ul>
        </section>
      )}

      {/* 一举一动活动流(倒序) */}
      {context.activities && context.activities.length > 0 && (
        <section className="ww-judge-panel__section">
          <h4 className="ww-judge-panel__section-title">{t('werewolf.judge.activity.title')}</h4>
          <ul className="ww-judge-panel__activities">
            {[...context.activities].reverse().map((a, i) => (
              <li key={i}>
                <span className="ww-judge-panel__activity-time">
                  {new Date(a.at).toLocaleTimeString()}
                </span>{' '}
                <span className="ww-judge-panel__activity-tool">⚙️ {a.tool}</span>
                {a.input && <span className="ww-judge-panel__activity-input">({a.input})</span>}
                {a.llm_ms ? (
                  <span className="ww-judge-panel__activity-ms">
                    {' · '}
                    {t('werewolf.judge.activity.llmMs' as TKey, { ms: a.llm_ms })}
                  </span>
                ) : null}
                <div className="ww-judge-panel__activity-out">→ {a.out}</div>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
};

export default JudgePanel;
