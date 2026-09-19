/**
 * StreetPropsLayer — 街景道具布点协调器（P1-C）：
 *
 * 总览：
 *   - 每个城区：2 棵树 + 1 根路灯 + 1-2 件屋顶杂物
 *   - 每条主干道沿线：1 辆移动车辆 + 1 个行人
 *
 * 布点策略：确定性伪随机（mulberry32 + hashStr），重渲染布局稳定（CLAUDE.md §3 风格一致）。
 * 与 DistrictBlock.tsx 的随机算法保持同源（DistrictDefs.id hashStr）。
 *
 * 预算：mesh 总数 ≤ 80（每城区 ~5 × 8 + 主干道 ~3 × 7 ≈ 60）。
 *
 * 限制：
 *   - props 默认 castShadow=false（仅 Tree 保留）
 *   - 不动 camera / orbit / token / 道路（道路自带路灯阵列，本层不再加）
 *   - 性能护栏：避免每帧 React re-render，seed 由 useMemo 缓存
 */

import { useMemo } from 'react';
import {
  WEALTH_DISTRICTS,
  districtCenter,
  type WealthDistrictDef,
} from '@/types/wealth';
import { Tree } from './props/Tree';
import { Vehicle } from './props/Vehicle';
import { Pedestrian } from './props/Pedestrian';
import { Sign } from './props/Sign';
import { RooftopAcc } from './props/RooftopAcc';

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
  /** 楼顶杂物（接 DistrictBlock 楼栋高度；y 在父层估算）。 */
  rooftop: Array<{ x: number; z: number; variant: 'ac' | 'tank' | 'antenna'; rotation: number; yOffset: number }>;
  /** 城区内行人。 */
  pedestrian: { x: number; z: number; variant: 'warm' | 'cool'; seed: number };
  /** 城区内标识牌（路口处）。 */
  sign: { x: number; z: number; rotation: number; variant: 'traffic' | 'info' };
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
  /** 主干道路口的标识牌（额外补充，区别于 Road.tsx 自带路灯）。 */
  intersectionSigns: Array<{ x: number; z: number; rotation: number; variant: 'traffic' | 'info' }>;
}

const TREE_VARIANTS: Array<'oak' | 'pine' | 'palm'> = ['oak', 'pine', 'palm'];
const ROOFTOP_VARIANTS: Array<'ac' | 'tank' | 'antenna'> = ['ac', 'tank', 'antenna'];
const VEHICLE_VARIANTS: Array<'sedan' | 'truck' | 'bus' | 'taxi'> = ['sedan', 'truck', 'bus', 'taxi'];

/** 为单个城区生成 props。 */
function propsForDistrict(def: WealthDistrictDef, idx: number): DistrictProps {
  const rnd = mulberry32(hashStr(def.id));
  const c = { x: def.x, z: def.z };

  // 2 棵树：分别放在城区四角（角度均匀偏移 + 抖动）
  const trees = [0, 1].map((i) => {
    const baseAngle = (i / 2) * Math.PI * 2 + Math.PI / 4;
    const jitterA = (rnd() - 0.5) * 0.6;
    const angle = baseAngle + jitterA;
    const radius = 3.4 + rnd() * 0.4;
    return {
      x: c.x + Math.cos(angle) * radius,
      z: c.z + Math.sin(angle) * radius,
      variant: TREE_VARIANTS[Math.floor(rnd() * TREE_VARIANTS.length)],
      scale: 0.7 + rnd() * 0.4,
    };
  });

  // 1-2 个楼顶杂物：放在城区中心附近的虚拟楼栋上
  // 简化：把楼顶杂物的位置用 y 偏移代替（实际楼栋高度由 DistrictBlock 控制；
  // 这里用 RooftopAcc 的 y=3 估算楼顶位置，足够让杂物出现在"楼上方"）
  const rooftop = [0, 1].slice(0, 1 + (rnd() < 0.5 ? 1 : 0)).map(() => {
    const angle = rnd() * Math.PI * 2;
    const radius = 0.8 + rnd() * 1.6;
    return {
      x: c.x + Math.cos(angle) * radius,
      z: c.z + Math.sin(angle) * radius,
      variant: ROOFTOP_VARIANTS[Math.floor(rnd() * ROOFTOP_VARIANTS.length)],
      rotation: rnd() * Math.PI * 2,
      // 楼顶高度估算：基高 1 + prosperity*4 + jitter，范围 ~3-5
      yOffset: 2.8 + rnd() * 1.4,
    };
  });

  // 1 个行人：在城区内某点
  const pedAngle = rnd() * Math.PI * 2;
  const pedR = 1.5 + rnd() * 1.5;
  const pedestrian = {
    x: c.x + Math.cos(pedAngle) * pedR,
    z: c.z + Math.sin(pedAngle) * pedR,
    variant: (rnd() < 0.5 ? 'warm' : 'cool') as 'warm' | 'cool',
    seed: idx + rnd(),
  };

  // 路口标识牌：放在城区靠近 finance 一侧（沿 → (0,0) 方向）
  const angleToFinance = Math.atan2(-c.z, -c.x);
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
    sign,
  };
}

/** 为每条主干道生成 1 辆移动车辆。 */
function vehiclesForRoads(): RoadVehicle[] {
  const roads: RoadVehicle[] = [];
  WEALTH_DISTRICTS
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

export function StreetPropsLayer() {
  const layout = useMemo<Layout>(() => {
    const districtProps = WEALTH_DISTRICTS.map((d, idx) => propsForDistrict(d, idx));
    const roadVehicles = vehiclesForRoads();
    // 主干道路口（finance 与最长道路交叉点）
    const intersectionSigns: Layout['intersectionSigns'] = [];
    return { districtProps, roadVehicles, intersectionSigns };
  }, []);

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
    </>
  );
}