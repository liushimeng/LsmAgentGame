/**
 * Xiangqi asset paths for both styles.
 *
 * When AI-generated images are not yet available, the board and piece
 * components fall back to CSS/SVG rendering so the game is playable
 * without external assets.
 */

export type StyleKey = 'warring' | 'robot';
export type PieceColor = 'red' | 'black';
export type PieceType = 'king' | 'advisor' | 'elephant' | 'horse' | 'chariot' | 'cannon' | 'soldier';

// Piece characters for CSS fallback
export const PIECE_CHARS: Record<PieceColor, Record<PieceType, string>> = {
  red: {
    king: '帅', advisor: '仕', elephant: '相',
    horse: '马', chariot: '车', cannon: '炮', soldier: '兵',
  },
  black: {
    king: '将', advisor: '士', elephant: '象',
    horse: '马', chariot: '车', cannon: '炮', soldier: '卒',
  },
};

// Style-specific CSS color palettes for the fallback rendering
export const STYLE_COLORS: Record<StyleKey, {
  board: string;
  boardLine: string;
  boardBg: string;
  redPiece: string;
  redPieceBorder: string;
  blackPiece: string;
  blackPieceBorder: string;
  text: string;
}> = {
  warring: {
    board: '#c4a56f',
    boardLine: '#5c3d1a',
    boardBg: '#f0d9a0',
    redPiece: '#8b2020',
    redPieceBorder: '#d4a030',
    blackPiece: '#1a1a2e',
    blackPieceBorder: '#7a6f4f',
    text: '#f5e6c8',
  },
  robot: {
    board: '#1e293b',
    boardLine: '#38bdf8',
    boardBg: '#0f172a',
    redPiece: '#dc2626',
    redPieceBorder: '#f97316',
    blackPiece: '#1e40af',
    blackPieceBorder: '#06b6d4',
    text: '#e0f2fe',
  },
};

// Resolve SVG image path via Vite asset handling
function imgPath(style: StyleKey, name: string): string {
  try {
    return new URL(`./${style}/${name}.svg`, import.meta.url).href;
  } catch {
    return '';
  }
}

export function getBoardBg(style: StyleKey): string {
  return imgPath(style, 'board_bg');
}

export function getPieceImg(style: StyleKey, color: PieceColor, type: PieceType): string {
  return imgPath(style, `${color}_${type}`);
}
