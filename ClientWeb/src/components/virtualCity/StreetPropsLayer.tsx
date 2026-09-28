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
 *   - 远景剪影原由 AtmosphereLayer 注入；批次 26 起四缘改由 edge/CityEdgeLayer
 *     真实环境带（雪山/沙漠/森林/海洋）承担，本层始终不处理远景
 *
 * 批次 24「真实马路与交通设施」（文档 24 §6）：
 *   - 红绿灯渲染职责移出本层：trafficSignalsForCity()（双端 + 环岛对角布点、
 *     A/B 相位组）交 VirtualCityCityMap → <TrafficSignals> 全局实例化 + 相位动画；
 *   - 新增 roadsideBinsForCity()（主干道 ≈6u 两侧交替 + 公交站台旁）；
 *   - busStopsForRoads 更名导出 busStopsForCity（站台旁垃圾桶布点复用）。
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
 *     可查 renderer.info 实时值（VirtualCityCityMap onCreated 挂载，实测记入批次 20 实施记录）
 *
 * 契约：lag_docs/虚拟城市/已实现/15-3D城市渲染深化/02-架构设计 §1
 * + 批次 20 文档 1 §2.3 / §3.2 / §3.3。
 */

import { useMemo } from 'react';
import { useThree } from '@react-three/fiber';
import {
  districtCenter,
  type VirtualCityDistrictDef,
} from '@/types/virtualCity';
import { detectQualityTier } from '@/engine3d';
import { DISTRICT_FLOORS, buildingHeight, spacing, u } from './cityScale';
import { MAIN_ROAD_MIN_LEN } from './VirtualCityCityMap';
import { ROAD_WIDTH_MAIN } from './Road';
import { buildingsFor } from './building_layout';
import { TreesInstanced } from './props/TreesInstanced'; // 批次 20 §3.3：TreeV3 逐实例 → 全局 InstancedMesh（形态同源）
import { Vehicle } from './props/Vehicle';
import { PedestrianV3, type PedestrianV3Props } from './props/PedestrianV3';
import { Sign } from './props/Sign';
import { RooftopAcc } from './props/RooftopAcc';
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
  busStops: Array<{ x: number; z: number; rotation: number }>;
}

const TREE_VARIANTS: Array<'oak' | 'pine' | 'palm'> = ['oak', 'pine', 'palm'];
const ROOFTOP_VARIANTS: Array<'ac' | 'tank' | 'antenna'> = ['ac', 'tank', 'antenna'];
const VEHICLE_VARIANTS: Array<'sedan' | 'truck' | 'bus' | 'taxi'> = ['sedan', 'truck', 'bus', 'taxi'];

/**
 * 全城行人总量上限（high 质量档）。批次 20（文档 1 §2.3）：56 → **110**（实例化只覆盖树/灯，
 * 行人仍逐体 V3 mixer，上限受动画帧耗时约束）。前 16 区合计 56 + 新 16 区 47
 * （§2.3 PEDESTRIAN_DENSITY 列合计，契约文中「新 51」与其表列差 4，按表列为准）
 * = 103 < 110，截断守卫不触发但保留（超出时按表序裁尾部新区）。
 * 批次 28 A4/A5：按质量档取值 —— low 60（软件渲染器/低配，行人 mixer 是 CPU 大户；
 * 游戏侧策略映射，engine3d 不持有行人字段）。
 */
export const PEDESTRIAN_TOTAL_CAP = 110;
/** low 质量档行人上限（批次 28 A4：超出按表序裁尾部新区）。 */
export const PEDESTRIAN_TOTAL_CAP_LOW = 60;
/** 全城树（行道 + 区内）实例总量上限（超出按 hash 种子稳定截断，§3.3）。
 *  批次 30 A5：行道树株距 25 m → 真实 9 m（REAL_SPACING_M.streetTree），
 *  总量 550 → 1800（实例化 3+2 draw call 不变，仅实例数增加）。 */
