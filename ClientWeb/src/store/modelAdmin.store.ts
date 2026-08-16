// modelAdmin.store.ts — Zustand store for the admin LLM model management page.
//
// Holds the cached `providers[]` list, loading flag, and a single toast-style
// error string. All mutations are forwarded to `api/modelAdmin.ts`; on success
// they re-load the list so optimistic updates don't drift from the server's
// authoritative state.
//
// §13 frontend-dev: only modifies ClientWeb/.

import { create } from 'zustand';
import { ApiError } from '@/services/http';
import { reportGlobalError } from '@/services/globalError';
import * as api from '@/api/modelAdmin';
import type {
  LlmProvider,
  LlmProviderCreate,
  ProviderTestResult,
} from '@/types/model';

interface ModelAdminState {
  providers: LlmProvider[];
  loading: boolean;
  /** Last error string (raw, English) — page renders & clears it. */
  error: string | null;
  /** Transient toast for one-shot operation results. */
  toast: string | null;
  toastKind: 'success' | 'error' | null;
  /**
   * 最近一次 CRUD 操作的错误消息。页面在弹窗内读取并显示(弹窗内联错误,
   * §7.1 最高层级显示),操作成功 / 弹窗打开时清空。null 表示无错误。
   */
  lastError: string | null;
  /** 上一次「测试」按钮的完整返回,供弹窗展示模型回复。null 表示无。 */
  lastTestResult: ProviderTestResult | null;
  /** 与 lastTestResult 配对的 provider id,用于标题显示。 */
  lastTestProviderId: string | null;
  /** §133 — 全局默认 endpoint(DB 行为空时回退到这里),页面/编辑弹窗展示用 */
  defaultEndpoint: string | null;
  /** §20260816-03 — 是否显示已停用(软删除)的模型行 */
  showDisabled: boolean;
  /** §20260816-03 — 库中已停用的行数,用于"另有 N 个已停用"提示 */
  disabledCount: number;

  loadProviders: () => Promise<void>;
  /** §20260816-03 — 切换"显示已停用",并立即按新口径重新拉取列表 */
  setShowDisabled: (v: boolean) => Promise<void>;
  createProvider: (body: LlmProviderCreate) => Promise<boolean>;
  updateProvider: (id: string, body: Partial<LlmProviderCreate>) => Promise<boolean>;
  deleteProvider: (id: string, hard?: boolean) => Promise<boolean>;
  testProvider: (id: string) => Promise<{ ok: boolean; message: string }>;
  reloadProviders: () => Promise<boolean>;
  clearToast: () => void;
  clearError: () => void;
  clearLastError: () => void;
  clearTestResult: () => void;
}

const handleErr = (e: unknown, fallback: string): string => {
  if (e instanceof ApiError) return e.message || fallback;
  if (e instanceof Error) return e.message;
  return fallback;
};

export const useModelAdminStore = create<ModelAdminState>((set, get) => ({
  providers: [],
  loading: false,
  error: null,
  toast: null,
  toastKind: null,
  lastError: null,
  lastTestResult: null,
  lastTestProviderId: null,
  defaultEndpoint: null,
  showDisabled: false,
  disabledCount: 0,

  loadProviders: async () => {
    set({ loading: true, error: null });
    try {
      // §20260816-03 — 按当前"显示已停用"口径拉取。后端默认只返回 enabled=true。
      const resp = await api.listProvidersResponse(get().showDisabled);
      set({
        providers: resp.providers,
        defaultEndpoint: resp.default_endpoint ?? null,
        disabledCount: resp.disabled_count ?? 0,
        loading: false,
      });
    } catch (e) {
      set({ loading: false, error: handleErr(e, '加载模型失败') });
    }
  },

  setShowDisabled: async (v) => {
    set({ showDisabled: v });
    await get().loadProviders();
  },

  createProvider: async (body) => {
    try {
      const created = await api.createProvider(body);
      set((s) => ({ providers: [...s.providers, created], toast: '已新增', toastKind: 'success' }));
      return true;
    } catch (e) {
      // §7.1 — API 失败必须在最高层级显示。弹窗内提交时页面读取 lastError
      // 显示为弹窗内联错误;同时 reportGlobalError 兜底,确保非弹窗路径也不丢错误。
      const msg = handleErr(e, '新增失败');
      set({ toast: msg, toastKind: 'error', lastError: msg });
      reportGlobalError({ message: msg, severity: 'error' });
      return false;
    }
  },

  updateProvider: async (id, body) => {
    try {
      const updated = await api.updateProvider(id, body);
      set((s) => ({
        providers: s.providers.map((p) => (p.id === id ? updated : p)),
        toast: '已保存',
        toastKind: 'success',
        lastError: null,
      }));
      return true;
    } catch (e) {
      const msg = handleErr(e, '保存失败');
      set({ toast: msg, toastKind: 'error', lastError: msg });
      reportGlobalError({ message: msg, severity: 'error' });
      return false;
    }
  },

  // §20260816-03 —— 删除后**必须重新拉取列表**,不再本地 filter。
  //
  // 历史缺陷:旧实现 `providers.filter(p => p.id !== id)` + 弹「已删除」,而后端
  // 只是软删除(enabled=false),列表接口又不过滤,于是刷新/重启后行原样重现,
  // 用户观感是「删不掉」。乐观移除把后端的**部分成功**渲染成了完全成功。
  // 删除类操作必须以服务端为准重新对账。
  deleteProvider: async (id, hard = false) => {
    try {
      const res = await api.deleteProvider(id, hard);
      const toast = res.hard ? '已彻底删除' : '已停用(软删除)';
      set({ toast, toastKind: 'success', lastError: null });
      await get().loadProviders();
      return true;
    } catch (e) {
      const msg = handleErr(e, '删除失败');
      set({ toast: msg, toastKind: 'error', lastError: msg });
      reportGlobalError({ message: msg, severity: 'error' });
      return false;
    }
  },

  testProvider: async (id) => {
    try {
      const r = await api.testProvider(id);
      const chatOK = !!r.chat_ok;
      set({
        toast: r.ok ? '测试成功' : (r.message || '测试失败'),
        toastKind: r.ok ? 'success' : 'error',
        lastTestResult: r,
        lastTestProviderId: id,
      });
      if (!r.ok) {
        reportGlobalError({ message: r.message || '测试失败', severity: 'error' });
      }
      return { ok: r.ok, message: chatOK ? 'chat_ok' : (r.chat_error || r.message) };
    } catch (e) {
      const msg = handleErr(e, '测试失败');
      set({ toast: msg, toastKind: 'error' });
      reportGlobalError({ message: msg, severity: 'error' });
      return { ok: false, message: msg };
    }
  },

  reloadProviders: async () => {
    set({ loading: true, error: null });
    try {
      await api.reloadProviders();
      const providers = await api.listProviders();
      set({ providers, loading: false, toast: '已重新加载', toastKind: 'success' });
      return true;
    } catch (e) {
      const msg = handleErr(e, '重新加载失败');
      set({ loading: false, error: msg });
      reportGlobalError({ message: msg, severity: 'error' });
      return false;
    }
  },

  clearToast: () => set({ toast: null, toastKind: null }),
  clearError: () => set({ error: null }),
  clearLastError: () => set({ lastError: null }),
  clearTestResult: () => set({ lastTestResult: null, lastTestProviderId: null }),
}));
