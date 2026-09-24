/**
 * StreetPropsLayer — 街景道具布点协调器（15-3D城市全面真实感深化 · 升级版）：
 *
 * 阶段 K 行人升级：
 *   - V1 单行人原地微抖 → V2 折线 path 漫步 + 圆柱身体 + 头部 Billboard sprite
 *   - 密度：核心城区 5-6 人 / 一般城区 3-4 人 / 郊区 2 人（按城区属性分档）
 *
 * 18 · 阶段 Z 行人升级：
 *   - V2 圆柱+Billboard「图钉」→ V3 体积行人（头/躯干/双臂双腿 + 摆臂摆腿）
 *   - 全城行人硬上限 56（密度表原合计 59，按「核心区优先、郊区先减」裁 3，见 PEDESTRIAN_DENSITY）
 *   - outfit 改 0..3 索引（对应 PEDESTRIAN_OUTFITS 四套服装色），phase 错开步态
 *
 * 阶段 L 树木升级：
 *   - V1 单 sphere 树冠 / Billboard → V2 3 层 sphere 叠加 + 行道树沿主干道
 *
 * 阶段 M 街道家具：
 *   - 新增 6 种家具：报刊亭 / 自行车 / 垃圾箱 / 电话亭 / 邮筒 / 停车牌
 *
 * 阶段 P 远景层：
 *   - FarBuildingSilhouette 由 AtmosphereLayer 注入，本层不再处理
 *
 * 总 mesh 预算核算（批次 20 InstancedMesh 改造后结构实测口径）：
 *   - 楼栋 ~240（DistrictBlock 持有，不变）
 *   - 行人 上限 110（V3 逐体动画 mixer，不实例化）→ ≤660 mesh
 *   - 树：行道树 + 区内树合并 <TreesInstanced>（主干/分枝/冠三段 InstancedMesh，
 *     ~490 棵 1 draw call/段 = 3 draw call，改造前 ~2.4k mesh）
 *   - 路灯：全道路汇总 <StreetLightsInstanced>（底座/主杆/灯头三段，
 *     ~370 盏 = 3 draw call，改造前每盏 3-4 mesh 且挂在旋转组内点位错算）
 *   - 家具/标识 60-80 + 车辆 ~50（GLB clone，不实例化）
 *   - draw call 目标 ≤1500 / mesh ≤3500；?debug=1 时 window.__cityRenderInfo
 *     可查 renderer.info 实时值（WealthCityMap onCreated 挂载，实测记入批次 20 实施记录）
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §1
 * + 批次 20 文档 1 §2.3 / §3.2 / §3.3。
 */

import { useMemo } from 'react';
import {
  districtCenter,
  type WealthDistrictDef,
} from '@/types/wealth';
import { DISTRICT_FLOORS, buildingHeight } from './cityScale';
import { MAIN_ROAD_MIN_LEN } from './WealthCityMap';
import { TreesInstanced } from './props/TreesInstanced'; // 批次 20 §3.3：TreeV3 逐实例 → 全局 InstancedMesh（形态同源）
import { Vehicle } from './props/Vehicle';
import { PedestrianV3, type PedestrianV3Props } from './props/PedestrianV3';
import { Sign } from './props/Sign';
import { RooftopAcc } from './props/RooftopAcc';
import { TrafficLight } from './props/TrafficLight';
import { BusStop } from './props/BusStop';
import { SolarPanel } from './props/SolarPanel';
import { VendorKiosk } from './props/VendorKiosk';
import { BicycleRack } from './props/BicycleRack';
import { TrashCan } from './props/TrashCan';
import { PhoneBooth } from './props/PhoneBooth';
import { Mailbox } from './props/Mailbox';
import { ParkingMeter } from './props/ParkingMeter';

// ── 确定性伪随机（同源 DistrictBlock.tsx）────────────────

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

interface TreeSpec {
  x: number;
  z: number;
  variant: 'oak' | 'pine' | 'palm';
  scale: number;
}

interface RooftopSpec {
  x: number;
  z: number;
  variant: 'ac' | 'tank' | 'antenna';
  rotation: number;
  yOffset: number;
}

