import { create } from 'zustand';
import type { WalletBalancePush } from '@/types/api';

// 用户钱包状态 —— 与 auth.store 拆分，因为余额是跨页面共享的纯展示态，
// 服务端是权威来源，本地仅在收到 WS 推送或主动拉取后缓存。
// 余额绝不本地修改，所有更新走 setBalance，由 WS wallet.balance 或
// /api/wallet/balance 返回触发。
export interface UserState {
  /** 当前余额（服务端权威）。null = 尚未拉取。 */
  balance: number | null;
  /** 最近一次变动金额（用于 Header 红点/动效）。 */
  lastDelta: number | null;
  /** 最近一次变动原因（显示用）。 */
  lastReason: string | null;
  /** 累计获得（Profile 页展示用）。 */
  totalEarned: number | null;
  /** 累计消耗（Profile 页展示用）。 */
  totalSpent: number | null;
  /** 每日奖励是否今日已领取。 */
  dailyClaimed: boolean;

  setBalance: (balance: number, delta?: number | null, reason?: string | null) => void;
  setBalanceFromPush: (push: WalletBalancePush) => void;
  setWalletStats: (totalEarned: number | null, totalSpent: number | null) => void;
  setDailyClaimed: (claimed: boolean) => void;
}

export const useUserStore = create<UserState>((set) => ({
  balance: null,
  lastDelta: null,
  lastReason: null,
  totalEarned: null,
  totalSpent: null,
  dailyClaimed: false,

  setBalance: (balance, delta = null, reason = null) =>
    set({ balance, lastDelta: delta, lastReason: reason }),

  setBalanceFromPush: (push: WalletBalancePush) =>
    set({
      balance: push.balance,
      lastDelta: push.delta,
      lastReason: push.reason,
    }),

  setWalletStats: (totalEarned, totalSpent) =>
    set({ totalEarned, totalSpent }),

  setDailyClaimed: (claimed) => set({ dailyClaimed: claimed }),
}));
