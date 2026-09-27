/**
 * cityScale — 虚拟城市 3D 高度系统 + **度量衡唯一事实来源**（2026-09-21，批次 29 升格）。
 *
 * 世界标尺：**1 世界单位 = 10 米**。地图 120×120 = 1200m×1200m 城区
 * （v2.12 阶段 2：40×40 → 80×80，16 城区；批次 20：80×80 → 120×120，32 城区）。
 * 所有 3D 建筑 / 道具的「真实米制尺寸 → 世界单位」换算统一经 u()，
 * 禁止在组件内硬编码米制尺寸（方案：tmpPlan/虚拟城市-3D高度系统与贴图渲染优化方案-20260921.md）。
 *
 * 批次 29（GLB 尺寸口径统一）起，**真实米制尺寸表 REAL_DIMS_M 是本文件的核心**：
 *   - 每个 3D 物件的真实尺寸（米）与落地约定（minY）只有这一处；
 *   - **同物件的两条渲染路径共用同一行表值** —— Blender `.glb` 路径（美术按本表重导出）
 *     与程序化 fallback 路径（前端消费本表），从而"包围盒一致"成为可测不变量；
 *   - 表值经 worldDims() 取世界单位（消费端零 scale，批次 26 轴向/单位契约）；
 *   - 表值经 sizeTargetFor() 变成 engine3d/glbSizeGuard 的校验目标：GLB 载入后
 *     dev 态自动量测 Box3 并比对，偏差超 ±5% 即告警（见 engine3d/glbSizeGuard.ts）。
 *   ⚠️ 改本表 = 同时改「美术导出脚本 + GLB + 两条前端路径」的契约，**改动需三侧同步**。
 *
 * 表值口径（务必遵守，否则 GLB 与 fallback 会再次漂移）：
 *   - x/y/z = 物件**包围盒**尺寸（米）：车/树 = 长(X) × 总高(Y) × 宽(Z)；
 *     建筑 = 占地轮廓宽(X) × **总高含塔尖/屋顶(Y)** × 进深(Z)。建筑的 x/z 取
 *     **主体轮廓（含屋檐腰线，不含门廊/雨棚/台阶/旗杆等外挑物）**；
 *   - minY = 包围盒底面（米）：`0` = GLB 原点落在物件底面（轮底/脚底/桶底/建筑基座），
 *     即 `<Model>` 零补偿直挂时自动贴地；缺省 = 不做落地校验。
 */

import type { VirtualCityDistrictId } from '@/types/virtualCity';
import type { ModelSizeTarget } from '@/engine3d';

/** 世界标尺：1 单位 = 10 米。 */
export const METERS_PER_UNIT = 10;

/** 米 → 世界单位。 */
export const u = (meters: number): number => meters / METERS_PER_UNIT;

/** 层高：3 m/层（0.3 世界单位）。 */
export const FLOOR_HEIGHT_M = 3;

/** 路面标高（世界单位）：0.15 m 抬高避免与地面 z-fighting（Road.tsx ROAD_Y 消费）。 */
export const ROAD_SURFACE_Y = 0.015;

/** 车辆落位 y（世界单位）= 路面之上 5 cm（胎底贴路面又不 z-fighting）。 */
export const VEHICLE_GROUND_Y = ROAD_SURFACE_Y + 0.005;

/**
 * 真实米制尺寸表（单位 = 米）—— **虚拟城市 3D 物件尺寸的唯一事实来源**。
 *
 * 与 art-designer 重导出 GLB 时使用的同一张表；`satisfies` 保证字段名受约束。
 * 只填已知值（缺省字段 = 不做该轴校验）；**不要凭感觉改数值** ——
 * 与实测 fallback 不符时先报备（改动需 GLB / 脚本 / 前端三侧同步）。
 */
export interface RealDimsM {
  /** X 向尺寸（米）：车长 / 建筑宽 / 树冠宽。 */
  x?: number;
  /** Y 向尺寸（米）：总高。 */
  y?: number;
  /** Z 向尺寸（米）：车宽 / 建筑进深 / 树冠深。 */
  z?: number;
  /** 包围盒底面（米）：0 = 原点在物件底面（贴地）；缺省 = 不校验落地。 */
  minY?: number;
}

