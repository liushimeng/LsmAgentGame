import { useState } from 'react';
import type { TexasActionType } from '@/types/texasholdem';
import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  isMyTurn: boolean;
  canCheck: boolean;
  canCall: boolean;
  callAmount: number;
  isOver: boolean;
  gameOver: { winners: number[]; reason: string } | null;
  onAction: (type: TexasActionType, amount?: number) => void;
  bigBlind: number;
  pot: number;
  stack: number;
  /**
   * 2026-08-20 §德州扑克Agent — 当前轮到 bot 思考时,人类按钮整体禁用。
   * 来自 `gameState.bot_thinking[turn]`,在 isMyTurn=false 时用于渲染
   * 「🤖 AI 决策中…」提示(避免人类误以为 UI 卡死)。
   */
  botTurnThinking?: boolean;
}

export function ActionControls({
  isMyTurn,
  canCheck,
  canCall,
  callAmount,
  isOver,
  gameOver,
  onAction,
  bigBlind,
  stack,
  botTurnThinking = false,
}: Props) {
  const t = useT();
  const [betAmount, setBetAmount] = useState(bigBlind);

  if (isOver && gameOver) {
    return (
      <div className="texas-action-controls game-over">
        <div className="game-over-banner">
          {(gameOver.winners ?? []).length > 0
            ? `${t('texasholdem.winners' as TKey)} ${gameOver.winners!.map(w => `#${w}`).join(', ')}`
            : t('texasholdem.splitPot' as TKey)}
        </div>
      </div>
    );
  }

  if (!isMyTurn) {
    // 当前不是玩家回合 —— 若对手是 bot 且正在思考,显式提示玩家不要反复点
    if (botTurnThinking) {
      return (
        <div className="texas-action-controls">
          <div
            className="waiting-turn bot-thinking-hint"
            data-testid="thp-action-bot-thinking"
          >
            {t('texasholdem.bot.actionDisabled' as TKey)}
          </div>
        </div>
      );
    }
    return (
      <div className="texas-action-controls">
        <div className="waiting-turn">{t('texasholdem.opponentTurn' as TKey)}</div>
      </div>
    );
  }

  return (
    <div className="texas-action-controls">
      <button className="btn btn-danger" onClick={() => onAction('fold')}>
        {t('texasholdem.fold' as TKey)}
      </button>
      {canCheck && (
        <button className="btn btn-ghost" onClick={() => onAction('check')}>
          {t('texasholdem.check' as TKey)}
        </button>
      )}
      {canCall && (
        <button className="btn btn-primary" onClick={() => onAction('call')}>
          {t('texasholdem.call' as TKey)} ${callAmount}
        </button>
      )}
      <div className="raise-controls">
        <input
          type="range"
          min={bigBlind}
          max={stack}
          step={bigBlind}
          value={betAmount}
          onChange={(e) => setBetAmount(Number(e.target.value))}
        />
        <span className="raise-value">${betAmount}</span>
        <button
          className="btn btn-ghost"
          onClick={() => onAction(callAmount > 0 ? 'raise' : 'bet', betAmount)}
        >
          {t(callAmount > 0 ? ('texasholdem.raise' as TKey) : ('texasholdem.bet' as TKey))}
        </button>
      </div>
      <button className="btn btn-warning" onClick={() => onAction('allin')}>
        {t('texasholdem.allIn' as TKey)} ${stack}
      </button>
    </div>
  );
}
