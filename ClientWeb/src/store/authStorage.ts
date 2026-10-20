/**
 * authStorage — lsm.auth 落盘存储层（批次 62 前置加固 · 2026-10-20）。
 *
 * ## 为什么单独成文件
 * 本模块**零业务依赖**（只 import zustand 的类型），因此可以被 Node 直接加载做
 * 回归验证：`ClientWeb/scripts/verify_auth_persist.mjs` 用真实 zustand 4.5.7 +
 * 真实本模块跑「刷新后登录态是否还在 / 坏 key 是否自愈」的断言，**不依赖浏览器**。
 * 验证入口：`npm run verify:auth-persist`。
 *
 * ## 这次改了什么（**行为等价**，不是修功能缺陷）
 * 此前 `auth.store.ts` 用 zustand 的**已废弃** `deserialize` 选项做「坏 key 防黑屏」：
 * ```ts
 * deserialize: (str) => { try { return JSON.parse(str) } catch { warn; return null } }
 * ```
 * 实测（CDP + 真实 zustand 4.5.7 源码核对）**原实现没有功能缺陷**：
 * `deserialize` 一旦出现在 options 里，zustand 4.5.7 的 `persistImpl` 会切到
 * **oldImpl**（`node_modules/zustand/esm/middleware.mjs` 第 571~578 行），
 * 而 oldImpl 用 `getStorage: () => localStorage`（**原始 localStorage**），
 * `getItem` 返回的是**未解析的字符串** ⇒ `JSON.parse(str)` 正常工作 ⇒
 * 登录态刷新后正常保留。已用 CDP 实测确认（注入合法 lsm.auth + lsm.token → 刷新
 * → token 保留、无登录框、console 无告警）。
 *
 * 但这条路径有三个隐患：
 *   1. **oldImpl 完全忽略 `storage` 选项** —— 仓库里若同时写了 `storage:`，
 *      它是**静默失效的死代码**；
 *   2. 每次页面加载都打 zustand 的 `[DEPRECATED] ...` 警告；
 *   3. **zustand v5 删除了 `deserialize`** ⇒ 升级后自动落回
 *      `createJSONStorage`，而它的 `getItem` 内部 `JSON.parse(str)` **没有
 *      try/catch** ⇒ 坏 key 同步抛错 ⇒ persist 链尾 `.catch` 吞错 ⇒
 *      `hasHydrated` 永假 ⇒ App.tsx 的 hydration 闸门
 *      （`if (!hasHydrated) return <div className="app-shell" />`）永久卡住
 *      ⇒ **全站黑屏**（这正是 a6197523fe 当初想防的场景，但它靠 deprecated
 *      选项实现，升级即失效）。
 *
 * ## 现在的做法
 * 把「读时容错」下沉到 **storage 层**（现代、非废弃 API），并**移除
 * `deserialize` 选项** ⇒ zustand 走 newImpl ⇒ 真正使用我们提供的 storage：
 *   - `safeLocalStorage.getItem`：解析失败**不抛**，清除坏 key 并返回 null
 *     （=「无持久化状态」）⇒ persist 走成功路径、`hasHydrated` 正常置位、
 *     **不黑屏**、坏 key 立即自愈；
 *   - `safeLocalStorage.setItem` / `removeItem`：包 try/catch，隐私模式/配额满
 *     时静默降级，不影响本次会话。
 *
 * App.tsx 的 1500ms 超时兜底闸门（第二道保险）保留不动。
 */

import type { PersistStorage, StorageValue } from 'zustand/middleware';

/** lsm.auth 落盘字段形状（与 `auth.store.ts` 的 partialize 一一对应）。 */
export interface AuthPersistedShape {
  userId: string | null;
  token: string | null;
  expiresAt: number | null;
  isAuthenticated: boolean;
  userType: number | null;
  myInviteCode: string | null;
}

/**
 * 读时容错的 `PersistStorage`。
 *
 * 不用 zustand 默认的 `createJSONStorage(() => localStorage)` 的原因：它的
 * `getItem` 内部直接 `JSON.parse(str)` 且**没有 try/catch** —— 当 `lsm.auth`
 * 被写成非 JSON（例如 `'[object Object]'`）时会**同步抛错**，persist 链尾
 * `.catch` 吞错 ⇒ `hasHydrated` 永假 ⇒ App.tsx 的 hydration 闸门
 * （`if (!hasHydrated) return <div className="app-shell" />`）永久卡住 ⇒ 全站黑屏。
 */
export const safeLocalStorage: PersistStorage<AuthPersistedShape> = {
  getItem: (name) => {
    let raw: string | null = null;
    try {
      raw = localStorage.getItem(name);
    } catch {
      // 隐私模式 / 存储被禁用：视为「无持久化状态」，不抛。
      return null;
    }
    if (raw == null) return null;
    try {
      return JSON.parse(raw) as StorageValue<AuthPersistedShape>;
    } catch {
      console.warn('[auth] lsm.auth 内容损坏（非法 JSON），已清除该键并按未登录启动');
      try {
        localStorage.removeItem(name);
      } catch {
        /* 存储不可用时忽略 */
      }
      return null;
    }
  },
  setItem: (name, value) => {
    try {
      localStorage.setItem(name, JSON.stringify(value));
    } catch {
      /* 配额满 / 隐私模式：静默降级，不影响本次会话 */
    }
  },
  removeItem: (name) => {
    try {
      localStorage.removeItem(name);
    } catch {
      /* 忽略 */
    }
  },
};
