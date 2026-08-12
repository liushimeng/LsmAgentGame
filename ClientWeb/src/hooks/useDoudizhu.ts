import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { useDoudizhuStore } from '@/store/doudizhu.store';
import type { DoudizhuCard, DoudizhuGameState } from '@/types/doudizhu';

/**
 * useDoudizhu — 订阅斗地主 WS 帧，写入 store，提供操作函数。
 *
 * 帧类型：
 *   服务端→客户端：game.joined / game.peer_joined / game.started / game.state
 *                   / game.bidded / game.played / game.passed / game.redealt
 *                   / game.over / game.error
 *   客户端→服务端：game.join / game.bid / game.play / game.pass / game.resign
 *                   / game.leave / game.state
 */
export function useDoudizhu(roomId: string) {
  const {
    setGameState,
    setMySeat,
    setGameOver,
    clearSelected,
  } = useDoudizhuStore();

  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  const navigate = useNavigate();

  // 订阅 game.* 帧
  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.')) return;
      const p = env.payload as Record<string, unknown>;
      // 只处理当前房间的帧
      if (p.room_id && p.room_id !== roomIdRef.current) return;

      switch (env.type) {
        case 'game.joined': {
          const seat = typeof p.my_seat === 'number' ? p.my_seat : 0;
          setMySeat(seat);
          // joined 帧不含完整状态，等后续 game.state
          break;
        }
        case 'game.peer_joined': {
          // 有人加入，等 game.state 更新人数
          break;
        }
        case 'game.started': {
          // 满员发牌或叫完地主进入出牌：等 game.state 刷新
          break;
        }
        case 'game.state': {
          const gs = p as unknown as DoudizhuGameState;
          setGameState(gs);
          // 观察者模式下 my_seat 为 -1；只在显式有效时才写 store。
          if (typeof gs.my_seat === 'number' && gs.my_seat >= 0) {
            setMySeat(gs.my_seat);
          }
          clearSelected();
          break;
        }
        case 'game.bidded':
        case 'game.played':
        case 'game.passed':
        case 'game.redealt': {
          // 操作已由服务端确认，等 game.state 刷新
          clearSelected();
          break;
        }
        case 'game.over': {
          const o = p as unknown as { winner: string; reason: string };
          setGameOver({ winner: o.winner, reason: o.reason });
          break;
        }
        case 'game.error': {
          const e = p as { code: number; message: string };
          console.error('doudizhu game error:', e.code, e.message);
          break;
        }
        case 'game.removed': {
          // 房间被管理员强制解散 / 系统清理 → 跳回大厅。
          // eslint-disable-next-line no-console
          console.warn('doudizhu: room removed by admin', {
            room_id: roomIdRef.current,
            reason: p.reason,
          });
          navigate('/doudizhu');
          break;
        }
      }
    });
    return () => unsub();
  }, [setGameState, setMySeat, setGameOver, clearSelected, navigate]);

  // ── 操作函数 ──

  const joinGame = useCallback(() => {
    wsClient.send('game.join', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  // Subscribe as a spectator — server returns the sanitized view (no my_hand).
  const spectate = useCallback(() => {
    wsClient.send('game.spectate', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  // Unsubscribe as a spectator.
  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  const bid = useCallback(
    (score: number) => {
      wsClient.send('game.bid', { room_id: roomId, game_kind: 'doudizhu', score });
    },
    [roomId],
  );

  const play = useCallback(
    (cards: DoudizhuCard[]) => {
      wsClient.send('game.play', { room_id: roomId, game_kind: 'doudizhu', cards });
    },
    [roomId],
  );

  const pass = useCallback(() => {
    wsClient.send('game.pass', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  const resign = useCallback(() => {
    wsClient.send('game.resign', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId, game_kind: 'doudizhu' });
  }, [roomId]);

  return { joinGame, spectate, unspectate, bid, play, pass, resign, leaveGame, requestState };
}
