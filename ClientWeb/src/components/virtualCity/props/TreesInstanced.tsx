/**
 * TreesInstanced — 批次 20 渲染性能专项（文档 1 §3.3）：
 *
 * 行道树 + 区内树合并为全局 InstancedMesh —— TreeV3 三部件（主干 / 分枝 / 3 球冠）
 * 各一个 drei <Instances>（同 geometry 同 material，逐实例矩阵 + 冠色），
 * 全城 ~500 棵 ≈ 3 draw call。
 *
 * 与 TreeV3Fallback 形态同源：treeSeed()/treeShape()/TRUNK_COLOR/CROWN_COLORS
 * 复用导出版，逐树确定性形态与改造前一致；oak_tree.glb 逐实例 clone 路径由
 * 实例化程序几何替代（车辆 / 行人 / 建筑不受影响，仍走各自链路）。
 * 数量超过 TREE_TOTAL_CAP 时按 hash 种子稳定截断（与随机无关、可复现）。
 *
 * 批次 27 §4.3：冠材质基色按季节 tint（春 #bde3c0 / 夏 #ffffff / 秋 #e8c99a /
 * 冬 #cfd8dc），cityTimeStore.subscribeSeason 低频订阅（仅季节变化才重渲染）。
 */

import { memo, useEffect, useMemo, useState } from 'react';
import { Instances, Instance } from '@react-three/drei';
import { useThree } from '@react-three/fiber';
import { detectQualityTier } from '@/engine3d';
import { useSynthPBR } from '../cityPbr';
import { u } from '../cityScale';
import { currentSeason, subscribeSeason } from '../cityTimeStore';
import type { CitySeason } from '../cityTimeStore';
import {
  TRUNK_COLOR,
  CROWN_COLORS,
  treeSeed,
  treeShape,
} from './TreeV3';
import { useObjectInfoProps, instancedEventsRaycast } from '../objectInfo/useObjectInfoProps';

export interface InstanceTree {
  x: number;
  z: number;
  scale: number;
  /** 批次 28 B2：行道树 / 区内园林树（catalog tree.road / tree.park 按实例区分）。 */
  kind?: 'road' | 'park';
}

/** 主干几何参数（= TreeV3Fallback 主干）。 */
const TRUNK_GEOM: [number, number, number, number] = [u(0.09), u(0.13), u(1.6), 8];
const TRUNK_Y = u(0.8);
/** 分枝几何参数（= TreeV3Fallback 分枝）。 */
const BRANCH_GEOM: [number, number, number, number] = [u(0.04), u(0.06), u(0.6), 6];
const BRANCH_Y = u(1.4);
const BRANCH_R = u(0.3);

interface InstTrunk { key: string; ti: number; x: number; y: number; z: number; s: number }
interface InstBranch extends InstTrunk { rotY: number }
interface InstCrown extends InstTrunk { r: number; color: string }

/** hash 种子稳定截断：超过 cap 时按 hashStr(x:z) 升序保留前 cap 棵（确定性可复现）。 */
function stableTruncate(trees: InstanceTree[], cap: number): InstanceTree[] {
  if (trees.length <= cap) return trees;
  const scored = trees.map((t, i) => ({
    t,
    i,
    h: treeSeed(t.x, t.z),
  }));
  scored.sort((a, b) => a.h - b.h);
  const kept = scored.slice(0, cap).sort((a, b) => a.i - b.i);
  return kept.map((k) => k.t);
}

/** 批次 27 §4.3 季节树冠 tint（与逐实例 color 相乘；夏 #ffffff = 旧版观感）。 */
const SEASON_CROWN_TINT: Record<CitySeason, string> = {
  spring: '#bde3c0',
  summer: '#ffffff',
  autumn: '#e8c99a',
  winter: '#cfd8dc',
};

