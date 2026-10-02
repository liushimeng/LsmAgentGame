/**
 * TreesInstanced — 批次 20 实例化 → **批次 44 城市树 GLB 化**。
 *
 * ## 批次 44 做了什么（一种物体 = 树，全城 1800 株）
 *
 * **改前**（批次 20/27/28/30 累积）：全城 1800 株树是**纯程序化**的 drei `<Instances>`
 * 三层（主干圆柱 + 分枝圆柱 + 5 个 `icosahedron` 球冠），约 15,300 个 `<Instance>`
 * 组件、每帧在 drei 的 `useFrame` 里逐个 decompose/compose；`variant` 在
 * `StreetPropsLayer` 合并时被丢弃 ⇒ 全城 1800 株**实际只有一种树**。
 *
 * **改后**：按 `variant` 分组，每组一个 `GlbInstanced`（原生 `InstancedMesh`，
 * 矩阵一次性写入 + `computeBoundingSphere()`）⇒
 *   · 三种真实树种各自成形（法桐 / 黑松 / 棕榈，见 `cityScale.REAL_DIMS_M`）；
 *   · 实例化组件数 15,300 → 0（每帧零 React/drei 实例开销）；
 *   · 视锥剔除重新生效（drei `<Instances>` 不算实例级包围球 ⇒ 包围球覆盖全城）。
 *
 * **保留**：程序化三层 `<Instances>` 作为**降级链**（GLB 缺失 / 加载中 / 失败 /
 * `disable-blender-models=1`），观感与改前逐像素一致。
 *
 * ## 季节行为（批次 44 C2）
 *
 * 改前是两套割裂的机制：城内树走材质 tint（`SEASON_CROWN_TINT`，只改色）、
 * 边缘森林走 GLB 变体切换（`seasonAssets.seasonOakModelUrl`），且松/棕榈完全不响应。
 * 改后统一为**按物候调制叶材质**：悬铃木是**落叶树**（CJJ/T 75-2023 §6.0.3
 * 寒冷积雪地区行道树宜用落叶树种），秋转黄、冬转枯褐；而黑松/棕榈是**常绿**，
 * 真实上不随季节变色 —— 故只调 `StreetTree_Leaf_Mat`，pine/palm 的
 * `Needle` / `Frond` 材质**不动**（这是植物学事实，不是偷懒）。
 *
 * 关键实现点：GLB 按 url 共享 scene（`useSharedGLTF` 缓存）⇒ 叶材质也是**全城共享
 * 同一 datablock**，切一次颜色即全城 1800 株同步生效，**零逐实例开销**。
 */

