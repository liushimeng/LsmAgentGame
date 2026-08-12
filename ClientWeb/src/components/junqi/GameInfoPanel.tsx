import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { useJunqiStore, type BoardStyle } from '@/store/junqi.store';
import { RulesButton } from '@/components/rules/RulesButton';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { TKey } from '@/i18n';

interface Props {
  myColor: 'red' | 'black' | null;
  /** When true, hides the resign action and shows a "观战中" badge instead of
   *  the player "you" chip. */
  spectator?: boolean;
  onResign: () => void;
  onLeave: () => void;
}

export function GameInfoPanel({ myColor, spectator, onResign, onLeave }: Props) {
  const t = useT();
  const { gameState, style, setStyle, gameOver } = useJunqiStore();
  const [confirmLeave, setConfirmLeave] = useState(false);

  if (!gameState) {
    return (
      <div className="game-info-panel">
        <div className="panel-section">
          <p>{t('junqi.waiting' as TKey)}</p>
        </div>
        <StyleSwitcher style={style} setStyle={setStyle} />
        <div className="panel-section">
          <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
            {t('junqi.exitRoom' as TKey)}
          </button>
        </div>
      </div>
    );
  }

  const isMyTurn = gameState.turn === myColor;
  const phaseLabel =
    gameState.phase === 'layout'
      ? t('junqi.phaseLayout' as TKey)
      : isMyTurn
        ? t('junqi.yourTurn' as TKey)
        : t('junqi.opponentTurn' as TKey);

  return (
    <div className="game-info-panel">
      <div className="panel-section">
        <div className={`player-info ${gameState.turn === 'red' ? 'active' : ''}`}>
          <span className="dot red" />
          <span>{t('junqi.red' as TKey)}: {gameState.red_id?.slice(0, 8) || '...'}</span>
          {myColor === 'red' && <span className="badge">{t('junqi.you' as TKey)}</span>}
        </div>
        <div className={`player-info ${gameState.turn === 'black' ? 'active' : ''}`}>
          <span className="dot black" />
          <span>{t('junqi.black' as TKey)}: {gameState.black_id?.slice(0, 8) || '...'}</span>
          {myColor === 'black' && <span className="badge">{t('junqi.you' as TKey)}</span>}
        </div>
      </div>

      <div className="panel-section">
        <div className="status">{phaseLabel}</div>
        <div className="move-count">
          {t('junqi.moveCount' as TKey)}: {gameState.move_count ?? 0}
        </div>
      </div>

      <StyleSwitcher style={style} setStyle={setStyle} />

      <div className="panel-section actions">
        {spectator ? (
          <div className="spectator-badge">
            <span className="dot" /> {t('junqi.spectating' as TKey)}
          </div>
        ) : gameOver ? (
          <div className="game-over-msg">
            <p>
              {gameOver.winner === myColor
                ? t('junqi.youWin' as TKey)
                : t('junqi.youLose' as TKey)}
            </p>
            <p className="reason">{gameOver.reason}</p>
          </div>
        ) : (
          gameState.phase === 'playing' && gameState.status === 'playing' && (
            <button className="btn btn-danger" onClick={onResign}>
              {t('junqi.resign' as TKey)}
            </button>
          )
        )}
        <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
          {t('spectator.exitRoom')}
        </button>
        <RulesButton kind="junqi" />
      </div>

      {confirmLeave && (
        <ConfirmModal
          messageKey={'junqi.confirmLeave' as TKey}
          danger
          onConfirm={() => { setConfirmLeave(false); onLeave(); }}
          onCancel={() => setConfirmLeave(false)}
        />
      )}
    </div>
  );
}

function StyleSwitcher({
  style,
  setStyle,
}: {
  style: BoardStyle;
  setStyle: (s: BoardStyle) => void;
}) {
  const t = useT();
  return (
    <div className="panel-section style-switcher">
      <span className="label">{t('junqi.style' as TKey)}:</span>
      <button
        className={`btn btn-sm ${style === 'naruto' ? 'btn-primary' : 'btn-ghost'}`}
        onClick={() => setStyle('naruto')}
      >
        🍥 {t('junqi.styleNaruto' as TKey)}
      </button>
      <button
        className={`btn btn-sm ${style === 'anti_japanese' ? 'btn-primary' : 'btn-ghost'}`}
        onClick={() => setStyle('anti_japanese')}
      >
        ⭐ {t('junqi.styleAntiJapanese' as TKey)}
      </button>
    </div>
  );
}