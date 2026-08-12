/**
 * 狼人杀 13 人标准竞技局 Zustand store (历史兼容 12/7 人局)
 *
 * 与 store/{texasholdem,xiangqi,chess,junqi,doudizhu}.store.ts 同构。
 * 进入对局页前 reset() 会被 GamePage 调用以清掉上一个会话残留。
 * 2026-07-10: 新增 patchSheriffStream / patchIdiotRevealed —— 对齐 12/13 人局 §14 WS 帧
 * (game.sheriff_stream_settle / game.idiot_revealed) 的前端状态镜像。
 */

import { create } from 'zustand';
import type { RoomInfo, WerewolfSettlement } from '@/types/api';
import type {
  CommentaryLineJSON,
  PropUseEvent,
  WerewolfGameState,
  WerewolfProp,
  WerewolfStyle,
} from '@/types/werewolf';

interface WerewolfStore {
  rooms: RoomInfo[];
  currentRoom: RoomInfo | null;
  gameState: WerewolfGameState | null;
  mySeat: number;
  style: WerewolfStyle;
  gameOver: { winner: string } | null;
  /** 最近一局结算明细。收到后端 game.settlement 帧后由 useWerewolf 写入;
   *  WerewolfGamePage 据此渲染 SettlementModal。人类玩家每局结束后刷新。 */
  settlement: WerewolfSettlement | null;
  /** §20260811-09 U1 — 观战模式 AI 解说 feed(最近 20 条;非观战者保留空数组)。 */
  commentaryFeed: CommentaryLineJSON[];
  /** 追加一条解说(seq 单调去重,FIFO 保留 20 条)。spectator-only。 */
  pushCommentary: (line: CommentaryLineJSON) => void;
  /** 一次性灌入初始 feed(从 game.state.commentary_feed 解析,避免重连后丢失)。 */
  setCommentaryFeed: (feed: CommentaryLineJSON[]) => void;
  /** Night action submission in-flight guards (per action key). */
  busy: boolean;

  // 2026-07-21 §13 道具系统 — 道具目录缓存。GET /api/games/werewolf/props 时填入。
  props: WerewolfProp[];
  /** 当前用户金币余额(由 REST 响应或 game.state.prop_my_balance 写入)。
   * §R183-P2-1 修复:`null` 表示尚未拉取(loading 哨兵);`>=0` 表示真实余额;
   * 历史版本用 `-1` 作 sentinel,被 §7.1 + UI 误展示为「金币余额 -1」歧义文本,故弃用。 */
  propMyBalance: number | null;
  /** v5 EconTier 当前档位。后端 ComputeEconTier 输出,缺省按 health 处理。 */
  propEconTier: 'boom' | 'health' | 'caution' | 'danger' | 'critical';
  /** v5 EconTier 当前销毁率(20/30/40/45/60 百分比)。 */
  propEconTierAbsorbPct: number;
  /** 当前用户本局剩余可购买次数。 */
  propMyRemaining: number;
  /** 冷却剩余秒数。0 = 可立即使用。 */
  propCooldownRemainingSec: number;
  /** 最近 50 条道具使用公开事件(挂在 game.state.prop_events[] 上,前端累加)。 */
  propRecentEvents: PropUseEvent[];
  /**
   * 2026-07-23 §道具特效:最新一条道具使用的去重快照 + 单调递增 seq。
   * appendPropEvent 每次"语义新事件"到来时改写本指针并 +1 seq;
   * PropUseOverlay 订阅 (lastPropEvent, lastPropSeq) 变化触发动画。
   * 用 seq 而不仅是对象引用,避免同对象覆盖时 React 不重渲染。
   */
  lastPropEvent: PropUseEvent | null;
  lastPropSeq: number;
  /**
   * 2026-07-23 §道具特效:当前被道具击中的目标座位(高亮用)。
   * -1 = 无高亮;>= 0 = 目标座位;PropUseOverlay 在展示期写入,超时后复位 -1。
   * 与 lastPropSeq 同步,保证 SeatCell 高亮与覆盖层动画同生命周期。
   */
  propTargetSeat: number;
  /**
   * 2026-07-30 BUG-FIX: §130 人类等待窗口 server deadline (Unix 毫秒)。
   * 收到 game.pre_wait 帧时写入;StartGame 触发后(收到 phase != filling 的
   * game.state)由 reset() 清零。null = 未在等待窗口(无需特殊 UI)。
   * 用途:WerewolfGamePage 据此把"等待 13 位玩家入座…"改画为
   * "等待人类玩家…(N 秒后自动开始)" + 倒计时,避免 12AI+1 人类房间永久卡死。
   */
  preWaitDeadlineAt: number | null;

