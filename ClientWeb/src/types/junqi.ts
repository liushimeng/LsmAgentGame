// ─── Junqi (中国军棋) types ───
//
// Mirrors the server-side game/junqi package. Coordinates are (x,y) where:
//   x ∈ [0,4], y ∈ [0,11]
//   Red side: y ∈ [0,5] (home row y=0)
//   Black side: y ∈ [6,11] (home row y=11)

export type JunqiPieceColor = 'red' | 'black';
export type JunqiPieceType =
  | 'flag' | 'commander' | 'general' | 'major' | 'colonel'
  | 'captain' | 'lieutenant' | 'sergeant' | 'engineer' | 'bomb' | 'mine';

export interface JunqiPiece {
  color: JunqiPieceColor;
  type: JunqiPieceType;
  name: string;
}

export interface JunqiHiddenPiece {
  hidden: true;
}

// A cell in the player's view: either fully visible or hidden.
export type JunqiCellView =
  | (JunqiPiece & { revealed?: boolean })
  | JunqiHiddenPiece
  | (JunqiPiece & { revealed: true });

// 12 rows × 5 cols.
export type JunqiBoardView = JunqiCellView[][];

export interface JunqiPosition {
  x: number;
  y: number;
}

export interface JunqiPlacement {
  type: JunqiPieceType;
  at: JunqiPosition;
}

export interface JunqiMove {
  from: JunqiPosition;
  to: JunqiPosition;
  piece: JunqiPiece;
  captured?: JunqiPiece | null;
  engineer_defused?: boolean;
  both_destroyed?: boolean;
  revealed_piece?: JunqiPiece | null;
}

export interface JunqiGameState {
  room_id: string;
  red_id?: string;
  black_id?: string;
  ready: boolean;
  my_color?: JunqiPieceColor;
  mode?: 'open' | 'hidden';
  phase?: 'layout' | 'playing' | 'over';
  turn?: JunqiPieceColor;
  status?: 'playing' | 'red_win' | 'black_win' | 'draw';
  board_view?: JunqiBoardView;
  move_count?: number;
}

export interface JunqiMoveResult {
  room_id: string;
  move: JunqiMove;
  turn: JunqiPieceColor;
  status: string;
  phase: string;
  my_color: JunqiPieceColor;
  board_view: JunqiBoardView;
}

export interface JunqiGameOver {
  room_id: string;
  winner: JunqiPieceColor | '';
  reason: string;
  status: string;
}

// Helper: type guard for a visible piece view.
export function isVisiblePiece(
  cell: JunqiCellView | undefined,
): cell is JunqiPiece & { revealed?: boolean } {
  if (!cell) return false;
  return (cell as { hidden?: boolean }).hidden !== true;
}

// Helper: type guard for a hidden piece view.
export function isHiddenPiece(cell: JunqiCellView | undefined): cell is JunqiHiddenPiece {
  if (!cell) return false;
  return (cell as { hidden?: boolean }).hidden === true;
}