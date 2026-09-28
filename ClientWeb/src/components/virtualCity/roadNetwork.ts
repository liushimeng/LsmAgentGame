/**
 * roadNetwork — 批次 31「混合式路网与一环路」：全城道路网单一事实来源（纯函数，无随机）。
 *
 * 设计文档：lag_docs/虚拟城市/已实现/31-混合式路网与一环路/01-方案设计.md
 *
 * 五层混合路网（环形放射式 + 方格网式 + 鱼骨式）：
 *   1. spoke     放射主干道 ×31：城区中心 → CBD 原点（批次 24 语义保留，len > mainRoadMinLen → main）
 *   2. connector 邻接次干道 ×≤36：每区连 2 最近邻（8u ≤ len ≤ 34u），居民区互联互通（方格/鱼骨感）
 *   3. arterial  方格骨干 ×4：x=±18 / z=±26 贯穿 ±48（选线已核对 32 区底板重叠 ≤1 块/条）
 *   4. edgeLink  高速联络线 ×≤16：中心 r>30 的区，放射路向外延长至高速环内缘 r=57.2
 *   5. gates     收费站 ×≤16：联络线 r=55 径向（龙门架 + 双收费亭，props/HighwayGates 渲染）
 *
 * 平面交叉 z-fighting 分层（设计 §2.4，非立交）：
 *   spoke/edgeLink +0；connector +0.002；arterial +0.004；一环路（FirstRingRoad）+0.007。
 *
 * 一环路（FIRST_RING r=20）为曲线环，不走本模块线段列表，由 FirstRingRoad 组件渲染；
 * firstRingJunctionAngles 提供放射主路与环交点角（停止线 + 红绿灯布点）。
 */

import { districtCenter, type VirtualCityDistrictDef } from '@/types/virtualCity';

// ── 一环路常量（FirstRingRoad 组件同读此处，单一事实来源）────────────────
/** 一环路半径（世界单位；内圈含 5 个内城区 tech/industry/oldtown/commerce/residential）。 */
export const FIRST_RING_RADIUS = 20;
/** 一环路路面宽（主干道断面 1.4u = 14m）。 */
export const FIRST_RING_WIDTH = 1.4;
/** 一环路分段数（32 段近似圆，段长 ≈ 3.9u）。 */
export const FIRST_RING_SEGMENTS = 32;
/** 一环路 y 微抬（§2.4 最高层，压所有下层平面交叉）。 */
export const FIRST_RING_Y_OFFSET = 0.007;
/** 高速环半径（Outskirts 环城高速同值；联络线终点 = 内缘）。 */
export const HIGHWAY_RING_RADIUS = 58;
/** 环城高速路面宽（Outskirts 同值 1.6u）。 */
export const HIGHWAY_RING_WIDTH = 1.6;
/** 收费站径向位置（r=55，联络线中段）。 */
export const GATE_RADIUS = 55;
/** 邻接次干道长度下限（8u = 80m，短于此即城区紧邻、无独立路意义）。 */
export const CONNECTOR_MIN_LEN = 8;
/** 邻接次干道长度上限（34u = 340m，防跨城长穿 + 道具超线性）。 */
export const CONNECTOR_MAX_LEN = 34;
/** 每区最近邻连接数。 */
export const CONNECTOR_NEIGHBORS = 2;
/** 邻接次干道总上限（防 draw call / 道具爆炸，超出按表序截断）。 */
export const CONNECTOR_CAP = 36;
/** 方格骨干横线 z 坐标 / 纵线 x 坐标（选线核对见设计 §4.1）。 */
export const ARTERIAL_LINES = [
  { axis: 'x' as const, at: -18 },
  { axis: 'x' as const, at: 18 },
  { axis: 'z' as const, at: -26 },
  { axis: 'z' as const, at: 26 },
];
/** 方格骨干半长（贯穿 [-48, 48]，城区带 |x|,|z| ≤ 50 之内）。 */
export const ARTERIAL_HALF = 48;
/** 平面交叉微抬（§2.4）。 */
export const Y_CONNECTOR = 0.002;
export const Y_ARTERIAL = 0.004;

/** 直线段道路（spoke / connector / arterial / edgeLink 共用 Road 组件渲染）。 */
export interface RoadSegment {
  key: string;
  from: [number, number];
  to: [number, number];
  kind: 'main' | 'side';
  cls: 'spoke' | 'connector' | 'arterial' | 'edgeLink';
  /** 平面交叉微抬（§2.4）。 */
  yOffset: number;
}

/** 收费站点位（props/HighwayGates 渲染契约）。 */
export interface GateSpot {
  x: number;
  z: number;
  /** 绕 Y 旋转：local +z 指向径向外（横梁横跨联络线）。 */
  rotation: number;
}

