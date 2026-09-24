/**
 * 虚拟城市 zustand store（仿 doudizhu.store / texasholdem.store）。
 *
 * 单向数据流：useVirtualCity hook 收 WS 帧 → 写 store → 页面/面板订阅渲染。
 * UI 态（panelTab / selectedDistrict）与对局态同库但分命名空间。
 */

import { create } from 'zustand';
import type { RoomInfo } from '@/types/api';
import type {
  VirtualCityDistrictId,
  VirtualCityEventFrame,
  VirtualCityGameState,
  VirtualCityListingBook,
  VirtualCityMonthFrame,
  VirtualCityOverFrame,
  VirtualCityStartedFrame,
  VirtualCitySurvey,
} from '@/types/virtualCity';
import { VIRTUAL_CITY_MIN_SEATS, virtualCityOccupiedSeats, virtualCitySeatCapacity } from '@/types/virtualCity';

/**
 * 右侧面板 Tab（聊天 / Agent 思维独立于 Tab 栈之外）。
 * P1 第二期扩展：economy（经济循环引擎）/ survey（社会调研系统）。
 * P2 第三期扩展：listing（挂单簿）/ loan（借贷）/ infomarket（信息市场）。
 * P1-4 扩展：insurance（商业保险，§财商流P1-4 §10.1）。
 */
/**
 * 15-3D城市全面真实感深化 · 阶段 Q：合并 Tab 9 → 6
 *  - survey → economy（含调研模态入口，见 VirtualCityGamePage::EconomyPanelWithSurvey）
 *  - listing / loan / infomarket → market-trade（子导航聚合，见 MarketTradePanel）
 */
export type VirtualCityPanelTab =
  | 'finance' | 'market' | 'ledger' | 'economy'
  | 'market-trade' | 'insurance';

/** 市场快照（走势迷你图 + 相对上月箭头用）。 */
export interface VirtualCityMarketPoint {
  month: number;
  stock: number;
  gold: number;
  bond: number;
}

const MAX_EVENT_FEED = 100;
const MAX_MONTH_FRAMES = 420;
const MAX_MARKET_HISTORY = 60;

interface VirtualCityStore {
  // ── 大厅 ──
  rooms: RoomInfo[];
  /** 大厅页临时错误条（§7.1 页面级展示）。 */
  lobbyError: string | null;

  // ── 对局 ──
  gameState: VirtualCityGameState | null;
  mySeat: number;
  startedInfo: VirtualCityStartedFrame | null;
  /** game.event 增量事件流（截断 100）。 */
  eventFeed: VirtualCityEventFrame[];
  /** game.month 月度汇总帧（截断 420；GameOverModal 净资产曲线数据源）。 */
  monthFrames: VirtualCityMonthFrame[];
  /** 每月一条市场快照（cap 60；走势迷你图）。 */
  marketHistory: VirtualCityMarketPoint[];
  gameOver: VirtualCityOverFrame | null;
  /** 最近一次 game.error（页面顶部 banner 就地显示 + 全局 toast 双通道）。 */
  lastError: { code: number; message: string } | null;
  /**
   * 社会调研列表（P1 社会调研系统）。来源三路合一：
   * SurveyPanel 挂载时 HTTP GET 全量 setSurveys / game.state 快照种子 /
   * game.survey_result 增量 mergeSurvey（按 id 去重覆盖）。
   */
  surveys: VirtualCitySurvey[];

  // ── P2 交易（挂单簿 / 议价 / 借贷 / 拍卖；game.state.listing_book 快照驱动）──
  listingBook: VirtualCityListingBook | null;

  // ── UI ──
  panelTab: VirtualCityPanelTab;
  selectedDistrict: VirtualCityDistrictId | null;

