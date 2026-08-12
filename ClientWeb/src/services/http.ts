// HTTP wrapper around fetch. Always uses same-origin /api paths so the dev
// proxy (and the production server) can route to the right backend.

import type { ApiEnvelope } from '@/types/api';
import { useAuthStore } from '@/store/auth.store';
import {
  isAuthSessionError,
  reasonFromCode,
  reportAuthExpired,
} from './auth.events';

let token: string | null = null;

export function setAuthToken(t: string | null) {
  token = t;
  if (t) localStorage.setItem('lsm.token', t);
  else localStorage.removeItem('lsm.token');
}

export function getAuthToken(): string | null {
  if (token) return token;
  token = localStorage.getItem('lsm.token');
  return token;
}

export class ApiError extends Error {
  constructor(public code: number, message: string, public status: number) {
    super(message);
  }
}

/**
 * Message sentinel placed on the ApiError that handleSessionError() throws for
 * expired/invalid/missing session codes. Callers that otherwise render
 * `e.message` verbatim (lobby pages, profile, etc.) should check
 * `isSessionExpiredError(e)` and treat it as a no-op — the friendly toast in
 * SessionExpiredToast + the reopened AuthModal have already handled it.
 *
 * Kept deliberately out-of-band from human-readable messages so it's never
 * accidentally i18n'd or shown to the user.
 */
export const SESSION_EXPIRED_SENTINEL = 'SESSION_EXPIRED';

export function isSessionExpiredError(e: unknown): boolean {
  return e instanceof ApiError && e.message === SESSION_EXPIRED_SENTINEL;
}

/**
 * On a backend auth-session error (token missing/invalid/expired), kick the
 * user back through the central "session expired" path instead of throwing a
 * raw `[10003] authorization token expired` string for pages to render.
 *
 * The raw message is internal jargon — end users don't know what "authorization
 * token expired" means. We log it locally for debugging, clear the session
 * (which flips isAuthenticated to false and reopens the AuthModal via App.tsx),
 * fire the auth-error bus (so the global toast can show a friendly notice),
 * and throw a *user-safe* ApiError whose message is intentionally unhelpful
 * because callers should NOT render it.
 */
function handleSessionError(code: number, rawMessage: string): never {
  const reason = reasonFromCode(code) ?? 'expired';
  // Surfaced to browser console for debugging; never shown to the user.
  console.warn(`[auth] session error ${code}: ${rawMessage}`);
  // Best-effort in-memory+storage cleanup. We deliberately do NOT await the
  // server logout() call — the token is already unusable there, and the user
  // is being bounced to login regardless.
  void useAuthStore.getState().logout();
  reportAuthExpired(reason);
  throw new ApiError(code, SESSION_EXPIRED_SENTINEL, 401);
}

/**
 * 请求超时错误码(前端本地生成,不与后端 errcode 表冲突 —— 后端码均为正整数)。
 * BUG-R212-P1-03 (2026-07-30):后端若因 bug 永久挂起(如 §92a 自死锁),
 * `fetch` 默认**永不超时**,调用方的 `await` 永远不 resolve,表现为
 * 「弹窗 loading 永久旋转、既不成功也不报错」。给 http() 一个可选超时,
 * 让「后端不响应」变成一个可被 catch、可被展示、可被重试的普通错误。
 */
export const ERR_REQUEST_TIMEOUT = -1;

export function isTimeoutError(e: unknown): boolean {
  return e instanceof ApiError && e.code === ERR_REQUEST_TIMEOUT;
}

export interface HttpOptions extends RequestInit {
  /** 超时毫秒数。未设置 = 不超时(保持既有行为,不影响存量调用方)。 */
  timeoutMs?: number;
}

export async function http<T>(
  path: string,
  init: HttpOptions = {},
): Promise<T> {
  const { timeoutMs, ...rest } = init;
  const headers = new Headers(rest.headers);
  if (!headers.has('Content-Type') && rest.body) {
    headers.set('Content-Type', 'application/json');
  }
  const t = getAuthToken();
  if (t) headers.set('Authorization', `Bearer ${t}`);

  // 仅在显式给了 timeoutMs 时才接管 signal;否则完全走原路径。
  let timer: ReturnType<typeof setTimeout> | undefined;
  let signal = rest.signal ?? undefined;
  if (timeoutMs && timeoutMs > 0) {
    const ac = new AbortController();
    timer = setTimeout(() => ac.abort(), timeoutMs);
    signal = ac.signal;
  }

  let res: Response;
  try {
    res = await fetch(path, { ...rest, headers, signal });
  } catch (e: any) {
    if (timer !== undefined) clearTimeout(timer);
    if (e?.name === 'AbortError' && timeoutMs) {
      throw new ApiError(
        ERR_REQUEST_TIMEOUT,
        `请求超时(${Math.round(timeoutMs / 1000)}秒无响应),服务器可能繁忙或异常,请稍后重试`,
        0,
      );
    }
    throw e;
  }
  if (timer !== undefined) clearTimeout(timer);
  const text = await res.text();
  let body: ApiEnvelope<T> | null = null;
  if (text) {
    try { body = JSON.parse(text) as ApiEnvelope<T>; } catch { /* not json */ }
  }
  if (!res.ok) {
    const code = body?.code ?? res.status;
    const msg = body?.message ?? res.statusText;
    // Classify auth-session failures so we don't leak raw jargon into the UI.
    if (isAuthSessionError(code) || res.status === 401) {
      handleSessionError(code, msg);
    }
    throw new ApiError(code, msg, res.status);
  }
  if (body && typeof body === 'object' && 'code' in body) {
    if (body.code !== 0) {
      // A 2xx body carrying an auth error code (defensive — backend generally
      // uses HTTP 401 for these). Same jargon-free handling.
      if (isAuthSessionError(body.code)) {
        handleSessionError(body.code, body.message);
      }
      throw new ApiError(body.code, body.message, res.status);
    }
    return body.data;
  }
  return text as unknown as T;
}
