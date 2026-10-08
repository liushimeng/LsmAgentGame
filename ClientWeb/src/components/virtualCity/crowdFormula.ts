/**
 * crowdFormula — 批次 34 §6.1「户外/户内非线性公式」+ §5.1 人物外观映射的**前端事实来源**。
 *
 * 后端同式实现见 `ServerGo/game/virtual_city/city/crowd.go::OutdoorCount`。
 * **两处必须逐式一致** —— 公式改动需双侧同步（与 `cityScale.REAL_DIMS_M` 同级契约）。
 * 前端侧的作用是：后端 `city.crowd` 缺失（旧房 / 未建城）时能自算兜底，
 * 以及单测能独立断言公式关键点（验收 V6）。
 *
 * 公式：
 *   V(N) = min( CAP_q , N , ceil( N0 · (N / N0)^γ ) )
 *   N0 = 100, γ = 0.45
 *
 * 语义（用户 R6 原文）：居民少（≤100）时**全部上街**；人越多，人均上街概率越低
 * （次线性），最终被画质上限截断 —— 其余居民「在建筑物内，隐藏起来」。
 */

import type {
  VirtualCityArchetype,
  VirtualCityCrowdEntry,
  VirtualCityModelKey,
} from '@/types/virtualCity';

/** 「人少就都上街」的拐点。N ≤ N0 时恒 V(N) = N。 */
export const CROWD_N0 = 100;
/** 次线性指数（0 < γ < 1 ⇒ 越多人、人均上街概率越低）。 */
export const CROWD_GAMMA = 0.45;
/**
 * 高画质档全城行人上限。
 * **沿用批次 28 的 110，本批次不抬** —— 行人不实例化（每人一个 GLB clone +
 * AnimationMixer，见批次 28 §1.1 A5），抬到 160 约多 100 DC，
 * 而默认视角 DC 已经超预算（实测 ~2230 / 目标 1500）。
 * 110 仍严格满足用户 R6：N ≤ 100 时 V(N)=N（全上街），N > 100 才触顶。
 */
export const CROWD_CAP_HIGH = 110;
/** 低画质档全城行人上限（批次 28 A4 口径，不变）。 */
export const CROWD_CAP_LOW = 60;
/** 换班候选（户内）随下发的条目名额上限（§6.4）。 */
export const CROWD_INDOOR_SEEDS = 16;

/**
 * 户外可见人数 V(N)。
 *
 * @param n 房间 resident_count（1..100000；0/非法 → 0）
 * @param cap 画质档上限（`crowdCapFor(quality)`）
 */
export function outdoorCount(n: number, cap: number): number {
  if (!Number.isFinite(n) || n <= 0 || !Number.isFinite(cap) || cap <= 0) return 0;
  const N = Math.floor(n);
  const curve = Math.ceil(CROWD_N0 * Math.pow(N / CROWD_N0, CROWD_GAMMA));
  return Math.max(0, Math.min(Math.floor(cap), N, curve));
}

/** 画质档 → 行人上限（与 `engine3d/quality` 的档位口径对齐）。 */
export function crowdCapFor(highQuality: boolean): number {
  return highQuality ? CROWD_CAP_HIGH : CROWD_CAP_LOW;
}

/**
 * 财富档 0..3（§5.1）：income 与 savings 各分 4 分位取较大。
 * 后端 `city/crowd.go::WealthTierFor` 同式。合成/兜底居民同样走本式，保证可复现。
 */
export function wealthTierFor(income: number, savings: number): number {
  const tierOf = (v: number): number => {
    if (!Number.isFinite(v) || v <= 0) return 0;
    if (v < 5_000) return 0;
    if (v < 15_000) return 1;
    if (v < 40_000) return 2;
    return 3;
  };
  return Math.max(tierOf(income), tierOf(savings));
}

/** L1 行业域下标（A=0 … Z=25）→ 原型判据用的分组（§5.1）。 */
const DOM_BUSINESS = new Set([15, 16, 17, 18]); // P 信息通信 / Q 金融 / R 专业服务 / S 科研
const DOM_WORKER = new Set([1, 6, 7, 8, 9, 10, 11]); // B 采矿 G 化工 H 金属 I 电子 J 汽车 K 能源 L 建筑
const DOM_SERVICE = new Set([12, 13, 14, 24]); // M 批发 N 交通 O 餐饮 Y 居民服务

/** `employment` 值域里归入服务/零工的两类。 */
const EMP_FLEXIBLE = new Set(['平台就业', '灵活就业']);

/**
 * 由居民特征推 3D 人物原型（§5.1 表，优先级自上而下）。
 * 后端 `city/crowd.go::ArchetypeFor` 同式；后端已算好时优先用下发值。
 */
