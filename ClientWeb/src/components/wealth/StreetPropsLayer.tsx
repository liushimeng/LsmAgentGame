/**
 * StreetPropsLayer — 街景道具布点协调器（P1-C）：
 *
 * 总览：
 *   - 每个城区：4 棵树（central_park 10 棵）+ 1 根路灯 + 1-2 件屋顶杂物 + 1 行人 + 1 标识牌
 *   - 每条主干道沿线：1 辆移动车辆
 *
 * 布点策略：确定性伪随机（mulberry32 + hashStr），重渲染布局稳定（CLAUDE.md §3 风格一致）。
 * 与 DistrictBlock.tsx 的随机算法保持同源（DistrictDefs.id hashStr）。
 *
 * v2.12 阶段 2：**props 化** —— 城区清单由父层 `districts` 注入（不再 import
 * WEALTH_DISTRICTS 静态表），数量随城区表自适应（8 区或 16 区同一代码路径）。
 *
 * v2.13 阶段 D（13-3D城市渲染优化）：每条主干道 t=0.12 路侧布 1 个红绿灯
 * （props/TrafficLight，替换从未接线的空 intersectionSigns 字段，§130）。
 *
 * 预算：mesh 总数 ≤ 2500（v2.12 阶段 2 上限；16 城区实际 ≈ 每城区 ~5 × 16
 * + 主干道车辆 ~12 ≈ 92，远低于护栏）。
 *
 * 限制：
 *   - props 默认 castShadow=false（仅 Tree 保留）
 *   - 不动 camera / orbit / token / 道路（道路自带路灯阵列，本层不再加）
 *   - 性能护栏：避免每帧 React re-render，seed 由 useMemo 缓存
 */

import { useMemo } from 'react';
import {
  districtCenter,
  type WealthDistrictDef,
} from '@/types/wealth';
import { DISTRICT_FLOORS, buildingHeight } from './cityScale';
import { Tree } from './props/Tree';
import { Vehicle } from './props/Vehicle';
import { Pedestrian } from './props/Pedestrian';
import { Sign } from './props/Sign';
import { RooftopAcc } from './props/RooftopAcc';
import { TrafficLight } from './props/TrafficLight';
import { BusStop } from './props/BusStop';
import { StreetFurniture } from './props/StreetFurniture';

// ── 确定性伪随机（与 DistrictBlock.tsx 同源）────────────────

function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

interface DistrictProps {
  districtId: string;
  trees: Array<{ x: number; z: number; variant: 'oak' | 'pine' | 'palm'; scale: number }>;
  /** 楼顶杂物（yOffset = 城区中位楼高 ×0.9，见 cityScale.ts）。 */
  rooftop: Array<{ x: number; z: number; variant: 'ac' | 'tank' | 'antenna'; rotation: number; yOffset: number }>;
  /** 城区内行人（14-3D渲染深化：pathR 环线漫步半径）。 */
  pedestrian: { x: number; z: number; variant: 'warm' | 'cool'; seed: number; pathR: number };
  /** 中央公园第 2 个漫步行人（绿化区多一人，契约 02 §7）。 */
  pedestrian2?: { x: number; z: number; variant: 'warm' | 'cool'; seed: number; pathR: number };
  /** 城区内标识牌（路口处）。 */
  sign: { x: number; z: number; rotation: number; variant: 'traffic' | 'info' };
  /** 街道小件家具（14-3D渲染深化：bench/hydrant 各城区 1 件）。 */
  furniture: { x: number; z: number; variant: 'bench' | 'hydrant'; rotation: number };
}

interface RoadVehicle {
  from: [number, number];
  to: [number, number];
  variant: 'sedan' | 'truck' | 'bus' | 'taxi';
  speed: number;
  phase: number;
}

