import { useEffect, useCallback, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useChessStore } from '@/store/chess.store';
import { useChess } from '@/hooks/useChess';
import { useSpectatorMode } from '@/hooks/useSpectatorMode';
import { ChessBoard } from '@/components/chess/ChessBoard';
import { ChessGameInfoPanel } from '@/components/chess/GameInfoPanel';
import { GameChatPanel } from '@/components/chat/GameChatPanel';
import { PromotionDialog } from '@/components/chess/PromotionDialog';
import { useT } from '@/hooks/useT';
import { wsClient } from '@/services/ws';
import { roomService } from '@/services/auth.service';
import type { PieceColor } from '@/assets/images/chess';
import type { ChessPromotion } from '@/types/api';
import type { TKey } from '@/i18n';
import { ConfirmModal } from '@/components/ui/ConfirmModal';

// Generate initial 8x8 board with piece layout.
function makeInitialBoard() {
  const emptyRow = () => Array(8).fill(null);
  const board = Array.from({ length: 8 }, emptyRow);
  const back = ['rook', 'knight', 'bishop', 'queen', 'king', 'bishop', 'knight', 'rook'];
  for (let x = 0; x < 8; x++) {
    board[0][x] = { color: 'white', type: back[x], name: back[x] };
    board[1][x] = { color: 'white', type: 'pawn', name: 'pawn' };
    board[6][x] = { color: 'black', type: 'pawn', name: 'pawn' };
    board[7][x] = { color: 'black', type: back[x], name: back[x] };
  }
  return board;
}

