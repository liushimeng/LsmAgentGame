import { useEffect, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { authService, isAgentBypassAccount } from '@/services/auth.service';
import { uiStorage } from '@/shared/utils/ui-storage';
import { useT } from '@/hooks/useT';

// LoginForm renders two modes (account | phone), both with captcha.
//
// Behavior:
//   - On mount, hydrate the form fields from the encrypted localStorage blob
//     (lsm.auth.ui) and fetch the first captcha challenge.
//   - On submit, save the form values back to the encrypted blob before
//     hitting authService — this guarantees persistence on success.
//   - The agent bypass account (test19082jauishf8) hides the captcha UI and
//     does NOT send captcha_id/answer; the server-side gate mirrors this.
//   - Errors from the server come back as ApiError(code, message). We show
//     the message verbatim (still localized by the server's English table).
export function LoginForm({ onSwitch }: { onSwitch: () => void }) {
  const login = useAuth((s) => s.login);
  const t = useT();

  const [mode, setMode] = useState<'account' | 'phone'>('account');
  const [account, setAccount] = useState('');
  const [phone, setPhone] = useState('');
  const [password, setPassword] = useState('');
  const [captchaId, setCaptchaId] = useState('');
  const [captchaSvg, setCaptchaSvg] = useState('');
  const [captchaAnswer, setCaptchaAnswer] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const isAgentBypass = isAgentBypassAccount(account);
  const requireCaptcha = !isAgentBypass;

  // Hydrate saved credentials once.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const saved = await uiStorage.load();
      if (cancelled) return;
      setAccount(saved.account);
      setPhone(saved.phone);
      setPassword(saved.password);
      setMode(saved.mode);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Fetch a captcha whenever one is needed (initial mount, after submit,
  // and when the user switches to a non-bypass mode while typing an account
  // name other than the bypass).
  async function refreshCaptcha() {
    if (isAgentBypass) return;
    try {
      const ch = await authService.getCaptcha();
      setCaptchaId(ch.captcha_id);
      setCaptchaSvg(ch.svg);
      setCaptchaAnswer('');
      setErr('');
    } catch (e) {
      setErr('failed to load captcha: ' + (e as Error).message);
    }
  }

  useEffect(() => {
    refreshCaptcha();
    // We intentionally exclude refreshCaptcha deps; we re-run only when the
    // account field becomes (non-)bypass.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAgentBypass]);

  // Strip the captcha payload if the user starts typing the bypass name —
  // prevents stale IDs from being submitted accidentally.
  useEffect(() => {
    if (isAgentBypass) {
      setCaptchaAnswer('');
      setErr('');
    }
  }, [isAgentBypass]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr('');
    setBusy(true);
    try {
      const payload =
        mode === 'phone'
          ? {
              phone: phone.trim(),
              password,
              captcha_id: requireCaptcha ? captchaId : undefined,
              captcha_answer: requireCaptcha ? captchaAnswer : undefined,
            }
          : {
              account: account.trim(),
              password,
              captcha_id: requireCaptcha ? captchaId : undefined,
              captcha_answer: requireCaptcha ? captchaAnswer : undefined,
            };
      await login(payload);
      // Persist encrypted localStorage only on success — if the user typed
      // wrong creds we don't want the wrong password saved.
      await uiStorage.save({
        account: account.trim(),
        phone: phone.trim(),
        password,
        mode,
      });
    } catch (e) {
      const err = e as Error & { code?: number };
      const code = err.code ?? 0;
      setErr(`[${code}] ${err.message}`);
      // Captcha is single-use on the server — always re-fetch after any
      // failure so the user can retry with a fresh challenge.
      refreshCaptcha();
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit}>
      <div className="tabs" style={{ marginBottom: 12 }}>
        <button
          type="button"
          className={mode === 'account' ? 'active' : ''}
          onClick={() => setMode('account')}
        >
          {t('auth.account')}
        </button>
        <button
          type="button"
          className={mode === 'phone' ? 'active' : ''}
          onClick={() => setMode('phone')}
        >
          {t('auth.phone')}
        </button>
      </div>

      {mode === 'account' ? (
        <div className="field">
          <label htmlFor="login-account">{t('auth.account')}</label>
          <input
            id="login-account"
            value={account}
            onChange={(e) => setAccount(e.target.value)}
            autoComplete="username"
            required
          />
        </div>
      ) : (
        <div className="field">
          <label htmlFor="login-phone">{t('auth.phone')}</label>
          <input
            id="login-phone"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            autoComplete="tel"
            required
            placeholder="+86138…"
          />
        </div>
      )}

      <div className="field">
        <label htmlFor="login-password">{t('auth.password')}</label>
        <input
          id="login-password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
          minLength={6}
        />
      </div>

      {requireCaptcha && (
        <div className="field">
          <label htmlFor="login-captcha">{t('auth.captcha')}</label>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <input
              id="login-captcha"
              value={captchaAnswer}
              onChange={(e) => setCaptchaAnswer(e.target.value)}
              autoComplete="off"
              required
              style={{ flex: 1 }}
              maxLength={8}
            />
            <button
              type="button"
              className="ghost"
              onClick={refreshCaptcha}
              aria-label={t('auth.refreshCaptcha')}
            >
              ↻
            </button>
          </div>
          {captchaSvg && (
            <div
              style={{ marginTop: 6, display: 'flex', justifyContent: 'flex-start' }}
              dangerouslySetInnerHTML={{ __html: captchaSvg }}
              aria-label="captcha image"
            />
          )}
        </div>
      )}

      {err && <div className="error">{err}</div>}

      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginTop: 16,
        }}
      >
        <button type="button" className="ghost" onClick={onSwitch}>
          {t('auth.register')}
        </button>
        <button type="submit" disabled={busy}>
          {busy ? t('auth.signingIn') : t('auth.signIn')}
        </button>
      </div>
    </form>
  );
}
