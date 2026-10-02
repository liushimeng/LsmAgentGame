/**
 * cityLandmarks — 地面场馆保留地（批次 47 体育场 · **leaf 模块，零 import**）。
 *
 * ## 为什么单独成文件
 * 体育场占地 110×74 m，而道路网（`roadNetwork.ts`）与布点（`cityObstacles.ts`）都
 * 以**城区中心** `districtCenter` 为锚点。把体育场放到「体育新城」(10, 40) 的正中心
 * 之后，两侧都炸了：
 *   - `roadNetwork` 的 `conn-fin_sub_center~sports_new_city` 与 `edge-sports_new_city`
 *     **两条路的端点都落在体育场中心**，直接横穿跑道与内场（CDP 顶视截图实锤）；
 *   - `cityObstacles.isBuildable` 不含保留地 → 区内楼体压在看台上。
 *
 * 保留地必须被**两侧同时**消费，而 `cityObstacles` 本身 import `roadNetwork`
 * （复用 `pointSegDist`），反向 import 会成环。故把纯数据 + 纯几何裁剪函数抽到这里：
 *
 * ```
 *   cityLandmarks（leaf，零 import）
 *        ▲                    ▲
 *        │                    │
 * roadNetwork.ts        cityObstacles.ts
 *   （道路在保留地边界截断）   （isBuildable 让开）
 * ```
 *
 * ## 尺寸口径
 * 与 `cityScale.REAL_DIMS_M.sportsField` 同源（110×74 m），两处须同改：
 * 保留地小于 GLB 会出现「围栏被楼压住 / 路从看台里穿过」，
 * 大于则多留一圈空地。中心坐标与 `civic/SportsField.tsx` 的 `position` 同源。
 */

/** 轴对齐保留矩形（世界 xz 平面）。 */
export interface LandmarkField {
  minX: number;
  maxX: number;
  minZ: number;
  maxZ: number;
  /** 仅用于日志与调试可读性。 */
  label: string;
}

/** 体育场用地：中心 (10, 38.5)、110 × 74 m。 */
export const SPORTS_FIELD_AREA: LandmarkField = {
  minX: 10 - 55.0 / 10,
  maxX: 10 + 55.0 / 10,
  minZ: 38.5 - 37.0 / 10,
  maxZ: 38.5 + 37.0 / 10,
  label: 'civic.sports-field',
};

/** 全部地面场馆保留地。 */
export const LANDMARK_FIELDS: readonly LandmarkField[] = [SPORTS_FIELD_AREA];

/** 线段与轴对齐矩形是否相交（Liang-Barsky 精确求交，不用采样）。 */
function segRect(
  ax: number, az: number, bx: number, bz: number,
  r: LandmarkField,
): [number, number] | null {
  const dx = bx - ax, dz = bz - az;
  let t0 = 0, t1 = 1;
  const p = [-dx, dx, -dz, dz];
  const q = [ax - r.minX, r.maxX - ax, az - r.minZ, r.maxZ - az];
  for (let i = 0; i < 4; i++) {
    if (p[i] === 0) {
      if (q[i] < 0) return null;
      continue;
    }
    const ratio = q[i] / p[i];
    if (p[i] < 0) {
      if (ratio > t1) return null;
      if (ratio > t0) t0 = ratio;
    } else {
      if (ratio < t0) return null;
      if (ratio < t1) t1 = ratio;
    }
  }
  return [t0, t1];
}

/** 线段是否穿过任一保留地。 */
export function segHitsLandmark(
  from: [number, number],
  to: [number, number],
  fields: readonly LandmarkField[] = LANDMARK_FIELDS,
): boolean {
  return fields.some((r) => segRect(from[0], from[1], to[0], to[1], r) !== null);
}

/**
 * 把线段在保留地边界处**截断**（返回 null 表示整段都在保留地里，应丢弃）。
 *
 * 为什么是「截断」而不是「整段拒绝」：
 *   - 拒绝会让「金融副中心 ↔ 体育新城」彻底断开，体育新城只剩一条高速联络线；
 *   - 截断后道路止于体育场围栏外侧，语义等价于**「路在场馆前庭 terminates」** ——
 *     这正是真实城市里大型场馆的做法（体育馆/机场路端于前庭广场）。
 *
 * 只处理**一段**保留地；多段重叠时逐段应用（当前全城只有一段，够了；真要扩到多段
 * 需改成「差集区间」实现，届时本函数的签名不变）。
 */
export function trimSegmentToLandmarks(
  from: [number, number],
  to: [number, number],
  fields: readonly LandmarkField[] = LANDMARK_FIELDS,
): { from: [number, number]; to: [number, number] } | null {
  let a: [number, number] = [from[0], from[1]];
  let b: [number, number] = [to[0], to[1]];
  for (const r of fields) {
    const hit = segRect(a[0], a[1], b[0], b[1], r);
    if (hit === null) continue;
    const [t0, t1] = hit;
    if (t0 <= 1e-6 && t1 >= 1 - 1e-6) return null;    // 整段在保留地里 → 丢弃
    // 入口在段内：从 t0 处起算新的 b；否则从 t1 处截断新的 a
    if (t0 > 1e-6) {
      b = [a[0] + (b[0] - a[0]) * t0, a[1] + (b[1] - a[1]) * t0];
    } else {
      a = [a[0] + (b[0] - a[0]) * t1, a[1] + (b[1] - a[1]) * t1];
    }
  }
  return { from: a, to: b };
}
