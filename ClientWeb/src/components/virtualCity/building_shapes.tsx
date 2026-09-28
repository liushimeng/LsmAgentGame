/**
 * building_shapes — 建筑体块形态系统（13-3D城市渲染优化 · 阶段 B + 18 · 阶段 X/Y +
 * 批次 28 二轮 · 楼栋按材质类合并）：
 *
 * 体块形态（13 阶段 B）：
 *   - tower    高层塔楼：裙楼(podium) + 塔身(body) + 顶部收分(crown)
 *   - slab     多层板楼：单 box + 深色檐口条
 *   - house    坡屋顶别墅：box 主体 + 三棱柱坡屋顶（自构 prism BufferGeometry）
 *   - shed     工业厂房：大跨 box + 山墙三角 + 烟囱
 *   - pavilion 公园景观低层：平顶小品 + 大挑檐
 *
 * 批次 28 二轮（G1 DC 攻坚）：每栋楼从 tower≈18 / slab≈13 / house≈6 / shed≈11 /
 * pavilion≈2 mesh 收敛为 **2 个 mesh**（几何全等合并，engine3d/geoMerge）：
 *   1) 墙体 mesh（多 group）：侧A/侧B/顶面/坡屋顶/锯齿齿面/楼冠/玻璃/发光件
 *      按材质索引分桶拼接 —— 三角形、UV、法向与原逐件渲染逐位一致，
 *      DC = 材质组数（tower 7-8 / slab 5 / house 5 / shed 4-5 / pavilion 2）。
 *   2) 点缀 mesh（单材质 + 顶点色）：女儿墙/角柱/空调外机/电梯机房+擦窗机轨道/
 *      裙楼栏杆/门框/雨棚/阳台线/屋顶设备/檐口/卷帘门/高窗带/烟囱/挑檐等纯色件
 *      合并为 1 mesh —— 部件颜色经顶点色 100% 保留；粗糙度/金属度统一为
 *      ACCENT_ROUGH/ACCENT_METAL（原 0.05–0.95 区间 → 0.7/0.25，视觉取舍见
 *      批次 28 报告），synth concrete/brick 法线贴图随之取消（城市尺度不可辨）。
 * 不跨楼合并（逐楼 hover 由 BuildingMesh 根 group 的 useObjectInfoProps 保留）。
 *
 * 契约：lag_docs/虚拟城市/已实现/13-3D城市渲染优化/02-架构设计-WebGL渲染管线优化-v1.md §2、
 *       lag_docs/虚拟城市/已实现/18-3D城市PBR材质与真实城市冲刺/02-架构设计-PBR材质管线与建筑几何深化-v1.md、
 *       lag_docs/虚拟城市/已实现/28-3D性能优化与物件信息交互/01-方案设计-v1.md §4 A2
 *
 * 技术要点：
 *   - 立面贴图分配：+X/-X 用 facadeBase（侧 A）、+Z/-Z 用 facadeMid（侧 B），
 *     塔楼裙楼固定 facadeBase、塔身固定 facadeMid（契约 §2.2）。
 *   - emissive 统一暖窗光 #ffd9a0；有贴图时 emissiveMap 复用立面贴图
 *     （窗格亮度差近似夜景窗灯，契约 §2.3），无贴图回退城区主色。
 *   - 所有米制尺寸经 cityScale.u() 换算，禁止硬编码米数（§2.1 标尺）。
 *   - building_shapes 不 import @/assets/images/virtualCity（02 §3.3 硬约束：stem 拼接
 *     只允许在 BuildingMesh.tsx 发生；合成材质 PBR 经 textureCache::useSynthPBR 间接取用）。
 */

import { useEffect } from 'react';
import * as THREE from 'three';
import type { VirtualCityDistrictId } from '@/types/virtualCity';
import { u } from './cityScale';
import {
  type SharedPBR,
  type SynthStem,
  withPBR,
} from './cityPbr';
import {
  type GroupedMergePart,
  type MergePart,
  boxFaces,
  boxPart,
  cylPart,
} from '@/engine3d';

// ── Archetype 分派（契约 §2.1 表格，勿随意改派）────────────────────

export type BuildingArchetype =
  | 'tower'
  | 'slab'
  | 'house'
  | 'shed'
  | 'pavilion';

