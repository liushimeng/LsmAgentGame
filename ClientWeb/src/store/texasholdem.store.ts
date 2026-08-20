import { create } from 'zustand';
import type { RoomInfo, RoomDetail } from '@/types/api';
import type { TexasHoldemGameState } from '@/types/texasholdem';
import type { StyleKey } from '@/assets/images/texasholdem';

export type TexasStyle = StyleKey;

interface TexasHoldemStore {
  rooms: RoomInfo[];
  currentRoom: RoomDetail | null;
  gameState: TexasHoldemGameState | null;
  mySeat: number;
  raiseAmount: number;
  style: TexasStyle;
  gameOver: { winners: number[]; reason: string } | null;
  /**
   * §20260819-02 P0-2 — WS game.error 帧(罕见)由 useTexasHoldem 写入。
   * TexasHoldemGamePage 注入 effect 把 lastError 同步到页内 banner,既保留本地
   * 上下文(§7.1「在当前页面以最高层级显示」),又由 hook 内的
   * reportGlobalError 走全局兜底(后者已就位,本字段仅供页面精确反馈)。
   * null = 无错误。
   */
  lastError: { code: number; message: string } | null;

  setRooms: (rooms: RoomInfo[]) => void;
  /** Patch a single room in place (called from useLobbyLiveUpdate on `room.state`). */
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  /** Remove a room from the local cache (status === 'removed'). */
  removeRoom: (roomId: string) => void;
  setCurrentRoom: (room: RoomDetail | null) => void;
  setGameState: (state: TexasHoldemGameState | null) => void;
  setMySeat: (seat: number) => void;
  setRaiseAmount: (amount: number) => void;
  setStyle: (style: TexasStyle) => void;
  setGameOver: (over: { winners: number[]; reason: string } | null) => void;
  setLastError: (err: { code: number; message: string } | null) => void;
  reset: () => void;
}

export const useTexasHoldemStore = create<TexasHoldemStore>((set) => ({
  rooms: [],
  currentRoom: null,
  gameState: null,
  mySeat: 0,
  raiseAmount: 0,
  style: 'western_cowboy',
  gameOver: null,
  lastError: null,

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
  setMySeat: (seat) => set({ mySeat: seat }),
  setRaiseAmount: (amount) => set({ raiseAmount: amount }),
  setStyle: (style) => set({ style }),
  setGameOver: (over) => set({ gameOver: over }),
  setLastError: (err) => set({ lastError: err }),
  reset: () =>
    set({
      currentRoom: null,
      gameState: null,
      mySeat: 0,
      raiseAmount: 0,
      gameOver: null,
      lastError: null,
    }),
}));