interface FurnitureSpec {
  type: 'kiosk' | 'bicycle' | 'trash' | 'phone' | 'mailbox' | 'parking';
  x: number;
  z: number;
  rotation: number;
  variant?: number;
}

/** 16 · 阶段 T：屋顶太阳能板布点规格。 */
interface SolarSpec {
  x: number;
  z: number;
  y: number;
  rotation: number;
}

interface SignSpec {
  x: number;
  z: number;
  rotation: number;
  variant: 'traffic' | 'info';
}

interface DistrictProps {
  districtId: string;
  trees: TreeSpec[];
  rooftop: RooftopSpec[];
  /** 16 · 阶段 T：屋顶太阳能板（suburb / oldtown）。 */
  solar: SolarSpec[];
  /** 城区内行人（V3 体积步态；outfit 0..3 对应 PEDESTRIAN_OUTFITS） */
  pedestrians: Array<{
    path: Array<[number, number]>;
    outfit: NonNullable<PedestrianV3Props['outfit']>;
    speed: number;
    phase: number;
    /** 中央公园看景行人：V3 契约无 stationary prop → speed=0 + phase=0 站定 path 起点。 */
    stationary: boolean;
  }>;
  furniture: FurnitureSpec[];
  sign: SignSpec;
}

interface RoadVehicle {
  from: [number, number];
  to: [number, number];
  variant: 'sedan' | 'truck' | 'bus' | 'taxi';
  speed: number;
  phase: number;
  /** 16 · 阶段 S：车道偏移（>0 右行 / <0 对向）。 */
  laneOffset: number;
}

interface RoadTree {
  x: number;
  z: number;
  variant: 'oak' | 'pine' | 'palm';
  scale: number;
  rotation: number;
}

interface Layout {
  districtProps: DistrictProps[];
  roadVehicles: RoadVehicle[];
  roadTrees: RoadTree[];
  trafficLights: Array<{ x: number; z: number; rotation: number }>;
  busStops: Array<{ x: number; z: number; rotation: number }>;
}

const TREE_VARIANTS: Array<'oak' | 'pine' | 'palm'> = ['oak', 'pine', 'palm'];
const ROOFTOP_VARIANTS: Array<'ac' | 'tank' | 'antenna'> = ['ac', 'tank', 'antenna'];
const VEHICLE_VARIANTS: Array<'sedan' | 'truck' | 'bus' | 'taxi'> = ['sedan', 'truck', 'bus', 'taxi'];

/**
 * 全城行人总量上限。批次 20（文档 1 §2.3）：56 → **110**（实例化只覆盖树/灯，
 * 行人仍逐体 V3 mixer，上限受动画帧耗时约束）。前 16 区合计 56 + 新 16 区 47
 * （§2.3 PEDESTRIAN_DENSITY 列合计，契约文中「新 51」与其表列差 4，按表列为准）
 * = 103 < 110，截断守卫不触发但保留（超出时按表序裁尾部新区）。
 */
export const PEDESTRIAN_TOTAL_CAP = 110;
/** 全城树（行道 + 区内）实例总量上限（超出按 hash 种子稳定截断，§3.3）。 */
export const TREE_TOTAL_CAP = 550;

/**
 * 城区行人密度档位（按城区属性分档：核心 5-6 / 一般 3-4 / 郊区 2）。
 *
 * 18 · 阶段 Z：全城行人硬上限 56（01 §3.3）。密度表原值合计 59 > 56，
 * 按「核心区优先、郊区先减」裁 3 人：industry / industrial_park / logistics_port
 * 各 2→1（-3 → 56）；核心区（finance/commerce/tech/hightech_park）与中央公园保满编。
 * 合计核对：核心 21 + 一般 16 + 医疗居住滨河 9 + 郊区工业 5 + 公园 5 = 56。
 *
 * 批次 20（文档 1 §2.3）：追加 16 新区档位（列值逐字），总上限重定 110。
 */
