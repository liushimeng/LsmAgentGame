import { create } from 'zustand';
import type { RoomInfo, RoomDetail } from '@/types/api';
import type {
  JunqiGameState,
  JunqiMove,
  JunqiPieceColor,
  JunqiPosition,
} from '@/types/junqi';
import type { StyleKey } from '@/assets/images/junqi';

export type BoardStyle = StyleKey;

/** 房间类型：练习房（无注） or 带注房。 */
export type RoomMode = 'practice' | 'ante';
/** 军棋模式：暗棋 or 明棋。 */
export type JunqiMode = 'hidden' | 'open';

interface JunqiStore {
  // Room list
  rooms: RoomInfo[];
  currentRoom: RoomDetail | null;

  // Game state
  gameState: JunqiGameState | null;
  myColor: JunqiPieceColor | null;

  // UI state
  selectedPos: JunqiPosition | null;
  lastMove: JunqiMove | null;
  style: BoardStyle;
  gameOver: { winner: string; reason: string } | null;
  // Pending layout submission (not yet sent to server)
  pendingLayout: Record<string, JunqiPieceColor> | null;

  // Ante（金币房间）状态 —— 军棋特规：balance >= ante × 5
  ante: number;
  roomMode: RoomMode;
  junqiMode: JunqiMode;
  /** 结算明细。 */
  settlement: {
    ante: number;
    netGain: number;
    platformFee?: number;
    finalBalance?: number;
    result: 'win' | 'lose' | 'draw';
  } | null;
  showSettlement: boolean;

  // Actions
  setRooms: (rooms: RoomInfo[]) => void;
  /** Patch a single room in place (called from useLobbyLiveUpdate on `room.state`). */
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  /** Remove a room from the local cache (status === 'removed'). */
  removeRoom: (roomId: string) => void;
  setCurrentRoom: (room: RoomDetail | null) => void;
  setGameState: (state: JunqiGameState | null) => void;
  setMyColor: (color: JunqiPieceColor | null) => void;
  selectPos: (pos: JunqiPosition | null) => void;
  setLastMove: (move: JunqiMove | null) => void;
  setStyle: (style: StyleKey) => void;
  setGameOver: (over: { winner: string; reason: string } | null) => void;
  setAnte: (ante: number) => void;
  setRoomMode: (mode: RoomMode) => void;
  setJunqiMode: (mode: JunqiMode) => void;
  setSettlement: (s: JunqiStore['settlement']) => void;
  dismissSettlement: () => void;
  reset: () => void;
}

export const useJunqiStore = create<JunqiStore>((set) => ({
  rooms: [],
  currentRoom: null,
  gameState: null,
  myColor: null,
  selectedPos: null,
  lastMove: null,
  style: 'naruto',
  gameOver: null,
  pendingLayout: null,
  ante: 0,
  roomMode: 'practice',
  junqiMode: 'hidden',
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
  setJunqiMode: (mode) => set({ junqiMode: mode }),
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
      pendingLayout: null,
      ante: 0,
      roomMode: 'practice',
      settlement: null,
      showSettlement: false,
    }),
}));