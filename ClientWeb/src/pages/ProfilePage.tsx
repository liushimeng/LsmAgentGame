import { useEffect, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useT } from '@/hooks/useT';
import { isSessionExpiredError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import { userService, walletService } from '@/services/auth.service';
import { useUserStore } from '@/store/user.store';
import type { UserProfile } from '@/types/api';
import type { TKey } from '@/i18n/types';

// 个人中心页 —— 展示当前登录用户的基础信息、昵称修改、个人邀请码及推荐统计。
// 钱包管理（每日奖励 / 流水）已迁移至全站工具栏的「💰 钱包管理」弹出窗口，
// 此处仅保留余额/累计展示 + 引导入口。
export function ProfilePage() {
  const isAuth = useAuth((s) => s.isAuthenticated);
  const t = useT();
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [err, setErr] = useState('');
  const [copied, setCopied] = useState(false);

  // Wallet 余额 + 累计 —— 每次进入页面拉取一次(供 Profile 页展示)
  const {
    totalEarned,
    totalSpent,
    setBalance,
    setWalletStats,
  } = useUserStore();
  const [walletBalance, setWalletBalance] = useState<number | null>(null);

  // Nickname editing state
  const [editingNick, setEditingNick] = useState(false);
  const [nickDraft, setNickDraft] = useState('');
  const [nickSaving, setNickSaving] = useState(false);
  const [nickErr, setNickErr] = useState('');
  const [nickOk, setNickOk] = useState('');

  useEffect(() => {
    if (!isAuth) return;
    let cancelled = false;
    userService.getProfile()
      .then((p) => { if (!cancelled) { setProfile(p); setNickDraft(p.nickname || ''); } })
      .catch((e: Error) => {
        if (!cancelled && !isSessionExpiredError(e)) {
          setErr(e.message);
          reportGlobalError({ message: e.message, severity: 'error' });
        }
      });
    return () => { cancelled = true; };
  }, [isAuth]);

  // 拉一次余额 + 累计,写入 store,供 AppHeader 的 💰 按钮与本页面共用
  useEffect(() => {
    if (!isAuth) return;
    let cancelled = false;
    const load = async () => {
      try {
        const bal = await walletService.balance();
        if (!cancelled) {
          setWalletBalance(bal.balance);
          setBalance(bal.balance, null, null);
          setWalletStats(bal.total_earned ?? null, bal.total_spent ?? null);
        }
      } catch {
        // 静默
      }
    };
    void load();
    return () => { cancelled = true; };
  }, [isAuth, setBalance, setWalletStats]);

  async function copyCode() {
    if (!profile?.my_invite_code) return;
    try {
      await navigator.clipboard.writeText(profile.my_invite_code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard 不可用时静默忽略 —— 用户仍可手动复制。
    }
  }

  async function saveNickname() {
    const nn = nickDraft.trim();
    if (!nn) {
      setNickErr(t('profile.nicknameRequired'));
      return;
    }
    setNickSaving(true);
    setNickErr('');
    setNickOk('');
    try {
      await userService.updateNickname(nn);
      setProfile((prev) => prev ? { ...prev, nickname: nn } : prev);
      setEditingNick(false);
      setNickOk(t('profile.nicknameSaved'));
      setTimeout(() => setNickOk(''), 2000);
    } catch (e: any) {
      if (!isSessionExpiredError(e)) {
        setNickErr(e.message || 'error');
        reportGlobalError({ message: e.message || 'error', severity: 'error' });
      }
    } finally {
      setNickSaving(false);
    }
  }

  if (!isAuth) {
    return (
      <div>
        <h1 style={{ marginTop: 0 }}>{t('profile.title')}</h1>
        <p style={{ color: 'var(--muted)' }}>{t('profile.loginRequired')}</p>
      </div>
    );
  }

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>{t('profile.title')}</h1>
      {err && <div className="error">{err}</div>}
      {!profile ? (
        <p style={{ color: 'var(--muted)' }}>{t('common.loading')}</p>
      ) : (
        <>
          <div className="panel-card">
            <div className="kv">
              <span className="kv__k">{t('profile.nickname')}</span>
              <span className="kv__v">
                {editingNick ? (
                  <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}>
                    <input
                      type="text"
                      value={nickDraft}
                      maxLength={64}
                      placeholder={t('profile.nicknamePlaceholder')}
                      onChange={(e) => setNickDraft(e.target.value)}
                      onKeyDown={(e) => { if (e.key === 'Enter') saveNickname(); }}
                      style={{ maxWidth: 200 }}
                    />
                    <button className="btn btn-sm btn-primary" onClick={saveNickname} disabled={nickSaving}>
                      {nickSaving ? '…' : '✓'}
                    </button>
                    <button className="btn btn-sm btn-ghost" onClick={() => { setEditingNick(false); setNickDraft(profile.nickname || ''); setNickErr(''); }}>
                      ✕
                    </button>
                  </span>
                ) : (
                  <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center' }}>
                    <span>{profile.nickname || profile.account}</span>
                    <button type="button" className="ghost" onClick={() => { setEditingNick(true); setNickDraft(profile.nickname || ''); }}>
                      ✏️ {t('profile.editNickname')}
                    </button>
                  </span>
                )}
              </span>
            </div>
            {nickErr && <div className="error" style={{ marginTop: 4 }}>{nickErr}</div>}
            {nickOk && <div style={{ color: 'var(--green, #4caf50)', marginTop: 4, fontSize: '0.9em' }}>{nickOk}</div>}
            <div className="kv">
              <span className="kv__k">{t('profile.account')}</span>
              <span className="kv__v">{profile.account}</span>
            </div>
            <div className="kv">
              <span className="kv__k">{t('profile.myInviteCode')}</span>
              <span className="kv__v">
                <code className="invite-code">{profile.my_invite_code}</code>
                <button type="button" className="ghost invite-copy" onClick={copyCode}>
                  {copied ? t('profile.copied') : t('profile.copy')}
                </button>
              </span>
            </div>
            <div className="kv">
              <span className="kv__k">{t('profile.referralCount')}</span>
              <span className="kv__v">{profile.referral_count}</span>
            </div>
          </div>

          <h2 style={{ marginBottom: 8 }}>{t('profile.referralsTitle')}</h2>
          {profile.referrals.length === 0 ? (
            <p style={{ color: 'var(--muted)' }}>{t('profile.referralsEmpty')}</p>
          ) : (
            <div className="panel-card">
              {profile.referrals.map((r) => (
                <div className="kv" key={r.user_id}>
                  <span className="kv__k">{r.nickname || r.account}</span>
                  <span className="kv__v">
                    {(() => {
                      const ts = r.created_at;
                      const d = typeof ts === 'number' ? new Date(ts * 1000) : new Date(ts);
                      return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
                    })()}
                  </span>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {/* ===== 我的钱包 ===== */}
      {/* 钱包管理功能已迁移至全站工具栏的「💰 钱包管理」弹出窗口,
          避免两处实现不同步(每日奖励、流水、累计等)。此处仅保留一个引导入口。 */}
      <h2 style={{ marginBottom: 8 }}>💰 {t('wallet.title' as TKey)}</h2>
      <div className="panel-card" style={{ marginBottom: 16 }}>
        <div className="kv">
          <span className="kv__k">{t('wallet.balance' as TKey)}</span>
          <span className="kv__v wallet-balance">
            💰 {walletBalance != null ? walletBalance.toLocaleString() : '—'}
          </span>
        </div>
        <div className="kv">
          <span className="kv__k">{t('wallet.totalEarned' as TKey)}</span>
          <span className="kv__v amount--gain">
            +{totalEarned != null ? totalEarned.toLocaleString() : '—'}
          </span>
        </div>
        <div className="kv">
          <span className="kv__k">{t('wallet.totalSpent' as TKey)}</span>
          <span className="kv__v amount--loss">
            -{totalSpent != null ? totalSpent.toLocaleString() : '—'}
          </span>
        </div>
        <p style={{ color: 'var(--muted)', fontSize: '0.9em', margin: '8px 0 0' }}>
          💡 {t('wallet.recentTx' as TKey)} / {t('wallet.claimDaily' as TKey)} ——
          请点击工具栏 💰 按钮打开「钱包管理」窗口。
        </p>
      </div>
    </div>
  );
}
