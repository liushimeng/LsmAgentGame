/**
 * cityFilterCategories —— `set_filter` 的类别词汇与匹配（批次 85 §3.2.3）。
 *
 * 背景（批次 84 §8 边界 1）：`set_filter` 只有**图层粒度**（category 经
 * `CATEGORY_LAYER_MAP` 映射到 7 图层），「只高亮所有医院 / 所有出租车」做不到。
 * 批次 85 把 category 下沉到「类别 → 实例」粒度，词汇取自批次 28 `objectInfo`
 * 物件注册表的类别体系 + 车辆类型：
 *
 *   1. **图层名**（既有 `CATEGORY_LAYER_MAP`，零回归，优先级最高）；
 *   2. **物件类别**（`ObjectCategory` 枚举值：civic / landmark / road / edge /
 *      sky / ground / agent …）；
 *   3. **物件种类**（注册表条目 id 的全形 / 族前缀 / 种类后缀，如
 *      `civic.city-hall` / `park` / `bench` / `sedan` —— 车辆类型即
 *      `vehicle.<variant>` 的种类后缀）。
 *
 * 匹配规则（实例 tag 形如 objectInfo id `X.Y`）：`c === X.Y`（全形）或
 * `c === X`（族前缀，如 `park` 命中 `park.bench`）或 `c === Y`（种类后缀，
 * 如 `sedan` 命中 `vehicle.sedan`、`park` 命中 `tree.park`）或
 * `注册表条目.category === c`（枚举值，如 `agent` 命中 `actor.player`）。
 * ⚠ 宽匹配是刻意口径：`road` 会同时命中 `road.*` 与 `tree.road`（行道树）。
 *
 * 呈现（mode）：
 *   - `highlight`：命中实例保留显示，未命中**显隐为不渲染**；
 *   - `dim`：命中实例显隐为不渲染。
 * ⚠ 这是**退化实现**（明示不静默，走 `onNotice` 的 `filter_showhide` 提示）：
 * 引擎无安全的逐实例调色入口（GLB `clone(true)` 跨实例共享材质引用，逐实例
 * 调色须新建材质实例 —— 违反 draw call 纪律，CLAUDE.md 批次 28/46 教训），
 * 故不做到「命中保留原色、未命中降饱和」，退化为「按类别显隐」。
 *
 * 本文件是**纯叶模块**（只依赖 objectInfo 注册表数据）：store / adapter /
 * 场景组件三方共用同一份词汇，防止口径漂移。
 */

import {
  allObjectInfoEntries,
  getObjectInfo,
  type ObjectCategory,
} from './objectInfo/catalog';

// ── 图层词汇（批次 84 原样迁入；cityShowStore re-export 保持零回归）────

/**
 * 可切换显隐的图层名（与后端 DisplayCommand 的 `layer` 字段逐字一致）。
 *
 * 落点（游戏侧薄适配层）：
 *   buildings   → DistrictBlock 内的 <DistrictBuildings>
 *   trees       → StreetPropsLayer 内的 <TreesInstanced>
 *   vehicles    → StreetPropsLayer 内的车辆列表
 *   pedestrians → StreetPropsLayer 内的行人列表（装饰行人 + 真实居民）
 *   props       → 街具 / 标识 / 屋顶设备 / 公交站台 / 停车咪表
 *   labels      → 名牌 / 气泡 / 城区悬浮信息卡 / 标注（批次 85 补全）
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

/** 类别筛选模式（后端 DisplayCommand 的 `mode` 字段）。 */
export type CityFilterMode = 'highlight' | 'dim';

/** 类别筛选（`category` 为空 = 取消筛选）。 */
export interface CityFilter {
  category: string;
  mode: CityFilterMode;
}

