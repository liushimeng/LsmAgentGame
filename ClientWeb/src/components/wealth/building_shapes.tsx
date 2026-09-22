/**
 * building_shapes — 建筑体块形态系统（13-3D城市渲染优化 · 阶段 B）：
 *
 * 把 BuildingMesh 的「单 box + 5 平面」升级为按城区 archetype 分派的体块组合：
 *   - tower    高层塔楼：裙楼(podium) + 塔身(body) + 顶部收分(crown)
 *   - slab     多层板楼：单 box + 深色檐口条
 *   - house    坡屋顶别墅：box 主体 + 三棱柱坡屋顶（自构 prism BufferGeometry）
 *   - shed     工业厂房：大跨 box + 山墙三角 + 烟囱
 *   - pavilion 公园景观低层：平顶小品 + 大挑檐
 *
 * 契约：lag_docs/虚拟城市/已实现/13-3D城市渲染优化/02-架构设计-WebGL渲染管线优化-v1.md §2。
 *
 * 技术要点：
 *   - 每个体块用 boxGeometry + 6 面材质数组（three BoxGeometry 面序稳定：
 *     [+X, -X, +Y, -Y, +Z, -Z]），R3F 以 attach="material-N" 声明式挂载，
 *     替代旧版「每面独立 plane mesh」（mesh 数从 5×体块数 降到 1×体块数）。
 *   - 立面贴图分配：+X/-X 用 facadeBase（侧 A）、+Z/-Z 用 facadeMid（侧 B），
 *     塔楼裙楼固定 facadeBase、塔身固定 facadeMid（契约 §2.2）。
 *   - emissive 统一暖窗光 #ffd9a0；有贴图时 emissiveMap 复用立面贴图
 *     （窗格亮度差近似夜景窗灯，契约 §2.3），无贴图回退城区主色。
 *   - 所有米制尺寸经 cityScale.u() 换算，禁止硬编码米数（§2.1 标尺）。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import type { WealthDistrictId } from '@/types/wealth';
import { u } from './cityScale';

// ── Archetype 分派（契约 §2.1 表格，勿随意改派）────────────────────

export type BuildingArchetype =
  | 'tower'
  | 'slab'
  | 'house'
  | 'shed'
  | 'pavilion';

export const DISTRICT_ARCHETYPE: Record<WealthDistrictId, BuildingArchetype> = {
  finance: 'tower', riverside: 'tower', medical_city: 'tower',
  tech: 'slab', hightech_park: 'slab', residential: 'slab',
  commerce: 'slab', edu_district: 'slab', transport_hub: 'slab',
  cultural_creative: 'slab',
  oldtown: 'house', suburb: 'house',
  industry: 'shed', industrial_park: 'shed', logistics_port: 'shed',
  central_park: 'pavilion',
};

/** 暖窗光（契约 §2.3；ACES 下强度由调用方 ≤0.35 控制）。 */
export const EMISSIVE_WINDOW = '#ffd9a0';

const CROWN_COLOR = '#3a4250';   // 塔楼收分金属
const CORNICE_COLOR = '#242a35'; // 板楼檐口
const ROOF_TILE_COLOR = '#8a4b3a'; // 坡屋顶红瓦兜底
const GABLE_COLOR = '#6b7280';   // 厂房山墙/烟囱
const EAVE_COLOR = '#3f3a33';    // 公园挑檐木色

export interface ShapeProps {
  /** 占地宽 / 深（世界单位）。 */
  w: number;
  d: number;
  /** 总高（世界单位，由 BuildingMesh 按 DISTRICT_FLOORS 算好）。 */
  h: number;
  facadeBase: THREE.Texture | null;
  facadeMid: THREE.Texture | null;
  roofMap: THREE.Texture | null;
  /** 无贴图时回退的城区主色。 */
  fallbackColor: string;
  /** 暖窗光强度（prosperity × 0.35）。 */
  emissive: number;
}

// ── 6 面材质声明（boxGeometry 面序 [+X,-X,+Y,-Y,+Z,-Z]）─────────────

