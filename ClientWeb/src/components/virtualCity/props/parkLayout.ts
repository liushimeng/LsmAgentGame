/**
 * parkLayout — 中央公园设施布点 + 净空表（批次 45 · C1/C3）。
 *
 * 单一事实来源：ParkExtras（凉亭/游乐/健身 GLB 挂点）、ParkBenches / ParkLamps
 * （实例化点位）、ParkGrounds（安全垫）与 StreetPropsLayer 公园树布点
 * （`parkFacilityClearOf` 净空剔除）全部消费本表 —— 改点位只改这一处。
 *
 * 布点依据（GB 51192-2016《公园设计规范》）：
 *   · 座椅：§3.5 条文说明「沿园路布置时隔 50~100 m」（园路 2×74 m ⇒ 每臂 2 把）
 *     + 游乐/健身场旁各 1 把看护座 ⇒ 全园 10 把；
 *   · 园灯：庭院灯厂家档（2.5~3.5 m 灯高 ⇒ 光池 ⌀5~8 m）⇒ 间距 10 m（1 u），
 *     四臂各 3 盏、两侧交错 ⇒ 全园 12 盏；
 *   · 儿童活动场：§6.2「宜选择柔性、耐磨的地面材料」⇒ 游乐/健身区 EPDM 垫
 *     （ParkGrounds 渲染，本表只出位置与尺寸）；
 *   · 全部为确定性常量（禁 Math.random）。
 *
 * 坐标系：**公园局部坐标**（公园心 = (0,-22)，园宽 ±4 u = ±40 m）；
 * `PARK_CENTER` 负责与世界坐标互转。
 */

import { districtCenter } from '@/types/virtualCity';

/** 公园中心（世界坐标）。 */
export const PARK_CENTER = districtCenter('central_park');

/** 公园局部坐标 → 世界坐标。 */
export const parkWorld = (lx: number, lz: number): [number, number] => [
  PARK_CENTER.x + lx,
  PARK_CENTER.z + lz,
];

/** 局部坐标 + 绕 Y 朝向（rad）的设施挂点。 */
export interface ParkSpot {
  x: number;
  z: number;
  /** 绕 Y 旋转（物件「面向」约定：长椅座面朝向 / 凉亭入口朝向）。 */
  rot: number;
}

/**
 * 局部 +Z 指向目标方向 (tx,tz) 的绕 Y 角（three R_y(θ)·(0,0,1) = (sinθ,0,cosθ)）。
 */
export const rotToward = (sx: number, sz: number, tx: number, tz: number): number =>
  Math.atan2(tx - sx, tz - sz);

// ── 三大件（单实例，ParkExtras 消费）─────────────────────────────────────

/** 六角亭：西南象限原位（批次 18-AA 起），入口台阶面向公园心（喷泉）。 */
export const PAVILION_SPOT: ParkSpot = { x: -2.2, z: -2.5, rot: rotToward(-2.2, -2.5, 0, 0) };

/** 儿童游乐组合：东南象限原位；滑道长轴沿 X，座区面向步道（-Z）。 */
export const PLAYGROUND_SPOT: ParkSpot = { x: 2.2, z: -2.5, rot: 0 };

/** 健身三件套：东北象限（批次 45 新设）；三件沿 X 一字排开，面向步道（-Z）。 */
export const FITNESS_SPOT: ParkSpot = { x: 2.1, z: 2.1, rot: 0 };

/** 公厕（批次 18-AA 原位，本批不动）：西北象限。 */
export const RESTROOM_SPOT: ParkSpot = { x: -2.5, z: 2.2, rot: 0 };

// ── 长椅 ×10（ParkBenches 消费；四臂各 2 + 看护 2）───────────────────────

/** 十字园路四臂的长椅：|横距| 0.55（园路半宽 0.25 + 座深一半），两侧交替，面向园路。 */
const PATH_BENCH_LOCAL: ParkSpot[] = [
  // +X 臂（沿 x，z=±0.55）
  { x: 1.2, z: 0.55, rot: Math.PI },
  { x: 2.6, z: -0.55, rot: 0 },
  // -X 臂
  { x: -1.2, z: -0.55, rot: 0 },
  { x: -2.6, z: 0.55, rot: Math.PI },
  // +Z 臂（沿 z，x=±0.55）
  { x: 0.55, z: 1.2, rot: -Math.PI / 2 },
  { x: -0.55, z: 2.6, rot: Math.PI / 2 },
  // -Z 臂
  { x: -0.55, z: -1.2, rot: Math.PI / 2 },
  { x: 0.55, z: -2.6, rot: -Math.PI / 2 },
  // 看护座：游乐场西侧 / 健身场西侧，面向设施
  { x: 0.9, z: -2.5, rot: rotToward(0.9, -2.5, PLAYGROUND_SPOT.x, PLAYGROUND_SPOT.z) },
  { x: 0.7, z: 2.1, rot: rotToward(0.7, 2.1, FITNESS_SPOT.x, FITNESS_SPOT.z) },
];

