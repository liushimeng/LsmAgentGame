/**
 * 财商流游戏 zustand store（仿 doudizhu.store / texasholdem.store）。
 *
 * 单向数据流：useWealth hook 收 WS 帧 → 写 store → 页面/面板订阅渲染。
 * UI 态（panelTab / selectedDistrict）与对局态同库但分命名空间。
 */

import { create } from 'zustand';
import type { RoomInfo } from '@/types/api';
import type {
  WealthDistrictId,
  WealthEventFrame,
  WealthGameState,
  WealthMonthFrame,
  WealthOverFrame,
  WealthStartedFrame,
  WealthSurvey,
} from '@/types/wealth';
import { WEALTH_MIN_SEATS, wealthOccupiedSeats, wealthSeatCapacity } from '@/types/wealth';

/**
 * 右侧面板 Tab（聊天 / Agent 思维独立于 Tab 栈之外）。
 * P1 第二期扩展：economy（经济循环引擎）/ survey（社会调研系统）。
 */
export type WealthPanelTab = 'finance' | 'market' | 'ledger' | 'economy' | 'survey';

/** 市场快照（走势迷你图 + 相对上月箭头用）。 */
export interface WealthMarketPoint {
  month: number;
  stock: number;
  gold: number;
  bond: number;
}

const MAX_EVENT_FEED = 100;
const MAX_MONTH_FRAMES = 420;
const MAX_MARKET_HISTORY = 60;

interface WealthStore {
  // ── 大厅 ──
  rooms: RoomInfo[];
  /** 大厅页临时错误条（§7.1 页面级展示）。 */
  lobbyError: string | null;

  // ── 对局 ──
  gameState: WealthGameState | null;
  mySeat: number;
  startedInfo: WealthStartedFrame | null;
  /** game.event 增量事件流（截断 100）。 */
  eventFeed: WealthEventFrame[];
  /** game.month 月度汇总帧（截断 420；GameOverModal 净资产曲线数据源）。 */
  monthFrames: WealthMonthFrame[];
  /** 每月一条市场快照（cap 60；走势迷你图）。 */
  marketHistory: WealthMarketPoint[];
  gameOver: WealthOverFrame | null;
  /** 最近一次 game.error（页面顶部 banner 就地显示 + 全局 toast 双通道）。 */
  lastError: { code: number; message: string } | null;
  /**
   * 社会调研列表（P1 社会调研系统）。来源三路合一：
   * SurveyPanel 挂载时 HTTP GET 全量 setSurveys / game.state 快照种子 /
   * game.survey_result 增量 mergeSurvey（按 id 去重覆盖）。
   */
  surveys: WealthSurvey[];

  // ── UI ──
  panelTab: WealthPanelTab;
  selectedDistrict: WealthDistrictId | null;

  // setters
  setRooms: (rooms: RoomInfo[]) => void;
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  removeRoom: (roomId: string) => void;
  setLobbyError: (msg: string | null) => void;
  setGameState: (state: WealthGameState | null) => void;
  setMySeat: (seat: number) => void;
  setStartedInfo: (info: WealthStartedFrame | null) => void;
  pushEvent: (ev: WealthEventFrame) => void;
  applyMonthFrame: (frame: WealthMonthFrame) => void;
  setGameOver: (over: WealthOverFrame | null) => void;
  setLastError: (err: { code: number; message: string } | null) => void;
  /** 整表替换（HTTP GET / game.state 快照种子）。 */
  setSurveys: (surveys: WealthSurvey[]) => void;
  /** 单条按 id 去重覆盖（game.survey_result 帧；新增置顶）。 */
  mergeSurvey: (survey: WealthSurvey) => void;
  setPanelTab: (tab: WealthPanelTab) => void;
  setSelectedDistrict: (id: WealthDistrictId | null) => void;
  reset: () => void;
}

