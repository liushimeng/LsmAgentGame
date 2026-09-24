/**
 * building_shapes — 建筑体块形态系统（13-3D城市渲染优化 · 阶段 B + 18-3D城市PBR与真实城市冲刺 · 阶段 X/Y）：
 *
 * 体块形态（13 阶段 B）：
 *   - tower    高层塔楼：裙楼(podium) + 塔身(body) + 顶部收分(crown)
 *   - slab     多层板楼：单 box + 深色檐口条
 *   - house    坡屋顶别墅：box 主体 + 三棱柱坡屋顶（自构 prism BufferGeometry）
 *   - shed     工业厂房：大跨 box + 山墙三角 + 烟囱
 *   - pavilion 公园景观低层：平顶小品 + 大挑檐
 *
 * 18 阶段 X：ShapeProps 追加 pbrBase / pbrMid / roofPbr，BoxFaces 透传 pbrA/pbrB/pbrTop，
 * sideMatProps / topMatProps spread matProps 并实现 withPBR（无 roughnessMap 保留硬编码，
 * 有 roughnessMap 不传 roughness 由贴图全权决定）。
 *
 * 18 阶段 Y：导出 mergeBoxes + 8 个构造件（ParapetRing / EntranceLobby / CornerQuoins /
 * AcUnits / LiftRoom / PodiumRail / SawtoothRoof / ShedWindowBands），BalconyLines 改为
 * 四面合并成 1 mesh；四种体块按 §3.3 表格追加构造件。
 *
 * 契约：lag_docs/虚拟城市/已实现/13-3D城市渲染优化/02-架构设计-WebGL渲染管线优化-v1.md §2、
 *       lag_docs/虚拟城市/已实现/18-3D城市PBR材质与真实城市冲刺/02-架构设计-PBR材质管线与建筑几何深化-v1.md
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
 *   - building_shapes 不 import @/assets/images/wealth（02 §3.3 硬约束：stem 拼接
 *     只允许在 BuildingMesh.tsx 发生；合成材质 PBR 经 textureCache::useSynthPBR 间接取用）。
 */

import { useEffect, useMemo } from 'react';
import * as THREE from 'three';
import type { WealthDistrictId } from '@/types/wealth';
import { u } from './cityScale';
import {
  type SharedPBR,
  type SynthStem,
  useSynthPBR,
  withPBR,
} from './textureCache';

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
  // ── 批次 20 城市扩张新增 16 区（archetype = 文档 1 §2.3 表）──
  fin_sub_center: 'tower', bay_new_town: 'tower',
  software_park: 'slab', airport_town: 'slab', university_town: 'slab',
  sports_new_city: 'slab', highspeed_rail_town: 'slab',
  mountain_resort: 'house', agri_park: 'house', health_town: 'house', old_city_culture: 'house',
  air_logistics: 'shed', auto_city: 'shed', chem_park: 'shed', steel_town: 'shed',
  wetland_park: 'pavilion',
};

/** 暖窗光（契约 §2.3；ACES 下强度由调用方 ≤0.35 控制）。 */
export const EMISSIVE_WINDOW = '#ffd9a0';

// ── 14-3D城市渲染深化 · 阶段 I：临街底商 + 广告牌（契约 02 §4）──────
const AWNING_COLOR = '#8a5a44';   // 雨棚暖木色
const BILLBOARD_POLE = '#5a6270'; // 广告牌支架

const CROWN_COLOR = '#3a4250';   // 塔楼收分金属
const CORNICE_COLOR = '#242a35'; // 板楼檐口
const ROOF_TILE_COLOR = '#8a4b3a'; // 坡屋顶红瓦兜底
const GABLE_COLOR = '#6b7280';   // 厂房山墙/烟囱
const EAVE_COLOR = '#3f3a33';    // 公园挑檐木色

// ── 18 阶段 X · ShapeProps（基类新增 pbrBase / pbrMid / roofPbr）────────

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
  /** 18-X：立面 base 贴图对应的 PBR 三件套（含法线/粗糙度）。 */
  pbrBase?: SharedPBR;
  /** 18-X：立面 mid 贴图对应的 PBR 三件套。base 与 mid 是两张不同贴图，PBR 必须各自独立
   *  否则窗洞凹凸会错位（02 §3.3 契约补丁）。 */
  pbrMid?: SharedPBR;
  /** 18-X：屋顶贴图对应的 PBR 三件套。 */
  roofPbr?: SharedPBR;
}

// ── 16 · 阶段 R：楼宇色相分化（确定性，不引随机源）────────────────

/**
 * 按占地 (w,d) hash → 0.92~1.08 灰度乘子 hex。
 * 同楼同色（确定性）；贴图路径 color 由 #ffffff 改乘此值，打破
 * 「同贴图 = 同色」的复制粘贴感。贴图缺失路径不走分化（避免双重变色）。
 */
export function buildingTint(w: number, d: number): string {
  let h = Math.imul((w * 4096) | 0, 2654435761) ^ Math.imul((d * 4096) | 0, 2246822519);
  h = (h ^ (h >>> 13)) >>> 0;
  const k = 0.92 + ((h % 1000) / 1000) * 0.16;
  const c = Math.round(255 * k);
  return `rgb(${c},${c},${c})`;
}

// ── 确定性伪随机（FNV-1a hash；与 DistrictBlock 同源实现，AcUnits 用）────

function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

// ── 6 面材质声明（boxGeometry 面序 [+X,-X,+Y,-Y,+Z,-Z]）─────────────

function sideMatProps(
  tex: THREE.Texture | null,
  fallback: string,
  emissive: number,
  tint: string,
  pbr?: SharedPBR,
) {
  const base = {
    map: tex ?? undefined,
    color: tex ? tint : fallback,
    emissive: tex ? EMISSIVE_WINDOW : fallback,
    emissiveMap: tex ?? undefined,
    emissiveIntensity: emissive,
    roughness: 0.7,
    metalness: 0.1,
    envMapIntensity: 0.5, // 16 · 阶段 R：天空环境反射（幕墙/立面）
  };
  // 18-X：贴图缺失时 matProps 自动为空对象，withPBR 安全降级回原 base。
  return withPBR(base, pbr ?? { map: null, normalMap: null, roughnessMap: null, matProps: {} });
}

