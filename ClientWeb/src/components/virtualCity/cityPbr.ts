/**
 * cityPbr — 虚拟城市私有的 synth 合成材质 PBR 薄适配层。
 *
 * 22-3D世界升级与引擎模块化：通用 PBR 拼装（useSharedPBR/withPBR/useSharedTexture）
 * 已迁入 `@/engine3d`；本文件只保留**游戏私有**的「stem → 资产 URL」拼接
 * （SynthStem 与 3d_script/procedural_pbr_maps.py::synth_jobs 对齐，
 * 依赖 `@/assets/images/virtualCity`，属游戏资产层，不进引擎）。
 *
 * 契约沿用 18-3D城市PBR材质与真实城市冲刺 · 02 架构设计 §2.3：
 *   - 颜色图为空 → 材质保持纯色 + 硬编码属性，仅由法线/粗糙度贴图贡献凹凸与光泽差。
 *   - 「stem 拼接只允许在 BuildingMesh.tsx」的约束语义不变（经 useSynthPBR 间接取用）。
 */

import { useSharedPBR } from '@/engine3d';
import type { SharedPBR, SharedPBROpts } from '@/engine3d';
import { pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';

// 兼容旧 import 路径的再导出（调用方逐步迁移到 '@/engine3d'，此处的类型出口保持不变）
export type { SharedPBR, SharedPBROpts };
export { useSharedPBR, withPBR, useSharedTexture } from '@/engine3d';

/** synth 合成材质固定 stem（与 3d_script/procedural_pbr_maps.py::synth_jobs 对齐）。 */
export type SynthStem =
  | 'water'
  | 'concrete'
  | 'brick'
  | 'metal_deck'
  | 'tile_roof'
  | 'asphalt_wear'
  | 'foliage';

/** synth 合成材质 PBR：颜色图恒为空，仅法线/粗糙度贡献质感（契约见文件头）。 */
export function useSynthPBR(name: SynthStem, opts?: SharedPBROpts): SharedPBR {
  return useSharedPBR('', pbrNormalUrl('synth', name), pbrRoughUrl('synth', name), opts);
}
