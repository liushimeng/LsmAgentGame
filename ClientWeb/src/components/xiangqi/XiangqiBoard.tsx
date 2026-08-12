import { useCallback, useState } from 'react';
import { XiangqiPiece } from './XiangqiPiece';
import { STYLE_COLORS, getBoardBg, type StyleKey, type PieceColor } from '@/assets/images/xiangqi';
import type { XiangqiBoard as BoardType } from '@/types/api';

interface Props {
  board: BoardType;
  myColor: PieceColor;
  turn: PieceColor;
  selectedPos: { x: number; y: number } | null;
  lastMove: { from: { x: number; y: number }; to: { x: number; y: number } } | null;
  boardStyle: StyleKey;
  onSelect: (pos: { x: number; y: number } | null) => void;
  onMove: (from: { x: number; y: number }, to: { x: number; y: number }) => void;
}

const CELL = 64; // cell size in px
const PADDING = 32;
const BOARD_W = CELL * 8 + PADDING * 2;
const BOARD_H = CELL * 9 + PADDING * 2;

/**
 * 2D SVG + HTML hybrid Xiangqi board.
 * Grid lines are drawn with SVG; pieces are positioned as HTML overlays.
 */
export function XiangqiBoard({
  board,
  myColor,
  turn,
  selectedPos,
  lastMove,
  boardStyle,
  onSelect,
  onMove,
}: Props) {
  const colors = STYLE_COLORS[boardStyle];
  // 服务端坐标系：红方阵营 y=0..4，黑方阵营 y=5..9。
  // "我方在底部" 规范要求我方阵营渲染在屏幕底部。
  // 红方视角翻转 (y → 9 - y), 黑方视角不翻转 → 双方 y 最大值都落在屏幕底部。
  const isFlipped = myColor === 'red';
  const [boardBgFailed, setBoardBgFailed] = useState(false);
  const boardBgSrc = getBoardBg(boardStyle);

  // Convert board position to pixel coordinates.
  const posToPixel = useCallback(
    (x: number, y: number) => {
      const bx = isFlipped ? 8 - x : x;
      const by = isFlipped ? 9 - y : y;
      return { px: PADDING + bx * CELL, py: PADDING + by * CELL };
    },
    [isFlipped],
  );

  // Convert pixel coordinates to board position.
  const pixelToPos = useCallback(
    (px: number, py: number) => {
      const bx = Math.round((px - PADDING) / CELL);
      const by = Math.round((py - PADDING) / CELL);
      const x = isFlipped ? 8 - bx : bx;
      const y = isFlipped ? 9 - by : by;
      if (x < 0 || x > 8 || y < 0 || y > 9) return null;
      return { x, y };
    },
    [isFlipped],
  );

  const handleClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const rect = e.currentTarget.getBoundingClientRect();
      const px = e.clientX - rect.left;
      const py = e.clientY - rect.top;
      const pos = pixelToPos(px, py);
      if (!pos) return;

      const piece = board[pos.y]?.[pos.x];

      if (selectedPos) {
        // If clicking own piece, reselect
        if (piece && piece.color === myColor) {
          onSelect(pos);
          return;
        }
        // Try to move
        onMove(selectedPos, pos);
        return;
      }

      // Select own piece
      if (piece && piece.color === myColor && turn === myColor) {
        onSelect(pos);
      }
    },
    [board, myColor, turn, selectedPos, onSelect, onMove, pixelToPos],
  );

  // Build grid lines
  //
  // NOTE: 象棋真实棋盘有"楚河汉界"空走道 —— 河界上没有横线穿过(中文规整),
  //       中间 1..7 列竖线在第 5 行(y=5) 断开,留出河道。
  //       早先实现把所有横向线拉满,导致横线压在"楚 河""汉 界"文字上,
  //       视觉上文字断裂、线重复穿过 —— 这里是修复点。
  const gridLines: React.ReactNode[] = [];
  const HLINE_LEFT = PADDING + 1.5 * CELL; // 河道左端点(竖线 anchor)
  const HLINE_RIGHT = PADDING + 6.5 * CELL; // 河道右端点
  // Horizontal lines (10 lines). y=4 与 y=5 是河道上下沿,需要在中段
  // (1.5*CELL .. 6.5*CELL) 留白以避免与"楚河""汉界"文字重叠。
  for (let y = 0; y <= 9; y++) {
    const py = PADDING + y * CELL;
    if (y === 4 || y === 5) {
      // 河道上下沿:左侧 1.5*CELL,右侧 6.5*CELL 留空,只画两端到边的部分
      gridLines.push(
        <line
          key={`h${y}-l`}
          x1={PADDING}
          y1={py}
          x2={HLINE_LEFT}
          y2={py}
          stroke={colors.boardLine}
          strokeWidth={1.5}
        />,
      );
      gridLines.push(
        <line
          key={`h${y}-r`}
          x1={HLINE_RIGHT}
          y1={py}
          x2={PADDING + 8 * CELL}
          y2={py}
          stroke={colors.boardLine}
          strokeWidth={1.5}
        />,
      );
    } else {
      gridLines.push(
        <line
          key={`h${y}`}
          x1={PADDING}
          y1={py}
          x2={PADDING + 8 * CELL}
          y2={py}
          stroke={colors.boardLine}
          strokeWidth={1.5}
        />,
      );
    }
  }
  // Vertical lines — full for columns 0,8; broken for middle columns
  for (let x = 0; x <= 8; x++) {
    const px = PADDING + x * CELL;
    if (x === 0 || x === 8) {
      gridLines.push(
        <line key={`v${x}`} x1={px} y1={PADDING} x2={px} y2={PADDING + 9 * CELL} stroke={colors.boardLine} strokeWidth={1.5} />,
      );
    } else {
      // Top half
      gridLines.push(
        <line key={`vt${x}`} x1={px} y1={PADDING} x2={px} y2={PADDING + 4 * CELL} stroke={colors.boardLine} strokeWidth={1.5} />,
      );
      // Bottom half
      gridLines.push(
        <line key={`vb${x}`} x1={px} y1={PADDING + 5 * CELL} x2={px} y2={PADDING + 9 * CELL} stroke={colors.boardLine} strokeWidth={1.5} />,
      );
    }
  }

  // River text
  const riverY = PADDING + 4.5 * CELL;
  gridLines.push(
    <text
      key="river-left"
      x={PADDING + 1.5 * CELL}
      y={riverY}
      textAnchor="middle"
      dominantBaseline="central"
      fill={colors.boardLine}
      fontSize={20}
      fontFamily="KaiTi, STKaiti, serif"
      fontWeight="bold"
    >
      楚 河
    </text>,
  );
  gridLines.push(
    <text
      key="river-right"
      x={PADDING + 6.5 * CELL}
      y={riverY}
      textAnchor="middle"
      dominantBaseline="central"
      fill={colors.boardLine}
      fontSize={20}
      fontFamily="KaiTi, STKaiti, serif"
      fontWeight="bold"
    >
      汉 界
    </text>,
  );

  // Palace diagonals
  const palaceDiags = [
    // Top palace (black)
    [3, 7, 5, 9],
    [5, 7, 3, 9],
    // Bottom palace (red)
    [3, 0, 5, 2],
    [5, 0, 3, 2],
  ];
  palaceDiags.forEach(([x1, y1, x2, y2], i) => {
    const p1 = posToPixel(x1, y1);
    const p2 = posToPixel(x2, y2);
    gridLines.push(
      <line
        key={`pd${i}`}
        x1={p1.px}
        y1={p1.py}
        x2={p2.px}
        y2={p2.py}
        stroke={colors.boardLine}
        strokeWidth={1}
      />,
    );
  });

  // Star markers on intersection points
  const starPositions = [
    [2, 3], [6, 3], // Red cannon positions
    [2, 6], [6, 6], // Black cannon positions
    [0, 3], [2, 3], [4, 3], [6, 3], [8, 3], // Red soldier positions (subset)
    [0, 6], [2, 6], [4, 6], [6, 6], [8, 6], // Black soldier positions (subset)
  ];
  const drawnStars = new Set<string>();
  starPositions.forEach(([x, y]) => {
    const key = `${x},${y}`;
    if (drawnStars.has(key)) return;
    drawnStars.add(key);
    const { px, py } = posToPixel(x, y);
    const s = 4;
    const g = 3;
    const segments = [
      // top-right
      [px + g, py - g - s, px + g, py - g, px + g + s, py - g],
      // top-left
      [px - g, py - g - s, px - g, py - g, px - g - s, py - g],
      // bottom-right
      [px + g, py + g + s, px + g, py + g, px + g + s, py + g],
      // bottom-left
      [px - g, py + g + s, px - g, py + g, px - g - s, py + g],
    ];
    segments.forEach((seg, si) => {
      gridLines.push(
        <polyline
          key={`star-${x}-${y}-${si}`}
          points={`${seg[0]},${seg[1]} ${seg[2]},${seg[3]} ${seg[4]},${seg[5]}`}
          stroke={colors.boardLine}
          strokeWidth={1}
          fill="none"
        />,
      );
    });
  });

  // Highlight last move
  const highlightRects: React.ReactNode[] = [];
  if (lastMove) {
    [lastMove.from, lastMove.to].forEach((pos, i) => {
      const { px, py } = posToPixel(pos.x, pos.y);
      highlightRects.push(
        <rect
          key={`hl-${i}`}
          x={px - CELL / 2}
          y={py - CELL / 2}
          width={CELL}
          height={CELL}
          fill="rgba(255,215,0,0.15)"
          rx={4}
        />,
      );
    });
  }

  return (
    <div
      onClick={handleClick}
      style={{
        position: 'relative',
        width: BOARD_W,
        height: BOARD_H,
        cursor: 'pointer',
        borderRadius: 8,
        overflow: 'hidden',
        boxShadow: `0 4px 24px rgba(0,0,0,0.4), inset 0 0 40px rgba(0,0,0,0.1)`,
      }}
    >
      {/* Board background — image with CSS fallback */}
      {boardBgSrc && !boardBgFailed ? (
        <img
          src={boardBgSrc}
          alt="棋盘"
          onError={() => setBoardBgFailed(true)}
          style={{
            position: 'absolute',
            inset: 0,
            width: '100%',
            height: '100%',
            objectFit: 'cover',
          }}
        />
      ) : (
        <div
          style={{
            position: 'absolute',
            inset: 0,
            background: colors.board,
          }}
        />
      )}

      {/* SVG grid */}
      <svg
        width={BOARD_W}
        height={BOARD_H}
        style={{ position: 'absolute', top: 0, left: 0 }}
      >
        {highlightRects}
        {gridLines}
      </svg>

      {/* Pieces */}
      {board.map((row, y) =>
        row.map((piece, x) => {
          if (!piece) return null;
          const { px, py } = posToPixel(x, y);
          const isSelected =
            selectedPos != null && selectedPos.x === x && selectedPos.y === y;
          return (
            <div
              key={`${x}-${y}`}
              style={{
                position: 'absolute',
                left: px - CELL / 2 + (CELL - 48) / 2,
                top: py - CELL / 2 + (CELL - 48) / 2,
                zIndex: isSelected ? 10 : 1,
              }}
            >
              <XiangqiPiece
                color={piece.color as PieceColor}
                type={piece.type}
                style={boardStyle}
                selected={isSelected}
                size={48}
              />
            </div>
          );
        }),
      )}
    </div>
  );
}