function topMatProps(
  tex: THREE.Texture | null,
  fallback: string,
  emissive: number,
  pbr?: SharedPBR,
) {
  const base = {
    map: tex ?? undefined,
    color: tex ? '#ffffff' : fallback,
    emissive: tex ? EMISSIVE_WINDOW : fallback,
    emissiveMap: tex ?? undefined,
    emissiveIntensity: emissive * 0.6,
    roughness: 0.8,
    metalness: 0.05,
    envMapIntensity: 0.5,
  };
  return withPBR(base, pbr ?? { map: null, normalMap: null, roughnessMap: null, matProps: {} });
}

/**
 * box 六面材质：侧 A（±X）/ 侧 B（±Z）/ 顶（+Y）/ 底（-Y，不可见但材质完整）。
 * 必须作为 <mesh> 的直接子节点（R3F attach 到父 mesh）。
 *
 * 18-X：pbrA / pbrB / pbrTop 各自传入对应立面的 PBR；底面无 PBR（降级硬编码 roughness 0.9）。
 */
function BoxFaces({
  sideA, sideB, top, fallback, emissive, tint, pbrA, pbrB, pbrTop,
}: {
  sideA: THREE.Texture | null;
  sideB: THREE.Texture | null;
  top: THREE.Texture | null;
  fallback: string;
  emissive: number;
  /** 16 · 阶段 R：楼宇色相分化乘子（buildingTint 产出）。 */
  tint: string;
  pbrA?: SharedPBR;
  pbrB?: SharedPBR;
  pbrTop?: SharedPBR;
}) {
  return (
    <>
      <meshStandardMaterial attach="material-0" {...sideMatProps(sideA, fallback, emissive, tint, pbrA)} />
      <meshStandardMaterial attach="material-1" {...sideMatProps(sideA, fallback, emissive, tint, pbrA)} />
      <meshStandardMaterial attach="material-2" {...topMatProps(top, fallback, emissive, pbrTop)} />
      <meshStandardMaterial attach="material-3" color={fallback} roughness={0.9} />
      <meshStandardMaterial attach="material-4" {...sideMatProps(sideB, fallback, emissive, tint, pbrB)} />
      <meshStandardMaterial attach="material-5" {...sideMatProps(sideB, fallback, emissive, tint, pbrB)} />
    </>
  );
}

// ── 三棱柱坡屋顶几何（ridge 沿 Z 轴；底面在 y=0）────────────────────

/**
 * 自构三角棱柱：横截面 XY 三角形 (-w/2,0)-(w/2,0)-(0,h)，沿 Z 挤出 d。
 * 非索引三角面 + computeVertexNormals；UV 走简单平面映射（屋顶贴图平铺感）。
 */
