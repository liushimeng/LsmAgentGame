/**
 * roadNetwork — 批次 61「全城连续正交方格路网」：全城道路网单一事实来源（纯函数，无随机）。
 *
 * 设计文档：lag_docs/虚拟城市/已实现/61-全城连续正交方格路网/01-方案设计.md
 *
 * ## 本批的范式转变（批次 31/59 → 61）
 * 批次 31「混合式路网」= 方格骨干 + 鱼骨 connector + 放射 spoke + 两道圆环；
 * 批次 59 把 connector 的斜向段拆成曼哈顿 L 形。两者都保留了「**按需拼线段**」的思路，
 * 于是全城路网是**若干互不对齐的碎线段**：每条 connector 的拐角坐标由它自己那一对
 * 城区独立评分决定，两条 connector 之间毫无共线关系；再加 17 条径向 edgeLink（斜向）
 * 与两道与方格网互斥的圆环 ⇒ 用户反馈「街道还是很乱，没有连续上，一块块还是斜向的」。
 *
 * 批次 61 换范式：**先定街区，再让街道成为街区边界**。
 *   - 32 个城区全部落到 12 的整数倍（= 街区中心，7×7 = 49 单元，见 `cityGrid.ts`）；
 *   - 街道中心线恒在 `6 + 12k`（[-42,-30,-18,-6,6,18,30,42]）⇒ **每条街道就是
 *     一条整直线段**，端点恰在 ±42 边界，路口恒在另一条街道的**内部**（真交叉）；
 *   - 路面是**一张连续长面片**（不是几十段拼接）⇒ 天然无接缝、无错位；
 *   - `z = 18` 那一行东西向街道**让位给运河**（运河带 [16.5,19.5] 恰落在街区行 12
 *     与行 24 之间的 4u 空隙中线，不切任何街区），南北向街道在此架桥通过。
 *
 * ## 已删除（与方格网互斥，见设计文档 §2.5）
 *   - CBD 环路（r=5.6）与一环路（r=20）：圆环必然切进 8×8 街区内部；
 *   - 邻接次干道 connector（L 形）与共线腿合并逻辑：方格网取代之；
 *   - 高速联络线 edgeLink（**径向斜线**，斜向观感的主因）：环城高速（r=58，城外）
 *     改由外沿街道 `x=±42` / `z=±42` 的 **8 处正交交叉**自然形成互通。
 *
 * ## 平面交叉 z-fighting 分层（沿用批次 31 §2.4 口径）
 *   次干道 +0.002 / 主干道 +0.004 / 外环快速路 +0.006。
 *   交叉口处高等级路面压在低等级之上（读作浅层立交），高差 0.002~0.004u = 2~4 cm。
 */

import {
  CANAL_HALF_WIDTH,
  CANAL_STREET_Z,
  GRID_ARTERIAL_LINES,
  GRID_CELL,
  GRID_EXPRESSWAY_LINES,
  GRID_EW_STREET_LINES,
  GRID_STREET_LINES,
} from './cityGrid';

/** 街道线在城市中的跨度（端点恰在此处 ⇒ 每条街道是一条整长线段）。 */
export const GRID_STREET_SPAN = 42;

/**
 * 平面交叉微抬阶梯（§2.4；单位 u）。
 *
 * 批次 61 修订：方格网下**同级线段互相平行、但南北向与东西向同级线段在每个
 * 交叉口同 y 共面重叠** ⇒ 22 处 z-fighting 闪烁。修法：**给每一条街道线一个
 * 唯一的层级**，任一交叉点的两层 y 差 ≥ `Y_LINE_STEP`（0.0006 u = **6 mm**，
 * 远超 z-fighting 的 4 mm 阈值，且远低于视觉可辨的 1 cm）。
 *
 * 层级顺序 = **类别优先级**（快速路 > 主干道 > 次干道）× **线序**：
 *   level 0..3   外环快速路（南北 2 + 东西 2）
 *   level 4..6   主干道（南北 2 + 东西 1）
 *   level 7..14  次干道（南北 4 + 东西 4）
 * ⇒ `y = Y_BASE + level × Y_LINE_STEP`，全域跨度 0.0020 ~ 0.0104 u（0.2~1.04 cm）。
 */