export function ChessGamePage() {
  const { roomId } = useParams<{ roomId: string }>();
  const nav = useNavigate();
  const t = useT();
  const spectator = useSpectatorMode();
  const [resignPromptOpen, setResignPromptOpen] = useState(false);

  const {
    gameState,
    myColor,
    selectedPos,
    legalTargets,
    lastMove,
    style,
    promotionPending,
    selectPos,
    setLegalTargets,
    setPromotionPending,
    reset,
  } = useChessStore();

  const {
    joinGame,
    spectate,
    unspectate,
    sendMove,
    resign,
    leaveGame,
    requestState,
  } = useChess(roomId!);

  // Connect WS and join (or spectate) the game room.
  useEffect(() => {
    if (!roomId) return;
    // 进入对局前清空上一次会话的 store 残留(选子 / 颜色 / 结算),避免 observer
    // 路由上看到旧棋盘或旧手数;再调 useSessionRestore 补 game.spectate/state。
    reset();
    wsClient.connect();
    let retries = 0;
    const tryHook = () => {
      if (retries++ > 10) return;
      const frame = spectator ? 'game.spectate' : 'game.join';
      const sent = wsClient.send(frame, {
        room_id: roomId,
        game_kind: 'chess',
      });
      if (!sent) setTimeout(tryHook, 500);
    };
    const timer = setTimeout(tryHook, 300);
    // 周期同步服务端状态，避免 reload 后只看到初始空棋盘（Bug #4 修复：参照 DoudizhuGamePage）。
    const stateTimer = setInterval(() => requestState(), 8000);
    return () => {
      clearTimeout(timer);
      clearInterval(stateTimer);
      if (spectator) unspectate();
    };
  }, [roomId, spectator, joinGame, spectate, unspectate, requestState, reset]);

  // Compute legal targets when a piece is selected (very lightweight client filter
  // based on board layout — server is still the source of truth).
  useEffect(() => {
    if (!selectedPos || !gameState?.board) {
      setLegalTargets([]);
      return;
    }
    const piece = gameState.board[selectedPos.y]?.[selectedPos.x];
    if (!piece) {
      setLegalTargets([]);
      return;
    }
    // Trust the server via API; for now we just collect empties as a hint.
    const targets: { x: number; y: number }[] = [];
    for (let y = 0; y < 8; y++) {
      for (let x = 0; x < 8; x++) {
        const target = gameState.board[y]?.[x];
        if (!target || target.color !== myColor) {
          targets.push({ x, y });
        }
      }
    }
    setLegalTargets(targets);
  }, [selectedPos, gameState?.board, myColor, setLegalTargets]);

  const handleSelect = useCallback(
    (pos: { x: number; y: number } | null) => {
      selectPos(pos);
    },
    [selectPos],
  );

  const handleMove = useCallback(
    (from: { x: number; y: number }, to: { x: number; y: number }) => {
      // Check if this is a promotion: pawn reaching the back rank.
      const movingPiece = gameState?.board?.[from.y]?.[from.x];
      if (
        movingPiece?.type === 'pawn' &&
        ((movingPiece.color === 'white' && to.y === 7) ||
          (movingPiece.color === 'black' && to.y === 0))
      ) {
        setPromotionPending({ from, to });
        return;
      }
      sendMove(from, to);
      selectPos(null);
    },
    [sendMove, selectPos, setPromotionPending, gameState?.board],
  );

  const handleResign = useCallback(() => {
    setResignPromptOpen(true);
  }, []);

  const confirmResign = useCallback(() => {
    setResignPromptOpen(false);
    resign();
  }, [resign]);

  const cancelResign = useCallback(() => {
    setResignPromptOpen(false);
  }, []);

  const handleLeave = useCallback(async () => {
    if (spectator) {
      unspectate();
      try {
        await roomService.leaveSpectate(roomId!);
      } catch {
        // best-effort
      }
    } else {
      leaveGame();
      try {
        await roomService.leave(roomId!);
      } catch {
        // best-effort
      }
    }
    reset();
    nav('/chess');
  }, [roomId, spectator, leaveGame, unspectate, reset, nav]);

  const handlePromotion = useCallback(
    (choice: ChessPromotion) => {
      if (!promotionPending) return;
      sendMove(
        promotionPending.from,
        promotionPending.to,
        choice,
      );
      setPromotionPending(null);
      selectPos(null);
    },
    [promotionPending, sendMove, setPromotionPending, selectPos],
  );

  if (!roomId) {
    return <div className="error">Missing room ID</div>;
  }

  // Spectators see the board identically to players, but receive no my_color
  // from the server. We force myColor to null so the player-side branches
  // (move buttons, resign controls) are skipped.
  const effectiveColor = spectator ? null : myColor;
  const hasBoard = gameState?.board && gameState.ready;
  const displayBoard = hasBoard && gameState?.board
    ? gameState.board
    : makeInitialBoard();

  return (
    <div className="xiangqi-game">
      <div className="game-area">
        <div className="board-container">
          {hasBoard && effectiveColor ? (
            <ChessBoard
              board={displayBoard}
              myColor={effectiveColor}
              turn={(gameState!.turn as PieceColor) ?? 'white'}
              selectedPos={selectedPos}
              legalTargets={legalTargets}
              lastMove={lastMove}
              boardStyle={style}
              onSelect={handleSelect}
              onMove={handleMove}
            />
          ) : spectator && hasBoard ? (
            <ChessBoard
              board={displayBoard}
              myColor={'white'}
              turn={(gameState!.turn as PieceColor) ?? 'white'}
              selectedPos={null}
              legalTargets={[]}
              lastMove={lastMove}
              boardStyle={style}
              onSelect={() => {}}
              onMove={() => {}}
            />
          ) : (
            <div className="waiting-board">
              <p>{t('chess.waitingOpponent')}</p>
              <div className="spinner" />
            </div>
          )}
        </div>
        <div className="game-sidebar">
          <ChessGameInfoPanel
            myColor={effectiveColor}
            spectator={spectator}
            onResign={handleResign}
            onLeave={handleLeave}
          />
          <GameChatPanel roomId={roomId} />
        </div>
      </div>
      {promotionPending && (
        <PromotionDialog
          onSelect={handlePromotion}
          onCancel={() => setPromotionPending(null)}
        />
      )}
      {resignPromptOpen && (
        <ConfirmModal
          messageKey={'chess.confirmResign' as TKey}
          danger
          onConfirm={confirmResign}
          onCancel={cancelResign}
        />
      )}
    </div>
  );
}