export function prismGeometry(w: number, h: number, d: number): THREE.BufferGeometry {
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

// ── 18-Y · mergeBoxes（02 §3.1；纯手写顶点拼接，不引入 three/examples/jsm）────

/**
 * box 描述：父组局部坐标下的中心偏移 + 尺寸。合并后共享一套简单盒式 UV（0..1 每面），
 * 多个 box 合 1 mesh → 把「每层阳台线」「四角柱」这类 N 件套从 N 个 draw call 压到 1 个。
 */
export interface BoxSpec {
  x: number; y: number; z: number;
  w: number; h: number; d: number;
}

/** 24 顶点 × N box + 36 索引 × N box；UV 走简单盒式（6 面，每面 (0,0)-(1,1)）。 */
export function mergeBoxes(boxes: BoxSpec[]): THREE.BufferGeometry {
  const positions: number[] = [];
  const normals: number[] = [];
  const uvs: number[] = [];
  const indices: number[] = [];

  for (const b of boxes) {
    const baseIdx = positions.length / 3;
    const x0 = b.x - b.w / 2, x1 = b.x + b.w / 2;
    const y0 = b.y - b.h / 2, y1 = b.y + b.h / 2;
    const z0 = b.z - b.d / 2, z1 = b.z + b.d / 2;

    // 每面 4 顶点 → 2 三角形 → 6 索引；面序 [+X,-X,+Y,-Y,+Z,-Z]（与 BoxGeometry 对齐）
    type Face = { v: Array<[number, number, number]>; n: [number, number, number] };
    const faces: Face[] = [
      // +X
      { v: [[x1, y0, z0], [x1, y0, z1], [x1, y1, z1], [x1, y1, z0]], n: [1, 0, 0] },
      // -X
      { v: [[x0, y0, z1], [x0, y0, z0], [x0, y1, z0], [x0, y1, z1]], n: [-1, 0, 0] },
      // +Y
      { v: [[x0, y1, z0], [x1, y1, z0], [x1, y1, z1], [x0, y1, z1]], n: [0, 1, 0] },
      // -Y
      { v: [[x0, y0, z1], [x1, y0, z1], [x1, y0, z0], [x0, y0, z0]], n: [0, -1, 0] },
      // +Z
      { v: [[x0, y0, z1], [x0, y1, z1], [x1, y1, z1], [x1, y0, z1]], n: [0, 0, 1] },
      // -Z
      { v: [[x1, y0, z0], [x1, y1, z0], [x0, y1, z0], [x0, y0, z0]], n: [0, 0, -1] },
    ];
    for (const f of faces) {
      for (const v of f.v) positions.push(v[0], v[1], v[2]);
      for (let i = 0; i < 4; i++) normals.push(f.n[0], f.n[1], f.n[2]);
      // UV：每面 (0,0) (1,0) (1,1) (0,1)
      uvs.push(0, 0, 1, 0, 1, 1, 0, 1);
    }
    for (let i = 0; i < 6; i++) {
      const a = baseIdx + i * 4;
      indices.push(a, a + 1, a + 2, a, a + 2, a + 3);
    }
  }

  const geo = new THREE.BufferGeometry();
  geo.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
  geo.setAttribute('normal', new THREE.Float32BufferAttribute(normals, 3));
  geo.setAttribute('uv', new THREE.Float32BufferAttribute(uvs, 2));
  geo.setIndex(indices);
  // 顶点法向已手动写入（每个面共享一法向）；不调 computeVertexNormals 避免硬边被平滑掉。
  return geo;
}

/** Hook 化的 mergeBoxes（useMemo + useEffect dispose，§92a 接线纪律）。 */
function useMergedGeometry(boxes: BoxSpec[]): THREE.BufferGeometry {
  const geo = useMemo(() => mergeBoxes(boxes), [boxes]);
  useEffect(() => () => geo.dispose(), [geo]);
  return geo;
}

// ── 18-Y · 8 个构造件（02 §3.2 逐条几何规格）────────────────────

/**
 * 女儿墙（02 §3.2 · ParapetRing）
 * 用途：tower 裙楼顶 y=pH / slab 顶 y=h / shed 檐口上 y=bH（锯齿顶时改为檐口收边）。
 * mergeBoxes 4 条：前后 [w, u(0.9), u(0.25)] @ z=±(d/2−u(0.125))；
 * 左右 [u(0.25), u(0.9), d−2·u(0.125)] @ x=±(w/2−u(0.125))。
 * 1 mesh；混凝土 PBR（synth/concrete [0.8,0.8]），无 PBR → #8a8f98。
 */
function ParapetRing({ w, d, y }: { w: number; d: number; y: number }) {
  const pbr = useSynthPBR('concrete', { wrap: 'repeat', repeat: [1, 1], normalScale: [0.8, 0.8] });
  const concreteColor = '#8a8f98';
  const t = u(0.25);
  const h = u(0.9);
  const half = u(0.125);
  const boxes: BoxSpec[] = [
    { x: 0, y: h / 2, z: +(d / 2 - half), w, h, d: t },
    { x: 0, y: h / 2, z: -(d / 2 - half), w, h, d: t },
    { x: +(w / 2 - half), y: h / 2, z: 0, w: t, h, d: d - 2 * half },
    { x: -(w / 2 - half), y: h / 2, z: 0, w: t, h, d: d - 2 * half },
  ];
  const geo = useMergedGeometry(boxes);
  const base = { color: concreteColor, roughness: 0.85, metalness: 0.05 };
  return (
    <mesh geometry={geo} position={[0, y, 0]} castShadow receiveShadow>
      <meshStandardMaterial {...withPBR(base, pbr)} />
    </mesh>
  );
}

/**
 * 入口门厅（02 §3.2 · EntranceLobby）
 * 用途：tower / slab / house 底层居中（shed 卷帘门、pavilion 挑檐 不加）。
 * - 玻璃门 plane [u(1.4), u(2.4)] @ z=+d/2+0.002, y=u(1.2)（在父组底下）
 * - 门框 2 竖 1 横（mergeBoxes）
 * - 雨棚 box [u(2.2), u(0.12), u(0.9)] @ y=u(2.6), z=d/2+u(0.45)
 * 材质：门 #2a4e6e glass；框 #3a414c；雨棚 #4a5568。
 * mesh 数 3（玻璃门 / 框 / 雨棚）——比预算 2 多 1：门框与雨棚颜色不同，无法 merge。
 */
function EntranceLobby({ w: _w, d, y }: { w: number; d: number; y: number }) {
  const glassW = u(1.4);
  const glassH = u(2.4);
  const awningW = u(2.2);
  const awningH = u(0.12);
  const awningD = u(0.9);
  // 门框：2 竖 + 1 横（横向顶封）
  const frameT = u(0.06);
  const frameBoxes: BoxSpec[] = [
    { x: -glassW / 2 + frameT / 2, y: glassH / 2, z: d / 2 + 0.004, w: frameT, h: glassH, d: frameT },
    { x: +glassW / 2 - frameT / 2, y: glassH / 2, z: d / 2 + 0.004, w: frameT, h: glassH, d: frameT },
    { x: 0, y: glassH - frameT / 2, z: d / 2 + 0.004, w: glassW + 2 * frameT, h: frameT, d: frameT },
  ];
  const frameGeo = useMergedGeometry(frameBoxes);
  return (
    <group position={[0, y, 0]}>
      {/* 玻璃门 plane（独立 mesh） */}
      <mesh position={[0, glassH / 2, d / 2 + 0.002]}>
        <planeGeometry args={[glassW, glassH]} />
        <meshStandardMaterial
          color="#2a4e6e"
          transparent
          opacity={0.7}
          roughness={0.1}
          metalness={0.3}
          envMapIntensity={0.9}
          side={THREE.DoubleSide}
        />
      </mesh>
      {/* 门框 mergeBoxes（1 mesh） */}
      <mesh geometry={frameGeo}>
        <meshStandardMaterial color="#3a414c" roughness={0.65} metalness={0.3} />
      </mesh>
      {/* 雨棚 box（独立 mesh，与门框颜色不同） */}
      <mesh position={[0, u(2.6), d / 2 + u(0.45)]} castShadow>
        <boxGeometry args={[awningW, awningH, awningD]} />
        <meshStandardMaterial color="#4a5568" roughness={0.6} metalness={0.35} />
      </mesh>
    </group>
  );
}

/**
 * 角柱（02 §3.2 · CornerQuoins）
 * 用途：house / slab（w>1.4）四角竖向凸条 [u(0.15), h, u(0.15)]。
 * 砖 PBR（synth/brick [1.0,1.0]），无 PBR → #9a8f84。
 */
function CornerQuoins({ w, d, h }: { w: number; d: number; h: number }) {
  const pbr = useSynthPBR('brick', { wrap: 'repeat', repeat: [1, 1], normalScale: [1.0, 1.0] });
  const t = u(0.15);
  const boxes: BoxSpec[] = [
    { x: +w / 2, y: h / 2, z: +d / 2, w: t, h, d: t },
    { x: +w / 2, y: h / 2, z: -d / 2, w: t, h, d: t },
    { x: -w / 2, y: h / 2, z: +d / 2, w: t, h, d: t },
    { x: -w / 2, y: h / 2, z: -d / 2, w: t, h, d: t },
  ];
  const geo = useMergedGeometry(boxes);
  const base = { color: '#9a8f84', roughness: 0.8, metalness: 0.05 };
  return (
    <mesh geometry={geo} castShadow receiveShadow>
      <meshStandardMaterial {...withPBR(base, pbr)} />
    </mesh>
  );
}

/**
 * 空调外机（02 §3.2 · AcUnits）
 * 用途：tower 塔身 / slab 临路侧挂墙。
 * 2–3 个小盒 [u(0.5), u(0.35), u(0.25)] @ z=+d/2+u(0.12)，y 由 hashStr 确定楼层。
 * 确定性伪随机：seed = `${w.toFixed(3)}-${d.toFixed(3)}-${y.toFixed(3)}`；
 * 数量 2–3、x 偏移、y 楼层比例均由 hashStr 驱动（同楼同形，刷新稳定）。
 */
function AcUnits({ w, d, y }: { w: number; d: number; y: number }) {
  const seed = `${w.toFixed(3)}-${d.toFixed(3)}-${y.toFixed(3)}`;
  const cnt = 2 + (hashStr(`ac-cnt-${seed}`) % 2); // 2 或 3
  const boxes: BoxSpec[] = [];
  for (let i = 0; i < cnt; i++) {
    const h = hashStr(`ac-${seed}-${i}`);
    const fy = ((h % 1000) / 1000) * 0.6 + 0.2; // 0.20..0.80 of wall height
    const fx = (((h >>> 10) % 1000) / 1000 - 0.5) * 0.6; // ±0.30 of w
    boxes.push({
      x: fx * w,
      y: fy * y,
      z: d / 2 + u(0.12),
      w: u(0.5),
      h: u(0.35),
      d: u(0.25),
    });
  }
  const geo = useMergedGeometry(boxes);
  return (
    <mesh geometry={geo} castShadow>
      <meshStandardMaterial color="#c5c8ce" roughness={0.55} metalness={0.25} />
    </mesh>
  );
}

/**
 * 电梯机房 + 擦窗机轨道（02 §3.2 · LiftRoom）
 * 用途：tower（w>1.2），置于 crown 顶。
 * 2 meshes：屋 box [w*0.3, u(2.4), d*0.3] + 轨道 mergeBoxes 一圈细条。
 */
function LiftRoom({ w, d, y }: { w: number; d: number; y: number }) {
  const rw = w * 0.3;
  const rh = u(2.4);
  const rd = d * 0.3;
  // 擦窗机轨道：在屋 box 略外一圈 4 条细条
  const trackT = u(0.06);
  const margin = u(0.15);
  const trackBoxes: BoxSpec[] = [
    { x: 0, y: rh + trackT / 2, z: +(rd / 2 + margin + trackT / 2), w: rw + 2 * margin, h: trackT, d: trackT },
    { x: 0, y: rh + trackT / 2, z: -(rd / 2 + margin + trackT / 2), w: rw + 2 * margin, h: trackT, d: trackT },
    { x: +(rw / 2 + margin + trackT / 2), y: rh + trackT / 2, z: 0, w: trackT, h: trackT, d: rd + 2 * margin },
    { x: -(rw / 2 + margin + trackT / 2), y: rh + trackT / 2, z: 0, w: trackT, h: trackT, d: rd + 2 * margin },
  ];
  const trackGeo = useMergedGeometry(trackBoxes);
  return (
    <group position={[0, y, 0]}>
      {/* 屋 */}
      <mesh position={[0, rh / 2, 0]} castShadow>
        <boxGeometry args={[rw, rh, rd]} />
        <meshStandardMaterial color="#6b7280" roughness={0.7} metalness={0.3} />
      </mesh>
      {/* 轨道（4 条合并 1 mesh） */}
      <mesh geometry={trackGeo}>
        <meshStandardMaterial color="#3a414c" roughness={0.65} metalness={0.45} />
      </mesh>
    </group>
  );
}

/**
 * 裙楼露台栏杆（02 §3.2 · PodiumRail）
 * 用途：tower 裙楼顶外缘一圈矮栏杆。
 * 1 mesh：每 u(1.5) 1 根立柱 [u(0.08), u(1.1), u(0.08)] + 顶部 4 条扶手（前后左右 4 长条围一圈）。
 */
function PodiumRail({ w, d, y }: { w: number; d: number; y: number }) {
  const stW = u(0.08);
  const stH = u(1.1);
  const stD = u(0.08);
  const spacing = u(1.5);
  const halfW = w / 2;
  const halfD = d / 2;

  const columns: BoxSpec[] = [];
  // 前后两根横杆间距内布柱
  for (let s = -halfW + spacing / 2; s <= halfW - spacing / 2; s += spacing) {
    columns.push({ x: s, y: stH / 2, z: +halfD, w: stW, h: stH, d: stD });
    columns.push({ x: s, y: stH / 2, z: -halfD, w: stW, h: stH, d: stD });
  }
  for (let s = -halfD + spacing / 2; s <= halfD - spacing / 2; s += spacing) {
    columns.push({ x: +halfW, y: stH / 2, z: s, w: stW, h: stH, d: stD });
    columns.push({ x: -halfW, y: stH / 2, z: s, w: stW, h: stH, d: stD });
  }
  // 顶部扶手 4 条
  const handT = u(0.04);
  const handY = stH + handT / 2;
  const hands: BoxSpec[] = [
    { x: 0, y: handY, z: +halfD, w, h: handT, d: handT },
    { x: 0, y: handY, z: -halfD, w, h: handT, d: handT },
    { x: +halfW, y: handY, z: 0, w: handT, h: handT, d },
    { x: -halfW, y: handY, z: 0, w: handT, h: handT, d },
  ];
  const geo = useMergedGeometry([...columns, ...hands]);
  return (
    <mesh geometry={geo} position={[0, y, 0]} castShadow>
      <meshStandardMaterial color="#3a414c" roughness={0.6} metalness={0.4} />
    </mesh>
  );
}

/**
 * 锯齿屋顶（02 §3.2 · SawtoothRoof）
 * 用途：shed（w>1.4）替代人字顶。
 * 3 齿：每齿 1 个 prismGeometry（齿高 u(1.2)，齿宽 w/3）+ 齿背面采光带 plane
 *   （浅蓝半透明 #bcd9ea opacity 0.55，envMapIntensity 0.9，DoubleSide）。
 * 齿面金属 PBR（synth/metal_deck [0.9,0.9]）。
 * mesh 数：3 prism + 3 glass plane = 6。
 */
function SawtoothRoof({ w, d, y }: { w: number; d: number; y: number }) {
  const pbr = useSynthPBR('metal_deck', { wrap: 'repeat', repeat: [1, 1], normalScale: [0.9, 0.9] });
  const teeth = 3;
  const toothW = w / teeth;
  const roofH = u(1.2);
  const half = toothW / 2; // 半基
  const slopeLen = Math.hypot(half, roofH);
  const slopeAngle = Math.atan2(roofH, half);

  // 每齿：prism(toothW, roofH, d) + 后坡采光带 plane
  const items: Array<{ key: number; toothX: number; geo: THREE.BufferGeometry }> = [];
  for (let i = 0; i < teeth; i++) {
    const toothX = (i - 1) * toothW; // -toothW, 0, +toothW
    const g = useMemo(
      () => prismGeometry(toothW, roofH, d),
      [toothW, roofH, d],
    );
    useEffect(() => () => g.dispose(), [g]);
    items.push({ key: i, toothX, geo: g });
  }

  const base = { color: '#5a6270', roughness: 0.45, metalness: 0.5 };

  return (
    <group position={[0, y, 0]}>
      {items.map((it) => (
        <group key={`tooth-${it.key}`}>
          {/* 齿面（prism + metal_deck PBR） */}
          <mesh geometry={it.geo} castShadow>
            <meshStandardMaterial {...withPBR(base, pbr)} />
          </mesh>
          {/* 后坡采光带：先水平再绕 Z 抬起到坡角 */}
          <group
            position={[it.toothX - half / 2, roofH / 2, 0]}
            rotation={[0, 0, slopeAngle]}
          >
            <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, 0, 0]}>
              <planeGeometry args={[slopeLen, d]} />
              <meshStandardMaterial
                color="#bcd9ea"
                transparent
                opacity={0.55}
                roughness={0.2}
                metalness={0.1}
                envMapIntensity={0.9}
                side={THREE.DoubleSide}
              />
            </mesh>
          </group>
        </group>
      ))}
    </group>
  );
}

