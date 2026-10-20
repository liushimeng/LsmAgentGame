/**
 * cityGrid — 批次 61「全城连续正交方格路网」：街区方格化的**唯一事实来源**。
 *
 * ## 为什么需要本模块（批次 61 §1 根因）
 * 批次 59 只把「每区连 2 最近邻」的斜向 connector 拆成曼哈顿 L 形，**没有触及**
 * 「城区中心是散点」这个根因：旧城区 x 坐标集合为
 * `-46,-44,-40,-32,-24,-22,-20,-14,-12,-10,-8,0,2,8,10,14,22,24,36,38,40,44,46`
 * ——**任意相邻两值之差都 < 9**，而街区半宽 4.0 + 路廊半宽 0.7 = 4.7，
 * 于是全城**不存在任何一条能连续贯穿、又不切进任何街区**的南北向街道线。
 * 街区与街道无法构成正交关系 ⇒ 视觉上永远是「一堆斜着的块」。
 *
 * ## 本模块的解法
 * 1. **街区方格化**：32 个城区全部落到 12 的整数倍坐标上（= 街区中心），
 *    7×7 = 49 个街区单元铺满 [-36, 36]²。
 * 2. **街道 = 街区边界**：街道中心线恒在 `6 + 12k`（k ∈ [-4, 3]）⇒
 *    `…, -42, -30, -18, -6, 6, 18, 30, 42`，每条街道是**一条整直线段**
 *    （不是几十段拼接）⇒ 路面是一张连续长面片，天然无接缝、无错位。
 * 3. **运河让位一整行**：`z = 18` 这一行东西向街道**不生成**，改由运河占据
 *    （运河带 [16.5, 19.5] 恰在街区行 12（z∈[8,16]）与行 24（z∈[20,28]）之间的
 *    4u 空隙中线 ⇒ **不切任何街区**）；南北向街道在运河处架桥通过。
 *
 * ## 单一事实来源纪律（§130）
 * - 网格常量（街区边长 / 街道线 / 街区中心集合）**只此一处**；
 * - 城区的格子归属**不另存一份表**——城区坐标本身即街区中心，
 *   由 `VIRTUAL_CITY_DISTRICTS` 派生（`types/virtualCity.ts` 与后端
 *   `ServerGo/game/virtual_city/districts.go` 逐字段一致）；
 * - 空街区单元（未被城区占用）由本模块从城区表反推，**不硬编码**。
 *
 * 契约：lag_docs/虚拟城市/已实现/61-全城连续正交方格路网/01-方案设计.md §2。
 */

import { VIRTUAL_CITY_DISTRICTS } from '@/types/virtualCity';

/** 街区单元边长（世界单位；1u = 10 m ⇒ **120 m 街区**，与真实城市街坊同量级）。 */
export const GRID_CELL = 12;

/** 街区中心索引范围（含端点）：i, j ∈ [-3, 3] ⇒ 7×7 = 49 个街区单元。 */
export const GRID_INDEX_MIN = -3;
export const GRID_INDEX_MAX = 3;

/**
 * 街道中心线坐标（= 街区边界）：`6 + 12k`，k ∈ [-4, 3]
 * ⇒ `[-42, -30, -18, -6, 6, 18, 30, 42]`。
 */
export const GRID_STREET_LINES: readonly number[] = [-42, -30, -18, -6, 6, 18, 30, 42];

/**
 * 运河占据的东西向街道线（该行**不生成街道**）。
 * 运河带 [16.5, 19.5] 恰落在街区行 12（z∈[8,16]）与行 24（z∈[20,28]）之间的
 * 4u 空隙中线 ⇒ 运河不切任何街区；南北向街道在此架桥通过。
 */
export const CANAL_STREET_Z = 18;

