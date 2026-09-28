/**
 * engine3d/controls/freeViewStore — 自由视角的跨层共享状态（zustand）。
 *
 * 批次 32「自由视角系统」新增。存在的理由：视角模式既被 R3F 场景内的
 * `<FreeViewControls>` 消费，也被 Canvas **之外**的 DOM 层 `<FreeViewHud>` 读写。
 * 若用组件 state，两者要跨 Canvas 边界传 props（引擎层就要知道宿主布局），违反
 * §2.1 硬约束 5 的解耦精神；zustand 单例是最小代价的解法（与既有
 * `components/virtualCity/objectInfo/objectInfoStore.ts` 同一模式）。
 *
 * **写入频率纪律**：只有低频量（模式 / 帮助开关 / 档位 / 碰撞态）进 store。
 * 每帧变化的量（相机位置、FOV、速度）一律走 ref，绝不进 store —— 否则
 * `useFreeView(s => s.mode)` 无关的 HUD 也会被 60 fps 拖着重渲染。
 *
 * 引擎层不 import 任何游戏私有模块（§2.1 硬约束 5）：本文件零依赖 game 资产，
 * 文案一律由调用方经 `FreeViewLabels` prop 注入。
 */

import { create } from 'zustand';

/** 三种视角模式。顺序即 `V` 键循环顺序。 */
export type FreeViewMode = 'orbit' | 'fly' | 'walk';

export const FREE_VIEW_MODES: readonly FreeViewMode[] = ['orbit', 'fly', 'walk'];

/** 速度档位倍率（与 `Shift` 加速正交：档位是基线，Shift 在其上再乘）。 */
export const FREE_VIEW_TIERS = [0.25, 1, 4] as const;
export type FreeViewTierIndex = 0 | 1 | 2;

const STORAGE_KEY = 'engine3d.freeView';

interface Persisted {
  mode: FreeViewMode;
  tier: FreeViewTierIndex;
}

function loadPersisted(): Persisted {
  const fallback: Persisted = { mode: 'orbit', tier: 1 };
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw) as Partial<Persisted>;
    return {
      mode: FREE_VIEW_MODES.includes(parsed.mode as FreeViewMode)
        ? (parsed.mode as FreeViewMode)
        : fallback.mode,
      tier: parsed.tier === 0 || parsed.tier === 1 || parsed.tier === 2 ? parsed.tier : fallback.tier,
    };
  } catch {
    return fallback;
  }
}

function persist(next: Persisted): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // 隐私模式等存储失败：仅内存态生效（与 VirtualCityMinimap 既有口径一致）
  }
}

export interface FreeViewState {
  /** 当前视角模式。 */
  mode: FreeViewMode;
  /** 进入当前模式之前的模式（`F` 键在 orbit ↔ fly 间往返用）。 */
  prevMode: FreeViewMode;
  /** 键位帮助面板是否展开。 */
  helpOpen: boolean;
  /** 速度档位下标。 */
  tier: FreeViewTierIndex;
  /** 本帧是否发生碰撞推出（节流上报，供 HUD 指示灯）。 */
  colliding: boolean;
  /** 控制器是否已挂载（未挂载时 HUD 置灰，避免点了没反应）。 */
  ready: boolean;

  setMode: (mode: FreeViewMode) => void;
  cycleMode: () => void;
  toggleMode: (mode: FreeViewMode) => void;
  setHelpOpen: (open: boolean) => void;
  toggleHelp: () => void;
  setTier: (tier: FreeViewTierIndex) => void;
  stepTier: (delta: number) => void;
  setColliding: (hit: boolean) => void;
  setReady: (ready: boolean) => void;
}

const initial = loadPersisted();

export const useFreeView = create<FreeViewState>((set, get) => ({
  mode: initial.mode,
  prevMode: 'orbit',
  helpOpen: false,
  tier: initial.tier,
  colliding: false,
  ready: false,

  setMode: (mode) => {
    const cur = get().mode;
    if (cur === mode) return;
    persist({ mode, tier: get().tier });
    set({ mode, prevMode: cur });
  },
  cycleMode: () => {
    const cur = get().mode;
    const next = FREE_VIEW_MODES[(FREE_VIEW_MODES.indexOf(cur) + 1) % FREE_VIEW_MODES.length];
    get().setMode(next);
  },
  toggleMode: (mode) => {
    const cur = get().mode;
    get().setMode(cur === mode ? get().prevMode : mode);
  },
  setHelpOpen: (helpOpen) => set({ helpOpen }),
  toggleHelp: () => set((s) => ({ helpOpen: !s.helpOpen })),
  setTier: (tier) => {
    if (tier === get().tier) return;
    persist({ mode: get().mode, tier });
    set({ tier });
  },
  stepTier: (delta) => {
    const next = Math.min(2, Math.max(0, get().tier + delta)) as FreeViewTierIndex;
    get().setTier(next);
  },
  setColliding: (colliding) => {
    // 每帧都可能调用，只有状态翻转才写 store（避免无谓的 React 重渲染）。
    if (get().colliding !== colliding) set({ colliding });
  },
  setReady: (ready) => set({ ready }),
}));

/** 供非 React 代码（快捷键挂在 window 上时）直接调用。 */
export const freeViewApi = {
  setMode: (m: FreeViewMode) => useFreeView.getState().setMode(m),
  cycleMode: () => useFreeView.getState().cycleMode(),
  toggleMode: (m: FreeViewMode) => useFreeView.getState().toggleMode(m),
  toggleHelp: () => useFreeView.getState().toggleHelp(),
  stepTier: (d: number) => useFreeView.getState().stepTier(d),
};