import { memo, useEffect, useMemo, useState } from 'react';
import { Instances, Instance } from '@react-three/drei';
import { useThree } from '@react-three/fiber';
import * as THREE from 'three';
import { detectQualityTier, useSharedGLTF, blenderModelsEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';
import { useSynthPBR } from '../cityPbr';
import { currentSeason, subscribeSeason } from '../cityTimeStore';
import type { CitySeason } from '../cityTimeStore';
import { sizeTargetFor } from '../cityScale';
import {
  TRUNK_COLOR,
  CROWN_COLORS,
  TRUNK_GEOM,
  TRUNK_Y,
  BRANCH_GEOM,
  BRANCH_Y,
  BRANCH_R,
  treeSeed,
  treeShape,
} from './TreeV3';
import { useObjectInfoProps, instancedEventsRaycast } from '../objectInfo/useObjectInfoProps';
import { GlbInstanced, type GlbInstanceTRS } from '../edge/glbInstanced';

/** 树种变体（与 `StreetPropsLayer.TREE_VARIANTS` 同一套字面量）。 */
export type TreeVariant = 'oak' | 'pine' | 'palm';

/**
 * 变体 → GLB 资产 + 尺寸表键。
 *
 * `oak` 指向**悬铃木（法桐）**而非既有 `oak_tree.glb`：后者是批次 19 的森林橡树
 * （10.1 m、无枝干结构、单株实例化后只服务东森林 150 株），尺寸/形态都不适合行道树。
 * 悬铃木是 CJJ/T 75-2023 下温带城市行道树的主力树种，且是**落叶树**。
 */
const VARIANT_ASSETS: Record<TreeVariant, { glb: string; dims: 'streetTree' | 'pineTree' | 'palmTree' }> = {
  oak: { glb: 'street_tree', dims: 'streetTree' },
  pine: { glb: 'pine_tree', dims: 'pineTree' },
  palm: { glb: 'palm_tree', dims: 'palmTree' },
};
const VARIANT_ORDER: TreeVariant[] = ['oak', 'pine', 'palm'];

/**
 * 落叶树（悬铃木）叶色随季节（批次 44 C2）。
 *
 * 春嫩绿 / 夏浓绿 / 秋金黄 / 冬枯褐 —— 冬季**不加白**（加白是常绿树的雪压观感，
 * 落叶树冬季的正确表现是「叶落枝裸」，此处以枯褐色近似；真正的裸枝需要
 * 独立的冬季 GLB 变体，见实施记录遗留表）。
 */
const DECIDUOUS_LEAF_TINT: Record<CitySeason, string> = {
  spring: '#9ed08a',
  summer: '#4a7a3f',
  autumn: '#c8912f',
  winter: '#8a7358',
};

export interface InstanceTree {
  x: number;
  z: number;
  scale: number;
  /** 批次 28 B2：行道树 / 区内园林树（catalog tree.road / tree.park 按实例区分）。 */
  kind?: 'road' | 'park';
  /**
   * 批次 44 B1：树种变体。**此前 StreetPropsLayer 写入后又在合并时丢弃**
   * ⇒ 全城 1800 株同种；本字段把那条链路接通。缺省按 `oak`（法桐）处理。
   */
  variant?: TreeVariant;
}

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

  // 批次 44 B2：GLB instancedMesh 无法像 drei `<Instances>` 那样「只让主干投影」
  // （干/枝/冠在 GLB 里是分开的 primitive，但整棵树必须一起投影才成比例），
  // 故按质量档整体取舍：low 档树不投影。
  const gl = useThree((s) => s.gl);
  const tier = useMemo(() => detectQualityTier(gl), [gl]);
  const highTier = tier === 'high';

  // 批次 27：季节低频订阅（仅 season 变化 setState）。
  const [season, setSeason] = useState<CitySeason>(currentSeason);
  useEffect(() => subscribeSeason(setSeason), []);

  // 批次 44 B1：按 variant 分组（缺失按 oak 兜底，保证老数据不渲染成「未知树」）。
  const groups = useMemo(() => {
    const g: Record<TreeVariant, InstanceTree[]> = { oak: [], pine: [], palm: [] };
    for (const t of effective) g[t.variant ?? 'oak'].push(t);
    return g;
  }, [effective]);

  // 每组一个 GlbInstanceTRS 列表（GlbInstanced 内部 useMemo 成矩阵）。
  const trsByVariant = useMemo(() => {
    const out = {} as Record<TreeVariant, GlbInstanceTRS[]>;
    for (const v of VARIANT_ORDER) {
      out[v] = groups[v].map((t) => ({ position: [t.x, 0, t.z] as [number, number, number], scale: t.scale }));
    }
    return out;
  }, [groups]);

  return (
    <>
      {VARIANT_ORDER.map((variant) => (
        <TreeVariantGroup
          key={variant}
          variant={variant}
          trees={groups[variant]}
          instances={trsByVariant[variant]}
          season={season}
          highTier={highTier}
        />
      ))}
    </>
  );
});

