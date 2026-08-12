import { useT } from '@/hooks/useT';
import type { ChessPromotion } from '@/types/api';

interface Props {
  onSelect: (choice: ChessPromotion) => void;
  onCancel?: () => void;
}

/**
 * Modal dialog for choosing a pawn promotion piece (Q/R/B/N).
 * Used when a pawn reaches the back rank and the player must decide which
 * piece to promote to.
 */
export function PromotionDialog({ onSelect, onCancel }: Props) {
  const t = useT();
  const choices: { key: ChessPromotion; label: string; ch: string }[] = [
    { key: 'queen', label: t('chess.promotion.queen'), ch: '♕' },
    { key: 'rook', label: t('chess.promotion.rook'), ch: '♖' },
    { key: 'bishop', label: t('chess.promotion.bishop'), ch: '♗' },
    { key: 'knight', label: t('chess.promotion.knight'), ch: '♘' },
  ];

  return (
    <div className="modal-overlay">
      <div
        className="modal"
        style={{
          background: 'var(--panel, #161b22)',
          padding: 24,
          borderRadius: 12,
          minWidth: 320,
          boxShadow: '0 8px 32px rgba(0,0,0,0.4)',
        }}
      >
        <h3 style={{ marginTop: 0 }}>{t('chess.promotion.title')}</h3>
        <p style={{ opacity: 0.8 }}>{t('chess.promotion.prompt')}</p>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '1fr 1fr',
            gap: 12,
            marginTop: 12,
          }}
        >
          {choices.map((c) => (
            <button
              key={c.key}
              className="btn"
              onClick={() => onSelect(c.key)}
              style={{
                padding: 16,
                fontSize: 24,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: 8,
              }}
            >
              <span style={{ fontSize: 32 }}>{c.ch}</span>
              <span>{c.label}</span>
            </button>
          ))}
        </div>
        {onCancel && (
          <button
            className="btn btn-ghost"
            onClick={onCancel}
            style={{ marginTop: 16, width: '100%' }}
          >
            {t('header.collapse')}
          </button>
        )}
      </div>
    </div>
  );
}