export const useWealthStore = create<WealthStore>((set) => ({
  rooms: [],
  lobbyError: null,
  gameState: null,
  mySeat: -1,
  startedInfo: null,
  eventFeed: [],
  monthFrames: [],
  marketHistory: [],
  gameOver: null,
  lastError: null,
  surveys: [],
  panelTab: 'finance',
  selectedDistrict: null,

  setRooms: (rooms) => set({ rooms }),
  patchRoom: (room) =>
    set((s) => ({
      rooms: s.rooms.map((r) =>
        r.id === room.id ? ({ ...r, ...room } as typeof r) : r,
      ),
    })),
  removeRoom: (roomId) =>
    set((s) => ({ rooms: s.rooms.filter((r) => r.id !== roomId) })),
  setLobbyError: (msg) => set({ lobbyError: msg }),

  setGameState: (state) =>
    set((s) => {
      // 每月首份快照落一条市场点（走势 + 箭头），cap 60。
      let marketHistory = s.marketHistory;
      if (state) {
        const last = marketHistory[marketHistory.length - 1];
        if (!last || last.month !== state.month) {
          marketHistory = [
            ...marketHistory,
            {
              month: state.month,
              stock: state.market.stock_index,
              gold: state.market.gold_price,
              bond: state.market.bond_yield,
            },
          ].slice(-MAX_MARKET_HISTORY);
        }
      }
      return { gameState: state, marketHistory };
    }),

  setMySeat: (seat) => set({ mySeat: seat }),
  setStartedInfo: (info) => set({ startedInfo: info }),

  pushEvent: (ev) =>
    set((s) => ({ eventFeed: [...s.eventFeed, ev].slice(-MAX_EVENT_FEED) })),

  applyMonthFrame: (frame) =>
    set((s) => ({
      monthFrames: [...s.monthFrames, frame].slice(-MAX_MONTH_FRAMES),
    })),

  setGameOver: (over) => set({ gameOver: over }),
  setLastError: (err) => set({ lastError: err }),

  setSurveys: (surveys) => set({ surveys }),

  mergeSurvey: (survey) =>
    set((s) => {
      const exists = s.surveys.some((x) => x.id === survey.id);
      return {
        surveys: exists
          ? s.surveys.map((x) => (x.id === survey.id ? survey : x))
          : [survey, ...s.surveys],
      };
    }),

  setPanelTab: (tab) => set({ panelTab: tab }),
  setSelectedDistrict: (id) => set({ selectedDistrict: id }),

  reset: () =>
    set({
      gameState: null,
      mySeat: -1,
      startedInfo: null,
      eventFeed: [],
      monthFrames: [],
      marketHistory: [],
      gameOver: null,
      lastError: null,
      surveys: [],
      panelTab: 'finance',
      selectedDistrict: null,
    }),
}));

/** 本月已用动作次数（近似值）：game.event 中本人 action/move 回执计数。
 *  服务端权威校验（35006），此处仅用于按钮禁用态 UX。 */
export function selectActionsUsedThisMonth(
  s: Pick<WealthStore, 'eventFeed' | 'gameState' | 'mySeat'>,
): number {
  const month = s.gameState?.month;
  if (month === undefined) return 0;
  return s.eventFeed.filter(
    (e) =>
      e.month === month &&
      e.seat === s.mySeat &&
      (e.type === 'action' || e.type === 'move'),
  ).length;
}

/** 本月动作预算（P0 = 3，与引擎 BotMaxActionsPerMonth 同值）。 */
export const WEALTH_ACTION_BUDGET = 3;

// ── 座位占用选择器（2026-09-16 §财商流10–12座位改造）─────────────────────
//
// 房间容量 8 → 12、最少开局座位 3 → 10（后端 wealth.MaxSeats / MinSeats）。
// 三者都返回原语（number / boolean），可安全用于 useWealthStore(selector) —— 返回
// 对象字面量的选择器会让 zustand 每次快照都判定「变了」从而死循环重渲染。

/** 已占座人数（players[] 恒为 max_seat 长度，空座位是占位对象，不能取 length）。 */
export const selectSeatedCount = (s: Pick<WealthStore, 'gameState'>): number =>
  wealthOccupiedSeats(s.gameState?.players);

/** 房间容量：服务端权威 game.state.max_seat 优先，未到达时回落 WEALTH_MAX_SEATS(12)。 */
export const selectSeatCapacity = (s: Pick<WealthStore, 'gameState'>): number =>
  wealthSeatCapacity(s.gameState);

/** 是否已达到开局最少座位（不小于 WEALTH_MIN_SEATS）。 */
export const selectSeatsReady = (s: Pick<WealthStore, 'gameState'>): boolean =>
  selectSeatedCount(s) >= WEALTH_MIN_SEATS;
