// ─── Texas Hold'em (德州扑克) types ───
//
// Mirrors the server-side game/texasholdem package.

export type TexasRank = number; // 2..14 (14=A)
export type TexasSuit = number; // 1..4

export interface TexasCard {
  rank: TexasRank;
  suit: TexasSuit;
}

export type TexasStreet = 'waiting' | 'preflop' | 'flop' | 'turn' | 'river' | 'showdown' | 'over';
export type TexasStatus = 'playing' | 'showdown' | 'over';
export type TexasActionType = 'fold' | 'check' | 'call' | 'bet' | 'raise' | 'allin';

export interface TexasPlayerInfo {
  user_id: string;
  hole: TexasCard[];
  hole_count: number;
  folded: boolean;
  all_in: boolean;
  stack: number;
  /** 本手总投入（跨街累积）—— 用于 UI 显示 "$已下注"。 */
  chips_committed: number;
  /** 本街累计下注（每轮 advanceToNextStreet 清零）—— 用于 canCheck 计算。
   *  对比 chips_committed：跨街累积的总额在 current_bet 被重置为 0 后永远无法
   *  与之相等，导致旧版 Bug #10 canCheck 永 false / "过牌"按钮误显示。
   *  后端 view.go 已暴露此字段（提交修复 §55）。 */
  round_committed: number;
  seat: number;
  has_player: boolean;
}

export interface TexasHoldemGameState {
  room_id: string;
  game_kind?: string;
  seats: string[];
  my_seat: number;
  ready: boolean;
  phase: TexasStreet;
  turn: number;
  pot: number;
  current_bet: number;
  big_blind: number;
  button: number;
  community: TexasCard[];
  community_count: number;
  players: TexasPlayerInfo[];
  my_hole: TexasCard[];
  hand_number: number;
  winners?: number[];
  showdown_hands?: TexasCard[][];
  status: TexasStatus;

  // 2026-08-19 §德州扑克Agent — Bot 状态透传(前端 PlayerSeat 渲染"🤖 AI"徽章 /
  // 思考中指示器 / 内心独白)。字段始终为长度 6 的数组,服务端初始化为零值以避免
  // JSON null.length 崩溃(BUG-TEXAS-HOLE-NULL 同源)。
  bot_seats?: [boolean, boolean, boolean, boolean, boolean, boolean];
  bot_models?: [string, string, string, string, string, string];
  bot_heart_thought?: [string, string, string, string, string, string];
  bot_thinking?: [boolean, boolean, boolean, boolean, boolean, boolean];
}

export const RANK_LABELS: Record<number, string> = {
  2:'2', 3:'3', 4:'4', 5:'5', 6:'6', 7:'7', 8:'8', 9:'9', 10:'10',
  11:'J', 12:'Q', 13:'K', 14:'A',
};

export const SUIT_LABELS: Record<number, string> = {
  1:'♠', 2:'♥', 3:'♣', 4:'♦',
};

export const SUIT_NAMES: Record<number, string> = {
  1:'spade', 2:'heart', 3:'club', 4:'diamond',
};

export function isRedSuit(suit: number): boolean {
  return suit === 2 || suit === 4;
}
