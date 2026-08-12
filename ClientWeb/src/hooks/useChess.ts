import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useChessStore } from '@/store/chess.store';
import type {
  ChessGameState,
  ChessMove,
  ChessPromotion,
} from '@/types/api';

/**
 * Hook that manages the WebSocket game lifecycle for an International Chess room.
 */
export function useChess(roomId: string) {
  const {
    setGameState,
    setMyColor,
    setLastMove,
    setGameOver,
    selectPos,
    setPromotionPending,
  } = useChessStore();

  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  const navigate = useNavigate();

  // Subscribe to game.* WS messages.
  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.')) return;

      switch (env.type) {
        case 'game.joined': {
          const p = env.payload as { my_color: string; ready: boolean; game_kind?: string };
          if (p.game_kind && p.game_kind !== 'chess') return;
          setMyColor(p.my_color as 'white' | 'black');
          break;
        }
        case 'game.started': {
          const p = env.payload as ChessGameState & { game_kind?: string };
          if (p.game_kind && p.game_kind !== 'chess') return;
          // Only adopt my_color from this frame when the server actually set it
          // (the direct `sendOK` ack does; the room-wide `BroadcastRoom` echo
          // does not). Otherwise the broadcast would clobber the value the
          // second player (黑方) just received and leave them stuck on the
          // waiting board. See TestReport Bug #1.
          if (p.my_color) setMyColor(p.my_color as 'white' | 'black');
          // 重连场景下服务端会把真实 move_count / board / turn 一并下发,
          // 这里必须保留服务端字段;否则 UI 永远停在初始 (回合 0、32 子开局)。
          // 详见 BUG-CHESS-RELOAD-MOVE-COUNT (TestReport 023903 Bug #4)。
          setGameState({
            room_id: p.room_id,
            white_id: p.white_id,
            black_id: p.black_id,
            ready: true,
            board: p.board,
            turn: p.turn,
            my_color: p.my_color,
            status: p.status,
            check: false,
            move_count: typeof p.move_count === 'number' ? p.move_count : 0,
          });
          break;
        }
        case 'game.state': {
          const p = env.payload as ChessGameState & { game_kind?: string };
          if (p.game_kind && p.game_kind !== 'chess') return;
          setGameState(p);
          if (p.my_color) setMyColor(p.my_color as 'white' | 'black');
          selectPos(null);
          break;
        }
        case 'game.moved': {
          const p = env.payload as {
            room_id: string;
            game_kind?: string;
            move: ChessMove;
            turn: string;
            status: string;
            check: boolean;
            board: ChessGameState['board'];
          };
          if (p.game_kind && p.game_kind !== 'chess') return;
          const current = useChessStore.getState().gameState;
          if (current) {
            setGameState({
              ...current,
              board: p.board,
              turn: p.turn as 'white' | 'black',
              status: p.status as ChessGameState['status'],
              check: p.check,
              move_count: current.move_count + 1,
            });
          }
          setLastMove(p.move);
          selectPos(null);
          // If pawn reached last rank, surface the promotion dialog.
          if (
            p.move?.piece?.type === 'pawn' &&
            ((p.move.piece.color === 'white' && p.move.to.y === 7) ||
              (p.move.piece.color === 'black' && p.move.to.y === 0))
          ) {
            // Server already promoted (defaults to Queen). Client may want to vary.
            setPromotionPending(null);
          } else {
            setPromotionPending(null);
          }
          break;
        }
        case 'game.over': {
          const p = env.payload as { winner: string; reason: string; game_kind?: string };
          if (p.game_kind && p.game_kind !== 'chess') return;
          setGameOver({ winner: p.winner, reason: p.reason });
          break;
        }
        case 'game.error': {
          const p = env.payload as { code: number; message: string };
          console.error('chess game error:', p.code, p.message);
          break;
        }
        case 'game.removed': {
          // 房间被管理员强制解散 / 系统清理 → 跳回大厅。
          const p = env.payload as { room_id?: string; reason?: string };
          // eslint-disable-next-line no-console
          console.warn('chess: room removed by admin', {
            room_id: p.room_id ?? roomIdRef.current,
            reason: p.reason,
          });
          navigate('/chess');
          break;
        }
      }
    });

    return () => unsub();
  }, [setGameState, setMyColor, setLastMove, setGameOver, selectPos, setPromotionPending, navigate]);

  // Join the chess room via WS. Backend is idempotent.
  const joinGame = useCallback(() => {
    wsClient.send('game.join', { room_id: roomId, game_kind: 'chess' });
  }, [roomId]);

  // Subscribe as a spectator — server returns a sanitized view (no my_color).
  const spectate = useCallback(() => {
    wsClient.send('game.spectate', { room_id: roomId, game_kind: 'chess' });
  }, [roomId]);

  // Unsubscribe as a spectator.
  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId, game_kind: 'chess' });
  }, [roomId]);

  // Send a move. `promotion` may be supplied for pawn-reaches-last-rank cases.
  const sendMove = useCallback(
    (from: { x: number; y: number }, to: { x: number; y: number }, promotion?: ChessPromotion) => {
      wsClient.send('game.move', {
        room_id: roomId,
        game_kind: 'chess',
        from,
        to,
        ...(promotion ? { promotion } : {}),
      });
    },
    [roomId],
  );

  // Resign.
  const resign = useCallback(() => {
    wsClient.send('game.resign', { room_id: roomId, game_kind: 'chess' });
  }, [roomId]);

  // Leave game.
  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId, game_kind: 'chess' });
  }, [roomId]);

  // Request current state (used after reconnect).
  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId, game_kind: 'chess' });
  }, [roomId]);

  return { joinGame, spectate, unspectate, sendMove, resign, leaveGame, requestState };
}
