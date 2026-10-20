/**
 * UrbanFillLayer — 批次 61「全城连续正交方格路网」：**空街区单元的城市功能用地**。
 *
 * ## 为什么需要这一层
 * 批次 61 把 32 个城区全部落到 7×7 = 49 个街区单元上 ⇒ **17 个单元没有城区**
 * （占 35%）。若留空，方格化后顶视图会出现**大片空地**（用户明确要求「不要大面积的
 * 显示空间留白」），而且方格网的规整感会被空缺打破。
 *
 * ## 处置
 * 17 个空单元分成两类（见 `cityGrid.emptyCells()` 派生结果）：
 *   - **10 个**：安置**市政设施**（加油站 / 变电站 / 通信塔 / 水塔 / 消防站 /
 *     市政厅 / 停机坪 / 施工工地 / 停车场 / 派出所）—— 这些设施原先散落在
 *     硬编码坐标上，批次 61 全部迁到空单元内（见 `civic/*.tsx` 的 position）。
 *   - **7 个**：本层渲染**口袋公园 / 绿地广场**（草地铺装 + 园林树 + 广场小品）。
 *
 * ## 单一事实来源
 * 空单元集合由 `cityGrid.emptyCells()`（城区表反推）派生；市政设施占用的单元
 * 由 `CIVIC_CELLS` 登记，两集合互补 ⇒ 17 = 10 + 7，**不重不漏**。
 *
 * 契约：lag_docs/虚拟城市/已实现/61-全城连续正交方格路网/01-方案设计.md §2.2/§6。
 */

import { useMemo } from 'react';
import { groundTileUrl, pbrNormalUrl, pbrRoughUrl } from '@/assets/images/virtualCity';
import { useSharedPBR, useSharedTexture, withPBR } from '@/engine3d';
import { DISTRICT_SURFACE_Y } from './cityScale';
import { emptyCells } from './cityGrid';
import { TreesInstanced, type InstanceTree } from './props/TreesInstanced';
import { currentSeason, subscribeSeason } from './cityTimeStore';
import type { CitySeason } from './cityTimeStore';
import { seasonGrassTileUrl } from './seasonAssets';
import { useEffect, useState } from 'react';

/**
 * 市政设施占用的空单元（10 个）——与 `civic/*.tsx` 的 position 一一对应。
 * ⚠ 改这里必须同步改对应组件的 position（§130 防漂移）。
 */
const CIVIC_CELLS: ReadonlySet<string> = new Set([
  '-24,-12', // GasStation 加油站
  '-24,0',   // Substation 变电站
  '24,0',    // CommTower 通信塔
  '36,-12',  // WaterTower 水塔
  '0,-36',   // FireStation 消防站
  '0,36',    // CityHall 市政厅（中轴南端）
  '24,-36',  // HeliPad 停机坪
  '-24,36',  // ConstructionSite 施工工地
  '12,-12',  // ParkingLot 停车场
  '-36,12',  // PoliceStation 派出所（紧邻物流港）
]);

/**
 * 地标占用的空单元（1 个）——体育场迁出体育新城街区，落在这里。
 * ⚠ 与 `cityLandmarks.SPORTS_FIELD_AREA` + `civic/SportsField.tsx` 的 position 同源。
 */
const LANDMARK_CELLS: ReadonlySet<string> = new Set([
  '24,36',   // SportsField 体育场
]);

/** 本层渲染的绿地单元（其余空单元归市政设施 / 地标）。 */
const GREEN_CELLS: ReadonlyArray<readonly [number, number]> = [
  [-12, -24],
  [0, 24],
  [12, -24],
  [24, -24],
  [36, 36],
  [-24, 24],
];

/** 每个绿地单元的树数（口径：城市口袋公园疏林，5~7 株）。 */
const TREES_PER_CELL = 6;

/** 确定性 PRNG（禁 Math.random —— 截图验收须可复现）。 */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** 坐标 → 稳定种子（同一坐标恒得同一种子）。 */
function seedFor(x: number, z: number): number {
  let h = Math.imul(Math.round(x * 100) ^ 0x9e3779b9, 2654435761) >>> 0;
  h = Math.imul(h ^ Math.round(z * 100), 2246822519) >>> 0;
  return (h ^ (h >>> 13)) >>> 0;
}

