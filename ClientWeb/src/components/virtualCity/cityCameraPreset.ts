/**
 * cityCameraPreset —— 显示指令 `camera_preset` 的相机预设（游戏侧薄适配层）。
 *
 * 批次 84：`camera_preset` 带 `preset` ∈ bird / eye / street / top / free。
 *
 * 落点：既有的 `<FreeViewControls>` 只有 orbit（俯瞰）模式具备「聚焦点 +
 * 视距 + 俯仰 + 方位」四元组语义，故预设统一落到 **orbit 位姿**：
 *
 *   - bird  鸟瞰：高空斜俯（φ≈0.42），全城尺度；
 *   - top   正俯视：φ≈0.06（近乎垂直向下），看布局；
 *   - eye   平视：φ≈1.42（近水平），小视距 —— 相当于「站在目标点平视」；
 *   - street 街面：φ≈1.52（近贴地），极小视距 —— 街道尺度；
 *   - free  自由：回到默认俯瞰位姿（φ≈0.85 / 视距 42）。
 *
 * 引擎侧只需一条「一次性轨道位姿请求」通道（`freeViewApi.setOrbitGoal`，
 * 批次 84 新增，见 engine3d/controls/freeViewStore.ts）—— 引擎不感知
 * 「预设」这一游戏概念（§2.1 约束 5）。
 */

import { freeViewApi } from '@/engine3d/controls/freeViewStore';
import { districtCenter, type VirtualCityDistrictId } from '@/types/virtualCity';
import { useVirtualCityStore } from '@/store/virtualCity.store';

/** 相机预设名（与后端 `preset` 字段逐字一致）。 */
export type CityCameraPreset = 'bird' | 'eye' | 'street' | 'top' | 'free';

/** 预设 → 轨道位姿参数（φ 弧度 / 视距世界单位）。 */
const PRESET_PARAMS: Record<CityCameraPreset, { phi: number; radius: number }> = {
  bird: { phi: 0.42, radius: 110 },
  top: { phi: 0.06, radius: 95 },
  eye: { phi: 1.42, radius: 14 },
  street: { phi: 1.52, radius: 8 },
  free: { phi: 0.85, radius: 42 },
};

/** 预设是否合法。 */
export function isCameraPreset(v: unknown): v is CityCameraPreset {
  return typeof v === 'string' && Object.prototype.hasOwnProperty.call(PRESET_PARAMS, v);
}

/**
 * 应用相机预设。
 *
 * 聚焦点：当前选中的城区中心；未选中则用城市中心 [0,0]。
 * 返回是否命中合法预设（非法值由调用方记「未支持」提示）。
 */
export function applyCameraPreset(
  preset: string,
  target?: { x: number; z: number },
): boolean {
  if (!isCameraPreset(preset)) return false;
  const p = PRESET_PARAMS[preset];
  let focus = target;
  if (!focus) {
    const sel = useVirtualCityStore.getState().selectedDistrict;
    focus = sel
      ? districtCenter(sel as VirtualCityDistrictId)
      : { x: 0, z: 0 };
  }
  // 预设只在 orbit 模式有语义 ⇒ 先切模式（模式切换走 600ms 位姿过渡，不瞬跳）。
  freeViewApi.setMode('orbit');
  freeViewApi.setOrbitGoal({
    target: { x: focus.x, z: focus.z },
    radius: p.radius,
    phi: p.phi,
  });
  return true;
}

/** 供非 React 代码（adapter）直接调用。 */
export const cityCameraApi = {
  applyPreset: applyCameraPreset,
};