export const TREE_TOTAL_CAP = 1800;

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
function pathForDistrict(def: VirtualCityDistrictDef, rnd: () => number): Array<[number, number]> {
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
function propsForDistrict(def: VirtualCityDistrictDef, idx: number): DistrictProps {
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
      // 批次 30 P0-3：scale 0.5~1.2 × 旧 3.4 m 基准树 = 1.7~4.4 m「棒棒糖」——
      // treeShape 基准已归一到 REAL_DIMS_M.streetTree（9 m），scale 收敛到 0.85~1.15。
      scale: isPark ? 0.9 + rnd() * 0.25 : 0.85 + rnd() * 0.2,
    };
  });

  // 1-2 个楼顶杂物（批次 30 A2：锚点改挂街墙楼栋槽位 —— 旧「区中心半径 0.8~2.4」
  // 在街墙布局下会悬在庭院半空；y 仍取楼层区间中值 ×0.9 的近似屋顶高）。
  const [minF, maxF] = DISTRICT_FLOORS[def.id];
  const roofY = buildingHeight((minF + maxF) / 2) * 0.9;
  const bSpecs = buildingsFor(def);
  const rooftop: RooftopSpec[] = [0, 1].slice(0, 1 + (rnd() < 0.5 ? 1 : 0)).map((k) => {
    const anchor = bSpecs[(k + Math.floor(rnd() * bSpecs.length)) % bSpecs.length];
    return {
      x: c.x + anchor.x + (rnd() - 0.5) * 0.6,
      z: c.z + anchor.z + (rnd() - 0.5) * 0.6,
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
      // 批次 30 A2：同屋顶杂物，锚到街墙楼栋（防庭院悬空）
      const anchor = bSpecs[(i + Math.floor(rnd() * bSpecs.length)) % bSpecs.length];
      solar.push({
        x: c.x + anchor.x + (rnd() - 0.5) * 0.5,
        z: c.z + anchor.z + (rnd() - 0.5) * 0.5,
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
      // 批次 30 P1-12：speed 是世界单位/秒（= m/s × u()）——原 0.3~0.5 ⇒ 3~5.5 m/s
      // 「滑步」，改真实步速 1.2~1.5 m/s。
      speed: u(1.2) + rnd() * u(0.3),
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
function vehiclesForRoads(districts: VirtualCityDistrictDef[]): RoadVehicle[] {
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
      // 批次 30 P1-12：speed 原是归一化 t/秒（外圈车道实测 123~140 km/h）——
      // 改为「真实米/秒」经 u() 换算再除以路径长：市区 32~43 km/h。
      const speedMapMs = { sedan: 11, truck: 9, bus: 9, taxi: 12 };
      const speed = u(speedMapMs[variant]) / Math.max(1, len);
      // 右行偏移：bus/truck 更宽，偏移略大
      const fwdOffset = variant === 'bus' || variant === 'truck' ? 0.36 : 0.32;
      roads.push({
        from: [c.x, c.z],
        to: [0, 0],
        variant,
        speed,
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
          speed: u(speedMapMs[backVariant]) / Math.max(1, len),
          phase: ((i * 0.37) + 0.5) % 1,
          laneOffset: backOffset,
        });
      }
    });
  return roads;
}

/**
 * 阶段 L 行道树：沿主干道等距布点（与路灯错相位 π/2 避免冲突）。
 * 批次 30 A5：株距 25 m（2.5u）→ 真实 9 m（REAL_SPACING_M.streetTree），
 * 道路两侧交替；scale 收敛 0.85~1.05（树形基准已归一 streetTree 9 m）。
 */
function roadTreesForRoads(districts: VirtualCityDistrictDef[]): RoadTree[] {
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
      const treeSpacing = spacing('streetTree');
      const count = Math.max(2, Math.floor(len / treeSpacing));
      const nx = -dz / len;
      const nz = dx / len;
      const sideOffset = ROAD_WIDTH_MAIN / 2 + 0.35 + 0.3; // 道路外 + 路灯偏移 + 树位
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
          scale: 0.85 + rnd() * 0.2,
          rotation: rnd() * Math.PI * 2,
        });
      }
    });
  return trees;
}

// ── 批次 24：红绿灯 / 路侧垃圾桶布点（文档 24 §6.2；取代 trafficLightsForRoads）──

