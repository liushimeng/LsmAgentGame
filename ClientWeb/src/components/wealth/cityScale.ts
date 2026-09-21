/**
 * cityScale — 虚拟城市 3D 高度系统唯一事实来源（2026-09-21）。
 *
 * 世界标尺：**1 世界单位 = 10 米**。地图 40×40 = 400m×400m 城区（小型城市尺度）。
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

/** 各城区楼层区间 [minF, maxF]（真实城市形态：CBD 摩天 / 郊区别墅）。 */
export const DISTRICT_FLOORS: Record<WealthDistrictId, [number, number]> = {
  finance:     [8, 20],   // 金融CBD   24~60 m
  tech:        [6, 15],   // 科技园    18~45 m
  industry:    [2, 6],    // 工业区     6~18 m（厂房高顶棚）
  oldtown:     [2, 7],    // 老城区     6~21 m（多层老公房）
  commerce:    [4, 12],   // 商业中心  12~36 m
  residential: [6, 18],   // 居住区    18~54 m（高层住宅）
  suburb:      [1, 3],    // 郊区       3~9 m（别墅/排屋）
  riverside:   [8, 20],   // 滨河新区  24~60 m
};

/** 楼层数 → 世界单位楼高。 */
export const buildingHeight = (floors: number): number => u(FLOOR_HEIGHT_M) * floors;