export const REAL_DIMS_M = {
  // ── 街具 ──
  /** 分类垃圾桶：⌀0.50 × H1.00（含盖）。 */
  trashCan: { x: 0.5, y: 1.0, z: 0.5, minY: 0 },
  /** 主干道路灯：杆高 12.00（含基座与灯头）。 */
  streetLight: { y: 12.0, minY: 0 },
  /** 次干道路灯总高 7.00（road_props.glb 仅含主干道灯，此行为项目现行值）。 */
  streetLightSide: { y: 7.0, minY: 0 },

  // ── 车辆（长 X × 高 Y × 宽 Z）──
  sedan: { x: 4.6, y: 1.45, z: 1.82, minY: 0 },
  taxi: { x: 4.7, y: 1.5, z: 1.85, minY: 0 },
  bus: { x: 12.0, y: 3.2, z: 2.55, minY: 0 },
  truck: { x: 8.5, y: 3.4, z: 2.5, minY: 0 },

  // ── 角色（与 18-架构设计 §4.1 一致：宽 × 高 × 深）──
  pedestrian: { x: 0.55, y: 1.67, z: 0.35, minY: 0 },

  // ── 植被 ──
  /** 阔叶树 · 夏 / 春 / 秋（oak_tree / _spring / _autumn，三变体同尺寸，实测 = 表值 ✓）。 */
  oakTree: { x: 4.5, y: 10.1, z: 4.7, minY: 0 },
  /**
   * 阔叶树 · 冬（oak_tree_winter.glb）—— **冬季落叶，冠幅本就更细**，故单列一行
   * （实测资产真值 0.412 × 0.955 × 0.382，节点 identity，不在美术重导清单内）。
   * 消费端 = EastForest（按 season 切 url/目标/fallback 归一化系数）。
   */
  oakTreeWinter: { x: 4.12, y: 9.55, z: 3.82, minY: 0 },
  /** 针叶树：**现行资产真值**（nature/pine_tree.glb 几何 0.340/0.540/0.340 + 节点 identity；
   *  本批不重导，故表值 = 资产现值，而非"真实云杉 8~12 m"的拟值）。 */
  pineTree: { x: 3.4, y: 5.4, z: 3.4, minY: 0 },
  cactus: { x: 1.7, y: 2.9, z: 0.7, minY: 0 },

  // ── 市政建筑（占地宽 X × 总高 Y × 进深 Z；不含门廊/雨棚外挑）──
  cityHall: { x: 14.4, y: 20.0, z: 8.4, minY: 0 },
  /** 通讯塔：x/z 由**塔基垫脚外缘**定义（3d_script/build_comm_tower.py FOOT=0.50 ⇒ 10 m）
   *  —— GLB 实测 1.000 × 2.050 × 1.000 ✓；fallback 已按同口径重建（垫脚 ±5 m + 4 柱 8.8 m 跨）。 */
  commTower: { x: 10.0, y: 20.5, z: 10.0, minY: 0 },
  waterTower: { x: 3.6, y: 13.3, z: 3.6, minY: 0 },
  policeStation: { x: 12.1, y: 6.6, z: 7.0, minY: 0 },
  fireStation: { x: 18.1, y: 8.5, z: 8.0, minY: 0 },
} as const satisfies Record<string, RealDimsM>;

/** 尺寸表键（= 虚拟城市 3D 物件类别）。 */
export type RealDimKey = keyof typeof REAL_DIMS_M;

/**
 * 取表值的**世界单位**副本（逐字段 u() 换算；字段集与表条目一致）。
 * 例：`worldDims('sedan').x` = 0.46（4.6 m 车长）。
 */
export function worldDims<K extends RealDimKey>(
  key: K,
): { [A in keyof (typeof REAL_DIMS_M)[K]]: number } {
  const src = REAL_DIMS_M[key] as Record<string, number>;
  const out: Record<string, number> = {};
  for (const k of Object.keys(src)) out[k] = u(src[k]);
  return out as { [A in keyof (typeof REAL_DIMS_M)[K]]: number };
}

/** sizeTargetFor 可选项。 */
export interface SizeTargetOpts {
  /** 日志标签（缺省用表键，建议传 `<category>/<name>` 与 GLB 路径对齐）。 */
  label?: string;
  /** 量测子节点名（合成 GLB 指定单变体，如 road/trash_can 的 `TrashCan_Green`）。 */
  measureNode?: string;
  /** 尺寸相对容差（缺省 engine3d 默认 ±5%）。 */
  tol?: number;
}

/**
 * 表值 → engine3d/glbSizeGuard 的校验目标（世界单位）。
 * **引用缓存**：同 (key,label,measureNode,tol) 恒返回同一对象，可直接作为
 * `useSharedGLTF` / `<Model sizeTarget>` 的稳定 prop（避免每次渲染重跑 effect）。
 */
