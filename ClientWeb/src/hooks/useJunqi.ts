import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useJunqiStore } from '@/store/junqi.store';
import type { JunqiPlacement } from '@/types/junqi';

/**
 * Hook that manages the WebSocket game lifecycle for a Junqi (中国军棋) room.
 *
 * WS message types handled:
 *   - game.joined        — initial join ack
 *   - game.peer_joined   — opponent joined, ready to layout
 *   - game.layout_accepted — own layout accepted
 *   - game.layout_submitted — opponent submitted layout
 *   - game.started       — both layouts done, battle phase begins
 *   - game.state         — full state dump (rejoin / refresh)
 *   - game.moved         — a move was executed
 *   - game.over          — game ended
 *   - game.error         — server-side error
 */
export function useJunqi(roomId: string) {
  const {
    setGameState,
    setMyColor,
    setLastMove,
    setGameOver,
    setSettlement,
    selectPos,
  } = useJunqiStore();

  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  const navigate = useNavigate();

  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.')) return;
      const p = env.payload as Record<string, unknown>;

      switch (env.type) {
        case 'game.joined': {
          const c = (p.my_color as 'red' | 'black') || null;
          if (c) setMyColor(c);
          setGameState({
            room_id: p.room_id as string,
            ready: Boolean(p.ready),
            my_color: c ?? undefined,
            mode: (p.mode as 'open' | 'hidden') ?? 'hidden',
            phase: (p.phase as 'layout' | 'playing' | 'over') ?? 'layout',
            status: 'playing',
            move_count: 0,
          });
          break;
        }
        case 'game.peer_joined':
        case 'game.layout_submitted': {
          // Opponent progress notification — refresh state.
          wsClient.send('game.state', { room_id: roomIdRef.current });
          break;
        }
        case 'game.layout_accepted': {
          // We submitted successfully. Wait for the other player.
          break;
        }
        case 'game.started': {
          const prev = useJunqiStore.getState().gameState;
          setGameState({
            ...(prev ?? { room_id: roomIdRef.current, ready: true, status: 'playing' as const, move_count: 0 }),
            ready: true,
            phase: 'playing' as const,
            turn: (p.turn as 'red' | 'black') ?? 'red',
            status: 'playing' as const,
          });
          break;
        }
        case 'game.state': {
          const gs = p as unknown as {
            room_id: string;
            red_id?: string;
            black_id?: string;
            ready: boolean;
            my_color?: 'red' | 'black';
            mode?: 'open' | 'hidden';
            phase?: 'layout' | 'playing' | 'over';
            turn?: 'red' | 'black';
            status?: string;
            board_view?: unknown;
            move_count?: number;
          };
          if (gs.my_color) setMyColor(gs.my_color);
          selectPos(null);
          setGameState({
            room_id: gs.room_id,
            red_id: gs.red_id,
            black_id: gs.black_id,
            ready: gs.ready,
            my_color: gs.my_color,
            mode: gs.mode,
            phase: gs.phase,
            turn: gs.turn,
            status: (gs.status as 'playing' | 'red_win' | 'black_win' | 'draw') ?? 'playing',
            board_view: gs.board_view as never,
            move_count: gs.move_count ?? 0,
          });
          break;
        }
        case 'game.moved': {
          const mr = p as unknown as {
            room_id: string;
            move: import('@/types/junqi').JunqiMove;
            turn: 'red' | 'black';
            status: string;
            phase: string;
            my_color: 'red' | 'black';
            board_view: import('@/types/junqi').JunqiBoardView;
          };
          const prev = useJunqiStore.getState().gameState;
          setGameState({
            ...(prev ?? { room_id: mr.room_id, ready: true, status: 'playing' as const, move_count: 0 }),
            room_id: mr.room_id,
            turn: mr.turn,
            status: mr.status as 'playing' | 'red_win' | 'black_win' | 'draw',
            phase: mr.phase as 'layout' | 'playing' | 'over',
            my_color: mr.my_color,
            board_view: mr.board_view,
            move_count: (prev?.move_count ?? 0) + 1,
          });
          setLastMove(mr.move);
          selectPos(null);
          break;
        }
        case 'game.over': {
          const o = p as unknown as {
            winner: string;
            reason: string;
            ante?: number;
            platformFee?: number;
            netGain?: number;
            finalBalance?: number;
          };
          setGameOver({ winner: o.winner, reason: o.reason });
          // 军棋带注房：提取结算明细。
          if (o.ante && o.ante > 0) {
            const myColorNow = useJunqiStore.getState().myColor;
            let result: 'win' | 'lose' | 'draw' = 'draw';
            if (o.winner === '') {
              result = 'draw';
            } else if (o.winner === myColorNow) {
              result = 'win';
            } else {
              result = 'lose';
            }
            setSettlement({
              ante: o.ante,
              netGain: o.netGain ?? 0,
              platformFee: o.platformFee,
              finalBalance: o.finalBalance,
              result,
            });
          }
          break;
        }
        case 'game.error': {
          const e = p as { code: number; message: string };
          // eslint-disable-next-line no-console
          console.error('junqi game error:', e.code, e.message);
          break;
        }
        case 'game.removed': {
          // 房间被管理员强制解散 / 系统清理 → 跳回大厅。
          // eslint-disable-next-line no-console
          console.warn('junqi: room removed by admin', {
            room_id: roomIdRef.current,
            reason: p.reason,
          });
          navigate('/junqi');
          break;
        }
      }
    });
    return () => unsub();
  }, [setGameState, setMyColor, setLastMove, setGameOver, setSettlement, selectPos, navigate]);

  const joinGame = useCallback(
    (mode: 'open' | 'hidden' = 'hidden') => {
      wsClient.send('game.join', { room_id: roomId, game_kind: 'junqi', mode });
    },
    [roomId],
  );

  // Subscribe as a spectator — server returns a sanitized view (no my_color).
  // Mode is honored so the spectator sees the same hidden/open variant.
  const spectate = useCallback(
    (mode: 'open' | 'hidden' = 'hidden') => {
      wsClient.send('game.spectate', { room_id: roomId, game_kind: 'junqi', mode });
    },
    [roomId],
  );

  // Unsubscribe as a spectator.
  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId, game_kind: 'junqi' });
  }, [roomId]);

  const submitLayout = useCallback(
    (placements: JunqiPlacement[]) => {
      wsClient.send('game.layout', { room_id: roomId, game_kind: 'junqi', placements });
    },
    [roomId],
  );

  const sendMove = useCallback(
    (from: { x: number; y: number }, to: { x: number; y: number }) => {
      wsClient.send('game.move', { room_id: roomId, game_kind: 'junqi', from, to });
    },
    [roomId],
  );

  const resign = useCallback(() => {
    wsClient.send('game.resign', { room_id: roomId, game_kind: 'junqi' });
  }, [roomId]);

  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId, game_kind: 'junqi' });
  }, [roomId]);

  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId, game_kind: 'junqi' });
  }, [roomId]);

  return { joinGame, spectate, unspectate, submitLayout, sendMove, resign, leaveGame, requestState };
}