/**
 * Civic · 合成材质共享 hook —— 砖 / 混凝土 / 压型钢板 / 瓦 / 树冠 一处接线。
 * 仅 re-export `useSynthPBR`（textureCache 内置）+ normalScale 默认值。
 */
import { useSynthPBR, type SharedPBR, type SharedPBROpts } from '../cityPbr';

export type CivicPBRKind = 'concrete' | 'brick' | 'metal_deck' | 'tile_roof' | 'foliage';

export function useCivicPBR(kind: CivicPBRKind, normalScale: [number, number] = [1, 1]): SharedPBR {
  const opts: SharedPBROpts = { normalScale };
  return useSynthPBR(kind as any, opts);
}