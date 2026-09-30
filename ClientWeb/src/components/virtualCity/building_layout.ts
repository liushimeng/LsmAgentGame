/**
 * building_layout — 城区楼群布局（批次 30 A2「城市肌理」）。
 *
 * 背景：旧布局每区仅 4–6 栋（3×2 网格 + 抖动）散在 8×8 底板中部，32 区共 ~160 栋
 * 落在 1200 m×1200 m ⇒ 空旷沙盘观感（用户主诉）。真实城市街区是**街墙**：
 * 楼栋贴街区边界排布、沿边成排，中间留庭院。
 *
 * 布局契约（本文件是楼群布点唯一事实来源；DistrictBlock 渲染、StreetPropsLayer
 * 屋顶杂物/太阳能板锚点共用，避免两套 rnd 序漂移）：
 *   - 普通城区 **8–14 栋**（中央公园 2–4 栋 pavilion 例外）；
 *   - 四边街墙：每边 4 个槽位（中心距边 2.85 u），沿边 w∈[1.5,1.9] u、
 *     进深 d∈[1.1,1.6] u ⇒ 楼面退线 3.5~6.0 m（贴街区边界、留人行道退线）；
 *     宽度统一 ≥1.5 保证 hasBillboard/useSaw 分支区内一致（matSpecs 同签名 ⇒
 *     可合并为单一材质组，见 DistrictBuildings）；
 *   - 确定性伪随机（FNV-1a + mulberry32，与旧实现同源）；重渲染布局稳定。
 *
 * 楼高仍由 BuildingMesh/DistrictBuildings 按 cityScale.DISTRICT_FLOORS ×
 * 繁荣度插值（本文件只管占地与布点）。
 */

import type { VirtualCityDistrictDef } from '@/types/virtualCity';
import { isBuildable, mainRoadCorridors } from './cityObstacles';

export interface BuildingSpec {
  /** 相对区中心偏移（x, z）。 */
  x: number;
  z: number;
  /** 楼栋占地（宽 / 深，世界单位）。 */
  w: number;
  d: number;
  /** 楼高系数 0.6–1.0。 */
  factor: number;
  /** 布局种子序（稳定编号 b-<districtId>-<idx>）。 */
  idx?: number;
}

// ── 确定性伪随机（FNV-1a hash + mulberry32；与 DistrictBlock 旧实现同源）────

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

/** 街墙槽位中心距区中心（世界单位；8×8 底板半宽 4.0 ⇒ 楼面退线 3.5~6 m）。 */
const EDGE_CENTER = 2.85;
/** 每边槽位数（4 边 × 4 = 16 槽，取 8~14）。 */
const SLOTS_PER_SIDE = 4;
/** 槽位沿边跨度半宽（槽位 x/z ∈ ±2.7，间距 1.8）。 */
const SLOT_SPAN = 2.7;
/** 建筑退让路廊/水域的余量（批次 38 R2，世界单位 0.25 = 2.5 m）。 */
const KEEP_OUT_MARGIN = 0.25;
/** 兜底放宽后的余量（§4.2：某区 keep-out 误伤过狠时重算一轮）。 */
const KEEP_OUT_MARGIN_RELAXED = 0.1;
/** 普通城区保底楼数（低于此触发兜底 + dev 告警，禁止静默降级）。 */
const MIN_BUILDINGS = 4;

/**
 * 楼体是否可放（批次 38 R2 路廊退让）：中心点判 `isBuildable`，margin 取
 * 「退让余量 + 外接圆半径 max(w,d)*0.5」（轴对齐盒 vs 路廊线段的精确相交更准，
 * 但中心+外接圆已拦住用户主诉的穿楼，零新增几何代码）。
 *
 * 路廊只取 **main 级**（arterial / edgeLink）+ 一环 + 水域：connector 走街区
 * 庭院、街墙楼在底板边缘天然不重叠，纳入外接圆判定会误杀全区楼
 * （见 cityObstacles.mainRoadCorridors 注释）。
 */
function slotBuildable(def: VirtualCityDistrictDef, spec: BuildingSpec, margin: number): boolean {
  const reach = margin + Math.max(spec.w, spec.d) * 0.5;
  return isBuildable(def.x + spec.x, def.z + spec.z, reach, mainRoadCorridors());
}