  setRooms: (rooms: RoomInfo[]) => void;
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  removeRoom: (roomId: string) => void;
  setCurrentRoom: (room: RoomInfo | null) => void;
  setGameState: (state: WerewolfGameState | null) => void;
  // 2026-07-10 12/13 人局: 把服务端下发的警徽流状态镜像到 gameState.sheriff_streams。
  patchSheriffStream: (streams: number[]) => void;
  // 2026-07-10 12/13 人局: 白痴翻牌结果追加到 gameState.idiot_revealed_seats。
  patchIdiotRevealed: (seat: number, revealed: boolean) => void;
  setMySeat: (seat: number) => void;
  setStyle: (style: WerewolfStyle) => void;
  setGameOver: (over: { winner: string } | null) => void;
  // 2026-07-17 金池结算 — 写入最近一局结算明细,供 SettlementModal 渲染。
  setSettlement: (s: WerewolfSettlement | null) => void;
  // 2026-07-17 金池结算 — 关闭结算弹层(同时清空 settlement,为下一局做准备)。
  dismissSettlement: () => void;
  setBusy: (busy: boolean) => void;
  // 2026-07-21 §13 道具系统 — 道具目录 + 我的余额/次数/冷却。
  setProps: (
    props: WerewolfProp[],
    myBalance: number | null,
    myRemaining: number,
    cooldownSec: number,
    econTier?: 'boom' | 'health' | 'caution' | 'danger' | 'critical',
    econTierAbsorbPct?: number,
  ) => void;
  // 2026-07-21 §13 道具系统 — 累加最近事件流(去重 by at+from+prop_key,最多 50 条)。
  // 2026-07-23 §道具特效:同时改写 lastPropEvent + lastPropSeq,驱动 PropUseOverlay。
  appendPropEvent: (ev: PropUseEvent) => void;
  // 2026-07-23 §道具特效:设置/复位当前道具目标座位高亮(-1 复位)。
  setPropTargetSeat: (seat: number) => void;
  // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 记录 server deadline (Unix 毫秒)。
  setPreWaitDeadlineAt: (deadlineMs: number | null) => void;
  reset: () => void;
}