function sideMatProps(tex: THREE.Texture | null, fallback: string, emissive: number) {
  return {
    map: tex ?? undefined,
    color: tex ? '#ffffff' : fallback,
    emissive: tex ? EMISSIVE_WINDOW : fallback,
    emissiveMap: tex ?? undefined,
    emissiveIntensity: emissive,
    roughness: 0.7,
    metalness: 0.1,
  };
}

function topMatProps(tex: THREE.Texture | null, fallback: string, emissive: number) {
  return {
    map: tex ?? undefined,
    color: tex ? '#ffffff' : fallback,
    emissive: tex ? EMISSIVE_WINDOW : fallback,
    emissiveMap: tex ?? undefined,
    emissiveIntensity: emissive * 0.6,
    roughness: 0.8,
    metalness: 0.05,
  };
}

/**
 * box 六面材质：侧 A（±X）/ 侧 B（±Z）/ 顶（+Y）/ 底（-Y，不可见但材质完整）。
 * 必须作为 <mesh> 的直接子节点（R3F attach 到父 mesh）。
 */
function BoxFaces({
  sideA,
  sideB,
  top,
  fallback,
  emissive,
}: {
  sideA: THREE.Texture | null;
  sideB: THREE.Texture | null;
  top: THREE.Texture | null;
  fallback: string;
  emissive: number;
}) {
  return (
    <>
      <meshStandardMaterial attach="material-0" {...sideMatProps(sideA, fallback, emissive)} />
      <meshStandardMaterial attach="material-1" {...sideMatProps(sideA, fallback, emissive)} />
      <meshStandardMaterial attach="material-2" {...topMatProps(top, fallback, emissive)} />
      <meshStandardMaterial attach="material-3" color={fallback} roughness={0.9} />
      <meshStandardMaterial attach="material-4" {...sideMatProps(sideB, fallback, emissive)} />
      <meshStandardMaterial attach="material-5" {...sideMatProps(sideB, fallback, emissive)} />
    </>
  );
}

// ── 三棱柱坡屋顶几何（ridge 沿 Z 轴；底面在 y=0）────────────────────

/**
 * 自构三角棱柱：横截面 XY 三角形 (-w/2,0)-(w/2,0)-(0,h)，沿 Z 挤出 d。
 * 非索引三角面 + computeVertexNormals；UV 走简单平面映射（屋顶贴图平铺感）。
 */
function prismGeometry(w: number, h: number, d: number): THREE.BufferGeometry {
  const hw = w / 2;
  const hd = d / 2;
  // 顶点：后山墙 z=-hd / 前山墙 z=+hd
  const Ab = [-hw, 0, -hd], Bb = [hw, 0, -hd], Cb = [0, h, -hd];
  const Af = [-hw, 0, hd], Bf = [hw, 0, hd], Cf = [0, h, hd];

  const tris: number[][][] = [
    // 后山墙（朝 -Z）
    [Bb, Ab, Cb],
    // 前山墙（朝 +Z）
    [Af, Bf, Cf],
    // 左坡（-X 侧）：Ab→Cb→Cf→Af
    [Ab, Cb, Cf], [Ab, Cf, Af],
    // 右坡（+X 侧）：Bb→Bf→Cf→Cb
    [Bb, Bf, Cf], [Bb, Cf, Cb],
    // 底面（朝 -Y，贴墙不可见但几何闭合）
    [Ab, Af, Bf], [Ab, Bf, Bb],
  ];

  const positions: number[] = [];
  const uvs: number[] = [];
  for (const tri of tris) {
    for (const v of tri) {
      positions.push(v[0], v[1], v[2]);
      // 坡面/山墙统一平面映射：u=x 归一，v=z 或 y 归一（近似即可）
      uvs.push(v[0] / w + 0.5, v[2] / d + 0.5);
    }
  }
  const geo = new THREE.BufferGeometry();
  geo.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  geo.setAttribute('uv', new THREE.Float32BufferAttribute(uvs, 2));
  geo.computeVertexNormals();
  return geo;
}

