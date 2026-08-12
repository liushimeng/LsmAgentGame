import { create } from 'zustand';
import { persist } from 'zustand/middleware';

// 视口断点 —— 与 docs/架构与协议/产品设计.md 保持一致。
export type Breakpoint = 'mobile' | 'pad' | 'desktop';

export interface UiState {
  // 折叠状态
  headerCollapsed: boolean;
  sidebarCollapsed: boolean;
  chatCollapsed: boolean;
  // 全屏模式（仅游戏页使用）
  fullscreen: boolean;
  // 上次退出全屏时要恢复的折叠状态
  lastCollapsed: {
    headerCollapsed: boolean;
    sidebarCollapsed: boolean;
    chatCollapsed: boolean;
  };
  // 断点
  breakpoint: Breakpoint;
  // 是否已完成首次从 localStorage 的 rehydration
  _hydrated: boolean;
  // 侧边栏分组展开/收起状态 (key=groupId, true=展开)
  sidebarGroupStates: Record<string, boolean>;

  // setters
  setHeaderCollapsed: (v: boolean) => void;
  setSidebarCollapsed: (v: boolean) => void;
  setChatCollapsed: (v: boolean) => void;
  toggleHeader: () => void;
  toggleSidebar: () => void;
  toggleChat: () => void;
  setFullscreen: (v: boolean) => void;
  setBreakpoint: (b: Breakpoint) => void;
  hydrateFromMedia: () => void;
  markHydrated: () => void;
  toggleSidebarGroup: (groupId: string) => void;
}

// 视口断点辅助函数
function detectBreakpoint(): Breakpoint {
  if (typeof window === 'undefined') return 'desktop';
  const w = window.innerWidth;
  if (w <= 640) return 'mobile';
  if (w <= 1024) return 'pad';
  return 'desktop';
}

export const useUiStore = create<UiState>()(
  persist(
    (set, get) => ({
      // 默认值在 hydrateFromMedia 中按断点覆盖,这里给个安全 fallback
      headerCollapsed: false,
      sidebarCollapsed: false,
      chatCollapsed: false,
      fullscreen: false,
      lastCollapsed: { headerCollapsed: false, sidebarCollapsed: false, chatCollapsed: false },
      breakpoint: 'desktop',
      _hydrated: false,
      // 分组默认全部展开
      sidebarGroupStates: {},

      setHeaderCollapsed: (v) => set({ headerCollapsed: v }),
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      setChatCollapsed: (v) => set({ chatCollapsed: v }),
      toggleHeader: () => set((s) => ({ headerCollapsed: !s.headerCollapsed })),
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      toggleChat: () => set((s) => ({ chatCollapsed: !s.chatCollapsed })),

      setFullscreen: (v) => {
        const s = get();
        if (v) {
          // 进入全屏:记住当前状态,然后全部收起
          set({
            fullscreen: true,
            lastCollapsed: {
              headerCollapsed: s.headerCollapsed,
              sidebarCollapsed: s.sidebarCollapsed,
              chatCollapsed: s.chatCollapsed,
            },
            headerCollapsed: true,
            sidebarCollapsed: true,
            chatCollapsed: true,
          });
        } else {
          // 退出全屏:恢复上次状态
          set({ fullscreen: false, ...s.lastCollapsed });
        }
      },

      setBreakpoint: (b) => set({ breakpoint: b }),

      hydrateFromMedia: () => {
        const bp = detectBreakpoint();
        const s = get();
        set({ breakpoint: bp });
        // 只有未从 localStorage rehydrate 过时,才按断点设置默认值。
        // 一旦持久化值恢复,用户通过按钮/页面切换所做的折叠选择必须保持不变,
        // 避免每次挂载或 resize 把持久化状态覆盖掉。
        if (s._hydrated) return;
        if (bp === 'mobile') {
          set({ chatCollapsed: true, sidebarCollapsed: true });
        } else if (bp === 'pad') {
          set({ chatCollapsed: true });
        }
      },

      markHydrated: () => set({ _hydrated: true }),
      toggleSidebarGroup: (groupId: string) =>
        set((s) => ({
          sidebarGroupStates: {
            ...s.sidebarGroupStates,
            [groupId]: s.sidebarGroupStates[groupId] === false ? true : false,
          },
        })),
    }),
    {
      name: 'lsm.ui',
      partialize: (s) => ({
        headerCollapsed: s.headerCollapsed,
        sidebarCollapsed: s.sidebarCollapsed,
        chatCollapsed: s.chatCollapsed,
        fullscreen: s.fullscreen,
        lastCollapsed: s.lastCollapsed,
        _hydrated: s._hydrated,
        sidebarGroupStates: s.sidebarGroupStates,
      }),
      onRehydrateStorage: () => (state) => {
        if (state) state.markHydrated();
      },
    },
  ),
);