export interface RoadNetwork {
  segments: RoadSegment[];
  gates: GateSpot[];
  /** 放射主路与一环路交点角（弧度，atan2(z, x) 口径；FirstRingRoad 停止线 +
   *  红绿灯布点用）。 */
  firstRingJunctionAngles: number[];
}

function len2(a: [number, number], b: [number, number]): number {
  const dx = b[0] - a[0];
  const dz = b[1] - a[1];
  return Math.sqrt(dx * dx + dz * dz);
}

/**
 * 生成全城道路网（确定性：城区表序 + 几何距离，无随机）。
 * @param districts    城区静态表（VIRTUAL_CITY_DISTRICTS）
 * @param mainRoadMinLen 主干道判定阈值（VirtualCityCityMap.MAIN_ROAD_MIN_LEN，单一事实来源在调用方）
 */
export function buildRoadNetwork(
  districts: VirtualCityDistrictDef[],
  mainRoadMinLen: number,
): RoadNetwork {
  const nonCbd = districts.filter((d) => d.id !== 'finance');
  const segments: RoadSegment[] = [];
  const gates: GateSpot[] = [];

  // ── ① 放射主干道（现状语义保留）──
  for (const d of nonCbd) {
    const c = districtCenter(d.id);
    const len = len2([c.x, c.z], [0, 0]);
    segments.push({
      key: `spoke-${d.id}`,
      from: [c.x, c.z],
      to: [0, 0],
      kind: len > mainRoadMinLen ? 'main' : 'side',
      cls: 'spoke',
      yOffset: 0,
    });
  }

  // ── ② 邻接次干道（k 近邻互通；去重；长度/总量双上限）──
  const centers = nonCbd.map((d) => ({ id: d.id, c: districtCenter(d.id) }));
  const seen = new Set<string>();
  for (const { id, c } of centers) {
    if (segments.filter((s) => s.cls === 'connector').length >= CONNECTOR_CAP) break;
    const neighbors = centers
      .filter((o) => o.id !== id)
      .map((o) => ({ id: o.id, d: len2([c.x, c.z], [o.c.x, o.c.z]) }))
      .filter((o) => o.d >= CONNECTOR_MIN_LEN && o.d <= CONNECTOR_MAX_LEN)
      .sort((a, b) => a.d - b.d || a.id.localeCompare(b.id))
      .slice(0, CONNECTOR_NEIGHBORS);
    for (const n of neighbors) {
      const pair = [id, n.id].sort().join('~');
      if (seen.has(pair)) continue;
      seen.add(pair);
      const nc = centers.find((o) => o.id === n.id)!.c;
      segments.push({
        key: `conn-${pair}`,
        from: [c.x, c.z],
        to: [nc.x, nc.z],
        kind: 'side',
        cls: 'connector',
        yOffset: Y_CONNECTOR,
      });
    }
  }

  // ── ③ 方格骨干（x=±18 / z=±26，贯穿 ±48）──
  for (const line of ARTERIAL_LINES) {
    const from: [number, number] =
      line.axis === 'x' ? [-ARTERIAL_HALF, line.at] : [line.at, -ARTERIAL_HALF];
    const to: [number, number] =
      line.axis === 'x' ? [ARTERIAL_HALF, line.at] : [line.at, ARTERIAL_HALF];
    segments.push({
      key: `arterial-${line.axis}${line.at}`,
      from,
      to,
      kind: 'main',
      cls: 'arterial',
      yOffset: Y_ARTERIAL,
    });
  }

  // ── ④ 高速联络线（中心 r>30 的区：放射路向外延长到高速内缘）+ ⑤ 收费站 ──
  for (const { id, c } of centers) {
    const r = Math.sqrt(c.x * c.x + c.z * c.z);
    if (r <= 30) continue;
    const ux = c.x / r;
    const uz = c.z / r;
    const endR = HIGHWAY_RING_RADIUS - HIGHWAY_RING_WIDTH / 2 - 0.2; // 57.2
    segments.push({
      key: `edge-${id}`,
      from: [c.x, c.z],
      to: [ux * endR, uz * endR],
      kind: 'main',
      cls: 'edgeLink',
      yOffset: 0,
    });
    gates.push({
      x: ux * GATE_RADIUS,
      z: uz * GATE_RADIUS,
      rotation: Math.atan2(ux, uz),
    });
  }

  // ── 一环路交点角：放射主路（len > 环半径）与环的交点（atan2(z,x) 口径）──
  const firstRingJunctionAngles = nonCbd
    .map((d) => districtCenter(d.id))
    .filter((c) => Math.sqrt(c.x * c.x + c.z * c.z) > FIRST_RING_RADIUS)
    .map((c) => Math.atan2(c.z, c.x))
    .sort((a, b) => a - b);

  return { segments, gates, firstRingJunctionAngles };
}
