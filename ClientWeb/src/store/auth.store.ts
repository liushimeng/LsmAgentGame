import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { authService } from '@/services/auth.service';
import { setAuthToken } from '@/services/http';
import { useI18nStore } from '@/store/i18n.store';
import { safeLocalStorage, type AuthPersistedShape } from '@/store/authStorage';

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

/*
 * auth persist 落盘字段形状（与下方 partialize 一一对应）现由
 * `./authStorage` 统一导出（批次 62 前置加固 · 2026-10-20）——
 * 存储层拆到零依赖模块，便于 Node 侧直接做回归验证。
 */

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
      partialize: (s): AuthPersistedShape => ({
        userId: s.userId,
        token: s.token,
        expiresAt: s.expiresAt,
        isAuthenticated: s.isAuthenticated,
        userType: s.userType,
        myInviteCode: s.myInviteCode,
      }),
      // 落盘读取容错（批次 62 前置加固 · 2026-10-20）：
      //
      // 【为什么改】此前这里用 zustand 的**已废弃** `deserialize` 选项做
      // 「坏 key 防黑屏」。实测（CDP + zustand 4.5.7 源码核对）**原实现没有功能
      // 缺陷**：`deserialize` 一旦出现在 options 里，persistImpl 会切到
      // **oldImpl**（esm/middleware.mjs 第 571~578 行），oldImpl 用原始
      // `localStorage`（`getItem` 返回**未解析的字符串**）⇒ `JSON.parse(str)`
      // 正常工作 ⇒ 刷新后登录态正常保留。已用 CDP 实测确认。
      //
      // 但这条路径有三个隐患：
      //   1. oldImpl **完全忽略 `storage` 选项** ⇒ 同时写的 `storage:` 是死代码；
      //   2. 每次页面加载都打 zustand 的 `[DEPRECATED] ...` 警告；
      //   3. **zustand v5 删除了 `deserialize`** ⇒ 升级后自动落回
      //      `createJSONStorage`，其 `getItem` 内部 `JSON.parse` **无 try/catch**
      //      ⇒ 坏 key 同步抛错 ⇒ persist 链尾 `.catch` 吞错 ⇒ hasHydrated 永假
      //      ⇒ App.tsx 的 hydration 闸门永久卡住 ⇒ **全站黑屏**
      //      （正是 a6197523fe 当初想防的场景，但它靠 deprecated 选项实现，
      //        升级即失效）。
      //
      // 【现在的做法】把「读时容错」下沉到 **storage 层**（现代、非废弃 API），
      // 并**移除 `deserialize` 选项** ⇒ zustand 走 newImpl ⇒ 真正使用我们的
      // storage：解析失败**不抛**、清除坏 key、返回 null ⇒ persist 走成功路径、
      // hasHydrated 正常置位、**不黑屏**、坏 key 立即自愈。详见 authStorage.ts。
      //
      // 【保住的能力】App.tsx 的 1500ms 超时兜底闸门（第二道保险）**保留不动**。
      storage: safeLocalStorage,
      onRehydrateStorage: () => (state) => {
        // Mark hydration complete once persist has read storage.
        state?.setHasHydrated(true);
      },
    },
  ),
);