/** 红绿灯点位规格（TrafficSignals 组件渲染契约）。 */
export interface TrafficSignalSpot {
  x: number;
  z: number;
  /**
   * 绕 Y 旋转（弧度）。语义 = TrafficSignals 组件 local +z（灯面法向）指向的
   * 世界方向：布点层保证灯面朝向来车（行进方向的反方向）。
   */
  rotation: number;
  /** 相位组：A = 周期 0s 起；B = +8s 偏移（16s 周期：绿 6 / 黄 2 / 红 8）。 */
  phase: 'A' | 'B';
}

/** 环岛半径（与 RingRoad RING_RADIUS 同标尺；IntersectionSignals 退役后此处为布点权威）。 */
const SIGNAL_RING_RADIUS = 5.6;
/** 灯杆离受控车道中心线的侧向距离（主干道半宽 + 路缘余量，立于右侧路缘；
 *  批次 30：半宽魔数改引 Road.ROAD_WIDTH_MAIN，§130 消重复）。 */
const SIGNAL_SIDE_OFFSET = ROAD_WIDTH_MAIN / 2 + 0.25;

/**
 * 全城红绿灯布点（批次 24 §6.2）：
 *   - 主干道（len ≥ MAIN_ROAD_MIN_LEN）双端 t=0.90 / t=0.10 各 1 座，立于受控
 *     车道行进方向右侧路缘（右行侧 = 行进向量 (dx,dz) 的 (dz,-dx)/len 侧，
 *     与 Vehicle laneOffset 同一右行语义），灯面朝来车；
 *   - 环岛路口：主干道放射线与 RING_RADIUS 圆交点对角 2 座（面向放射来车 = A 组、
 *     面向环岛来车 = B 组；环道按逆时针切向）；替代原 IntersectionSignals 的
 *     len<=12 硬编码（顺带修正其交点被镜像到环对侧的坐标 bug）；
 *   - 相位组：道路索引奇偶定 A / B；环岛 2 座固定 A/B 对置。
 */
export function trafficSignalsForCity(districts: VirtualCityDistrictDef[]): TrafficSignalSpot[] {
  const out: TrafficSignalSpot[] = [];
  let mainIdx = 0;
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < MAIN_ROAD_MIN_LEN) return;
      const phase: TrafficSignalSpot['phase'] = mainIdx % 2 === 0 ? 'A' : 'B';
      mainIdx++;
      // 行进方向单位向量（district → origin，正向车）；右行侧 = (uz, -ux)
      const ux = dx / len;
      const uz = dz / len;

      // ① 原点端（t=0.90）：控制正向车（district→origin），杆在正向车右行侧，灯面朝 -u
      out.push({
        x: c.x + dx * 0.9 + uz * SIGNAL_SIDE_OFFSET,
        z: c.z + dz * 0.9 - ux * SIGNAL_SIDE_OFFSET,
        rotation: Math.atan2(-ux, -uz),
        phase,
      });
      // ② 城区端（t=0.10）：控制反向车（origin→district），杆在反向车右行侧 = (−uz, ux)，灯面朝 +u
      out.push({
        x: c.x + dx * 0.1 - uz * SIGNAL_SIDE_OFFSET,
        z: c.z + dz * 0.1 + ux * SIGNAL_SIDE_OFFSET,
        rotation: Math.atan2(ux, uz),
        phase,
      });

      // ③ 环岛路口对角 2 座：交点 = 原点向城区方向 RING_RADIUS 处（径向外向 o = (ox,oz)）
      const ox = -ux; // = c.x / len
      const oz = -uz; // = c.z / len
      const jx = ox * SIGNAL_RING_RADIUS;
      const jz = oz * SIGNAL_RING_RADIUS;
      // 环道逆时针切向 t = (−oz, ox)；放射正向车右行侧 = (uz, −ux) = −t
      const tx = -oz;
      const tz = ox;
      // 面向放射来车（A 组）：位于放射正向车右行侧（junction − t·off），灯面朝径向外 o
      out.push({
        x: jx - tx * SIGNAL_SIDE_OFFSET,
        z: jz - tz * SIGNAL_SIDE_OFFSET,
        rotation: Math.atan2(ox, oz),
        phase: 'A',
      });
      // 面向环岛来车（B 组）：位于环道车右行侧（junction + o·off），灯面朝 −t
      out.push({
        x: jx + ox * SIGNAL_SIDE_OFFSET,
        z: jz + oz * SIGNAL_SIDE_OFFSET,
        rotation: Math.atan2(-tx, -tz),
        phase: 'B',
      });
    });
  return out;
}