/**
 * 厂房侧高窗带（02 §3.2 · ShedWindowBands）
 * 用途：shed +Z 面 2 条横向高窗。
 * 1 mesh：mergeBoxes 2 条 [w*1.2*0.9, u(0.6), u(0.06)] @ z=+d/2+0.003，y=h*0.55 / h*0.75。
 * w 为原始楼宽（公式内部 *1.2*0.9 匹配 shed bw）；h 取 body 高度 bH。
 */
function ShedWindowBands({ w, d, h }: { w: number; d: number; h: number }) {
  const bandW = w * 1.2 * 0.9;
  const bandH = u(0.6);
  const bandT = u(0.06);
  const zOff = d / 2 + 0.003;
  const boxes: BoxSpec[] = [
    { x: 0, y: h * 0.55, z: zOff, w: bandW, h: bandH, d: bandT },
    { x: 0, y: h * 0.75, z: zOff, w: bandW, h: bandH, d: bandT },
  ];
  const geo = useMergedGeometry(boxes);
  return (
    <mesh geometry={geo}>
      <meshStandardMaterial color="#7fa8c4" roughness={0.15} metalness={0.2} envMapIntensity={0.9} />
    </mesh>
  );
}

// ── 五种体块渲染器 ─────────────────────────────────────────────