export const Y_BASE = 0.002;
export const Y_LINE_STEP = 0.0006;

/** 直线段道路（grid / arterial / expressway 共用 Road 组件渲染）。 */
export interface RoadSegment {
  key: string;
  from: [number, number];
  to: [number, number];
  /** 'main' = 主干道 / 外环快速路（宽路幅 + 主干道贴图）；'side' = 次干道。 */
  kind: 'main' | 'side';
  /** 路网类别：'grid' 次干道 / 'arterial' 主干道 / 'expressway' 外环快速路。 */
  cls: 'grid' | 'arterial' | 'expressway';
  /** 平面交叉微抬（§2.4）。 */
  yOffset: number;
}

/** 干道交叉点（红绿灯 / 悬臂灯 / 停止线布点用）。 */
export interface RoadJunction {
  x: number;
  z: number;
  /** 交叉角（atan2(z,x) 口径；仅供稳定排序）。 */
  angle: number;
  /** 至少一侧是**主干道**（⇒ 布红绿灯）。 */
  major: boolean;
  /** 两侧都是主干道或快速路（⇒ 布悬臂信号灯，宽路口）。 */
  wide: boolean;
}

export interface RoadNetwork {
  segments: RoadSegment[];
  /** 全部正交交叉口（南北向街道 × 东西向街道；8 × 7 = 56）。 */
  junctions: RoadJunction[];
  /** 含主干道的交叉口（红绿灯布点）。 */
  arterialIntersections: RoadJunction[];
}

/**
 * 点 (px,pz) 到线段 a→b 的最短距离（批次 38：去私有化导出，cityObstacles 复用本实现，
 *  禁止复制第二份 —— 布局互知唯一入口 `cityObstacles.ts` 的 keep-out 判定同源）。
 */
export function pointSegDist(px: number, pz: number, a: [number, number], b: [number, number]): number {
  const dx = b[0] - a[0];
  const dz = b[1] - a[1];
  const A = dx * dx + dz * dz;
  if (A < 1e-9) return len2(a, [px, pz]);
  const t = Math.max(0, Math.min(1, ((px - a[0]) * dx + (pz - a[1]) * dz) / A));
  return len2([a[0] + dx * t, a[1] + dz * t], [px, pz]);
}

function len2(a: [number, number], b: [number, number]): number {
  const dx = b[0] - a[0];
  const dz = b[1] - a[1];
  return Math.sqrt(dx * dx + dz * dz);
}

/** 该线是否属于「主干道级」（主干道或外环快速路）。 */
function isArterialLine(v: number): boolean {
  return GRID_ARTERIAL_LINES.includes(v);
}
function isExpresswayLine(v: number): boolean {
  return GRID_EXPRESSWAY_LINES.includes(v);
}

/**
 * 生成全城道路网（确定性：纯几何，**无随机、无输入依赖**）。
 *
 * @param _districts 保留兼容（批次 31 起参数为城区表；批次 61 起路网只由方格网决定，
 *   城区表仅用于「城区是否落在街区中心上」的诊断，不再参与线段生成）。
 * @param _mainRoadMinLen 保留兼容（同上）。
 */
