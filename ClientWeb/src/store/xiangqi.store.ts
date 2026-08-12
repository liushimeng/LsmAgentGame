import { create } from 'zustand';
import type { RoomInfo, RoomDetail, XiangqiGameState, XiangqiMove } from '@/types/api';

export type BoardStyle = 'warring' | 'robot';

/** 房间类型：练习房（无注） or 带注房。 */
export type RoomMode = 'practice' | 'ante';

interface XiagqiStore {
  // Room list
  rooms: RoomInfo[];
  currentRoom: RoomDetail | null;

  // Game state
  gameState: XiangqiGameState | null;
  myColor: 'red' | 'black' | null;

  // UI state
  selectedPos: { x: number; y: number } | null;
  lastMove: XiangqiMove | null;
  style: BoardStyle;
  gameOver: { winner: string; reason: string } | null;

  // Ante（金币房间）状态
  /** 本局底注：0 = 练习房。 */
  ante: number;
  /** 房间模式。 */
  roomMode: RoomMode;
  /** 结算明细（game.over 触发后由 useXiangqi 写入）。 */
  settlement: {
    ante: number;
    netGain: number;
    streakBonus?: number;
    finalBalance?: number;
    result: 'win' | 'lose' | 'draw';
  } | null;
  /** 结算弹层是否展示。 */
  showSettlement: boolean;

  // Actions
  setRooms: (rooms: RoomInfo[]) => void;
  /** Patch a single room in place (called from useLobbyLiveUpdate on `room.state`). */
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  /** Remove a room from the local cache (status === 'removed'). */
  removeRoom: (roomId: string) => void;
  setCurrentRoom: (room: RoomDetail | null) => void;
  setGameState: (state: XiangqiGameState | null) => void;
  setMyColor: (color: 'red' | 'black' | null) => void;
  selectPos: (pos: { x: number; y: number } | null) => void;
  setLastMove: (move: XiangqiMove | null) => void;
  setStyle: (style: BoardStyle) => void;
  setGameOver: (over: { winner: string; reason: string } | null) => void;
  /** 设置创建房间时的底注（仅影响 POST body，不直接创建）。 */
  setAnte: (ante: number) => void;
  setRoomMode: (mode: RoomMode) => void;
  /** 本局结算结果写入。 */
  setSettlement: (s: XiagqiStore['settlement']) => void;
  /** 关闭结算弹层（同时清空 settlement，为下一局做准备）。 */
  dismissSettlement: () => void;
  reset: () => void;
}

export const useXiangqiStore = create<XiagqiStore>((set) => ({
  rooms: [],
  currentRoom: null,
  gameState: null,
  myColor: null,
  selectedPos: null,
  lastMove: null,
  style: 'warring',
  gameOver: null,
  ante: 0,
  roomMode: 'practice',
  settlement: null,
  showSettlement: false,

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
  setMyColor: (color) => set({ myColor: color }),
  selectPos: (pos) => set({ selectedPos: pos }),
  setLastMove: (move) => set({ lastMove: move }),
  setStyle: (style) => set({ style }),
  setGameOver: (over) => set({ gameOver: over }),
  setAnte: (ante) => set({ ante }),
  setRoomMode: (mode) => set({ roomMode: mode }),
  setSettlement: (s) => set({ settlement: s, showSettlement: true }),
  dismissSettlement: () => set({ settlement: null, showSettlement: false }),
  reset: () =>
    set({
      currentRoom: null,
      gameState: null,
      myColor: null,
      selectedPos: null,
      lastMove: null,
      gameOver: null,
      ante: 0,
      roomMode: 'practice',
      settlement: null,
      showSettlement: false,
    }),
}));