interface Layout {
  districtProps: DistrictProps[];
  roadVehicles: RoadVehicle[];
  /** 主干道路侧红绿灯（v2.13 阶段 D；t=0.12，与车辆同路判定）。 */
  trafficLights: Array<{ x: number; z: number; rotation: number }>;
  /** 主干道路侧公交站台（14-3D渲染深化；t=0.35）。 */
  busStops: Array<{ x: number; z: number; rotation: number }>;
}

const TREE_VARIANTS: Array<'oak' | 'pine' | 'palm'> = ['oak', 'pine', 'palm'];
const ROOFTOP_VARIANTS: Array<'ac' | 'tank' | 'antenna'> = ['ac', 'tank', 'antenna'];
const VEHICLE_VARIANTS: Array<'sedan' | 'truck' | 'bus' | 'taxi'> = ['sedan', 'truck', 'bus', 'taxi'];

/** 为单个城区生成 props。 */
function propsForDistrict(def: WealthDistrictDef, idx: number): DistrictProps {
  const rnd = mulberry32(hashStr(def.id));
  const c = { x: def.x, z: def.z };
  // 城区朝向 finance（原点）的方向角（标识牌/树避让共用）
  const angleToFinance = Math.atan2(-c.z, -c.x);

  // 13-3D优化 阶段 B/D 树群（契约 02 文档 §2.4/§3.4）：
  //   central_park → 10 棵（半径 1.5~3.5 散布，scale 0.8~1.2，绿化率 >60%）；
  //   其余城区     → 4 棵沿城区边缘（半径 3.6±0.3），避开路口 sign 角度 ±0.4 rad。
  const isPark = def.id === 'central_park';
  const treeCount = isPark ? 10 : 4;
  const angleDiff = (a: number, b: number) => {
    let d = a - b;
    while (d > Math.PI) d -= Math.PI * 2;
    while (d < -Math.PI) d += Math.PI * 2;
    return Math.abs(d);
  };
  const trees = Array.from({ length: treeCount }, (_, i) => {
    let angle = (i / treeCount) * Math.PI * 2 + (rnd() - 0.5) * 0.6;
    if (!isPark && angleDiff(angle, angleToFinance) < 0.4) angle += 0.5; // 避让路口标识牌
    const radius = isPark ? 1.5 + rnd() * 2.0 : 3.3 + rnd() * 0.6;
    return {
      x: c.x + Math.cos(angle) * radius,
      z: c.z + Math.sin(angle) * radius,
      variant: TREE_VARIANTS[Math.floor(rnd() * TREE_VARIANTS.length)],
      // 行道树 5~10m → scale 0.5~1.0；公园乔木 8~12m → 0.8~1.2（见 cityScale.ts）
      scale: isPark ? 0.8 + rnd() * 0.4 : 0.5 + rnd() * 0.5,
    };
  });

  // 1-2 个楼顶杂物：放在城区中心附近的虚拟楼栋上
  // 楼顶高度：按城区中位楼高 ×0.9（2026-09-21 高度系统，去掉硬编码 2.8+rnd*1.4，
  // 杂物不再悬空 / 埋楼，郊区低层也贴合）。
  const [minF, maxF] = DISTRICT_FLOORS[def.id];
  const roofY = buildingHeight((minF + maxF) / 2) * 0.9;
  const rooftop = [0, 1].slice(0, 1 + (rnd() < 0.5 ? 1 : 0)).map(() => {
    const angle = rnd() * Math.PI * 2;
    const radius = 0.8 + rnd() * 1.6;
    return {
      x: c.x + Math.cos(angle) * radius,
      z: c.z + Math.sin(angle) * radius,
      variant: ROOFTOP_VARIANTS[Math.floor(rnd() * ROOFTOP_VARIANTS.length)],
      rotation: rnd() * Math.PI * 2,
      yOffset: roofY,
    };
  });

  // 1 个行人：城区内小环线漫步（14-3D渲染深化；pathR 0.8–2.0 确定性）
  const pedAngle = rnd() * Math.PI * 2;
  const pedestrian = {
    x: c.x + Math.cos(pedAngle) * 0.5,
    z: c.z + Math.sin(pedAngle) * 0.5,
    variant: (rnd() < 0.5 ? 'warm' : 'cool') as 'warm' | 'cool',
    seed: idx + rnd(),
    pathR: 0.8 + rnd() * 1.2,
  };
  // 中央公园第 2 个漫行人
  const pedestrian2 = isPark
    ? {
        x: c.x + Math.cos(pedAngle + 2) * 0.5,
        z: c.z + Math.sin(pedAngle + 2) * 0.5,
        variant: (rnd() < 0.5 ? 'warm' : 'cool') as 'warm' | 'cool',
        seed: idx + 1 + rnd(),
        pathR: 1.2 + rnd() * 0.8,
      }
    : undefined;

  // 街道小件家具（14-3D渲染深化：idx 偶=长椅 / 奇=消防栓，树池旁半径 2.6）
  const furnAngle = rnd() * Math.PI * 2;
  const furniture = {
    x: c.x + Math.cos(furnAngle) * 2.6,
    z: c.z + Math.sin(furnAngle) * 2.6,
    variant: (idx % 2 === 0 ? 'bench' : 'hydrant') as 'bench' | 'hydrant',
    rotation: rnd() * Math.PI * 2,
  };

  // 路口标识牌：放在城区靠近 finance 一侧（angleToFinance 已在树群段计算）
  const signR = 3.5;
  const sign = {
    x: c.x + Math.cos(angleToFinance) * signR,
    z: c.z + Math.sin(angleToFinance) * signR,
    rotation: angleToFinance + Math.PI / 2,
    variant: (idx % 2 === 0 ? 'traffic' : 'info') as 'traffic' | 'info',
  };

  return {
    districtId: def.id,
    trees,
    rooftop,
    pedestrian,
    pedestrian2,
    sign,
    furniture,
  };
}