/** 世界坐标长椅点位（10 把）。 */
export const PARK_BENCHES: ParkSpot[] = PATH_BENCH_LOCAL.map((s) => ({
  x: PARK_CENTER.x + s.x,
  z: PARK_CENTER.z + s.z,
  rot: s.rot,
}));

// ── 园灯 ×12（ParkLamps 消费；四臂各 3，|横距| 0.42，间距 1 u = 10 m 交错）──

const PATH_LAMP_LOCAL: Array<[number, number]> = [
  // +X 臂
  [0.85, 0.42], [1.85, -0.42], [2.85, 0.42],
  // -X 臂
  [-0.85, -0.42], [-1.85, 0.42], [-2.85, -0.42],
  // +Z 臂
  [0.42, 0.85], [-0.42, 1.85], [0.42, 2.85],
  // -Z 臂
  [-0.42, -0.85], [0.42, -1.85], [-0.42, -2.85],
];

/** 世界坐标园灯点位（12 盏；径向对称件，rot 恒 0）。 */
export const PARK_LAMPS: ParkSpot[] = PATH_LAMP_LOCAL.map(([lx, lz]) => ({
  x: PARK_CENTER.x + lx,
  z: PARK_CENTER.z + lz,
  rot: 0,
}));

// ── 安全铺装（ParkGrounds 消费；GB 51192 §6.2 柔性铺装语义）───────────────

/** 游乐区 EPDM 垫（局部坐标 + 尺寸，米）。 */
export const PLAYGROUND_PAD = { x: 2.2, z: -2.5, w: 5.2, d: 3.6 } as const;
/** 健身区沙石垫（局部坐标 + 尺寸，米）。 */
export const FITNESS_PAD = { x: 2.1, z: 2.1, w: 4.6, d: 2.4 } as const;

// ── 净空表（C1：公园树布点剔除；防树穿设施/花坛/座椅/园灯）───────────────

/**
 * 净空圆（局部坐标，半径 = 设施外接半径 + 0.05 m 树干半格）。
 * 消费端：`StreetPropsLayer` 公园区树 filter（只剔树，不改树本体 —— 批次 45
 * 方案 §6 例外①）。
 */
const KEEPOUT_LOCAL: Array<[number, number, number]> = [
  [PAVILION_SPOT.x, PAVILION_SPOT.z, 0.42],      // 屋面出檐对角半径 ~0.36 + 余量
  [PLAYGROUND_SPOT.x, PLAYGROUND_SPOT.z, 0.38],  // 滑道端 0.23 + 摆动余量
  [FITNESS_SPOT.x, FITNESS_SPOT.z, 0.38],        // 三件套半长 + 单杠跌落
  [RESTROOM_SPOT.x, RESTROOM_SPOT.z, 0.28],
  // 花坛 ×4（ParkGrounds BED_OFFSETS ±1.7，r 0.42）
  [1.7, 1.7, 0.10], [-1.7, 1.7, 0.10], [1.7, -1.7, 0.10], [-1.7, -1.7, 0.10],
  // 长椅 / 园灯点位
  ...PATH_BENCH_LOCAL.map((s) => [s.x, s.z, 0.13] as [number, number, number]),
  ...PATH_LAMP_LOCAL.map(([lx, lz]) => [lx, lz, 0.08] as [number, number, number]),
];

/** 世界坐标（x,z）是否避开全部公园设施净空圆（供公园树布点 filter）。 */
export function parkFacilityClearOf(x: number, z: number): boolean {
  for (const [lx, lz, r] of KEEPOUT_LOCAL) {
    const dx = x - (PARK_CENTER.x + lx);
    const dz = z - (PARK_CENTER.z + lz);
    if (dx * dx + dz * dz < r * r) return false;
  }
  return true;
}