/**
 * 底商雨棚 + 灯带（tower 裙楼 / slab 沿街，面向 -Z，契约 02 §4.1）：
 * 雨棚挑檐 u(1.2) 深 / u(0.35) 高；灯带贴雨棚下沿 emissive #ffd9a0（上限 0.45）。
 */
function Shopfront({ w, d, y, emissive }: { w: number; d: number; y: number; emissive: number }) {
  const awningD = u(1.2);
  return (
    <>
      {/* 雨棚 */}
      <mesh castShadow position={[0, y, -(d / 2 + awningD * 0.5)]}>
        <boxGeometry args={[w * 0.9, u(0.35), awningD]} />
        <meshStandardMaterial color={AWNING_COLOR} roughness={0.8} metalness={0.05} />
      </mesh>
      {/* 暖光灯带 */}
      <mesh position={[0, y - u(0.25), -(d / 2 + awningD * 0.95)]}>
        <boxGeometry args={[w * 0.85, u(0.18), u(0.06)]} />
        <meshStandardMaterial
          color={EMISSIVE_WINDOW}
          emissive={EMISSIVE_WINDOW}
          emissiveIntensity={Math.min(0.45, emissive * 1.2)}
          roughness={0.3}
        />
      </mesh>
    </>
  );
}

/**
 * 广告牌（tower 且 w>1.4 时，契约 02 §4.2）：crown 顶双杆 + 发光面板，面向 -Z。
 * 出现与否由 w 决定（同楼同形，不引 Math.random）。
 */
