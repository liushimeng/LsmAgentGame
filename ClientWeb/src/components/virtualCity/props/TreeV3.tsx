/**
 * TreeV3（18-Z 后续并入 18-AA · §4.3）—— 立体树冠 3 球错落 + foliage 法线凹凸。
 *   替换 StreetPropsLayer 中 TreeV2 调用；V1/V2 文件保留。
 *   形态：主干（圆柱）+ 1–2 分枝 + 3 球错落树冠（icosahedronGeometry）。
 *   材质：树干 #5a4634 粗糙；树冠 #2f7a3a + `pbr/synth/foliage_n/r`。
 *   props 形状故意与 TreeV2 对齐（x/z/scale + 内部 hashStr 生成 seed），
 *   让 StreetPropsLayer 无痛替换。
 *
 * 19-Blender3D模型集成：用 <Model url={...}> 包一层，原程序化几何保留为 children fallback。
 *   - url 缺失或加载失败 → children 渲染（程序化几何）
 *   - url 加载成功 → 渲染真实模型
 */
import { useMemo } from 'react';
import { useSynthPBR } from '../cityPbr';
import { u, sizeTargetFor } from '../cityScale';
import { Model, blenderModelsEnabled as blenderEnabled } from '@/engine3d';
import { modelUrl } from '@/assets/models';

/** 阔叶树 GLB 尺寸/落地校验目标（批次 29，dev 态；表值 4.5 × 10.1 m）。 */
const OAK_SIZE_TARGET = sizeTargetFor('oakTree', { label: 'nature/oak_tree' });

export const TRUNK_COLOR = '#5a4634';
export const CROWN_COLORS = ['#2f7a3a', '#3a8a45', '#4a9a55'];

/**
 * 主干/分枝几何参数（TreesInstanced 与 TreeV3Fallback **同源**，批次 30 P0-3
 * 随树形整体放大到真实行道树尺度：干高 4.6 m、⌀0.28~0.36 m）。
 */
export const TRUNK_GEOM: [number, number, number, number] = [u(0.14), u(0.18), u(4.6), 8];
export const TRUNK_Y = u(2.3);
export const BRANCH_GEOM: [number, number, number, number] = [u(0.06), u(0.09), u(1.4), 6];
export const BRANCH_Y = u(3.9);
export const BRANCH_R = u(0.5);

/** 树形态种子（StreetPropsLayer / TreesInstanced 共用，保证逐实例形态一致）。 */
export function treeSeed(x: number, z: number): number {
  return hashStr(`tree:${x.toFixed(3)}:${z.toFixed(3)}`);
}

/**
 * 确定性树形态（主干/分枝/**5 球 2~3 层错落冠**）——TreeV3Fallback 与 TreesInstanced 同源。
 * 批次 30 P0-3/A5：原 3 球紧凑冠（总高仅 3.4~3.7 m × scale 0.5~1.2 ⇒ 1.7~4.4 m
 * "棒棒糖"）改为分层不规则冠：低层 2 球 + 中层 2 球 + 顶 1 球，包围盒归一到
 * cityScale.REAL_DIMS_M.streetTree（高 9.0 / 冠幅 4.5）。
 */
export function treeShape(effectiveSeed: number): {
  branchCount: number;
  crowns: Array<{ x: number; y: number; z: number; r: number; color: number }>;
} {
  const h = (s: number) => {
    let xh = (s * 2654435761) >>> 0;
    xh ^= xh >>> 13;
    xh = Math.imul(xh, 2246822519) >>> 0;
    xh ^= xh >>> 16;
    return xh / 0xffffffff;
  };
  const r1 = h(effectiveSeed);
  const r2 = h(effectiveSeed * 31 + 1);
  const r3 = h(effectiveSeed * 131 + 7);
  /** 确定性抖动：±amp/2 比例。 */
  const j = (r: number, amp: number) => 1 + (r - 0.5) * amp;
  return {
    branchCount: r1 > 0.6 ? 3 : 2,
    crowns: [
      // 低层（大冠幅，主视觉质量）
      { x: 0, y: u(5.3) * j(r2, 0.12), z: 0, r: u(1.6) * j(r1, 0.2), color: 0 },
      // 中层（左右错开）
      { x: u(0.8) * j(r1, 0.3), y: u(6.6), z: u(0.3) * j(r3, 0.3), r: u(1.3) * j(r2, 0.2), color: 1 },
      { x: -u(0.7) * j(r3, 0.3), y: u(6.5) * j(r1, 0.12), z: -u(0.5) * j(r2, 0.3), r: u(1.3) * j(r3, 0.2), color: 2 },
      // 顶（收分）
      { x: u(0.2) * j(r2, 0.3), y: u(7.8), z: -u(0.6) * j(r1, 0.3), r: u(1.1) * j(r1, 0.15), color: 0 },
      { x: -u(0.3) * j(r3, 0.3), y: u(8.5) * j(r3, 0.1), z: u(0.4) * j(r2, 0.3), r: u(0.9) * j(r2, 0.15), color: 1 },
    ],
  };
}

