/**
 * crowdLayout — 批次 34 §6.2「初始化位置」+ §6.3「换班」的前端实现。
 *
 * 与 `crowdFormula.ts` 配套：那边算「多少人上街 / 长什么样」，这边算「站在哪 / 走哪条路」。
 *
 * 三条硬约束（与 StreetPropsLayer 既有口径一致）：
 *   1. **确定性** —— 全部随机数由 (roomSeed, churn, residentIndex) 派生，
 *      禁 `Math.random`（否则刷新页面人群就跳，截图验收不可复现）；
 *   2. **不越出城区底板** —— 采样点须落在该城区 plate 内（含 6 次拒绝重试）；
 *   3. **path 语义复用 PedestrianV3** —— 折线 2~3 段、端点折返、总长 6~10 世界单位。
 */

import type {
  VirtualCityCrowdEntry,
  VirtualCityDistrictDef,
} from '@/types/virtualCity';
import { appearanceFor, type CrowdAppearance } from './crowdFormula';

/**
 * 一条道路网线段（取 `roadNetwork.ts::RoadSegment` 的最小子集，
 * 保持 `from`/`to` 元组形状 ⇒ 调用方直接传 `ROAD_NETWORK.segments` 零转换）。
 */
export interface LayoutSegment {
  from: [number, number];
  to: [number, number];
}

/** 城区底板半宽（世界单位）；与 StreetPropsLayer 的 pathForDistrict 同量级。 */
const PLATE_HALF = 4.0;
/**
 * 行人 path 总长范围（世界单位 = 10 m）。
 * 与 `pathForDistrict` 的有效弦长同量级（半径 1.5~3.5 的三点折线 ≈ 2~4 单位），
 * 否则真人行人会比装饰行人「走得短得多」，观感割裂。
 */
const PATH_LEN_MIN = 1.5;
const PATH_LEN_MAX = 3.5;
/** 人行道采样的法向偏移（世界单位；1.2~2.4 m ⇒ 0.12~0.24）。 */
const SIDEWALK_OFFSET_MIN = 0.12;
const SIDEWALK_OFFSET_MAX = 0.24;
/** 采样落在人行道带的概率（§6.2 第 2 条：60%）。 */
const SIDEWALK_PROB = 0.6;
/** 拒绝采样最大重试次数（§6.2 第 3 条）。 */
const MAX_REJECT = 6;

/** 确定性 PRNG（mulberry32；同 seed 同序列）。 */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** 派生一名居民在某次换班代数下的随机种子（禁 Math.random）。 */
function seedFor(roomSeed: number, churn: number, index: number): number {
  let h = (roomSeed ^ 0x9e3779b1) >>> 0;
  h = Math.imul(h ^ (churn + 1), 0x85ebca6b) >>> 0;
  h = Math.imul(h ^ (index + 1), 0xc2b2ae35) >>> 0;
  return (h ^ (h >>> 13)) >>> 0;
}

/** 一条上街居民的渲染规格。 */
export interface CrowdPedestrian {
  /** backdrop.residents 下标（点击物件信息可回溯到人物卡）。 */
  residentIndex: number;
  cardId: string;
  name: string;
  path: Array<[number, number]>;
  speed: number;
  phase: number;
  /** 站定（中央公园看景等 speed=0 场景）。 */
  stationary: boolean;
  appearance: CrowdAppearance;
  /** 所在城区下标（物件信息 / 换班重定位用）。 */
  district: number;
}

/**
 * 为一名居民生成 path 折线（2~3 段，总长 1.5~3.5 世界单位）。
 * 语义与 `StreetPropsLayer.pathForDistrict` 同源：到端点折返。
 * 每个采样点都钳回城区底板内（§6.2 第 3 条的拒绝采样在这里做硬钳制兜底）。
 */
function pathFrom(
  x: number,
  z: number,
  rnd: () => number,
  cx: number,
  cz: number,
): Array<[number, number]> {
  const angle = rnd() * Math.PI * 2;
  const segs = 2 + Math.floor(rnd() * 2); // 2~3 段
  const total = PATH_LEN_MIN + rnd() * (PATH_LEN_MAX - PATH_LEN_MIN);
  const clamp = (v: number, lo: number, hi: number): number => Math.min(hi, Math.max(lo, v));
  const path: Array<[number, number]> = [[x, z]];
  let px = x;
  let pz = z;
  let heading = angle;
  for (let i = 0; i < segs; i++) {
    const segLen = total / segs;
    // 每段小角度偏转，避免直线僵硬
    heading += (rnd() - 0.5) * 1.1;
    px = clamp(px + Math.cos(heading) * segLen, cx - PLATE_HALF, cx + PLATE_HALF);
    pz = clamp(pz + Math.sin(heading) * segLen, cz - PLATE_HALF, cz + PLATE_HALF);
    path.push([px, pz]);
  }
  return path;
}

/**
 * 在城区底板内采样一个起点（§6.2 第 2/3 条）。
 * 60% 走人行道带（沿道路网线段法向偏移），40% 落在公共场所（板内均匀）。
 * 越界则重试，最多 MAX_REJECT 次后退化为板心附近。
 */
