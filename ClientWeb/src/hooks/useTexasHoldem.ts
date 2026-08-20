import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useTexasHoldemStore } from '@/store/texasholdem.store';
import { reportGlobalError } from '@/services/globalError';
import type { TexasActionType, TexasHoldemGameState } from '@/types/texasholdem';

export function useTexasHoldem(roomId: string) {
  const { setGameState, setMySeat, setGameOver, setLastError } = useTexasHoldemStore();
  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  const navigate = useNavigate();

  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.')) return;
      const p = env.payload as Record<string, unknown>;
      if (p.room_id && p.room_id !== roomIdRef.current) return;

      switch (env.type) {
        case 'game.joined': {
          const seat = typeof p.my_seat === 'number' ? p.my_seat : 0;
          setMySeat(seat);
          break;
        }
        case 'game.state': {
          const gs = p as unknown as TexasHoldemGameState;
          setGameState(gs);
          // 观察者的 my_seat 为 -1；只在合法座位上写 store，避免覆盖本会话的座位。
          if (typeof gs.my_seat === 'number' && gs.my_seat >= 0) {
            setMySeat(gs.my_seat);
          }
          // §R7 P1 修复 — game.state 到达且状态非 over/showdown 时清掉上一局的 gameOver,
          // 避免「胜者 #4」横幅跨手残留(实测 hand1 结束 → hand2 preflop 期间仍显示)。
          if (gs.status !== 'over' && gs.status !== 'showdown') {
            setGameOver(null);
          }
          break;
        }
        case 'game.over': {
          const o = p as unknown as { winners: number[]; reason: string };
          setGameOver({ winners: o.winners, reason: o.reason });
          break;
        }
        case 'game.error': {
          // §20260819-02 P0-2 — game.error 必须按 §7.1 报到全局 toast,不允许
          // 只 console.error(此前 5 款游戏 4 个 hook 都违反此规约,本轮只修
          // 德扑)。同时写 store.lastError,让 TexasHoldemGamePage 就地显示
          // 错误 banner(§7.1「在当前页面以最高层级显示给用户」,不依赖 15s
          // 兜底)。对齐 useWerewolf.ts:166-173 的同款处理。
          const e = p as { code: number; message: string };
          setLastError({ code: e.code, message: e.message });
          reportGlobalError({
            message: e.message || `游戏操作失败(code=${e.code})`,
            severity: 'error',
          });
          break;
        }
        case 'game.removed': {
          // 房间被管理员强制解散 / 系统清理 → 跳回大厅。
          // eslint-disable-next-line no-console
          console.warn('texasholdem: room removed by admin', {
            room_id: roomIdRef.current,
            reason: p.reason,
          });
          navigate('/texasholdem');
          break;
        }
      }
    });
    return () => unsub();
  }, [setGameState, setMySeat, setGameOver, setLastError, navigate]);

  const joinGame = useCallback(() => {
    wsClient.send('game.join', { room_id: roomId, game_kind: 'texasholdem' });
  }, [roomId]);

  // Subscribe as a spectator — server returns the sanitized view (no my_hole).
  const spectate = useCallback(() => {
    wsClient.send('game.spectate', { room_id: roomId, game_kind: 'texasholdem' });
  }, [roomId]);

  // Unsubscribe as a spectator.
  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId, game_kind: 'texasholdem' });
  }, [roomId]);

  const sendAction = useCallback(
    (type: TexasActionType, amount?: number) => {
      wsClient.send('game.action', {
        room_id: roomId,
        game_kind: 'texasholdem',
        type,
        ...(amount !== undefined ? { amount } : {}),
      });
    },
    [roomId],
  );

  const resign = useCallback(() => {
    wsClient.send('game.resign', { room_id: roomId, game_kind: 'texasholdem' });
  }, [roomId]);

  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId, game_kind: 'texasholdem' });
  }, [roomId]);

  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId, game_kind: 'texasholdem' });
  }, [roomId]);

  return { joinGame, spectate, unspectate, sendAction, resign, leaveGame, requestState };
}
