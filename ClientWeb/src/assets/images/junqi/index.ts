/**
 * Junqi (中国军棋 / 陆战棋) asset paths for both themes.
 *
 * Two themes are supported:
 *   - 'naruto'         — 火影忍者风格 (anime ninja aesthetic)
 *   - 'anti_japanese'  — 抗日战争风格 (1937-1945 military aesthetic)
 *
 * When AI-generated images are not yet available, the board and piece
 * components fall back to CSS/SVG rendering so the game is playable
 * without external assets.
 */

export type StyleKey = 'naruto' | 'anti_japanese';
export type PieceColor = 'red' | 'black';
export type PieceType =
  | 'flag' | 'commander' | 'general' | 'major' | 'colonel'
  | 'captain' | 'lieutenant' | 'sergeant' | 'engineer' | 'bomb' | 'mine';

// Piece display names for UI (Chinese characters).
export const PIECE_NAMES: Record<PieceType, string> = {
  flag: '军旗',
  commander: '司令',
  general: '军长',
  major: '师长',
  colonel: '旅长',
  captain: '团长',
  lieutenant: '营长',
  sergeant: '连排长',
  engineer: '工兵',
  bomb: '炸弹',
  mine: '地雷',
};

// Style-specific CSS color palettes for the fallback rendering.
export const STYLE_COLORS: Record<StyleKey, {
  board: string;
  boardLine: string;
  boardBg: string;
  railLine: string;
  camp: string;
  hq: string;
  mountain: string;
  redPiece: string;
  redPieceBorder: string;
  blackPiece: string;
  blackPieceBorder: string;
  text: string;
}> = {
  naruto: {
    board: '#fef3c7',           // warm sand
    boardLine: '#92400e',       // brown ink
    boardBg: '#fffbeb',
    railLine: '#451a03',
    camp: '#fbbf24',
    hq: '#dc2626',
    mountain: '#fb923c',
    redPiece: '#dc2626',
    redPieceBorder: '#f59e0b',
    blackPiece: '#1e293b',
    blackPieceBorder: '#0ea5e9',
    text: '#fef3c7',
  },
  anti_japanese: {
    board: '#5d4e3a',           // vintage military tan
    boardLine: '#2d1f0e',       // dark ink
    boardBg: '#7a6849',
    railLine: '#1f1306',
    camp: '#3f3517',
    hq: '#7c1818',
    mountain: '#3d2814',
    redPiece: '#7c1818',        // 八路军红
    redPieceBorder: '#a3a3a3',
    blackPiece: '#3a3a3a',      // 日军灰
    blackPieceBorder: '#a3a3a3',
    text: '#f5e6c8',
  },
};

// Resolve SVG image path via Vite asset handling.
// Returns empty string when the file doesn't exist — callers fall back to CSS.
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