const PEDESTRIAN_DENSITY: Record<string, number> = {
  // 核心商务区（高密度）
  finance: 6, commerce: 5, tech: 5, hightech_park: 5,
  // 一般城区
  oldtown: 4, edu_district: 4, transport_hub: 4, cultural_creative: 4,
  medical_city: 3, residential: 3, riverside: 3,
  // 工业/低密度（18-Z 裁剪后 5 人）
  suburb: 2, industry: 1, industrial_park: 1, logistics_port: 1,
  // 公园
  central_park: 5, // 含 2 静止
  // ── 批次 20 新增 16 区（= 文档 1 §2.3 PEDESTRIAN_DENSITY 列）──
  fin_sub_center: 4, software_park: 4, airport_town: 4, air_logistics: 1,
  auto_city: 2, mountain_resort: 2, chem_park: 1, agri_park: 2,
  health_town: 3, steel_town: 1, old_city_culture: 4, university_town: 5,
  wetland_park: 3, sports_new_city: 4, bay_new_town: 3, highspeed_rail_town: 4,
};

/** 城区 outfit 倾向（0 商务蓝 / 1 休闲灰 / 2 亮色红 / 3 卡其，对应 PEDESTRIAN_OUTFITS）。 */
const OUTFIT_AFFINITY: Record<string, Array<NonNullable<PedestrianV3Props['outfit']>>> = {
  finance: [0, 1],
  commerce: [0, 2],
  tech: [1, 2],
  hightech_park: [1, 0],
  oldtown: [3, 1],
  edu_district: [1, 2],
  transport_hub: [2, 1],
  cultural_creative: [2, 1],
  medical_city: [0, 1],
  residential: [1, 3],
  riverside: [0, 1],
  suburb: [3],
  industry: [3, 2],
  industrial_park: [3, 2],
  logistics_port: [3, 2],
  central_park: [1, 2],
  // ── 批次 20 新增 16 区（= 文档 1 §2.3 OUTFIT_AFFINITY 列）──
  fin_sub_center: [0, 1], software_park: [1, 0], airport_town: [2, 1], air_logistics: [3, 2],
  auto_city: [3, 2], mountain_resort: [3], chem_park: [3, 2], agri_park: [3, 1],
  health_town: [1, 3], steel_town: [3, 2], old_city_culture: [3, 1], university_town: [1, 2],
  wetland_park: [1, 2], sports_new_city: [2, 1], bay_new_town: [0, 1], highspeed_rail_town: [2, 1],
};

/** 报刊亭布点列表（按城区属性；批次 20 §2.3 白名单就近补 fin_sub/bay/sports/高铁新城/大学城）。 */
const KIOSK_DISTRICTS = new Set([
  'finance', 'commerce', 'oldtown', 'transport_hub', 'cultural_creative',
  'fin_sub_center', 'bay_new_town', 'sports_new_city', 'highspeed_rail_town', 'university_town',
]);
/** 自行车布点列表。 */
const BICYCLE_DISTRICTS = new Set(['residential', 'edu_district', 'commerce', 'transport_hub', 'riverside']);
/** 电话亭布点列表。 */
const PHONE_DISTRICTS = new Set(['oldtown', 'commerce', 'finance']);
/** 停车牌布点列表。 */
const PARKING_DISTRICTS = new Set(['finance', 'commerce', 'tech', 'transport_hub']);

/**
 * 为单个城区生成 path（折线 2-3 段；总长 6-10 单位；起点远离城区中心 >2）。
 */
function pathForDistrict(def: WealthDistrictDef, rnd: () => number): Array<[number, number]> {
  const c = { x: def.x, z: def.z };
  const baseAngle = rnd() * Math.PI * 2;
  // path 半径范围 1.5-3.5（城区底板 8×8，半径 3.5 仍不出界）
  const r1 = 1.5 + rnd() * 1.0;
  const r2 = 2.0 + rnd() * 1.5;
  const a1 = baseAngle;
  const a2 = baseAngle + Math.PI / 2 + (rnd() - 0.5) * 0.6;
  const a3 = baseAngle + Math.PI + (rnd() - 0.5) * 0.6;
  const path: Array<[number, number]> = [
    [c.x + Math.cos(a1) * r1, c.z + Math.sin(a1) * r1],
    [c.x + Math.cos(a2) * r2, c.z + Math.sin(a2) * r2],
    [c.x + Math.cos(a3) * r1, c.z + Math.sin(a3) * r1],
  ];
  return path;
}