  // setters
  setRooms: (rooms: RoomInfo[]) => void;
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  removeRoom: (roomId: string) => void;
  setLobbyError: (msg: string | null) => void;
  setGameState: (state: VirtualCityGameState | null) => void;
  setMySeat: (seat: number) => void;
  setStartedInfo: (info: VirtualCityStartedFrame | null) => void;
  pushEvent: (ev: VirtualCityEventFrame) => void;
  applyMonthFrame: (frame: VirtualCityMonthFrame) => void;
  setGameOver: (over: VirtualCityOverFrame | null) => void;
  setLastError: (err: { code: number; message: string } | null) => void;
  /** 整表替换（HTTP GET / game.state 快照种子）。 */
  setSurveys: (surveys: VirtualCitySurvey[]) => void;
  /** 单条按 id 去重覆盖（game.survey_result 帧；新增置顶）。 */
  mergeSurvey: (survey: VirtualCitySurvey) => void;
  setPanelTab: (tab: VirtualCityPanelTab) => void;
  setSelectedDistrict: (id: VirtualCityDistrictId | null) => void;
  setListingBook: (book: VirtualCityListingBook | null) => void;
  /** 单帧内局部挂单/议价/借贷更新（game.listing_created 等增量帧）。 */
  mergeListingBook: (patch: Partial<VirtualCityListingBook>) => void;
  reset: () => void;
}

export const useVirtualCityStore = create<VirtualCityStore>((set) => ({
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
  listingBook: null,
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
      // 挂单簿快照种子（后端 game.state.listing_book 全量下发；无则保留）。
      const listingBook = state?.listing_book ?? s.listingBook;
      return { gameState: state, marketHistory, listingBook };
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

  setListingBook: (book) => set({ listingBook: book }),

  mergeListingBook: (patch) =>
    set((s) => {
      const prev = s.listingBook ?? { listings: [], negotiates: [], p2p_loans: [], auctions: [] };
      const mergeById = <T extends { id: string }>(arr: T[] = [], incoming: T[] = []) => {
        const map = new Map(arr.map((x) => [x.id, x]));
        for (const x of incoming) map.set(x.id, x);
        return [...map.values()];
      };
      return {
        listingBook: {
          listings: mergeById(prev.listings, patch.listings),
          negotiates: mergeById(prev.negotiates, patch.negotiates),
          p2p_loans: mergeById(prev.p2p_loans, patch.p2p_loans),
          auctions: mergeById(prev.auctions, patch.auctions),
        },
      };
    }),

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
      listingBook: null,
      panelTab: 'finance',
      selectedDistrict: null,
    }),
}));

/** 本月已用动作次数（近似值）：game.event 中本人 action/move 回执计数。
 *  服务端权威校验（35006），此处仅用于按钮禁用态 UX。 */
export function selectActionsUsedThisMonth(
  s: Pick<VirtualCityStore, 'eventFeed' | 'gameState' | 'mySeat'>,
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
// 房间容量 8 → 12、最少开局座位 3 → 10（后端 virtualCity.MaxSeats / MinSeats）。
// 三者都返回原语（number / boolean），可安全用于 useVirtualCityStore(selector) —— 返回
// 对象字面量的选择器会让 zustand 每次快照都判定「变了」从而死循环重渲染。

/** 已占座人数（players[] 恒为 max_seat 长度，空座位是占位对象，不能取 length）。 */
export const selectSeatedCount = (s: Pick<VirtualCityStore, 'gameState'>): number =>
  virtualCityOccupiedSeats(s.gameState?.players);

/** 房间容量：服务端权威 game.state.max_seat 优先，未到达时回落 VIRTUAL_CITY_MAX_SEATS(12)。 */
export const selectSeatCapacity = (s: Pick<VirtualCityStore, 'gameState'>): number =>
  virtualCitySeatCapacity(s.gameState);

/** 是否已达到开局最少座位（不小于 VIRTUAL_CITY_MIN_SEATS）。 */
export const selectSeatsReady = (s: Pick<VirtualCityStore, 'gameState'>): boolean =>
  selectSeatedCount(s) >= VIRTUAL_CITY_MIN_SEATS;
