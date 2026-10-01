/**
 * building_layout — 城区楼群布局（批次 30 A2「城市肌理」）。
 *
 * 背景：旧布局每区仅 4–6 栋（3×2 网格 + 抖动）散在 8×8 底板中部，32 区共 ~160 栋
 * 落在 1200 m×1200 m ⇒ 空旷沙盘观感（用户主诉）。真实城市街区是**街墙**：
 * 楼栋贴街区边界排布、沿边成排，中间留庭院。
 *
 * 布局契约（本文件是楼群布点唯一事实来源；DistrictBlock 渲染、StreetPropsLayer
 * 屋顶杂物/太阳能板锚点共用，避免两套 rnd 序漂移）：
 *   - 普通城区街墙 **12 槽**（3 槽/边，批次 39 C1 由 4 改 3）取 8–12 候选，判定后
 *     产出 4–8 栋（中央公园 2–4 候选 → 1–2 栋 pavilion 例外）；
 *   - 四边街墙：每边 4 个槽位（中心距边 2.85 u），沿边 w∈[1.5,1.9] u、
 *     进深 d∈[1.1,1.6] u ⇒ 楼面退线 3.5~6.0 m（贴街区边界、留人行道退线）；
 *     宽度统一 ≥1.5 保证 hasBillboard/useSaw 分支区内一致（matSpecs 同签名 ⇒
 *     可合并为单一材质组，见 DistrictBuildings）；
 *   - **楼-楼净距**（批次 39 C1）：相邻槽位中心距 1.8 u 而楼宽可达 1.9 u + jitter
 *     ±0.15 u ⇒ 最坏情况相邻两楼重叠约 2.3 u（23 m）。本文件按槽位顺序逐个与
 *     「已接受集」判 AABB 净距（`rectsOverlap`，最小净距 u(0.3) = 3 m）；
 *   - **临街轴**（批次 39 C3）：南北边楼的临街面是 ±Z、东西边楼是 ±X ⇒ `streetAxis`
 *     + `streetSign` 一并透出，供 `buildBuildingParts` 决定「首层商业 / 门 / 雨棚」
 *     挂哪一面；
 *   - 确定性伪随机（FNV-1a + mulberry32，与旧实现同源）；重渲染布局稳定。
 *
 * 楼高仍由 BuildingMesh/DistrictBuildings 按 cityScale.DISTRICT_FLOORS ×
 * 繁荣度插值（本文件只管占地与布点）。批次 39 C2：`factor` 改幂律采样，
 * 区间与归一化口径由 `cityScale.BUILDING_FACTOR_MIN/MAX` 单点定义。
 */

import type { VirtualCityDistrictDef } from '@/types/virtualCity';
import { isBuildable, mainRoadCorridors } from './cityObstacles';
import { BUILDING_FACTOR_MAX, BUILDING_FACTOR_MIN, u } from './cityScale';

