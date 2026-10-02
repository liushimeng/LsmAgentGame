/**
 * cityObstacles — 布局互知唯一入口（批次 38 · 真实世界布局与交互修正 §4.1）。
 *
 * 背景：路网 / 建筑 / 街具 / 水系 / 桥 五套布局各自为政、互不查询，导致
 * 「一环路穿楼 / 树长在河上 / 桥是歪的」等系统性错位。本模块提供**纯函数、
 * 无副作用、无随机**的统一 keep-out 查询，所有布点方（建筑 / 街具 / 桥）共用。
 *
 * 唯一事实来源约定：
 *   - 运河 / 港池常量（CANAL_Z / CANAL_HALF_X / 河宽 / 港池矩形）**只此一处**，
 *     `VirtualCityCityMap::WaterLayer` 改为 import 消费；
 *   - `pointSegDist` 复用 `roadNetwork.ts` 的导出实现，禁止复制第二份；
 *   - 道路廊道由 `buildRoadCorridors(net)` 从真实路网线段派生（含人行道半宽）。
 *
 * 确定性：同输入必同输出（与 roadNetwork 同规约）。
 */

import {
  buildRoadNetwork,
  pointSegDist,
  FIRST_RING_RADIUS,
  FIRST_RING_WIDTH,
  type RoadNetwork,
} from './roadNetwork';
import { VIRTUAL_CITY_DISTRICTS } from '@/types/virtualCity';
import { LANDMARK_FIELDS } from './cityLandmarks';
import { ROAD_WIDTH_MAIN, ROAD_WIDTH_SIDE } from './cityScale';

// ── 水域常量（唯一事实来源；原 VirtualCityCityMap::WaterLayer 持有）──────────

/** 运河中心 z（东西走向直线 z=CANAL_Z）。 */
export const CANAL_Z = 17;
/** 运河 x 半跨（水面横贯 x ∈ [-CANAL_HALF_X, CANAL_HALF_X]）。 */
export const CANAL_HALF_X = 48;
/** 运河半宽（河宽 3u ⇒ z ∈ [CANAL_Z-1.5, CANAL_Z+1.5]）。 */
export const CANAL_HALF_WIDTH = 1.5;

/** 运河（轴对齐矩形水域）。 */
export const CANAL_WATER: WaterBody = {
  minX: -CANAL_HALF_X,
  maxX: CANAL_HALF_X,
  minZ: CANAL_Z - CANAL_HALF_WIDTH,
  maxZ: CANAL_Z + CANAL_HALF_WIDTH,
};

/** 物流港港池（6×8，中心 (-30,-4)；WaterLayer 同源）。 */
export const PORT_POOL_WATER: WaterBody = {
  minX: -33,
  maxX: -27,
  minZ: -8,
  maxZ: 0,
};

/** 全部水域（布点 keep-out 与桥位过滤共用）。 */
export const WATER_BODIES: readonly WaterBody[] = [CANAL_WATER, PORT_POOL_WATER];

// ── 地面场馆保留地（批次 47 新增）────────────────────────────────────────
/**
 * 体育场用地（轴对齐矩形）**与水域同档**：`isBuildable()` 一律让开。
 *
 * 定义放在 leaf 模块 `cityLandmarks.ts`（零 import），因为道路网也要消费同一份数据 ——
 * `roadNetwork` 的 `conn-fin_sub_center~sports_new_city` 与 `edge-sports_new_city`
 * 两条路的端点都落在体育场中心，不截断就会横穿跑道；而 `cityObstacles` 本身 import
 * `roadNetwork`，反向 import 会成环。
 *
 * 与中央公园的差异：`central_park` 是在 `building_layout.ts` 里按 `isPark` 走**另一条
 * 布点分支**（散布小品而非街墙）；体育场是「单一大场馆 + 周边街墙」，所以按
 * **矩形保留地**处理，`building_layout` 的既有语义（不通过则丢弃槽位、不递补）直接生效。
 *
 * 尺寸口径：110×74 m = `cityScale.REAL_DIMS_M.sportsField`（即 GLB 包围盒）。
 */
export { LANDMARK_FIELDS, SPORTS_FIELD_AREA, segHitsLandmark, trimSegmentToLandmarks } from './cityLandmarks';

/** 全部地面保留地（水域 + 场馆）。 */
export const RESERVED_FIELDS: readonly WaterBody[] = [
  ...WATER_BODIES,
  ...LANDMARK_FIELDS.map((f) => ({ minX: f.minX, maxX: f.maxX, minZ: f.minZ, maxZ: f.maxZ })),
];

// ── 类型 ─────────────────────────────────────────────────────────────────

