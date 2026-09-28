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
 *   - **轴向约定**（护栏判据 ⑤ 直立 / ⑥ 向前轴）集中在 MODEL_AXIS_CONVENTIONS，
 *     随 sizeTargetFor() 自动带上；只对"能确证"的物件声明（扁平物件/多物件合成 GLB
 *     不声明，理由逐条留在该表注释里）—— 见 guard 文件头「已知边界」。
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
 * 地块表面标高（世界单位）= DistrictBlock 底板顶面（y=0.02）。
 * 批次 30 P0-6「落地基准三源统一」：站在**地块**上的物件（行人 / 街具）以此为
 * 落位 y；路面系物件用 ROAD_SURFACE_Y / VEHICLE_GROUND_Y，人行道用
 * SIDEWALK_Y（Road.tsx = ROAD_SURFACE_Y + 0.012）。禁止再出现第四个 0/0.02/0.035。
 */
export const DISTRICT_SURFACE_Y = 0.02;

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
  /** 分类垃圾桶：⌀0.50 × H1.00（含盖）—— 批次 30 复核维持原值。
   *  二进制级证据（raw accessor 顶点解析 + 3d_script/verify_glb_aabb.py）：
   *  TrashCan_Green/Blue 节点跨度恰 0.050×0.100×0.050 world、minY=0、节点 scale
   *  全 1 ⇒ GLB 路径 ⌀0.50；程序化 fallback 经 worldDims 同行表值，双路径一致。
   *  （批次 30 曾误判 0.68 —— 系把改表后的 fallback 桶当成 GLB 实测，已回退。） */
  trashCan: { x: 0.5, y: 1.0, z: 0.5, minY: 0 },
  /** 主干道路灯：杆高 12.00（含基座与灯头）。 */
  streetLight: { y: 12.0, minY: 0 },
  /** 次干道路灯总高 7.00（项目现行值；road_props.glb 已随批次 31 删除）。 */
  streetLightSide: { y: 7.0, minY: 0 },

  // ── 批次 30 新增（只增不改：旧值不动 ⇒ 无需重导 GLB）──
  /**
   * 城内行道树/园林树（程序化 fallback 归一目标；森林橡树 oakTree 10.1 m 同量级，
   * 行道树取 9.0 m 高 / 4.5 m 冠幅 —— 批次 30 P0-3 修复「城内树 1.7~4.4 m 棒棒糖」）。
   */
  streetTree: { x: 4.5, y: 9.0, z: 4.5, minY: 0 },
  /** 交通信号杆（杆 + 灯头总高 5.50；灯头尺寸见 TrafficSignals 消费点）。 */
  trafficSignalPole: { y: 5.5, minY: 0 },

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

/**
 * 真实米制**间距**表（单位 = 米；批次 30 A1）。
 *
 * 与 REAL_DIMS_M 平行：尺寸表管"物件多大"，本表管"物件摆多密"。
 * 消费端经 `spacing(key)` 换算世界单位；禁止组件内硬编码间距魔数。
 */
export const REAL_SPACING_M = {
  /** 主干道路灯间距（单侧）；两侧交替布置 ⇒ 同侧实际 2×。 */
  lampMain: 37,
  /** 次干道路灯间距（单侧；两侧交替）。 */
  lampSide: 50,
  /** 行道树株距（两侧交替 ⇒ 同侧 2×；真实行道树 8~10 m，取 9）。 */
  streetTree: 9,
  /** 路侧垃圾桶沿线间距（两侧交替 ⇒ 单侧 120 m；公交站台旁另补 1 个）。 */
  trashCan: 60,
  /** 公交站台间距（主干道沿线）。 */
  busStop: 500,
} as const;

export type RealSpacingKey = keyof typeof REAL_SPACING_M;

/** 间距表值 → 世界单位。 */
export function spacing(key: RealSpacingKey): number {
  return u(REAL_SPACING_M[key]);
}

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