/** 路侧垃圾桶点位规格（RoadsideBins 组件渲染契约）。 */
export interface RoadsideBinSpot {
  x: number;
  z: number;
  rotation: number;
  /** 分类双色：green = 厨余绿 / blue = 可回收蓝（TrashCan_Green / TrashCan_Blue）。 */
  variant: 'green' | 'blue';
}

/** 路侧垃圾桶间距（沿主干道 ≈6u 一对，两侧交替 → 单侧 ≈12u；
 *  批次 30 A1：值入 REAL_SPACING_M.trashCan = 60 m）。 */
const BIN_SPACING = spacing('trashCan');
/** 垃圾桶横向偏移（主干道半宽 + 0.16，人行道外缘附近；§130 消重复）。 */
const BIN_SIDE_OFFSET = ROAD_WIDTH_MAIN / 2 + 0.16;

/**
 * 全城路侧垃圾桶布点（批次 24 §5/§6）：
 *   - 主干道每 ≈6u 沿线、道路两侧交替（sideOffset = roadWidth/2 + 0.16），
 *     t 起点与路灯（2.5u 网格、起点 2.5）错开半格（起点 3.75）；
 *   - 每个公交站台旁（沿路向 +0.45）追加 1 个；
 *   - variant 按布点顺序 green / blue 交替（确定性，无随机）。
 */
export function roadsideBinsForCity(districts: VirtualCityDistrictDef[]): RoadsideBinSpot[] {
  const out: RoadsideBinSpot[] = [];
  let seq = 0;
  const nextVariant = (): RoadsideBinSpot['variant'] => (seq++ % 2 === 0 ? 'green' : 'blue');
  districts
    .filter((d) => d.id !== 'finance')
    .forEach((d) => {
      const c = districtCenter(d.id);
      const dx = -c.x;
      const dz = -c.z;
      const len = Math.sqrt(dx * dx + dz * dz);
      if (len < MAIN_ROAD_MIN_LEN) return;
      const ux = dx / len;
      const uz = dz / len;
      const rx = uz; // 右行侧单位向量
      const rz = -ux;
      // 沿线：s = 3.75 + i·6（与路灯 2.5 网格错开 1.25 = 半格）；两端各留 ≥3u 给路口标线
      const count = Math.max(0, Math.floor((len - 3 - 3.75) / BIN_SPACING) + 1);
      for (let i = 0; i < count; i++) {
        const s = 3.75 + i * BIN_SPACING;
        if (s > len - 3) break;
        const side = i % 2 === 0 ? 1 : -1; // 两侧交替
        out.push({
          x: c.x + ux * s + rx * BIN_SIDE_OFFSET * side,
          z: c.z + uz * s + rz * BIN_SIDE_OFFSET * side,
          rotation: Math.atan2(rx * side, rz * side),
          variant: nextVariant(),
        });
      }
    });
  // 每个公交站台旁追加 1 个（站台 rotation = atan2(dx,dz)，可还原路向 u=(sin,cos)；
  // 桶沿路向 +0.45 错开雨棚，站台本身只布在主干道上，无重复计入问题）
  for (const bs of busStopsForCity(districts)) {
    out.push({
      x: bs.x + Math.sin(bs.rotation) * 0.45,
      z: bs.z + Math.cos(bs.rotation) * 0.45,
      rotation: bs.rotation,
      variant: nextVariant(),
    });
  }
  return out;
}