/** 为单个城区生成 props。 */
function propsForDistrict(def: WealthDistrictDef, idx: number): DistrictProps {
  const rnd = mulberry32(hashStr(def.id));
  const c = { x: def.x, z: def.z };
  // 城区朝向 finance（原点）的方向角（标识牌/树避让共用）
  const angleToFinance = Math.atan2(-c.z, -c.x);

  // 阶段 L 树群（V2 立体树）：
  //   central_park → 10 棵（半径 1.5~3.5 散布，scale 0.8~1.2）
  //   其余城区     → 4 棵沿城区边缘（半径 3.3±0.3），避开路口 sign 角度 ±0.4 rad
  const isPark = def.id === 'central_park';
  const treeCount = isPark ? 10 : 4;
  const angleDiff = (a: number, b: number) => {
    let d = a - b;
    while (d > Math.PI) d -= Math.PI * 2;
    while (d < -Math.PI) d += Math.PI * 2;
    return Math.abs(d);
  };
  const trees: TreeSpec[] = Array.from({ length: treeCount }, (_, i) => {
    let angle = (i / treeCount) * Math.PI * 2 + (rnd() - 0.5) * 0.6;
    if (!isPark && angleDiff(angle, angleToFinance) < 0.4) angle += 0.5;
    const radius = isPark ? 1.5 + rnd() * 2.0 : 3.3 + rnd() * 0.6;
    return {
      x: c.x + Math.cos(angle) * radius,
      z: c.z + Math.sin(angle) * radius,
      variant: TREE_VARIANTS[Math.floor(rnd() * TREE_VARIANTS.length)],
      scale: isPark ? 0.8 + rnd() * 0.4 : 0.5 + rnd() * 0.5,
    };
  });

  // 1-2 个楼顶杂物
  const [minF, maxF] = DISTRICT_FLOORS[def.id];
  const roofY = buildingHeight((minF + maxF) / 2) * 0.9;
  const rooftop: RooftopSpec[] = [0, 1].slice(0, 1 + (rnd() < 0.5 ? 1 : 0)).map(() => {
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

  // 16 · 阶段 T：太阳能板（suburb / oldtown 屋顶确定性 1-2 块）
  const solar: SolarSpec[] = [];
  if (def.id === 'suburb' || def.id === 'oldtown') {
    const panelCount = 1 + (rnd() < 0.5 ? 1 : 0);
    for (let i = 0; i < panelCount; i++) {
      const angle = rnd() * Math.PI * 2;
      const radius = 1.2 + i * 1.1;
      solar.push({
        x: c.x + Math.cos(angle) * radius,
        z: c.z + Math.sin(angle) * radius,
        y: roofY * 0.72, // house 主体高 = 总高 ×0.7（坡顶下方贴合）
        rotation: rnd() * Math.PI * 2,
      });
    }
  }

  // 阶段 K 行人（V3 折线 path）：按城区密度档位 + outfit 倾向（确定性 rnd，禁 Math.random）
  const pedCount = PEDESTRIAN_DENSITY[def.id] ?? 3;
  const outfitPool = OUTFIT_AFFINITY[def.id] ?? [1, 3];
  const pedestrians: DistrictProps['pedestrians'] = Array.from({ length: pedCount }, (_, i) => {
    const path = pathForDistrict(def, rnd);
    // 中央公园最后 2 个行人设为静止（看景）
    const isStationary = isPark && i >= pedCount - 2;
    return {
      path,
      outfit: outfitPool[i % outfitPool.length],
      speed: 0.3 + rnd() * 0.2,
      phase: (i + rnd()) / pedCount,
      stationary: isStationary,
    };
  });

  // 阶段 M 街道家具（按城区属性选择性布点）
  const furniture: FurnitureSpec[] = [];
  // 垃圾箱：每城区 1
  const trashAngle = rnd() * Math.PI * 2;
  furniture.push({
    type: 'trash',
    x: c.x + Math.cos(trashAngle) * 3.5,
    z: c.z + Math.sin(trashAngle) * 3.5,
    rotation: rnd() * Math.PI * 2,
    variant: idx % 3, // 0/1/2 → 蓝/灰/红
  });
  // 邮筒：每城区 1
  const mailAngle = rnd() * Math.PI * 2;
  furniture.push({
    type: 'mailbox',
    x: c.x + Math.cos(mailAngle) * 3.2,
    z: c.z + Math.sin(mailAngle) * 3.2,
    rotation: rnd() * Math.PI * 2,
  });
  // 报刊亭：选择性
  if (KIOSK_DISTRICTS.has(def.id)) {
    const kAngle = rnd() * Math.PI * 2;
    furniture.push({
      type: 'kiosk',
      x: c.x + Math.cos(kAngle) * 3.0,
      z: c.z + Math.sin(kAngle) * 3.0,
      rotation: rnd() * Math.PI * 2,
    });
  }
  // 自行车：选择性（1-2 辆）
  if (BICYCLE_DISTRICTS.has(def.id)) {
    const bikeCount = 1 + Math.floor(rnd() * 2);
    for (let i = 0; i < bikeCount; i++) {
      const bAngle = rnd() * Math.PI * 2;
      const bR = 2.8 + i * 0.6;
      furniture.push({
        type: 'bicycle',
        x: c.x + Math.cos(bAngle) * bR,
        z: c.z + Math.sin(bAngle) * bR,
        rotation: rnd() * Math.PI * 2,
      });
    }
  }
  // 电话亭：选择性
  if (PHONE_DISTRICTS.has(def.id)) {
    const pAngle = rnd() * Math.PI * 2;
    furniture.push({
      type: 'phone',
      x: c.x + Math.cos(pAngle) * 3.0,
      z: c.z + Math.sin(pAngle) * 3.0,
      rotation: rnd() * Math.PI * 2,
      variant: rnd() < 0.5 ? 0 : 1, // red / green
    });
  }
  // 停车牌：选择性
  if (PARKING_DISTRICTS.has(def.id)) {
    const pkAngle = rnd() * Math.PI * 2;
    furniture.push({
      type: 'parking',
      x: c.x + Math.cos(pkAngle) * 3.5,
      z: c.z + Math.sin(pkAngle) * 3.5,
      rotation: rnd() * Math.PI * 2,
    });
  }

  // 路口标识牌
  const sign: SignSpec = {
    x: c.x + Math.cos(angleToFinance) * 3.5,
    z: c.z + Math.sin(angleToFinance) * 3.5,
    rotation: angleToFinance + Math.PI / 2,
    variant: idx % 2 === 0 ? 'traffic' : 'info',
  };

  return {
    districtId: def.id,
    trees,
    rooftop,
    solar,
    pedestrians,
    furniture,
    sign,
  };
}

/**
 * 16 · 阶段 S：双向车流编排。
 * 每条主干道正向 1 辆（district→origin，右行 +0.32/+0.36）；
 * len > 15 的再追加反向 1 辆（origin→district，laneOffset 取负，variant 错开）。
 */
function vehiclesForRoads(districts: WealthDistrictDef[]): RoadVehicle[] {
  const roads: RoadVehicle[] = [];
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d, i) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < MAIN_ROAD_MIN_LEN) return;
      const variant = VEHICLE_VARIANTS[i % VEHICLE_VARIANTS.length];
      const speedMap = { sedan: 0.07, truck: 0.04, bus: 0.05, taxi: 0.08 };
      // 右行偏移：bus/truck 更宽，偏移略大
      const fwdOffset = variant === 'bus' || variant === 'truck' ? 0.36 : 0.32;
      roads.push({
        from: [c.x, c.z],
        to: [0, 0],
        variant,
        speed: speedMap[variant],
        phase: (i * 0.37) % 1,
        laneOffset: fwdOffset,
      });
      // 对向车流（较长主干道才有，控制总量 ~20 辆）。
      // 注意：laneOffset 取「行进方向右侧」语义，方向反转后世界侧自动翻转，
      // 因此对向车传同样的正值（取负会落到同侧 → 对撞）。
      if (len > 15) {
        const backVariant = VEHICLE_VARIANTS[(i + 2) % VEHICLE_VARIANTS.length];
        const backOffset = backVariant === 'bus' || backVariant === 'truck' ? 0.36 : 0.32;
        roads.push({
          from: [0, 0],
          to: [c.x, c.z],
          variant: backVariant,
          speed: speedMap[backVariant],
          phase: ((i * 0.37) + 0.5) % 1,
          laneOffset: backOffset,
        });
      }
    });
  return roads;
}

