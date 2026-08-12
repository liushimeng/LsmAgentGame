import { create } from 'zustand';
import type { GameInfo } from '@/types/api';

export interface GameState {
  games: GameInfo[];
  setGames: (g: GameInfo[]) => void;
}

export const useGameStore = create<GameState>((set) => ({
  games: [],
  setGames: (games) => set({ games }),
}));
