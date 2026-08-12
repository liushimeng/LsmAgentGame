import { useEffect, useState, useCallback } from 'react';
import { walletService } from '@/services/auth.service';
import { useT } from '@/hooks/useT';
import { useUserStore } from '@/store/user.store';
import { isSessionExpiredError } from '@/services/http';
import { formatBalance, signedAmount } from '@/shared/utils/balance';
import { useI18nStore } from '@/store/i18n.store';
import type { WalletTransaction } from '@/types/api';
import type { TKey } from '@/i18n';
import './WalletModal.css';

interface Props {
  open: boolean;
  onClose: () => void;
}

// WalletModal —— 工具栏 💰 按钮触发的「钱包管理」弹出式窗口。
//
// 起源：旧版是点击工具栏 💰 按钮后弹出 dropdown，会把整个工具栏布局
// 挤乱（wallet-dropdown 是 absolute 定位在按钮下方，宽度自适应内容，
// 但在窄屏/多按钮挤兑下会触发换行）。重构后改为标准 modal 居中弹出，
// 与 GitLogModal / ChatSettingsModal 风格一致。
//
// 移植内容（来自 ProfilePage 的「我的钱包」区块）：
//   1. 每日奖励领取（`/api/wallet/claim-daily`）
//   2. 当前余额 / 累计获得 / 累计消耗
//   3. 最近流水（`/api/wallet/transactions?limit=10`）
// 保留 dailyClaimed 状态：未领时显示领取按钮，已领时禁用为"今日已领取"。

// 后端 tx_type 字段已知值集合 —— 与 i18n key 命名一一对应。
const KNOWN_TX_TYPES = new Set([
  'register_bonus',
  'daily_login',
  'win_reward',
  'lose_deduct',
  'ante_buyin',
  'ante_refund',
  'task_reward',
  'referral_bonus',
  'admin_adjust',
  'game_win',
  'game_lose',
  'daily_bonus',
  'ante',
  'settle',
]);

function txReasonKey(rawType: string | undefined | null): TKey {
  const r = rawType ?? 'other';
  return (KNOWN_TX_TYPES.has(r)
    ? `wallet.tx.${r}`
    : 'wallet.tx.other') as TKey;
}

// 防御性解析 created_at —— 后端可能下发 ISO 字符串或 unix 秒数。
function parseTxTime(ts: string | number | undefined | null): Date | null {
  if (ts == null) return null;
  const d = typeof ts === 'number' ? new Date(ts * 1000) : new Date(ts);
  return Number.isNaN(d.getTime()) ? null : d;
}

// 防御性兼容：API 响应可能为 {entries:[]} 或历史字段 {transactions:[]}，
// 也可能直接是数组（早期版本）。统一抽出 entries。
function extractEntries(data: unknown): WalletTransaction[] {
  if (Array.isArray(data)) return data as WalletTransaction[];
  if (!data || typeof data !== 'object') return [];
  const obj = data as { entries?: WalletTransaction[]; transactions?: WalletTransaction[] };
  return obj.entries ?? obj.transactions ?? [];
}

