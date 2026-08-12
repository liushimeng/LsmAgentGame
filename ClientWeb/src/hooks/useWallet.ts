import { useEffect, useCallback, useRef } from 'react';
import { wsClient } from '@/services/ws';
import { walletService } from '@/services/auth.service';
import { useUserStore } from '@/store/user.store';
import { useAuth } from '@/hooks/useAuth';
import type { WalletTransaction, WalletBalancePush } from '@/types/api';
import { formatBalance } from '@/shared/utils/balance';
import { useI18nStore } from '@/store/i18n.store';
import { reportGlobalError, errorMessage } from '@/services/globalError';
import { isSessionExpiredError } from '@/services/http';

/**
 * useWallet —— 登录态下的钱包 Hook。
 * 1) 首次调用 /api/wallet/balance 拉取初始余额
 * 2) 订阅 WS wallet.balance 推送 —— 收到即更新 store
 * 3) 暴露流水拉取函数与格式化余额
 *
 * 设计注意:仅在已登录时启用;余额以服务端为准,本地不主动修改。
 *
 * §7.1 合规(2026-08-02 BUG-R233-P2-02 修复):`refresh()` / `listTransactions()`
 * 的 catch 块原本仅 console.warn——token 过期或网络 5xx 时用户看不到任何反馈,
 * 点击道具「没反应」的体感把责任推回给前端。修复后:
 *   - `isSessionExpiredError(e)` 短路:由 `http.ts::handleSessionError` 已经
 *     触发 AuthModal 重登,这里 **不**再 reportGlobalError,避免「登录弹层
 *     + 全局 toast」双声;
 *   - 其它错误:`reportGlobalError({ message: errorMessage(e, '钱包刷新失败'),
 *     severity: 'error' })` 上报到 GlobalToast,让用户看到「刷新余额失败,请重试」。
 */
export function useWallet() {
  const isAuth = useAuth((s) => s.isAuthenticated);
  const lang = useI18nStore((s) => s.lang);
  const {
    balance,
    lastDelta,
    lastReason,
    setBalance,
    setBalanceFromPush,
  } = useUserStore();

  // 防重复标记:同一次调用只拉一次初始余额
  const didInit = useRef(false);

  // 拉取初始余额(登录后首次进入任一使用页面时调用)。
  const refresh = useCallback(async () => {
    try {
      const data = await walletService.balance();
      setBalance(data.balance, null, null);
      useUserStore.getState().setWalletStats(data.total_earned ?? null, data.total_spent ?? null);
      didInit.current = true;
      return data;
    } catch (e) {
      // §7.1 BUG-R233-P2-02:不再静默吞错;session 过期例外交由全局 AuthModal,
      // 其余错误上报到 GlobalToast。
      if (!isSessionExpiredError(e)) {
        const msg = errorMessage(e, '钱包余额刷新失败,请稍后重试');
        reportGlobalError({ message: msg, severity: 'error' });
      }
      return null;
    }
  }, [setBalance]);

  // WS 订阅:只在登录态挂载时绑定一次。
  useEffect(() => {
    if (!isAuth) return;
    const unsub = wsClient.on((env) => {
      if (env.type !== 'wallet.balance') return;
      const push = env.payload as WalletBalancePush;
      setBalanceFromPush(push);
    });
    return unsub;
  }, [isAuth, setBalanceFromPush]);

  // 登录后首次自动刷新余额(避免 WS 推送间隙看到空值)。
  useEffect(() => {
    if (isAuth && !didInit.current) {
      void refresh();
    }
    if (!isAuth) {
      didInit.current = false;
    }
  }, [isAuth, refresh]);

  // 流水拉取 —— 按需调用,不缓存(Profile 页每次进入重新拉)。
  // 防御性兼容:后端可能返回 {entries} 或历史字段 {transactions},两者都兜底。
  const listTransactions = useCallback(
    async (limit = 50, skip = 0): Promise<WalletTransaction[]> => {
      try {
        const data = (await walletService.transactions(limit, skip)) as unknown as
          | { entries?: WalletTransaction[]; transactions?: WalletTransaction[] }
          | WalletTransaction[];
        if (Array.isArray(data)) return data;
        if (!data) return [];
        return (
          data.entries ??
          (data as { transactions?: WalletTransaction[] }).transactions ??
          []
        );
      } catch (e) {
        // §7.1 BUG-R233-P2-02:同上,session 过期除外,其余错误上报全局 toast。
        if (!isSessionExpiredError(e)) {
          const msg = errorMessage(e, '钱包流水加载失败');
          reportGlobalError({ message: msg, severity: 'error' });
        }
        return [];
      }
    },
    [],
  );

  // 格式化余额显示
  const formattedBalance = formatBalance(balance, lang);

  return {
    balance,
    formattedBalance,
    lastDelta,
    lastReason,
    refresh,
    listTransactions,
  };
}