/**
 * 阶段 L 行道树：沿主干道等距布点（与路灯错相位 π/2 避免冲突）。
 * 每 2.5 单位 1 棵；道路两侧交替（t 错开 0.5 间距）。
 */
function roadTreesForRoads(districts: WealthDistrictDef[]): RoadTree[] {
  const trees: RoadTree[] = [];
  let a = hashStr('road-trees-v2') >>> 0;
  const rnd = () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };

  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < MAIN_ROAD_MIN_LEN) return; // 仅主干道
      const count = Math.max(2, Math.floor(len / 2.5));
      const nx = -dz / len;
      const nz = dx / len;
      const sideOffset = 1.4 / 2 + 0.35 + 0.3; // 道路外 + 路灯偏移 + 树位
      for (let i = 1; i <= count; i++) {
        const t = i / (count + 1) + 0.25; // 错相位 π/2（路灯在 0.25 t 起步）
        if (t >= 1) continue;
        const side = i % 2 === 0 ? 1 : -1; // 两侧交替
        const x = c.x + dx * t + nx * sideOffset * side;
        const z = c.z + dz * t + nz * sideOffset * side;
        trees.push({
          x,
          z,
          variant: TREE_VARIANTS[Math.floor(rnd() * TREE_VARIANTS.length)],
          scale: 0.6 + rnd() * 0.3,
          rotation: rnd() * Math.PI * 2,
        });
      }
    });
  return trees;
}