export const DISTRICT_ARCHETYPE: Record<VirtualCityDistrictId, BuildingArchetype> = {
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
const FRAME_COLOR = '#3a414c';    // 门框/轨道/栏杆深金属
const AWNING_BLUE = '#4a5568';    // 门厅雨棚
const AC_COLOR = '#c5c8ce';       // 空调外机
const LIFT_COLOR = '#6b7280';     // 电梯机房
const CONCRETE_COLOR = '#8a8f98'; // 女儿墙
const QUOIN_COLOR = '#9a8f84';    // 角柱
const BALCONY_COLOR = '#2a2e36';  // 阳台线 / 卷帘门横纹
const SHUTTER_COLOR = '#3a414c';  // 卷帘门主体
const WINBAND_COLOR = '#7fa8c4';  // 厂房高窗带
const ROOFTOP_TANK = '#a8a4a0';   // 屋顶水箱
const ROOFTOP_VENT = '#8a8d96';   // 屋顶通风管
const ROOFTOP_SKY = '#5a6270';    // 屋顶天窗小盒

const CROWN_COLOR = '#3a4250';   // 塔楼收分金属
const CORNICE_COLOR = '#242a35'; // 板楼檐口
const ROOF_TILE_COLOR = '#8a4b3a'; // 坡屋顶红瓦兜底
const GABLE_COLOR = '#6b7280';   // 厂房山墙/烟囱
const EAVE_COLOR = '#3f3a33';    // 公园挑檐木色

/** 点缀类统一材质参数（批次 28 二轮视觉取舍：原 0.05–0.95 区间取中；DistrictBuildings 复用）。 */
export const ACCENT_ROUGH = 0.7;
export const ACCENT_METAL = 0.25;

/** 玻璃类参数（门厅玻璃门 / 锯齿顶采光带；二者不同楼型不共存）。 */
const GLASS_DOOR = { color: '#2a4e6e', opacity: 0.7, roughness: 0.1, metalness: 0.3 };
const GLASS_SKY = { color: '#bcd9ea', opacity: 0.55, roughness: 0.2, metalness: 0.1 };

// ── 18 · 阶段 X：ShapeProps（基类新增 pbrBase / pbrMid / roofPbr）────────

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

// ── 6 面材质声明（面序 [+X,-X,+Y,-Y,+Z,-Z]；与原 BoxSolid 完全同源）─────────

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

// ── 批次 28 二轮：墙体材质规格 → 材质数组 ──────────────────────

/** 墙体 mesh 的材质规格（每 archetype 固定顺序；部件按 mat 索引挂 group）。 */
export type WallMatSpec =
  | { kind: 'sideA' }                                    // 侧 A（±X，facadeBase）
  | { kind: 'sideB' }                                    // 侧 B（±Z，facadeMid）
  | { kind: 'top'; roof: boolean }                       // 顶 + 底（roof=true 用 roofMap / false 纯色兜底）
  | { kind: 'crown' }                                    // 塔楼收分（emissive 金属）
  | { kind: 'prism'; fallback: string; stem: SynthStem } // 坡屋顶（贴图 PBR / synth 兜底）
  | { kind: 'deck' }                                     // 锯齿顶齿面（synth metal_deck）
  | { kind: 'glass'; c: typeof GLASS_DOOR }              // 玻璃件
  | { kind: 'glow'; intensity: number; rough: number; metal: number }; // 暖光灯带/广告牌

/** 材质构建上下文（ShapeProps 直通 + synth 兜底 PBR）。 */
export interface WallCtx {
  facadeBase: THREE.Texture | null;
  facadeMid: THREE.Texture | null;
  roofMap: THREE.Texture | null;
  fallbackColor: string;
  emissive: number;
  tint: string;
  pbrBase?: SharedPBR;
  pbrMid?: SharedPBR;
  roofPbr?: SharedPBR;
  /** PrismRoof synth 兜底（18-X 契约：有贴图走 roofPbr，无贴图走 synth）。 */
  tilePbr?: SharedPBR;
  metalPbr?: SharedPBR;
}

export function buildWallMaterial(spec: WallMatSpec, ctx: WallCtx): THREE.MeshStandardMaterial {
  switch (spec.kind) {
    case 'sideA':
      return new THREE.MeshStandardMaterial(sideMatProps(ctx.facadeBase, ctx.fallbackColor, ctx.emissive, ctx.tint, ctx.pbrBase));
    case 'sideB':
      return new THREE.MeshStandardMaterial(sideMatProps(ctx.facadeMid, ctx.fallbackColor, ctx.emissive, ctx.tint, ctx.pbrMid));
    case 'top':
      // roof=false 的体块（塔楼裙楼/house/shed 主体）顶面原就是纯色兜底（top={null}）
      return new THREE.MeshStandardMaterial(
        topMatProps(spec.roof ? ctx.roofMap : null, ctx.fallbackColor, ctx.emissive, spec.roof ? ctx.roofPbr : undefined),
      );
    case 'crown':
      // 原 TowerShape 楼冠材质逐字段保留
      return new THREE.MeshStandardMaterial({
        color: CROWN_COLOR,
        emissive: EMISSIVE_WINDOW,
        emissiveIntensity: ctx.emissive * 0.8,
        roughness: 0.4,
        metalness: 0.6,
        envMapIntensity: 0.9,
      });
    case 'prism': {
      // 原 PrismRoof 材质逐字段保留（DoubleSide 防背面剔除；synth 兜底随 stem）
      const synth = spec.stem === 'metal_deck' ? ctx.metalPbr : ctx.tilePbr;
      const eff = (ctx.roofMap ? ctx.roofPbr : undefined) ?? synth;
      const base = {
        map: ctx.roofMap ?? undefined,
        color: ctx.roofMap ? '#ffffff' : spec.fallback,
        emissive: ctx.roofMap ? EMISSIVE_WINDOW : spec.fallback,
        emissiveMap: ctx.roofMap ?? undefined,
        emissiveIntensity: ctx.emissive * 0.5,
        roughness: 0.85,
        metalness: 0.05,
        envMapIntensity: 0.35,
      };
      return new THREE.MeshStandardMaterial({
        ...withPBR(base, eff ?? { map: null, normalMap: null, roughnessMap: null, matProps: {} }),
        side: THREE.DoubleSide,
      });
    }
    case 'deck':
      // 原 SawtoothRoof 齿面材质逐字段保留（synth/metal_deck PBR）
      return new THREE.MeshStandardMaterial({
        ...withPBR(
          { color: '#5a6270', roughness: 0.45, metalness: 0.5 },
          ctx.metalPbr ?? { map: null, normalMap: null, roughnessMap: null, matProps: {} },
        ),
      });
    case 'glass':
      return new THREE.MeshStandardMaterial({
        color: spec.c.color,
        transparent: true,
        opacity: spec.c.opacity,
        roughness: spec.c.roughness,
        metalness: spec.c.metalness,
        envMapIntensity: 0.9,
        side: THREE.DoubleSide,
      });
    case 'glow':
      return new THREE.MeshStandardMaterial({
        color: EMISSIVE_WINDOW,
        emissive: EMISSIVE_WINDOW,
        emissiveIntensity: spec.intensity,
        roughness: spec.rough,
        ...(spec.metal > 0 ? { metalness: spec.metal, envMapIntensity: 0.9 } : {}),
      });
  }
}

/** useMemo 产物的 dispose 纪律（§92a 接线纪律；几何/材质随依赖变化即释放）。 */
export function useDisposable<T extends { dispose(): void }>(obj: T): T {
  useEffect(() => () => obj.dispose(), [obj]);
  return obj;
}

/** 单栋楼的纯几何产物（批次 30 A2：区级合并渲染的输入；原 MergedBuilding 组件退役）。 */
export interface BuildingParts {
  wallParts: GroupedMergePart[];
  matSpecs: WallMatSpec[];
  accentParts: MergePart[];
  /** 广告牌辉光点光高度（0 = 无；区级合并时点光由调用方裁量，防光源爆炸）。 */
  billboardLightY: number;
}

/**
 * 材质表基线 emissive 记录（批次 30 B2 窗灯昼夜：调用方 useFrame 按
 * userData.baseEmissive × 昼夜系数改写 emissiveIntensity）。
 */
export function tagBaseEmissive(mats: THREE.MeshStandardMaterial[]): THREE.MeshStandardMaterial[] {
  for (const m of mats) m.userData.baseEmissive = m.emissiveIntensity;
  return mats;
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

// ── 18-Y · mergeBoxes（02 §3.1）—— 旧导出保留（外部组件沿用该口径）────

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

// ── 18-Y 构造件 → 点缀部件发射器（几何与原 JSX 逐件全等）──────────────

/** 女儿墙（02 §3.2 · ParapetRing）：4 条 box @ y（原 mesh position=[0,y,0] 内 y=h/2）。 */
function parapetAccent(w: number, d: number, y: number): MergePart[] {
  const t = u(0.25);
  const h = u(0.9);
  const half = u(0.125);
  return [
    boxPart(w, h, t, 0, y + h / 2, +(d / 2 - half), CONCRETE_COLOR),
    boxPart(w, h, t, 0, y + h / 2, -(d / 2 - half), CONCRETE_COLOR),
    boxPart(t, h, d - 2 * half, +(w / 2 - half), y + h / 2, 0, CONCRETE_COLOR),
    boxPart(t, h, d - 2 * half, -(w / 2 - half), y + h / 2, 0, CONCRETE_COLOR),
  ];
}

/** 入口门厅框 + 雨棚（02 §3.2 · EntranceLobby；玻璃门另入墙体 glass 组）。 */
function entranceAccent(d: number): MergePart[] {
  const glassW = u(1.4);
  const glassH = u(2.4);
  const frameT = u(0.06);
  return [
    boxPart(frameT, glassH, frameT, -glassW / 2 + frameT / 2, glassH / 2, d / 2 + 0.004, FRAME_COLOR),
    boxPart(frameT, glassH, frameT, +glassW / 2 - frameT / 2, glassH / 2, d / 2 + 0.004, FRAME_COLOR),
    boxPart(glassW + 2 * frameT, frameT, frameT, 0, glassH - frameT / 2, d / 2 + 0.004, FRAME_COLOR),
    // 雨棚 box [u(2.2), u(0.12), u(0.9)] @ y=u(2.6)
    boxPart(u(2.2), u(0.12), u(0.9), 0, u(2.6), d / 2 + u(0.45), AWNING_BLUE),
  ];
}

/** 入口玻璃门 plane（原 EntranceLobby 玻璃门，材质参数 GLASS_DOOR）。 */
function entranceGlass(d: number): GroupedMergePart {
  const glassW = u(1.4);
  const glassH = u(2.4);
  return { geo: new THREE.PlaneGeometry(glassW, glassH), x: 0, y: glassH / 2, z: d / 2 + 0.002, mat: -1 };
}

/** 角柱（02 §3.2 · CornerQuoins）：四角竖条 [u(0.15), h, u(0.15)]。 */
function quoinAccent(w: number, d: number, h: number): MergePart[] {
  const t = u(0.15);
  return [
    boxPart(t, h, t, +w / 2, h / 2, +d / 2, QUOIN_COLOR),
    boxPart(t, h, t, +w / 2, h / 2, -d / 2, QUOIN_COLOR),
    boxPart(t, h, t, -w / 2, h / 2, +d / 2, QUOIN_COLOR),
    boxPart(t, h, t, -w / 2, h / 2, -d / 2, QUOIN_COLOR),
  ];
}

/** 空调外机（02 §3.2 · AcUnits）：hash 确定性 2–3 个小盒挂 +Z 墙。 */
function acUnitsAccent(w: number, d: number, wallH: number): MergePart[] {
  const seed = `${w.toFixed(3)}-${d.toFixed(3)}-${wallH.toFixed(3)}`;
  const cnt = 2 + (hashStr(`ac-cnt-${seed}`) % 2); // 2 或 3
  const out: MergePart[] = [];
  for (let i = 0; i < cnt; i++) {
    const h = hashStr(`ac-${seed}-${i}`);
    const fy = ((h % 1000) / 1000) * 0.6 + 0.2; // 0.20..0.80 of wall height
    const fx = (((h >>> 10) % 1000) / 1000 - 0.5) * 0.6; // ±0.30 of w
    out.push(boxPart(u(0.5), u(0.35), u(0.25), fx * w, fy * wallH, d / 2 + u(0.12), AC_COLOR));
  }
  return out;
}

/** 电梯机房 + 擦窗机轨道（02 §3.2 · LiftRoom）@ y。 */
function liftRoomAccent(w: number, d: number, y: number): MergePart[] {
  const rw = w * 0.3;
  const rh = u(2.4);
  const rd = d * 0.3;
  const trackT = u(0.06);
  const margin = u(0.15);
  return [
    boxPart(rw, rh, rd, 0, y + rh / 2, 0, LIFT_COLOR),
    boxPart(rw + 2 * margin, trackT, trackT, 0, y + rh + trackT / 2, +(rd / 2 + margin + trackT / 2), FRAME_COLOR),
    boxPart(rw + 2 * margin, trackT, trackT, 0, y + rh + trackT / 2, -(rd / 2 + margin + trackT / 2), FRAME_COLOR),
    boxPart(trackT, trackT, rd + 2 * margin, +(rw / 2 + margin + trackT / 2), y + rh + trackT / 2, 0, FRAME_COLOR),
    boxPart(trackT, trackT, rd + 2 * margin, -(rw / 2 + margin + trackT / 2), y + rh + trackT / 2, 0, FRAME_COLOR),
  ];
}

/** 裙楼露台栏杆（02 §3.2 · PodiumRail）：立柱每 u(1.5) + 顶部 4 扶手 @ y。 */
function podiumRailAccent(w: number, d: number, y: number): MergePart[] {
  const stW = u(0.08);
  const stH = u(1.1);
  const stD = u(0.08);
  const spacing = u(1.5);
  const halfW = w / 2;
  const halfD = d / 2;
  const out: MergePart[] = [];
  for (let s = -halfW + spacing / 2; s <= halfW - spacing / 2; s += spacing) {
    out.push(boxPart(stW, stH, stD, s, y + stH / 2, +halfD, FRAME_COLOR));
    out.push(boxPart(stW, stH, stD, s, y + stH / 2, -halfD, FRAME_COLOR));
  }
  for (let s = -halfD + spacing / 2; s <= halfD - spacing / 2; s += spacing) {
    out.push(boxPart(stW, stH, stD, +halfW, y + stH / 2, s, FRAME_COLOR));
    out.push(boxPart(stW, stH, stD, -halfW, y + stH / 2, s, FRAME_COLOR));
  }
  const handT = u(0.04);
  const handY = y + stH + handT / 2;
  out.push(boxPart(w, handT, handT, 0, handY, +halfD, FRAME_COLOR));
  out.push(boxPart(w, handT, handT, 0, handY, -halfD, FRAME_COLOR));
  out.push(boxPart(handT, handT, d, +halfW, handY, 0, FRAME_COLOR));
  out.push(boxPart(handT, handT, d, -halfW, handY, 0, FRAME_COLOR));
  return out;
}

/** 底商雨棚（14 阶段 I · Shopfront 雨棚件；灯带另入墙体 glow 组）。 */
function shopfrontAwningAccent(w: number, d: number, y: number): MergePart {
  const awningD = u(1.2);
  return boxPart(w * 0.9, u(0.35), awningD, 0, y, -(d / 2 + awningD * 0.5), AWNING_COLOR);
}

/** 底商暖光灯带（原 Shopfront 灯带，emissive min(0.45, e*1.2)）。 */
function shopfrontGlow(w: number, d: number, y: number, mat: number): GroupedMergePart {
  const awningD = u(1.2);
  return { geo: new THREE.BoxGeometry(w * 0.85, u(0.18), u(0.06)), x: 0, y: y - u(0.25), z: -(d / 2 + awningD * 0.95), mat };
}

/** 广告牌（14 阶段 I：双杆 @ y + 发光面板；辉光 pointLight 由 Shape 组件挂）。 */
function billboardParts(w: number, y: number, panelMat: number): { accent: MergePart[]; panel: GroupedMergePart; panelTopY: number } {
  const panelW = w * 0.5;
  const poleH = u(2.0);
  return {
    accent: [
      cylPart(u(0.06), u(0.06), poleH, 8, -panelW * 0.35, y + poleH / 2, 0, BILLBOARD_POLE),
      cylPart(u(0.06), u(0.06), poleH, 8, +panelW * 0.35, y + poleH / 2, 0, BILLBOARD_POLE),
    ],
    panel: { geo: new THREE.BoxGeometry(panelW, u(1.4), u(0.08)), x: 0, y: y + poleH * 0.7, z: -u(0.1), mat: panelMat },
    panelTopY: y + poleH * 0.7,
  };
}

/** 阳台线（阶段 N + 18-Y 四面）：每 u(3) 一道 × 4 面（跳过底层/顶层）。 */
function balconyAccent(w: number, d: number, h: number): MergePart[] {
  const lineSpacing = u(3);
  const count = Math.floor(h / lineSpacing);
  const out: MergePart[] = [];
  for (let i = 0; i < count; i++) {
    const y = i * lineSpacing + lineSpacing / 2 + u(1.5);
    if (y < u(3.5)) continue;          // 跳过底层（贴底商雨棚）
    if (y >= h - u(1)) continue;       // 跳过顶层（贴屋顶）
    const lineT = u(0.04);
    const lineD = u(0.15);
    const off = u(0.02);
    out.push(boxPart(w * 0.95, lineT, lineD, 0, y, d / 2 + off, BALCONY_COLOR));
    out.push(boxPart(w * 0.95, lineT, lineD, 0, y, -(d / 2 + off), BALCONY_COLOR));
    out.push(boxPart(lineD, lineT, d * 0.95, w / 2 + off, y, 0, BALCONY_COLOR));
    out.push(boxPart(lineD, lineT, d * 0.95, -(w / 2 + off), y, 0, BALCONY_COLOR));
  }
  return out;
}

/** 屋顶设备（阶段 N：确定性 1-2 件，w>1.5 分支）。 */
function rooftopAccent(w: number, d: number, y: number): MergePart[] {
  const baseY = y + u(0.05);
  if (w > 1.5) {
    return [
      cylPart(u(0.15), u(0.15), u(0.3), 8, w * 0.3, baseY + u(0.15), d * 0.3, ROOFTOP_TANK),
      cylPart(u(0.08), u(0.08), u(0.35), 6, -w * 0.3, baseY + u(0.18), -d * 0.3, ROOFTOP_VENT),
    ];
  }
  return [
    cylPart(u(0.06), u(0.06), u(0.3), 6, 0, baseY + u(0.15), 0, ROOFTOP_VENT),
    boxPart(u(0.2), u(0.06), u(0.2), w * 0.25, baseY + u(0.04), 0, ROOFTOP_SKY),
  ];
}

/** 厂房卷帘门（阶段 N；批次 28 A2 横纹已并件）@ z=+d/2。 */
function shutterAccent(w: number, h: number, d: number): MergePart[] {
  const out: MergePart[] = [
    // 卷帘门主体 plane（原 planeGeometry [w*0.6, h*0.4] @ z=d/2+0.001）
    { geo: new THREE.PlaneGeometry(w * 0.6, h * 0.4), x: 0, y: h * 0.2, z: d / 2 + 0.001, color: SHUTTER_COLOR },
  ];
  for (const t of [0.15, 0.25, 0.35, 0.45]) {
    out.push(boxPart(w * 0.6, u(0.02), 0.004, 0, h * t, d / 2 + 0.002, BALCONY_COLOR));
  }
  return out;
}

/** 厂房侧高窗带（02 §3.2 · ShedWindowBands）：+Z 面 2 条。 */
function windowBandAccent(w: number, d: number, h: number): MergePart[] {
  const bandW = w * 1.2 * 0.9;
  return [
    boxPart(bandW, u(0.6), u(0.06), 0, h * 0.55, d / 2 + 0.003, WINBAND_COLOR),
    boxPart(bandW, u(0.6), u(0.06), 0, h * 0.75, d / 2 + 0.003, WINBAND_COLOR),
  ];
}

/** 烟囱（ShedShape：1-2 根细圆柱 @ -d*0.2）。 */
function chimneyAccent(cx: number, bH: number, chimneyH: number, d: number): MergePart {
  return cylPart(u(0.8), u(0.9), chimneyH, 10, cx, bH + chimneyH / 2, -d * 0.2, GABLE_COLOR);
}

/** 锯齿屋顶（02 §3.2 · SawtoothRoof）：3 齿 prism（deck 组）+ 3 采光带 plane（glass 组）。 */
function sawtoothParts(w: number, d: number, y: number, deckMat: number, glassMat: number): {
  deck: GroupedMergePart[]; glass: GroupedMergePart[];
} {
  const teeth = 3;
  const toothW = w / teeth;
  const roofH = u(1.2);
  const half = toothW / 2; // 半基
  const slopeLen = Math.hypot(half, roofH);
  const slopeAngle = Math.atan2(roofH, half);
  const deck: GroupedMergePart[] = [];
  const glass: GroupedMergePart[] = [];
  for (let i = 0; i < teeth; i++) {
    const toothX = (i - 1) * toothW; // -toothW, 0, +toothW
    deck.push({ geo: prismGeometry(toothW, roofH, d), x: toothX, y, z: 0, mat: deckMat });
    // 采光带：先绕 X 放平再绕 Z 抬到坡角（原嵌套 group rotation 的复合矩阵）
    const q = new THREE.Quaternion()
      .setFromEuler(new THREE.Euler(0, 0, slopeAngle))
      .multiply(new THREE.Quaternion().setFromEuler(new THREE.Euler(-Math.PI / 2, 0, 0)));
    glass.push({
      geo: new THREE.PlaneGeometry(slopeLen, d),
      matrix: new THREE.Matrix4().compose(
        new THREE.Vector3(toothX - half / 2, y + roofH / 2, 0),
        q,
        new THREE.Vector3(1, 1, 1),
      ),
      mat: glassMat,
    });
  }
  return { deck, glass };
}

// ── 五种体块的**纯几何构造器**（批次 30 A2 区级合并改造）──────────────
// 原 5 个 React 组件（TowerShape 等）+ MergedBuilding 退役：几何构造抽为纯函数，
// 同城区多栋楼共用一份材质表（matSpecs 一致 ⇒ mergeGrouped 分组数 = 材质数），
// 由 DistrictBuildings 合并为「每区 1 墙体 mesh + 1 点缀 mesh」（DC ≈ 材质组数 + 1）。
// 点缀/墙体几何与原组件逐件全等（仅去 React 包装）。

/** tower：裙楼 + 塔身 + 楼冠/玻璃门/灯带/广告牌 + 点缀件。 */
export function buildTowerParts(w: number, d: number, h: number, emissive: number): BuildingParts {
  const pH = Math.min(u(10), h * 0.35);
  const cH = Math.min(u(6), h * 0.22);
  const bodyH = Math.max(h - pH - cH, u(3)); // 塔身至少 1 层
  const shopY = Math.min(u(3.6), pH * 0.5);
  const hasBillboard = w > 1.4;

  // 材质表：0 裙楼侧(facadeBase) / 1 裙楼顶(纯色) / 2 塔身侧(facadeMid) / 3 塔身顶(roof) /
  // 4 楼冠 / 5 玻璃门 / 6 灯带 / 7 广告牌面板（hasBillboard 才有）
  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'top', roof: false }, { kind: 'sideB' }, { kind: 'top', roof: true },
    { kind: 'crown' }, { kind: 'glass', c: GLASS_DOOR },
    { kind: 'glow', intensity: Math.min(0.45, emissive * 1.2), rough: 0.3, metal: 0 },
  ];
  if (hasBillboard) matSpecs.push({ kind: 'glow', intensity: 0.5, rough: 0.4, metal: 0.4 });

  const wall: GroupedMergePart[] = [
    // 裙楼（商业基座，facadeBase 四面；几何 = 原 BoxSolid pod box 三组面）
    { geo: boxFaces(w, pH, d, 0, pH / 2, 0, 'A'), mat: 0 },
    { geo: boxFaces(w, pH, d, 0, pH / 2, 0, 'B'), mat: 0 },
    { geo: boxFaces(w, pH, d, 0, pH / 2, 0, 'top'), mat: 1 },
    // 塔身（facadeMid 四面 + roof 顶面）
    { geo: boxFaces(w * 0.8, bodyH, d * 0.8, 0, pH + bodyH / 2, 0, 'A'), mat: 2 },
    { geo: boxFaces(w * 0.8, bodyH, d * 0.8, 0, pH + bodyH / 2, 0, 'B'), mat: 2 },
    { geo: boxFaces(w * 0.8, bodyH, d * 0.8, 0, pH + bodyH / 2, 0, 'top'), mat: 3 },
    // 顶部收分（原独立 crown mesh）
    { geo: new THREE.BoxGeometry(w * 0.55, cH, d * 0.55), x: 0, y: pH + bodyH + cH / 2, z: 0, mat: 4 },
    // 入口玻璃门
    { ...entranceGlass(d), mat: 5 },
    // 底商灯带
    shopfrontGlow(w, d, shopY, 6),
  ];
  const accent: MergePart[] = [
    ...parapetAccent(w, d, pH),
    ...acUnitsAccent(w * 0.8, d * 0.8, bodyH + pH),
    ...podiumRailAccent(w, d, pH),
    ...entranceAccent(d),
    shopfrontAwningAccent(w, d, shopY),
    ...balconyAccent(w * 0.8, d * 0.8, bodyH + pH),
    ...rooftopAccent(w * 0.55, d * 0.55, pH + bodyH + cH),
  ];
  if (w > 1.2) accent.push(...liftRoomAccent(w * 0.55, d * 0.55, pH + bodyH + cH));
  let lightY = 0;
  if (hasBillboard) {
    const bb = billboardParts(w, pH + bodyH + cH, 7);
    accent.push(...bb.accent);
    wall.push(bb.panel);
    lightY = bb.panelTopY;
  }
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: lightY };
}

