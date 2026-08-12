import { useState, useCallback } from 'react';
import {
  PIECE_NAMES,
  STYLE_COLORS,
  type StyleKey,
  type PieceColor,
  type PieceType,
} from '@/assets/images/junqi';
import { JunqiPiece } from './JunqiPiece';
import type { JunqiPlacement, JunqiPosition, JunqiPieceColor } from '@/types/junqi';

interface Props {
  myColor: PieceColor;
  boardStyle: StyleKey;
  onSubmit: (placements: JunqiPlacement[]) => void;
}

// Required counts matching server-side game/junqi/layout.go.
const REQUIRED_COUNTS: Record<PieceType, number> = {
  flag: 1,
  commander: 1,
  general: 1,
  major: 2,
  colonel: 2,
  captain: 2,
  lieutenant: 2,
  sergeant: 6,
  engineer: 3,
  bomb: 2,
  mine: 3,
};

const CELL = 56;
const PADDING = 32;
const BOARD_W = 5 * CELL + PADDING * 2;
const BOARD_H = 6 * CELL + PADDING * 2;

// Red-side constants (board-space). For Black, add yBase=6 to all y values.
const CAMP_CELLS_RED: ReadonlyArray<JunqiPosition> = [
  { x: 1, y: 2 }, { x: 3, y: 2 }, { x: 1, y: 4 }, { x: 3, y: 4 },
];
const HQ_CELLS_RED: ReadonlyArray<JunqiPosition> = [
  { x: 0, y: 0 }, { x: 4, y: 0 },
];
const FRONT_ROW_RED = 5;
const BACK_ROWS_RED: ReadonlyArray<number> = [0, 1];

function isMyHomeCell(pos: JunqiPosition, myColor: JunqiPieceColor): boolean {
  if (myColor === 'red') {
    return pos.y >= 0 && pos.y <= 5;
  }
  return pos.y >= 6 && pos.y <= 11;
}

/**
 * LayoutPanel — drag-and-drop board for the 25-piece layout phase.
 *
 * Layout rules (mirroring server-side validation):
 *   - Flag must be in one of the 2 HQs
 *   - Mines only in back two rows
 *   - Bombs never on the front row
 *   - No piece on camps
 *   - Exactly 25 pieces, correct counts per type
 */