/**
 * 运河半宽（河宽 3u ⇒ z ∈ [CANAL_STREET_Z-1.5, CANAL_STREET_Z+1.5]）。
 * **唯一事实来源在本模块**（`cityObstacles.ts` re-export）——
 * `roadNetwork.ts` 需要它来**在运河处把南北向街道拆成两段**，
 * 而 `cityObstacles.ts` 依赖 `roadNetwork.ts`（`pointSegDist`）⇒ 常量若放在
 * cityObstacles 会形成**循环 import**。故常量下沉到本 leaf 模块。
 */
export const CANAL_HALF_WIDTH = 1.5;

/** 该街道线是否为主干道级（主干道或外环快速路 ⇒ 宽路幅 + 主干道贴图）。 */
export function isMainStreetLine(v: number): boolean {
  return GRID_ARTERIAL_LINES.includes(v) || GRID_EXPRESSWAY_LINES.includes(v);
}

/** 主干道线（城市中环，贯穿全城；`x = ±18` 两条南北向 + `z = -18` 一条东西向）。 */
export const GRID_ARTERIAL_LINES: readonly number[] = [-18, 18];

/** 外环快速路线（城市最外圈方形快速路；`x = ±42` / `z = ±42`）。 */
export const GRID_EXPRESSWAY_LINES: readonly number[] = [-42, 42];

/** 街区底板半宽（DistrictBlock 底板 8×8 ⇒ 半宽 4；建筑街墙外缘 ≈3.75）。 */
export const BLOCK_HALF = 4;

/**
 * 建筑退让净距下限（世界单位）：街区半宽 4 + 主干道半宽 0.7 = **4.7**。
 * 街区中心到任一街道中心线的距离恒为 6 ⇒ 余量 1.3u = 13 m（人行道 + 绿化带）。
 */
export const BLOCK_ROAD_CLEARANCE = 4.7;

/** 街道线在 z 方向（东西向街道）的有效集合 —— 排除运河行。 */
export const GRID_EW_STREET_LINES: readonly number[] = GRID_STREET_LINES.filter(
  (z) => z !== CANAL_STREET_Z,
);

/** 街区中心集合（7×7 = 49 个），顺序：i 外层、j 内层（确定性）。 */
export const GRID_CENTERS: readonly (readonly [number, number])[] = (() => {
  const out: Array<readonly [number, number]> = [];
  for (let i = GRID_INDEX_MIN; i <= GRID_INDEX_MAX; i++) {
    for (let j = GRID_INDEX_MIN; j <= GRID_INDEX_MAX; j++) {
      out.push([i * GRID_CELL, j * GRID_CELL]);
    }
  }
  return out;
})();

/** 街区单元键（"x,z"；用于集合判定）。 */
export function cellKey(x: number, z: number): string {
  return `${x},${z}`;
}

/** 该坐标是否落在某个街区单元的中心上。 */
export function isGridCenter(x: number, z: number): boolean {
  return (
    Number.isInteger(x / GRID_CELL) &&
    Number.isInteger(z / GRID_CELL) &&
    Math.abs(x / GRID_CELL) <= GRID_INDEX_MAX &&
    Math.abs(z / GRID_CELL) <= GRID_INDEX_MAX
  );
}

/** 已被城区占用的街区单元（由城区静态表派生，**不另存一份表**）。 */
export function occupiedCells(): ReadonlySet<string> {
  const s = new Set<string>();
  for (const d of VIRTUAL_CITY_DISTRICTS) s.add(cellKey(d.x, d.z));
  return s;
}

/**
 * 空街区单元（未被城区占用）—— 批次 61 §2.2 末尾列出的 17 个。
 * 由城区表反推 ⇒ 城区表一改，这里自动跟随（不漂移）。
 */
export function emptyCells(): Array<[number, number]> {
  const used = occupiedCells();
  return GRID_CENTERS.filter(([x, z]) => !used.has(cellKey(x, z))).map(
    ([x, z]) => [x, z] as [number, number],
  );
}

/** 城区 id → 街区单元中心（派生视图）。 */
export function districtCell(id: string): [number, number] {
  const d = VIRTUAL_CITY_DISTRICTS.find((x) => x.id === id);
  return d ? [d.x, d.z] : [0, 0];
}