const SIZE_TARGET_CACHE = new Map<string, ModelSizeTarget>();

export function sizeTargetFor(key: RealDimKey, opts?: SizeTargetOpts): ModelSizeTarget {
  const cacheKey = `${key}|${opts?.label ?? ''}|${opts?.measureNode ?? ''}|${opts?.tol ?? ''}`;
  const hit = SIZE_TARGET_CACHE.get(cacheKey);
  if (hit) return hit;
  const m: RealDimsM = REAL_DIMS_M[key];
  const target: ModelSizeTarget = { label: opts?.label ?? key };
  if (m.x !== undefined) target.x = u(m.x);
  if (m.y !== undefined) target.y = u(m.y);
  if (m.z !== undefined) target.z = u(m.z);
  if (m.minY !== undefined) target.minY = u(m.minY);
  if (opts?.measureNode) target.measureNode = opts.measureNode;
  if (opts?.tol !== undefined) target.tol = opts.tol;
  SIZE_TARGET_CACHE.set(cacheKey, target);
  return target;
}

/** 各城区楼层区间 [minF, maxF]（真实城市形态：CBD 摩天 / 郊区别墅；
 *  v2.12 阶段 2 追加 8 个新城区的楼层区间，前 8 区不可修改）。 */
export const DISTRICT_FLOORS: Record<VirtualCityDistrictId, [number, number]> = {
  finance:     [8, 20],   // 金融CBD   24~60 m
  tech:        [6, 15],   // 科技园    18~45 m
  industry:    [2, 6],    // 工业区     6~18 m（厂房高顶棚）
  oldtown:     [2, 7],    // 老城区     6~21 m（多层老公房）
  commerce:    [4, 12],   // 商业中心  12~36 m
  residential: [6, 18],   // 居住区    18~54 m（高层住宅）
  suburb:      [1, 3],    // 郊区       3~9 m（别墅/排屋）
  riverside:   [8, 20],   // 滨河新区  24~60 m
  // ── v2.12 阶段 2 扩展城区（80×80 地图外圈）──
  logistics_port:    [3, 8],   // 物流港     9~24 m（低层仓库/厂房）
  hightech_park:     [6, 15],  // 高新园区  18~45 m（科技办公楼）
  edu_district:      [4, 10],  // 教育园区  12~30 m（教学楼/宿舍）
  medical_city:      [8, 18],  // 医疗城    24~54 m（医院大楼）
  industrial_park:   [2, 5],   // 产业基地   6~15 m（厂房）
  central_park:      [1, 3],   // 中央公园   3~9 m（低层景观建筑）
  transport_hub:     [3, 12],  // 交通枢纽   9~36 m（车站/商业）
  cultural_creative: [3, 8],   // 文创区     9~24 m（创意园区）
  // ── 批次 20 城市扩张新增 16 区（楼层区间 = 文档 1 §2.3 表）──
  fin_sub_center:    [10, 24], // 金融副中心 30~72 m（CBD 外溢塔楼群）
  software_park:     [5, 14],  // 软件园    15~42 m（板楼办公）
  airport_town:      [3, 10],  // 空港小镇   9~30 m（临空商业）
  air_logistics:     [2, 6],   // 航空物流园 6~18 m（仓库厂房）
  auto_city:         [2, 7],   // 汽车城     6~21 m（整车制造厂房）
  mountain_resort:   [1, 3],   // 山居民宿区 3~9 m（低层度假）
  chem_park:         [2, 6],   // 化工园区   6~18 m（重工业）
  agri_park:         [1, 4],   // 现代农业园 3~12 m（大棚/农舍）
  health_town:       [2, 6],   // 康养小镇   6~18 m（疗养低层）
  steel_town:        [2, 6],   // 特钢镇     6~18 m（厂区宿舍）
  old_city_culture:  [1, 4],   // 古城文化区 3~12 m（坡顶院落）
  university_town:   [4, 12],  // 大学城    12~36 m（校园板楼）
  wetland_park:      [1, 3],   // 湿地公园   3~9 m（pavilion 景观）
  sports_new_city:   [3, 14],  // 体育新城   9~42 m（场馆商业）
  bay_new_town:      [8, 20],  // 湾区新城  24~60 m（高层滨水）
  highspeed_rail_town: [3, 12],// 高铁新城   9~36 m（TOD 综合体）
};

/** 楼层数 → 世界单位楼高。 */
export const buildingHeight = (floors: number): number => u(FLOOR_HEIGHT_M) * floors;
