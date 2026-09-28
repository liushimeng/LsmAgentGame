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

/**
 * 楼群布局（seed = district id；仅高度随行情缩放，布局与 price_index 无关）。
 * 普通城区 8–14 栋街墙；中央公园 2–4 栋 pavilion（绿化由 StreetPropsLayer 树群承担）。
 */
export function buildingsFor(def: VirtualCityDistrictDef): BuildingSpec[] {
  const rnd = mulberry32(hashStr(def.id));
  const isPark = def.id === 'central_park';
  const count = isPark ? 2 + Math.floor(rnd() * 3) : 8 + Math.floor(rnd() * 7);
  const out: BuildingSpec[] = [];

  if (isPark) {
    // 公园景观小品：中心附近散布（半径 0.6~2.2），尺度小
    for (let i = 0; i < count; i++) {
      const angle = (i / count) * Math.PI * 2 + rnd() * 0.8;
      const radius = 0.6 + rnd() * 1.6;
      out.push({
        x: Math.cos(angle) * radius,
        z: Math.sin(angle) * radius,
        w: 1.0 + rnd() * 0.8,
        d: 0.9 + rnd() * 0.5,
        factor: 0.6 + rnd() * 0.4,
        idx: i,
      });
    }
    return out;
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
    out.push({
      x: slot.x + (slot.faceX ? jitter : 0),
      z: slot.z + (slot.faceX ? 0 : jitter),
      w: 1.5 + rnd() * 0.4,
      d: 1.1 + rnd() * 0.5,
      factor: 0.6 + rnd() * 0.4,
      idx: pi,
    });
  }
  return out;
}
