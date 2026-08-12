import React from 'react';
import { useWerewolfStore } from '@/store/werewolf.store';
import { useT } from '@/hooks/useT';

/**
 * §20260811-09 U1 — 观战模式 AI 实时解说席面板。
 *
 * 仅观战者路由(`/spectate/`)渲染。读取 werewolfStore.commentaryFeed(由
 * useWerewolf 在 game.state.commentary_feed 一次灌入 + chat.commentary
 * 增量 pushCommentary(seq 去重)维护),逐条渲染风格 badge + 文本。
 *
 * §119 协议层隔离:本组件只接收 narration 文本(不含任何身份/夜间行动);
 * 上帝视角数据走 server-side snapshot → LLM prompt,**绝不**进入本组件。
 */
export const CommentaryPanel: React.FC<{ className?: string }> = ({ className }) => {
  const t = useT();
  const feed = useWerewolfStore((s) => s.commentaryFeed);
  return (
    <div className={`ww-commentary-panel ${className ?? ''}`}>
      <div className="ww-commentary-panel__header">{t('werewolf.commentary.title')}</div>
      {feed.length === 0 ? (
        <div className="ww-commentary-panel__empty">
          {t('werewolf.commentary.empty')}
        </div>
      ) : (
        feed.map((line) => (
          <div
            key={line.seq}
            className={`ww-commentary-panel__line ww-commentary-panel__line--${line.style}`}
            data-testid={`ww-commentary-line-${line.seq}`}
          >
            <span className="ww-commentary-panel__style">
              {line.style === 'pro'
                ? t('werewolf.commentary.stylePro')
                : t('werewolf.commentary.styleFun')}
            </span>
            <span className="ww-commentary-panel__text">{line.text}</span>
          </div>
        ))
      )}
    </div>
  );
};