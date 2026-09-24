/**
 * cityScale — 虚拟城市 3D 高度系统唯一事实来源（2026-09-21）。
 *
 * 世界标尺：**1 世界单位 = 10 米**。地图 120×120 = 1200m×1200m 城区
 * （v2.12 阶段 2：40×40 → 80×80，16 城区；批次 20：80×80 → 120×120，32 城区）。
 * 所有 3D 建筑 / 道具的「真实米制尺寸 → 世界单位」换算统一经 u()，
 * 禁止在组件内硬编码米制尺寸（方案：tmpPlan/虚拟城市-3D高度系统与贴图渲染优化方案-20260921.md）。
 */

import type { VirtualCityDistrictId } from '@/types/virtualCity';

/** 世界标尺：1 单位 = 10 米。 */
export const METERS_PER_UNIT = 10;

/** 米 → 世界单位。 */
export const u = (meters: number): number => meters / METERS_PER_UNIT;

/** 层高：3 m/层（0.3 世界单位）。 */
export const FLOOR_HEIGHT_M = 3;

/** 各城区楼层区间 [minF, maxF]（真实城市形态：CBD 摩天 / 郊区别墅；
 *  v2.12 阶段 2 追加 8 个新城区的楼层区间，前 8 区不可修改）。 */
export const DISTRICT_FLOORS: Record<VirtualCityDistrictId, [number, number]> = {
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
  // ── 批次 20 城市扩张新增 16 区（楼层区间 = 文档 1 §2.3 表）──
  fin_sub_center:    [10, 24], // 金融副中心 30~72 m（CBD 外溢塔楼群）
  software_park:     [5, 14],  // 软件园    15~42 m（板楼办公）
  airport_town:      [3, 10],  // 空港小镇   9~30 m（临空商业）
  air_logistics:     [2, 6],   // 航空物流园 6~18 m（仓库厂房）
  auto_city:         [2, 7],   // 汽车城     6~21 m（整车制造厂房）
  mountain_resort:   [1, 3],   // 山居民宿区 3~9 m（低层度假）
  chem_park:         [2, 6],   // 化工园区   6~18 m（重工业）
  agri_park:         [1, 4],   // 现代农业园 3~12 m（大棚/农舍）
  health_town:       [2, 6],   // 康养小镇   6~18 m（疗养低层）
  steel_town:        [2, 6],   // 特钢镇     6~18 m（厂区宿舍）
  old_city_culture:  [1, 4],   // 古城文化区 3~12 m（坡顶院落）
  university_town:   [4, 12],  // 大学城    12~36 m（校园板楼）
  wetland_park:      [1, 3],   // 湿地公园   3~9 m（pavilion 景观）
  sports_new_city:   [3, 14],  // 体育新城   9~42 m（场馆商业）
  bay_new_town:      [8, 20],  // 湾区新城  24~60 m（高层滨水）
  highspeed_rail_town: [3, 12],// 高铁新城   9~36 m（TOD 综合体）
};

/** 楼层数 → 世界单位楼高。 */
export const buildingHeight = (floors: number): number => u(FLOOR_HEIGHT_M) * floors;
