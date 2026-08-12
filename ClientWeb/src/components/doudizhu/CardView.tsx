import { useState } from 'react';
import { RANK_LABELS, SUIT_LABELS, isRedSuit, type DoudizhuCard } from '@/types/doudizhu';
import { cardImg, STYLE_COLORS, type StyleKey } from '@/assets/images/doudizhu';

interface Props {
  card: DoudizhuCard;
  style: StyleKey;
  selected?: boolean;
  faceDown?: boolean;
  small?: boolean;
  onClick?: () => void;
}

/**
 * CardView — 单张扑克牌展示。
 * PNG 优先加载，onError 时回退为纯 CSS 文字卡面（点数+花色），
 * 保证无论图片是否就绪都能正常游戏。
 */
export function CardView({ card, style, selected, faceDown, small, onClick }: Props) {
  const [imgFailed, setImgFailed] = useState(false);
  const colors = STYLE_COLORS[style];
  const src = faceDown ? undefined : cardImg(style, card.rank, card.suit);

  const rankLabel = RANK_LABELS[card.rank] ?? '?';
  const suitLabel = SUIT_LABELS[card.suit] ?? '';
  const red = isRedSuit(card.rank, card.suit);
  const textColor = red ? colors.textRed : colors.textBlack;

  const className = [
    'doudizhu-card',
    selected ? 'selected' : '',
    small ? 'card-small' : '',
    faceDown ? 'face-down' : '',
  ]
    .filter(Boolean)
    .join(' ');

  const cardStyle: React.CSSProperties = {
    backgroundColor: faceDown ? colors.cardBack : colors.cardBg,
    borderColor: selected ? '#f59e0b' : colors.cardBorder,
  };

  // 牌背
  if (faceDown) {
    return (
      <div className={className} style={cardStyle} onClick={onClick}>
        <div className="card-back-pattern" />
      </div>
    );
  }

  // PNG 可用：直接 <img>
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

  // 回退：纯 CSS 文字卡面
  return (
    <div className={className} style={cardStyle} onClick={onClick}>
      <div className="card-css" style={{ color: textColor }}>
        <div className="card-rank">{rankLabel}</div>
        <div className="card-suit">{suitLabel}</div>
      </div>
    </div>
  );
}

/** 牌背（背面朝上的牌）。 */
export function CardBack({ style }: { style: StyleKey }) {
  const colors = STYLE_COLORS[style];
  return (
    <div className="doudizhu-card card-small face-down" style={{ backgroundColor: colors.cardBack }}>
      <div className="card-back-pattern" />
    </div>
  );
}