/**
 * 类别 → 图层集合（批次 84 既有语义，**零回归**：命中键一律走图层级显隐）。
 *
 * 后端 `set_filter` 只给一个 `category` 字符串（自由文本），前端按**已知同义
 * 词**映射到图层集合；未识别的类别再走实例级词汇（见下），全不中 ⇒ 未识别。
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

// ── 实例级词汇（批次 85：objectInfo 注册表类别体系 + 车辆类型）────────

/** 实例 tag 的合法类别集合（小写）：注册表全形 id / 族前缀 / 种类后缀 / 枚举值。 */
const INSTANCE_VOCAB: ReadonlySet<string> = (() => {
  const out = new Set<string>();
  for (const e of allObjectInfoEntries()) {
    const id = e.id.toLowerCase();
    out.add(id); // 全形：civic.city-hall
    const dot = id.indexOf('.');
    if (dot > 0) {
      out.add(id.slice(0, dot)); // 族前缀：civic / park / vehicle …
      out.add(id.slice(dot + 1)); // 种类后缀：city-hall / bench / sedan …
    }
    out.add(e.category); // ObjectCategory 枚举值：agent / road / edge …
  }
  return out;
})();

/** 类别解析结果（`set_filter` 的 category → 三种归宿）。 */
export type CityFilterCategoryResolution =
  | { scope: 'layer'; layers: CityLayerName[] }
  | { scope: 'instance'; normalized: string }
  | { scope: 'unknown' };

/**
 * 解析 category（空串调用方先按「取消筛选」处理，本函数视为空未识别）。
 *
 * 优先级（零回归）：图层名 → 实例词汇（物件类别 / 物件种类 / 车辆类型）→ 未识别。
 */
export function resolveCityFilterCategory(raw: string): CityFilterCategoryResolution {
  const c = (raw ?? '').trim().toLowerCase();
  if (!c) return { scope: 'unknown' };
  const layers = CATEGORY_LAYER_MAP[c];
  if (layers) return { scope: 'layer', layers: [...layers] };
  if (INSTANCE_VOCAB.has(c)) return { scope: 'instance', normalized: c };
  return { scope: 'unknown' };
}

/**
 * 实例 tag（objectInfo id，如 `vehicle.sedan`）是否命中实例级类别 `c`。
 * 规则：全形 / 族前缀 / 种类后缀 / 注册表条目 category（见文件头）。
 */
export function cityFilterMatchesTag(c: string, tag: string): boolean {
  const t = (tag ?? '').trim().toLowerCase();
  if (!t || !c) return false;
  if (t === c) return true;
  const dot = t.indexOf('.');
  const family = dot > 0 ? t.slice(0, dot) : t;
  const kind = dot > 0 ? t.slice(dot + 1) : '';
  if (family === c || (kind && kind === c)) return true;
  return getObjectInfo(t)?.category === (c as ObjectCategory);
}

/**
 * 实例是否应渲染（`set_filter` 的实例级显隐判据，批次 85 退化口径）。
 *
 * - filter 空 / 图层级 category ⇒ 恒 true（图层级由 `effectiveLayerVisible` 收口）；
 * - `mode='highlight'` ⇒ 只渲染命中实例（隔离高亮）；
 * - `mode='dim'` ⇒ 隐藏命中实例。
 */
export function filterAllowsInstance(
  filter: CityFilter | null,
  tag: string,
): boolean {
  if (!filter) return true;
  const r = resolveCityFilterCategory(filter.category);
  if (r.scope !== 'instance') return true;
  const hit = cityFilterMatchesTag(r.normalized, tag);
  return filter.mode === 'dim' ? !hit : hit;
}

/**
 * 生效可见性 = 显式图层开关 ∧ 筛选允许（图层级）。
 *
 * - filter.mode='highlight' ⇒ 只有命中类别的图层可见（其余隐藏 = 隔离高亮）；
 * - filter.mode='dim'       ⇒ 命中类别的图层隐藏（其余不变）；
 * - 实例级 category ⇒ 整层不受影响（实例自裁，见 `filterAllowsInstance`）。
 */
export function effectiveLayerVisible(
  layers: Record<CityLayerName, boolean>,
  filter: CityFilter | null,
  layer: CityLayerName,
): boolean {
  if (!layers[layer]) return false;
  if (!filter) return true;
  const r = resolveCityFilterCategory(filter.category);
  if (r.scope !== 'layer') return true;
  const hit = r.layers.includes(layer);
  return filter.mode === 'dim' ? !hit : hit;
}
