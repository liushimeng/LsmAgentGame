import { useState } from 'react';
import { useT } from '@/hooks/useT';
import { useChessStore, type ChessBoardStyle } from '@/store/chess.store';
import { RulesButton } from '@/components/rules/RulesButton';
import { ConfirmModal } from '@/components/ui/ConfirmModal';
import type { TKey } from '@/i18n';
import type { PieceColor } from '@/assets/images/chess';

interface Props {
  myColor: PieceColor | null;
  /** When true, hides the resign action and shows a "观战中" badge instead of
   *  the player "you" chip. */
  spectator?: boolean;
  onResign: () => void;
  onLeave: () => void;
}

export function ChessGameInfoPanel({ myColor, spectator, onResign, onLeave }: Props) {
  const t = useT();
  const { gameState, style, setStyle, gameOver, lastMove } = useChessStore();
  const [confirmLeave, setConfirmLeave] = useState(false);

  if (!gameState) {
    return (
      <div className="game-info-panel">
        <div className="panel-section">
          <p>{t('chess.waitingOpponent')}</p>
        </div>
        <StyleSwitcher style={style} setStyle={setStyle} t={t} />
        <div className="panel-section">
          <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
            {t('chess.exitRoom')}
          </button>
        </div>
      </div>
    );
  }

  const isMyTurn = gameState.turn === myColor;
  const statusMap: Record<string, TKey> = {
    playing_white: 'chess.yourTurn',
    playing_black: 'chess.opponentTurn',
    white_win: 'chess.whiteWin',
    black_win: 'chess.blackWin',
    draw: 'chess.draw',
  };
  const statusKey = gameState.status === 'playing'
    ? (isMyTurn ? 'playing_white' : 'playing_black')
    : (gameState.status ?? 'draw');
  const statusLabel = t(statusMap[statusKey] ?? 'chess.draw');

  const reasonMap: Record<string, TKey> = {
    checkmate: 'chess.reason.checkmate',
    resign: 'chess.reason.resign',
    stalemate: 'chess.reason.stalemate',
    fifty_move: 'chess.reason.fiftyMove',
    insufficient: 'chess.reason.insufficient',
    threefold: 'chess.reason.threefold',
  };

  const fmtMove = (m: { from: { x: number; y: number }; to: { x: number; y: number } }) => {
    const a = `${String.fromCharCode(97 + m.from.x)}${m.from.y + 1}`;
    const b = `${String.fromCharCode(97 + m.to.x)}${m.to.y + 1}`;
    return `${a} → ${b}`;
  };

  return (
    <div className="game-info-panel">
      {/* Players */}
      <div className="panel-section">
        <div className={`player-info ${gameState.turn === 'white' ? 'active' : ''}`}>
          <span className="dot white" />
          <span>{t('chess.white')}: {gameState.white_id?.slice(0, 8) || '...'}</span>
          {myColor === 'white' && <span className="badge">{t('chess.you')}</span>}
        </div>
        <div className={`player-info ${gameState.turn === 'black' ? 'active' : ''}`}>
          <span className="dot black" />
          <span>{t('chess.black')}: {gameState.black_id?.slice(0, 8) || '...'}</span>
          {myColor === 'black' && <span className="badge">{t('chess.you')}</span>}
        </div>
      </div>

      {/* Status */}
      <div className="panel-section">
        <div className={`status ${gameState.check ? 'check' : ''}`}>
          {gameState.check ? `⚠ ${t('chess.check')}` : statusLabel}
        </div>
        <div className="move-count">
          {t('chess.moveCount', { n: gameState.move_count })}
        </div>
      </div>

      {/* Last move */}
      {lastMove && (
        <div className="panel-section last-move">
          <span className="label">{t('chess.lastMove')}: </span>
          <span>
            {fmtMove(lastMove)}
            {lastMove.promotion && (
              <em>={t(`chess.promotion.${lastMove.promotion}` as TKey)}</em>
            )}
            {lastMove.castle_kind && (
              <em> (O-O{lastMove.castle_kind === 'queen' ? '-O' : ''})</em>
            )}
          </span>
        </div>
      )}

      <StyleSwitcher style={style} setStyle={setStyle} t={t} />

      <div className="panel-section actions">
        {spectator ? (
          <div className="spectator-badge">
            <span className="dot" /> {t('chess.spectating')}
          </div>
        ) : gameOver ? (
          <div className="game-over-msg">
            <p>
              {gameOver.winner === myColor
                ? t('chess.youWin')
                : t('chess.youLose')}
            </p>
            <p className="reason">{t(reasonMap[gameOver.reason] ?? 'chess.draw')}</p>
          </div>
        ) : (
          gameState.status === 'playing' && (
            <button className="btn btn-danger" onClick={onResign}>
              {t('chess.resign')}
            </button>
          )
        )}
        <button className="btn btn-secondary" onClick={() => setConfirmLeave(true)}>
          {t('spectator.exitRoom')}
        </button>
        <RulesButton kind="chess" />
      </div>

      {confirmLeave && (
        <ConfirmModal
          messageKey={'chess.confirmLeave' as TKey}
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
  style: ChessBoardStyle;
  setStyle: (s: ChessBoardStyle) => void;
  t: (key: TKey, vars?: Record<string, string | number>) => string;
}) {
  return (
    <div className="panel-section style-switcher">
      <span className="label">{t('chess.style')}:</span>
      <button
        className={`btn btn-sm ${style === 'european' ? 'btn-primary' : 'btn-ghost'}`}
        onClick={() => setStyle('european')}
      >
        {t('chess.styleEuropean')}
      </button>
      <button
        className={`btn btn-sm ${style === 'cyberpunk' ? 'btn-primary' : 'btn-ghost'}`}
        onClick={() => setStyle('cyberpunk')}
      >
        {t('chess.styleCyberpunk')}
      </button>
    </div>
  );
}
