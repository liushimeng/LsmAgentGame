import { useEffect, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { authService, isAgentBypassAccount } from '@/services/auth.service';
import { uiStorage } from '@/shared/utils/ui-storage';
import { useT } from '@/hooks/useT';

// §20260821-05: LoginForm 按登录模式完全隔离状态
//
// 账号登录和手机号登录现在是两个独立的子页面，各自保存自己的凭证：
//   - account 模式：使用 account + accountPassword
//   - phone 模式：使用 phone + phonePassword
// 切换模式时自动加载对应保存的凭证

interface ModeCredentials {
  identifier: string;
  password: string;
}

interface CaptchaState {
  id: string;
  svg: string;
  answer: string;
}

// §20260821-05: 类型别名避免 JSX 解析问题
type LoginPayload = {
  account?: string;
  phone?: string;
  password: string
  captcha_id?: string;
  captcha_answer?: string;
};

export function LoginForm({ onSwitch }: { onSwitch: () => void }) {
  const login = useAuth((s) => s.login);
  const t = useT();

  const [mode, setMode] = useState<'account' | 'phone'>('account');

  // §20260821-05: 按模式隔离凭证
  const [accountCreds, setAccountCreds] = useState<ModeCredentials>({ identifier: '', password: '' });
  const [phoneCreds, setPhoneCreds] = useState<ModeCredentials>({ identifier: '', password: '' });

  // §20260821-05: 按模式隔离验证码
  const [accountCaptcha, setAccountCaptcha] = useState<CaptchaState>({ id: '', svg: '', answer: '' });
  const [phoneCaptcha, setPhoneCaptcha] = useState<CaptchaState>({ id: '', svg: '', answer: '' });

  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  // 当前模式的凭证和验证码
  const creds = mode === 'account' ? accountCreds : phoneCreds;
  const setCreds = mode === 'account' ? setAccountCreds : setPhoneCreds;
  const captcha = mode === 'account' ? accountCaptcha : phoneCaptcha;
  const setCaptcha = mode === 'account' ? setAccountCaptcha : setPhoneCaptcha;

  const isAgentBypass = isAgentBypassAccount(mode === 'account' ? accountCreds.identifier : '');
  const requireCaptcha = !isAgentBypass;

  // Hydrate saved credentials once.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const saved = await uiStorage.load();
      if (cancelled) return;
      setAccountCreds({ identifier: saved.account, password: saved.accountPassword });
      setPhoneCreds({ identifier: saved.phone, password: saved.phonePassword });
      setMode(saved.mode);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Fetch a captcha for the current mode.
  async function refreshCaptcha() {
    if (isAgentBypass) return;
    try {
      const ch = await authService.getCaptcha();
      setCaptcha({ id: ch.captcha_id, svg: ch.svg, answer: '' });
      setErr('');
    } catch (e) {
      setErr('failed to load captcha: ' + (e as Error).message);
    }
  }

  // Refresh captcha when mode changes or bypass status changes.
  useEffect(() => {
    refreshCaptcha();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, isAgentBypass]);

  // Clear captcha answer when entering bypass mode.
  useEffect(() => {
    if (isAgentBypass) {
      setCaptcha((prev) => ({ ...prev, answer: '' }));
      setErr('');
    }
  }, [isAgentBypass, setCaptcha]);

  function updateIdentifier(value: string) {
    setCreds((prev) => ({ ...prev, identifier: value }));
  }

  function updatePassword(value: string) {
    setCreds((prev) => ({ ...prev, password: value }));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr('');
    setBusy(true);
    try {
      const captchaId = requireCaptcha ? captcha.id : undefined;
      const captchaAnswer = requireCaptcha ? captcha.answer : undefined;
      const identifier = creds.identifier.trim();
      const passwordValue = creds.password;
      let payload: LoginPayload;
      if (mode === 'phone') {
        payload = { phone: identifier, password: passwordValue, captcha_id: captchaId, captcha_answer: captchaAnswer };
      } else {
        payload = { account: identifier, password: passwordValue, captcha_id: captchaId, captcha_answer: captchaAnswer };
      }
      await login(payload);
      // §20260821-05: 按模式分别保存凭证
      const accountVal = mode === 'account' ? creds.identifier.trim() : accountCreds.identifier.trim();
      const phoneVal = mode === 'phone' ? creds.identifier.trim() : phoneCreds.identifier.trim();
      const accountPwd = mode === 'account' ? creds.password : accountCreds.password;
      const phonePwd = mode === 'phone' ? creds.password : phoneCreds.password;
      await uiStorage.save({
        account: accountVal,
        phone: phoneVal,
        password: '',
        mode,
        accountPassword: accountPwd,
        phonePassword: phonePwd
      });
    } catch (e) {
      const err = e as Error & { code?: number };
      const code = err.code ?? 0;
      setErr(`[${code}] ${err.message}`);
      refreshCaptcha();
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit}>
      <div className="tabs" data-testid="login-form-tabs" style={{ marginBottom: 12 }}>
        <button
          type="button"
          data-testid="login-tab-account"
          className={mode === 'account' ? 'active' : ''}
          onClick={() => setMode('account')}
        >
          {t('auth.account')}
        </button>
        <button
          type="button"
          data-testid="login-tab-phone"
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
            value={accountCreds.identifier}
            onChange={(e) => updateIdentifier(e.target.value)}
            autoComplete="username"
            required
          />
        </div>
      ) : (
        <div className="field">
          <label htmlFor="login-phone">{t('auth.phone')}</label>
          <input
            id="login-phone"
            value={phoneCreds.identifier}
            onChange={(e) => updateIdentifier(e.target.value)}
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
          value={creds.password}
          onChange={(e) => updatePassword(e.target.value)}
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
              value={captcha.answer}
              onChange={(e) => setCaptcha((prev) => ({ ...prev, answer: e.target.value }))}
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
          {captcha.svg && (
            <div
              style={{ marginTop: 6, display: 'flex', justifyContent: 'flex-start' }}
              dangerouslySetInnerHTML={{ __html: captcha.svg }}
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
        <button type="button" className="ghost" data-testid="login-switch-to-register" onClick={onSwitch}>
          {t('auth.register')}
        </button>
        <button type="submit" data-testid="login-submit" disabled={busy}>
          {busy ? t('auth.signingIn') : t('auth.signIn')}
        </button>
      </div>
    </form>
  );
}