export interface TreeV3Props {
  /** 世界坐标 x。 */
  x: number;
  /** 世界坐标 z。 */
  z: number;
  /** 形态种子（确定性；默认由 hashStr(x,z) 派生）。 */
  seed?: number;
  /** 整体缩放（默认 1.0）。 */
  scale?: number;
  /** 是否 castShadow（性能控制：远景可关）。 */
  castShadow?: boolean;
}

/** 与 DistrictBlock.tsx 同款的 24 行 FNV-1a（避免主路径跨文件引用）。 */
function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

export function TreeV3({ x, z, seed, scale = 1, castShadow = true }: TreeV3Props) {
  const url = modelUrl('nature', 'oak_tree');
  if (!url || !blenderEnabled()) {
    return <TreeV3Fallback x={x} z={z} seed={seed} scale={scale} castShadow={castShadow} />;
  }
  return (
    <Model
      url={url}
      sizeTarget={OAK_SIZE_TARGET}
      position={[x, 0, z]}
      scale={scale}
      castShadow={castShadow}
    >
      <TreeV3Fallback x={x} z={z} seed={seed} scale={scale} castShadow={castShadow} />
    </Model>
  );
}

/** 程序化几何 fallback（19-Blender3D模型集成 · 02 架构设计 §5.2 降级链）。 */
function TreeV3Fallback({ x, z, seed, scale = 1, castShadow = true }: TreeV3Props) {
  const foliage = useSynthPBR('foliage', { normalScale: [1.2, 1.2] });
  const fp = foliage.matProps;

  const effectiveSeed = seed ?? treeSeed(x, z);

  const { branchCount, crowns } = useMemo(() => treeShape(effectiveSeed), [effectiveSeed]);

  return (
    <group position={[x, 0, z]} scale={scale}>
      {/* 主干（批次 30 P0-3：与 TreesInstanced 共用 TRUNK_GEOM，真实 ⌀0.28~0.36 m） */}
      <mesh position={[0, TRUNK_Y, 0]} castShadow={castShadow}>
        <cylinderGeometry args={TRUNK_GEOM} />
        <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
      </mesh>
      {/* 分枝 */}
      {Array.from({ length: branchCount }).map((_, i) => {
        const a = (i / Math.max(1, branchCount)) * Math.PI * 2 + effectiveSeed * 0.0001;
        return (
          <mesh
            key={`branch-${i}`}
            position={[Math.cos(a) * BRANCH_R, BRANCH_Y, Math.sin(a) * BRANCH_R]}
            rotation={[0.4, a, 0.5]}
            castShadow={castShadow}
          >
            <cylinderGeometry args={BRANCH_GEOM} />
            <meshStandardMaterial color={TRUNK_COLOR} roughness={0.9} />
          </mesh>
        );
      })}
      {/* 3 球树冠 */}
      {crowns.map((c, i) => (
        <mesh key={`crown-${i}`} position={[c.x, c.y, c.z]} castShadow={castShadow}>
          <icosahedronGeometry args={[c.r, 1]} />
          <meshStandardMaterial
            color={CROWN_COLORS[c.color]}
            {...fp}
            roughness={fp.roughnessMap ? undefined : 0.85}
            metalness={0.05}
          />
        </mesh>
      ))}
    </group>
  );
}