function BillboardSign({ w, y }: { w: number; y: number }) {
  const panelW = w * 0.5;
  const poleH = u(2.0);
  return (
    <group position={[0, y, 0]}>
      {[-1, 1].map((side) => (
        <mesh key={`bp-${side}`} castShadow position={[side * panelW * 0.35, poleH / 2, 0]}>
          <cylinderGeometry args={[u(0.06), u(0.06), poleH, 8]} />
          <meshStandardMaterial color={BILLBOARD_POLE} roughness={0.6} metalness={0.4} />
        </mesh>
      ))}
      <mesh castShadow position={[0, poleH * 0.7, -u(0.1)]}>
        <boxGeometry args={[panelW, u(1.4), u(0.08)]} />
        <meshStandardMaterial
          color={EMISSIVE_WINDOW}
          emissive={EMISSIVE_WINDOW}
          emissiveIntensity={0.5}
          roughness={0.4}
          metalness={0.4}
          envMapIntensity={0.9}
        />
      </mesh>
      {/* 阶段 P：辉光（仅 w > 1.6 时启用，避免点光源过多拖性能） */}
      {w > 1.6 && (
        <pointLight
          position={[0, poleH * 0.7, -u(0.5)]}
          color={EMISSIVE_WINDOW}
          intensity={0.3}
          distance={5}
          decay={2}
        />
      )}
    </group>
  );
}

/**
 * 阶段 N + 18-Y：阳台线四面（02 §3.3 改造点）。
 * 现状只画 +Z → 四面（±X/±Z）都画；count 条 × 4 面 mergeBoxes 合并成 1 mesh。
 * 跳过底层 y<u(3.5) 与顶层 y>h−u(1)（02 §3.3「不变」）。
 */
function BalconyLines({ w, d, h }: { w: number; d: number; h: number }) {
  const lineSpacing = u(3);
  const count = Math.floor(h / lineSpacing);
  const boxes: BoxSpec[] = [];
  for (let i = 0; i < count; i++) {
    const y = i * lineSpacing + lineSpacing / 2 + u(1.5);
    if (y < u(3.5)) continue;          // 跳过底层（贴底商雨棚）
    if (y >= h - u(1)) continue;       // 跳过顶层（贴屋顶）
    const lineT = u(0.04);
    const lineD = u(0.15);
    const off = u(0.02);
    // +Z
    boxes.push({ x: 0, y, z: d / 2 + off, w: w * 0.95, h: lineT, d: lineD });
    // -Z
    boxes.push({ x: 0, y, z: -(d / 2 + off), w: w * 0.95, h: lineT, d: lineD });
    // +X
    boxes.push({ x: w / 2 + off, y, z: 0, w: lineD, h: lineT, d: d * 0.95 });
    // -X
    boxes.push({ x: -(w / 2 + off), y, z: 0, w: lineD, h: lineT, d: d * 0.95 });
  }
  const geo = useMergedGeometry(boxes);
  return (
    <mesh geometry={geo}>
      <meshStandardMaterial color="#2a2e36" roughness={0.85} />
    </mesh>
  );
}

/** 阶段 N：屋顶设备（确定性 1-2 件；用楼栋尺寸 w/d 决定）。 */
function RooftopEquipment({ w, d, y }: { w: number; d: number; y: number }) {
  const baseY = y + u(0.05);
  if (w > 1.5) {
    return (
      <>
        {/* 水箱 */}
        <mesh position={[w * 0.3, baseY + u(0.15), d * 0.3]} castShadow>
          <cylinderGeometry args={[u(0.15), u(0.15), u(0.3), 8]} />
          <meshStandardMaterial color="#a8a4a0" roughness={0.7} metalness={0.3} />
        </mesh>
        {/* 通风管 */}
        <mesh position={[-w * 0.3, baseY + u(0.18), -d * 0.3]} castShadow>
          <cylinderGeometry args={[u(0.08), u(0.08), u(0.35), 6]} />
          <meshStandardMaterial color="#8a8d96" roughness={0.7} metalness={0.4} />
        </mesh>
      </>
    );
  }
  return (
    <>
      {/* 通风管（小楼 1 个） */}
      <mesh position={[0, baseY + u(0.15), 0]} castShadow>
        <cylinderGeometry args={[u(0.06), u(0.06), u(0.3), 6]} />
        <meshStandardMaterial color="#8a8d96" roughness={0.7} metalness={0.4} />
      </mesh>
      {/* 天窗（小盒） */}
      <mesh position={[w * 0.25, baseY + u(0.04), 0]} castShadow>
        <boxGeometry args={[u(0.2), u(0.06), u(0.2)]} />
        <meshStandardMaterial color="#5a6270" roughness={0.6} metalness={0.5} />
      </mesh>
    </>
  );
}

/** 阶段 N：厂房卷帘门（shed 临路侧 z=+d/2）。 */
function ShutterDoor({ w, h }: { w: number; h: number }) {
  return (
    <>
      {/* 卷帘门主体（深色 box） */}
      <mesh position={[0, h * 0.2, 0.001]}>
        <planeGeometry args={[w * 0.6, h * 0.4]} />
        <meshStandardMaterial color="#3a414c" roughness={0.95} />
      </mesh>
      {/* 横纹装饰（4 道） */}
      {[0.15, 0.25, 0.35, 0.45].map((t, i) => (
        <mesh key={`stripe-${i}`} position={[0, h * t, 0.002]}>
          <planeGeometry args={[w * 0.6, u(0.02)]} />
          <meshStandardMaterial color="#2a2e36" roughness={0.95} />
        </mesh>
      ))}
    </>
  );
}

