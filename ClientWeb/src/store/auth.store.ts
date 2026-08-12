import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { authService } from '@/services/auth.service';
import { setAuthToken } from '@/services/http';
import { useI18nStore } from '@/store/i18n.store';

export interface LoginPayload {
  account?: string;
  phone?: string;
  password: string;
  captcha_id?: string;
  captcha_answer?: string;
}

export interface AuthState {
  userId: string | null;
  token: string | null;
  expiresAt: number | null;
  isAuthenticated: boolean;
  userType: number | null; // 1=normal, 2=admin, 3=super admin
  // The user's own personal invite code. Surfaced on register so the UI can
  // immediately tell the new user "here's your code, share it with friends".
  // Mirrored on /api/user/profile so the personal-center page can show
  // "my invite code" without a second round-trip.
  myInviteCode: string | null;
  // Whether Zustand persist has finished rehydrating from storage.
  // Used by App.tsx to avoid rendering the auth-modal/layout mismatch
  // during the first paint after F5 refresh.
  hasHydrated: boolean;
  setHasHydrated: (v: boolean) => void;
  login: (payload: LoginPayload) => Promise<void>;
  // Invitation model (CLAUDE.md invite refactor 2026-06): registration
  // takes ONE invite field — the personal invite code of an existing user
  // (their MyInviteCode on the server). There is no separate admin gate.
  register: (
    account: string,
    password: string,
    phone?: string,
    email?: string,
    inviteCode?: string,
  ) => Promise<void>;
  logout: () => Promise<void>;
  hydrateFromStorage: () => void;
}

/**
 * Defensive sanity-check on the `expires_at` Unix-seconds returned by
 * login / register / refresh. We NEVER reject the token here — the real
 * arbiter is the next API call's 401 path. But when the timestamp is
 * clearly wrong (non-positive, or already in the past from the client's
 * point of view) we log a warning so clock-skew between the server and
 * the client can be spotted in the devtools console. See
 * docs/design/login-bug-fix-design.md §2.2.2.
 */
function validateExpiresAt(
  expiresAt: number | null | undefined,
  tag: 'login' | 'register' | 'refresh',
): void {
  if (expiresAt == null || !Number.isFinite(expiresAt) || expiresAt <= 0) {
    console.warn(`[auth] ${tag}: expects positive expires_at, got ${expiresAt}; token still applied, API will arbitrate`);
    return;
  }
  const nowSec = Math.floor(Date.now() / 1000);
  if (expiresAt <= nowSec) {
    const drift = nowSec - expiresAt;
    console.warn(`[auth] ${tag}: expires_at already past (server=${expiresAt}, client=${nowSec}, drift=+${drift}s). Token still applied — client clock may be fast, or server clock slow. API call will arbitrate.`);
  }
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      userId: null,
      token: null,
      expiresAt: null,
      isAuthenticated: false,
      userType: null,
      myInviteCode: null,
      hasHydrated: false,
      setHasHydrated: (v) => set({ hasHydrated: v }),
      async login(payload) {
        const data = await authService.login(payload);
        validateExpiresAt(data.expires_at, 'login');
        setAuthToken(data.token);
        set({
          userId: data.user_id,
          token: data.token,
          expiresAt: data.expires_at,
          isAuthenticated: true,
          userType: data.user_type ?? 1,
          myInviteCode: data.my_invite_code ?? null,
        });
        // 服务器保存的语言为准,覆盖本地选择。
        useI18nStore.getState().applyServerLang(data.language);
      },
      async register(account, password, phone, email, inviteCode) {
        const data = await authService.register({
          account,
          password,
          phone,
          email,
          referrer_code: inviteCode ?? '',
        });
        validateExpiresAt(data.expires_at, 'register');
        setAuthToken(data.token);
        set({
          userId: data.user_id,
          token: data.token,
          expiresAt: data.expires_at,
          isAuthenticated: true,
          userType: data.user_type ?? 1,
          myInviteCode: data.my_invite_code ?? null,
        });
        useI18nStore.getState().applyServerLang(data.language);
      },
      async logout() {
        try {
          await authService.logout();
        } finally {
          setAuthToken(null);
          set({
            userId: null,
            token: null,
            expiresAt: null,
            isAuthenticated: false,
            userType: null,
            myInviteCode: null,
          });
        }
      },
      hydrateFromStorage() {
        const raw = localStorage.getItem('lsm.token');
        if (raw) setAuthToken(raw);
        // Quiet auth bootstrap — Zustand `persist` already rehydrated
        // `isAuthenticated` from `lsm.auth`, so we should NOT clobber it
        // with `false` just because some other persisted field (e.g.
        // `expiresAt`) is missing. The ONE validation we DO enforce here:
        //   1. A token must be present (in store and in localStorage).
        //
        // IMPORTANT (login-bug-fix-design.md §2.2.1): we deliberately do
        // NOT check `expiresAt*1000 > Date.now()` here. The hydrate stage
        // has no access to the server's current clock, so any client-side
        // expiry check is vulnerable to clock skew (VM resume / NTP jump /
        // device clock running fast) — the exact trigger for the symptom
        // "登录已过期,请重新登录。" / "请先登录。" appearing right after a
        // successful login. We let the token through and let the next API
        // call's 401 path (http.ts::handleSessionError) arbitrate real
        // expiry. That path is robust (logout + reportAuthExpired).
        const persisted = get();
        const hasToken = !!persisted.token && !!raw;
        if (hasToken) {
          set({
            isAuthenticated: true,
            userType: persisted.userType ?? 1,
            myInviteCode: persisted.myInviteCode ?? null,
          });
        } else if (persisted.isAuthenticated) {
          // Was previously authenticated but the token is no longer present
          // — explicit downgrade so App.tsx reopens AuthModal.
          set({ isAuthenticated: false });
        }
      },
    }),
    {
      name: 'lsm.auth',
      partialize: (s) => ({
        userId: s.userId,
        token: s.token,
        expiresAt: s.expiresAt,
        isAuthenticated: s.isAuthenticated,
        userType: s.userType,
        myInviteCode: s.myInviteCode,
      }),
      onRehydrateStorage: () => (state) => {
        // Mark hydration complete once persist has read storage.
        state?.setHasHydrated(true);
      },
    },
  ),
);
