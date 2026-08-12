import { useCallback, useState } from 'react';
import { JunqiPiece } from './JunqiPiece';
import { STYLE_COLORS, getBoardBg, type StyleKey } from '@/assets/images/junqi';
import type { JunqiBoardView, JunqiPieceColor } from '@/types/junqi';

interface Props {
  boardView: JunqiBoardView;
  myColor: JunqiPieceColor;
  turn: JunqiPieceColor;
  selectedPos: { x: number; y: number } | null;
  lastMove: { from: { x: number; y: number }; to: { x: number; y: number } } | null;
  boardStyle: StyleKey;
  onSelect: (pos: { x: number; y: number } | null) => void;
  onMove: (from: { x: number; y: number }, to: { x: number; y: number }) => void;
}

const CELL = 56;
const COLS = 5;
const ROWS = 12;
const PADDING = 32;
const BOARD_W = CELL * COLS + PADDING * 2;
const BOARD_H = CELL * ROWS + PADDING * 2;

/**
 * Junqi (中国军棋) board: 5 cols × 12 rows.
 *
 * The board renders:
 *   - 5 columns of road cells with 2-row alternation of rail rows
 *   - Camps (行营) at row 2 and row 4, columns 1 and 3 (4 per side, mirrored)
 *   - HQs (大本营) at row 0 col 0/4 (Red) and row 11 col 0/4 (Black)
 *   - Mountain border visual between row 5 and row 6
 *
 * Pieces are absolutely-positioned divs placed over the grid.
 */
