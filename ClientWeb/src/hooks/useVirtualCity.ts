/**
 * useVirtualCity — 订阅虚拟城市 WS 帧，写入 store，提供操作函数。
 *
 * 帧协议（lag_docs/虚拟城市/已实现/02-架构设计/虚拟城市-WS与HTTP协议契约-v1.md）：
 *   C→S：game.join / game.leave / game.spectate / game.unspectate / game.state
 *         / game.virtual_city_action{room_id, action:{type,…}}
 *         / game.virtual_city_start{room_id} / game.virtual_city_pause{room_id, pause}
 *   S→C：game.joined / game.started / game.state（按座位脱敏单发）
 *         / game.event{month, seat?, type, text} / game.month（全房月度汇总）
 *         / game.over / game.error / game.removed
 *
 * WS 连接由 AppLayout 唯一持有（§16）；本 hook 只订阅 game.* 帧并过滤 room_id。
 */

import { useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { wsClient, type WsEnvelope } from '@/services/ws';
import { reportGlobalError } from '@/services/globalError';
import { useVirtualCityStore } from '@/store/virtualCity.store';
import type {
  VirtualCityAction,
  VirtualCityErrorFrame,
  VirtualCityEventFrame,
  VirtualCityGameState,
  VirtualCityJoinedFrame,
  VirtualCityListingBook,
  VirtualCityMonthFrame,
  VirtualCityOverFrame,
  VirtualCityStartedFrame,
  VirtualCitySurvey,
  VirtualCityTradeAction,
} from '@/types/virtualCity';

export function useVirtualCity(roomId: string) {
  const {
    setGameState,
    setMySeat,
    setStartedInfo,
    pushEvent,
    applyMonthFrame,
    setGameOver,
    setLastError,
    mergeSurvey,
    setListingBook,
    mergeListingBook,
  } = useVirtualCityStore();

  const roomIdRef = useRef(roomId);
  roomIdRef.current = roomId;
  const navigate = useNavigate();

  // 订阅 game.* 帧
  useEffect(() => {
    const unsub = wsClient.on((env: WsEnvelope) => {
      if (!env.type.startsWith('game.')) return;
      const p = env.payload as Record<string, unknown>;
      // 只处理当前房间的帧（game.joined 等无 room_id 的帧不过滤）。
      if (p.room_id && p.room_id !== roomIdRef.current) return;

      switch (env.type) {
        case 'game.joined': {
          const f = p as unknown as VirtualCityJoinedFrame;
          if (typeof f.my_seat === 'number' && f.my_seat >= 0) {
            setMySeat(f.my_seat);
          }
          break;
        }
        case 'game.started': {
          setStartedInfo(p as unknown as VirtualCityStartedFrame);
          break;
        }
        case 'game.state': {
          const gs = p as unknown as VirtualCityGameState;
          setGameState(gs);
          if (typeof gs.my_seat === 'number' && gs.my_seat >= 0) {
            setMySeat(gs.my_seat);
          }
          // 新对局开始（月回卷）时清掉上一局的终局态。
          if (gs.status !== 'over') setGameOver(null);
          // P1 社会调研：快照携带 surveys（open + 最近 4 closed）作种子/刷新。
          // 逐条 merge 而非整表替换——保住 SurveyPanel 已拉取的更长历史（≤20）。
          if (Array.isArray(gs.surveys)) {
            gs.surveys.forEach((sv: VirtualCitySurvey) => mergeSurvey(sv));
          }
          break;
        }
        case 'game.event': {
          const ev = p as unknown as VirtualCityEventFrame;
          pushEvent(ev);
          // 产品设计 §8.2：市场周期切换 / 大幅波动 → 全屏级提示。
          if (ev.type === 'market') {
            reportGlobalError({ message: ev.text, severity: 'info' });
          }
          break;
        }
        case 'game.month': {
          applyMonthFrame(p as unknown as VirtualCityMonthFrame);
          break;
        }
        case 'game.survey_result': {
          // P1 社会调研系统 §5.2：调研关闭（deadline / 全员已答）时广播。
          // 载荷 {room_id, survey: SurveyJSON} → 按 id 去重覆盖进 store.surveys。
          const sv = (p as { survey?: VirtualCitySurvey }).survey;
          if (sv && sv.id) mergeSurvey(sv);
          break;
        }
        // ── P2 交易系统帧（§8.1）──
        case 'game.listings': {
          // 挂单簿全量快照（game.virtualCity_listing_view 响应）。
          setListingBook(p as unknown as VirtualCityListingBook);
          break;
        }
        case 'game.listing_created':
        case 'game.listing_cancelled': {
          const listing = (p as { listing?: VirtualCityListingBook['listings'][number] }).listing;
          if (listing) mergeListingBook({ listings: [listing] });
          break;
        }
        case 'game.negotiate_started':
        case 'game.negotiate_responded': {
          const neg = (p as { negotiate?: VirtualCityListingBook['negotiates'][number] }).negotiate;
          if (neg) mergeListingBook({ negotiates: [neg] });
          break;
        }
        case 'game.loan_contract':
        case 'game.loan_repaid': {
          const loan = (p as { loan?: VirtualCityListingBook['p2p_loans'][number] }).loan;
          if (loan) mergeListingBook({ p2p_loans: [loan] });
          break;
        }
        case 'game.auction_bid':
        case 'game.auction_ended': {
          const auction = (p as { auction?: VirtualCityListingBook['auctions'][number] }).auction;
          if (auction) mergeListingBook({ auctions: [auction] });
          break;
        }
        case 'game.over': {
          setGameOver(p as unknown as VirtualCityOverFrame);
          break;
        }
        case 'game.error': {
          // §7.1：game.error 双通道 —— store.lastError 页面就地 banner
          // + reportGlobalError 全局 toast（useTexasHoldem 同款处理）。
          const e = p as unknown as VirtualCityErrorFrame;
          setLastError({ code: e.code, message: e.message });
          reportGlobalError({
            message: e.message || `游戏操作失败(code=${e.code})`,
            severity: 'error',
          });
          break;
        }
        case 'game.removed': {
          // 终局 60s 后房间清理 / 管理员解散 → 跳回大厅。
          // eslint-disable-next-line no-console
          console.warn('virtualCity: room removed', {
            room_id: roomIdRef.current,
            reason: p.reason,
          });
          navigate('/virtual-city');
          break;
        }
      }
    });
    return () => unsub();
  }, [
    setGameState, setMySeat, setStartedInfo, pushEvent,
    applyMonthFrame, setGameOver, setLastError, mergeSurvey,
    setListingBook, mergeListingBook, navigate,
  ]);

  // ── 操作函数 ──

  const joinGame = useCallback(() => {
    wsClient.send('game.join', { room_id: roomId, game_kind: 'virtual_city' });
  }, [roomId]);

  const spectate = useCallback(() => {
    wsClient.send('game.spectate', { room_id: roomId, game_kind: 'virtual_city' });
  }, [roomId]);

  const unspectate = useCallback(() => {
    wsClient.send('game.unspectate', { room_id: roomId });
  }, [roomId]);

  const leaveGame = useCallback(() => {
    wsClient.send('game.leave', { room_id: roomId });
  }, [roomId]);

  const requestState = useCallback(() => {
    wsClient.send('game.state', { room_id: roomId, game_kind: 'virtual_city' });
  }, [roomId]);

  /** 人类动作：game.virtual_city_action{room_id, action:{type,…}}（协议 §4）。 */
  const sendAction = useCallback(
    (action: VirtualCityAction) => {
      wsClient.send('game.virtual_city_action', { room_id: roomId, action });
    },
    [roomId],
  );

  /**
   * 房主提前开始（`game.virtual_city_start`）。
   * 2026-09-16 §财商流10–12座位：后端要求已占座 ≥ VIRTUAL_CITY_MIN_SEATS(10)，
   * 不足返回 35003 ErrWealthNotEnoughPlayers；占座达 10 或满 12 座时后端在
   * game.join / RegisterAgentSeats 阶段已自动开局，通常无需再发此帧。
   */
  const startEarly = useCallback(() => {
    wsClient.send('game.virtual_city_start', { room_id: roomId });
  }, [roomId]);

  /** 房主暂停 / 恢复（月结完成后生效）。 */
  const sendPause = useCallback(
    (pause: boolean, reason?: string) => {
      wsClient.send('game.virtual_city_pause', { room_id: roomId, pause, ...(reason ? { reason } : {}) });
    },
    [roomId],
  );

  /**
   * P2 交易动作（§8.1）：走 game.virtualCity_xxx 帧（不走 game.virtual_city_action）。
   * 帧名 = `game.virtualCity_${action.type}`，载荷 = { room_id, ...action }。
   */
  const sendTrade = useCallback(
    (action: VirtualCityTradeAction) => {
      wsClient.send(`game.virtualCity_${action.type}`, { room_id: roomId, ...action });
    },
    [roomId],
  );

  return {
    joinGame, spectate, unspectate, leaveGame, requestState,
    sendAction, sendTrade, startEarly, sendPause,
  };
}
