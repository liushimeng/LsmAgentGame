/**
 * 批次 26 · 城市四缘环境带总装层（CityEdgeLayer）。
 *
 * 职责（方案文档 §2.4）：
 *   - 总装四缘带组件：NorthMountains（北·雪山）/ WestDesert（西·沙漠）/
 *     EastForest（东·森林）/ SouthOcean（南·海洋+南港）；
 *   - 铺设三缘地表 patch（南缘沙滩 patch 在 SouthOcean 内，与海面同层管理）：
 *       北 rock_snow_tile（z ∈ [−80,−62]，x ∈ [−90,+90]）
 *       西 sand_tile      （x ∈ [−80,−62]，z ∈ [−62,+58]）
 *       东 forest_floor_tile（x ∈ [+62,+80]，z ∈ [−62,+58]）
 *     贴图缺失 → 纯色降级（照 VirtualCityCityMap::Ground 既有模式）；
 *   - 纯渲染层，无 props；由 VirtualCityCityMap 在 AtmosphereLayer 之后注入一次。
 *
 * 罗盘契约（方案 §1.1）：北 = −Z / 南 = +Z / 东 = +X / 西 = −X。
 * 过渡带（60 < max(|x|,|z|) ≤ 62）留白，地面 urban_base 自然延伸。
 *
 * 契约：lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md。
 */

import { useSharedTexture } from '@/engine3d';
import { groundTileUrl } from '@/assets/images/virtualCity';
import { NorthMountains } from './NorthMountains';
import { WestDesert } from './WestDesert';
import { EastForest } from './EastForest';
import { SouthOcean } from './SouthOcean';

// ── 三缘地表 patch 几何（方案 §2.1 坐标带契约；y=0.02 略高于地面防 z-fight）──
/** 北·岩雪 patch：x ∈ [−90,+90]（180），z ∈ [−80,−62]（18）。 */
const NORTH_PATCH = { cx: 0, cz: -71, w: 180, d: 18, fallback: '#7d848d' };
/** 西·沙地 patch：x ∈ [−80,−62]（18），z ∈ [−62,+58]（120）。 */
const WEST_PATCH = { cx: -71, cz: -2, w: 18, d: 120, fallback: '#d9b36c' };
/** 东·林地 patch：x ∈ [+62,+80]（18），z ∈ [−62,+58]（120）。 */
const EAST_PATCH = { cx: 71, cz: -2, w: 18, d: 120, fallback: '#3d5a34' };

/** 地表 patch：贴图 repeat 按 8 单位/砖（与 GROUND_TILE 同标尺）；缺失降级纯色。 */
function EdgePatch({
  tile,
  patch,
}: {
  tile: 'rock_snow_tile' | 'sand_tile' | 'forest_floor_tile';
  patch: { cx: number; cz: number; w: number; d: number; fallback: string };
}) {
  const tex = useSharedTexture(groundTileUrl(tile), {
    wrap: 'repeat',
    repeat: [Math.max(1, Math.round(patch.w / 8)), Math.max(1, Math.round(patch.d / 8))],
  });
  return (
    <mesh
      rotation={[-Math.PI / 2, 0, 0]}
      position={[patch.cx, 0.02, patch.cz]}
      receiveShadow
    >
      <planeGeometry args={[patch.w, patch.d]} />
      <meshStandardMaterial
        map={tex ?? undefined}
        color={tex ? '#ffffff' : patch.fallback}
        roughness={0.95}
      />
    </mesh>
  );
}

export function CityEdgeLayer() {
  return (
    <group>
      {/* 三缘地表 patch（南缘沙滩/海面在 SouthOcean 内） */}
      <EdgePatch tile="rock_snow_tile" patch={NORTH_PATCH} />
      <EdgePatch tile="sand_tile" patch={WEST_PATCH} />
      <EdgePatch tile="forest_floor_tile" patch={EAST_PATCH} />
      {/* 四缘环境带 */}
      <NorthMountains />
      <WestDesert />
      <EastForest />
      <SouthOcean />
    </group>
  );
}

export default CityEdgeLayer;
