/**
 * objectInfoStore — 物件 hover/selected 状态（批次 28 · 工作线 B1）。
 *
 * 全场景单例悬浮卡 / 详情卡（ObjectInfoOverlay）订阅本 store；接线组件只写不读，
 * 因此 hover/点击不会牵动任何场景组件重渲染。
 */

import { create } from 'zustand';
import type { ObjectInfoExtra } from './catalog';

/** 一次命中（hover / click）携带的展示目标。 */
export interface ObjectInfoTarget {
  /** catalog id（如 `building.tower` / `district.finance`）。 */
  id: string;
  /** 世界坐标锚点（命中点 + anchorY；overlay 按此定位）。 */
  pos: [number, number, number];
  /** 动态附加行（楼层 / 所属城区 / #实例 …），由接线组件现场注入。 */
  extra?: ObjectInfoExtra[];
}

export interface ObjectInfoState {
  /** 悬停目标（null = 无悬浮卡）。 */
  hovered: ObjectInfoTarget | null;
  /** 点选目标（null = 无详情卡）。 */
  selected: ObjectInfoTarget | null;
  setHovered: (t: ObjectInfoTarget | null) => void;
  setSelected: (t: ObjectInfoTarget | null) => void;
  clearSelected: () => void;
}

export const useObjectInfoStore = create<ObjectInfoState>()((set) => ({
  hovered: null,
  selected: null,
  setHovered: (t) => set({ hovered: t }),
  setSelected: (t) => set({ selected: t }),
  clearSelected: () => set({ selected: null }),
}));
