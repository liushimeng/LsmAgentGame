/**
 * cityShowAdapter × 真实 cityEntityResolver 链路单测（批次 85 C2 修复）。
 *
 * 背景（C2 E2E 缺陷）：`focus_entity` 用无效 id（`vehicle:nope-b85`）时
 * 未出现 `entity_not_found` 提示。本文件用**真实** `cityEntityApi`
 * （不 mock resolve）驱动 `applyDisplayCommand`，钉死三条契约：
 *
 *   1. 无效 `vehicle:<id>` ⇒ `applied=false` + `notice.code='entity_not_found'`
 *      且回调 `onNotice`（显式提示，绝不静默）；
 *   2. 已注册车辆 ⇒ `applied=true`、focusRef 落点 = 注册坐标、**零** notice；
 *   3. 注册表直连 id / 无效 resident / 无效 building 同样走 `entity_not_found`。
 *
 * 零依赖断言写法与 cityShowAdapter.test.ts 同款（项目无 vitest）；
 * 模块加载即自跑，由 `scripts/verify_city_show_adapter.mjs` 打包执行。
 */

import {
  applyDisplayCommand,
  type CityShowDeps,
  type CityShowNotice,
} from '../cityShowAdapter';
import {
  cityEntityApi,
  clearEntityRegistry,
  setEntityPosition,
} from '../cityEntityResolver';
import {
  clearVehicleRegistry,
  vehicleRegistry,
} from '../cityVehicleRegistry';
import { resolveCityFilterCategory } from '../cityFilterCategories';
import { cityShowApi } from '../cityShowStore';
import type { VirtualCityDisplayCommand } from '@/types/virtualCitySystem';

// ── 零依赖断言 helper ────────────────────────────────────────

