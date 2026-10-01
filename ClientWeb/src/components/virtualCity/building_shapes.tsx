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
 * 批次 37 · P0/P1（3D 资产贴图匹配与渲染保真）：盒面 UV 不再恒 0..1。
 *   - 引擎层新增 `engine3d/boxFacesUV`（物理尺寸 UV 投影，缺省参数与 boxFaces 逐位一致）；
 *   - 本模块的侧 A / 侧 B / 顶三组盒面统一经 `wallBox()` 出口，按 `facadeTiles` 开关二选一：
 *       true  → 物理 UV（6 m×12 m 开间 / 10 m×10 m 屋顶周期）⇒ 各向异性 = 1、零变形；
 *       false → 原 boxFaces 0..1（降级态 2/3，**几何逐位不变**，零回归）；
 *   - 贴图包裹（`wrap: 'repeat'`）由 DistrictBuildings 依同一开关统一切换；
 *   - prism 坡屋顶 / glass / glow / crown / 点缀 mesh 一律不动（自构几何或纯色，无贴图 UV 问题）。
 *
 * 技术要点：
 *   - 立面贴图分配（批次 39 C3 修正）：**含首层商业的 facadeBase 挂「临街面」** ——
 *     `BuildingSpec.streetAxis`/`streetSign` 给出临街法向轴与朝向，单层体块
 *     （slab / house / shed）据此把 facadeBase 挂到 ±X 或 ±Z（此前恒挂 ±X ⇒
 *     一半的楼首层商业朝街区内部，B9）；塔楼裙楼四面 facadeBase、塔身四面
 *     facadeMid（分界在体块而非面轴，无需换面）；pavilion 两面均 facadeBase。
 *     `matSpecs` 顺序恒定，只换「面类 → 材质索引」映射，故不产生第二种材质签名。
 *   - **emissive 只属于「窗」**（批次 39）：
 *       · 侧墙 `emissiveMap` = 独立亮窗遮罩 `facade_tiles/<family>_lit.png`（只有窗玻璃非黑）；
 *         遮罩缺失 ⇒ emissiveIntensity = 0（安全降级，**不回退 albedo**）；
 *       · 顶面 / 坡屋顶**完全无自发光**（`topMatProps` / `prism` 已删三项 emissive）。
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
  boxFacesUV,
  boxPart,
  cylPart,
} from '@/engine3d';
import { FACADE_UV, ROOF_UV } from './texScale';

// ── 批次 37 · P0/P1：盒面物理 UV 接线（三级降级链的引擎侧入口）─────────

/**
 * 盒面几何 + 物理尺寸 UV 的唯一入口（批次 37 P0）。
 *
 * `facadeTiles = false`（降级态 2/3：缺 `facade_tiles/` 资产）时**返回与旧
 * `boxFaces` 逐位一致**的几何（UV 恒 0..1）—— 故新接线对存量城区零回归；
 * `facadeTiles = true`（降级态 1：开间贴图就位）时返回物理尺寸 UV 投影几何，
 * 调用方须配套 `wrap: 'repeat'`（由 DistrictBuildings 依同一开关统一切换）。
 *
 * 顶面用 `ROOF_UV`（10 m × 10 m 正方形周期）⇒ 相邻楼栋屋顶纹素密度一致；
 * 侧墙用 `FACADE_UV`（6 m × 12 m）⇒ 侧 A(面宽 d) 与侧 B(面宽 w) 纹素密度相等，
 * 转角处窗格对得上（旧 0..1 映射下两面差 1.55x，缺陷 D2）。
 *
 * @param uvOffset 批次 39 B1：**立面**UV 相位偏移，单位 = 贴图周期数（整周期）。
 *   只对 `cls` 为 'A'/'B' 的侧墙生效（'top' 是屋面，走 ROOF_UV 另一套周期，不打散）。
 *   偏移量取**整数**周期 ⇒ RepeatWrapping 下平移后仍然无缝，且纹素密度/各向异性
 *   完全不变（只换相位）。见 `districtFacadeUvOffset`。
 */
export function wallBox(
  w: number, h: number, d: number,
  x: number, y: number, z: number,
  cls: 'A' | 'B' | 'top',
  facadeTiles: boolean,
  uvOffset?: FacadeUvOffset,
): THREE.BufferGeometry {
  const geo = !facadeTiles
    ? boxFaces(w, h, d, x, y, z, cls)
    : boxFacesUV(w, h, d, x, y, z, cls, cls === 'top' ? ROOF_UV : FACADE_UV);
  // ⚠ 只在物理 UV 管线（facadeTiles = true，贴图以 `wrap: 'repeat'` 加载）下打散：
  //   降级态（facadeTiles = false）的贴图是 **clamp** 包裹的整栋立面图，UV 恒 0..1，
  //   平移后会被 clamp 拉成边缘像素 = 整面糊成一片。
  if (facadeTiles && uvOffset && cls !== 'top') applyUvOffset(geo, uvOffset);
  return geo;
}

/**
 * 批次 39 B1：立面 UV 相位偏移（单位 = 贴图周期数）。
 * 消费方 `DistrictBuildings::districtFacadeUvOffset` 派生出**逐城区确定性**取值。
 */
export interface FacadeUvOffset {
  /** U 方向周期数偏移（横向）。 */
  u: number;
  /** V 方向周期数偏移（纵向）。 */
  v: number;
}

/** 就地平移 uv 属性（批次 39 B1；只动 uv，position/normal 逐位不变）。 */
function applyUvOffset(geo: THREE.BufferGeometry, off: FacadeUvOffset): void {
  const uv = geo.getAttribute('uv');
  if (!uv) return;
  for (let i = 0; i < uv.count; i++) {
    uv.setXY(i, uv.getX(i) + off.u, uv.getY(i) + off.v);
  }
  uv.needsUpdate = true;
}

/**
 * 批次 39 B1：逐城区**立面 UV 相位**（由城区 id hash 派生，确定性、零随机源）。
 *
 * 为什么必须逐城区：同一张 `facade_tiles/<family>_lit.png` 被同族多个城区复用
 * （32 城区 → 16 材质族），不平移的话 finance / riverside / fin_sub_center 的
 * 亮窗分布**完全一样**（B4）。
 *
 * 为什么打在**几何 UV** 上而不是 `texture.offset`：
 *   - 贴图经 `engine3d/textureCache` 按 url 共享，同族的 `map` 是**同一个
 *     THREE.Texture 实例** —— 在其上写 `.offset` 是全局副作用，会被最后挂载的
 *     城区覆盖（同族其余城区跟着一起变），拿不到「逐区打散」；
 *   - `texture.clone()` 可解，但 three r169 的 `WebGLTextures` 按 **Texture 对象**
 *     分配 GPU 存储（`source` 只共享图片解码、不共享 `__webglTexture`），
 *     32 区各克隆一份 = 立面贴图显存翻倍；
 *   - 几何 UV 是**逐区独立**的（区级合并本就是逐区构建），且同一 mesh 上
 *     `map` 与 `emissiveMap` 共用同一套 UV ⇒ 亮窗遮罩与立面窗格**天然对齐**，
 *     不必两边各配一次 offset。
 *
 * 取值：偏移量取**整数**周期（U ∈ {0,1,2}、V ∈ {0,1,2,3}），整周期平移在
 * RepeatWrapping 下无缝，纹素密度与各向异性完全不变，只换相位。
 */