export function UrbanFillLayer() {
  // 批次 27：草地贴图随季节（与 DistrictBlock 的公园草地同源）。
  const [season, setSeason] = useState<CitySeason>(currentSeason);
  useEffect(() => subscribeSeason(setSeason), []);
  const grassTileUrl = seasonGrassTileUrl(season);
  const grassTex = useSharedTexture(grassTileUrl, {
    wrap: 'repeat',
    repeat: [3, 3],
  });
  const grassPbr = useSharedPBR(
    grassTileUrl,
    pbrNormalUrl('ground', 'grass_tile'),
    pbrRoughUrl('ground', 'grass_tile'),
    { wrap: 'repeat', repeat: [3, 3], normalScale: [0.7, 0.7] },
  );
  const plazaTex = useSharedTexture(groundTileUrl('plaza_tile'), {
    wrap: 'repeat',
    repeat: [2, 2],
  });
  const plazaPbr = useSharedPBR(
    groundTileUrl('plaza_tile'),
    pbrNormalUrl('ground', 'plaza_tile'),
    pbrRoughUrl('ground', 'plaza_tile'),
    { wrap: 'repeat', repeat: [2, 2], normalScale: [0.8, 0.8] },
  );

  /**
   * 自检：绿地单元 + 市政单元必须**恰好覆盖**全部空单元（不重不漏）。
   * 开发期告警（不抛错，避免一条不一致就黑屏）。
   */
  useEffect(() => {
    if (!import.meta.env.DEV) return;
    const empties = emptyCells();
    const covered = new Set<string>([
      ...CIVIC_CELLS,
      ...LANDMARK_CELLS,
      ...GREEN_CELLS.map(([x, z]) => `${x},${z}`),
    ]);
    const missing = empties.filter(([x, z]) => !covered.has(`${x},${z}`));
    const extra = [...covered].filter((k) => !empties.some(([x, z]) => `${x},${z}` === k));
    if (missing.length || extra.length) {
      console.warn(
        `[urbanFill] 空单元覆盖不一致：缺 ${missing.length} 个、多 ${extra.length} 个`,
        { missing, extra },
      );
    }
  }, []);

  /** 园林树点位：单元内 5~7 株确定性散布（避开中央十字小径）。 */
  const trees = useMemo<InstanceTree[]>(() => {
    const out: InstanceTree[] = [];
    for (const [cx, cz] of GREEN_CELLS) {
      const rnd = mulberry32(seedFor(cx, cz));
      for (let i = 0; i < TREES_PER_CELL; i++) {
        // 在 ±3.2 的方形内散布；跳过中央 |x|<0.7 或 |z|<0.7 的小径带
        let px = 0;
        let pz = 0;
        for (let tries = 0; tries < 6; tries++) {
          px = (rnd() * 2 - 1) * 3.2;
          pz = (rnd() * 2 - 1) * 3.2;
          if (Math.abs(px) > 0.8 && Math.abs(pz) > 0.8) break;
        }
        out.push({
          x: cx + px,
          z: cz + pz,
          scale: 0.9 + rnd() * 0.35,
          kind: 'park',
          variant: 'oak',
        });
      }
    }
    return out;
  }, []);

  return (
    <group>
      {GREEN_CELLS.map(([cx, cz]) => (
        <group key={`fill-${cx}-${cz}`} position={[cx, 0, cz]}>
          {/* 草地铺装（7.8×7.8，与 DistrictBlock 公园草地同口径，略高于底板） */}
          <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, DISTRICT_SURFACE_Y + 0.016, 0]} receiveShadow>
            <planeGeometry args={[7.8, 7.8]} />
            <meshStandardMaterial
              {...withPBR(
                { map: grassTex ?? undefined, color: grassTex ? '#ffffff' : '#3f7a3a', roughness: 0.95 },
                grassPbr,
              )}
            />
          </mesh>
          {/* 中央广场铺装（4×4），给绿地一个「有人气」的硬质核心 */}
          <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, DISTRICT_SURFACE_Y + 0.017, 0]} receiveShadow>
            <planeGeometry args={[4, 4]} />
            <meshStandardMaterial
              {...withPBR(
                { map: plazaTex ?? undefined, color: plazaTex ? '#ffffff' : '#9aa1ab', roughness: 0.85 },
                plazaPbr,
              )}
            />
          </mesh>
        </group>
      ))}
      {/* 园林树（1 draw call/段；批次 44 GLB 实例化） */}
      <TreesInstanced trees={trees} totalCap={128} />
    </group>
  );
}

export default UrbanFillLayer;