function eq(actual: unknown, expected: unknown, msg: string): void {
  if (actual !== expected) {
    throw new Error(`[cityShowAdapterEntity] ${msg}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function assert(cond: boolean, msg: string): void {
  if (!cond) throw new Error(`[cityShowAdapterEntity] ${msg}`);
}

// ── 记录型 deps（实体解析走**真实** cityEntityApi）────────────

function makeRealDeps(): {
  deps: CityShowDeps;
  focusRef: { current: { x: number; z: number } | null };
  notices: CityShowNotice[];
} {
  const focusRef: { current: { x: number; z: number } | null } = { current: null };
  const notices: CityShowNotice[] = [];
  const deps: CityShowDeps = {
    focusRef,
    setSelectedDistrict: () => undefined,
    setMode: () => undefined,
    setAnnotation: () => undefined,
    clearAnnotation: () => undefined,
    // 真实 resolver（cityEntityResolver 的导出句柄），零 mock。
    entityApi: cityEntityApi,
    onNotice: (n) => notices.push(n),
  };
  return { deps, focusRef, notices };
}

function run(cmd: VirtualCityDisplayCommand) {
  const h = makeRealDeps();
  const result = applyDisplayCommand(cmd, h.deps);
  return { result, ...h };
}

// ── 用例（模块加载即自跑）────────────────────────────────────

// 1. 无效车辆 id ⇒ entity_not_found 显式提示（C2 的决定性断言）。
{
  const { result, notices } = run({
    type: 'focus_entity',
    entity_id: 'vehicle:nope-b85',
    kind: 'vehicle',
  });
  eq(result.applied, false, '无效 vehicle id 必须 applied=false');
  eq(result.notice?.code, 'entity_not_found', 'notice.code');
  eq(result.notice?.detail, 'vehicle:nope-b85', 'notice.detail 应为原始实体 id');
  eq(notices.length, 1, 'onNotice 必须被调用恰好一次');
  eq(notices[0].code, 'entity_not_found', 'onNotice 的 code');
  eq(notices[0].detail, 'vehicle:nope-b85', 'onNotice 的 detail');
}

// 2. 已注册车辆 ⇒ 聚焦成功、零 notice（正向对照，防「一律报未找到」）。
{
  vehicleRegistry.register('sedan-7', { x: 5, z: 6 }, 'sedan');
  const { result, notices, focusRef } = run({
    type: 'focus_entity',
    entity_id: 'vehicle:sedan-7',
    kind: 'vehicle',
  });
  eq(result.applied, true, '已注册车辆必须 applied=true');
  eq(result.notice, undefined, '成功路径不得带 notice');
  eq(notices.length, 0, '成功路径不得回调 onNotice');
  eq(focusRef.current?.x, 5, 'focusRef.x 必须是注册坐标');
  eq(focusRef.current?.z, 6, 'focusRef.z 必须是注册坐标');
  vehicleRegistry.unregister('sedan-7');
}

// 3. 车辆注销后同 id 立即回落 entity_not_found（防残留坐标误命中）。
{
  vehicleRegistry.register('truck-1', { x: 9, z: 9 }, 'truck');
  vehicleRegistry.unregister('truck-1');
  const { result } = run({
    type: 'focus_entity',
    entity_id: 'vehicle:truck-1',
    kind: 'vehicle',
  });
  eq(result.applied, false, '注销后必须解析失败');
  eq(result.notice?.code, 'entity_not_found', '注销后 notice.code');
}

// 4. 注册表直连 id（非前缀形态）命中与未命中。
{
  setEntityPosition('my-object', 3, 4, 'T');
  const hit = run({ type: 'focus_entity', entity_id: 'my-object' });
  eq(hit.result.applied, true, '注册表直连 id 必须命中');
  eq(hit.focusRef.current?.x, 3, '直连 id 落点 x');
  clearEntityRegistry();
  const miss = run({ type: 'focus_entity', entity_id: 'my-object' });
  eq(miss.result.applied, false, '清注册表后必须解析失败');
  eq(miss.result.notice?.code, 'entity_not_found', '清注册表后 notice.code');
}

// 5. 无效 resident / building id 同样显式提示（不静默、不瞎聚）。
{
  const r = run({ type: 'focus_entity', entity_id: 'resident:999999', kind: 'resident' });
  eq(r.result.applied, false, '无效 resident 必须解析失败');
  eq(r.result.notice?.code, 'entity_not_found', '无效 resident notice.code');

  const b = run({ type: 'focus_entity', entity_id: 'building:nowhere:99', kind: 'building' });
  eq(b.result.applied, false, '无效 building 必须解析失败');
  eq(b.result.notice?.code, 'entity_not_found', '无效 building notice.code');
}

// 6. 空 / 缺 entity_id ⇒ invalid_params（与「未找到」严格区分）。
{
  const { result } = run({ type: 'focus_entity', entity_id: '' });
  eq(result.applied, false, '空 entity_id 必须 applied=false');
  eq(result.notice?.code, 'invalid_params', '空 entity_id notice.code');
}

// 7. entityApi 未接线 ⇒ unwired 显式提示（能力缺失不静默）。
{
  const focusRef: { current: { x: number; z: number } | null } = { current: null };
  const notices: CityShowNotice[] = [];
  const result = applyDisplayCommand(
    { type: 'focus_entity', entity_id: 'vehicle:nope-b85', kind: 'vehicle' },
    {
      focusRef,
      setSelectedDistrict: () => undefined,
      setMode: () => undefined,
      setAnnotation: () => undefined,
      clearAnnotation: () => undefined,
      onNotice: (n) => notices.push(n),
    },
  );
  eq(result.applied, false, '未接线必须 applied=false');
  eq(result.notice?.code, 'unwired', '未接线 notice.code');
  eq(notices.length, 1, '未接线必须回调 onNotice');
}

// 8. set_filter 提示契约（批次 85 C5/C6）：实例级 ⇒ filter_showhide 明示退化；
//    未识别 ⇒ unknown_category 显式提示；空 category = 取消筛选、零提示。
//    layerApi 封装与 SystemDialogPanel 逐字同口径（resolveCityFilterCategory 收口）。
{
  const notices: CityShowNotice[] = [];
  const filterCalls: { category: string; mode: string }[] = [];
  const deps: CityShowDeps = {
    focusRef: { current: null },
    setSelectedDistrict: () => undefined,
    setMode: () => undefined,
    setAnnotation: () => undefined,
    clearAnnotation: () => undefined,
    layerApi: {
      setLayerVisibility: () => true,
      setFilter: (category, mode) => {
        if (!category) {
          cityShowApi.clearFilter();
          return true;
        }
        if (resolveCityFilterCategory(category).scope === 'unknown') return false;
        cityShowApi.setFilter(category, mode);
        filterCalls.push({ category, mode });
        return true;
      },
    },
    onNotice: (n) => notices.push(n),
  };

  // 8a. 实例级类别（'sedan' = vehicle.sedan 的种类后缀）⇒ 生效 + filter_showhide 明示。
  assert(
    resolveCityFilterCategory('sedan').scope === 'instance',
    "'sedan' 必须解析为实例级类别（objectInfo 种类后缀）",
  );
  const inst = applyDisplayCommand(
    { type: 'set_filter', category: 'sedan', mode: 'highlight' },
    deps,
  );
  eq(inst.applied, true, '实例级 set_filter 必须生效');
  eq(notices.length, 1, '实例级 set_filter 必须回一条 filter_showhide 提示');
  eq(notices[0].code, 'filter_showhide', '实例级 set_filter 提示码');
  eq(notices[0].detail, 'sedan', '实例级 set_filter 提示 detail');

  // 8b. 未识别类别（'hospital'，E2E C6 契约）⇒ 不生效 + unknown_category。
  notices.length = 0;
  const unknown = applyDisplayCommand(
    { type: 'set_filter', category: 'hospital', mode: 'highlight' },
    deps,
  );
  eq(unknown.applied, false, '未识别类别必须 applied=false');
  eq(unknown.notice?.code, 'unknown_category', '未识别类别 notice.code');
  eq(notices.length, 1, '未识别类别必须回调 onNotice');
  eq(notices[0].detail, 'hospital', '未识别类别 detail');

  // 8c. 空 category = 取消筛选 ⇒ 生效且零提示。
  notices.length = 0;
  const cancel = applyDisplayCommand({ type: 'set_filter', category: '' }, deps);
  eq(cancel.applied, true, '空 category 必须按取消筛选生效');
  eq(notices.length, 0, '取消筛选不得回调 onNotice');

  cityShowApi.clearFilter();
}

// 清理（勿污染同进程其它用例）。
clearEntityRegistry();
clearVehicleRegistry();

// eslint-disable-next-line no-console
console.log('[cityShowAdapterEntity] all assertions passed');
