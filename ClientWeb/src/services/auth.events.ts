// Centralized "auth failed, send the user back to the login modal" channel.
//
// The raw server error ("authorization token expired", code 10003) is internal
// jargon we never want to render to users. Whenever an API call or the WS layer
// detects the session is gone, those callers call reportAuthExpired() instead
// of dumping the server message verbatim. Anything subscribed to onAuthError
// (currently the global toast in AppLayout) shows a localized, friendly notice.
//
// Why a module-level bus and not just a zustand field:
//   - services/http.ts and services/ws.ts run outside React (fetch
//     interceptors, WS timers). They can't use hooks, but they CAN import a
//     plain subscriber set.
//   - The signal carries *one* of a small set of classified reasons so the UI
//     can pick the right localized string — not display the raw message.

/** Why the user is being bounced back to the login modal. */
export type AuthExpiredReason = 'expired' | 'invalid' | 'missing';

type AuthErrorListener = (reason: AuthExpiredReason) => void;

const listeners = new Set<AuthErrorListener>();

/** Subscribe to session-expiry events. Returns an unsubscribe fn. */
export function onAuthError(fn: AuthErrorListener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/**
 * Notify every subscriber that the session can no longer be used. Callers are
 * responsible for also logging the user out (which then opens the AuthModal via
 * App.tsx's `!isAuthenticated && <AuthModal/>` rule).
 */
export function reportAuthExpired(reason: AuthExpiredReason = 'expired'): void {
  // Copy the set before iterating so a subscriber that unsubscribes mid-fire
  // doesn't mutate the collection under our feet.
  for (const fn of [...listeners]) fn(reason);
}

/** Translate a backend auth error code into our classified reason, if it is one. */
export function reasonFromCode(code: number): AuthExpiredReason | null {
  switch (code) {
    case 10003: // ErrAuthTokenExpired
      return 'expired';
    case 10002: // ErrAuthInvalidToken
      return 'invalid';
    case 10001: // ErrAuthMissingToken
      return 'missing';
    default:
      return null;
  }
}

/** True when the backend error code represents an unusable session. */
export function isAuthSessionError(code: number): boolean {
  return reasonFromCode(code) !== null;
}
