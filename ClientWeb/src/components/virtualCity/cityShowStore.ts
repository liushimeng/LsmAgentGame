/**
 * cityShowStore —— 城市显示指令的跨层共享状态（zustand 单例）。
 *
 * 批次 81「系统对话」：City-System Agent 产出结构化显示指令（方案 §3.3），
 * 经 `cityShowAdapter` 转成既有 3D 控制动作。其中 `annotate` 需要一条
 * **DOM 浮动标签**（3D 场景内无对应既有组件），故用单例 store 让
 * Canvas 内外的消费者都能读到 —— 与 `engine3d/controls/freeViewStore.ts`
 * 同一模式（跨 R3F Canvas 边界的最小代价解法）。
 *
 * 批次 84「全维度事件体系与 Agent 双向对话」扩到 14 个显示原语，本文件随之
 * 承载三类新增的**跨层显示状态**：
 *   1. `highlight` —— 高亮城区 + **颜色**（批次 84 起 `highlight_district`
 *      的 `color` 字段真正生效；此前该字段被忽略）；
 *   2. `layers` —— 7 个图层的显隐开关（buildings / trees / vehicles /
 *      pedestrians / props / labels / water），由 3D 场景组件订阅；
 *   3. `filter` —— 类别筛选（按图层隔离 / 隐藏）。
 * 另加 `notice` —— **未支持 / 未接引擎**的显示指令提示（§11 风险缓解：
 * 「点了没反应」必须显式提示，禁止静默吞掉）。
 *
 * 昼夜 / 天气 / 时间流速的覆盖**不在本 store**：它们的真实渲染落点是
 * `cityTimeStore`（采样器直读），见 `cityEnvApi`。
 *
 * 写入频率：只有 Agent 显示指令到达时写（低频），无每帧数据，无定时器。
 * 8s 自动消除由渲染端（`<CityAnnotationLabel>`）按 ts 判定，store 不挂定时器
 * （与 virtualCity.store 的 speechBubbles 同口径）。
 */

import { create } from 'zustand';
import type { CityShowNotice } from './cityShowAdapter';

// ── 批次 84：显示能力的**命令式句柄**统一出口（非 React，adapter 直调）──
// 与既有 `cityShowApi` 同款模式：具体实现分散在各自的薄适配层
// （昼夜/天气/流速 → cityTimeStore；实体 → cityEntityResolver；
// 飞行 → cityPathFlight；相机预设 → cityCameraPreset），本文件只做**聚合出口**，
// 让 `cityShowAdapter` 的 deps 有一处统一的注入点。
export { cityEnvApi } from './cityTimeStore';
export { cityEntityApi } from './cityEntityResolver';
export { cityPathApi } from './cityPathFlight';
export { cityCameraApi } from './cityCameraPreset';

export interface CityAnnotation {
  text: string;
  district?: string;
  /** Date.now()，过期判定基准。 */
  ts: number;
}

/**
 * 批次 84：可切换显隐的图层名（与后端 DisplayCommand 的 `layer` 字段逐字一致）。
 *
 * 落点（游戏侧薄适配层）：
 *   buildings   → DistrictBlock 内的 <DistrictBuildings>
 *   trees       → StreetPropsLayer 内的 <TreesInstanced>
 *   vehicles    → StreetPropsLayer 内的车辆列表
 *   pedestrians → StreetPropsLayer 内的行人列表（装饰行人 + 真实居民）
 *   props       → 街具 / 标识 / 屋顶设备 / 公交站台 / 停车咪表
 *   labels      → 城区悬浮信息卡（<Html>）
 *   water       → VirtualCityCityMap 的水系 group
 */
export type CityLayerName =
  | 'buildings'
  | 'trees'
  | 'vehicles'
  | 'pedestrians'
  | 'props'
  | 'labels'
  | 'water';

/** 全部图层名（顺序即 UI 展示顺序）。 */
export const CITY_LAYER_NAMES: readonly CityLayerName[] = [
  'buildings',
  'trees',
  'vehicles',
  'pedestrians',
  'props',
  'labels',
  'water',
];

/** 图层默认全可见。 */
function defaultLayers(): Record<CityLayerName, boolean> {
  const out = {} as Record<CityLayerName, boolean>;
  for (const n of CITY_LAYER_NAMES) out[n] = true;
  return out;
}

/** 类别筛选模式（后端 DisplayCommand 的 `mode` 字段）。 */
export type CityFilterMode = 'highlight' | 'dim';

/** 类别筛选（`category` 为空 = 取消筛选）。 */
export interface CityFilter {
  category: string;
  mode: CityFilterMode;
}

/**
 * 显示指令提示（批次 84）。
 * - `unsupported`：后端下发了前端不认识的显示指令类型；
 * - `unwired`：指令合法，但本批**未接引擎**（能力缺失，显式告知用户）。
 *
 * 提示的**结构**由 adapter 定义（`CityShowNotice`：code / type / detail），
 * 本 store 只负责保存与广播（UI 侧按 code 取 i18n 文案渲染）。
 */
export type { CityShowNotice, CityShowNoticeCode } from './cityShowAdapter';

interface CityShowState {
  annotation: CityAnnotation | null;
  /** 批次 84：高亮城区 + 颜色（null = 无高亮）。 */
  highlight: { district: string; color: string } | null;
  /** 批次 84：图层显隐。 */
  layers: Record<CityLayerName, boolean>;
  /** 批次 84：类别筛选（null = 无筛选）。 */
  filter: CityFilter | null;
  /** 批次 84：最近的显示指令提示（null = 无）。 */
  notice: CityShowNotice | null;