/** 道路廊道（世界 xz 平面线段 + 半宽）。 */
export interface RoadCorridor {
  from: [number, number];
  to: [number, number];
  /** 路幅半宽（含人行道），单位 u。 */
  halfWidth: number;
  /** 路幅等级（建筑退让只看 main：connector 穿街区庭院，楼在街墙边缘不与其重叠）。 */
  kind: 'main' | 'side';
}

/** 水域（轴对齐矩形）。 */
export interface WaterBody {
  minX: number;
  maxX: number;
  minZ: number;
  maxZ: number;
}

/** 一环带域（环形廊道）—— 一环路是圆，不能用线段表达。 */
export interface RingCorridor {
  radius: number;
  halfWidth: number;
}

// ── 廊道几何 ─────────────────────────────────────────────────────────────

/** 人行道每侧宽（RoadMarkings.SIDEWALK_WIDTH 同值；keep-out 计入路廊半宽）。 */
const SIDEWALK_W = 0.25;

/** 一环带域（r=20 圆环；半宽含人行道）。 */
export const FIRST_RING_CORRIDOR: RingCorridor = {
  radius: FIRST_RING_RADIUS,
  halfWidth: FIRST_RING_WIDTH / 2 + SIDEWALK_W,
};

/**
 * 由 RoadNetwork 生成全部道路廊道（确定性：按 segments 表序）。
 * halfWidth 取路幅半宽 + 人行道（主 0.95u / 次 0.7u）。
 */
export function buildRoadCorridors(net: RoadNetwork): RoadCorridor[] {
  return net.segments.map((s) => ({
    from: s.from,
    to: s.to,
    halfWidth: (s.kind === 'main' ? ROAD_WIDTH_MAIN : ROAD_WIDTH_SIDE) / 2 + SIDEWALK_W,
    kind: s.kind,
  }));
}

// ── 查询（全部纯函数；margin 默认 0）──────────────────────────────────────

/** 点到线段距离（复用 roadNetwork 实现，§4.1 硬约束 1）。 */
export { pointSegDist };

/** 点是否落入任一水域（含 margin 外扩，默认 0）。 */
export function inWater(x: number, z: number, margin = 0): boolean {
  for (const w of WATER_BODIES) {
    if (x >= w.minX - margin && x <= w.maxX + margin && z >= w.minZ - margin && z <= w.maxZ + margin) {
      return true;
    }
  }
  return false;
}

/** 点是否落入任一路廊（含 margin 外扩）。 */
export function onRoadCorridor(x: number, z: number, margin = 0, corridors?: readonly RoadCorridor[]): boolean {
  const list = corridors ?? ensureCorridors();
  for (const c of list) {
    if (pointSegDist(x, z, c.from, c.to) < c.halfWidth + margin) return true;
  }
  return false;
}

/** 点是否落入一环带域 |dist(point,0) − radius| < halfWidth + margin。 */
export function onFirstRing(x: number, z: number, margin = 0): boolean {
  const d = Math.sqrt(x * x + z * z);
  return Math.abs(d - FIRST_RING_CORRIDOR.radius) < FIRST_RING_CORRIDOR.halfWidth + margin;
}

/** 点是否落入任一**地面保留地**（水域 + 体育场等场馆；含 margin 外扩）。 */
export function inReservedField(x: number, z: number, margin = 0): boolean {
  for (const w of RESERVED_FIELDS) {
    if (x >= w.minX - margin && x <= w.maxX + margin && z >= w.minZ - margin && z <= w.maxZ + margin) {
      return true;
    }
  }
  return false;
}

/**
 * 综合判定：该点可否放置「地面实体」（建筑 / 树 / 灯 / 桶）。
 * = !inWater && !inReservedField && !onRoadCorridor && !onFirstRing
 */
export function isBuildable(
  x: number,
  z: number,
  margin = 0,
  corridors?: readonly RoadCorridor[],
): boolean {
  return !inWater(x, z, margin)
    && !inReservedField(x, z, margin)
    && !onRoadCorridor(x, z, margin, corridors)
    && !onFirstRing(x, z, margin);
}

/**
 * 线段是否与任一水域矩形相交（过河段判定；RoadMarkings 整段跳过铺装、
 * Road 过河段不渲染路面由桥面承载，共用此判定）。
 *
 * ⚠ 必须用**精确求交**（Liang-Barsky 线段裁剪），不能均匀采样：运河宽仅 3u，
 * 96u 长的纵贯线按 21 点采样步长 4.8u 会整步跨过水面（批次 38 实测漏判）。
 */
export function segIntersectsWater(from: [number, number], to: [number, number], margin = 0): boolean {
  for (const w of WATER_BODIES) {
    if (segIntersectsRect(from, to, w.minX - margin, w.maxX + margin, w.minZ - margin, w.maxZ + margin)) {
      return true;
    }
  }
  return false;
}