/** 为每条主干道生成 1 辆移动车辆（districts 由 props 注入，v2.12 阶段 2）。 */
function vehiclesForRoads(districts: WealthDistrictDef[]): RoadVehicle[] {
  const roads: RoadVehicle[] = [];
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d, i) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < 12) return; // 只在主干道放车
      const variant = VEHICLE_VARIANTS[i % VEHICLE_VARIANTS.length];
      // 不同车不同速度
      const speedMap = { sedan: 0.07, truck: 0.04, bus: 0.05, taxi: 0.08 };
      roads.push({
        from: [c.x, c.z],
        to: [0, 0],
        variant,
        speed: speedMap[variant],
        // 不同车不同起始相位
        phase: (i * 0.37) % 1,
      });
    });
  return roads;
}

/**
 * 为每条主干道布 1 个路侧红绿灯（v2.13 阶段 D，02-架构 §3.2）：
 * t=0.12（靠近城区入口，与斑马线 t=0.08 相邻成组），路侧偏移方向与
 * Road.tsx 路灯阵列一致（垂直向量 (-dz/len, dx/len) × (roadWidth/2 + 0.35)，
 * 主干道 ROAD_WIDTH_MAIN=1.4 → 偏移 1.05）；rotation = angle + π（面向来车）。
 */
function trafficLightsForRoads(districts: WealthDistrictDef[]): Layout['trafficLights'] {
  const out: Layout['trafficLights'] = [];
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < 12) return; // 仅主干道（与 vehiclesForRoads 同判定）
      const t = 0.12;
      const nx = -dz / len;
      const nz = dx / len;
      const sideOffset = 1.4 / 2 + 0.35; // ROAD_WIDTH_MAIN/2 + 路肩
      out.push({
        x: c.x + dx * t + nx * sideOffset,
        z: c.z + dz * t + nz * sideOffset,
        rotation: Math.atan2(dx, dz) + Math.PI, // 面向来车（from → to 方向的反方向来车）
      });
    });
  return out;
}

