// ─── Doudizhu (斗地主) types ───
//
// Mirrors the server-side game/doudizhu package.

/** 点数：3..10, 11=J, 12=Q, 13=K, 14=A, 15=2, 16=小王, 17=大王 */
export type DoudizhuRank = number; // 3..17

/** 花色：1=♠, 2=♥, 3=♣, 4=♦, 0=无花色（王） */
export type DoudizhuSuit = number;

export interface DoudizhuCard {
  rank: DoudizhuRank;
  suit: DoudizhuSuit;
}

export type DoudizhuPhase = 'bidding' | 'playing' | 'over';
export type DoudizhuStatus = 'playing' | 'landlord_win' | 'farmer_win';

export interface DoudizhuLastPlay {
  seat: number;
  cards: DoudizhuCard[];
}

/** 后端 game.state 帧的完整 payload（按座位过滤后）。 */
export interface DoudizhuGameState {
  room_id: string;
  game_kind?: string;
  seats: string[];       // 3 个座位的 userID
  my_seat: number;       // 接收方座位
  ready: boolean;
  phase: DoudizhuPhase;
  turn: number;          // 当前行动座位
  status: DoudizhuStatus;
  my_hand: DoudizhuCard[];
  hand_counts: number[]; // 三家剩余张数
  first_bidder: number;
  bids: number[];        // 3 个座位叫分（-1 未叫）
  current_bid: number;
  landlord_seat: number; // -1 未定
  bottom: DoudizhuCard[];
  last_play: DoudizhuLastPlay | null;
  multiplier: number;
  bomb_count: number;
  winner: string;        // 'landlord' | 'farmer' | ''
  score: number;
}

// ── 点数/花色常量，与后端 cards.go 对齐 ──

export const RANK_3 = 3;
export const RANK_2 = 15;
export const RANK_SMALL = 16;
export const RANK_BIG = 17;

export const SUIT_SPADE = 1;
export const SUIT_HEART = 2;
export const SUIT_CLUB = 3;
export const SUIT_DIAMOND = 4;
export const SUIT_NONE = 0;

/** 点数显示标签。 */
export const RANK_LABELS: Record<number, string> = {
  3: '3', 4: '4', 5: '5', 6: '6', 7: '7', 8: '8', 9: '9', 10: '10',
  11: 'J', 12: 'Q', 13: 'K', 14: 'A', 15: '2',
  16: '小', 17: '大',
};

/** 花色显示符号。 */
export const SUIT_LABELS: Record<number, string> = {
  0: '🃏', 1: '♠', 2: '♥', 3: '♣', 4: '♦',
};

/** 花色是否为红色（红桃、方块、大王）。 */
export function isRedSuit(rank: number, suit: number): boolean {
  if (rank === RANK_BIG) return true;
  if (rank === RANK_SMALL) return false;
  return suit === SUIT_HEART || suit === SUIT_DIAMOND;
}