export interface BuildingSpec {
  /** 相对区中心偏移（x, z）。 */
  x: number;
  z: number;
  /** 楼栋占地（宽 / 深，世界单位）。 */
  w: number;
  d: number;
  /**
   * 楼高系数 {@link BUILDING_FACTOR_MIN}–{@link BUILDING_FACTOR_MAX}（幂律采样）。
   * **不是** 0..1 归一化系数，禁止当归一化量直接乘；唯一消费点是
   * `cityScale.buildingTopY(id, prosperity, factor)`（渲染与相机碰撞体同源）。
   */
  factor: number;
  /**
   * 批次 39 C3：临街面法向轴。街墙布局下南北边楼（z=±EDGE_CENTER）为 `'z'`、
   * 东西边楼（x=±EDGE_CENTER）为 `'x'`。决定 `facadeBase`（含首层商业）挂哪一面。
   */
  streetAxis: 'x' | 'z';
  /** 批次 39 C3：临街面朝向符号（+1 = 该轴正侧临街，−1 = 负侧临街）。 */
  streetSign: 1 | -1;
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
/**
 * 每边槽位数（批次 39 C1：4 → 3）。**这不是随手调参，是 C1 的几何前提**：
 * 槽位沿边跨度半宽固定 2.7 时，4 槽的相邻中心距只有 1.8 u，而楼宽 w∈[1.5,1.9]
 * + jitter ±0.15 ⇒ 相邻两楼**必然**互相穿模 0~10 m（C1 实测：相邻楼对 100% 冲突，
 * 判定一开就把全城楼数从 336 砍到 180，且净距从 0.03 放宽到 0 也救不回来 ——
 * 说明瓶颈是槽位间距而非 margin 口径）。改 3 槽后中心距 2.7 u，最坏情况净距
 * 仍有 2.7 − 1.9 − 0.3 = 0.5 u（5 m）> 3 m ⇒ **同一条街墙上的相邻楼恒过判定**，
 * 楼数（12 槽）与批次 30 的密排观感基本保持。
 * 代价：转角处南北边楼与东西边楼的两个端点槽位仍会互相穿模（中心距仅 0.21 u），
 * 由 C1 顺序判定自然丢掉**后出现的那一个**（每个转角留一栋楼），这也是真实街角的样子。
 */
const SLOTS_PER_SIDE = 3;
/** 槽位沿边跨度半宽（槽位 x/z ∈ ±2.7；3 槽 ⇒ 中心距 2.7 u）。 */
const SLOT_SPAN = 2.7;
/** 建筑退让路廊/水域的余量（批次 38 R2，世界单位 0.25 = 2.5 m）。 */
const KEEP_OUT_MARGIN = 0.25;
/** 兜底放宽后的余量（§4.2：某区 keep-out 误伤过狠时重算一轮）。 */
const KEEP_OUT_MARGIN_RELAXED = 0.1;
/** 普通城区保底楼数（低于此触发兜底 + dev 告警，禁止静默降级）。 */
const MIN_BUILDINGS = 4;
/**
 * 批次 39 C1：楼-楼最小净距（世界单位）。`u(0.3)` = 3 m —— 真实防火间距远大于此，
 * 但街墙楼贴边成排，3 m 净距足以消除可见穿模且不至于把楼数砍到不足。
 */
const MIN_GAP = u(0.3);
/** 兜底放宽后的净距（仍 > 0 ⇒ 绝不重叠，只是不再强制留缝）。 */
const MIN_GAP_RELAXED = u(0.1);

/**
 * 批次 39 C2：楼高系数幂律采样 —— `rnd()^2.2` 把分布压向区间下端
 * （多数楼偏低、少数楼拔高），配合 `cityScale.buildingTopY` 的层数增量口径
 * 打出高低差分明的天际线。
 *
 * ⚠ **只改分布不改随机流**：本函数与批次 38 的 `0.6 + rnd()*0.4` 一样**只消耗一个
 * rnd()**，因此后续槽位的坐标/尺寸与 `idx` 编号逐位不变（布局稳定性契约）。
 */
function sampleFactor(rnd: () => number): number {
  return BUILDING_FACTOR_MIN + (BUILDING_FACTOR_MAX - BUILDING_FACTOR_MIN) * Math.pow(rnd(), 2.2);
}

/** 轴对齐 XZ 占地矩形（中心 + 半宽；区相对坐标即可，判定与平移无关）。 */
export interface Footprint {
  cx: number;
  cz: number;
  /** X 向半宽（= 楼宽 w 的一半）。 */
  hw: number;
  /** Z 向半深（= 楼深 d 的一半）。 */
  hd: number;
}

/** 楼栋占地矩形（批次 39 C1；`BuildingSpec` → 判定用矩形）。 */
export function footprintOf(spec: BuildingSpec): Footprint {
  return { cx: spec.x, cz: spec.z, hw: spec.w / 2, hd: spec.d / 2 };
}

/**
 * 批次 39 C1：两个轴对齐 XZ 矩形是否**净距不足**（含 `tol` 净距要求）。
 *
 * 纯函数、无随机、无副作用（可单测）。判据：两轴向的间隙都小于 `tol` 才算冲突
 * —— 分离轴定理在 2D 矩形上的直接结论，等价于「膨胀 tol 后的 AABB 相交」。
 */
export function rectsOverlap(a: Footprint, b: Footprint, tol: number): boolean {
  return (
    Math.abs(a.cx - b.cx) < a.hw + b.hw + tol &&
    Math.abs(a.cz - b.cz) < a.hd + b.hd + tol
  );
}

/**
 * 批次 39 C1：按槽位顺序**逐个**与「已接受集」判净距，返回无冲突子序列。
 *
 * ⚠ **顺序敏感（勿改成两两全比）**：判定必须按传入顺序依次进行，新候选只与
 * **已接受**的楼比。若改成两两互比（互相排斥），同一排相邻的两个槽位会互相
 * 判为冲突而双双落空，街墙出现空洞。
 *
 * 口径与 keep-out 一致：冲突则**丢弃该槽位、不递补**（批次 38 定）。
 */
export function filterNoOverlap(
  specs: readonly BuildingSpec[],
  tol: number,
): BuildingSpec[] {
  const accepted: BuildingSpec[] = [];
  const rects: Footprint[] = [];
  for (const s of specs) {
    const r = footprintOf(s);
    if (rects.some((a) => rectsOverlap(r, a, tol))) continue;
    accepted.push(s);
    rects.push(r);
  }
  return accepted;
}

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
 * 普通城区街墙 12 槽（3/边）取 8–12 候选，判定后实际产出 **4–8 栋**（32 区实测
 * min 1 / max 8 / 合计 178；判定前 keep-out 基线为 224 栋，8 个区本就 < 4）；
 * 中央公园 2–4 栋 pavilion 取 1–2 栋（绿化由 StreetPropsLayer 树群承担）。
 *
 * 批次 38 R2：候选楼位（含 jitter 之后）判 `isBuildable`，不通过则**丢弃该槽位、
 * 不递补**（保持确定性与楼层高度可预测；空位 = 真实世界路口/环路旁的空地）。
 * 批次 39 C1：再判楼-楼净距，同样丢弃不递补。
 * 兜底：普通城区产出 < 4 栋时同时放宽 keep-out 余量与净距重算一轮，仍不足则保留
 * 结果 + console.warn（禁止静默降级）。
 */
export function buildingsFor(def: VirtualCityDistrictDef): BuildingSpec[] {
  const rnd = mulberry32(hashStr(def.id));
  const isPark = def.id === 'central_park';
  const count = isPark
    ? 2 + Math.floor(rnd() * 3)
    // 槽位数是硬上限（批次 39 C1 改 3 槽/边 ⇒ 12 槽）：抽超了只取前 count 个，
    // 不生成不存在的槽位（旧 4 槽/边 = 16 槽时 count ≤ 14，这条分支恒不触发）。
    : Math.min(SLOTS_PER_SIDE * 4, 8 + Math.floor(rnd() * 7));
  const candidates: BuildingSpec[] = [];

  if (isPark) {
    // 公园景观小品：中心附近散布（半径 1.2~2.8），尺度小
    // 批次 39 C1：散布半径下限 0.6 → 1.2。旧半径下两座小品中心距常 < 2.1 u
    // （楼宽 1.0~1.8 + 净距 0.3），C1 判定会把它们成对丢掉（中央公园只剩 1 座）；
    // 抬高半径下限让「互不穿模」由布点本身保证，判定只兜底。
    for (let i = 0; i < count; i++) {
      const angle = (i / count) * Math.PI * 2 + rnd() * 0.8;
      const radius = 1.2 + rnd() * 1.6;
      const x = Math.cos(angle) * radius;
      const z = Math.sin(angle) * radius;
      candidates.push({
        x,
        z,
        w: 1.0 + rnd() * 0.8,
        d: 0.9 + rnd() * 0.5,
        factor: sampleFactor(rnd),
        // 批次 39 C3：公园小品不贴街墙，按所在方位取「离区中心更远的那根轴」
        // 为临街轴（朝向外环），符号 = 该轴所在半区。
        streetAxis: Math.abs(x) >= Math.abs(z) ? 'x' : 'z',
        streetSign: (Math.abs(x) >= Math.abs(z) ? x : z) >= 0 ? 1 : -1,
        idx: i,
      });
    }
    // 批次 39 C1：公园小品同样判楼-楼净距（尺寸不比街墙小，会互相穿模）
    return filterNoOverlap(
      candidates.filter((s) => slotBuildable(def, s, KEEP_OUT_MARGIN)),
      MIN_GAP,
    );
  }

  // 街墙：4 边 × SLOTS_PER_SIDE 槽，确定性去尾抽 count 个（保留槽位顺序环绕）
  // faceX = 槽位沿 x 排（南北边楼，临街面 ±Z）；否则沿 z 排（东西边楼，临街面 ±X）
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
      factor: sampleFactor(rnd),
      // 批次 39 C3：临街面 = 该楼所在那一边（z=-EDGE_CENTER ⇒ −Z 面朝外环，+EDGE_CENTER ⇒ +Z）
      streetAxis: slot.faceX ? 'z' : 'x',
      streetSign: slot.faceX ? (slot.z > 0 ? 1 : -1) : (slot.x > 0 ? 1 : -1),
      idx: pi,
    });
  }

  // 批次 38 R2：路廊/水域/一环 keep-out（丢弃不递补）
  // 批次 39 C1：再判楼-楼净距（同样丢弃不递补）；两级判定口径一致
  const keepOut = (margin: number) => candidates.filter((s) => slotBuildable(def, s, margin));
  let out = filterNoOverlap(keepOut(KEEP_OUT_MARGIN), MIN_GAP);
  if (out.length < MIN_BUILDINGS) {
    // 兜底：同时放宽 keep-out 余量与楼-楼净距重算一轮（只放宽判定，不重掷 rnd
    // ⇒ 布局确定性与 idx 稳定性不变）
    const relaxed = filterNoOverlap(keepOut(KEEP_OUT_MARGIN_RELAXED), MIN_GAP_RELAXED);
    if (relaxed.length > out.length) out = relaxed;
    if (out.length < MIN_BUILDINGS) {
      // §5.3 禁止静默降级：keep-out 误伤导致楼数骤降必须 dev 告警
      console.warn(
        `[cityObstacles] buildingsFor(${def.id}) keep-out + 楼-楼净距判定后仅 ${out.length} 栋 < ${MIN_BUILDINGS}：` +
        `候选 ${candidates.length} 栋中多数落入路廊/水域/一环带域或与相邻楼净距不足` +
        `（margin ${KEEP_OUT_MARGIN}→${KEEP_OUT_MARGIN_RELAXED}、gap ${MIN_GAP}→${MIN_GAP_RELAXED} 仍不足），` +
        '保留当前结果不递补。',
      );
    }
  }
  return out;
}