export function archetypeFor(a: {
  age: number;
  domain: number;
  wealth: number;
  employment?: string;
  childrenCount?: number;
}): VirtualCityArchetype {
  const age = Number.isFinite(a.age) ? a.age : 35;
  const domain = Number.isFinite(a.domain) ? a.domain : -1;
  const wealth = Math.max(0, Math.min(3, Math.floor(a.wealth ?? 0)));
  if (age >= 55) return 'char_elder';
  if (age <= 24) return 'char_student';
  if (wealth === 3) return 'char_formal';
  if (DOM_BUSINESS.has(domain) && wealth >= 1) return 'char_business';
  if (DOM_WORKER.has(domain)) return 'char_worker';
  if (DOM_SERVICE.has(domain) || EMP_FLEXIBLE.has(a.employment ?? '')) return 'char_service';
  if (age >= 30 && age <= 54 && (a.childrenCount ?? 0) > 0) return 'char_parent';
  return 'char_casual';
}

// ── 批次 36 §4.1/§4.5：性别 × 年龄段 15 人物模型选型 ─────────────────────
// 几何 = 谁（性别/年龄），颜色 = 干什么（职业/财富）。人物卡无外貌字段，
// gender/age 是仅有的两个「体貌」信号 ⇒ 模型主轴；archetype 仅保留调色/scale 通道。

/**
 * 年龄段（§4.1 判据）：youth ≤24 / young 25–34 / middle 35–54 / senior 55–64 / elder ≥65。
 * 缺省（undefined / 非有限数）按 `young`。
 */
export function ageBandFor(age?: number): 'youth' | 'young' | 'middle' | 'senior' | 'elder' {
  if (!Number.isFinite(age)) return 'young';
  const a = age as number;
  if (a <= 24) return 'youth';
  if (a <= 34) return 'young';
  if (a <= 54) return 'middle';
  if (a <= 64) return 'senior';
  return 'elder';
}

/**
 * 性别 × 年龄段 → 15 模型 key 之一（`char_${g}_${band}`，§4.1 表）。
 * gender 归一 `m|f|u`（非 m/f 一律 u）；文件缺失时调用方走 char_casual → pedestrian_walk
 * 二段兜底（§4.5），故此处不做存在性检查。
 */
export function modelKeyFor(gender?: string, age?: number): VirtualCityModelKey {
  const g = gender === 'm' || gender === 'f' ? gender : 'u';
  return `char_${g}_${ageBandFor(age)}` as VirtualCityModelKey;
}

// ── 运行时二次差异化调色板（§5.1）──────────────────────────────────────
// 8 原型 × 色板组合 ≈ 可稳定区分 200+ 种观感。GLB 路径按材质槽名换色，
// 程序化 fallback 路径直接吃这三元组（对应 PEDESTRIAN_OUTFITS 的扩展）。

/** 上衣色（索引 0..7）：深蓝 / 灰 / 白 / 卡其 / 工装蓝 / 服务红 / 运动绿 / 素黑。 */
export const TOP_PALETTE = [
  '#2f4f7a', '#8a8f98', '#e6e8ec', '#a08a5c',
  '#2c5f8a', '#c23b3b', '#2f7a52', '#2a2d33',
] as const;
/** 裤色（索引 0..3）：随财富档 低→高 由深到浅、由粗到精。 */
export const PANTS_PALETTE = ['#2a3340', '#3d4a5c', '#5c626b', '#6e5b3e'] as const;
/** 肤色（索引 0..3）：由浅到深。户外体力劳动/低年龄偏深。 */
export const SKIN_PALETTE = ['#f0c9a0', '#e8c39a', '#d8a878', '#c08a5a'] as const;
/** 发色（索引 0..2）：深 / 中 / 灰白（≥50 岁取灰白）。 */
export const HAIR_PALETTE = ['#2a2118', '#4a3a2c', '#c9c4bc'] as const;

/** 外观三元组 + 发色 + 模型 key（GLB 换色通道 / 程序化几何共用）。 */
export interface CrowdAppearance {
  /** 调色/协议语义（批次 34 八原型；不再作为 GLB 选型主路径，见 §4.6）。 */
  archetype: VirtualCityArchetype;
  /** 批次 36 §4.5：GLB 选型 key（`char_{m,f,u}_{band}`，15 键之一）。 */
  model: string;
  top: string;
  pants: string;
  skin: string;
  hair: string;
  /**
   * 相对身高系数。批次 36 起由**年龄段主导**（§4.5：身高差已烘进几何，
   * scale 仅留 2~5% 微抖动，防双重叠加）：youth 1.02 / young·middle 1.0 /
   * senior 0.98 / elder 0.95 × health C 0.97 × index jitter ±1.5%。
   */
  scale: number;
}