export function LayoutPanel({ myColor, boardStyle, onSubmit }: Props) {
  const colors = STYLE_COLORS[boardStyle];
  // Black player's home is y=6..11; add offset to all board-space y constants.
  const yBase = myColor === 'black' ? 6 : 0;
  const CAMP_CELLS = CAMP_CELLS_RED.map((c) => ({ ...c, y: c.y + yBase }));
  const HQ_CELLS = HQ_CELLS_RED.map((c) => ({ ...c, y: c.y + yBase }));
  const FRONT_ROW = FRONT_ROW_RED + yBase;
  const BACK_ROWS = BACK_ROWS_RED.map((r) => r + yBase);
  // Map position → piece type placed there.
  const [board, setBoard] = useState<Record<string, PieceType>>({});
  // Map piece type → count remaining to place.
  const [remaining, setRemaining] = useState<Record<PieceType, number>>({
    ...REQUIRED_COUNTS,
  });
  const [draggedType, setDraggedType] = useState<PieceType | null>(null);
  const [err, setErr] = useState<string | null>(null);

  // Convert a board-position key to display coordinates. Red's home row is
  // at y=0 (bottom of panel); for Black it would be at y=11 (top of panel).
  // We display the panel with the player's home row at the bottom of the grid.

  const posKey = (p: JunqiPosition) => `${p.x},${p.y}`;
  const parsePosKey = (k: string): JunqiPosition => {
    const [xs, ys] = k.split(',');
    return { x: parseInt(xs, 10), y: parseInt(ys, 10) };
  };

  const handleDragStart = useCallback((type: PieceType) => {
    setDraggedType(type);
  }, []);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent, targetPos: JunqiPosition) => {
      e.preventDefault();
      if (!draggedType) return;
      // Validate constraints client-side (server re-validates).
      // Rule 1: own half.
      if (!isMyHomeCell(targetPos, myColor)) {
        setErr('piece must be placed in your own half');
        return;
      }
      // Rule 2: cell not occupied.
      if (board[posKey(targetPos)]) {
        setErr('cell is already occupied');
        return;
      }
      // Rule 3: not on camp.
      if (CAMP_CELLS.some((c) => c.x === targetPos.x && c.y === targetPos.y)) {
        setErr('pieces may not be placed on camps');
        return;
      }
      // Rule 4: HQ only for flag (and we use one of the two HQs for the flag).
      if (HQ_CELLS.some((c) => c.x === targetPos.x && c.y === targetPos.y) && draggedType !== 'flag') {
        setErr('only the flag may be placed in a HQ');
        return;
      }
      // Rule 5: flag must be in HQ.
      if (draggedType === 'flag' && !HQ_CELLS.some((c) => c.x === targetPos.x && c.y === targetPos.y)) {
        setErr('flag must be placed in one of the two headquarters');
        return;
      }
      // Rule 6: mines only in back rows.
      if (draggedType === 'mine' && !BACK_ROWS.includes(targetPos.y)) {
        setErr('mines may only be placed in the back two rows');
        return;
      }
      // Rule 7: bombs not in front row.
      if (draggedType === 'bomb' && targetPos.y === FRONT_ROW) {
        setErr('bombs may not be placed in the front row');
        return;
      }
      // Rule 8: have remaining pieces of this type.
      if (remaining[draggedType] <= 0) {
        setErr(`no ${draggedType} remaining`);
        return;
      }
      setBoard((prev) => ({ ...prev, [posKey(targetPos)]: draggedType }));
      setRemaining((prev) => ({ ...prev, [draggedType]: prev[draggedType] - 1 }));
      setErr(null);
      setDraggedType(null);
    },
    [draggedType, board, remaining, myColor],
  );

  const handleRemovePiece = useCallback(
    (pos: JunqiPosition) => {
      const t = board[posKey(pos)];
      if (!t) return;
      setBoard((prev) => {
        const next = { ...prev };
        delete next[posKey(pos)];
        return next;
      });
      setRemaining((prev) => ({ ...prev, [t]: prev[t] + 1 }));
    },
    [board],
  );

  const totalPlaced = Object.values(REQUIRED_COUNTS).reduce((a, b) => a + b, 0) -
    Object.values(remaining).reduce((a, b) => a + b, 0);

  const handleSubmit = useCallback(() => {
    if (totalPlaced !== 25) {
      setErr(`must place all 25 pieces (placed ${totalPlaced})`);
      return;
    }
    const placements: JunqiPlacement[] = Object.entries(board).map(([k, t]) => ({
      type: t,
      at: parsePosKey(k),
    }));
    onSubmit(placements);
  }, [board, totalPlaced, onSubmit]);

  // Compute the visual Y range. For Red, we display rows 0..5 normally (bottom-up);
  // for Black, we'd display rows 11..6. To keep this simple, we display always with
  // y=0 at the bottom regardless of color (so user sees their own home at bottom).
  const displayRow = (y: number): number => {
    if (myColor === 'red') return 5 - y; // y=0 at bottom, y=5 at top
    return 11 - y;                        // y=11 at bottom, y=6 at top
  };

  const pieceTypes: PieceType[] = ['flag', 'commander', 'general', 'major', 'colonel',
    'captain', 'lieutenant', 'sergeant', 'engineer', 'bomb', 'mine'];

  return (
    <div className="junqi-layout-panel">
      <h3>布局阶段 — 摆放你的 25 枚棋子</h3>

      <div style={{ display: 'flex', gap: 24, alignItems: 'flex-start' }}>
        {/* Board */}
        <div
          className="junqi-board-wrapper"
          style={{
            position: 'relative',
            width: BOARD_W,
            height: BOARD_H,
            background: colors.boardBg,
            borderRadius: 8,
            boxShadow: '0 6px 18px rgba(0,0,0,0.4)',
          }}
        >
          {/* Background rails/roads as grid lines */}
          <svg
            width={BOARD_W}
            height={BOARD_H}
            style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}
          >
            {/* horizontal lines — 6 rows = 6 boundary lines (y=0..5) */}
            {Array.from({ length: 6 }).map((_, idx) => {
              const y = idx;
              const py = PADDING + displayRow(y) * CELL;
              return (
                <line key={`h${y}`}
                      x1={PADDING} y1={py}
                      x2={PADDING + 4 * CELL} y2={py}
                      stroke={colors.boardLine} strokeWidth={y === 0 || y === 5 ? 1.5 : 1} />
              );
            })}
            {/* vertical lines */}
            {Array.from({ length: 5 }).map((_, x) => (
              <line key={`v${x}`}
                    x1={PADDING + x * CELL} y1={PADDING}
                    x2={PADDING + x * CELL} y2={PADDING + 5 * CELL}
                    stroke={colors.boardLine} strokeWidth={1} />
            ))}
            {/* rail rows doubled */}
            {[1, 3, 5].map((y) => {
              const py = PADDING + displayRow(y) * CELL;
              return (
                <line key={`r${y}`}
                      x1={PADDING} y1={py}
                      x2={PADDING + 4 * CELL} y2={py}
                      stroke={colors.railLine} strokeWidth={2.5} />
              );
            })}
            {/* HQ triangles */}
            {[0, 4].map((x) => {
              const px = PADDING + x * CELL;
              const py = PADDING + displayRow(0) * CELL;
              return (
                <polygon key={`hq${x}`}
                  points={`${px - CELL / 2},${py - CELL / 2} ${px + CELL / 2},${py - CELL / 2} ${px},${py}`}
                  fill={colors.hq} fillOpacity={0.85} stroke={colors.boardLine} strokeWidth={1.5} />
              );
            })}
            {/* Camps */}
            {[{ x: 1, y: 2 }, { x: 3, y: 2 }, { x: 1, y: 4 }, { x: 3, y: 4 }].map((c, i) => {
              const px = PADDING + c.x * CELL;
              const py = PADDING + displayRow(c.y) * CELL;
              return (
                <rect key={`c${i}`} x={px - CELL / 3} y={py - CELL / 3}
                      width={(CELL * 2) / 3} height={(CELL * 2) / 3}
                      fill={colors.camp} fillOpacity={0.6} stroke={colors.boardLine}
                      strokeWidth={1.5} rx={CELL / 6} />
              );
            })}
          </svg>
          {/* Drop targets */}
          {Array.from({ length: 6 }).flatMap((_, y) =>
            Array.from({ length: 5 }).map((__, x) => {
              const targetPos: JunqiPosition = { x, y: y + yBase };
              const dy = displayRow(y);
              const px = PADDING + x * CELL;
              const py = PADDING + dy * CELL;
              const placedType = board[posKey(targetPos)];
              return (
                <div
                  key={`cell-${x}-${y}`}
                  onDragOver={handleDragOver}
                  onDrop={(e) => handleDrop(e, targetPos)}
                  onClick={() => placedType && handleRemovePiece(targetPos)}
                  style={{
                    position: 'absolute',
                    left: px - CELL / 2,
                    top: py - CELL / 2,
                    width: CELL,
                    height: CELL,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    cursor: 'pointer',
                  }}
                  title={placedType ? 'click to remove' : ''}
                >
                  {placedType && (
                    <JunqiPiece
                      color={myColor}
                      type={placedType}
                      style={boardStyle}
                      size={CELL - 8}
                    />
                  )}
                </div>
              );
            }),
          )}
        </div>

        {/* Sidebar: piece palette */}
        <div className="layout-palette" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <p>剩余棋子: <strong>{25 - totalPlaced}</strong> / 25</p>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
            {pieceTypes.map((t) => {
              const left = remaining[t];
              return (
                <div
                  key={t}
                  draggable={left > 0}
                  onDragStart={() => handleDragStart(t)}
                  style={{
                    padding: 8,
                    border: '1px solid #555',
                    borderRadius: 6,
                    cursor: left > 0 ? 'grab' : 'not-allowed',
                    opacity: left > 0 ? 1 : 0.4,
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    background: '#1f2937',
                    color: '#e5e7eb',
                  }}
                >
                  <JunqiPiece color={myColor} type={t} style={boardStyle} size={32} />
                  <div>
                    <div>{PIECE_NAMES[t]}</div>
                    <div style={{ fontSize: 12, color: '#9ca3af' }}>×{left}</div>
                  </div>
                </div>
              );
            })}
          </div>
          {err && <div style={{ color: '#ef4444', fontSize: 14 }}>{err}</div>}
          <button
            className="btn btn-primary"
            onClick={handleSubmit}
            disabled={totalPlaced !== 25}
          >
            提交布局 ({totalPlaced}/25)
          </button>
        </div>
      </div>
    </div>
  );
}