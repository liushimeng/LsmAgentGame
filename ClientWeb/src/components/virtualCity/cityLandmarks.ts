/**
 * cityLandmarks — 地面场馆保留地（批次 47 体育场 · **leaf 模块，零 import**）。
 *
 * ## 为什么单独成文件
 * 体育场与码头是**大场馆**，而道路网（`roadNetwork.ts`）与布点（`cityObstacles.ts`）都
 * 以**城区中心** `districtCenter` 为锚点。场馆放在城区中心会让道路端点落在场馆正中
 * （批次 47 旧 `conn-fin_sub_center~sports_new_city` 直穿跑道，CDP 顶视截图实锤），
 * 且 `cityObstacles.isBuildable` 不含保留地 ⇒ 区内楼体压在看台上。
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
 * ## 尺寸口径（批次 61 方格网）
 * 保留地必须**整块落在街区单元内部**（8×8 u，中心在方格点上），否则会切断街道线。
 * 体育场 70×47 m（中心 (12,36)）、码头陆侧 8×8 u（中心 (-36,-2)）——
 * 与 `civic/SportsField.tsx` / `civic/PortTerminal.tsx` 的 position + scale 同源，
 * 两处须同改：保留地小于 GLB 会出现「围栏被楼压住 / 路从看台里穿过」，
 * 大于则多留一圈空地。
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

/**
 * 体育场用地：**中心 (24, 36)、110 × 74 m**（批次 61）。
 *
 * 批次 61：体育场迁到**空街区单元 (24, 36)**（体育新城 (12,36) 保留街墙楼群，
 * 体育场不与街区建筑打架）。GLB 自然尺寸 11 × 7.4 u ⇒ 用地取
 * x∈[18.5,29.5]、z∈[32.3,39.7]；与相邻街道（x=18 主干道 / x=30 次干道）
 * 各有 0.2 u（2 m）的边角叠压 —— 读作「体育场外广场直接临街」，真实且可接受。
 */
export const SPORTS_FIELD_AREA: LandmarkField = {
  minX: 18.5,
  maxX: 29.5,
  minZ: 32.3,
  maxZ: 39.7,
  label: 'civic.sports-field',
};

/**
 * 港口码头**陆侧用地**（轴对齐矩形；只圈陆域，港池本身已由
 * `cityObstacles.PORT_POOL_WATER` 作为水域保留）。
 *
 * 批次 61：物流港落在方格网街区 **(-36, 0)**（紧邻西侧港池水域）⇒
 * 码头 GLB 绕 Y 转 90°（岸壁朝西正对港池），世界足迹
 * X = 6.4 u（局部 Z 旋转而来）× Z = 8.16 u（局部 X 旋转而来），
 * 中心 (-36, -1.2) ⇒ 陆侧用地 x∈[-39.2,-32.8]、z∈[-5.28,2.88]。
 * 与 `cityObstacles.PORT_POOL_WATER`（x∈[-50,-43]）不重叠；
 * 岸桥悬臂伸到 x∈[-44,-41.7] ⇒ 伸进港池水域（由水域保留地覆盖）。
 */
export const PORT_TERMINAL_AREA: LandmarkField = {
  minX: -39.2,
  maxX: -32.8,
  minZ: -5.28,
  maxZ: 2.88,
  label: 'civic.port-terminal',
};

/** 全部地面场馆保留地。 */
export const LANDMARK_FIELDS: readonly LandmarkField[] = [SPORTS_FIELD_AREA, PORT_TERMINAL_AREA];

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
