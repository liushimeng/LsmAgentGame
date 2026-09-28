/**
 * roadNetwork — 批次 31「混合式路网与一环路」：全城道路网单一事实来源（纯函数，无随机）。
 *
 * 设计文档：lag_docs/虚拟城市/已实现/31-混合式路网与一环路/01-方案设计.md
 *
 * 四层混合路网（方格网式 + 鱼骨式 + 环形；2026-09-28 用户反馈删 CBD 星型放射后）：
 *   1. connector 邻接次干道 ×≤36：每区连 2 最近邻（8u ≤ len ≤ 34u），居民区互联互通（方格/鱼骨感）
 *   2. arterial  方格骨干 ×4：x=±18 / z=±26 贯穿 ±48（选线已核对 32 区底板重叠 ≤1 块/条）
 *   3. edgeLink  高速联络线 ×≤17：中心 r>30 的区，向外延长至高速环内缘 r=57.2
 *   4. gates     收费站 ×≤17：联络线 r=55 径向（龙门架 + 双收费亭，props/HighwayGates 渲染）
 *
 * ~~spoke 放射主干道 ×31~~：**已删除**（2026-09-28 用户反馈：CBD 向外星型辐射路
 * 与建筑 3D 模型重叠、不符合真实路网——放射语义整体移除，CBD 经一环路/方格骨干/
 * 邻接次干道接入全网；后端无道路逻辑，grep 实证零改动）。
 *
 * 平面交叉 z-fighting 分层（设计 §2.4，非立交）：
 *   edgeLink +0；connector +0.002；arterial +0.004；一环路（FirstRingRoad）+0.007。
 *
 * 一环路（FIRST_RING r=20）与 CBD 环路（r=5.6）为曲线环，不走本模块线段列表，
 * 由 FirstRingRoad / RingRoad 组件渲染；cbdRingJunctionAngles /
 * firstRingJunctionAngles = **线段与环圆的真实交点角**（通用 segment-圆求交），
 * arterialIntersections = 方格骨干互交点（红绿灯布点用）。
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
/** CBD 建筑区禁入半径（世界单位）：connector 线段距原点小于此值即拒绝——
 *  穿金融 CBD 底板（半宽 4.05）的路与建筑 3D 模型重叠（2026-09-28 用户反馈）。 */
export const CONNECTOR_CBD_KEEP_OUT = 5.0;

/** 点 (px,pz) 到线段 a→b 的最短距离。 */
function pointSegDist(px: number, pz: number, a: [number, number], b: [number, number]): number {
  const dx = b[0] - a[0];
  const dz = b[1] - a[1];
  const A = dx * dx + dz * dz;
  if (A < 1e-9) return len2(a, [px, pz]);
  const t = Math.max(0, Math.min(1, ((px - a[0]) * dx + (pz - a[1]) * dz) / A));
  return len2([a[0] + dx * t, a[1] + dz * t], [px, pz]);
}
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
  cls: 'connector' | 'arterial' | 'edgeLink';
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

/** 干道交叉点（红绿灯布点用）。 */
export interface RoadJunction {
  x: number;
  z: number;
  /** 交叉角（atan2(z,x) 口径；环交点 = 交点角）。 */
  angle: number;
}

export interface RoadNetwork {
  segments: RoadSegment[];
  gates: GateSpot[];
  /** CBD 环路（r=5.6）与线段的交点角（RingRoad 停止线 + 信号灯）。 */
  cbdRingJunctionAngles: number[];
  /** 一环路（r=20）与线段的交点角（FirstRingRoad 停止线 + 信号灯）。 */
  firstRingJunctionAngles: number[];
  /** 方格骨干互交点（arterial × arterial，信号灯布点）。 */
  arterialIntersections: RoadJunction[];
}

function len2(a: [number, number], b: [number, number]): number {
  const dx = b[0] - a[0];
  const dz = b[1] - a[1];
  return Math.sqrt(dx * dx + dz * dz);
}

/**
 * 生成全城道路网（确定性：城区表序 + 几何距离，无随机）。
 * @param districts    城区静态表（VIRTUAL_CITY_DISTRICTS）
 * @param _mainRoadMinLen 保留兼容（放射路删除后不再使用主干道阈值；调用方仍传
 *   VirtualCityCityMap.MAIN_ROAD_MIN_LEN，后续批次若复用可唤醒）
 */
