/**
 * 批次 69 · 虚拟城市「实时看板」一级 Tab 静态目录（前端权威来源）。
 *
 * 设计要点（任务书「后端契约」节）：
 *   - **前端渲染 Tab 的权威来源是本目录** —— 首屏不依赖网络，后端不可达也能出全量 Tab；
 *   - 后端 GET /api/virtual_city/dashboard 的 `dimensions[]` 只做**校验/补充**：
 *     按 id 匹配覆盖 title_key / group；后端返回的**未知 id 一律忽略**，
 *     静态目录项始终保留（防止后端目录缺项导致 Tab 消失）。
 *
 * 分组（顺序固定，3 组 11 个维度）：
 *   城市运行 city    → city-overview / finance / ledger / economy / behavior / macro
 *   市场金融 market  → market / market-trade / insurance
 *   外部实时 external → realtime / datasources
 */

import type { VirtualCityPanelTab } from '@/store/virtualCity.store';

/** 看板分组 id。 */
export type DashboardGroupId = 'city' | 'market' | 'external';

/** 单个维度（= 一级 Tab）的静态元数据。 */
export interface DashboardDimension {
  /** 维度 id —— 与 store.VirtualCityPanelTab 成员一一对应。 */
  id: VirtualCityPanelTab;
  /** 所属分组。 */
  group: DashboardGroupId;
  /** 维度标题的 i18n 键。 */
  titleKey: string;
  /** 数据来源（game_state / realtime_feed / catalog）。 */
  source: string;
  /** 作用域（room = 随房间状态变化 / global = 全局数据）。 */
  scope: string;
  /** 单位（mixed / text）。 */
  unit: string;
  /** 刷新方式（push = WS 推送 / pull = 手动刷新 / static = 静态目录）。 */
  refresh: string;
}

/** 看板分组（渲染顺序 = 本数组顺序）。 */
export const DASHBOARD_GROUPS: { id: DashboardGroupId; labelKey: string }[] = [
  { id: 'city', labelKey: 'virtualCity.dash.group.city' },
  { id: 'market', labelKey: 'virtualCity.dash.group.market' },
  { id: 'external', labelKey: 'virtualCity.dash.group.external' },
];

/** 已知分组 id 集合（校验后端目录的 group 字段 —— 外部输入必须收敛）。 */
export const DASHBOARD_GROUP_IDS: DashboardGroupId[] = DASHBOARD_GROUPS.map((g) => g.id);

/** 11 个维度的静态目录（权威）。 */
export const DASHBOARD_CATALOG: DashboardDimension[] = [
  // ── 城市运行 ──
  {
    id: 'city-overview',
    group: 'city',
    titleKey: 'virtualCity.dash.dim.cityOverview',
    source: 'game_state',
    scope: 'room',
    unit: 'mixed',
    refresh: 'push',
  },
  {
    id: 'finance',
    group: 'city',
    titleKey: 'virtualCity.dash.dim.finance',
    source: 'game_state',
    scope: 'self',
    unit: 'cny',
    refresh: 'push',
  },
  {
    id: 'ledger',
    group: 'city',
    titleKey: 'virtualCity.dash.dim.ledger',
    source: 'game_state',
    scope: 'self',
    unit: 'cny',
    refresh: 'push',
  },
  {
    id: 'economy',
    group: 'city',
    titleKey: 'virtualCity.dash.dim.economy',
    source: 'game_state',
    scope: 'room',
    unit: 'mixed',
    refresh: 'push',
  },
  {
    id: 'behavior',
    group: 'city',
    titleKey: 'virtualCity.dash.dim.behavior',
    source: 'game_state',
    scope: 'self',
    unit: 'mixed',
    refresh: 'push',
  },
  {
    id: 'macro',
    group: 'city',
    titleKey: 'virtualCity.dash.dim.macro',
    source: 'game_state',
    scope: 'room',
    unit: 'mixed',
    refresh: 'push',
  },
  // ── 市场金融 ──
  {
    id: 'market',
    group: 'market',
    titleKey: 'virtualCity.dash.dim.market',
    source: 'game_state',
    scope: 'room',
    unit: 'index',
    refresh: 'push',
  },
  {
    id: 'market-trade',
    group: 'market',
    titleKey: 'virtualCity.dash.dim.marketTrade',
    source: 'game_state',
    scope: 'room',
    unit: 'cny',
    refresh: 'push',
  },
  {
    id: 'insurance',
    group: 'market',
    titleKey: 'virtualCity.dash.dim.insurance',
    source: 'game_state',
    scope: 'self',
    unit: 'cny',
    refresh: 'push',
  },
  // ── 外部实时 ──
  {
    id: 'realtime',
    group: 'external',
    titleKey: 'virtualCity.dash.dim.realtime',
    source: 'citychrome',
    scope: 'global',
    unit: 'mixed',
    refresh: 'realtime',
  },
  {
    id: 'datasources',
    group: 'external',
    titleKey: 'virtualCity.dash.dim.dataSources',
    source: 'static',
    scope: 'global',
    unit: 'text',
    refresh: 'static',
  },
];

/** 分组 id → 该组维度（保持目录内顺序）。 */
export function dimensionsByGroup(
  group: DashboardGroupId,
  catalog: DashboardDimension[] = DASHBOARD_CATALOG,
): DashboardDimension[] {
  return catalog.filter((d) => d.group === group);
}