/**
 * 轴向约定（engine3d/glbSizeGuard 判据 ⑤ 直立 / ⑥ 向前轴）—— **按表键的唯一事实来源**。
 *
 * 与 REAL_DIMS_M 同源同理：轴向是**模型的属性**（资产轴 ↔ 消费端 yaw 公式的契约），
 * 与具体调用点无关，故在此集中声明，`sizeTargetFor()` 自动带上（调用点无需改）。
 *
 * ⚠️ 声明纪律（**只声明能确证的**，判据是「尺寸主导性」，无容差）：
 *   - `upright: true` 只在**该物件确实 Y 主导**（Y 尺寸 > X 且 > Z）时成立。扁平物件
 *     （车辆 长>宽>高；低层站房 宽>高）主导轴**本就不是 Y** ⇒ **一律不得声明**：
 *     声明即"正确资产被判侧躺"的反向误报，而真正的"车尾立起"（长轴竖直）反倒能通过。
 *     这类物件的轴向错误由 engine3d 判据 ① 兜住（三轴目标互异 ⇒ 任意 90° 轴置换都会
 *     让某轴偏差超 ±5%）。
 *   - `forward` 只在**该物件沿行进方向拉长**时成立（车辆长轴 = 行进轴）。行人不沿
 *     行进轴拉长（宽 0.55 > 深 0.35）⇒ 不声明；其「+Z 向前」约定由消费端
 *     `PedestrianV3.tsx` 的 `atan2(dx, dz)` 唯一承载，包围盒无法自证。
 */
export interface ModelAxisConvention {
  /** ⑤ 直立：竖直轴 = +Y（Y 为主导轴）。缺省 = 不校验。 */
  upright?: boolean;
  /** ⑥ 向前轴：水平长轴 = 消费端 yaw 公式假设的向前轴。缺省 = 不校验。 */
  forward?: NonNullable<ModelSizeTarget['forward']>;
}

export const MODEL_AXIS_CONVENTIONS: Readonly<Partial<Record<RealDimKey, ModelAxisConvention>>> = {
  // ── 车辆（长轴 = +X）──
  // `Vehicle.tsx` 的 yaw = `atan2(-dz, dx)`（+X 对齐行进方向）⇒ 声明 forward:'x'。
  // **不声明 upright**：轿车 4.6×1.45×1.82 / 公交 12×3.2×2.55 等 Y 都是最小轴，
  // 声明即反向误报（见 ModelAxisConvention 注释）。
  sedan: { forward: 'x' },
  taxi: { forward: 'x' },
  bus: { forward: 'x' },
  truck: { forward: 'x' },

  // ── 角色 ──
  // 行人 Y 主导（0.55×1.67×0.35）⇒ upright 成立；
  // **不声明 forward**：行人宽 0.55 > 深 0.35，不沿行进轴拉长，
  // 其 +Z 向前由 `PedestrianV3.tsx` 的 `atan2(dx, dz)` 承载（与车辆约定不同、各自自洽）。
  pedestrian: { upright: true },

  // ── 街具 ──
  // ⚠️ 量测口径 = `RoadsideBins.tsx` 传的 `measureNode: 'TrashCan_Green'`（单桶
  // 0.5×1.0×0.5 ⇒ Y 主导 ✓）。若哪天改为量测整棵场景（Green+Blue 两变体沿 X 排布
  // ⇒ 1.7×1.0×0.5，X 主导），本声明会误报 —— 届时须同时改 measureNode 与本节。
  trashCan: { upright: true },

  // ── 市政建筑 ──
  // 仅声明 **Y 主导** 的塔/高层楼；`policeStation`(12.1×6.6×7.0) 与
  // `fireStation`(18.1×8.5×8.0) 是低层站房（宽 > 高）⇒ 不声明 upright（轴向错误由 ① 兜住）。
  cityHall: { upright: true },
  commTower: { upright: true },
  waterTower: { upright: true },

  // ── 植被 ──
  // 橡树四季（前 3 变体同尺寸 4.5×10.1×4.7、冬季 4.12×9.55×3.82）、针叶树 3.4×5.4×3.4、
  // 仙人掌 1.7×2.9×0.7 —— 全部 Y 主导（判据 ⑤ 实测 ✓）。
  oakTree: { upright: true },
  oakTreeWinter: { upright: true },
  pineTree: { upright: true },
  cactus: { upright: true },

  // ── 明文豁免（不声明，理由留档）──
  // 下列 4 项均经 `<Model url={modelUrl(...)}>` **不带 sizeTarget** 直挂
  //（SouthOcean.tsx / NorthMountains.tsx）⇒ 护栏本就看不到它们；此处留档的是
  //「**即便**将来接入校验也不得声明 upright」的理由。
  // streetLight / streetLightSide：原 `road/road_props.glb` 是**多物件合成 GLB**
  //   （批次 31 已删文件，此处留档豁免理由）
  //   （3 盏路灯沿 X 排布 ⇒ 场景盒 31.3×12.0×5.05，主导轴 = X 是"排布"而非"侧躺"）
  //   ⇒ 声明 upright 必然误报；判据 ⑤ 不可用于多物件合成 GLB（勿为迁就改断言语义）。
  // ocean/cargo_ship (68.2×14.2×10.4) · ocean/sailboat：长轴 X 即**正确摆放**
  //   （船体沿航向），同理不声明 upright；且有独立 yaw/浮沉约定，不在本表校验范围。
  // nature/snow_mountain：**地形基准**（非"物件"，3.1×2.55×2.8 三轴同级，主导轴无意义）
  //   ⇒ 不声明 upright。
  // ocean/lighthouse：Y 主导但**无 sizeTargetFor 调用点**（未接入校验），故不入表。
};