/**
 * 楼群布局（seed = district id；仅高度随行情缩放，布局与 price_index 无关）。
 * 普通城区 8–14 栋街墙；中央公园 2–4 栋 pavilion（绿化由 StreetPropsLayer 树群承担）。
 *
 * 批次 38 R2：候选楼位（含 jitter 之后）判 `isBuildable`，不通过则**丢弃该槽位、
 * 不递补**（保持确定性与楼层高度可预测；空位 = 真实世界路口/环路旁的空地）。
 * 兜底：普通城区产出 < 4 栋时放宽 margin 重算一轮，仍不足则保留结果 + console.warn。
 */
export function buildingsFor(def: VirtualCityDistrictDef): BuildingSpec[] {
  const rnd = mulberry32(hashStr(def.id));
  const isPark = def.id === 'central_park';
  const count = isPark ? 2 + Math.floor(rnd() * 3) : 8 + Math.floor(rnd() * 7);
  const candidates: BuildingSpec[] = [];

  if (isPark) {
    // 公园景观小品：中心附近散布（半径 0.6~2.2），尺度小
    for (let i = 0; i < count; i++) {
      const angle = (i / count) * Math.PI * 2 + rnd() * 0.8;
      const radius = 0.6 + rnd() * 1.6;
      candidates.push({
        x: Math.cos(angle) * radius,
        z: Math.sin(angle) * radius,
        w: 1.0 + rnd() * 0.8,
        d: 0.9 + rnd() * 0.5,
        factor: 0.6 + rnd() * 0.4,
        idx: i,
      });
    }
    return candidates.filter((s) => slotBuildable(def, s, KEEP_OUT_MARGIN));
  }

  // 街墙：4 边 × 4 槽，确定性去尾抽 count 个（保留槽位顺序环绕）
  interface Slot { x: number; z: number; faceX: boolean }
  const slots: Slot[] = [];
  for (let s = 0; s < SLOTS_PER_SIDE; s++) {
    const t = -SLOT_SPAN + (s * 2 * SLOT_SPAN) / (SLOTS_PER_SIDE - 1);
    slots.push({ x: t, z: -EDGE_CENTER, faceX: true });   // 北边（沿 x 排）
    slots.push({ x: EDGE_CENTER, z: t, faceX: false });   // 东边
    slots.push({ x: t, z: EDGE_CENTER, faceX: true });    // 南边
    slots.push({ x: -EDGE_CENTER, z: t, faceX: false });  // 西边
  }
  // 确定性抽样：按 rnd 权重排序取前 count（可复现），再按原槽位顺序摆回
  const picked = slots
    .map((slot, i) => ({ slot, i, k: rnd() }))
    .sort((a, b) => a.k - b.k)
    .slice(0, count)
    .sort((a, b) => a.i - b.i);

  for (const [pi, { slot }] of picked.entries()) {
    // 沿边方向 = w（1.5~1.9，保证 hasBillboard/useSaw 分支全区一致）；进深 = d
    const jitter = (rnd() - 0.5) * 0.3;
    candidates.push({
      x: slot.x + (slot.faceX ? jitter : 0),
      z: slot.z + (slot.faceX ? 0 : jitter),
      w: 1.5 + rnd() * 0.4,
      d: 1.1 + rnd() * 0.5,
      factor: 0.6 + rnd() * 0.4,
      idx: pi,
    });
  }

  // 批次 38 R2：路廊/水域/一环 keep-out（丢弃不递补）
  let out = candidates.filter((s) => slotBuildable(def, s, KEEP_OUT_MARGIN));
  if (out.length < MIN_BUILDINGS) {
    // 兜底：放宽 margin 重算一轮（只放宽判定，不重掷 rnd —— 布局确定性不变）
    const relaxed = candidates.filter((s) => slotBuildable(def, s, KEEP_OUT_MARGIN_RELAXED));
    if (relaxed.length > out.length) out = relaxed;
    if (out.length < MIN_BUILDINGS) {
      // §5.3 禁止静默降级：keep-out 误伤导致楼数骤降必须 dev 告警
      console.warn(
        `[cityObstacles] buildingsFor(${def.id}) keep-out 后仅 ${out.length} 栋 < ${MIN_BUILDINGS}：` +
        `候选 ${candidates.length} 栋中多数落入路廊/水域/一环带域（margin ${KEEP_OUT_MARGIN}→${KEEP_OUT_MARGIN_RELAXED} 仍不足），` +
        '保留当前结果不递补。',
      );
    }
  }
  return out;
}
