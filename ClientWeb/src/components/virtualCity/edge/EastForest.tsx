/**
 * 批次 26 · 东缘森林带（x ∈ [+62,+80]，z ∈ [−62,+58]）。
 *
 * 内容（方案文档 §2.1 坐标带契约）：
 *   阔叶树（oak_tree.glb）/ 针叶树（pine_tree.glb）混种 instanced ×~140，
 *   两个 GlbInstanced 集合（draw call = 各自 GLB 子网格数）；
 *   过渡带密度渐变：x ∈ [62,66] 区间仅保留 10% 布点（rnd 决定，确定性）。
 *
 * 林地地表 patch 由 CityEdgeLayer 统一铺设（forest_floor_tile 贴图，缺失降级纯色）。
 * 布点确定性（hashStr + mulberry32）。罗盘契约：东 = +X（方案文档 §1.1）。
 *
 * 批次 27 §4.3：阔叶树按季节切 GLB 变体（seasonAssets：夏 oak_tree / 春秋冬
 * oak_tree_<season>，缺失回退 oak_tree → 程序化 fallback，§27.3 降级链不变）；
 * subscribeSeason 低频触发，GlbInstanced 的 InstancedMesh 几何源随 url 重建。
 *
 * 契约：lag_docs/虚拟城市/已实现/26-坐标系统与城市边缘环境/01-现状分析与方案设计.md §2.4。
 */

import { useEffect, useMemo, useState } from 'react';
import { modelUrl } from '@/assets/models';
import { hashStr, mulberry32 } from '../civic/rand';
import { currentSeason, subscribeSeason } from '../cityTimeStore';
import type { CitySeason } from '../cityTimeStore';
import { seasonOakModelUrl } from '../seasonAssets';
import { sizeTargetFor } from '../cityScale';
import { blenderModelsEnabled } from '@/engine3d';
import { GlbInstanced, type GlbInstanceTRS } from './glbInstanced';
import { ProceduralOaks, ProceduralPines, type FloraSpot } from './proceduralFlora';
import { useObjectInfoProps } from '../objectInfo/useObjectInfoProps';

/** 混种林总棵数（方案 §2.1：×~140；过渡带 10% 密度裁剪后实际略少）。 */
const FOREST_COUNT = 150;

/**
 * 尺寸/落地校验目标（批次 29；与 proceduralFlora 的 fallback 同表值、与 season 同步）：
 * 夏/春/秋 → oakTree（10.1×4.5），冬 → oakTreeWinter（落叶 ⇒ 9.55×4.12，单列一行）。
 */
const OAK_SIZE_TARGETS: Record<CitySeason, ReturnType<typeof sizeTargetFor>> = {
  spring: sizeTargetFor('oakTree', { label: 'nature/oak_tree_spring' }),
  summer: sizeTargetFor('oakTree', { label: 'nature/oak_tree' }),
  autumn: sizeTargetFor('oakTree', { label: 'nature/oak_tree_autumn' }),
  winter: sizeTargetFor('oakTreeWinter', { label: 'nature/oak_tree_winter' }),
};
const PINE_SIZE_TARGET = sizeTargetFor('pineTree', { label: 'nature/pine_tree' });
/** 阔叶 : 针叶 配比阈值（rnd < 0.55 → 阔叶）。 */
const OAK_RATIO = 0.55;
/** 过渡带 x ∈ [62,66] 的保留概率（10% 密度渐变）。 */
const TRANSITION_KEEP = 0.1;

export function EastForest() {
  // 批次 27：季节低频订阅（仅 season 变化 setState）。
  // 批次 44 D3：§27.5 总闸 —— `disable-blender-models=1` 时强制走程序化 fallback。
  // 此前本组件**漏挂**总闸（批次 41 只在 props/ 一族补齐），是降级链破洞。
  const blenderOn = useMemo(() => blenderModelsEnabled(), []);

  const [season, setSeason] = useState<CitySeason>(currentSeason);
  useEffect(() => subscribeSeason(setSeason), []);

  const { oaks, pines } = useMemo(() => {
    const rnd = mulberry32(hashStr('edge26:east:forest'));
    const oaks: FloraSpot[] = [];
    const pines: FloraSpot[] = [];
    let guard = 0;
    // 过渡带裁剪后以重试补足 150 棵（guard 防死循环）
    while (oaks.length + pines.length < FOREST_COUNT && guard < FOREST_COUNT * 4) {
      guard++;
      const x = 62 + rnd() * 18;
      const z = -62 + rnd() * 120;
      const keepRnd = rnd();
      const kindRnd = rnd();
      const scale = 0.9 + rnd() * 0.8;
      const rotY = rnd() * Math.PI * 2;
      // 过渡带（x<66）仅 10% 保留 → 密度由稀到密自然渐变
      if (x < 66 && keepRnd >= TRANSITION_KEEP) continue;
      const spot = { x, z, scale, rotY };
      if (kindRnd < OAK_RATIO) oaks.push(spot);
      else pines.push(spot);
    }
    return { oaks, pines };
  }, []);

  const oakInstances = useMemo<GlbInstanceTRS[]>(
    () => oaks.map((t) => ({ position: [t.x, 0, t.z], rotationY: t.rotY, scale: t.scale })),
    [oaks],
  );
  const pineInstances = useMemo<GlbInstanceTRS[]>(
    () => pines.map((t) => ({ position: [t.x, 0, t.z], rotationY: t.rotY, scale: t.scale })),
    [pines],
  );

  // 批次 28 B2：东部森林信息交互。
  const info = useObjectInfoProps('edge.east-forest', { anchorY: 4 });
  return (
    <group {...info}>
      {/* 阔叶树（oak_tree GLB 实例化，批次 27 按季节切变体；fallback 程序化球冠树，
          批次 29：按 season 同步取表值 —— 冬 oakTreeWinter / 其余 oakTree） */}
      <GlbInstanced
        url={blenderOn ? seasonOakModelUrl(season) : ''}
        instances={oakInstances}
        fallback={<ProceduralOaks spots={oaks} season={season} />}
        sizeTarget={OAK_SIZE_TARGETS[season]}
      />
      {/* 针叶树（pine_tree GLB 实例化；fallback 程序化圆锥树） */}
      <GlbInstanced
        url={blenderOn ? modelUrl('nature', 'pine_tree') : ''}
        instances={pineInstances}
        fallback={<ProceduralPines spots={pines} />}
        sizeTarget={PINE_SIZE_TARGET}
      />
    </group>
  );
}

export default EastForest;