// 批次 28 A1：memo —— trees（useMemo 布局）/ totalCap（常量）稳定，父层不重渲染。
export const TreesInstanced = memo(function TreesInstanced({
  trees,
  totalCap,
}: {
  trees: InstanceTree[];
  /** 全城树总量上限（超出按 hash 种子稳定截断）。 */
  totalCap: number;
}) {
  const effective = useMemo(() => stableTruncate(trees, totalCap), [trees, totalCap]);

  // 批次 28 A3 caster 裁剪：树冠/树枝不投影（保留树干投影）；low 档树全不投影。
  // 游戏侧策略映射（engine3d 不含 pedestrianCap/treeShadows 类游戏字段，§2.1 硬约束 5）。
  const gl = useThree((s) => s.gl);
  const tier = useMemo(() => detectQualityTier(gl), [gl]);
  const trunkShadow = tier === 'high';

  const { trunks, branches, crowns } = useMemo(() => {
    const tr: InstTrunk[] = [];
    const br: InstBranch[] = [];
    const cr: InstCrown[] = [];
    effective.forEach((t, ti) => {
      const s = t.scale;
      const seed = treeSeed(t.x, t.z);
      const { branchCount, crowns: cs } = treeShape(seed);
      tr.push({ key: `trunk-${ti}`, ti, x: t.x, y: TRUNK_Y * s, z: t.z, s });
      for (let i = 0; i < branchCount; i++) {
        const a = (i / Math.max(1, branchCount)) * Math.PI * 2 + seed * 0.0001;
        br.push({
          key: `branch-${ti}-${i}`,
          ti,
          x: t.x + Math.cos(a) * BRANCH_R * s,
          y: BRANCH_Y * s,
          z: t.z + Math.sin(a) * BRANCH_R * s,
          s,
          rotY: a,
        });
      }
      cs.forEach((c, ci) => {
        cr.push({
          key: `crown-${ti}-${ci}`,
          ti,
          x: t.x + c.x * s,
          y: c.y * s,
          z: t.z + c.z * s,
          s,
          r: c.r * s,
          color: CROWN_COLORS[c.color],
        });
      });
    });
    return { trunks: tr, branches: br, crowns: cr };
  }, [effective]);

  // 批次 28 B2：树信息交互。三个实例层各自映射回树序 ti → tree.road / tree.park
  // （行道树 / 区内园林树），instanceId 自动进 `#实例` 动态行。
  const idForTree = (ti: number | undefined): string =>
    (ti !== undefined && effective[ti]?.kind === 'park') ? 'tree.park' : 'tree.road';
  const trunkInfo = useObjectInfoProps('tree.road', {
    anchorY: 1.6,
    idFor: (e) => idForTree(trunks[e.instanceId ?? -1]?.ti),
  });
  const branchInfo = useObjectInfoProps('tree.road', {
    anchorY: 1.6,
    idFor: (e) => idForTree(branches[e.instanceId ?? -1]?.ti),
  });
  const crownInfo = useObjectInfoProps('tree.road', {
    anchorY: 1.6,
    idFor: (e) => idForTree(crowns[e.instanceId ?? -1]?.ti),
  });

  // 批次 27：季节低频订阅（仅 season 变化 setState；无变化时 React 直接 bail out）。
  const [season, setSeason] = useState<CitySeason>(currentSeason);
  useEffect(() => subscribeSeason(setSeason), []);

  // 树冠材质与 TreeV3Fallback 同源（foliage 法线/粗糙 PBR）；逐实例 color
  // 与材质基色相乘，基色 = 季节 tint（夏 #ffffff = 旧版观感）。
  const foliage = useSynthPBR('foliage', { normalScale: [1.2, 1.2] });
  const fp = foliage.matProps;

  return (
    <group>
      {/* ① 主干 ×N → 1 draw call（批次 28 A3：仅 high 档投影） */}
      <Instances
        {...trunkInfo}
        raycast={instancedEventsRaycast}
        limit={Math.max(1, trunks.length)}
        range={trunks.length}
        castShadow={trunkShadow}
      >
        <cylinderGeometry args={TRUNK_GEOM} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {trunks.map((t) => (
          <Instance key={t.key} position={[t.x, t.y, t.z]} scale={t.s} />
        ))}
      </Instances>
      {/* ② 分枝 ×N → 1 draw call（批次 28 A3：不投影，阴影由树干承担） */}
      <Instances
        {...branchInfo}
        raycast={instancedEventsRaycast}
        limit={Math.max(1, branches.length)}
        range={branches.length}
        castShadow={false}
      >
        <cylinderGeometry args={BRANCH_GEOM} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {branches.map((b) => (
          <Instance
            key={b.key}
            position={[b.x, b.y, b.z]}
            rotation={[0.4, b.rotY, 0.5]}
            scale={b.s}
          />
        ))}
      </Instances>
      {/* ③ 树冠 3 球 ×N → 1 draw call（单位 icosahedron + 逐实例半径/色；基色随季节 tint；
          批次 28 A3：不投影 —— 1506 个 icosahedron 是阴影 pass 大户，树干投影足够） */}
      <Instances
        {...crownInfo}
        raycast={instancedEventsRaycast}
        limit={Math.max(1, crowns.length)}
        range={crowns.length}
        castShadow={false}
      >
        <icosahedronGeometry args={[1, 1]} />
        <meshStandardMaterial
          color={SEASON_CROWN_TINT[season]}
          {...fp}
          roughness={fp.roughnessMap ? undefined : 0.85}
          metalness={0.05}
        />
        {crowns.map((c) => (
          <Instance key={c.key} position={[c.x, c.y, c.z]} scale={c.r} color={c.color} />
        ))}
      </Instances>
    </group>
  );
});
