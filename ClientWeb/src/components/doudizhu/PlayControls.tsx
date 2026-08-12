import { useT } from '@/hooks/useT';
import type { TKey } from '@/i18n';

interface Props {
  isMyTurn: boolean;
  canPass: boolean; // true 时可过（非自由出牌）
  isOver: boolean;
  gameOver: { winner: string; reason: string } | null;
  onPlay: () => void;
  onPass: () => void;
}

/**
 * PlayControls — 出牌区操作按钮。
 * 轮到自己时显示：出牌 / 过（要不起）。
 * 对局结束时显示胜负结果。
 */
export function PlayControls({ isMyTurn, canPass, isOver, gameOver, onPlay, onPass }: Props) {
  const t = useT();

  if (isOver && gameOver) {
    const winLabel =
      gameOver.winner === 'landlord'
        ? t('doudizhu.landlordWin' as TKey)
        : t('doudizhu.farmerWin' as TKey);
    return (
      <div className="doudizhu-play-controls game-over">
        <div className="result-banner">{winLabel}</div>
      </div>
    );
  }

  if (!isMyTurn) {
    return (
      <div className="doudizhu-play-controls">
        <div className="turn-hint">{t('doudizhu.opponentTurn' as TKey)}</div>
      </div>
    );
  }

  return (
    <div className="doudizhu-play-controls">
      <button className="btn btn-primary" onClick={onPlay}>
        {t('doudizhu.play' as TKey)}
      </button>
      {canPass && (
        <button className="btn btn-ghost" onClick={onPass}>
          {t('doudizhu.pass' as TKey)}
        </button>
      )}
    </div>
  );
}