/** 坡屋顶 mesh（几何 useMemo + dispose；材质 DoubleSide 防背面剔除）。 */
function PrismRoof({
  w, h, d, y, tex, fallback, emissive,
}: {
  w: number; h: number; d: number; y: number;
  tex: THREE.Texture | null; fallback: string; emissive: number;
}) {
  const geo = useMemo(() => prismGeometry(w, h, d), [w, h, d]);
  useEffect(() => () => geo.dispose(), [geo]);
  return (
    <mesh castShadow geometry={geo} position={[0, y, 0]}>
      <meshStandardMaterial
        map={tex ?? undefined}
        color={tex ? '#ffffff' : fallback}
        emissive={tex ? EMISSIVE_WINDOW : fallback}
        emissiveMap={tex ?? undefined}
        emissiveIntensity={emissive * 0.5}
        roughness={0.85}
        metalness={0.05}
        side={THREE.DoubleSide}
      />
    </mesh>
  );
}

// ── 五种体块渲染器 ─────────────────────────────────────────────

/** tower：裙楼(u(10) 高满铺) + 塔身(0.8× 占地) + 顶部收分(0.55× 占地, u(6) 高)。 */
export function TowerShape({ w, d, h, facadeBase, facadeMid, roofMap, fallbackColor, emissive }: ShapeProps) {
  const pH = Math.min(u(10), h * 0.35);
  const cH = Math.min(u(6), h * 0.22);
  const bodyH = Math.max(h - pH - cH, u(3)); // 塔身至少 1 层
  return (
    <>
      {/* 裙楼（商业基座，facadeBase 四面） */}
      <mesh castShadow position={[0, pH / 2, 0]}>
        <boxGeometry args={[w, pH, d]} />
        <BoxFaces sideA={facadeBase} sideB={facadeBase} top={null} fallback={fallbackColor} emissive={emissive} />
      </mesh>
      {/* 塔身（facadeMid 四面 + roof 顶面） */}
      <mesh castShadow position={[0, pH + bodyH / 2, 0]}>
        <boxGeometry args={[w * 0.8, bodyH, d * 0.8]} />
        <BoxFaces sideA={facadeMid} sideB={facadeMid} top={roofMap} fallback={fallbackColor} emissive={emissive} />
      </mesh>
      {/* 顶部收分（纯色金属，无贴图） */}
      <mesh castShadow position={[0, pH + bodyH + cH / 2, 0]}>
        <boxGeometry args={[w * 0.55, cH, d * 0.55]} />
        <meshStandardMaterial
          color={CROWN_COLOR}
          emissive={EMISSIVE_WINDOW}
          emissiveIntensity={emissive * 0.8}
          roughness={0.4}
          metalness={0.6}
        />
      </mesh>
    </>
  );
}

/** slab：单 box + 顶部深色檐口条（现状 8 城区贴图逻辑原样保留）。 */
export function SlabShape({ w, d, h, facadeBase, facadeMid, roofMap, fallbackColor, emissive }: ShapeProps) {
  return (
    <>
      <mesh castShadow position={[0, h / 2, 0]}>
        <boxGeometry args={[w, h, d]} />
        <BoxFaces sideA={facadeBase} sideB={facadeMid} top={roofMap} fallback={fallbackColor} emissive={emissive} />
      </mesh>
      {/* 檐口条（0.05 高深色压顶线，契约 §2.2） */}
      <mesh position={[0, h + 0.025, 0]}>
        <boxGeometry args={[w + 0.08, 0.05, d + 0.08]} />
        <meshStandardMaterial color={CORNICE_COLOR} roughness={0.8} metalness={0.2} />
      </mesh>
    </>
  );
}

