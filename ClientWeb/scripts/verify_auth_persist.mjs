#!/usr/bin/env node
/**
 * verify_auth_persist.mjs —— lsm.auth 持久化回归验证（批次 62 前置加固 · 2026-10-20）
 *
 * ## 背景
 * `auth.store.ts` 过去用 zustand 的**已废弃** `deserialize` 选项实现「坏 key
 * 防黑屏」。实测**原实现没有功能缺陷**（zustand 4.5.7 的 persistImpl 见到
 * `deserialize` 会切到 oldImpl，oldImpl 传的是**未解析的原始字符串**，
 * `JSON.parse(str)` 正常工作）。但该路径有三个隐患：
 *   1. oldImpl **完全忽略 `storage` 选项** ⇒ 同时写的 `storage:` 是死代码；
 *   2. 每次页面加载都打 zustand 的 `[DEPRECATED] ...` 警告；
 *   3. **zustand v5 删除了 `deserialize`** ⇒ 升级后自动落回
 *      `createJSONStorage`，其 `getItem` 内部 `JSON.parse` **无 try/catch**
 *      ⇒ 坏 key 同步抛错 ⇒ persist 链尾 `.catch` 吞错 ⇒ `hasHydrated` 永假
 *      ⇒ App.tsx 的 hydration 闸门永久卡住 ⇒ **全站黑屏**。
 *
 * 现在的做法：容错下沉到 **storage 层**（`src/store/authStorage.ts`），
 * 并移除 `deserialize` 选项 ⇒ zustand 走 newImpl ⇒ 真正使用我们的 storage。
 *
 * ## 本脚本验什么
 * 用**真实 zustand 4.5.7** + **真实 src/store/authStorage.ts**（零依赖模块）
 * 复刻 auth.store.ts 的 persist 配置（**不带** deserialize ⇒ 走 newImpl 真路径），断言：
 *   (a) 合法持久化内容 ⇒ rehydrate 后 `token` 仍在、`isAuthenticated === true`、
 *       落盘内容未被空状态覆盖；
 *   (b) 损坏内容（`'[object Object]'`）⇒ **不抛错**、`hasHydrated` 最终为 true、
 *       store 回到默认（未登录）、坏 key 被清除；
 *   (c) 源码断言：`auth.store.ts` 必须真的引用 `safeLocalStorage`，且**不再**
 *       使用 deprecated 的 `deserialize` 选项（防止悄悄退回旧路径）。
 *
 * ## 运行
 *   npm run verify:auth-persist
 *   （需要 Node ≥ 22.6，依赖内置的 TS 类型擦除；如提示不支持请加
 *    `--experimental-strip-types`）
 */

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

// ── 1. 假 localStorage（Map 背书）──────────────────────────────────────
class FakeLocalStorage {
  constructor() {
    this.map = new Map();
  }
  getItem(k) {
    return this.map.has(k) ? this.map.get(k) : null;
  }
  setItem(k, v) {
    this.map.set(k, String(v));
  }
  removeItem(k) {
    this.map.delete(k);
  }
  clear() {
    this.map.clear();
  }
}

const store = new FakeLocalStorage();
globalThis.localStorage = store;

// ── 2. 加载真实模块（TS 类型擦除）────────────────────────────────────
const here = dirname(fileURLToPath(import.meta.url));
const clientRoot = resolve(here, '..');
const { safeLocalStorage } = await import(
  resolve(clientRoot, 'src/store/authStorage.ts')
);
const { create } = await import('zustand');
const { persist } = await import('zustand/middleware');

