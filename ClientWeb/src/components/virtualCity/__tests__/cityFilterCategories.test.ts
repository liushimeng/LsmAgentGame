/**
 * `set_filter` 类别 → 实例粒度单测（批次 85 §3.2.3）。
 *
 * 由 `scripts/verify_city_show_adapter.mjs` 经入口 import 打包执行
 * （npm run verify:city-show-adapter）；模块加载即自跑，失败抛错 ⇒ 非零退出。
 *
 * 覆盖（方案 §7.1）：
 *   1. category 词汇：图层名（既有，零回归）/ 物件类别（objectInfo 注册表）/
 *      车辆类型；未知类别 ⇒ `unknown_category`；空 = 取消；
 *   2. 实例匹配规则（全形 / 族前缀 / 种类后缀 / 注册表 category）；
 *   3. mode 语义（highlight = 只留命中 / dim = 藏命中）；
 *   4. 实例级筛选**明示不静默**：生效同时回调 `filter_showhide` 提示
 *      （退化为「按类别显隐」的口径披露）。
 */

import {
  CATEGORY_LAYER_MAP,
  cityFilterMatchesTag,
  effectiveLayerVisible,
  filterAllowsInstance,
  layersForCategory,
  resolveCityFilterCategory,
  type CityFilter,
} from '../cityFilterCategories';
import {
  applyDisplayCommand,
  type CityShowDeps,
  type CityShowNotice,
} from '../cityShowAdapter';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[cityFilterCategories] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function deepEq(actual: unknown, expected: unknown, msg: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`[cityFilterCategories] ${msg}: expected ${b}, got ${a}`);
  }
}