/** slab：单 box（侧A/侧B/顶 3 group）+ 檐口/雨棚/灯带/阳台/屋顶件/构造件。 */
export function buildSlabParts(w: number, d: number, h: number, emissive: number): BuildingParts {
  const shopY = Math.min(u(3.6), h * 0.3);
  const hasQuoins = w > 1.4;

  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'sideB' }, { kind: 'top', roof: true },
    { kind: 'glass', c: GLASS_DOOR },
    { kind: 'glow', intensity: Math.min(0.45, emissive * 1.2), rough: 0.3, metal: 0 },
  ];

  const wall: GroupedMergePart[] = [
    { geo: boxFaces(w, h, d, 0, h / 2, 0, 'A'), mat: 0 },
    { geo: boxFaces(w, h, d, 0, h / 2, 0, 'B'), mat: 1 },
    { geo: boxFaces(w, h, d, 0, h / 2, 0, 'top'), mat: 2 },
    { ...entranceGlass(d), mat: 3 },
    shopfrontGlow(w, d, shopY, 4),
  ];
  const accent: MergePart[] = [
    // 檐口条（0.05 高深色压顶线，契约 §2.2）
    boxPart(w + 0.08, 0.05, d + 0.08, 0, h + 0.025, 0, CORNICE_COLOR),
    shopfrontAwningAccent(w, d, shopY),
    ...balconyAccent(w, d, h),
    ...rooftopAccent(w, d, h),
    ...parapetAccent(w, d, h),
    ...acUnitsAccent(w, d, h),
    ...entranceAccent(d),
  ];
  if (hasQuoins) accent.push(...quoinAccent(w, d, h));
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** house：box 主体 + 三棱柱坡屋顶（prism group）+ 入口/角柱（点缀）。 */
export function buildHouseParts(w: number, d: number, h: number, emissive: number): BuildingParts {
  void emissive; // house 材质表无 glow 组（emissive 只经 ctx 传入侧墙/屋顶）
  const bodyH = h * 0.7;
  const roofH = h * 0.3;
  const hasQuoins = w > 1.4;

  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'sideB' }, { kind: 'top', roof: false },
    { kind: 'prism', fallback: ROOF_TILE_COLOR, stem: 'tile_roof' },
    { kind: 'glass', c: GLASS_DOOR },
  ];

  const wall: GroupedMergePart[] = [
    { geo: boxFaces(w, bodyH, d, 0, bodyH / 2, 0, 'A'), mat: 0 },
    { geo: boxFaces(w, bodyH, d, 0, bodyH / 2, 0, 'B'), mat: 1 },
    { geo: boxFaces(w, bodyH, d, 0, bodyH / 2, 0, 'top'), mat: 2 },
    // 坡屋顶（w+0.12 外扩，几何 = 原 PrismRoof）
    { geo: prismGeometry(w + 0.12, roofH, d + 0.12), x: 0, y: bodyH, z: 0, mat: 3 },
    { ...entranceGlass(d), mat: 4 },
  ];
  const accent: MergePart[] = [...entranceAccent(d)];
  if (hasQuoins) accent.push(...quoinAccent(w, d, bodyH));
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** shed：大跨 box + 卷帘门 + 高窗带 + 锯齿顶/人字顶 + 烟囱。 */
export function buildShedParts(w: number, d: number, h: number, emissive: number): BuildingParts {
  void emissive;
  const bw = w * 1.2;
  const bH = h * 0.8;
  const chimneyH = h * 0.5;
  const useSaw = w > 1.4;

  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'sideB' }, { kind: 'top', roof: false },
    useSaw ? { kind: 'deck' } : { kind: 'prism', fallback: GABLE_COLOR, stem: 'metal_deck' },
  ];
  if (useSaw) matSpecs.push({ kind: 'glass', c: GLASS_SKY });

  const wall: GroupedMergePart[] = [
    { geo: boxFaces(bw, bH, d, 0, bH / 2, 0, 'A'), mat: 0 },
    { geo: boxFaces(bw, bH, d, 0, bH / 2, 0, 'B'), mat: 1 },
    { geo: boxFaces(bw, bH, d, 0, bH / 2, 0, 'top'), mat: 2 },
  ];
  const accent: MergePart[] = [
    ...shutterAccent(bw, bH, d),
    ...windowBandAccent(w, d, bH),
  ];
  if (useSaw) {
    // 锯齿顶：3 齿 deck + 3 采光带 glass
    const saw = sawtoothParts(bw, d, bH, 3, 4);
    wall.push(...saw.deck, ...saw.glass);
    // 锯齿顶时屋檐加女儿墙作檐口收边（人字顶与坡屋顶几何冲突，跳过）
    accent.push(...parapetAccent(bw, d, bH));
  } else {
    // 人字顶（几何 = 原 PrismRoof）
    wall.push({ geo: prismGeometry(bw, h * 0.2, d), x: 0, y: bH, z: 0, mat: 3 });
  }
  // 烟囱 1-2 根：由占地宽确定性决定（不引随机源，同楼同形）
  const chimneys = w > 1.5 ? [-bw * 0.25, bw * 0.25] : [bw * 0.25];
  for (const cx of chimneys) accent.push(chimneyAccent(cx, bH, chimneyH, d));
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** pavilion：低矮平顶 box + 大挑檐（点缀）。 */
export function buildPavilionParts(w: number, d: number, h: number, emissive: number): BuildingParts {
  void emissive;
  const bH = Math.min(h, u(9));
  const matSpecs: WallMatSpec[] = [{ kind: 'sideA' }, { kind: 'top', roof: true }];
  const wall: GroupedMergePart[] = [
    { geo: boxFaces(w, bH, d, 0, bH / 2, 0, 'A'), mat: 0 },
    { geo: boxFaces(w, bH, d, 0, bH / 2, 0, 'B'), mat: 0 },
    { geo: boxFaces(w, bH, d, 0, bH / 2, 0, 'top'), mat: 1 },
  ];
  // 大挑檐（外扩 0.15，木色）
  const accent: MergePart[] = [boxPart(w + 0.3, 0.04, d + 0.3, 0, bH + 0.02, 0, EAVE_COLOR)];
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** archetype → 纯构造器分发表（塔楼高度不足时降级 slab，契约 §7 回退原则）。 */
export function buildBuildingParts(
  archetype: BuildingArchetype,
  w: number,
  d: number,
  h: number,
  emissive: number,
): BuildingParts {
  if (archetype === 'tower' && h < u(16)) return buildSlabParts(w, d, h, emissive);
  switch (archetype) {
    case 'tower': return buildTowerParts(w, d, h, emissive);
    case 'house': return buildHouseParts(w, d, h, emissive);
    case 'shed': return buildShedParts(w, d, h, emissive);
    case 'pavilion': return buildPavilionParts(w, d, h, emissive);
    case 'slab':
    default: return buildSlabParts(w, d, h, emissive);
  }
}
