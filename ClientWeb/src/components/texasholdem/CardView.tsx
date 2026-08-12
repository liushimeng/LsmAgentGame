import { useState } from 'react';
import { RANK_LABELS, SUIT_LABELS, isRedSuit, type TexasCard } from '@/types/texasholdem';
import { cardImg, STYLE_COLORS, type StyleKey } from '@/assets/images/texasholdem';

interface Props {
  card: TexasCard;
  style: StyleKey;
  faceDown?: boolean;
  small?: boolean;
  onClick?: () => void;
}

export function TexasCardView({ card, style, faceDown, small, onClick }: Props) {
  const [imgFailed, setImgFailed] = useState(false);
  const colors = STYLE_COLORS[style];
  const src = faceDown ? undefined : cardImg(style, card.rank, card.suit);
  const rankLabel = RANK_LABELS[card.rank] ?? '?';
  const suitLabel = SUIT_LABELS[card.suit] ?? '';
  const red = isRedSuit(card.suit);
  const textColor = red ? colors.textRed : colors.textBlack;

  const className = [
    'texas-card',
    small ? 'card-small' : '',
    faceDown ? 'face-down' : '',
  ].filter(Boolean).join(' ');

  const cardStyle: React.CSSProperties = {
    backgroundColor: faceDown ? colors.cardBack : colors.cardBg,
    borderColor: colors.cardBorder,
  };

  if (faceDown) {
    return (
      <div className={className} style={cardStyle} onClick={onClick}>
        <div className="card-back-pattern" />
      </div>
    );
  }

  if (src && !imgFailed) {
    return (
      <div className={className} style={cardStyle} onClick={onClick}>
        <img
          src={src}
          alt={rankLabel + suitLabel}
          draggable={false}
          onError={() => setImgFailed(true)}
          className="card-png"
        />
      </div>
    );
  }

  return (
    <div className={className} style={cardStyle} onClick={onClick}>
      <div className="card-css" style={{ color: textColor }}>
        <div className="card-rank">{rankLabel}</div>
        <div className="card-suit">{suitLabel}</div>
      </div>
    </div>
  );
}

export function TexasCardBack({ style, small }: { style: StyleKey; small?: boolean }) {
  const colors = STYLE_COLORS[style];
  const className = `texas-card ${small ? 'card-small' : ''} face-down`;
  return (
    <div className={className} style={{ backgroundColor: colors.cardBack }}>
      <div className="card-back-pattern" />
    </div>
  );
}