/** 主干道路侧红绿灯。 */
function trafficLightsForRoads(districts: WealthDistrictDef[]): Layout['trafficLights'] {
  const out: Layout['trafficLights'] = [];
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < MAIN_ROAD_MIN_LEN) return;
      const t = 0.12;
      const nx = -dz / len;
      const nz = dx / len;
      const sideOffset = 1.4 / 2 + 0.35;
      out.push({
        x: c.x + dx * t + nx * sideOffset,
        z: c.z + dz * t + nz * sideOffset,
        rotation: Math.atan2(dx, dz) + Math.PI,
      });
    });
  return out;
}

/** 主干道路侧公交站台。 */
function busStopsForRoads(districts: WealthDistrictDef[]): Layout['busStops'] {
  const out: Layout['busStops'] = [];
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < MAIN_ROAD_MIN_LEN) return;
      const t = 0.35;
      const nx = -dz / len;
      const nz = dx / len;
      const sideOffset = 1.4 / 2 + 0.35;
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

/**
 * 全城行人总上限守卫（批次 20：PEDESTRIAN_TOTAL_CAP=110）。
 * 超出时按卡池表序裁尾部（新区先减，前 16 区满编）；确定性，无随机。
 */
function capPedestrians(districtProps: DistrictProps[]): void {
  let total = 0;
  for (const dp of districtProps) total += dp.pedestrians.length;
  if (total <= PEDESTRIAN_TOTAL_CAP) return;
  for (let i = districtProps.length - 1; i >= 0 && total > PEDESTRIAN_TOTAL_CAP; i--) {
    const dp = districtProps[i];
    const excess = Math.min(dp.pedestrians.length, total - PEDESTRIAN_TOTAL_CAP);
    if (excess > 0) {
      dp.pedestrians.splice(dp.pedestrians.length - excess, excess);
      total -= excess;
    }
  }
}

