import { useEffect, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { uiStorage } from '@/shared/utils/ui-storage';
import { useT } from '@/hooks/useT';

// RegisterForm mirrors LoginForm's encrypted localStorage hydration so users
// who flip back and forth don't have to retype account/phone. We do NOT
// persist the password on the register path — registration rarely repeats
// and we want the encrypted vault to capture only stable values.
//
// Invitation model (CLAUDE.md invite refactor 2026-06): registration takes ONE
// invite field — the personal invite code (MyInviteCode) of an existing user.
// This single field is what gates registration now; the form keeps an inline
// hint so the user understands where to find it (e.g. from a friend's share).
export function RegisterForm({ onSwitch }: { onSwitch: () => void }) {
  const register = useAuth((s) => s.register);
  const myInviteCode = useAuth((s) => s.myInviteCode);
  const t = useT();
  const [account, setAccount] = useState('');
  const [password, setPassword] = useState('');
  const [phone, setPhone] = useState('');
  const [email, setEmail] = useState('');
  const [inviteCode, setInviteCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const saved = await uiStorage.load();
      if (cancelled) return;
      if (saved.account) setAccount(saved.account);
      if (saved.phone) setPhone(saved.phone);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr('');
    setBusy(true);
    try {
      await register(
        account,
        password,
        phone || undefined,
        email || undefined,
        inviteCode,
      );
      // §20260821-05: 注册成功后不保存密码到本地存储，只保存 account/phone
      await uiStorage.save({
        account: account.trim(),
        phone: phone.trim(),
        password: '',
        mode: 'account',
        accountPassword: '',
        phonePassword: '',
      });
    } catch (e) {
      // §20260817-04 P1 — 与 LoginForm 错误前缀口径统一(code 缺省时省略方括号)。
      const err = e as Error & { code?: number };
      const code = err.code ?? 0;
      setErr(code ? `[${code}] ${err.message}` : err.message);
    } finally {
      setBusy(false);
    }
  }

  async function copyMyCode() {
    if (!myInviteCode) return;
    try {
      await navigator.clipboard.writeText(myInviteCode);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard 不可用时静默忽略 —— 用户仍可手动复制。
    }
  }

  // 成功创建账号后 —— 展示新用户的专属邀请码。
  if (myInviteCode) {
    return (
      <div className="panel-card" style={{ maxWidth: 480 }}>
        <h2 style={{ marginTop: 0 }}>{t('auth.registerSuccessTitle')}</h2>
        <p style={{ color: 'var(--muted)' }}>{t('auth.registerSuccessHint')}</p>
        <div className="kv">
          <span className="kv__k">{t('profile.account')}</span>
          <span className="kv__v">{account}</span>
        </div>
        <div className="kv">
          <span className="kv__k">{t('profile.myInviteCode')}</span>
          <span className="kv__v">
            <code className="invite-code">{myInviteCode}</code>
            <button type="button" className="ghost invite-copy" onClick={copyMyCode}>
              {copied ? t('profile.copied') : t('profile.copy')}
            </button>
          </span>
        </div>
        <div style={{ marginTop: 16 }}>
          <button type="button" className="btn btn-primary" onClick={onSwitch}>
            {t('auth.goToLogin')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={submit}>
      <div className="field">
        <label htmlFor="reg-account">{t('auth.account')}</label>
        <input
          id="reg-account"
          value={account}
          onChange={(e) => setAccount(e.target.value)}
          autoComplete="username"
          required
          minLength={3}
          maxLength={32}
        />
      </div>
      <div className="field">
        <label htmlFor="reg-password">{t('auth.password')}</label>
        <input
          id="reg-password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="new-password"
          required
          minLength={6}
          maxLength={64}
        />
      </div>
      <div className="field">
        <label htmlFor="reg-phone">{t('auth.phone')}</label>
        <input
          id="reg-phone"
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          autoComplete="tel"
          placeholder="+86138…"
        />
      </div>
      <div className="field">
        <label htmlFor="reg-email">{t('auth.email')}</label>
        <input
          id="reg-email"
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoComplete="email"
        />
      </div>
      <div className="field">
        <label htmlFor="reg-invite">{t('auth.inviteCode')}</label>
        <input
          id="reg-invite"
          value={inviteCode}
          onChange={(e) => setInviteCode(e.target.value)}
          autoComplete="off"
          required
          minLength={8}
          maxLength={32}
          placeholder={t('auth.inviteCodePlaceholder')}
        />
      </div>
      {err && <div className="error">{err}</div>}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginTop: 16,
        }}
      >
        <button type="button" className="ghost" data-testid="register-switch-to-login" onClick={onSwitch}>
          {t('auth.signIn')}
        </button>
        <button type="submit" data-testid="register-submit" disabled={busy}>
          {busy ? t('auth.creating') : t('auth.createAccount')}
        </button>
      </div>
    </form>
  );
}