/** 主干道路侧公交站台（批次 24 起导出：roadsideBinsForCity 站台旁布桶复用）。 */
export function busStopsForCity(districts: VirtualCityDistrictDef[]): Layout['busStops'] {
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
      const sideOffset = ROAD_WIDTH_MAIN / 2 + 0.35;
      out.push({
        x: c.x + dx * t + nx * sideOffset,
        z: c.z + dz * t + nz * sideOffset,
        rotation: Math.atan2(dx, dz),
      });
    });
  return out;
}

interface StreetPropsLayerProps {
  /** 城区静态表（v2.12 阶段 2 props 化；由 VirtualCityCityMap 注入 VIRTUAL_CITY_DISTRICTS）。 */
  districts: VirtualCityDistrictDef[];
}

/**
 * 全城行人总上限守卫（批次 20：high 110 / 批次 28 A4：low 60）。
 * 超出时按卡池表序裁尾部（新区先减，前 16 区满编）；确定性，无随机。
 */
function capPedestrians(districtProps: DistrictProps[], cap: number): void {
  let total = 0;
  for (const dp of districtProps) total += dp.pedestrians.length;
  if (total <= cap) return;
  for (let i = districtProps.length - 1; i >= 0 && total > cap; i--) {
    const dp = districtProps[i];
    const excess = Math.min(dp.pedestrians.length, total - cap);
    if (excess > 0) {
      dp.pedestrians.splice(dp.pedestrians.length - excess, excess);
      total -= excess;
    }
  }
}

export function StreetPropsLayer({ districts }: StreetPropsLayerProps) {
  // 批次 28 A4/A5：行人上限按质量档映射（high 110 / low 60，游戏侧策略；
  // detectQualityTier 读 GL 上下文，engine3d 不持有游戏字段）。
  const gl = useThree((s) => s.gl);
  const tier = useMemo(() => detectQualityTier(gl), [gl]);
  const pedCap = tier === 'low' ? PEDESTRIAN_TOTAL_CAP_LOW : PEDESTRIAN_TOTAL_CAP;

  const layout = useMemo<Layout>(() => {
    const districtProps = districts.map((d, idx) => propsForDistrict(d, idx));
    capPedestrians(districtProps, pedCap);
    const roadVehicles = vehiclesForRoads(districts);
    const roadTrees = roadTreesForRoads(districts);
    const busStops = busStopsForCity(districts);
    return { districtProps, roadVehicles, roadTrees, busStops };
  }, [districts, pedCap]);

  // 批次 20 §3.3：区内树 + 行道树合并单一 InstancedMesh 集合（3 draw call）。
  // 批次 28 B2：kind 标记 —— 区内树 tree.park / 行道树 tree.road（物件信息按实例区分）。
  const allTrees = useMemo(
    () => [
      ...layout.districtProps.flatMap((dp) =>
        dp.trees.map((t) => ({ x: t.x, z: t.z, scale: t.scale, kind: 'park' as const })),
      ),
      ...layout.roadTrees.map((t) => ({ x: t.x, z: t.z, scale: t.scale, kind: 'road' as const })),
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

      {/* 主干道红绿灯：批次 24 起移至 <TrafficSignals>（VirtualCityCityMap
          RoadsLayer 汇总 trafficSignalsForCity 全局实例化渲染，含相位动画） */}

      {/* 主干道公交站台 */}
      {layout.busStops.map((bs, i) => (
        <BusStop key={`busstop-${i}`} x={bs.x} z={bs.z} rotation={bs.rotation} />
      ))}
    </>
  );
}

// 注（批次 30 死文件清理）：V1 Pedestrian / PedestrianV2 / Tree / TreeV2 /
// StreetFurniture / StreetLight 六个零引用组件文件已删除（git grep 确认后 rm，
// tsc + npm run build 通过）；TreeV3.tsx 保留 —— 其 treeSeed/treeShape/TRUNK_GEOM
// 等是 TreesInstanced 的形态同源导出（组件本体不渲染）。
// 行人：V3（PedestrianV3）逐体动画 mixer，批次 20 不实例化（总上限 110）。
// 树：批次 20 §3.3 起改走 TreesInstanced（形态与 TreeV3Fallback 同源 treeSeed/treeShape）。
// 路灯：批次 20 §3.3 起由 VirtualCityCityMap 汇总点位走 StreetLightsInstanced。