export function StreetPropsLayer({ districts }: StreetPropsLayerProps) {
  const layout = useMemo<Layout>(() => {
    const districtProps = districts.map((d, idx) => propsForDistrict(d, idx));
    capPedestrians(districtProps);
    const roadVehicles = vehiclesForRoads(districts);
    const roadTrees = roadTreesForRoads(districts);
    const trafficLights = trafficLightsForRoads(districts);
    const busStops = busStopsForRoads(districts);
    return { districtProps, roadVehicles, roadTrees, trafficLights, busStops };
  }, [districts]);

  // 批次 20 §3.3：区内树 + 行道树合并单一 InstancedMesh 集合（3 draw call）。
  const allTrees = useMemo(
    () => [
      ...layout.districtProps.flatMap((dp) => dp.trees.map((t) => ({ x: t.x, z: t.z, scale: t.scale }))),
      ...layout.roadTrees.map((t) => ({ x: t.x, z: t.z, scale: t.scale })),
    ],
    [layout],
  );

  return (
    <>
      {/* 楼顶杂物 + 行人 + 家具 + 标识（树已抽出为全局 TreesInstanced） */}
      {layout.districtProps.map((dp) => (
        <group key={dp.districtId}>
          {/* 楼顶杂物 */}
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
          {/* 16 · 阶段 T：屋顶太阳能板（suburb / oldtown） */}
          {dp.solar.map((sp, i) => (
            <SolarPanel key={`solar-${i}`} x={sp.x} y={sp.y} z={sp.z} rotation={sp.rotation} />
          ))}
          {/* 阶段 K→18-Z V3 体积漫步行人（站定行人 speed=0 + phase=0，见 DistrictProps） */}
          {dp.pedestrians.map((p, i) => (
            <PedestrianV3
              key={`ped-${i}`}
              path={p.path}
              outfit={p.outfit}
              speed={p.stationary ? 0 : p.speed}
              phase={p.stationary ? 0 : p.phase}
            />
          ))}
          {/* 阶段 M 街道家具（按 type 分派渲染器） */}
          {dp.furniture.map((f, i) => {
            const key = `fur-${f.type}-${i}`;
            switch (f.type) {
              case 'kiosk':
                return <VendorKiosk key={key} x={f.x} z={f.z} rotation={f.rotation} />;
              case 'bicycle':
                return <BicycleRack key={key} x={f.x} z={f.z} rotation={f.rotation} />;
              case 'trash':
                return <TrashCan key={key} x={f.x} z={f.z} rotation={f.rotation} variant={(f.variant ?? 0) as 0 | 1 | 2} />;
              case 'phone':
                return <PhoneBooth key={key} x={f.x} z={f.z} rotation={f.rotation} variant={(f.variant ?? 0) === 0 ? 'red' : 'green'} />;
              case 'mailbox':
                return <Mailbox key={key} x={f.x} z={f.z} rotation={f.rotation} />;
              case 'parking':
                return <ParkingMeter key={key} x={f.x} z={f.z} rotation={f.rotation} />;
              default:
                return null;
            }
          })}
          {/* 路口标识牌 */}
          <Sign
            x={dp.sign.x}
            z={dp.sign.z}
            rotation={dp.sign.rotation}
            variant={dp.sign.variant}
          />
        </group>
      ))}

      {/* 批次 20 §3.3：区内树 + 行道树 → 单一全局 InstancedMesh 集合（3 draw call） */}
      <TreesInstanced trees={allTrees} totalCap={TREE_TOTAL_CAP} />

      {/* 主干道车辆（16 · 阶段 S：双向车道，右行偏移） */}
      {layout.roadVehicles.map((v, i) => (
        <Vehicle
          key={`vehicle-${i}`}
          from={v.from}
          to={v.to}
          variant={v.variant}
          speed={v.speed}
          phase={v.phase}
          laneOffset={v.laneOffset}
        />
      ))}

      {/* 主干道红绿灯 */}
      {layout.trafficLights.map((tl, i) => (
        <TrafficLight key={`tl-${i}`} x={tl.x} z={tl.z} rotation={tl.rotation} />
      ))}

      {/* 主干道公交站台 */}
      {layout.busStops.map((bs, i) => (
        <BusStop key={`busstop-${i}`} x={bs.x} z={bs.z} rotation={bs.rotation} />
      ))}
    </>
  );
}

// 注：V1 Pedestrian / PedestrianV2 / Tree / TreeV2 / TreeV3 / StreetLight 组件文件仍保留在
// props/ 目录（契约要求保留，未来如需回退可 import）。
// 行人：V3（PedestrianV3）逐体动画 mixer，批次 20 不实例化（总上限 110）。
// 树：批次 20 §3.3 起改走 TreesInstanced（形态与 TreeV3Fallback 同源 treeSeed/treeShape）。
// 路灯：批次 20 §3.3 起由 WealthCityMap 汇总点位走 StreetLightsInstanced。
// V1 Pedestrian 仍由原 props/Pedestrian.tsx 导出，本层不再引用。