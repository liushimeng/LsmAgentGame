/**
 * cityShowStore —— 城市显示指令的跨层共享状态（zustand 单例）。
 *
 * 批次 81「系统对话」：City-System Agent 产出结构化显示指令（方案 §3.3），
 * 经 `cityShowAdapter` 转成既有 3D 控制动作。其中 `annotate` 需要一条
 * **DOM 浮动标签**（3D 场景内无对应既有组件），故用单例 store 让
 * Canvas 内外的消费者都能读到 —— 与 `engine3d/controls/freeViewStore.ts`
 * 同一模式（跨 R3F Canvas 边界的最小代价解法）。
 *
 * 写入频率：只有 Agent 显示指令到达时写（低频），无每帧数据，无定时器。
 * 8s 自动消除由渲染端（`<CityAnnotationLabel>`）按 ts 判定，store 不挂定时器
 * （与 virtualCity.store 的 speechBubbles 同口径）。
 */

import { create } from 'zustand';

export interface CityAnnotation {
  text: string;
  district?: string;
  /** Date.now()，过期判定基准。 */
  ts: number;
}

interface CityShowState {
  annotation: CityAnnotation | null;
  setAnnotation: (text: string, district?: string) => void;
  clearAnnotation: () => void;
}

export const useCityShowStore = create<CityShowState>((set) => ({
  annotation: null,
  setAnnotation: (text, district) =>
    set({
      annotation: {
        text,
        ...(district ? { district } : {}),
        ts: Date.now(),
      },
    }),
  clearAnnotation: () => set({ annotation: null }),
}));

/** 供非 React 代码（adapter）直接调用，避免为一次指令触发整树订阅。 */
export const cityShowApi = {
  setAnnotation: (text: string, district?: string) =>
    useCityShowStore.getState().setAnnotation(text, district),
  clearAnnotation: () => useCityShowStore.getState().clearAnnotation(),
};