/** 单个树变体组：GLB 优先 + 程序化降级 + 季节叶色 + 物件信息交互。 */
const TreeVariantGroup = memo(function TreeVariantGroup({
  variant,
  trees,
  instances,
  season,
  highTier,
}: {
  variant: TreeVariant;
  trees: InstanceTree[];
  instances: GlbInstanceTRS[];
  season: CitySeason;
  highTier: boolean;
}) {
  // 批次 44 D6 修正：曾限定「只有 oak 投影」（pine/palm 不投影）以省 shadow DC。
  // 复验证明该前提**不成立**：① 「全城 DC 逼近 1035 上限」是**半帧读数伪影**
  // （three 在 render() 内部才 info.reset()），正确口径下 low 档中位 1023、
  // high 档（真机 GPU）约 1495 —— 1035 本就只是软件渲染降档下的数字；
  // ② 该限制在默认机位**实测收益为 0**（pine/palm 落在阴影相机视锥外），
  // ③ 代价却是实打实的：北缘松与湾区棕榈在近景**没有影子**。
  // 收益为 0、代价可见 ⇒ 撤销，恢复三变体全投影。
  const castShadow = highTier;
  const asset = VARIANT_ASSETS[variant];
  const sizeTarget = useMemo(
    () => sizeTargetFor(asset.dims, { label: `nature/${asset.glb}` }),
    [asset.dims, asset.glb],
  );
  // §27.5 总闸：url 置空 ⇒ useSharedGLTF 立即返回 null ⇒ 直接渲染程序化降级树。
  const blenderOn = useMemo(() => blenderModelsEnabled(), []);
  const url = blenderOn ? modelUrl('nature', asset.glb) : '';

  // 批次 44 C2：落叶树叶色调制。GLB 按 url 共享 scene ⇒ 材质是全城同一 datablock，
  // 这里改一次即全城生效。**只动落叶树**：pine 的 Needle / palm 的 Frond 是常绿，
  // 真实上不随季节变色（故本 effect 挂在 oak 的 url 上，物理上碰不到那两件）。
  const { scene } = useSharedGLTF(url, sizeTarget);
  useEffect(() => {
    if (!scene || variant !== 'oak') return;
    scene.traverse((o) => {
      const m = (o as THREE.Mesh).material;
      if (!m) return;
      const mat = (Array.isArray(m) ? m[0] : m) as THREE.MeshStandardMaterial;
      if (mat && mat.name && mat.name.includes('StreetTree_Leaf')) {
        mat.color.set(DECIDUOUS_LEAF_TINT[season]);
      }
    });
  }, [scene, season, variant]);

  // 批次 28 B2：树信息交互。GLB 路径下 instanceId = **本变体组内**的下标，
  // 因此 idFor 要先在本组内取树再判 kind（不能拿全城下标）。
  const info = useObjectInfoProps('tree.road', {
    anchorY: 1.6,
    idFor: (e) => (trees[e.instanceId ?? -1]?.kind === 'park' ? 'tree.park' : 'tree.road'),
  });

  if (!instances.length) return null;

  return (
    <group {...info}>
      <GlbInstanced
        url={url}
        instances={instances}
        sizeTarget={sizeTarget}
        castShadow={castShadow}
        fallback={<ProceduralTrees trees={trees} season={season} castShadow={castShadow} />}
      />
    </group>
  );
});

/**
 * 程序化降级树（GLB 缺失 / 加载中 / 失败 / `disable-blender-models=1`）。
 *
 * 形态与批次 30 归一后的 `treeShape` 逐棵一致（确定性 `treeSeed`），
 * 观感与批次 20 改前相同 —— 降级不产生视觉跳变。
 */
function ProceduralTrees({
  trees,
  season,
  castShadow,
}: {
  trees: InstanceTree[];
  season: CitySeason;
  castShadow: boolean;
}) {
  const { trunks, branches, crowns } = useMemo(() => {
    const tr: InstTrunk[] = [];
    const br: InstBranch[] = [];
    const cr: InstCrown[] = [];
    trees.forEach((t, ti) => {
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
  }, [trees]);

  // 树冠材质与 TreeV3 同源（foliage 法线/粗糙 PBR）；基色 = 季节 tint。
  const foliage = useSynthPBR('foliage', { normalScale: [1.2, 1.2] });
  const fp = foliage.matProps;
  const crownTint = DECIDUOUS_LEAF_TINT[season];

  return (
    <group>
      {/* ① 主干 ×N → 1 draw call */}
      <Instances
        raycast={instancedEventsRaycast}
        limit={Math.max(1, trunks.length)}
        range={trunks.length}
        castShadow={castShadow}
      >
        <cylinderGeometry args={TRUNK_GEOM} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
        {trunks.map((t) => (
          <Instance key={t.key} position={[t.x, t.y, t.z]} scale={t.s} />
        ))}
      </Instances>
      {/* ② 分枝 ×N → 1 draw call（不投影） */}
      <Instances
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
      {/* ③ 树冠球 ×N → 1 draw call（基色随季节 tint；不投影） */}
      <Instances
        raycast={instancedEventsRaycast}
        limit={Math.max(1, crowns.length)}
        range={crowns.length}
        castShadow={false}
      >
        <icosahedronGeometry args={[1, 1]} />
        <meshStandardMaterial
          color={crownTint}
          {...fp}
          roughness={fp.roughnessMap ? undefined : 0.85}
          metalness={0.05}
        />
        {crowns.map((c) => (
          <Instance
            key={c.key}
            position={[c.x, c.y, c.z]}
            scale={c.r}
            color={c.color}
          />
        ))}
      </Instances>
    </group>
  );
}