export function districtFacadeUvOffset(districtId: string): FacadeUvOffset {
  const h = hashStr(`facade-uv-${districtId}`);
  return { u: h % 3, v: (h >>> 8) % 4 };
}

/**
 * 降级态 2 告警（dev-only，每个 stem + variant 打印**一次**；范式参照
 * engine3d/glbSizeGuard 的 dev-only 告警 + 模块级 Set 去重）。
 *
 * 场景：`facade_tiles/<stem>_{base,mid}.png` 缺失 ⇒ 回落到 `facades/` 整栋立面图 +
 * 0..1 UV，此时批次 37 §1.3 的各向异性变形（D1）**仍然存在**。这是**已知且被记录**
 * 的降级态，但必须可见 —— 静默降级正是本批要根除的失效模式。
 */
const FACADE_DEGRADE_WARNED = new Set<string>();

export function warnFacadeDegradeOnce(
  stem: string,
  variant: 'base' | 'mid',
  legacyUrl: string,
): void {
  if (!import.meta.env.DEV || legacyUrl === '') return;
  const key = `${stem}_${variant}`;
  if (FACADE_DEGRADE_WARNED.has(key)) return;
  FACADE_DEGRADE_WARNED.add(key);
  console.warn(
    `[building_shapes] 立面贴图降级（批次 37 降级态 2）：facade_tiles/${key}.png 缺失，` +
    `回落到整栋立面图 facades/${key}.png（${legacyUrl}）+ 0..1 UV，` +
    '该立面**仍有各向异性变形**（方案 §1.3 D1：裙楼横向 3.40x / 塔身纵向 1.61x / house 16.2x）。' +
    '补齐开间贴图（facade_tiles/）后自动升级到物理 UV + repeat，无需改代码。',
  );
}

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
const ROOFTOP_DECK = '#6f7683';   // 屋顶设备平台格栅（批次 39 A3）
const ROOFTOP_MAST = '#5d646f';   // 屋顶天线杆（批次 39 A3）

const CROWN_COLOR = '#3a4250';   // 塔楼收分金属
const CORNICE_COLOR = '#242a35'; // 板楼檐口
const ROOF_TILE_COLOR = '#8a4b3a'; // 坡屋顶红瓦兜底
const GABLE_COLOR = '#6b7280';   // 厂房山墙/烟囱
const EAVE_COLOR = '#3f3a33';    // 公园挑檐木色
const PLINTH_COLOR = '#71767e';  // 建筑基座 / 勒脚（批次 39 C5；比女儿墙略深，突出接地线）

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
  /**
   * 批次 37 P0/P1：是否使用**物理尺寸 UV**（`facade_tiles/` 开间贴图就位 = true）。
   * true ⇒ 盒面走 `boxFacesUV` + 贴图 `wrap: 'repeat'`（零变形）；
   * false ⇒ 沿用 `boxFaces` 0..1（降级态 2/3，行为与批次 37 之前逐位一致）。
   */
  facadeTiles?: boolean;
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

/**
 * 侧墙材质规格（批次 39 B1：emissiveMap 由 albedo 换为**独立亮窗遮罩**）。
 *
 * @param tex   立面 albedo（`facade_tiles/<family>_{base,mid}.png`）
 * @param lit   亮窗遮罩（`facade_tiles/<family>_lit.png`；缺失 = null ⇒ 完全不发光）。
 *              批次 39 之前 `emissiveMap: tex` ⇒ 整个墙面（窗间墙 / 贴图里画好的空调
 *              外机 / 晾衣绳）一起发光（B3）；且同材质族全城共用一张 albedo ⇒ 亮窗分布
 *              完全一致（B4）。遮罩图只有窗玻璃非黑，故「只有窗发光」。
 *              ⚠ 遮罩缺失时**不得**回退到 albedo（那正是本项要根除的缺陷），
 *              走 `emissiveIntensity: 0` 的安全降级 = 夜间无窗灯，而不是错误地整墙发光。
 */
function sideMatProps(
  tex: THREE.Texture | null,
  lit: THREE.Texture | null,
  fallback: string,
  emissive: number,
  tint: string,
  pbr?: SharedPBR,
) {
  const base = {
    map: tex ?? undefined,
    color: tex ? tint : fallback,
    // 遮罩自身带三档暖色 ⇒ emissive 取白，让遮罩色原样透出（省一套 uniform）；
    // 遮罩缺失时 emissiveIntensity = 0，emissive 颜色取什么都不上屏。
    emissive: lit ? '#ffffff' : (tex ? EMISSIVE_WINDOW : fallback),
    emissiveMap: lit ?? undefined,
    emissiveIntensity: lit ? emissive : 0,
    roughness: 0.7,
    metalness: 0.1,
    envMapIntensity: 0.5, // 16 · 阶段 R：天空环境反射（幕墙/立面）
  };
  // 18-X：贴图缺失时 matProps 自动为空对象，withPBR 安全降级回原 base。
  return withPBR(base, pbr ?? { map: null, normalMap: null, roughnessMap: null, matProps: {} });
}

/**
 * 顶面材质规格（批次 39 A2）。
 *
 * ⚠ **屋顶不发光**：`emissive` / `emissiveMap` / `emissiveIntensity` 三项已删除。
 * 批次 39 之前 `emissiveMap: tex`（= 屋面 albedo 自发光），夜景里全城屋顶连贴图上的
 * 占位水印一起发暖光（B2）。屋面是纯漫反射面，夜间只应靠月光/环境光变暗。
 */