  setAnnotation: (text: string, district?: string) => void;
  clearAnnotation: () => void;
  /** 设置高亮（颜色缺省 = 主题默认金）。 */
  setHighlight: (district: string, color?: string) => void;
  clearHighlight: () => void;
  /** 设置单个图层显隐。 */
  setLayerVisibility: (layer: CityLayerName, visible: boolean) => void;
  /** 全部图层恢复可见。 */
  resetLayers: () => void;
  /** 设置 / 取消类别筛选（category 空串 = 取消）。 */
  setFilter: (category: string, mode?: CityFilterMode) => void;
  clearFilter: () => void;
  /** 记录一条显示指令提示。 */
  setNotice: (notice: CityShowNotice | null) => void;
}

export const useCityShowStore = create<CityShowState>((set) => ({
  annotation: null,
  highlight: null,
  layers: defaultLayers(),
  filter: null,
  notice: null,

  setAnnotation: (text, district) =>
    set({
      annotation: {
        text,
        ...(district ? { district } : {}),
        ts: Date.now(),
      },
    }),
  clearAnnotation: () => set({ annotation: null }),

  setHighlight: (district, color) =>
    set({ highlight: { district, color: color && color.trim() ? color.trim() : '#d4a017' } }),
  clearHighlight: () => set({ highlight: null }),

  setLayerVisibility: (layer, visible) =>
    set((s) => ({ layers: { ...s.layers, [layer]: visible } })),
  resetLayers: () => set({ layers: defaultLayers() }),

  setFilter: (category, mode = 'highlight') =>
    set({ filter: category ? { category, mode } : null }),
  clearFilter: () => set({ filter: null }),

  setNotice: (notice) => set({ notice }),
}));

/**
 * 合法高亮色（仅接受 #rgb / #rrggbb —— 拒绝其它写法，避免把任意字符串
 * 塞进材质 color 造成渲染异常或注入）。
 */
export function sanitizeHighlightColor(raw: unknown): string | null {
  if (typeof raw !== 'string') return null;
  const v = raw.trim();
  if (/^#[0-9a-fA-F]{3}$/.test(v)) return v;
  if (/^#[0-9a-fA-F]{6}$/.test(v)) return v;
  return null;
}

/** 供非 React 代码（adapter）直接调用，避免为一次指令触发整树订阅。 */
export const cityShowApi = {
  setAnnotation: (text: string, district?: string) =>
    useCityShowStore.getState().setAnnotation(text, district),
  clearAnnotation: () => useCityShowStore.getState().clearAnnotation(),
  /** 批次 84：高亮（含颜色）。 */
  setHighlight: (district: string, color?: string) =>
    useCityShowStore.getState().setHighlight(district, color),
  clearHighlight: () => useCityShowStore.getState().clearHighlight(),
  /** 批次 84：图层显隐。 */
  setLayerVisibility: (layer: CityLayerName, visible: boolean) =>
    useCityShowStore.getState().setLayerVisibility(layer, visible),
  resetLayers: () => useCityShowStore.getState().resetLayers(),
  /** 批次 84：类别筛选。 */
  setFilter: (category: string, mode: CityFilterMode = 'highlight') =>
    useCityShowStore.getState().setFilter(category, mode),
  clearFilter: () => useCityShowStore.getState().clearFilter(),
  /** 批次 84：显示指令提示（未支持 / 未接引擎）。 */
  setNotice: (notice: CityShowNotice | null) =>
    useCityShowStore.getState().setNotice(notice),
};

// ── 批次 84：类别筛选 → 图层映射 ─────────────────────────────────

/**
 * 类别 → 图层集合。
 *
 * 后端 `set_filter` 只给一个 `category` 字符串（自由文本），前端按**已知同义
 * 词**映射到图层集合；未识别的类别 ⇒ 空集合 ⇒ 记「未支持」提示（不静默）。
 */
export const CATEGORY_LAYER_MAP: Record<string, CityLayerName[]> = {
  building: ['buildings'],
  buildings: ['buildings'],
  tree: ['trees'],
  trees: ['trees'],
  vehicle: ['vehicles'],
  vehicles: ['vehicles'],
  pedestrian: ['pedestrians'],
  pedestrians: ['pedestrians'],
  'street-prop': ['props'],
  props: ['props'],
  label: ['labels'],
  labels: ['labels'],
  water: ['water'],
  district: ['labels'],
  civic: ['props'],
  landmark: ['props'],
};

/** 类别 → 图层集合（未识别返回空数组）。 */
export function layersForCategory(category: string): CityLayerName[] {
  const key = (category || '').trim().toLowerCase();
  return CATEGORY_LAYER_MAP[key] ? [...CATEGORY_LAYER_MAP[key]] : [];
}

/**
 * 生效可见性 = 显式图层开关 ∧ 筛选允许。
 *
 * - filter.mode='highlight' ⇒ 只有命中类别的图层可见（其余隐藏 = 隔离高亮）；
 * - filter.mode='dim'       ⇒ 命中类别的图层隐藏（其余不变）。
 */
export function effectiveLayerVisible(
  layers: Record<CityLayerName, boolean>,
  filter: CityFilter | null,
  layer: CityLayerName,
): boolean {
  if (!layers[layer]) return false;
  if (!filter) return true;
  const hit = layersForCategory(filter.category).includes(layer);
  return filter.mode === 'dim' ? !hit : hit;
}
