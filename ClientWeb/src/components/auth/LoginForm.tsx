import { useEffect, useRef, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { authService } from '@/services/auth.service';
import { ApiError } from '@/services/http';
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

// 2026-09-23 安全加固（tmpPlan 20260923-01 §5/§6）：服务端 errcode ——
// 暴力破解锁定（429 + Retry-After）与 IP 突发限流（429，Retry-After 可缺省）。
const ERR_TOO_MANY_ATTEMPTS = 10501;
const ERR_RATE_LIMITED = 10502;

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

  // 2026-09-23 安全加固：10501/10502 触发的前端锁定倒计时状态。
  // lockedUntilTs 是**绝对时间戳** —— 切换 account/phone 模式不重置它
  //（锁定是账号+IP 维度的服务端状态，不是前端局部状态）。
  const [lockedUntilTs, setLockedUntilTs] = useState(0);
  const [lockSeconds, setLockSeconds] = useState(0);
  const locked = lockedUntilTs > 0;

  // 当前模式的凭证和验证码
  const creds = mode === 'account' ? accountCreds : phoneCreds;
  const setCreds = mode === 'account' ? setAccountCreds : setPhoneCreds;
  const captcha = mode === 'account' ? accountCaptcha : phoneCaptcha;
  const setCaptcha = mode === 'account' ? setAccountCaptcha : setPhoneCaptcha;

  // 2026-08-25 安全加固：CAPTCHA 全员强制，无旁路账号。
  const requireCaptcha = true;

  // Hydrate saved credentials once.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const saved = await uiStorage.load();
      if (cancelled) return;
      // 2026-08-25 安全加固：只回填账号/手机号，不回填密码。
      setAccountCreds({ identifier: saved.account, password: '' });
      setPhoneCreds({ identifier: saved.phone, password: '' });
      setMode(saved.mode);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Fetch a captcha for the current mode.
  async function refreshCaptcha() {
    try {
      const ch = await authService.getCaptcha();
      setCaptcha({ id: ch.captcha_id, svg: ch.svg, answer: '' });
      setErr('');
    } catch (e) {
      setErr('failed to load captcha: ' + (e as Error).message);
    }
  }

  // 定时器/跨渲染帧回调读取的「最新闭包」ref：refreshCaptcha 捕获了当前 mode
  // 对应的 setCaptcha，解锁 tick 触发时必须调用最新一份，否则会刷错模式的验证码。
  const lockedRef = useRef(locked);
  lockedRef.current = locked;
  const refreshCaptchaRef = useRef(refreshCaptcha);
  refreshCaptchaRef.current = refreshCaptcha;

  // 锁定倒计时：每秒刷新剩余秒数；归零自动解锁并刷新一次验证码。
  // 锁定期间刻意不发任何验证码请求（见 catch / 模式切换 effect），避免
  // 自我放大 /api/captcha 请求加剧限流。effect 以绝对时间戳为键，组件
  // 卸载时 cleanup 清理定时器，无泄漏。
  useEffect(() => {
    if (lockedUntilTs <= 0) return;
    const tick = () => {
      const remain = Math.max(0, Math.ceil((lockedUntilTs - Date.now()) / 1000));
      setLockSeconds(remain);
      if (remain <= 0) {
        setLockedUntilTs(0);
        void refreshCaptchaRef.current();
      }
    };
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [lockedUntilTs]);

  // Refresh captcha when mode changes.
  useEffect(() => {
    // 锁定期间不重新领码；到期由上面的 unlock tick 统一刷新一次。
    if (lockedRef.current) return;
    refreshCaptcha();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

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
      // §20260821-05 按模式保存账号；2026-08-25 安全加固：不再保存密码，
      // 密码字段恒为空串（同时以 v3 覆盖旧存量密文）。
      const accountVal = mode === 'account' ? creds.identifier.trim() : accountCreds.identifier.trim();
      const phoneVal = mode === 'phone' ? creds.identifier.trim() : phoneCreds.identifier.trim();
      await uiStorage.save({
        account: accountVal,
        phone: phoneVal,
        password: '',
        mode,
        accountPassword: '',
        phonePassword: ''
      });
    } catch (e) {
      const ae = e as ApiError;
      const code = ae?.code ?? 0;
      if (code === ERR_TOO_MANY_ATTEMPTS || code === ERR_RATE_LIMITED) {
        // 秒数优先级：Retry-After 头 > 消息正文正则 > 缺省 30s（10502 常缺头）。
        const m = /retry after (\d+)/i.exec(ae?.message ?? '');
        const fromMsg = m ? Number.parseInt(m[1], 10) : NaN;
        const seconds =
          ae?.retryAfter && ae.retryAfter > 0
            ? ae.retryAfter
            : Number.isFinite(fromMsg) && fromMsg > 0
              ? fromMsg
              : 30;
        setErr('');
        setLockSeconds(seconds);
        setLockedUntilTs(Date.now() + seconds * 1000);
        // 刻意不调用 refreshCaptcha()：锁定/限流期间任何领码都是自我放大请求。
      } else {
        setErr(`[${code}] ${ae?.message}`);
        refreshCaptcha();
      }
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
              disabled={locked}
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

      {locked ? (
        <div className="error" data-testid="login-lock-notice" role="alert">
          {t('auth.tooManyAttempts', { seconds: lockSeconds })}
        </div>
      ) : (
        err && <div className="error">{err}</div>
      )}

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
        <button type="submit" data-testid="login-submit" disabled={busy || locked}>
          {busy ? t('auth.signingIn') : t('auth.signIn')}
        </button>
      </div>
    </form>
  );
}