function topMatProps(
  tex: THREE.Texture | null,
  fallback: string,
  pbr?: SharedPBR,
) {
  const base = {
    map: tex ?? undefined,
    color: tex ? '#ffffff' : fallback,
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
  /**
   * 批次 39 B1：立面**亮窗遮罩**（`facade_tiles/<family>_lit.png`），侧墙 emissiveMap
   * 的唯一来源。缺失（null）⇒ 侧墙 `emissiveIntensity = 0`（夜间无窗灯），
   * **不回退到 albedo**。着色器层面与 `map` 共用同一套几何 UV ⇒ 遮罩窗格永远
   * 与立面窗格对齐，无需另设 offset。
   */
  litMap?: THREE.Texture | null;
  /**
   * 批次 37：立面/屋顶贴图是否走物理尺寸 UV 管线（贴图以 `wrap: 'repeat'` 加载）。
   * 材质侧据此做**包裹一致性自检**（物理 UV 遇到 clamp 贴图 = 整面拉成边缘竖条），
   * 几何侧由 `build*Parts` 的同名开关决定 UV 投影（`wallBox`）。
   */
  facadeTiles?: boolean;
}

/**
 * 批次 37 包裹一致性自检（dev-only）：声明了物理 UV 却挂了 clamp 贴图时告警。
 * 这正是方案 §3.2 点名的失效模式（`v = 3.67` 被 clamp 成边缘像素 → 整面竖条）。
 */
function warnIfNotRepeat(tex: THREE.Texture | null, ctx: WallCtx, label: string): void {
  if (!import.meta.env.DEV || !ctx.facadeTiles || !tex) return;
  if (tex.wrapS !== THREE.RepeatWrapping || tex.wrapT !== THREE.RepeatWrapping) {
    console.warn(
      `[building_shapes] ${label}：facadeTiles=true（盒面走物理尺寸 UV，UV 可 > 1），` +
      '但贴图不是 RepeatWrapping ⇒ 多周期取值会被 clamp 成边缘像素，整面拉成竖条。' +
      '请检查 DistrictBuildings 的 useSharedTexture/useSharedPBR 是否随 facadeTiles 传 wrap:"repeat"。',
    );
  }
}

export function buildWallMaterial(spec: WallMatSpec, ctx: WallCtx): THREE.MeshStandardMaterial {
  switch (spec.kind) {
    case 'sideA':
      warnIfNotRepeat(ctx.facadeBase, ctx, '侧 A（facadeBase）');
      if (ctx.litMap) warnIfNotRepeat(ctx.litMap, ctx, '侧 A 亮窗遮罩（_lit）');
      return new THREE.MeshStandardMaterial(sideMatProps(ctx.facadeBase, ctx.litMap ?? null, ctx.fallbackColor, ctx.emissive, ctx.tint, ctx.pbrBase));
    case 'sideB':
      warnIfNotRepeat(ctx.facadeMid, ctx, '侧 B（facadeMid）');
      if (ctx.litMap) warnIfNotRepeat(ctx.litMap, ctx, '侧 B 亮窗遮罩（_lit）');
      return new THREE.MeshStandardMaterial(sideMatProps(ctx.facadeMid, ctx.litMap ?? null, ctx.fallbackColor, ctx.emissive, ctx.tint, ctx.pbrMid));
    case 'top':
      // roof=false 的体块（塔楼裙楼/house/shed 主体）顶面原就是纯色兜底（top={null}）
      if (spec.roof) warnIfNotRepeat(ctx.roofMap, ctx, '顶面（roofMap）');
      return new THREE.MeshStandardMaterial(
        topMatProps(spec.roof ? ctx.roofMap : null, ctx.fallbackColor, spec.roof ? ctx.roofPbr : undefined),
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
      // 批次 39 A5：坡屋顶**一律走 synth 瓦面/金属面**，不再吃平屋顶贴图。
      // 批次 39 之前 `eff = ctx.roofMap ? ctx.roofPbr : synth` ⇒ 红瓦色坡屋顶被贴上
      // 深灰卷材纹理（B15：`roofs/finance.png` 这类**平屋面**资产贴在 30° 斜面上）。
      // `roofMap` / `roofPbr` 自此只服务 `case 'top'`（平屋面）。
      // 同 A2：坡屋顶不发光（原 emissiveMap = 屋面 albedo 自发光），三项一并去掉。
      const synth = spec.stem === 'metal_deck' ? ctx.metalPbr : ctx.tilePbr;
      const base = {
        color: spec.fallback,
        roughness: 0.85,
        metalness: 0.05,
        envMapIntensity: 0.35,
      };
      return new THREE.MeshStandardMaterial({
        ...withPBR(base, synth ?? { map: null, normalMap: null, roughnessMap: null, matProps: {} }),
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

/** 女儿墙几何常数（批次 39 A3 提取：女儿墙 / 压顶线脚共用同一套口径）。 */
const PARAPET_T = u(0.25);   // 墙厚 0.25 m
const PARAPET_H = u(0.9);    // 墙高 0.9 m（真实规范 0.9~1.2 m）
const PARAPET_HALF = u(0.125); // 环心内缩（t/2）
/** 压顶线脚：女儿墙顶再加宽 `t + u(0.06)`、高 `u(0.08)` 的薄挑檐（批次 39 A3）。 */
const COPING_H = u(0.08);
const COPING_OVER = u(0.06);

/** 女儿墙（02 §3.2 · ParapetRing）：4 条 box @ y（原 mesh position=[0,y,0] 内 y=h/2）。 */
function parapetAccent(w: number, d: number, y: number): MergePart[] {
  const t = PARAPET_T;
  const h = PARAPET_H;
  const half = PARAPET_HALF;
  return [
    boxPart(w, h, t, 0, y + h / 2, +(d / 2 - half), CONCRETE_COLOR),
    boxPart(w, h, t, 0, y + h / 2, -(d / 2 - half), CONCRETE_COLOR),
    boxPart(t, h, d - 2 * half, +(w / 2 - half), y + h / 2, 0, CONCRETE_COLOR),
    boxPart(t, h, d - 2 * half, -(w / 2 - half), y + h / 2, 0, CONCRETE_COLOR),
  ];
}

/**
 * 压顶线脚（批次 39 A3）：女儿墙**顶面**再加一圈薄挑檐（ coping ），4 条。
 * 薄挑檐比墙身厚 `COPING_OVER`（两侧各挑出 0.06 m），比墙身高 `COPING_H`（0.08 m），
 * 坐在女儿墙顶上 —— 真实女儿墙的收头件，缺了它墙顶就是一道光板。
 */
function copingAccent(w: number, d: number, y: number): MergePart[] {
  const t = PARAPET_T + COPING_OVER;
  const half = PARAPET_HALF + COPING_OVER / 2;
  const cy = y + PARAPET_H + COPING_H / 2;
  return [
    boxPart(w + COPING_OVER, COPING_H, t, 0, cy, +(d / 2 - half), CONCRETE_COLOR),
    boxPart(w + COPING_OVER, COPING_H, t, 0, cy, -(d / 2 - half), CONCRETE_COLOR),
    boxPart(t, COPING_H, d - 2 * half, +(w / 2 - half), cy, 0, CONCRETE_COLOR),
    boxPart(t, COPING_H, d - 2 * half, -(w / 2 - half), cy, 0, CONCRETE_COLOR),
  ];
}

/** 女儿墙 + 压顶线脚（批次 39 A3：有女儿墙的顶面统一带收头）。 */
function parapetWithCoping(w: number, d: number, y: number): MergePart[] {
  return [...parapetAccent(w, d, y), ...copingAccent(w, d, y)];
}

// ── 批次 39 C3/C4：临街面定位（门 / 雨棚 / 底商 一律挂同一面）────────────

/**
 * 临街面：法向轴 + 朝向符号。布局层（`building_layout.BuildingSpec`）已知
 * 南北边楼临 ±Z、东西边楼临 ±X（C3），经 `buildBuildingParts` 透传下来。
 *
 * 批次 39 之前所有底层构件都硬编码在固定轴上：门 / 门框 / 门厅雨棚在 **+Z**、
 * 底商雨棚 / 灯带 / 厂房卷帘门 / 高窗带在 **−Z 或 +Z**，于是
 *   - 一半的楼「首层商业朝街区内部、背街面无门」（B9）；
 *   - 门与底商雨棚**永不在同一面**（B10）。
 * 现统一改为「按临街面定位 + 按墙高自适应」。
 */
interface Street {
  /** 临街面法向轴：'x' = ±X 面临街；'z' = ±Z 面临街。 */
  axis: 'x' | 'z';
  /** 朝向符号：+1 = 该轴正侧临街，−1 = 负侧临街。 */
  sign: 1 | -1;
}

/** 缺省临街面（与批次 39 之前的 +Z 门 / −Z 底商在**南北边楼**上等价）。 */
const STREET_DEFAULT: Street = { axis: 'z', sign: 1 };

/**
 * 临街面局部坐标 → 世界 (x, z)：`along` = 沿街墙方向（切向），
 * `out` = 沿法向朝街面外挑（恒为正，符号由 `sign` 决定）。
 */
function streetXZ(st: Street, along: number, out: number): [number, number] {
  return st.axis === 'z' ? [along, st.sign * out] : [st.sign * out, along];
}

/** 门 / 门厅雨棚高度随墙高自适应（批次 39 C4）。 */
function entranceHeight(wallH: number): number {
  return Math.min(u(2.4), 0.62 * wallH);
}

/** 门厅雨棚挂高随墙高自适应（批次 39 C4）。 */
function canopyHeight(wallH: number): number {
  return Math.min(u(2.6), 0.75 * wallH);
}

/**
 * 入口门厅框 + 雨棚（02 §3.2 · EntranceLobby；玻璃门另入墙体 glass 组）。
 *
 * 批次 39 C3/C4：整体挂到临街面；门高 `min(u(2.4), 0.62·墙高)`、
 * 雨棚挂高 `min(u(2.6), 0.75·墙高)` —— 郊区 2~3 m 的矮楼不再长出穿出屋顶的
 * 2.4 m 门与浮在檐口之上的雨棚（B11）。
 */
function entranceAccent(d: number, st: Street, wallH: number): MergePart[] {
  const glassW = u(1.4);
  const glassH = entranceHeight(wallH);
  const frameT = u(0.06);
  const out = d / 2 + 0.004;
  const [flx, flz] = streetXZ(st, -glassW / 2 + frameT / 2, out);
  const [frx, frz] = streetXZ(st, +glassW / 2 - frameT / 2, out);
  const [tlx, tlz] = streetXZ(st, 0, out);
  const [ctx, ctz] = streetXZ(st, 0, d / 2 + u(0.45));
  const canopyY = canopyHeight(wallH);
  return [
    boxPart(frameT, glassH, frameT, flx, glassH / 2, flz, FRAME_COLOR),
    boxPart(frameT, glassH, frameT, frx, glassH / 2, frz, FRAME_COLOR),
    boxPart(glassW + 2 * frameT, frameT, frameT, tlx, glassH - frameT / 2, tlz, FRAME_COLOR),
    // 雨棚 box [u(2.2), u(0.12), u(0.9)]（±X 临街时切向/法向互换）
    st.axis === 'z'
      ? boxPart(u(2.2), u(0.12), u(0.9), ctx, canopyY, ctz, AWNING_BLUE)
      : boxPart(u(0.9), u(0.12), u(2.2), ctx, canopyY, ctz, AWNING_BLUE),
  ];
}

/**
 * 把一张**面片**挂到临街面（宽 `w`、高 `h`、中心高 `y`、外挑 `out`）。
 *
 * `PlaneGeometry` 默认面朝 +Z；临街轴为 'x' 时必须绕 Y 转 ∓90°，否则面片与墙面
 * 平行（审计实测：转角/侧向楼出现「门是一条看不见的线」）。
 */
function facePlane(
  w: number, h: number, out: number, y: number, st: Street,
): Pick<MergePart, 'geo' | 'x' | 'y' | 'z' | 'matrix'> {
  const [x, z] = streetXZ(st, 0, out);
  const geo = new THREE.PlaneGeometry(w, h);
  if (st.axis === 'z') return { geo, x, y, z };
  const q = new THREE.Quaternion().setFromEuler(
    new THREE.Euler(0, st.sign > 0 ? Math.PI / 2 : -Math.PI / 2, 0),
  );
  return {
    geo,
    matrix: new THREE.Matrix4().compose(new THREE.Vector3(x, y, z), q, new THREE.Vector3(1, 1, 1)),
  };
}

/** 入口玻璃门 plane（原 EntranceLobby 玻璃门，材质参数 GLASS_DOOR）。 */
function entranceGlass(d: number, st: Street, wallH: number): GroupedMergePart {
  const glassH = entranceHeight(wallH);
  return { ...facePlane(u(1.4), glassH, d / 2 + 0.002, glassH / 2, st), mat: -1 };
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

/** 底商雨棚（14 阶段 I · Shopfront 雨棚件；灯带另入墙体 glow 组）。批次 39 C4 改挂临街面。 */
function shopfrontAwningAccent(w: number, d: number, y: number, st: Street): MergePart {
  const awningD = u(1.2);
  const [x, z] = streetXZ(st, 0, d / 2 + awningD * 0.5);
  return st.axis === 'z'
    ? boxPart(w * 0.9, u(0.35), awningD, x, y, z, AWNING_COLOR)
    : boxPart(awningD, u(0.35), d * 0.9, x, y, z, AWNING_COLOR);
}

/** 底商暖光灯带（原 Shopfront 灯带，emissive min(0.45, e*1.2)）。批次 39 C4 改挂临街面。 */
function shopfrontGlow(w: number, d: number, y: number, mat: number, st: Street): GroupedMergePart {
  const awningD = u(1.2);
  const [x, z] = streetXZ(st, 0, d / 2 + awningD * 0.95);
  const geo = st.axis === 'z'
    ? new THREE.BoxGeometry(w * 0.85, u(0.18), u(0.06))
    : new THREE.BoxGeometry(u(0.06), u(0.18), d * 0.85);
  return { geo, x, y: y - u(0.25), z, mat };
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

/**
 * 屋顶设备平台 + 检修马道（批次 39 A3）。
 *
 * 平台是一片 `w*0.6 × d*0.6` 的**格栅板**（5 根细条 + 2 条边梁拼出网格感，
 * 站在俯视机位能看出"这是一块格栅"而不是一块实心板），另加一条从平台西缘
 * 伸向屋面中心的窄检修马道。所有件走 `boxPart` 发射器 ⇒ 并进区级点缀 mesh，
 * 不新增 draw call。
 */
function rooftopPlatform(w: number, d: number, y: number): MergePart[] {
  const pw = w * 0.6;
  const pd = d * 0.6;
  const slatT = u(0.05);   // 格栅条厚 0.05 m
  const slatW = u(0.08);   // 格栅条宽 0.08 m
  const deckY = y + u(0.3); // 平台面高出屋面 0.3 m
  const out: MergePart[] = [];
  const n = 5;
  for (let i = 0; i < n; i++) {
    const x = -pw / 2 + (pw * (i + 0.5)) / n;
    out.push(boxPart(slatW, slatT, pd, x, deckY, 0, ROOFTOP_DECK));
  }
  // 平台边梁（沿 X 的两条边框，略高于格栅条）
  const beamT = u(0.09);
  out.push(boxPart(pw + u(0.12), slatT * 1.6, beamT, 0, deckY, +(pd / 2), FRAME_COLOR));
  out.push(boxPart(pw + u(0.12), slatT * 1.6, beamT, 0, deckY, -(pd / 2), FRAME_COLOR));
  // 检修马道：自平台 -Z 边沿伸向屋面边的窄板（宽 u(0.9)）。
  // 长度**按「平台边到女儿墙内缘」的净距**取，绝不越出屋面轮廓。
  const walkLen = Math.max((d / 2 - pd / 2) * 0.8, u(0.8));
  out.push(boxPart(u(0.9), slatT, walkLen, 0, deckY, -(pd / 2 + walkLen / 2), FRAME_COLOR));
  return out;
}

/**
 * 屋顶天线（批次 39 A3）：1 根 3~6 m 桅杆 + 2~3 段横杆。
 * 高度/横杆数由 **roof 尺寸 hash** 决定（§92a 布局确定性：禁 `Math.random`，
 * 同楼同形，跨帧/跨重载稳定）。桅杆立在**平台与女儿墙之间的环带**上（0.4w/0.4d），
 * 不与设备平台格栅互相穿插。
 */
function antennaMast(w: number, d: number, y: number): MergePart[] {
  const seed = hashStr(`antenna-${w.toFixed(3)}-${d.toFixed(3)}-${y.toFixed(3)}`);
  const h = u(3 + (seed % 4));        // 3~6 m
  const r = u(0.06);                 // 直径 0.12 m
  const cx = -w * 0.4;
  const cz = +d * 0.4;
  const baseY = y + u(0.05);
  const out: MergePart[] = [
    cylPart(r, r * 1.5, h, 6, cx, baseY + h / 2, cz, ROOFTOP_MAST),
  ];
  // 2~3 段横杆：自下而上等分，绕 Z 转 90° 变成水平横担（沿 X）
  const arms = 2 + ((seed >>> 5) % 2);
  for (let i = 0; i < arms; i++) {
    const ay = baseY + (h * (i + 1)) / (arms + 1);
    const al = u(0.5 + i * 0.35);
    out.push({ ...cylPart(r * 0.7, r * 0.7, al, 5, cx, ay, cz, FRAME_COLOR), rotZ: Math.PI / 2 });
  }
  return out;
}

/** 屋顶设备（阶段 N 基础件 + 批次 39 A3：设备平台 + 检修马道 + 天线）。 */
function rooftopAccent(w: number, d: number, y: number): MergePart[] {
  const baseY = y + u(0.05);
  if (w > 1.5) {
    return [
      // 水箱 / 通风管同样移到 0.4w·0.4d 的环带上（原 0.3w 恰好压在平台边缘）
      cylPart(u(0.15), u(0.15), u(0.3), 8, w * 0.4, baseY + u(0.15), d * 0.4, ROOFTOP_TANK),
      cylPart(u(0.08), u(0.08), u(0.35), 6, -w * 0.4, baseY + u(0.18), -d * 0.4, ROOFTOP_VENT),
      ...rooftopPlatform(w, d, y),
      ...antennaMast(w, d, y),
    ];
  }
  return [
    cylPart(u(0.06), u(0.06), u(0.3), 6, 0, baseY + u(0.15), 0, ROOFTOP_VENT),
    boxPart(u(0.2), u(0.06), u(0.2), w * 0.25, baseY + u(0.04), 0, ROOFTOP_SKY),
  ];
}

/**
 * 厂房卷帘门（阶段 N；批次 28 A2 横纹已并件）。批次 39 C4 改挂临街面（原固定 +Z）。
 *
 * @param bw 厂房体量 X 向尺寸（shed 为 `w*1.2`）
 * @param bd 厂房体量 Z 向尺寸（shed 为 `d`）
 * ⚠ 临街面距离随轴而变（'x' 面在 `bw/2`、'z' 面在 `bd/2`）—— 厂房是**扁体量**
 * （bw ≫ bd），沿用 d/2 会把门埋进墙体内部。
 */
function shutterAccent(bw: number, bH: number, bd: number, st: Street): MergePart[] {
  const faceAt = (st.axis === 'x' ? bw : bd) / 2;
  const alongW = (st.axis === 'x' ? bd : bw) * 0.6;  // 卷帘门宽度 = 临街面宽的 60%
  const out: MergePart[] = [
    // 卷帘门主体 plane（原 planeGeometry [w*0.6, h*0.4] @ 面外 0.001）
    { ...facePlane(alongW, bH * 0.4, faceAt + 0.001, bH * 0.2, st), color: SHUTTER_COLOR },
  ];
  for (const t of [0.15, 0.25, 0.35, 0.45]) {
    const [rx, rz] = streetXZ(st, 0, faceAt + 0.002);
    out.push(
      st.axis === 'z'
        ? boxPart(alongW, u(0.02), 0.004, rx, bH * t, rz, BALCONY_COLOR)
        : boxPart(0.004, u(0.02), alongW, rx, bH * t, rz, BALCONY_COLOR),
    );
  }
  return out;
}

/**
 * 厂房侧高窗带（02 §3.2 · ShedWindowBands）：临街面 2 条（批次 39 C4，原固定 +Z）。
 *
 * @param bandW 窗带宽度（调用方按临街面实际面宽裁剪，避免沿用固定 `w*1.2*0.9` 时
 *   在侧向楼（面宽 = bd ≪ bw）上横向出挑出墙体）。
 */
function windowBandAccent(bandW: number, bH: number, faceAt: number, st: Street): MergePart[] {
  return [0.55, 0.75].map((fy) => {
    const [x, z] = streetXZ(st, 0, faceAt + 0.003);
    return st.axis === 'z'
      ? boxPart(bandW, u(0.6), u(0.06), x, bH * fy, z, WINBAND_COLOR)
      : boxPart(u(0.06), u(0.6), bandW, x, bH * fy, z, WINBAND_COLOR);
  });
}

/**
 * 建筑基座 / 勒脚（批次 39 C5）：沿四边一圈 `u(0.35)` 高、`u(0.08)` 出挑的基座带。
 *
 * 现状缺陷（B20）：建筑 box 底面 y=0 而区底板顶面 `DISTRICT_SURFACE_Y = 0.010`
 * （10 cm）⇒ 基座被埋 10 cm，box 与地面之间没有任何过渡。基座带从 y=0 起
 * （**不下探**，绝不穿到区底板之下），露出 25 cm 勒脚 + 8 cm 出挑的阴影线。
 *
 * 4 件全部走 `boxPart` ⇒ 并入区级点缀 mesh，**不新增 draw call**。
 */
function plinthAccent(w: number, d: number): MergePart[] {
  const h = u(0.35);
  const over = u(0.08);
  const y = h / 2;
  return [
    boxPart(w + 2 * over, h, over, 0, y, +(d / 2 + over / 2), PLINTH_COLOR),
    boxPart(w + 2 * over, h, over, 0, y, -(d / 2 + over / 2), PLINTH_COLOR),
    boxPart(over, h, d, +(w / 2 + over / 2), y, 0, PLINTH_COLOR),
    boxPart(over, h, d, -(w / 2 + over / 2), y, 0, PLINTH_COLOR),
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

/**
 * 盒面工厂（批次 39 B1）：把「物理 UV + 逐区相位偏移」两件事收进一个闭包，
 * 5 个构造器共用。`cls === 'top'` 是屋面（ROOF_UV 另一套周期），不打散。
 */
function wallBoxFactory(facadeTiles: boolean, uvOffset?: FacadeUvOffset) {
  return (
    w: number, h: number, d: number,
    x: number, y: number, z: number,
    cls: 'A' | 'B' | 'top',
  ): THREE.BufferGeometry => wallBox(w, h, d, x, y, z, cls, facadeTiles, uvOffset);
}

/**
 * 批次 39 C3：临街轴 → 侧墙材质索引。
 *
 * `matSpecs` 的**顺序恒定不变**（[0]=sideA/facadeBase、[1]=sideB/facadeMid …），
 * 只交换「哪个面类用哪个索引」—— 否则同城区两种朝向的楼会产生**两种 matSpecs
 * 签名**，`DistrictBuildings` 会分裂成两个材质组（多一套材质 + 多一个 mesh）。
 * 面类本身也不动：`'A'` 恒 = ±X（面宽 d）、`'B'` 恒 = ±Z（面宽 w），
 * 物理 UV 密度因此保持不变，只换贴图语义。
 *
 * @param axis 临街面法向轴（布局层给出）：'z' ⇒ ±Z 面临街 ⇒ base 挂 `'B'` 面。
 */
function facadeMats(
  axis: 'x' | 'z',
  matBase: number,
  matMid: number,
): { mA: number; mB: number } {
  return axis === 'x' ? { mA: matBase, mB: matMid } : { mA: matMid, mB: matBase };
}

/** tower：裙楼 + 塔身 + 楼冠/玻璃门/灯带/广告牌 + 点缀件。 */
export function buildTowerParts(
  w: number, d: number, h: number, emissive: number, facadeTiles = false,
  uvOffset?: FacadeUvOffset,
  streetAxis: 'x' | 'z' = STREET_DEFAULT.axis,
  streetSign: 1 | -1 = STREET_DEFAULT.sign,
): BuildingParts {
  const pH = Math.min(u(10), h * 0.35);
  const cH = Math.min(u(6), h * 0.22);
  const bodyH = Math.max(h - pH - cH, u(3)); // 塔身至少 1 层
  const shopY = Math.min(u(3.6), pH * 0.5);
  const hasBillboard = w > 1.4;
  const st: Street = { axis: streetAxis, sign: streetSign };

  // 材质表：0 裙楼侧(facadeBase) / 1 裙楼顶(纯色) / 2 塔身侧(facadeMid) / 3 塔身顶(roof) /
  // 4 楼冠 / 5 玻璃门 / 6 灯带 / 7 广告牌面板（hasBillboard 才有）
  // 批次 39 C3：**塔楼不参与 base/mid 换面** —— 裙楼（= 首层商业基座）四面都用
  // facadeBase、塔身四面都用 facadeMid，base/mid 的分界在「裙楼 vs 塔身」而非面轴，
  // 临街面天然带商业首层（换面只对单层体块的 slab/house/shed 有意义）。
  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'top', roof: false }, { kind: 'sideB' }, { kind: 'top', roof: true },
    { kind: 'crown' }, { kind: 'glass', c: GLASS_DOOR },
    { kind: 'glow', intensity: Math.min(0.45, emissive * 1.2), rough: 0.3, metal: 0 },
  ];
  if (hasBillboard) matSpecs.push({ kind: 'glow', intensity: 0.5, rough: 0.4, metal: 0.4 });

  const wx = wallBoxFactory(facadeTiles, uvOffset);
  const wall: GroupedMergePart[] = [
    // 裙楼（商业基座，facadeBase 四面；几何 = 原 BoxSolid pod box 三组面）
    { geo: wx(w, pH, d, 0, pH / 2, 0, 'A'), mat: 0 },
    { geo: wx(w, pH, d, 0, pH / 2, 0, 'B'), mat: 0 },
    { geo: wx(w, pH, d, 0, pH / 2, 0, 'top'), mat: 1 },
    // 塔身（facadeMid 四面 + roof 顶面）
    { geo: wx(w * 0.8, bodyH, d * 0.8, 0, pH + bodyH / 2, 0, 'A'), mat: 2 },
    { geo: wx(w * 0.8, bodyH, d * 0.8, 0, pH + bodyH / 2, 0, 'B'), mat: 2 },
    { geo: wx(w * 0.8, bodyH, d * 0.8, 0, pH + bodyH / 2, 0, 'top'), mat: 3 },
    // 顶部收分（原独立 crown mesh）
    { geo: new THREE.BoxGeometry(w * 0.55, cH, d * 0.55), x: 0, y: pH + bodyH + cH / 2, z: 0, mat: 4 },
    // 入口玻璃门（批次 39 C3/C4：挂临街面 + 高度随裙墙自适应）
    { ...entranceGlass(d, st, pH), mat: 5 },
    // 底商灯带（与门 / 雨棚同面）
    shopfrontGlow(w, d, shopY, 6, st),
  ];
  const crownTopY = pH + bodyH + cH;
  const accent: MergePart[] = [
    // 批次 39 C5：基座带贴裙楼轮廓（裙楼才是接地体量）
    ...plinthAccent(w, d),
    ...parapetAccent(w, d, pH),
    ...acUnitsAccent(w * 0.8, d * 0.8, bodyH + pH),
    // 批次 39 A3.2：退台面护栏改挂**塔身**尺寸、抬到塔身顶（`pH + bodyH`）。
    // 旧口径 `podiumRailAccent(w, d, pH)` 与裙楼女儿墙 `parapetAccent(w, d, pH)`
    // 同尺寸同高度 ⇒ 两套构件在同一圈互相穿插（B30）。
    ...podiumRailAccent(w * 0.8, d * 0.8, pH + bodyH),
    ...entranceAccent(d, st, pH),
    shopfrontAwningAccent(w, d, shopY, st),
    ...balconyAccent(w * 0.8, d * 0.8, bodyH + pH),
    ...rooftopAccent(w * 0.55, d * 0.55, crownTopY),
    // 批次 39 A3.1：塔冠顶原是裸 BoxGeometry（顶面无任何收头，B12）⇒ 补女儿墙 + 压顶线脚。
    ...parapetWithCoping(w * 0.55, d * 0.55, crownTopY),
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
export function buildSlabParts(
  w: number, d: number, h: number, emissive: number, facadeTiles = false,
  uvOffset?: FacadeUvOffset,
  streetAxis: 'x' | 'z' = STREET_DEFAULT.axis,
  streetSign: 1 | -1 = STREET_DEFAULT.sign,
): BuildingParts {
  const shopY = Math.min(u(3.6), h * 0.3);
  const hasQuoins = w > 1.4;
  const st: Street = { axis: streetAxis, sign: streetSign };

  // 0 = sideA(facadeBase, 含首层商业) / 1 = sideB(facadeMid)
  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'sideB' }, { kind: 'top', roof: true },
    { kind: 'glass', c: GLASS_DOOR },
    { kind: 'glow', intensity: Math.min(0.45, emissive * 1.2), rough: 0.3, metal: 0 },
  ];
  // 批次 39 C3：base（含首层商业）挂临街面 —— 临 ±X 用原口径、临 ±Z 换面
  const { mA, mB } = facadeMats(streetAxis, 0, 1);

  const wx = wallBoxFactory(facadeTiles, uvOffset);
  const wall: GroupedMergePart[] = [
    { geo: wx(w, h, d, 0, h / 2, 0, 'A'), mat: mA },
    { geo: wx(w, h, d, 0, h / 2, 0, 'B'), mat: mB },
    { geo: wx(w, h, d, 0, h / 2, 0, 'top'), mat: 2 },
    { ...entranceGlass(d, st, h), mat: 3 },
    shopfrontGlow(w, d, shopY, 4, st),
  ];
  const accent: MergePart[] = [
    // 批次 39 C5：基座带（贴地勒脚，出挑 0.08 m）
    ...plinthAccent(w, d),
    // 檐口条（高 0.5 m 深色压顶线，两侧各挑出 0.4 m；契约 §2.2）。
    // 批次 39 A6：原为世界单位裸值 0.05 / 0.08 / 0.025（读作 0.5 m / 0.8 m / 0.25 m，
    // 数值恰好合理但语义是"把世界单位当米用"）⇒ 改走 u()，取值不变 ⇒ 零视觉回归。
    boxPart(w + u(0.8), u(0.5), d + u(0.8), 0, h + u(0.25), 0, CORNICE_COLOR),
    shopfrontAwningAccent(w, d, shopY, st),
    ...balconyAccent(w, d, h),
    ...rooftopAccent(w, d, h),
    // 批次 39 A3：slab 平屋面与塔冠统一带压顶线脚（真实城市所有平屋顶女儿墙顶都有收头，
    // 只给塔冠加会让两种平屋面一眼看出「只改了一处」）。
    ...parapetWithCoping(w, d, h),
    ...acUnitsAccent(w, d, h),
    ...entranceAccent(d, st, h),
  ];
  if (hasQuoins) accent.push(...quoinAccent(w, d, h));
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** house：box 主体 + 三棱柱坡屋顶（prism group）+ 入口/角柱（点缀）。 */
export function buildHouseParts(
  w: number, d: number, h: number, emissive: number, facadeTiles = false,
  uvOffset?: FacadeUvOffset,
  streetAxis: 'x' | 'z' = STREET_DEFAULT.axis,
  streetSign: 1 | -1 = STREET_DEFAULT.sign,
): BuildingParts {
  void emissive; // house 材质表无 glow 组（emissive 只经 ctx 传入侧墙/屋顶）
  const bodyH = h * 0.7;
  const roofH = h * 0.3;
  const hasQuoins = w > 1.4;
  const st: Street = { axis: streetAxis, sign: streetSign };

  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'sideB' }, { kind: 'top', roof: false },
    { kind: 'prism', fallback: ROOF_TILE_COLOR, stem: 'tile_roof' },
    { kind: 'glass', c: GLASS_DOOR },
  ];
  // 批次 39 C3：0 = sideA(facadeBase) / 1 = sideB(facadeMid)，base 挂临街面
  const { mA, mB } = facadeMats(streetAxis, 0, 1);

  const wx = wallBoxFactory(facadeTiles, uvOffset);
  const wall: GroupedMergePart[] = [
    { geo: wx(w, bodyH, d, 0, bodyH / 2, 0, 'A'), mat: mA },
    { geo: wx(w, bodyH, d, 0, bodyH / 2, 0, 'B'), mat: mB },
    { geo: wx(w, bodyH, d, 0, bodyH / 2, 0, 'top'), mat: 2 },
    // 坡屋顶（四周各外扩 1.2 m 出檐，批次 39 A6：原为世界单位裸值 0.12）
    { geo: prismGeometry(w + u(1.2), roofH, d + u(1.2)), x: 0, y: bodyH, z: 0, mat: 3 },
    { ...entranceGlass(d, st, bodyH), mat: 4 },
  ];
  const accent: MergePart[] = [
    ...plinthAccent(w, d),   // 批次 39 C5：基座带
    ...entranceAccent(d, st, bodyH),
  ];
  if (hasQuoins) accent.push(...quoinAccent(w, d, bodyH));
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** shed：大跨 box + 卷帘门 + 高窗带 + 锯齿顶/人字顶 + 烟囱。 */
export function buildShedParts(
  w: number, d: number, h: number, emissive: number, facadeTiles = false,
  uvOffset?: FacadeUvOffset,
  streetAxis: 'x' | 'z' = STREET_DEFAULT.axis,
  streetSign: 1 | -1 = STREET_DEFAULT.sign,
): BuildingParts {
  void emissive;
  const bw = w * 1.2;
  const bH = h * 0.8;
  const chimneyH = h * 0.5;
  const useSaw = w > 1.4;
  const st: Street = { axis: streetAxis, sign: streetSign };

  const matSpecs: WallMatSpec[] = [
    { kind: 'sideA' }, { kind: 'sideB' }, { kind: 'top', roof: false },
    useSaw ? { kind: 'deck' } : { kind: 'prism', fallback: GABLE_COLOR, stem: 'metal_deck' },
  ];
  if (useSaw) matSpecs.push({ kind: 'glass', c: GLASS_SKY });
  // 批次 39 C3：0 = sideA(facadeBase) / 1 = sideB(facadeMid)，base 挂临街面
  const { mA, mB } = facadeMats(streetAxis, 0, 1);

  const wx = wallBoxFactory(facadeTiles, uvOffset);
  const wall: GroupedMergePart[] = [
    { geo: wx(bw, bH, d, 0, bH / 2, 0, 'A'), mat: mA },
    { geo: wx(bw, bH, d, 0, bH / 2, 0, 'B'), mat: mB },
    { geo: wx(bw, bH, d, 0, bH / 2, 0, 'top'), mat: 2 },
  ];
  // 批次 39 C4：卷帘门 / 高窗带改挂临街面（原固定 +Z）。临街面距离随轴而变，
  // 窗带宽度按**实际面宽**裁剪（原 `w*1.2*0.9` 在侧向楼会横向出挑出墙体）。
  const faceAt = st.axis === 'x' ? bw / 2 : d / 2;
  const alongW = st.axis === 'x' ? d : bw;
  const bandW = Math.min(w * 1.2 * 0.9, alongW * 0.95);
  const accent: MergePart[] = [
    ...plinthAccent(bw, d),  // 批次 39 C5：基座带贴厂房轮廓（bw = w*1.2）
    ...shutterAccent(bw, bH, d, st),
    ...windowBandAccent(bandW, bH, faceAt, st),
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
export function buildPavilionParts(
  w: number, d: number, h: number, emissive: number, facadeTiles = false,
  uvOffset?: FacadeUvOffset,
  streetAxis: 'x' | 'z' = STREET_DEFAULT.axis,
  streetSign: 1 | -1 = STREET_DEFAULT.sign,
): BuildingParts {
  void emissive;
  void streetAxis;
  void streetSign;
  // 批次 39 C3：pavilion **两面都用 facadeBase**（mat 0），无 base/mid 之分 ⇒ 不换面；
  // 门前构件（门 / 雨棚 / 底商）pavilion 原本就没有，也不在本批新增。
  const bH = Math.min(h, u(9));
  const matSpecs: WallMatSpec[] = [{ kind: 'sideA' }, { kind: 'top', roof: true }];
  const wx = wallBoxFactory(facadeTiles, uvOffset);
  const wall: GroupedMergePart[] = [
    { geo: wx(w, bH, d, 0, bH / 2, 0, 'A'), mat: 0 },
    { geo: wx(w, bH, d, 0, bH / 2, 0, 'B'), mat: 0 },
    { geo: wx(w, bH, d, 0, bH / 2, 0, 'top'), mat: 1 },
  ];
  // 大挑檐（四周各挑出 3 m、厚 0.4 m，木色；批次 39 A6：原为世界单位裸值 0.3/0.04/0.02）
  // + 批次 39 C5：基座带
  const accent: MergePart[] = [
    ...plinthAccent(w, d),
    boxPart(w + u(3.0), u(0.4), d + u(3.0), 0, bH + u(0.2), 0, EAVE_COLOR),
  ];
  return { wallParts: wall, matSpecs, accentParts: accent, billboardLightY: 0 };
}

/** archetype → 纯构造器分发表（塔楼高度不足时降级 slab，契约 §7 回退原则）。 */
export function buildBuildingParts(
  archetype: BuildingArchetype,
  w: number,
  d: number,
  h: number,
  emissive: number,
  /** 批次 37：是否使用物理尺寸 UV（缺省 false = 与批次 37 之前逐位一致）。 */
  facadeTiles = false,
  /** 批次 39 B1：立面 UV 相位偏移（逐区打散；仅 facadeTiles=true 时有意义）。 */
  uvOffset?: FacadeUvOffset,
  /**
   * 批次 39 C3/C4：临街面法向轴（布局层 `BuildingSpec.streetAxis`）。
   * 缺省 `'z'` = 与批次 39 之前在**南北边楼**上等价；消费方 `DistrictBuildings`
   * 须传 `spec.streetAxis` / `spec.streetSign` 才会按实际街墙朝向换面。
   */
  streetAxis: 'x' | 'z' = STREET_DEFAULT.axis,
  /** 批次 39 C3/C4：临街面朝向符号（`BuildingSpec.streetSign`）。 */
  streetSign: 1 | -1 = STREET_DEFAULT.sign,
): BuildingParts {
  if (archetype === 'tower' && h < u(16)) {
    return buildSlabParts(w, d, h, emissive, facadeTiles, uvOffset, streetAxis, streetSign);
  }
  switch (archetype) {
    case 'tower': return buildTowerParts(w, d, h, emissive, facadeTiles, uvOffset, streetAxis, streetSign);
    case 'house': return buildHouseParts(w, d, h, emissive, facadeTiles, uvOffset, streetAxis, streetSign);
    case 'shed': return buildShedParts(w, d, h, emissive, facadeTiles, uvOffset, streetAxis, streetSign);
    case 'pavilion': return buildPavilionParts(w, d, h, emissive, facadeTiles, uvOffset, streetAxis, streetSign);
    case 'slab':
    default: return buildSlabParts(w, d, h, emissive, facadeTiles, uvOffset, streetAxis, streetSign);
  }
}
