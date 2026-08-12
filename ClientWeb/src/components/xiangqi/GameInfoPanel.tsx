import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { useXiangqiStore, type BoardStyle } from '@/store/xiangqi.store';
import { RulesButton } from '@/components/rules/RulesButton';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { TKey } from '@/i18n';
import type { PieceColor } from '@/assets/images/xiangqi';

interface Props {
  myColor: PieceColor | null;
  /** When true, the panel hides the resign action and shows a "观战中" badge
   *  instead of "you" chips next to the players. */
  spectator?: boolean;
  onResign: () => void;
  onLeave: () => void;
}

export function GameInfoPanel({ myColor, spectator, onResign, onLeave }: Props) {
  const t = useT();
  const { gameState, style, setStyle, gameOver, lastMove } = useXiangqiStore();
  const [confirmLeave, setConfirmLeave] = useState(false);

  if (!gameState) {
    return (
      <div className="game-info-panel">
        <div className="panel-section">
          <p>{t('xiangqi.waiting')}</p>
        </div>
        <StyleSwitcher style={style} setStyle={setStyle} t={t} />
        <div className="panel-section">
          <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
            {t('xiangqi.exitRoom')}
          </button>
        </div>
      </div>
    );
  }

  const isMyTurn = gameState.turn === myColor;
  const statusMap: Record<string, TKey> = {
    playing_red: 'xiangqi.yourTurn',
    playing_opp: 'xiangqi.opponentTurn',
    red_win: 'xiangqi.redWin',
    black_win: 'xiangqi.blackWin',
    draw: 'xiangqi.draw',
  };
  const statusKey = gameState.status === 'playing'
    ? (isMyTurn ? 'playing_red' : 'playing_opp')
    : (gameState.status ?? 'draw');
  const statusLabel = t(statusMap[statusKey] ?? 'xiangqi.draw');

  return (
    <div className="game-info-panel">
      {/* Players */}
      <div className="panel-section">
        <div className={`player-info ${gameState.turn === 'red' ? 'active' : ''}`}>
          <span className="dot red" />
          <span>{t('xiangqi.red')}: {gameState.red_id?.slice(0, 8) || '...'}</span>
          {myColor === 'red' && <span className="badge">{t('xiangqi.you')}</span>}
        </div>
        <div className={`player-info ${gameState.turn === 'black' ? 'active' : ''}`}>
          <span className="dot black" />
          <span>{t('xiangqi.black')}: {gameState.black_id?.slice(0, 8) || '...'}</span>
          {myColor === 'black' && <span className="badge">{t('xiangqi.you')}</span>}
        </div>
      </div>

      {/* Status */}
      <div className="panel-section">
        <div className={`status ${gameState.check ? 'check' : ''}`}>
          {gameState.check && '⚠ '}
          {statusLabel}
        </div>
        <div className="move-count">
          {t('xiangqi.moveCount')}: {gameState.move_count}
        </div>
      </div>

      {/* Last move */}
      {lastMove && (
        <div className="panel-section last-move">
          <span className="label">{t('xiangqi.lastMove')}:</span>
          <span>
            ({lastMove.from.x},{lastMove.from.y}) → ({lastMove.to.x},{lastMove.to.y})
          </span>
        </div>
      )}

      {/* Style switcher */}
      <StyleSwitcher style={style} setStyle={setStyle} t={t} />

      {/* Actions */}
      <div className="panel-section actions">
        {spectator ? (
          // No move / resign controls for observers. They stay subscribed until
          // they hit the exit-room button.
          <div className="spectator-badge">
            <span className="dot" /> {t('xiangqi.spectating')}
          </div>
        ) : gameOver ? (
          <div className="game-over-msg">
            <p>
              {gameOver.winner === myColor
                ? t('xiangqi.youWin')
                : t('xiangqi.youLose')}
            </p>
            <p className="reason">{gameOver.reason === 'checkmate' ? t('xiangqi.reason.checkmate') : gameOver.reason === 'resign' ? t('xiangqi.reason.resign') : t('xiangqi.reason.stalemate')}</p>
          </div>
        ) : (
          gameState.status === 'playing' && (
            <button className="btn btn-danger" onClick={onResign}>
              {t('xiangqi.resign')}
            </button>
          )
        )}
        <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
          {t('spectator.exitRoom')}
        </button>
        <RulesButton kind="xiangqi" />
      </div>

      {confirmLeave && (
        <ConfirmModal
          messageKey={'xiangqi.confirmLeave' as TKey}
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
  t,
}: {
  style: BoardStyle;
  setStyle: (s: BoardStyle) => void;
  t: (key: TKey, vars?: Record<string, string | number>) => string;
}) {
  return (
    <div className="panel-section style-switcher">
      <span className="label">{t('xiangqi.style')}:</span>
      <button
        className={`btn btn-sm ${style === 'warring' ? 'btn-primary' : 'btn-ghost'}`}
        onClick={() => setStyle('warring')}
      >
        {t('xiangqi.styleWarring')}
      </button>
      <button
        className={`btn btn-sm ${style === 'robot' ? 'btn-primary' : 'btn-ghost'}`}
        onClick={() => setStyle('robot')}
      >
        {t('xiangqi.styleRobot')}
      </button>
    </div>
  );
}