/** tower：裙楼(u(10) 高满铺) + 塔身(0.8× 占地) + 顶部收分(0.55× 占地, u(6) 高)。 */
export function TowerShape(props: ShapeProps) {
  const {
    w, d, h,
    facadeBase, facadeMid, roofMap, fallbackColor, emissive,
    pbrBase, pbrMid, roofPbr,
  } = props;
  const tint = buildingTint(w, d);
  const pH = Math.min(u(10), h * 0.35);
  const cH = Math.min(u(6), h * 0.22);
  const bodyH = Math.max(h - pH - cH, u(3)); // 塔身至少 1 层
  return (
    <>
      {/* 裙楼（商业基座，facadeBase 四面） */}
      <mesh castShadow position={[0, pH / 2, 0]}>
        <boxGeometry args={[w, pH, d]} />
        <BoxFaces
          sideA={facadeBase} sideB={facadeBase} top={null}
          fallback={fallbackColor} emissive={emissive} tint={tint}
          pbrA={pbrBase} pbrB={pbrBase}
        />
      </mesh>
      {/* 塔身（facadeMid 四面 + roof 顶面） */}
      <mesh castShadow position={[0, pH + bodyH / 2, 0]}>
        <boxGeometry args={[w * 0.8, bodyH, d * 0.8]} />
        <BoxFaces
          sideA={facadeMid} sideB={facadeMid} top={roofMap}
          fallback={fallbackColor} emissive={emissive} tint={tint}
          pbrA={pbrMid} pbrB={pbrMid} pbrTop={roofPbr}
        />
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
          envMapIntensity={0.9}
        />
      </mesh>
      {/* 14-3D渲染深化：底商雨棚（裙楼沿街） */}
      <Shopfront w={w} d={d} y={Math.min(u(3.6), pH * 0.5)} emissive={emissive} />
      {/* 14-3D渲染深化：塔楼广告牌（w > 1.4 确定性出现） */}
      {w > 1.4 && <BillboardSign w={w} y={pH + bodyH + cH} />}
      {/* 15 阶段 N：阳台线（沿塔身高度每 u(3) 一道；现在四面） */}
      <BalconyLines w={w * 0.8} d={d * 0.8} h={bodyH + pH} />
      {/* 15 阶段 N：屋顶设备（crown 顶上） */}
      <RooftopEquipment w={w * 0.55} d={d * 0.55} y={pH + bodyH + cH} />
      {/* 18-Y 构造件：裙楼顶 ParapetRing(y=pH)；塔身 AcUnits；crown 顶 LiftRoom（w>1.2）；
          裙楼顶外缘 PodiumRail；底层 EntranceLobby */}
      <ParapetRing w={w} d={d} y={pH} />
      <AcUnits w={w * 0.8} d={d * 0.8} y={bodyH + pH} />
      {w > 1.2 && <LiftRoom w={w * 0.55} d={d * 0.55} y={pH + bodyH + cH} />}
      <PodiumRail w={w} d={d} y={pH} />
      <EntranceLobby w={w} d={d} y={0} />
    </>
  );
}

/** slab：单 box + 顶部深色檐口条（现状 8 城区贴图逻辑原样保留）。 */
export function SlabShape(props: ShapeProps) {
  const {
    w, d, h,
    facadeBase, facadeMid, roofMap, fallbackColor, emissive,
    pbrBase, pbrMid, roofPbr,
  } = props;
  const tint = buildingTint(w, d);
  return (
    <>
      <mesh castShadow position={[0, h / 2, 0]}>
        <boxGeometry args={[w, h, d]} />
        <BoxFaces
          sideA={facadeBase} sideB={facadeMid} top={roofMap}
          fallback={fallbackColor} emissive={emissive} tint={tint}
          pbrA={pbrBase} pbrB={pbrMid} pbrTop={roofPbr}
        />
      </mesh>
      {/* 檐口条（0.05 高深色压顶线，契约 §2.2） */}
      <mesh position={[0, h + 0.025, 0]}>
        <boxGeometry args={[w + 0.08, 0.05, d + 0.08]} />
        <meshStandardMaterial color={CORNICE_COLOR} roughness={0.8} metalness={0.2} />
      </mesh>
      {/* 14-3D渲染深化：底商雨棚（板楼沿街） */}
      <Shopfront w={w} d={d} y={Math.min(u(3.6), h * 0.3)} emissive={emissive} />
      {/* 15 阶段 N：阳台线（四面） */}
      <BalconyLines w={w} d={d} h={h} />
      {/* 15 阶段 N：屋顶设备 */}
      <RooftopEquipment w={w} d={d} y={h} />
      {/* 18-Y 构造件：顶层 ParapetRing(y=h)；塔身 AcUnits；四角 CornerQuoins（w>1.4）；
          底层 EntranceLobby */}
      <ParapetRing w={w} d={d} y={h} />
      <AcUnits w={w} d={d} y={h} />
      {w > 1.4 && <CornerQuoins w={w} d={d} h={h} />}
      <EntranceLobby w={w} d={d} y={0} />
    </>
  );
}

/** house：box 主体（h×0.7）+ 三棱柱坡屋顶（h×0.3，roof 贴图/红瓦兜底）。
 *  18-X：屋顶接 roofPbr；无贴图时 PrismRoof 内部兜底走 synth/tile_roof（红瓦保留）。
 *  18-Y：底层 EntranceLobby；四角 CornerQuoins（w>1.4）。 */
export function HouseShape(props: ShapeProps) {
  const {
    w, d, h,
    facadeBase, facadeMid, roofMap, fallbackColor, emissive,
    pbrBase, pbrMid, roofPbr,
  } = props;
  const tint = buildingTint(w, d);
  const bodyH = h * 0.7;
  const roofH = h * 0.3;
  return (
    <>
      <mesh castShadow position={[0, bodyH / 2, 0]}>
        <boxGeometry args={[w, bodyH, d]} />
        <BoxFaces
          sideA={facadeBase} sideB={facadeMid} top={null}
          fallback={fallbackColor} emissive={emissive} tint={tint}
          pbrA={pbrBase} pbrB={pbrMid}
        />
      </mesh>
      <PrismRoof
        w={w + 0.12} h={roofH} d={d + 0.12} y={bodyH}
        tex={roofMap} fallback={ROOF_TILE_COLOR} emissive={emissive}
        pbr={roofPbr} synthStem="tile_roof"
      />
      {/* 18-Y：底层 EntranceLobby；四角 CornerQuoins（w>1.4） */}
      <EntranceLobby w={w} d={d} y={0} />
      {w > 1.4 && <CornerQuoins w={w} d={d} h={bodyH} />}
    </>
  );
}

/** shed：大跨 box（h×0.8，w×1.2）+ 山墙三角 + 1-2 根烟囱（顶到 h×1.3）。
 *  18-Y：w>1.4 用 SawtoothRoof 替代 PrismRoof（北向采光带可辨）；
 *  锯齿顶时屋檐加 ParapetRing 作檐口收边（人字顶与坡屋顶几何冲突，故跳过）。
 *  18-X：屋顶接 roofPbr；人字顶无贴图时 PrismRoof 走 synth/metal_deck 兜底（厂房屋面）。 */