/**
 * 由 crowd entry 推外观（确定性：同一 entry 恒同一观感）。
 * 没有 entry 明细时（旧房兜底）可只给 domain/age/wealth 造一个合成 entry。
 */
export function appearanceFor(e: {
  archetype?: VirtualCityArchetype;
  age: number;
  domain: number;
  wealth: number;
  gender?: string;
  health?: string;
  index: number;
}): CrowdAppearance {
  const arch = e.archetype
    ?? archetypeFor({ age: e.age, domain: e.domain, wealth: e.wealth });
  // 用 index 做稳定的细粒度抖动：同原型内也不同人不同色（禁 Math.random）。
  const jitter = (salt: number): number => {
    let h = (e.index + 1) * 0x9e3779b1 + salt * 0x85ebca6b;
    h = Math.imul(h ^ (h >>> 15), 0x2545f491);
    h = (h ^ (h >>> 13)) >>> 0;
    return h / 0xffffffff;
  };
  const wealth = Math.max(0, Math.min(3, Math.floor(e.wealth ?? 0)));
  const topIdx = Math.min(
    TOP_PALETTE.length - 1,
    // 财富决定色系倾向，index 抖动打散同档重复
    wealth * 2 + (jitter(1) < 0.5 ? 0 : 1),
  );
  const skinIdx = Math.min(
    SKIN_PALETTE.length - 1,
    // 低财富 + 体力域偏深肤色（户外日晒），再加抖动
    (DOM_WORKER.has(e.domain) || wealth === 0 ? 2 : 0) + (jitter(2) < 0.5 ? 0 : 1),
  );
  const hairIdx = e.age >= 50 ? 2 : jitter(3) < 0.45 ? 1 : 0;
  return {
    archetype: arch,
    model: modelKeyFor(e.gender, e.age),
    top: TOP_PALETTE[topIdx],
    pants: PANTS_PALETTE[wealth],
    skin: SKIN_PALETTE[skinIdx],
    hair: HAIR_PALETTE[hairIdx],
    scale: ageBandScale(e.age, e.health, jitter(4)),
  };
}

/**
 * 年龄段 × 健康档 → 身高系数（批次 36 §4.5；`archetypeScale` 已并入年龄段 ——
 * 身高差已烘进 15 件 GLB 几何，scale 只留 2~5% 微抖动，防双重叠加）。
 * @param j01 index 抖动（0..1）→ ±1.5% 微差，同段内不同人不同高。
 */
function ageBandScale(age: number | undefined, health: string | undefined, j01: number): number {
  const band = ageBandFor(age);
  const base = band === 'youth' ? 1.02
    : band === 'senior' ? 0.98
      : band === 'elder' ? 0.95
        : 1.0;
  // C 档健康（重大风险）略缩，读作体态衰弱；index jitter ±1.5%
  return (health === 'C' ? base * 0.97 : base) * (1 + (j01 - 0.5) * 0.03);
}

/**
 * 兜底合成 crowd entry（后端 `city.crowd` 缺失时用）。
 * 确定性：同一 (seed, index) 恒同一结果，禁 Math.random。
 */
export function synthCrowdEntry(index: number, seed: number, districtCount: number): VirtualCityCrowdEntry {
  const r = (salt: number): number => {
    let h = (index + 1) * 0x9e3779b1 ^ (seed + salt) * 0x85ebca6b;
    h = Math.imul(h ^ (h >>> 15), 0x2545f491);
    h = (h ^ (h >>> 13)) >>> 0;
    return h / 0xffffffff;
  };
  const age = 18 + Math.floor(r(1) * 52);
  const domain = Math.floor(r(2) * 26);
  const wealth = Math.min(3, Math.floor(r(3) * 4));
  // 就业形态 / 子女数：让 char_service（平台就业/灵活就业）与 char_parent
  // 两路判据在兜底路径上也能命中（§130 防「声明了却从不接线」）。
  const employment = r(7) < 0.22 ? '平台就业' : r(7) < 0.4 ? '灵活就业' : '全职';
  const childrenCount = age >= 30 && age <= 54 && r(8) < 0.55 ? 1 + Math.floor(r(9) * 2) : 0;
  return {
    index,
    card_id: `synth-${index}`,
    name: `居民${index + 1}`,
    archetype: archetypeFor({ age, domain, wealth, employment, childrenCount }),
    gender: r(4) < 0.5 ? 'm' : 'f',
    age,
    domain,
    district: Math.floor(r(5) * districtCount),
    wealth,
    health: r(6) < 0.7 ? 'A' : r(6) < 0.92 ? 'B' : 'C',
    indoor: false,
  };
}
