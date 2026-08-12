import { create } from 'zustand';
import type {
  RoomInfo,
  RoomDetail,
  ChessGameState,
  ChessMove,
} from '@/types/api';

export type ChessBoardStyle = 'european' | 'cyberpunk';

interface ChessStore {
  // Room list
  rooms: RoomInfo[];
  currentRoom: RoomDetail | null;

  // Game state
  gameState: ChessGameState | null;
  myColor: 'white' | 'black' | null;

  // UI state
  selectedPos: { x: number; y: number } | null;
  legalTargets: { x: number; y: number }[];
  lastMove: ChessMove | null;
  style: ChessBoardStyle;
  gameOver: { winner: string; reason: string } | null;

  // Promotion dialog: when non-null, the user must choose a promotion piece
  promotionPending: { from: { x: number; y: number }; to: { x: number; y: number } } | null;

  // Actions
  setRooms: (rooms: RoomInfo[]) => void;
  /** Patch a single room in place (called from useLobbyLiveUpdate on `room.state`). */
  patchRoom: (room: Partial<RoomInfo> & { id: string }) => void;
  /** Remove a room from the local cache (status === 'removed'). */
  removeRoom: (roomId: string) => void;
  setCurrentRoom: (room: RoomDetail | null) => void;
  setGameState: (state: ChessGameState | null) => void;
  setMyColor: (color: 'white' | 'black' | null) => void;
  selectPos: (pos: { x: number; y: number } | null) => void;
  setLegalTargets: (targets: { x: number; y: number }[]) => void;
  setLastMove: (move: ChessMove | null) => void;
  setStyle: (style: ChessBoardStyle) => void;
  setGameOver: (over: { winner: string; reason: string } | null) => void;
  setPromotionPending: (p: { from: { x: number; y: number }; to: { x: number; y: number } } | null) => void;
  reset: () => void;
}

export const useChessStore = create<ChessStore>((set) => ({
  rooms: [],
  currentRoom: null,
  gameState: null,
  myColor: null,
  selectedPos: null,
  legalTargets: [],
  lastMove: null,
  style: 'european',
  gameOver: null,
  promotionPending: null,

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
  setLegalTargets: (targets) => set({ legalTargets: targets }),
  setLastMove: (move) => set({ lastMove: move }),
  setStyle: (style) => set({ style }),
  setGameOver: (over) => set({ gameOver: over }),
  setPromotionPending: (p) => set({ promotionPending: p }),
  reset: () =>
    set({
      currentRoom: null,
      gameState: null,
      myColor: null,
      selectedPos: null,
      legalTargets: [],
      lastMove: null,
      gameOver: null,
      promotionPending: null,
    }),
}));