/**
 * 为每条主干道布 1 个路侧公交站台（14-3D渲染深化，02-架构 §3.2/§7）：
 * t=0.35，路侧偏移与红绿灯同公式（面向路侧）。
 */
function busStopsForRoads(districts: WealthDistrictDef[]): Layout['busStops'] {
  const out: Layout['busStops'] = [];
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < 12) return; // 仅主干道（与 vehiclesForRoads 同判定）
      const t = 0.35;
      const nx = -dz / len;
      const nz = dx / len;
      const sideOffset = 1.4 / 2 + 0.35; // ROAD_WIDTH_MAIN/2 + 路肩
      out.push({
        x: c.x + dx * t + nx * sideOffset,
        z: c.z + dz * t + nz * sideOffset,
        rotation: Math.atan2(dx, dz),
      });
    });
  return out;
}

interface StreetPropsLayerProps {
  /** 城区静态表（v2.12 阶段 2 props 化；由 WealthCityMap 注入 WEALTH_DISTRICTS）。 */
  districts: WealthDistrictDef[];
}

export function StreetPropsLayer({ districts }: StreetPropsLayerProps) {
  const layout = useMemo<Layout>(() => {
    const districtProps = districts.map((d, idx) => propsForDistrict(d, idx));
    const roadVehicles = vehiclesForRoads(districts);
    const trafficLights = trafficLightsForRoads(districts);
    const busStops = busStopsForRoads(districts);
    return { districtProps, roadVehicles, trafficLights, busStops };
  }, [districts]);

  return (
    <>
      {/* 城区树 + 楼顶杂物 + 行人 + 标识 */}
      {layout.districtProps.map((dp) => (
        <group key={dp.districtId}>
          {dp.trees.map((t, i) => (
            <Tree key={`tree-${i}`} x={t.x} z={t.z} variant={t.variant} scale={t.scale} />
          ))}
          {dp.rooftop.map((r, i) => (
            <RooftopAcc
              key={`roof-${i}`}
              x={r.x}
              y={r.yOffset}
              z={r.z}
              variant={r.variant}
              rotation={r.rotation}
            />
          ))}
          <Pedestrian
            x={dp.pedestrian.x}
            z={dp.pedestrian.z}
            variant={dp.pedestrian.variant}
            seed={dp.pedestrian.seed}
            pathR={dp.pedestrian.pathR}
          />
          {dp.pedestrian2 && (
            <Pedestrian
              x={dp.pedestrian2.x}
              z={dp.pedestrian2.z}
              variant={dp.pedestrian2.variant}
              seed={dp.pedestrian2.seed}
              pathR={dp.pedestrian2.pathR}
            />
          )}
          <StreetFurniture
            x={dp.furniture.x}
            z={dp.furniture.z}
            variant={dp.furniture.variant}
            rotation={dp.furniture.rotation}
          />
          <Sign
            x={dp.sign.x}
            z={dp.sign.z}
            rotation={dp.sign.rotation}
            variant={dp.sign.variant}
          />
        </group>
      ))}

      {/* 主干道车辆 */}
      {layout.roadVehicles.map((v, i) => (
        <Vehicle
          key={`vehicle-${i}`}
          from={v.from}
          to={v.to}
          variant={v.variant}
          speed={v.speed}
          phase={v.phase}
        />
      ))}

      {/* 主干道红绿灯（v2.13 阶段 D） */}
      {layout.trafficLights.map((tl, i) => (
        <TrafficLight key={`tl-${i}`} x={tl.x} z={tl.z} rotation={tl.rotation} />
      ))}

      {/* 主干道公交站台（14-3D渲染深化） */}
      {layout.busStops.map((bs, i) => (
        <BusStop key={`busstop-${i}`} x={bs.x} z={bs.z} rotation={bs.rotation} />
      ))}
    </>
  );
}