export function buildRoadNetwork(
  districts: VirtualCityDistrictDef[],
  _mainRoadMinLen: number,
): RoadNetwork {
  const nonCbd = districts.filter((d) => d.id !== 'finance');
  const segments: RoadSegment[] = [];
  const gates: GateSpot[] = [];

  // ── ① 邻接次干道（k 近邻互通；去重；长度/总量双上限）──
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
      const nc = centers.find((o) => o.id === n.id)!.c;
      // CBD 禁入：穿金融 CBD 建筑区的邻接路拒绝（防 3D 重叠，用户反馈 2026-09-28）
      if (pointSegDist(0, 0, [c.x, c.z], [nc.x, nc.z]) < CONNECTOR_CBD_KEEP_OUT) continue;
      seen.add(pair);
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

  // ── ② 方格骨干（x=±18 / z=±26，贯穿 ±48）──
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

  // ── ③ 高速联络线（中心 r>30 的区：向外延长到高速内缘）+ ④ 收费站 ──
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

  // ── 环交点角：线段与环圆的真实交点（通用求交；无放射路后由 connector /
  //    arterial 与环的真实穿越构成，CBD 环可能零交点 = 纯环岛，符合真实语义）──
  const ringJunctionAngles = (radius: number): number[] => {
    const angles: number[] = [];
    for (const seg of segments) {
      const ax = seg.from[0];
      const az = seg.from[1];
      const bx = seg.to[0];
      const bz = seg.to[1];
      const dx = bx - ax;
      const dz = bz - az;
      const A = dx * dx + dz * dz;
      if (A < 1e-9) continue;
      const B = 2 * (ax * dx + az * dz);
      const C = ax * ax + az * az - radius * radius;
      const disc = B * B - 4 * A * C;
      if (disc < 0) continue;
      const sq = Math.sqrt(disc);
      for (const t of [(-B - sq) / (2 * A), (-B + sq) / (2 * A)]) {
        if (t <= 0.001 || t >= 0.999) continue; // 端点在环外/环上不生成交点（路口段不算）
        angles.push(Math.atan2(az + dz * t, ax + dx * t));
      }
    }
    // 角度去重（多线近乎同点穿越时合并，0.05 rad ≈ 1u @ r=20）
    angles.sort((a, b) => a - b);
    const out: number[] = [];
    for (const a of angles) {
      if (!out.length || Math.abs(a - out[out.length - 1]) > 0.05) out.push(a);
    }
    return out;
  };
  const cbdRingJunctionAngles = ringJunctionAngles(5.6);
  const firstRingJunctionAngles = ringJunctionAngles(FIRST_RING_RADIUS);

  // ── 方格骨干互交点（arterial × arterial；x=±18 × z=±26 中 r<20 的城内交叉）──
  const arterialIntersections: RoadJunction[] = [];
  const arterials = segments.filter((s) => s.cls === 'arterial');
  for (let i = 0; i < arterials.length; i++) {
    for (let j = i + 1; j < arterials.length; j++) {
      const A = arterials[i];
      const B = arterials[j];
      // 仅横×纵相交（同向平行无交点）
      const aDx = A.to[0] - A.from[0];
      const aDz = A.to[1] - A.from[1];
      const bDx = B.to[0] - B.from[0];
      const bDz = B.to[1] - B.from[1];
      const denom = aDx * bDz - aDz * bDx;
      if (Math.abs(denom) < 1e-9) continue;
      const t = ((B.from[0] - A.from[0]) * bDz - (B.from[1] - A.from[1]) * bDx) / denom;
      const u = ((B.from[0] - A.from[0]) * aDz - (B.from[1] - A.from[1]) * aDx) / denom;
      if (t < 0.01 || t > 0.99 || u < 0.01 || u > 0.99) continue;
      const x = A.from[0] + aDx * t;
      const z = A.from[1] + aDz * t;
      arterialIntersections.push({ x, z, angle: Math.atan2(z, x) });
    }
  }
  arterialIntersections.sort((a, b) => a.angle - b.angle);

  return { segments, gates, cbdRingJunctionAngles, firstRingJunctionAngles, arterialIntersections };
}