/** sizeTargetFor 可选项。 */
export interface SizeTargetOpts {
  /** 日志标签（缺省用表键，建议传 `<category>/<name>` 与 GLB 路径对齐）。 */
  label?: string;
  /** 量测子节点名（合成 GLB 指定单变体，如 road/trash_can 的 `TrashCan_Green`）。 */
  measureNode?: string;
  /** 尺寸相对容差（缺省 engine3d 默认 ±5%）。 */
  tol?: number;
  /** ⑤ 直立断言覆盖（缺省取 `MODEL_AXIS_CONVENTIONS[key].upright`；`false` = 强制关闭）。 */
  upright?: boolean;
  /** ⑥ 向前轴断言覆盖（缺省取 `MODEL_AXIS_CONVENTIONS[key].forward`）。 */
  forward?: NonNullable<ModelSizeTarget['forward']>;
}

/** 声明层自校验的入参（缺省 = 取表内声明）。 */
export interface AxisConventionDecl {
  /** ⑤ 直立声明。 */
  upright?: boolean;
  /** ⑥ 向前轴声明。 */
  forward?: NonNullable<ModelSizeTarget['forward']>;
}

const AXIS_DECL_MOTIVE =
  '\n  → 本校验的动机（批次 29 收尾实况）：主 Agent 按「直觉物理」给车辆写声明，' +
  '若非实现方按纪律拒绝，会在 dev 控制台留下 7 条误报 —— 「狼来了」噪声会掩盖真问题。' +
  '这类错误的本质是「**声明与表值自相矛盾**」，不需要模型即可判定。';

/**
 * **声明层自校验**（纯函数，不依赖 GLB）：声明与 `REAL_DIMS_M` 表值是否自洽。
 *
 *   - `upright: true` ⇒ 要求表值 **y 严格最大**（`x ≥ y` 或 `z ≥ y` 即矛盾）；
 *   - `forward: 'x'` ⇒ 要求表值 `x > z`；`forward: 'z'` ⇒ 要求 `z > x`；
 *   - **缺对应表值字段即跳过**（如只填 y 而声明 upright），不误报。
 *
 * 返回告警文案（`null` = 自洽）。由 `sizeTargetFor()` 在 dev 态**每份唯一声明各调一次**
 * （引用缓存保证不随模型/渲染重复）。
 */
export function axisConventionIssue(
  key: RealDimKey,
  decl: AxisConventionDecl = MODEL_AXIS_CONVENTIONS[key] ?? {},
): string | null {
  const m: RealDimsM = REAL_DIMS_M[key];
  const fmt = (v: number | undefined): string => (v === undefined ? '—' : v.toFixed(2));
  if (decl.upright && m.x !== undefined && m.y !== undefined && m.z !== undefined) {
    if (!(m.y > m.x && m.y > m.z)) {
      return (
        '[cityScale] 声明自相矛盾：`upright` 要求竖直轴为 Y，' +
        `但 REAL_DIMS_M.${key} 的 y(${fmt(m.y)}) 不是最大轴（x=${fmt(m.x)}, z=${fmt(m.z)}）` +
        '—— 扁平物件（车辆 / 低层站房）**不得**声明 `upright`，其轴向由 engine3d 判据 ① 兜住' +
        '（三轴目标两两互异 ⇒ 任意 90° 轴置换都会让某轴偏差超 ±5%）。' +
        '（反向失效：扁平物件"长轴竖直"反而能通过该断言。）' +
        AXIS_DECL_MOTIVE
      );
    }
  }
  if (decl.forward && m.x !== undefined && m.z !== undefined) {
    const want = decl.forward;
    const pair: [string, number, string, number] =
      want === 'x' ? ['x', m.x, 'z', m.z] : ['z', m.z, 'x', m.x];
    const [wl, wv, sl, sv] = pair;
    if (!(wv > sv)) {
      return (
        `[cityScale] 声明自相矛盾：\`forward='${want}'\` 要求水平长轴为 ${wl.toUpperCase()}，` +
        `但 REAL_DIMS_M.${key} 的 ${wl}(${fmt(wv)}) ≤ ${sl}(${fmt(sv)})` +
        '—— `forward` 是「水平长轴」声明，与表值矛盾说明声明写错，或该物件本就不该声明' +
        '（如行人不沿行进轴拉长，"朝向"无法由包围盒自证，由消费端 yaw 公式承载）。' +
        AXIS_DECL_MOTIVE
      );
    }
  }
  return null;
}

