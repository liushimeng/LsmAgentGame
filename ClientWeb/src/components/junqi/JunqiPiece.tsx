import { useState } from 'react';
import {
  PIECE_NAMES,
  STYLE_COLORS,
  getPieceImg,
  type StyleKey,
  type PieceColor,
  type PieceType,
} from '@/assets/images/junqi';

interface Props {
  color: PieceColor;
  type: PieceType;
  style: StyleKey;
  selected?: boolean;
  size?: number;
  hidden?: boolean;
}

/**
 * Renders a single Junqi piece. Tries to load the AI-generated image first;
 * falls back to a CSS circle with the Chinese character.
 *
 * In dark (暗棋) mode, the opponent's pieces are not shown at all — the
 * board will pass `hidden=true` and render a "?" placeholder instead.
 */
export function JunqiPiece({
  color,
  type,
  style: boardStyle,
  selected,
  size = 44,
  hidden,
}: Props) {
  const [imgFailed, setImgFailed] = useState(false);
  const src = getPieceImg(boardStyle, color, type);
  const colors = STYLE_COLORS[boardStyle];
  const char = hidden ? '?' : PIECE_NAMES[type];
  const bgColor = color === 'red' ? colors.redPiece : colors.blackPiece;
  const borderColor = selected
    ? '#ffd700'
    : color === 'red'
      ? colors.redPieceBorder
      : colors.blackPieceBorder;

  const wrapperStyle: React.CSSProperties = {
    width: size,
    height: size,
    borderRadius: '50%',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    position: 'relative',
    cursor: 'pointer',
    userSelect: 'none',
    transition: 'transform 0.1s, box-shadow 0.15s',
    transform: selected ? 'scale(1.12)' : 'scale(1)',
    boxShadow: selected
      ? `0 0 12px 3px rgba(255,215,0,0.6)`
      : `0 2px 6px rgba(0,0,0,0.3)`,
  };

  if (hidden) {
    return (
      <div
        style={{
          ...wrapperStyle,
          background: 'repeating-linear-gradient(45deg, #4b5563, #4b5563 4px, #374151 4px, #374151 8px)',
          border: `3px dashed ${borderColor}`,
          color: '#fde68a',
          fontSize: size * 0.6,
          fontWeight: 'bold',
        }}
      >
        {char}
      </div>
    );
  }

  if (src && !imgFailed) {
    return (
      <div style={wrapperStyle}>
        <img
          src={src}
          alt={char}
          onError={() => setImgFailed(true)}
          style={{
            width: '100%',
            height: '100%',
            borderRadius: '50%',
            objectFit: 'cover',
          }}
        />
      </div>
    );
  }

  return (
    <div
      style={{
        ...wrapperStyle,
        background: `radial-gradient(circle at 35% 35%, ${bgColor}ee, ${bgColor})`,
        border: `3px solid ${borderColor}`,
        color: colors.text,
        fontSize: size * 0.42,
        fontWeight: 'bold',
        fontFamily: '"KaiTi", "STKaiti", serif',
        textShadow: '1px 1px 2px rgba(0,0,0,0.5)',
      }}
    >
      {char}
    </div>
  );
}