export function WalletModal({ open, onClose }: Props) {
  const t = useT();
  const lang = useI18nStore((s) => s.lang);

  // ---- 用户态 ----
  const balance = useUserStore((s) => s.balance);
  const totalEarned = useUserStore((s) => s.totalEarned);
  const totalSpent = useUserStore((s) => s.totalSpent);
  const setBalance = useUserStore((s) => s.setBalance);
  const setWalletStats = useUserStore((s) => s.setWalletStats);
  const dailyClaimed = useUserStore((s) => s.dailyClaimed);
  const setDailyClaimed = useUserStore((s) => s.setDailyClaimed);

  // ---- 流水 / 加载 / 错误 ----
  const [tx, setTx] = useState<WalletTransaction[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // ---- 领取每日奖励 ----
  const [claimLoading, setClaimLoading] = useState(false);
  const [claimMsg, setClaimMsg] = useState<string>('');
  const [claimMsgKind, setClaimMsgKind] = useState<'success' | 'info' | 'error' | null>(null);

  // Esc 关闭弹窗
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  // 打开时重新拉取余额 + 流水 —— 与 ProfilePage 行为一致（每次进入拉新数据）
  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [bal, txList] = await Promise.all([
        walletService.balance(),
        walletService.transactions(20, 0),
      ]);
      setBalance(bal.balance, null, null);
      setWalletStats(bal.total_earned ?? null, bal.total_spent ?? null);
      setTx(extractEntries(txList));
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        setError(e?.message || 'load failed');
      }
    } finally {
      setLoading(false);
    }
  }, [setBalance, setWalletStats]);

  useEffect(() => {
    if (!open) return;
    void reload();
  }, [open, reload]);

  // 领取每日奖励 —— 与 ProfilePage handleClaim 同源实现
  const handleClaim = async () => {
    if (claimLoading || dailyClaimed) return;
    setClaimLoading(true);
    setClaimMsg('');
    setClaimMsgKind(null);
    try {
      const res = await walletService.claimDaily();
      setClaimMsg(t('wallet.claimSuccess' as TKey));
      setClaimMsgKind('success');
      setDailyClaimed(true);
      // 领取成功后刷新余额 + 流水
      const bal = await walletService.balance();
      setBalance(bal.balance, res.amount, 'daily_bonus');
      setWalletStats(bal.total_earned ?? null, bal.total_spent ?? null);
      const txList = await walletService.transactions(20, 0);
      setTx(extractEntries(txList));
    } catch (e: any) {
      if (isSessionExpiredError(e)) {
        // session 过期无需渲染 —— 上层 AuthModal 会处理
      } else if (e?.code === 30014) {
        // 后端"今日已领"——按友好文案处理，不当作错误
        setClaimMsg(t('wallet.claimed' as TKey));
        setClaimMsgKind('info');
        setDailyClaimed(true);
      } else {
        setClaimMsg(e?.message || 'error');
        setClaimMsgKind('error');
      }
    } finally {
      setClaimLoading(false);
    }
  };

  if (!open) return null;

  return (
    <div
      className="modal-backdrop"
      role="dialog"
      aria-modal="true"
      aria-label={t('wallet.title' as TKey)}
      onClick={onClose}
    >
      <div
        className="modal wallet-modal"
        onClick={(e) => e.stopPropagation()}
      >
        <h2>
          <span>💰 {t('wallet.title' as TKey)}</span>
          <button
            className="wallet-modal__close"
            onClick={onClose}
            title="Esc"
            aria-label={t('gitLog.close' as TKey)}
          >
            {t('gitLog.close' as TKey)} ✕
          </button>
        </h2>

        {error && <div className="error">{error}</div>}

        {/* 每日奖励区 */}
        <div className="panel-card wallet-modal__claim">
          <div className="wallet-modal__claim-label">
            🎁 {t('wallet.claimDaily' as TKey)}
          </div>
          {dailyClaimed ? (
            <button disabled className="btn btn-sm wallet-claimed">
              ✓ {t('wallet.claimed' as TKey)}
            </button>
          ) : (
            <button
              className="btn btn-sm btn-primary"
              onClick={handleClaim}
              disabled={claimLoading}
            >
              {claimLoading ? '…' : `🎁 ${t('wallet.claimDaily' as TKey)}`}
            </button>
          )}
          {claimMsg && (
            <span
              className={`wallet-modal__claim-msg wallet-modal__claim-msg--${claimMsgKind ?? 'info'}`}
            >
              {claimMsg}
            </span>
          )}
        </div>

        {/* 余额 + 累计 */}
        <div className="panel-card wallet-modal__stats">
          <div className="kv">
            <span className="kv__k">{t('wallet.balance' as TKey)}</span>
            <span className="kv__v wallet-balance">
              💰 {balance != null ? formatBalance(balance, lang) : '—'}
            </span>
          </div>
          <div className="kv">
            <span className="kv__k">{t('wallet.totalEarned' as TKey)}</span>
            <span className="kv__v amount--gain">
              +{totalEarned != null ? formatBalance(totalEarned, lang) : '—'}
            </span>
          </div>
          <div className="kv">
            <span className="kv__k">{t('wallet.totalSpent' as TKey)}</span>
            <span className="kv__v amount--loss">
              -{totalSpent != null ? formatBalance(totalSpent, lang) : '—'}
            </span>
          </div>
        </div>

        {/* 流水 */}
        <h3 className="wallet-modal__tx-title">{t('wallet.recentTx' as TKey)}</h3>
        {loading ? (
          <p className="wallet-modal__empty">{t('common.loading')}</p>
        ) : tx.length === 0 ? (
          <p className="wallet-modal__empty">{t('wallet.noTx' as TKey)}</p>
        ) : (
          <div className="panel-card wallet-modal__tx-panel">
            <div className="wallet-tx wallet-tx--header">
              <span className="wallet-tx__reason">{t('wallet.reason' as TKey)}</span>
              <span className="wallet-tx__amount">{t('wallet.amount' as TKey)}</span>
              <span className="wallet-tx__time">{t('wallet.time' as TKey)}</span>
            </div>
            {tx.map((row) => {
              const { text, cls } = signedAmount(row.amount);
              const rawType = row.tx_type ?? row.reason ?? 'other';
              const date = parseTxTime(row.created_at);
              return (
                <div key={row.id} className="wallet-tx">
                  <span className="wallet-tx__reason">{t(txReasonKey(rawType))}</span>
                  <span className={`wallet-tx__amount ${cls}`}>{text}</span>
                  <span className="wallet-tx__time">
                    {date ? date.toLocaleString() : '—'}
                  </span>
                </div>
              );
            })}
          </div>
        )}

        <div className="wallet-modal__footer">
          <button className="btn btn-sm" onClick={onClose}>
            {t('gitLog.close' as TKey)}
          </button>
        </div>
      </div>
    </div>
  );
}
