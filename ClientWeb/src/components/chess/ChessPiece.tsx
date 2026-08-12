import { useState } from 'react';
import {
  PIECE_CHARS,
  STYLE_COLORS,
  getPieceImg,
  type StyleKey,
  type PieceColor,
  type PieceType,
} from '@/assets/images/chess';

interface Props {
  color: PieceColor;
  type: PieceType;
  style: StyleKey;
  selected?: boolean;
  size?: number;
}

/**
 * Renders a single Chess piece. Tries to load the SVG/AI image first;
 * falls back to a CSS circle with the Unicode chess glyph.
 */
export function ChessPiece({ color, type, style: boardStyle, selected, size = 48 }: Props) {
  const [imgFailed, setImgFailed] = useState(false);
  const src = getPieceImg(boardStyle, color, type);
  const colors = STYLE_COLORS[boardStyle];
  const char = PIECE_CHARS[color][type];
  const bgColor = color === 'white' ? colors.whitePiece : colors.blackPiece;
  const borderColor = selected
    ? '#ffd700'
    : color === 'white'
      ? colors.whitePieceBorder
      : colors.blackPieceBorder;
  const isCyber = boardStyle === 'cyberpunk';

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
      : isCyber
        ? `0 0 12px ${color === 'white' ? 'rgba(34,211,238,0.7)' : 'rgba(236,72,153,0.7)'}`
        : `0 2px 6px rgba(0,0,0,0.3)`,
  };

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
        fontSize: size * 0.55,
        fontWeight: 'bold',
        fontFamily: isCyber ? 'monospace' : 'serif',
        textShadow: isCyber
          ? `0 0 4px ${colors.text}`
          : '1px 1px 2px rgba(0,0,0,0.5)',
      }}
    >
      {char}
    </div>
  );
}
