import { create } from 'zustand';
import type { RoomInfo, RoomDetail } from '@/types/api';
import type { DoudizhuGameState } from '@/types/doudizhu';
import type { StyleKey } from '@/assets/images/doudizhu';

export type DoudizhuStyle = StyleKey;

interface DoudizhuStore {
  // 房间
  rooms: RoomInfo[];
  currentRoom: RoomDetail | null;

  // 对局
  gameState: DoudizhuGameState | null;
  mySeat: number;
  selectedCards: Set<number>; // 已选手牌的索引集合
  style: DoudizhuStyle;
  gameOver: { winner: string; reason: string } | null;

  /** 本局底注（lobby 创建时传入，进入游戏页后顶部显示）。 */
  anteHint: number | null;
  /** 最近一次倍数变动（用于飘字动画）；由 useDoudizhu hook 写入。 */
  lastMultiplierBump: { delta: number; ts: number } | null;

  // setters
  setRooms: (rooms: RoomInfo[]) => void;
  /** Patch a single room in place (called from useLobbyLiveUpdate on `room.state`). */
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  /** Remove a room from the local cache (status === 'removed'). */
  removeRoom: (roomId: string) => void;
  setCurrentRoom: (room: RoomDetail | null) => void;
  setGameState: (state: DoudizhuGameState | null) => void;
  setMySeat: (seat: number) => void;
  toggleCard: (index: number) => void;
  clearSelected: () => void;
  setStyle: (style: DoudizhuStyle) => void;
  setGameOver: (over: { winner: string; reason: string } | null) => void;
  setAnteHint: (ante: number | null) => void;
  setLastMultiplierBump: (bump: { delta: number; ts: number } | null) => void;
  reset: () => void;
}

export const useDoudizhuStore = create<DoudizhuStore>((set, get) => ({
  rooms: [],
  currentRoom: null,
  gameState: null,
  mySeat: 0,
  selectedCards: new Set<number>(),
  style: 'traditional_landlord',
  gameOver: null,
  anteHint: null,
  lastMultiplierBump: null,

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
  toggleCard: (index) => {
    const current = new Set(get().selectedCards);
    if (current.has(index)) {
      current.delete(index);
    } else {
      current.add(index);
    }
    set({ selectedCards: current });
  },
  clearSelected: () => set({ selectedCards: new Set() }),
  setStyle: (style) => set({ style }),
  setGameOver: (over) => set({ gameOver: over }),
  setAnteHint: (ante) => set({ anteHint: ante }),
  setLastMultiplierBump: (bump) => set({ lastMultiplierBump: bump }),
  reset: () =>
    set({
      currentRoom: null,
      gameState: null,
      mySeat: 0,
      selectedCards: new Set(),
      gameOver: null,
      anteHint: null,
      lastMultiplierBump: null,
    }),
}));
