import { useState } from 'react';
import { ChessPiece } from './ChessPiece';
import {
  STYLE_COLORS,
  getBoardBg,
  type StyleKey,
  type PieceColor,
} from '@/assets/images/chess';
import type { ChessBoard as BoardType } from '@/types/api';

interface Props {
  board: BoardType;
  myColor: PieceColor;
  turn: PieceColor;
  selectedPos: { x: number; y: number } | null;
  legalTargets?: { x: number; y: number }[];
  lastMove: { from: { x: number; y: number }; to: { x: number; y: number } } | null;
  boardStyle: StyleKey;
  onSelect: (pos: { x: number; y: number } | null) => void;
  onMove: (from: { x: number; y: number }, to: { x: number; y: number }) => void;
}

const CELL = 64;
const PADDING = 36;
const BOARD_W = CELL * 8 + PADDING * 2;
const BOARD_H = CELL * 8 + PADDING * 2;
const FILES = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'];

/**
 * 8×8 chess board. Board coordinates: x=0..7 (file a..h), y=0..7 (rank 1..8).
 * White sees from rank 1 at the bottom; Black sees flipped (rank 1 at top).
 */
export function ChessBoard({
  board,
  myColor,
  turn,
  selectedPos,
  legalTargets = [],
  lastMove,
  boardStyle,
  onSelect,
  onMove,
}: Props) {
  const colors = STYLE_COLORS[boardStyle];
  // 服务端坐标系:白方阵营 y=0..3 (rank 1..4), 黑方阵营 y=4..7 (rank 5..8)。
  // "我方在底部" 规范要求我方阵营渲染在屏幕底部。
  // 白方视角翻转 (y → 7 - y), 黑方视角不翻转 → 双方 y 最大值都落在屏幕底部。
  const isFlipped = myColor === 'white';
  const [boardBgFailed, setBoardBgFailed] = useState(false);
  const boardBgSrc = getBoardBg(boardStyle);

  // Map logical (x,y) → visual cell index.
  // 文件(a-h)始终从左到右，不随视角翻转——翻转只影响排名(行)。
  // Visual column = x（文件永远不变）。
  // Visual row    = isFlipped ? 7-y : y（白方视角 rank 1 在底部）。
  const visualCol = (x: number) => x;
  // Translate visual row y=0 (top) to logical rank 8.
  const visualRow = (y: number) => (isFlipped ? 7 - y : y);
  // Pixel center of logical (x,y):
  const visualTopPx = (y: number) => PADDING + visualRow(y) * CELL;
  const visualLeftPx = (x: number) => PADDING + visualCol(x) * CELL;

  const handleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left - PADDING;
    const py = e.clientY - rect.top - PADDING;
    if (px < 0 || py < 0) {
      onSelect(null);
      return;
    }
    let dx = Math.floor(px / CELL);
    let dy = Math.floor(py / CELL);
    if (isFlipped) {
      // 文件(a-h)不翻转，仅翻转排名(行)——与 visualCol/visualRow 一致。
      dy = 7 - dy;
    }
    if (dx < 0 || dx > 7 || dy < 0 || dy > 7) {
      onSelect(null);
      return;
    }
    const pos = { x: dx, y: dy };
    const piece = board[dy]?.[dx];
    if (selectedPos) {
      if (piece && piece.color === myColor) {
        onSelect(pos);
        return;
      }
      onMove(selectedPos, pos);
      return;
    }
    if (piece && piece.color === myColor && turn === myColor) {
      onSelect(pos);
    }
  };

  const layers: React.ReactNode[] = [];
  for (let r = 0; r < 8; r++) {
    for (let c = 0; c < 8; c++) {
      const isDark = (r + c) % 2 === 1;
      const left = visualLeftPx(c);
      const top = visualTopPx(r);
      const isSelected =
        selectedPos != null && selectedPos.x === c && selectedPos.y === r;
      const isLegal = legalTargets.some((t) => t.x === c && t.y === r);
      const isLast =
        lastMove != null &&
        ((lastMove.from.x === c && lastMove.from.y === r) ||
          (lastMove.to.x === c && lastMove.to.y === r));

      layers.push(
        <div
          key={`sq-${r}-${c}`}
          style={{
            position: 'absolute',
            left,
            top,
            width: CELL,
            height: CELL,
            background: isDark ? colors.boardDark : colors.boardLight,
            border: isSelected
              ? '2px solid #ffd700'
              : isLegal
                ? '2px dashed rgba(34, 211, 238, 0.65)'
                : '1px solid transparent',
            boxSizing: 'border-box',
          }}
        />,
      );
      if (isLast) {
        layers.push(
          <div
            key={`hl-${r}-${c}`}
            style={{
              position: 'absolute',
              left,
              top,
              width: CELL,
              height: CELL,
              background: 'rgba(255, 215, 0, 0.18)',
              pointerEvents: 'none',
            }}
          />,
        );
      }
      if (isLegal) {
        const pieceAt = board[r]?.[c];
        const size = pieceAt ? CELL * 0.32 : CELL * 0.22;
        layers.push(
          <div
            key={`lt-${r}-${c}`}
            style={{
              position: 'absolute',
              left: left + CELL / 2 - size / 2,
              top: top + CELL / 2 - size / 2,
              width: size,
              height: size,
              borderRadius: '50%',
              background: pieceAt
                ? 'radial-gradient(circle, transparent 36%, rgba(34,211,238,0.65) 38%, rgba(34,211,238,0.65) 100%)'
                : 'rgba(34, 211, 238, 0.55)',
              pointerEvents: 'none',
            }}
          />,
        );
      }
    }
  }

  // Pieces
  const pieces: React.ReactNode[] = [];
  for (let r = 0; r < 8; r++) {
    for (let c = 0; c < 8; c++) {
      const piece = board[r]?.[c];
      if (!piece) continue;
      const left = visualLeftPx(c);
      const top = visualTopPx(r);
      const isSelected =
        selectedPos != null && selectedPos.x === c && selectedPos.y === r;
      pieces.push(
        <div
          key={`pc-${r}-${c}`}
          style={{
            position: 'absolute',
            left: left + (CELL - 52) / 2,
            top: top + (CELL - 52) / 2,
            zIndex: isSelected ? 10 : 1,
          }}
        >
          <ChessPiece
            color={piece.color as PieceColor}
            type={piece.type as any}
            style={boardStyle}
            selected={isSelected}
            size={52}
          />
        </div>,
      );
    }
  }

  // Coordinate labels
  // 文件标签(a-h)跟随 visualCol 排列:底部始终 a..h,顶部在翻转时显示 h..a
  // (对手视线),排名标签按 visualRow 显示正确数字。
  const labels: React.ReactNode[] = [];
  for (let c = 0; c < 8; c++) {
    const left = visualLeftPx(c);
    const topLabelFile = isFlipped ? FILES[7 - c] : FILES[c];
    const bottomLabelFile = FILES[c]; // 底部始终 a..h
    labels.push(
      <div
        key={`ft-${c}`}
        style={{
          position: 'absolute',
          left: left + CELL / 2 - 4,
          top: 4,
          fontSize: 14,
          color: colors.boardLine,
          fontFamily: 'serif',
          fontWeight: 'bold',
          pointerEvents: 'none',
        }}
      >
        {topLabelFile}
      </div>,
    );
    labels.push(
      <div
        key={`fb-${c}`}
        style={{
          position: 'absolute',
          left: left + CELL / 2 - 4,
          top: BOARD_H - PADDING + 6,
          fontSize: 14,
          color: colors.boardLine,
          fontFamily: 'serif',
          fontWeight: 'bold',
          pointerEvents: 'none',
        }}
      >
        {bottomLabelFile}
      </div>,
    );
  }
  for (let r = 0; r < 8; r++) {
    const top = visualTopPx(r);
    labels.push(
      <div
        key={`ll-${r}`}
        style={{
          position: 'absolute',
          left: 6,
          top: top + CELL / 2 - 7,
          fontSize: 14,
          color: colors.boardLine,
          fontFamily: 'serif',
          fontWeight: 'bold',
          pointerEvents: 'none',
        }}
      >
        {(isFlipped ? 8 - r : r + 1).toString()}
      </div>,
    );
    labels.push(
      <div
        key={`lr-${r}`}
        style={{
          position: 'absolute',
          left: BOARD_W - PADDING + 6,
          top: top + CELL / 2 - 7,
          fontSize: 14,
          color: colors.boardLine,
          fontFamily: 'serif',
          fontWeight: 'bold',
          pointerEvents: 'none',
        }}
      >
        {(isFlipped ? 8 - r : r + 1).toString()}
      </div>,
    );
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
        boxShadow: '0 4px 24px rgba(0,0,0,0.4)',
      }}
    >
      {boardBgSrc && !boardBgFailed ? (
        <img
          src={boardBgSrc}
          alt="chess board"
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
            background: colors.boardLight,
          }}
        />
      )}
      {layers}
      {pieces}
      {labels}
    </div>
  );
}