/** 与 auth.store.ts 的 persist 配置同构的最小复刻（**不带 deserialize** ⇒ newImpl）。 */
function makeAuthStore() {
  return create(
    persist(
      (set) => ({
        userId: null,
        token: null,
        expiresAt: null,
        isAuthenticated: false,
        userType: null,
        myInviteCode: null,
        hasHydrated: false,
        setHasHydrated: (v) => set({ hasHydrated: v }),
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
        storage: safeLocalStorage,
        onRehydrateStorage: () => (state) => {
          state?.setHasHydrated(true);
        },
      },
    ),
  );
}

const results = [];
function check(name, fn) {
  try {
    fn();
    results.push({ name, ok: true });
    console.log(`  ✅ ${name}`);
  } catch (e) {
    results.push({ name, ok: false, err: e });
    console.log(`  ❌ ${name}\n     ${e.message}`);
  }
}

// ── (a) 合法持久化内容 ⇒ 登录态还在 ───────────────────────────────────
console.log('\n(a) 合法 lsm.auth ⇒ rehydrate 后登录态保留');
store.clear();
store.setItem(
  'lsm.auth',
  JSON.stringify({
    state: {
      userId: 'u-1',
      token: 'tk-abc',
      expiresAt: Math.floor(Date.now() / 1000) + 3600,
      isAuthenticated: true,
      userType: 1,
      myInviteCode: null,
    },
    version: 0,
  }),
);
const okStore = makeAuthStore();
await okStore.persist.rehydrate();
const okState = okStore.getState();
check('rehydrate 后 token 仍为 "tk-abc"', () => {
  assert.equal(okState.token, 'tk-abc');
});
check('rehydrate 后 isAuthenticated === true', () => {
  assert.equal(okState.isAuthenticated, true);
});
check('rehydrate 后 userId 仍为 "u-1"', () => {
  assert.equal(okState.userId, 'u-1');
});
check('hasHydrated 已置位（不卡 App.tsx 闸门）', () => {
  assert.equal(okState.hasHydrated, true);
});
check('落盘内容未被空状态覆盖（token 未被抹掉）', () => {
  const raw = JSON.parse(store.getItem('lsm.auth'));
  assert.equal(raw.state.token, 'tk-abc');
  assert.equal(raw.state.isAuthenticated, true);
});

// ── (b) 损坏持久化内容 ⇒ 不抛错、不黑屏、坏 key 自愈 ─────────────────
console.log('\n(b) 损坏 lsm.auth（非 JSON）⇒ 不抛错 + hasHydrated 抬闸 + 坏 key 清除');
store.clear();
store.setItem('lsm.auth', '[object Object]');
let brokenThrew = null;
let brokenStore = null;
try {
  brokenStore = makeAuthStore();
  await brokenStore.persist.rehydrate();
} catch (e) {
  brokenThrew = e;
}
check('rehydrate 未抛异常', () => {
  assert.equal(brokenThrew, null, brokenThrew ? String(brokenThrew) : '');
});
check('hasHydrated 已置位（App.tsx 不会卡黑屏）', () => {
  assert.equal(brokenStore.getState().hasHydrated, true);
});
check('store 回到默认未登录态', () => {
  assert.equal(brokenStore.getState().token, null);
  assert.equal(brokenStore.getState().isAuthenticated, false);
});
check('坏 key 已被清除', () => {
  const raw = store.getItem('lsm.auth');
  assert.ok(
    raw === null || raw === undefined || raw === '' || raw.includes('"token":null'),
    `lsm.auth 仍是非法内容: ${raw}`,
  );
});

// ── (c) 源码断言：真实 store 必须走非废弃路径 ─────────────────────────
console.log('\n(c) 源码断言：auth.store.ts 使用显式 storage、未用 deprecated deserialize');
const authStoreSrc = readFileSync(resolve(clientRoot, 'src/store/auth.store.ts'), 'utf8');
check('auth.store.ts 引用 safeLocalStorage', () => {
  assert.match(authStoreSrc, /storage:\s*safeLocalStorage/);
});
check('auth.store.ts 不再使用 deprecated 的 deserialize 选项', () => {
  assert.doesNotMatch(authStoreSrc, /^\s*deserialize:/m);
});
check('auth.store.ts 仍保留 App.tsx 兜底依赖的 onRehydrateStorage', () => {
  assert.match(authStoreSrc, /onRehydrateStorage:/);
});

// ── 汇总 ─────────────────────────────────────────────────────────────
const failed = results.filter((r) => !r.ok);
console.log(`\n结果：${results.length - failed.length}/${results.length} 通过`);
if (failed.length > 0) {
  console.error('失败项：' + failed.map((f) => f.name).join('、'));
  process.exit(1);
}
console.log('全部通过 ✅');
