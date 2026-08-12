/**
 * International chess (Western chess) asset paths for both styles.
 *
 * Two styles are provided:
 *   - european : European King/Knight style (warm classical)
 *   - cyberpunk: Cyberpunk style (neon glow)
 *
 * Components fall back to CSS/SVG rendering when AI-generated images are not
 * yet available, so the game remains playable without external assets.
 */

export type StyleKey = 'european' | 'cyberpunk';
export type PieceColor = 'white' | 'black';
export type PieceType = 'king' | 'queen' | 'rook' | 'bishop' | 'knight' | 'pawn';

// Piece characters for CSS fallback (Unicode chess glyphs).
export const PIECE_CHARS: Record<PieceColor, Record<PieceType, string>> = {
  white: {
    king: '♔', queen: '♕', rook: '♖',
    bishop: '♗', knight: '♘', pawn: '♙',
  },
  black: {
    king: '♚', queen: '♛', rook: '♜',
    bishop: '♝', knight: '♞', pawn: '♟',
  },
};

// Style-specific CSS color palettes for the fallback rendering.
export const STYLE_COLORS: Record<StyleKey, {
  boardLight: string;
  boardDark: string;
  boardLine: string;
  whitePiece: string;
  whitePieceBorder: string;
  blackPiece: string;
  blackPieceBorder: string;
  text: string;
}> = {
  european: {
    boardLight: '#f0d9b0',
    boardDark: '#8b5a2b',
    boardLine: '#3d2817',
    whitePiece: '#fdfbf3',
    whitePieceBorder: '#3d2817',
    blackPiece: '#1a1a1a',
    blackPieceBorder: '#d4af37',
    text: '#3d2817',
  },
  cyberpunk: {
    boardLight: '#0f172a',
    boardDark: '#1e1b4b',
    boardLine: '#22d3ee',
    whitePiece: '#e2e8f0',
    whitePieceBorder: '#22d3ee',
    blackPiece: '#0c0a3e',
    blackPieceBorder: '#ec4899',
    text: '#22d3ee',
  },
};

// Resolve SVG image path via Vite asset handling.
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
