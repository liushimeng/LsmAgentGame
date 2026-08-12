// Friendly, localized notice shown when the session dies (token expired/invalid
// or WS refresh failed). Replaces the leak of the raw server string
// "authorization token expired" that used to be dumped into the page.

import { useEffect, useState } from 'react';
import { onAuthError, type AuthExpiredReason } from '@/services/auth.events';
import { useT } from '@/hooks/useT';

/** Map each classified reason to the i18n key for the user-facing message. */
const KEY_BY_REASON: Record<AuthExpiredReason, 'auth.sessionExpired' | 'auth.sessionInvalid' | 'auth.sessionMissing'> = {
  expired: 'auth.sessionExpired',
  invalid: 'auth.sessionInvalid',
  missing: 'auth.sessionMissing',
};

const AUTO_DISMISS_MS = 8000;

export function SessionExpiredToast() {
  const t = useT();
  const [msg, setMsg] = useState<string | null>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    return onAuthError((reason) => {
      setMsg(t(KEY_BY_REASON[reason]));
      setVisible(true);
    });
  }, [t]);

  useEffect(() => {
    if (!visible) return;
    const id = setTimeout(() => setVisible(false), AUTO_DISMISS_MS);
    return () => clearTimeout(id);
  }, [visible, msg]);

  if (!visible || !msg) return null;

  return (
    <div className="auth-error-toast" role="alert">
      <span className="auth-error-toast__text">{msg}</span>
      <button className="auth-error-toast__close" onClick={() => setVisible(false)} aria-label="dismiss">
        ×
      </button>
    </div>
  );
}