export function runCityFilterCategoriesTests(): void {
  // ── 1. category 词汇解析 ────────────────────────────────────────
  // 图层名（既有 16 键零回归：解析为 layer scope 且图层集合不变）。
  for (const [key, layers] of Object.entries(CATEGORY_LAYER_MAP)) {
    const r = resolveCityFilterCategory(key);
    eq(r.scope, 'layer', `图层词 ${key} 应解析为 layer scope`);
    if (r.scope === 'layer') deepEq(r.layers, layers, `图层词 ${key} 的图层集合应零回归`);
    deepEq(layersForCategory(key), layers, `layersForCategory(${key}) 应零回归`);
  }
  // 大小写 / 空白宽容。
  eq(resolveCityFilterCategory('  BUILDING ').scope, 'layer', '图层词应大小写宽容');

  // 物件类别 / 物件种类 / 车辆类型 ⇒ instance scope。
  for (const c of ['park', 'sedan', 'truck', 'bus', 'taxi', 'civic.city-hall', 'bench', 'agent', 'road', 'edge', 'sky', 'ground', 'prop']) {
    const r = resolveCityFilterCategory(c);
    eq(r.scope, 'instance', `类别 ${c} 应解析为 instance scope`);
    if (r.scope === 'instance') eq(r.normalized, c, `normalized 应为小写原词 ${c}`);
  }

  // 未知类别 ⇒ unknown（adapter 转 `unknown_category` 提示）。
  for (const c of ['hospital', 'school', 'nope', '火星车']) {
    eq(resolveCityFilterCategory(c).scope, 'unknown', `类别 ${c} 应为 unknown`);
  }
  eq(resolveCityFilterCategory('').scope, 'unknown', '空类别应为 unknown（取消语义由调用方先处理）');

  // ── 2. 实例匹配规则 ────────────────────────────────────────────
  // 全形。
  eq(cityFilterMatchesTag('civic.city-hall', 'civic.city-hall'), true, '全形应命中');
  // 种类后缀（车辆类型是 vehicle.<variant> 的后缀）。
  eq(cityFilterMatchesTag('sedan', 'vehicle.sedan'), true, 'sedan 应命中 vehicle.sedan');
  eq(cityFilterMatchesTag('sedan', 'vehicle.truck'), false, 'sedan 不应命中 vehicle.truck');
  eq(cityFilterMatchesTag('city-hall', 'civic.city-hall'), true, '种类后缀应命中');
  // 族前缀。
  eq(cityFilterMatchesTag('park', 'park.bench'), true, 'park 应命中 park.bench');
  eq(cityFilterMatchesTag('park', 'tree.park'), true, 'park 应命中 tree.park（后缀）');
  eq(cityFilterMatchesTag('park', 'civic.city-hall'), false, 'park 不应命中 city-hall');
  // 注册表 category（actor.player 的 category 是 agent）。
  eq(cityFilterMatchesTag('agent', 'actor.player'), true, 'agent 应命中 actor.player');
  eq(cityFilterMatchesTag('agent', 'actor.pedestrian'), false, 'agent 不应命中 actor.pedestrian');
  // 空 / 未知 tag。
  eq(cityFilterMatchesTag('park', ''), false, '空 tag 不应命中');
  eq(cityFilterMatchesTag('park', 'no.such.object'), false, '未知 tag 不应命中');

  // ── 3. mode 语义（退化口径 = 按类别显隐）────────────────────────
  const hi: CityFilter = { category: 'sedan', mode: 'highlight' };
  const dim: CityFilter = { category: 'sedan', mode: 'dim' };
  eq(filterAllowsInstance(hi, 'vehicle.sedan'), true, 'highlight 命中实例应显示');
  eq(filterAllowsInstance(hi, 'vehicle.truck'), false, 'highlight 未命中实例应隐藏');
  eq(filterAllowsInstance(dim, 'vehicle.sedan'), false, 'dim 命中实例应隐藏');
  eq(filterAllowsInstance(dim, 'vehicle.truck'), true, 'dim 未命中实例应显示');
  eq(filterAllowsInstance(null, 'vehicle.sedan'), true, '无筛选恒显示');
  // 图层级筛选不裁实例（由 effectiveLayerVisible 收口）。
  eq(filterAllowsInstance({ category: 'vehicle', mode: 'highlight' }, 'vehicle.sedan'), true, '图层级筛选不裁实例');

  // ── 4. effectiveLayerVisible 零回归 + 实例级不动整层 ─────────────
  const allOn = {
    buildings: true, trees: true, vehicles: true, pedestrians: true,
    props: true, labels: true, water: true,
  } as Record<'buildings' | 'trees' | 'vehicles' | 'pedestrians' | 'props' | 'labels' | 'water', boolean>;
  // 图层级 highlight：只留命中图层（批次 84 语义）。
  eq(effectiveLayerVisible(allOn, { category: 'vehicle', mode: 'highlight' }, 'vehicles'), true, '图层 highlight 命中应可见');
  eq(effectiveLayerVisible(allOn, { category: 'vehicle', mode: 'highlight' }, 'trees'), false, '图层 highlight 未命中应隐藏');
  // 图层级 dim：藏命中图层。
  eq(effectiveLayerVisible(allOn, { category: 'vehicle', mode: 'dim' }, 'vehicles'), false, '图层 dim 命中应隐藏');
  eq(effectiveLayerVisible(allOn, { category: 'vehicle', mode: 'dim' }, 'trees'), true, '图层 dim 未命中应可见');
  // 实例级 category：整层不受影响。
  eq(effectiveLayerVisible(allOn, { category: 'sedan', mode: 'highlight' }, 'vehicles'), true, '实例级筛选不动整层');
  eq(effectiveLayerVisible(allOn, { category: 'sedan', mode: 'highlight' }, 'buildings'), true, '实例级筛选不动整层(2)');
  // 显式图层开关优先。
  const someOff = { ...allOn, trees: false };
  eq(effectiveLayerVisible(someOff, null, 'trees'), false, '显式关闭优先于筛选');

  // ── 5. adapter：物件类别命中 / 未知类别 / 空 = 取消 ──────────────
  const filterCalls: { category: string; mode: string }[] = [];
  const notices: CityShowNotice[] = [];
  const deps: CityShowDeps = {
    focusRef: { current: null },
    setSelectedDistrict: () => {},
    setMode: () => {},
    setAnnotation: () => {},
    clearAnnotation: () => {},
    layerApi: {
      setLayerVisibility: () => true,
      // 与 SystemDialogPanel 真实接线同口径：resolve 不认识才拒绝。
      setFilter: (category, mode) => {
        if (category && resolveCityFilterCategory(category).scope === 'unknown') return false;
        filterCalls.push({ category, mode });
        return true;
      },
    },
    onNotice: (n) => notices.push(n),
  };
  const cmd = (p: Partial<VirtualCityDisplayCommand> & { type: 'set_filter' }) =>
    p as VirtualCityDisplayCommand;

  // 物件类别命中 ⇒ 生效 + 明示「按类别显隐」（退化披露，不静默）。
  const hit = applyDisplayCommand(cmd({ type: 'set_filter', category: 'park', mode: 'highlight' }), deps);
  eq(hit.applied, true, '物件类别命中应生效');
  deepEq(filterCalls, [{ category: 'park', mode: 'highlight' }], '应下发实例级筛选');
  eq(notices.length, 1, '实例级筛选应同时给出口径提示');
  eq(notices[0].code, 'filter_showhide', '口径提示应为 filter_showhide');
  eq(notices[0].detail, 'park', '口径提示应带类别');

  // 车辆类型命中 ⇒ 同样生效 + 明示。
  const v = applyDisplayCommand(cmd({ type: 'set_filter', category: 'sedan', mode: 'dim' }), deps);
  eq(v.applied, true, '车辆类型命中应生效');
  eq(notices.length, 2, '车辆类型筛选也应明示口径');

  // 图层名（既有语义）⇒ 生效且**不**追加口径提示（零回归）。
  const layer = applyDisplayCommand(cmd({ type: 'set_filter', category: 'building', mode: 'highlight' }), deps);
  eq(layer.applied, true, '图层名筛选应生效');
  eq(notices.length, 2, '图层名筛选不应追加口径提示');

  // 未知类别 ⇒ unknown_category 提示且无副作用。
  const before = filterCalls.length;
  const unknown = applyDisplayCommand(cmd({ type: 'set_filter', category: 'hospital' }), deps);
  eq(unknown.applied, false, '未知类别不应生效');
  eq(unknown.notice?.code, 'unknown_category', '未知类别应给出 unknown_category 提示');
  eq(filterCalls.length, before, '未知类别不应下发筛选');
  eq(notices.filter((n) => n.code === 'filter_showhide').length, 2, '未知类别不应产生口径提示');

  // 空 = 取消筛选（既有语义不变）。
  const cancel = applyDisplayCommand(cmd({ type: 'set_filter', category: '' }), deps);
  eq(cancel.applied, true, '空类别应为取消筛选');
  deepEq(
    filterCalls[filterCalls.length - 1],
    { category: '', mode: 'highlight' },
    '取消筛选应下发空类别',
  );

  // eslint-disable-next-line no-console
  console.log('[cityFilterCategories] 全部断言通过');
}

// 直接执行（被打包器当作入口 import）时自动运行。
runCityFilterCategoriesTests();