export function JunqiBoard({
  boardView,
  myColor,
  turn,
  selectedPos,
  lastMove,
  boardStyle,
  onSelect,
  onMove,
}: Props) {
  const colors = STYLE_COLORS[boardStyle];
  // 服务端坐标系:红方阵营 y=0..5, 黑方阵营 y=6..11。
  // "我方在底部" 规范要求我方阵营渲染在屏幕底部。
  // 红方视角翻转 (y → 11 - y), 黑方视角不翻转 → 双方 y 最大值都落在屏幕底部。
  const isFlipped = myColor === 'red';
  const [boardBgFailed, setBoardBgFailed] = useState(false);
  const boardBgSrc = getBoardBg(boardStyle);

  const posToPixel = useCallback(
    (x: number, y: number) => {
      const bx = isFlipped ? COLS - 1 - x : x;
      const by = isFlipped ? ROWS - 1 - y : y;
      return { px: PADDING + bx * CELL, py: PADDING + by * CELL };
    },
    [isFlipped],
  );

  const pixelToPos = useCallback(
    (px: number, py: number): { x: number; y: number } | null => {
      const bx = Math.round((px - PADDING) / CELL);
      const by = Math.round((py - PADDING) / CELL);
      const x = isFlipped ? COLS - 1 - bx : bx;
      const y = isFlipped ? ROWS - 1 - by : by;
      if (x < 0 || x >= COLS || y < 0 || y >= ROWS) return null;
      return { x, y };
    },
    [isFlipped],
  );

  const handleClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const rect = e.currentTarget.getBoundingClientRect();
      const pos = pixelToPos(e.clientX - rect.left, e.clientY - rect.top);
      if (!pos) return;
      const cell = boardView[pos.y]?.[pos.x];
      const visible = cell && !(cell as { hidden?: boolean }).hidden;

      if (selectedPos) {
        if (visible && (cell as { color?: string }).color === myColor) {
          onSelect(pos);
          return;
        }
        onMove(selectedPos, pos);
        return;
      }
      if (visible && (cell as { color?: string }).color === myColor && turn === myColor) {
        onSelect(pos);
      }
    },
    [boardView, myColor, turn, selectedPos, onSelect, onMove, pixelToPos],
  );

  // Build SVG overlays.
  const cells: React.ReactNode[] = [];
  // Road grid lines (走 posToPixel 让红方翻转视角下网格也翻转)。
  // 山界(r=5 与 r=6 之间)由 mountain-bar 整条遮挡,所以横向线不在山界处重复画两层。
  for (let r = 0; r < ROWS; r++) {
    const { py: pyLeft } = posToPixel(0, r);
    const { py: pyRight } = posToPixel(COLS - 1, r);
    cells.push(
      <line key={`h${r}`} x1={PADDING} y1={pyLeft} x2={PADDING + (COLS - 1) * CELL} y2={pyRight}
            stroke={colors.boardLine} strokeWidth={r === 0 || r === ROWS - 1 ? 1.5 : 1} />
    );
  }
  for (let c = 0; c < COLS; c++) {
    const { px: pxTop } = posToPixel(c, 0);
    const { px: pxBottom } = posToPixel(c, ROWS - 1);
    cells.push(
      <line key={`v${c}`} x1={pxTop} y1={PADDING} x2={pxBottom} y2={PADDING + (ROWS - 1) * CELL}
            stroke={colors.boardLine} strokeWidth={1} />
    );
  }
  // 铁路线:road-style 标记。r=5 与 r=6 是山界,被 mountain-bar 完全遮挡,
  // 但底下的 iron-line 仍然要从山界两侧"对接",所以这两个 rail 行画在
  // mountain-bar 之上的层(layer 在 cells 之后追加),保证不与山界重复。
  const railRows = [1, 3, 5, 6, 8, 10];
  const railLines: React.ReactNode[] = [];
  for (const r of railRows) {
    const { py: pyLeft } = posToPixel(0, r);
    const { py: pyRight } = posToPixel(COLS - 1, r);
    railLines.push(
      <line key={`r${r}`} x1={PADDING} y1={pyLeft} x2={PADDING + (COLS - 1) * CELL} y2={pyRight}
            stroke={colors.railLine} strokeWidth={2.2} strokeDasharray="6 3" />
    );
  }

  // Camps (4 per side: y∈{2,4} for red, y∈{7,9} for black — but we model 12 rows
  // so camps appear at y=2,4 (red half) and y=7,9 (black half), x=1 and x=3).
  const campCells = [
    { x: 1, y: 2 }, { x: 3, y: 2 }, { x: 1, y: 4 }, { x: 3, y: 4 },
    { x: 1, y: 7 }, { x: 3, y: 7 }, { x: 1, y: 9 }, { x: 3, y: 9 },
  ];
  const campOverlays = campCells.map((c, i) => {
    const { px, py } = posToPixel(c.x, c.y);
    return (
      <g key={`camp${i}`}>
        <rect x={px - CELL / 3} y={py - CELL / 3} width={(CELL * 2) / 3} height={(CELL * 2) / 3}
              fill={colors.camp} fillOpacity={0.6} stroke={colors.boardLine} strokeWidth={1.5} rx={CELL / 6} />
      </g>
    );
  });

  // HQs (大本营): 4 corners.
  const hqCells = [
    { x: 0, y: 0 }, { x: 4, y: 0 },
    { x: 0, y: 11 }, { x: 4, y: 11 },
  ];
  const hqOverlays = hqCells.map((c, i) => {
    const { px, py } = posToPixel(c.x, c.y);
    return (
      <g key={`hq${i}`}>
        <polygon
          points={`${px - CELL / 2},${py - CELL / 2} ${px + CELL / 2},${py - CELL / 2} ${px},${py}`}
          fill={colors.hq} fillOpacity={0.85} stroke={colors.boardLine} strokeWidth={1.5}
        />
      </g>
    );
  });

  // Mountain border (between row 5 and row 6): draw a visual separator with text.
  //
  // 之前实现存在两个小问题:
  //   1) 宽度计算 `PADDING + (COLS-1)*CELL - PADDING` 多余地抵消了 PADDING,导致
  //      rect 实际从 PADDING 起绘到 PADDING + (COLS-1)*CELL,正好等价到 (COLS-1)*CELL
  //      的纯列宽,左右两侧紧贴第一列与最后一列,但山界的中段会被棋子压住,不优雅;
  //   2) 山界中央的"楚河 · 汉界"文字位置在 midY+5,在视觉偏下;此处居中到 midY+5。
  // 现在标准化:
  //   - rect 宽度用 (COLS-1)*CELL,左右各加 0 列宽(与列边界对齐);
  //   - 高度 = CELL(完整覆盖两格之间的空白带 + 上下边线);
  //   - 文字垂直居中(dominantBaseline=central)。
  const mountainOverlays: React.ReactNode[] = [];
  {
    const yTop = PADDING + 5 * CELL;
    const yBottom = PADDING + 6 * CELL;
    const midY = (yTop + yBottom) / 2;
    const barWidth = (COLS - 1) * CELL;
    const barHeight = yBottom - yTop;
    mountainOverlays.push(
      <rect key="mountain-bar"
            x={PADDING} y={yTop} width={barWidth} height={barHeight}
            fill={colors.mountain} fillOpacity={0.92} />,
    );
    mountainOverlays.push(
      <text key="mountain-text"
            x={PADDING + barWidth / 2} y={midY}
            textAnchor="middle" dominantBaseline="central"
            fontSize={Math.max(12, CELL * 0.32)} fontWeight="bold"
            fill="#fff" stroke={colors.boardLine} strokeWidth={0.5}>
        楚河 · 汉界
      </text>,
    );
  }

  // Last move highlight.
  const lastMoveOverlays: React.ReactNode[] = [];
  if (lastMove) {
    for (const p of [lastMove.from, lastMove.to]) {
      const { px, py } = posToPixel(p.x, p.y);
      lastMoveOverlays.push(
        <rect key={`lm-${p.x}-${p.y}`}
              x={px - CELL / 2 + 2} y={py - CELL / 2 + 2}
              width={CELL - 4} height={CELL - 4}
              fill="#ffd700" fillOpacity={0.25} stroke="#ffd700" strokeWidth={2} rx={4} />,
      );
    }
  }

  // Selected highlight.
  const selectedOverlay = selectedPos
    ? (() => {
        const { px, py } = posToPixel(selectedPos.x, selectedPos.y);
        return (
          <rect x={px - CELL / 2 + 2} y={py - CELL / 2 + 2}
                width={CELL - 4} height={CELL - 4}
                fill="#3b82f6" fillOpacity={0.35} stroke="#3b82f6" strokeWidth={2.5} rx={4} />
        );
      })()
    : null;

  // Render pieces.
  const pieces: React.ReactNode[] = [];
  for (let r = 0; r < ROWS; r++) {
    for (let c = 0; c < COLS; c++) {
      const cell = boardView[r]?.[c];
      if (!cell) continue;
      const { px, py } = posToPixel(c, r);
      const isSelected = selectedPos?.x === c && selectedPos?.y === r;
      if ((cell as { hidden?: boolean }).hidden) {
        pieces.push(
          <div key={`p${r}-${c}`} style={{ position: 'absolute', left: px - CELL / 2, top: py - CELL / 2 }}>
            <JunqiPiece color="black" type="flag" style={boardStyle} hidden size={CELL - 8} selected={isSelected} />
          </div>,
        );
        continue;
      }
      const visible = cell as { color?: string; type?: string; revealed?: boolean };
      if (!visible.color || !visible.type) continue;
      pieces.push(
        <div key={`p${r}-${c}`} style={{ position: 'absolute', left: px - CELL / 2, top: py - CELL / 2 }}>
          <JunqiPiece
            color={visible.color as 'red' | 'black'}
            type={visible.type as never}
            style={boardStyle}
            selected={isSelected}
            size={CELL - 8}
          />
        </div>,
      );
    }
  }

  return (
    <div
      className="junqi-board-wrapper"
      style={{
        position: 'relative',
        width: BOARD_W,
        height: BOARD_H,
        background: colors.boardBg,
        borderRadius: 8,
        boxShadow: '0 6px 18px rgba(0,0,0,0.4)',
        cursor: 'pointer',
      }}
      onClick={handleClick}
    >
      {/* Background image (optional) */}
      {boardBgSrc && !boardBgFailed && (
        <img
          src={boardBgSrc}
          alt="junqi board"
          onError={() => setBoardBgFailed(true)}
          style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover', borderRadius: 8 }}
        />
      )}
      {/* SVG grid overlay */}
      <svg
        width={BOARD_W}
        height={BOARD_H}
        style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}
      >
        {cells}
        {campOverlays}
        {hqOverlays}
        {mountainOverlays}
        {railLines}
        {lastMoveOverlays}
        {selectedOverlay}
      </svg>
      {/* Pieces layer */}
      <div style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>{pieces}</div>
    </div>
  );
}