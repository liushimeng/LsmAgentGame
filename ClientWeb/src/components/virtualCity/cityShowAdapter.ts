/**
 * cityShowAdapter —— 结构化显示指令 → 既有 3D 控制动作（纯函数 + 副作用适配层）。
 *
 * 批次 81「系统对话」：City-WebGL-Show Agent 只产出「看哪里 / 怎么显示」的
 * 结构化指令（方案 §3.3），渲染与相机控制仍由前端既有引擎执行。本文件是
 * **游戏侧薄适配层**（§2.1 约束 5：`engine3d/` 不 import 游戏私有模块，
 * 反之游戏侧可以依赖引擎），把指令翻译成既有控制面：
 *
 *   focus_district    → 选中区 + 相机聚焦该区中心（与 handleSelectDistrict 同一 ref）
 *   focus_point       → 相机聚焦任意世界坐标
 *   set_mode          → freeViewApi.setMode（orbit ↔ fly）
 *   highlight_district→ 选中态高亮（DistrictBlock 既有 selected 样式）
 *   annotate          → cityShowStore 的 DOM 浮动标签（8s 自动消）
 *   clear             → 清选中 + 清标注
 *
 * 设计为**纯映射 + 注入式依赖**（不 import freeViewStore / 页面 store / 3D 组件），
 * 便于单测直接断言「指令 → 动作序列」（`__tests__/cityShowAdapter.test.ts`）。
 */

import type { MutableRefObject } from 'react';
import { districtCenter, type VirtualCityDistrictId } from '@/types/virtualCity';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

/** 视角模式（与 engine3d/controls/freeViewStore 的 FreeViewMode 同集）。 */
export type CityShowMode = 'orbit' | 'fly';

export interface CityShowDeps {
  /** 与 `VirtualCityGamePage::handleSelectDistrict` 共用的相机聚焦 ref。 */
  focusRef: MutableRefObject<{ x: number; z: number } | null>;
  /** `useVirtualCityStore.setSelectedDistrict`（选区高亮）。 */
  setSelectedDistrict: (id: VirtualCityDistrictId | null) => void;
  /** `freeViewApi.setMode`。 */
  setMode: (mode: CityShowMode) => void;
  /** `cityShowApi.setAnnotation`。 */
  setAnnotation: (text: string, district?: string) => void;
  /** `cityShowApi.clearAnnotation`。 */
  clearAnnotation: () => void;
}

/** 有限数判定（focus_point 参数校验）。 */
function isFiniteNumber(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v);
}

/**
 * 执行一条显示指令。未知 / 缺参指令一律**静默忽略**（不抛错、不产生动作）——
 * 显示指令是旁路增强，绝不能让它把对话框或 3D 场景打崩。
 */
export function applyDisplayCommand(
  cmd: VirtualCityDisplayCommand,
  deps: CityShowDeps,
): void {
  if (!cmd || typeof cmd.type !== 'string') return;

  switch (cmd.type) {
    case 'focus_district': {
      const d = typeof cmd.district === 'string' ? cmd.district : '';
      if (!d) return;
      const id = d as VirtualCityDistrictId;
      deps.setSelectedDistrict(id);
      const c = districtCenter(d);
      deps.focusRef.current = { x: c.x, z: c.z };
      return;
    }

    case 'focus_point': {
      if (!isFiniteNumber(cmd.x) || !isFiniteNumber(cmd.z)) return;
      deps.focusRef.current = { x: cmd.x, z: cmd.z };
      return;
    }

    case 'set_mode': {
      if (cmd.mode !== 'orbit' && cmd.mode !== 'fly') return;
      deps.setMode(cmd.mode);
      return;
    }

    case 'highlight_district': {
      if (typeof cmd.district !== 'string' || !cmd.district) return;
      deps.setSelectedDistrict(cmd.district as VirtualCityDistrictId);
      return;
    }

    case 'annotate': {
      const text = typeof cmd.text === 'string' ? cmd.text : '';
      if (!text) return;
      deps.setAnnotation(text, cmd.district);
      return;
    }

    case 'clear': {
      deps.setSelectedDistrict(null);
      deps.clearAnnotation();
      return;
    }

    default:
      return;
  }
}