/** house：box 主体（h×0.7）+ 三棱柱坡屋顶（h×0.3，roof 贴图/红瓦兜底）。 */
export function HouseShape({ w, d, h, facadeBase, facadeMid, roofMap, fallbackColor, emissive }: ShapeProps) {
  const bodyH = h * 0.7;
  const roofH = h * 0.3;
  return (
    <>
      <mesh castShadow position={[0, bodyH / 2, 0]}>
        <boxGeometry args={[w, bodyH, d]} />
        <BoxFaces sideA={facadeBase} sideB={facadeMid} top={null} fallback={fallbackColor} emissive={emissive} />
      </mesh>
      <PrismRoof
        w={w + 0.12} h={roofH} d={d + 0.12} y={bodyH}
        tex={roofMap} fallback={ROOF_TILE_COLOR} emissive={emissive}
      />
    </>
  );
}

/** shed：大跨 box（h×0.8，w×1.2）+ 山墙三角 + 1-2 根烟囱（顶到 h×1.3）。 */
export function ShedShape({ w, d, h, facadeBase, facadeMid, roofMap, fallbackColor, emissive }: ShapeProps) {
  const bw = w * 1.2;
  const bH = h * 0.8;
  const chimneyH = h * 0.5;
  // 烟囱 1-2 根：由占地宽确定性决定（不引随机源，同楼同形）
  const chimneys = w > 1.5 ? [-bw * 0.25, bw * 0.25] : [bw * 0.25];
  return (
    <>
      <mesh castShadow position={[0, bH / 2, 0]}>
        <boxGeometry args={[bw, bH, d]} />
        <BoxFaces sideA={facadeBase} sideB={facadeMid} top={null} fallback={fallbackColor} emissive={emissive} />
      </mesh>
      {/* 山墙三角（坡屋顶同构，金属灰兜底） */}
      <PrismRoof
        w={bw} h={h * 0.2} d={d} y={bH}
        tex={roofMap} fallback={GABLE_COLOR} emissive={emissive}
      />
      {/* 烟囱（细圆柱，半径 u(0.8)，顶到 1.3×总高） */}
      {chimneys.map((cx, i) => (
        <mesh key={i} castShadow position={[cx, bH + chimneyH / 2, -d * 0.2]}>
          <cylinderGeometry args={[u(0.8), u(0.9), chimneyH, 10]} />
          <meshStandardMaterial color={GABLE_COLOR} roughness={0.75} metalness={0.3} />
        </mesh>
      ))}
    </>
  );
}

/** pavilion：低矮平顶 box（h ≤ u(9)）+ 大挑檐（顶面外扩 0.15）。 */
export function PavilionShape({ w, d, h, facadeBase, roofMap, fallbackColor, emissive }: ShapeProps) {
  const bH = Math.min(h, u(9));
  return (
    <>
      <mesh castShadow position={[0, bH / 2, 0]}>
        <boxGeometry args={[w, bH, d]} />
        <BoxFaces sideA={facadeBase} sideB={facadeBase} top={roofMap} fallback={fallbackColor} emissive={emissive} />
      </mesh>
      {/* 大挑檐（外扩 0.15，木色） */}
      <mesh castShadow position={[0, bH + 0.02, 0]}>
        <boxGeometry args={[w + 0.3, 0.04, d + 0.3]} />
        <meshStandardMaterial color={EAVE_COLOR} roughness={0.85} metalness={0.05} />
      </mesh>
    </>
  );
}

/** archetype → 渲染器分发表（BuildingMesh 唯一入口）。 */
export function BuildingShape({ archetype, ...props }: ShapeProps & { archetype: BuildingArchetype }) {
  // 塔楼高度护栏：总高不足以分三段时降级 slab（契约 §7 回退原则）
  if (archetype === 'tower' && props.h < u(16)) {
    return <SlabShape {...props} />;
  }
  switch (archetype) {
    case 'tower': return <TowerShape {...props} />;
    case 'house': return <HouseShape {...props} />;
    case 'shed': return <ShedShape {...props} />;
    case 'pavilion': return <PavilionShape {...props} />;
    case 'slab':
    default: return <SlabShape {...props} />;
  }
}
