/**
 * cityScale — 虚拟城市 3D 高度系统唯一事实来源（2026-09-21）。
 *
 * 世界标尺：**1 世界单位 = 10 米**。地图 80×80 = 800m×800m 城区
 * （v2.12 阶段 2：40×40 → 80×80，16 城区）。
 * 所有 3D 建筑 / 道具的「真实米制尺寸 → 世界单位」换算统一经 u()，
 * 禁止在组件内硬编码米制尺寸（方案：tmpPlan/虚拟城市-3D高度系统与贴图渲染优化方案-20260921.md）。
 */

import type { WealthDistrictId } from '@/types/wealth';

/** 世界标尺：1 单位 = 10 米。 */
export const METERS_PER_UNIT = 10;

/** 米 → 世界单位。 */
export const u = (meters: number): number => meters / METERS_PER_UNIT;

/** 层高：3 m/层（0.3 世界单位）。 */
export const FLOOR_HEIGHT_M = 3;

/** 各城区楼层区间 [minF, maxF]（真实城市形态：CBD 摩天 / 郊区别墅；
 *  v2.12 阶段 2 追加 8 个新城区的楼层区间，前 8 区不可修改）。 */
export const DISTRICT_FLOORS: Record<WealthDistrictId, [number, number]> = {
  finance:     [8, 20],   // 金融CBD   24~60 m
  tech:        [6, 15],   // 科技园    18~45 m
  industry:    [2, 6],    // 工业区     6~18 m（厂房高顶棚）
  oldtown:     [2, 7],    // 老城区     6~21 m（多层老公房）
  commerce:    [4, 12],   // 商业中心  12~36 m
  residential: [6, 18],   // 居住区    18~54 m（高层住宅）
  suburb:      [1, 3],    // 郊区       3~9 m（别墅/排屋）
  riverside:   [8, 20],   // 滨河新区  24~60 m
  // ── v2.12 阶段 2 扩展城区（80×80 地图外圈）──
  logistics_port:    [3, 8],   // 物流港     9~24 m（低层仓库/厂房）
  hightech_park:     [6, 15],  // 高新园区  18~45 m（科技办公楼）
  edu_district:      [4, 10],  // 教育园区  12~30 m（教学楼/宿舍）
  medical_city:      [8, 18],  // 医疗城    24~54 m（医院大楼）
  industrial_park:   [2, 5],   // 产业基地   6~15 m（厂房）
  central_park:      [1, 3],   // 中央公园   3~9 m（低层景观建筑）
  transport_hub:     [3, 12],  // 交通枢纽   9~36 m（车站/商业）
  cultural_creative: [3, 8],   // 文创区     9~24 m（创意园区）
};

/** 楼层数 → 世界单位楼高。 */
export const buildingHeight = (floors: number): number => u(FLOOR_HEIGHT_M) * floors;