export const useWerewolfStore = create<WerewolfStore>((set) => ({
  rooms: [],
  currentRoom: null,
  gameState: null,
  mySeat: -1,
  style: 'dark_medieval',
  gameOver: null,
  settlement: null,
  busy: false,
  // 2026-07-21 §13 道具系统 — 初始状态。
  props: [],
  propMyBalance: null,
  propMyRemaining: 0,
  propCooldownRemainingSec: 0,
  propEconTier: 'health' as const,
  propEconTierAbsorbPct: 30,
  propRecentEvents: [],
  // 2026-07-23 §道具特效:初始无最新事件 / 无目标高亮。
  lastPropEvent: null,
  lastPropSeq: 0,
  propTargetSeat: -1,
  // §20260811-09 U1 — 观战模式 AI 解说 feed 初始空(非观战者始终为空数组)。
  commentaryFeed: [],
  // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 初始无等待。
  preWaitDeadlineAt: null,

  setRooms: (rooms) => set({ rooms }),
  patchRoom: (room) =>
    set((s) => ({
      rooms: s.rooms.map((r) =>
        r.id === room.id ? { ...r, ...room } as typeof r : r,
      ),
    })),
  removeRoom: (roomId) =>
    set((s) => ({ rooms: s.rooms.filter((r) => r.id !== roomId) })),
  setCurrentRoom: (room) => set({ currentRoom: room }),
  setGameState: (state) => set({ gameState: state }),
  patchSheriffStream: (streams) =>
    set((s) => {
      if (!s.gameState) return {};
      return { gameState: { ...s.gameState, sheriff_streams: streams } };
    }),
  patchIdiotRevealed: (seat, revealed) =>
    set((s) => {
      if (!s.gameState) return {};
      const prev = s.gameState.idiot_revealed_seats ?? [];
      const next = revealed && !prev.includes(seat) ? [...prev, seat] : prev;
      return { gameState: { ...s.gameState, idiot_revealed_seats: next } };
    }),
  setMySeat: (seat) => set({ mySeat: seat }),
  // §20260811-09 U1 — 追加解说(seq 去重,FIFO ≤ 20;与后端 ring buffer 一致)。
  pushCommentary: (line) =>
    set((s) => {
      const seq = line.seq;
      if (s.commentaryFeed.some((x) => x.seq === seq)) return {};
      const next = [...s.commentaryFeed, line];
      if (next.length > 20) next.splice(0, next.length - 20);
      return { commentaryFeed: next };
    }),
  setCommentaryFeed: (feed) => set({ commentaryFeed: feed.slice(-20) }),
  setStyle: (style) => set({ style }),
  setGameOver: (over) => set({ gameOver: over }),
  setSettlement: (s) => set({ settlement: s }),
  dismissSettlement: () => set({ settlement: null }),
  setBusy: (busy) => set({ busy }),
  // 2026-07-21 §13 道具系统 — 同时设置目录与我的余额/次数/冷却。
  // v5:可选 econTier + econTierAbsorbPct,后端 PropListResponse 透传。
  setProps: (props, myBalance, myRemaining, cooldownSec, econTier, econTierAbsorbPct) =>
    set({
      props,
      propMyBalance: myBalance,
      propMyRemaining: myRemaining,
      propCooldownRemainingSec: cooldownSec,
      propEconTier: econTier ?? 'health',
      propEconTierAbsorbPct: econTierAbsorbPct ?? 30,
    }),
  // 2026-07-21 §13 道具系统 — 累加事件;按 at+from_seat+prop_key 去重,截断 50 条。
  // 2026-07-23 §道具特效:每次语义新事件到来时,同步改写 lastPropEvent 并 +1
  // lastPropSeq,驱动 PropUseOverlay 触发动画(seq 保证 React 可检测变化)。
  appendPropEvent: (ev) =>
    set((s) => {
      const last = s.propRecentEvents;
      // 去重:同一时刻+使用者+道具视为同一条事件(防止 game.state + game.werewolf_prop_used 双源重复)。
      const dup = last.find(
        (x) => x.at === ev.at && x.from_seat === ev.from_seat && x.prop_key === ev.prop_key,
      );
      if (dup) return s;
      const next = [...last, ev];
      // 仅保留最近 50 条
      if (next.length > 50) next.splice(0, next.length - 50);
      return { propRecentEvents: next, lastPropEvent: ev, lastPropSeq: s.lastPropSeq + 1 };
    }),
  // 2026-07-23 §道具特效:PropUseOverlay 写入/复位目标座位高亮。
  setPropTargetSeat: (seat) => set({ propTargetSeat: seat }),
  // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 记录 server deadline (Unix 毫秒)。
  setPreWaitDeadlineAt: (deadlineMs) => set({ preWaitDeadlineAt: deadlineMs }),
  reset: () =>
    set({
      currentRoom: null,
      gameState: null,
      mySeat: -1,
      gameOver: null,
      settlement: null,
      busy: false,
      // 道具状态在 reset 时一并清空,避免上一局残留。
      props: [],
      propMyBalance: null,
      propMyRemaining: 0,
      propCooldownRemainingSec: 0,
      propRecentEvents: [],
      lastPropEvent: null,
      lastPropSeq: 0,
      propTargetSeat: -1,
      // 2026-07-30 BUG-FIX: §130 人类等待窗口 — 离开对局时清零。
      preWaitDeadlineAt: null,
    }),
}));
