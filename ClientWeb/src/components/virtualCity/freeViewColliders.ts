/**
 * 批次 32「自由视角系统」· 游戏侧薄适配层 —— 城市相机碰撞体装配。
 *
 * 引擎层（`@/engine3d`）只认识通用的 `CameraBox`（AABB）与边界盒，不知道
 * 「什么是楼」；本文件负责把**虚拟城市的静态建筑数据**翻译成碰撞盒，
 * 是 §2.1 硬约束 5 的边界所在：游戏私有数据只在这里出现，引擎层保持零游戏依赖。
 *
 * 数据来源（全部确定性、模块级可复现，与 `DistrictBlock` 渲染**同源**）：
 *   - `buildingsFor(def)`  —— 布局：区相对偏移 `{x,z,w,d,factor}`，FNV-1a + mulberry32
 *     确定性伪随机 ⇒ 每次调用结果逐字节一致，碰撞盒与渲染楼体严丝合缝；
 *   - `cityScale.buildingTopY(id, prosperity, factor)` —— 楼高：与 `DistrictBuildings`
 *     渲染公式共用同一函数（§130 数据侧防漂移）。
 *
 * 楼高依赖 `price_index`（WS 推送变化）⇒ 碰撞体按 `useMemo([marketById])` 重装配。
 * 装配成本 ≈ 340 次对象字面量，可忽略；解算侧有平方距离早退，每帧实际参与计算的 < 10 个。
 *
 * **不纳入碰撞的对象**（有意为之）：道路 / 绿化 / 车辆 / 行人 / 四缘环境带。
 *   - 道路是平铺 plane，无立体空间，统一由 `groundY` 地面钳制处理；
 *   - 车辆行人是贴图级小物件，「穿行它们」的观感优于「贴地平移时被路灯挡住」；
 *   - 四缘雪山/沙漠/森林靠 `FREE_VIEW_BOUNDS` 边界盒限制，边界盒已覆盖其范围。
 */

import type { CameraBox } from '@/engine3d';
import { buildingsFor } from './building_layout';
import { buildingTopY, prosperityOf, DISTRICT_SURFACE_Y } from './cityScale';
import {
  VIRTUAL_CITY_DISTRICTS,
  type VirtualCityDistrictId,
} from '@/types/virtualCity';

/** 城区底板边长（世界单位）。`DistrictBlock` 的 plate 与 curb 均按 8×8 铺。 */
export const DISTRICT_PLATE_SIZE = 8;

/**
 * 相机活动边界（世界单位）—— 覆盖「建成区 ±60 + 四缘环境带 ±80 + 南海洋 z→150」。
 * 批次 22 的漫游边界是 ±60（只看建成区），自由视角放开到四缘才算「自由」。
 */
export const FREE_VIEW_BOUNDS = {
  // 批次 36 §3：y 下限 0.25 → −20 —— 相机贴地保护由 FREE_VIEW_GROUND_Y（0.25）独立承担
  // （resolveCameraCollision 先地面钳制后边界钳制），bounds 的 y 下限实际只钳 orbit
  // 聚焦点。聚焦点是抽象点（不可见），允许潜到地面下，俯瞰态按 W 才能沿视线俯冲
  // （旧值 0.25/0 会把聚焦点顶在地面，W 退化成水平滑动）。相机本体永不低于地面。
  min: [-88, -20, -88] as [number, number, number],
  max: [88, 140, 155] as [number, number, number],
};

/** 地面/水面安全高度：城区地面 0.02 + 2.3 m 余量，相机不钻到路面以下。 */
export const FREE_VIEW_GROUND_Y = DISTRICT_SURFACE_Y + 0.23;

/** 碰撞球半径：3 m —— 建筑退让半米 + 相机近裁面缓冲，够用且不会在窄巷里卡死。 */
export const FREE_VIEW_COLLISION_RADIUS = 0.3;

/**
 * 装配全城建筑碰撞盒。
 *
 * @param priceByDistrict 城区 id → 当前 `price_index`（来自 WS 的 `market.districts`）；
 *                        缺项按 1.0（繁荣度 0.25）处理，与渲染的 `?? 1` 缺省一致。
 * @returns 楼体 AABB 列表（底面贴地，顶面 = 当前 prosperity 下的真实楼顶）
 */
export function buildCityCameraColliders(
  priceByDistrict: ReadonlyMap<string, number>,
): CameraBox[] {
  const out: CameraBox[] = [];
  for (const def of VIRTUAL_CITY_DISTRICTS) {
    const prosperity = prosperityOf(priceByDistrict.get(def.id) ?? 1);
    for (const spec of buildingsFor(def)) {
      const w = spec.w;
      const d = spec.d;
      out.push({
        minX: def.x + spec.x - w / 2,
        maxX: def.x + spec.x + w / 2,
        minZ: def.z + spec.z - d / 2,
        maxZ: def.z + spec.z + d / 2,
        minY: DISTRICT_SURFACE_Y,
        maxY: DISTRICT_SURFACE_Y + buildingTopY(def.id, prosperity, spec.factor),
      });
    }
  }
  return out;
}

/** 城区底板 AABB（含 curb 环），用于小地图/调试的城区高亮取用。 */
export function districtPlateBox(def: VirtualCityDistrictId): CameraBox | null {
  const d = VIRTUAL_CITY_DISTRICTS.find((x) => x.id === def);
  if (!d) return null;
  const h = DISTRICT_PLATE_SIZE / 2;
  return {
    minX: d.x - h, maxX: d.x + h,
    minY: DISTRICT_SURFACE_Y, maxY: DISTRICT_SURFACE_Y,
    minZ: d.z - h, maxZ: d.z + h,
  };
}

/**
 * 批次 32 v2：选中物体的聚焦点检索。
 *
 * 从命中点反查「这是哪一栋建筑」：遍历 colliders，找 XZ 投影包含命中点、且
 * 命中点高度 ≤ 楼顶的那栋楼 ⇒ 取其 XZ 中心 + 半径（半宽/半深的较大者 + 碰撞余量）。
 *
 * 未命中（点中了树/路灯/水面等非建筑物体，或点在楼顶上方）⇒ 退化：
 *   focus = 命中点本身 + fallbackRadius（调用方按物体类别给默认值）。
 *
 * 这样「选中特效的环」和「楼体的实际投影」严丝合缝（同一份 colliders 数据）。
 */
export function selectedTargetFor(
  colliders: readonly CameraBox[],
  pos: [number, number, number],
  fallbackRadius: number,
): { x: number; y: number; z: number; radius: number } {
  const [px, py, pz] = pos;
  for (const b of colliders) {
    if (px >= b.minX && px <= b.maxX && pz >= b.minZ && pz <= b.maxZ && py <= b.maxY) {
      const cx = (b.minX + b.maxX) / 2;
      const cz = (b.minZ + b.maxZ) / 2;
      const cy = (b.minY + b.maxY) / 2;
      const r = Math.max(b.maxX - b.minX, b.maxZ - b.minZ) / 2 + FREE_VIEW_COLLISION_RADIUS;
      return { x: cx, y: cy, z: cz, radius: r };
    }
  }
  return { x: px, y: py, z: pz, radius: fallbackRadius };
}