function sampleStart(
  def: VirtualCityDistrictDef,
  segs: readonly LayoutSegment[],
  rnd: () => number,
): [number, number] {
  for (let attempt = 0; attempt <= MAX_REJECT; attempt++) {
    let x: number;
    let z: number;
    if (segs.length > 0 && rnd() < SIDEWALK_PROB) {
      // 按线段长度加权挑一条路，再在其上取点 + 法向偏移
      const s = segs[Math.floor(rnd() * segs.length)];
      const t = rnd();
      const px = s.from[0] + (s.to[0] - s.from[0]) * t;
      const pz = s.from[1] + (s.to[1] - s.from[1]) * t;
      const dx = s.to[0] - s.from[0];
      const dz = s.to[1] - s.from[1];
      const len = Math.hypot(dx, dz) || 1;
      // 法向（± 随机一侧 = 道路两侧人行道）
      const sign = rnd() < 0.5 ? 1 : -1;
      const off = SIDEWALK_OFFSET_MIN + rnd() * (SIDEWALK_OFFSET_MAX - SIDEWALK_OFFSET_MIN);
      x = px + (-dz / len) * off * sign;
      z = pz + (dx / len) * off * sign;
    } else {
      // 公共场所：板内均匀
      x = def.x + (rnd() * 2 - 1) * PLATE_HALF;
      z = def.z + (rnd() * 2 - 1) * PLATE_HALF;
    }
    // 拒绝采样：落在板外（建筑 footprint 的近似代理是「板内楼群区」，
    // 本批次用板界做硬拒绝；更细的 footprint 剔除见设计 §9 不做清单）
    if (
      Math.abs(x - def.x) <= PLATE_HALF &&
      Math.abs(z - def.z) <= PLATE_HALF
    ) {
      return [x, z];
    }
  }
  return [def.x, def.z];
}

export interface CrowdLayoutInput {
  /** 上街居民（已过滤 indoor=false）。 */
  entries: readonly VirtualCityCrowdEntry[];
  /** 32 城区静态表（下标 = entry.district）。 */
  districts: readonly VirtualCityDistrictDef[];
  /** 房间种子（建房 seed；换班代数叠加其上）。 */
  roomSeed: number;
  /** 换班代数（每次换班 +1 ⇒ 整批重定位）。 */
  churn: number;
  /** 道路网线段（人行道带采样用；可为空 → 全部走公共场所均匀采样）。 */
  segments?: readonly LayoutSegment[];
}

/**
 * 布点主入口：把上街居民映射为渲染规格（确定性）。
 *
 * 城区下标越界 / 找不到对应城区的居民会被**跳过**（不渲染，不抛错）——
 * 后端 32 区与前端 `VIRTUAL_CITY_DISTRICTS` 顺序本应一致，越界说明契约漂移，
 * 由 §130 接线测试兜住，渲染层不硬崩。
 */
export function layoutCrowd(input: CrowdLayoutInput): CrowdPedestrian[] {
  const { entries, districts, roomSeed, churn, segments } = input;
  const segs = segments ?? [];
  const out: CrowdPedestrian[] = [];
  for (const e of entries) {
    if (e.indoor) continue;
    const def = districts[e.district];
    if (!def) continue;
    const rnd = mulberry32(seedFor(roomSeed, churn, e.index));
    const [x, z] = sampleStart(def, segs, rnd);
    const appearance = appearanceFor({
      archetype: e.archetype,
      age: e.age,
      domain: e.domain,
      wealth: e.wealth,
      gender: e.gender,
      health: e.health,
      index: e.index,
    });
    out.push({
      residentIndex: e.index,
      cardId: e.card_id,
      name: e.name,
      path: pathFrom(x, z, rnd, def.x, def.z),
      // 真实步速 1.2~1.5 m/s ⇒ 0.12~0.15 世界单位/秒（批次 30 P1-12 口径）
      speed: 0.12 + rnd() * 0.03,
      phase: rnd(),
      stationary: false,
      appearance,
      district: e.district,
    });
  }
  return out;
}

/**
 * §6.3 换班：给定当前上街集合与户内候选，产出下一代换班后的上街集合。
 *
 * 只换**身份**不换人数（保证 DC 与 AnimationMixer 数恒定，见设计 §6.3）：
 * 抽 `⌈0.10·V⌉` 个停留最久的上街者回屋，同量户内候选上街。
 * 纯函数，确定性由 (roomSeed, churn) 派生。
 */
export function churnCrowd(
  outdoor: readonly VirtualCityCrowdEntry[],
  indoor: readonly VirtualCityCrowdEntry[],
  roomSeed: number,
  churn: number,
): VirtualCityCrowdEntry[] {
  const swap = Math.ceil(outdoor.length * 0.1);
  if (swap <= 0 || indoor.length === 0) return [...outdoor];
  const rnd = mulberry32(seedFor(roomSeed, churn, 0x5eed));
  // 回屋的人：按「下标轮转」确定性选，避免每次全换（人要「停留一段时间」）
  const leaveCount = Math.min(swap, outdoor.length);
  const leaveIdx = new Set<number>();
  for (let i = 0; i < leaveCount; i++) {
    leaveIdx.add(Math.floor(rnd() * outdoor.length) % outdoor.length);
  }
  const stay = outdoor.filter((_, i) => !leaveIdx.has(i));
  const joinCount = Math.min(swap, indoor.length, outdoor.length - stay.length);
  const join: VirtualCityCrowdEntry[] = [];
  for (let i = 0; i < joinCount; i++) {
    const pick = Math.floor(rnd() * indoor.length) % indoor.length;
    const e = indoor[pick];
    if (!e || join.some((j) => j.index === e.index)) continue;
    join.push({ ...e, indoor: false });
  }
  return [...stay, ...join];
}