export function ShedShape(props: ShapeProps) {
  const {
    w, d, h,
    facadeBase, facadeMid, roofMap, fallbackColor, emissive,
    pbrBase, pbrMid, roofPbr,
  } = props;
  const tint = buildingTint(w, d);
  const bw = w * 1.2;
  const bH = h * 0.8;
  const chimneyH = h * 0.5;
  // 烟囱 1-2 根：由占地宽确定性决定（不引随机源，同楼同形）
  const chimneys = w > 1.5 ? [-bw * 0.25, bw * 0.25] : [bw * 0.25];
  const useSaw = w > 1.4;
  return (
    <>
      <mesh castShadow position={[0, bH / 2, 0]}>
        <boxGeometry args={[bw, bH, d]} />
        <BoxFaces
          sideA={facadeBase} sideB={facadeMid} top={null}
          fallback={fallbackColor} emissive={emissive} tint={tint}
          pbrA={pbrBase} pbrB={pbrMid}
        />
      </mesh>
      {/* 15 阶段 N：卷帘门（临路侧 z=+d/2） */}
      <group position={[0, 0, d / 2]}>
        <ShutterDoor w={bw} h={bH} />
      </group>
      {/* 18-Y：高窗带（沿 +Z 面两条横向高窗） */}
      <ShedWindowBands w={w} d={d} h={bH} />
      {/* 屋面：w>1.4 锯齿（替代人字顶）；否则人字顶 + metal_deck 兜底 PBR */}
      {useSaw ? (
        <SawtoothRoof w={bw} d={d} y={bH} />
      ) : (
        <PrismRoof
          w={bw} h={h * 0.2} d={d} y={bH}
          tex={roofMap} fallback={GABLE_COLOR} emissive={emissive}
          pbr={roofPbr} synthStem="metal_deck"
        />
      )}
      {/* 锯齿顶时屋檐加 ParapetRing 作檐口收边（人字顶与坡屋顶几何冲突，跳过） */}
      {useSaw && <ParapetRing w={bw} d={d} y={bH} />}
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
export function PavilionShape(props: ShapeProps) {
  const {
    w, d, h,
    facadeBase, roofMap, fallbackColor, emissive,
    pbrBase, roofPbr,
  } = props;
  const tint = buildingTint(w, d);
  const bH = Math.min(h, u(9));
  return (
    <>
      <mesh castShadow position={[0, bH / 2, 0]}>
        <boxGeometry args={[w, bH, d]} />
        <BoxFaces
          sideA={facadeBase} sideB={facadeBase} top={roofMap}
          fallback={fallbackColor} emissive={emissive} tint={tint}
          pbrA={pbrBase} pbrB={pbrBase} pbrTop={roofPbr}
        />
      </mesh>
      {/* 大挑檐（外扩 0.15，木色） */}
      <mesh castShadow position={[0, bH + 0.02, 0]}>
        <boxGeometry args={[w + 0.3, 0.04, d + 0.3]} />
        <meshStandardMaterial color={EAVE_COLOR} roughness={0.85} metalness={0.05} />
      </mesh>
    </>
  );
}

/**
 * 坡屋顶 mesh（几何 useMemo + dispose；材质 DoubleSide 防背面剔除）。
 * 18-X：合成材质兜底走 pbr/synth/tile_roof（house）或 metal_deck（shed），normalScale 1.0/0.9。
 * 有贴图时 spread roofPbr 的 matProps（窗格/瓦纹凹凸）。synth 兜底由 useSynthPBR 拼 URL，
 * building_shapes 不直接 import assets（满足 02 §3.3 stem 拼接约束）。
 */
function PrismRoof({
  w, h, d, y, tex, fallback, emissive, pbr, synthStem,
}: {
  w: number; h: number; d: number; y: number;
  tex: THREE.Texture | null; fallback: string; emissive: number;
  pbr?: SharedPBR;
  synthStem: SynthStem;
}) {
  const geo = useMemo(() => prismGeometry(w, h, d), [w, h, d]);
  useEffect(() => () => geo.dispose(), [geo]);
  // 两套 synth 常驻 hook（缓存共享，零额外请求）——house 走 tile_roof，shed 走 metal_deck。
  const tilePbr = useSynthPBR('tile_roof', { wrap: 'repeat', repeat: [1, 1], normalScale: [1.0, 1.0] });
  const metalPbr = useSynthPBR('metal_deck', { wrap: 'repeat', repeat: [1, 1], normalScale: [0.9, 0.9] });
  const synth = synthStem === 'metal_deck' ? metalPbr : tilePbr;
  // 02 §2.3 行 3：有贴图时用贴图 PBR；无贴图时用 synth兜底（红瓦/压型钢板）。
  const eff = (tex ? pbr : undefined) ?? synth;
  const base = {
    map: tex ?? undefined,
    color: tex ? '#ffffff' : fallback,
    emissive: tex ? EMISSIVE_WINDOW : fallback,
    emissiveMap: tex ?? undefined,
    emissiveIntensity: emissive * 0.5,
    roughness: 0.85,
    metalness: 0.05,
    envMapIntensity: 0.35,
  };
  return (
    <mesh castShadow geometry={geo} position={[0, y, 0]}>
      <meshStandardMaterial
        {...withPBR(base, eff)}
        side={THREE.DoubleSide}
      />
    </mesh>
  );
}

/** archetype → 渲染器分发表（BuildingMesh 唯一入口）。 */
export function BuildingShape(props: ShapeProps & { archetype: BuildingArchetype }) {
  // 塔楼高度护栏：总高不足以分三段时降级 slab（契约 §7 回退原则）
  if (props.archetype === 'tower' && props.h < u(16)) {
    return <SlabShape {...props} />;
  }
  switch (props.archetype) {
    case 'tower': return <TowerShape {...props} />;
    case 'house': return <HouseShape {...props} />;
    case 'shed': return <ShedShape {...props} />;
    case 'pavilion': return <PavilionShape {...props} />;
    case 'slab':
    default: return <SlabShape {...props} />;
  }
}