/**
 * 表值 → engine3d/glbSizeGuard 的校验目标（世界单位）。
 * **引用缓存**：同 (key,label,measureNode,tol,upright,forward) 恒返回同一对象，可直接作为
 * `useSharedGLTF` / `<Model sizeTarget>` 的稳定 prop（避免每次渲染重跑 effect）。
 * 缓存未命中时（= 每份唯一声明恰好一次）顺带跑**声明层自校验**（dev 态，只告警）。
 */
const SIZE_TARGET_CACHE = new Map<string, ModelSizeTarget>();

export function sizeTargetFor(key: RealDimKey, opts?: SizeTargetOpts): ModelSizeTarget {
  const conv: ModelAxisConvention | undefined = MODEL_AXIS_CONVENTIONS[key];
  const upright = opts?.upright ?? conv?.upright;
  const forward = opts?.forward ?? conv?.forward;
  const cacheKey =
    `${key}|${opts?.label ?? ''}|${opts?.measureNode ?? ''}|${opts?.tol ?? ''}` +
    `|${upright ?? ''}|${forward ?? ''}`;
  const hit = SIZE_TARGET_CACHE.get(cacheKey);
  if (hit) return hit;
  if (import.meta.env.DEV) {
    const issue = axisConventionIssue(key, { upright, forward });
    if (issue) console.warn(issue);
  }
  const m: RealDimsM = REAL_DIMS_M[key];
  const target: ModelSizeTarget = { label: opts?.label ?? key };
  if (m.x !== undefined) target.x = u(m.x);
  if (m.y !== undefined) target.y = u(m.y);
  if (m.z !== undefined) target.z = u(m.z);
  if (m.minY !== undefined) target.minY = u(m.minY);
  if (opts?.measureNode) target.measureNode = opts.measureNode;
  if (opts?.tol !== undefined) target.tol = opts.tol;
  if (upright) target.upright = true;
  if (forward) target.forward = forward;
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

/**
 * 批次 32：房价指数（0.8–1.6）→ prosperity（0–1）。
 *
 * **单一事实来源**：`DistrictBlock` 渲染与 `freeViewColliders` 相机碰撞体装配
 * 必须共用此函数，否则改了渲染公式而漏改碰撞公式 ⇒ 相机穿楼 / 被空气挡住
 * （CLAUDE.md §130「声明了却从不接线」在数据侧的等价形态）。
 */
export const prosperityOf = (priceIndex: number): number =>
  Math.min(1, Math.max(0, (priceIndex - 0.8) / 0.8));

/**
 * 批次 32：单栋楼的**顶面 y**（世界单位）。
 * 与 `DistrictBuildings` 的渲染楼高公式逐字节同源，碰撞体直接复用。
 *
 * @param districtId 城区 id（取 DISTRICT_FLOORS 的楼层区间）
 * @param prosperity 0–1，由 `prosperityOf(price_index)` 得到
 * @param factor 布局系数 0.6–1.0（`buildingsFor` 产出）
 */
export const buildingTopY = (
  districtId: VirtualCityDistrictId,
  prosperity: number,
  factor: number,
): number => {
  const [minF, maxF] = DISTRICT_FLOORS[districtId];
  const floors = minF + (maxF - minF) * prosperity;
  return buildingHeight(floors) * (0.85 + factor * 0.15);
};
