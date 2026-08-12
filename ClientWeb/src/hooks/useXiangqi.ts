import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useXiangqiStore } from '@/store/xiangqi.store';
import type {
  XiangqiGameState,
  XiangqiMoveResult,
  XiangqiGameOverDetail,
} from '@/types/api';

/**
 * Hook that manages the WebSocket game lifecycle for a Xiangqi room.
 */
export function useXiangqi(roomId: string) {
  const {
    setGameState,
    setMyColor,
    setLastMove,
    setGameOver,
    setSettlement,
    selectPos,
  } = useXiangqiStore();

  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  const navigate = useNavigate();

  // Subscribe to game.* WS messages.
  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.')) return;

      switch (env.type) {
        case 'game.joined': {
          const p = env.payload as { my_color: string; ready: boolean };
          setMyColor(p.my_color as 'red' | 'black');
          break;
        }
        case 'game.started': {
          const p = env.payload as XiangqiGameState & { move_count?: number };
          // Only adopt my_color from this frame when the server actually set it
          // (the direct `sendOK` ack does; the room-wide `BroadcastRoom` echo
          // does not). Otherwise the broadcast would clobber the value the
          // second player (黑方) just received and leave them stuck on the
          // waiting board. See TestReport Bug #1.
          if (p.my_color) setMyColor(p.my_color as 'red' | 'black');
          // 重连场景下服务端会把真实 move_count / board / turn 一并下发,
          // 这里必须保留服务端字段;否则 UI 永远停在初始 (回合 0、满盘开局)。
          // 详见 BUG-XIANGQI-RELOAD-MOVE-COUNT (TestReport 023903 Bug #4 同源)。
          setGameState({
            room_id: p.room_id,
            red_id: p.red_id,
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
          const p = env.payload as XiangqiGameState;
          // 收到服务端状态：覆盖 store、收尾选子高亮。仅当 server 显式给了
          // my_color 时才更新（observer 的 game.state 中 my_color 为空串），
          // 否则会清空本会话的颜色，触发 XiangqiGamePage 的"无颜色"分支。
          setGameState(p);
          if (p.my_color) setMyColor(p.my_color as 'red' | 'black');
          selectPos(null);
          break;
        }
        case 'game.moved': {
          const p = env.payload as XiangqiMoveResult;
          const current = useXiangqiStore.getState().gameState;
          if (current) {
            setGameState({
              ...current,
              board: p.board,
              turn: p.turn as 'red' | 'black',
              status: p.status as XiangqiGameState['status'],
              check: p.check,
              move_count: current.move_count + 1,
            });
          }
          setLastMove(p.move);
          selectPos(null);
          break;
        }
        case 'game.over': {
          const p = env.payload as XiangqiGameOverDetail;
          setGameOver({ winner: p.winner, reason: p.reason });
          // 带注房：提取结算明细，写入 store 供结算弹层使用。
          if (p.ante && p.ante > 0) {
            const myColorNow = useXiangqiStore.getState().myColor;
            let result: 'win' | 'lose' | 'draw' = 'draw';
            if (p.winner === '') {
              result = 'draw';
            } else if (p.winner === myColorNow) {
              result = 'win';
            } else {
              result = 'lose';
            }
            setSettlement({
              ante: p.ante,
              netGain: p.netGain ?? 0,
              streakBonus: p.streakBonus,
              finalBalance: p.finalBalance,
              result,
            });
          }
          break;
        }
        case 'game.error': {
          const p = env.payload as { code: number; message: string };
          console.error('game error:', p.code, p.message);
          break;
        }
        case 'game.removed': {
          // 房间被管理员强制解散 / 系统清理 → 跳回大厅。
          const p = env.payload as { room_id?: string; reason?: string };
          // eslint-disable-next-line no-console
          console.warn('xiangqi: room removed by admin', {
            room_id: p.room_id ?? roomIdRef.current,
            reason: p.reason,
          });
          navigate('/xiangqi');
          break;
        }
      }
    });

    return () => unsub();
  }, [setGameState, setMyColor, setLastMove, setGameOver, setSettlement, selectPos, navigate]);

  // Join the game room via WS. Backend is idempotent — safe to call multiple times.
  const joinGame = useCallback(() => {
    wsClient.send('game.join', { room_id: roomId });
  }, [roomId]);

  // Subscribe as a spectator — server returns a sanitized view (no my_color).
  const spectate = useCallback(() => {
    wsClient.send('game.spectate', { room_id: roomId });
  }, [roomId]);

  // Unsubscribe as a spectator — counterpart to spectate().
  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId });
  }, [roomId]);

  // Send a move.
  const sendMove = useCallback(
    (from: { x: number; y: number }, to: { x: number; y: number }) => {
      wsClient.send('game.move', { room_id: roomId, from, to });
    },
    [roomId],
  );

  // Resign.
  const resign = useCallback(() => {
    wsClient.send('game.resign', { room_id: roomId });
  }, [roomId]);

  // Leave game.
  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId });
  }, [roomId]);

  // Request current state.
  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId });
  }, [roomId]);

  return { joinGame, spectate, unspectate, sendMove, resign, leaveGame, requestState };
}