export function buildRoadNetwork(
  _districts?: readonly unknown[],
  _mainRoadMinLen?: number,
): RoadNetwork {
  const segments: RoadSegment[] = [];

  // ── 层级分配（类别优先级 × 线序；见 Y_BASE / Y_LINE_STEP 注释）──
  // 顺序：外环快速路 → 主干道 → 次干道；每类内先南北向、���东西向。
  const nsLines = GRID_STREET_LINES;
  const ewLines = GRID_EW_STREET_LINES;
  const levelOf = new Map<string, number>();
  let level = 0;
  const take = (axis: 'ns' | 'ew', v: number) => {
    levelOf.set(`${axis}:${v}`, level++);
  };
  for (const x of nsLines) {
    if (isExpresswayLine(x)) take('ns', x);
  }
  for (const z of ewLines) {
    if (isExpresswayLine(z)) take('ew', z);
  }
  for (const x of nsLines) {
    if (isArterialLine(x)) take('ns', x);
  }
  for (const z of ewLines) {
    if (isArterialLine(z)) take('ew', z);
  }
  for (const x of nsLines) {
    if (!isExpresswayLine(x) && !isArterialLine(x)) take('ns', x);
  }
  for (const z of ewLines) {
    if (!isExpresswayLine(z) && !isArterialLine(z)) take('ew', z);
  }
  const yFor = (axis: 'ns' | 'ew', v: number): number =>
    Y_BASE + (levelOf.get(`${axis}:${v}`) ?? 0) * Y_LINE_STEP;

  // ── ① 南北向街道（8 条；每条在运河处**拆成两段**，中间由桥面接上）──
  //   批次 61 修订：原实现整段跳过 `crossesCanal` 的路面 ⇒ 8 条街道在运河处
  //   **整条消失**（8×84u ≈ 67 km 路面不画）。修法：按运河两岸线把街道拆成
  //   南段 z ∈ [-42, 16.5] 与北段 z ∈ [19.5, 42]，中间的 3u 缺口由
  //   `CanalBridge` 的桥面承载（桥长 4.8u > 3u 缺口，两端各压 0.9u 引桥）。
  const canalSouth = CANAL_STREET_Z - CANAL_HALF_WIDTH; // 16.5
  const canalNorth = CANAL_STREET_Z + CANAL_HALF_WIDTH; // 19.5
  for (const x of GRID_STREET_LINES) {
    const cls = isExpresswayLine(x) ? 'expressway' : isArterialLine(x) ? 'arterial' : 'grid';
    const kind = cls === 'grid' ? 'side' : 'main';
    const y = yFor('ns', x);
    segments.push({
      key: `street-ns-${x}-s`,
      from: [x, -GRID_STREET_SPAN],
      to: [x, canalSouth],
      kind,
      cls,
      yOffset: y,
    });
    segments.push({
      key: `street-ns-${x}-n`,
      from: [x, canalNorth],
      to: [x, GRID_STREET_SPAN],
      kind,
      cls,
      yOffset: y,
    });
  }

  // ── ② 东西向街道（8 条 − 运河行 1 条 = 7 条，每条一条整长线段）──
  for (const z of GRID_EW_STREET_LINES) {
    const cls = isExpresswayLine(z) ? 'expressway' : isArterialLine(z) ? 'arterial' : 'grid';
    segments.push({
      key: `street-ew-${z}`,
      from: [-GRID_STREET_SPAN, z],
      to: [GRID_STREET_SPAN, z],
      kind: cls === 'grid' ? 'side' : 'main',
      cls,
      yOffset: yFor('ew', z),
    });
  }

  // ── ③ 交叉口（南北向 × 东西向；全部为**正交点**，且交点都在两条街的内部）──
  const junctions: RoadJunction[] = [];
  for (const x of GRID_STREET_LINES) {
    for (const z of GRID_EW_STREET_LINES) {
      const nsMajor = isArterialLine(x);
      const ewMajor = isArterialLine(z);
      const nsWide = nsMajor || isExpresswayLine(x);
      const ewWide = ewMajor || isExpresswayLine(z);
      junctions.push({
        x,
        z,
        angle: Math.atan2(z, x),
        major: nsMajor || ewMajor,
        wide: nsWide && ewWide,
      });
    }
  }
  junctions.sort((a, b) => a.angle - b.angle || a.x - b.x || a.z - b.z);

  const arterialIntersections = junctions.filter((j) => j.major);

  return { segments, junctions, arterialIntersections };
}

/** 街区中心集合是否与方格网一致（自检用；`cityGrid.ts` 为网格唯一事实来源）。 */
export function gridCellOf(x: number, z: number): [number, number] {
  return [Math.round(x / GRID_CELL) * GRID_CELL, Math.round(z / GRID_CELL) * GRID_CELL];
}

/** 运河占用的街道线（供 canalBridge 等消费；本模块不生成该行街道）。 */
export { CANAL_STREET_Z };