/** 线段 vs 轴对齐矩形（Liang-Barsky 裁剪；t∈[0,1] 有解即相交）。 */
function segIntersectsRect(
  from: [number, number],
  to: [number, number],
  minX: number,
  maxX: number,
  minZ: number,
  maxZ: number,
): boolean {
  return segRectInterval(from, to, minX, maxX, minZ, maxZ) !== null;
}

/** 线段被矩形裁剪出的参数区间 [t0,t1]⊂[0,1]；不相交返回 null。 */
function segRectInterval(
  from: [number, number],
  to: [number, number],
  minX: number,
  maxX: number,
  minZ: number,
  maxZ: number,
): [number, number] | null {
  const dx = to[0] - from[0];
  const dz = to[1] - from[1];
  let t0 = 0;
  let t1 = 1;
  const clip = (p: number, q: number): boolean => {
    if (Math.abs(p) < 1e-12) return q >= 0; // 平行于该边界：在内侧才可能相交
    const t = q / p;
    if (p < 0) {
      if (t > t1) return false;
      if (t > t0) t0 = t;
    } else {
      if (t < t0) return false;
      if (t < t1) t1 = t;
    }
    return true;
  };
  if (
    !clip(-dx, from[0] - minX) ||
    !clip(dx, maxX - from[0]) ||
    !clip(-dz, from[1] - minZ) ||
    !clip(dz, maxZ - from[1]) ||
    t0 > t1
  ) {
    return null;
  }
  return [t0, t1];
}

/**
 * 线段是否**真过河**（穿越运河中心线段 z=CANAL_Z、|x|≤CANAL_HALF_X）。
 * 用于「过河段不渲染路面，由桥面承载」——只跳**过河**段，不跳与河道纵向重叠的段
 * （如 arterial z=18 全程压在河带 [15.5,18.5] 内，整段跳过会吞掉 96u 道路）。
 */
export function segCrossesCanal(from: [number, number], to: [number, number]): boolean {
  // 中心线视为极薄矩形（宽 1e-6），用区间求交判穿越
  return (
    segRectInterval(
      from,
      to,
      -CANAL_HALF_X,
      CANAL_HALF_X,
      CANAL_Z - 1e-6,
      CANAL_Z + 1e-6,
    ) !== null
  );
}

/**
 * 线段在水域上的重叠比例 [0,1]（Liang-Barsky 区间长度）。
 * >0 且不真过河 = 纵向压水段（路面需抬到桥面高度才可见）。
 */
export function segWaterOverlapFrac(from: [number, number], to: [number, number]): number {
  let best = 0;
  for (const w of WATER_BODIES) {
    const iv = segRectInterval(from, to, w.minX, w.maxX, w.minZ, w.maxZ);
    if (iv) best = Math.max(best, iv[1] - iv[0]);
  }
  return best;
}

/**
 * 模块级默认路廊缓存。优先由 `installRoadCorridors(ROAD_NETWORK)` 在
 * VirtualCityCityMap 建网后注入；未注入时**惰性自建**同规约路廊
 * （`buildRoadNetwork(VIRTUAL_CITY_DISTRICTS, 0)` 与建网入参确定性同源），
 * 保证任何调用方（含单测 / 未挂 CityMap 的入口）都不会拿到「空廊道 = 全放行」
 * 的静默降级。
 */
let DEFAULT_CORRIDORS: readonly RoadCorridor[] | null = null;

function ensureCorridors(): readonly RoadCorridor[] {
  if (!DEFAULT_CORRIDORS) {
    DEFAULT_CORRIDORS = buildRoadCorridors(buildRoadNetwork(VIRTUAL_CITY_DISTRICTS, 0));
  }
  return DEFAULT_CORRIDORS;
}

/** 注入全局路廊表（VirtualCityCityMap 建网后一次性调用；幂等覆盖）。 */
export function installRoadCorridors(net: RoadNetwork): void {
  DEFAULT_CORRIDORS = buildRoadCorridors(net);
}

/** 当前全局路廊表（只读；未注入时惰性自建）。 */
export function roadCorridors(): readonly RoadCorridor[] {
  return ensureCorridors();
}

/**
 * 仅 main 级路廊（arterial / edgeLink；不含 connector）。
 * 建筑退让用：connector 走街区**庭院**（区心↔区心），街墙楼在底板边缘
 * （中心距 2.85、进深 ≤1.6）与之天然不重叠；若把 connector 也纳入
 * 「中心点 + 外接圆」保守判定，会把全区楼误杀（批次 38 实测 14/32 区 <4 栋）。
 * 一环 / 水域 keep-out 不受影响（onFirstRing / inWater 独立判定）。
 */
export function mainRoadCorridors(): readonly RoadCorridor[] {
  return ensureCorridors().filter((c) => c.kind === 'main');
}
